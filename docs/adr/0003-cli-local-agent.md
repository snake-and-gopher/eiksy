# ADR 0003: Make the standalone CLI a local-host agent

- Status: Accepted
- Date: 2026-10-31

## Context

The standalone `eiksy-cli` currently starts the full multi-view TUI and reuses the SSH-oriented application runtime. The product requirement is different: the CLI is a focused agent for the host on which the process is running. It must not require an SSH profile, create an SSH connection, or simulate a remote session.

The existing agent execution path is coupled to SSH session IDs in its tool schema, command-policy evaluation, pending-approval persistence, execution dispatch, and audit records. Replacing only the command executor would leave misleading session semantics and could bypass or weaken security checks.

## Decision

### CLI experience

- Running `eiksy-cli` without arguments starts directly in a single agent conversation.
- The transcript contains user prompts, assistant responses, requested operations, command lines, stdout, stderr, exit status, and approval outcomes.
- The CLI does not expose Sessions, SSH Terminal, SFTP/Files, remote host-key management, or remote connection settings.
- AI provider configuration, secure token storage, master-password unlock, and agent approval controls remain available through a focused CLI interaction.
- Help and version flags remain non-interactive.

### Local execution

- The agent executes commands on the local host where `eiksy-cli` is running.
- Commands run as the operating-system user that launched the CLI. Eiksy must not elevate privileges or implicitly invoke `sudo`, `runas`, or an equivalent privilege escalation.
- Local file operations use the same process identity and are subject to operating-system access controls.
- The model does not choose a host or session ID. The local execution target is established by trusted CLI/application code.
- The implementation must use a dedicated local execution boundary. It must not represent the local host as an SSH session or route local work through SSH/SFTP adapters.

### Policy, approvals, and audit

- Local operations use an explicit local-agent tool set and a policy scope distinct from SSH session policy.
- Potentially destructive or sensitive operations require approval according to the command policy. Denied operations must not execute.
- Approval requests must persist enough trusted context to resume the exact pending local tool call. The approval flow must validate that context before execution.
- Every operation produces a bounded, redacted transcript/audit result including command, status, duration, exit code, stdout, and stderr.
- Existing safe defaults and deny rules must not become more permissive as a side effect of introducing local execution.
- The local tool set must not expose SSH or SFTP tools.

### Separation from desktop application

- The desktop application's SSH/SFTP workflows and session-bound agent behavior remain unchanged.
- Shared provider configuration, secure storage, policy primitives, and transcript/event contracts may be reused where their semantics are genuinely shared.
- CLI-specific wiring must select the local runtime explicitly; it must not change the desktop application's default runtime.

## Implementation sequence

1. Introduce a dedicated local execution interface and local-agent tool registry without changing the SSH agent path.
2. Add local command/file operations with bounded results, policy evaluation, approval persistence/resumption, and audit coverage.
3. Add a local-agent application runtime that reuses provider configuration and secure storage but does not construct SSH/SFTP managers.
4. Replace the standalone CLI's full TUI entry point with a focused agent-only terminal experience and retain provider setup, master-password unlock, and approval handling.
5. Update CLI documentation and add regression tests for local execution, no-SSH startup, policy enforcement, approval continuation, and desktop isolation.

## Acceptance criteria

- Starting `eiksy-cli` without arguments opens the local agent directly and does not initialize SSH/SFTP managers.
- No SSH profile, remote session, host key, or SFTP operation is required or exposed by the CLI.
- Local commands and file operations run only with the launching user's operating-system permissions.
- The agent can show the exact operations it requested and their bounded results in the terminal transcript.
- Approval is required and enforced for operations classified as sensitive/destructive; denial guarantees no execution.
- Pending approvals can be resumed safely after approval without retargeting to a remote or different execution context.
- Provider credentials remain in secure storage and are never printed in the transcript or logs.
- The desktop GUI's SSH agent, SSH terminal, and SFTP behavior is covered by regression tests and remains unchanged.
