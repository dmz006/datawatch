---
title: "Model placement — when the model is missing on a node"
summary: "The missing-model policy (ask / auto_load / refuse), the headroom and size settings, the one-shot placement ask queue and its five surfaces (YAML, CLI, web UI, REST, comm channel — plus MCP)."
topic: "compute model placement"
aliases:
  - model placement
  - placement ask
docs-index: howtos
topics: [howto, compute, placement, operator-guide]
exec_steps:
  - description: "Read the current compute placement policy and limits from the live config."
    tool: get_config
    args: {}
    read_only: true
  - description: "Set the missing-model policy to ask (confirm with the operator before loading a missing model)."
    tool: config_set
    args:
      key: compute_placement.missing_model
      value: "ask"
    read_only: false
  - description: "List the placement asks pending approval and the ones already decided or expired."
    tool: compute_placement_list
    args: {}
    read_only: true
  - description: "Approve a pending placement ask; the model is loaded onto the named node, and the ask is closed."
    tool: compute_placement_answer
    args:
      id: "{{params.ask_id}}"
      decision: approve
    read_only: false
exec_params:
  ask_id:
    description: "Placement ask id (the first line of a list entry) of a pending ask."
example_input: "Set the missing-model policy to ask, then list the pending placement asks and approve the first one."
---

# Compute model placement — how to

What you will be able to do by the end:

- Set and read the missing-model policy (`compute_placement.missing_model`: `ask` / `auto_load` / `refuse`) and its limits (`mem_headroom_pct`, `disk_headroom_gb`, `ask_timeout_minutes`, `assume_model_size_gb`) from any of the five surfaces — YAML, CLI, the web UI, REST, and the comm channel (plus MCP).
- Read the placement ask queue: which asks are pending, what node and model each one would load, and which answers are one-shot.
- Approve or deny a pending ask, and see the result in the audit trail.

## Before you start

- A Datawatch daemon with at least one compute node in the registry (`datawatch compute node list`).
- A model that a request wants but that is not resident on the node being routed to. Placement decisions only happen then — nodes that already serve the model are picked with no ask, no load and no audit event beyond the load itself.
- For the `ask` policy, somewhere to answer from: the PWA card, the CLI, the comm channel, REST, MCP, or the API.

## Concepts

A **model placement** is the pick of which compute node a model runs on when the model is
not already resident. Datawatch plans the placement with the node's known state:

- Free memory and free disk, both minus a headroom you set, so the node does not run
  out of room for the model and the work that runs on it.
- The model's size in GiB; a node with a small disk cannot hold a large model.
- The node's own state: disabled nodes, nodes inside their maintenance window, and
  nodes pinned to another model (see [the pin how-to](compute-nodes.md)) are never
  silently overridden.

When no eligible node has the model, the daemon applies the **placement policy**
(`compute_placement.missing_model`):

| Policy | What happens |
|---|---|
| `ask` (default) | Daemon records a **placement ask** — the node, the model, the reason and the size — and waits for you to **approve** (it then loads the model) or **deny** (it refuses the placement). If nobody answers inside `ask_timeout_minutes` the ask expires automatically, which is the same as a refusal. |
| `auto_load` | Daemon loads the model onto the chosen node without asking. |
| `refuse` | Daemon does not load the model; the placement is refused and the load is audited as `placement_refused`. |

One-shot: a given ask can be answered exactly once. An approved, denied or expired ask
is closed and cannot be re-decided — a second answer attempt is refused (HTTP `409`), and
the audit log carries exactly one answer per ask.

**Every state change is in the audit trail.** The five events, each with its own CEF
signature (88–92), are `placement_asked`, `placement_approved`, `placement_denied`,
`placement_loaded` and `placement_refused`. Each names the node, the model, the policy in
force at the time, and the reason text. See [the audit logging how-to](audit-logging.md).

## The settings

All five keys live under `compute_placement:` in `~/.datawatch/config.yaml`. All of them
are read at decision time, so a change takes effect the next time placement is decided —
no restart is needed for a policy change.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `compute_placement.missing_model` | enum `ask` \| `auto_load` \| `refuse` | `ask` | What happens when the requested model is not resident on the node it was routed to. `ask` confirms with the operator first (this page); `auto_load` loads it; `refuse` skips the load. |
| `compute_placement.mem_headroom_pct` | non-negative int (percent) | `10` | Free memory kept out of reach when a node is considered. A node needs the model to fit *and* this much free memory left over. |
| `compute_placement.disk_headroom_gb` | non-negative int (GiB) | `5` | Free disk kept out of reach when a node is considered. A node needs the model to fit *and* this much free disk left over. |
| `compute_placement.ask_timeout_minutes` | non-negative int (minutes) | `30` | How long a placement ask waits for an answer before it expires automatically. Expired is a closure, not an approval: the load does not happen. `0` means the ask has no timeout and stays pending until someone answers it. |
| `compute_placement.assume_model_size_gb` | non-negative int (GiB) | `0` | Size to assume for a model that a node is known to carry but the daemon is not told its size. `0` means "unknown". This only affects the free-space estimate in the placement check. |

Values are whole numbers of their unit; the CLI and the PWA reject negatives at the surface.

## Set the policy (YAML)

In `~/.datawatch/config.yaml`:

```yaml
compute_placement:
  missing_model: ask           # ask | auto_load | refuse — default ask
  mem_headroom_pct: 10         # free memory kept out of reach — default 10
  disk_headroom_gb: 5          # free disk kept out of reach — default 5
  ask_timeout_minutes: 30      # how long an ask waits before it expires — default 30
  assume_model_size_gb: 0      # size of a node's model the daemon is not told about — default 0
```

Save the file and restart the daemon (`systemctl --user restart datawatch`). The same
keys can be changed live without a restart through the CLI, PWA, REST, comm channel or
MCP surfaces below; the YAML file is the durable record and what those surfaces write.

## Set the policy and answer asks — surface by surface

The five surfaces, identical in effect:

### CLI

- Read the current settings: `datawatch compute placements config-get` — the `compute_placement` policy block, live values with defaults read through
- Change a setting: `datawatch compute placements config-set --missing-model auto_load --mem-headroom-pct 20 --disk-headroom-gb 10 --ask-timeout-minutes 60 --assume-model-size-gb 40` — applied live, no restart; every flag is optional, omit the ones you do not want to change (`mem_headroom_pct` must be 0–100)
- List asks: `datawatch compute placements list` — prints the daemon's JSON: the pending asks under `asks` (each with `id`, `model`, `node`, `reason`, `model_size_gb`, `requested_at`, `expires_at`, `decision`) and their count under `pending`
- Answer: `datawatch compute placements approve <ask-id>` / `datawatch compute placements deny <ask-id>` — returns the ask record with the final `decision`, `decided_at` and `decided_by` (`operator`)

The generic `datawatch config set compute_placement.missing_model ask` works for any of
the five keys too.

### Web UI (PWA)

Settings → General carries a **Compute placements** card. It shows the policy and the
numeric settings, and inside the card the pending-ask queue: each pending ask with its
model, node, reason, size and time, with an **Approve** / **Deny** button. When an ask
is decided — or expires — it leaves the queue and the card shows *No placement asks
pending.*; a failed answer (for example a 409 on an already-decided ask) refreshes the
queue from the server so no stale row is left behind. The card's help text records the
one-shot rule: a closed ask (approved, denied or expired) cannot be decided a second
time.

### REST

- `GET /api/compute/placements` — the pending asks: `{"asks": [ …PlacementAsk… ], "pending": N}` (oldest first; expired asks are dropped out of the pending list by this read)
- `POST /api/compute/placements/{id}/approve` — approve; returns `{"ask": …}` with the final `decision`
- `POST /api/compute/placements/{id}/deny` — deny; same response
- Policy: read and write through `GET` / `PUT /api/config` with keys such as `compute_placement.missing_model`

Answering an ask that is already closed returns `409`; an unknown id returns `404`.

### Comm channel

- `compute placement list` — list
- `compute placement approve <ask-id>` / `compute placement deny <ask-id>` — answer
- Policy: `configure compute_placement.missing_model=ask` (any of the five keys)

### MCP

Tools: `compute_placement_list` (read the queue) and `compute_placement_answer` (args
`id` and `decision: "approve" | "deny"`). Policy: `config_set` (args `key` and
`value`) / `get_config`.

Capability-gated where federated: `compute_placement_list` maps to the `compute:list`
capability and `compute_placement_answer` to `compute:write`. Decisions are recorded
on the ask record itself (`decided_by` is `operator`, `timeout` when the ask expires).

## Walkthrough: confirm a model load from the comm channel

You added a 40B model to a pool and a request routes to a node that does not have it.
The policy is `ask` (the default), so the daemon has recorded an ask and nothing has
been loaded:

```
$ datawatch compute placements list
{"asks":[{"id":"ask-7","model":"qwen3.8-64k:40b","node":"gpu-1","reason":"missing model: ask policy, node fits (memory + disk headroom)","model_size_gb":24,"requested_at":"2026-11-02T15:58:41Z","expires_at":"2026-11-02T16:28:41Z","decision":"pending"}],"pending":1}
```

Answer it. Approve pulls (if the node does not have the model) and loads it on the
target node; deny refuses the placement. The reply carries the ask with its final
state:

```
$ datawatch compute placements approve ask-7
{"ask":{"id":"ask-7","model":"qwen3.8-64k:40b","node":"gpu-1","model_size_gb":24,"decision":"approved","decided_at":"2026-11-02T16:05:03Z","decided_by":"operator"}}
```

The audit log now has `placement_asked`, `placement_approved` and — once the load
completes — `placement_loaded`, each naming the node, the model, the policy in force
(`ask`) and the reason. Answering the same ask a second time is refused (409) — it is
already closed and cannot be re-decided. And the ask is gone from the pending list:

```
$ datawatch compute placements list
{"asks":[],"pending":0}
```

If you had left it, the ask would have expired at 16:28:41 and closed as `expired` —
the load would not have happened.

## Where to see the result

| Surface | What you see |
|---|---|
| PWA — Settings → General → **Compute placements** | Policy, the numeric settings, and the pending-ask queue with Approve / Deny |
| CLI | `datawatch compute placements list`, `… approve/deny <id>`, `… config-get` / `… config-set` |
| MCP | `compute_placement_list`, `compute_placement_answer` (args `id`, `decision`); policy via `get_config` / `config_set` |
| REST | `GET /api/compute/placements`; `POST /api/compute/placements/{id}/approve` or `…/deny`; policy via `GET/PUT /api/config` |
| Comm channel | `compute placement list`, `compute placement approve/deny <id>`; policy via `configure` |
| Audit trail | `placement_asked`, `placement_approved`, `placement_denied`, `placement_loaded`, `placement_refused` (CEF 88–92), each naming node, model, policy in force and reason |

## Limits you should know about

- An ask is one-shot. A second approve or deny is refused and audited as such — there is
  no "change my mind" on a closed ask.
- An expired ask is a refusal, not an approval. The load does not happen, and the ask
  is closed — it leaves the pending list and cannot be decided.
- `assume_model_size_gb` is a guess for the free-space estimate only. If the node's
  reported model sizes are the real ones, the setting should stay at 0.
- The headroom keys are global, not per node. A node with more or less real headroom
  needs is handled by choosing the model or its size, not by editing a per-node key.
  (Per-node pins are a different setting — see [the pin how-to](compute-nodes.md).)

## Related

- [Compute nodes](compute-nodes.md) — node registry, pinning, and where the free-space numbers come from.
- [Audit logging](audit-logging.md) — the five placement events and their CEF signatures.
- [Config reference](../config-reference.yaml) — the five `compute_placement.*` keys in the file shape.
