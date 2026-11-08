package app

import (
	"context"
	"fmt"
	"strings"

	"eiksy/internal/domain/sessions"
)

type sshStructuredCommandExecutor interface {
	ExecCommandResult(context.Context, string, string) (sessions.CommandExecutionResult, error)
}

func (s *Service) ExecuteCommand(sessionID, command string) (sessions.CommandExecutionResult, error) {
	return s.executeSessionCommandResult(sessionID, command)
}

func (s *Service) executeSessionCommandResult(sessionID, command string) (sessions.CommandExecutionResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	command = strings.TrimSpace(command)
	if sessionID == "" {
		return sessions.CommandExecutionResult{ExitCode: -1}, fmt.Errorf("session id is required")
	}
	if command == "" {
		return sessions.CommandExecutionResult{ExitCode: -1}, fmt.Errorf("command cannot be empty")
	}
	if s.sshManager == nil {
		return sessions.CommandExecutionResult{ExitCode: -1}, fmt.Errorf("ssh manager is not configured")
	}

	for _, tab := range s.store.RuntimeTabs() {
		if tab.ID != sessionID {
			continue
		}
		if strings.ToLower(strings.TrimSpace(tab.ProtocolID)) != "ssh" {
			return sessions.CommandExecutionResult{ExitCode: -1}, fmt.Errorf("session %q is not an ssh session", sessionID)
		}
		if strings.ToLower(strings.TrimSpace(tab.Status)) != "connected" {
			return sessions.CommandExecutionResult{ExitCode: -1, ErrorType: sessions.CommandExecutionErrorConnection}, fmt.Errorf("session %q is not connected", sessionID)
		}
		executor, ok := s.sshManager.(sshStructuredCommandExecutor)
		if !ok {
			return sessions.CommandExecutionResult{ExitCode: -1}, fmt.Errorf("ssh manager does not support structured command execution")
		}
		s.emitDiagnosticLogf("INFO", "SSH_EXEC_START session=%s commandBytes=%d", sessionID, len([]byte(command)))
		result, execErr := executor.ExecCommandResult(s.resolveContext(s.ctx), sessionID, command)
		s.emitDiagnosticLogf("INFO", "SSH_EXEC_FINISHED session=%s exitCode=%d stdoutBytes=%d stderrBytes=%d durationMs=%d error=%t", sessionID, result.ExitCode, len([]byte(result.Stdout)), len([]byte(result.Stderr)), result.DurationMs, execErr != nil)
		return result, execErr
	}
	return sessions.CommandExecutionResult{ExitCode: -1}, fmt.Errorf("active ssh session %q not found", sessionID)
}
