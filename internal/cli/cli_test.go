package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunWithoutArgumentsStartsInteractiveCLI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := Run(nil, "v1.2.3", &stdout, &stderr)
	if handled {
		t.Fatal("expected no-argument invocation to start the interactive CLI")
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := Run([]string{"--help"}, "v1.2.3", &stdout, &stderr)
	if !handled || exitCode != 0 {
		t.Fatalf("expected handled success, got handled=%v exitCode=%d", handled, exitCode)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage in help output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := Run([]string{"--version"}, "v1.2.3", &stdout, &stderr)
	if !handled || exitCode != 0 {
		t.Fatalf("expected handled success, got handled=%v exitCode=%d", handled, exitCode)
	}
	if stdout.String() != "v1.2.3\n" {
		t.Fatalf("expected version output, got %q", stdout.String())
	}
}

func TestRunUnknownArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := Run([]string{"--unknown"}, "v1.2.3", &stdout, &stderr)
	if !handled || exitCode != 2 {
		t.Fatalf("expected handled error, got handled=%v exitCode=%d", handled, exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command or argument") {
		t.Fatalf("expected unknown-argument error, got %q", stderr.String())
	}
}
