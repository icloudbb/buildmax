package conversation

import (
	"context"
	"errors"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
)

var ErrWebhookChannelRequired = errors.New("webhook engine only accepts webhook channel")

// WebhookEngine implements TurnEngine for webhook turns: no LLM, create one TaskRun.
type WebhookEngine struct {
	TaskService   *task.Service
	Conversations coreconv.Store
}

// Process creates exactly one TaskRun for webhook turns. Rejects other channels.
func (e *WebhookEngine) Process(ctx context.Context, conversationID, taskID string, turn convchannel.Turn) (ConversationResult, error) {
	if turn.Channel != convchannel.ChannelWebhook {
		return ConversationResult{}, ErrWebhookChannelRequired
	}
	if e.TaskService == nil {
		return ConversationResult{}, task.ErrTaskRunsNotConfigured
	}
	if e.Conversations == nil {
		return ConversationResult{}, errors.New("conversation store not configured")
	}
	if conversationID == "" {
		return ConversationResult{}, errors.New("conversation_id required")
	}
	if turn.Message == "" {
		return ConversationResult{}, errors.New("message required")
	}
	userID := turn.UserID
	if userID == "" {
		return ConversationResult{}, errors.New("user required")
	}
	conv, err := e.Conversations.GetConversation(ctx, conversationID)
	if err != nil {
		return ConversationResult{}, err
	}
	if conv == nil {
		return ConversationResult{}, errors.New("conversation not found")
	}
	if taskID == "" {
		result, err := e.TaskService.StartBackgroundTask(ctx, task.CreateTaskCmd{
			ConversationID: conversationID,
			UserID:         userID,
			SpaceID:        conv.SpaceID,
			Input:          turn.Message,
			AgentID:        nil,
			CreatedByType:  coretask.RunCreatedByTypeWebhook,
			TriggerSource:  coretask.RunTriggerSourceWebhook,
		})
		if err != nil {
			return ConversationResult{}, err
		}
		return ConversationResult{Runs: []SpawnedRun{{TaskID: result.TaskID, RunID: result.RunID}}}, nil
	}
	run, err := e.TaskService.CreateRun(ctx, task.CreateRunCmd{
		UserID:        userID,
		TaskID:        taskID,
		Input:         turn.Message,
		CreatedByType: coretask.RunCreatedByTypeWebhook,
		TriggerSource: coretask.RunTriggerSourceWebhook,
	})
	if err != nil {
		return ConversationResult{}, err
	}
	return ConversationResult{Runs: []SpawnedRun{{TaskID: taskID, RunID: run.ID}}}, nil
}
