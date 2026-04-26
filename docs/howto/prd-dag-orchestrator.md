# How-to: PRD-DAG orchestrator

Compose multiple autonomous PRDs into a dependency graph with
guardrails (rules, security, release-readiness, docs integrity). The
orchestrator runs each PRD node, evaluates its guardrails, and only
advances dependents when none returned `block`.

## Base requirements

- Daemon up, autonomous loop on (see
  [autonomous-planning](autonomous-planning.md) for the prerequisite
  setup).
- One or more existing PRDs. The orchestrator can both run *new*
  PRDs by ID and re-run failed ones after operator intervention.
- A guardrail strategy. Defaults are sane (v1 stub returns
  `pass` + a summary line); subprocess plugin or per-name validator
  (`/api/ask`) backings are configurable.

## Setup

```bash
datawatch config set orchestrator.enabled true
datawatch config set orchestrator.guardrail_backend stub        # or "plugin" / "validator"
datawatch config set orchestrator.block_on_warn  false          # warn ≠ block by default
```

Same fields are reachable from Settings → General → Orchestrator
in the web UI.

## Walkthrough

### 1. Create the graph

Three PRDs that depend on each other linearly: design → migration → tests.

```bash
datawatch orchestrator graph create \
  --title "Auth-service v2" \
  --prd-id prd_design_a3f9 \
  --prd-id prd_migration_b1c2 \
  --prd-id prd_tests_d4e5 \
  --deps prd_migration_b1c2:prd_design_a3f9 \
  --deps prd_tests_d4e5:prd_migration_b1c2
#  → {"id":"graph_g1","status":"draft", "nodes":[…]}
```

### 2. Plan it

`plan` materializes guardrail nodes alongside each PRD node.

```bash
datawatch orchestrator graph plan graph_g1
#  → {"status":"planned",
#     "nodes":[
#       {"id":"prd:prd_design_a3f9", "kind":"prd"},
#       {"id":"guard:prd_design_a3f9:rules", "kind":"guardrail"},
#       {"id":"guard:prd_design_a3f9:security", "kind":"guardrail"},
#       …
#     ]}
```

### 3. Run

Fire-and-forget; the runner returns immediately and works through
ready nodes in topo order.

```bash
datawatch orchestrator graph run graph_g1
#  → {"status":"running", "started_at":"…"}
```

### 4. Watch

```bash
watch -n 2 'datawatch orchestrator graph get graph_g1 | jq ".status, .nodes[] | {id,status,verdict_outcome:.verdict.outcome}"'
```

The PWA Settings → Orchestrator card shows the same picture with a
per-node progress strip + verdict badges.

### 5. Inspect verdicts

```bash
datawatch orchestrator verdicts --graph graph_g1
```

Each verdict carries `outcome` (pass / warn / block), `severity`,
the verifier's `summary`, and an `issues[]` list. A `block` halts
the graph and waits for operator intervention. Re-run after fixing:

```bash
datawatch orchestrator graph run graph_g1   # idempotent — picks up where it stopped
```

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | create | `datawatch orchestrator graph create --title … --prd-id …` |
| CLI | run | `datawatch orchestrator graph run <id>` |
| REST | create | `POST /api/orchestrator/graphs {"title":…, "prd_ids":[…], "deps":{…}}` |
| REST | run | `POST /api/orchestrator/graphs/<id>/run` |
| MCP | create | tool `orchestrator_graph_create` |
| MCP | run | tool `orchestrator_graph_run` |
| PWA | view | Settings → Orchestrator → Graph list + per-graph drill-down |
| Chat | (no direct verb yet — use the `rest` passthrough: `rest POST /api/orchestrator/graphs ...`) |

## See also

- [`docs/api/orchestrator.md`](../api/orchestrator.md) — full REST + MCP reference
- [`docs/flow/orchestrator-flow.md`](../flow/orchestrator-flow.md) — Mermaid sequence
- [`docs/plans/2026-04-20-bl117-prd-dag-orchestrator.md`](../plans/2026-04-20-bl117-prd-dag-orchestrator.md) — design rationale
- [How-to: Autonomous planning](autonomous-planning.md) — single-PRD prerequisite
