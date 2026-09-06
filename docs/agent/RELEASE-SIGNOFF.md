# Release Sign-Off: vX.Y.Z

> Copy this template into the release commit body. Fill every row.
> Write `N/A — <one-line reason>` for inapplicable items.
> A missing row means the sprint is not done.

---

## Section A — Every commit/push

| # | Check | Result |
|---|---|---|
| A1 | AGENT.md rules fired | `rules: ` |
| A2 | `go test ./...` passes | `tests: ok` |
| A3 | Version in BOTH files (`main.go` + `api.go`) | `version: vX.Y.Z (both files)` |
| A4 | `CHANGELOG.md` entry added | `changelog: added` |
| A5 | `README.md` current-release line updated | `readme: updated` |
| A6 | `docs/plans/README.md` backlog refactored | `backlog: refactored` |
| A7 | No B/BL/F IDs in user-facing docs (grep clean) | `id-check: clean` |
| A8 | No local-env leaks in diff (grep clean) | `leak-check: clean` |
| A9 | `node --check app.js` passes | `node-check: ok` |
| A10 | `make build` clean | `make-build: ok` |
| A11 | CI runner check (`gh run list --limit 20`) | `ci: clean` |

---

## Section B — Conditional (N/A if trigger didn't fire)

| # | Trigger | Result |
|---|---|---|
| B1 | Changed endpoint contract | `tracker: ` |
| B2 | New UI string | `locales: ` |
| B3 | New high-visibility locale key | `locale-guard: ` |
| B4 | Operator-visible PWA change | `mobile-parity: ` |
| B5 | Internal bug fixed | `plans: ` |
| B6 | New/changed config field | `config-parity: ` |
| B7 | New feature — observability | `observability: ` |
| B8 | New feature — access-method docs | `access-docs: ` |
| B9 | Reuse-and-Expand audit | `reuse-audit: ` |
| B10 | New session/PRD/agent/plugin path | `skills-hook: ` |
| B11 | New smoke entity type | `smoke-cleanup: ` |
| B12 | New operator-facing endpoint | `smoke-extended: ` |
| B13 | Multi-step project | `cookbook: ` |
| B14 | Security finding patched | `sec-downstream: ` |
| B15 | New backend | `secrets-store: ` |
| B16 | New audit event emitter | `audit-tests: ` |
| B17 | New Go dependency | `deps: ` |

---

## Section C — Release testing

| # | Check | Result |
|---|---|---|
| C1 | Dependency audit | `dep-audit: ` |
| C2 | gosec scan | `gosec: ` |
| C3 | Smoke run | `smoke: ` |

---

## Section D — GH release (minor/major only)

| # | Check | Result |
|---|---|---|
| D1 | `make cross` + 5 binary assets | `make-cross: ok` |
| D2 | Container maintenance audit | `containers: audited` |
| D3 | Asset retention cleanup | `asset-cleanup: ok` |

---

## Section E — Major release only (X.0.0)

| # | Check | Result |
|---|---|---|
| E1 | LLM alias refresh | done / N/A |
| E2 | PAI compatibility audit | done / N/A |
| E3 | Full test suite (Docker + k8s + cross-feature + UI) | done / N/A |
