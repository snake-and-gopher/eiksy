package ai

import "context"

// EventType identifies a lifecycle event emitted by an AI agent.
type EventType string

const (
	EventMessageStarted  EventType = "message_started"
	EventTextDelta       EventType = "text_delta"
	EventToolStarted     EventType = "tool_started"
	EventToolOutput      EventType = "tool_output"
	EventToolFinished    EventType = "tool_finished"
	EventApprovalRequired EventType = "approval_required"
	EventMessageFinished EventType = "message_finished"
	EventError           EventType = "error"
	EventCancellation    EventType = "cancellation"
)

// Event is a normalized, transport-agnostic agent event.
// Payload is intentionally opaque to keep the protocol independent of any TUI framework.
type ApprovalRequest struct {
	RequestID string
	ToolID    string
	Target    string
	SessionID string
	Command   string
	Reason    string
}

type Event struct {
	Type     EventType
	Content  string
	Tool     string
	Err      error
	Approval *ApprovalRequest
}

// Agent starts an AI interaction and emits normalized lifecycle events.
type Agent interface {
	Run(ctx context.Context, sessionID string, input string) (<-chan Event, error)
}
