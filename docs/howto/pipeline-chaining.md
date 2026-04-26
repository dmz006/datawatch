# How-to: Pipeline + session chaining

Chain a sequence of tasks into a DAG with optional before/after
test gates between steps. Useful when one autonomous task feeds the
next (e.g. "implement", "test", "doc-update", "release-notes")
without operator intervention between steps.

## Base requirements

- Daemon up; `pipeline.enabled: true` if you want to use the named
  pipeline store. Otherwise pipelines can be created ad-hoc via REST.
- A baseline understanding of [autonomous planning](autonomous-planning.md) —
  pipelines are the simpler / explicit "do A then B" alternative
  to LLM-driven decomposition. Use pipelines when you already know
  the step list; use autonomous when you want the LLM to figure it
  out.

## Setup

```bash
datawatch config set pipeline.enabled true
datawatch config set pipeline.gate_timeout_seconds 300
```

## Walkthrough

### 1. Define the pipeline

Pipelines have a `name`, an ordered list of `tasks`, and per-task
`before` / `after` shell gates. A non-zero exit from a gate halts
the pipeline.

```bash
datawatch pipeline create --name auth-rebuild \
  --task '{"name":"impl","spec":"implement RLS-based tenant isolation in internal/auth/middleware.go"}' \
  --task '{"name":"test","spec":"add coverage for the new middleware in middleware_test.go","after":"go test ./internal/auth/..."}' \
  --task '{"name":"doc","spec":"update docs/api/sessions.md with the new auth flow"}' \
  --task '{"name":"notes","spec":"add a CHANGELOG entry under [Unreleased]"}'
```

### 2. Run

```bash
datawatch pipeline run auth-rebuild
#  → {"id":"pipe_run_p1","status":"running",…}
```

### 3. Watch

```bash
datawatch pipeline status pipe_run_p1
#  → tasks:
#       impl   ✓ done   (session ses_a3f9, 14m elapsed)
#       test   ⏳ running (session ses_b1c2, gate `go test ./internal/auth/...` queued)
#       doc    ⏸ pending (depends on test)
#       notes  ⏸ pending
```

In the PWA: Settings → General → Pipelines → click the pipeline
to drill into the per-task session list.

### 4. Inspect a failed gate

If `go test` returns non-zero, the pipeline halts at `test` with
`status: gate_failed` and the gate's stdout/stderr captured. Fix
the underlying issue and re-run from the failed step:

```bash
datawatch pipeline resume pipe_run_p1 --from test
```

### 5. Combine with manual sessions

Pipelines and ad-hoc sessions share the same session store. You can
start a manual session in the middle of a paused pipeline (`new:
debug the failing test`), then resume the pipeline once the manual
session finishes.

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | create | `datawatch pipeline create --name … --task '{…}'` |
| CLI | run | `datawatch pipeline run <name>` |
| CLI | resume | `datawatch pipeline resume <run-id> --from <task>` |
| REST | create | `POST /api/pipelines {"name":…, "tasks":[…]}` |
| REST | run | `POST /api/pipelines/<name>/run` |
| MCP | create | tool `pipeline_create` |
| MCP | run | tool `pipeline_run` |
| Chat | (pipeline verbs not yet wired in chat — use the `rest` passthrough: `rest POST /api/pipelines/<name>/run`) |
| PWA | all | Settings → General → Pipelines |

## When to use what

| Scenario | Use |
|---|---|
| You know the step sequence ahead of time | **Pipeline** |
| You want the LLM to break down a free-form spec | [Autonomous planning](autonomous-planning.md) |
| You want guardrails (rules / security / docs / release-readiness) gating each step | [PRD-DAG orchestrator](prd-dag-orchestrator.md) |
| You want one-shot, no chaining | A single [session](../api/sessions.md) |

## See also

- [`docs/api/sessions.md`](../api/sessions.md) — sessions spawned by pipeline tasks
- [How-to: Autonomous planning](autonomous-planning.md) — LLM-decomposed alternative
- [How-to: PRD-DAG orchestrator](prd-dag-orchestrator.md) — guardrail-gated alternative
- [How-to: Cross-agent memory](cross-agent-memory.md) — share decisions between pipeline tasks
