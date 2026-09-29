package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Serialize Task admission and its node link with Workflow stop intent. Without
// this transaction a crashed or stale dispatcher can leave an executing Task
// behind a blocked node, invisible to cancellation and restart recovery.
func (s *Store) admitWorkflowNodeTask(ctx context.Context, in *coretask.CreateInput) (*coretask.Task, error) {
	var result *coretask.Task
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node workflowNodeRunRow
		if err := tx.Where("public_id = ?", in.WorkflowNodeRunID).Take(&node).Error; err != nil {
			return err
		}
		var run workflowRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", node.WorkflowRunID).Take(&run).Error; err != nil {
			return err
		}
		// A locking read sees the winner even under MySQL repeatable-read.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", node.ID).Take(&node).Error; err != nil {
			return err
		}
		var wf workflowRow
		if err := tx.Where("id = ?", run.WorkflowID).Take(&wf).Error; err != nil {
			return err
		}
		spaceID, err := publicIDForKey(ctx, tx, "space", wf.SpaceID)
		if err != nil {
			return err
		}
		if in.SpaceID != spaceID || in.AdmissionKey != coreworkflow.TaskAdmissionKey(run.PublicID, node.NodeID) {
			return apierr.ErrNotFound
		}
		if node.TaskID != nil {
			var existing taskRow
			if err := tx.Where("id = ?", *node.TaskID).Take(&existing).Error; err != nil {
				return err
			}
			if existing.AdmissionFingerprint == nil || *existing.AdmissionFingerprint != coretask.AdmissionFingerprint(in) {
				return coretask.ErrTaskAdmissionConflict
			}
			var err error
			result, err = (&Store{db: tx}).GetTask(ctx, existing.PublicID)
			return err
		}
		if run.Status != string(coreworkflow.RunStatusRunning) || node.Status != string(coreworkflow.NodeRunStatusPending) {
			return apierr.New(apierr.KindConflict, "workflow is stopping or node is no longer pending")
		}
		taskRow, taskRun := newTaskRows(in)
		if err := createTaskAndRunTx(ctx, tx, in, taskRow, taskRun); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&workflowNodeRunRow{}).Where("id = ?", node.ID).Updates(map[string]any{
			"status": string(coreworkflow.NodeRunStatusRunning), "task_id": taskRow.ID, "task_run_id": taskRun.ID,
			"resolved_input": in.Input, "started_at": now,
			"attempt": 1, "deadline_at": coreworkflow.Deadline(now, node.TimeoutSeconds),
		}).Error; err != nil {
			return err
		}
		result = createdTask(in, taskRow, taskRun)
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apierr.ErrNotFound
	}
	return result, err
}

// admitWorkflowNodeRetryRun creates a node's next TaskRun -- a retry attempt or
// an answered question's continuation -- and links it onto the node under the
// run lock, for the same reason admitWorkflowNodeTask does: a run admitted
// after stop intent would execute behind a canceled node.
// Locks are taken run, node, then task -- the order first admission uses.
func (s *Store) admitWorkflowNodeRetryRun(ctx context.Context, canonicalTaskID string, in coretask.CreateRunInput) (*coretask.Run, error) {
	var created *createdTaskRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node workflowNodeRunRow
		if err := tx.Where("public_id = ?", in.WorkflowNodeRunID).Take(&node).Error; err != nil {
			return err
		}
		var run workflowRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", node.WorkflowRunID).Take(&run).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", node.ID).Take(&node).Error; err != nil {
			return err
		}
		taskKey, err := lookupKey(ctx, tx, "task", canonicalTaskID)
		if err != nil {
			return err
		}
		if node.TaskID == nil || *node.TaskID != taskKey || in.IdempotencyKey == nil ||
			!strings.HasPrefix(*in.IdempotencyKey, coreworkflow.TaskAdmissionKey(run.PublicID, node.NodeID)+"/") {
			return apierr.ErrNotFound
		}
		// A repeated admission of the run already linked returns it.
		if node.TaskRunID != nil {
			var existing taskRunReadRow
			if err := taskRunSelectTx(tx).Where("task_run.id = ?", *node.TaskRunID).Take(&existing).Error; err != nil {
				return err
			}
			if existing.Row.IdempotencyKey != nil && *existing.Row.IdempotencyKey == *in.IdempotencyKey {
				created = &createdTaskRun{row: &existing.Row, previous: existing.PreviousPublicID, retryOf: existing.RetryOfPublicID, sourceMessage: existing.SourceMessagePublicID}
				return nil
			}
		}
		wantAttempt := in.WorkflowAttempt
		if in.WorkflowNodeFrom == string(coreworkflow.NodeRunStatusRetryWait) {
			wantAttempt--
		}
		if run.Status != string(coreworkflow.RunStatusRunning) || node.Status != in.WorkflowNodeFrom || node.Attempt != wantAttempt {
			return apierr.New(apierr.KindConflict, "workflow is stopping or node is not waiting for this run")
		}
		created, err = createTaskRunTx(ctx, tx, canonicalTaskID, in)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&workflowNodeRunRow{}).Where("id = ?", node.ID).Updates(map[string]any{
			"status": string(coreworkflow.NodeRunStatusRunning), "task_run_id": created.row.ID,
			"attempt": in.WorkflowAttempt, "deadline_at": coreworkflow.Deadline(now, node.TimeoutSeconds),
			"next_attempt_at": nil, "error_message": nil,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apierr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return created.toRun(canonicalTaskID), nil
}
