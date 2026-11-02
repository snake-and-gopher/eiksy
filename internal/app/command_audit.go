package app

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"eiksy/internal/domain/ai"
)

const maxCommandAuditEvents = 500
const maxCommandAuditText = 4096

type commandAuditBuffer struct {
	mu     sync.RWMutex
	events []ai.CommandAuditEvent
}

var commandAuditBuffers sync.Map

type persistentCommandAuditStore interface {
	AppendCommandAudit(ai.CommandAuditEvent) error
	CommandAuditTrail() []ai.CommandAuditEvent
}

var sensitiveCommandPatterns = []struct {
	pattern *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)(authorization\s*:\s*(?:bearer|basic)\s+)[^\s'"]+`), `$1[REDACTED]`},
	{regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key|authorization)\s*[:=]\s*["']?[^\s,"']+["']?`), `$1=[REDACTED]`},
	{regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key)\s+[^\s]+`), `$1 [REDACTED]`},
	{regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api[-_]?key|authorization))(?:[=\s]+)[^\s]+`), `$1=[REDACTED]`},
	{regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_]*(?:PASSWORD|PASSWD|TOKEN|SECRET|API[_-]?KEY|AUTHORIZATION)[A-Za-z0-9_]*)\s*=\s*[^\s]+`), `$1=[REDACTED]`},
	{regexp.MustCompile(`(?i)(https?://[^\s/@:]+:)[^\s/@]+(@)`), `$1[REDACTED]$2`},
}

func (s *Service) recordCommandAudit(providerID, sessionID, command, policyDecision, approval, result string, exitCode int, durationMs int64, extra ai.CommandAuditEvent) {
	s.recordCommandAuditForTarget(commandTargetSSH, nativeSSHExecPolicyToolID, providerID, sessionID, command, policyDecision, approval, result, exitCode, durationMs, extra)
}

func (s *Service) recordCommandAuditForTarget(target, toolID, providerID, sessionID, command, policyDecision, approval, result string, exitCode int, durationMs int64, extra ai.CommandAuditEvent) {
	if s == nil { return }
	event := ai.CommandAuditEvent{
		ID: "audit-cmd-" + time.Now().UTC().Format("20060102T150405.000000000Z"),
		At: time.Now().UTC().Format(time.RFC3339Nano),
		ChatSessionID: redactAuditValue(s.store.AIState().ChatSessionID),
		ProviderID: redactAuditValue(providerID),
		Target: redactAuditValue(target),
		ToolID: redactAuditValue(toolID),
		SessionID: redactAuditValue(sessionID),
		Command: redactAuditValue(command),
		PolicyDecision: redactAuditValue(policyDecision),
		Approval: redactAuditValue(approval),
		Result: redactAuditValue(result),
		ExitCode: exitCode,
		DurationMs: durationMs,
		ErrorType: redactAuditValue(extra.ErrorType),
		Error: redactAuditValue(extra.Error),
	}
	if persistent, ok := s.store.(persistentCommandAuditStore); ok {
		if err := persistent.AppendCommandAudit(event); err == nil { return }
	}
	value, _ := commandAuditBuffers.LoadOrStore(s, &commandAuditBuffer{})
	buffer := value.(*commandAuditBuffer)
	buffer.mu.Lock(); defer buffer.mu.Unlock()
	buffer.events = append(buffer.events, event)
	if len(buffer.events) > maxCommandAuditEvents {
		buffer.events = append([]ai.CommandAuditEvent(nil), buffer.events[len(buffer.events)-maxCommandAuditEvents:]...)
	}
}

func (s *Service) GetCommandAuditTrail() []ai.CommandAuditEvent {
	if s == nil { return nil }
	if persistent, ok := s.store.(persistentCommandAuditStore); ok {
		events := persistent.CommandAuditTrail()
		result := make([]ai.CommandAuditEvent, 0, len(events))
		for i := len(events)-1; i >= 0; i-- { result = append(result, events[i]) }
		return result
	}
	value, ok := commandAuditBuffers.Load(s)
	if !ok { return []ai.CommandAuditEvent{} }
	buffer := value.(*commandAuditBuffer)
	buffer.mu.RLock(); defer buffer.mu.RUnlock()
	result := make([]ai.CommandAuditEvent, 0, len(buffer.events))
	for i := len(buffer.events)-1; i >= 0; i-- { result = append(result, buffer.events[i]) }
	return result
}

func redactAuditValue(value string) string {
	redacted := redactCommand(strings.TrimSpace(value))
	redacted = strings.Map(func(r rune) rune {
		switch r { case '\n', '\r', '\t': return ' '; default: if r < 0x20 || r == 0x7f { return -1 }; return r }
	}, redacted)
	if len(redacted) > maxCommandAuditText { redacted = redacted[:maxCommandAuditText] + "...[TRUNCATED]" }
	return redacted
}

func redactCommand(command string) string {
	redacted := strings.TrimSpace(command)
	for _, rule := range sensitiveCommandPatterns {
		redacted = rule.pattern.ReplaceAllString(redacted, rule.replacement)
	}
	return redacted
}
