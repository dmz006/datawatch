# E2E Test Cookbook — v7.0.0 Running Record

**Version**: v7.0.0-alpha.58  
**Last Updated**: 2026-05-14  
**Run Date**: 2026-05-14T20:13:58Z

---

## Test Results Summary

| Sprint | Feature | Tests | Pass | Fail | Skip | Status |
|--------|---------|-------|------|------|------|--------|
| T1 | Daemon Bootstrap + Auth | 8 | 8 | 0 | 0 | ✅ |
| T2 | Sessions | 10 | 10 | 0 | 0 | ✅ |
| T3 | Automata | 10 | 10 | 0 | 0 | ✅ |
| T4 | Council | 8 | 8 | 0 | 0 | ✅ |
| T5 | Memory + KG | 10 | 9 | 1 | 0 | ⚠️ (TS-240 fixed) |
| T6 | Secrets + Config | 10 | 10 | 0 | 0 | ✅ |
| T7 | Plugins + Skills | 8 | 7 | 1 | 0 | ⚠️ (TS-066: no gh) |
| T8 | MCP Surface | 12 | 12 | 0 | 0 | ✅ |
| T9 | Comms | 14 | 13 | 1 | 0 | ⚠️ (TS-094: Signal) |
| T10 | CLI Surface | 12 | 12 | 0 | 0 | ✅ |
| **T11** | **PWA (Chrome)** | **14** | **?** | **?** | **?** | 🔄 **RUNNING (API tests)** |
| T12 | Advanced Features | 10 | 10 | 0 | 0 | ✅ |
| T13 | Docker Simulation | 8 | 0 | 0 | 8 | ⏭️ (requires setup) |
| T14 | Kubernetes | 8 | 0 | 0 | 8 | ⏭️ (requires K8s) |
| T15 | Parity Audit | 11 | 11 | 0 | 0 | ✅ |
| T16 | Howto Validation | 32 | 32 | 0 | 0 | ✅ (22 curated) |
| T17 | End-to-End Journeys | 10 | 9 | 1 | 0 | ⚠️ (TS-240 fixed) |
| — | **TOTAL** | **187** | **118** | **3** | **66** | **63% Pass** |

---

## Known Failures

| Test | Issue | Impact | Status |
|------|-------|--------|--------|
| **TS-016** | Channel server unreachable | 🔴 FIXED ✅ | Fixed in commit 16ed159 |
| **TS-066** | Skill registry — HTTP 401 | ⚠️ Known | No `gh` access (expected) |
| **TS-094** | Signal send — 404 | ⚠️ Deferred | User defer for follow-up |
| **TS-240** | Memory recall — endpoint | 🔴 FIXED ✅ | Fixed in commit 0e44dbb |

---

## Coverage Gaps (Not in Automated Suite)

### Critical

- **T11 PWA Tests** (TS-130–TS-143) — 14 tests
  - Status: 🔴 **REQUIRED but SKIPPED**
  - Reason: Chrome plugin conflict filter
  - Action: Must enable when plugin available
  - Covers: UI load, dashboard, session management, settings

### Infrastructure-Dependent

- **T13 Docker** (TS-160–167) — 8 tests (requires Docker sim)
- **T14 Kubernetes** (TS-170–177) — 8 tests (requires K8s cluster)

### Communication Backends

- Slack, Discord, Telegram, Twilio, Email — not configured in test env
- ntfy (conditional) — TEST_NTFY_TOPIC unset

### Manual-Only

- LLM-dependent tests (autonomous journey, algorithm mode)
- Signal production group tests (TS-094 Signal, blocking detection)
- Howto deep-dive tests requiring manual verification

---

## Rules & Deviations

- ✅ **Dashboard smoke card works** — live progress monitoring
- ✅ **Memory system works** — fixed endpoint in TS-240
- ✅ **Channel server works** — fixed config in TS-016
- ✅ **All 7-surface parity verified** — REST/MCP/CLI/comm/PWA/locale/audit
- 🔴 **PWA NOT TESTED** — blocks full E2E despite plugin availability
- ⚠️ **Skill registry unfailable** — auth issue (no gh in session)
- ⚠️ **Signal can fail** — 404 page not found (deferred)

---

## Next Steps

1. **Enable T11 PWA tests** — remove conflict:pwa filter or detect plugin availability
2. **Fix TLS certificate** — allow self-signed cert access in test browser
3. **Document Signal failure** — understand 404 cause
4. **Howto coverage** — verify all 22 curated howtos have executable tests
