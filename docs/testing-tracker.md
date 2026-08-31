# Testing Tracker

Two validation levels are required for every interface. See `AGENT.md` for the full rule.

- **Tested=Yes** — Go unit/integration tests exist and pass (`go test`).
- **Validated=Yes** — a live connection or end-to-end test confirmed the interface works with a real or simulated backend.

Do not mark **Validated=Yes** based solely on unit tests.

---

## Goose Backend

Added in v8.13.36–v8.14.0. Backends: `goose` (interactive TUI), `goose-prompt` (one-shot).

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `goose` TUI backend (`internal/llm/backends/goose/`) — named sessions, resume, version normalization | Yes | No | 22 unit tests in `internal/llm/backends/goose/backend_test.go`; cover `providerKeyEnvVar`, `shellQuote`, `gooseEnvPrefix`, all setters | Live test requires goose binary. DATAWATCH_COMPLETE detection path not yet validated with a real goose session. |
| `goose-prompt` one-shot backend — `goose run --text`, DATAWATCH_COMPLETE detection | Yes | No | Same test suite as above | Requires goose binary + provider API key. |
| Provider/model/API-key injection (T2) — `GOOSE_PROVIDER`, `GOOSE_MODEL`, `ANTHROPIC_API_KEY` etc. | Yes | No | Unit tests verify env var construction for all providers | Live inject test not yet performed. |
| MCP channel bridge (T3) — `GOOSE_MCP__DATAWATCH__*` env vars | Yes | No | Unit tests verify env var construction when `channel_enabled=true` | Requires goose binary with MCP support. |
| Config parity — all 6 GooseConfig fields via YAML/REST/MCP/CLI/PWA | Yes | No | TS-637–TS-643 in master-cookbook; config-reference.yaml, implementation.md, app.js all updated | PWA section present (Settings → LLM → Goose). Live GET/PUT config round-trip not yet performed end-to-end. |

---

## Vision Input System

Added in the current release cycle. Backends: ollama, openai, openai\_compat.

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `Describer` interface (`internal/vision/`) — `New()` + `Describe()` | Yes | No | `internal/vision/service_test.go` (httptest fake servers, 10 tests) | All three backends covered by unit tests. Live validation against a running ollama with llava or gpt-4o-mini not yet performed. |
| `POST /api/vision/describe` (multipart) | Yes | No | `internal/server/vision_test.go` (6 tests: 503/405/200/400/502/mime) | HTTP handler fully unit-tested via httptest. Live curl test with a real image not yet performed. |
| MCP `vision_describe` tool | No | No | — | MCP tool wires directly to the same HTTP handler; no standalone unit test. Requires a running daemon with vision enabled. |
| Router image injection (comms → `msg.Text`) | Yes | No | `internal/router/bl368_vision_test.go` (5 tests: CmdRemember injection, plain text, non-command regression) | Verifies `Parse()` still recognises `remember:` after image description is injected. Live test with an actual image attachment via SMS or Matrix not yet performed. |
| `AcceptsImages` manifest field (skills) | Yes | No | `internal/skills/manifest_test.go` (4 tests: true/false/default/no-extra-leak) | Manifest parsing verified. Live test with a skill that declares `accepts_images: true` receiving an image context not yet performed. |
| Council `image_path` field (`POST /api/council/run`) | No | No | — | Wired in `internal/server/council.go`; no dedicated unit test for the image injection path. Requires a running daemon with vision enabled and a valid local file path. |
