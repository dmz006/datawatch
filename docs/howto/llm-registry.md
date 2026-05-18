---
docs:
  index: true
  topics: [llm, registry, compute, inference, v7]
exec_params:
  - name: name
    description: LLM name (e.g. my-ollama, claude-api, gpu-box)
    required: true
  - name: kind
    description: "LLM kind: ollama | openwebui | opencode | claude | claude-code | aider | goose | gemini | shell"
    required: true
  - name: model
    description: Model name (e.g. llama3.1:8b, claude-sonnet-4-6)
    required: false
    default: ""
exec_steps:
  - tool: llm_add
    description: Register the LLM in the registry
    args:
      name: "{{params.name}}"
      kind: "{{params.kind}}"
      model: "{{params.model}}"
    read_only: false
  - tool: llm_test
    description: Send a one-shot reachability probe through the new LLM
    args:
      name: "{{params.name}}"
    read_only: true
---
# How-to: LLM Registry (v7.0.0)

One registry. Every LLM — local inference engines, cloud APIs, and
coding-agent backends — defined by name, routed by the dispatcher.

## What it is

The LLM registry is the single source of truth for every language
model datawatch can reach. Each entry is a named definition:

```
name     — the identifier consumers use (sessions, Council, /api/ask, Automata)
kind     — the protocol adapter (ollama, openwebui, claude, claude-code, …)
models[] — per-node model assignments ({node, model} pairs)
compute_nodes — ordered failover list of Compute Node names
```

Before v7, every backend had its own config block (`cfg.ollama`,
`cfg.aider`, …) and separate mental model. In v7 they collapse here:
Settings → Compute → LLM Configuration.

> **Auto-registered LLMs**: The daemon automatically registers a `claude-code` entry (kind `claude-code`) at startup if the `claude` binary is on PATH. No manual `llm add` step is needed for Claude Code sessions. Other kinds (`ollama`, `claude`, `aider`, etc.) must be registered explicitly. Use `GET /api/llms` (plural) or `datawatch llm list` to see what is currently registered, including any auto-registered entries.

### LLM kinds

| Kind | Protocol | Compute nodes needed | Typical use |
|---|---|---|---|
| `ollama` | Ollama HTTP API | Yes | Local GPU / homelab inference |
| `openwebui` | OpenWebUI HTTP API | Yes | OpenWebUI front-end proxy |
| `opencode` | Ollama-compatible wrapper | Yes | opencode session backend |
| `claude` | Anthropic API (cloud) | No | Cloud inference, Council |
| `claude-code` | tmux session agent | No | AI coding assistant sessions |
| `opencode-acp` | tmux session agent | Optional | opencode ACP sessions |
| `opencode-prompt` | tmux session agent | Optional | opencode prompt sessions |
| `aider` | tmux session agent | Optional | aider coding assistant |
| `goose` | tmux session agent | Optional | goose coding assistant |
| `gemini` | tmux session agent | Optional | Gemini coding assistant |
| `shell` | tmux session / shell script | Optional | Custom shell automation |

**Inference kinds** (`ollama`, `openwebui`, `opencode`, `claude`) go
through the dispatcher for one-shot calls (Council debates, `/api/ask`,
agent spawning). **Session-backend kinds** (`claude-code`, `aider`,
`goose`, etc.) own a tmux session end-to-end; the registry holds their
binary path, model selection, and console preferences.

### EnabledModel — per-node model pairs

The `models[]` list on an LLM entry holds `{node, model}` pairs. Each
pair pins one model name to one Compute Node. The dispatcher consults
this list when routing a call: it picks the first reachable node that
has a matching pair. For SaaS kinds (`claude`, `gemini`) the `node`
field is empty — only `model` matters.

Example (multi-node Ollama):

```json
{
  "name": "my-ollama",
  "kind": "ollama",
  "compute_nodes": ["gpu-primary", "gpu-secondary"],
  "models": [
    {"node": "gpu-primary",   "model": "qwen3:32b"},
    {"node": "gpu-secondary", "model": "llama3.1:8b"}
  ]
}
```

If `gpu-primary` is unreachable, the dispatcher falls to
`gpu-secondary` and uses `llama3.1:8b` there.

### AutoAddModels

When `auto_add_models: true`, the daemon's model-refresh loop
automatically appends newly-discovered models from the LLM's Compute
Nodes to the `models[]` list. Useful for Ollama boxes where you
frequently `ollama pull` new models and want the registry to stay
current without manual edits.

### Back-compat: single Model field

Entries written by older versions (or via older REST clients) may have
a flat `model` string instead of `models[]`. The daemon expands it on
load: if `model` is non-empty and `models` is absent, it creates one
`{node, model}` pair per Compute Node (or one pair with no node for
SaaS kinds). REST `POST`/`PUT` handlers apply the same expansion.

## What migrated from v6

If you upgraded from v6, the daemon auto-created one registry entry per
populated backend block on first start. Auto-created entries are tagged
internally as `auto`. See [`v7-compute-migration.md`](v7-compute-migration.md)
for the full migration table and verification steps.

## Base requirements

- `datawatch start` — daemon up (v7.0.0).
- For `ollama` / `openwebui` / `opencode` kinds: at least one Compute
  Node registered. See [`compute-mode.md`](compute-mode.md).
- For `claude` kind: an Anthropic API key (literal or `${secret:name}`
  reference — see [`secrets-manager.md`](secrets-manager.md)).
- For session-backend kinds (`claude-code`, `aider`, …): the relevant
  binary on PATH on the Compute Node host.

## Setup

### Add a local inference LLM (Ollama)

```sh
# Register the Compute Node first (if not already present).
datawatch compute add gpu-box --url http://192.168.1.50:11434

# Add the LLM, pointing at that node.
datawatch llm add my-ollama \
  --kind ollama \
  --compute-nodes gpu-box \
  --model llama3.1:8b

# Verify reachability.
datawatch llm test my-ollama
```

### Add a cloud inference LLM (Anthropic)

```sh
# Store the API key in secrets (recommended over inline literal).
datawatch secrets set ANTHROPIC_KEY "sk-ant-..."

# Register the LLM — no compute-nodes for SaaS kinds.
datawatch llm add claude-api \
  --kind claude \
  --model claude-sonnet-4-6 \
  --api-key-ref '${secret:ANTHROPIC_KEY}'

datawatch llm test claude-api
```

### Add a coding-agent LLM (claude-code)

```sh
# claude-code has no compute nodes — it resolves against the local binary.
datawatch llm add claude-code \
  --kind claude-code

# Optionally specify a model list (presented to users in the session wizard).
datawatch llm models add claude-code --model claude-opus-4-5
datawatch llm models add claude-code --model claude-sonnet-4-6
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. List what's registered.
datawatch llm list
#  → name            kind         enabled  models
#    claude-api      claude       yes      claude-sonnet-4-6
#    my-ollama       ollama       yes      llama3.1:8b  (node: gpu-box)
#    claude-code     claude-code  yes      —

# 2. Fetch one entry.
datawatch llm get my-ollama

# 3. Add a second model on the same node.
datawatch llm models add my-ollama \
  --node gpu-box \
  --model qwen3:32b

# 4. Enable auto-model discovery.
datawatch llm update my-ollama \
  --kind ollama \
  --compute-nodes gpu-box \
  --auto-add-models

# 5. Refresh the model list now (instead of waiting for the next poll).
datawatch llm refresh-models my-ollama

# 6. See what's currently bound to this LLM.
datawatch llm in-use my-ollama
#  → sessions: 2, automata: 0, personas: 0

# 7. Disable an LLM while the GPU box is offline.
datawatch llm disable my-ollama

# 8. Re-enable — optionally run a probe first.
datawatch llm enable my-ollama --pretest

# 9. Delete (blocked if active sessions are using it).
datawatch llm delete my-ollama
#  → 409 Conflict: blocked_by: [{kind:session, id:ralfthewise-7a3c, ...}]

# 10. Safe path: reassign bindings first, then delete.
datawatch llm reassign my-ollama --to-llm claude-api
datawatch llm delete my-ollama   # now succeeds

# 11. Force path: cancel active work and delete in one shot (destructive).
datawatch llm force-delete my-ollama
```

### 4b. Happy path — PWA

1. Settings → Compute → **LLM Configuration** card.

![LLM Configuration card — entries with kind badges, enabled toggles, action buttons](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/settings-llm.png)

2. Click **+ Add LLM**. Fill in:
   - **Name** — unique identifier (`my-ollama`, `claude-api`, …)
   - **Kind** — dropdown of all supported kinds
   - **Compute Nodes** — ordered failover list (local kinds only; hidden for SaaS kinds)
   - **Model** — starting model (you can add more per-node later)
   - **API Key** — literal or `${secret:name}` (cloud kinds only)
   - **Timeout** — override the adapter default (local: 300 s, cloud: 60 s; 0 = use default)
   - **Tags** — optional operator labels

![Add LLM modal — Name, Kind, ComputeNodes, Enabled Models, API key, Test and Save buttons](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/settings-llm-add.png)

3. Click **Save**. The entry appears in the list with an enabled toggle.

4. Click the toggle to enable or disable. When enabling, a one-shot
   reachability probe runs first; the toggle only flips if the probe
   succeeds. Probe result appears in the alert dock (bell icon).

![LLM list — aider disabled (grey toggle) alongside enabled (green) entries](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/settings-llm-toggle.png)

5. Click the pencil icon to open the Edit LLM form. The **Enabled Models**
   section shows `{node, model}` pairs with × to remove and **+ Add model**
   to pin a new pair. Click **Save** to apply changes.

![Edit LLM form — Enabled Models section showing node/model pairs, + Add model, Auto-enable toggle](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/settings-llm-detail.png)

6. **Delete** is blocked when active bindings exist. The modal shows active
   sessions/automata and a "Reassign to" dropdown. Pick the target LLM
   and click **Reassign + Delete**, or expand **Force delete** to terminate
   active work immediately.

![Delete blocked — active binding listed, Reassign to dropdown, Reassign + Delete button](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/settings-llm-delete.png)

## Other channels

### 5a. Mobile (Compose Multiplatform)

Settings → Compute → LLM Configuration. Same CRUD as PWA. Enable
toggle with pretest. In-use list. Reassign before delete. Force-delete
with confirmation dialog.

### 5b. REST

```sh
BASE=https://localhost:8443
TOKEN=<your-auth-token>

# List all LLMs.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/llms | jq .

# Fetch one.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/llms/my-ollama | jq .

# Create.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-ollama",
    "kind": "ollama",
    "compute_nodes": ["gpu-box"],
    "models": [{"node": "gpu-box", "model": "llama3.1:8b"}]
  }' $BASE/api/llms

# Update (full replace).
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-ollama","kind":"ollama","compute_nodes":["gpu-box"],
       "models":[{"node":"gpu-box","model":"qwen3:32b"}]}' \
  $BASE/api/llms/my-ollama

# Enable / disable.
curl -sk -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true,"pretest":true}' \
  $BASE/api/llms/my-ollama/enabled

# Run a one-shot test call.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"Reply with OK to confirm reachability."}' \
  $BASE/api/llms/my-ollama/test | jq .

# Models — list, add, remove.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/llms/my-ollama/models
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node":"gpu-box","model":"qwen3:32b"}' \
  $BASE/api/llms/my-ollama/models
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node":"gpu-box","model":"qwen3:32b"}' \
  $BASE/api/llms/my-ollama/models

# Trigger model-list refresh.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/llms/my-ollama/refresh_models

# In-use — see what's bound.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/llms/my-ollama/in_use?page=1&size=10&filter=running" | jq .

# Reassign all bindings to another LLM.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"to_llm":"claude-api"}' \
  $BASE/api/llms/my-ollama/reassign

# Delete (409 if active bindings exist).
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/llms/my-ollama

# Force delete (terminates active work).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"confirm":"yes I understand this terminates active work"}' \
  $BASE/api/llms/my-ollama/force_delete
```

### 5c. MCP

Tools available in every MCP host (Claude Code, Cursor, etc.):

| Tool | What it does |
|---|---|
| `llm_list` | List all LLMs |
| `llm_get` | Fetch one LLM by name |
| `llm_add` | Register a new LLM |
| `llm_update` | Replace an existing LLM |
| `llm_delete` | Remove an LLM (blocked if active bindings) |
| `llm_test` | Send a one-shot reachability probe |
| `llm_enable` | Enable an LLM (optional pretest) |
| `llm_disable` | Disable an LLM |
| `llm_in_use` | List sessions / Automata / personas bound to this LLM |
| `llm_add_model` | Add an enabled model (node + model pair) |
| `llm_remove_model` | Remove an enabled model |
| `llm_list_models` | List enabled models for an LLM |
| `llm_refresh_models` | Trigger model-list refresh from Compute Nodes |

Examples:

```
llm_list
llm_add  name=gpu-ollama kind=ollama compute_nodes=gpu-box model=llama3.1:8b
llm_test name=gpu-ollama
llm_enable name=gpu-ollama pretest=true
llm_in_use name=gpu-ollama filter=running page=1 size=10
llm_add_model llm=gpu-ollama node=gpu-box model=qwen3:32b
llm_remove_model llm=gpu-ollama node=gpu-box model=llama3.1:8b
llm_refresh_models name=gpu-ollama
```

### 5d. Comm channel

| Verb | Example | Effect |
|---|---|---|
| `llm list` | `llm list` | Returns all LLM names + kinds |
| `llm get <name>` | `llm get my-ollama` | Returns one entry |
| `llm add <name> <kind>` | `llm add gpu-box ollama` | Creates minimal entry |
| `llm test <name>` | `llm test claude-api` | Runs reachability probe, returns result |
| `llm enable <name>` | `llm enable my-ollama` | Enables (with pretest) |
| `llm disable <name>` | `llm disable my-ollama` | Disables |
| `llm delete <name>` | `llm delete my-ollama` | Deletes (blocked if active) |
| `llm in-use <name>` | `llm in-use my-ollama` | Returns binding summary |

### 5e. YAML

LLM entries live in `~/.datawatch/inference/llms.json` (managed by the
daemon — do not hand-edit while the daemon is running). To pre-seed
entries before first start or in a deployment, write the file before
launching:

```json
[
  {
    "name": "my-ollama",
    "kind": "ollama",
    "compute_nodes": ["gpu-box"],
    "models": [
      {"node": "gpu-box", "model": "llama3.1:8b"}
    ],
    "auto_add_models": true
  },
  {
    "name": "claude-api",
    "kind": "claude",
    "models": [{"model": "claude-sonnet-4-6"}],
    "api_key_ref": "${secret:ANTHROPIC_KEY}",
    "cost_per_1k_input": 0.003,
    "cost_per_1k_output": 0.015
  }
]
```

`${secret:name}` references in `api_key_ref` are resolved at call time
via the secrets store (never written to disk in resolved form).

## Block-on-delete and the reassign flow

Deleting an LLM that has active sessions, running Automata, or bound
personas returns `409 Conflict` with a `blocked_by` list:

```json
{
  "blocked_by": [
    {"kind":"session","id":"ralfthewise-7a3c","name":"my-session","state":"running"},
    {"kind":"automata","id":"prd-001","title":"Deploy feature X","state":"planning"}
  ]
}
```

Two resolution paths:

**Reassign (recommended)** — migrates all bindings to a different LLM.
Running sessions pick up the change on their next LLM call; sessions in
`waiting_input` or `planning` state switch immediately.

```sh
datawatch llm reassign my-ollama --to-llm claude-api
# optionally pin to a specific model in the target LLM:
datawatch llm reassign my-ollama --to-llm claude-api --to-model claude-haiku-3
```

**Force delete** — cancels all active work and deletes in one shot.
Requires an explicit confirmation string to prevent accidents:

```sh
datawatch llm force-delete my-ollama
# The CLI sends: {"confirm":"yes I understand this terminates active work"}
```

## Diagram

```
  Consumer (Council / /api/ask / Session start / Automata)
                      │
                      │ resolve by LLM name
                      ▼
          ┌──────────────────────┐
          │    LLM Registry      │
          │  name / kind / models│
          │  compute_nodes[]     │
          └──────────┬───────────┘
                     │ first reachable node (left-to-right failover)
                     ▼
          ┌──────────────────────┐
          │   Dispatcher         │
          │   adapter per kind   │
          └──────────┬───────────┘
                     │
          ┌──────────┴───────────┐
          │                      │
          ▼                      ▼
  Compute Node A          Compute Node B
  (ollama/openwebui/…)    (fallback)
```

For session-backend kinds (`claude-code`, `aider`, …) the dispatcher
does not make a network call — it resolves the binary path and model
selection stored in the LLM entry, then the session manager spawns the
tmux process.

## Common pitfalls

- **LLM disabled.** Trying to start a session with a disabled LLM
  returns `"llm <name> is disabled (toggle on in Settings → Compute →
  LLMs)"`. Enable it with `datawatch llm enable <name>`.
- **No Compute Nodes for a local kind.** Adding an `ollama` LLM with
  no `compute_nodes` means the dispatcher has nowhere to route. Add at
  least one Compute Node to `compute_nodes`.
- **Model not in `models[]`.** If the dispatcher finds the node but
  not the model, the call fails. Either add the pair with
  `llm models add` or enable `auto_add_models` and run
  `llm refresh-models`.
- **`api_key_ref` with wrong secret name.** The LLM will test as
  reachable if the network is OK, but calls fail at runtime when the
  secret can't resolve. Verify with `datawatch secrets get <name>`.
- **Delete blocked by stale session.** A session stuck in `running`
  after its tmux pane died will still show as a binding. Kill the
  stale session first (`datawatch sessions kill <id>`), then delete
  the LLM.
- **Auto-migration name collision.** If you manually added an entry
  named `ollama-default` before upgrading, the v6→v7 migration skips
  it. Check that your manual entry has the correct `kind` and
  `compute_nodes`. See [`v7-compute-migration.md`](v7-compute-migration.md).

## Screenshots needed (operator weekend pass)

- [ ] LLM Configuration card — full list with enabled toggles and kind badges
- [ ] Add LLM modal — kind dropdown open showing all 11 kinds
- [ ] LLM detail drawer — Models tab with {node, model} pairs and Refresh button
- [ ] LLM detail drawer — In-use tab with paginated binding list
- [ ] Delete blocked — inline reassign prompt with target LLM dropdown
- [ ] Enable toggle flip — probe spinner then green enabled state
- [ ] CLI `datawatch llm list` output

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/v7-compute-migration](v7-compute-migration.md)
- [howto/compute-mode](compute-mode.md)
- [howto/secrets-manager](secrets-manager.md)
- [howto/council-mode](council-mode.md)
- [howto/chat-and-llm-quickstart](chat-and-llm-quickstart.md)
- [api/llms](../api/llms.md)
