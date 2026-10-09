# Plan: Result panel for scheduled jobs and integrations (GH#204 / BL402)

- **Date**: 2026-10-09
- **Version at planning**: v8.79.0
- **Status**: Planned — not started.
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
- `internal/session/manager.go`: inject `DATAWATCH_SESSION_ID` +
  `DATAWATCH_BASE_URL` + a newly-minted, `sessions:result_write`-scoped
  token (env var `DATAWATCH_RESULT_TOKEN`) into `runSubprocess`'s
  `cmd.Env` — currently nil/inherited, confirmed by reading the code.
- `internal/federation/capabilities.go`: new `CapSessionsResultWrite`
  constant, documented as deliberately not in any built-in group.
- Add `Session.ScheduleName` field, set at spawn time from
  `DeferredSession`/the originating `ScheduledCommand`.

### Phase 2 — result store + REST/MCP endpoint
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
- New panel in the Status tab, alongside focus/sprint/tests/git —
  status badge, summary, metrics, tables, rendered via the existing
  escaped-markdown pipeline. Shown only when the session has posted a
  result (same "only render what's present" pattern the other panels
  already use).

### Phase 4 — "Integration status" dashboard card (PWA)
- New, **opt-in** card type (not in `defaultDashCards()`) — one row
  per `schedule_name`: last status, summary, time, a sparkline across
  recent runs (reusing `results.schedule_history_limit`'s retained
  history), click-through to that run's session.
- Listed prominently in the card picker; documented with a
  screenshot/diagram showing exactly where to find and add it.

### Phase 5 — docs + imap-mcp coordination
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
- **CLI**: no new subcommand planned initially — the shell-wrapper use
  case (imap-mcp's own wrapper) talks REST directly with its minted
  token; revisit if an operator wants a `datawatch schedule-results`
  CLI verb.
- **Comm channel**: excluded, same reasoning as every other
  operator-diagnostic surface this session has excluded it for — a
  result payload isn't a chat-channel concern. Revisit only if
  explicitly requested.
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
- A CLI verb for posting/reading results — revisit if asked.
- Comm-channel exposure — revisit if asked.
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
