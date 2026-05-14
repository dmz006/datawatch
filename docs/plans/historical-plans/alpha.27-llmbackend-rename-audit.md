# alpha.27 — Session.LLMBackend → LLMRef hard-rename audit

**Date:** 2026-05-10
**Status:** Audit (pre-implementation)
**Operator directive (2026-05-09):** "go with 2 (hard-rename) but make sure all scripts, smoke tests and anything we have internally for datawatch (and datawatch-app) is fully and completely updated, may need to audit each and make detailed plan to make sure nothing is missed"

## Current state

`internal/session/store.go:41-46` shows the dual fields:

```go
LLMBackend  string `json:"llm_backend,omitempty"` // legacy v6 — kept for backward compat; v7 prefers LLMRef
LLMRef      string `json:"llm_ref,omitempty"`     // v7.0.0-alpha.21
```

`LLMRef` was introduced alpha.21 and already populates from `LLMBackend` in many code paths. Hard-rename = remove `LLMBackend` field + drop `llm_backend` JSON tag + migrate any reader still using it.

## Scope (grep-derived)

| Surface | LLMBackend hits | llm_backend hits |
|---|---:|---:|
| Go files | 91 | 26 |
| Files affected | 16 | — |
| scripts/ | 1 | 1 |
| docs/ | — | 25 files |
| CHANGELOG.md | 7 | (historical, preserve) |
| Locale × 5 | "Backend" labels | — |

## File-level inventory

### Go — code change required

| File | What's there | Action |
|---|---|---|
| `internal/session/store.go` | Field declaration | **Remove `LLMBackend` field**. Add migration in `(s *Session) UnmarshalJSON` (or load path) to populate `LLMRef` from legacy `llm_backend` JSON before drop. |
| `internal/session/manager.go` | Field reads/writes | Replace all `sess.LLMBackend` reads with `sess.LLMRef`. Writes: replace setter calls. |
| `internal/session/tracker.go` | Field reads | Same. |
| `internal/session/cost.go` | Field reads | Same. |
| `internal/session/bl6_cost_test.go` | Test fixtures | Update to use LLMRef. |
| `internal/llm/backend.go` | Backend resolution | Field name unchanged (this is `Backend`, not `Session.LLMBackend`). **Verify no overlap.** |
| `internal/autonomous/executor.go` | PRD execution | Replace reads with LLMRef. |
| `internal/autonomous/models.go` | Model definitions | Replace if `LLMBackend` field is exposed. |
| `internal/router/router.go` | comm verb routing | Update comm verb handlers that set `body["llm_backend"]` to use `llm_ref`. |
| `internal/config/config.go` | Config struct | Verify no `LLMBackend` config key (likely just SessionConfig). |
| `internal/config/config_test.go` | Tests | Update fixtures. |
| `internal/config/template.go` | Template defaults | Update sample. |
| `internal/wizard/defs.go` | Wizard schema | Replace if exposed in wizard fields. |
| `internal/server/api.go` | REST handlers | All `?llm_backend=` query params + body fields → `llm_ref`. |
| `internal/server/backends_active.go` | Active-backend logic | Verify naming (this is "active backend", may stay). |
| `internal/server/cost.go` | Cost computation | Replace reads. |
| `internal/server/bl6_cost_test.go` | Tests | Update fixtures. |
| `internal/server/web/openapi.yaml` | API schema | Replace `llm_backend` field/param with `llm_ref` in all session endpoints. |
| `internal/mcp/autonomous.go` | MCP autonomous tools | Update `llm_backend` parameter to `llm_ref`. |
| `cmd/datawatch/main.go` | Daemon startup / migration | Add migration sweep: at startup, walk session store, copy `LLMBackend` → `LLMRef` for any session still missing LLMRef. Persist. |

### Locale × 5

Each bundle (`en/fr/de/es/ja.json`) has session-related strings using "Backend" label. Need to scan for `session_backend_*`, `llm_backend_*`, similar keys. Action: rename keys to `*_llm_ref_*` if any, OR keep keys but update the *value* string to "LLM" (since `llm_backend` was the user-visible label "Backend").

### Scripts

- `scripts/release-smoke.sh` — 2 hits. Update query params.
- `docker/entrypoint.sh` — 1 file flagged. Inspect + update if it sets a default.

### Docs (25 files)

- `docs/design.md` — architecture refs.
- `docs/howto/*.md` — operator-facing references.
- `docs/plans/historical-*` — DO NOT REWRITE (historical accuracy).
- `docs/plans/2026-*.md` — current plans, update if active.
- Action: grep + targeted edit on **active** docs only; historical-plans/ stays.

### CHANGELOG

Historical entries (v6.x, alpha.0-alpha.26) MUST stay as written. New `## [7.0.0-alpha.27]` entry documents the rename + migration logic.

### datawatch-app cross-references

This repo references `datawatch-app` in:
- `internal/server/web/docs/plans/...` plan docs that mention app-side parity.
- AGENT.md / memory rules.

Action: file a separate issue on `dmz006/datawatch-app` describing the rename + migration: app should switch all `llm_backend` reads to `llm_ref`, write only `llm_ref` going forward. Cite the alpha.27 server release tag.

## Migration strategy

**Stored sessions (forward-compat).** Daemon startup walks `<data_dir>/sessions/*.json`. For each session lacking `llm_ref` but having `llm_backend`, copy value forward. Re-persist. Idempotent. Run before opening REST handlers.

**Stored sessions (read-time tolerance).** During the migration window (this release) the JSON unmarshaler accepts BOTH `llm_backend` (legacy) and `llm_ref` (current). After persistence, only `llm_ref` is written. One release later, drop the legacy alias.

**REST request body.** Server accepts both `llm_backend` and `llm_ref` keys for one release; logs a deprecation note when `llm_backend` is used. Removes legacy in alpha.28.

**External tooling (datawatch-app).** App team gets a parity issue. Until they ship, the daemon's dual-key acceptance keeps them working.

## Recommendation

**Phase the rename across two alpha cuts:**
- **alpha.27** (this cut): server-side rename complete. Field renamed in struct. Reads/writes migrated. Migration sweep on startup. Dual-key REST acceptance for one release. Smoke updated. App parity issue filed.
- **alpha.28**: drop the dual-key acceptance after app ships.

This satisfies "fully and completely updated internally" for datawatch (alpha.27) AND gives datawatch-app a release to catch up without a hard break (alpha.28).

If operator prefers hard break (no dual-key): collapse into single alpha.27 cut, app team must ship before tag. **Defaulting to phased per AGENT.md "lockstep ship" reading.**

## Implementation order (alpha.27)

1. Add JSON unmarshal alias (read-time): `llm_backend` accepted, populates LLMRef.
2. Startup migration sweep in `cmd/datawatch/main.go`.
3. Remove `Session.LLMBackend` field; replace all 91 Go reads with `LLMRef`.
4. Update REST handlers: accept both keys on input; emit only `llm_ref` in output.
5. Update MCP `autonomous.go` parameter rename.
6. Update comm verbs in `router.go`.
7. Update `openapi.yaml` schema.
8. Update locale × 5 (label values, not keys, if keys are domain-named).
9. Update smoke + tests.
10. Update active docs (skip historical-plans).
11. CHANGELOG entry.
12. Build + smoke + ship.
13. File datawatch-app parity issue.
