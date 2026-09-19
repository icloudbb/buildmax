package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/core/agent"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/llm"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

const (
	maxIterations = 10
	// maxParallelTools matches config.DefaultMaxParallelTools by value rather
	// than by import: agent.max_parallel_tools is a local surface setting and
	// no service package reads config. A Portal turn's read-only tools (ListTasks,
	// GetTask, ListWorkflows, GetWorkflowRun) are the only ones that overlap, so
	// the practical ceiling is well under this.
	maxParallelTools = 4
)

const systemPromptBase = `You are the user's assistant. You coordinate between the user and background tasks. Reply concisely.

# Decision order
First evaluate whether the user's request should continue an existing task (use ContinueTask) rather than creating a new one (StartTask). When the user refers to an existing task (e.g. "add to that task", "try again", "what about the last run?"), prefer ContinueTask. Use ListTasks/GetTask to decide when needed. When the user wants to run a reusable, named multi-step process the space already has, that is a workflow: use ListWorkflows then RunWorkflow rather than StartTask.

# Tools
- StartTask: create and schedule a new background task (long-running job, analysis). Tell the user it has started. The client presents the task as a deterministic card; do not promise a later assistant message.
- ListTasks: list recent tasks in the current conversation (up to 10). Use when the user asks what tasks they have or for recent activity.
- GetTask: get detail for one task by task_id. Use when the user asks about a specific task's status or result.
- ContinueTask: add a follow-up message to an existing task (new run). Use when the user wants to continue, retry, or add to an existing task.
- ListWorkflows: list the space's published workflows and the input each needs. Use when the user asks what workflows exist or wants to run one.
- RunWorkflow: start a run of a published workflow by workflow_id (find it with ListWorkflows), passing input that matches its input_schema. It runs in the background like a task. You cannot create or edit a workflow, only run one.
- GetWorkflowRun: get the status and result of a workflow run by workflow_run_id.

When starting or continuing a task, or running a workflow, tell the user it is running. Do not expose internal IDs.`

func currentSystemPrompt() string {
	return systemPromptBase + "\n\nToday's date: " + time.Now().Format("2006-01-02") + "."
}

// turnRunInput configures one conversation turn execution.
type turnRunInput struct {
	ConversationID  string
	Message         string
	Channel         string
	UserID          string
	SpaceID         string
	TaskService     *task.Service
	WorkflowService *workflow.Service
	AgentSummaries  []agentSummary
	TitleGenerator  llm.TitleGenerator
	StreamSink      llm.StreamSink
	// Fence is the conversation lease's token, stamped on every message-history
	// write this turn makes. Zero disables fencing. See
	// docs/design/server-coordination.md §7.
	Fence int64
}

// buildConversationTools builds this turn's task tools.
//
// sourceMessageID is the message the turn is answering, already stored. Every
// run these tools create records it, so the request a worker was given can be
// compared with what the person actually asked for.
func buildConversationTools(in turnRunInput, sourceMessageID *string) []llm.Tool {
	if in.Channel == convchannel.ChannelSystem || in.TaskService == nil {
		return nil
	}
	svc := in.TaskService
	tools := []llm.Tool{
		newStartTaskTool(
			newStartTaskServiceRunner(svc, in.ConversationID, in.SpaceID, in.UserID, sourceMessageID),
			in.AgentSummaries,
		),
	}
	if r := newListTasksStoreRunner(svc.Tasks); r != nil {
		tools = append(tools, newListTasksTool(in.ConversationID, r))
	}
	if r := newGetTaskServiceRunner(svc); r != nil {
		tools = append(tools, newGetTaskTool(in.ConversationID, r))
	}
	if r := newContinueTaskServiceRunner(svc, sourceMessageID); r != nil {
		tools = append(tools, newContinueTaskTool(in.ConversationID, in.UserID, r))
	}
	// Workflow tools are space-scoped: a published Workflow is a callable unit on
	// the same Task plane, so the foreground turn can invoke and observe one the
	// way it starts and reads a Task. It cannot author one; that stays a reviewed
	// publish action. See docs/proposals/assistant-orchestration-and-workflow-boundary.md §9.5.
	if wf := in.WorkflowService; wf != nil && in.SpaceID != "" {
		if r := newListWorkflowsServiceRunner(wf, in.SpaceID); r != nil {
			tools = append(tools, newListWorkflowsTool(r))
		}
		if r := newRunWorkflowServiceRunner(wf, in.SpaceID, in.UserID); r != nil {
			tools = append(tools, newRunWorkflowTool(r))
		}
		if r := newGetWorkflowRunServiceRunner(wf, in.SpaceID); r != nil {
			tools = append(tools, newGetWorkflowRunTool(r))
		}
	}
	return tools
}

type conversationBuffer struct {
	ctx            context.Context
	conversationID string
	msgStore       coreconv.MessageStore
	msgs           []llm.Message
	// fence stamps every persisted message with the turn's lease token so a
	// stale replica's write is rejected. Zero disables the check.
	fence int64
}

func (b *conversationBuffer) HistoryMessages() []llm.Message {
	return b.msgs
}

func (b *conversationBuffer) Append(m llm.Message) error {
	b.msgs = append(b.msgs, m)
	var toolCallID *string
	if m.Role == "tool" && m.ToolCallID != "" {
		toolCallID = &m.ToolCallID
	}
	var toolCallsJSON *string
	if m.Role == "assistant" && len(m.ToolCalls) > 0 {
		js, err := marshalToolCalls(m.ToolCalls)
		if err != nil {
			return fmt.Errorf("marshal tool calls: %w", err)
		}
		toolCallsJSON = js
	}
	// Reasoning state is persisted so a turn that resumes from the stored
	// conversation carries what the upstream protocol needs back. A failure to
	// encode it drops it rather than failing the turn: the run continues
	// without reasoning continuity, which is what a protocol without it does
	// anyway.
	var providerStateJSON *string
	if m.Role == "assistant" && m.ProviderState != nil {
		if encoded, err := json.Marshal(m.ProviderState); err == nil {
			js := string(encoded)
			providerStateJSON = &js
		}
	}
	// Non-text content is stored the same way and for the same reason: a Tier 1
	// turn replays from these rows, so an image a tool returned has to survive
	// or the next turn discusses something it can no longer see.
	var partsJSON *string
	if len(m.Parts) > 0 {
		if encoded, err := json.Marshal(m.Parts); err == nil {
			js := string(encoded)
			partsJSON = &js
		}
	}
	_, err := b.msgStore.AppendMessage(b.ctx, coreconv.AppendInput{
		ConversationID:    b.conversationID,
		Role:              m.Role,
		Content:           m.Content,
		ToolCallID:        toolCallID,
		ToolCallsJSON:     toolCallsJSON,
		ProviderStateJSON: providerStateJSON,
		PartsJSON:         partsJSON,
		Fence:             b.fence,
	})
	return err
}

func replayMessageFromStore(m coreconv.Message) llm.Message {
	toolCallID := ""
	if m.ToolCallID != nil {
		toolCallID = *m.ToolCallID
	}
	msg := llm.Message{Role: m.Role, Content: m.Content, ToolCallID: toolCallID}
	if m.ToolCallsJSON != nil && *m.ToolCallsJSON != "" {
		var toolCalls []llm.ToolCall
		if err := json.Unmarshal([]byte(*m.ToolCallsJSON), &toolCalls); err == nil {
			msg.ToolCalls = toolCalls
		}
	}
	if m.ProviderStateJSON != nil && *m.ProviderStateJSON != "" {
		var state llm.ProviderState
		if err := json.Unmarshal([]byte(*m.ProviderStateJSON), &state); err == nil {
			msg.ProviderState = &state
		}
	}
	if m.PartsJSON != nil && *m.PartsJSON != "" {
		var parts []llm.ContentPart
		if err := json.Unmarshal([]byte(*m.PartsJSON), &parts); err == nil {
			msg.Parts = parts
		}
	}
	return msg
}

type preparedRun struct {
	firstRound bool
	buffer     *conversationBuffer
	toolsList  []llm.Tool
}

func prepareRun(ctx context.Context, msgStore coreconv.MessageStore, in turnRunInput) (*preparedRun, error) {
	msgs, err := msgStore.ListMessages(ctx, in.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	firstRound := len(msgs) == 0
	channelPtr := &in.Channel
	incoming, err := msgStore.AppendMessage(ctx, coreconv.AppendInput{
		ConversationID: in.ConversationID,
		Role:           "user",
		Content:        in.Message,
		Channel:        channelPtr,
		Fence:          in.Fence,
	})
	if err != nil {
		return nil, fmt.Errorf("append incoming message: %w", err)
	}
	var sourceMessageID *string
	if incoming != nil && incoming.ID != "" {
		sourceMessageID = &incoming.ID
	}
	llmMsgs := make([]llm.Message, 0, len(msgs)+2)
	for _, m := range msgs {
		llmMsgs = append(llmMsgs, replayMessageFromStore(m))
	}
	llmMsgs = append(llmMsgs, llm.Message{Role: "user", Content: in.Message})

	return &preparedRun{
		firstRound: firstRound,
		buffer: &conversationBuffer{
			ctx:            ctx,
			conversationID: in.ConversationID,
			msgStore:       msgStore,
			msgs:           llmMsgs,
			fence:          in.Fence,
		},
		toolsList: buildConversationTools(in, sourceMessageID),
	}, nil
}

func executeRun(ctx context.Context, llmClient llm.LLMClient, in turnRunInput, prepared *preparedRun) (string, error) {
	tools := llm.NewToolRegistry()
	tools.AppendTools(prepared.toolsList...)

	reply, _, _, err := agent.RunLoop(ctx, agent.RunLoopOpts{
		LLMClient:        llmClient,
		SystemPrompt:     currentSystemPrompt(),
		ToolRegistry:     tools,
		MaxIter:          maxIterations,
		History:          prepared.buffer,
		StreamSink:       in.StreamSink,
		Policy:           agent.AllowAllPolicy(),
		MaxParallelTools: maxParallelTools,
	})
	if err != nil {
		return "", err
	}
	return reply, nil
}

func maybeUpdateTitle(ctx context.Context, convStore coreconv.Store, in turnRunInput, prepared *preparedRun) {
	if !prepared.firstRound || in.Message == "" || in.TitleGenerator == nil {
		return
	}
	if title, _, _, err := in.TitleGenerator.GenerateTitle(ctx, in.Message); err == nil && title != "" {
		_ = convStore.UpdateConversationTitle(ctx, in.ConversationID, title)
	}
}

func runLoop(ctx context.Context, convStore coreconv.Store, msgStore coreconv.MessageStore, llmClient llm.LLMClient, in turnRunInput) (string, error) {
	prepared, err := prepareRun(ctx, msgStore, in)
	if err != nil {
		return "", err
	}
	reply, err := executeRun(ctx, llmClient, in, prepared)
	if err != nil {
		return "", err
	}
	maybeUpdateTitle(ctx, convStore, in, prepared)
	return reply, nil
}

// runConversationTurn executes one turn. Streaming is enabled when StreamSink is non-nil.
func runConversationTurn(ctx context.Context, convStore coreconv.Store, msgStore coreconv.MessageStore, llmClient llm.LLMClient, in turnRunInput) (string, error) {
	if llmClient == nil {
		if in.StreamSink != nil {
			return "", fmt.Errorf("conversation stream LLM not configured")
		}
		return "", fmt.Errorf("conversation LLM not configured")
	}
	return runLoop(ctx, convStore, msgStore, llmClient, in)
}

func marshalToolCalls(toolCalls []llm.ToolCall) (*string, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(toolCalls)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}
