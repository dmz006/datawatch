# datawatch v5.26.9 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.8 → v5.26.9
**Patch release** (no binaries — operator directive: every release until v6.0 is a patch).
**Closed:** Autonomous loopback fix (broken since v3.10.0) + `release-smoke.sh` + per-release smoke-test rule + scroll-mode redraw guard.

This is the most consequential patch since v5.0 — it fixes a 4-month-old latent bug in the autonomous + orchestrator loopback code paths.

## What's new

### The autonomous loopback was broken since v3.10.0 (now fixed)

Operator: *"I'm not sure claude can run autonomously, validate and make adjustments as needed to make it work."*

Validating end-to-end uncovered a multi-layer bug stack:

1. **HTTP→HTTPS redirect chain.** The autonomous decomposer / verifier / guardrail callbacks loopback-POST to `/api/ask` on the HTTP port (8080). The redirect handler 307'd to HTTPS (8443), where the daemon's self-signed cert tripped Go's HTTP client with x509. v5.18.0 added a redirect bypass for `/api/channel/*` paths but the autonomous loopback was never added.
   - **Fix:** extended `loopbackBypassPaths` to cover `/api/ask`, `/api/sessions`, `/api/sessions/`, `/api/orchestrator/`, `/api/autonomous/`. Exact-match for collection paths (e.g. `/api/ask` ≠ `/api/asksomething`); prefix-match for sub-paths (e.g. `/api/sessions/start`). Tests in `redirect_bypass_test.go` cover both bypass + deny-overshoot cases.
2. **Field-name mismatch.** The decomposer + verifier + per-task guardrail + orchestrator guardrail all built request bodies with `"prompt"` — but `/api/ask` decodes into `AskRequest{Question string \`json:"question"\`}`, so `Question` was always empty and every call returned `400 question required`. Bug shipped in v3.10.0 with BL24+BL25 and never tripped a test because nothing exercised the loopback end-to-end.
   - **Fix:** all four sites send `"question"`. The `effort` field (which `/api/ask` doesn't honor anyway) was dropped.
3. **Backend incompatibility.** `/api/ask` only supports `ollama` + `openwebui` as headless ask targets. When the operator created a PRD with `backend: claude-code` (the worker backend per BL203), the decomposer inherited that and got `400 unsupported backend: claude-code`.
   - **Fix:** new `askCompatible(b)` predicate. Decomposer resolution order: `amgrCfg.DecompositionBackend` → `req.Backend` (only if ask-compatible) → `"ollama"` default. Verifier + guardrails fall back to `ollama` when `VerificationBackend` / `GuardrailBackend` isn't ask-compatible. The PRD's `Backend` continues to drive the *worker* session (claude-code, opencode, etc.); the decomposer + verifier are decoupled and run on a headless backend.
4. **First-token timeout.** Cold ollama models (qwen3:8b+ on a busy host) regularly take > 60s to first token; `/api/ask` timed out half-way through decompose.
   - **Fix:** `askOllama` + `askOpenWebUI` HTTP client timeout bumped 60s → 300s.

End-to-end validation: created a PRD targeting `claude-code`, called `POST /api/autonomous/prds/{id}/decompose` — daemon transparently routed to ollama, decomposed into stories+tasks, returned 200. Smoke test #7 asserts this every release going forward.

### `scripts/release-smoke.sh` — every release runs this

Operator: *"This should have been caught with rules required functional testing. Make sure all releases functionally test everything and not just code test only" / "All releases should have smoke testing" / "If smoke tests create instances or configurations or data or PRDs, etc. They should be cleaned up when smoke tests are done."*

New `scripts/release-smoke.sh` runs against a live daemon and exercises every operator-facing surface:

1. `/api/health` + version
2. `/api/backends` shape
3. `/api/stats?v=2` observer roll-up
4. `/api/diagnose` battery
5. `/api/channel/history` shape (regression check for v5.26.1)
6. Autonomous PRD CRUD: create + `/children` + `set_llm` round-trip + hard-delete
7. **Autonomous decompose loopback** — explicit regression check for the v3.10.0 bug closed above
8. Observer peer register + push + cross-host aggregator (BL173 path)
9. Memory recall (skipped if disabled)
10. Voice transcribe availability (skipped if whisper disabled)
11. Orchestrator graph CRUD (skipped if disabled)

Cleanup: every PRD / peer / graph the smoke creates is registered with `add_cleanup` and removed via an `EXIT` trap that fires on success OR failure. Already-deleted resources are silently tolerated.

Initial run: **14 PASS / 0 FAIL / 2 SKIP** (memory + orchestrator skipped on this host).

`AGENT.md` § "Release testing" updated with the operator directive: every release — patch, minor, OR major — must run `release-smoke.sh` before tagging. PRs adding new operator-facing surface MUST extend the smoke to cover it before merge. Saved to memory as `feedback_per_release_smoke.md`.

### Channel history empty response now returns `[]` not `null`

Side-effect of the smoke run. `GET /api/channel/history?session_id=unknown` was returning `{"messages": null}` because `append([]T(nil), ...)` of an empty slice marshals as JSON null. v5.26.9 returns `{"messages": []}` for the empty case. PWA already handled both, but the smoke was strict.

### Scroll-mode redraw guard

Operator: *"When in scrolling mode the page keeps refreshing, should not do that, it should be in scroll mode."*

v5.24.0 added scroll-back protection to xterm pane redraws via `buf.viewportY >= buf.baseY`, but that check only catches the case where xterm itself has scrolled up. When the operator hits the **Scroll** button (which sends `Ctrl-b [` to put tmux in copy mode), tmux's pane content IS the scroll-back at the operator's position. Subsequent `pane_capture` frames would clobber that view because xterm was "at-bottom" of the captured frame.

v5.26.9: `pane_capture` handler now checks `state._scrollMode` first — if true, the redraw is skipped entirely until the operator exits scroll mode. The next pane_capture after exit catches up.

## Configuration parity

No new config knob.

## Tests

- 1397 Go unit tests passing (1395 baseline + 2 from v5.26.8 cascade-delete additions).
- New: `redirect_bypass_test.go` cases for the 5 added loopback bypass paths + deny-overshoot for `/api/asksomething`.
- **Functional smoke:** 14 PASS / 0 FAIL / 2 SKIP via `scripts/release-smoke.sh`.

## Known follow-ups

All operator-driven items closed. v6.0 packaging items unchanged (cumulative release notes, CI for `parent-full` + `agent-goose`, CI for `release-smoke.sh` against a kind cluster).

## Upgrade path

```bash
git pull
# Hard-refresh PWA tab once to pick up new SW cache + app.js.
# Restart the daemon: datawatch restart
# Try: create a PRD with backend=claude-code, click Decompose — it'll
# work for the first time since v3.10.0.
# Operators running their own smoke before tag:
#   ./scripts/release-smoke.sh   # against live daemon at localhost:8443
```
