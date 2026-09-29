package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// workflowRequestRow is one durable request a Workflow run waits on.
// (workflow_run_id, request_key) makes opening it idempotent.
type workflowRequestRow struct {
	ID             uint64     `gorm:"primaryKey;autoIncrement"`
	PublicID       string     `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_workflow_request_public_id;not null"`
	WorkflowRunID  uint64     `gorm:"column:workflow_run_id;not null;uniqueIndex:uq_workflow_request_key,priority:1"`
	RequestKey     string     `gorm:"column:request_key;type:varchar(191);not null;uniqueIndex:uq_workflow_request_key,priority:2"`
	NodeRunID      uint64     `gorm:"column:node_run_id;not null;index"`
	Kind           string     `gorm:"type:varchar(16);not null"`
	Prompt         string     `gorm:"type:longtext;not null"`
	Questions      *string    `gorm:"type:text"`
	ResponseSchema *string    `gorm:"type:text"`
	TaskRunID      *uint64    `gorm:"column:task_run_id"`
	Status         string     `gorm:"type:varchar(16);not null;index"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
	Response       *string    `gorm:"type:longtext"`
	RespondedBy    *uint64    `gorm:"column:responded_by"`
	RespondedAt    *time.Time `gorm:"column:responded_at"`
	CreatedAt      time.Time  `gorm:"autoCreateTime"`
}

func (workflowRequestRow) TableName() string { return "workflow_request" }

type workflowRequestReadRow struct {
	Row                 workflowRequestRow `gorm:"embedded"`
	WorkflowRunPublicID string             `gorm:"column:workflow_run_public_id"`
	NodeRunPublicID     string             `gorm:"column:node_run_public_id"`
	NodeID              string             `gorm:"column:node_id"`
	TaskRunPublicID     *string            `gorm:"column:task_run_public_id"`
	RespondedByPublicID *string            `gorm:"column:responded_by_public_id"`
}

func workflowRequestSelectTx(tx *gorm.DB) *gorm.DB {
	return tx.Model(&workflowRequestRow{}).
		Select("workflow_request.*, wr.public_id AS workflow_run_public_id, nr.public_id AS node_run_public_id, " +
			"nr.node_id AS node_id, r.public_id AS task_run_public_id, u.public_id AS responded_by_public_id").
		Joins("INNER JOIN workflow_run wr ON wr.id = workflow_request.workflow_run_id").
		Joins("INNER JOIN workflow_node_run nr ON nr.id = workflow_request.node_run_id").
		Joins("LEFT JOIN task_run r ON r.id = workflow_request.task_run_id").
		Joins("LEFT JOIN `user` u ON u.id = workflow_request.responded_by")
}

func toWorkflowRequest(row *workflowRequestReadRow) *coreworkflow.Request {
	out := &coreworkflow.Request{
		ID:             row.Row.PublicID,
		WorkflowRunID:  row.WorkflowRunPublicID,
		NodeRunID:      row.NodeRunPublicID,
		NodeID:         row.NodeID,
		Kind:           row.Row.Kind,
		Prompt:         row.Row.Prompt,
		Questions:      row.Row.Questions,
		ResponseSchema: row.Row.ResponseSchema,
		TaskRunID:      row.TaskRunPublicID,
		Status:         row.Row.Status,
		ExpiresAt:      row.Row.ExpiresAt,
		Response:       row.Row.Response,
		RespondedBy:    row.RespondedByPublicID,
		RespondedAt:    row.Row.RespondedAt,
		CreatedAt:      row.Row.CreatedAt,
	}
	return out
}

func toWorkflowRequests(rows []workflowRequestReadRow) []coreworkflow.Request {
	out := make([]coreworkflow.Request, len(rows))
	for i := range rows {
		out[i] = *toWorkflowRequest(&rows[i])
	}
	return out
}

// OpenWorkflowRequest takes the run lock stop intent and Task admission use,
// so a request can only open on a run still running, and never behind a stop.
func (s *Store) OpenWorkflowRequest(ctx context.Context, in coreworkflow.OpenRequestInput) (*coreworkflow.Request, bool, error) {
	if !coreworkflow.ValidNodeRunTransition(in.NodeExpected, coreworkflow.NodeRunStatusWaiting) {
		return nil, false, coreworkflow.ErrInvalidNodeRunTransition
	}
	runID, ok := util.CanonicalPublicID(in.WorkflowRunID)
	if !ok {
		return nil, false, nil
	}
	nodeID, ok := util.CanonicalPublicID(in.NodeRunID)
	if !ok {
		return nil, false, nil
	}
	var requestID string
	opened := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run workflowRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", runID).Take(&run).Error; err != nil {
			return err
		}
		var existing workflowRequestRow
		err := tx.Where("workflow_run_id = ? AND request_key = ?", run.ID, in.Key).Take(&existing).Error
		if err == nil {
			requestID, opened = existing.PublicID, true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if run.Status != string(coreworkflow.RunStatusRunning) {
			return nil
		}
		var node workflowNodeRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("public_id = ? AND workflow_run_id = ?", nodeID, run.ID).Take(&node).Error; err != nil {
			return err
		}
		if node.Status != string(in.NodeExpected) {
			return nil
		}
		taskRunKey, err := optionalKey(ctx, tx, "task_run", in.TaskRunID)
		if err != nil {
			return err
		}
		row := &workflowRequestRow{
			WorkflowRunID:  run.ID,
			RequestKey:     in.Key,
			NodeRunID:      node.ID,
			Kind:           in.Kind,
			Prompt:         in.Prompt,
			Questions:      in.Questions,
			ResponseSchema: in.ResponseSchema,
			TaskRunID:      taskRunKey,
			Status:         coreworkflow.RequestStatusPending,
			ExpiresAt:      in.ExpiresAt,
			CreatedAt:      in.Now,
		}
		if err := createWithPublicID(ctx, tx, "uq_workflow_request_public_id", func(id string) { row.PublicID = id }, row); err != nil {
			return err
		}
		nodeUpdates := map[string]any{"status": string(coreworkflow.NodeRunStatusWaiting), "deadline_at": nil}
		if in.ResolvedInput != nil {
			nodeUpdates["resolved_input"] = *in.ResolvedInput
		}
		if node.StartedAt == nil {
			nodeUpdates["started_at"] = in.Now
		}
		if err := tx.Model(&workflowNodeRunRow{}).Where("id = ?", node.ID).Updates(nodeUpdates).Error; err != nil {
			return err
		}
		requestID, opened = row.PublicID, true
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, apierr.ErrNotFound
	}
	if err != nil || !opened {
		return nil, false, err
	}
	req, err := s.GetWorkflowRequest(ctx, requestID)
	return req, req != nil, err
}

func (s *Store) ResolveWorkflowRequest(ctx context.Context, in coreworkflow.ResolveRequestInput) (bool, error) {
	switch in.Status {
	case coreworkflow.RequestStatusAnswered, coreworkflow.RequestStatusDeclined, coreworkflow.RequestStatusExpired:
	default:
		return false, errors.New("a request resolves only to answered, declined, or expired")
	}
	id, ok := util.CanonicalPublicID(in.RequestID)
	if !ok {
		return false, nil
	}
	updates := map[string]any{"status": in.Status}
	q := s.db.WithContext(ctx).Model(&workflowRequestRow{}).
		Where("public_id = ? AND status = ?", id, coreworkflow.RequestStatusPending)
	if in.Status == coreworkflow.RequestStatusExpired {
		q = q.Where("expires_at IS NOT NULL AND expires_at <= ?", in.Now)
	} else {
		q = q.Where("expires_at IS NULL OR expires_at > ?", in.Now)
		updates["responded_at"] = in.Now
		if in.Response != nil {
			updates["response"] = *in.Response
		}
		responder, err := optionalKey(ctx, s.db, "user", in.RespondedBy)
		if err != nil {
			return false, err
		}
		updates["responded_by"] = responder
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (s *Store) GetWorkflowRequest(ctx context.Context, requestID string) (*coreworkflow.Request, error) {
	id, ok := util.CanonicalPublicID(requestID)
	if !ok {
		return nil, nil
	}
	var row workflowRequestReadRow
	err := workflowRequestSelectTx(s.db.WithContext(ctx)).Where("workflow_request.public_id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toWorkflowRequest(&row), nil
}

func (s *Store) ListWorkflowRequestsByRun(ctx context.Context, workflowRunID string) ([]coreworkflow.Request, error) {
	id, ok := util.CanonicalPublicID(workflowRunID)
	if !ok {
		return nil, nil
	}
	var rows []workflowRequestReadRow
	err := workflowRequestSelectTx(s.db.WithContext(ctx)).Where("wr.public_id = ?", id).
		Order("workflow_request.id ASC").Find(&rows).Error
	return toWorkflowRequests(rows), err
}

func (s *Store) ListPendingWorkflowRequestsBySpace(ctx context.Context, spaceID string, limit, offset int) ([]coreworkflow.Request, int, error) {
	limit, offset = capPage(limit, offset)
	id, ok := util.CanonicalPublicID(spaceID)
	if !ok {
		return nil, 0, nil
	}
	scope := func(q *gorm.DB) *gorm.DB {
		return q.Joins("INNER JOIN workflow w ON w.id = wr.workflow_id").
			Joins("INNER JOIN space sp ON sp.id = w.space_id").
			Where("sp.public_id = ? AND workflow_request.status = ?", id, coreworkflow.RequestStatusPending)
	}
	var total int64
	if err := scope(s.db.WithContext(ctx).Model(&workflowRequestRow{}).
		Joins("INNER JOIN workflow_run wr ON wr.id = workflow_request.workflow_run_id")).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	base := func() *gorm.DB { return scope(workflowRequestSelectTx(s.db.WithContext(ctx))) }
	var rows []workflowRequestReadRow
	if err := base().Order("workflow_request.id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return toWorkflowRequests(rows), int(total), nil
}
