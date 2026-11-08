package app

import (
	"fmt"
	"testing"
)

func TestRuntimeDiagnosticsCollectionIsOptIn(t *testing.T) {
	emitted := 0
	service := &Service{emitFn: func(eventName string, data ...interface{}) {
		if eventName == "diagnostics:log" {
			emitted++
		}
	}}

	service.emitDiagnosticLog("INFO", "ignored while disabled")
	if got := len(service.GetRuntimeDiagnostics()); got != 0 {
		t.Fatalf("expected no entries while disabled, got %d", got)
	}
	if emitted != 0 {
		t.Fatalf("expected no emitted events while disabled, got %d", emitted)
	}

	service.SetRuntimeDiagnosticsEnabled(true)
	service.emitDiagnosticLog("INFO", "captured")
	if got := len(service.GetRuntimeDiagnostics()); got != 1 {
		t.Fatalf("expected one captured entry, got %d", got)
	}
	if emitted != 1 {
		t.Fatalf("expected one emitted event, got %d", emitted)
	}
}

func TestDisablingRuntimeDiagnosticsStopsCollectionAndKeepsCapturedEntries(t *testing.T) {
	service := &Service{emitFn: func(string, ...interface{}) {}}
	service.SetRuntimeDiagnosticsEnabled(true)
	service.emitDiagnosticLog("INFO", "before disable")
	service.SetRuntimeDiagnosticsEnabled(false)
	service.emitDiagnosticLog("INFO", "after disable")

	entries := service.GetRuntimeDiagnostics()
	if len(entries) != 1 || entries[0].Message != "before disable" {
		t.Fatalf("unexpected retained entries after disabling: %#v", entries)
	}
}

func TestRuntimeDiagnosticsBufferIsBounded(t *testing.T) {
	service := &Service{emitFn: func(string, ...interface{}) {}}
	service.SetRuntimeDiagnosticsEnabled(true)
	for i := 0; i < maxRuntimeDiagnosticEntries+25; i++ {
		service.emitDiagnosticLog("DEBUG", fmt.Sprintf("event-%d", i))
	}

	entries := service.GetRuntimeDiagnostics()
	if len(entries) != maxRuntimeDiagnosticEntries {
		t.Fatalf("expected %d entries, got %d", maxRuntimeDiagnosticEntries, len(entries))
	}
	if entries[0].Message != "event-25" || entries[len(entries)-1].Message != fmt.Sprintf("event-%d", maxRuntimeDiagnosticEntries+24) {
		t.Fatalf("unexpected buffer bounds: first=%q last=%q", entries[0].Message, entries[len(entries)-1].Message)
	}
}
