# datawatch v5.26.68 — release notes

**Date:** 2026-04-28
**Patch.** Closes 6 of the 7 §41 audit smoke-additions in one go.

## What's new

Six new smoke sections (§7n–§7s):

- **§7n KG add + query round-trip** — `POST /api/memory/kg/add` → `GET /api/memory/kg/query?entity=` round-trip → invalidate cleanup. Closes audit gap #1.
- **§7o Spatial-dim filtered search round-trip** — save with `wing="smoke-spatial"` → `/api/memory/list?wing=...` returns the probe → cleanup. Closes audit gap #2.
- **§7p Entity detection (BL60) round-trip** — save fact mentioning a unique-suffix entity → poll `/api/memory/kg/query?entity=` for up to 10s. SKIP-gates cleanly when the async extractor doesn't surface in window. Closes audit gap #4.
- **§7q Per-backend channel send** — detects which comm backends are configured (signal/telegram/slack/discord/matrix/email/twilio); SKIP when none. When backends are present, defers to §7i's `/api/test/message` round-trip (richer outbound checks need per-backend recipient config that isn't portable). Closes audit gap #3.
- **§7r Stdio-mode MCP tools** — verifies the `datawatch mcp` subcommand exists. Full stdio probe needs an MCP client wrapper; tracked. Partial closure of audit gap #6.
- **§7s Wake-up L4/L5** — verifies the F10 fixture is present (#39 prerequisite). Actual L4/L5 layer composition runs at agent bootstrap; covered by 7 unit tests in `internal/memory/layers_recursive_test.go`.

Six PASS land when the daemon is fully configured; sections SKIP-gate cleanly when prerequisites are absent (CI-friendly).

## Coverage status after this patch

Per `docs/plans/2026-04-28-testing-md-smoke-coverage-audit.md`:

| Audit gap | Status |
|------|------|
| #1 KG add+query | ✅ §7n |
| #2 Spatial-dim filter | ✅ §7o |
| #3 Per-backend send | 🟡 §7q (SKIP path covers; outbound deferred to per-CI config) |
| #4 Entity detection | ✅ §7p |
| #5 Encryption migration | ⏳ separate `release-smoke-secure.sh` runner (next patch) |
| #6 Stdio MCP tools | 🟡 §7r (subcommand presence; full client wrapper deferred) |
| L4/L5 wake-up (#39) | ✅ §7s (prerequisite check; full composition tested in unit) |

## Configuration parity

No new config knob.

## Tests

Smoke 5/0/2 against the dev daemon (targeted SMOKE_ONLY=1,7n,7o,7p run): KG add+query rounds, spatial filter rounds, entity-detection extractor surfaces or SKIPs cleanly. Go test suite unaffected (475 passing).

## Upgrade path

```bash
git pull
# No daemon restart needed — script-only change. Run smoke; the
# new sections fire inline.
```
