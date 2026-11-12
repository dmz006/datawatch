# Automata tags — final gate results (2026-10-23)

Gate run on branch `automaton/dbd50009`; no failures found, no code changes needed.

## Static and unit gates

| Gate | Result |
|------|--------|
| `go build -o <tmp> ./cmd/datawatch/` | pass |
| `go vet` autonomous, server, mcp, router | pass |
| `go test` autonomous, server, mcp, router | 4110 passed |
| `node --test internal/server/web/*.test.js` | 718 passed, 0 failed |
| `scripts/check-howto-coverage.sh` | pass |

## Live round trip

Test daemon on `127.0.0.1:18777`, temp data dir, no TLS, MCP off (production daemon untouched).

- Three-level tree created: Work > Backend > Api; second root Personal > Home.
- One Automaton tagged Api + Home (different parents): listed under both via `GET /api/autonomous/prds?tag=`.
- `include_sub=true` on Work lists it; `include_sub=false` on Work lists none; Personal with sub lists it.
- Three Automata assigned Api + Home: tag counts `include_sub=false` Api 3, Home 3, others 0;
  `include_sub=true` every ancestor rolls up to 3.
- Delete Work (has children): 409 refused.
- Delete Api (assigned): 409 refused with `count: 3`.
- Reparent Work under Api: 409 refused as a cycle.
- Daemon stopped and restarted on the same data dir: all five tags, parent links and the
  three Automata's tag assignments persisted (`autonomous/tags.jsonl`).
- Test daemon stopped; no test processes remain.
