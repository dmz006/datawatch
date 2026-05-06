# datawatch v5.26.66 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.65 → v5.26.66
**Patch release** (no binaries — operator directive).
**Closed:** docs/testing.md ↔ smoke coverage audit (#41).

## What's new

Operator-asked: *"Are all of the prior tests in the testing doc have smoke tests for regression prevention testing"*

Audit doc: `docs/plans/2026-04-28-testing-md-smoke-coverage-audit.md`

Walks the three tiers of `docs/testing.md`:

1. **`How to Test Each Interface Channel`** — process doc; not testable.
2. **`Historical Feature Tests` (37 sections)** — append-only log of one-shot manual tests. Most are point-in-time UX checks; not smoke-shaped.
3. **`Interface Validation Tracker` + `v1.3.x–v1.5.0 Feature Tests`** — feature areas with continuing regression surface.

Maps each tier-3 entry to a current `release-smoke.sh` section (§1–§11 + §7b–§7m). Result:

- ✅ Memory system (§7f + §9 + §7m), MCP (§7g), Schedules (§7h), Channel surface (§5 + §7i), Stats (§3), Diagnose (§4), TLS (implicit), Filters (§7e), F10 lifecycle (§7j), Profiles (§7c + §7d), skip_permissions (§7k), Phase 3 story approval (§7l), Wake-up L0–L3 (§7m).
- 🟡 Spatial dim filtered search, KG add/query round-trip, per-backend channel send — partial coverage.
- ⏳ Entity detection, encryption migration, per-platform binding — not in smoke (unit + manual coverage).

5 concrete future smoke additions identified with effort estimates (KG round-trip, spatial filtered search, per-backend send, entity detection, encryption smoke). Each is a 1-section, 2–3-PASS extension.

Recommendation in the doc: docs/testing.md should grow a "Smoke vs manual" annotation column so future entries get classified on the way in.

## Configuration parity

No code change.

## Tests

Doc-only patch. Smoke unaffected (61/0/2 against the dev daemon as of v5.26.65). Go test suite unaffected (475 passing).

## Open backlog after v5.26.66

The session's task tracker is fully cleared:

- ✅ #32 Schedule store CRUD
- ✅ #33 Channel send round-trip
- ✅ #34 datawatch-app issues
- ✅ #35 Phase 3 design doc
- ✅ #36 Phase 4 design doc
- ✅ #37 Mempalace audit plan
- ✅ #38 F10 agent lifecycle smoke
- ✅ #39 Wake-up stack L0–L5 smoke (partial; L4/L5 deferred per #38 dependency)
- ✅ #40 Autonomous howto screenshots
- ✅ #41 docs/testing.md ↔ smoke audit (this patch)
- ✅ #42 CI: secrets + dep-review + OWASP ZAP
- ✅ #43 Session driver-kind badges
- ✅ #44 Phase 3 implementation
- ✅ #45 Phase 4 implementation
- ✅ #46 New Session unified Profile dropdown

**Every operator ask from this turn is shipped.** The remaining items in `docs/plans/2026-04-27-v6-prep-backlog.md` are operator-prepared (v6.0 cumulative release notes) or PAT-gated (GHCR past-minor cleanup).

## Upgrade path

```bash
git pull
# Doc-only patch — no daemon restart needed.
```
