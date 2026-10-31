package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/sessions"
)

// ClearChat starts a fresh ordinary AI chat. Agent history and pending agent
// approvals belong to a separate interaction and must remain untouched.
func (s *Service) ClearChat() error {
	state := s.store.AIState()
	state.Messages = []ai.ChatMessage{}
	state.ChatSessionID = fmt.Sprintf("chat-%d", time.Now().UTC().UnixNano())
	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("clear AI chat: %w", err)
	}
	return nil
}

func (s *Service) UpdateCommandPolicy(policy ai.CommandPolicy) error {
	state := s.store.AIState()
	state.CommandPolicy = normalizeCommandPolicy(policy)
	if err := s.store.UpdateAIState(state); err != nil {
		return fmt.Errorf("persist command policy: %w", err)
	}
	return nil
}

func (s *Service) ResolveCommandPolicyRequest(requestID string, mode ai.CommandPermissionMode) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" { return fmt.Errorf("request id is required") }
	state := s.store.AIState()
	policy := normalizeCommandPolicy(state.CommandPolicy)
	index := -1
	var request ai.CommandRequest
	for i, entry := range policy.PendingRequests {
		if entry.ID == requestID { index = i; request = entry; break }
	}
	if index < 0 { return fmt.Errorf("command request %q not found", requestID) }
	removePending := func() { policy.PendingRequests = append(policy.PendingRequests[:index], policy.PendingRequests[index+1:]...) }

	if mode == ai.CommandPermissionModeDeny {
		removePending()
		state.CommandPolicy = policy
		if state.PendingNativeToolCall != nil && state.PendingNativeToolCall.RequestID == requestID {
			pending := state.PendingNativeToolCall
			state.PendingNativeToolCall = nil
			if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist denied command request: %w", err) }
			if request.ToolID == nativeSFTPWriteToolName {
				s.emitNativeSFTPOperation(nativeSFTPWriteToolName, "denied", request.SessionID, request.Command, 0, "required", "user denied the SFTP write", 0)
			} else {
				s.emitNativeOperation("denied", request.SessionID, request.Command, sessions.CommandExecutionResult{ExitCode: -1}, "required", "user denied the command")
			}
			return s.resumePendingNativeToolCall(s.resolveContext(context.Background()), pending, `{"status":"approval_denied","reason":"user denied the operation"}`)
		}
		message := fmt.Sprintf("Denied command request for `%s`.", request.Command)
		state.AgentMessages = append(state.AgentMessages, ai.ChatMessage{Role: "assistant", Content: message})
		if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist denied command request: %w", err) }
		s.emitFn("agent:message", map[string]string{"role":"assistant","content":message})
		return nil
	}
	switch mode { case ai.CommandPermissionModeAlways, ai.CommandPermissionModeSession, ai.CommandPermissionModeNow: default: return fmt.Errorf("unsupported permission mode %q", mode) }
	if request.ToolID != nativeSFTPWriteToolName && !commandToolEnabled(policy, request.ToolID) { return fmt.Errorf("tool %q is disabled in command policy", request.ToolID) }
	if normalizeCommandTarget(request.Target) == commandTargetLocal {
		return fmt.Errorf("local approval execution is not available in this runtime")
	}
	if normalizeCommandTarget(request.Target) != commandTargetSSH {
		return fmt.Errorf("unsupported command execution target %q", request.Target)
	}
	if _, ok := s.runtimeTab(request.SessionID); !ok {
		removePending(); state.CommandPolicy = policy
		if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist stale command request cleanup: %w", err) }
		return fmt.Errorf("active session %q not found", request.SessionID)
	}
	if state.PendingNativeToolCall != nil && state.PendingNativeToolCall.RequestID == requestID {
		pending := state.PendingNativeToolCall
		if pendingToolID := policyToolIDForNativeTool(pending.ToolName); pendingToolID != request.ToolID {
			return fmt.Errorf("pending tool %q does not match approved tool %q", pending.ToolName, request.ToolID)
		}
		if strings.TrimSpace(pending.SessionID) != request.SessionID {
			return fmt.Errorf("pending tool session %q does not match approved session %q", pending.SessionID, request.SessionID)
		}
	}
	if request.ToolID == nativeSFTPWriteToolName {
		if state.PendingNativeToolCall == nil || state.PendingNativeToolCall.RequestID != requestID || state.PendingNativeToolCall.ToolName != nativeSFTPWriteToolName { return fmt.Errorf("pending SFTP write call %q not found", requestID) }
		pending := state.PendingNativeToolCall
		if pendingToolID := policyToolIDForNativeTool(pending.ToolName); pendingToolID != request.ToolID {
			return fmt.Errorf("pending tool %q does not match approved tool %q", pending.ToolName, request.ToolID)
		}
		if strings.TrimSpace(pending.SessionID) != request.SessionID {
			return fmt.Errorf("pending tool session %q does not match approved session %q", pending.SessionID, request.SessionID)
		}
		state.PendingNativeToolCall = nil
		if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist approved SFTP write request: %w", err) }
		var args struct { SessionID string `json:"sessionId"`; Path string `json:"path"`; Content string `json:"content"`; Reason string `json:"reason"` }
		decoder := json.NewDecoder(strings.NewReader(pending.ToolArguments)); decoder.DisallowUnknownFields()
		if err := decoder.Decode(&args); err != nil { return fmt.Errorf("decode pending SFTP write: %w", err) }
		args.SessionID = strings.TrimSpace(args.SessionID); args.Path = strings.TrimSpace(args.Path)
		if args.SessionID != request.SessionID || args.Path == "" { return fmt.Errorf("pending SFTP write no longer matches approved request") }
		if len([]byte(args.Content)) > maxNativeSFTPWriteSize { return fmt.Errorf("SFTP write content exceeds %d byte limit", maxNativeSFTPWriteSize) }
		tab, ok := s.runtimeTab(args.SessionID)
		if !ok || tab.ProtocolID != "ssh" || tab.Status != "connected" { return fmt.Errorf("sftp.write requires a connected active SSH session") }
		start := time.Now(); err := s.SaveSFTPFile(args.SessionID, args.Path, args.Content); duration := time.Since(start).Milliseconds()
		status := "executed"; if err != nil { status = "execution_failed" }
		s.emitNativeSFTPOperation(nativeSFTPWriteToolName, status, args.SessionID, args.Path, len([]byte(args.Content)), string(mode), errString(err), duration)
		result := map[string]any{"status":status,"sessionId":args.SessionID,"path":args.Path,"bytes":len([]byte(args.Content))}
		if err != nil { result["error"] = err.Error() }
		encoded, err := json.Marshal(result); if err != nil { return err }
		return s.resumePendingNativeToolCall(s.resolveContext(context.Background()), pending, string(encoded))
	}
	decision, reason := evaluateCommandPolicy(policy, request.ToolID, request.SessionID, request.Command)
	if decision == commandPolicyDecisionDeny {
		removePending(); state.CommandPolicy = policy
		message := fmt.Sprintf("Command denied by Command Policy: `%s`", request.Command)
		if strings.TrimSpace(reason) != "" { message += fmt.Sprintf(" (%s)", reason) }
		state.AgentMessages = append(state.AgentMessages, ai.ChatMessage{Role:"assistant",Content:message})
		if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist policy-denied command request: %w", err) }
		s.emitFn("agent:message", map[string]string{"role":"assistant","content":message})
		return nil
	}
	removePending()
	if request.ToolID != nativeSFTPWriteToolName {
		switch mode {
		case ai.CommandPermissionModeAlways:
			policy.CommandRules = append(policy.CommandRules, ai.CommandRule{Target: normalizeCommandTarget(request.Target), ToolID:request.ToolID,Pattern:request.Command,Action:ai.CommandPermissionAllow,Description:"Approved by user: always allow this exact command"})
		case ai.CommandPermissionModeSession:
			policy.CommandRules = append(policy.CommandRules, ai.CommandRule{Target: normalizeCommandTarget(request.Target), ToolID:request.ToolID,SessionID:request.SessionID,Pattern:request.Command,Action:ai.CommandPermissionAllow,Description:"Approved by user: allow this exact command for this session"})
		}
	}
	policy.CommandRules = normalizeCommandRules(policy.CommandRules); state.CommandPolicy = policy
	if state.PendingNativeToolCall != nil && state.PendingNativeToolCall.RequestID == requestID {
		pending := state.PendingNativeToolCall
		state.PendingNativeToolCall = nil
		if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist approved command request: %w", err) }
		result, err := s.executeSessionCommandResult(request.SessionID, request.Command)
		status := "executed"; if err != nil { status = "execution_failed" }
		s.recordCommandAudit(pending.ProviderID, request.SessionID, request.Command, string(decision), string(mode), status, result.ExitCode, result.DurationMs, ai.CommandAuditEvent{ErrorType: string(result.ErrorType), Error: result.Error})
		s.emitNativeOperation(status, request.SessionID, request.Command, result, string(mode), errString(err))
		payload := map[string]any{"status":status,"sessionId":request.SessionID,"command":request.Command,"result":result}
		if err != nil { payload["message"] = err.Error() }
		encoded, err := json.Marshal(payload); if err != nil { return err }
		return s.resumePendingNativeToolCall(s.resolveContext(context.Background()), pending, string(encoded))
	}
	result, err := s.executeSessionCommandResult(request.SessionID, request.Command)
	message := fmt.Sprintf("Approved command in session %s: `%s`", request.SessionID, request.Command)
	if err != nil { message = fmt.Sprintf("Command execution failed: %v", err) } else if !result.Success { message = fmt.Sprintf("Command completed with exit code %d in session %s: `%s`", result.ExitCode, request.SessionID, request.Command) }
	state.AgentMessages = append(state.AgentMessages, ai.ChatMessage{Role:"assistant",Content:message})
	if err := s.store.UpdateAIState(state); err != nil { return fmt.Errorf("persist command result: %w", err) }
	s.emitFn("agent:message", map[string]string{"role":"assistant","content":message})
	return nil
}

func normalizeCommandPolicy(policy ai.CommandPolicy) ai.CommandPolicy {
	defaults := []ai.CommandTool{
		{ID: "shell", Name: "Shell command", Description: "Run command in active SSH session", Enabled: true},
		{ID: "sftp", Name: "SFTP operations", Description: "Browse and edit files over SFTP", Enabled: true},
		{ID: "search", Name: "Search", Description: "Run grep/find-like queries on host", Enabled: true},
	}
	toolByID := make(map[string]ai.CommandTool, len(defaults))
	order := make([]string, 0, len(defaults))
	for _, tool := range defaults {
		toolByID[tool.ID] = tool
		order = append(order, tool.ID)
	}
	for _, tool := range policy.Tools {
		id := strings.ToLower(strings.TrimSpace(tool.ID))
		if id == "" {
			continue
		}
		clean := ai.CommandTool{
			ID:          id,
			Name:        strings.TrimSpace(tool.Name),
			Description: strings.TrimSpace(tool.Description),
			Enabled:     tool.Enabled,
		}
		if clean.Name == "" {
			clean.Name = id
		}
		if _, exists := toolByID[id]; !exists {
			order = append(order, id)
		}
		toolByID[id] = clean
	}
	normalizedTools := make([]ai.CommandTool, 0, len(order))
	for _, id := range order {
		normalizedTools = append(normalizedTools, toolByID[id])
	}
	policy.Tools = normalizedTools
	policy.LocalDocsPath = strings.TrimSpace(policy.LocalDocsPath)
	if policy.PendingRequests == nil {
		policy.PendingRequests = []ai.CommandRequest{}
	}
	filteredRequests := make([]ai.CommandRequest, 0, len(policy.PendingRequests))
	for _, request := range policy.PendingRequests {
		command := strings.TrimSpace(request.Command)
		toolID := strings.ToLower(strings.TrimSpace(request.ToolID))
		if command == "" || toolID == "" {
			continue
		}
		filteredRequests = append(filteredRequests, ai.CommandRequest{
			Target: normalizeCommandTarget(request.Target),
			ID:          strings.TrimSpace(request.ID),
			ToolID:      toolID,
			SessionID:   strings.TrimSpace(request.SessionID),
			Command:     command,
			Reason:      strings.TrimSpace(request.Reason),
			RequestedAt: strings.TrimSpace(request.RequestedAt),
		})
	}
	policy.PendingRequests = filteredRequests
	return policy
}

func commandToolEnabled(policy ai.CommandPolicy, toolID string) bool {
	toolID = strings.ToLower(strings.TrimSpace(toolID))
	if toolID == "" {
		return false
	}
	normalized := normalizeCommandPolicy(policy)
	for _, tool := range normalized.Tools {
		if tool.ID == toolID {
			return tool.Enabled
		}
	}
	return false
}

func policyToolIDForNativeTool(toolName string) string {
	switch toolName {
	case nativeSSHExecToolName:
		return nativeSSHExecPolicyToolID
	case nativeSFTPWriteToolName:
		return nativeSFTPWriteToolName
	default:
		return toolName
	}
}
