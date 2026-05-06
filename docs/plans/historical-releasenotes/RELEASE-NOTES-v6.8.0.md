# Release Notes — v6.8.0 (BL257 Phase 1 — Identity / Telos layer)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.8.0
Smoke: 96/0/6

## Summary

BL257 Phase 1 — operator identity / Telos layer with full 7-surface parity. Replaces the silent gap exposed in the 2026-05-05 PAI parity audit (filed as part of BL257-BL260). Phase 2 (interview-style init via robot-icon nav) is filed for v6.8.1.

## Why

The PAI comparison analysis (`docs/plans/2026-05-02-pai-comparison-analysis.md` §8 + Recommendation H3) called this out as the highest-value missing PAI feature. Plan-attribution.md had marked it `v6.0.3 (target)` since 2026-05-02 but the BL221 closure dropped it silently. Operator caught the gap and asked for it to be tracked + shipped.

## Added

### `internal/identity` package

- `Identity` struct: `role`, `north_star_goals`, `current_projects`, `values`, `current_focus`, `context_notes`, `updated_at`.
- `Manager` with `Get/Set/Update/SetField/PromptText`. Persists to `~/.datawatch/identity.yaml` (0600). Hot-reload-friendly via `Reload()`.
- `IsEmpty()` short-circuits empty wake-up injection.
- 6 unit tests pass.

### REST surface

- `GET /api/identity` — full read.
- `PUT /api/identity` — replace whole document (empty = clear).
- `PATCH /api/identity` — merge non-empty fields.
- 503 when identity disabled; audit-logged on writes (`identity_set` / `identity_update`).
- 5 unit tests pass.

### MCP tools

- `get_identity` / `set_identity` (PUT) / `update_identity` (PATCH).
- All three proxy to REST so the wire format stays single-source.

### CLI

- `datawatch identity get [--field <name>]`
- `datawatch identity show`
- `datawatch identity set --field <f> --value <v>` (PATCH)
- `datawatch identity edit` — opens `~/.datawatch/identity.yaml` in `$EDITOR` (or `vi`).

### Comm-channel verb

- `identity` / `identity show` — pretty-print full document.
- `identity get [field]` — single-field readout.
- `identity set <field> <value>` — PATCH one field (comma-separated for list fields).

### PWA

- New card **Settings → Agents → Identity** (first card in the Agents tab).
- Edit form for all six fields; Save (PUT) + Reset (refetch).
- Loads on Settings tab open; no separate fetch needed.

### Locale

- 16 new keys × 5 bundles (en/de/es/fr/ja): section title, intro, loading/error states, 6 field labels + 2 placeholders, save/reset buttons, saved toast.

### Wake-up L0 integration

- `memory.Layers.SetIdentityProvider(fn)` — wires the identity manager so `L0()` concatenates legacy `identity.txt` + structured `identity.yaml`-derived prompt text.
- Wired in `cmd/datawatch/main.go` at session-spawn wake-up build site.
- Empty identity yields empty injection (no spurious section header).

### Smoke

- New step "13. v6.8.0 BL257 P1 — Identity / Telos: GET → PATCH round-trip" — verifies endpoint reachability + PATCH merge round-trip + cleanup.

## Backward compatibility

- Legacy `~/.datawatch/identity.txt` continues to work — the new `L0()` reads both and concatenates when present.
- Identity manager loads gracefully when the YAML file is missing (treated as empty).
- All existing tests still pass (1725 vs 1714 pre-BL257; +11 new).

## What didn't change

- No breaking REST/MCP/CLI/comm changes.
- No new go-mod dependencies (uses the already-vendored `gopkg.in/yaml.v3`).
- No new persisted state schema (single YAML file, no migration).

## Mobile parity

[`datawatch-app#53`](https://github.com/dmz006/datawatch-app/issues/53) updated with shipped scope.

## Sequence reminder

This is the first release in the BL257-BL260 PAI parity arc. Next:

- v6.8.1 — BL257 Phase 2 — interview automaton + robot-icon nav (closes BL257)
- v6.9.0 — BL258 — Algorithm Mode (7-phase per-session harness)
- v6.10.0 — BL259 P1 — Evals framework
- v6.10.1 — BL259 P2 — migrate BL221 scan to evals
- v6.11.0 — BL260 — Council Mode

See `docs/plans/2026-05-05-bl257-260-pai-parity-plan.md`.

## See also

- CHANGELOG.md `[6.8.0]` entry
- `docs/plan-attribution.md` (BL257 row updated to ✅ v6.8.0 partial — Phase 2 still open)
