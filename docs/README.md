# Eiksy Documentation

Eiksy documentation is organized around the permanent product and implementation model.

## Product

- [product-contract.md](product-contract.md) — product-level Definition of Done and the **Think → Connect → Operate → Think** workflow.

## Architecture

- [architecture.md](architecture.md) — stable application boundaries, responsibilities and extension rules.
- [adr/0001-cli-tui-architecture.md](adr/0001-cli-tui-architecture.md) — original CLI/TUI frontend boundary.
- [adr/0003-cli-local-agent.md](adr/0003-cli-local-agent.md) — standalone CLI as a local-host agent.
- [cli.md](cli.md) — CLI startup, secure storage, provider setup, local tools and approvals.
- [ai.md](ai.md) — AI architecture, context boundaries, native tools, approval and execution flow.
- [agent-skills.md](agent-skills.md) — global and project-level Agent Skills, discovery format and security boundaries.
- [release-acceptance.md](release-acceptance.md) — final release acceptance gate for the product contract.
- [release-readiness.md](release-readiness.md) — engineering checklist for the exact release candidate.

## Product acceptance

The primary product acceptance gate is the end-to-end workflow:

**Think → Connect → Operate → Think**

The acceptance test must demonstrate that:

1. a user task can be turned into an AI operation proposal;
2. the required resource is explicitly connected;
3. the AI receives bounded infrastructure context without secrets;
4. a security-sensitive operation pauses for explicit user approval;
5. the approved operation executes through the existing backend executor;
6. the result is bounded and returned to the AI workflow;
7. the executed operation is represented in the audit trail;
8. the result can drive the next reasoning step.

The permanent architecture and product contract are the source of truth. Implementation plans and pull-request sequences are not product documentation.

## Security

Security-sensitive behavior is part of the product contract and architecture. Vulnerability reports should follow the repository [SECURITY.md](../SECURITY.md) process rather than public issues.

## Documentation rule

New documentation should describe stable product behavior, contracts and boundaries. Temporary implementation plans, milestone descriptions and superseded architecture variants should not be added to the permanent documentation set.
