# Parity issue draft — observer pull mode (datawatch-app)

**Title (filed on datawatch-app):** `feat(i18n): observer pull mode — DE/ES/FR/JA translations + Android/iOS parity`
**Status:** DRAFT — no GitHub issue has been filed, and this task does not file one (per the operator instruction for this task: draft only, no `gh`). File it before the app ports observer pull mode.
**Parent release:** v9.67.1 (next patch above main at the time of this draft — placeholder; confirm at merge time).
**Daemon counterpart (this repo):** `docs/howto/federated-observer.md` — *Pull mode*, `docs/api/observer.md` — *Observer pull mode*, `docs/config-reference.yaml` — `observer.pull`, the PWA federation peer form and peer rows (`internal/server/web/app.js`), and the twelve locale keys below.

## What changed in datawatch

The federated observer gained a **pull mode** that inverts the push protocol: the daemon fetches a peer's v2 stats on a schedule (`GET {peer-url}/api/observer/stats?v=2` with the peer's bearer token) instead of the peer pushing them. It is **off by default** — globally gated by `observer.pull.enabled` (default `false`) and per peer by the `observer_pull` flag (default `false`), with per-peer `observer_pull_interval_s` (0 = the global `observer.pull.default_interval_s`, default 30 s, minimum 5) and `observer_pull_timeout_s` (0 = the global `observer.pull.default_timeout_s`, default 5 s). The pull state read is `GET /api/observer/pull`; every observer peer row carries the new `source` (push/pull), `shadow` and `pull` object fields. The PWA already ships the federation peer form controls (pull opt-in, interval, timeout), the source badge (push / pull / shadow), the stale chip with the reason, and all five locale bundles.

This is a capability-parity and i18n issue: the app needs the pull-mode settings and status in Android and iOS, and the new keys in the four non-English bundles.

## What the app needs

1. **Federation peer form** — per peer: the **Observer pull** opt-in, the pull interval (seconds) and pull timeout (seconds), with blank meaning "server default" (the `default` hint).
2. **Observer peer rows** — a **Source** badge reading push / pull / shadow, and a stale chip that shows the reason when present (`timeout`, `error`, `forbidden`, `not_permitted`, `bad_payload`, `unknown`; none is the healthy state).
3. **Pull status read** — the same rows as `GET /api/observer/pull`: `name`, `source`, `shadow`, `stale`, `stale_reason`, `last_success_at`, `interval_s`, `timeout_s` (empty list when no peer is opted in).
4. **Settings** — `observer.pull.enabled`, `observer.pull.default_interval_s` (minimum 5), `observer.pull.default_timeout_s`, through `PUT`/`GET /api/config`; a daemon refusal must be shown with its message and nothing saved.

## New keys (English values are the canonical strings)

Context: the **federation peer form** and the **observer peer rows** (the pull opt-in, the interval/timeout fields, the source badge and the stale chip).

| Key | English value | Rendered as |
|---|---|---|
| `federation_observer_pull_label` | `Observer pull` | the per-peer pull opt-in label |
| `federation_observer_pull_label_title` | `Opt this peer into the observer pull protocol: the daemon fetches the peer's v2 stats on an interval instead of waiting for the peer to push them (off by default).` | the opt-in's help text |
| `federation_observer_pull_interval` | `Pull interval (sec)` | the interval field label |
| `federation_observer_pull_interval_title` | `Seconds between pull fetches for this peer; blank uses the server default.` | the interval field's help text |
| `federation_observer_pull_timeout` | `Pull timeout (sec)` | the timeout field label |
| `federation_observer_pull_timeout_title` | `Per-fetch timeout in seconds for this peer; blank uses the server default.` | the timeout field's help text |
| `federation_observer_pull_default_hint` | `default` | the placeholder for a blank interval or timeout |
| `federation_observer_pull_source` | `Source` | the source badge's label |
| `federation_observer_pull_source_push` | `push` | the badge for a push-registered peer |
| `federation_observer_pull_source_pull` | `pull` | the badge for a pulled peer |
| `federation_observer_pull_source_shadow` | `shadow` | the badge for a visibility-only peer |
| `federation_observer_pull_stale` | `stale` | the stale chip (reason appended when present) |

## Translations

Request **DE, ES, FR and JA** translations of all twelve keys in the app's locale bundles, copying the key names and the English values above as the source of truth. The datawatch PWA already carries all five bundles (the four translations in `internal/server/web/locales/{de,es,fr,ja}.json` of the datawatch repository can be used as the reference translations).

## Acceptance

- The federation peer form shows the pull opt-in and the two numeric fields on both platforms; blank fields send nothing (server default applies), set fields round-trip, and the daemon's rejection message is shown on a bad value with nothing saved.
- The peer rows show the source badge (push / pull / shadow) and the stale chip with the reason; a healthy peer shows neither warning.
- The pull status read renders the same rows as `GET /api/observer/pull`, including the empty state when no peer is opted in.
- All twelve keys exist in the DE, ES, FR and JA bundles with the English placeholders intact.

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | `GET /api/observer/pull`; `POST`/`PUT /api/federation/peers[/name]` with `observer_pull`, `observer_pull_interval_s`, `observer_pull_timeout_s`; `GET`/`PUT /api/config` under `observer.pull`; the `source`/`shadow`/`pull` fields on `GET /api/observer/peers` |
| MCP | Yes | `observer_pull_status` (read-only, `observers:read`); `config_set` for the `observer.pull.*` keys |
| CLI | Yes | `datawatch observer pull status [--json]`; `federation peer add/update --observer-pull [--observer-pull-interval N] [--observer-pull-timeout N]`; `config set observer.pull.*` |
| comm channel | Yes | `observer pull status`; `rest` passthrough for the config and peer routes |
| YAML/config | Yes | the `observer.pull` block in `docs/config-reference.yaml` |
| PWA | Yes (reference) | federation peer form + peer rows in `internal/server/web/app.js`; five locale bundles already ship |
| Android | Not yet implemented | follows in this issue |
| iOS | Not yet implemented | follows in this issue |
