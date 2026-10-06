# Plan: PWA parity adoption sweep (GH#107, #172, #176, #177, #178, #181, #182) + APNs push (BL335/#107/#158)

- **Date**: 2026-10-05
- **Version at planning**: v8.39.26
- **Status**: In progress.
  - **Phase 0 ✅ complete** (v8.39.27, patch).
  - **Phase 1 ✅ complete** (v8.40.0–v8.44.0, 5 batches: D70/71/72/74,
    D64, D68, D81, GH#182×6). D52, D75, D66's Chrome-badge half, and
    GH#182's deep link re-scoped into Phase 3 (missing backend/
    platform infra, found live). D82 and D66's agent badge already
    shipped elsewhere — no new work needed.
  - **Phase 2 ✅ complete** (v8.45.0–v8.54.0, 10 batches: D76, GH#181,
    D77, D63, D73, D61, D69, D60, D62, D59). D83 and GH#177 already
    shipped elsewhere. D65/D67 still await a GH#172 reply.
  - **Phase 3 in progress**: batch 1 (D75) ✅ shipped v8.55.0 (minor)
    — added the missing `set_permission_mode` backend + PWA picker;
    batch 2 (D66's Chrome badge) ✅ shipped v8.56.0 (minor) — added
    `Session.ChromeEnabled` + PWA badge; batch 3 (D52) ✅ shipped
    v8.57.0 (minor) — added the missing `pause`/`resume` REST +
    Manager/executor backend (new `PRDPaused` status, cooperative
    drain in the dispatch loop); the PWA buttons/strings already
    existed speculatively. Live-verification also caught and fixed 3
    pre-existing PWA gaps where `paused` was missing from the active-
    statuses filter set, the sort-rank map, and the filter badge row.
    Batch 4 (D78) ✅ shipped v8.58.0 (minor) — of D78's 7 sub-items,
    4 (server info, session ring, Ollama, eBPF-degraded banner) were
    already shipped; added the 3 genuinely missing ones: a Backend
    Health card, an Envelopes card, and a quick add-memory input, all
    in the Observer tab. Batch 5 (D80) ✅ shipped v8.59.0 (minor) —
    added subsystem reload (`POST /api/reload?subsystem=…`, previously
    PWA-unreachable), an MCP Channel card in About (mirroring
    Observer's), and an MCP Tools summary card. Batch 6 (D79) ✅
    shipped v8.60.0 (minor) — Config Viewer + raw editor in About,
    reusing `/api/config`'s existing server-side redaction; the editor
    flattens+redaction-filters before saving so an untouched secret is
    never overwritten with the `"***"` placeholder (live-verified).
    Batch 7 (GH#182 DAG card) ✅ shipped v8.61.0 (minor) — hand-rolled
    SVG dependency graph (stories as columns, tasks as rows, curved
    edges for `depends_on`), added to both the Automata list card's
    inline expand and the Automaton detail page's Stories tab.
    **Phase 3 now complete** except GH#182's deep link (blocked on a
    URL-shape decision) and D65/D67 (awaiting a GH#172 reply).
  - **Phase 3 follow-up (datawatch-app's 2026-10-06 re-audit, 20-item
    gap list, operator-approved including the 2 cosmetic items)**: row
    1 (FilesTouched verifier wiring) ✅ v8.61.1; WS liveness watchdog ✅
    v8.61.2–v8.61.3; activity-broadcast-without-transition fix ✅
    v8.61.4; MCP Tools card corrected to match the app's shape ✅
    v8.61.5; remaining brand-casing instances (9 found, 4 more than the
    peer's list) ✅ v8.61.6; row 4 (restart confirm dialog) ✅ v8.61.7;
    row 5 (swipe-to-mute) ✅ v8.61.8; rows 7-9 (Agent/Chrome badges +
    Watch toggle in session detail header, D66a/D61a) ✅ v8.61.9.
    Remaining from the approved 20-item list: #17 (alert badge
    watched-only count, D61a), #18 (per-automaton memory stats on
    Automaton detail, D77a), #19 (server info card, D78a), #20
    (add-memory dialog tags field, D78a), cosmetic #1 (skeleton shimmer
    timing — may already be resolved, app says it adopted the PWA's
    1.4s shimmer; re-confirm with peer), cosmetic #2 (splash status
    granularity). Blocked, no work without further input: #1, #2, #10,
    #16 (no decision made yet) and #6, #11, #12, #15 (D65a/D67a —
    **correction 2026-10-06**: this was never actually blocked on a
    GH#172 reply; GH#172 is confused with an unrelated, already-closed
    datawatch-app#172. The real tracking issues are `dmz006/datawatch`
    #172 and #182 — see the Mobile-Parity Audit note below.)
  - **Mobile-Parity Audit (2026-10-06, operator-triggered)**: this plan
    and `docs/plans/README.md`'s BL396 entry had cited `GH#172`/`#182`
    throughout assuming the `dmz006/datawatch-app` repo; they're
    actually `dmz006/datawatch#172` ("parity: PWA changes from the
    2026-10-04 three-way parity decisions", holds the real D59–D83
    list) and `dmz006/datawatch#182` ("PWA: adopt app features
    (operator decisions 2026-10-05)") — both open, both in *this* repo.
    No commit in the v8.55.0–v8.61.8 run carried the AGENT.md B4
    `mobile-parity:` token or a `rules:` token; going forward every
    commit that ships a D#/GH#182 item gets both, and progress is
    recorded on the two real issues directly (comment + tick box per
    D#, close when each issue's list is done) instead of a
    datawatch-app-side filing. `docs/parity-status.md` is also stale
    (last touched v8.33.32) and needs a refresh pass.
  - **Phase 4** planned, not started.

## Context

The three-way parity audit (PWA ↔ Android ↔ iOS, recorded 2026-10-04 in
`dmz006/datawatch-app`) and a follow-up operator decision pass on
2026-10-05 produced a backlog of concrete, already-decided PWA changes —
not open design questions, just a queue of "the apps had a better idea,
adopt it into the PWA" items, plus one still-missing server capability
(APNs push) that is the single thing fully blocking iOS push end-to-end
(`datawatch-app#185` is explicitly blocked on `#158`). This plan sequences
that queue so it ships in safe, reviewable batches instead of one
giant change, per AGENT.md's Planning Rules (3+ files / non-trivial
work → a dated plan doc with phases).

**Everything in Phases 0–3 is a one-directional parity fix: Android/iOS
already have the feature; the PWA is catching up.** No new datawatch-app
work is implied by those phases. BL335 (Phase 4) is the one item that is
genuinely unbuilt on *any* platform's server dependency — it unblocks the
iOS side, not the other way around.

Confirmed via direct investigation (not re-stating the issues' own text):
- `docs/parity-status.md` (last touched v8.33.32 / 2026-09-17, now stale —
  this plan's items aren't reflected yet) already has the iOS column; it
  needs a refresh pass once Phase 0–4 land, not a structural change.
- `internal/devices/store.go` already has `KindAPNS` in its enum and
  `Valid()` accepts it — `POST /api/devices/register` with `kind=apns`
  already works today. What's missing is *dispatch*: no APNs HTTP/2 call
  exists anywhere, and `ListByKind` (store.go:190) is never called outside
  tests. **There is no FCM function to mirror** — there is no
  Firebase/FCM code in this repo at all (checked: `grep -rln
  "fcm.googleapis\|firebase\|SendFCM"` → empty). All push today is a
  generic UnifiedPush/ntfy webhook POST via `publishToEndpoint`/
  `publishToTopic` (`internal/server/push.go:154-172`) — mirror *that*
  shape (register → store token → dispatch-on-alert), not an FCM pattern.
- No DAG/graph-rendering library or canvas node/edge renderer exists in
  `app.js` (orchestrator graphs today are a flat card list,
  `loadOrchestratorPanel`, app.js:24816-24860) — the Automaton DAG card
  (GH#182) is new UI surface, not a wire-up of something existing.
- No raw-config-viewer UI exists in `app.js` — GH#172 D79 is new surface.
- The Observer panel (`renderObserverView`, app.js:23567+) establishes a
  reusable card pattern (`secContent`-wrapped sections, async `apiFetch`
  into a named `*Block` div) but none of D78's named cards (server info,
  session ring, Ollama, envelopes, backend health, eBPF-degraded banner,
  add-memory) exist under those names today.
- `GET /api/council/runs` and `/api/council/personas` both return **bare
  arrays** (`internal/server/council.go` — "bare array for mobile client
  compat"). `loadCouncilPanel` (app.js:26359-26363) already handles
  `personas` correctly (`data || []`) but still does `(rdata &&
  rdata.runs) || []` for runs, which is always `[]` against a bare array —
  this is GH#178, one line, confirmed as the only call site hitting that
  bug (a near-identical pattern exists for `/api/evals/runs` at
  app.js:26287-26288, outside #178's scope but worth a follow-up note).
- The markdown renderer GH#181 asks for already exists and is already used
  for the Automata spec view — reuse it verbatim for council replies, no
  new renderer needed.
- The inline file-viewer GH#172 D83 asks for (for story/task file chips)
  already exists as `_showFileViewer`/`_fileChip` (built this session for
  SEC-006's nonce-scoped download flow) — reuse it, don't rebuild it.

## Phases

### Phase 0 ✅ Shipped v8.39.27 — zero-design fixes
No UI decisions, no new surfaces, each a single localized change:
- **GH#178** — `loadCouncilPanel` (app.js:26359-26363): change
  `(rdata && rdata.runs) || []` to `Array.isArray(rdata) ? rdata : (rdata
  && rdata.runs) || []`, matching the already-correct `personas` handling
  two lines above.
- **GH#176** — remove the "Updated to vX" splash badge (`app.js` ~316-324,
  the `isNewVersion` → badge div), drop the `status_updated_to` locale key
  from all 5 bundles (`internal/server/web/locales/*.json`). Keep the rest
  of the splash gating (first visit / version change / >24h) untouched.
- **GH#172 D9/D1** — lowercase "datawatch" brand casing: header title,
  `manifest.json` `name`/`short_name`, page `<title>`. Splash already
  lowercase. **Follow-up v8.61.6** (datawatch-app parity re-audit,
  2026-10-06): found 9 remaining capitalized instances this pass
  missed — the input-needed browser notification title, 2 inline
  Settings labels, `api-docs.html`'s `<title>`, the push-notification
  service worker's fallback title, and `tile_voice_label` in all 5
  locale bundles (German's `widget_monitor_label`/`widget_sessions_label`/
  `widget_monitor_description` were also capitalized — not a German
  noun-capitalization requirement, since the rest of the German bundle
  already uses lowercase "datawatch" consistently elsewhere).

### Phase 1 — small, well-specified UI adoptions (existing APIs, no new surface)
Each item is a button/field/badge wired to an API that already exists:
- **GH#172 D64 ✅ shipped v8.41.0** — Council 🎭 badge + filter chip
  (session list/filters).
- **GH#172 D66 (narrowed, checked live 2026-10-05)** — of the two badges
  this item asks for, the agent ⬡ "worker" badge already exists on the
  session list card (`sess.agent_id`, app.js:2549) — nothing left to
  build there. The "Chrome" badge has no backend to read from: `Chrome
  *bool` (`internal/session/manager.go:1508`) is only a session-creation
  option consumed once (`cm.SetChrome(*opt.Chrome)`, line 1912) — it's
  never persisted on the `Session` struct or serialized to JSON, so
  there's no `chrome`/`chrome_enabled` field for the PWA to read after
  creation. Needs a small backend addition (persist + serialize a
  chrome-enabled flag) before the badge is real UI wiring. Moved to
  Phase 3 alongside D52/D75.
- **GH#172 D68 ✅ shipped v8.42.0** — chat quick-reply chips (Yes/No/Stop).
- **GH#172 D70 ✅ shipped v8.40.0** — Alert-rule "Recent Firings" list
  (data already served by the alert-rules REST surface).
- **GH#172 D71 ✅ shipped v8.40.0** — parent-PRD ↗ link on an Automaton
  card.
- **GH#172 D72 ✅ shipped v8.40.0** — inline Reject/Revise buttons on the
  Automaton list card (existing `reject`/`request_revision` endpoints).
- **GH#172 D74 ✅ shipped v8.40.0** — approve-with-note (native prompt +
  existing `approve` call, across all three approve call sites).
- **GH#172 D75 (re-scoped, found live 2026-10-05)** — edit
  `permission_mode` on an Automaton (PWA-only per the issue's own
  decision — apps don't need this one). The PRD model already has a
  `PermissionMode` field, but `grep -n 'case "set_'` in
  `internal/server/autonomous.go` shows every sibling setter (`set_llm`,
  `set_priority`, `set_type`, etc.) except `set_permission_mode` — no
  REST case, no manager setter exists for this field. Same class of gap
  as D52: needs a small backend addition (a `set_permission_mode` REST
  case + manager method) before this is PWA-only UI wiring. Moved to
  Phase 3 alongside D52.
- **GH#172 D81 ✅ shipped v8.43.0** — saved-command library picker in the
  New Session task field (existing saved-commands REST surface).
- **GH#172 D82 (checked live 2026-10-05, already shipped)** — "Resume
  previous session" field on New Session. Already fully built: a
  `resumeSelect` dropdown populated from an "optgroup label='Previous
  sessions'" list, a custom session-ID fallback input, and a separate
  "Restart a previous session" backlog section below the form
  (app.js:5802-5833). No new work needed.
- **GH#182 ✅ shipped v8.44.0 (6 of 7 items)** — '?' help icons on
  Alerts/Dashboard headers; restart confirm dialog (operator-initiated
  only — `triggerAutoRestart()`'s unattended path deliberately stays
  unprompted, see app.js's `confirmRestartDaemon` comment); sessions
  list error banner on unreachable server; filter chip-row expand/
  collapse animation; floating ＋ button on Templates tab (now the
  shared FAB, same as Automata's ⚡ launch button); 3 of the plan's
  originally-named 4 Automata settings fields (`planning_effort`,
  `verification_effort`, `stale_task_seconds`). **Correction found
  live 2026-10-05**: `decomposition_backend`/`decomposition_effort`
  (2 of the plan's original 4 names) are legacy YAML-only aliases —
  `json:"-"` on both (`internal/config/config.go:1610,1620`), accepted
  on read, never written. The current field is `planning_backend`,
  which was **already** on the config card (line ~12470) before this
  batch — nothing to add there. The 7th item, the
  `datawatch://alert/<id>` deep link, is re-scoped below: it's new
  surface, not a wire-up.

### Phase 2 — medium items, bounded but touching more than one file
- **GH#181 ⚠️ partially shipped v8.46.0** — render council persona
  replies/consensus/dissent as markdown, untruncated/collapsible,
  reusing the Automata-spec-view markdown renderer verbatim. Found
  live: the prior "view run" affordance was a raw `alert(JSON.stringify(run))` — not
  truncated text as the issue assumed, no rendering at all. **Correction
  2026-10-06**: this covers the *completed*-run modal only. The *live*
  in-progress run log (`councilOpenLiveWatch`) still appends plain
  `textContent` truncated to 600/400 chars, exactly as GH#181 originally
  described — that surface was never actually touched. Still open on
  GH#181; needs either a live-markdown pass or an operator call that
  the completed-run modal is sufficient.
- **GH#172 D83 (checked live 2026-10-06, already shipped)** — inline
  file viewer for story/task file chips. Already fully wired since
  v8.35.0 (`git log -L` on app.js:10460-10467 confirms): story files,
  task files, and `files_touched` all already render via `_fileChip()`
  → `_showFileViewer()`. No new work needed.
- **GH#172 D76 ✅ shipped v8.45.0** — "repair depends_on" button.
  Confirmed server-side on the REST surface too (not just MCP):
  `case "repair_depends_on":` in `internal/server/autonomous.go:892`.
- **GH#172 D77 ✅ shipped v8.47.0** — memory recall / scopes / lifecycle
  UI. Added an inventory table + cross-layer recall browser + promote,
  reusing the existing `memory_scope_*` REST surface. Found live: the
  recall endpoint returns the same physical row once per layer it
  overlaps with — the new UI dedups before rendering.
- **GH#177 (checked live 2026-10-06, already shipped)** — per-story
  resource bars (CPU%/RSS) + a remote compute-node card (per-GPU util/
  temp/power/VRAM) on Automata detail. Already fully built since
  v8.36.0 (`_loadPRDActiveSessionCard`, app.js:18503+): one card per
  active session with its story/task context line, full CPU/RAM +
  per-GPU util/temp/power/VRAM bars, resolved against that session's
  *actual* remote compute node (`compute_node_ref`) — not a single
  aggregate total as this plan originally assumed. No new work needed.
- **GH#172 D59 ✅ shipped v8.54.0** — Android splash extras (status
  line, "Replay splash"). Replay clears the gating + reloads, reusing
  the real launch splash rather than a separate hand-rolled path.
- **GH#172 D62 ✅ shipped v8.53.0** — swipe-to-mute + muted icon.
  Tap-to-mute instead of swipe (no native gesture layer); gates
  `handleNeedsInput`'s browser Notification + toast, client-side only.
- **GH#172 D60 ✅ shipped v8.52.0** — skeleton shimmer loading list,
  shown on the sessions view while the WS connection is still
  establishing instead of a misleading "No active sessions".
- **GH#172 D69 ✅ shipped v8.51.0** — terminal search/copy. Hand-rolled
  on xterm.js's own core buffer/selection API rather than the official
  search addon, avoiding a new bundled dependency file.
- **GH#172 D73 ✅ shipped v8.49.0** — wizard "memory promote to" field.
  Reads the existing `set_memory_harvest` REST action (BL386 Phase 2);
  fires right after PRD creation, same pattern as the wizard's
  backend/effort follow-up call.
- **GH#172 D61 ✅ shipped v8.50.0** — watch sessions/automata +
  watched-badge filter. Implemented client-side only, same pattern as
  the already-shipped "pin" feature (localStorage, no backend state).
- **GH#172 D63 ✅ shipped v8.48.0** — Whisper 🎤 voice reply in quick
  commands. Added to the sessions-list card's custom-reply field,
  reusing `micButtonHTML`/`startGenericVoiceInput` verbatim (already
  used on 7+ other text fields).
- **GH#172 D65, D67 (comment posted 2026-10-06, awaiting reply)** —
  three-finger swipe-up gesture and "other Android session-detail
  extras" are under-specified for a PWA (no native gesture layer, and
  D67 doesn't name what the extras are) — posted a clarifying comment
  on GH#172 asking for the specific target behaviors before
  implementing, don't guess.
- **GH#172 D52 (re-scoped out of Phase 1, found live 2026-10-05)** —
  originally assumed to be pure UI wiring ("`automataPause`/
  `automataResume` exist in app.js but aren't reachable from the UI").
  Direct investigation found the opposite: `grep -rn "pause\|resume"`
  across `internal/autonomous/` and `internal/server/autonomous.go`
  shows **no `/pause` or `/resume` REST route, no `Pause`/`Resume`
  manager method, and no `paused` PRD status exist anywhere** — the two
  JS functions call endpoints that 404. This needs real backend work
  (a new PRD status, persistence, and executor awareness to skip a
  paused PRD), not a Phase 1 button. Moved to Phase 3 scope.

### Phase 3 — new UI surfaces (need their own small design pass, no existing pattern to copy exactly)
- **GH#182 ✅ shipped v8.61.0** — Automaton DAG card. Built the
  hand-rolled layout as planned — stories as columns, tasks as rows,
  curved SVG paths for dependency edges — rather than pulling in a
  dependency like dagre/cytoscape, consistent with the PWA's current
  zero-heavy-dependency footprint. Edges cover both `Task.DependsOn`
  (resolved directly by task id) and `Story.DependsOn` (anchored to
  each story's first task, since a story itself isn't a drawable node).
  Added in two places: the Automata list card's inline "Stories &
  tasks" expand (`renderAutomataCard`/`renderDetailStoriesTree`) and
  the Automaton detail page's Stories tab (`_renderDetailStories`) —
  found live, mid-implementation, that these are two genuinely separate
  render paths (the first version only reached the former; the detail
  page people actually click into uses the latter). Live-verified
  against a real daemon with a 3-story, 6-task PRD carrying both
  task-level and story-level dependencies: correct column layout,
  status-colored nodes, and curved edges for every dependency.
- **GH#172 D78 ✅ shipped v8.58.0** — Android-only Observer cards (server
  info, session ring + `max_sessions`, Ollama, envelopes, backend
  health, eBPF-degraded banner, add-memory), following the existing
  `renderObserverView` card pattern (`secContent` + async `apiFetch`
  into a named block). Direct investigation found 4 of the 7 sub-items
  already shipped: server info (Daemon + Infrastructure stat-cards),
  session ring + `max_sessions` (the existing conic-gradient donut in
  the Session Statistics section), Ollama (a full "Ollama Server"
  stat-card with running-model/VRAM breakdown), and the eBPF-degraded
  banner (already shown when `ebpf_enabled && !ebpf_active`). Built the
  3 genuinely missing ones: a **Backend Health** card (`/api/backends`,
  previously only consumed for session-create picker filtering, never
  shown to the operator), an **Envelopes** card (`/api/observer/envelopes`
  had a full REST surface with no PWA consumer anywhere), and a
  **quick add-memory** input in the Memory Browser section
  (`/api/memory/save`, previously only reachable via MCP/CLI).
- **GH#172 D79 ✅ shipped v8.60.0** — Config Viewer + raw config editor.
  Found `GET /api/config` already redacts every secret server-side
  (the existing `mask()` helper in `handleGetConfig` — same SEC-014
  spirit, already comprehensive) so the viewer itself needed no new
  redaction logic. The real risk was the editor: `PUT /api/config`
  expects flat dotted keys (`"ntfy.token"`), not the nested shape GET
  returns, and naively round-tripping GET's JSON back through PUT
  would either no-op silently (shape mismatch) or, after a proper
  flatten, overwrite every untouched secret with the literal `"***"`
  placeholder. Fixed by flattening client-side and dropping any leaf
  still equal to `"***"` before sending the patch — PUT leaves an
  omitted key's existing value untouched, same round-trip safety as
  the federation peer/server token `Redacted()` pattern. Live-verified
  against a real daemon: seeded a real secret, edited an unrelated
  field via the PWA, confirmed the real secret was unchanged on disk
  afterward. 6 new unit tests pin the flatten/redaction-safety logic.
- **GH#172 D80 ✅ shipped v8.59.0** — subsystem reload + MCP
  channel/tools cards in About. Found only 3 registered hot-reload
  subsystems (`config`, `filters`, `memory`, via `RegisterReloader` in
  `cmd/datawatch/main.go`) behind an existing `POST
  /api/reload?subsystem=…` endpoint with zero PWA consumers — added a
  dropdown + Reload button + applied/requires-restart result display.
  The MCP channel bridge card already existed on Observer
  (`loadChannelBridge`/`channelBridgeStatus`); generalized it to take
  an optional target element id and added a second instance in About
  for Android parity. Added an MCP Tools summary card alongside the
  existing raw JSON/HTML export links, which only served the scripting
  use case. **Corrected v8.61.5** per the datawatch-app session
  (operator decision D80a: the app is the reference for this specific
  card) — rebuilt to source `/api/mcp/docs` (name + description per
  tool, grouped by category when present) instead of the originally-
  used `/api/mcp/tools` (bare name + annotations, no description).
- **GH#172 D52 ✅ shipped v8.57.0** (re-scoped from Phase 1, see note
  above) — added the `PRDPaused` status, `POST
  /api/autonomous/prds/{id}/pause|resume` REST actions, `Manager.Pause`/
  `Resume` (persisting the status + a decision record), and a
  cooperative-drain check in the executor's dispatch loop (same drain
  pattern used for `PRDBlocked`: let in-flight tasks finish, skip
  `autoFailDeps`/`launch`, don't touch the status the pause already
  set). `API.Resume` reuses `API.Run`'s existing goroutine-launch/
  idempotency machinery rather than duplicating it. The already-existing
  `automataPause`/`automataResume` JS functions now hit real endpoints
  instead of 404ing; added visible Pause/Resume buttons to the Automata
  card action row, gated on status. Live-verification (a real daemon +
  Playwright) caught 3 pre-existing PWA bugs unrelated to this feature's
  own code: `paused` was missing from `_AUTOMATA_ACTIVE_STATUSES` (so a
  paused Automaton vanished from the default list entirely), from
  `_AUTOMATA_STATE_RANK` (sorted last instead of needs-attention
  priority), and from the status-filter badge row — all fixed, plus a
  `paused` card border color and filter-badge color.
- **GH#172 D75 ✅ shipped v8.55.0** (re-scoped from Phase 1, see note
  above) — added `Manager.SetPermissionMode` + `set_permission_mode`
  REST case (mirroring `set_llm`/`set_memory_harvest`), then the PWA
  "Permission" picker on the Automaton detail view.
- **GH#172 D66's Chrome badge ✅ shipped v8.56.0** (re-scoped from
  Phase 1, see note above) — added `Session.ChromeEnabled` (set at
  creation, gated on the same backend-supports-SetChrome check), then
  the PWA badge.
- **GH#182's `datawatch://alert/<id>` deep link** (re-scoped from Phase
  1, found live 2026-10-05) — checked for existing infrastructure to
  wire this onto and found none: `manifest.json` has no
  `protocol_handlers` entry (the standard way a PWA registers as a
  custom-scheme handler — and per spec would need a `web+` prefix, e.g.
  `web+datawatch://`, since bare custom schemes are native-app-only),
  `sw.js`'s `notificationclick` unconditionally opens `/` with no
  `data`/URL passed through from the push payload, and `app.js` has no
  `URLSearchParams`/`location.hash` parsing anywhere at page-load time
  for routing. This needs a real design decision (what URL shape the
  PWA actually receives the link as, end to end from push payload to
  route) before it's buildable — not a Phase 1 "existing API" wire-up.

### Phase 4 — APNs push (BL335 / GH#107 / GH#158) — independent backend track, can run in parallel with Phases 0–3
This is the one item blocking an entire platform's push notifications
(`datawatch-app#185`). Per BL335's existing spec in
`docs/plans/README.md` and `docs/parity-status.md`'s "APNs Server Work"
section:
1. `internal/config/config.go` — extend `PushConfig` with `apns.key_id`,
   `apns.team_id`, `apns.bundle_id`, `apns.key_path`.
2. `internal/server/push.go` — new APNs dispatch function alongside
   `publishToEndpoint`/`publishToTopic`: JWT (ES256, signed with the `.p8`
   key) auth per Apple's APNs provider API, HTTP/2 POST to
   `api.push.apple.com`, payload `{"aps":{"alert":{...},
   "content-available":1,"badge":N},"sessionId":...,"type":...}`.
3. Wire the dispatch into the existing alert-fire path, filtered on
   `device.Kind == devices.KindAPNS` (enum already exists;
   `ListByKind` already exists but is called nowhere outside tests — wire
   it in here).
4. 7-surface parity (BL335's own requirement): REST (device registration
   already accepts `kind=apns`) + MCP + CLI + comm + PWA (device
   management settings surfaces an APNs-registered badge) + YAML config
   for the 4 new `push.apns.*` fields.
5. Docs: `docs/config-reference.yaml` new fields; flip
   `docs/parity-status.md`'s "APNs send — ❌ Pending" row to ✅ once
   shipped.

## Parity surface

This plan is *about* parity, so the surface framing is inverted from a
normal feature plan:
- **Phases 0–3**: direction is Android/iOS → PWA. Android and iOS already
  have every one of these behaviors; implementing them closes the PWA gap.
  No new datawatch-app work is implied. REST/MCP/CLI/comm/YAML are
  unaffected — these are PWA-only UI changes (none of D59-D83, #182, #176,
  #178, #181, #177 change an API contract).
- **Phase 4 (BL335/APNs)**: REST, MCP, CLI, comm, YAML, PWA all get the
  new `push.apns.*` surface per item 4 above. Android is unaffected (it
  already has FCM). iOS is the platform this unblocks, but the native iOS
  client code itself lives in `dmz006/datawatch-app` and is out of scope
  here — only the server-side dispatch this plan builds.

## Out of scope (cross-repo / explicitly deferred)
- Any `dmz006/datawatch-app` (Android/iOS) code — this plan is
  datawatch-server/PWA-only; the apps already have the behaviors being
  adopted.
- GH#4 and GH#107's "parity tracking doc" ask beyond a refresh pass — #4
  is a standing process umbrella (no code), and #107's doc-structure ask
  is already satisfied (iOS column exists); only its APNs ask (Phase 4)
  is still open.
- GH#172 D65/D67 — deferred pending clarification (see Phase 2 note).

## Files (representative, not exhaustive — most items touch only these two)
- `internal/server/web/app.js` — nearly every Phase 0-3 item.
- `internal/server/web/locales/*.json` — any item adding/removing a
  user-facing string (Phase 0's badge removal, new labels in Phases 1-3),
  per AGENT.md's Localization Rule (5 bundles + `v5280_locales_test.go`
  `mustHave` update for high-visibility keys).
- `internal/config/config.go`, `internal/server/push.go`,
  `internal/devices/store.go` — Phase 4 only.
- `docs/parity-status.md`, `docs/plans/README.md` (new BL-number for this
  sweep; BL335 already exists and is reused for Phase 4) — updated as
  phases ship, not written upfront.

## Verification
- **Every phase, no exceptions (added after an operator check 2026-10-05
  — Phase 0 initially shipped on JS-unit-tests alone, which is not
  sufficient on its own for a UI change): start a real daemon + open the
  actual PWA in a browser (Playwright/`chromium-cli` per the `run` skill)
  and click through every new/changed affordance before calling the
  phase done** — a passing jsdom-style unit test proves the function's
  logic is correct in isolation, not that the button renders, is
  reachable, and does the right thing in the real page. Screenshot or
  describe what was actually clicked and observed in the commit/report,
  not just "tests pass."
- Each phase: `node --test internal/server/web/*.test.js` (existing
  escaping/XSS/prototype-pollution guards must keep passing — any new
  `innerHTML` assembly in Phases 1-3 should go through the existing
  `escHtml`/`DOMPurify` patterns, not raw interpolation).
- Phase 0's #178 fix: add a regression test asserting
  `_renderCouncilPanel`'s runs argument is non-empty when `/api/council/
  runs` returns a bare array (confirmed-fails-without-fix pattern used
  throughout this session).
- Phase 4: new unit tests for the APNs JWT signing + payload shape
  (mirroring how `internal/server/push_test.go` tests the existing
  webhook dispatch), plus a `docs/testing-tracker.md` entry (new endpoint
  surface) and a manual smoke against a real `.p8` key in sandbox before
  calling it shipped — do not mark iOS push "done" on unit tests alone,
  per this session's own standard of live-verifying security/capability
  surfaces.
- Mobile-Parity Rule audit (AGENT.md): since Phases 0-3 are themselves
  *closing* parity gaps (not opening new ones), no new datawatch-app issue
  is filed for them — comment on/close the originating issue (#172's
  checkboxes, #182, #176, #178, #181, #177) as each ships instead.
- Before tagging any release that includes Phase 4: confirm `docs/parity-
  status.md`'s APNs row and GH#107/#158 are updated together — don't let
  the doc drift stale again (it was 6 minor versions behind at the start
  of this plan).
