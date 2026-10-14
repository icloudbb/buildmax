package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"

	"gorm.io/gorm"
)

// An Issue's latest run is the newest of its Agent runs and Workflow runs, in
// the order the Issue's own run list merges them: a Task a Workflow step
// dispatched is not an Agent run of the Issue, and a Task and a Workflow run
// created in the same instant count the Task as the newer. Each condition is
// correlated on the outer `issue` row and repeated in SELECT and WHERE, since
// MySQL cannot filter on a select alias.
var (
	// issueAgentRun matches alias as one of the Issue's own Agent runs.
	issueAgentRun = func(alias string) string {
		return fmt.Sprintf("%[1]s.issue_id = issue.id AND NOT EXISTS "+
			"(SELECT 1 FROM workflow_node_run %[1]s_step WHERE %[1]s_step.task_id = %[1]s.id)", alias)
	}
	latestAgentRun = "SELECT 1 FROM task lt WHERE " + issueAgentRun("lt") +
		" AND NOT EXISTS (SELECT 1 FROM task nt WHERE " + issueAgentRun("nt") +
		" AND (nt.created_at > lt.created_at OR (nt.created_at = lt.created_at AND nt.id > lt.id)))" +
		" AND NOT EXISTS (SELECT 1 FROM workflow_run nw WHERE nw.issue_id = issue.id AND nw.created_at > lt.created_at)"
	latestRunFailed = "EXISTS (" + latestAgentRun + " AND lt.status = '" + string(coretask.RunStatusFailed) + "')" +
		" OR EXISTS (SELECT 1 FROM workflow_run lw WHERE lw.issue_id = issue.id" +
		" AND lw.status = '" + string(coreworkflow.RunStatusFailed) + "'" +
		" AND NOT EXISTS (SELECT 1 FROM workflow_run nw WHERE nw.issue_id = issue.id" +
		" AND (nw.created_at > lw.created_at OR (nw.created_at = lw.created_at AND nw.id > lw.id)))" +
		" AND NOT EXISTS (SELECT 1 FROM task nt WHERE " + issueAgentRun("nt") + " AND nt.created_at >= lw.created_at))"
	latestRunAwaiting = "EXISTS (" + latestAgentRun + " AND lt.awaiting_answer = TRUE)"
	pendingRequest    = "EXISTS (SELECT 1 FROM workflow_request pr INNER JOIN workflow_run pw ON pw.id = pr.workflow_run_id" +
		" WHERE pw.issue_id = issue.id AND pr.status = '" + coreworkflow.RequestStatusPending + "')"
)

type issueAttentionRow struct {
	issueReadRow   `gorm:"embedded"`
	RunFailed      bool `gorm:"column:attention_run_failed"`
	RunAwaiting    bool `gorm:"column:attention_run_awaiting"`
	PendingRequest bool `gorm:"column:attention_pending_request"`
}

// ListIssuesNeedingOwner returns the Space's open Issues one person owns whose
// latest run failed or awaits an answer, or that have a pending Workflow
// request. A done Issue is left out: marking it done is how its owner says it
// needs nothing more.
func (s *Store) ListIssuesNeedingOwner(ctx context.Context, spaceID, ownerID string, limit int) ([]coreissue.Attention, int, error) {
	limit, _ = clampPage(limit, 0)
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	ownerKey, err := lookupKey(ctx, s.db, "user", ownerID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	scope := func(q *gorm.DB) *gorm.DB {
		return q.Where("issue.space_id = ? AND issue.owner_id = ? AND issue.status <> ?", spaceKey, ownerKey, coreissue.StatusDone).
			Where("((" + latestRunFailed + ") OR (" + latestRunAwaiting + ") OR (" + pendingRequest + "))")
	}
	var total int64
	if err := scope(s.db.WithContext(ctx).Model(&issueRow{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []issueAttentionRow
	err = scope(s.issueSelect(ctx).Select(issueColumns +
		", (" + latestRunFailed + ") AS attention_run_failed" +
		", (" + latestRunAwaiting + ") AS attention_run_awaiting" +
		", (" + pendingRequest + ") AS attention_pending_request")).
		Order("issue.updated_at DESC, issue.id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]coreissue.Attention, len(rows))
	for i := range rows {
		out[i] = coreissue.Attention{Issue: *toIssue(&rows[i].issueReadRow), Reason: attentionReason(rows[i])}
	}
	return out, int(total), nil
}

// attentionReason names one reason when several hold, the one that most
// directly asks something of the owner first.
func attentionReason(row issueAttentionRow) coreissue.AttentionReason {
	switch {
	case row.RunAwaiting:
		return coreissue.AttentionAwaitingAnswer
	case row.PendingRequest:
		return coreissue.AttentionWorkflowRequest
	default:
		return coreissue.AttentionRunFailed
	}
}
