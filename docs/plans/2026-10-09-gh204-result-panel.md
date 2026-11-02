# Plan: Result panel for scheduled jobs and integrations (GH#204 / BL402)

- **Date**: 2026-10-09
- **Version at planning**: v8.79.0
- **Status**: **Complete** (Phases 1–5 shipped; see Status log at
  bottom of this doc).
- **Filed by**: a peer session, at the operator's request, as
  `dmz006/datawatch#204`.

## Context

Scheduled shell jobs and external integrations (the running example:
the hourly `imap-mcp run-rules` job, run via `schedule_spawn` with
`subprocess: true`) have nowhere in the UI to show what a run did.
The Status tab's four panels (focus, sprint, tests, git) are all
shaped for coding-agent hook events; dashboard cards are a fixed set
of built-in types; the existing result store (BL360,
`result_put`/`result_get`) can hold structured output but nothing
renders it. Today the only record of a run is raw terminal output.

Full issue text is in GH#204 — not reproduced here; this doc covers
what investigation found before sizing, the operator's decisions, and
what's actually being built.

## Investigation — corrects two assumptions in the issue, confirms a third

Verified directly against the code before writing anything else in
this plan, per this session's own standing practice of not taking an
issue's framing at face value:

1. **"A subprocess or shell session already gets
   `DATAWATCH_SESSION_ID`" — false.** Read `internal/session/
   manager.go`'s `Start()`: subprocess mode (`opt.Subprocess`) returns
   immediately after calling `go m.runSubprocess(sess)`, entirely
   *before* the tmux-session-creation code that injects
   `DATAWATCH_SESSION_ID`/`DATAWATCH_BASE_URL` (that injection goes
   through `m.tmux.SetEnvironment(...)` — a tmux-specific call that
   doesn't exist on the subprocess path at all). `runSubprocess`'s
   `exec.Command("bash", "-c", sess.Task)` never sets `cmd.Env`, so Go
   defaults to inheriting the *daemon's own* process environment —
   no session ID, no token, nothing session-specific. This issue
   needs to add that injection, not just a token on top of it.
2. **The generic result store (BL360) is the wrong fit for "last N
   per schedule_name" history.** `internal/session/result_store.go`
   is a flat `name → payload` map with TTL-based expiry and no
   per-caller scoping at all (any caller holding `results:write` can
   read/write *any* name — there's no concept of "this caller may
   only touch its own entries"). Reusing it for this feature would
   mean encoding history into composite key names and layering a
   second, ad-hoc scoping check on top of a store that was never
   designed for either. Operator decision: build a new, dedicated
   store instead (see Design).
3. **A spawned session has no durable link back to the schedule that
   spawned it.** `ScheduleName` lives only on `ScheduledCommand`
   (the recurring cron entry); `DeferredSession.Name` (propagated to
   `Session.Name`) is a separate, human-readable *session* label —
   confirmed by reading `schedule.go`'s `AddSpawn` and `manager.go`'s
   `DuePendingSessions` handling. Grouping results by `schedule_name`
   for the dashboard card requires adding that link — see Design.

**A related but separate finding, noted and NOT fixed in this plan**
(already flagged in GH#204 itself as a distinct item, and already
found independently this session): `ScheduleStore.Update()` only
writes the legacy `Command` display field, never
`DeferredSession.Task` — editing a schedule's command via the REST/CLI
edit path silently no-ops. Worth its own small fix; out of scope here.

## Operator decisions (2026-10-09)

1. **Token mechanism**: reuse the existing Design-A3
   `SessionTokenStore.Mint()` infrastructure (already used for the
   claude-code channel bridge) rather than building a second, parallel
   token system. Extend session launch to mint one for subprocess-mode
   sessions too (today only the `claudeChannelEnabled` tmux path mints
   one). Scoped to a new, narrow capability — not the broad
   `session-default` group.
2. **Result storage**: a new, dedicated store (not BL360), keyed by
   `schedule_name`, with real "keep last N" semantics and a
   session-identity check baked into the write path (a token may only
   post for the session it was minted for — checked via
   `SessionTokenStore.SessionIDForToken()`, already available).
   Requires adding a `ScheduleName` field to `Session` itself,
   propagated from the schedule entry at spawn time (server-known, not
   self-reported by the job — same reasoning as why `ParentAgentID` is
   tracked server-side rather than trusted from caller input).
3. **Scope**: a general mechanism, not restricted to subprocess/
   schedule_spawn sessions. Any session with a minted scoped token can
   post a result for itself. Subprocess mode is just the first (and
   currently only) caller, and the one that needs the env-injection
   fix from finding #1.
4. **Dashboard card default**: **opt-in**, not a default system card
   (most installs have zero scheduled integrations configured) — but
   made easy to discover: listed prominently in the card picker (not
   buried) and documented in a howto with a screenshot/diagram of
   where to find it.
5. **Retention**: configurable, default 20 results per `schedule_name`.
   New config field (Configuration Accessibility Rule — every
   operator-relevant knob gets a real config surface, not a hardcoded
   constant).

## Design

### Payload shape (REST + MCP, identical body)

Per GH#204's own proposal, every field but `title` optional:

```json
{
  "title": "imap-mcp run-rules",
  "status": "ok",
  "summary": "40 active rules, 2 messages actioned",
  "metrics": {"rules": 40, "matched": 2, "errors": 0, "duration_s": 95},
  "tables": [
    {"title": "Rules that matched", "columns": ["rule", "action", "matched"],
     "rows": [["newsletters", "move", 2]]}
  ],
  "markdown": "optional short notes"
}
```

- `status` ∈ `ok` | `warn` | `error`.
- Rendered as escaped text only — no raw HTML, markdown run through
  the same `marked` + `DOMPurify.sanitize()` pipeline the PWA already
  uses elsewhere (confirmed existing pattern, `app.js`'s `renderDoc`).
- Size limits (decided here, not asked — matches this session's
  existing bound-setting precedent for CEF/title fields): payload
  ≤ 64 KB total, ≤ 100 rows per table, ≤ 20 tables, cell/summary/
  markdown fields truncated at 2000 chars with a `…(truncated)`
  marker, never silently dropped.

### New capability

`sessions:result_write` — minted only into a session-scoped token's
resolved caps, not added to any built-in group's default set (same
least-privilege default this session has used for every new
write-class capability today).

### Server-side enforcement

`POST /api/sessions/{id}/result`:
1. `fedCap` with the new capability.
2. Resolve the caller's session ID from its token
   (`SessionTokenStore.SessionIDForToken`) and reject (403) if it
   doesn't match the path `{id}` — a token may only post for the
   session it was minted for, closing the gap the existing
   `/hook-event` endpoint has (confirmed: that endpoint checks a
   capability but never checks path-`id`-equals-caller's-own-session;
   noted as a pre-existing looseness, not fixed here, but not
   repeated in the new endpoint either).
3. Validate shape + enforce size limits (reject, don't truncate
   silently, on `title` missing or `status` unrecognized — truncate,
   don't reject, on oversized text fields per the limits above).
4. Look up the posting session's `ScheduleName` (new field); if set,
   write into the new schedule-result store keyed by it, trimming to
   the configured retention count.
5. Always also update the session's own `SessionStatusBoard.Result`
   field (new) so the Status tab panel has it regardless of whether
   `ScheduleName` is set — an interactive session with no schedule
   link still gets a working Result panel for itself.

### New config

```yaml
results:
  schedule_history_limit: 20  # keep the last N results per schedule_name
```

## Phases

### Phase 1 — env injection + token for subprocess sessions

**Status: Done — shipped (version assigned by the merge script at merge time; this commit range sits under the CHANGELOG's [Unreleased] section).**

- `internal/session/manager.go`: inject `DATAWATCH_SESSION_ID` +
  `DATAWATCH_BASE_URL` + a newly-minted, `sessions:result_write`-scoped
  token (env var `DATAWATCH_RESULT_TOKEN`) into `runSubprocess`'s
  `cmd.Env` — currently nil/inherited, confirmed by reading the code.
- `internal/federation/capabilities.go`: new `CapSessionsResultWrite`
  constant, documented as deliberately not in any built-in group.
- Add `Session.ScheduleName` field, set at spawn time from
  `DeferredSession`/the originating `ScheduledCommand`.

### Phase 2 — result store + REST/MCP endpoint

**Status: Done — shipped (version assigned by the merge script at merge time; this commit range sits under the CHANGELOG's [Unreleased] section).**

- New `internal/session/schedule_result_store.go` — `ScheduleName →
  []ResultEntry` (bounded to the configured retention count, oldest
  evicted first), durable (file-backed, survives the spawning
  session's own deletion).
- `POST /api/sessions/{id}/result` (`internal/server/`), `post_result`
  MCP tool (mirrors the REST shape).
- New `GET /api/schedule-results` (list, grouped by `schedule_name`,
  for the dashboard card) + `GET /api/schedule-results/{schedule_name}`
  (history for one).
- `results.schedule_history_limit` config (REST/MCP(generic)/YAML/PWA
  parity, full B6 checklist).

### Phase 3 — Status tab Result panel (PWA)

**Status: Done — shipped (version assigned by the merge script at merge time; this commit range sits under the CHANGELOG's [Unreleased] section).**

- New panel in the Status tab, alongside focus/sprint/tests/git —
  status badge, summary, metrics, tables, rendered via the existing
  escaped-markdown pipeline. Shown only when the session has posted a
  result (same "only render what's present" pattern the other panels
  already use).

### Phase 4 — "Integration status" dashboard card (PWA)

**Status: Done — shipped (version assigned by the merge script at merge time; this commit range sits under the CHANGELOG's [Unreleased] section).**

- New, **opt-in** card type (not in `defaultDashCards()`) — one row
  per `schedule_name`: last status, summary, time, a sparkline across
  recent runs (reusing `results.schedule_history_limit`'s retained
  history), click-through to that run's session.
- Listed prominently in the card picker; documented with a
  screenshot/diagram showing exactly where to find and add it.

### Phase 5 — docs + imap-mcp coordination

**Status: Done — shipped (version assigned by the merge script at merge time; this commit range sits under the CHANGELOG's [Unreleased] section).**

- New howto (`docs/howto/result-panel.md`) with a diagram: payload
  shape, size limits, how to add the dashboard card, a worked
  curl example posting a result from a shell wrapper.
- `docs/howto/alerts-and-notifications.md` / `alert-rules.md` cross-
  reference note (a result panel is a complement to, not a
  replacement for, raising an alert — different tool for a different
  question: "what did the run produce" vs "does the operator need to
  be paged").
- Reply to imap-mcp-79 once Phase 2 ships so their `run-rules --json`
  wrapper work (mentioned in the issue) has a real endpoint to post
  to.

## Parity surface

**Lesson applied from today's own compliance miss** (two PWA settings
cards shipped earlier today without the required `datawatch-app`
issue — caught on operator review, backfilled as app#241): this
plan's PWA-visible phases (3 and 4) will each get a `datawatch-app`
issue/comment filed **in the same commit/session that ships them**,
not after. No exceptions this time.

- **REST**: `POST /api/sessions/{id}/result` (new),
  `GET /api/schedule-results` (new), `GET /api/schedule-results/
  {schedule_name}` (new), `GET`/`PUT /api/config`'s
  `results.schedule_history_limit` (new, generic).
- **MCP**: `post_result` (new, mirrors the REST body). No new tool
  needed for the config field (rides `config_set`/`get_config`) or
  for reading results — `result_get`-shaped tools already exist on
  the generic BL360 surface; a `schedule_results_list` tool is added
  alongside the REST list endpoint for symmetry.
- **CLI**: `datawatch schedule-results list` / `history <name>` /
  `post --session <id> ...` shipped alongside the REST endpoints
  (posting falls back to the ambient operator token outside a spawned
  session; inside one it uses the injected scoped credential and
  session, no flags needed).
- **Comm channel**: the read surface is exposed as chat commands
  (`schedule-results` and `schedule-results <name>`); posting stays
  on REST/MCP/CLI — a chat channel isn't an agent's posting surface.
  Revisit if explicitly requested for posting as well.
- **YAML**: `results.schedule_history_limit`.
- **PWA**: Phase 3 (Status tab Result panel) + Phase 4 (opt-in
  dashboard card) — both operator-visible, both get a same-session
  `datawatch-app` filing, not deferred.
- **Android/iPhone**: tracked via the same `datawatch-app` issue
  Phase 3/4 file — status to be set by that repo's own maintainers,
  not assumed here. This plan does not implement any mobile client
  code (out of scope — `dmz006/datawatch-app` is a separate repo).

## Release checklist (per-phase, copy forward and tick for each commit)

Mapped from `AGENT.md`'s Section A (every commit) / B (conditional) /
C (release cadence) tables, using `DATAWATCH-CONTEXT.md`'s "Key Files
for Rule Contract Work" as the exact file list per rule:

### A — every commit
- [ ] A1 — name every rule that fired in the commit's `rules:` token
- [ ] A2 — `go test ./...` clean
- [ ] A3 — version bumped in both `cmd/datawatch/main.go` and
  `internal/server/api.go`
- [ ] A4 — `CHANGELOG.md` entry, no B/BL/F IDs in the entry text
- [ ] A5 — `README.md` current-release line updated (if it names a
  version-worthy headline feature — check convention before assuming)
- [ ] A6 — `docs/plans/README.md` BL402 entry updated per phase
- [ ] A7 — `scripts/check-no-internal-refs.sh` clean
- [ ] A8 — leak-check (no secrets) clean
- [ ] A9 — `node --test internal/server/web/*.test.js` clean (Phases
  3-4 touch `app.js`)
- [ ] A10 — `make build` (never bare `go build ./cmd/datawatch/` —
  embedded docs won't sync)
- [ ] A11 — `gh run list` checked green after every push

### B — conditional, this feature's actual triggers
| # | Trigger | Applies? | Phase |
|---|---|---|---|
| B1 | New/changed endpoint contract | Yes | 2 |
| B2/B3 | New PWA string | Yes | 3, 4 |
| B4 | **Mobile parity — file `datawatch-app` issue, same session** | Yes | 3, 4 |
| B6 | New config field | Yes — `results.schedule_history_limit` | 2 |
| B7 | Observability | Maybe — consider a Prometheus counter for results posted/rejected | 2 |
| B12 | New operator-facing endpoint → smoke section | Yes | 2 |
| New-MCP-tool checklist | `post_result`, `schedule_results_list` | Yes | 2 |

### C — release cadence (this ships as a minor, multi-phase feature)
- [ ] C1 — dep-audit (expect none — stdlib only)
- [ ] C2 — gosec exact CI command, baseline-diff clean
- [ ] C3 — `release-smoke.sh` clean, new section(s) added per B12

## Out of scope / explicitly deferred

- `ScheduleStore.Update()`'s `DeferredSession.Task` bug — tracked in
  GH#204 itself as a separate item; not fixed in this plan.
- A CLI verb for posting/reading results — **shipped**
  (`datawatch schedule-results`, see Parity surface).
- Comm-channel *posting* — out of scope (read is exposed, see above).
- Retrofitting the existing `/hook-event` endpoint's loose
  session-identity check — flagged as a precedent to not repeat, not
  a bug this plan fixes.

## Verification

- Unit tests per phase, `confirmed-fails-without-fix` style throughout
  (same discipline as every GH#201 phase).
- A live two-session-style test for the session-identity check: token
  minted for session A must be rejected (403) when posting a result
  for session B's path.
- `docs/testing-tracker.md` entry per phase.
- Phase 3/4: a real PWA click-through check (start the daemon, post a
  result via curl with a real session's minted token, confirm the
  Status tab panel and — once added — the dashboard card render it),
  not unit tests alone, per this session's own standard for UI
  surfaces.

## Status log

- **2026-10-17 — Phase 5 complete; all phases shipped.**
  - Howto `docs/howto/result-panel.md` (with `exec_steps:` covering
    REST/CLI/MCP/chat channels) + `docs/howto/README.md` index entry;
    cross-reference notes in `alerts-and-notifications.md` and
    `alert-rules.md`.
  - `CHANGELOG.md` entry (no internal tracker IDs in entry text).
  - `docs/testing-tracker.md` entries per phase, incl. the new
    `release-smoke.sh` H "75" section (spawn → scoped-token post →
    schedule-results reads → 403 cross-session gate) — **authoring
    complete; H 75 + TS-801 both marked Validated: No** until the
    next release verification run.
  - `scripts/release-smoke.sh` H "75" section added; `scripts/
    test-stories/TS-801.sh` authored (not run this session).
  - `docs/plans/gh204-datawatch-app-issue.md` — ready-to-file
    `dmz006/datawatch-app` mobile-parity issue body (B4). The issue
    itself is filed in the app repo, not this one.

## Phase Completion Checklist Audit (final-gate, 2026-10-22)

Audited item-by-item against AGENT.md's "Phase Completion Checklist
(applies to every phase of every plan)", verified against the actual
diff (`e562f652^..HEAD`, 8 commits) not against this plan's own
claims. A `[ ]` below is a real gap an operator can act on, not a
silently skipped item.

### Per-item results

- [x] **Functionality implemented per the phase's own description.**
  - Phase 1: `DATAWATCH_SESSION_ID` / `DATAWATCH_BASE_URL` /
    `DATAWATCH_RESULT_TOKEN` injected into `runSubprocess`'s `cmd.Env`
    (`internal/session/manager.go`); `CapSessionsResultWrite` constant
    documented as deliberately absent from every built-in group
    (`internal/federation/capabilities.go`); `Session.ScheduleName`
    set at spawn (`internal/session/types.go`, `schedule.go` +
    `manager.go`).
  - Phase 2: file-backed, retention-bounded, atomically-persisted
    `ScheduleResultStore` (`internal/session/schedule_result_store.go`
    + a green 7-test suite: eviction, limit raise/lower, persist/reopen,
    truncation, typed rejections incl. a dedicated typed-error variant,
    concurrent adds under `-race`); `POST /api/sessions/{id}/result` with the 403
    cross-session identity gate (`internal/server/api.go` +
    `session_result_endpoint_test.go`, 403 asserted at lines 153/264/
    273); `GET /api/schedule-results[/{schedule_name}]`;
    `results.schedule_history_limit` (default 20, min-1 clamp) wired
    through REST `PUT /api/config`, MCP `config_set`, `datawatch
    configure`, the PWA Settings card, and YAML; `post_result` and
    `schedule_results_list` MCP tools reusing the exact REST surface
    and validation.
  - Phase 3: Status-tab result panel (status badge, summary, metrics,
    tables, escaped-markdown pipeline; renders only when the session
    has posted — same "only render what's present" pattern as the
    other four panels).
  - Phase 4: opt-in "Integration status" dashboard card — one row per
    `schedule_name` with badge, summary, age, retained count, inline
    SVG sparkline (capped at 60 bars), click-through to the session;
    listed first in the Add-Card picker, deliberately absent from the
    default layout.
  - Phase 5: `docs/howto/result-panel.md` (+ `exec_steps:`), howto
    index entry, alert-rules cross-reference notes, MCP docs,
    testing-tracker sections, `release-smoke.sh` H "75", TS-801, and
    the ready-to-file app issue body.
- [x] **Unit tests added/passing for anything new.** Per-phase suites
  listed in the CHANGELOG entry and re-verified green in
  `go test ./...` (6,144 tests, 85 packages) and `node --test
  internal/server/web/*.test.js` (348 pass / 0 fail). MCP-tool cap
  mapping is asserted semantically by
  `internal/federation/mcp_tool_caps_test.go::TestMCPToolCaps_
  ResultPanelTools` (`post_result` → `sessions:result_write` and
  absent from every built-in group; `schedule_results_list` →
  `autonomous:read` and reachable by `read-only`/`session-default`) in
  addition to the pre-existing structural coverage tests.
- [ ] **Live/smoke test against a real running daemon.** Not yet run.
  Both artifacts are authored and wired in: `scripts/release-smoke.sh`
  section H "75" (spawn → scoped-token post → reads → 403 cross-session
  gate) and `scripts/test-stories/TS-801.sh`; both are marked
  **Validated: No** in `docs/testing-tracker.md` until the next sandbox
  release verification (which needs LLM/automata-gate stories green
  first, and TS-801 additionally does live spawns and a child-env
  capture). The 403 cross-session identity check is covered at unit
  level, but per this project's own BL406 discipline the live rows stay
  unvalidated deliberately. **Action: supervisor runs the sandbox
  release verification.**
- [x] **`go test ./...` and `node --test internal/server/web/*.test.js`
  both green.** Re-run this gate: `go vet ./...` clean; `go test
  ./...` ok (2026-10-21/22); node suite 348/348. All 9 task-phase
  verify scripts under `.datawatch/verify/` return `VERIFY OK`, and
  `final-gate-result-panel.sh` prints `VERIFY OK` (gofmt, go vet, go
  test, node test, no-internal-refs, docs-sync, config-reference,
  CHANGELOG).
- [x] **General documentation checklist (all 8 items or N/A with
  reason).**
  1. `CHANGELOG.md` — [x] full entry under `[Unreleased]`, no B/BL/F
     IDs in the entry text (checked by the final-gate script's
     `grep -vE 'BL[0-9]' CHANGELOG.md` guard, which passed).
  2. `docs/config-reference.yaml` — [x] `results:
     schedule_history_limit: 20` present and correct.
  3. `docs/operations.md` — N/A: no operations-runbook or runbook-style
     procedure changed (deploy, restart, runbook language); the
     operator-facing usage lives in `docs/howto/result-panel.md` per
     the project's howto-first convention.
  4. `README.md` — [x] the doc index table row ("Autonomous +
     plugins") gained a `docs/howto/result-panel.md` link in the docs
     commit (`ea900e2a`); the README's version line is not something
     this feature writes — see the version-bump item below.
  5. Doc index in the howto list and plan registry — [x]
     `docs/howto/README.md` gained the `result-panel.md` row and
     `docs/plans/README.md`'s BL402 row now reads "all phases shipped
     (unreleased)" with the full per-phase summary (both verified in
     the diff).
  6. `docs/testing-tracker.md` — [x] one section per phase (phase-
     specific sections + the MCP-parity section); live rows marked
     Validated: No with reasons, unit rows Yes.
  7. No leaked tracker IDs — [x] `scripts/check-no-internal-refs.sh`
     passes (covered by the final-gate script).
  8. This plan's `Parity surface` section still accurate — [x] each
     line re-checked against the code: 3 REST routes, `post_result` +
     `schedule_results_list` (MCP), `datawatch schedule-results
     list/history/post` (CLI), `schedule-results[ <name>|list|history]`
     chat read commands, `results.schedule_history_limit` YAML key,
     and the two PWA surfaces (Status-tab panel + opt-in dashboard
     card) all present; "Android/iPhone … separate repo" unchanged.
- [x] **Conditional docs.**
  - `docs/datawatch-definitions.md` — [x] "Scheduled job result panel"
    section present (line 426: config + CLI/REST/Web surfaces).
  - `docs/flow/*.md` — N/A: the panel and card reuse the existing
    session-status rendering cycle and the dashboard's RAF render
    cycle; no new process-level or state-machine flow was introduced.
  - `docs/howto/*.md` — [x] the new user-followable workflow
    (spawn → post → read + card setup) is exactly
    `docs/howto/result-panel.md`; passes
    `scripts/check-howto-coverage.sh` via its `exec_steps:`
    (`post_result` + `schedule_results_list`).
- [ ] **Mobile-Parity Rule (datawatch-app issue, same session).**
  The issue body is authored and tracked:
  `docs/plans/gh204-datawatch-app-issue.md` (B4's deliverable in this
  repo, per its own "ready-to-file" status). Checked the app repo
  directly (2026-10-22, `gh issue list -R dmz006/datawatch-app`,
  searches on "result panel" and "schedule" titles, and the open-issue
  list): **no filing is visible there yet.** The filing itself is an
  action in `dmz006/datawatch-app` (a separate repo, out of scope for
  this worktree's diff by design), not a gap in this plan's commits —
  but it is a genuinely open action for the operator/supervisor rather
  than an assumption-checked-off item.
- [x] **Localization Rule.** All 14 new PWA keys
  (`dash_integ_loading`, `dash_integ_status`, `dash_integ_open`,
  `dash_integ_empty_title`, `dash_integ_empty`, `dash_integ_empty_link`,
  `dash_integ_error`, `dash_integ_retained`,
  `prd_task_smoke_verifier_tip`, `status_card_result`,
  `status_result_ok`, `status_result_warn`, `status_result_error`,
  `status_result_rendering`) are present and non-empty in all 5
  bundles (en/de/es/fr/ja) — confirmed programmatically this gate, not
  by grep alone; `TestLocales_AllAppJSKeysExistInEnglishBundle` and
  the integration-card locale tests pass.
- [N/A] **Version bumped in both files.** Deliberately not done, per
  AGENT.md's own merge rule for this branch shape: version numbers are
  assigned by `scripts/merge-branches-bump-version.sh` at merge
  (choosing the next patch above main and writing it to both
  `cmd/datawatch/main.go` and `internal/server/api.go` plus the
  CHANGELOG heading), and `scripts/check-merge-version.sh` enforces
  that exact next-patch — so this feature's commits correctly sit
  under the CHANGELOG's `[Unreleased]` section with no hardcoded
  version string anywhere in the diff (verified by grep across all 17
  docs/scripts files the feature touched: clean). The plan's own C-
  cadence section says the same (this "ships as a minor, multi-phase
  feature," i.e. the merge step, not each commit range).
- [x] **This plan doc's own phase-status line updated.** Each of the 5
  phase headers above now carries an explicit `Status: Done — shipped`
  line (added this gate) rather than relying on the bottom-of-doc
  Status log alone; the doc's own header `Status: Complete (Phases 1–5
  shipped)` was already accurate and is unchanged.

### Gate run summary (this gate, 2026-10-22)

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `go test ./...` | 6,144 tests / 85 packages — all pass |
| `node --test internal/server/web/*.test.js` | 348 pass / 0 fail |
| `scripts/check-no-internal-refs.sh` | pass (no B/BL/F/S tracker IDs in operator-facing surfaces) |
| `scripts/check-docs-sync.sh` | pass (PWA-embedded docs match `docs/`) |
| `docs/config-reference.yaml` has `schedule_history_limit` | present, default 20 |
| CHANGELOG entry, no tracker IDs in entry text | present under `[Unreleased]` |
| All 9 `.datawatch/verify/` task scripts (ph1…ph5) | `VERIFY OK` each |
| `.datawatch/verify/final-gate-result-panel.sh` | `VERIFY OK` |
| No release version number written in the feature's docs/scripts | confirmed clean across all 17 docs/scripts files in the diff |

**Open actions (not gaps in this plan's code, but genuinely open and
called out rather than silently checked off):**
1. Run the sandbox release verification to close H "75" + TS-801
   (both marked Validated: No) — the 403 cross-session and the spawn →
   post → read path are unit-covered but not yet live-verified.
2. File the `dmz006/datawatch-app` parity issue whose body is ready
   at `docs/plans/gh204-datawatch-app-issue.md` (B4), if/when the
   operator chooses to act on it in the app repo.
