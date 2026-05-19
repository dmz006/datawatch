# Test Plan — v8.2.0

**Version**: v8.2.0  
**Sprint**: T40 — Android 1.0.0 blockers + settings UX  
**Stories**: TS-637–TS-682 (46 stories)  
**Go unit tests**: 479 (378 server + 94 autonomous + 7 federation push cap)

## Scope

| BL | Feature | Stories | Go tests |
|---|---|---|---|
| BL327 | Badge/chip multi-select | TS-637–644 (PWA only) | — |
| BL328 | Async PRD decompose (SSE) | TS-645–657 | TS-645–648 (in autonomous_decompose_test.go) |
| BL329 | Identity POST alias | TS-658–664 | — |
| BL330 | UnifiedPush register/unregister/notify | TS-665–673 | federation_push_cap_test.go (7 tests) |
| App#133–135 | Android parity verification | TS-674–682 | — |

## New Go tests in v8.2.0

| File | Tests | What they cover |
|---|---|---|
| `internal/server/autonomous_decompose_test.go` | 4 | TS-645 (202), TS-646 (SSE events), TS-647 (status poll), TS-648 (Last-Event-ID replay) |
| `internal/server/federation_push_cap_test.go` | 7 | Push cap enforcement: CapCommRead/Write on all push endpoints; CapAutonomousWrite/Read on decompose |

## Federation cap enforcement verified

All BL330 push endpoints now use the `comm:*` capability surface (not `observers:*`).  
Decompose endpoints use `autonomous:*` (unchanged from BL328 implementation).  
Identity endpoints use `config:*` (unchanged from BL329 implementation).

See [cookbook.md](cookbook.md) § Federation Access Matrix for the full endpoint→cap table.

## Run

```sh
# Go unit tests
go test ./internal/server/... ./internal/autonomous/... -timeout 90s

# E2E against a live daemon
# See cookbook.md for full REST/CLI/MCP/PWA story scripts
```
