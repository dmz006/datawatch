---
docs:
  index: true
  topics: [orchestrator, dag, automata, autonomous, guardrails]
exec_params:
  - {name: prd_ids, required: true, description: "Comma-separated Automaton IDs to compose"}
  - {name: name, required: true, description: "Graph name"}
exec_steps:
  - tool: orchestrator_graph_list
    description: List existing graphs
    args: {}
    read_only: true
  - tool: orchestrator_graph_create
    description: Create the new graph from the supplied Automaton IDs
    args:
      name: "{{params.name}}"
      prd_ids: "{{params.prd_ids}}"
    read_only: false
  - tool: orchestrator_graph_plan
    description: Render the planned topological order for review
    args:
      name: "{{params.name}}"
    read_only: true
  - tool: orchestrator_graph_run
    description: Run the graph (proceeds through gates as approved)
    args:
      name: "{{params.name}}"
    read_only: false
---
# How-to: Automaton-DAG orchestrator

Compose multiple Automata into a dependency graph with
guardrails. The orchestrator runs Automata in topological order, gates
each on its declared verifiers (rules / security / release-readiness
/ docs integrity), and lets you approve or block at the graph level
instead of one Automaton at a time.

## What it is

An Automaton-DAG is a YAML spec that names Automata (existing or to-be-created),
declares dependencies between them (`depends_on:`), and attaches
guardrails. The executor:

1. Picks the next Automaton with all dependencies satisfied + status
   `approved`.
2. Spawns its tasks (per [`autonomous-planning.md`](autonomous-planning.md)).
3. Runs declared guardrails on completion.
4. If guardrails pass → marks Automaton `completed`; checks for the next
   eligible Automaton.
5. If guardrails fail → marks Automaton `blocked`; pauses the graph for
   operator action.

States: `draft` → `planning` → `needs_review` → `approved` → `running`
→ `paused` / `completed` / `failed`.

## Base requirements

- `datawatch start` — daemon up.
- Autonomous subsystem enabled (`autonomous.enabled: true`).
- One or more Automata to compose (or define them inline in the graph
  spec).
- (Optional) configured guardrail backends: `rules` (in-process),
  `security` (Trivy / Snyk), `release-readiness` (custom),
  `docs-integrity` (in-process).

## Setup

```sh
# Confirm orchestrator subsystem.
datawatch config get orchestrator.enabled
#  → true

# Configure guardrail backends as needed.
datawatch config set orchestrator.guardrails.security.enabled true
datawatch config set orchestrator.guardrails.security.tool trivy
datawatch reload
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Author a graph spec.
cat > /tmp/auth-graph.yaml <<'EOF'
name: auth-refactor-graph
goal: Refactor auth from cookies to JWT in 3 phases
prds:
  - id: audit
    name: auth-audit
    title: Audit current auth flow
    backend: claude-code
  - id: implement
    name: jwt-implementation
    title: Implement JWT issuance + middleware
    backend: claude-code
    depends_on: [audit]
  - id: migrate
    name: migrate-cookies
    title: Migrate live sessions from cookies to JWT
    backend: claude-code
    depends_on: [implement]
guardrails:
  - rules
  - security
  - release-readiness
  - docs-integrity
EOF

# 2. Create the graph.
GRAPH_ID=$(datawatch orchestrator create --spec /tmp/auth-graph.yaml 2>&1 \
  | grep -oP 'id=\K[0-9a-f]+')

# 3. Inspect.
datawatch orchestrator get $GRAPH_ID
#  → name: auth-refactor-graph  status: draft
#    prds:
#      audit       (status: draft)
#      implement   (depends: [audit])  (status: draft)
#      migrate     (depends: [implement])  (status: draft)
#    guardrails: rules, security, release-readiness, docs-integrity

# 4. Decompose all child Automata (LLM-driven; async per Automaton).
datawatch orchestrator decompose $GRAPH_ID
sleep 90

# 5. Approve the graph (or per-Automaton via the autonomous CLI).
datawatch orchestrator approve $GRAPH_ID

# 6. Run.
datawatch orchestrator run $GRAPH_ID

# 7. Watch.
watch -n 5 "datawatch orchestrator get $GRAPH_ID | jq '{status, prds: .prds|map({id,status,n_completed_tasks})}'"

# 8. If an Automaton blocks on a guardrail:
datawatch orchestrator unblock $GRAPH_ID --prd implement \
  --reason "Security finding accepted: SAST false-positive in legacy file"
```

### 4b. Happy path — PWA

1. Settings → Automate → **Automata Orchestrator** card → click
   **+ New Graph**.
2. Editor opens with a starter YAML template; fill in name + Automata +
   `depends_on` + guardrails. **Save**.
3. The new graph appears in the Orchestrator card list with status
   `draft`. Click into it.
4. Detail view shows a graph visualization (nodes = Automata; edges =
   `depends_on`) and a control toolbar:
   - **Plan all** — kicks off LLM planning for every child
     Automaton in parallel.
   - **Review** — opens the per-Automaton review pane (same UI as
     [`autonomous-review-approve.md`](autonomous-review-approve.md)).
   - **Approve graph** — marks all Automata approved when you've reviewed.
   - **Run** — start execution in topological order.
5. While running, nodes color: gray (waiting), blue (running), green
   (completed), red (blocked). Click a blocked node for the guardrail
   verdict.
6. To unblock: per-node action menu → **Unblock with rationale** →
   modal asks for the rationale text. Recorded in the audit log.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Settings → Automate → Automata Orchestrator card. Graph view renders
as a vertical timeline on narrow viewports. Same actions.

### 5b. REST

```sh
# Create.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @/tmp/auth-graph.yaml \
  $BASE/api/orchestrator/graphs

# Get.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/orchestrator/graphs/$GRAPH_ID

# Decompose / Approve / Run / Cancel.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/orchestrator/graphs/$GRAPH_ID/decompose
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/orchestrator/graphs/$GRAPH_ID/approve
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/orchestrator/graphs/$GRAPH_ID/run
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/orchestrator/graphs/$GRAPH_ID/cancel

# Unblock an Automaton inside the graph.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prd":"implement","reason":"Accepted SAST FP"}' \
  $BASE/api/orchestrator/graphs/$GRAPH_ID/unblock
```

### 5c. MCP

Tools: `orchestrator_create`, `orchestrator_get`, `orchestrator_list`,
`orchestrator_decompose`, `orchestrator_approve`, `orchestrator_run`,
`orchestrator_cancel`, `orchestrator_unblock`.

Useful for a meta-coordinator LLM that composes large multi-Automaton work
streams; can monitor progress and react to blocks autonomously.

### 5d. Comm channel

```
You: orchestrator run abc123
Bot: graph abc123 running; first Automaton: audit
... (audit completes)
Bot: graph abc123 — audit completed; running implement
... (implement blocks on security guardrail)
Bot: graph abc123 — implement BLOCKED (security: 1 high finding in jwt.go:42)
You: orchestrator unblock abc123 prd=implement reason="Accepted; legacy code path"
Bot: graph abc123 unblocked; resuming
```

### 5e. YAML

Graph spec at `~/.datawatch/orchestrator/graphs/<id>.yaml`:

```yaml
id: <graph-id>
name: auth-refactor-graph
goal: ...
status: running
prds:
  - id: audit
    prd_id: <child-prd-uuid>
    status: completed
  - id: implement
    prd_id: <child-prd-uuid>
    depends_on: [audit]
    status: blocked
    block_reason: "security: 1 high finding in jwt.go:42"
guardrails:
  - rules
  - security
  - release-readiness
  - docs-integrity
unblocks:
  - prd: implement
    reason: "Accepted; legacy code path"
    actor: operator
    timestamp: 2026-05-06T15:30:00Z
```

## Diagram

```
   ┌──────┐         ┌──────────┐         ┌─────────┐
   │ audit│ ──────► │implement │ ──────► │ migrate │
   └──────┘         └──────────┘         └─────────┘
      │                  │                    │
      ▼                  ▼                    ▼
   guardrails        guardrails          guardrails
   (rules / sec /    (rules / sec /      (rules / sec /
    rel / docs)       rel / docs)         rel / docs)

   On any guardrail failure → Automaton: blocked → graph: paused
   Operator unblock with rationale → graph resumes
```

## Common pitfalls

- **Cyclic depends_on.** Daemon refuses with a clear error; remove
  the cycle.
- **Decompose timeout on a large graph.** Kicks off N decomposes in
  parallel; if all use the same backend you may rate-limit. Stagger
  with `orchestrator decompose --concurrency 2`.
- **Approve graph without reviewing children.** Same risk as
  per-Automaton. The graph view's per-node click opens the per-Automaton review
  modal — use it.
- **Unblock without rationale.** API requires a `reason`; the audit
  log entry is permanent.
- **Mid-run spec edits.** Editing a child Automaton spec while its graph
  is running is allowed but only the next-spawned task picks up the
  edit. In-flight tasks complete under the old spec.

## Linked references

- See also: [`autonomous-planning.md`](autonomous-planning.md) — single-Automaton lifecycle.
- See also: [`autonomous-review-approve.md`](autonomous-review-approve.md) — review gate.
- See also: [`evals.md`](evals.md) — per-story graded verification.
- Architecture: `../architecture-overview.md` § Orchestrator.

## Screenshots needed (operator weekend pass)

- [ ] Orchestrator card with multiple graphs
- [ ] Graph detail with visualization (3-node DAG)
- [ ] Blocked node with guardrail verdict popover
- [ ] Unblock modal with rationale input
- [ ] CLI `datawatch orchestrator get` output

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/autonomous-planning](autonomous-planning.md)
- [howto/autonomous-review-approve](autonomous-review-approve.md)
- [howto/pipeline-chaining](pipeline-chaining.md)
- [api/orchestrator](../api/orchestrator.md)
