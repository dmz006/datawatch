---
docs:
  index: true
  topics: [autonomous, prd, recovery, dependencies]
exec_params:
  - {name: prd_id, required: true, description: "Automaton ID (8-char hex)"}
  - {name: task_id, required: false, description: "Task ID to reset/cancel, when acting on a single task"}
exec_steps:
  - tool: autonomous_prd_get
    description: Read the Automaton record — status, per-story/per-task status, DependsOn
    args: {id: "{{params.prd_id}}"}
    read_only: true
  - tool: autonomous_prd_repair_depends_on
    description: Resolve any DependsOn entries still stuck as raw titles instead of real IDs
    args: {id: "{{params.prd_id}}"}
    read_only: false
  - tool: autonomous_prd_reset_task
    description: Reset a failed/blocked task to pending, or force-requeue a completed/cancelled one
    args: {id: "{{params.prd_id}}", task_id: "{{params.task_id}}", force: true}
    read_only: false
---
# How-to: Recover a stuck, failed, or incorrectly-terminal Automaton

[`autonomous-planning.md`](autonomous-planning.md) covers the normal
spawn → plan → run → complete loop. This howto covers the unhappy paths:
a task fails or gets stuck, a story gets cancelled and never restarted,
or an Automaton reaches `completed` while a story inside it is still
`cancelled`. It also covers `repair_depends_on`, a one-time fix for
Automata whose stories/tasks were created before v8.36.5.

## What it is

Three operator-facing repair tools, all scoped to a single story or task
so you never have to discard a partially-completed run:

- **`reset_task`** — puts one task back to pending so the run picks it
  up again. Without `force`, only `failed`/`blocked` tasks qualify. With
  `force: true`, it also requeues a `completed` or `cancelled` task —
  and, as of v8.36.6, this works even when the *Automaton* itself has
  already reached `completed`, and correctly reopens the task's story if
  that story was `cancelled` (previously only `completed`/`failed`
  stories reopened — a cancelled one had no recovery path at all).
- **`cancel_story`** / **`cancel_task`** — stop an individual story or
  task without cancelling the whole Automaton. The most common cause of
  the "completed Automaton with a cancelled story inside it" scenario
  below is cancelling a story mid-run to fix something, then forgetting
  to `reset_task` it back — the run then proceeds around the gap.
- **`repair_depends_on`** — resolves `depends_on` entries against real
  story/task IDs. Decompose can only reference a sibling by its title
  (IDs don't exist until stories are saved), and versions before v8.36.5
  never translated those titles to IDs afterward — so on an Automaton
  planned before that fix, `depends_on` still holds raw titles and was
  never actually enforced. Safe to call on any Automaton; a no-op if
  everything already resolves.

A fourth path, `rejudge` (covered below), is for the opposite stuck
state: an Automaton whose tasks are all already `completed` but which
ended `blocked` on its completion guardrail — it re-runs that guardrail
as a fresh judgment without resetting anything.

## Base requirements

- `autonomous.enabled: true`
- An Automaton that has been decomposed (has stories/tasks) for
  `reset_task`/`cancel_story`/`cancel_task`; `repair_depends_on` works
  even pre-decompose (no-op).

## Recognizing the "completed but a dependency was skipped" scenario

Symptoms in `autonomous_prd_get`:

- Automaton `status: completed`
- One story shows `status: cancelled` with all its tasks `cancelled`
- A sibling story shows `status: completed`, and — before v8.36.5 — its
  tasks' `depends_on` *titles* actually named tasks in the cancelled
  story. Those tasks ran anyway, without their stated prerequisites.

## Fixing it

`reset_task`, `cancel_story`, `cancel_task`, and `repair_depends_on` are
REST + MCP only — there's no `datawatch autonomous` CLI subcommand for
any of them yet (the CLI has `prd-get` and whole-Automaton `prd-cancel`,
but nothing per-story/per-task). Use `datawatch autonomous prd-get <id>`
to confirm the diagnosis, then the REST or MCP calls below to fix it:

1. Confirm the diagnosis — look for a cancelled story next to a
   completed one that depends on it (`prd-get`, or the REST/MCP reads
   below).
2. If this Automaton predates v8.36.5, resolve `depends_on` titles to
   real IDs first, so dependency ordering is actually enforced on the
   retry below (no-op if already resolved).
3. Force-requeue the cancelled story's tasks, in dependency order
   (oldest task ID first is a safe proxy for creation order if you'd
   rather not read the `depends_on` graph by hand).
4. Once the skipped story's tasks complete for real, force-requeue any
   downstream tasks that ran without them, so they redo their work
   using the now-real prerequisite output.

Each `reset_task` call restores the Automaton to `running` and reopens
its story to `pending` automatically; the daemon's executor picks the
newly-pending task back up without any separate "resume" step.

### REST

```bash
curl -sk -X POST https://HOST:PORT/api/autonomous/prds/<prd_id>/repair_depends_on \
  -H 'Content-Type: application/json' -d '{"actor":"operator"}'

curl -sk -X POST https://HOST:PORT/api/autonomous/prds/<prd_id>/reset_task \
  -H 'Content-Type: application/json' \
  -d '{"task_id":"<task_id>","actor":"operator","force":true}'
```

### MCP

```
autonomous_prd_repair_depends_on(id="<prd_id>")
autonomous_prd_reset_task(id="<prd_id>", task_id="<task_id>", force=true)
autonomous_prd_cancel_story(id="<prd_id>", story_id="<story_id>", reason="...")
autonomous_prd_cancel_task(id="<prd_id>", task_id="<task_id>", reason="...")
```

## Re-judging a blocked Automaton whose guardrail verdict was lost (`rejudge`)

A different stuck state from the ones above: every task completed fine, but
the Automaton ended `blocked` because the completion guardrail's verdict was
lost or in doubt. The **rejudge** action asks for that same completion
guardrail to run again — as a fresh judgment — without resetting or retrying
any task:

- The Automaton must be `blocked`; re-judging one that isn't is refused.
- **Every task in every story must be `completed`.** If any task is pending,
  running, failed, or cancelled, the call is refused and names the first such
  task and its status — fix that one first (`reset_task` above) and re-judge
  once the genuine work is done.
- It never resets anything: no task goes back to pending, no story reopens.

Outcome, in every case written to the Automaton's decision log (attributed
to the acting operator) **before** any status change:

- **Pass** — the blocked reason is cleared and the Automaton becomes
  `completed` in the same step.
- **Block** — the Automaton stays `blocked`, and its blocked reason is
  updated from the new verdict.
- **No verdict** (the judge failed or returned nothing) — the Automaton
  stays `blocked` with its previous blocked reason, and an error is
  returned so you see the re-judge produced no judgment.

Reading the judge's verdict:

- **Issues come back normalized to strings, whatever the judge emitted.**
  The guardrail judge and the per-task verifier may return `issues` as
  plain strings or as objects (with `description`/`message`/`file`/`line`/
  `severity` keys); objects are rendered into one readable line (for
  example `[high] pkg/file.go:42: description / message`) before the
  verdict is stored, so an object-shaped or mixed issues value never
  drops the verdict.
- **The raw judge text is kept on the verdict** in `raw_judge` (truncated
  to 16 KiB), so you can inspect exactly what the judge wrote even when
  its format was off.
- **A PRD that was blocked earlier by
  `json: cannot unmarshal object into GuardrailVerdict.issues.0 of type
  string` now passes when rejudged** — the lenient decoder accepts the
  object-shaped issues that previously failed the whole verdict, so the
  re-judge surfaces the real judgment instead of a decode failure.

### REST

```bash
curl -sk -X POST https://HOST:PORT/api/autonomous/prds/<prd_id>/rejudge \
  -H 'Content-Type: application/json' -d '{"actor":"operator"}'
```

### MCP

```
autonomous_prd_rejudge(id="<prd_id>")
```

## What happens to Automata when the daemon restarts

When the daemon starts, it looks at every Automaton that was in flight and decides whether to pick it back up. Two kinds are considered:

- a **running** Automaton, and
- a **failed** Automaton whose failed tasks were all killed by the restart itself (as opposed to tasks that failed on their own merits).

Each is checked against two things: its age, compared with `autonomous.resume_max_age_seconds`, and whether its project directory still exists.

| `autonomous.resume_max_age_seconds` | Meaning |
|---|---|
| `3600` (default) | Resume only Automata that were active within the last hour |
| `0` | Never auto-resume; every candidate is parked |
| `-1` | No age limit (a missing project directory still parks it) |

What happens next:

- **Fresh** (within the limit, directory present): the Automaton is recovered and continues.
- **Stale** (older than the limit, or directory missing): the Automaton is parked as `blocked` with a plain-English reason, an operator alert is raised, and an entry is added to the boot-resume report (`Status().BootResume`). Nothing runs on its own; review it and resume it with the resume action if you still want it.
- **Live worker**: a worker session that is still alive on an `in_progress` task counts as activity, and so does its recent output. An Automaton with a live worker is never treated as stale just because its stored timestamps are old.
- **Resuming a parked Automaton**: use the resume action, which is available everywhere. Approval is not the right action, because a parked Automaton is already approved. REST: `POST /api/autonomous/prds/{id}/resume` with an optional `note`. MCP: `autonomous_prd_resume`. CLI: `datawatch autonomous prd-resume <id> [--note "..."]`. Chat: `autonomous resume <id> [note]`. PWA: the Resume button on the parked Automaton, which shows the park reason. Only an Automaton parked by the boot-resume gate can be resumed this way; any other state is rejected with an error naming the allowed state. A live worker is re-adopted rather than respawned.
- **Cancelled**: an Automaton you cancelled is never recovered.

### Reading the boot-resume report

Each decision is an entry in `Status().BootResume` with an `outcome` of `resumed`, `parked` or `resumed_by_operator`, the age in seconds and a reason. A `parked` entry that also has `live_sessions` means the Automaton was parked (for example its project directory was missing, or auto-resume is off) while a worker session was still alive. Nothing was killed: resuming it re-adopts those sessions instead of starting new ones. A `resumed_by_operator` entry records your resume action and lists the sessions that were adopted.

Old failed Automata are therefore no longer re-launched at every restart.

## Restarting the daemon while a task is in progress

Restarting the daemon no longer wastes a task that was already `in_progress`
with a live worker session. On boot, the resumed run looks at each
`in_progress` task and does one of three things:

- **Adopts the live worker** (the common case, when the worker's session
  survived the restart and its agent had started). The task stays `in_progress` on the *same* session
  id, keeps holding its capacity slot, and the resumed run simply waits for
  the worker to finish and then verifies its result. No second worker is
  spawned and no duplicate session appears for the same task.
- **Sends the task straight to verification** when the worker session is
  gone but its output already recorded a completion marker — the work
  finished in the shutdown window, so verifying the existing output is
  faster than re-running it.
- **Resets the task to `pending`** only when the session is dead *and* no
  completion marker exists, or when the live session is a bare shell whose
  agent never started (see the next section). The old behavior was to reset unconditionally
  and kill the worker; that is no longer the case.

This applies to every worker backend (`claude-code`, `opencode` and
`pi-prompt`). claude-code workers are adopted too: completion detection after
a restart ignores the task text the daemon typed into the session and the
input placeholder, so a restart does not make a running claude-code worker
look finished. The Automaton itself also stays `running` across the restart:
boot-resume recreates its executor under the same lock a manual run uses, so
there is no gap in which another run request could start a second executor
for the same Automaton.

As operator this needs no action. If a restart happens mid-run, watch the
Automaton with `prd_get`: an adopted task keeps its `session_id` and
surfaces an `adopted_session` marker, and the session list shows the same
worker session continuing to run. A reset task is the one to question —
check the session's log for why it died (crash, OOM, operator kill) before
resetting it again.

### A launch that was held at restart

If the daemon restarts while a worker's first launch is still held, the pane
holds only a bare shell with no agent in it. The daemon does not adopt that
shell as the task's session:

- At boot the shell is ended and the task goes back to `pending` with the
  error `session never started`. No retry is counted, and the task is launched
  once more on a new session. A task that was `verifying` on such a session is
  requeued the same way.
- An adopted claude-code worker is held to the same start-confirm bound as a
  launched one, `autonomous.session_start_confirm_seconds` (default 120
  seconds). The bound is measured from the worker's last evidence of life, so a
  worker that was active just before the restart is not ended early. A launched
  worker has no evidence of life until its first sign of work, so its clock
  starts at the first check that finds it silent. A worker that sits in
  `waiting_input` with no sign of work past the bound is ended as a start
  failure. The first two such failures requeue the task and the third fails
  it. A bare shell found at boot does not count toward that budget, and neither
  does the normal retry budget.

What you see: after a restart the task's error reads `session never started`
and it runs again on a new session id. If a task fails on its start budget, its
last pane lines are kept on the task. Check the agent install and any startup
dialog in that pane before you retry the task.

### Launches still running at shutdown

On a stop the daemon waits up to 6 seconds for the HTTP server to finish, so
no new launch arrives over HTTP. It then refuses new session launches and
gives each launch still in progress a fixed 15 seconds to finish. A launch that
finishes inside that time is adopted by the next boot like any other live
session. A launch still running after it is rolled back: its session is ended,
the next boot does not adopt it, and the daemon logs
`[session] rolled back launch <id>: still running at shutdown` for each one.
When every launch finishes in time, nothing is logged. The 15 seconds is a
fixed value, not a config key.

### How a task stays tied to its worker session

- **The link is saved at launch.** The link between a task and its worker
  session is written to disk when the session is created, before the first
  prompt is delivered. A restart at any point after launch therefore still
  knows which session belongs to the task.
- **Boot reconcile adopts an unlinked live session.** If the link is empty
  (for example the daemon stopped between creating the session and saving the
  link), boot reconcile still looks for a live session that belongs to the
  task and adopts it instead of starting a second worker.
- **A reset ends every live session first.** When a task is reset and
  relaunched, every live session of that task and of its worktree is ended
  before the new worker starts, so two workers never write to the same
  worktree. The task history records which sessions were ended.

### Orphan session sweep

A periodic sweep looks for live task worker sessions that are bound to no
non-terminal task. A session is given a grace period before anything is done
to it; the period is `autonomous.orphan_grace_seconds` (default `300`; `0` or
unset uses the default). The sweep compares session ids in short form, the text after the last hyphen, so a worker bound to a task is never ended because of how its id is written.

1. **Report.** The first time a session is seen unbound, the sweep records a
   note in the Automaton's decision history and writes an audit entry
   (`autonomous_orphan_session_seen`).
2. **End.** If the session is still unbound after the grace period, the sweep
   ends it, records the outcome in the history and writes an audit entry
   (`autonomous_orphan_session_ended`).

A session that gets bound to a task again within the grace period is left
alone.

### Checking this against a sandbox daemon (opt-in live test)

The deterministic tests run with `go test ./internal/autonomous/`. The live
check starts a long worker, restarts the daemon mid-task, and asserts the
session id is unchanged, the worker was not killed, no second session was
spawned for the task, and the task reaches verification. It is skipped unless
`DW_LIVE_RESTART_TEST=1`, so normal test runs never touch a daemon. Run it
only against a sandbox daemon, never production, from the daemon's host with
the same `TMUX_TMPDIR` as that daemon:

```sh
env -u TMUX -u TMUX_TMPDIR \
DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 DW_BASE_TOKEN=... \
  DW_LIVE_RESTART_CMD='<command that restarts the sandbox daemon>' \
  go test -count=1 -timeout 25m -run TestDaemonRestartAdoptsLiveWorkerLive -v ./internal/session/
```

The same check for a claude-code worker is a separate test:

```sh
env -u TMUX -u TMUX_TMPDIR \
DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 DW_BASE_TOKEN=... \
  go test -count=1 -timeout 25m -run TestDaemonRestartAdoptsLiveClaudeCodeWorkerLive -v ./internal/session/
```

`DW_LIVE_RESTART_BACKEND` selects the worker backend for that test (default
`claude-code`); the default-backend test ignores it.

Leave `DW_LIVE_RESTART_CMD` unset to restart through the operator path
(`systemctl --user restart datawatch`, falling back to `datawatch restart`);
only do that when that unit is the daemon behind `DW_BASE`. Optional:
`DW_LIVE_RESTART_PROJECT_DIR` sets the probe's project directory.

## Task failed with `no matching files found` (docs-bm25-index) or "no new changes to verify"

Symptom: a task is reported `failed` even though the worker's own work
was fine — either the verification build shows
`pattern assets/docs-bm25-index.json: no matching files found`, or the
task simply failed with "no new changes to verify".

**Cause.** The `internal/docsindex` package go:embeds
`assets/docs-bm25-index.json`, a git-ignored generated artifact. A
fresh git worktree checkout does not contain the file, so packages that
import it (`internal/server`, `cmd/datawatch`) cannot compile, the
build/verification step fails, and the task ends up reported failed
with nothing to verify.

**Fixed now — worktrees self-heal.** When the daemon creates or
reuses a PRD worktree it now prepares the tree: in repos with
`internal/docsindex` it writes the same `{}` placeholder `make
docs-index` writes, only if the file is missing (an existing index is
never touched), keeps it git-ignored (falling back to the worktree's
own `info/exclude` if the ignore rule is absent), and never stages or
commits it. Both new and reused worktrees get this, so this failure
should not recur for worktrees created by the current daemon.

**Manual fix for a worktree created by an older daemon.** Run `make
docs-index` inside the worktree, or create
`internal/docsindex/assets/docs-bm25-index.json` containing `{}` (the
placeholder `make docs-index` would seed). Then retry the task with
`reset_task` (above) if it was left `failed`.

## Worker finished a turn without the completion marker

A claude-code worker sometimes finishes its turn, goes quiet at the prompt
and never prints `DATAWATCH_COMPLETE`. Instead of waiting for the longer
session idle timeout, datawatch sends the worker one nudge.

- **When it fires:** the worker session has been in `waiting_input` or
  `idle` with no new output for `autonomous.worker_idle_nudge_seconds`
  (default `180`), and no completion marker has appeared.
- **What it sends:** one message asking the worker to print its completion
  line, or to continue if it is not finished. Only one nudge is ever sent
  per task attempt.
- **No nudge while work is running:** only work children block a nudge. A
  work child is a process the worker's runner started, such as a build, a
  test run or a long command; while one is alive the worker is not idle, so
  no nudge is sent, and the idle clock restarts once it exits. The worker's
  own channel and MCP servers do not count as work, so a worker that has
  only those running is still nudged. The rule holds after a daemon restart:
  an adopted worker is nudged and blocked by the same work-child check.
- **What gets recorded:** each nudge is stored on the task as an event
  (`nudge_events`: time, count, `session_id` and outcome) and the task's
  `nudge_count` goes up. Both show in `autonomous_prd_get`. The `session_id`
  names the session the nudge was typed into, so a retry in a fresh session
  does not inherit the old nudge state.
- **Outcome values:** a new event is `sent` once the nudge is typed. It then
  becomes `answered` when the worker prints its completion marker or fresh
  output follows, or `stalled` when the worker stays silent past the stall
  window.
- **If the worker answers:** a completion marker or fresh output after the
  nudge ends the wait normally (outcome `answered`) and the task is verified
  like any other.
- **If it stays silent:** after `autonomous.worker_idle_nudge_stall_seconds`
  (default `600`, counted from the nudge) the worker is treated as stalled.
  It goes through the existing retry path (`auto_fix_retries`), the same as
  any other stalled worker; once that budget is spent the task fails.
- **Where the counts show:** `GET /api/autonomous/status` and the MCP
  `autonomous_status` tool report `nudges_total`, `nudge_tasks` (per-task
  counts) and `nudges_by_backend`. The fields are omitted when no nudge has
  been sent.
- **Setting them:** both keys are hot-applied, no restart. Use `config.yaml`,
  the web settings card, `PUT /api/config`, MCP `autonomous_config_set`, or
  the `configure` chat command. A value of `0` means "use the default".

```yaml
autonomous:
  worker_idle_nudge_seconds: 180
  worker_idle_nudge_stall_seconds: 600
```

### Confirming a nudge fired

Three places show what the daemon did with a worker's nudge: the journal,
the task record and the config read.

1. **The wait-start line.** When a claude-code worker finishes its turn, the
   daemon logs one line as the wait for the completion marker begins:

   ```
   [autonomous] post-turn wait start: session <session> task <automaton>/<task>: backend family claude-code, nudge applies=yes, nudge window 3m0s, stall window 10m0s, session idle limit 15m0s, persisted nudge=false
   ```

   The second part of `task <automaton>/<task>` is the Automaton ID and the
   task ID. `nudge applies=yes` means the nudge owns this worker's idle clock.
   `pending` means the session was not visible yet; the daemon reads the
   backend family on each tick until it is, then logs
   `backend family resolved to claude-code; nudge applies=yes`. `no` means the
   worker is not claude-code and is never nudged. `persisted nudge=true` means
   a nudge for this session is already recorded on the task, so it is not sent
   again.

   If the nudge window is not shorter than the session idle limit, a
   `post-turn wait warning` line follows, once for that wait. The idle detector
   can then end the worker before the nudge is sent.

2. **The nudge and its outcome.** When the nudge is typed, the journal gets:

   ```
   [autonomous] idle worker <session> task <automaton>/<task>: nudge 1 sent after 3m0s idle
   ```

   The answer is logged the same way: `nudge answered by completion marker`,
   `nudge answered by new output` or `nudge answered by session completion`.
   A worker still silent after the stall window logs
   `still silent for <duration> after nudge; ending session for retry`.

3. **The task record.** `autonomous_prd_get` shows the task's `nudge_count`
   and `nudge_events`. Each event has `at`, `count`, `session_id` and
   `outcome`. Find the event whose `session_id` is the worker's session: its
   `outcome` is `sent` (typed, no answer yet), `answered` or `stalled`.

**Reading the effective windows.** Unset settings are not shown in
`config.yaml`, so read the windows the daemon actually applies. `GET /api/config`
has an `autonomous` section with two read-only keys:
`worker_idle_nudge_effective_seconds` (the nudge window) and
`worker_idle_nudge_stall_effective_seconds` (the stall window). With both
settings unset they read `180` and `600`. A setting of `0` or less also reads
as the default. The same section is returned by MCP `get_config` with
`section: autonomous`. The keys cannot be set; change the settings themselves.

**What the idle detector says now.** If the session idle detector ends a
worker, its end reason finishes with the nudge history for that session:

- `..., no nudge sent`: no nudge was recorded for the session.
- `..., nudge 1 sent at 2026-10-25T16:20:00Z, outcome answered`: the nudge
  came first. The outcome is `sent` when no answer was recorded.

**Which workers are nudged.** The nudge applies to every claude-code worker
waiting after its turn, whatever status the task shows, including a task in
`verifying` whose only lease is the worker's. A worker whose session was not
yet visible when its wait began is nudged from its first visible tick.

## Task failed with `session never started`

A claude-code worker was handed its task and then sat in `waiting_input`
with no sign of work for longer than `autonomous.session_start_confirm_seconds`
(default `120`). The daemon ends that session and retries the task. After two
such start failures the task stays failed.

- **What counts as started:** an assistant turn; a tool call in progress or
  finished (a `⏺` tool line, a `⎿` result line or a spinner); a process the
  worker started; a change in the pane; or a channel event after the task was
  delivered. Delivering the task alone does not count.
- **A long silent tool call is not a failure:** a worker that is inside one
  long command keeps its tool line and spinner on screen, which keeps the clock
  running. Only silence longer than the bound ends a session.
- **After a daemon restart:** a worker that was adopted is measured from its
  last evidence of life, not from the adoption, so a live worker is not ended
  just because the daemon restarted.
- **Where the output is:** the last 20 pane lines are stored on the task as
  `start_failure_tail`. Read them with `autonomous_prd_get` to see where the
  worker stopped. A dialog nobody answered shows there; a bare shell prompt
  means the agent never launched.
- **If the bound is too short for your first step:** raise it with
  `datawatch config set autonomous.session_start_confirm_seconds 300`. A value
  of `0` means "use the default".

## Task failed with `descendant-only stall`

A worker using a one-shot `pi` backend ended its own run, but has a still-running child process (build, test, long-running command), and for longer than `autonomous.descendant_stall_seconds` (default 2700 = 45 minutes) is not showing any other sign of life. The daemon terminates the attempt as an infrastructure failure and retries the task through the existing `auto_fix_retries` budget; as soon as that budget is exhausted, the task remains failed.

Symptom: the task is reported as `failed` (or is silently retrying), its
detail shows an error terminating with “worker was in descendant-only
silence for …”, and the worktree may still contain the working child
processes that attempt had been waiting for.

- **Why this exists:** a `pi` run writes nothing to the log until it finishes, so the usual log-based stall check does not reach it. Without the descendant-only check, an attempt with a running work child and no other output would wait forever with no safety net.
- **What counts as output:** a live working descendant process, a session log that is up to date, and files written to the working directory. If there is newer evidence, the clock resets.
- **What you see:** a `Task worker descendant-only stall` alert; the task's error ends with “worker was in descendant-only silence for …”; the session's termination reason reads `descendant-only stall detector: …`; and the pane tail and the surviving descendant process are recorded on the attempt.
- **Leftover process cleanup:** the surviving work child processes are signalled with real kernel signals, with a short grace period in between, only while they remain the recorded ones — recycled process IDs are never touched. Which ended, and which survived, are recorded.
- **If you want a different value:** `datawatch config set autonomous.descendant_stall_seconds 5400`. A value of `0` means “use the default 2700” — this setting can not be accidentally disabled.

## Common pitfalls

- **Cancelling a story mid-run and not restarting it.** The rest of the
  Automaton keeps going — a sibling story that `depends_on` the cancelled
  one will still run against missing prerequisites unless you also fix
  `depends_on` (see above) or manually sequence the retries.
- **Forgetting the downstream redo.** Fixing the skipped story alone
  isn't enough — anything that already ran off its absence produced
  output without the real prerequisite content and needs `force`-reset
  too.
- **Reject vs Cancel vs reset_task.** Reject = the Automaton shouldn't
  exist. Cancel (story/task) = stop this piece; the Automaton is fine.
  `reset_task force=true` = bring a stopped or already-finished piece
  back to pending for a genuine retry.

## See also

- [howto/autonomous-planning](autonomous-planning.md)
- [howto/autonomous-review-approve](autonomous-review-approve.md)
- [howto/automata-orchestrator](automata-orchestrator.md)
- [datawatch-definitions](../datawatch-definitions.md)
