package conversation

import (
	"context"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/apierr"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/llm"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

var (
	ErrInvalidTarget = apierr.New(apierr.KindInvalid, "invalid conversation target")
	ErrLLMRequired   = apierr.New(apierr.KindNotConfigured, "conversation LLM not configured")
)

// Model supplies the conversation model, bound to one turn.
//
// A turn gets a client of its own rather than sharing one because every call it
// makes is paid for by someone: binding the person, the conversation, and its
// Space before the first call is what lets each call be recorded in the managed
// call ledger and counted against the Space's quota. See
// docs/design/llm-gateway.md section 10.
//
// model names a catalog model; empty uses the deployment's conversation model.
type Model interface {
	ForConversation(ctx context.Context, userID, spaceID, conversationID, model string) (llm.LLMClient, error)
}

// Service is the single Tier 1 orchestration entry point for portal turns.
type Service struct {
	TaskService       *task.Service
	WorkflowService   *workflow.Service
	ConversationStore coreconv.Store
	MessageStore      coreconv.MessageStore
	// Model answers the turn and titles a new conversation. Nil leaves
	// conversations unable to answer.
	Model      Model
	AgentStore agentdef.Store
	// Spaces names the conversation's Space in the prompt and backs ListSpaces.
	// Nil leaves both out.
	Spaces corespace.Store
	// Files lets a Space Assistant read its readable files. Nil leaves its
	// file tools out.
	Files Files
	// Issues lets a Space Assistant escalate. Nil leaves Escalate out.
	Issues Issues
}

// HandleTurnCmd describes one normalized portal conversation turn.
type HandleTurnCmd struct {
	UserID         string
	Channel        string
	Message        string
	ConversationID string
	StreamSink     llm.StreamSink
	// Fence is the conversation lease's fencing token, carried into this turn's
	// message-history writes. Zero on the single-instance path. See
	// docs/design/server-coordination.md §7.
	Fence int64
	// Assistant runs the turn as a Space Assistant's front door; UserID is
	// then the requester. Set by that front door only.
	Assistant *AssistantTurn
}

// RerunTaskCmd describes a direct task-rerun request (bypasses the LLM layer).
type RerunTaskCmd struct {
	UserID  string
	Channel string
	Message string
	TaskID  string
}

// HandleTurn runs one conversation turn through the Tier 1 LLM loop.
func (s *Service) HandleTurn(ctx context.Context, cmd HandleTurnCmd) (ConversationResult, error) {
	if cmd.ConversationID == "" {
		return ConversationResult{}, ErrInvalidTarget
	}
	return s.handleConversationTurn(ctx, cmd)
}

// RerunTask creates a new task run for an existing task, bypassing the LLM layer.
func (s *Service) RerunTask(ctx context.Context, cmd RerunTaskCmd) (ConversationResult, error) {
	if s.TaskService == nil {
		return ConversationResult{}, task.ErrTaskRunsNotConfigured
	}
	createdByType := coretask.RunCreatedByTypeUser
	triggerSource := coretask.RunTriggerSourcePortalTaskRerun
	if cmd.Channel == convchannel.ChannelWebhook {
		createdByType = coretask.RunCreatedByTypeWebhook
		triggerSource = coretask.RunTriggerSourceWebhook
	}
	run, err := s.TaskService.CreateRun(ctx, task.CreateRunCmd{
		UserID:        cmd.UserID,
		TaskID:        cmd.TaskID,
		Input:         cmd.Message,
		CreatedByType: createdByType,
		TriggerSource: triggerSource,
	})
	if err != nil {
		return ConversationResult{}, err
	}
	return ConversationResult{Runs: []SpawnedRun{{TaskID: cmd.TaskID, RunID: run.ID}}}, nil
}

func (s *Service) handleConversationTurn(ctx context.Context, cmd HandleTurnCmd) (ConversationResult, error) {
	if s.ConversationStore == nil || s.MessageStore == nil {
		return ConversationResult{}, fmt.Errorf("conversation stores not configured")
	}
	if s.Model == nil {
		return ConversationResult{}, ErrLLMRequired
	}

	conv, err := s.ConversationStore.GetConversation(ctx, cmd.ConversationID)
	if err != nil {
		return ConversationResult{}, fmt.Errorf("read conversation %s: %w", cmd.ConversationID, err)
	}
	if conv == nil {
		return ConversationResult{}, ErrInvalidTarget
	}
	// Which assistant answers is the conversation's, decided when it was
	// created: an Assistant's conversation never takes a personal turn, which
	// would run with the person's tools, and the reverse.
	if (conv.AssistantID != "" || cmd.Assistant != nil) &&
		(cmd.Assistant == nil || cmd.Assistant.ID != conv.AssistantID) {
		return ConversationResult{}, ErrAssistantConversation
	}
	spaceID := conv.SpaceID
	model := ""
	if cmd.Assistant != nil {
		model = cmd.Assistant.Model
	}
	client, err := s.Model.ForConversation(ctx, cmd.UserID, spaceID, cmd.ConversationID, model)
	if err != nil {
		return ConversationResult{}, err
	}
	agents := s.fetchAgentSummaries(ctx, spaceID)
	spaces := s.Spaces
	if cmd.Assistant != nil {
		agents = assistantAgentSummaries(agents, cmd.Assistant.ids(coreassistant.KindAgent))
		spaces = nil
	}

	runInput := turnRunInput{
		ConversationID:  cmd.ConversationID,
		Message:         cmd.Message,
		Channel:         cmd.Channel,
		UserID:          cmd.UserID,
		SpaceID:         spaceID,
		SpaceName:       s.fetchSpaceName(ctx, spaceID),
		Spaces:          spaces,
		TaskService:     s.TaskService,
		WorkflowService: s.WorkflowService,
		AgentSummaries:  agents,
		// The title is one more call of this turn's, so it is made through the
		// turn's client and recorded with the rest.
		TitleGenerator: llm.NewTitleGenerator(client),
		StreamSink:     cmd.StreamSink,
		Fence:          cmd.Fence,
		Assistant:      cmd.Assistant,
		Files:          s.Files,
		Issues:         s.Issues,
	}
	reply, err := runConversationTurn(ctx, s.ConversationStore, s.MessageStore, client, runInput)
	return ConversationResult{Reply: reply}, err
}

// fetchSpaceName returns "" when the Space cannot be read; the turn then runs
// without naming it rather than failing.
func (s *Service) fetchSpaceName(ctx context.Context, spaceID string) string {
	if s.Spaces == nil || spaceID == "" {
		return ""
	}
	sp, err := s.Spaces.GetSpace(ctx, spaceID)
	if err != nil || sp == nil {
		return ""
	}
	return sp.Name
}

func (s *Service) fetchAgentSummaries(ctx context.Context, spaceID string) []agentSummary {
	if s.AgentStore == nil || spaceID == "" {
		return nil
	}
	agents, err := s.AgentStore.ListAgentsBySpace(ctx, spaceID)
	if err != nil || len(agents) == 0 {
		return nil
	}
	summaries := make([]agentSummary, len(agents))
	for i, a := range agents {
		summaries[i] = agentSummary{ID: a.ID, Name: a.Name, Description: a.Description}
	}
	return summaries
}
