# How-to: Autonomous planning

Describe a feature in plain English; datawatch decomposes it into a
small graph of stories and tasks, runs each task as a real worker
session, and has an independent verifier attest each result before
the next step starts.

## Base requirements

- A running daemon you can reach (`datawatch ping` returns `pong`).
- An LLM backend configured (`session.llm_backend` in your config or
  `autonomous.decomposition_backend` if you want a different one for
  decomposition).
- `autonomous.enabled: true` in your config (or set via the
  Settings UI / `PUT /api/config`).

## Setup

```bash
# 1. Confirm the daemon sees your LLM backend.
datawatch backends list
#  → claude   enabled
#    ollama   enabled
#  …

# 2. Turn the autonomous loop on (default off).
datawatch config set autonomous.enabled true
datawatch config set autonomous.decomposition_backend claude
datawatch config set autonomous.verification_backend  claude
datawatch config set autonomous.max_parallel_tasks    3

# 3. (Optional) Pick how aggressive the auto-fix retry should be.
datawatch config set autonomous.auto_fix_retries 2
```

You can do all of the above from Settings → General → Autonomous in
the web UI instead — same config keys.

## Walkthrough

### 1. Submit a spec

```bash
datawatch autonomous prd create \
  --title "RTK token-savings widget" \
  --spec  "Add a RTK Token Savings card to the Settings → Monitor tab. \
  It should show: total tokens saved, average savings %, command count. \
  Pull from /api/rtk/savings (already exists). Mobile parity not required."
#  → {"id":"prd_a3f9","status":"draft", …}
```

Same call from the chat channels:

```
new prd: title="RTK token-savings widget" spec="Add a RTK Token Savings card …"
```

### 2. Decompose

```bash
datawatch autonomous prd decompose prd_a3f9
#  → calling decomposition LLM (claude, effort=normal) …
#  → {"id":"prd_a3f9","status":"decomposed", "stories":[
#      {"id":"st_01","title":"Add /api/rtk/savings client wiring", "tasks":[…]},
#      {"id":"st_02","title":"Render the card in app.js", "tasks":[…]}
#    ]}
```

Inspect what the LLM produced before running anything:

```bash
datawatch autonomous prd get prd_a3f9 | jq '.stories[].title'
#  "Add /api/rtk/savings client wiring"
#  "Render the card in app.js"
```

### 3. Run

```bash
datawatch autonomous prd run prd_a3f9
#  → run started (fire-and-forget); poll status with `prd get`
```

The runner topo-sorts the tasks, spawns one ephemeral worker per
task (you'll see new sessions appear in the PWA), waits for each
to finish, runs verification (the verifier is its own LLM session
with a focused prompt), and either marks the task `done` or feeds
the verifier's findings back into a retry, up to
`autonomous.auto_fix_retries`.

### 4. Watch progress

```bash
watch -n 2 'datawatch autonomous prd get prd_a3f9 | jq ".status, .stories[].tasks[] | {id,status}"'
```

Or open the PWA → Settings → Autonomous: each PRD shows a small
progress bar + per-task status.

### 5. Inspect verifier verdicts

```bash
datawatch autonomous prd get prd_a3f9 | jq '.stories[].tasks[].verdicts'
```

Each entry has `outcome` (pass / warn / block), `severity`, the
verifier's `summary`, and a list of `issues`. A `block` halts the
PRD and waits for operator input — datawatch will not auto-retry
past the configured limit, and it will not silently override a
block.

## Where the data lives

- PRD records: `<data_dir>/autonomous/prds.json`
- Per-task session output: regular session log under
  `<data_dir>/sessions/<session-id>/`
- Verdict log: append-only, accessible via
  `GET /api/autonomous/prds/<id>` (`stories[].tasks[].verdicts`).

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | create | `datawatch autonomous prd create --title … --spec …` |
| CLI | run | `datawatch autonomous prd run <id>` |
| REST | create | `POST /api/autonomous/prds {"title":…, "spec":…}` |
| REST | run | `POST /api/autonomous/prds/<id>/run` |
| MCP | create | tool `autonomous_prd_create` |
| MCP | run | tool `autonomous_prd_run` |
| Chat | create | `new prd: title=… spec=…` |
| PWA | all | Settings → Autonomous → "New PRD" |

## See also

- [`docs/api/autonomous.md`](../api/autonomous.md) — full REST + MCP reference
- [`docs/flow/orchestrator-flow.md`](../flow/orchestrator-flow.md) — when you want to compose multiple PRDs into a graph with guardrails (PRD-DAG)
- [How-to: PRD-DAG orchestrator](prd-dag-orchestrator.md) — compose multiple PRDs
- [How-to: Cross-agent memory](cross-agent-memory.md) — share context between PRD workers
