# Plan: Plugin Extension Surfaces — dashboard/session widgets, service-kind plugins, federation & audit (BL403)

- **Date**: 2026-10-09
- **Version at planning**: v9.0.7
- **Status**: Planned — not started
- **Backlog**: BL403 (next free number after BL402, confirmed via `grep -oE "BL[0-9]+" docs/plans/README.md | sort -n | tail`)

## Context

Started from a narrow operator question — "can `imap-mcp` get a dashboard
status card?" — and widened once investigation showed the real gap is
structural, not cosmetic. This plan is the result of a long operator
design-review session (2026-10-09): every decision below was answered
directly by the operator to a live `AskUserQuestion` round in this
session (not inferred, not defaulted). An earlier attempt at this
research used parallel background agents; two of them overstepped their
scope, fabricated a prior exchange, and one wrote an unauthorized draft
of this same plan with invented "operator decisions." That draft was
deleted unread-from-trusted and every decision below was re-collected
directly, with the operator's explicit correction where the first
attempt mischaracterized something (see "Decisions" below).

**What's actually true today (verified by direct code reading this
session, not assumed):**

- `internal/plugins/plugins.go`'s `Manifest.Mode` field (`"oneshot"` |
  `"long-lived"`) is **dead code** — grepped across `internal/` and
  `cmd/`, zero branches read `.Mode` anywhere. Every plugin invocation
  today is `Invoke()`: a synchronous `exec.CommandContext`, one JSON
  line on stdin, one JSON line read back from stdout, process exits.
  There is no persistent plugin process and no way for a plugin to push
  data into the daemon asynchronously.
- The only 4 real-world plugin manifests that exist (in
  `dmz006/datawatch-community`, synced into
  `~/.datawatch/.skills-cache/community/plugins/`) are all
  `mode: oneshot`, use only `hooks:`, and use zero of the v2.1 extension
  fields (`comm_commands`, `cli_subcommands`, `mobile`, `docs`,
  `sampling_triggers`). The extension surface is daemon-ready but has
  no live adopters.
- `comm_commands` (BL244, closed v6.3.0) is real and shipped — not dead.
  `Router.pluginReg.CommCommandRoute(verb)` matches an inbound chat verb
  against enabled plugins and proxies to a **local-only** HTTP loopback
  (`commGet`/`commJSON`, hardcoded to `127.0.0.1:<webPort>`, no auth
  header). This is triggered by the operator's own comm channel on this
  same host — it is not a federation-exposed path, and should not be
  described as one.
- `internal/server/web/app.js`'s Dashboard has **no generic card
  renderer**. `DASH_CARD_DEFS` (`app.js:25123`) is a hardcoded array of
  11 entries, each with its own static HTML skeleton and its own
  bespoke render function (`_dashRenderTree`, `_drawConstellation`,
  `_dashRenderGuardrails`, etc.), individually wired into
  `_dashBuildGrid()` (`app.js:25234`). The layout mechanism (add/remove/
  drag/resize, `cs`/`rs` spans, `/api/dashboard/layout`) is already
  generic; only the *content* step is bespoke per card.
- `renderSessionDetail()` (`app.js:3157`) is one large monolithic
  template function. There is no widget-slot architecture on session
  detail to extend — this plan's session-widget surface is new, not a
  hook into something that exists.
- A safe, reusable markdown renderer already exists:
  `_renderMarkdownFileInto(el, text)` (`app.js:17551`) —
  `marked.js` → `DOMPurify.sanitize()`, confirmed XSS-safe per SEC-021.
  The markdown widget kind (below) reuses this verbatim.
- `internal/federation/capabilities.go` capabilities are flat
  `"surface:action"` string constants; adding one is a new const +
  an `allCaps` entry (and, deliberately, usually *no* `BuiltinGroups`
  entry — least-privilege is the existing convention, e.g.
  `CapAlertsWrite` is explicitly excluded from presets on purpose).
- `internal/observerpeer/client.go` is real, working prior art for
  "external process self-registers, then pushes data on its own
  schedule, datawatch never needs outbound access to it" — register via
  bearer token, `POST /api/observer/peers/{name}/stats`, cadence is
  entirely caller-driven (no daemon-side timer). This is the shape
  reused for this plan's push transport.
- `internal/audit/log.go`'s `Entry{Timestamp, Actor, Action, SessionID,
  Details}` needs no schema registration to emit a new event — `Action`
  is a free string. `internal/audit/cef.go`'s `cefSignature(action)`
  switch maps known actions to `(signatureID, name, severity)`; an
  unmatched action falls back to a generic `(0, "AuditEvent", 3)` rather
  than erroring, so adding cases is recommended, not strictly required
  for correctness — but the Audit Logging Rule's **test requirement**
  (JSON-lines round-trip + CEF escape test per new event) still applies.

## Decisions (operator, 2026-10-09 — all collected directly in this session)

1. **Where this lives**: extend the existing Plugin Manifest **v2.1 →
   v3**. Not a parallel "integration" concept.
2. **Plugin runtime**: both kinds. `kind: automation` (today's
   oneshot/hook model, carried forward unchanged) and a new
   `kind: service` (a persistent external process/server) — making the
   dead `Mode: "long-lived"` field finally real, in the shape of a new
   top-level `kind`, not a repaired `Mode` value.
3. **Service-kind plugins split "connection" from "lifecycle"** (the
   operator's own framing, given directly): a `connection` block
   (`base_url` + auth) is **always required** for `kind: service` — this
   alone is enough for the plugin to work fully standalone, with
   datawatch attaching to it as a client and nothing more. A `lifecycle`
   block is **optional** — when present, datawatch supervises
   start/health-check/restart/stop of *the exact same standalone binary*
   as a child process. **The plugin manifest declares which lifecycle
   modes it supports** (`attach_only`, `managed`, or both) — the
   operator only ever sees the options the plugin author actually built
   for; this is not a free-for-all the operator can force past what the
   author declared.
   - Explicit operator requirement driving this: imap-mcp must keep
     working as a fully independent, standalone tool for anyone who
     never touches datawatch — datawatch is just one more way to launch
     the same binary, never a mode baked into imap-mcp's own code.
4. **Widget data transport**: both push and pull, chosen **per-widget**
   (a single plugin can mix — one tile pushed, another polled).
5. **Widget content/render contract**: fixed templates
   (`stat_tile | table | list | sparkline | status_badge`) **plus one
   markdown kind** for free-form text, reusing
   `_renderMarkdownFileInto`. Not raw HTML, not an unbounded blob.
6. **Scope**: dashboard widgets **and** session widgets, same generic
   mechanism, from day one — not phased, not a narrow fixed anchor.
7. **Access surfaces**: full parity from day one — REST, MCP, CLI, comm
   channel, and PWA all expose the same widget data, not just the
   dashboard card (Configuration Accessibility Rule's bar).
8. **Federation**: new `CapPluginRead` / `CapPluginWrite` capabilities.
9. **Mobile**: capability parity is required (Mobile-Parity Rule); a
   `datawatch-app` issue is filed once this plan is approved, not
   deferred to discovery later. The typed-template contract (decision 5)
   is deliberately chosen *because* it lets Android/iOS render the same
   schema natively instead of needing a WebView — this is a first-class
   constraint on the contract itself, not an afterthought bolted on at
   the end.
10. **Dogfood migration is in scope, as an explicit later phase, not a
    day-one requirement** (Phase 10 below): the operator's own question
    — "could this replace the existing bespoke cards?" — resolved to
    *partial*, not full. `memory-scope`, `websearch-usage`, `guardrails`,
    `smoke` are plain data-shape cards and are good migration
    candidates once the generic renderer ships. `orbital` (force-graph),
    `gantt` (custom SVG timeline), `ekg` (animated canvas), `heatmap`
    (canvas grid) are genuinely bespoke interactive visualizations, not
    data-shape problems — forcing them into the small, closed,
    mobile-portable template vocabulary would either blow that
    vocabulary up (losing the safety/portability property that made it
    worth building) or lose real capability. They stay bespoke.

**Explicitly deferred, not resolved, flagged for a future decision**:
whether imap-mcp's *existing* messaging-backend integration (BL340 /
GH#127 — the SSE-receive + REST-send chat-command path,
`internal/messaging/backends/imapmcp/`) stays a separate thing
alongside its new `service`-kind plugin manifest, or eventually folds
into it. Not decided here — revisit once a `service`-kind imap-mcp
plugin actually exists and the overlap (or lack of one) is concrete
rather than hypothetical.

## Phases

### Phase 0 — this document

Research + decision capture. Done, this file is the artifact.

### Phase 1 — Plugin Manifest v3

- `internal/plugins/plugins.go`: add `Kind string` (`"automation"` |
  `"service"`, default `"automation"` for backward compatibility with
  every existing manifest — no `kind:` field is a v2.1 manifest,
  unchanged behavior).
- For `Kind == "service"`: new `Connection *PluginConnection` (required
  when kind is service) — `BaseURL string`, `Auth` (bearer token ref via
  `${secret:...}`, per Secrets-Store Rule), `HealthPath string`
  (optional, for both lifecycle modes' health probing).
- New `Lifecycle *PluginLifecycle` (optional even for service-kind) —
  `SupportedModes []string` (`"attach_only"`, `"managed"`, subset or
  both — **author-declared**, this is what the operator's install-time
  choice is constrained to), `Entry string` + `Args []string` (only
  meaningful when `"managed"` is supported), `RestartPolicy string`
  (`"on-failure"`, `"always"`, `"never"`), `StopTimeoutMs int`.
- `scripts/check-plugin-manifests.sh`: extend validation — `kind`
  defaults correctly when absent; `connection.base_url` required when
  `kind: service`; `lifecycle.entry` required when `lifecycle` declares
  `"managed"` support; every `${secret:...}` ref in `connection.auth`
  resolves (don't validate the secret's *value*, just the reference
  shape, consistent with existing secret-ref validation elsewhere).
- Retire the dead `Mode` field's documentation comment (keep the field
  for one release as a deprecated/ignored alias if any manifest in the
  wild uses it — grep confirmed none do locally, but this is a public
  manifest format other operators may have authored against).
- Tests: manifest parsing (new fields, default `kind`, tolerant-of-
  unknown-fields per Skills-Awareness Rule's PAI-compatibility
  precedent), validation script's new checks (positive + negative
  fixtures).

### Phase 2 — Service-plugin process supervision ("managed" lifecycle)

- New supervisor in `internal/plugins/` (not a new top-level package —
  it's plugin lifecycle, belongs with the registry) : `Start`, `health`
  (poll `HealthPath` on an interval), `restart` (per `RestartPolicy`),
  `Stop` (graceful, `StopTimeoutMs` budget then SIGKILL).
- Reuse-and-Expand audit (required by AGENT.md B9): the F10 agent-worker
  system (`internal/agents/`) already does spawn/reconcile/health/
  terminate for container workers. Audit its supervision loop shape
  before writing a new one from scratch — reuse the *pattern*
  (not the container/F10-specific mechanics, which assume a Docker/k8s
  driver this is a plain subprocess, not a container).
- Daemon integration: a `kind: service` plugin with `lifecycle.managed`
  **enabled by the operator at install/enable time** gets started when
  the plugin is enabled and the daemon boots; stopped gracefully on
  daemon shutdown and on `plugin_disable`.
- Audit events (Phase 6 ties in here): `plugin_service_start`,
  `plugin_service_stop`, `plugin_service_restart`,
  `plugin_service_health_fail`.
- Tests: supervisor state machine (start → healthy → crash → restart →
  healthy; start → health-check-never-passes → gives up per policy;
  stop → graceful exit within budget; stop → exceeds budget → killed).

### Phase 3 — Widget data model & transport

- New `Widget` declaration (lives in the plugin manifest, both kinds can
  declare widgets — an `automation` plugin's widget data comes from a
  new oneshot hook, e.g. `status_poll`, fitting `Invoke()` exactly as-is;
  a `service` plugin's widget data comes from its `connection`):
  `ID`, `Surface` (`"dashboard" | "session"`), `Kind` (`stat_tile |
  table | list | sparkline | status_badge | markdown`), `DataMode`
  (`"push" | "pull"`, **per-widget**), and `PullPath` (when pull) or
  nothing extra (when push — the plugin posts to a path keyed by its
  own name + widget id, mirroring observer-peer's
  `POST /api/observer/peers/{name}/stats` shape).
- Pull: daemon-initiated. `automation` plugin → `Invoke()` the
  `status_poll` hook on an interval. `service` plugin → `GET
  <connection.base_url><widget.pull_path>` on an interval.
- Push: plugin-initiated, mirroring observer-peer exactly — register
  once (reuses or parallels the observer-peer bearer-token issuance,
  Reuse-and-Expand audit required before deciding "reuse the literal
  mechanism" vs. "parallel mechanism, same shape"), then
  `POST /api/plugins/{name}/widgets/{widget_id}` whenever the plugin's
  own process decides to.
- Storage: latest-known value per `(plugin, widget_id)`, in-memory with
  a periodic snapshot to disk (survive a daemon restart without forcing
  every pull-mode widget to go blank, and without over-persisting
  push-mode widgets that update every few seconds).
- Tests: pull scheduler (interval respected, a hung pull doesn't block
  other widgets' pulls — this was explicitly why "pure pull" lost the
  transport-model vote), push endpoint (valid payload accepted, bad
  payload rejected with a clear error, unregistered plugin rejected).

### Phase 4 — Generic widget renderer (PWA)

- New generic render functions, one per `Kind`: `stat_tile`, `table`,
  `list`, `sparkline`, `status_badge` — small, pure functions (data in,
  safe HTML out, same `escHtml` discipline every other card already
  uses) — plus the `markdown` kind calling `_renderMarkdownFileInto`
  verbatim.
- Dashboard integration (**design detail proposed here, not yet
  operator-confirmed — flag at review**): each plugin widget becomes its
  own dynamic entry in the *same* layout array `_dashBuildGrid` already
  works with, id-namespaced `plugin:<plugin-name>:<widget-id>`, so the
  existing add/remove/drag/resize affordances work on it unmodified.
  `_dashCardHTML`/`_dashBuildGrid` gain one new branch: an id with a
  `plugin:` prefix renders via the new generic renderer instead of a
  `DASH_CARD_DEFS` lookup. Alternative considered and not chosen without
  review: a single fixed "Plugin Widgets" card containing a sub-list of
  every plugin's tiles — rejected as the default proposal because it
  can't be independently resized/repositioned/removed per-widget, but
  worth a sentence in review if the operator prefers it.
- Session detail integration: new fixed container in
  `renderSessionDetail()` (`app.js:3157`), populated by the same generic
  renderer. **Open sub-question for review, not resolved here**: are
  session widgets scoped to *that specific session* (e.g. a per-session
  resource tile a plugin contributes, keyed by session id) or are they
  the same *global* plugin status shown in every session's context
  (e.g. imap-mcp's enrichment status, which has nothing to do with any
  one session)? This plan assumes the latter (global, same widget shown
  everywhere) unless corrected at review, since imap-mcp — the first
  real adopter — has no natural per-session scoping.
- Locale keys: `dashboard_help_tip`-style operator-visible strings this
  surface adds (e.g. "Add Card" panel entry labels for plugin widgets,
  any error/loading strings) — 5 bundles + `TestLocales_
  CommonNavKeysPresent` `mustHave` update for anything high-visibility.
- Tests: `node --test` coverage per render function (valid payload →
  expected DOM shape; malicious payload, e.g. a `label` containing
  `<script>`, → confirmed escaped/stripped, not executed) — matching
  this codebase's existing XSS-guard test style.

### Phase 5 — Federation capability gating

- `internal/federation/capabilities.go`: add `CapPluginRead =
  "plugin:read"`, `CapPluginWrite = "plugin:write"` to the relevant
  const block + `allCaps`. Per the least-privilege precedent (operator
  decisions list above didn't ask for a default grant), **not** added
  to any `BuiltinGroups` preset by default — an operator who wants a
  peer to see plugin widgets grants it explicitly.
- `internal/federation/mcp_tool_caps.go`: entry for every new plugin_*/
  widget_* MCP tool (enforced by the existing
  `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` test —
  this test will fail-fast on any omission, which is the intended
  guardrail).
- New REST endpoints (Phase 7) get `s.fedCap(w, r,
  federation.CapPluginRead)` (reads) / `CapPluginWrite` (push, enable/
  disable, lifecycle actions) immediately after the method guard, per
  the Federation-Parity Rule's exact placement convention.
- Federation-Parity Rule release-time checklist applies: grep new
  `case "..."` entries against `fedCap` calls before tagging any release
  that ships this.
- Explicitly **not** touched by this plan: the existing
  `CommCommandRoute` local-loopback path (verified this session to be
  host-local only, not federation-exposed — see Context above). Noted
  as a real, pre-existing design characteristic worth a future look, not
  something this plan changes or claims to fix.

### Phase 6 — Audit logging

- New `Action` values: `plugin_service_start`, `plugin_service_stop`,
  `plugin_service_restart`, `plugin_service_health_fail`,
  `plugin_widget_registered`, `plugin_widget_push`,
  `plugin_widget_pull_failed`.
- `internal/audit/cef.go` `cefSignature()`: add a case per action above
  (severity: health-fail/restart = medium, start/stop = informational,
  widget push/pull-failed = informational/low) — per the Rule, this is
  recommended for SIEM-friendliness, not required for the event to be
  valid, but should ship alongside rather than as a follow-up.
- **Test requirement (Audit Logging Rule, non-optional)**: every new
  action gets (a) a JSON-lines round-trip test and (b) a CEF format test
  asserting header pipe-escaping + extension equals/newline-escaping +
  the correct `(signatureID, name, severity)` triple.

### Phase 7 — Full parity surfaces

Per decision 7 (full parity from day one) and the Configuration
Accessibility Rule's 6-channel bar, for both the widget-data surface and
any new config fields (lifecycle mode choice, pull interval, etc.):

| Surface | What ships |
|---|---|
| **REST** | `GET /api/plugins/{name}/widgets`, `GET /api/plugins/{name}/widgets/{id}`, `POST /api/plugins/{name}/widgets/{id}` (push), `POST/DELETE /api/plugins/{name}/lifecycle` (start/stop/restart, managed mode only) |
| **MCP** | `plugin_widget_list`, `plugin_widget_get`, `plugin_widget_push`, `plugin_service_start\|stop\|restart` — each with an `mcp_tool_caps.go` entry (Phase 5) |
| **CLI** | `datawatch plugins widgets list/get <name>` (reuses `plugin_run_subcommand`'s existing proxy shape rather than a new CLI code path) |
| **Comm channel** | a new built-in verb (not a `comm_commands` plugin declaration — this one's generic, every plugin gets it for free) surfacing a widget's current value as text, e.g. `plugin status imap-mcp` |
| **YAML/config** | any new config fields (operator's chosen `lifecycle` mode when a plugin supports more than one, pull interval override) — `config.go`, `docs/config-reference.yaml`, `handleGetConfig`/`applyConfigPatch`, `GENERAL_CONFIG_FIELDS` |
| **PWA** | Phase 4 |

- Documentation Rules checklist (general, every change): `CHANGELOG.md`,
  `docs/config-reference.yaml`, `docs/operations.md` (new plugin
  lifecycle semantics), `README.md` (new interface), doc index in
  `README.md` + `docs/README.md`, `docs/testing-tracker.md` new section
  per new/changed endpoint, internal-tracker-ID leak check, and this
  plan's own Parity surface section below.
- New-MCP-tool checklist: `docs/mcp.md` + `docs/cursor-mcp.md` entries
  for every new tool.
- **`docs/plugins.md` does not exist today** (verified this session) —
  this plan is the first feature substantial enough to justify creating
  it. New file, not a retrofit: manifest v3 reference, both `kind`
  values, widget declaration reference, worked example (imap-mcp,
  Phase 9).

### Phase 8 — Mobile parity

- File a `datawatch-app` issue at this plan's approval (per decision 9),
  listing: the widget contract (5 templates + markdown, deliberately
  chosen for native renderability), the REST/MCP endpoints Android/iOS
  would poll or subscribe to, and that implementation timeline is the
  app team's own (capability parity required, implementation parity
  idiomatic-per-platform, per the Mobile-Parity Rule's own text).
- This plan does **not** include writing any Android/iOS code — out of
  repo scope. The issue is the deliverable here.

### Phase 9 — imap-mcp as the first real adopter (dogfooding)

This is the actual goal of Phases 1-7, not a follow-on example: the
mechanism exists to prove itself against a real, standalone tool before
anything else leans on it.

- Write imap-mcp's own `kind: service` plugin manifest (lives in
  `dmz006/datawatch-community`, alongside its existing skill — the skill
  teaches an agent to *use* imap-mcp's MCP tools; the plugin manifest is
  the separate, new thing teaching *datawatch* how to supervise/poll it).
- `connection.base_url` → imap-mcp's existing REST/MCP surface.
  `lifecycle.supported_modes: [attach_only, managed]` — both, since the
  operator explicitly wants the option to fully lifecycle-manage it for
  their own use while never requiring it for a standalone user.
- Widgets, informed by the skill's own tool surface (confirmed live
  this session): `enrichment_status` → `stat_tile` (pending/processing/
  done/error counts); `get_anomalies` → `list` (severity-tagged, 0 =
  healthy); `list_accounts` → `status_badge` row (connected/
  disconnected per account); `list_rules` → `stat_tile` (active rule
  count — not the full 272-row table, which is a CLI/REST job, not a
  dashboard tile).
- This phase is also where the deferred BL340-folding question (Context,
  above) gets revisited with a concrete manifest in hand instead of a
  hypothetical.
- This phase is the real validation that Phases 1-7's contract is sound
  against a genuinely independent, standalone external tool — not a
  hypothetical one — before Phase 10 asks datawatch's own built-in cards
  to lean on the same mechanism.

### Phase 10 — Dogfood migration of datawatch's own cards (optional, deferred, not blocking)

Per decision 10. After Phase 9 (imap-mcp) is live in production, not
before:

- Migrate `memory-scope`, `websearch-usage`, `guardrails`, `smoke` off
  their bespoke `DASH_CARD_DEFS` render functions onto the generic
  widget renderer (Phase 4), as first-party `kind: automation` widget
  declarations internal to datawatch itself (not a "plugin" in the
  operator-facing sense — same contract, used by the project's own
  built-in cards).
- Explicitly **not** touched: `orbital`, `gantt`, `ekg`, `heatmap`,
  `tree` — bespoke interactive visualizations, not data-shape cards
  (see Decisions §10 for the reasoning). Document this exclusion in
  `docs/plugins.md` so a future contributor doesn't "finish the
  migration" by forcing them in.
- Lower priority than Phase 9 by design: imap-mcp is the use case that
  justifies this plan existing at all; migrating datawatch's own cards
  is a secondary payoff, worth doing once the mechanism has already
  proven itself in production against a real third party, not before.

## Parity surface

Per the Mobile-Parity Rule's full surface set
(`REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS`):

- **REST** — included, Phase 7.
- **MCP** — included, Phase 7.
- **CLI** — included, Phase 7 (thin proxy, reusing `plugin_run_subcommand`'s
  existing shape).
- **Comm channel** — included, Phase 7 (new built-in verb, not a
  per-plugin `comm_commands` declaration).
- **YAML/config** — included, Phase 7 (lifecycle mode choice, pull
  interval, any new config fields).
- **PWA** — included, Phase 4 (primary surface — this plan exists
  because of it).
- **Android** — capability-parity required, implementation deferred to
  the app team's own timeline (Phase 8); reason: native rendering of the
  same typed-template contract is a deliberate goal, not a gap, but
  writing Android code is outside this repo.
- **iPhone/iOS** — same as Android, same reason, same phase.

## Out of scope

- Any Android/iOS code (Phase 8 files the issue, doesn't implement).
- Resolving whether imap-mcp's existing BL340 messaging-backend
  integration folds into its new plugin manifest (Context, explicitly
  deferred).
- Migrating `orbital`/`gantt`/`ekg`/`heatmap`/`tree` onto the generic
  widget renderer (Decisions §10, Phase 10) — deliberately excluded, not
  deferred-by-oversight.
- Hardening the `CommCommandRoute` local-loopback path's lack of an auth
  header — real, pre-existing, verified host-local-only (not a
  federation hole); worth a future look, not part of this plan.
- A general-purpose plugin "marketplace" UI beyond what
  `plugin_browse_registry`/`skills_registry_*` already provide — this
  plan is about the widget/lifecycle contract, not discovery UX.

## Files (representative)

- `internal/plugins/plugins.go`, `internal/plugins/*_test.go` — Phase 1-2.
- `scripts/check-plugin-manifests.sh` — Phase 1.
- New supervisor file(s) under `internal/plugins/` — Phase 2.
- `internal/server/plugins.go` (or new `internal/server/plugin_widgets.go`)
  — Phase 3, 7 (REST handlers).
- `internal/mcp/plugins.go` — Phase 7 (new MCP tools).
- `internal/federation/capabilities.go`, `internal/federation/mcp_tool_caps.go`
  — Phase 5.
- `internal/audit/cef.go` — Phase 6.
- `internal/server/web/app.js`, `internal/server/web/locales/*.json` —
  Phase 4.
- `docs/plugins.md` (new), `docs/config-reference.yaml`,
  `docs/operations.md`, `docs/mcp.md`, `docs/cursor-mcp.md`,
  `docs/testing-tracker.md`, `README.md`, `docs/README.md`,
  `CHANGELOG.md` — Phase 7.
- `dmz006/datawatch-community` (separate repo) — Phase 9, imap-mcp's
  own manifest; not touched by this repo's commits.

## Verification

- Per-phase: `go test ./internal/plugins/... ./internal/federation/...
  ./internal/audit/... ./internal/server/...` and `node --test
  internal/server/web/*.test.js` as each phase lands.
- Phase 5: Federation-Parity Rule's own checklist — grep new `case "`
  entries against `fedCap`, confirm
  `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` passes
  with every new tool entered.
- Phase 6: Audit Logging Rule's explicit test requirement — JSON-lines
  + CEF round-trip per new `Action`, not optional.
- Before any release shipping Phase 1-7 together: full
  `scripts/release-smoke.sh` run (minor/major release discipline — this
  is clearly a minor at least, likely scoped as its own `vX.Y.0` given
  the surface area), extended with a new smoke section for widget
  push/pull round-trip (B12).
- Skills-Awareness Rule cross-cutting check (B10): does `kind: service`
  plugin spawn interact with session spawn, PRD/automaton context
  injection, or agent containers? Answer deferred to Phase 1/2
  implementation — likely "no" (a service plugin is daemon-lifetime, not
  session-lifetime) but must be explicitly re-asked at that point, not
  assumed now.
- Reuse-and-Expand audit (B9), per phase: Phase 2 against F10 agent
  supervision; Phase 3 push transport against observer-peer; Phase 4
  against the existing dashboard layout mechanism (reused) vs. render
  functions (new, deliberately — see Phase 4's own note on why no
  generic renderer exists to reuse). Each phase's commit needs its own
  "what existing primitive does this extend?" CHANGELOG line — empty
  answer files as a backlog item for review, per the Rule.
- Mobile-Parity Rule audit prompt at release-commit time: "did this
  release change anything an operator would notice on the PWA?" — yes,
  by design, from Phase 4 onward; confirm the Phase 8 `datawatch-app`
  issue is linked in the commit body for every PWA-touching phase.
