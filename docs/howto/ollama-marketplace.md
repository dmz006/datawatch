---
docs:
  index: true
  topics: [ollama, marketplace, models, pull]
exec_params:
  - name: node
    description: ComputeNode name (kind=ollama), e.g. my-ollama-node
    required: true
  - name: model
    description: Model name with tag, e.g. llama3.1:8b
    required: true
exec_steps:
  - tool: compute_node_list
    description: Confirm Ollama Compute Nodes are registered and reachable
    read_only: true
  - tool: compute_node_models
    description: List models currently installed on the target node
    args:
      name: "{{params.node}}"
    read_only: true
---
# How-to: Ollama Model Marketplace

Browse the Ollama model library, select a size variant, and pull it
directly to a Compute Node — without leaving datawatch and without
running `ollama pull` manually on the host.

## What it is

The Ollama Marketplace is a browseable, searchable catalog of curated
models (llama3.1, qwen3, gemma3, deepseek-r1, codellama, and more).
Each model entry shows available tag variants with their disk size,
minimum RAM, minimum VRAM, and a hardware-fit indicator that checks
whether your target node's declared capacity is sufficient.

Pulling a model is a background operation — the pull runs on the
Ollama host while the daemon tracks progress and surfaces it in the
alert dock. You can continue working while the download completes.
You can also delete models from a node through the same surface.

The embedded catalog is curated and ships with the daemon. It covers
the most commonly used open models. Live refresh from ollama.com is a
planned follow-up.

```
  PWA / CLI / REST / MCP / comm
          │
          ▼
  datawatch daemon
  POST /api/compute/nodes/<node>/models/pull
          │
          ▼   (background goroutine)
  Ollama host — /api/pull (streaming progress)
          │
          ▼
  PullTask in daemon memory
  alert dock: "Pulling llama3.1:8b (N%)"
```

## Base requirements

- datawatch daemon running. See [`setup-and-install.md`](setup-and-install.md).
- At least one Compute Node registered with `kind=ollama` and a reachable
  `address`. See [`compute-nodes.md`](compute-nodes.md) to add one.
- The Ollama service must be running on the target host
  (`ollama serve`). The node's address should point to it
  (default: `http://localhost:11434`).
- The node must be reachable from the daemon at pull time. Verify with:

  ```sh
  datawatch compute node health <node-name>
  ```

## Setup

No extra configuration is needed. The marketplace and pull/delete
endpoints are available on any registered `kind=ollama` Compute Node.

Verify the node is registered and Ollama is reachable:

```sh
# List registered nodes and confirm kind=ollama is present.
datawatch compute node list

# Confirm the node's declared capacity matches your hardware.
datawatch compute node health <node-name>

# List models currently installed on the node.
datawatch compute node get <node-name>
```

## Happy path — CLI

```sh
# 1. Browse the embedded catalog.
datawatch marketplace catalog

# 2. Pull a model to a node (background; returns a task descriptor).
datawatch compute pull-model <node-name> llama3.1:8b
# Output: {"id":"pull-XXXX","node_name":"...","model":"llama3.1:8b","status":"pending",...}

# 3. Poll the pull task until status=done.
datawatch marketplace task <task-id>
# Output: {"id":"pull-XXXX","status":"in_progress","progress":0.42,...}
# ...repeat until "status":"done"

# 4. Confirm the model is now available on the node.
curl -sk http://<ollama-host>:11434/api/tags | jq '.models[].name'

# 5. Delete a model from the node when no longer needed.
datawatch compute remove-model <node-name> llama3.1:8b
```

## Happy path — PWA

![Ollama Marketplace modal](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/ollama-marketplace-modal.png)

1. Bottom nav → **Settings** → **Compute** tab → **Compute Nodes**.
2. Tap or click the Compute Node row for your Ollama node to open its
   edit panel.
3. In the edit panel, find the **Models** sub-section (visible only
   for `kind=ollama` nodes). The installed model list is shown here.
4. Click **Browse marketplace** to open the Ollama Marketplace modal.
5. The modal loads the embedded catalog. Use the **Search models...**
   field to filter by name or description (e.g. "coder", "embed",
   "reasoning").
6. Click **Pick variant →** on any model card to open the tag-grid.

![Tag-grid modal with hardware fit columns](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/ollama-marketplace-tags.png)

7. The tag-grid shows every available size variant with:
   - **Tag** — the full `model:tag` string (e.g. `llama3.1:8b`)
   - **Size** — estimated disk footprint
   - **Min RAM** — floor RAM for inference
   - **Min VRAM** — floor GPU memory (blank = CPU-only is fine)
   - **Fit** — green checkmark if the node's declared capacity covers
     the variant; warning triangle if not
8. Click **Pull** on the row you want. If the variant exceeds the
   node's declared RAM or VRAM, a confirmation dialog asks you to
   confirm before proceeding.
9. The modal closes and a progress entry appears in the **alert dock**
   at the bottom of the screen: *"Pulling llama3.1:8b (0%)"*. It
   updates as the download progresses.

![Alert dock showing pull progress — "🔽 Pulling llama3.1:8b on datawatch-ollama (47%)"](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/howto/screenshots/ollama-marketplace-pull-progress.png)

10. When the pull completes the dock entry shows *done*. Refresh the
    node edit panel to see the new model in the installed list.
11. To delete a model, click the **✕** button next to its name in the
    installed list. The deletion is immediate (no background task).

## REST

All marketplace operations are available over the REST API.

### List the embedded catalog

```sh
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/marketplace/ollama/catalog | jq '.catalog[].name'
```

### List models currently on a node

```sh
# kind=ollama is the default when the node is kind=ollama.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/compute/nodes/<node-name>/models?kind=ollama" | jq .
```

### Start a pull (background; returns task descriptor)

```sh
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model": "llama3.1:8b"}' \
  $BASE/api/compute/nodes/<node-name>/models/pull
# Returns: {"id":"pull-XXXX","node_name":"...","model":"llama3.1:8b","status":"pending",...}
```

### Poll a pull task

```sh
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/marketplace/ollama/tasks/<task-id> | jq '{status:.status,progress:.progress}'
```

### Delete a model

```sh
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/compute/nodes/<node-name>/models/llama3.1:8b
# Returns: {"name":"<node-name>","model":"llama3.1:8b","ok":true}
```

> **Note on model name encoding in DELETE:** URL-encode the colon in
> the tag. Most CLI tools handle this automatically but double-check
> if you get a 404.

## MCP

Four MCP tools cover the full pull lifecycle. They are available in
any MCP host (Claude Code, Cursor, etc.) with the datawatch MCP
server connected.

| Tool | Purpose |
|------|---------|
| `marketplace_ollama_catalog` | List the embedded curated catalog |
| `compute_node_pull_model` | Start a background pull on a named node |
| `marketplace_pull_task` | Poll a pull task by ID |
| `compute_node_remove_model` | Delete a model from a node |

```
# Browse the catalog (no parameters required)
marketplace_ollama_catalog

# Pull a model — returns the task descriptor
compute_node_pull_model
  name: my-ollama-node
  model: llama3.1:8b

# Poll until status=done
marketplace_pull_task
  task_id: pull-XXXX

# Remove a model
compute_node_remove_model
  name: my-ollama-node
  model: llama3.1:8b
```

## Comm channel (chat)

The comm router exposes `pull-model` and `remove-model` as verbs on
the `compute node` path:

```
compute node pull-model <node-name> llama3.1:8b
# Datawatch: {"id":"pull-XXXX","status":"pending",...}

compute node remove-model <node-name> llama3.1:8b
# Datawatch: {"name":"...","model":"llama3.1:8b","ok":true}
```

To list installed models from chat, use the REST surface via a
`compute node get <name>` call (which returns the full node record
including the current model list from Ollama when the node is online).

## YAML

There is no marketplace-specific YAML block. Compute Node registration
(including the `address` that the pull uses) is configured in
`~/.datawatch/datawatch.yaml`:

```yaml
compute:
  nodes:
    - name: my-ollama-node
      kind: ollama
      address: http://localhost:11434
      declared_capacity:
        ram_gb: 32
        gpu_mem_gb: 12
```

Once a node is registered, all pull/delete operations are runtime
calls — no YAML changes needed.

## Curated catalog

The embedded catalog ships with the following models. Each entry
supports the listed tag variants.

| Model | Tags | Notes |
|-------|------|-------|
| `llama3.1` | 8b · 70b · 405b | Meta general-purpose instruct |
| `llama3.3` | 70b | Meta improved 70B instruct |
| `qwen3` | 0.6b · 1.7b · 4b · 8b · 14b · 32b | Alibaba multilingual + math |
| `qwen2.5-coder` | 1.5b · 7b · 32b | Code generation |
| `gemma3` | 1b · 4b · 12b · 27b | Google efficient variants |
| `phi4` | 14b | Microsoft reasoning-tuned |
| `deepseek-r1` | 1.5b · 7b · 8b · 14b · 32b · 70b | Reasoning / chain-of-thought |
| `mistral` | 7b | Mistral fast instruct |
| `mixtral` | 8x7b | Mistral mixture-of-experts |
| `codellama` | 7b · 13b · 34b | Meta code-tuned |
| `nomic-embed-text` | latest | Embedding model for RAG |

For models not in the catalog, skip the marketplace and use the CLI
or REST pull endpoint directly with any valid Ollama model name + tag.

## Common pitfalls

- **Model name must include a tag.** `llama3.1:8b` works; `llama3.1`
  alone will be rejected or pull the wrong default. Always specify the
  colon-separated tag.
- **Pull can take several minutes.** Large models (70b+) are 40–230 GB.
  The progress percentage in the alert dock reflects bytes transferred.
  Do not assume the node is broken if progress stalls briefly — Ollama
  downloads layer-by-layer and may pause between layers.
- **Node must be reachable from the daemon, not just from your browser.**
  If the node is on a remote host that your workstation can reach but
  the daemon cannot (e.g. different network), the pull request will
  succeed at the daemon (task is created) but then immediately fail
  with a connection error. Check `marketplace task <id>` to see the
  error field.
- **Fit warning is advisory.** The hardware-fit column compares the
  tag's minimum requirements against the node's *declared* capacity,
  not measured capacity. If you set `ram_gb` or `gpu_mem_gb` lower
  than actual hardware, the warning fires incorrectly. Update the
  node's declared capacity to reflect real hardware.
- **Delete is immediate and permanent.** There is no recycle bin. If
  you delete a model that a running session is using, that session
  will fail on its next inference call. Stop sessions that use the
  model before deleting it.
- **Pull task state is in-memory.** The daemon stores pull tasks in
  memory with 1-hour retention after completion. If the daemon
  restarts mid-pull the task record is lost, but the pull itself
  continues on the Ollama side. After restart, query Ollama directly
  to check whether the model landed.

## Linked references

- See also: [`compute-nodes.md`](compute-nodes.md) — register, configure, and
  monitor Compute Nodes.
- See also: [`llm-registry.md`](llm-registry.md) — add the newly pulled model
  to an LLM registry entry so sessions can use it.
- See also: [`chat-and-llm-quickstart.md`](chat-and-llm-quickstart.md) — spawn
  a session with a freshly pulled model.

## Screenshots needed

- [ ] Ollama Marketplace modal open — model cards with search bar active
- [ ] Tag-grid modal — Size / Min RAM / Min VRAM / Fit columns visible
- [ ] Alert dock — pull in progress entry with percentage

---

## See also

- [howto/compute-nodes](compute-nodes.md)
- [howto/llm-registry](llm-registry.md)
- [howto/chat-and-llm-quickstart](chat-and-llm-quickstart.md)
- [howto/mcp-tools](mcp-tools.md)
