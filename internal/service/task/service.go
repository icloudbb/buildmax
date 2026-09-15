package task

import (
	"context"
	"fmt"
	"github.com/icloudbb/buildmax/internal/core/apierr"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

const defaultTitleRunes = 50

var (
	ErrInputRequired         = apierr.New(apierr.KindInvalid, "input required")
	ErrAgentsNotConfigured   = apierr.New(apierr.KindNotConfigured, "agents not configured")
	ErrTasksNotConfigured    = apierr.New(apierr.KindNotConfigured, "tasks not configured")
	ErrTaskRunsNotConfigured = apierr.New(apierr.KindNotConfigured, "task runs not configured")
	ErrAgentNotFound         = apierr.New(apierr.KindInvalid, "agent not found")
	ErrTaskNotFound          = apierr.New(apierr.KindNotFound, "task not found")
	// ErrAdmissionKeyRequired guards AdmitWorkflowTask: idempotent admission is
	// meaningless without a key, so an empty one is a caller error rather than a
	// silent fall-through to a plain create.
	ErrAdmissionKeyRequired = apierr.New(apierr.KindInvalid, "admission key required")
	// ErrNoRunToRetry means the task has no finished run to repeat: it has
	// never run, or its only run is still in flight.
	ErrNoRunToRetry = apierr.New(apierr.KindConflict, "this task has no finished run to retry")
	// ErrRetryOfWorkflowStep means the task belongs to a workflow step. The
	// workflow owns that task's lifecycle — it reacts to the step run's
	// outcome — so a run started behind its back would mark a settled step
	// succeeded and dispatch the next step of a workflow run that is already
	// over.
	ErrRetryOfWorkflowStep = apierr.New(apierr.KindConflict, "this run belongs to a workflow step and cannot be retried on its own")
)

// WorkflowStepLookup answers whether a task is a workflow step's task. It is
// optional: a deployment with no workflow store has no workflow steps, so a nil
// lookup means nothing to protect rather than an unanswered question.
type WorkflowStepLookup interface {
	GetWorkflowNodeRunByTaskID(ctx context.Context, taskID string) (*coreworkflow.NodeRun, error)
}

// QuotaChecker is the narrow quota surface needed by task workflows.
type QuotaChecker interface {
	Check(ctx context.Context, spaceID string, runsToAdd, tokensToAdd int) (allowed bool, reason string, err error)
}

// Service owns task-related application workflows.
type Service struct {
	Agents         agentdef.Store
	Tasks          coretask.Store
	TaskRuns       coretask.RunStore
	QuotaChecker   QuotaChecker
	TitleGenerator llm.TitleGenerator
	// WorkflowSteps is only consulted by RetryRun. Callers that never retry
	// leave it nil.
	WorkflowSteps WorkflowStepLookup
}

// CreateTaskCmd creates a new task and its first run.
type CreateTaskCmd struct {
	ConversationID string
	UserID         string
	SpaceID        string
	Input          string
	AgentID        *string
	IssueID        *string
	// ScheduleID names the recurring time trigger that created this task, when a
	// schedule dispatcher admitted it. An origin relation, never an owner.
	ScheduleID    *string
	CreatedByType string
	TriggerSource string
	// SourceMessageID names the conversation message that asked for this task.
	SourceMessageID *string
	// AdmissionKey, when set, makes creation idempotent through AdmitWorkflowTask:
	// a replayed or concurrent dispatch under the same key resolves to the one
	// task instead of a duplicate. Empty for ordinary CreateTask callers.
	AdmissionKey string
	// OutputSchema is a JSON Schema (shared subset) the task's runs must satisfy
	// as their final answer, or nil for free text. A Workflow node with an
	// output_schema sets it. See docs/design/structured-output.md.
	OutputSchema *string
}

// CreateRunCmd creates a new run on an existing task.
type CreateRunCmd struct {
	UserID        string
	TaskID        string
	Input         string
	CreatedByType string
	TriggerSource string
	// RetryOfTaskRunID names the run this one repeats, when it repeats one.
	RetryOfTaskRunID *string
	// SourceMessageID names the conversation message that asked for this run.
	SourceMessageID *string
	// IdempotencyKey is the caller's dedup key for this Continue request. A
	// repeat with the same key on the same task returns the run the first call
	// created instead of starting a second one.
	IdempotencyKey *string
}

// RetryRunCmd repeats a task's most recent run.
type RetryRunCmd struct {
	UserID string
	TaskID string
}

// RetryResult reports the new run and the one it repeats.
type RetryResult struct {
	Run        *coretask.Run
	RetriedRun coretask.Run
}

// StartBackgroundTaskResult is returned when a background task is created.
type StartBackgroundTaskResult struct {
	TaskID string
	RunID  string
}

// CreateTask resolves input, applies title/quota rules, and persists a new task.
func (s *Service) CreateTask(ctx context.Context, cmd CreateTaskCmd) (*coretask.Task, error) {
	if s.Tasks == nil {
		return nil, ErrTasksNotConfigured
	}
	create, err := s.buildCreateInput(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return s.Tasks.CreateTask(ctx, create)
}

// AdmitWorkflowTask idempotently creates a Workflow node's task, keyed by
// cmd.AdmissionKey, so a coordinator that retries or races its dispatch — the
// crash window between admitting the task and recording the step link — resolves
// to the one task instead of a second execution. It is otherwise CreateTask: it
// resolves the same input, agent, and provenance and applies the same rules.
func (s *Service) AdmitWorkflowTask(ctx context.Context, cmd CreateTaskCmd) (*coretask.Task, error) {
	if s.Tasks == nil {
		return nil, ErrTasksNotConfigured
	}
	if cmd.AdmissionKey == "" {
		return nil, ErrAdmissionKeyRequired
	}
	create, err := s.buildCreateInput(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return s.Tasks.AdmitTask(ctx, create)
}

// buildCreateInput resolves the command into the CreateInput both CreateTask
// and AdmitWorkflowTask persist. The only difference between those two is
// idempotency, which the store applies from CreateInput.AdmissionKey.
func (s *Service) buildCreateInput(ctx context.Context, cmd CreateTaskCmd) (*coretask.CreateInput, error) {
	input, agentID, selectedAgent, err := s.resolveInput(ctx, cmd.SpaceID, cmd.UserID, cmd.Input, cmd.AgentID)
	if err != nil {
		return nil, err
	}
	// Ask about the run allowance before spending a title model call, and never
	// against the title afterwards. The token half was once charged here against
	// the freshly generated title, which refused a task only once its
	// conversation already existed -- and nothing deletes a conversation, so a
	// space under its run limit stranded one every time a title tipped it over.
	// Those tokens are recorded as usage below rather than gating a task whose
	// title has already spent them.
	if err := s.checkQuota(ctx, cmd.SpaceID, 0); err != nil {
		return nil, err
	}
	createdByType, triggerSource := normalizeCreateTaskProvenance(cmd.CreatedByType, cmd.TriggerSource)
	title, promptTokens, completionTokens := s.resolveTitle(ctx, input)
	create := &coretask.CreateInput{
		ConversationID:            cmd.ConversationID,
		SpaceID:                   cmd.SpaceID,
		Input:                     input,
		Title:                     title,
		CreatedBy:                 cmd.UserID,
		InitialRunCreatedBy:       cmd.UserID,
		InitialRunCreatedByType:   createdByType,
		InitialRunTriggerSource:   triggerSource,
		InitialRunSourceMessageID: cmd.SourceMessageID,
		TitlePromptTokens:         promptTokens,
		TitleCompletionTokens:     completionTokens,
		AgentID:                   agentID,
		IssueID:                   cmd.IssueID,
		ScheduleID:                cmd.ScheduleID,
		AdmissionKey:              cmd.AdmissionKey,
		OutputSchema:              cmd.OutputSchema,
	}
	if selectedAgent != nil {
		revision := selectedAgent.Revision
		networkTier := selectedAgent.SandboxNetworkTier
		filesystemTier := selectedAgent.SandboxFilesystemTier
		create.InitialRunAgentRevision = &revision
		create.InitialRunSandboxNetworkTier = &networkTier
		create.InitialRunSandboxFilesystemTier = &filesystemTier
	}
	return create, nil
}

// GetTaskInConversation reads a task and confirms it belongs to conversationID.
// A task in another conversation is reported as not found, because a
// conversation may only see its own tasks. This is the one place that scoping
// is decided, so a caller that already holds the Service does not reach past it
// into the store to re-check ConversationID.
func (s *Service) GetTaskInConversation(ctx context.Context, conversationID, taskID string) (*coretask.Task, error) {
	if s.Tasks == nil {
		return nil, fmt.Errorf("tasks not configured")
	}
	t, err := s.Tasks.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t == nil || t.ConversationID != conversationID {
		return nil, fmt.Errorf("task not found or not in this conversation")
	}
	return t, nil
}

// CreateRun enforces basic run creation rules and delegates to TaskRunStore.
func (s *Service) CreateRun(ctx context.Context, cmd CreateRunCmd) (*coretask.Run, error) {
	if s.TaskRuns == nil {
		return nil, ErrTaskRunsNotConfigured
	}
	if cmd.Input == "" {
		return nil, ErrInputRequired
	}
	if s.Tasks == nil {
		return nil, ErrTasksNotConfigured
	}
	target, err := s.Tasks.GetTask(ctx, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrTaskNotFound
	}
	if err := s.checkQuota(ctx, target.SpaceID, 0); err != nil {
		return nil, err
	}
	var revision *int
	var networkTier, filesystemTier *string
	if target.AgentID != nil && *target.AgentID != "" {
		if s.Agents == nil {
			return nil, ErrAgentsNotConfigured
		}
		agent, err := s.Agents.GetAgent(ctx, *target.AgentID)
		if err != nil {
			return nil, err
		}
		if agent == nil || agent.SpaceID != target.SpaceID {
			return nil, ErrAgentNotFound
		}
		rev := agent.Revision
		network := agent.SandboxNetworkTier
		filesystem := agent.SandboxFilesystemTier
		revision, networkTier, filesystemTier = &rev, &network, &filesystem
	}
	createdByType, triggerSource := normalizeCreateRunProvenance(cmd.CreatedByType, cmd.TriggerSource)
	return s.TaskRuns.CreateTaskRun(ctx, coretask.CreateRunInput{
		TaskID:                cmd.TaskID,
		Input:                 cmd.Input,
		CreatedBy:             cmd.UserID,
		CreatedByType:         createdByType,
		TriggerSource:         triggerSource,
		RetryOfTaskRunID:      cmd.RetryOfTaskRunID,
		SourceMessageID:       cmd.SourceMessageID,
		AgentRevision:         revision,
		SandboxNetworkTier:    networkTier,
		SandboxFilesystemTier: filesystemTier,
		IdempotencyKey:        cmd.IdempotencyKey,
	})
}

// RetryRun repeats a task's most recent run with the same input.
//
// The input comes from the run rather than the task because a task's later runs
// can carry follow-up instructions, and retrying means running that again — not
// running whatever the task was first asked to do.
//
// A run still in flight is not retried: one task holds at most one active run,
// and the answer to "it is taking too long" is to stop it first.
func (s *Service) RetryRun(ctx context.Context, cmd RetryRunCmd) (*RetryResult, error) {
	if s.TaskRuns == nil {
		return nil, ErrTaskRunsNotConfigured
	}
	if s.Tasks == nil {
		return nil, ErrTasksNotConfigured
	}
	target, err := s.Tasks.GetTask(ctx, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrTaskNotFound
	}
	if err := s.refuseWorkflowStepRetry(ctx, cmd.TaskID); err != nil {
		return nil, err
	}
	// Ask about an in-flight run before looking for a finished one. The store
	// refuses a second active run anyway, but task.last_run_id already names the
	// active run, so without this check Retry would misreport it as having no
	// finished run to repeat.
	active, err := s.TaskRuns.GetActiveTaskRunByTask(ctx, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, coretask.ErrRunInProgress
	}
	if target.LastRunID == nil {
		return nil, ErrNoRunToRetry
	}
	previous, err := s.TaskRuns.GetTaskRun(ctx, *target.LastRunID)
	if err != nil {
		return nil, err
	}
	if previous == nil || !coretask.RunStatusTerminal(previous.Status) {
		return nil, ErrNoRunToRetry
	}
	run, err := s.CreateRun(ctx, CreateRunCmd{
		UserID:           cmd.UserID,
		TaskID:           cmd.TaskID,
		Input:            previous.Input,
		CreatedByType:    coretask.RunCreatedByTypeUser,
		TriggerSource:    coretask.RunTriggerSourceTaskRetry,
		RetryOfTaskRunID: &previous.ID,
	})
	if err != nil {
		return nil, err
	}
	return &RetryResult{Run: run, RetriedRun: *previous}, nil
}

func (s *Service) refuseWorkflowStepRetry(ctx context.Context, taskID string) error {
	if s.WorkflowSteps == nil {
		return nil
	}
	step, err := s.WorkflowSteps.GetWorkflowNodeRunByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	if step != nil {
		return ErrRetryOfWorkflowStep
	}
	return nil
}

// StartBackgroundTask creates a task and returns its task/run ids.
func (s *Service) StartBackgroundTask(ctx context.Context, cmd CreateTaskCmd) (*StartBackgroundTaskResult, error) {
	task, err := s.CreateTask(ctx, cmd)
	if err != nil {
		return nil, err
	}
	runID := ""
	if task.LastRunID != nil {
		runID = *task.LastRunID
	}
	return &StartBackgroundTaskResult{
		TaskID: task.ID,
		RunID:  runID,
	}, nil
}

func (s *Service) resolveInput(ctx context.Context, spaceID, userID, input string, agentID *string) (string, *string, *agentdef.Agent, error) {
	if agentID == nil || *agentID == "" {
		if input == "" {
			return "", nil, nil, ErrInputRequired
		}
		return input, nil, nil, nil
	}
	if s.Agents == nil {
		return "", nil, nil, ErrAgentsNotConfigured
	}
	agent, err := s.Agents.GetAgent(ctx, *agentID)
	if err != nil {
		return "", nil, nil, err
	}
	if agent == nil || agent.SpaceID != spaceID {
		return "", nil, nil, ErrAgentNotFound
	}
	if input != "" {
		return input, agentID, agent, nil
	}
	return buildTaskInputFromAgent(agent, ""), agentID, agent, nil
}

func (s *Service) resolveTitle(ctx context.Context, input string) (string, int, int) {
	title := truncateTaskTitle(input, defaultTitleRunes)
	if s.TitleGenerator == nil {
		return title, 0, 0
	}
	genTitle, promptTokens, completionTokens, err := s.TitleGenerator.GenerateTitle(ctx, input)
	if err != nil || genTitle == "" {
		return title, 0, 0
	}
	return genTitle, promptTokens, completionTokens
}

// Admits reports whether the space can start one more run right now, before a
// caller writes anything a refusal would strand.
//
// CreateTask checks the same run allowance when it persists the task, but an
// orchestrator that opens a conversation first and asks afterwards leaves one
// behind on every refusal, and nothing deletes a conversation. Asking here
// keeps that refusal ahead of the first write.
//
// The run allowance is the whole gate. A task is no longer refused for the
// tokens its generated title spent: those are already spent by the time the
// title exists, so refusing then only stranded a conversation. CreateTask
// records the title's tokens as usage instead of gating on them.
func (s *Service) Admits(ctx context.Context, spaceID string) error {
	return s.checkQuota(ctx, spaceID, 0)
}

func (s *Service) checkQuota(ctx context.Context, spaceID string, tokens int) error {
	if s.QuotaChecker == nil {
		return nil
	}
	if spaceID == "" {
		return nil
	}
	allowed, reason, err := s.QuotaChecker.Check(ctx, spaceID, 1, tokens)
	if err != nil {
		// A limit that could not be read is not a limit that passed. Admitting
		// the run would spend a space's allowance without metering it.
		return fmt.Errorf("check quota for space %s: %w", spaceID, err)
	}
	if allowed {
		return nil
	}
	// The quota service's reason is already the whole sentence a caller should
	// read, so it is the message rather than a detail appended to one. The Kind
	// is what carries the 429; no transport needs to know this package's types.
	return apierr.New(apierr.KindQuotaExceeded, reason)
}

func buildTaskInputFromAgent(agent *agentdef.Agent, userInput string) string {
	out := fmt.Sprintf("Agent: %s\nDescription: %s\nInstructions:\n%s", agent.Name, agent.Description, agent.Instructions)
	if userInput != "" {
		out = out + "\n\n" + userInput
	}
	return out
}

func truncateTaskTitle(input string, maxRunes int) string {
	return util.TruncateRunes(input, maxRunes)
}

func normalizeCreateTaskProvenance(createdByType, triggerSource string) (string, string) {
	if createdByType == "" {
		createdByType = coretask.RunCreatedByTypeUser
	}
	if triggerSource == "" {
		triggerSource = coretask.RunTriggerSourceTaskCreate
	}
	return createdByType, triggerSource
}

func normalizeCreateRunProvenance(createdByType, triggerSource string) (string, string) {
	if createdByType == "" {
		createdByType = coretask.RunCreatedByTypeUser
	}
	if triggerSource == "" {
		triggerSource = coretask.RunTriggerSourceTaskRerun
	}
	return createdByType, triggerSource
}
