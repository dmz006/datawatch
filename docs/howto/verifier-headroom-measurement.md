---
docs:
  index: true
  topics: [compute, verifier, headroom, verifier_slots, capacity, measurement, node, concurrency]
exec_steps:
  - tool: compute_node_headroom_get
    description: Read the stored headroom measurement for one node: age, freshness, and the cap it supports
    args: {name: "{{params.node}}"}
    read_only: true
  - tool: compute_node_headroom_measure
    description: Run a fresh headroom measurement on an idle node and store it
    args: {name: "{{params.node}}"}
    read_only: false
---

# Verifier headroom measurement

**Verifier headroom measurement** is the check that keeps a compute node's
verifier concurrency cap honest. Every node runs at a default cap of **one
concurrent verifier session**. Raising the cap to 2 or more is only accepted
when a measurement taken while the node was idle shows enough free memory to
cover the extra verifiers on top of the model that is resident. A node that
cannot prove the headroom runs at the default and the gate says why.

This guide covers what a measurement records, the safety conditions that
govern it, how the gate decides, how to raise a cap and what every refusal
message reads, what a daemon restart does to a measurement, and every access
method: YAML, CLI, web UI, REST API, MCP, and the comm channel.

## Base requirements

- A daemon with at least one compute node registered (`compute_nodes`).
- The node's model loaded — a measurement reads free memory **with the
  resident model loaded**, so it must be resident.
- A pull-allowed model list on the node. An empty list refuses every
  measurement with **403 no-allowed-models**.
- Autonomous mode running, for the cap to matter: the cap bounds how many
  verifier sessions the admission path may admit on the node at once. The
  cap sits next to the worker slots and never replaces the executor pin —
  one worker session per node still holds.

## How a measurement works

A measurement records, for one node at one instant:

| Field | Meaning |
|-------|---------|
| `free_mb` | Free memory with the resident model loaded — free GPU memory summed over the node's GPUs, or free system memory when the node has no GPU data |
| `per_request_mb` | How much memory one extra concurrent request needs beyond the resident model, estimated from one small request at the node's Ollama endpoint against the smallest pull-allowed model |
| `resident_model` | The model resident on the node at measurement time |
| `measured_at` / `age_seconds` | When the measurement was taken and how old it is now |
| `fresh` | Whether the measurement is still inside the freshness bound (`autonomous.verifier_headroom_max_age_hours`) |
| `supported_cap` | The verifier cap this measurement covers (see the gate below) |

The measurement is stored **per node and in memory only**: a new measurement
replaces the previous one for that node, and a daemon restart clears it.

### Safety conditions

A measurement is taken only when every one of these holds:

1. **The node is idle.** Any worker holding a lease on the node refuses the
   measurement with **409** and the body `{"error":"node busy","node":"gpu-a","held":2}`.
   A loaded node reports less free memory than its true idle headroom, so a
   loaded node is never measured.
2. **The pull-allowed model list is not empty.** An empty list refuses the
   measurement with **403** and the body
   `{"error":"model not allowed","node":"gpu-a"}`.
3. **The resident model is on the allow list.** A measurement taken while a
   model outside the list is resident would report numbers the gate must not
   rely on, so the same **403** refusal applies.
4. **The probe succeeds.** The probe reads the node's observer data for free
   memory and issues one small Ollama request against the smallest allowed
   model. If the request fails, the measurement fails with **502** and the
   probe's error.

Because the probe issues one real request, `per_request_mb` is an estimate:
a model that was already resident before the probe request costs little or
nothing.

## How the gate decides

One gate, the same rule in both places it runs:

> A node may run a verifier cap of **N** above 1 only while a fresh
> measurement shows `free_mb − min_free_mb ≥ (N−1) × per_request_mb`.

- A cap of 1 or below — including `verifier_slots: 0`, which reads as the
  default cap 1 — always passes; no measurement is needed for it.
- `min_free_mb` is `autonomous.verifier_min_free_mem_mb` (default **2048**,
  must be ≥ 0): the memory the node must keep free even with the extra
  verifiers running.
- A measurement older than `autonomous.verifier_headroom_max_age_hours`
  (default **24**; unset or 0 reads as 24; negative is a config error)
  counts as no measurement.

Two checks in practice, both answering with the same gate:

| Check | When it runs | Freshness bound used | What a refusal does |
|-------|--------------|----------------------|---------------------|
| **Write gate** | Saving a node with `verifier_slots` above 1 (`POST/PUT /api/compute/nodes[/{name}]` and the surfaces that call it) | `autonomous.verifier_headroom_max_age_hours` | The save is refused with **409** and the gate's message — nothing is written |
| **Admission gate** | Every capacity sync, when the daemon computes the node's effective verifier cap | the admission path's own default (10 min) | The node runs at the supported value (1 while stale) and the refusal is logged once per node per change |

The gate has three refusal reasons:

| Reason | Message (the node is named) |
|--------|-----------------------------|
| No measurement (never measured, or no measurement source) | `verifier cap on node gpu-a refused: no fresh measurement` |
| Measurement older than the bound | `verifier cap on node gpu-a refused: measurement stale` |
| Free memory does not cover the extra requests | `verifier cap on node gpu-a refused: measured free 6000 MB, shortfall 2048 MB (insufficient free memory)` |

`supported_cap` in a measurement read is the cap that measurement covers:
**1** while the measurement is stale (the gate's fallback), **0** when
unbounded (the per-request cost measured zero — the ledger's "0 = unlimited"
convention), otherwise `1 + (free_mb − min_free_mb) / per_request_mb` with
the fractional part dropped.

## Setup

Nothing to set: every node runs at the default cap 1 and the two autonomous
keys read their defaults when absent. The knobs are:

| Key | Default | Meaning |
|-----|---------|---------|
| `autonomous.verifier_min_free_mem_mb` | 2048 | Free-memory floor (MB) the node must keep free with the extra verifiers running |
| `autonomous.verifier_headroom_max_age_hours` | 24 | How old a measurement may be and still count as fresh |
| `compute_nodes[].verifier_slots` (per node) | 1 (absent or 0) | The node's verifier cap; a negative value is a config error |

All three are read at config load and by the capacity sync, so changing them
takes effect without a restart.

## Walkthrough

Node `gpu-a` is idle with its model loaded. Goal: let it run two verifiers.

**1. Measure while idle** (any method in the table below):

```console
$ datawatch compute headroom measure gpu-a
{"node":"gpu-a","free_mb":8000,"per_request_mb":2000,"resident_model":"qwen2.5:32b",
 "measured_at":"2026-11-05T17:40:00Z","age_seconds":0,"fresh":true,
 "min_free_mb":2048,"max_age_hours":24,"supported_cap":3}
```

**2. Read back what the measurement supports.** Cap 2 needs
`(2−1) × 2000 = 2000 MB` on top of the 2048 MB floor: `8000 − 2048 = 5952
≥ 2000`, so it is covered — in fact the measurement supports cap 3
(`1 + 5952/2000`, fraction dropped).

**3. Raise the cap.** Node saves are whole-node updates, so the update
carries the node's other fields with it:

```console
$ curl -s -X PUT http://localhost:8080/api/compute/nodes/gpu-a \
    -H 'Content-Type: application/json' \
    -d '{"name":"gpu-a","kind":"ollama","address":"http://gpu-a:11434","verifier_slots":2}'
# → 200, node saved with verifier_slots 2
```

**4. Confirm the cap is running.** The capacity snapshot carries the
per-node verifier cap (`configured`, `effective`, and the gate's refusal
when the cap is refused):

```console
$ datawatch capacity show
# … pool node:gpu-a — verifier cap configured 2, effective 2 (no refusal)
```

or `GET /api/capacity` / the `capacity_status` MCP tool.

**5. Watch the refusals.** Two refusals to expect while operating:

- Saving `verifier_slots: 4` today would be refused with **409**, body
  `{"error":"verifier cap on node gpu-a refused: measured free 8000 MB,
  shortfall 48 MB (insufficient free memory)","node":"gpu-a","free_mb":8000,
  "shortfall_mb":48}` — cap 4 needs `3 × 2000 = 6000 MB` on top of the
  floor, and only 5952 MB is free (`8000 − 2048`).
- After the measurement ages past the bound (or a restart clears it), the
  admission gate refuses with `no fresh measurement` / `measurement stale`,
  the node runs at 1, and the daemon logs
  `[capacity] node gpu-a verifier cap: configured 2 refused (… reason …),
  running 1` — once per change, and again as `refusal cleared, running 2`
  when the next measurement covers it.

## All options: five access methods

Every method reaches the same store and the same gate. MCP is a sixth,
proxying the same REST routes.

| Method | How | Notes |
|--------|-----|-------|
| **YAML** | `config.yaml` → `compute_nodes[].verifier_slots` per node, plus `autonomous.verifier_min_free_mem_mb` and `autonomous.verifier_headroom_max_age_hours` | Config load validates the keys; a negative `verifier_slots` refuses the load |
| **CLI** | `datawatch compute headroom measure <node>` (run a measurement), `datawatch compute headroom show <node>` (read the stored one); node updates take `--verifier-slots` | The measure command exits non-zero with the server's refusal text on 403/409/502/503 |
| **Web UI** | Compute → node row → detail: the **Headroom** block shows the stored measurement (measured at, age, free, per-request, supported cap) with a fresh/stale badge, and a **Measure now** button that runs the measurement and refreshes the block | 403/409 surface as an error toast with the server's reason |
| **REST API** | `POST /api/compute/nodes/{name}/headroom` (measure), `GET /api/compute/nodes/{name}/headroom` (read); `POST/PUT /api/compute/nodes[/{name}]` for capped saves | Statuses below |
| **Comm channel** | `compute headroom measure <node>` / `compute headroom <node>` | Replies are the JSON body pretty-printed, or the refusal text |
| **MCP** | `compute_node_headroom_measure` / `compute_node_headroom_get` | Thin proxies over the two REST routes; same bodies and status semantics |

### REST status and body reference

| Route | Status | Meaning |
|-------|--------|---------|
| `POST …/headroom` | 409 `{"error":"node busy","node":N,"held":H}` | Workers hold the node; wait for it to go idle |
| `POST …/headroom` | 403 `{"error":"model not allowed","node":N}` | Pull-allowed list empty, or resident model not on it |
| `POST …/headroom` | 502 | The probe's memory request failed |
| `POST …/headroom` | 503 | Headroom store not wired into this daemon build |
| `GET …/headroom` | 200 | The stored measurement (fields above) |
| `GET …/headroom` | 404 `{"error":"no headroom measurement for node N"}` | Never measured (yet) on this build |
| `GET …/headroom` | 503 | Headroom store not wired into this daemon build |
| `POST/PUT …/nodes` with `verifier_slots` > 1 | 409 `{"error":<gate message>,"node":N,"free_mb":F,"shortfall_mb":S}` | The write gate refused; `free_mb`/`shortfall_mb` only when a measurement existed |

### Where to see

| Surface | What you see |
|---------|--------------|
| PWA node detail | The Headroom block: stored measurement, fresh/stale badge, supported cap, Measure now |
| `GET /api/capacity` (CLI `capacity show`, MCP `capacity_status`) | Per-node `verifier_cap` — `configured`, `effective`, and the gate's refusal text when the cap is refused |
| Daemon log | `[capacity] node N verifier cap: configured C refused (… reason …), running R` — once per node per change |
| Any node-save refusal | The 409 body with the gate's message, `free_mb` and `shortfall_mb` |

## Raising the cap

1. Make the node idle (no worker leases).
2. Measure (`compute headroom measure <node>` or any method in the table).
3. Check `supported_cap`: it is the highest cap the measurement covers.
4. Save the node with `verifier_slots` at or below `supported_cap`.
5. Re-measure after any restart, or after the measurement ages past
   `autonomous.verifier_headroom_max_age_hours` — a stale or missing
   measurement drops the node back to 1 until it is measured again while
   idle.

To lower the cap, save the node with a smaller `verifier_slots` — the gate
only ever refuses *higher* caps.

## What a restart does

The store is memory-only by design: a daemon restart clears every
measurement. After a restart, a node with `verifier_slots` above 1 runs at
1 (`no fresh measurement`) and the save path refuses any *higher* cap until
the node is measured again while idle. Nothing is lost on disk and nothing
is re-run automatically — the operator re-measures when the node is idle.

## What this never does

- It never measures a loaded node (409).
- It never measures with a model outside the pull-allowed list (403).
- It never raises the cap above what a fresh measurement covers, in either
  gate.
- It never replaces the executor pin — worker slots and the one-session-per-
  node pin are untouched by the verifier cap.
- It never persists measurements across restarts.

## Parity surface

Config input and node detail exist on every channel the node registry has:
YAML, CLI, web UI, REST API, MCP, and the comm channel, all against the same
store and the same gate. Android and iPhone (iOS) are out of scope for this
change: it is config input and node detail only, with no mobile surface. A
datawatch-app parity issue for the mobile surfaces is still to be filed.

## Where this stands in the current build

The gates, the config keys, the store and every surface above exist and are
covered by tests. In the daemon build shipping today, the measurement store
is not wired into the HTTP surface: the measure/read routes answer **503
"headroom store not wired"**, the write gate sees no measurement (a cap
above 1 is refused with `no fresh measurement`), and the admission path
runs every node at 1. The wiring setters exist on the server
(`SetVerifierHeadroom`, `SetHeadroomProbe`, `SetHeadroomAllowedModels`);
once the daemon calls them, everything in this guide is what the surfaces
return. Until then, plan on the refusal columns above as the operator-visible
behaviour.
