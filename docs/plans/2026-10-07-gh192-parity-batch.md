# GH#192 — 8-item Android/iOS parity batch (PWA)

**Status**: Phase 1 ✅ shipped v8.70.0 (#8, #1, #3, #5, #6). Phase 2 ✅ shipped v8.71.0 (#2). Phases 3-4 (#7, #4) in progress.

Operator-approved, filed by the datawatch-app peer session. Full issue body
and explicit exclusions are in GH#192 itself; not duplicated here. This plan
covers investigation findings, phasing, and the concrete implementation shape
for each item, per AGENT.md's planning rule for multi-item batches.

Researched 2026-10-07 before writing this plan. Two items turned out to need
an operator decision beyond what the issue text assumed — both resolved via
AskUserQuestion before this doc was written; decisions recorded inline below.

## Phasing

Per the operator's instruction: data-only/existing-API items first, new-UI-
surface items last, riskiest (schema change) last of all.

1. **Phase 1 — data-only / existing API, PWA-only or small Go addition**: #8, #1, #3, #5, #6
2. **Phase 2 — new UI surface, zero backend risk**: #2 (reuses existing BL386 endpoints)
3. **Phase 3 — new UI surface, bigger**: #7 (picker modal — operator confirmed this reverses a prior deliberate decision)
4. **Phase 4 — full-stack, schema change, highest risk**: #4 (memory tags — operator confirmed full end-to-end scope)

Each phase ships as its own commit (or a few, if a phase's items are large
enough to separate), with full go/js test suites, docs/testing-tracker.md,
docs/plans/README.md, CHANGELOG.md, version bump (api.go + main.go in
lockstep), and a daemon redeploy — same discipline as the BL316/317/318 pass.

---

## #8 — Agent badge shows the agent id (D66a)

**Current**: `internal/server/web/app.js` renders `⬡ worker` with the real
`agent_id` only in the `title` tooltip (two call sites: Sessions-list card
~line 2747, session-detail header ~line 3212). Tooltips don't work on phones.

**Fix**: both sites become `⬡ ${escHtml(sess.agent_id)}` directly; keep the
`title` attribute too (harmless on desktop, no-op on phones).

**Risk**: none. Pure text change, 2 call sites.

---

## #1 — Watched-only alert badge (D61a)

**Current**: `updateAlertBadge()` always shows `state.alertUnread` (a flat
total unread count, incremented in `handleAlert()` on every WS push, seeded
from `GET /api/alerts`'s `unread_count` field at load). There is no per-
session breakdown tracked client-side.

**"the watched filter"**: the only existing global "watched" concept is
`state.sessionWatchFilter` (boolean, Sessions view's own toggle) +
`state.watchedSessions` (a `Set` of session IDs, already shared/global
state used by Sessions and session-detail). Alerts has no watch-filter of
its own — the issue's "the watched filter" must mean this one, the only
candidate.

**Fix**: track a second running counter, `state.alertWatchedUnread`,
alongside the existing one:
- Seed it at load (`GET /api/alerts` already returns the full `alerts`
  array, each with `session_id` + `read` — filter client-side:
  `!a.read && a.session_id && watchedSessions.has(a.session_id)`).
- Increment it in `handleAlert(a)` alongside `alertUnread`, gated on
  `watchedSessions.has(a.session_id)`. System alerts (no `session_id`)
  never count toward it — they aren't "a watched session."
- Reset both counters together wherever `alertUnread`/`alertSystemUnread`
  are reset today (renderAlertsView's mark-all-read path).
- `updateAlertBadge()` picks `state.sessionWatchFilter ? alertWatchedUnread
  : alertUnread`.

**Known limitation** (documented, not fixed): toggling a session's watched
status doesn't retroactively reclassify already-counted alerts — consistent
with the existing architecture (flat running counters, not a stored list).
Acceptable for a glance-indicator badge; noted in the changelog.

**Risk**: low. No backend change.

---

## #3 — Observer server info (D78a)

**Current**: Observer's "System Statistics" panel (`renderStatsData`,
app.js ~22365) has "Daemon" and "Infrastructure" `stat-card`s — neither
shows hostname or daemon version. `internal/stats.SystemStats` (the
`/api/stats` response struct) has no `Hostname`/`DaemonVersion` fields at
all — this needs a small server-side addition, not just a PWA display fix.

**Fix**:
- `internal/stats/collector.go`: add `Hostname string` + `DaemonVersion
  string` (`json:"hostname,omitempty"`/`"daemon_version,omitempty"`) to
  `SystemStats`, in the existing "Server interfaces (for infrastructure
  card)" field group. New `(c *Collector) SetServerIdentity(hostname,
  version string)` setter, mirroring the existing `SetServerInterfaces`
  pattern exactly; populate the two new fields in `Collect()`/`Latest()`
  the same way `WebPort`/`TLSEnabled` already are.
- `cmd/datawatch/main.go`: call `statsCollector.SetServerIdentity(hostname,
  Version)` right next to the existing `SetServerInterfaces(...)` call
  (~line 5700) — both vars already in scope there.
- `app.js`'s `renderStatsData`: show hostname in the Daemon card, daemon
  version in the Infrastructure card (matching the apps' Daemon/
  Infrastructure split per the issue).

**Risk**: low. Small, additive, precedented (same shape as every other
`Set*` collector setter already in the file).

---

## #5 — Connect splash two-stage text + min/max dwell (D10a)

**Current** (`app.js` ~3280-3470): the terminal-loading splash
(`#termLoadingSplash`) shows a single string ("Connecting to session…" /
"Reconnecting to session…" on retry) and is removed the instant the first
`pane_capture` frame arrives (`state._termHasContent = true`, splash
`.remove()`), with no minimum dwell at all — can flash for under 100ms on
a fast reconnect. A separate retry watchdog (`startTermConnectWatchdog`)
already exists: 3 retries × 5000ms = 15000ms before showing a hard
"Unable to connect" error with a Retry button.

**Why new vs. existing differ (2s/15s vs 0.5s/8s)**: a brand-new session
needs tmux to cold-start before any `pane_capture` can even be requested —
higher minimum (no point flashing <2s) and a more generous ceiling (15s)
before giving up. A reconnect to an already-running session should be much
faster on both ends.

**Design decision** (resolved by reasoning through the existing watchdog
interaction, not re-asked): `maxDwell` is **the total failure budget**
before the existing error/Retry UI shows — it replaces the current fixed
`3×5000ms` with a type-aware total (15000ms new / 8000ms existing), it does
**not** silently hide the splash and reveal a blank terminal with no error
path. This preserves the valuable "eventually show Retry" behavior instead
of regressing to a silent hang for a genuinely broken existing session.
`minDwell` is a floor: even if `pane_capture` arrives instantly, the splash
stays until `minDwell` has elapsed, so it can't flash away in a handful of
milliseconds.

**"new" vs. "existing" session detection**: no such flag exists today.
Add `state._justStartedSessionId` set right after a session (1) is freshly
started (`apiFetch('/api/sessions/start', ...)`'s `.then` — the one that
calls `navigate('session-detail', sess.full_id)`, ~line 6930) or (2)
restarted in place (`_doRestartSession`'s `.then`, ~line 4832) — both are a
cold tmux pane from the terminal's perspective. Consumed (read once, then
cleared) in the splash-mount code so later re-renders of the same session
don't keep treating it as "new."

**Two-stage text**: stage 1 "Connecting…" shown immediately (reusing
`term_connecting`); after a short delay (e.g. 1200ms) with no frame yet,
switch to stage 2 "Waiting for terminal…" (new i18n key `term_waiting`).
This is purely a `setTimeout` text swap, independent of the dwell/watchdog
timers.

**Risk**: medium — touches 3 timers (min-dwell gate on the success path,
the existing watchdog's now-parameterized total, and the new stage-2 text
delay) interacting with the terminal-mount lifecycle. Needs careful testing
of each permutation (fast local reconnect vs. slow cold start vs. genuine
failure).

---

## #6 — Header "refreshing" spinner

**Current**: no global refresh indicator exists; each view's own card-level
`loadingEyeBlock()` is the only in-flight signal, scoped to that card.

**Scope decision** (not re-asked — low-stakes, reversible UI nicety):
"matching the apps" can't be verified against the Android/iOS source from
here, so this implements the most useful, clearly-bounded reading: a small
spinner appears in the header actions row for the duration of the *initial*
data fetch each time `navigate(view)` switches to a new view — not during
every background poll (which would make it almost permanently visible and
defeat the purpose).

**Fix**: new `#headerRefreshSpinner` element in `index.html`'s header,
hidden by default, next to the existing icon buttons. Two small global
helpers, `_showHeaderRefreshSpinner()` / `_hideHeaderRefreshSpinner()`.
`navigate(view)` calls show before dispatching to the view's render
function and the render function's own first-fetch `.finally()` calls hide
— wired at the handful of view-render functions that do an initial fetch
(Sessions, Alerts, Automata/PRD list, Dashboard, Observer; same set that
already has the server-picker bar).

**Risk**: low-medium. Touches multiple view-render functions' fetch
chains, but additively (one show call, one hide call each) — no existing
logic changes.

---

## #2 — Per-Automaton memory section (D77a)

**Current**: this entire feature (stats tile, memory report, scoped recall)
only exists in Observer (`loadMemoryScopeInventory`/`memoryScopeRecall`,
app.js ~15584-15667), which is project-wide and requires the operator to
type IDs into a generic form.

**Server-side check (per the issue's own instruction) — already exists,
no backend change needed**:
- `GET /api/autonomous/prds/{id}/memory-report` (BL386 Phase 4,
  `internal/server/autonomous.go:1637`) already aggregates memories across
  prd-shared + story-shared (+ optional session-local) scopes for a given
  PRD, deduplicated, grouped by scope — exactly the "memory report" the
  issue asks for.
- `GET /api/memory/scopes/recall?prd_id=X&project=Y` (used by Observer's
  own `memoryScopeRecall()`) already walks the scope hierarchy down from a
  specific `prd_id` — exactly the "recall search scoped to that Automaton"
  the issue asks for (it's a structural/scope-filtered walk, not a
  free-text search — matching what Observer already offers; the REST
  handler has no `q`/free-text param to add).

**Fix (PWA-only)**: new collapsible section on the Automaton/PRD detail
view (`renderPRDRow`'s expanded detail, or wherever the detail body
renders — confirm exact mount point when implementing):
- Stats tile: counts derived from the memory-report response, grouped by
  `scope` (prd-shared count, story-shared count summed across all stories,
  session-local count when included).
- Memory report: the same response rendered as a list (reusing the
  existing memory-row markup style from `listMemories()`).
- Scoped recall: the same `memoryScopeRecall()` REST call, but pre-filled
  and **locked** to this PRD's `prd_id` + `project_dir` (no generic ID-
  typing form — the whole point of scoping it to "this Automaton").

**Risk**: low. No backend change; new PWA section reusing existing,
already-tested endpoints and existing row-rendering patterns.

---

## #7 — Three-finger swipe opens a real picker (D65a)

**Current** (app.js ~23334-23387, `initThreeFingerSwipeGesture`): a
*deliberate* prior decision — the PWA's server picker is an always-visible
toolbar bar (decision D2a), not a hidden dialog like the apps, so the
gesture scrolls to top + highlights the bar for 1.2s instead of "opening"
anything. With zero remote servers configured, `_serverPickerBar()` returns
`''` and the gesture no-ops entirely.

**Operator decision**: reverse this — build a real modal/dropdown overlay
that the gesture opens, matching the apps, rather than extending the
scroll+highlight approach.

**Fix**:
- New modal (reuses the existing `showConfirmModal`-style overlay
  mechanics, not a new modal framework): server list (All/Local/named,
  same `selectServer(name)` actions as the toolbar bar) + a "+ Add server"
  action that opens the existing Settings → Remote Servers add-server
  form/modal (reuse, don't rebuild).
- `initThreeFingerSwipeGesture`'s trigger calls a new `openServerPickerModal()`
  instead of `highlightServerPicker()`. Works even with zero configured
  servers (shows just "Local" + "+ Add server" — no more no-op).
- The existing always-visible toolbar bar (`_serverPickerBar`/
  `_injectServerPickerBar`) is **not removed** — it stays as the primary,
  discoverable picker for mouse/trackpad use; the gesture is a phone-
  specific shortcut to the same action set, now in a real overlay instead
  of a scroll+pulse.

**Risk**: medium. New UI surface (a modal), but built from existing pieces
(selectServer, the add-server form) rather than new mechanics.

---

## #4 — Tags on Add memory (D78a)

**Current**: `addMemoryQuick()` (app.js ~15512) sends `{content}` only.
**Checked per the issue's own instruction — the assumption was wrong**:
there is no tags concept anywhere in the memory system. Not in the SQLite
schema (`internal/memory/store.go`'s `memories` table), not in the
`Memory` struct, not in any of `Remember()`'s 2 real call chains, not in
the REST handler (`handleMemorySave` decodes only `Content`/`ProjectDir`),
not in the MCP `memory_remember` tool (`text`/`project_dir` only).

**Operator decision**: full end-to-end support, not a front-end-only field.

**Fix shape** — chosen to avoid touching the existing `Save`/`SaveWithMeta`/
`SaveWithNamespace`/`SaveWithNamespaceAndSource` call chain (heavily used,
many existing callers) at all. Mirrors the **existing pinning precedent**
exactly (`SetPinned`/`PinnableBackend`/`ErrNamespaceUnsupported` — added
once already for Mempalace QW#2, same shape needed here):

1. `internal/memory/store.go`: `ALTER TABLE memories ADD COLUMN tags TEXT
   DEFAULT ''` + index, following the exact established migration pattern
   (there are 8 prior examples in this same function to copy). `Memory.Tags
   string` field (`json:"tags,omitempty"`). New `(s *Store) SetTags(id
   int64, tags string) error`, mirroring `SetPinned` exactly. Add `tags` to
   the SELECT + Scan in the 3 read paths the PWA's existing memory browser
   actually uses — `ListRecent`, `ListFiltered`, `Search` — not all ~28
   Memory-returning methods in the file (scope boundary: tags need to
   persist and be visible where the operator already looks, not retrofit
   every internal query path).
2. `internal/memory/backend.go`: new `TaggableBackend` interface (`SetTags(id
   int64, tags string) error`), mirroring `PinnableBackend` — the Postgres
   backend simply won't implement it, degrading to `ErrNamespaceUnsupported`
   exactly like pinning already does there.
3. `internal/memory/api_adapter.go`: `(a *ServerAdapter) SetTags(...)` —
   interface-cast-and-delegate, mirroring `ServerAdapter.SetPinned` exactly.
   `ServerAdapter` is confirmed the only concrete type ever wired as both
   `server.MemoryAPI` and `mcp.MemoryMCP` (checked both wiring call sites in
   `main.go`), so this one implementation covers both surfaces.
4. `internal/server/api.go`: add `SetTags(id int64, tags string) error` to
   the `MemoryAPI` interface. `handleMemorySave` decodes an optional `Tags
   string` field; after a successful `Remember()`, best-effort calls
   `SetTags` (an unsupported-backend error doesn't fail the save — the
   memory itself still saved correctly, same graceful-degradation standard
   as pinning).
5. `internal/mcp/server.go`: add `SetTags` to the `MemoryMCP` interface.
   `internal/mcp/memory_tools.go`: `toolMemoryRemember()` gets an optional
   `tags` string param; `handleMemoryRemember` calls `SetTags` the same
   best-effort way after `Remember()` succeeds.
6. PWA: `addMemoryQuick()` gets a tags input (comma-separated, matching the
   plain-TEXT-column storage shape) and sends `tags` in the POST body.
   `listMemories()`/`searchMemories()` render a small tag-chip row per
   memory when `m.tags` is non-empty (now returned by the 3 read paths
   updated in step 1).

**Risk**: highest of the 8 — only item touching the DB schema and 2
interface definitions. Mitigated by mirroring an already-shipped,
already-tested precedent (pinning) shape-for-shape rather than inventing a
new pattern, and by not touching any existing `Save*`/`Remember` call
signature (purely additive new methods).

---

## Explicitly not in scope (operator decision 2026-10-07, do not touch)

- Remembered-output-tab difference (web UI keeps resetting to Tmux) —
  accepted as-is.
- Error banner colour — apps are adopting the web UI's
  `rgba(239,68,68,0.12)`, not the reverse. No PWA change.
