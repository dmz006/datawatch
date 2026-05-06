# datawatch v5.24.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.23.0 → v5.24.0
**Closed:** Autonomous tab auto-refresh on PRD changes (operator-reported v5.22.0 carry-over)

## Why this release

Operator: *"I shouldn't have to refresh the autonomous tab, it should refresh on changes."*

Pre-v5.24.0 the PWA Autonomous tab required a manual Refresh button click after every CLI / chat / REST mutation. v5.24.0 adds a WS broadcast on every PRD persist so the tab auto-reloads.

## What's new

### `MsgPRDUpdate` WS message

New WS broadcast (`internal/server/ws.go`):

```go
MsgPRDUpdate MessageType = "prd_update"  // {"prd_id": "...", "status": "...", "deleted"?: true}
```

Fired from `HTTPServer.BroadcastPRDUpdate(payload)`. The payload carries the affected PRD id + current status, or `deleted: true` when the PRD was hard-deleted.

### Manager.SetOnPRDUpdate indirection

`internal/autonomous/manager.go` gains:

```go
type PRDUpdateFn func(prdID string, prd *PRD)

func (m *Manager) SetOnPRDUpdate(fn PRDUpdateFn)
func (m *Manager) EmitPRDUpdate(prdID string)
```

`EmitPRDUpdate` looks up the PRD (nil for deletions) and forwards to the callback. Callers (the wrapping `*API`) call `EmitPRDUpdate(prdID)` after every successful mutation.

### Wired through `*API` (every mutating method)

`internal/autonomous/api.go` API methods all call `a.M.EmitPRDUpdate(id)` after a successful save:

- `CreatePRD`, `Decompose`, `Run`, `Cancel`
- `Approve`, `Reject`, `RequestRevision`, `EditTaskSpec`, `InstantiateTemplate`
- `SetTaskLLM`, `SetPRDLLM`
- `DeletePRD`, `EditPRDFields`
- A trailing emit fires inside the `Run` goroutine when the executor walk finishes (so terminal `completed` / `blocked` / `cancelled` states reach the PWA).

### main.go binds the broadcast

```go
amgr.SetOnPRDUpdate(func(prdID string, prd *autonomouspkg.PRD) {
    payload := map[string]any{"prd_id": prdID}
    if prd != nil {
        payload["status"] = string(prd.Status)
    } else {
        payload["deleted"] = true
    }
    httpServer.BroadcastPRDUpdate(payload)
})
```

### PWA — debounced reload on `prd_update`

`internal/server/web/app.js` handles the new WS type:

```js
case 'prd_update':
  if (state.activeView === 'autonomous') {
    clearTimeout(state._prdReloadTimer);
    state._prdReloadTimer = setTimeout(() => loadPRDPanel(), 250);
  }
  break;
```

The 250 ms debounce handles bursty mutations (e.g. a `Run` that flips dozens of tasks in a second) so the panel reloads once at the end of the burst rather than dozens of times.

## Configuration parity (per the no-hard-coded-config rule)

No new config knob — auto-refresh is always-on. Operators who don't want the auto-refresh can ignore it (the WS broadcast is per-event, ~100 bytes; client-side debounced).

If a future operator wants to disable: add `autonomous.broadcast_prd_updates` to `AutonomousConfig` + `applyConfigPatch` cases + main.go bridge (the v5.17.0 / v5.21.0 pattern). Not building it speculatively.

## Tests

4 new in `internal/autonomous/prd_update_broadcast_test.go`:

- `TestEmitPRDUpdate_FiresWithCurrentPRD` — callback receives the populated PRD pointer.
- `TestEmitPRDUpdate_NilForMissingPRD` — deletion semantics: nil PRD pointer means "deleted".
- `TestEmitPRDUpdate_NoCallbackWhenUnset` — no panic when SetOnPRDUpdate hasn't been called.
- `TestSetOnPRDUpdate_LastWriterWins` — successive SetOnPRDUpdate replaces the previous handler.

1390 passed in 58 packages (1386 → 1390).

## Known follow-ups

Per the audit doc:

- Diagrams page restructure (drop /plans, add app-docs + howtos)
- Design doc audit / refresh
- Settings card-section docs chips + howto links
- datawatch-app#10 catch-up issue
- Container parent-full retag
- GHCR container image cleanup
- gosec HIGH-severity review

## Upgrade path

```bash
datawatch update                     # check + install
datawatch restart                    # apply

# Verify: open Settings → Autonomous in the PWA, make a CLI mutation
# in another terminal:
datawatch autonomous prd-create "test auto-refresh"
# The new PRD should appear in the PWA list within ~250 ms with no
# operator action.
```
