package db

import (
	"context"
	"errors"
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
		if err := tx.Model(&workflowNodeRunRow{}).Where("id = ?", node.ID).Updates(map[string]any{
			"status": string(coreworkflow.NodeRunStatusRunning), "task_id": taskRow.ID, "task_run_id": taskRun.ID,
			"resolved_input": in.Input, "started_at": time.Now().UTC(),
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
