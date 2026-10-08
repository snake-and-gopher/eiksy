package ai

type ProviderClass string

const (
	ProviderClassOpenAICompatible ProviderClass = "openai_compatible"
	ProviderClassLocalOpenAI      ProviderClass = "local_openai"
)

type ProviderDescriptor struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Class       ProviderClass `json:"class"`
	Model       string        `json:"model"`
	Endpoint    string        `json:"endpoint,omitempty"`
	DownloadURL string        `json:"downloadUrl,omitempty"`
	LocalPath   string        `json:"localPath,omitempty"`
	Status      string        `json:"status"`
	Selected    bool          `json:"selected"`
	Token       string        `json:"-"`
	HasToken    bool          `json:"hasToken"`
	Configured  bool          `json:"configured"`
}

type ContextPolicy struct {
	SendTerminalSelection bool `json:"sendTerminalSelection"`
	SendRecentOutput      bool `json:"sendRecentOutput"`
	RequireConfirmation   bool `json:"requireConfirmation"`
}

type CommandPermissionMode string

const (
	CommandPermissionModeNow     CommandPermissionMode = "now"
	CommandPermissionModeAlways  CommandPermissionMode = "always"
	CommandPermissionModeSession CommandPermissionMode = "session"
	CommandPermissionModeDeny    CommandPermissionMode = "deny"
)

type CommandPermissionAction string

const (
	CommandPermissionAllow CommandPermissionAction = "allow"
	CommandPermissionAsk   CommandPermissionAction = "ask"
	CommandPermissionDeny  CommandPermissionAction = "deny"
)

type CommandTool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type CommandRule struct {
	ToolID      string                  `json:"toolId,omitempty"`
	SessionID   string                  `json:"sessionId,omitempty"`
	Pattern     string                  `json:"pattern"`
	Action      CommandPermissionAction `json:"action"`
	Description string                  `json:"description,omitempty"`
}

type CommandRequest struct {
	ID          string `json:"id"`
	ToolID      string `json:"toolId"`
	SessionID   string `json:"sessionId"`
	Command     string `json:"command"`
	Reason      string `json:"reason,omitempty"`
	RequestedAt string `json:"requestedAt"`
}

type PendingNativeToolCall struct {
	RequestID     string `json:"requestId"`
	ProviderID    string `json:"providerId"`
	ToolCallID    string `json:"toolCallId"`
	ToolName      string `json:"toolName"`
	ToolArguments string `json:"toolArguments"`
	UserMessage   string `json:"userMessage"`
	SessionID     string `json:"sessionId"`
	MessagesJSON  string `json:"messagesJson"`
}

type CommandPolicy struct {
	Tools               []CommandTool       `json:"tools"`
	CommandRules        []CommandRule       `json:"commandRules,omitempty"`
	PendingRequests     []CommandRequest    `json:"pendingRequests"`
	LocalDocsPath       string              `json:"localDocsPath,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatSession struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	CreatedAt string        `json:"createdAt"`
	UpdatedAt string        `json:"updatedAt"`
	Messages  []ChatMessage `json:"messages,omitempty"`
}

type WorkspaceState struct {
	Providers             []ProviderDescriptor   `json:"providers"`
	ContextPolicy         ContextPolicy          `json:"contextPolicy"`
	CommandPolicy         CommandPolicy          `json:"commandPolicy"`
	Messages              []ChatMessage          `json:"messages"`
	ChatSessionID         string                 `json:"chatSessionId"`
	ChatSessions          []ChatSession          `json:"chatSessions,omitempty"`
	PendingNativeToolCall *PendingNativeToolCall `json:"pendingNativeToolCall,omitempty"`
}
