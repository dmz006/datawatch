# datawatch v5.21.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.20.0 → v5.21.0
**Closed:** Observer + whisper config-parity sweep — audit follow-up #2

## Why this release

Same pattern as v5.17.0 (autonomous knobs), but for the observer
subsystem and the missing whisper HTTP-shape fields.

Pre-v5.21.0:

- **Every `observer.*` key silently no-op'd through `PUT /api/config`** —
  `applyConfigPatch` had zero observer cases. `datawatch config set
  observer.tick_interval_ms 2000` returned 200 with no effect.
- **`observer.conn_correlator` (BL293, v5.6.1) wasn't reachable** from
  YAML or REST because `internal/config.ObserverConfig` didn't have
  the field; only `internal/observer.Config` did. The opt-in flag
  for the BL180 Phase 2 procfs correlator was effectively
  always-default-false on dev workstations regardless of YAML.
- **`observer.peers.*` (BL172, v4.5.0) — same pattern.** No
  `internal/config.ObserverPeersConfig`, no patch cases.
- **`whisper.backend / endpoint / api_key`** weren't in the
  `applyConfigPatch` set. v5.8.0 BL201 closed the daemon-side
  inheritance, but the operator-facing surface for setting these
  three keys directly silently no-op'd.

## What changed

### `internal/config/ObserverConfig`

Added two fields:

```go
ConnCorrelator bool                `yaml:"conn_correlator,omitempty"`
Peers          ObserverPeersConfig `yaml:"peers,omitempty"`
```

New `ObserverPeersConfig` struct with `AllowRegister`,
`TokenRotationGraceS`, `PushIntervalSeconds`, `ListenAddr`.

### `cmd/datawatch/main.go` observer-Manager bridge

Copies `ConnCorrelator` + `Peers` from `cfg.Observer` into
`observerpkg.Config` so the observer Collector picks up the YAML
values at startup.

### `internal/server/api.go.applyConfigPatch`

20 new cases:

- Observer scalars: `tick_interval_ms`, `top_n_broadcast`,
  `include_kthreads`, `ebpf_enabled`, `conn_correlator`
- Observer pointer-bools: `plugin_enabled`, `process_tree_enabled`,
  `session_attribution`, `backend_attribution`, `docker_discovery`,
  `gpu_attribution`
- Observer federation: `parent_url`, `peer_name`,
  `push_interval_seconds`, `token_path`, `insecure`
- Observer ollama_tap: `endpoint`
- Observer peers: `allow_register`, `token_ttl_rotation_grace_s`,
  `push_interval_seconds`, `listen_addr`
- Whisper HTTP fields: `backend`, `endpoint`, `api_key`

### Tests

6 new tests in `internal/server/observer_whisper_patch_test.go`:

- scalars (5 keys)
- pointer-bools (6 keys including `*bool` deref correctness)
- federation (5 keys)
- peers (4 keys)
- ollama_tap (1 key)
- whisper HTTP (3 keys)

Total: **1382 passed** in 58 packages (1376 → 1382).

## Known follow-ups

Per the audit doc:

- **v5.22.0** — observability fill-in (stats metrics + Prom metrics).
- **v5.22.x patches** — datawatch-app#10; container parent-full retag; gosec HIGH-severity review.

## Upgrade path

```bash
datawatch update                                        # check + install
datawatch restart                                       # apply

# Round-trip the new keys:
datawatch config set observer.conn_correlator true       # opt-in BL293
datawatch config set observer.peers.allow_register true
datawatch config set whisper.backend openwebui
grep -A6 '^observer:' ~/.datawatch/config.yaml | head -20
```
