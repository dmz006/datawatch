---
docs:
  index: true
  topics: [compute, llm, v7, nodes, ollama, openai-compat, gpu, scheduling]
exec_params:
  - name: name
    description: ComputeNode name (kebab-case, e.g. gpu-box-1)
    required: true
  - name: kind
    description: API protocol the node speaks — ollama or openai-compat
    required: true
  - name: address
    description: URL of the LLM endpoint (e.g. http://gpu-box:11434)
    required: true
exec_steps:
  - tool: compute_node_add
    description: Register the new ComputeNode
    args:
      name: "{{params.name}}"
      kind: "{{params.kind}}"
      address: "{{params.address}}"
    read_only: false
  - tool: compute_node_health
    description: Verify the node is reachable and not in maintenance
    args:
      name: "{{params.name}}"
    read_only: true
---
# How-to: Compute Nodes

A Compute Node is the registry entry that tells datawatch where an LLM endpoint lives — a local machine, a remote GPU box, a cluster, or any host running an Ollama or OpenAI-compatible server. Every LLM in the LLM registry references one or more Compute Nodes for failover. Sessions, Council debates, `/api/ask`, and Automata all route through this layer.

This guide covers the full lifecycle: what a Compute Node is, how to add one through every available channel, how to monitor it, and how to troubleshoot problems.

## What it is

A Compute Node has:

- **Identity** — a unique kebab-case name, a protocol kind, and the URL of the endpoint.
- **Declared capacity** — GPU count, GPU memory, system RAM, and max concurrent model slots. Operator-stated; the monitoring sidecar fills in live data on top.
- **Hardware spec** — OS, CPU architecture, GPU vendor, GPU platform, and GPU model. Used by the scheduler and the Compute filter UI.
- **Observer peer** — an optional binding to a `datawatch-stats` monitoring peer that pushes live metrics for this node.
- **Scheduling priority** — a 0–100 hint that tells the dispatcher which node to prefer when multiple nodes are eligible.
- **Permissions** — which consumers (Council, sessions, Automata, `/api/ask`) may place workloads here.
- **Maintenance windows** — operator-declared blackout periods when the scheduler skips this node.

### Supported kinds

| Kind | Protocol | Typical use |
|------|----------|-------------|
| `ollama` | Ollama HTTP API on port 11434 | Local or remote Ollama instances |
| `openai-compat` | OpenAI-compatible `/v1/chat/completions` | OpenWebUI, vLLM, LMStudio, llama.cpp server, hosted APIs |

Earlier kind values (`local`, `ssh`, `docker`, `k8s`, `remote`, `remote-proxy`) are accepted for backwards compatibility but are deprecated. If your node was registered with one of these, the Settings > Compute UI shows a migration banner asking you to re-select a supported kind.

### Relationship to LLMs and sessions

```
  ┌─────────────────────────────────────┐
  │ Consumer                            │
  │ (Session / Council / Ask / Automata)│
  └──────────────┬──────────────────────┘
                 │ resolves LLM by name
                 ▼
  ┌─────────────────────────────────────┐
  │ LLM registry entry                  │
  │  kind, model, compute_nodes: [...]  │
  └──────────────┬──────────────────────┘
                 │ ordered failover list
                 ▼
  ┌─────────────────────────────────────┐
  │ Compute Node (this registry)        │
  │  name, kind, address, capacity, …   │
  └──────────────┬──────────────────────┘
                 │ HTTP / protocol call
                 ▼
  ┌─────────────────────────────────────┐
  │ Actual LLM endpoint                 │
  │  (Ollama, vLLM, OpenWebUI, …)       │
  └─────────────────────────────────────┘
```

---

## Base requirements

- `datawatch` daemon running (`datawatch start`).
- The LLM endpoint reachable from the daemon host (test with `curl`).
- For live monitoring: `datawatch-stats` sidecar installed on the target host (optional but recommended).

---

## nodes.json structure

Compute Nodes are persisted to `~/.datawatch/compute/nodes.json`. You can inspect it directly, but always use the API, CLI, PWA, MCP, or comm channel to make changes so validation runs and the daemon stays in sync.

A minimal entry looks like:

```json
{
  "name": "gpu-box-1",
  "kind": "ollama",
  "address": "http://gpu-box-1:11434",
  "scheduling_priority": 50,
  "declared_capacity": {},
  "permissions": {},
  "hardware": {}
}
```

A fully-populated entry:

```json
{
  "name": "gpu-box-1",
  "kind": "ollama",
  "address": "http://gpu-box-1:11434",
  "monitoring_endpoint": "https://gpu-box-1:9001/api/stats",
  "observer_peer": "gpu-box-1",
  "scheduling_priority": 80,
  "declared_capacity": {
    "gpus": 2,
    "gpu_mem_gb": 48,
    "ram_gb": 128,
    "max_concurrent_models": 4,
    "gpu_vendor": "nvidia",
    "gpu_model": "RTX 4090"
  },
  "hardware": {
    "os": "linux",
    "arch": "x86_64",
    "gpu_vendor": "nvidia",
    "gpu_platform": "bare-metal",
    "gpu_model": "rtx-4090",
    "gpu_count": 2,
    "memory_gb": 128,
    "cpu_cores": 32
  },
  "tags": ["production", "high-mem"],
  "permissions": {
    "allowed_consumers": ["council", "ask", "session_spawn"],
    "denied_consumers": []
  },
  "cost_per_hour": 0.50,
  "created_at": "2026-05-10T20:00:00Z",
  "updated_at": "2026-05-10T20:00:00Z"
}
```

### Field reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | required | Unique kebab-case identifier (`[a-z0-9._-]+`) |
| `kind` | string | required | `ollama` or `openai-compat` |
| `address` | string | — | URL of the LLM endpoint |
| `monitoring_endpoint` | string | — | Sidecar pull URL (e.g. `https://host:9001/api/stats`) |
| `observer_peer` | string | — | Registered `datawatch-stats` peer name for live metrics |
| `scheduling_priority` | int 0–100 | 50 | Higher = preferred when multiple nodes are eligible |
| `declared_capacity.gpus` | int | 0 | Number of GPUs |
| `declared_capacity.gpu_mem_gb` | int | 0 | GPU memory in GB |
| `declared_capacity.ram_gb` | int | 0 | System RAM in GB |
| `declared_capacity.max_concurrent_models` | int | 0 | Max simultaneous model slots |
| `declared_capacity.gpu_vendor` | string | — | `nvidia`, `amd`, `intel` |
| `declared_capacity.gpu_model` | string | — | e.g. `RTX 4090` |
| `hardware.os` | string | — | `linux`, `macos`, `windows` |
| `hardware.arch` | string | — | `x86_64`, `arm64`, `aarch64` |
| `hardware.gpu_vendor` | string | — | `nvidia`, `amd`, `intel`, `apple`, `none`, `other` |
| `hardware.gpu_platform` | string | — | `bare-metal`, `desktop`, `jetson-thor`, `grace-hopper`, `cloud-h100`, … |
| `hardware.gpu_model` | string | — | e.g. `blackwell`, `h100`, `rtx-4090`, `m3-max` |
| `hardware.gpu_count` | int | 0 | Number of GPUs |
| `hardware.memory_gb` | int | 0 | Total system RAM |
| `hardware.cpu_cores` | int | 0 | Logical CPU cores |
| `tags` | []string | — | Operator-applied labels (visible in UI) |
| `permissions.allowed_consumers` | []string | all | Which consumers may route here (`council`, `ask`, `agent_spawn`, `session_spawn`, `*`) |
| `permissions.denied_consumers` | []string | none | Explicitly blocked consumers (overrides allowed) |
| `cost_per_hour` | float | 0 | USD/hour cost for scheduler accounting |
| `disabled` | bool | false | When true, dispatcher skips this node entirely |
| `maintenance_windows` | []object | — | Operator-declared blackout periods |

---

## Happy path 1 — CLI

```sh
# 1. List existing Compute Nodes.
datawatch compute node list
#  → {"nodes": [...]}

# 2. Add a new Ollama node.
datawatch compute node add gpu-box-1 \
  --kind ollama \
  --address http://gpu-box-1:11434 \
  --gpus 2 \
  --gpu-mem-gb 48 \
  --ram-gb 128 \
  --max-models 4 \
  --gpu-vendor nvidia \
  --gpu-model "RTX 4090" \
  --priority 80 \
  --tags production,high-mem
#  → {"name": "gpu-box-1", "ok": true}

# 3. Add an OpenAI-compatible node (e.g. OpenWebUI or vLLM).
datawatch compute node add openwebui-1 \
  --kind openai-compat \
  --address http://openwebui-host:3000 \
  --priority 60
#  → {"name": "openwebui-1", "ok": true}

# 4. Fetch one node.
datawatch compute node get gpu-box-1

# 5. Check health (declared capacity + maintenance state).
datawatch compute node health gpu-box-1
#  → {"name":"gpu-box-1","in_maintenance":false,"declared_capacity":{...},...}

# 6. Pull live detail from the monitoring sidecar (requires observer_peer
#    bound or monitoring_endpoint set).
datawatch compute node detail gpu-box-1

# 7. Update an existing node (replaces all fields — include all you want).
datawatch compute node update gpu-box-1 \
  --kind ollama \
  --address http://gpu-box-1:11434 \
  --priority 90 \
  --max-models 6

# 8. Delete a node.
datawatch compute node delete old-node

# 9. Attach a datawatch-stats observer peer for live metrics.
#    First, list peers that have no bound node yet:
datawatch compute node observer-free
#  → {"peers": ["gpu-box-1"]}
datawatch compute node attach-observer gpu-box-1 gpu-box-1
#  → {"name":"gpu-box-1","observer_peer":"gpu-box-1","ok":true}

# 10. Detach an observer peer.
datawatch compute node detach-observer gpu-box-1

# 11. See all local observer peers grouped by their bound node.
datawatch compute node observer-by-node

# 12. Pull a specific Ollama model onto a node (runs in background).
datawatch compute node pull-model gpu-box-1 llama3.1:8b
#  → {"task_id":"abc123", "status":"queued"}

# 13. Poll the pull task status.
datawatch marketplace task abc123

# 14. Remove an Ollama model from a node.
datawatch compute node remove-model gpu-box-1 llama3.1:8b

# 15. Browse the Ollama model catalog.
datawatch marketplace catalog
```

---

## Happy path 2 — PWA

<!-- screenshot: Settings > Compute tab with Compute Nodes card expanded -->

1. Open the datawatch PWA and navigate to **Settings** (gear icon) → **Compute** tab.
2. The **Compute Nodes** card lists every registered node with its kind badge and an enabled/disabled toggle.
3. If any node uses a deprecated kind value, a migration banner appears at the top of the card. Click it to open the kind migration dialog and re-select a supported kind for each affected node.

**Adding a node:**

1. Click **+ Add ComputeNode** at the bottom of the card.
2. Fill in the form:
   - **Name** — kebab-case, e.g. `gpu-box-1`.
   - **Kind** — choose `ollama` or `openai-compat` from the dropdown.
   - **Address** — URL of the LLM endpoint (e.g. `http://gpu-box-1:11434`).
   - **Monitoring endpoint** (optional) — sidecar pull URL for on-demand live stats.
   - **Declared capacity** — GPU count, GPU memory (GB), RAM (GB), max concurrent models.
   - **GPU vendor / model** — e.g. `nvidia` / `RTX 4090`.
   - **Tags** — comma-separated labels.
   - **Scheduling priority** — 0–100, default 50.
   - **Allowed / Denied consumers** — leave blank to allow all.
   - **Cost per hour** — USD for accounting.
3. Click **Add**. The daemon probes the endpoint before saving; if the probe fails, a warning appears. Use the **Save anyway (skip probe)** link if the node is temporarily unreachable but you want to persist the entry.

<!-- screenshot: Add ComputeNode form with Ollama kind selected -->

**Editing a node:**

1. Click the pencil (edit) icon on any node row.
2. The same form opens pre-populated with the current values.
3. The form also shows the node's **current models** (fetched live from the endpoint) and a **Browse marketplace** button for Ollama nodes.
4. Click **Save** when done.

<!-- screenshot: Edit ComputeNode form with models list and Browse marketplace button -->

**Ollama marketplace (Ollama nodes only):**

1. In the edit form for an Ollama node, click **Browse marketplace**.
2. A catalog of curated Ollama models appears with size, RAM requirements, and VRAM requirements.
3. Models that fit within the node's declared capacity are highlighted.
4. Click **Pull** next to a model to start a background download. A dock notification tracks the task.

<!-- screenshot: Ollama marketplace modal with catalog entries and Pull buttons -->

**Disabling / enabling a node:**

- The toggle switch on each node row enables or disables it.
- Disabling stops the dispatcher from routing to the node immediately; existing in-flight requests are not interrupted.
- When a node is re-enabled, any stale dispatch error is cleared.

<!-- screenshot: Compute Nodes card showing enabled/disabled toggles and health badges -->

**Attaching an observer peer:**

1. In the edit form, find the **Observer peer** field.
2. The dropdown lists registered `datawatch-stats` peers that match the node name or are currently unbound.
3. Select a peer and click Save.

**On-demand detail / health:**

- Click a node row to open its detail panel (live stats from the monitoring sidecar or the last cached observer snapshot).
- The detail panel auto-refreshes on a short interval while open.

---

## Other channels

### REST

All endpoints require the standard `Authorization: Bearer $TOKEN` header. Set `BASE=https://localhost:8443` (or your daemon's address).

```sh
# List all nodes.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/compute/nodes

# Fetch one node.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/compute/nodes/gpu-box-1

# Add a node.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "gpu-box-1",
    "kind": "ollama",
    "address": "http://gpu-box-1:11434",
    "scheduling_priority": 80,
    "declared_capacity": {
      "gpus": 2,
      "gpu_mem_gb": 48,
      "ram_gb": 128,
      "max_concurrent_models": 4
    },
    "tags": ["production"]
  }' \
  $BASE/api/compute/nodes
# → {"name":"gpu-box-1","ok":true}

# Skip the probe when the node is temporarily unreachable.
curl -sk -X POST "..." $BASE/api/compute/nodes?probe=skip

# Update a node (full replace).
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"gpu-box-1","kind":"ollama","address":"http://gpu-box-1:11434","scheduling_priority":90}' \
  $BASE/api/compute/nodes/gpu-box-1

# Delete a node.
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  $BASE/api/compute/nodes/gpu-box-1

# Health — declared capacity + maintenance state.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/compute/nodes/gpu-box-1/health
# → {"name":"gpu-box-1","in_maintenance":false,"declared_capacity":{...},...}

# Detail — live stats from monitoring sidecar or cached observer snapshot.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/compute/nodes/gpu-box-1/detail

# List models available on this node.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/compute/nodes/gpu-box-1/models?kind=ollama"
# → {"models":["llama3.1:8b","qwen3:8b"],"kind":"ollama","node":"gpu-box-1"}

# Pull an Ollama model (background; returns task_id).
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.1:8b"}' \
  $BASE/api/compute/nodes/gpu-box-1/models/pull
# → {"task_id":"abc123","status":"queued"}

# Poll pull task status.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/marketplace/ollama/tasks/abc123

# Remove a model.
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  $BASE/api/compute/nodes/gpu-box-1/models/llama3.1:8b

# Enable / disable a node.
curl -sk -X PATCH \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}' \
  $BASE/api/compute/nodes/gpu-box-1/enabled

# Attach an observer peer.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"peer":"gpu-box-1"}' \
  $BASE/api/compute/nodes/gpu-box-1/observer-peer

# Detach an observer peer.
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  $BASE/api/compute/nodes/gpu-box-1/observer-peer

# List peers with no bound node.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/peers/free

# Local peers grouped by bound node.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/peers/by-node

# Federation meta-peers view (cross-instance).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/federation/meta-peers

# Browse the Ollama model catalog.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/marketplace/ollama/catalog
```

### REST endpoint summary

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/compute/nodes` | List all nodes |
| POST | `/api/compute/nodes` | Create a node (`?probe=skip` to bypass connectivity check) |
| GET | `/api/compute/nodes/{name}` | Fetch one node |
| PUT | `/api/compute/nodes/{name}` | Replace a node (`?probe=skip` available) |
| DELETE | `/api/compute/nodes/{name}` | Remove a node |
| GET | `/api/compute/nodes/{name}/health` | Declared capacity + maintenance state |
| GET | `/api/compute/nodes/{name}/detail` | Live stats from sidecar or cached observer snapshot |
| GET | `/api/compute/nodes/{name}/models` | List models (`?kind=ollama` or `?kind=openai-compat`) |
| POST | `/api/compute/nodes/{name}/models/pull` | Pull an Ollama model (background) |
| DELETE | `/api/compute/nodes/{name}/models/{model}` | Remove a model |
| PATCH | `/api/compute/nodes/{name}/enabled` | Enable or disable a node |
| PUT | `/api/compute/nodes/{name}/observer-peer` | Attach an observer peer |
| DELETE | `/api/compute/nodes/{name}/observer-peer` | Detach the observer peer |
| GET | `/api/observer/peers/free` | Peers with no bound node |
| GET | `/api/observer/peers/by-node` | Peers grouped by bound node |
| GET | `/api/federation/meta-peers` | Cross-instance peer view |
| GET | `/api/marketplace/ollama/catalog` | Curated Ollama model catalog |
| GET | `/api/marketplace/ollama/tasks/{id}` | Poll a model-pull task |

### MCP

Connect your MCP host (Claude Code, Cursor, etc.) to the datawatch MCP server. All tools proxy to the REST layer.

```
# List all Compute Nodes.
compute_node_list

# Fetch one Compute Node.
compute_node_get name=gpu-box-1

# Add a Compute Node.
compute_node_add name=gpu-box-1 kind=ollama address=http://gpu-box-1:11434 \
  max_concurrent_models=4 gpu_mem_gb=48 scheduling_priority=80 tags=production,high-mem

# Update a Compute Node.
compute_node_update name=gpu-box-1 kind=ollama address=http://gpu-box-1:11434 \
  scheduling_priority=90

# Delete a Compute Node.
compute_node_delete name=gpu-box-1

# Health check.
compute_node_health name=gpu-box-1

# On-demand live detail.
compute_node_detail name=gpu-box-1

# Attach an observer peer.
compute_node_attach_observer name=gpu-box-1 peer=gpu-box-1

# Detach the observer peer.
compute_node_detach_observer name=gpu-box-1

# List unbound observer peers.
observer_peers_free

# Local peers grouped by bound node.
observer_peers_by_node

# Federation meta-peers view.
federation_meta_peers

# Browse Ollama catalog.
marketplace_ollama_catalog

# Pull an Ollama model onto a node.
compute_node_pull_model name=gpu-box-1 model=llama3.1:8b

# Poll a pull task.
marketplace_pull_task task_id=abc123

# Remove a model from a node.
compute_node_remove_model name=gpu-box-1 model=llama3.1:8b
```

### Comm channel

Send these verbs to your configured comm channel (Signal, Telegram, Discord, etc.):

```
# List nodes.
compute

# List nodes (explicit form).
compute node list

# Fetch one node.
compute node get gpu-box-1

# Add a node.
compute node add gpu-box-1 kind=ollama address=http://gpu-box-1:11434 \
  max_concurrent_models=4 gpu_mem_gb=48 scheduling_priority=80 tags=production,high-mem

# Update a node.
compute node update gpu-box-1 scheduling_priority=90

# Delete a node.
compute node delete gpu-box-1

# Health check.
compute node health gpu-box-1

# On-demand live detail.
compute node detail gpu-box-1

# Attach an observer peer.
compute node attach-observer gpu-box-1 gpu-box-1

# Detach.
compute node detach-observer gpu-box-1

# Unbound peers.
compute node observer-free

# Peers grouped by node.
compute node observer-by-node

# Federation meta view.
compute node federation-meta-peers

# Pull model.
compute node pull-model gpu-box-1 llama3.1:8b

# Remove model.
compute node remove-model gpu-box-1 llama3.1:8b
```

---

## YAML — direct file edit

You can seed `~/.datawatch/compute/nodes.json` before starting the daemon. This is useful for scripted deploys. The daemon loads and validates the file on startup.

```json
[
  {
    "name": "gpu-box-1",
    "kind": "ollama",
    "address": "http://gpu-box-1:11434",
    "scheduling_priority": 80,
    "declared_capacity": {
      "gpus": 2,
      "gpu_mem_gb": 48,
      "ram_gb": 128,
      "max_concurrent_models": 4
    },
    "hardware": {
      "os": "linux",
      "arch": "x86_64",
      "gpu_vendor": "nvidia",
      "gpu_model": "rtx-4090",
      "gpu_count": 2,
      "memory_gb": 128,
      "cpu_cores": 32
    }
  }
]
```

After editing `nodes.json` directly, send `datawatch reload` (or `SIGHUP`) to apply changes without a full restart.

---

## Auto-created nodes

When a `datawatch-stats` monitoring peer pushes its first metrics snapshot and no Compute Node with that peer's name exists yet, the daemon auto-creates a node with:

- `kind = ollama` (safe default — edit via the form if needed)
- `address` = the peer's reported address
- `max_concurrent_models = 1`
- `scheduling_priority = 50`
- `observer_peer` set to the peer name
- `auto_created = true`

Auto-created nodes appear with a migration banner in the PWA prompting you to confirm the kind and capacity. They work immediately for routing but are incomplete until you fill in the hardware spec and confirm the kind.

Leaked auto-created nodes (where the backing observer peer was deleted) are swept on daemon startup.

---

## Per-node model assignment

After adding a Compute Node, go to **Settings → Compute → LLMs** to create or edit an LLM registry entry. The `compute_nodes` field on each LLM entry is an ordered list of Compute Node names. The dispatcher tries them in order and fails over to the next when a node is unreachable, disabled, or in maintenance.

```json
{
  "name": "my-llama",
  "kind": "ollama",
  "model": "llama3.1:70b",
  "compute_nodes": ["gpu-box-1", "gpu-box-2"]
}
```

See [`v7-compute-migration.md`](v7-compute-migration.md) for the full migration from v6 per-backend config blocks to the LLM registry.

---

## Scheduling priority and permissions

`scheduling_priority` (0–100, default 50) is a tiebreaker hint for the dispatcher. When multiple nodes are eligible for the same workload, the highest priority wins. Use higher values for nodes with faster GPUs or lower latency.

`permissions.allowed_consumers` restricts which callers may route to this node. Valid consumer names are `council`, `ask`, `agent_spawn`, `session_spawn`, and `*` (wildcard). An empty `allowed_consumers` list means all consumers are allowed. `denied_consumers` always wins over `allowed_consumers`.

Example — a high-priority inference-only node that Automata may not use:

```sh
datawatch compute node add inference-only-1 \
  --kind ollama \
  --address http://inference-1:11434 \
  --priority 95 \
  --allowed-consumers "council,ask" \
  --denied-consumers "agent_spawn"
```

---

## Maintenance windows

Maintenance windows tell the scheduler to avoid placing workloads on a node during a defined period. Windows are set via REST or direct `nodes.json` edit (the CLI and MCP tools do not yet expose a maintenance window flag — use REST or the PWA form).

```sh
# Set a maintenance window via REST.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "gpu-box-1",
    "kind": "ollama",
    "address": "http://gpu-box-1:11434",
    "maintenance_windows": [
      {
        "from": "2026-06-01T02:00:00Z",
        "to":   "2026-06-01T06:00:00Z",
        "reason": "kernel upgrade"
      }
    ]
  }' \
  $BASE/api/compute/nodes/gpu-box-1
```

An open-ended window (no `to`) means "from now until manually removed." The health endpoint reports `in_maintenance: true` during active windows.

---

## Diagram

```
  Settings → Compute → Compute Nodes card
         │
         │ + Add / Edit / Delete / Toggle
         │
  ~/.datawatch/compute/nodes.json  ←→  REST /api/compute/nodes/*
         │                               MCP compute_node_*
         │                               CLI datawatch compute node *
         │                               Comm: compute node *
         │
         ▼
  Dispatcher (ordered failover)
    ┌──────────────────────┐
    │ Compute Node A       │←── observer_peer (datawatch-stats push)
    │  kind, address, cap  │←── monitoring_endpoint (on-demand pull)
    └──────────────────────┘
    ┌──────────────────────┐
    │ Compute Node B       │  (failover target)
    └──────────────────────┘
         │
         ▼
  LLM endpoint (Ollama / OpenAI-compat)
```

---

## Troubleshooting

### Probe failed when adding or updating

The daemon probes the endpoint before saving (15-second timeout). Add `?probe=skip` to the REST call, or check the "Save anyway" checkbox in the PWA form, to skip the probe when the node is temporarily unreachable.

```sh
# CLI: probe is always run; to skip, use REST directly:
curl -sk -X POST "..." "$BASE/api/compute/nodes?probe=skip"
```

### Node shows `in_maintenance: true` unexpectedly

Check `maintenance_windows` on the node. An open-ended window (no `to` field) stays active until removed. Update the node via REST with an empty `maintenance_windows` array to clear all windows.

### `detail` endpoint returns 503

The detail endpoint falls back in this order: (1) pull from `monitoring_endpoint`, (2) last cached snapshot from the bound `observer_peer`, (3) error. If neither is available, the response says so. Set `monitoring_endpoint` or wait for the observer peer to push a snapshot.

### Auto-created node has wrong kind

Auto-created nodes default to `ollama`. If your endpoint is an OpenAI-compatible API (e.g. vLLM, OpenWebUI), edit the node and change `kind` to `openai-compat`. The PWA shows a migration banner on auto-created nodes until the kind is confirmed.

### Deprecated kind value (`local`, `ssh`, `docker`, `k8s`, `remote`, `remote-proxy`)

These kinds are accepted for parsing but the dispatcher refuses to route through them. The PWA shows a migration banner. Open the edit form, select `ollama` or `openai-compat`, and save.

### Orphaned auto-created nodes after observer peer deletion

The daemon sweeps leaked auto-created nodes on startup (matches `auto_created: true` nodes whose bound peer no longer exists). If you find orphaned nodes, restart the daemon or delete them manually:

```sh
datawatch compute node delete leaked-node-name
```

### Observer peer attached but detail shows stale data

Observer-peer going offline does **not** clear the binding (by design — offline ≠ decommissioned). When the peer comes back online, it resumes pushing snapshots and the detail endpoint shows fresh data again. To permanently detach:

```sh
datawatch compute node detach-observer gpu-box-1
```

### Model pull task stays in `queued` or `running`

Check the daemon log for errors. Ollama must be reachable on the node and have sufficient disk space. The task status is available at:

```sh
datawatch marketplace task <task_id>
# or
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/marketplace/ollama/tasks/<task_id>
```

---

## See also

- [`v7-compute-migration.md`](v7-compute-migration.md) — migrating v6 backend config blocks to the v7 LLM registry
- [`federated-observer.md`](federated-observer.md) — multi-host stats + observer peer federation
- [`council-mode.md`](council-mode.md) — how Council Mode selects Compute Nodes via the LLM registry
- [`sessions-deep-dive.md`](sessions-deep-dive.md) — how sessions resolve their LLM and Compute Node
- [`daemon-operations.md`](daemon-operations.md) — reload, restart, log inspection
- API: `/api/compute/nodes` (Swagger UI at `/api/docs`)
- Architecture: [`../architecture-overview.md`](../architecture-overview.md) § Compute
