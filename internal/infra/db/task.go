package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	"github.com/icloudbb/buildmax/internal/core/session"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/util"

	"gorm.io/gorm"
)

type taskRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_task_public_id;not null"`
	// The space index carries created_at: a space's task list is always ordered
	// by it, and the single-column index the string model left could not serve
	// the sort.
	ConversationID        *uint64 `gorm:"column:conversation_id;index"`
	SpaceID               uint64  `gorm:"column:space_id;not null;index:idx_task_space_created,priority:1;uniqueIndex:uq_task_admission_key,priority:1"`
	IssueID               *uint64 `gorm:"column:issue_id;index"`
	ScheduleID            *uint64 `gorm:"column:schedule_id;index"`
	Status                string  `gorm:"type:varchar(32);not null"`
	Input                 string  `gorm:"type:text;not null"`
	Title                 string  `gorm:"type:varchar(256)"`
	TitlePromptTokens     int     `gorm:""`
	TitleCompletionTokens int     `gorm:""`
	Output                *string `gorm:"type:text"`
	// OutputSchema is the JSON Schema a run's final answer must satisfy, nil for a
	// free-text task. See docs/design/structured-output.md.
	OutputSchema *string    `gorm:"type:text"`
	CreatedBy    uint64     `gorm:"column:created_by;not null"`
	CreatedAt    time.Time  `gorm:"autoCreateTime;index:idx_task_space_created,priority:2"`
	StartedAt    *time.Time `gorm:""`
	EndedAt      *time.Time `gorm:""`
	ErrorMessage *string    `gorm:"type:text"`
	SessionID    *string    `gorm:"type:varchar(36)"`
	LastRunID    *uint64    `gorm:"column:last_run_id;index"`
	AgentID      *uint64    `gorm:"column:agent_id;index"`
	// WorkspaceHeadCheckpointID points at the latest checkpoint accepted as this
	// Task's recoverable workspace (its seed, then each successful result). A
	// projection maintained in the same transaction as checkpoint finalization;
	// nil until the first run commits one. See
	// docs/design/task-workspace-checkpoints.md §9.2.
	WorkspaceHeadCheckpointID *uint64 `gorm:"column:workspace_head_checkpoint_id;index"`
	// PluginEnvironmentHeadID points at the immutable Plugin environment the
	// next Continue uses; nil for a Task that installs nothing autonomously.
	PluginEnvironmentHeadID *uint64 `gorm:"column:plugin_environment_head_id;index"`
	// AdmissionKey binds a task to a coordinator's stable idempotency key, unique
	// within its space (uq_task_admission_key, composite with space_id). NULL for
	// the ordinary task no coordinator replays; NULL is not a duplicate of NULL
	// in a MySQL unique index, so those coexist freely. AdmissionFingerprint is
	// the digest of the admitted payload, compared on replay to tell an identical
	// admission from a conflicting reuse of the same key. See AdmitTask.
	AdmissionKey         *string `gorm:"column:admission_key;type:varchar(191);uniqueIndex:uq_task_admission_key,priority:2"`
	AdmissionFingerprint *string `gorm:"column:admission_fingerprint;type:char(64)"`
}

func (taskRow) TableName() string { return "task" }

// taskReadRow is the row plus the handles its converted references resolve to.
// A pointer field is one a LEFT JOIN may leave NULL.
type taskReadRow struct {
	Row                  taskRow `gorm:"embedded"`
	ConversationPublicID *string `gorm:"column:conversation_public_id"`
	SpacePublicID        string  `gorm:"column:space_public_id"`
	CreatedByPublicID    string  `gorm:"column:created_by_public_id"`
	LastRunPublicID      *string `gorm:"column:last_run_public_id"`
	IssuePublicID        *string `gorm:"column:issue_public_id"`
	SchedulePublicID     *string `gorm:"column:schedule_public_id"`
	AgentPublicID        *string `gorm:"column:agent_public_id"`
}

// taskSelect is the one place the join set for a task read is written down, so
// the detail read, the listings, and the run-output read cannot drift apart.
// Every join is a primary-key lookup, which is what keeps a listing one query.
func (s *Store) taskSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&taskRow{}).
		Select("task.*, c.public_id AS conversation_public_id, t.public_id AS space_public_id, " +
			"cb.public_id AS created_by_public_id, lr.public_id AS last_run_public_id, " +
			"i.public_id AS issue_public_id, sc.public_id AS schedule_public_id, " +
			"a.public_id AS agent_public_id").
		Joins("LEFT JOIN conversation c ON c.id = task.conversation_id").
		Joins("INNER JOIN space t ON t.id = task.space_id").
		Joins("INNER JOIN `user` cb ON cb.id = task.created_by").
		Joins("LEFT JOIN task_run lr ON lr.id = task.last_run_id").
		Joins("LEFT JOIN issue i ON i.id = task.issue_id").
		Joins("LEFT JOIN schedule sc ON sc.id = task.schedule_id").
		Joins("LEFT JOIN agent a ON a.id = task.agent_id")
}

func toTask(row *taskReadRow) *coretask.Task {
	if row == nil {
		return nil
	}
	out := &coretask.Task{
		ID:                    row.Row.PublicID,
		ConversationID:        derefPublicID(row.ConversationPublicID),
		SpaceID:               row.SpacePublicID,
		Status:                row.Row.Status,
		Input:                 row.Row.Input,
		Title:                 row.Row.Title,
		TitlePromptTokens:     row.Row.TitlePromptTokens,
		TitleCompletionTokens: row.Row.TitleCompletionTokens,
		Output:                row.Row.Output,
		OutputSchema:          row.Row.OutputSchema,
		CreatedBy:             row.CreatedByPublicID,
		CreatedAt:             row.Row.CreatedAt,
		StartedAt:             row.Row.StartedAt,
		EndedAt:               row.Row.EndedAt,
		ErrorMessage:          row.Row.ErrorMessage,
		SessionID:             row.Row.SessionID,
	}
	if row.Row.LastRunID != nil {
		lastRun := derefPublicID(row.LastRunPublicID)
		out.LastRunID = &lastRun
	}
	if row.Row.IssueID != nil {
		issue := derefPublicID(row.IssuePublicID)
		out.IssueID = &issue
	}
	if row.Row.ScheduleID != nil {
		schedule := derefPublicID(row.SchedulePublicID)
		out.ScheduleID = &schedule
	}
	if row.Row.AgentID != nil {
		agent := derefPublicID(row.AgentPublicID)
		out.AgentID = &agent
	}
	return out
}

func toTasks(rows []taskReadRow) []coretask.Task {
	out := make([]coretask.Task, len(rows))
	for i := range rows {
		out[i] = *toTask(&rows[i])
	}
	return out
}

// ListTasksByConversation returns tasks in the conversation, ordered by created_at.
// order is "asc" (oldest first) or "desc" (latest first); default "desc".
func (s *Store) ListTasksByConversation(ctx context.Context, conversationID string, order string) ([]coretask.Task, error) {
	id, ok := util.CanonicalPublicID(conversationID)
	if !ok {
		return nil, nil
	}
	var list []taskReadRow
	q := s.taskSelect(ctx).Where("c.public_id = ?", id)
	if order == "asc" {
		q = q.Order("task.created_at ASC")
	} else {
		q = q.Order("task.created_at DESC")
	}
	err := q.Find(&list).Error
	return toTasks(list), err
}

// ListTasksByConversationPaginated returns tasks with optional executed_only filter, ordered by created_at DESC.
// executedOnly: when true, only tasks that have been run (last_run_id IS NOT NULL) are returned.
// total is the total number of matching tasks (ignoring limit/offset).
func (s *Store) ListTasksByConversationPaginated(ctx context.Context, conversationID string, executedOnly bool, limit, offset int) ([]coretask.Task, int, error) {
	limit, offset = capPage(limit, offset)
	convKey, err := lookupKey(ctx, s.db, "conversation", conversationID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	count := s.db.WithContext(ctx).Model(&taskRow{}).Where("conversation_id = ?", convKey)
	if executedOnly {
		count = count.Where("last_run_id IS NOT NULL")
	}
	var total int64
	if err := count.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := s.taskSelect(ctx).Where("task.conversation_id = ?", convKey)
	if executedOnly {
		q = q.Where("task.last_run_id IS NOT NULL")
	}
	var list []taskReadRow
	err = q.Order("task.created_at DESC").Limit(limit).Offset(offset).Find(&list).Error
	return toTasks(list), int(total), err
}

func (s *Store) ListTasksByIssue(ctx context.Context, issueID string, limit, offset int) ([]coretask.Task, int, error) {
	limit, offset = capPage(limit, offset)
	issueKey, err := lookupKey(ctx, s.db, "issue", issueID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&taskRow{}).Where("issue_id = ?", issueKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := s.taskSelect(ctx).Where("task.issue_id = ?", issueKey).Order("task.created_at DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	var list []taskReadRow
	err = q.Find(&list).Error
	return toTasks(list), int(total), err
}

// ListTasksByAgent returns a space's threads for one agent, newest first.
func (s *Store) ListTasksByAgent(ctx context.Context, spaceID, agentID string, limit, offset int) ([]coretask.Task, int, error) {
	limit, offset = capPage(limit, offset)
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	agentKey, err := lookupKey(ctx, s.db, "agent", agentID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&taskRow{}).
		Where("space_id = ? AND agent_id = ?", spaceKey, agentKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []taskReadRow
	err = s.taskSelect(ctx).
		Where("task.space_id = ? AND task.agent_id = ?", spaceKey, agentKey).
		Order("task.created_at DESC").Limit(limit).Offset(offset).Find(&list).Error
	return toTasks(list), int(total), err
}

// ListTasksBySchedule returns a space's tasks created by one schedule, newest first.
func (s *Store) ListTasksBySchedule(ctx context.Context, spaceID, scheduleID string, limit, offset int) ([]coretask.Task, int, error) {
	limit, offset = capPage(limit, offset)
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	scheduleKey, err := lookupKey(ctx, s.db, "schedule", scheduleID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&taskRow{}).
		Where("space_id = ? AND schedule_id = ?", spaceKey, scheduleKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []taskReadRow
	err = s.taskSelect(ctx).
		Where("task.space_id = ? AND task.schedule_id = ?", spaceKey, scheduleKey).
		Order("task.created_at DESC").Limit(limit).Offset(offset).Find(&list).Error
	return toTasks(list), int(total), err
}

// GetTask returns the task by task_id, or (nil, nil) if not found.
func (s *Store) GetTask(ctx context.Context, taskID string) (*coretask.Task, error) {
	id, ok := util.CanonicalPublicID(taskID)
	if !ok {
		return nil, nil
	}
	var task taskReadRow
	err := s.taskSelect(ctx).Where("task.public_id = ?", id).Take(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTask(&task), nil
}

// GetTaskBySessionID returns the task with the given session_id, or (nil, nil) if not found.
func (s *Store) GetTaskBySessionID(ctx context.Context, sessionID string) (*coretask.Task, error) {
	var task taskReadRow
	err := s.taskSelect(ctx).Where("task.session_id = ?", sessionID).Take(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toTask(&task), nil
}

// CreateTask creates a new task and its first coretask.Run (PENDING) in one transaction. Returns the task with last_run_id and session_id set.
func (s *Store) CreateTask(ctx context.Context, in *coretask.CreateInput) (*coretask.Task, error) {
	if in == nil {
		return nil, errors.New("coretask.CreateInput is required")
	}
	taskDB, runDB := newTaskRows(in)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return createTaskAndRunTx(ctx, tx, in, taskDB, runDB)
	}); err != nil {
		return nil, err
	}
	return createdTask(in, taskDB, runDB), nil
}

// AdmitTask idempotently creates a task and its first run for in.AdmissionKey.
//
// The unique index on (space_id, admission_key) is the correctness mechanism,
// not a lock: the first insert wins, and a concurrent or replayed admission
// that loses the race reads the winner's task back and returns it. That is what
// closes the crash window a Workflow node dispatch opens between admitting the
// Task and recording the link — the coordinator calls this again with the same
// key and receives the same task rather than a second execution. A different
// payload under the same key is a caller mistake and returns a conflict rather
// than silently adopting unrelated work.
func (s *Store) AdmitTask(ctx context.Context, in *coretask.CreateInput) (*coretask.Task, error) {
	if in == nil {
		return nil, errors.New("coretask.CreateInput is required")
	}
	if in.AdmissionKey == "" {
		return nil, errors.New("AdmitTask requires an admission key")
	}
	fingerprint := coretask.AdmissionFingerprint(in)
	// Already admitted: return it (or a conflict) without attempting a second
	// insert that the unique index would reject anyway.
	existing, err := s.taskByAdmissionKey(ctx, in.SpaceID, in.AdmissionKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.admittedTaskOrConflict(ctx, existing, fingerprint)
	}
	taskDB, runDB := newTaskRows(in)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return createTaskAndRunTx(ctx, tx, in, taskDB, runDB)
	})
	if err == nil {
		return createdTask(in, taskDB, runDB), nil
	}
	// A concurrent admitter committed the same key between the read above and
	// this insert. Read theirs and reconcile against it rather than fail.
	if isDuplicateOnIndex(err, "uq_task_admission_key") {
		winner, rerr := s.taskByAdmissionKey(ctx, in.SpaceID, in.AdmissionKey)
		if rerr != nil {
			return nil, rerr
		}
		if winner != nil {
			return s.admittedTaskOrConflict(ctx, winner, fingerprint)
		}
	}
	return nil, err
}

// admittedTaskOrConflict returns the already-admitted task when its stored
// fingerprint matches the presented one, and ErrTaskAdmissionConflict when it
// does not. A row admitted without a fingerprint (there is no such path today)
// is treated as conflicting rather than silently adopted.
func (s *Store) admittedTaskOrConflict(ctx context.Context, existing *taskRow, fingerprint string) (*coretask.Task, error) {
	if existing.AdmissionFingerprint == nil || *existing.AdmissionFingerprint != fingerprint {
		return nil, coretask.ErrTaskAdmissionConflict
	}
	return s.GetTask(ctx, existing.PublicID)
}

// taskByAdmissionKey reads the raw row bound to (space, key), or (nil, nil) when
// none is. It returns the row rather than a coretask.Task because the caller
// compares the stored admission fingerprint, which the public projection omits.
func (s *Store) taskByAdmissionKey(ctx context.Context, spaceID, key string) (*taskRow, error) {
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row taskRow
	err = s.db.WithContext(ctx).Where("space_id = ? AND admission_key = ?", spaceKey, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// newTaskRows builds the task and first-run rows a create or admission inserts.
// It sets the admission key and fingerprint when the input carries a key, and
// leaves both NULL otherwise so an ordinary task never enters the unique index.
func newTaskRows(in *coretask.CreateInput) (*taskRow, *taskRunRow) {
	now := time.Now().UTC()
	sessionID := session.NewID() // UUID for buildmax CLI (session not exposed to user)
	taskDB := &taskRow{
		Status:                "PENDING",
		Input:                 in.Input,
		Title:                 in.Title,
		TitlePromptTokens:     in.TitlePromptTokens,
		TitleCompletionTokens: in.TitleCompletionTokens,
		CreatedAt:             now,
		SessionID:             &sessionID,
		OutputSchema:          in.OutputSchema,
	}
	if in.AdmissionKey != "" {
		key := in.AdmissionKey
		fingerprint := coretask.AdmissionFingerprint(in)
		taskDB.AdmissionKey = &key
		taskDB.AdmissionFingerprint = &fingerprint
	}
	// CreatedByType and TriggerSource are not defaulted here: the service
	// layer (internal/service/task.normalizeCreateTaskProvenance) is this
	// value's one authoritative source, and defaulting it again here would
	// give "what does an empty TriggerSource become" two independent answers
	// that could drift apart. See docs/design/portal-work-and-execution-experience.md.
	runDB := &taskRunRow{
		Input:                 in.Input,
		CreatedBy:             defaultString(in.InitialRunCreatedBy, in.CreatedBy),
		CreatedByType:         in.InitialRunCreatedByType,
		TriggerSource:         in.InitialRunTriggerSource,
		Status:                "PENDING",
		CreatedAt:             now,
		AgentRevision:         in.InitialRunAgentRevision,
		SandboxNetworkTier:    in.InitialRunSandboxNetworkTier,
		SandboxFilesystemTier: in.InitialRunSandboxFilesystemTier,
	}
	return taskDB, runDB
}

// createTaskAndRunTx resolves the input's references and inserts the task and
// its first run inside one transaction. It is the shared body of CreateTask and
// AdmitTask; the only difference between them is idempotency, which AdmitTask
// wraps around this.
func createTaskAndRunTx(ctx context.Context, tx *gorm.DB, in *coretask.CreateInput, taskDB *taskRow, runDB *taskRunRow) error {
	spaceKey, err := lookupKey(ctx, tx, "space", in.SpaceID)
	if err != nil {
		return err
	}
	taskDB.SpaceID = spaceKey
	if in.ConversationID != "" {
		var conv conversationRow
		convID, ok := util.CanonicalPublicID(in.ConversationID)
		if !ok {
			return apierr.ErrNotFound
		}
		if err := tx.Where("public_id = ?", convID).First(&conv).Error; err != nil {
			return err
		}
		if conv.SpaceID != spaceKey {
			return apierr.ErrNotFound
		}
		taskDB.ConversationID = &conv.ID
	}
	creator, err := lookupKey(ctx, tx, "user", in.CreatedBy)
	if err != nil {
		return err
	}
	taskDB.CreatedBy = creator
	if in.AgentID != nil && *in.AgentID != "" {
		key, err := lookupKey(ctx, tx, "agent", *in.AgentID)
		if err != nil {
			return err
		}
		taskDB.AgentID = &key
	}
	if in.IssueID != nil && *in.IssueID != "" {
		key, err := lookupKey(ctx, tx, "issue", *in.IssueID)
		if err != nil {
			return err
		}
		taskDB.IssueID = &key
	}
	if in.ScheduleID != nil && *in.ScheduleID != "" {
		key, err := lookupKey(ctx, tx, "schedule", *in.ScheduleID)
		if err != nil {
			return err
		}
		taskDB.ScheduleID = &key
	}
	// See CreateTaskRun: an unresolvable message leaves the run
	// unattributed rather than refusing to create the task.
	sourceKey, err := optionalKey(ctx, tx, "conversation_message", in.InitialRunSourceMessageID)
	if err != nil && !errors.Is(err, apierr.ErrNotFound) {
		return err
	}
	runDB.SourceMessageID = sourceKey
	if err := createWithPublicID(ctx, tx, "uq_task_public_id",
		func(id string) { taskDB.PublicID = id }, taskDB); err != nil {
		return err
	}
	runDB.TaskID = taskDB.ID
	if err := createWithPublicID(ctx, tx, "uq_task_run_public_id",
		func(id string) { runDB.PublicID = id }, runDB); err != nil {
		return err
	}
	// The task names its latest run, so the run has to exist first.
	return tx.Model(&taskRow{}).Where("id = ?", taskDB.ID).
		Update("last_run_id", runDB.ID).Error
}

// createdTask projects a freshly inserted task and run into the public shape,
// resolving the handles from the input the caller already holds rather than
// reading them back.
func createdTask(in *coretask.CreateInput, taskDB *taskRow, runDB *taskRunRow) *coretask.Task {
	taskDB.LastRunID = &runDB.ID
	return toTask(&taskReadRow{
		Row:                  *taskDB,
		ConversationPublicID: optionalCanonicalPublicID(&in.ConversationID),
		SpacePublicID:        canonicalPublicID(in.SpaceID),
		CreatedByPublicID:    canonicalPublicID(in.CreatedBy),
		LastRunPublicID:      &runDB.PublicID,
		IssuePublicID:        optionalCanonicalPublicID(in.IssueID),
		SchedulePublicID:     optionalCanonicalPublicID(in.ScheduleID),
		AgentPublicID:        optionalCanonicalPublicID(in.AgentID),
	})
}

func defaultString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func buildTaskUpdates(status string, startedAt, endedAt *time.Time, output, errorMessage, sessionID *string) map[string]interface{} {
	updates := map[string]interface{}{"status": status}
	if startedAt != nil {
		updates["started_at"] = *startedAt
	}
	if endedAt != nil {
		updates["ended_at"] = *endedAt
	}
	if output != nil {
		updates["output"] = *output
	}
	if errorMessage != nil {
		updates["error_message"] = *errorMessage
	}
	if sessionID != nil {
		updates["session_id"] = *sessionID
	}
	return updates
}

// UpdateTask updates a task's status and optional fields.
// Only non-nil pointer fields are written; status is always set.
func (s *Store) UpdateTask(ctx context.Context, in coretask.UpdateInput) error {
	id, ok := util.CanonicalPublicID(in.TaskID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&taskRow{}).Where("public_id = ?", id).Updates(
		buildTaskUpdates(in.Status, in.StartedAt, in.EndedAt, in.Output, in.ErrorMessage, in.SessionID),
	).Error
}

// ClaimTask updates a task's status and optional fields only when current status equals expectedStatus.
// Returns updated = (exactly one row was updated). Used for atomic claim (e.g. PENDING→SCHEDULED, SCHEDULED→RUNNING).
func (s *Store) ClaimTask(ctx context.Context, in coretask.ClaimInput) (bool, error) {
	id, ok := util.CanonicalPublicID(in.TaskID)
	if !ok {
		return false, nil
	}
	result := s.db.WithContext(ctx).Model(&taskRow{}).Where("public_id = ? AND status = ?", id, in.ExpectedStatus).Updates(
		buildTaskUpdates(in.NewStatus, in.StartedAt, in.EndedAt, in.Output, in.ErrorMessage, in.SessionID),
	)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
