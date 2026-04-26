# How-to: Cross-agent memory

> **Status: stub.** Full walkthrough pending. In the meantime:
> the memory reference is at [`docs/memory.md`](../memory.md), the
> recall flow at
> [`docs/flow/memory-recall-flow.md`](../flow/memory-recall-flow.md),
> and the namespace + sharing rules in
> [`docs/api/memory.md`](../api/memory.md).

This how-to will cover: episodic memory between agents on the same
host, sharing project knowledge across builds + tests, federated
memory between peers (mutual-opt-in profiles), the wake-up stack
that auto-injects identity + critical facts at session start, and
practical examples (cross-build context, regression-test memory).
Track at [BL190](../plans/README.md).
