# ADR 0002: Separate AI chat from the session agent

- Status: Proposed
- Date: 2026-10-09

## Context

Eiksy currently exposes AI chat and command-capable agent behavior through overlapping application and UI paths. This couples a general conversation to Eiksy-specific operational context and makes it unclear which UI owns session-bound execution.

The product needs two distinct experiences: a general-purpose model chat in the sidebar, and an operational agent in the workspace, presented as a tab alongside SFTP.

## Decision

### Sidebar chat

The sidebar is a normal chat client for the configured model provider.

- It sends user messages and renders model responses and conversation history.
- It may use provider configuration and ordinary chat settings.
- It must not receive an SSH session ID, active terminal contents, command-execution tools, agent state, or Eiksy operational context.
- It must not advertise, invoke, or know about the workspace agent.
- The chat request contract must not expose Eiksy-specific tool schemas or command-execution callbacks.

### Workspace agent

The agent is a first-class workspace tab, parallel to SFTP.

- The application binds the agent tab to one explicit runtime SSH session ID when the tab is created/opened.
- The agent receives that session ID from application state; the model must not choose or supply it.
- Agent commands execute only through existing application services, command policy, approval gates, bounded result handling, and audit.
- The agent has its own task/transcript state and lifecycle; it does not reuse or mutate sidebar chat history.
- If the bound session closes, the agent must report the unavailable session and must not silently switch to another session.

### Shared provider layer

Share only provider transport/configuration and model-response primitives where practical. Keep chat and agent orchestration separate. Tool calling is enabled only in the agent orchestration path, not in the generic chat path.

### UI and persistence

- Sidebar chat remains available independently of the selected workspace tab.
- Agent appears in the workspace tab strip beside SFTP and is bound to a session.
- Persisted chat history and agent task history must be distinguishable and must not be cross-loaded.
- The CLI/TUI should preserve the same separation where its interface supports both workflows.

## Security invariants

1. No session ID is injected into sidebar chat requests or prompts.
2. No command tools are registered for the generic chat request.
3. Agent session binding is established by trusted application code, never by model output.
4. Every command still passes through the existing policy, approval, result-bounding, and audit boundaries.
5. Secrets and credential material remain unavailable to both conversation histories.

## Implementation sequence

1. Split the provider-facing plain-chat path from the agent/tool loop at the application-service boundary.
2. Add a workspace agent-tab model and bind it to the selected runtime session ID.
3. Move agent progress, approval, and result UI into that tab; keep the sidebar chat UI plain.
4. Add regression tests proving chat requests have no session/agent/tools context and agent requests always use their bound session.
5. Update architecture and user documentation after the code path is migrated.

## Acceptance criteria

- A normal sidebar chat message can be sent without selecting or opening an SSH session.
- The chat path cannot execute commands, invoke agent tools, or read active session state.
- Opening an agent tab for session A binds it to A; changing the selected terminal to session B does not retarget the agent.
- Agent command approval, policy enforcement, and audit remain enforced.
- Closing the bound session leaves the agent safely unavailable rather than redirecting it.
- Automated tests cover the isolation and session-binding invariants.
