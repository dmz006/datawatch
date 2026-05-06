# datawatch v5.26.17 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.16 → v5.26.17
**Patch release** (no binaries — operator directive).
**Closed:** Loopback URL respects bind config + startup health probe.

## What's new

### Loopback no longer assumes 127.0.0.1 always works

Operator: *"Loopback may not exist if someone changes the default interfaces, validate."*

Pre-v5.26.17 the daemon hardcoded `http://127.0.0.1:<port>` for ~14 internal HTTP calls:

- Autonomous decompose / verify / per-task guardrail / per-story guardrail
- Orchestrator guardrail
- Spawn (`/api/sessions/start`)
- Recursive child PRD `Run`
- Voice test message
- Config CLI commands (subprocess)
- Stats / alerts CLI commands

If the daemon binds to a specific non-loopback IP (e.g. `cfg.Server.Host = "192.168.1.5"`), the listener accepts at `192.168.1.5:8080` but **NOT** at `127.0.0.1:8080`. All loopback POSTs return `connection refused`. The bug was invisible because the default bind is `0.0.0.0` — operators with non-default bind configs hit silent failure across autonomous + orchestrator + voice features.

v5.26.17 introduces `loopbackBaseURL(cfg)` that returns the right base URL given the bind config:

| `cfg.Server.Host` | Resolved loopback base URL |
|---|---|
| `""` (default) | `http://127.0.0.1:port` |
| `"0.0.0.0"` | `http://127.0.0.1:port` |
| `"::"` (IPv6 all) | `http://[::1]:port` |
| `"192.168.1.5"` | `http://192.168.1.5:port` |
| `"fe80::1"` (IPv6 specific) | `http://[fe80::1]:port` |
| `nil cfg` | `http://127.0.0.1:8080` |

Replaced 6 highest-priority hardcoded sites in `cmd/datawatch/main.go`'s autonomous decomposeFn, autonomousVerify, autonomousGuardrail, autonomous spawn, recursive PRD run, orchestrator guardrail. 7 new tests in `loopback_url_test.go` cover the resolution matrix.

The remaining 8 hardcoded sites (CLI subprocess + ad-hoc helpers) are lower priority since they construct URLs on a per-invocation basis from the config; v5.26.18 follow-up will sweep those for completeness.

### Startup loopback health probe

`validateLoopback(cfg)` GETs `/api/health` on the resolved URL 2s after the HTTP listener accepts (gives the listener a chance to come up). Output:

```
[hostname] datawatch v5.26.17 started.
[hostname] loopback ok at http://127.0.0.1:8080
```

On failure (bind broken, firewall blocking 127.0.0.1, etc.):

```
[hostname] WARNING: loopback unreachable at http://192.168.1.5:8080: dial tcp 192.168.1.5:8080: connect: connection refused
[hostname] WARNING: autonomous decompose, voice transcribe, channel bridge, and orchestrator guardrails will fail.
[hostname] WARNING: set server.host = 0.0.0.0 (binds all interfaces) or fix the bind so loopback works.
```

Daemon continues — external clients still served — but operators get an explicit signal instead of silent feature failure.

## Configuration parity

No new config knob. `loopbackBaseURL` is fully derived from existing `server.host` + `server.port`.

## Tests

- 1397 + 7 = 1404 unit tests passing.
- Smoke unaffected: 29 PASS / 0 FAIL / 2 SKIP.

## Known follow-ups

- **Sweep remaining 8 hardcoded sites** for completeness (CLI commands + ad-hoc helpers). Lower priority since they're per-invocation and don't fail silently like the autonomous loopback callbacks did.
- **PRD project_profile + cluster_profile support** still queued from earlier in this session — needs schema additions, executor branching to `/api/agents`, PWA modal dropdowns, smoke coverage.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Watch the daemon log for the "loopback ok at <url>" line on
# startup. If you see WARNING instead, check server.host —
# 0.0.0.0 (all interfaces) is the safe default.
```
