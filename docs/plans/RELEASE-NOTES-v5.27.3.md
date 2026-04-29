# datawatch v5.27.3 — release notes

**Date:** 2026-04-29
**Patch.** Hotfix on top of v5.27.2.

## What's fixed

### Chat-channel `reload [subsystem]` was returning "Reload not wired by this build" in v5.27.2

The v5.27.2 commit wired `SetReloadFn` on the production comm-router (Signal / Telegram / Discord / Matrix) but missed the **`testRouter`** that backs `POST /api/test/message`. The test router is the one the smoke script + the operator's PWA "test message" affordance hits, so:

- `curl -X POST /api/test/message {"text":"reload filters"}` → response `"Reload not wired by this build."`
- Real Signal/Telegram routers worked correctly.

Caught during the post-v5.27.2 functional test pass; fixed by wiring `SetReloadFn` on the test router symmetrically. Verified via §7u smoke section + the same end-to-end test now passes:

```
curl -X POST -d '{"text":"reload filters"}' /api/test/message
→ {"count":1,"responses":["[ralfthewise] reload filters: filters"]}
```

### Refactor — `claudeDisclaimerResponse` extracted as pure helper

The disclaimer pattern-classifier inside the `DetectPrompt` callback is now `cmd/datawatch.claudeDisclaimerResponse(line) string`. **No behaviour change** — same matrix of "trust this folder" / "Quick safety check" → `1\n` and "Loading development channels" / "I am using this for local development" → `\n`. The extract makes the matrix unit-testable without spinning up a session manager (4 new test cases in `cmd/datawatch/v5272_claude_disclaimer_test.go`).

## What's new

Tests + smoke supplement (would normally have shipped with v5.27.2 if the wire-up bug hadn't been caught after publish):

- 9 new Go tests (`cmd/datawatch/v5272_claude_disclaimer_test.go` + `internal/server/v5272_reload_subsystem_test.go`) covering both v5.27.2 features.
- New smoke section §7u: REST reload (full + subsystem + bogus error), chat-channel `reload filters`, PUT/GET round-trip on `session.claude_auto_accept_disclaimer`.
- Doc-sweep fixes: `docs/memory.md` v6.0.0 mempalace heading → v5.27.0; dead `RELEASE-NOTES-v6.0.0.md` cross-link → `RELEASE-NOTES-v5.27.0.md`. `docs/testing.md` BL113 entry pointed at the now-redirected `docs/install.md`; updated to `docs/howto/setup-and-install.md`.

## Tests

```
Go build:  Success (via `make build` — embedded docs synced)
Go test:   1480 passed in 58 packages
Smoke:     77 pass / 0 fail / 4 skip (was 72/0/4 in v5.27.2)
```

## Process notes

The v5.27.2 commit included a non-trivial `cmd/datawatch/main.go` change but was committed without a version bump (annotated as a "test/doc supplement"). That was a process violation — code-on-main must bump version + tag + cut a GH release per AGENT.md § Versioning + § Release vs Patch Discipline. v5.27.3 corrects that: same code on main, but now properly tagged + binaries published.

Lesson recorded for the next session: **any `cmd/`/`internal/` source change merits a version bump even when bundled with tests + docs.**

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-3).
```

If you were already on v5.27.2 and hit the chat-reload "not wired" error, this patch fixes it. No data migration. Same behaviour for REST/CLI/MCP — those weren't affected.
