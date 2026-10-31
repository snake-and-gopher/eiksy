package localexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultMaxOutputBytes = 16 << 10
	DefaultMaxReadBytes   = 64 << 10
	DefaultMaxWriteBytes  = 256 << 10
)

// Result is the bounded result of one local command execution.
type Result struct {
	Command    string
	Stdout     string
	Stderr     string
	ExitCode   int
	Duration   time.Duration
	Truncated  bool
}

// Executor runs commands and accesses files as the operating-system user
// running the CLI. It never elevates privileges or changes user identity.
type Executor struct {
	workingDir      string
	maxOutputBytes int
	maxReadBytes   int
	maxWriteBytes  int
}

func New() (*Executor, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get local working directory: %w", err)
	}
	return &Executor{
		workingDir:      workingDir,
		maxOutputBytes: DefaultMaxOutputBytes,
		maxReadBytes:   DefaultMaxReadBytes,
		maxWriteBytes:  DefaultMaxWriteBytes,
	}, nil
}

func (e *Executor) WorkingDirectory() string {
	if e == nil {
		return ""
	}
	return e.workingDir
}

// Run executes exactly one shell invocation on the local host. Policy and
// approval decisions must be made by the application service before calling
// this method.
func (e *Executor) Run(ctx context.Context, command string) (Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Result{ExitCode: -1}, fmt.Errorf("command cannot be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/D", "/S", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	cmd.Dir = e.workingDir

	stdout := newLimitedBuffer(e.maxOutputBytes)
	stderr := newLimitedBuffer(e.maxOutputBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	err := cmd.Run()
	result := Result{
		Command:   command,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  0,
		Duration:  time.Since(started),
		Truncated: stdout.Truncated() || stderr.Truncated(),
	}
	if err == nil {
		return result, nil
	}

	result.ExitCode = 1
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if ctx.Err() != nil {
		result.ExitCode = -1
		return result, ctx.Err()
	}
	return result, fmt.Errorf("execute local command: %w", err)
}

func (e *Executor) ReadFile(path string) (string, error) {
	path, err := e.resolvePath(path)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open local file: %w", err)
	}
	defer file.Close()

	content, readErr := io.ReadAll(io.LimitReader(file, int64(e.maxReadBytes+1)))
	if readErr != nil {
		return "", fmt.Errorf("read local file: %w", readErr)
	}
	if len(content) > e.maxReadBytes {
		return string(content[:e.maxReadBytes]) + "\n[content truncated]", nil
	}
	return string(content), nil
}

func (e *Executor) WriteFile(path, content string) error {
	if len([]byte(content)) > e.maxWriteBytes {
		return fmt.Errorf("file content exceeds %d byte limit", e.maxWriteBytes)
	}
	path, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write local file: %w", err)
	}
	return nil
}

func (e *Executor) ListDirectory(path string) ([]os.DirEntry, error) {
	path, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("list local directory: %w", err)
	}
	const maxEntries = 256
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	return entries, nil
}

func (e *Executor) resolvePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.workingDir, path)
	}
	return filepath.Clean(path), nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buffer.Write(value)
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	value := b.buffer.String()
	if b.truncated {
		return value + "\n[output truncated]"
	}
	return value
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}
