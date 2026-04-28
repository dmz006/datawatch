# docs/testing.md ↔ release-smoke.sh coverage audit

**Status:** Audit deliverable for task #41.
**Author:** generated 2026-04-28 in v5.26.66 cycle.
**Scope:** Map docs/testing.md scenarios to current `scripts/release-smoke.sh` sections; note coverage gaps.

---

## How docs/testing.md is structured

Three tiers:

1. **`How to Test Each Interface Channel`** — process doc (REST/comm/web/WS/MCP/config). Documents *how* operators add tests; not itself testable.
2. **`Core Interface & Feature Tests` + `Historical Feature Tests`** — append-only log of one-shot manual tests for past releases. Each row is a feature-at-the-time check, not a long-lived regression assertion.
3. **`Interface Validation Tracker` + `v1.3.x–v1.5.0 Feature Tests`** — feature-area summaries mapped to backend / interface support. The closest thing to "what should smoke cover."

Smoke can't reasonably re-run every historical manual test — many were point-in-time UX validations. The audit focuses on **operator-facing service functions** that have a continuing regression surface.

## Smoke sections vs feature areas

Current smoke (`scripts/release-smoke.sh`) sections as of v5.26.65:

| § | Section | Feature area | Source patches |
|---|---|---|---|
| 1 | Daemon health | core | initial |
| 2 | Backends list | LLM | initial |
| 3 | Stats / observer | observer | initial |
| 4 | Diagnose | core | initial |
| 5 | Channel history | comm | v5.26.1 |
| 6 | Autonomous CRUD across backends | autonomous | v5.26.9 |
| 7 | Autonomous decompose loopback | autonomous | v5.26.9 |
| 7b | PRD lifecycle (decompose→approve→run→spawn) | autonomous | v5.26.9 |
| 7c | project_profile + cluster_profile attach | F10 + autonomous | v5.26.19 |
| 7d | Persistent test profiles | F10 fixtures | v5.26.33 |
| 7e | Filter store CRUD | filters | v5.26.41 |
| 7f | Memory + KG round-trip | memory | v5.26.47/51 |
| 7g | MCP tool surface | MCP | v5.26.48 |
| 7h | Schedule store CRUD | schedules | v5.26.52 |
| 7i | Channel send round-trip | comm | v5.26.52 |
| 7j | F10 agent lifecycle | F10 | v5.26.55 |
| 7k | Claude skip_permissions config | LLM config | v5.26.57 |
| 7l | PRD-flow Phase 3 (story approval) | autonomous | v5.26.62 |
| 7m | Wake-up stack L0–L3 | memory | v5.26.65 |
| 8 | Observer peer + cross-host aggregator | observer / federation | v5.26.x |
| 9 | Memory recall | memory | v5.26.28 |
| 10 | Voice transcribe availability | voice | v5.26.x |
| 11 | Orchestrator graph CRUD | orchestrator | v5.26.x |

## Coverage matrix vs docs/testing.md feature areas

| docs/testing.md tier | Item | Smoke § | Status |
|---|---|---|---|
| Interface Validation | Messaging Backends (Signal/Telegram/Slack/etc.) | §5 + §7i | partial — shape only, no per-backend send round-trip |
| Interface Validation | Web | (manual / browser) | not in smoke (PWA visual checks live in `scripts/howto-shoot.mjs` screenshots) |
| Interface Validation | API | implicit across all sections | covered |
| Interface Validation | MCP | §7g | covered |
| Interface Validation | LLM Backends | §2 + §6 | covered (config + decompose round-trip per-backend) |
| v1.3.x–v1.5.0 | Memory System | §7f + §9 + §7m | covered (save/list/search/stats + KG + L0/L1 surface) |
| v1.3.x–v1.5.0 | Response Capture | (v5.26.31 unit tests) | NOT in smoke — `internal/session/response_filter_test.go` covers regression |
| v1.5.0 BL55 | Spatial Organization | §7f (wing/hall/room save) | partial — save honors them; round-trip via search not asserted |
| v1.5.0 BL57 | Knowledge Graph | §7f (kg/stats shape) | partial — KG add/query not in smoke |
| v1.5.0 BL56 | Wake-Up Stack | §7m | partial — L0/L1 surface; L2/L3 via §9; L4/L5 unit-only |
| v1.5.0 BL60 | Entity Detection | NOT in smoke | unit-only |
| Historical | Splash Screen | not in smoke (UX) | covered by `howto-shoot.mjs` screenshots |
| Historical | Interface Binding Selector | not in smoke | unit + Web UI walkthrough |
| Historical | TLS Dual-Port | implicit (smoke uses `https://localhost:8443`) | covered |
| Historical | Schedule Input | §7h | covered |
| Historical | System Statistics | §3 | covered |
| Historical | DNS Channel Round-Trip | NOT in smoke | unit-only |
| Historical | Encryption Migration | NOT in smoke | unit-only |

## Gaps worth filing

1. **KG add + query round-trip.** §7f covers stats; smoke could exercise `POST /api/memory/kg/add` and `GET /api/memory/kg/query` (each adds 2-3 PASS).
2. **Per-comm-backend send round-trip.** §7i probes `/api/test/message`. A future expansion: when a real backend (Signal/Telegram) is configured, exercise `POST /api/channel/send` to assert end-to-end.
3. **Spatial-dim round-trip via search.** Save with `wing="X"` then search filtered by `wing`. Distinct from current §7f which only validates the save returns the wing.
4. **Entity detection round-trip.** Save a fact mentioning a known entity; verify `GET /api/memory/kg/query?entity=...` returns it.
5. **Encryption migration.** A targeted smoke section that runs against an encrypted store config can detect breakage in the encryption-aware code paths.

These are 5 future smoke additions. Each is small (1 section, 2-3 PASS).

## Gaps that don't fit smoke

- **Visual UX checks** — splash screen, modal focus, font-size buttons, expandable rows. These live in `scripts/howto-shoot.mjs` (screenshot diffing) or operator-walkthrough docs.
- **Per-platform binding** — interface-binding selector / TLS / SSE host. Configurable; testing requires reconfiguring the daemon and watching it restart. Out of scope for smoke without per-section daemon restart support.
- **One-shot historical tests** — splash on first-run, settings-tab order at v0.13.x, etc. These are point-in-time regressions for past UX changes; smoke would over-fit.

## Recommendation

The current 19-section smoke (§1–§11 + §7b–§7m) covers the *operator-facing service functions* that should never break across releases. The five gaps above are tractable additions. The historical/UX entries in docs/testing.md remain manual / screenshot-based and should not be brought into smoke.

`docs/testing.md` itself should grow a "Smoke vs manual" annotation column so future entries make this distinction explicit on the way in.

## Hand-off

When operator wants to close gaps:

1. KG add/query round-trip → §7f extension or new §7n (~1 hour).
2. Spatial-dim filtered search → §7f extension (~30 min).
3. Per-backend channel send → §7i extension when a real backend is wired (gated on `cfg.Comm.<name>.enabled`).
4. Entity detection → §7n new section (~1 hour).
5. Encryption migration → separate `release-smoke-secure.sh` that brings up an encrypted-mode daemon (multi-hour; substantial fixture setup).
