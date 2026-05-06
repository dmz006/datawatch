# datawatch v5.26.65 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.64 → v5.26.65
**Patch release** (no binaries — operator directive).
**Closed:** §7m wake-up stack L0–L3 surface checks (#39 partial — what smoke can probe without a spawned-agent fixture).

## What's new

Operator-asked: *"Wake-up stack layer probes (L0–L5)"* (task #39).

The wake-up layers (`internal/memory/layers.go` / `layers_recursive.go`) compose at agent bootstrap time and don't have a direct REST endpoint. v5.26.65 ships a partial smoke (§7m) that probes the underlying surfaces a regression in the layer-composer would also break:

```
== 7m. Wake-up stack L0–L3 surface checks ==
  PASS  L0: identity.txt present at <data_dir>/identity.txt
  PASS  L1 source: memory subsystem reachable (validated by §7f / §9)
```

Coverage:

| Layer | Probe | Source |
|------|------|------|
| **L0** | `<data_dir>/identity.txt` exists (operator-set or empty) | filesystem |
| **L1** | memory subsystem enabled | `/api/memory/stats` (existing §7f) |
| **L2** | spatial-dim search | `/api/memory/search` (existing §9) |
| **L3** | deep search | `/api/memory/search` (existing §9) |
| **L4** | parent context | needs spawned-agent fixture |
| **L5** | sibling visibility | needs spawned-agent fixture |

L4/L5 still need the F10 fixture wiring. Full L0–L5 round-trip lives in `internal/memory/layers_recursive_test.go` Go unit tests (7 cases) — those run on every PR.

### What this is NOT

A full operator-facing wake-up endpoint that returns the composed L0+L1+L2 string. That'd be an additive feature, not a smoke probe — tracked as a follow-up if operators want to inspect the wake-up bundle directly via REST/MCP.

## Configuration parity

No new config knob.

## Tests

Smoke unaffected by section-count: §7m adds 2 PASS to a full run. Total: 61/0/2 against the dev daemon (was 59/0/2). Go test suite unaffected (475 passing).

## Known follow-ups

- **#41** docs/testing.md ↔ smoke coverage audit
- L4/L5 smoke probes (need F10 spawned-agent fixture)
- Optional: `/api/memory/wakeup` REST endpoint that composes L0+L1+L2 for a given project_dir, so smoke can probe the layer composer end-to-end

## Upgrade path

```bash
git pull
# No daemon restart needed — script-only change. Run smoke;
# §7m fires inline.
```
