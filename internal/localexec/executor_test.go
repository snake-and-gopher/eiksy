package localexec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testExecutor(t *testing.T) *Executor {
	t.Helper()
	dir := t.TempDir()
	return &Executor{
		workingDir:      dir,
		maxOutputBytes:  8,
		maxReadBytes:    8,
		maxWriteBytes:   8,
	}
}

func TestRunExecutesOnLocalHostAndBoundsOutput(t *testing.T) {
	executor := testExecutor(t)
	result, err := executor.Run(context.Background(), "echo eiksy-local")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "eiksy-") {
		t.Fatalf("stdout = %q, want local command output", result.Stdout)
	}
	if !result.Truncated {
		t.Fatal("expected output to be marked truncated")
	}
	if !strings.Contains(result.Stdout, "[output truncated]") {
		t.Fatalf("stdout = %q, want truncation marker", result.Stdout)
	}
}

func TestRunReturnsProcessExitCode(t *testing.T) {
	executor := testExecutor(t)
	command := "exit 7"
	if runtime.GOOS == "windows" {
		command = "exit /b 7"
	}
	result, err := executor.Run(context.Background(), command)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
}

func TestRunRejectsEmptyCommand(t *testing.T) {
	executor := testExecutor(t)
	if _, err := executor.Run(context.Background(), "  "); err == nil {
		t.Fatal("expected empty command to be rejected")
	}
}

func TestReadWriteFileUseLocalFilesystemPermissions(t *testing.T) {
	executor := testExecutor(t)
	if err := executor.WriteFile("note.txt", "hello"); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	content, err := executor.ReadFile("note.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if content != "hello" {
		t.Fatalf("ReadFile() = %q, want %q", content, "hello")
	}

	info, err := os.Stat(filepath.Join(executor.workingDir, "note.txt"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestReadFileBoundsContent(t *testing.T) {
	executor := testExecutor(t)
	if err := os.WriteFile(filepath.Join(executor.workingDir, "large.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := executor.ReadFile("large.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(content, "[content truncated]") {
		t.Fatalf("content = %q, want truncation marker", content)
	}
	if strings.HasPrefix(content, "0123456789") {
		t.Fatalf("content was not bounded: %q", content)
	}
}

func TestWriteFileRejectsOversizedContent(t *testing.T) {
	executor := testExecutor(t)
	if err := executor.WriteFile("large.txt", "0123456789"); err == nil {
		t.Fatal("expected oversized content to be rejected")
	}
}

func TestListDirectoryReturnsEntries(t *testing.T) {
	executor := testExecutor(t)
	if err := os.WriteFile(filepath.Join(executor.workingDir, "entry.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := executor.ListDirectory(".")
	if err != nil {
		t.Fatalf("ListDirectory() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "entry.txt" {
		t.Fatalf("entries = %#v, want entry.txt", entries)
	}
}
