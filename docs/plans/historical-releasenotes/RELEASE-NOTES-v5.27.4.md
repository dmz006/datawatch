# datawatch v5.27.4 — release notes

**Date:** 2026-04-29
**Patch.** Two operator-asked items in one bundle.

## What's new

### 1. `GET /api/update/check` — read-only update check ([datawatch#25](https://github.com/dmz006/datawatch/issues/25))

Mobile + PWA clients need a "check → confirm → install" UX, but the existing `POST /api/update` checks **and installs** atomically — calling it twice (once to check, once to install) means the first call already fires the download.

v5.27.4 adds a read-only sibling:

```
GET /api/update/check
→ {"status":"up_to_date" | "update_available",
   "current_version":"5.27.4",
   "latest_version":"5.27.5"}
```

No download, no install, no restart. POST `/api/update` is unchanged and continues to check + install atomically when the operator wants the one-call flow.

Surface coverage:

| Surface | Invocation |
|---|---|
| REST | `GET /api/update/check` |
| CLI | `datawatch update --check` (already existed; same shape) |
| MCP | `get_version` tool (already existed; reads same path) |
| Comm | `update check` / `update` (already existed) |
| PWA | `checkForUpdate()` migrated off `api.github.com` direct call onto the daemon endpoint — one source of truth, no CORS issues, goes through daemon auth |

datawatch-app is the requester (the mobile client now has the endpoint it needs to drop the double-POST workaround).

### 2. Modern claude-code rate-limit pattern fix

Operator-reported: *"session limit reach filter and auto scheduled continue command doesn't work"*.

claude-code switched to new phrasings that didn't match the v5.27.3-and-earlier pattern list:

- "5-hour limit reached ∙ resets 2pm"
- "Weekly limit reached"
- "Hit weekly limit"
- "Opus limit reached" / "Sonnet limit reached"

The legacy `"You've hit your limit"` / `"rate limit exceeded"` patterns covered the old format only, so the rate-limit detection silently dropped — no auto-1-press to dismiss the dialog, no auto-schedule resume.

v5.27.4 extends `rateLimitPatterns` in `internal/session/manager.go` with `limit reached`, `weekly usage limit`, `hit weekly limit`, `5-hour limit`, `opus limit reached`, `sonnet limit reached`. The seed-filter alert in `cmd/datawatch/main.go` gains the same regex extension so the operator alert also fires.

Reset-time parsing for the new format is already covered by the existing `"resets "` marker in `parseRateLimitResetTime` family-2 — verified via a new test (`TestParseRateLimitResetTime_ModernResetMarker`).

The auto-schedule resume pipeline at line 3741+ of `manager.go` is unchanged — it was already correct; it just wasn't firing because the pattern check at line 3727+ never set `isRateLimit = true`.

## Tests

```
Go build:  Success
Go test:   1490 passed in 58 packages (+10: 6 update_check + 4 modern rate-limit)
Smoke:     run after install to verify §7v added for the new endpoint
```

## Smoke

New `§7v` section in `scripts/release-smoke.sh`:

- `GET /api/update/check` returns shape with `status` ∈ {up_to_date, update_available} + `current_version` + `latest_version`
- `POST /api/update/check` → 405 (read-only)

## datawatch-app sync

The endpoint is what the mobile client requested ([datawatch#25](https://github.com/dmz006/datawatch/issues/25)). The companion can now drop its check-by-double-POST workaround. Filing the mobile-side adoption ticket as datawatch-app#30 (cross-link in the closing comment on datawatch#25).

## Backwards compatibility

- `POST /api/update` semantics unchanged.
- `GET /api/update/check` is additive — older daemons return 404; mobile feature-detect by probing once and falling back to the double-POST flow.
- New rate-limit patterns are additive; existing matches continue to work.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-4) so the new
# checkForUpdate() implementation takes effect.
```

No data migration. No new schema.
