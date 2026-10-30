# Status / Stats tab gap for Automaton task sessions — findings

Status: needs_review

Date: 2026-10-14. Read-only investigation, run as an Automaton task (PRD
`5d599cef`, story `09610293`, task `09611fc3`, session `local-node-148a`).
That session is a claude-code Automaton task session itself, so it was used
as the live "autonomous" specimen. The interactive comparison is
`local-node-ab22` (claude-code, same project dir).

## What the operator saw

On the Status sub-tab:

- **Current focus** showed "—".
- The **Sprint/Automata** card showed a raw JSON dump.
- The **Tests** card said "no test signal yet — hook payload tests=… expected".
- The **Git** card said "no git state — hook payload git=… expected".
- The footer said "Council / Skills / Tracker / closed-task summaries appear
  once hook payloads include those fields".
- The header showed "hooks alive" and "last UserPromptSubmit".

On the Stats sub-tab:

- `session_stats_no_envelope_body` (no process envelope).
- `stats_llm_more_soon` ("Token rate / latency / model state coming POST v7.0").

Summary: the Status board was built around a hook payload that nothing
produces. The Stats envelope is missing for a different reason:
`observer.RegisterSessionRoot` has no production caller, so no session of any
type ever gets a session envelope.

## Findings

### Q1. Which payload fields feed each card, and does anything send them?

The intended shape is documented in the header comment at
`internal/server/hook_events.go:9-21`:

- `current_task` (free text)
- `sprint` (the whole `.claude/sprint/state.json`)
- `tests` (`{pass, fail, …}`)
- `git` (`{branch, dirty}`)

How the server stores them:

- `hookEventStore.record` (`hook_events.go:156-167`) copies the legacy map
  fields `current_focus`, `sprint`, `tests` and `git` onto the board.
- `mergeTelemetry` (`hook_events.go:195-210`) copies `current_task`, `file`,
  `sprint`, `tests`, `progress`, `parent_session_id`, `guardrail_verdicts` and
  `tasks[]` into `board.Telemetry`. `git` is not mirrored into Telemetry.

How the PWA reads them:

- **Current focus** is `board.current_focus.task || last_event.payload.current_task || '—'`
  (`internal/server/web/app.js` ~4855).
- **Sprint** is `JSON.stringify(board.sprint)` (`app.js:4873`).
- **Tests** reads `board.tests` (`app.js:4876`).
- **Git** reads `board.git` (`app.js:4883`).

Nothing sends these fields today, for any session type:

- **No code anywhere generates `state.json`.** A repo-wide grep finds
  `state.json` only in the `hook_events.go` comment, the hook script that
  *reads* it, and three docs (`docs/howto/claude-hooks.md:100,203`,
  `docs/howto/session-telemetry.md:59`, `docs/flow/telemetry-flow.md:9`). All
  three docs describe it as something the operator's own project maintains.
- **The installed `post-event.sh` builds its payload from `state.json` or `{}`**
  (`internal/hookinstaller/install.go:138-142`). The only thing it adds is
  `tasks[]`, parsed from TodoWrite's stdin (`install.go:144-160`). It never
  computes `current_task`, `tests` or `git`.
- **The script ignores Claude Code's native hook JSON.** Claude Code passes a
  JSON object on stdin: `session_id`, `transcript_path`, `cwd`,
  `hook_event_name`, plus `tool_name`/`tool_input`/`tool_response` for
  PostToolUse and `prompt` for UserPromptSubmit. The script reads stdin only
  for PostToolUse (`install.go:132-136`) and uses it only for the TodoWrite
  branch. The prompt text, cwd and tool data are all dropped.
- **The TodoWrite branch probably never runs (to confirm).** The settings
  entry passes the tool name as `$TOOL_NAME` (`install.go:112`). Claude Code
  delivers the tool name in stdin `tool_name`, not as an environment variable,
  so `$2` is probably empty and the `"$TOOL" == "TodoWrite"` check
  (`install.go:146`) never matches. If so, `tasks[]` is never populated either.
  Recent Claude Code builds may also track todos with tools other than
  `TodoWrite`. Both points need confirming against the installed Claude Code
  version before anyone relies on them.
- **Non-claude backends emit events without these fields.** The daemon itself
  emits synthesized events via `server.RecordHookEvent`
  (`cmd/datawatch/main.go:7140-7165`). Their payload is
  `{backend, state, prev_state, task}`. The key is `task`, not `current_task`,
  so the focus card ignores it.
- **Live check, 2026-10-14 20:52.** `telemetry_get` returns only
  `{"updated_at": …}` for both `148a` (autonomous) and `ab22` (interactive),
  so the telemetry struct exists but every field is empty.

### Q2. Does datawatch ship a hook script that computes these fields, and is it installed for Automaton task sessions?

It ships a script, but the script does not compute these fields (see Q1).

**Install path.** `hookinstaller.Install` runs from the `SetOnSessionStart`
callback (`cmd/datawatch/main.go:2222-2246`). Conditions:
`sess.BackendFamily == "claude-code"` and `session.auto_install_hooks` is not
`false`. A nil value defaults to true (`config.go:1661-1667`).

**Automaton tasks take the same path as interactive sessions:**

- The local spawn path POSTs to `/api/sessions/start`
  (`main.go:~4495-4520`). The request body comes from
  `buildAutonomousSessionBody` (`cmd/datawatch/autonomous_effort.go:89-128`):
  `one_shot: true`, the task's `project_dir`, `prd_id`, `task_id`, `story_id`.
- That goes through `Manager.Start`, a tmux one-shot rather than a
  subprocess, which calls `onSessionStart` (`internal/session/manager.go:2167`).
- So an Automaton claude-code task gets the same install attempt into its
  `project_dir` (the worktree, in worktree-mode PRDs) as an interactive
  session.
- Exceptions: the subprocess path (`manager.go:1877-1890`) forces
  `BackendFamily = "subprocess"`, and the agent-virtual path (`main.go:4466`)
  never calls Start. Neither installs hooks.

**Live comparison: neither session type is populated.**

- `/home/user/workspace/datawatch/.claude/` has no `settings.json` and no
  `sprint/` directory. Both `ab22` (interactive) and `148a` (autonomous, just
  started) run in that project dir.
- `/home/user/workspace/datawatch-app/.claude/sprint` (session `9245`) is also
  absent.
- So on this host `Install` is not taking effect for either session type.
  Possible causes:
  - `session.auto_install_hooks: false` in the operator's YAML. The MCP
    `get_config session` output does not show the key, and the config file is
    outside this task's read scope.
  - `Install` returning an error, which is logged as `[hookinstall] <id>: …`.
    The daemon-log tail available here is from an older boot and does not
    cover these sessions.
- Even if installed, the script would only add `tasks[]` (Q1).

**Why the header says "hooks alive / last UserPromptSubmit" anyway.** The
daemon's own state-change events produce it. `RecordHookEvent` stamps
`HookHealth = "alive"` (`hook_events.go:140`) and maps a
`waiting_input → running` transition to `UserPromptSubmit`
(`main.go:7146-7148`). The header says "hooks alive" whether or not a hook
script is installed, which is misleading.

**Where the raw JSON comes from.** `handleSessionStatus`
(`hook_events.go:497-519`) builds the `sprint` map
`{sprint_id, title, status, automata, task_id}` from the PRD when
`sess.PRDID != ""` (B74, v8.20.9). The PWA prints it with `JSON.stringify`.

### Q3. Why does the observer have no envelope for this session?

**The config is not the cause:**

- `cfg.Observer.SessionAttribution` is a `*bool` (`internal/config/config.go:2227`).
- The daemon starts from `observerpkg.DefaultConfig()` (`main.go:5724`), which
  sets `SessionAttribution: true` (`internal/observer/types.go:434`).
- It is overridden only when the pointer is non-nil (`main.go:5738-5740`), so
  an empty `observer: {}` keeps the default `true`.
- Live `observer_config_get` confirms `envelopes.session_attribution: true`.
- The REST setter is `internal/server/api.go:7044`. The only place it is forced
  off is the standalone `cmd/datawatch-stats/main.go:143`.

**The real cause:**

- Pass 1 of `classify` (`internal/observer/envelopes.go:95-107`) attributes
  only processes under a `SessionRoot.RootPID` taken from `gRoots`.
- `gRoots` is filled only by `RegisterSessionRoot` (`envelopes.go:52`), which
  is keyed by session FullID and rooted at the tmux pane PID.
- `RegisterSessionRoot` has **no production caller**. The only call is in
  `internal/observer/observer_test.go:12`. Its doc comment ("called by
  session.Manager when a session starts") and the `docs/flow` pipeline diagram
  describe wiring that was never built (`git log -S` shows only the original
  BL171 commit `f48c0a2b` and a docs regeneration).
- So the roots map is always empty, and no session of **any** type gets a
  `session:<full_id>` envelope.
- Live `observer_envelopes` confirms this: zero `kind: session` envelopes
  while `ab22`, `9245`, `ddeb` and `148a` are all running.

**The PWA fallback also misses:**

- When there is no session envelope, the PWA looks for `backend:claude-code`
  (`app.js:4565-4569`), using `sess.backend_family`.
- The observer's signature name is `claude`, so its envelopes are
  `backend:claude` / `backend:claude-docker` (`types.go` signatures).
- The names never match, so a claude-code session always falls through to
  `session_stats_no_envelope_body`.
- Separately, live output has `backend:claude-docker` but no host
  `backend:claude`. Host claude processes therefore land in `system`, probably
  because their `comm` / `cmdline[0]` does not match `claude`. That was not
  investigated further.

**Can tmux-less sessions be attributed?** Autonomous claude-code tasks are tmux
one-shots (`TmuxSession != ""`), so once roots are registered they can be
attributed by pane PID. Subprocess spawns (`TmuxSession == ""`) and
agent-virtual sessions have no pane. They would need a root PID from
`runSubprocess`'s `exec.Cmd.Process.Pid`, or no envelope at all.

**Other knobs that gate attribution:**

- `observer.plugin_enabled` and `process_tree.enabled` (both true live).
- The pane-PID lookup depends on the daemon reaching the dedicated tmux socket.
  `TMUX_TMPDIR` must be set in the daemon environment; see the dedicated-socket
  setup in `datawatch-tmux.service`.

### Q4. Are Council / Skills / Tracker / closed-task summaries and LLM token rate / latency planned, or broken?

**Council / Skills / Tracker / closed-task summaries were never built.** The
footer copy is locale key `status_followup_note` (`app.js:4911`, plus the
de/es/fr/ja locale files). The only plan reference is
`docs/plans/historical-plans/2026-05-10-datawatch-app-pending-issues.md:66`,
which is about "Set up ↗" empty-state links, not about building these cards.
No active backlog item in `docs/plans/README.md` builds them, and no server
code produces those payload fields. They are aspirational placeholders from
the v7.0.0-alpha.34 Status board.

**Token rate / latency / model state is also unbuilt.** The copy
(`app.js:4744`, `stats_llm_more_soon`) points to "#276 Grafana sprint" and
"POST v7.0". Neither appears in `docs/plans/README.md`. The current version is
v9.0.86, so the copy is stale. Per-session `tokens_in`, `tokens_out` and
`est_cost_usd` already exist (`internal/session/store.go:161-167`) and are
served per session by `handleCostSummary` (`internal/server/cost.go:33-45`,
`?session=`). The Stats tab simply doesn't show them.

### Q5. Would a live feed via the lifecycle event dispatcher fix a real latency problem?

No, not for this gap.

- The Status sub-tab already re-polls `/api/sessions/<id>/status` every 5s
  while open (`app.js:4803-4812`).
- It also receives `hook_update` WS pushes (`app.js:1056`, from
  `handleSessionHookEvent` → `hub.BroadcastHookUpdate`, `hook_events.go:407`).
  In-process `RecordHookEvent` events do not broadcast, but the 5s poll covers
  them.
- The Stats sub-tab also polls every 5s (`app.js:4579`).
- Task status changes already reach the Automata panel through the `prd_update`
  WS message (`app.js:1033`), now driven by the BL427 Phase 1 dispatcher
  (v9.0.85).

Task transitions take minutes, so a 5s refresh is adequate. The problem is
missing or unrendered data, not latency. If the Status tab later gets an
`automaton` block, a cheap improvement would be to re-fetch on a `prd_update`
whose PRD id matches `sess.prd_id`, reusing the existing message rather than
adding a new feed. BL427's later external-delivery phases could also feed the
Status tab, but nothing here depends on them.

## Already-available data map

| Panel card | Existing source field / function | How to reach it from a session id |
|---|---|---|
| Current focus (task title) | `Task.Title` (`internal/autonomous/models.go:556`) | `sess.PRDID` + `sess.TaskID` (`internal/session/store.go` ~241), then `autonomousMgr.GetPRD(prdID)` and find the task in `Story[].Tasks[]`. Fallback: `autonomousMgr.FindTaskBySessionID(fullID)` (`internal/autonomous/manager.go:2299`, O(PRDs×tasks) scan). |
| Current focus (detail) | `Task.Spec` (`models.go:557`) | same as above |
| Sprint / Automata: story | `Story.Title`, `Story.Status` (`models.go:464-469`) | `sess.StoryID`, or the story containing `TaskID` |
| Sprint / Automata: PRD | `PRD.Title`, `PRD.Status` (`models.go:121,177`) | `sess.PRDID` → `GetPRD` (already used by `handleSessionStatus`) |
| Task status / wait | `Task.Status`, `Task.WaitReason` (`models.go:558,561`) | task lookup as above |
| Verification | `Task.Verification` → `VerificationResult{ok, severity, summary, issues, verifier_backend, verifier_model, verifier_source}` (`models.go:567,722-741`) | task lookup |
| Files | `Task.FilesPlanned` (json `files`), `Task.FilesTouched` (`models.go:684-685`) | task lookup |
| Git | `Task.PreTaskSHA` (`models.go:612`), `Task.ForeignFileDrift` (`models.go:628`), `Verification.DiffSHA` | task lookup |
| Decisions / history | `PRD.Decisions []Decision` (`models.go:50-62,192`) | `GetPRD`. **`Decision` has no task-id field**, so filtering to one task has to match on `Note` / `Kind` / time window around the task's run. Flag this to the implementer; don't paper over it. |
| Cost / tokens | `sess.TokensIn`, `sess.TokensOut`, `sess.EstCostUSD` | `GET /api/cost?session=<id>` → `handleCostSummary` (`internal/server/cost.go:33-45`), or directly from the session record |

Future enrichments, noted only and not designed here:

- Capacity wait reasons from the not-yet-built project-tree lock phase of
  BL428 would extend `WaitReason`.
- BL427's later external-delivery phases are a possible live-feed consumer for
  this panel.

## Recommendation

Follow-on fix tasks, in order:

1. **Status tab: render an Automaton card (bounded, high value).**
   - Add an additive `automaton` block to `GET /api/sessions/<id>/status`,
     populated server-side from `sess.PRDID` / `sess.TaskID` with the fields in
     the map above: PRD title/status, story title, task title/status/spec
     excerpt, wait_reason, verification summary, files planned/touched,
     pre_task_sha, foreign_file_drift, plus the session's tokens and cost.
   - In the PWA, render it as structured cards instead of
     `JSON.stringify(board.sprint)`.
   - Use `Task.Title` for Current focus when there is no hook focus.
   - Keep the legacy `sprint` synthesis for backward compatibility.
   - For non-Automaton sessions, hide the Tests/Git placeholders, or reword
     them to "not reported by this backend" rather than "hook payload …
     expected".
   - Remove or reword the `status_followup_note` footer and the stale
     `stats_llm_more_soon` "POST v7.0" copy in all locales.
   - Show tokens and cost on the Stats tab from data that already exists.
2. **Observer session attribution: bounded and confidently fixable.**
   - The cause is a single missing call. The minimal fix: in the existing
     per-tick loop at `cmd/datawatch/main.go:~6185-6200`, which already
     resolves `#{pane_pid}` for every running / waiting / rate-limited tmux
     session, call `observerpkg.RegisterSessionRoot(sess.FullID, sess.Name, st.PanePID)`.
   - Call `UnregisterSessionRoot` for sessions that are no longer active.
     Registering every tick is idempotent; "last write wins" is documented.
   - Also fix the PWA backend fallback name mismatch (`backend:claude-code` vs
     the `claude` signature), either by mapping `claude-code → claude` in
     `app.js:4568` or by matching on the envelope label.
   - Subprocess and agent-virtual sessions are out of scope for the minimal fix.
3. **Hook script fields: defer.** Making the hook script compute `tests` and
   `git` would mean inventing a convention: running the test suite or
   `git status` inside every hook call is costly and specific to each project.
   Most of the value is covered by step 1, which uses daemon-side data that
   already exists. Two narrower items are worth separate small tasks, but only
   after the operator confirms them:
   - **(a) Why `Install` isn't taking effect on this host.** Check
     `session.auto_install_hooks` in the YAML and grep the daemon log for
     `[hookinstall]`. Do not change code until that's known.
   - **(b) The `$TOOL_NAME` argument (Q1).** If it is confirmed empty under
     current Claude Code, the TodoWrite parse should read `tool_name` from
     stdin instead.
   - Also: the "hooks alive" header should distinguish "hook script events"
     from "daemon-synthesized events". The current label overstates hook
     installation.
   - `session.hook_script_dir` (`config.go:1669-1673`) has no consumer anywhere
     in the codebase. It is dead config; record it for later cleanup and do
     not fix it now.

## Parity surface

Parity surface (inherit into every plan/story note):

- **REST:** `GET /api/sessions/<id>/status` gains an additive `automaton`
  block. Existing fields are unchanged.
- **PWA:** Status sub-tab cards (Current focus, Automaton/Sprint, Tests/Git
  empty-state copy, footer) and Stats sub-tab (envelope lookup fallback,
  tokens/cost, stale copy removal).
- **MCP:** the existing `telemetry_get` / `telemetry_list` / session tools stay
  unchanged. MCP does **not** need a new `automaton` block: an MCP client
  already gets the same data from `autonomous_prd_get` (task, verification,
  files, decisions) plus `cost_summary` / `list_sessions` (tokens, cost,
  `prd_id`/`task_id`/`story_id`). The block is a display convenience for the
  PWA. Revisit if an MCP consumer asks for a single-call session view.
- **CLI / comm channel / YAML-config:** excluded. This is a read-only display
  of data already exposed elsewhere, with no new config.
- **Android + iPhone/iOS:** excluded from this PRD's code. The PWA change is
  visible to the operator, though, so a datawatch-app parity issue is required
  under the AGENT.md Mobile-Parity Rule.

## Resolution

Date: 2026-10-14. Acted on by a follow-on Automaton fix task.

| Item | Outcome | Where |
|---|---|---|
| Observer session attribution | **Fixed** | `22382029` — see below |
| Stats tab backend fallback / copy | **Fixed** | `98e233a9` — see below |
| Hook payload (`sprint` / `tests` / `git` / `current_task`) | **Deferred, no code change** | see below |
| Council / Skills / Tracker / closed-task summaries | **Confirmed not yet built** | out of scope |
| LLM token rate / latency / model state | **Confirmed not yet built**; copy corrected | out of scope |
| Live feed via lifecycle events | **Not needed for this gap** | see below |

**Observer attribution: fixed (`22382029`).** The cause in Q3 was bounded
and confirmed: `RegisterSessionRoot` had no production caller.
`internal/observer/envelopes.go` gains `SyncSessionRoots`, which replaces
the roots map in one step and skips entries with an empty id or no PID.
`cmd/datawatch/main.go` calls it from the session stats callback, which
already resolves `#{pane_pid}` for every running / waiting-input /
rate-limited session. Ended sessions drop out on the next tick. Sessions
with no tmux session name (subprocess, agent-virtual) are never rooted,
because `tmux list-panes -t ""` resolves an unrelated pane. Automaton
claude-code tasks are tmux one-shots, so they now get a
`session:<full_id>` envelope like interactive sessions. The observer's
process-tree walk is unchanged. Unit test:
`TestSyncSessionRoots_ReplacesAndDropsEnded` (`internal/observer/observer_test.go`).
The config `*bool` was confirmed not to be a cause (Q3), so it was not
touched.

Still open, recorded rather than guessed at:

- Subprocess sessions could be rooted at `exec.Cmd.Process.Pid`. That
  needs the PID plumbed out of `runSubprocess`; not done.
- Live output showed no host `backend:claude` envelope (only
  `backend:claude-docker`), so host claude processes may not match the
  `claude` signature. Not investigated; with session roots registered,
  the session envelope no longer depends on it.
- Needs a live check after the daemon runs the new binary:
  `observer_envelopes` should list `kind: session` entries for running
  tmux sessions.

**Stats tab fallback and copy: fixed (`98e233a9`).**

- `app.js` maps `backend_family` `claude-code` to the observer's `claude`
  signature for the backend-envelope fallback.
- `stats_llm_more_soon` now reads "Token rate / latency / model state are
  not built yet" in all five locales (EN placeholder in de/es/fr/ja). The
  `app.js` fallback literal no longer cites an issue number or version.
- `session_stats_no_envelope_body` was incomplete per Q3: it now also
  names "the session has no tmux pane (subprocess or agent sessions) or
  is not running" as a cause. All five locales carry the EN text as a
  placeholder; the app.js fallback literal matches.

**Hook payload: deferred, no code change.** Q1 shows `.claude/sprint/state.json`
is written by nothing, so `sprint` / `tests` / `git` / `current_task` are
aspirational for every session type. Q2 shows Automaton claude-code tasks
already take the same `hookinstaller.Install` path as interactive
sessions, so there is no autonomous-only call-site gap to fix. Per the
recommendation, no hook-side generator was built. The daemon-side
`automaton` block (`9ccd6983`, `ab319bf8`) is the primary source for
Automaton sessions. The narrower follow-ups in Recommendation 3 still
stand and need the operator first: (a) why `Install` isn't taking effect
on this host (`session.auto_install_hooks`, `[hookinstall]` log lines);
(b) whether `$TOOL_NAME` is empty under current Claude Code; and the
"hooks alive" header overstating hook installation.

**Council / Skills / Tracker / closed-task summaries: confirmed not yet
built** (Q4). No backlog item builds them. Building them is out of scope.

**LLM token rate / latency / model state: confirmed not yet built** (Q4).
Only the stale copy was corrected. Recommendation: surface the per-session
`tokens_in` / `tokens_out` / `est_cost_usd` that already exist before
building rate or latency metrics.

**Live feed via lifecycle events: not needed for this gap** (Q5). Both
sub-tabs poll every 5s and receive `hook_update` / `prd_update` pushes.
Optional later improvement: re-fetch the Status tab on a `prd_update`
whose PRD id matches `sess.prd_id`. BL427's external-delivery phases are
out of scope.

Status stays `needs_review`: the hook-payload items and the unbuilt cards
remain open.
