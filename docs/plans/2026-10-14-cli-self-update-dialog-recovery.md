# CLI self-update dialog recovery

Status: investigation only, 2026-10-14, HEAD b51ee247 (v9.0.65). No
non-test Go code was changed.

**Headline finding.** The repeated re-paste of the task does **not**
depend on the Claude Code self-update screen. `handleChannelReady`
re-delivers the full task to a OneShot claude-code session every time
the channel bridge's 60 s ready heartbeat lands while the session is in
(or reaches, within 60 s) `waiting_input`. The update notice was on
screen at the same time, but it did not cause the re-sends. It is also
not a modal dialog. The `Enter to confirm · Esc to cancel` line in these
sessions' timelines comes from the *development-channels startup
dialog*, not from the update.

## Incident

- **348b**: a one-shot verify session. Created 2026-10-14T02:51:12Z,
  killed 03:31:30Z (about 40 min). Its retained pane shows
  `[Pasted text #N]` chips numbered at least up to #80, and
  `✔ Update installed · /restart to apply` in the status/footer row.
- **258d**: a one-shot eval-driver task. Created 04:21:33Z, killed
  04:27:18Z. Its timeline (`session_timeline`) records the full original
  task re-sent with `source:channel-task` about one second after
  **every** transition to `waiting_input` on the idle `❯` prompt:

```
04:22:13Z | state-change | running → waiting_input   (needs-input: ❯)
04:22:14Z | input-sent   | source:channel-task | You are running non-interactively ...
04:25:07Z | state-change | running → waiting_input
04:25:08Z | input-sent   | source:channel-task | You are running non-interactively ...
04:25:37Z | state-change | running → waiting_input
04:25:38Z | input-sent   | source:channel-task | You are running non-interactively ...
04:26:45Z | state-change | running → waiting_input
04:26:46Z | input-sent   | source:channel-task | You are running non-interactively ...
04:27:09Z | state-change | running → waiting_input
04:27:18Z | killed
```

258d's pane tail at kill time shows a normal working Claude Code TUI
(`2 shells, 1 monitor still running`, idle `❯`). It was idling between
turns while a background monitor ran. That idle prompt is a perfectly
normal `waiting_input`, and every heartbeat in that state pasted the
task again.

348b's timeline has no `input-sent` entries, only the three startup
`needs-input` events. Its pane alone proves the repeated pastes. Two
things about 348b are still unexplained: why its timeline lacks those
events, and why its chips alternate between `+81 lines` and
`+161 lines`. One guess is two different texts per cycle, but that is
unverified. See *Open questions*.

## Real wording

Source: **(a) retained pane output of session 348b via the datawatch
`session_output` MCP tool**, plus the 258d/348b `session_timeline`.
Corroborated by **(c)**: public Claude Code issue reports quote the
same "Update installed · Restart to apply" notice
([issue 1730](https://claudeissues.com/issue/1730-bug-restart-to-apply-stuck-no-way-to-update),
[issue 425](https://claudeissues.com/issue/425-feature-add-a-restart-option)).
Source (b), a forced self-update on a sandbox daemon, was not attempted.
An auto-update cannot be triggered on demand without changing the
installed CLI, which is outside the write scope.

The verbatim update text is one segment of the footer row:

```
✔ Update installed · /restart to apply
```

- **Confidence that this string is verbatim: high.** It was captured
  directly and matches the public reports.
- **Confidence that it is non-modal: high.** It shares a row with
  `paste again to expand` and sits directly above
  `⏵⏵ bypass permissions on (shift+tab to cycle)`. Neither pane shows a
  header, box or Enter/Esc line belonging to it.
- **No `/restart to apply` modal with an Enter/Esc line was observed.**
  The only `Enter to confirm · Esc to cancel` line in either session is
  the development-channels startup dialog:
  ```
  WARNING: Loading development channels
  ❯ 1. I am using this for local development
  Enter to confirm · Esc to cancel
  ```
  That dialog is already handled by the startup auto-accept. Both
  sessions passed it within a second.

Fixture: `internal/session/testdata/claude_self_update_dialog.txt` is
one repeat block of 348b's pane, saved exactly as `session_output`
returned it (already ANSI-stripped). The missing letters
(`byp ss permissio s`, `paste  gain to expa d`) and the stray `(B` are
capture artifacts of the overlapping redraws. They are deliberately
left as captured. The filename keeps the requested name, but the
content is a footer notice, not a dialog.

## Dismiss key

**No keystroke is needed.** The notice is a passive footer message. It
does not take focus, and the CLI keeps running the current task with it
on screen. 258d kept working: it ran bash and monitors and posted
progress. Sending either key "to dismiss it" would land in the input
box: Enter would submit whatever is there, and Esc would interrupt a
running turn. Neither is safe to send blindly.

If a future CLI version ever shows a real modal update prompt, the
recommendation is **Esc** (dismiss without restarting, keeping the
conversation). That recommendation is **not confirmed live**, because
no such modal could be reproduced. `/restart` is not a documented
command (see issue 425), and the documented way to apply an update is
`claude update` / relaunch. A one-shot worker should never apply it
mid-task.

## Reproduction

Throwaway test, run and then deleted (not committed). It used
`internal/server/gh202_channel_ready_test.go`'s `bl90Server` harness and
`FakeTmux`. The pane was seeded with the fixture above, so the update
footer was on screen during the test:

- Session: OneShot, `BackendFamily: claude-code`,
  `State: waiting_input`, Task set.
- POST `/api/channel/ready` three times, the same idempotent body the
  heartbeat sends.
- After each delivery, set the state back to `waiting_input`. This is
  what the monitor does when Claude goes idle at `❯`.
- Count the `send-keys*` calls whose text contains the task.

```
$ go test ./internal/server/ -run TestRepro_ChannelReadyHeartbeatResendsTask -v -count=1
[channel] ready for session testhost-258d (channel_ready=true, port=40000)
    POST #1 /api/channel/ready -> 200 {"session_id":"testhost-258d","status":"ok"}
[channel] task delivered to testhost-258d via send_input
    after POST #1: task text sent via tmux send-keys 1 time(s)
[channel] ready for session testhost-258d (channel_ready=true, port=40000)
    POST #2 /api/channel/ready -> 200 {"session_id":"testhost-258d","status":"ok"}
[channel] task delivered to testhost-258d via send_input
    after POST #2: task text sent via tmux send-keys 2 time(s)
[channel] ready for session testhost-258d (channel_ready=true, port=40000)
    POST #3 /api/channel/ready -> 200 {"session_id":"testhost-258d","status":"ok"}
[channel] task delivered to testhost-258d via send_input
    after POST #3: task text sent via tmux send-keys 3 time(s)
    REPRO CONFIRMED: full task text re-sent 3 times by handleChannelReady
--- PASS: TestRepro_ChannelReadyHeartbeatResendsTask (7.50s)
```

`GetActiveStartupPromptInput` returned "" for the update footer, so the
footer does not hold delivery back either.

No live sandbox-daemon run was done: the unit repro plus the 258d
production timeline already show the behaviour on today's code. The
`daemon_logs` MCP tail turned out to be from an old daemon log (it
showed v9.0.5), so the `[channel] task delivered` lines for these
sessions could not be quoted. The `source:channel-task` timeline
entries are the equivalent evidence.

## Root cause

1. `cmd/datawatch-channel/main.go:233` starts `readyHeartbeat`, which
   re-POSTs `/api/channel/ready` every
   `channelReadyHeartbeatInterval = 60 * time.Second`
   (`cmd/datawatch-channel/main.go:61`, loop at `:475`). It was added so
   a restarted daemon re-learns bridge ports, and it is intentionally
   idempotent.
2. `handleChannelReady` (`internal/server/api.go:8333`) is **not**
   idempotent for OneShot sessions. Every call that finds `Task != ""`
   and `OneShot` starts a goroutine (`api.go:8442`). That goroutine
   polls for up to 60 s for `GetActiveStartupPromptInput == ""` and
   `State == waiting_input`, then calls
   `s.manager.SendInput(sessID, input, "channel-task")` (`api.go:8451`).
   Nothing records that the task was already delivered.
3. The heartbeat period and the poll window are both 60 s, so in effect
   **any** `waiting_input` moment of a live claude-code one-shot gets the
   task re-pasted. That includes the idle `❯` between turns while
   background work runs, a permission prompt, or any other prompt match.
   Roughly one paste per minute while the session is idle explains 348b's
   ~80 chips over ~40 min.
4. A side effect that makes it worse: each re-delivery calls
   `MarkTaskDelivered` (`api.go:8457`, which sets `taskDeliveredAt` at
   `internal/session/manager.go:2776`). That re-arms the task-echo
   completion suppression read at `manager.go:2389`, `:5555` and
   `:5869`, so a session that keeps getting its task re-pasted also
   keeps having genuine `DATAWATCH_COMPLETE` detection suppressed.

The self-update notice is incidental. Both affected sessions were
simply long-lived one-shots that went idle, and the update happened to
be displayed at the same time.

## Call site

Every path that sends a session's task text as input:

| Path | Location | Guard | Fired here? |
|---|---|---|---|
| Channel ready → OneShot send_input | `internal/server/api.go:8451` (goroutine at `:8442`, handler `:8333`) | none | **Yes.** `source:channel-task` in 258d's timeline; claude-code session; reproduced |
| TUI one-shot delivery | `cmd/datawatch/main.go:7125` (`isTUITask` at `:7082`, `tuiTaskDelivered` at `:248`) | `BackendFamily != "claude-code"` plus `tuiTaskDelivered.LoadOrStore` (once per session) | No. It excludes claude-code. It also appends a `DATAWATCH_COMPLETE` suffix and logs `[tui-task]` |
| Autonomous worker-stall retry | `internal/autonomous/executor.go:909` (`ErrWorkerStalled`, `internal/autonomous/capacity.go:25`) | `auto_fix_retries` budget | No. It `continue`s the task loop, which spawns a **new** session, not a paste into the same pane |

Both send-input paths use the same `"channel-task"` source label. That
does not cause confusion here only because the TUI path excludes
claude-code. The fix should give the TUI path its own label (for
example `tui-task`) so timelines can tell them apart.

## Fix plan

1. **Call-site guard (the actual fix).** In `handleChannelReady`, deliver
   a OneShot task at most once per session:
   - Add a persisted `TaskDeliveredAt *time.Time` (or bool) on the
     session record. Check and set it atomically before starting the
     delivery goroutine, and clear it only if `SendInput` fails, as the
     TUI path does with `tuiTaskDelivered.Delete`.
   - It must be persisted rather than an in-memory `sync.Map`. The
     heartbeat exists precisely to survive daemon restarts, and an
     in-memory guard would re-paste once after every restart.
   - Optionally, also have the bridge send `"heartbeat": true` on
     periodic re-announces, and have the handler never deliver a task on
     a heartbeat, only refresh `ChannelReady`/`ChannelPort`. Done
     together, these two changes also cover sessions created before the
     field existed.
   - Regression test, in a new file named for the behaviour (for example
     `channel_ready_task_once_test.go`): this repro, asserting exactly
     one `send-keys` carrying the task after N ready POSTs, and that a
     second POST after a daemon-restart-equivalent (reload the store)
     also sends nothing.
2. **Pattern list.** Add the update notice as *recognised, non-blocking*
   text and do not add it to any prompt or rate-limit list. Concretely:
   - A detection unit test using the fixture to assert that the footer
     alone does not classify as `waiting_input` and does not trip
     `GetActiveStartupPromptInput`.
   - Optionally, a timeline/info event (`cli-update-pending`) so
     operators can see that the worker's CLI changed underneath it.
   - Keep `Enter to confirm`/`Esc to cancel` as they are in
     `promptPatterns` (`manager.go:241`). They correctly match real
     dialogs, such as the development-channels warning.
3. **Recovery.** For the update notice itself, nothing: no keystroke
   should be sent, per *Dismiss key*. If a modal update prompt ever
   appears (none has been observed), handle it the way rate-limit
   recovery works in `processOutputLine`: pattern, then state, then a
   goroutine sending **Esc** once, with an alert. Gate it behind
   `detection` config so it can be disabled.
4. **Related, same patch.** Give the TUI path its own source label (see
   *Call site*), and only call `MarkTaskDelivered` on the first, real
   delivery, so completion suppression is not re-armed.

## Pre-fix test run

The regression tests were written first and run against the unmodified
code at v9.0.65. `internal/server/channel_ready_redelivery_test.go`
fails on the first heartbeat after delivery: the task is sent a second
time into a session that already had it. The fresh-session case passes,
which shows first-time delivery worked.

```
$ go test -count=1 ./internal/server/ -run 'TestHandleChannelReady_(HeartbeatNeverRepastes|FreshOneShot)' -v
=== RUN   TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask
[channel] ready for session testhost-258d (channel_ready=true, port=40000)
[channel] task delivered to testhost-258d via send_input
[channel] ready for session testhost-258d (channel_ready=true, port=40000)
[channel] task delivered to testhost-258d via send_input
    channel_ready_redelivery_test.go:142: heartbeat #1: task sent 2 times total, want 1 — task re-pasted into a session that already had it
--- FAIL: TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask (2.01s)
=== RUN   TestHandleChannelReady_FreshOneShotStillGetsTaskOnce
[channel] ready for session testhost-f00d (channel_ready=true, port=40000)
[channel] task delivered to testhost-f00d via send_input
--- PASS: TestHandleChannelReady_FreshOneShotStillGetsTaskOnce (1.00s)
FAIL
FAIL	github.com/dmz006/datawatch/internal/server	3.013s
FAIL
```

`internal/session/self_update_dialog_test.go` does not compile against
the unmodified code. The notice recognition (`selfUpdatePatterns`,
`matchesSelfUpdateDialog`, `noteSelfUpdateDialog`) and the once-only
delivery record (`ClaimTaskDelivery`/`TaskDelivered`) did not exist yet:

```
$ go test -count=1 ./internal/session/ -run 'SelfUpdate|TaskDelivery_Claimed'
# github.com/dmz006/datawatch/internal/session [github.com/dmz006/datawatch/internal/session.test]
internal/session/self_update_dialog_test.go:65:9: m.matchesSelfUpdateDialog undefined (type *Manager has no field or method matchesSelfUpdateDialog)
internal/session/self_update_dialog_test.go:102:8: m.matchesSelfUpdateDialog undefined (type *Manager has no field or method matchesSelfUpdateDialog)
internal/session/self_update_dialog_test.go:175:18: mgr.selfUpdateNotedAt undefined (type *Manager has no field or method selfUpdateNotedAt)
internal/session/self_update_dialog_test.go:186:10: mgr.noteSelfUpdateDialog undefined (type *Manager has no field or method noteSelfUpdateDialog)
internal/session/self_update_dialog_test.go:192:10: mgr.noteSelfUpdateDialog undefined (type *Manager has no field or method noteSelfUpdateDialog)
internal/session/self_update_dialog_test.go:200:10: mgr.noteSelfUpdateDialog undefined (type *Manager has no field or method noteSelfUpdateDialog)
internal/session/self_update_dialog_test.go:233:18: mgr.selfUpdateNotedAt undefined (type *Manager has no field or method selfUpdateNotedAt)
internal/session/self_update_dialog_test.go:249:9: mgr.TaskDelivered undefined (type *Manager has no field or method TaskDelivered)
internal/session/self_update_dialog_test.go:252:10: mgr.ClaimTaskDelivery undefined (type *Manager has no field or method ClaimTaskDelivery)
internal/session/self_update_dialog_test.go:255:9: mgr.ClaimTaskDelivery undefined (type *Manager has no field or method ClaimTaskDelivery)
internal/session/self_update_dialog_test.go:255:9: too many errors
FAIL	github.com/dmz006/datawatch/internal/session [build failed]
FAIL
```

The session tests assert **zero** keystrokes for the notice, not "one
dismiss key". *Dismiss key* above establishes that the notice is a
passive footer and that sending either key would be unsafe.

## Outcome (v9.0.66)

- `handleChannelReady` claims delivery once per session via the
  persisted `Session.TaskDeliveredAt` (`Manager.ClaimTaskDelivery`). A
  failed or timed-out send releases the claim, and `Restart` clears it.
  `MarkTaskDelivered` now only runs on the single real delivery, so
  completion suppression is no longer re-armed.
- Delivery is **not** held back while the update footer is on screen.
  The footer stays until the CLI restarts and does not block input, so
  gating on it would strand a fresh session's task.
- The update notice is matched by `selfUpdatePatterns` in
  `processOutputLine`, in the screen-capture scan, and in
  `matchPromptInLines` (skipped). It is logged as `[self-update]`, at
  most once per 10 s per session, and refreshes `LastChannelEventAt`
  without changing state. No key is sent.
- Not done: the optional bridge `"heartbeat": true` flag, the TUI path's
  separate source label, and the open questions above.

## Live validation

Run 2026-10-14 04:58–05:05Z against a **sandbox** daemon built from
`507c0ccb` (`make build` → `bin/datawatch`, `datawatch v9.0.66`). The
production daemon was not touched. **Result: PASS** for once-only
delivery and for the notice being handled with no keystroke, with two
deviations from the expectations in the request, described below.

### Two expectations that don't match the shipped fix

- The request expected **one `[self-update] dismissed ...` line and a
  key reaching the pane**. The shipped fix deliberately sends **no**
  key (see *Dismiss key*), and its log line reads
  `[self-update] CLI update notice on <id> — passive footer, no key sent, session left as-is`.
  The live check therefore tests the shipped behaviour: the line is
  logged and **zero** keys reach the pane. It does not test a dismiss
  keystroke, because none exists.
- The line is **not logged exactly once per session**. It is debounced
  to once per 10 s and only fires when new output arrives or the
  captured screen changes. That gave three lines per session over the
  run: the first render, the pane redraw after the task was typed, and
  the first scan after each sandbox daemon restart. A static pane logged
  nothing more (01:01–01:03 local, through two heartbeats).

### Setup

Everything lived under `bin/sandbox-selfupdate/`, which is gitignored
and was deleted afterwards. The sandbox had its own `--config` and its
own `data_dir`. `datawatch start` has no `--data-dir` flag, so the data
dir is set via `data_dir:` in the sandbox config, which is how
`scripts/release-smoke.sh` does it. The sandbox also had its own
`HOME`, its own `TMUX_TMPDIR`, and `TMUX` unset. Server:
`127.0.0.1:57399`, plain HTTP, no token, `memory.enabled: false`.

```sh
env -u TMUX -u TMUX_PANE HOME=$SB/home TMUX_TMPDIR=$SB/tmux \
  ./bin/datawatch --config $SB/config.yaml start --foreground --no-mcp
```

The first start inherited `TMUX` from the calling shell, which takes
precedence over `TMUX_TMPDIR`. Its one session
(`cs-dw-selfupdate-sandbox-94a1`) briefly landed on the operator's tmux
server. It was killed by exact name within ~15 s, nothing else on that
server was touched, and the sandbox was restarted from a clean data dir
with `env -u TMUX`. Anyone repeating this from inside a datawatch
session must unset `TMUX`.

Two disposable one-shot sessions were used. Each pane renders
`internal/session/testdata/claude_self_update_dialog.txt` and then
blocks on `read`, appending every line that reaches the pane to a
receive log:

| Session | Backend | Pane program | Delivery path exercised |
|---|---|---|---|
| `2f09` | `claude-code`. The sandbox's `claude-code` LLM `binary` was pointed at a stand-in script: `claude mcp …` is a no-op, and an interactive launch prints the fixture and an idle `❯ ` | fake `claude` | `handleChannelReady` (the incident path) |
| `8414` | `shell` (`shell_backend.script_path` = fixture script, idle `datawatch:fixture$ `) | fixture script | TUI one-shot path (`[tui-task]`) |

There is no real channel bridge in the sandbox, so the bridge's 60 s
`readyHeartbeat` was simulated with the same body it sends, posted on
the same 60 s cadence. That gave three heartbeat posts, spanning two
full intervals. The sandbox daemon was then restarted and a fourth post
was sent.

```sh
for n in 1 2 3; do
  curl -s -XPOST $B/api/channel/ready -H 'content-type: application/json' \
    -d '{"session_id":"dw-selfupdate-sandbox-2f09","port":40999}'
  sleep 60
done
# restart sandbox daemon, wait 20 s, POST once more
```

### Observed

```
05:00:57Z POST #1: {"session_id":"dw-selfupdate-sandbox-2f09","status":"ok"}
05:01:57Z POST #2: {"session_id":"dw-selfupdate-sandbox-2f09","status":"already_delivered"}
05:02:57Z POST #3: {"session_id":"dw-selfupdate-sandbox-2f09","status":"already_delivered"}
05:03:41Z POST #4 (after restart): {"session_id":"dw-selfupdate-sandbox-2f09","status":"already_delivered"}

$ cat -A claude-pane-received.log          # every line that reached 2f09's pane
05:00:58Z^ISELFUPDATE-LIVE-VALIDATION claude-code one-shot task$

daemon stdout:
[channel] ready for session dw-selfupdate-sandbox-2f09 (channel_ready=true, port=40999)
[channel] task delivered to dw-selfupdate-sandbox-2f09 via send_input
[channel] ready for session dw-selfupdate-sandbox-2f09 (channel_ready=true, port=40999)
[channel] task for dw-selfupdate-sandbox-2f09 already delivered — ready re-announce, not re-sending
[channel] ready for session dw-selfupdate-sandbox-2f09 (channel_ready=true, port=40999)
[channel] task for dw-selfupdate-sandbox-2f09 already delivered — ready re-announce, not re-sending
[channel] ready for session dw-selfupdate-sandbox-2f09 (channel_ready=true, port=40999)   # after restart
[channel] task for dw-selfupdate-sandbox-2f09 already delivered — ready re-announce, not re-sending

data_dir/daemon-crash.log (log.Printf target):
00:59:21 [self-update] CLI update notice on dw-selfupdate-sandbox-8414 — passive footer, no key sent, session left as-is
01:00:25 [self-update] CLI update notice on dw-selfupdate-sandbox-2f09 — passive footer, no key sent, session left as-is
01:00:28 [self-update] CLI update notice on dw-selfupdate-sandbox-8414 — ...   (after daemon restart)
01:00:58 [self-update] CLI update notice on dw-selfupdate-sandbox-2f09 — ...   (pane redraw after task typed)
01:03:24 [self-update] CLI update notice on dw-selfupdate-sandbox-2f09 — ...   (after daemon restart)
01:03:24 [self-update] CLI update notice on dw-selfupdate-sandbox-8414 — ...   (after daemon restart)

2f09 session record before restart: state=waiting_input channel_ready=true
  task_delivered_at=2026-10-14T01:00:57.070898716-04:00
2f09 final pane tail:
  ────────────────────────────────────────
  ❯ SELFUPDATE-LIVE-VALIDATION claude-code one-shot task
  ❯
```

- **Once-only delivery: PASS.** Four ready posts, two of them spaced a
  full heartbeat interval apart and one sent after a daemon restart,
  produced exactly **one** task delivery. `task_delivered_at` was
  persisted and survived the restart.
- **Notice gets no keystroke: PASS.** Exactly one line reached 2f09's
  pane over ~4 min, and it was the task itself, with no leading ESC
  byte (`cat -A`) and no stray empty Enter lines. The footer stayed on
  screen the whole time, and the session sat in `waiting_input`. It was
  never classified as rate-limited or stalled.
- **`[self-update]` line: logged, but not exactly once.** It appeared
  three times for 2f09, as explained under *Two expectations that don't
  match the shipped fix*.

### Separate finding (not fixed here)

The **TUI one-shot path** (`cmd/datawatch/main.go`, non-claude-code
backends) re-delivered the shell session `8414`'s task after **every
sandbox daemon restart**. The pane received the four-line task block
three times: 04:59:28Z (first delivery), 05:00:33Z (after restart 1)
and 05:03:29Z (after restart 2). Its once-only guard,
`tuiTaskDelivered`, is an in-memory `sync.Map`, which is the same gap
*Fix plan* item 1 called out for the channel path. That path never
handles claude-code, so it was not the cause of the 348b/258d incident.
It is reported here for the operator to decide on, and has not been
fixed. One fix would be for the TUI path to use the same
`ClaimTaskDelivery` record.

### Cleanup

Both sessions were killed and deleted through the sandbox API
(`/api/sessions/kill`, `/api/sessions/delete` → `{"status":"ok"}`;
`/api/sessions` → `[]`). The sandbox daemon was stopped by its own PID.
Its private tmux server exited with no sessions left
(`no server running on …/bin/sandbox-selfupdate/tmux/…`), and
`bin/sandbox-selfupdate/` was removed. The operator's tmux session list
afterwards contained no sandbox sessions.

## Open questions

- 348b's timeline has no `input-sent` events even though its pane shows
  about 80 pastes. Check whether timeline writes are dropped for some
  spawn path (verify sessions?), or whether a different path delivered
  them.
- 348b's chips alternate between `+81` and `+161` lines, which may be
  two different texts per cycle. 258d's timeline shows only the task.
  Confirm, from the session's stored task length, whether 161 lines
  equals the verify prompt and what the 81-line paste is.
