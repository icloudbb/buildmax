package conversation

import (
	"context"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/apierr"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
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

// Service is the single Tier 1 orchestration entry point for portal turns.
type Service struct {
	TaskService       *task.Service
	WorkflowService   *workflow.Service
	ConversationStore coreconv.Store
	MessageStore      coreconv.MessageStore
	LLMClient         llm.LLMClient
	TitleGenerator    llm.TitleGenerator
	AgentStore        agentdef.Store
	// Spaces names the conversation's Space in the prompt and backs ListSpaces.
	// Nil leaves both out.
	Spaces corespace.Store
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
	if s.LLMClient == nil {
		return ConversationResult{}, ErrLLMRequired
	}

	spaceID := s.fetchSpaceID(ctx, cmd.ConversationID, cmd.Channel)

	runInput := turnRunInput{
		ConversationID:  cmd.ConversationID,
		Message:         cmd.Message,
		Channel:         cmd.Channel,
		UserID:          cmd.UserID,
		SpaceID:         spaceID,
		SpaceName:       s.fetchSpaceName(ctx, spaceID),
		Spaces:          s.spacesForChannel(cmd.Channel),
		TaskService:     s.taskServiceForChannel(cmd.Channel),
		WorkflowService: s.workflowServiceForChannel(cmd.Channel),
		AgentSummaries:  s.fetchAgentSummaries(ctx, spaceID, cmd.Channel),
		TitleGenerator:  s.TitleGenerator,
		StreamSink:      cmd.StreamSink,
		Fence:           cmd.Fence,
	}
	reply, err := runConversationTurn(ctx, s.ConversationStore, s.MessageStore, s.LLMClient, runInput)
	return ConversationResult{Reply: reply}, err
}

func (s *Service) taskServiceForChannel(channel string) *task.Service {
	if channel == convchannel.ChannelSystem {
		return nil
	}
	return s.TaskService
}

func (s *Service) workflowServiceForChannel(channel string) *workflow.Service {
	if channel == convchannel.ChannelSystem {
		return nil
	}
	return s.WorkflowService
}

// fetchSpaceID looks up the conversation's space once so StartTask and agent listing share it.
// Returns "" when the channel is system or no TaskService is configured (task tools disabled).
func (s *Service) fetchSpaceID(ctx context.Context, conversationID, channel string) string {
	if channel == convchannel.ChannelSystem || s.TaskService == nil || s.ConversationStore == nil {
		return ""
	}
	conv, err := s.ConversationStore.GetConversation(ctx, conversationID)
	if err != nil || conv == nil {
		return ""
	}
	return conv.SpaceID
}

func (s *Service) spacesForChannel(channel string) spaceLister {
	if channel == convchannel.ChannelSystem || s.Spaces == nil {
		return nil
	}
	return s.Spaces
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

func (s *Service) fetchAgentSummaries(ctx context.Context, spaceID, channel string) []agentSummary {
	if s.AgentStore == nil || spaceID == "" || channel == convchannel.ChannelSystem {
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
