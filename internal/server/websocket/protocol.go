// Package wsconn defines the typed event protocol for WebSocket communication
// between the portal frontend and the server. All messages are JSON envelopes
// with a "type" field and a typed "payload".
package websocket

import "encoding/json"

// Envelope is the wire format for every WebSocket message (both directions).
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Encode marshals an event type and payload into JSON bytes suitable for WebSocket write.
func Encode(eventType string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: eventType, Payload: raw})
}

// Decode unmarshals raw bytes into an Envelope.
func Decode(data []byte) (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(data, &env)
	return env, err
}

// DecodePayload unmarshals the envelope's raw payload into the target type T.
func DecodePayload[T any](env Envelope) (T, error) {
	var v T
	err := json.Unmarshal(env.Payload, &v)
	return v, err
}

// ---------------------------------------------------------------------------
// Client → Server event types
// ---------------------------------------------------------------------------

const (
	TypeConversationCreate  = "conversation.create"
	TypeConversationMessage = "conversation.message"
	TypeSubscribeTask       = "subscribe.task"
	TypeUnsubscribeTask     = "unsubscribe.task"
)

// ConversationCreate is the payload for TypeConversationCreate.
type ConversationCreate struct {
	Channel string `json:"channel,omitempty"`
	Message string `json:"message"`
}

// ConversationMessage is the payload for TypeConversationMessage.
type ConversationMessage struct {
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
}

// Remote Control agent socket (client → server). These travel on the dedicated
// device/agent WebSocket a local session dials out to, not the per-space browser
// socket. See docs/design/remote-control.md.
const (
	TypeAgentRegister         = "agent.register"
	TypeAgentHeartbeat        = "agent.heartbeat"
	TypeAgentEvent            = "agent.event"
	TypeAgentApproval         = "agent.approval"
	TypeAgentApprovalResolved = "agent.approval_resolved"
)

// AgentRegister is the payload for TypeAgentRegister: a local session announcing
// itself. On a reconnect it carries the session id it was previously assigned, to
// reattach rather than create a new session.
type AgentRegister struct {
	SessionID   string `json:"session_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Host        string `json:"host,omitempty"`
}

// AgentEvent is the payload for TypeAgentEvent: one relayed run event. Phase 1
// carries a content delta (matching the Task output stream); richer structured
// records are a later enrichment.
type AgentEvent struct {
	Delta string `json:"delta,omitempty"`
}

// AgentApproval is the payload for TypeAgentApproval: a tool-approval prompt the
// session raised, to be shown on connected devices so one of them can answer.
type AgentApproval struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Summary string `json:"summary,omitempty"`
}

// AgentApprovalResolved is the payload for TypeAgentApprovalResolved: an approval
// was answered (locally or remotely), so devices dismiss their copy.
type AgentApprovalResolved struct {
	ID string `json:"id"`
}

// Remote Control agent socket (server → client).
const (
	TypeAgentRegistered       = "agent.registered"
	TypeAgentPrompt           = "agent.prompt"
	TypeAgentApprovalResponse = "agent.approval_response"
	TypeAgentCancel           = "agent.cancel"
)

// AgentRegistered is the payload for TypeAgentRegistered: the server's reply to a
// register, carrying the session id that is the stream key and the URL another
// device opens.
type AgentRegistered struct {
	SessionID string `json:"session_id"`
}

// AgentPrompt is the payload for TypeAgentPrompt: a follow-up message another
// device sent, to be delivered into the local session as if the user typed it.
type AgentPrompt struct {
	Content string `json:"content"`
}

// AgentApprovalResponse is the payload for TypeAgentApprovalResponse: a decision
// another device made on a pending approval. Decision is "once", "session", or
// "deny".
type AgentApprovalResponse struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
}

// SubscribeTask is the payload for TypeSubscribeTask.
type SubscribeTask struct {
	TaskID string `json:"task_id"`
}

// UnsubscribeTask is the payload for TypeUnsubscribeTask.
type UnsubscribeTask struct {
	TaskID string `json:"task_id"`
}

// ---------------------------------------------------------------------------
// Server → Client event types
// ---------------------------------------------------------------------------

const (
	TypeConversationCreated = "conversation.created"
	TypeMessageDelta        = "conversation.message.delta"
	TypeMessageQueued       = "conversation.message.queued"
	TypeMessageDequeued     = "conversation.message.dequeued"
	TypeMessageCompleted    = "conversation.message.completed"
	TypeConversationError   = "conversation.error"
	TypeTaskStatusChanged   = "task.status.changed"
	TypeTaskStreamDelta     = "task.stream.delta"
	TypeTaskStreamDone      = "task.stream.done"
	TypeSystemError         = "system.error"
)

// ConversationCreated is the payload for TypeConversationCreated.
type ConversationCreated struct {
	ConversationID string `json:"conversation_id"`
}

// MessageDelta is the payload for TypeMessageDelta.
type MessageDelta struct {
	ConversationID string `json:"conversation_id"`
	Delta          string `json:"delta"`
}

// MessageQueued is the payload for TypeMessageQueued: the message arrived while a
// turn was running and will run as its own turn once that one finishes.
type MessageQueued struct {
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
	// Position is 1-based: 1 is the next turn to run after the current one.
	Position int `json:"position"`
}

// MessageDequeued is the payload for TypeMessageDequeued: a queued message is
// starting its own turn now.
type MessageDequeued struct {
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
}

// MessageCompleted is the payload for TypeMessageCompleted.
type MessageCompleted struct {
	ConversationID string `json:"conversation_id"`
	// QueuedRemaining is how many messages are still waiting for their turn. A
	// client uses it to decide whether the conversation is idle or merely between
	// turns.
	QueuedRemaining int `json:"queued_remaining,omitempty"`
}

// ErrorCodeQueueFull marks a conversation error that refused a new message
// because the conversation's queue is full. It is not a failure of the turn in
// progress, which is still running — a client must not read it as "the
// conversation went idle".
const ErrorCodeQueueFull = "queue_full"

// ConversationError is the payload for TypeConversationError.
type ConversationError struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Error          string `json:"error"`
	// Code is an optional machine-readable reason. Empty means the turn itself failed.
	Code string `json:"code,omitempty"`
}

// TaskStatusChanged is the payload for TypeTaskStatusChanged.
type TaskStatusChanged struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Title  string `json:"title,omitempty"`
}

// TaskStreamDelta is the payload for TypeTaskStreamDelta.
type TaskStreamDelta struct {
	TaskID string `json:"task_id"`
	Delta  string `json:"delta"`
}

// TaskStreamDone is the payload for TypeTaskStreamDone.
type TaskStreamDone struct {
	TaskID string `json:"task_id"`
}

// SystemError is the payload for TypeSystemError.
type SystemError struct {
	Error string `json:"error"`
}
