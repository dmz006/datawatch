# How-to Guides

Practical walkthroughs for the most common datawatch workflows. Each
guide follows the same structure:

1. **Base requirements** — what you need installed/configured first.
2. **Setup** — minimum steps to get the feature working.
3. **Walkthrough** — a real example from start to finish, with the
   commands you run and the output you should see.

If you're just trying to get datawatch running for the first time,
start at [`docs/setup.md`](../setup.md). The how-to guides assume
you already have a daemon you can talk to.

## Index

| Guide | What it covers |
|-------|----------------|
| [Autonomous planning](autonomous-planning.md) | Submit a free-form spec, watch it decompose into stories + tasks, run them with verification |
| [PRD-DAG orchestrator](prd-dag-orchestrator.md) | Compose multiple PRDs into a graph with guardrails (rules, security, release-readiness, docs integrity) |
| [Container workers](container-workers.md) | Configure project + cluster profiles, spawn ephemeral container workers, monitor + collect attestations |
| [Pipeline + session chaining](pipeline-chaining.md) | Chain tasks into DAG pipelines with before/after gates; combine with manual sessions |
| [Cross-agent memory](cross-agent-memory.md) | Use episodic memory + the knowledge graph across builds, tests, and federated peers |

> **Looking for something else?** [`docs/setup.md`](../setup.md) has
> first-time install. [`docs/api/`](../api/) has REST/MCP/CLI
> reference. [`docs/architecture-overview.md`](../architecture-overview.md)
> explains how the pieces fit together. [`docs/flow/`](../flow/) has
> sequence + flowchart diagrams.

## Conventions

- Every command shown is the operator-canonical form (full flags, no
  abbreviations).
- Output blocks show what you should see — `…` marks elided lines,
  `<vars>` mark substituted values.
- Where a feature is reachable through multiple channels (REST / MCP
  / CLI / chat), each guide picks one and notes the others at the
  bottom.
