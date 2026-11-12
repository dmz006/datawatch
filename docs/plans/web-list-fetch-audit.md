# Web UI list-fetch audit

Scope: every `fetch(` / `apiFetch(` call in the non-test files under
`internal/server/web/` whose URL is a list endpoint (an array, `{items}` or a
named-array envelope). The inventory was taken with
`grep -nE "(fetch|apiFetch)\("` over `app.js`, `sw.js`, `api-docs.js`,
`diagrams.js` and `splash-art.js`; line numbers below refer to `app.js` unless
a file is named.

## Server support the audit was measured against

| Endpoint | Summary form | Paging | Conditional request |
|---|---|---|---|
| `GET /api/sessions` | `view=summary` | `limit`, `cursor`, `states=` filter | `ETag` / `If-None-Match` |
| `GET /api/autonomous/prds` | summary is the default form (`full=1` opts out) | `limit` (default 50, max 500), `cursor`, `status=`, `active=1` | `ETag` / `If-None-Match` |
| `GET /api/alerts` | n/a | `limit`, `before`, `unread=1` (reply gains `has_more`, `total`, `unread_count`) | `ETag` |
| `GET /api/automations` | n/a | none | `ETag` + `Cache-Control: no-cache` |
| `GET /api/evals/runs`, `/api/council/runs`, `/api/docs/search` | n/a | `limit` | none |
| `GET /api/memory/list` | n/a | `n` | none |
| everything else in the tables below | n/a | none | none |

`Cache-Control: no-cache` plus an `ETag` means the browser itself revalidates
with `If-None-Match` on every repeat request of the same URL, so no script change
is needed to get a conditional request on those endpoints.

## Dispositions

- **converted**: requested the full form on a tab open, reconnect or timer and
  now asks for the summary form with a limit (or pages).
- **already summary**: the call already uses the summary form, a limit, or an
  endpoint whose default reply is the summary form.
- **user action only**: only a click, a form open or a modal triggers it.
- **left alone**: loads on a tab open, reconnect or timer, has no summary,
  limit or conditional form on the server, and is documented with a reason.

## Sessions, alerts, Automata

| Location | Endpoint | Trigger | Disposition | Reason / change |
|---|---|---|---|---|
| `app.js:355` (`_checkFederatedConnection`) | `/api/sessions?view=summary&limit=50` via the server's proxy prefix | reconnect probe | converted | Was the full list; now the first summary page, with the cursor kept so Load more works for named servers. |
| `app.js:2701` (`_sessionsFetchPage`) | `/api/sessions?view=summary&limit=50[&cursor=]` | tab open, reconnect, Load more | already summary | Shared paged fetcher; reconnect resync reuses it. |
| `app.js:12747`, `12879` (`_prdPanelFetch`, load-more handler) | `/api/autonomous/prds?view=summary&limit=…` | tab open, 30 s timer, Load more | already summary | Paged through `_prdPanelQuery`. |
| `app.js:21393`, `21491` (`_automataFetchAll`, `automataLoadMore`) | `/api/autonomous/prds?view=summary&limit=…` | tab open, 30 s timer, Load more | already summary | Paged through `_automataQuery`. |
| `app.js:23086` (`_loadPRDActiveSessionCard`) | `/api/sessions?states=active` | tab open (detail card) | converted | Was the whole session list filtered client-side; now the server returns only active sessions. |
| `app.js:27670` (`_alertsFetch`) | `/api/alerts?limit=N` | tab open, 30 s timer | converted | Was the full list. Now a window of `limit` rows that widens by a page on each Load more, so a refresh never drops rows the operator already loaded. |
| `app.js:30336` (boot init, unread alert count) | `/api/alerts?unread=1` | boot | converted | The badge needs only unread alerts; `unread_count` and `total` come back in the reply. |
| `app.js:32498` (`_dashFetchPRDs`) | `/api/autonomous/prds?status=running,blocked,planning&limit=500` | dashboard open, timer | converted | Dashboard counts need only the three live statuses. **Trade-off:** 500 is the server maximum (`prdListMaxLimit`), so a deployment with more than 500 Automata in those statuses would under-count the dashboard totals where the old unpaged form returned every row. No server form returns more, so the cap is accepted. |
| `app.js:24932` (`_automationFetchAll`) | `/api/automations` | tab open, 30 s timer | left alone | No limit or summary form exists on the server. The reply carries `ETag` with `no-cache`, so the 30 s refresh is already a conditional request that answers 304 when nothing changed. |
| `app.js:25566` (`loadAutomationPrdInfo`) | `/api/autonomous/prds` | tab open, 30 s timer, insert dialog | already summary | The bare path is the summary form (default limit 50 plus every active row) and carries an `ETag`; the row has the `title`, `status`, `is_template` and `parent_prd_id` fields the picker reads. |
| `app.js:26303` (`openAutomationInsertDialog`) | `/api/automations` | user action only | left alone | Opens a dialog; same endpoint and reasons as above. |
| `app.js:27213`, `27215` (`openAutomationForm`) | `/api/autonomous/prds`, `/api/automations` | user action only | already summary / left alone | Form open only; the first is the summary form, the second has no paged form. |
| `app.js:27560` (`openAlertDeepLink`) | `/api/alerts` | user action only | left alone | A deep link has to find one alert by id wherever it sits in history, so it needs the unwindowed list; it runs once per link click, never on a timer. |
| `app.js:18362` (`_loadAllServersSessions`) | `/api/sessions/aggregated` | tab open (All-servers view), reconnect | left alone | Federation fan-out endpoint; it has no summary, limit or conditional form on the server. Paging it would need a server change. |
| `app.js:27669`/`27670` (`_alertsFetch`, All-servers branch) | `/api/alerts/aggregated` | tab open, 30 s timer | left alone | Same: fan-out endpoint with no paged form. |
| `app.js:32498` (`_dashFetchPRDs`, All-servers branch) | `/api/autonomous/prds/aggregated` | dashboard open, timer | left alone | No paged form on the aggregated route. |
| `app.js:21393` (`_automataFetchAll`, All-servers branch) | `/api/autonomous/prds/aggregated` | tab open, 30 s timer | left alone | Same. |
| `app.js:32506` (`_dashFetchCost`) | `/api/cost/aggregated` | dashboard timer | left alone | Single aggregate object, not a growing list. |

## Observer, schedules, compute, plugins (timer and tab-open, no server paging)

None of these handlers read a `limit`, `cursor` or summary parameter and none
sets an `ETag`; they are bounded by the number of configured peers, nodes,
plugins or schedules rather than by history, so they are left alone.

| Location | Endpoint | Trigger | Disposition |
|---|---|---|---|
| `app.js:3082` (`loadGlobalScheduleBadge`), `5255` (`_loadSessionSchedules`) | `/api/schedules?state=pending`, `/api/schedules?session_id=…&state=pending` | tab open, session open | left alone (server filters only by `state` and `session_id`) |
| `app.js:29584` (`loadSchedulesList`) | `/api/schedules` | Settings tab open, user action | left alone (same; user-paginated client side) |
| `app.js:20681` (`updatePeerStaleBadge`) | `/api/observer/peers` | 30 s timer | left alone (peer count is bounded by configuration) |
| `app.js:5491` (`renderSessionStats`), `23303` (`_loadStatusGraphsCompute`), `28238` (`loadObserverEnvelopesCard`), `33525` (`renderObserverPeersCard`) | `/api/observer/envelopes`, `/api/observer/peers` | tab open, stats refresh | left alone |
| `app.js:11777`, `28364` (`loadPeerResourceOverview`), `28494` (`loadSystemStatsGrid`), `28701` (`loadObserverPeers`), `28853` (`showCrossHostView`) | `/api/observer/peers`, `/api/observer/envelopes/all-peers` | tab open | left alone |
| `app.js:31079` (`_dashLoop`), `10286`, `12150`, `28702` | `/api/compute/nodes` | dashboard timer (about every 60 s), tab open | left alone (bounded by configured nodes) |
| `app.js:28641`, `31634`, `34118` | `/api/plugins` | tab open | left alone (bounded by installed plugins) |
| `app.js:28214`, `8222`, `23064`, `23500` | `/api/backends` | tab open | left alone (small fixed set; `23064` is cached in `_backendsCache`) |

## Settings and configuration lists (tab open or user action)

These are small, operator-curated collections with no history growth. None has
a server-side summary, limit or conditional form, so they are **left alone**
when they load on a tab open and **user action only** otherwise.

`/api/llms` (7823, 12004, 12322, 12748, 14476, 16540, 16692, 21394, 21931),
`/api/profiles` and `/api/profiles/projects|clusters` (8000, 8136, 14479,
20101, 21933), `/api/commands` (3617, 7240, 7554, 27688, 29672),
`/api/filters` (29775), `/api/exit-hooks` (30007), `/api/servers` and
`/api/servers/health` (11532, 18248, 18249, 18380, 18710),
`/api/federation/peers`, `/groups/builtins`, `/meta-peers` and
`/fingerprint-matches` (18430, 28789, 33296, 33357, 33358, 33403),
`/api/skills`, `/api/skills/registries` (15428, 34166, 35233, 35313, 35353),
`/api/routing-rules` (34284), `/api/orchestrator/graphs` (34489),
`/api/llm-roles` (34749), `/api/templates` (35774), `/api/pipelines`
(36035), `/api/algorithm` (36333), `/api/cost/rates` (35644), `/api/secrets`
(33948), `/api/docs/trust` and `/pending` (33777, 33788), `/api/queue` (37342),
`/api/projects` (14654), `/api/mcp/resources`, `/templates`, `/prompts`
(17330, 17379, 17470), `/api/ollama/models`, `/api/openwebui/models`
(17783, 17831), `/api/sessions/finished-oneshots` (12505), `/api/files`
(8298, 17091), `/api/files/meta` (33650), `/api/tooling/status` (33615),
`/api/memory/discussion` (33707), `/api/memory/scopes/inventory` (18150),
`/api/memory/stats` (17962, 18042, 32008).

## Lists that already take a limit

| Location | Endpoint | Note |
|---|---|---|
| `app.js:36422` | `/api/evals/runs?limit=10` | already limited |
| `app.js:36496` | `/api/council/runs?limit=5` | already limited |
| `app.js:33758`, `diagrams.js:200`, `diagrams.js:237` | `/api/docs/search?q=…&limit=…` | already limited, user-typed query |
| `app.js:18083` | `/api/memory/list?n=50` | already limited |
| `app.js:18122`, `24441`, `18176` | `/api/memory/search`, `/api/memory/scopes/recall` | query results, user action |
| `app.js:36000` | `/api/audit?…` | filtered by the audit panel's own query form; user action |
| `app.js:36494`, `36858`, `36924` | `/api/council/personas` | small fixed set; tab open or user action |

## Files with no list fetches

- `sw.js:109`, `sw.js:121`: fetch of the request being intercepted (static
  assets and navigation); not list endpoints.
- `api-docs.js`, `splash-art.js`: no `fetch(` calls.
- `diagrams.js:275`: loads a diagram file by path; not a list.
- `app.js:44`: locale JSON file.

## Single-resource reads that were skipped on purpose

`/api/sessions/<id>`, `/status`, `/current-status`, `/response`,
`/api/autonomous/prds/<id>` and its sub-paths, `/api/llms/<name>`,
`/api/compute/nodes/<name>/…` and `/api/observer/peers/<name>/stats` return one
record, not a list, and are out of scope. A PRD's `/children` (13191, 14030) is
bounded by that PRD's own fan-out and is only requested when its detail is open.

## Verification

- `node --check internal/server/web/app.js` passes.
- The existing web tests (`app-*.test.js`, `tab-loading.test.js`,
  `list-state.test.js`) pass under `env -u TMUX -u TMUX_TMPDIR node --test`.
- New tests: `app-list-fetch-conversions.test.js`, `app-reconnect-resync.test.js`,
  `app-prd-panel-paging.test.js`.
