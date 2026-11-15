# State/Status Filter Inventory — Automata, Projects, and Plans pages

Source: `internal/server/web/app.js` (65,244 lines), worktree `cc5b0001`, all line
numbers verified by `grep -n` / `sed` on 2026-11-09. Scope = Automata, Projects and
Plans pages and all their sub-pages. The Sessions page state filter is out of scope.

Page map (for reference):

- Projects page: `renderProjectsView()` L6358, sub-tabs
  `['overview','plans','records','gantt','unassigned','history','events']`
  (`_PROJECTS_SUBTABS` L3893, strip `selectProjectsSubtab` L3916).
- Automata panel sub-tabs: `['projects','automation','automata','events','templates']`
  (`_AUTOMATA_TABS` L32049). The 'projects' sub-tab renders the same Projects page
  (L33438, L43527).

---

## 1. Plan Records state filter — `planRecordsStateFilter` (Projects → Records)

Per the kind tab, a `<select>` that filters the registry-row / finding records of
one project. **Applied server-side** (a `state=` query parameter).

- Constants: `_PLAN_RECORD_STATES` L5244-5247 —
  `registry_row: ['open','planned','in_progress','blocked','shipped','done','deferred','cancelled']`,
  `finding: ['open','triaged','fixed','wontfix','duplicate']`;
  `_PLAN_RECORDS_PAGE = 50` L5248.
- Selection variable: `_planRecords.stateFilter` (string, `''` = all); the state
  object is built by `_planRecordsFresh(project)` L5250 (fields incl. `kind`,
  `stateFilter`, `q`, `recs`, `total`, `status`, `seq`).
- Rendering: `planRecordsSectionHtml(v)` L5505 — kind tabs
  `planRecordsSetKind('registry_row'|'finding')` L5510 and, in list view:
  `<select id="planRecordsStateFilter" onchange="planRecordsSetState(this.value)">`
  L5511, options from `_planRecordsStateOptions(kind, v.stateFilter, true)` L5336;
  plus the search box `#planRecordsSearch` → `planRecordsSearchInput(this.value)`
  L5512. Slotted into the Records sub-tab via `_planRecordsSlotHtml(p)` L5531 and
  the sub-tab body at L6342.
- Setter: `window.planRecordsSetState` L5593 → `_planRecordsLoad(false)`.
  Kind switch `window.planRecordsSetKind` L5584 resets `stateFilter` and reloads.
  Search debounce: `window.planRecordsSearchInput` L5599, timer
  `window._planRecordsSearchTimer` L5602 (250 ms → `_planRecordsLoad(false)`).
- Fetch: `_planRecordsLoad(more)` L5550 —
  base params `project=,limit=50,offset=`; appends `state=<stateFilter>` when set
  (L5560); when a query is present the route becomes
  `GET /api/plan-records/search?q=&kind=&state=...` (L5562-5563), otherwise
  `GET /api/plan-records/{registry-rows|findings}?project=&limit=&offset=&state=`
  (route `_planRecordsRoute` L5276).
- Where applied: **server** (state is a request parameter; option set changes per
  kind).

## 2. Plans Browser state filter — `#plansBrowserState` (Projects → Plans)

A single `<select>` filtering the project's plan list. **Applied client-side
only** — the plans list fetch carries no state parameter.

- Selection variable: `_plansBrowser.stateFilter` (string, `''` = all), created by
  `_plansBrowserFresh(project)` L5820 (field at L5828). `_PLANS_PAGE = 50` L5815;
  expand/collapse state persisted under `_PLANS_BROWSER_KEY` (localStorage).
- Rendering: `plansBrowserHTML(p)` L6103 — search input `#plansBrowserSearch`
  L6107 and `<select id="plansBrowserState" onchange="plansBrowserSetState(this.value)">`
  L6108, options = `_PLAN_RECORD_STATES.registry_row` (L6105), i.e.
  open / planned / in_progress / blocked / shipped / done / deferred / cancelled.
- Setter: `window.plansBrowserSetState` L5970 → `_plansBrowserPaint()` L6120
  (re-render only, no fetch).
- Match: `_plansMatch(st, p)` L5944 — `if (st.stateFilter && p.state !== st.stateFilter) return false;`
  (exact string compare against `p.state`).
- Fetch: `_plansBrowserLoad(more)` L5862 —
  `GET /api/projects/{name}/plans?limit=50&offset=..&backlog_offset=..` — **no
  state parameter**.
- Search (side note): `window.plansBrowserSearchInput` L5962 (250 ms) matches
  title/next/automaton locally and asks the server which plans match their
  records via `GET /api/plan-records/browse?project=..&limit=500&q=..` (L5969).
- Where applied: **client-side only** (after the response, before paint).

## 3. Records browse tab — kind + state selects (Projects → Records)

The browse table for one project's records (registry rows + findings). **Both the
kind and the state are applied server-side** as query parameters.

- Constants: `_RECORDS_TAB_STATES` L6234 (concatenation of the
  `_PLAN_RECORD_STATES` option lists), `_RECORDS_TAB_PAGE = 50` L6233.
- Selection variables: `_recordsTab.kind` and `_recordsTab.stateFilter`
  (string, `''` = all), from `_recordsTabFresh(project)` L6236.
- Rendering: `recordsTabHTML(p)` L6297 — kind select
  (class `records-tab-kind-filter`, L6300) and state select
  (class `records-tab-state-filter`, L6301), both in the Records sub-tab body
  (L6342).
- Setters: `window.recordsTabSetKind` L6322, `window.recordsTabSetState` L6323 —
  each sets the field and calls `_recordsTabLoad(false)`.
- Fetch: `_recordsTabLoad(more)` L6241 —
  `GET /api/plan-records/browse?project=&limit=50&offset=` + optional
  `q` (L6251), `kind=<kind>` (L6252), `state=<stateFilter>` (L6253).
- Paint: `recordsTabPaint` via `_recordsTabPaint()` L6310.
- Where applied: **server** (both parameters ride the request).

## 4. Projects overview / plan lists — no state filter

- Overview: `_projectOverviewSummaryHTML` (drawn in L3844-3879) shows the
  `active_plans` list with per-plan status badges (L3856) — display only. Loaded by
  `_projectsLoadSummary` L5749 → `GET /api/projects/{name}/overview-summary`
  (L5751), no filter parameters.
- Project rollup: `_projectsLoadRollup` L6384 → `GET /api/projects/{name}/rollup`
  (L6391), no state filter.
- Gantt sub-tab: rows carry `data-state` attributes (e.g. L4819) for styling only;
  no filter controls exist in the Gantt code.
- Events sub-tab (Projects, L3861): renders the rollup's `events` text lines only
  when `hasEvents` (L3911-3914); display only, no filter.

## 5. Events sub-tab (Automata → events)

Event-log panel. The equivalent of a "status filter" here is the **event kind**
chip set; there is no separate state field. **Applied server-side** (`kind=`
parameter).

- Constants: `_AUTOMATA_EVENT_KINDS` L33452 —
  `['watch','decision','retry','check','cleanup','alert','error']`;
  `_AUTOMATA_EVENTS_LIMIT = 50` L33449.
- Selection variables: `_automataEventsState` L33456 — `kinds` (Set), `project`
  (string), `q` (string), `nextCursor`, `events`, `projects`, `gen`.
- Rendering: `renderAutomataEventsPanel()` L33636 — embeds
  `automataEventsFilterHtml()` L33742 (kind chips + project dropdown
  `#automataEventsProject` L33667 + search `#automataEventsSearch` L33670).
- Setters: `toggleAutomataEventKind(kind)` L33839 (`st.kinds` add/remove →
  `automataEventsReload()` L33792); `setAutomataEventsProject(name)` L33847;
  `scheduleAutomataEventsSearch(text)` L33857 (300 ms → reload).
- Fetch: `automataEventsQuery(before)` L33561 —
  `GET /api/autonomous/event-log?limit=50` + optional `kind=a,b,c`
  (L33564-33565), `project=..` (L33566), `q=..` (L33567), `before=<cursor>`
  (L33568). Performed by `automataEventsFetch(before)` L33573 (single in-flight
  guard, one retry, gen token), invoked from `automataEventsLoad(append)` L33810
  and `automataEventsLoadMore()` (load-more path).
- Paint: `paintAutomataEvents(events, errorText)` L33601; control sync
  `automataEventsSyncControls()` L33774.
- Where applied: **server** (kind/project/q are request parameters).

## 6. Automata list (Automata → automata tab)

Status badge chips (+ history toggle, type badges, search). **Applied both
server-side** (status/active/shipped/q parameters) **and re-applied client-side**
(active-status gate, type, search, tags).

- Selection variables: `_automataState` L32066 —
  `statusFilter` (Set of status strings, L32067), `historyOn` (bool, L32065;
  false = active-only), `typeFilter` (Set, L32068), `search` (string, L32069),
  `pinned` (Set), `pageSize: 50`. localStorage: `cs_automata_tab`,
  `cs_automata_filter`, `cs_automata_pinned`.
- Constants: `_AUTOMATA_ACTIVE_STATUSES` L32179 —
  `draft, planning, decomposing, needs_review, revisions_asked, approved, running,
  paused, blocked, completed`; `_AUTOMATA_STATE_RANK` L32128 (sort order);
  `_TAG_AUTOMATA_STATUS_BADGES` L60121 (active vs. history badge groups).
- Rendering (badges): `_tagFacetsHTML(scope)` L60983 — for the automata scope the
  status badge buttons call `toggleAutomataStatusFilter('<status>')` (L61050-61051,
  active + history groups with the active set at full opacity, history set dimmed);
  type badges call `toggleAutomataTypeFilter` (L33929). Badge counts are computed
  per status at L61026-61040 (shipped-count adjustment L61030-61040 when
  `_shippedState.enabled && _shippedState.count_separately`).
- Setters (all global on `window`):
  - `toggleAutomataStatusFilter(s)` L33919 — toggles the value in
    `_automataState.statusFilter`, re-renders (`_automataRenderCards()`) and
    refetches (`loadAutomataPanel()`).
  - `toggleAutomataHistory()` L33906 — flips `historyOn`, same effect (comment:
    "history / active is a server-side filter now").
  - `toggleAutomataTypeFilter(type)` L33929 — client-side only (no refetch).
  - `onAutomataSearchChange(val)` L33934 — sets `search`, 300 ms debounce →
    `loadAutomataPanel`; also matched client-side in `_automataBaseList`
    (title/id) L32199-32201.
- Query build: `_automataQuery(cursor)` L32712 —
  `/api/autonomous/prds?limit=50[&cursor=...]` plus:
  - `status=<csv>` of `statusFilter` minus `shipped`/`unshipped` (L32720-32722);
  - else, if shipped selection is `'all'` and `!historyOn`, `active=1` (L32723);
  - `shipped=true` / `shipped=false` when the shipped selection is active
    (L32724-32725, via `shippedFilterSelection` L20977);
  - `q=<search>` when set (L32726).
- Fetch: `loadAutomataPanel()` L32844 → `_automataLoader()` L32815 (generic
  `listLoader` keyed `automata`, poll `30000` ms) → `_automataFetchAll` L32730:
  `GET /api/autonomous/prds` (or `/api/autonomous/prds/aggregated` in
  all-servers mode, L32733) + best-effort `/api/llms`, tags, `/api/capacity`.
  Load-more: `window.automataLoadMore` L32825 fetches
  `_automataQuery(st.nextCursor)` (L32833) and appends.
- Client-side passes (after the response):
  - `_automataBaseList()` L32184 — active-status gate: if `!historyOn` and no
    status badges are selected, keep only `_AUTOMATA_ACTIVE_STATUSES` (L32191-32193);
    then `typeFilter`, `watchFilter` (GH#172), search.
  - `_automataFilteredList()` L32203 — `_tagBrowseItems('automata', base)`
    (L60511) applies tag/backend/project/status facets client-side (statuses from
    `_automataState.statusFilter`, L60513), then sorts: pinned → status rank
    (`_AUTOMATA_STATE_RANK`) → activity.
- Card painting: `_automataCardsHtml()` L32784-32792 →
  `_tagBrowseMountHTML('automata', ...)`.
- Where applied: **server** (status / active / shipped / q) **and** client
  (active gate, type, tag, watch, search re-match).

## 7. Automations list (Automata → automation sub-tab)

Status badge chips filter Automations. **Applied client-side only** — the fetch
takes no state parameter.

- Selection variables: `_automationState` L43551 — `statusFilter` Set, seeded
  `new Set(['draft','active','paused','blocked','completed'])` (L43562 — i.e.
  `cancelled` is **off by default**), `historyOn` (L43563, default false),
  `search` (L43561). Sub-tabs L43515:
  `['inbox','automations','runs','pipelines','usage','unlanded','repair','settings','projects']`.
- Constants: `_AUTOMATION_STATUSES` L43599 —
  `['draft','active','paused','blocked','completed','cancelled']`;
  `_AUTOMATION_STATUS_ORDER` L46519 (sort order).
- Rendering (badges): `_tagFacetsHTML('automation')` L60983 — one chip per status,
  `onclick="toggleAutomationStatusFilter('<s>')"` (L61053); the same shipped-count
  adjustment applies (L61030-61040).
- Setter: `toggleAutomationStatusFilter(s)` L44695 — toggles membership in
  `_automationState.statusFilter` and calls `_automationRenderCards()`.
  Search: `onAutomationSearchChange(val)` L44704.
- Fetch: `_automationFetchAll()` L46459-46460 —
  `automationRequest('GET', '/api/automations')` (proxied via
  `/api/proxy/{server}` for remote servers; L404/`_projectsApiBase` not involved) —
  **no status parameters**.
- Visibility: `automationListVisible(list, st)` L46577-46615 —
  - status rule: an Automation passes if its status is in `statusFilter` (an empty
    Set passes everything); `cancelled` is special: shown if it carries the
    shipped-state, or `historyOn` is set, or `cancelled` is in the filter
    (L46602-46607);
  - shipped selection: `shippedFilterSelection(f)` L20977 with
    `shippedCarriesState(a)` (merge commit) L46595-46600;
  - search on name/id (L46612-46613);
  - sort by `_AUTOMATION_STATUS_ORDER`, then name (L46614-46615).
- Where applied: **client-side only** (after the response, inside
  `_automationRenderCards()`).

## 8. Shipped-state (cross-cutting, not a page control)

- State: `_shippedState` L20762 — `enabled`, `show_chip`, `filter_default: 'all'`,
  `count_separately`, `counts { prds, automations }`.
- `shippedFilterSelection(statusFilter)` L20977 returns `'all' | 'shipped' |
  'unshipped'` from the Set's contents; consumed by:
  - `_automataQuery` L32720 (→ `shipped=true|false` server param),
  - `automationListVisible` L46595 (→ client-side selection),
  - badge count adjustment `_tagFacetsHTML` L61030-61040.
- `shippedCarriesState(rec)` predicate (near L20773) — non-empty `merge_commit`.

## 9. Non-filters (recorded to avoid false positives)

- `state.activeView`, `state.activeServer`, `state.servers` (L366, L393) — navigation
  and server scope, not state/status filters.
- Gantt `data-state` row attributes (L4819, L4169, L6503) — rendering metadata.
- `state.active` inside the automata events state object (`paintAutomataEvents` at
  L33601) is the panel-active flag, not a filter.
- The dashboard "Live Events" card (L54424) and `_dashFetchDaemonEvents` (L54856)
  are dashboard cards, not page state filters.
- `_tagFacetsHTML` tag/backend chips (L60985-60989) are tag facet filters (client-
  side), not state/status filters; they share the badge bar but a different code
  path (`tagBrowseToggleProject`, backend multi-select).

## 10. Summary table

| # | Page / sub-tab | Control | Selection variable | Render function | Fetch | Where applied |
|---|---|---|---|---|---|---|
| 1 | Projects → Records (plan records) | state `<select> #planRecordsStateFilter` | `_planRecords.stateFilter` (string) | `planRecordsSectionHtml` L5505 | `GET /api/plan-records/{registry-rows\|findings}?state=` or `/search?q=&kind=&state=` (L5550) | **server** |
| 2 | Projects → Plans (plans browser) | state `<select> #plansBrowserState` | `_plansBrowser.stateFilter` (string) | `plansBrowserHTML` L6103 / paint L6120 | `GET /api/projects/{name}/plans` (L5862, no state param) | **client** (`_plansMatch` L5944) |
| 3 | Projects → Records (browse) | kind + state selects | `_recordsTab.kind`, `_recordsTab.stateFilter` | `recordsTabHTML` L6297 / paint L6310 | `GET /api/plan-records/browse?kind=&state=` (L6241) | **server** |
| 4 | Projects → Plans (list HTML) | active-plan state labels | — | `plansBrowserListHTML` (L3867 ctx) | same as #2 | n/a (labels only) |
| 5 | Projects → overview | none (display only) | — | `_projectOverviewSummaryHTML` | `GET /api/projects/{name}/overview-summary` (L5749) | n/a |
| 6 | Projects → gantt | none (display only) | — | Gantt HTML (data-state attrs) | via rollup L6391 | n/a |
| 7 | Automata → events | event-kind chips | `_automataEventsState.kinds` (Set) | `renderAutomataEventsPanel` L33636 / `automataEventsFilterHtml` L33742 | `GET /api/autonomous/event-log?kind=&project=&q=` (L33561) | **server** |
| 8 | Automata → automata (list) | status badge chips + History + search | `_automataState.statusFilter` (Set), `.historyOn`, `.search` | `_tagFacetsHTML` L60983 / `_automataCardsHtml` L32784 | `GET /api/autonomous/prds?status=&active=1&shipped=&q=` (L32712) | **server** + client gate (`_automataBaseList` L32184) |
| 9 | Automata → automation (list) | status badge chips (+search) | `_automationState.statusFilter` (Set, default ≠ cancelled), `.search` | `_tagFacetsHTML` L60983 / `automationListVisible` L46577 | `GET /api/automations` (L46460, no state param) | **client** |
| — | cross-cutting | shipped/unshipped | `_shippedState` (L20762) + `shippedFilterSelection` L20977 | badge bar (L61030-61040) | `shipped=true\|false` (automata list) | server (automata) / client (automation) |
