# How-to: Pipeline + session chaining

Chain a sequence of tasks into a DAG with optional before/after gates;
each step spawns a session, waits for completion, propagates context
into the next. Pipelines are lighter-weight than full Automatons —
they don't go through the PRD review gate, they just run the steps.

## What it is

A pipeline is a YAML spec listing steps (each = a session-spawn) +
edges (`after:` declares ordering). Optionally per-step:

- `requires_approval: true` — pauses the pipeline before this step;
  operator must approve to continue.
- `retry: { max_attempts, backoff }` — auto-retry on failure.
- `eval_suite: <name>` — grade the step's output; gate the next step
  on pass.
- `inherit_context: true` — pass the previous step's last response
  into this step's prompt.

## Base requirements

- `datawatch start` — daemon up.
- A configured LLM backend.
- (Optional) Evals suites if you want gated grading per step.

## Setup

No setup beyond a backend.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Author a pipeline spec.
cat > /tmp/release-pipeline.yaml <<'EOF'
name: release-prep
goal: Prepare a release candidate end-to-end
steps:
  - id: changelog
    task: "Write the CHANGELOG entry for the changes since vX.Y.Z-1"
    backend: claude-code

  - id: tests
    task: "Run the full test suite; fix any failures."
    backend: claude-code
    after: [changelog]
    eval_suite: tests-green
    retry: { max_attempts: 2, backoff: 30s }

  - id: review
    task: "Review the diff for security issues."
    backend: claude-code
    after: [tests]
    requires_approval: true

  - id: tag
    task: "Tag + push the release."
    backend: claude-code
    after: [review]
    inherit_context: true
EOF

# 2. Create.
PID=$(datawatch pipelines create --spec /tmp/release-pipeline.yaml 2>&1 \
  | grep -oP 'id=\K[0-9a-f]+')

# 3. Run.
datawatch pipelines run $PID

# 4. Watch.
watch -n 2 "datawatch pipelines status $PID"
#  → status: running
#    steps:
#      changelog  completed  (2m 14s)
#      tests      running    (started 30s ago)
#      review     pending
#      tag        pending

# 5. Approve a paused step.
datawatch pipelines approve $PID --step review

# 6. Inspect a specific step's session.
datawatch pipelines step-session $PID --step tests
#  → ralfthewise-abcd
datawatch sessions tail ralfthewise-abcd | head -50

# 7. Cancel.
datawatch pipelines cancel $PID
```

### 4b. Happy path — PWA

1. Settings → Automate → **Pipelines** card → **+ New Pipeline**.
2. Editor opens with starter YAML; fill in steps + `after` edges.
   **Save**.
3. New pipeline appears in the card list. Click into it.
4. Detail view shows a DAG visualization (steps as nodes, after-edges
   as arrows) + a status bar across the top.
5. **Run** in the toolbar. Nodes color: gray (pending) → blue
   (running) → green (completed) → red (failed) / yellow (waiting
   approval).
6. For paused-on-approval steps, click the yellow node → modal with
   step output preview + **Approve / Reject** buttons.
7. To inspect a step's session, click the node → modal exposes "Open
   session" linking into the session detail view.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Settings → Automate → Pipelines card. DAG visualization renders
as a vertical timeline on narrow viewports.

### 5b. REST

```sh
# Create.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @/tmp/release-pipeline.yaml \
  $BASE/api/pipelines

# Run.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/pipelines/$PID/run

# Status.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/pipelines/$PID/status

# Approve a paused step.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"step":"review"}' \
  $BASE/api/pipelines/$PID/approve

# Cancel.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/pipelines/$PID/cancel
```

### 5c. MCP

Tools: `pipeline_create`, `pipeline_run`, `pipeline_status`,
`pipeline_approve`, `pipeline_cancel`.

Useful for an LLM coordinator that wants to express a release flow as
explicit steps, gate them, and re-run on failure without operator
intervention.

### 5d. Comm channel

```
You: pipeline run release-prep
Bot: pipeline release-prep started; first step: changelog
... (later)
Bot: pipeline release-prep — review step PAUSED (requires_approval)
You: pipeline approve release-prep step=review
Bot: pipeline release-prep — review approved; running tag
```

### 5e. YAML

Pipeline spec at `~/.datawatch/pipelines/<id>.yaml`:

```yaml
id: <pipeline-id>
name: release-prep
goal: ...
status: running
steps:
  - id: changelog
    task: "..."
    backend: claude-code
    status: completed
    started_at: ...
    completed_at: ...
    session_id: ralfthewise-...
  - id: tests
    task: "..."
    after: [changelog]
    eval_suite: tests-green
    retry: { max_attempts: 2, backoff: 30s }
    status: running
    attempt: 1
  ...
```

Operators can edit step-level `task:` text between runs to refine.
`status:` is daemon-managed — don't hand-edit.

## Diagram

```
   ┌───────────┐         ┌───────┐         ┌────────┐         ┌─────┐
   │ changelog │ ──────► │ tests │ ──────► │ review │ ──────► │ tag │
   └───────────┘         └───────┘         └────────┘         └─────┘
                            │                  │
                         eval_suite       requires_approval
                            │                  │
                            ▼                  ▼
                       gate next       pause until operator
```

## Common pitfalls

- **Cyclic `after:` edges.** Daemon refuses with a clear error.
- **Step inherits no context but expected to.** Set `inherit_context:
  true` on the step that needs the previous step's output.
- **Eval suite name typo.** Step fails before running because the
  suite isn't found. Verify with `datawatch evals list`.
- **Retry exhausted.** Pipeline fails on the failing step's last
  attempt. Inspect the step's session to debug; re-run the pipeline
  or extract just that step.
- **Approval blocks forgotten.** Pipeline sits at the yellow node.
  PWA's pipeline list shows a paused indicator; chat-channel pipelines
  send a `requires approval` notification when configured.

## Linked references

- See also: [`autonomous-planning.md`](autonomous-planning.md) — heavier-weight PRDs.
- See also: [`evals.md`](evals.md) — per-step grading.
- See also: [`sessions-deep-dive.md`](sessions-deep-dive.md) — each step is a session.
- Architecture: `../architecture-overview.md` § Pipelines.

## Screenshots needed (operator weekend pass)

- [ ] Pipelines card with multiple pipelines
- [ ] Pipeline detail DAG visualization
- [ ] Paused-on-approval node with approve modal
- [ ] Step-failure view with retry attempts
- [ ] CLI `datawatch pipelines status` output
