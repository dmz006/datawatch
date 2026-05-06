# Release Notes — v6.11.3 (BL262 — rate-limit prompt detection)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.3

## Summary

BL262 — added new trigger phrases to the Claude rate-limit detector so the operator's reported prompt format is auto-detected and the session pauses correctly.

## Why

Operator caught a Claude prompt the existing detector missed:

> `You're out of extra usage · resets 11:50am (America/New_York)`

The existing `rateLimitPatterns` list (BL248, BL240) covered "usage limit reached", "limit will reset", "weekly usage limit", etc. — but not "out of extra usage" wording.

The time-extraction half (`parseClaudeClockTime` with the "resets " marker) already handled `resets 11:50am (America/New_York)` correctly from BL185 (v6.0.x). Only the trigger pattern needed updating.

## Changed

- **`internal/session/manager.go`** `rateLimitPatterns` — appended:
  - `"out of extra usage"` — the canonical phrase from the operator's report
  - `"you're out of"` — wider catch for related "you're out of credits/quota/usage" phrasings
- **`internal/session/ratelimit_parser_test.go`** — 2 new tests:
  - `TestParseRateLimitResetTime_BL262OutOfExtraUsage` — 4 prompt fixtures including the operator's exact wording, with `⎿` prefix variant, AM/PM variant, 24h-time variant
  - `TestRateLimitPatterns_BL262` — asserts the trigger phrases are in the `rateLimitPatterns` slice

## Tests

1765 pass (was 1763 + 2 new BL262 tests).

## What didn't change

- No surface changes (REST/MCP/CLI/comm/PWA/locale all unchanged).
- No new dependencies.
- Existing rate-limit handling (auto-pause + auto-resume + persistence across daemon restarts) flows through unchanged once the prompt is classified.
- Mobile parity: not needed — rate-limit handling is daemon-internal.

## Closes

BL262.

## See also

- CHANGELOG.md `[6.11.3]`
- BL262 entry in `docs/plans/README.md`
