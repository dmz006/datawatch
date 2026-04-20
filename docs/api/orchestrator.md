# PRD-DAG orchestrator (v4.0.0 — BL117)

**Shipped in v4.0.0.** Composes BL24 autonomous PRDs into a graph and
runs each PRD under a **guardrail sub-agent overlay** — every PRD
node is attested by one or more independent validators (rules,
security, release-readiness, docs-diagrams-architecture) before the
DAG advances.

Disabled by default — opt in with `orchestrator.enabled: true`.

Design doc: [`../plans/2026-04-20-bl117-prd-dag-orchestrator.md`](../plans/2026-04-20-bl117-prd-dag-orchestrator.md).

---

## Surfaces (full parity)

| Channel | Entry-point |
|---|---|
| YAML  | `orchestrator:` block |
| REST  | `/api/orchestrator/*` |
| MCP   | `orchestrator_config_get/set`, `orchestrator_graph_*`, `orchestrator_verdicts` |
| CLI   | `datawatch orchestrator <subcmd>` |
| Comm  | via the comm `rest` passthrough |

---

## Configuration

```yaml
orchestrator:
  enabled:                false
  default_guardrails: ["rules", "security", "release-readiness", "docs-diagrams-architecture"]
  guardrail_timeout_ms:   120000      # 2 min per guardrail
  guardrail_backend:      ""          # empty = inherit session.llm_backend
  max_parallel_prds:      2           # per-graph PRD parallelism
```

---

## REST endpoints

```
GET    /api/orchestrator/config
PUT    /api/orchestrator/config

POST   /api/orchestrator/graphs                 body: {title, project_dir, prd_ids, [deps]}
GET    /api/orchestrator/graphs                 list
GET    /api/orchestrator/graphs/{id}            one graph with node tree + verdicts
DELETE /api/orchestrator/graphs/{id}            cancel
POST   /api/orchestrator/graphs/{id}/plan       rebuild node tree
POST   /api/orchestrator/graphs/{id}/run        fire-and-forget runner

GET    /api/orchestrator/verdicts               flatten guardrail verdicts across graphs
```

When `orchestrator.enabled` is false, every endpoint returns `503`.

---

## Graph model

Each graph is a DAG of **Nodes**. Two node kinds:

| Kind        | Contents | Wrapped work |
|-------------|----------|--------------|
| `prd`       | `prd_id` | BL24 autonomous PRD execution (via `/api/autonomous/prds/{id}/run` loopback) |
| `guardrail` | `prd_id, guardrail` | One attestation per (PRD × guardrail) |

`Plan` generates one PRD node per PRD ID, then one guardrail node per
(PRD × configured guardrail). Operator-supplied `deps` (PRD → [PRDs])
become node-level dependencies so downstream PRDs wait for upstream
PRDs and their guardrails.

---

## Verdicts

Each guardrail returns a Verdict:

```json
{"outcome": "pass | warn | block",
 "severity": "info | low | medium | high | critical",
 "summary": "…", "issues": ["…"]}
```

`block` halts the DAG — the graph status becomes `blocked` and
operator intervention is required. `warn` records the verdict without
halting. `pass` clears the node.

---

## CLI

```
datawatch orchestrator config-get
datawatch orchestrator config-set '{"enabled":true}'

datawatch orchestrator graph-list
datawatch orchestrator graph-create "ship v4.0" '["prd1","prd2"]' '{"prd2":["prd1"]}'
datawatch orchestrator graph-get <id>
datawatch orchestrator graph-plan <id> '{"prd2":["prd1"]}'
datawatch orchestrator graph-run <id>
datawatch orchestrator graph-cancel <id>
datawatch orchestrator verdicts
```

---

## MCP tools (AI-ready)

Typical AI workflow:
1. `autonomous_prd_create` + `autonomous_prd_decompose` for each PRD.
2. `orchestrator_graph_create(title, prd_ids, deps)`.
3. `orchestrator_graph_run(id)`.
4. Poll `orchestrator_graph_get(id)` until `status` is `completed` or `blocked`.
5. On `blocked`, inspect the `Verdict` of the offending node, fix, re-run.

---

## Guardrails — v1 stub

v4.0.0 ships with a **stub GuardrailFn** that returns `pass` with an
informational summary per guardrail name. This lets the graph
structure + DAG traversal + verdict persistence ship end-to-end while
the concrete BL103-validator-image wiring for each guardrail type
(rules checklist evaluation, security diff review, release-readiness
gate, docs integrity checker) lands as v4.0.x patches.

To author a real guardrail today, register a **plugin** (BL33) on the
`on_guardrail` hook — the orchestrator will prefer plugin verdicts
over the stub when the hook is declared. Plugin manifest:

```yaml
name: my-rules-check
entry: ./check
hooks: [on_guardrail]
```

and the plugin receives the same `GuardrailRequest` JSON as the built-in stub.

---

## Storage

Graphs persist as JSON-lines under `<data_dir>/orchestrator/graphs.jsonl`.
Full rewrite on every update; small-dataset assumption.

---

## Non-goals

- Not a replacement for BL24 — composes its PRDs.
- Not a replacement for F10 — every node ultimately spawns an F10
  worker.
- Not a replacement for `pipeline.Executor` — that runs the *task*
  DAG inside a single PRD; the orchestrator runs the *PRD* DAG above it.
