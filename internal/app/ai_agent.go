package app

import (
	"context"
	"encoding/json"

	agentai "eiksy/internal/ai"
)

// AIBackendAgent adapts Eiksy's existing AI application service to the
// transport-agnostic agent event protocol used by CLI/TUI frontends.
//
// The application service remains the single owner of provider selection,
// credentials, context construction, native tools, policy, approval, execution,
// persistence, and audit. This adapter only translates the interaction into
// frontend-neutral events.
type AIBackendAgent struct {
	service *Service
}

// NewAIBackendAgent returns an Agent backed by the existing Eiksy AI service.
func NewAIBackendAgent(service *Service) agentai.Agent {
	return &AIBackendAgent{service: service}
}

func (a *AIBackendAgent) Run(ctx context.Context, sessionID string, input string) (<-chan agentai.Event, error) {
	events := make(chan agentai.Event, 8)
	go a.run(ctx, events, sessionID, input)
	return events, nil
}

func (a *AIBackendAgent) run(ctx context.Context, events chan<- agentai.Event, sessionID, input string) {
	defer close(events)

	if ctx.Err() != nil {
		events <- agentai.Event{Type: agentai.EventCancellation, Err: ctx.Err()}
		return
	}

	events <- agentai.Event{Type: agentai.EventMessageStarted}

	state := a.service.store.AIState()
	provider, err := a.service.activeConfiguredProvider(state)
	if err != nil {
		events <- agentai.Event{Type: agentai.EventError, Err: err}
		return
	}
	if provider == nil {
		events <- agentai.Event{Type: agentai.EventError, Err: errNoAIProviderConfigured}
		return
	}

	tools := []map[string]any(nil)
	if commandToolEnabled(state.CommandPolicy, nativeSSHExecPolicyToolID) || commandToolEnabled(state.CommandPolicy, "sftp") {
		tools = openAIToolDefinitions()
	}
	messages := a.service.nativeMessagesFromState(state, sessionID)
	messages = append(messages, nativeChatMessage{Role: "user", Content: input})
	reply, _, err := a.service.runNativeToolLoopWithEvents(
		ctx,
		provider,
		state.ChatSessionID,
		state.CommandPolicy,
		sessionID,
		input,
		messages,
		tools,
		func(event agentai.Event) { events <- event },
	)
	if err != nil {
		if ctx.Err() != nil {
			events <- agentai.Event{Type: agentai.EventCancellation, Err: ctx.Err()}
		} else {
			events <- agentai.Event{Type: agentai.EventError, Err: err}
		}
		return
	}

	// A tool call can suspend the turn while waiting for user approval. The
	// returned value is then an approval-required JSON payload, not assistant
	// text. Do not fall back to AgentMessages here: that history may end with
	// an earlier assistant reply, which would be replayed as if it answered
	// the current prompt.
	var continuation struct {
		Status    string `json:"status"`
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal([]byte(reply), &continuation) == nil && continuation.Status == "approval_required" {
		latest := a.service.store.AIState()
		if latest.PendingNativeToolCall != nil && latest.PendingNativeToolCall.RequestID == continuation.RequestID {
			events <- agentai.Event{Type: agentai.EventMessageFinished}
			return
		}
	}

	if reply != "" {
		// The current OpenAI-compatible backend returns a complete assistant
		// message. It is exposed as one delta now; future streaming providers can
		// emit multiple deltas without changing the Agent contract.
		events <- agentai.Event{Type: agentai.EventTextDelta, Content: reply}
	}
	events <- agentai.Event{Type: agentai.EventMessageFinished, Content: reply}
}

var errNoAIProviderConfigured = errorString("no AI provider is configured")

type errorString string

func (e errorString) Error() string { return string(e) }

