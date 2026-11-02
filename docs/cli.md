# Eiksy CLI

The standalone eiksy-cli is a local-host AI agent. It starts directly in one terminal conversation and operates on the computer where the process is running.

The CLI uses the same provider configuration, encrypted credential storage, command-policy primitives, local executor and approval boundary as the Eiksy application. It has a dedicated local runtime and does not initialize SSH or SFTP managers.

## Start

Run the standalone CLI without arguments:

    eiksy-cli

Running the desktop eiksy binary without arguments continues to start the desktop application.

## First start and secure storage

On first start, the CLI asks you to create and confirm a master password. On later starts, it asks for the password when secure storage is locked.

The master password unlocks the OS-keychain-backed key used to protect saved provider credentials. Password input is masked. If secure storage becomes locked while a prompt or approved operation is in progress, the CLI asks you to unlock it and automatically retries that same request after a successful unlock. It does not require you to retype the prompt or repeat the approval choice.

If the operating-system keychain is unavailable, the CLI does not bypass secure storage or continue with unprotected credentials.

## Configure an AI provider

Use **/provider** in the conversation to open provider configuration.

### OpenAI-compatible provider

Configure:
- model name;
- API endpoint (the base URL, for example https://api.example.com/v1);
- optional API token.

Use **Ctrl+L** to list models exposed by the endpoint and **Ctrl+S** to save. The token is stored in encrypted secure storage and is never displayed in the transcript.

### Local provider

Choose the local Qwen provider to configure its model download URL. The local provider screen supports:
- **Ctrl+S** — save the model URL;
- **Ctrl+D** — download the model;
- **Ctrl+R** — start or stop the local model server.

The local model workflow requires the corresponding local model runtime to be installed.

## Conversation commands

| Input | Action |
| --- | --- |
| /provider | Configure or switch AI provider |
| /help | Show available conversation commands |
| /quit or /exit | Exit the CLI |
| Enter | Send the current prompt |
| Left/Right, Home/End | Move the input cursor |
| Backspace/Delete | Edit around the cursor |
| Ctrl+J | Insert a newline in a multi-line prompt |
| PgUp/PgDown | Scroll the conversation |
| Alt+Home/Alt+End | Jump to oldest/latest transcript content |
| Ctrl+C | Exit the CLI |

The transcript presents user prompts, assistant responses, local tool calls, operation details, bounded results and approval outcomes in chronological order.

## Local operations and approvals

The local agent has a dedicated tool set:

- **local.exec** — run a local command;
- **local.file.read** — read a bounded local file;
- **local.file.write** — write a local file;
- **local.file.list** — list a local directory.

All operations run as the operating-system user who launched the CLI and are constrained by that user's permissions. The CLI never opens SSH connections, creates remote sessions, uses SFTP, or treats the local host as an SSH session.

The local execution boundary also enforces no-elevation restrictions: on Linux, the local-agent process sets `PR_SET_NO_NEW_PRIVS`, which is inherited by its child processes and prevents setuid/setgid binaries and file capabilities from granting additional privileges. On Windows, the CLI refuses to start if its parent process is already elevated (for example, launched with **Run as administrator**), because file tools execute in the CLI process itself. Each command also runs with a restricted token that disables the built-in Administrators group and removes enabled privileges. Direct privilege-launcher commands such as `sudo`, `doas`, `runas` and `pkexec` are denied by policy. Platforms without a supported restriction fail closed instead of silently running unrestricted commands.

Command Policy evaluates local operations separately from SSH policy. When approval is required, the requested operation is shown in the conversation. File writes include the target path, byte count, SHA-256 fingerprint and a bounded content preview so you can inspect what will be written before approving:

| Key | Action |
| --- | --- |
| Y | Allow this operation once |
| A | Always allow this exact operation |
| N | Deny the operation |

A denied operation is not executed. The approval continuation remains bound to the original local tool call and cannot be redirected to an SSH session.

Command output is bounded. File reads and writes have explicit size limits. Local operations are represented in the command audit trail.

## Help and version

    eiksy-cli --help
    eiksy-cli --version

The -h, -v, help and version aliases are also supported. Help and version do not initialize the local runtime or access secure storage.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Help/version completed successfully |
| 1 | CLI initialization or terminal UI failed |
| 2 | Unknown command-line argument |

## Desktop separation

The standalone CLI is local-only. The desktop application retains its existing SSH profiles, remote terminal, SFTP browser and session-bound agent workflows. The CLI uses a dedicated service constructor with no SSH/SFTP managers.

See [ADR 0003](adr/0003-cli-local-agent.md) for the local-agent decision and [AI architecture](ai.md) for the shared security model.
