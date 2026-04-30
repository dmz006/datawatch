# datawatch v5.28.1 — release notes

**Date:** 2026-04-30
**Patch.** BL214 wave-2 i18n string-coverage extension + BL173-followup production-cluster verification doc.

## What's new

### BL214 wave-2 — i18n string-coverage extension

The v5.28.0 foundation wired bottom nav + Settings tabs through `t()`. Wave 2 extends coverage to the most-touched dialogs and tab surfaces using existing Android keys where possible + four new universal keys filed for upstream mirror.

**Wired through `t()`:**

| Where | Android key |
|---|---|
| `showConfirmModal` Yes/No buttons | `action_yes` / `action_no` (new) |
| Single-session delete dialog | `dialog_delete_session_title` |
| Batch-session delete dialog (with `%1$d` count) | `dialog_delete_sessions_title` |
| Stop-session dialog | `dialog_stop_session_title` |
| Alerts-tab loading spinner | `common_loading` (new) |
| Alerts-tab empty state | `common_no_alerts` (new) |
| Autonomous-tab `templates` filter label | `autonomous_filter_templates` |
| Autonomous-tab New PRD FAB (title + aria-label) | `autonomous_fab_new` |

**New keys not yet in Android — filed for upstream mirror at [datawatch-app#39](https://github.com/dmz006/datawatch-app/issues/39):**

| Key | EN | DE | ES | FR | JA |
|---|---|---|---|---|---|
| `action_yes` | Yes | Ja | Sí | Oui | はい |
| `action_no` | No | Nein | No | Non | いいえ |
| `common_loading` | Loading… | Wird geladen… | Cargando… | Chargement… | 読み込み中… |
| `common_no_alerts` | No alerts. | Keine Alarme. | No hay alertas. | Aucune alerte. | アラートはありません。 |

These follow the v5.28.0 Localization Rule (AGENT.md): every new PWA key triggers a datawatch-app issue requesting the matching translations through the Compose-Multiplatform pipeline. Values shipped per-locale to unblock the wave; Android may override with preferred phrasing on the next mirror pull.

### BL173-followup — production-cluster reachability runbook

The cluster→parent push handler (`handlePeerPush` in `internal/server/observer_peers.go`, `POST /api/observer/peers/{name}/stats`) is exercised end-to-end on every release by `scripts/release-smoke.sh` section 8 — register → push → aggregator includes peer. v5.28.1 verifies the same path on this release's daemon (peer `bl173-verify` round-tripped: register `{token}` → push `{status:ok}` → `/api/observer/envelopes/all-peers` reflects `bl173-verify` in `by_peer` → DELETE cleans up).

What was always operator-side: confirming the **network path** from a real production-cluster pod to the parent daemon. The dev-workstation parent isn't reachable from the testing-cluster pod overlay (NAT / overlay-routing gap), so we can't verify the live wire from here. New "Production-cluster reachability check (BL173-followup)" section in `docs/howto/federated-observer.md` with the exact pod-side commands (register → curl push → verify aggregate → cleanup) so the operator can run the check inside a prod pod when convenient. Failure modes documented: connection error = network gap, 401/403 = auth/token plumbing — both deploy-side, not daemon code.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1544 passed in 58 packages (parity guard list extended in v5280_locales_test.go)
Smoke:     run after install
```

`TestLocales_CommonNavKeysPresent` extended with the 4 new keys + the 5 wave-2 dialog/autonomous keys so a future stale-mirror pull is caught at build time.

## datawatch-app sync

- [datawatch-app#39](https://github.com/dmz006/datawatch-app/issues/39) — request matching translations for `action_yes`/`action_no`/`common_loading`/`common_no_alerts`. Carries forward [datawatch-app#38](https://github.com/dmz006/datawatch-app/issues/38) (Settings → System → MCP channel mirror).

## Backwards compatibility

- All additive. Strings not yet keyed continue to render in English (the harness returns the raw key on miss with EN-fallback bundle catching it).
- New universal keys ship with operator-pickable values per locale; harmless to override on the next Android mirror.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-28-1).
```

No data migration. No new schema. No new server-side config keys.
