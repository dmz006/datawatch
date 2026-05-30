# Dual-Summary AI Pipeline

**Date**: 2026-05-30  
**Version at planning**: v8.8.15  
**Ships in**: v8.9.0

## Scope

- `internal/summarizer/summarizer.go` — dual-summary LLM call, model context query, parsers
- `internal/session/manager.go` — SessionSummary struct, SetDualSummarizer, triggerSummarize, pre-push inline
- `internal/session/store.go` — LastSummaryLong field on Session
- `internal/server/api.go` — SummarizerSvc interface, current-status + summarize endpoints
- `cmd/datawatch/main.go` — wiring
- `internal/server/web/app.js` — envelope expand, manual summarize button, fetchCurrentStatus

## Problem

Raw tmux capture is dense and unreadable on mobile/auto surfaces. The existing single-string
summarizer (v8.8.5) produced only a 3-sentence short summary. No long-form narrative was
available. No model-awareness for capture depth. No repetition avoidance. Brief sessions
(< 8 lines of output) still triggered LLM calls with meaningless content.

## Solution

### Single LLM call, dual output

Prompt uses `===SHORT===` / `===LONG===` markers to produce both summaries in one call:
- `===SHORT===` — 3 sentences ≤15 words, for push/auto/lock-screen
- `===LONG===` — 3–5 sentence narrative, for UI detail view

If the model doesn't follow the format, the full response becomes `short`; `long` is empty.

### Model-aware capture depth

Queries Ollama `/api/show` for `details.context_length` (or parses `num_ctx` from `parameters`).
Maps to history lines: <8K→100, 8–32K→200, 32–128K→400, 128K+→600. Non-Ollama defaults to 200.
Result cached per service lifetime.

### Repetition avoidance

Previous `short` summary for the session is prepended to the prompt as "Previously reported
(do not repeat this):" so the LLM avoids recycling the same sentences.

### Content gate

`triggerSummarize` and the inline pre-push path skip summarization when output has < 8
non-empty lines (< 7 newlines after trim). Prevents meaningless summaries from start/stop flips.

## API Changes

| Endpoint | Change |
|----------|--------|
| `GET /api/sessions` | `last_summary_long` added to session object |
| `GET /api/sessions/{id}/last-summary` | Returns `long_summary` field |
| `POST /api/sessions/{id}/summarize` | Returns `long_summary` field |
| `GET /api/sessions/{id}/current-status` | Returns `current_status_long` field |

## PWA Changes

- Envelope expand (▼/▲) on session cards for both waiting-input and running sessions
- "↻ Summary" button on waiting-input and completed cards for manual re-summarization
- `state.summaryLongExpanded` tracks expanded state per session
- `state.currentStatus` now carries `longText` and `longExpanded` fields

## Phases

- [x] Phase 1 — summarizer.go: SummarizeDual, ContextLines, queryOllamaContextLen, parseDualSummary
- [x] Phase 2 — store.go: LastSummaryLong field
- [x] Phase 3 — manager.go: SessionSummary.LongSummary, SetDualSummarizer, content gate, prev context
- [x] Phase 4 — main.go: wire SetDualSummarizer
- [x] Phase 5 — api.go: SummarizerSvc interface, endpoint updates
- [x] Phase 6 — app.js: envelope expand, manual summarize button
- [x] Phase 7 — CHANGELOG, plan doc

## Status: Done — shipped in v8.9.0
