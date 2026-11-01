package ai

// CommandAuditEvent records the security-relevant lifecycle of an AI-issued command.
// It intentionally excludes command output and user message content.
type CommandAuditEvent struct {
	ID             string `json:"id"`
	At             string `json:"at"`
	ChatSessionID  string `json:"chatSessionId,omitempty"`
	ProviderID     string `json:"providerId,omitempty"`
	Target         string `json:"target,omitempty"`
	ToolID         string `json:"toolId"`
	SessionID      string `json:"sessionId"`
	Command        string `json:"command"`
	PolicyDecision string `json:"policyDecision"`
	Approval       string `json:"approval,omitempty"`
	Result         string `json:"result"`
	ExitCode       int    `json:"exitCode"`
	DurationMs     int64  `json:"durationMs"`
	ErrorType      string `json:"errorType,omitempty"`
	Error          string `json:"error,omitempty"`
}
