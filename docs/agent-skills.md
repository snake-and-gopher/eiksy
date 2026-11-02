# Agent Skills

Eiksy supports two sources of agent instructions:

- **Global skills** are created and managed in the Agent workspace and apply across agent sessions.
- **Project skills** are discovered automatically by the local-host agent from the current repository.

## Project skill layout

Create one directory per skill under `.agents/skills` at the repository root:

```text
.agents/
  skills/
    go-review/
      SKILL.md
    release-notes/
      SKILL.md
```

Each `SKILL.md` starts with YAML frontmatter containing a `name` and a short `description`. The Markdown body contains the instructions:

```markdown
---
name: go-review
description: Review Go code changes and tests
---

# Go review

- Check error handling and resource cleanup.
- Prefer focused table-driven tests.
- Run the relevant Go tests before proposing a change.
```

The skill directory name must match the frontmatter `name`. Names use lowercase letters and digits separated by single hyphens. The local agent searches from its working directory up to the nearest Git repository root, so the skills are available when it starts in a nested project directory. In a non-Git directory, only the exact working directory is searched.

## Limits and validation

- At most 20 project skills are loaded.
- Each `SKILL.md` is limited to 12 KiB.
- Combined project skill files are limited to 48 KiB.
- A skill must have valid frontmatter, a non-empty description and a non-empty Markdown body.
- Symlinked skill directories and files are ignored.
- Invalid or oversized skills are skipped without preventing the agent from starting.

## Scope

Project skill discovery currently applies to the **local-host agent**. Global skills continue to apply to both local and SSH agent prompts. Project skills do not currently add tools, change the working directory, or modify the active Command Policy.

Project instructions are repository-provided content. The agent must not treat them as authority to reveal secrets, escalate privileges, invent tools, or bypass Eiksy security controls.
