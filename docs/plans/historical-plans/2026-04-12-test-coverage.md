# B7: Code Test Coverage Plan

**Date:** 2026-04-12
**Priority:** high
**Current coverage:** 12.6% overall (was 11.2%)

---

## Current Status

| Package | Coverage | Tests | Notes |
|---------|----------|-------|-------|
| llm (registry) | **100%** | 3 | Complete |
| alerts | **86.4%** | 11 | New — store CRUD, persistence, encryption, listeners |
| dns (messaging) | 83.0% | 11 | Existing |
| tlsutil | **80.0%** | 5 | New — auto-generate, custom cert, SANs |
| transcribe | 66.7% | 5 | Existing |
| proxy | 65.8% | 14 | Existing |
| secfile | 51.8% | 10 | Existing |
| metrics | **50.0%** | 1 | New — handler test |
| memory | 48.3% | 45 | Existing |
| pipeline | 43.4% | tests | Existing |
| stats | **34.0%** | 6 | New — collect, setters, channel counters |
| claudecode | 26.1% | 6 | Existing |
| rtk | 13.8% | 3 | Existing |
| config | 10.6% | 13 | Existing |
| session | 9.8% | 29 | Existing |
| openwebui | 5.7% | 5 | Existing |
| router | 3.9% | 17 | Existing |
| cmd/datawatch | 0.7% | 6 | Existing |
| **16 packages** | **0.0%** | 0 | No tests |

**Total: 12.6% (228 tests across 40 packages)**

---

## Why 100% is hard for this project

Many packages require external services that can't be easily mocked:

| Package | External dependency | Why hard to test |
|---------|-------------------|-----------------|
| server | HTTP server, WebSocket, tmux | Needs running daemon, browser |
| mcp | MCP SDK, stdio/SSE transport | Needs MCP client connection |
| signal | signal-cli Java process | Needs real Signal account |
| telegram | Telegram Bot API | Needs real bot token |
| slack/discord/matrix | Platform APIs | Need real accounts |
| ollama | Ollama server | Need running model server |
| opencode | OpenCode binary | Need installed binary |
| channel | Node.js MCP channel | Need Node.js runtime |

## Realistic target: 40-50% overall

### Phase 1: Done — easy wins (12.6%)
alerts (86%), tlsutil (80%), metrics (50%), stats (34%), llm (100%)

### Phase 2: Medium effort — improve existing low-coverage packages
- **config** (10.6% → 60%): more config parsing, defaults, per-backend overrides
- **session** (9.8% → 40%): store CRUD, state transitions, TailOutput, backend state
- **router** (3.9% → 30%): command parsing (already good), handler mock tests
- **pipeline** (43.4% → 70%): executor, cycle detection, parse spec
- **rtk** (13.8% → 50%): version check mock, update binary mock

### Phase 3: Hard — requires mocking/integration
- **server** (0% → 20%): httptest server for API endpoints
- **mcp** (0% → 15%): mock MCP tool handlers
- **messaging backends** (0%): require platform credentials, test only parsers/formatters

### Phase 4: Functional tests (non-Go)
These are documented in testing.md and validated manually:
- API endpoint tests via curl
- WebSocket tests via Python
- Comm channel tests via /api/test/message
- Browser tests via Chrome automation
- Pipeline tests via API
- Memory search/recall tests

---

## Test count by category

| Category | Count |
|----------|-------|
| Unit tests (Go) | 228 |
| API functional tests | 13 (documented in testing.md) |
| Profile CRUD tests | 6 (documented in testing.md) |
| Browser/WS tests | ~10 (manual, documented) |
| Pre-release checklist | 10 items |
| **Total** | **~267** |
