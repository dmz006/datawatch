# datawatch v5.19.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.18.0 → v5.19.0
**Closed:** Operator-blocking CRUD + UX cleanup + audit gaps

## Why this release

Operator audit found:

1. **Autonomous list isn't full CRUD** — Cancel button only shows on `running` status; no hard-delete anywhere; no PRD-level Edit. Operators with cancelled / completed / archived PRDs can't actually clear the list.
2. **Whisper test-button missing** — operator-reported. Test transcription endpoint rendered as an empty input box.
3. **Tmux arrow keys missing** — regression of v5.2.0; the Response button + ↑↓←→ row got blown away by the saved-commands renderer.
4. **"Input Required" label duplicated** — inline label above the tmux input box duplicated the top-of-page badge.
5. **RTK section duplicated in Settings** — appeared in both General and LLM sub-tabs.
6. **README.md marquee 12 releases stale** — discipline broke at v5.0.4 (previous session) and continued through v5.18.0 (this session).

v5.19.0 closes all six.

## What's new

### Autonomous full CRUD (the big one)

```go
// Store
Store.DeletePRD(id)              // hard-removes + recursive-descendant cleanup
Store.UpdatePRDFields(id, title, spec)  // edit on non-running PRDs only

// Manager
Manager.DeletePRD(id)            // refuses running; otherwise delegates to Store
Manager.EditPRDFields(id, title, spec, actor)  // appends a Decision row
```

Reachable everywhere:

| Surface | Delete | Edit |
|---------|--------|------|
| REST | `DELETE /api/autonomous/prds/{id}?hard=true` | `PATCH /api/autonomous/prds/{id}` |
| CLI | `datawatch autonomous prd-delete <id>` | `datawatch autonomous prd-edit <id> --title=… --spec=…` |
| PWA | "Delete" button on every PRD card (confirm dialog) | "Edit" button on every non-running PRD card (modal w/ title + spec textarea) |

Recursion-aware: deleting a parent PRD also removes every descendant
spawned via `Task.SpawnPRD` so the genealogy tree stays consistent.
Refuses to delete a `running` PRD — Cancel first.

8 new unit tests cover delete-from-map, recursive-descendant-cleanup,
not-found-error, refuses-running, update-title-only, update-spec-only,
update-refuses-running, edit-appends-decision.

### PWA fixes

- **Whisper test-button** — `loadGeneralConfig` lacked an `f.type === 'button'` branch (the comms renderer had it, the general renderer fell through to a generic `<input>`). Added the branch; the BL289 v5.4.0 test transcription button renders correctly again.
- **Tmux arrow keys** — `loadSavedCmdsQuick` was overwriting `#savedCmdsQuick.innerHTML` after the initial render placed the Response button + ↑↓←→ group there. Restored Response + arrows in the rebuild path so they survive the saved-commands fetch.
- **Input-required label** — removed the inline `<div class="input-label">Input Required</div>` above the tmux input box; the top-of-page yellow badge already conveys waiting_input state.
- **RTK section** — removed from `GENERAL_CONFIG_FIELDS`; the fuller version (with `auto_update` + `update_check_interval`) stays in `LLM_CONFIG_FIELDS`.

### README marquee

Bumped from `v5.7.0 (2026-04-26)` to `v5.19.0 (2026-04-26)`. Per AGENT.md § Release-discipline rules, this update was missed for 12 consecutive releases across two sessions. The audit doc at `docs/plans/2026-04-26-audit-thursday-night-to-now.md` recommends a CI gate to enforce the rule going forward.

## Tests

1376 passed in 58 packages (1358 → 1376; 18 new):

- 8 in `internal/autonomous/crud_test.go` (DeletePRD + EditPRDFields)
- 10 already-shipped in `internal/server/redirect_bypass_test.go` (v5.18.0 follow-up audit work)

## Known follow-ups

Per the audit doc, remaining cleanup releases:

- **v5.20.0** — docs alignment (mcp.md, commands.md, api/*.md, testing-tracker)
- **v5.21.0** — observer + whisper config-parity sweep (same pattern as v5.17.0)
- **v5.22.0** — observability fill-in (stats metrics + Prom metrics for BL191 Q4/Q6 + BL180 cross-host)
- **v5.22.x patches** — datawatch-app#10 catch-up ticket; container parent-full retag; gosec HIGH-severity review

## Upgrade path

```bash
datawatch update                         # check + install
datawatch restart                        # apply

# Verify CRUD reaches all surfaces:
datawatch autonomous prd-list
datawatch autonomous prd-delete <some-archived-id>
datawatch autonomous prd-edit <some-draft-id> --title="New title"

# In the PWA: Settings → General no longer has an RTK section. The
# Autonomous tab now has Edit + Delete buttons on every PRD card.
# The session detail has the tmux arrow keys back next to saved-commands.
```
