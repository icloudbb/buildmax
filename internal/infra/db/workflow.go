package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"

	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

type workflowRow struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	PublicID    string    `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_workflow_public_id;not null"`
	SpaceID     uint64    `gorm:"column:space_id;not null;index"`
	Name        string    `gorm:"type:varchar(255);not null"`
	Description string    `gorm:"type:text;not null"`
	Definition  string    `gorm:"type:longtext;not null"`
	Status      string    `gorm:"type:varchar(32);not null;default:'draft'"`
	Revision    int       `gorm:"column:revision;not null;default:1"`
	CreatedBy   uint64    `gorm:"column:created_by;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (workflowRow) TableName() string { return "workflow" }

// workflowReadRow is the row plus the handles its references resolve to.
type workflowReadRow struct {
	Row               workflowRow `gorm:"embedded"`
	SpacePublicID     string      `gorm:"column:space_public_id"`
	CreatedByPublicID string      `gorm:"column:created_by_public_id"`
}

func (s *Store) workflowSelect(ctx context.Context) *gorm.DB {
	return workflowSelectTx(s.db.WithContext(ctx))
}

func workflowSelectTx(tx *gorm.DB) *gorm.DB {
	return tx.Model(&workflowRow{}).
		Select("workflow.*, t.public_id AS space_public_id, cb.public_id AS created_by_public_id").
		Joins("INNER JOIN space t ON t.id = workflow.space_id").
		Joins("INNER JOIN `user` cb ON cb.id = workflow.created_by")
}

// workflowRevisionRow is one recorded version of a workflow. Rows are appended,
// never updated or deleted.
type workflowRevisionRow struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	WorkflowID  uint64    `gorm:"column:workflow_id;not null;index:idx_workflow_revision,unique,priority:1"`
	Revision    int       `gorm:"column:revision;not null;index:idx_workflow_revision,unique,priority:2"`
	Name        string    `gorm:"type:varchar(255);not null"`
	Description string    `gorm:"type:text;not null"`
	Definition  string    `gorm:"type:longtext;not null"`
	Status      string    `gorm:"type:varchar(32);not null"`
	CreatedBy   uint64    `gorm:"column:created_by;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (workflowRevisionRow) TableName() string { return "workflow_revision" }

// workflowRevisionReadRow is the row plus the handles its references resolve
// to. Like an agent revision, it has none of its own.
type workflowRevisionReadRow struct {
	Row               workflowRevisionRow `gorm:"embedded"`
	WorkflowPublicID  string              `gorm:"column:workflow_public_id"`
	CreatedByPublicID string              `gorm:"column:created_by_public_id"`
}

func (s *Store) workflowRevisionSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&workflowRevisionRow{}).
		Select("workflow_revision.*, w.public_id AS workflow_public_id, cb.public_id AS created_by_public_id").
		Joins("INNER JOIN workflow w ON w.id = workflow_revision.workflow_id").
		Joins("INNER JOIN `user` cb ON cb.id = workflow_revision.created_by")
}

type workflowRunRow struct {
	ID               uint64  `gorm:"primaryKey;autoIncrement"`
	PublicID         string  `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_workflow_run_public_id;not null"`
	WorkflowID       uint64  `gorm:"column:workflow_id;not null;index:idx_workflow_run_workflow_created,priority:1"`
	WorkflowRevision int     `gorm:"column:workflow_revision;not null;default:0"`
	IssueID          *uint64 `gorm:"column:issue_id;index"`
	// Input is the run's immutable input JSON, validated against the definition's
	// input_schema at admission. NULL when the definition declares no input_schema.
	Input        *string    `gorm:"column:input;type:longtext"`
	Status       string     `gorm:"type:varchar(32);not null"`
	CreatedBy    uint64     `gorm:"column:created_by;not null"`
	CreatedAt    time.Time  `gorm:"autoCreateTime;index:idx_workflow_run_workflow_created,priority:2"`
	StartedAt    *time.Time `gorm:""`
	EndedAt      *time.Time `gorm:""`
	ErrorMessage *string    `gorm:"type:text"`
	// Reconciliation lease and schedule. The due query walks
	// idx_workflow_run_next_reconcile; idx_workflow_run_lease_expires supports the
	// expired-lease takeover branch of the same query. All three are nulled when
	// the run reaches a terminal status.
	ReconcileOwner  *string    `gorm:"column:reconcile_owner;type:varchar(64)"`
	LeaseExpiresAt  *time.Time `gorm:"column:lease_expires_at;type:datetime(6);index:idx_workflow_run_lease_expires"`
	NextReconcileAt *time.Time `gorm:"column:next_reconcile_at;type:datetime(6);index:idx_workflow_run_next_reconcile"`
}

func (workflowRunRow) TableName() string { return "workflow_run" }

// workflowRunReadRow is the row plus the handles its references resolve to. A
// pointer field is one a LEFT JOIN may leave NULL.
type workflowRunReadRow struct {
	Row               workflowRunRow `gorm:"embedded"`
	WorkflowPublicID  string         `gorm:"column:workflow_public_id"`
	IssuePublicID     *string        `gorm:"column:issue_public_id"`
	CreatedByPublicID string         `gorm:"column:created_by_public_id"`
}

func (s *Store) workflowRunSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&workflowRunRow{}).
		Select("workflow_run.*, w.public_id AS workflow_public_id, i.public_id AS issue_public_id, " +
			"cb.public_id AS created_by_public_id").
		Joins("INNER JOIN workflow w ON w.id = workflow_run.workflow_id").
		Joins("LEFT JOIN issue i ON i.id = workflow_run.issue_id").
		Joins("INNER JOIN `user` cb ON cb.id = workflow_run.created_by")
}

type workflowStepRunRow struct {
	ID            uint64  `gorm:"primaryKey;autoIncrement"`
	PublicID      string  `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_workflow_step_run_public_id;not null"`
	WorkflowRunID uint64  `gorm:"column:workflow_run_id;not null;index:idx_step_run_run_index,priority:1"`
	StepID        string  `gorm:"column:step_id;type:varchar(128);not null"`
	StepIndex     int     `gorm:"column:step_index;not null;index:idx_step_run_run_index,priority:2"`
	StepType      string  `gorm:"column:step_type;type:varchar(32);not null"`
	TargetAgentID *uint64 `gorm:"column:target_agent_id;index"`
	// Agent definition captured when the run started; empty on rows written before
	// step runs snapshotted their agent.
	AgentName         string `gorm:"column:agent_name;type:varchar(255);not null"`
	AgentDescription  string `gorm:"column:agent_description;type:text;not null"`
	AgentInstructions string `gorm:"column:agent_instructions;type:longtext;not null"`
	AgentRevision     int    `gorm:"column:agent_revision;not null;default:0"`
	Prompt            string `gorm:"type:text;not null"`
	// Bindings is the run's snapshot of this step's input bindings as a JSON
	// array, NULL when the step binds nothing.
	Bindings *string `gorm:"type:text"`
	// OutputSchema is the run's snapshot of this step's output schema (JSON text),
	// NULL for a free-text step.
	OutputSchema  *string `gorm:"type:text"`
	Status        string  `gorm:"type:varchar(32);not null"`
	TaskID        *uint64 `gorm:"column:task_id;index"`
	TaskRunID     *uint64 `gorm:"column:task_run_id;index"`
	OutputSummary *string `gorm:"type:text"`
	// Structured is the validated structured-output value the accepted run
	// produced, as JSON text; NULL for a free-text step or a failed validation.
	Structured   *string    `gorm:"type:text"`
	ErrorMessage *string    `gorm:"type:text"`
	CreatedAt    time.Time  `gorm:"autoCreateTime"`
	StartedAt    *time.Time `gorm:""`
	EndedAt      *time.Time `gorm:""`
}

func (workflowStepRunRow) TableName() string { return "workflow_step_run" }

// workflowStepRunReadRow is the row plus the handles its references resolve to.
// A pointer field is one a LEFT JOIN may leave NULL.
type workflowStepRunReadRow struct {
	Row                 workflowStepRunRow `gorm:"embedded"`
	WorkflowRunPublicID string             `gorm:"column:workflow_run_public_id"`
	TargetAgentPublicID *string            `gorm:"column:target_agent_public_id"`
	TaskPublicID        *string            `gorm:"column:task_public_id"`
	TaskRunPublicID     *string            `gorm:"column:task_run_public_id"`
}

func (s *Store) workflowStepRunSelect(ctx context.Context) *gorm.DB {
	return workflowStepRunSelectTx(s.db.WithContext(ctx))
}

func workflowStepRunSelectTx(tx *gorm.DB) *gorm.DB {
	return tx.Model(&workflowStepRunRow{}).
		Select("workflow_step_run.*, wr.public_id AS workflow_run_public_id, a.public_id AS target_agent_public_id, " +
			"t.public_id AS task_public_id, r.public_id AS task_run_public_id").
		Joins("INNER JOIN workflow_run wr ON wr.id = workflow_step_run.workflow_run_id").
		Joins("LEFT JOIN agent a ON a.id = workflow_step_run.target_agent_id").
		Joins("LEFT JOIN task t ON t.id = workflow_step_run.task_id").
		Joins("LEFT JOIN task_run r ON r.id = workflow_step_run.task_run_id")
}

func toWorkflow(row *workflowReadRow) *coreworkflow.Workflow {
	if row == nil {
		return nil
	}
	return &coreworkflow.Workflow{
		ID:          row.Row.PublicID,
		SpaceID:     row.SpacePublicID,
		Name:        row.Row.Name,
		Description: row.Row.Description,
		Definition:  row.Row.Definition,
		Status:      row.Row.Status,
		Revision:    row.Row.Revision,
		CreatedBy:   row.CreatedByPublicID,
		CreatedAt:   row.Row.CreatedAt,
		UpdatedAt:   row.Row.UpdatedAt,
	}
}

func toWorkflows(rows []workflowReadRow) []coreworkflow.Workflow {
	out := make([]coreworkflow.Workflow, len(rows))
	for i := range rows {
		out[i] = *toWorkflow(&rows[i])
	}
	return out
}

func toWorkflowRevision(row *workflowRevisionReadRow) *coreworkflow.Revision {
	if row == nil {
		return nil
	}
	return &coreworkflow.Revision{
		WorkflowID:  row.WorkflowPublicID,
		Revision:    row.Row.Revision,
		Name:        row.Row.Name,
		Description: row.Row.Description,
		Definition:  row.Row.Definition,
		Status:      row.Row.Status,
		CreatedBy:   row.CreatedByPublicID,
		CreatedAt:   row.Row.CreatedAt,
	}
}

func toWorkflowRevisions(rows []workflowRevisionReadRow) []coreworkflow.Revision {
	out := make([]coreworkflow.Revision, len(rows))
	for i := range rows {
		out[i] = *toWorkflowRevision(&rows[i])
	}
	return out
}

func toWorkflowRun(row *workflowRunReadRow) *coreworkflow.Run {
	if row == nil {
		return nil
	}
	out := &coreworkflow.Run{
		ID:               row.Row.PublicID,
		WorkflowID:       row.WorkflowPublicID,
		WorkflowRevision: row.Row.WorkflowRevision,
		Input:            row.Row.Input,
		Status:           row.Row.Status,
		CreatedBy:        row.CreatedByPublicID,
		CreatedAt:        row.Row.CreatedAt,
		StartedAt:        row.Row.StartedAt,
		EndedAt:          row.Row.EndedAt,
		ErrorMessage:     row.Row.ErrorMessage,
		ReconcileOwner:   row.Row.ReconcileOwner,
		LeaseExpiresAt:   row.Row.LeaseExpiresAt,
		NextReconcileAt:  row.Row.NextReconcileAt,
	}
	if row.Row.IssueID != nil {
		issue := derefPublicID(row.IssuePublicID)
		out.IssueID = &issue
	}
	return out
}

func toWorkflowRuns(rows []workflowRunReadRow) []coreworkflow.Run {
	out := make([]coreworkflow.Run, len(rows))
	for i := range rows {
		out[i] = *toWorkflowRun(&rows[i])
	}
	return out
}

func toWorkflowStepRun(row *workflowStepRunReadRow) *coreworkflow.StepRun {
	if row == nil {
		return nil
	}
	out := &coreworkflow.StepRun{
		ID:                row.Row.PublicID,
		WorkflowRunID:     row.WorkflowRunPublicID,
		StepID:            row.Row.StepID,
		StepIndex:         row.Row.StepIndex,
		StepType:          row.Row.StepType,
		AgentName:         row.Row.AgentName,
		AgentDescription:  row.Row.AgentDescription,
		AgentInstructions: row.Row.AgentInstructions,
		AgentRevision:     row.Row.AgentRevision,
		Prompt:            row.Row.Prompt,
		Bindings:          decodeStepBindings(row.Row.Bindings),
		OutputSchema:      row.Row.OutputSchema,
		Status:            row.Row.Status,
		OutputSummary:     row.Row.OutputSummary,
		Structured:        row.Row.Structured,
		ErrorMessage:      row.Row.ErrorMessage,
		CreatedAt:         row.Row.CreatedAt,
		StartedAt:         row.Row.StartedAt,
		EndedAt:           row.Row.EndedAt,
	}
	if row.Row.TargetAgentID != nil {
		agent := derefPublicID(row.TargetAgentPublicID)
		out.TargetAgentID = &agent
	}
	if row.Row.TaskID != nil {
		task := derefPublicID(row.TaskPublicID)
		out.TaskID = &task
	}
	if row.Row.TaskRunID != nil {
		run := derefPublicID(row.TaskRunPublicID)
		out.TaskRunID = &run
	}
	return out
}

// encodeStepBindings serializes a step's snapshotted bindings to the JSON text
// the row stores, or nil when the step binds nothing so the column stays NULL.
func encodeStepBindings(bindings []coreworkflow.StepBinding) *string {
	if len(bindings) == 0 {
		return nil
	}
	encoded, err := json.Marshal(bindings)
	if err != nil {
		// StepBinding is two strings; marshalling it cannot fail. Treat an
		// impossible error as no bindings rather than panicking a store write.
		return nil
	}
	return util.Ptr(string(encoded))
}

// decodeStepBindings parses the stored bindings JSON. A NULL, empty, or
// unparseable column yields no bindings.
func decodeStepBindings(encoded *string) []coreworkflow.StepBinding {
	if encoded == nil || *encoded == "" {
		return nil
	}
	var bindings []coreworkflow.StepBinding
	if err := json.Unmarshal([]byte(*encoded), &bindings); err != nil {
		return nil
	}
	return bindings
}

func toWorkflowStepRuns(rows []workflowStepRunReadRow) []coreworkflow.StepRun {
	out := make([]coreworkflow.StepRun, len(rows))
	for i := range rows {
		out[i] = *toWorkflowStepRun(&rows[i])
	}
	return out
}

func (s *Store) ListWorkflowsBySpace(ctx context.Context, spaceID string) ([]coreworkflow.Workflow, error) {
	id, ok := util.CanonicalPublicID(spaceID)
	if !ok {
		return nil, nil
	}
	var list []workflowReadRow
	err := s.workflowSelect(ctx).Where("t.public_id = ?", id).Order("workflow.created_at ASC").Find(&list).Error
	return toWorkflows(list), err
}

func (s *Store) CreateWorkflow(ctx context.Context, spaceID, createdBy, name, description, definition string) (*coreworkflow.Workflow, error) {
	now := time.Now().UTC()
	workflow := &coreworkflow.Workflow{
		SpaceID:     spaceID,
		Name:        name,
		Description: description,
		Definition:  definition,
		Status:      coreworkflow.StatusDraft,
		Revision:    1,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	row := &workflowRow{
		Name:        name,
		Description: description,
		Definition:  definition,
		Status:      coreworkflow.StatusDraft,
		Revision:    1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", spaceID)
		if err != nil {
			return err
		}
		row.SpaceID = spaceKey
		creator, err := lookupKey(ctx, tx, "user", createdBy)
		if err != nil {
			return err
		}
		row.CreatedBy = creator
		if err := createWithPublicID(ctx, tx, "uq_workflow_public_id",
			func(id string) { row.PublicID = id }, row); err != nil {
			return err
		}
		return appendWorkflowRevision(tx, row.ID, creator, workflow)
	})
	if err != nil {
		return nil, err
	}
	workflow.ID = row.PublicID
	return workflow, nil
}

// appendWorkflowRevision records the workflow's current content as its
// revision. It runs in the same transaction as the write it describes, and the
// unique (workflow_id, revision) index makes a concurrent second write fail
// rather than record two definitions under one number.
func appendWorkflowRevision(tx *gorm.DB, workflowKey, createdBy uint64, w *coreworkflow.Workflow) error {
	return tx.Create(&workflowRevisionRow{
		WorkflowID:  workflowKey,
		Revision:    w.Revision,
		Name:        w.Name,
		Description: w.Description,
		Definition:  w.Definition,
		Status:      w.Status,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now().UTC(),
	}).Error
}

func (s *Store) GetWorkflow(ctx context.Context, workflowID string) (*coreworkflow.Workflow, error) {
	id, ok := util.CanonicalPublicID(workflowID)
	if !ok {
		return nil, nil
	}
	var workflow workflowReadRow
	err := s.workflowSelect(ctx).Where("workflow.public_id = ?", id).Take(&workflow).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toWorkflow(&workflow), nil
}

func (s *Store) UpdateWorkflow(ctx context.Context, workflowID, spaceID string, in coreworkflow.UpdateInput) (*coreworkflow.Workflow, error) {
	workflow, err := s.GetWorkflow(ctx, workflowID)
	if err != nil || workflow == nil {
		return nil, err
	}
	if workflow.SpaceID != spaceID {
		return nil, nil
	}
	updated := *workflow
	if in.Name != nil {
		updated.Name = *in.Name
	}
	if in.Description != nil {
		updated.Description = *in.Description
	}
	if in.Definition != nil {
		updated.Definition = *in.Definition
	}
	if in.Status != nil {
		updated.Status = *in.Status
	}
	// A save that changes nothing is not a revision. Status counts as content
	// here: publishing is the act that lets a workflow run, so the record of who
	// published which definition is exactly what history is for.
	if updated.Name == workflow.Name && updated.Description == workflow.Description &&
		updated.Definition == workflow.Definition && updated.Status == workflow.Status {
		return workflow, nil
	}
	updated.Revision = nextRevision(in.ExpectedRevision)
	updated.UpdatedAt = time.Now().UTC()
	updates := map[string]interface{}{
		"name":        updated.Name,
		"description": updated.Description,
		"definition":  updated.Definition,
		"status":      updated.Status,
		"revision":    updated.Revision,
		"updated_at":  updated.UpdatedAt,
	}
	// The revision guard lives in the WHERE clause, not a read-then-check: only the
	// database can decide which of two edits that started from the same revision
	// commits. Guarding the row update and appending the matching revision in one
	// transaction makes the loser write nothing -- so it cannot overwrite the
	// winner's definition, and the append never collides with the unique
	// (workflow_id, revision) index to leak a duplicate-key error.
	applied := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		workflowKey, err := lookupKey(ctx, tx, "workflow", workflowID)
		if err != nil {
			return err
		}
		res := tx.Model(&workflowRow{}).
			Where("id = ? AND revision = ?", workflowKey, in.ExpectedRevision).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // another writer advanced the revision first; write nothing
		}
		applied = true
		updatedBy, err := lookupKey(ctx, tx, "user", in.UpdatedBy)
		if err != nil {
			return err
		}
		return appendWorkflowRevision(tx, workflowKey, updatedBy, &updated)
	})
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, coreworkflow.ErrRevisionConflict
	}
	return s.GetWorkflow(ctx, workflowID)
}

// ListWorkflowRevisions returns a workflow's revisions, newest first.
func (s *Store) ListWorkflowRevisions(ctx context.Context, workflowID string, limit, offset int) ([]coreworkflow.Revision, int, error) {
	limit, offset = capPage(limit, offset)
	workflowKey, err := lookupKey(ctx, s.db, "workflow", workflowID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := listRevisions[workflowRevisionReadRow](ctx, s.db, s.workflowRevisionSelect(ctx),
		"workflow_revision", "workflow_id", workflowKey, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return toWorkflowRevisions(rows), total, nil
}

// GetWorkflowRevision returns one revision, or (nil, nil) when there is no such revision.
func (s *Store) GetWorkflowRevision(ctx context.Context, workflowID string, revision int) (*coreworkflow.Revision, error) {
	workflowKey, err := lookupKey(ctx, s.db, "workflow", workflowID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row, err := getRevision[workflowRevisionReadRow](s.workflowRevisionSelect(ctx),
		"workflow_revision", "workflow_id", workflowKey, revision)
	if err != nil || row == nil {
		return nil, err
	}
	return toWorkflowRevision(row), nil
}

func (s *Store) CreateWorkflowRun(ctx context.Context, in coreworkflow.CreateRunInput) (*coreworkflow.Run, error) {
	now := time.Now().UTC()
	run := &coreworkflow.Run{
		WorkflowID:       in.WorkflowID,
		WorkflowRevision: in.WorkflowRevision,
		IssueID:          in.IssueID,
		Input:            in.Input,
		Status:           in.Status,
		CreatedBy:        in.CreatedBy,
		CreatedAt:        now,
		StartedAt:        in.StartedAt,
	}
	row := &workflowRunRow{
		WorkflowRevision: in.WorkflowRevision,
		Input:            in.Input,
		Status:           in.Status,
		CreatedAt:        now,
		StartedAt:        in.StartedAt,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		workflowKey, err := lookupKey(ctx, tx, "workflow", in.WorkflowID)
		if err != nil {
			return err
		}
		row.WorkflowID = workflowKey
		creator, err := lookupKey(ctx, tx, "user", in.CreatedBy)
		if err != nil {
			return err
		}
		row.CreatedBy = creator
		if in.IssueID != nil && *in.IssueID != "" {
			issueKey, err := lookupKey(ctx, tx, "issue", *in.IssueID)
			if err != nil {
				return err
			}
			row.IssueID = &issueKey
		}
		return createWithPublicID(ctx, tx, "uq_workflow_run_public_id",
			func(id string) { row.PublicID = id }, row)
	}); err != nil {
		return nil, err
	}
	run.ID = row.PublicID
	return run, nil
}

func (s *Store) ListWorkflowRunsByWorkflow(ctx context.Context, workflowID string, limit, offset int) ([]coreworkflow.Run, int, error) {
	limit, offset = capPage(limit, offset)
	workflowKey, err := lookupKey(ctx, s.db, "workflow", workflowID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&workflowRunRow{}).Where("workflow_id = ?", workflowKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []workflowRunReadRow
	q := s.workflowRunSelect(ctx).Where("workflow_run.workflow_id = ?", workflowKey).Order("workflow_run.created_at DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toWorkflowRuns(list), int(total), nil
}

func (s *Store) ListWorkflowRunsByIssue(ctx context.Context, issueID string, limit, offset int) ([]coreworkflow.Run, int, error) {
	limit, offset = capPage(limit, offset)
	issueKey, err := lookupKey(ctx, s.db, "issue", issueID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&workflowRunRow{}).Where("issue_id = ?", issueKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []workflowRunReadRow
	q := s.workflowRunSelect(ctx).Where("workflow_run.issue_id = ?", issueKey).Order("workflow_run.created_at DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toWorkflowRuns(list), int(total), nil
}

func (s *Store) GetWorkflowRun(ctx context.Context, workflowRunID string) (*coreworkflow.Run, error) {
	id, ok := util.CanonicalPublicID(workflowRunID)
	if !ok {
		return nil, nil
	}
	var run workflowRunReadRow
	err := s.workflowRunSelect(ctx).Where("workflow_run.public_id = ?", id).Take(&run).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toWorkflowRun(&run), nil
}

func (s *Store) ListWorkflowStepRuns(ctx context.Context, workflowRunID string) ([]coreworkflow.StepRun, error) {
	runKey, err := lookupKey(ctx, s.db, "workflow_run", workflowRunID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []workflowStepRunReadRow
	err = s.workflowStepRunSelect(ctx).
		Where("workflow_step_run.workflow_run_id = ?", runKey).
		Order("workflow_step_run.step_index ASC, workflow_step_run.created_at ASC").
		Find(&list).Error
	return toWorkflowStepRuns(list), err
}

func (s *Store) CreateWorkflowStepRuns(ctx context.Context, workflowRunID string, steps []coreworkflow.CreateStepRunInput) ([]coreworkflow.StepRun, error) {
	if len(steps) == 0 {
		return []coreworkflow.StepRun{}, nil
	}
	now := time.Now().UTC()
	runKey, err := lookupKey(ctx, s.db, "workflow_run", workflowRunID)
	if err != nil {
		return nil, err
	}
	out := make([]coreworkflow.StepRun, len(steps))
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range steps {
			row := &workflowStepRunRow{
				WorkflowRunID:     runKey,
				StepID:            steps[i].StepID,
				StepIndex:         steps[i].StepIndex,
				StepType:          steps[i].StepType,
				AgentName:         steps[i].AgentName,
				AgentDescription:  steps[i].AgentDescription,
				AgentInstructions: steps[i].AgentInstructions,
				AgentRevision:     steps[i].AgentRevision,
				Prompt:            steps[i].Prompt,
				Bindings:          encodeStepBindings(steps[i].Bindings),
				OutputSchema:      steps[i].OutputSchema,
				Status:            steps[i].Status,
				CreatedAt:         now,
			}
			if steps[i].TargetAgentID != nil && *steps[i].TargetAgentID != "" {
				agentKey, err := lookupKey(ctx, tx, "agent", *steps[i].TargetAgentID)
				if err != nil {
					return err
				}
				row.TargetAgentID = &agentKey
			}
			if err := createWithPublicID(ctx, tx, "uq_workflow_step_run_public_id",
				func(id string) { row.PublicID = id }, row); err != nil {
				return err
			}
			out[i] = *toWorkflowStepRun(&workflowStepRunReadRow{
				Row:                 *row,
				WorkflowRunPublicID: canonicalPublicID(workflowRunID),
				TargetAgentPublicID: optionalCanonicalPublicID(steps[i].TargetAgentID),
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// runStatusUpdates builds the column writes a run transition lands. status is
// always written; the rest only when supplied. A move to a terminal status also
// clears the reconciliation lease and schedule, so a finished run leaves the due
// set and no owner keeps a lease on it.
func runStatusUpdates(status coreworkflow.RunStatus, startedAt, endedAt *time.Time, errorMessage *string) map[string]interface{} {
	updates := map[string]interface{}{"status": string(status)}
	if coreworkflow.RunStatusTerminal(status) {
		updates["reconcile_owner"] = nil
		updates["lease_expires_at"] = nil
		updates["next_reconcile_at"] = nil
	}
	if startedAt != nil {
		updates["started_at"] = *startedAt
	}
	if endedAt != nil {
		updates["ended_at"] = *endedAt
	}
	if errorMessage != nil {
		updates["error_message"] = *errorMessage
	}
	return updates
}

// stepTransitionUpdates builds the column writes a step transition lands,
// resolving the task and task-run handles to their row keys. An empty string
// clears a handle; nil leaves it untouched.
func stepTransitionUpdates(ctx context.Context, tx *gorm.DB, in coreworkflow.TransitionStepRunInput) (map[string]interface{}, error) {
	updates := map[string]interface{}{"status": string(in.NewStatus)}
	if in.TaskID != nil {
		if *in.TaskID == "" {
			updates["task_id"] = nil
		} else {
			key, err := lookupKey(ctx, tx, "task", *in.TaskID)
			if err != nil {
				return nil, err
			}
			updates["task_id"] = key
		}
	}
	if in.TaskRunID != nil {
		if *in.TaskRunID == "" {
			updates["task_run_id"] = nil
		} else {
			key, err := lookupKey(ctx, tx, "task_run", *in.TaskRunID)
			if err != nil {
				return nil, err
			}
			updates["task_run_id"] = key
		}
	}
	if in.OutputSummary != nil {
		if *in.OutputSummary == "" {
			updates["output_summary"] = nil
		} else {
			updates["output_summary"] = *in.OutputSummary
		}
	}
	if in.Structured != nil {
		if *in.Structured == "" {
			updates["structured"] = nil
		} else {
			updates["structured"] = *in.Structured
		}
	}
	if in.ErrorMessage != nil {
		if *in.ErrorMessage == "" {
			updates["error_message"] = nil
		} else {
			updates["error_message"] = *in.ErrorMessage
		}
	}
	if in.StartedAt != nil {
		updates["started_at"] = *in.StartedAt
	}
	if in.EndedAt != nil {
		updates["ended_at"] = *in.EndedAt
	}
	return updates, nil
}

func (s *Store) TransitionWorkflowRun(ctx context.Context, in coreworkflow.TransitionRunInput) (bool, error) {
	if !coreworkflow.ValidRunStatusTransition(in.ExpectedStatus, in.NewStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidRunTransition, in.ExpectedStatus, in.NewStatus)
	}
	id, ok := util.CanonicalPublicID(in.WorkflowRunID)
	if !ok {
		return false, nil
	}
	res := s.db.WithContext(ctx).Model(&workflowRunRow{}).
		Where("public_id = ? AND status = ?", id, string(in.ExpectedStatus)).
		Updates(runStatusUpdates(in.NewStatus, in.StartedAt, in.EndedAt, in.ErrorMessage))
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (s *Store) TransitionWorkflowStepRun(ctx context.Context, in coreworkflow.TransitionStepRunInput) (bool, error) {
	if !coreworkflow.ValidStepRunTransition(in.ExpectedStatus, in.NewStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidStepRunTransition, in.ExpectedStatus, in.NewStatus)
	}
	id, ok := util.CanonicalPublicID(in.StepRunID)
	if !ok {
		return false, nil
	}
	updated := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates, err := stepTransitionUpdates(ctx, tx, in)
		if err != nil {
			return err
		}
		res := tx.Model(&workflowStepRunRow{}).
			Where("public_id = ? AND status = ?", id, string(in.ExpectedStatus)).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		updated = res.RowsAffected > 0
		return nil
	})
	return updated, err
}

// FinalizeFailedWorkflowRun ends a run because one step ended badly. In one
// transaction it moves the step to its terminal status, blocks every later step
// still pending, and moves the run to its terminal status. The step move is a
// guarded CAS: a false result means the step was no longer at its expected
// status, so another actor finished it first and nothing is written. The run
// move is guarded too, so a run a concurrent cancel already finalized keeps that
// outcome.
func (s *Store) FinalizeFailedWorkflowRun(ctx context.Context, in coreworkflow.FinalizeFailedRunInput) (bool, error) {
	if !coreworkflow.ValidStepRunTransition(in.StepExpected, in.StepStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidStepRunTransition, in.StepExpected, in.StepStatus)
	}
	if !coreworkflow.ValidRunStatusTransition(in.RunExpected, in.RunStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidRunTransition, in.RunExpected, in.RunStatus)
	}
	stepID, ok := util.CanonicalPublicID(in.StepRunID)
	if !ok {
		return false, nil
	}
	runID, ok := util.CanonicalPublicID(in.WorkflowRunID)
	if !ok {
		return false, nil
	}
	stepApplied := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stepUpdates := map[string]interface{}{"status": string(in.StepStatus)}
		if in.TaskRunID != nil && *in.TaskRunID != "" {
			key, err := lookupKey(ctx, tx, "task_run", *in.TaskRunID)
			if err != nil {
				return err
			}
			stepUpdates["task_run_id"] = key
		}
		if in.ErrorMessage != nil {
			stepUpdates["error_message"] = *in.ErrorMessage
		}
		if in.StartedAt != nil {
			stepUpdates["started_at"] = *in.StartedAt
		}
		if in.EndedAt != nil {
			stepUpdates["ended_at"] = *in.EndedAt
		}
		res := tx.Model(&workflowStepRunRow{}).
			Where("public_id = ? AND status = ?", stepID, string(in.StepExpected)).
			Updates(stepUpdates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // lost the race; leave the already-terminal step and run alone
		}
		stepApplied = true

		runKey, err := lookupKey(ctx, tx, "workflow_run", in.WorkflowRunID)
		if err != nil {
			return err
		}
		// Block every later step still pending. The status filter makes this a
		// guarded bulk pending -> blocked, which is a valid transition.
		if err := tx.Model(&workflowStepRunRow{}).
			Where("workflow_run_id = ? AND step_index > ? AND status = ?",
				runKey, in.StepIndex, string(coreworkflow.StepRunStatusPending)).
			Update("status", string(coreworkflow.StepRunStatusBlocked)).Error; err != nil {
			return err
		}

		// The run always goes terminal here, so runStatusUpdates also clears its
		// reconciliation lease and schedule.
		runUpdates := runStatusUpdates(in.RunStatus, nil, in.EndedAt, in.ErrorMessage)
		return tx.Model(&workflowRunRow{}).
			Where("public_id = ? AND status = ?", runID, string(in.RunExpected)).
			Updates(runUpdates).Error
	})
	return stepApplied, err
}

// defaultDueRunLimit bounds a due-run batch when the caller passes no limit. It
// keeps one sweep's work finite; a caller that wants more pages again.
const defaultDueRunLimit = 100

// terminalRunStatusStrings is the terminal run statuses as the column stores
// them, for the NOT IN filter the due query and lease guards share.
func terminalRunStatusStrings() []string {
	terminal := coreworkflow.TerminalRunStatuses()
	out := make([]string, len(terminal))
	for i, st := range terminal {
		out[i] = string(st)
	}
	return out
}

// ListDueWorkflowRuns returns non-terminal runs that need a reconciliation pass:
// their next_reconcile_at has arrived (or was never set) or their lease expired.
// Ordering is next_reconcile_at ascending then id, which MySQL sorts NULLs
// first, so never-scheduled runs lead and ties are stable.
func (s *Store) ListDueWorkflowRuns(ctx context.Context, now time.Time, limit int) ([]coreworkflow.Run, error) {
	if limit <= 0 {
		limit = defaultDueRunLimit
	}
	var list []workflowRunReadRow
	err := s.workflowRunSelect(ctx).
		Where("workflow_run.status NOT IN ?", terminalRunStatusStrings()).
		Where("workflow_run.next_reconcile_at IS NULL OR workflow_run.next_reconcile_at <= ? OR "+
			"(workflow_run.lease_expires_at IS NOT NULL AND workflow_run.lease_expires_at <= ?)", now, now).
		Order("workflow_run.next_reconcile_at ASC, workflow_run.id ASC").
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return toWorkflowRuns(list), nil
}

// ClaimWorkflowRunLease acquires the lease for in.Owner with a guarded CAS: the
// run must be non-terminal and either unowned or holding an expired lease. A
// false result means another owner holds an unexpired lease or the run is done.
func (s *Store) ClaimWorkflowRunLease(ctx context.Context, in coreworkflow.ClaimLeaseInput) (bool, error) {
	id, ok := util.CanonicalPublicID(in.WorkflowRunID)
	if !ok {
		return false, nil
	}
	res := s.db.WithContext(ctx).Model(&workflowRunRow{}).
		Where("public_id = ? AND status NOT IN ? AND (reconcile_owner IS NULL OR lease_expires_at <= ?)",
			id, terminalRunStatusStrings(), in.Now).
		Updates(map[string]interface{}{
			"reconcile_owner":  in.Owner,
			"lease_expires_at": in.LeaseExpiresAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// RenewWorkflowRunLease extends the lease in.Owner already holds. The owner
// guard means a stale owner a takeover replaced, or a run gone terminal (which
// cleared the owner), matches nothing and gets false.
func (s *Store) RenewWorkflowRunLease(ctx context.Context, in coreworkflow.RenewLeaseInput) (bool, error) {
	id, ok := util.CanonicalPublicID(in.WorkflowRunID)
	if !ok {
		return false, nil
	}
	res := s.db.WithContext(ctx).Model(&workflowRunRow{}).
		Where("public_id = ? AND reconcile_owner = ?", id, in.Owner).
		Update("lease_expires_at", in.LeaseExpiresAt)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ReleaseWorkflowRunLease clears the lease in.Owner holds and sets when the run
// next wants a pass; a nil NextReconcileAt leaves it unscheduled. The owner
// guard refuses a stale owner, so it cannot disturb a replacement's lease.
func (s *Store) ReleaseWorkflowRunLease(ctx context.Context, in coreworkflow.ReleaseLeaseInput) (bool, error) {
	id, ok := util.CanonicalPublicID(in.WorkflowRunID)
	if !ok {
		return false, nil
	}
	updates := map[string]interface{}{
		"reconcile_owner":   nil,
		"lease_expires_at":  nil,
		"next_reconcile_at": nil,
	}
	if in.NextReconcileAt != nil {
		updates["next_reconcile_at"] = *in.NextReconcileAt
	}
	res := s.db.WithContext(ctx).Model(&workflowRunRow{}).
		Where("public_id = ? AND reconcile_owner = ?", id, in.Owner).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (s *Store) GetWorkflowStepRunByTaskID(ctx context.Context, taskID string) (*coreworkflow.StepRun, error) {
	return s.getWorkflowStepRunByOwner(ctx, "task", "workflow_step_run.task_id", taskID)
}

func (s *Store) GetWorkflowStepRunByTaskRunID(ctx context.Context, taskRunID string) (*coreworkflow.StepRun, error) {
	return s.getWorkflowStepRunByOwner(ctx, "task_run", "workflow_step_run.task_run_id", taskRunID)
}

// getWorkflowStepRunByOwner finds the step run that produced a task or a run.
//
// table and col reach a query as text, so both stay constants from this package
// and never values from a request.
func (s *Store) getWorkflowStepRunByOwner(ctx context.Context, table, col, publicID string) (*coreworkflow.StepRun, error) {
	key, err := lookupKey(ctx, s.db, table, publicID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var step workflowStepRunReadRow
	err = s.workflowStepRunSelect(ctx).Where(col+" = ?", key).Take(&step).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toWorkflowStepRun(&step), nil
}
