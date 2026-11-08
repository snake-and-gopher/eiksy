package app

import (
	"fmt"
	"time"
)

const maxRuntimeDiagnosticEntries = 2000

// RuntimeDiagnosticEntry exists only in memory for the lifetime of this process.
type RuntimeDiagnosticEntry struct {
	Time string `json:"time"`
	Level string `json:"level"`
	Message string `json:"message"`
}

func (s *Service) SetRuntimeDiagnosticsEnabled(enabled bool) bool {
	s.diagnosticsMu.Lock()
	s.diagnosticsEnabled = enabled
	s.diagnosticsMu.Unlock()
	return enabled
}

func (s *Service) RuntimeDiagnosticsEnabled() bool {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	return s.diagnosticsEnabled
}

func (s *Service) GetRuntimeDiagnostics() []RuntimeDiagnosticEntry {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	entries := make([]RuntimeDiagnosticEntry, len(s.diagnosticsEntries))
	copy(entries, s.diagnosticsEntries)
	return entries
}

// AddRuntimeDiagnostic accepts diagnostic events observed by the frontend.
func (s *Service) AddRuntimeDiagnostic(level, message, timestamp string) {
	s.appendRuntimeDiagnostic(level, message, timestamp)
}

func (s *Service) appendRuntimeDiagnostic(level, message, timestamp string) {
	if s == nil {
		return
	}
	s.diagnosticsMu.Lock()
	if !s.diagnosticsEnabled {
		s.diagnosticsMu.Unlock()
		return
	}
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	entry := RuntimeDiagnosticEntry{Time: timestamp, Level: level, Message: message}
	s.diagnosticsEntries = append(s.diagnosticsEntries, entry)
	if len(s.diagnosticsEntries) > maxRuntimeDiagnosticEntries {
		s.diagnosticsEntries = append([]RuntimeDiagnosticEntry(nil), s.diagnosticsEntries[len(s.diagnosticsEntries)-maxRuntimeDiagnosticEntries:]...)
	}
	emit := s.emitFn
	s.diagnosticsMu.Unlock()
	if emit != nil {
		emit("diagnostics:log", entry)
	}
}

func (s *Service) emitDiagnosticLogf(level, format string, args ...any) {
	if !s.RuntimeDiagnosticsEnabled() {
		return
	}
	s.emitDiagnosticLog(level, fmt.Sprintf(format, args...))
}

func (s *Service) emitDiagnosticLog(level, message string) {
	s.appendRuntimeDiagnostic(level, message, "")
}
