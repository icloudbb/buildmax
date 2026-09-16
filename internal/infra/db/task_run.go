package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"

	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type taskRunRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_task_run_public_id;not null"`
	TaskID   uint64 `gorm:"column:task_id;not null;index:idx_task_run_task_created,priority:1;uniqueIndex:idx_task_run_idempotency,priority:1"`
	// PreviousTaskRunID is the immutable predecessor in this Task's linear run
	// history. Task.last_run_id is a mutable projection and cannot answer which
	// bundle this run must restore after the new run becomes current.
	PreviousTaskRunID *uint64 `gorm:"column:previous_task_run_id;index"`
	Input             string  `gorm:"type:text;not null"`
	// IdempotencyKey is the caller's dedup key for Continue, scoped to this
	// task by the composite unique index above. NULL is not a duplicate of
	// NULL in MySQL's unique index, so every run created without a key (the
	// common case: retries, workflow steps, issue agent runs) coexists freely.
	IdempotencyKey *string `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex:idx_task_run_idempotency,priority:2"`
	// CreatedBy stays an opaque handle: created_by_type admits "webhook" and
	// "system", neither of which is a user row.
	CreatedBy     string  `gorm:"type:varchar(64);index"`
	CreatedByType string  `gorm:"type:varchar(32)"`
	TriggerSource string  `gorm:"type:varchar(64)"`
	Status        string  `gorm:"type:varchar(32);not null"`
	Output        *string `gorm:"type:text"`
	// Structured is the validated structured-output value as JSON text, nil for a
	// free-text run. See docs/design/structured-output.md.
	Structured       *string    `gorm:"type:text"`
	ErrorMessage     *string    `gorm:"type:text"`
	StartedAt        *time.Time `gorm:""`
	EndedAt          *time.Time `gorm:""`
	SessionID        *string    `gorm:"type:varchar(36)"`
	WorkerType       string     `gorm:"type:varchar(32)"`
	K8sJobName       *string    `gorm:"type:varchar(128)"`
	K8sJobCreatedAt  *time.Time `gorm:"column:k8s_job_created_at"`
	PromptTokens     *int       `gorm:""`
	CompletionTokens *int       `gorm:""`
	TracePath        *string    `gorm:"column:trace_path;type:varchar(512)"`
	// CancelRequestedAt is when someone asked this run to stop. It is a
	// separate column rather than a status because a started run stays RUNNING
	// until its own worker reports the outcome.
	CancelRequestedAt *time.Time `gorm:"column:cancel_requested_at;index"`
	CancelRequestedBy *uint64    `gorm:"column:cancel_requested_by"`
	CancelReason      string     `gorm:"column:cancel_reason;type:varchar(32);not null;default:''"`
	// RetryOfTaskRunID names the run this one repeats. A nullable column rather
	// than a trigger_source detail because the question a reader asks is which
	// run this repeated, and a source string cannot answer it.
	RetryOfTaskRunID *uint64 `gorm:"column:retry_of_task_run_id;index"`
	// SourceMessageID names the conversation message this run was asked for in.
	// Nullable because most origins are not a message: a workflow step, an
	// issue agent run, a retry, and a task created from the API all have none.
	SourceMessageID *uint64 `gorm:"column:source_message_id;index"`
	// AgentRevision numbers the agent definition served to this run's worker.
	// Not a reference to agent_revision.id: the pair (task.agent_id, this) is
	// what addresses a revision, and the task already holds the agent.
	AgentRevision                  *int `gorm:"column:agent_revision"`
	SpaceAgentInstructionsRevision *int `gorm:"column:space_agent_instructions_revision"`
	// PluginPins is a JSON array of the releases this run was given, written
	// beside AgentRevision and at the same moment. A JSON column for the reason
	// plugin_release.inspection is one: written once, read whole, and nothing
	// queries inside it.
	PluginPins string `gorm:"column:plugin_pins;type:text"`
	// SandboxNetworkTier and SandboxFilesystemTier are the agent-declared
	// sandbox tiers this run was given, written beside AgentRevision and
	// PluginPins and at the same moment. Nullable, distinct from an empty
	// string: NULL means "not yet resolved", empty means "resolved to the
	// strictest tier."
	SandboxNetworkTier    *string `gorm:"column:sandbox_network_tier;type:varchar(64)"`
	SandboxFilesystemTier *string `gorm:"column:sandbox_filesystem_tier;type:varchar(64)"`
	// LastSeenAt is when this run's worker last called a route scoped to it.
	// Indexed because the stale-run reaper's liveness sweep is the only reader
	// and it selects on this column.
	LastSeenAt *time.Time `gorm:"column:last_seen_at;index"`
	CreatedAt  time.Time  `gorm:"autoCreateTime;index:idx_task_run_task_created,priority:2"`

	// Workspace checkpoint provenance. See
	// docs/design/task-workspace-checkpoints.md §9.3. Numeric references to
	// workspace_checkpoint.id; the status columns exist because a missing
	// pointer alone cannot tell "not requested" from "attempted and failed".
	// Errors are bounded operator-facing text, never raw provider responses.
	WorkspaceBaseCheckpointID    *uint64 `gorm:"column:workspace_base_checkpoint_id;index"`
	WorkspaceResultCheckpointID  *uint64 `gorm:"column:workspace_result_checkpoint_id;index"`
	WorkspacePartialCheckpointID *uint64 `gorm:"column:workspace_partial_checkpoint_id;index"`
	WorkspaceRestoreStatus       string  `gorm:"column:workspace_restore_status;type:varchar(32)"`
	WorkspaceRestoreError        *string `gorm:"column:workspace_restore_error;type:text"`
	WorkspaceCheckpointStatus    string  `gorm:"column:workspace_checkpoint_status;type:varchar(32)"`
	WorkspaceCheckpointError     *string `gorm:"column:workspace_checkpoint_error;type:text"`
	PluginEnvironmentBaseID      *uint64 `gorm:"column:plugin_environment_base_id;index"`
	PluginEnvironmentResultID    *uint64 `gorm:"column:plugin_environment_result_id;index"`
	PluginEnvironmentStatus      string  `gorm:"column:plugin_environment_status;type:varchar(32)"`
	PluginEnvironmentError       *string `gorm:"column:plugin_environment_error;type:text"`
}

func (taskRunRow) TableName() string { return "task_run" }

// taskRunReadRow is the row plus the handles its references resolve to. A
// pointer field is one a LEFT JOIN may leave NULL.
type taskRunReadRow struct {
	Row                   taskRunRow `gorm:"embedded"`
	TaskPublicID          string     `gorm:"column:task_public_id"`
	PreviousPublicID      *string    `gorm:"column:previous_public_id"`
	RetryOfPublicID       *string    `gorm:"column:retry_of_public_id"`
	CancelRequestedByPub  *string    `gorm:"column:cancel_requested_by_public_id"`
	SourceMessagePublicID *string    `gorm:"column:source_message_public_id"`
}

func (s *Store) taskRunSelect(ctx context.Context) *gorm.DB {
	return taskRunSelectTx(s.db.WithContext(ctx))
}

func taskRunSelectTx(tx *gorm.DB) *gorm.DB {
	return tx.Model(&taskRunRow{}).
		Select("task_run.*, t.public_id AS task_public_id, pr.public_id AS previous_public_id, ro.public_id AS retry_of_public_id, " +
			"cb.public_id AS cancel_requested_by_public_id, sm.public_id AS source_message_public_id").
		Joins("INNER JOIN task t ON t.id = task_run.task_id").
		Joins("LEFT JOIN task_run pr ON pr.id = task_run.previous_task_run_id").
		Joins("LEFT JOIN task_run ro ON ro.id = task_run.retry_of_task_run_id").
		Joins("LEFT JOIN `user` cb ON cb.id = task_run.cancel_requested_by").
		Joins("LEFT JOIN conversation_message sm ON sm.id = task_run.source_message_id")
}

func toTaskRun(row *taskRunReadRow) *coretask.Run {
	if row == nil {
		return nil
	}
	out := &coretask.Run{
		ID:                             row.Row.PublicID,
		TaskID:                         row.TaskPublicID,
		PreviousTaskRunID:              optionalCanonicalPublicID(row.PreviousPublicID),
		Input:                          row.Row.Input,
		CreatedBy:                      row.Row.CreatedBy,
		CreatedByType:                  row.Row.CreatedByType,
		TriggerSource:                  row.Row.TriggerSource,
		Status:                         row.Row.Status,
		Output:                         row.Row.Output,
		Structured:                     row.Row.Structured,
		ErrorMessage:                   row.Row.ErrorMessage,
		StartedAt:                      row.Row.StartedAt,
		EndedAt:                        row.Row.EndedAt,
		SessionID:                      row.Row.SessionID,
		WorkerType:                     row.Row.WorkerType,
		K8sJobName:                     row.Row.K8sJobName,
		K8sJobCreatedAt:                row.Row.K8sJobCreatedAt,
		PromptTokens:                   row.Row.PromptTokens,
		CompletionTokens:               row.Row.CompletionTokens,
		TracePath:                      row.Row.TracePath,
		AgentRevision:                  row.Row.AgentRevision,
		SpaceAgentInstructionsRevision: row.Row.SpaceAgentInstructionsRevision,
		PluginPins:                     decodePluginPins(row.Row.PluginPins),
		SandboxNetworkTier:             row.Row.SandboxNetworkTier,
		SandboxFilesystemTier:          row.Row.SandboxFilesystemTier,
		CancelRequestedAt:              row.Row.CancelRequestedAt,
		CancelReason:                   row.Row.CancelReason,
		LastSeenAt:                     row.Row.LastSeenAt,
		CreatedAt:                      row.Row.CreatedAt,
		IdempotencyKey:                 row.Row.IdempotencyKey,
		WorkspaceRestoreStatus:         row.Row.WorkspaceRestoreStatus,
		WorkspaceRestoreError:          row.Row.WorkspaceRestoreError,
		WorkspaceCheckpointStatus:      row.Row.WorkspaceCheckpointStatus,
		WorkspaceCheckpointError:       row.Row.WorkspaceCheckpointError,
	}
	if row.Row.CancelRequestedBy != nil {
		by := derefPublicID(row.CancelRequestedByPub)
		out.CancelRequestedBy = &by
	}
	if row.Row.RetryOfTaskRunID != nil {
		of := derefPublicID(row.RetryOfPublicID)
		out.RetryOfTaskRunID = &of
	}
	if row.Row.SourceMessageID != nil {
		from := derefPublicID(row.SourceMessagePublicID)
		out.SourceMessageID = &from
	}
	return out
}

// taskRunUpdate is the set of columns a run status transition may write. A
// struct rather than positional parameters: four of the fields are *string, and
// transposing two of them would still compile.
type taskRunUpdate struct {
	status           string
	startedAt        *time.Time
	endedAt          *time.Time
	output           *string
	structured       *string
	errorMessage     *string
	sessionID        *string
	tracePath        *string
	promptTokens     *int
	completionTokens *int
	cancelReason     *string
}

// buildTaskRunUpdates renders the update into GORM's column map. A nil field
// means "leave this column alone", so a later transition cannot blank a value
// an earlier one wrote.
func buildTaskRunUpdates(in taskRunUpdate) map[string]interface{} {
	updates := map[string]interface{}{"status": in.status}
	if in.startedAt != nil {
		updates["started_at"] = *in.startedAt
	}
	if in.endedAt != nil {
		updates["ended_at"] = *in.endedAt
	}
	if in.output != nil {
		updates["output"] = *in.output
	}
	if in.structured != nil {
		updates["structured"] = *in.structured
	}
	if in.errorMessage != nil {
		updates["error_message"] = *in.errorMessage
	}
	if in.sessionID != nil {
		updates["session_id"] = *in.sessionID
	}
	if in.tracePath != nil {
		updates["trace_path"] = *in.tracePath
	}
	if in.promptTokens != nil {
		updates["prompt_tokens"] = *in.promptTokens
	}
	if in.completionTokens != nil {
		updates["completion_tokens"] = *in.completionTokens
	}
	if in.cancelReason != nil {
		updates["cancel_reason"] = *in.cancelReason
	}
	return updates
}

// CreateTaskRun creates a new run (PENDING). Returns coretask.ErrRunInProgress if the task has any run in PENDING, SCHEDULED, or RUNNING.
//
// The idempotency check, the active-run check, and the insert run inside one
// transaction that first takes a locking read on the task row. Without that
// lock, two concurrent callers for the same task — a doubled client retry of
// Continue, or a Continue racing a scheduler-issued retry — could each count
// zero active runs and each insert one, defeating the one-active-run-per-task
// rule the count exists to enforce. Locking the task row rather than the count
// query itself is what makes the second caller wait instead of racing it:
// MySQL has no locking read over an aggregate. The same lock is what lets a
// repeated idempotency key be looked up and an in-flight active run be
// refused as one atomic decision rather than two separate racing reads.
func (s *Store) CreateTaskRun(ctx context.Context, in coretask.CreateRunInput) (*coretask.Run, error) {
	canonicalTaskID, ok := util.CanonicalPublicID(in.TaskID)
	if !ok {
		return nil, apierr.ErrNotFound
	}
	var row *taskRunRow
	var previous, retryOf, sourceMessage *string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var taskLock taskRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "last_run_id", "workspace_head_checkpoint_id").Where("public_id = ?", canonicalTaskID).Take(&taskLock).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apierr.ErrNotFound
			}
			return err
		}
		taskKey := taskLock.ID

		// An idempotency key wins over everything below it, including the
		// active-run check: a client retrying a Continue it already sent must
		// get back the run that request created, whether that run is still
		// active or has since finished — not ErrRunInProgress while it runs and
		// a second row once it does not.
		if in.IdempotencyKey != nil && *in.IdempotencyKey != "" {
			var existing taskRunReadRow
			err := taskRunSelectTx(tx).
				Where("task_run.task_id = ? AND task_run.idempotency_key = ?", taskKey, *in.IdempotencyKey).
				Take(&existing).Error
			if err == nil {
				row = &existing.Row
				previous = existing.PreviousPublicID
				retryOf = existing.RetryOfPublicID
				sourceMessage = existing.SourceMessagePublicID
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var inProgress int64
		if err := tx.Model(&taskRunRow{}).Where("task_id = ? AND status IN ?", taskKey, coretask.ActiveRunStatuses()).
			Count(&inProgress).Error; err != nil {
			return err
		}
		if inProgress > 0 {
			return coretask.ErrRunInProgress
		}
		if taskLock.LastRunID != nil {
			var previousRow taskRunRow
			if err := tx.Select("public_id").Where("id = ?", *taskLock.LastRunID).
				Take(&previousRow).Error; err != nil {
				return err
			}
			previous = &previousRow.PublicID
		}

		// CreatedByType and TriggerSource are not defaulted here: the service
		// layer (internal/service/task.normalizeCreateRunProvenance) is this
		// value's one authoritative source. See
		// docs/design/portal-work-and-execution-experience.md.
		row = &taskRunRow{
			TaskID:            taskKey,
			PreviousTaskRunID: taskLock.LastRunID,
			Input:             in.Input,
			CreatedBy:         in.CreatedBy,
			CreatedByType:     in.CreatedByType,
			TriggerSource:     in.TriggerSource,
			Status:            "PENDING",
			CreatedAt:         time.Now().UTC(),
			AgentRevision:     in.AgentRevision,
			// A run continuing the Task starts from the Task's committed workspace
			// head. A first run has none, so the base stays null and the worker
			// establishes the seed. Retry overrides this below. See
			// docs/design/task-workspace-checkpoints.md §5.2.
			WorkspaceBaseCheckpointID: taskLock.WorkspaceHeadCheckpointID,
			SandboxNetworkTier:        in.SandboxNetworkTier,
			SandboxFilesystemTier:     in.SandboxFilesystemTier,
			IdempotencyKey:            in.IdempotencyKey,
		}
		if in.RetryOfTaskRunID != nil && *in.RetryOfTaskRunID != "" {
			canonicalRetryOf, ok := util.CanonicalPublicID(*in.RetryOfTaskRunID)
			if !ok {
				return apierr.ErrNotFound
			}
			var retryOfRow taskRunRow
			if err := tx.Select("id", "workspace_base_checkpoint_id").
				Where("public_id = ?", canonicalRetryOf).
				Take(&retryOfRow).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apierr.ErrNotFound
				}
				return err
			}
			row.RetryOfTaskRunID = &retryOfRow.ID
			// Retry repeats an attempt, so it receives the exact base the repeated
			// run received — never that run's partial or result. See §5.3.
			row.WorkspaceBaseCheckpointID = retryOfRow.WorkspaceBaseCheckpointID
			retryOf = optionalCanonicalPublicID(in.RetryOfTaskRunID)
		}
		// A message that cannot be resolved leaves the run unattributed rather
		// than refusing it. Losing the provenance of work someone asked for is
		// bad; refusing to do the work because its provenance would not resolve
		// is worse.
		sourceKey, err := optionalKey(ctx, tx, "conversation_message", in.SourceMessageID)
		if err != nil && !errors.Is(err, apierr.ErrNotFound) {
			return err
		}
		row.SourceMessageID = sourceKey
		if sourceKey != nil {
			sourceMessage = optionalCanonicalPublicID(in.SourceMessageID)
		}
		if err := createWithPublicID(ctx, tx, "uq_task_run_public_id",
			func(id string) { row.PublicID = id }, row); err != nil {
			return err
		}
		// Mirror the new run onto the task immediately, the same fields
		// TransitionTaskRun keeps in sync on every later transition. Without
		// this, the task row keeps reporting the previous run's terminal
		// status — output included — until the scheduler's next tick claims
		// this one, which a caller reading the task right after Continue or
		// Retry would otherwise see as a stale success or failure.
		return tx.Model(&taskRow{}).Where("id = ?", taskKey).Updates(map[string]interface{}{
			"last_run_id":   row.ID,
			"status":        row.Status,
			"output":        nil,
			"started_at":    nil,
			"ended_at":      nil,
			"error_message": nil,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return toTaskRun(&taskRunReadRow{
		Row:                   *row,
		TaskPublicID:          canonicalTaskID,
		PreviousPublicID:      previous,
		RetryOfPublicID:       retryOf,
		SourceMessagePublicID: sourceMessage,
	}), nil
}

// RecordTaskRunAgentRevision stores which agent definition a run was given.
//
// The `agent_revision IS NULL` guard is what makes the first write win. A worker
// polls its run repeatedly, and an agent edited mid-run would otherwise rewrite
// the record of what actually ran on the next poll.
func (s *Store) RecordTaskRunAgentRevision(ctx context.Context, taskRunID string, revision int) error {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ? AND agent_revision IS NULL", id).
		Update("agent_revision", revision).Error
}

func (s *Store) RecordTaskRunSpaceAgentInstructionsRevision(ctx context.Context, taskRunID string, revision int) error {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ? AND space_agent_instructions_revision IS NULL", id).
		Update("space_agent_instructions_revision", revision).Error
}

// RecordTaskRunPluginPins stores the releases a run was given.
//
// The `plugin_pins = ”` guard is what makes the first write win, for the same
// reason the agent revision has one: a worker polls its run, and a space's
// activation edited mid-run must not rewrite the record of what actually ran.
func (s *Store) RecordTaskRunPluginPins(ctx context.Context, taskRunID string, pins []coreplugin.Pin) error {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return apierr.ErrNotFound
	}
	encoded, err := json.Marshal(pins)
	if err != nil {
		return fmt.Errorf("encode plugin pins: %w", err)
	}
	return s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ? AND (plugin_pins IS NULL OR plugin_pins = '')", id).
		Update("plugin_pins", string(encoded)).Error
}

// RecordTaskRunSandboxTiers stores the agent-declared sandbox tiers a run
// was given.
//
// The `sandbox_network_tier IS NULL` guard is what makes the first write win,
// for the same reason the agent revision and plugin pins have one. Both
// columns are written together and unconditionally -- an empty string is a
// resolved value ("strictest tier"), not "nothing to write" -- which is why
// this does not skip on empty the way RecordTaskRunPluginPins does.
func (s *Store) RecordTaskRunSandboxTiers(ctx context.Context, taskRunID string, networkTier, filesystemTier string) error {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ? AND sandbox_network_tier IS NULL", id).
		Updates(map[string]any{
			"sandbox_network_tier":    networkTier,
			"sandbox_filesystem_tier": filesystemTier,
		}).Error
}

// decodePluginPins reads the column. A document that will not decode costs the
// record of what a run had, not the run: the pins it actually used were sent to
// it at claim time.
func decodePluginPins(raw string) []coreplugin.Pin {
	if raw == "" {
		return nil
	}
	var out []coreplugin.Pin
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// CountTaskRunsByStatus implements coretask.RunStore.
func (s *Store) CountTaskRunsByStatus(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		Status string
		N      int
	}
	if err := s.db.WithContext(ctx).
		Model(&taskRunRow{}).
		Select("status, count(*) as n").
		Group("status").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

// GetNextPendingTaskRun returns the oldest run with status PENDING (by created_at), or (nil, nil) if none.
func (s *Store) GetNextPendingTaskRun(ctx context.Context) (*coretask.Run, error) {
	var r taskRunReadRow
	err := s.taskRunSelect(ctx).Where("task_run.status = ?", "PENDING").Order("task_run.created_at ASC").Take(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTaskRun(&r), nil
}

// GetTaskRun returns the run by task_run_id, or (nil, nil) if not found.
func (s *Store) GetTaskRun(ctx context.Context, taskRunID string) (*coretask.Run, error) {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return nil, nil
	}
	var r taskRunReadRow
	err := s.taskRunSelect(ctx).Where("task_run.public_id = ?", id).Take(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTaskRun(&r), nil
}

func (s *Store) ListTaskRunsByTask(ctx context.Context, taskID string) ([]coretask.Run, error) {
	key, err := lookupKey(ctx, s.db, "task", taskID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []taskRunReadRow
	if err := s.taskRunSelect(ctx).Where("task_run.task_id = ?", key).
		Order("task_run.created_at ASC, task_run.id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coretask.Run, len(rows))
	for i := range rows {
		out[i] = *toTaskRun(&rows[i])
	}
	return out, nil
}

// GetTaskRunWithTask returns the run and its task, or (nil, nil, nil) if run not found.
func (s *Store) GetTaskRunWithTask(ctx context.Context, taskRunID string) (*coretask.Run, *coretask.Task, error) {
	run, err := s.GetTaskRun(ctx, taskRunID)
	if err != nil || run == nil {
		return nil, nil, err
	}
	task, err := s.GetTask(ctx, run.TaskID)
	if err != nil || task == nil {
		return run, nil, err
	}
	return run, task, nil
}

// GetActiveTaskRunByTask returns the task's run in PENDING, SCHEDULED, or
// RUNNING, or (nil, nil) when the task has none.
//
// CreateTaskRun refuses a second run while one is active, so at most one row
// can match; the ordering only makes the answer deterministic if that ever
// stops being true.
func (s *Store) GetActiveTaskRunByTask(ctx context.Context, taskID string) (*coretask.Run, error) {
	taskKey, err := lookupKey(ctx, s.db, "task", taskID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r taskRunReadRow
	err = s.taskRunSelect(ctx).
		Where("task_run.task_id = ? AND task_run.status IN ?", taskKey, coretask.ActiveRunStatuses()).
		Order("task_run.created_at DESC").
		Take(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTaskRun(&r), nil
}

// RequestTaskRunCancel records a cancel request on a run that is still active
// and does not already carry one.
//
// The status stays where it is. A started run belongs to its worker until that
// worker reports an outcome, so writing CANCELED here would describe a run that
// is still executing, and the worker's own report would overwrite it moments
// later.
func (s *Store) RequestTaskRunCancel(ctx context.Context, taskRunID, requestedBy, reason string, requestedAt time.Time) (bool, error) {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return false, nil
	}
	updates := map[string]any{
		"cancel_requested_at": requestedAt,
		"cancel_reason":       reason,
	}
	// A background reconciler asks with no name; the reason carries why. A person
	// asking resolves to their row so "who stopped this" has an answer.
	if requestedBy != "" {
		requester, err := lookupKey(ctx, s.db, "user", requestedBy)
		if err != nil {
			return false, err
		}
		updates["cancel_requested_by"] = requester
	}
	result := s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ? AND status IN ? AND cancel_requested_at IS NULL", id, coretask.ActiveRunStatuses()).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ListCancelRequestedTaskRuns returns runs that were asked to stop before
// cutoff and are still active.
//
// A cancel is honored by the run's worker, and a worker can be gone — killed,
// evicted, or built before cancellation existed. Nothing else would ever move
// those runs, which is why this query exists; see StaleRunReaper.
func (s *Store) ListCancelRequestedTaskRuns(ctx context.Context, cutoff time.Time, limit int) ([]coretask.Run, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []taskRunReadRow
	err := s.taskRunSelect(ctx).
		Where("task_run.status IN ?", coretask.ActiveRunStatuses()).
		Where("task_run.cancel_requested_at IS NOT NULL AND task_run.cancel_requested_at <= ?", cutoff).
		Order("task_run.cancel_requested_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coretask.Run, 0, len(rows))
	for i := range rows {
		out = append(out, *toTaskRun(&rows[i]))
	}
	return out, nil
}

// ListActiveTaskRunsForEligibility returns runs still in an active status that
// have no cancel request yet, each with its initiator and Space, so the
// eligibility reconciler can re-check authority and cancel work whose initiator
// lost it. It is a bounded scan ordered by id; the caller pages by passing the
// last id it saw as afterID (empty for the first page).
func (s *Store) ListActiveTaskRunsForEligibility(ctx context.Context, afterID string, limit int) ([]coretask.ActiveRunRef, error) {
	if limit <= 0 {
		limit = 100
	}
	q := activeRunRefSelect(s.db.WithContext(ctx)).
		Where("task_run.cancel_requested_at IS NULL")
	if afterID != "" {
		if key, ok := util.CanonicalPublicID(afterID); ok {
			var last taskRunRow
			if err := s.db.WithContext(ctx).Select("id").Where("public_id = ?", key).First(&last).Error; err == nil {
				q = q.Where("task_run.id > ?", last.ID)
			}
		}
	}
	return scanActiveRunRefs(q.Order("task_run.id ASC").Limit(limit))
}

// ListActiveTaskRunsByCreator returns every active run a given account
// initiated, with its Space and status. It is the per-account scan a deactivation
// uses to cancel that account's in-flight work and to project the impact of
// doing so; unlike the reconciler's scan it includes runs already asked to stop,
// so an impact count reflects all active work.
func (s *Store) ListActiveTaskRunsByCreator(ctx context.Context, createdBy string) ([]coretask.ActiveRunRef, error) {
	return scanActiveRunRefs(activeRunRefSelect(s.db.WithContext(ctx)).
		Where("task_run.created_by = ?", createdBy).
		Order("task_run.id ASC"))
}

func activeRunRefSelect(tx *gorm.DB) *gorm.DB {
	return tx.Model(&taskRunRow{}).
		Select("task_run.id AS id, task_run.public_id AS task_run_public_id, "+
			"sp.public_id AS space_public_id, task_run.created_by AS created_by_public_id, task_run.status AS status").
		Joins("INNER JOIN task t ON t.id = task_run.task_id").
		Joins("INNER JOIN space sp ON sp.id = t.space_id").
		Where("task_run.status IN ?", coretask.ActiveRunStatuses())
}

func scanActiveRunRefs(q *gorm.DB) ([]coretask.ActiveRunRef, error) {
	type refRow struct {
		TaskRunPublicID   string `gorm:"column:task_run_public_id"`
		SpacePublicID     string `gorm:"column:space_public_id"`
		CreatedByPublicID string `gorm:"column:created_by_public_id"`
		Status            string `gorm:"column:status"`
		ID                uint64 `gorm:"column:id"`
	}
	var refs []refRow
	if err := q.Find(&refs).Error; err != nil {
		return nil, err
	}
	out := make([]coretask.ActiveRunRef, 0, len(refs))
	for i := range refs {
		out = append(out, coretask.ActiveRunRef{
			TaskRunID: refs[i].TaskRunPublicID,
			SpaceID:   refs[i].SpacePublicID,
			CreatedBy: refs[i].CreatedByPublicID,
			Status:    refs[i].Status,
		})
	}
	return out, nil
}

// MarkTaskRunSeen records that this run's worker is still reporting.
//
// The status guard is what makes the column mean "last seen while working": a
// worker's terminal PATCH is itself a call scoped to the run, and letting it
// stamp would move the timestamp past the moment the work stopped.
//
// It is one unconditional write per call rather than a read-then-write, because
// the only caller is the run's own poll route and that arrives on a fixed
// interval. Do not stamp from the streaming route, which fires many times a
// second.
func (s *Store) MarkTaskRunSeen(ctx context.Context, taskRunID string, seenAt time.Time) error {
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ? AND status IN ?", id, []string{
			string(coretask.RunStatusScheduled), string(coretask.RunStatusRunning),
		}).
		Update("last_seen_at", seenAt).Error
}

// ListLostWorkerTaskRuns returns RUNNING runs whose worker stopped reporting
// before cutoff.
//
// It is deliberately narrower than ListStaleTaskRuns. A RUNNING run has a
// worker polling its own route every few seconds for as long as it lives, so
// silence there is evidence the process is gone rather than slow. A SCHEDULED
// run makes no such promise — it may be materializing a workspace or pulling
// plugin packages without touching the API — so it stays under the run timeout.
//
// A NULL last_seen_at is never reaped here: it means no signal was ever
// recorded, and absence of evidence is not the observation this sweep is for.
func (s *Store) ListLostWorkerTaskRuns(ctx context.Context, cutoff time.Time, limit int) ([]coretask.Run, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []taskRunReadRow
	err := s.taskRunSelect(ctx).
		Where("task_run.status = ?", string(coretask.RunStatusRunning)).
		Where("task_run.last_seen_at IS NOT NULL AND task_run.last_seen_at <= ?", cutoff).
		Order("task_run.last_seen_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coretask.Run, 0, len(rows))
	for i := range rows {
		out = append(out, *toTaskRun(&rows[i]))
	}
	return out, nil
}

// TransitionTaskRun moves a run only from the status the caller observed. The
// run and its task projection commit together, so a worker and a recovery loop
// cannot overwrite one another's outcome or leave the task disagreeing with its
// last run.
func (s *Store) TransitionTaskRun(ctx context.Context, in coretask.TransitionRunInput) (bool, error) {
	if !coretask.ValidRunStatusTransition(in.ExpectedStatus, in.NewStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coretask.ErrInvalidRunTransition, in.ExpectedStatus, in.NewStatus)
	}
	id, ok := util.CanonicalPublicID(in.TaskRunID)
	if !ok {
		return false, nil
	}

	updated := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&taskRunRow{}).
			Where("public_id = ? AND status = ?", id, string(in.ExpectedStatus)).
			Updates(buildTaskRunUpdates(taskRunUpdate{
				status:           string(in.NewStatus),
				startedAt:        in.StartedAt,
				endedAt:          in.EndedAt,
				output:           in.Output,
				structured:       in.Structured,
				errorMessage:     in.ErrorMessage,
				sessionID:        in.SessionID,
				tracePath:        in.TracePath,
				promptTokens:     in.PromptTokens,
				completionTokens: in.CompletionTokens,
				cancelReason:     in.CancelReason,
			}))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		var run taskRunRow
		if err := tx.Where("public_id = ?", id).First(&run).Error; err != nil {
			return err
		}
		taskUpdates := map[string]interface{}{
			"last_run_id":   run.ID,
			"status":        run.Status,
			"output":        run.Output,
			"started_at":    run.StartedAt,
			"ended_at":      run.EndedAt,
			"error_message": run.ErrorMessage,
		}
		if run.SessionID != nil {
			taskUpdates["session_id"] = *run.SessionID
		}
		if err := tx.Model(&taskRow{}).Where("id = ?", run.TaskID).Updates(taskUpdates).Error; err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}

// UpdateTaskRunWorkerInfo updates worker_type, k8s_job_name, k8s_job_created_at for the run.
func (s *Store) UpdateTaskRunWorkerInfo(ctx context.Context, taskRunID, workerType string, k8sJobName *string, k8sJobCreatedAt *time.Time) error {
	updates := map[string]interface{}{"worker_type": workerType}
	if k8sJobName != nil {
		updates["k8s_job_name"] = *k8sJobName
	}
	if k8sJobCreatedAt != nil {
		updates["k8s_job_created_at"] = *k8sJobCreatedAt
	}
	id, ok := util.CanonicalPublicID(taskRunID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&taskRunRow{}).Where("public_id = ?", id).Updates(updates).Error
}

// ListTaskRunIDsByTasks groups run IDs by their task, newest first.
func (s *Store) ListTaskRunIDsByTasks(ctx context.Context, taskIDs []string) (map[string][]string, error) {
	if len(taskIDs) == 0 {
		return map[string][]string{}, nil
	}
	// Both handles come back through the join rather than being resolved one
	// task at a time; the grouping is still on the row key.
	ids := make([]string, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		if id, ok := util.CanonicalPublicID(taskID); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return map[string][]string{}, nil
	}
	var rows []struct {
		TaskPublicID    string
		TaskRunPublicID string
	}
	err := s.db.WithContext(ctx).Model(&taskRunRow{}).
		Select("t.public_id AS task_public_id, task_run.public_id AS task_run_public_id").
		Joins("INNER JOIN task t ON t.id = task_run.task_id").
		Where("t.public_id IN ?", ids).
		Order("task_run.created_at DESC, task_run.id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(taskIDs))
	for _, row := range rows {
		out[row.TaskPublicID] = append(out[row.TaskPublicID], row.TaskRunPublicID)
	}
	return out, nil
}

// ListStaleTaskRuns returns runs that were dispatched before cutoff and have
// not reached a terminal status.
//
// A run leaves SCHEDULED or RUNNING when its worker reports the outcome, so a
// run still in one of those states long after dispatch has no worker coming for
// it: the process died, the pod was evicted, or its credential expired before it
// could report. Nothing else notices, which is why this query exists.
func (s *Store) ListStaleTaskRuns(ctx context.Context, cutoff time.Time, limit int) ([]coretask.Run, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []taskRunReadRow
	err := s.taskRunSelect(ctx).
		Where("task_run.status IN ?", []string{string(coretask.RunStatusScheduled), string(coretask.RunStatusRunning)}).
		Where("task_run.created_at <= ?", cutoff).
		Order("task_run.created_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coretask.Run, 0, len(rows))
	for i := range rows {
		out = append(out, *toTaskRun(&rows[i]))
	}
	return out, nil
}

// ListTaskRunsWithExpiredTrace returns runs ended on or before cutoff that still
// point at a trace, oldest first. It selects only the public ids and the path a
// retention sweep needs, joining to the task and space for the coordinates a
// storage backend keys a run-global object by.
func (s *Store) ListTaskRunsWithExpiredTrace(ctx context.Context, cutoff time.Time, limit int) ([]coretask.RunTraceRef, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []struct {
		SpaceID   string
		TaskID    string
		RunID     string
		TracePath string
	}
	err := s.db.WithContext(ctx).
		Model(&taskRunRow{}).
		Select("sp.public_id AS space_id, t.public_id AS task_id, task_run.public_id AS run_id, task_run.trace_path AS trace_path").
		Joins("INNER JOIN task t ON t.id = task_run.task_id").
		Joins("INNER JOIN space sp ON sp.id = t.space_id").
		Where("task_run.trace_path IS NOT NULL AND task_run.trace_path <> ''").
		Where("task_run.ended_at IS NOT NULL AND task_run.ended_at <= ?", cutoff).
		Order("task_run.ended_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coretask.RunTraceRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, coretask.RunTraceRef{
			SpaceID:   r.SpaceID,
			TaskID:    r.TaskID,
			TaskRunID: r.RunID,
			TracePath: r.TracePath,
		})
	}
	return out, nil
}

// ClearTaskRunTracePath sets a run's trace pointer to NULL. A run that already
// has none, or an id that names no run, is not an error: the sweep's job is that
// the pointer end up empty, not that this call be the one that emptied it.
func (s *Store) ClearTaskRunTracePath(ctx context.Context, taskRunID string) error {
	return s.db.WithContext(ctx).
		Model(&taskRunRow{}).
		Where("public_id = ?", taskRunID).
		Update("trace_path", nil).Error
}
