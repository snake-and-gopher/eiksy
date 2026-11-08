package app

import "time"

// emitDiagnosticLog publishes process-lifetime diagnostics to the GUI.
// The frontend keeps these entries in memory only; they are never persisted.
func (s *Service) emitDiagnosticLog(level, message string) {
	if s == nil || s.emitFn == nil {
		return
	}
	s.emitFn("diagnostics:log", map[string]string{
		"level": level,
		"message": message,
		"time": time.Now().UTC().Format(time.RFC3339Nano),
	})
}
