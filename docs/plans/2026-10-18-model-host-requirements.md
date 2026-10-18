# Model host requirements and shared-model accounting

**Date:** 2026-10-18. Operator-set requirements discovered while running the autonomous
build on two Ollama GPU nodes. This is the single reference for the matrix tests
([BL449](2026-10-15-bl449-open-model-matrix.md)), the usage calculator (BL472), the
automation policy (BL483, BL473, BL431 Phase 2) and the host and repo preparation
instructions.

## 1. A 64k context window is required

Local models used for planning, execution and verification need a 64k context
(`num_ctx` 65536). The 32k default overflowed (`stopReason=length`) and a task with a
large diff could not be verified. Two ways to get it, and the operator uses the first
on both nodes:

- Host default: set `OLLAMA_CONTEXT_LENGTH=65536` for the Ollama service.
- Per model: use a derived model with `num_ctx 65536` (here `qwen3.8-64k:27b`).

Consequences to keep in every document that describes a run:

- **Matrix tests.** Context size is a precondition and a recorded axis of every cell: a
  cell is only valid when the model ran at 64k. Record the effective context and the
  model digest with each result. A result from a 32k run is not comparable.
- **Automation policy.** The policy carries a minimum model context, the pinned model
  name per node, and whether a model swap is allowed (BL494).
- **Operator guide and host preparation.** Setting up a model host includes the context
  length, keep-alive, one large model per node, and a note about other users of the node.

## 2. Keep the chosen model resident

`OLLAMA_KEEP_ALIVE=-1` keeps loaded models in memory. Without it the pinned model lapsed
repeatedly, and any request for another large model evicts it. Do not set
`OLLAMA_MAX_LOADED_MODELS=1`: small helper models (embeddings, summaries) would evict the
large model on every call. One large model per node, as many small ones as fit.

Suggested systemd drop-in (`/etc/systemd/system/ollama.service.d/64k.conf`):

```
[Service]
Environment="OLLAMA_CONTEXT_LENGTH=65536"
Environment="OLLAMA_KEEP_ALIVE=-1"
```

then `systemctl daemon-reload && systemctl restart ollama`. A restart unloads models and
interrupts running sessions on that node, so do it between runs, then re-pin the model.

## 3. Other users of a node are part of the picture

On 2026-10-18 the pinned model was replaced by the base `qwen3.8:27b` (the daemon's own
`ollama.model` default), and a `Gemma3:12b` model stays loaded on the second node
(probably the hourly mail-rules daemon or a web UI; not confirmed). Plugins, integrated
services and scheduled jobs that use the same models are consumers like any other.

- **Accounting (BL472).** Every consumer of a tracked model reports into the usage and
  cost accounting with: consumer name and kind (PRD session, one-shot job, plugin,
  integrated service, helper such as the memory embedder or summariser), model, node,
  tokens in and out where known, wall-clock seconds, and whether it caused an eviction.
  The calculator shows PRD work and everything else separately, so build capacity is not
  overstated.
- **Scheduling (BL491, BL494).** Consumers that load a large model go through the same
  per-node queue and the pinned-model rules as PRD sessions, or are explicitly allowed
  to swap.

## 4. Where this lands

| Document or item | Change |
|---|---|
| BL449 matrix doc | 64k context is a precondition and a recorded axis |
| BL472 calculator and concurrency design 4c | other consumers tie into the accounting |
| BL483 automation policy, BL473 coordinator | minimum model context, pinned model per node, swap rule |
| Supervisor rules, sections 3 and 12 | the host settings and the shared-node rule |
| Host installation instructions (`docs/install-ollama-host.md`) | model host settings section |
