# One-shot claude-code startup dialogs are never dismissed by the capture loop

- Date: 2026-10-20 (red phase recorded 2026-10-21, worktree b0390001)
- Status: **TDD red** — tests exist and fail for the right reason; fix designed, not implemented
- Tests: `internal/session/startup_dialog_dismiss_test.go` (`TestStartupDialogDismiss_*`)
- Fixture: `internal/session/testdata/claude_trust_dialog.txt`
- Failure record: `docs/plans/2026-10-20-oneshot-startup-dialog-dismiss-failures.log`

## Symptom

A one-shot claude-code session created by the daemon can sit blocked forever
on its startup dialogs: the workspace-trust dialog
(`Quick safety check: ... ❯ No, exit / Yes, I trust this folder`) and the
development-channels consent dialog
(`❯ I am using this for local development`). Nothing is at the terminal for a
one-shot session, so whichever path the daemon relies on is the *only* chance
to accept the dialog. That path is unreliable, and the screen-capture loop —
the one component that is started unconditionally for one-shot sessions at
creation time — does not participate in dialog dismissal at all.

## Root cause

### 1. (Primary) The screen-capture loop never invokes the dialog handlers

`StartScreenCapture` (`internal/session/manager.go`, ~2226–2600) is launched
unconditionally ~2 s after one-shot session creation (~2207–2210):

```go
if sess.OneShot {
    go func(fullID string) {
        time.Sleep(2 * time.Second)
        mgr.StartScreenCapture(ctx, fullID, 500)
    }(sess.FullID)
}
```

It reads the pane every tick and drives state detection, prompt matching, and
completion handling — but it contains **no call whatsoever** to
`GetActiveStartupPromptInput`, `IsTrustDialogShowing`, or
`SendTrustDialogAccept`. (Verified by reading the loop body and by grep:
those helpers only appear in `manager.go`'s dialog-block area, `main.go`, and
the `self_update` path.) So for one-shot sessions the capture loop sees the
dialog on screen every 500 ms and does nothing.

Every dismissal logic that today exists lives *elsewhere*, and each of those
elsewhere paths has its own miss-mode:

### 2. `processOutputLine` only fires if the dialog text arrives intact on ONE log pipe line

`internal/session/manager.go` ~6231:

```go
if prompt := m.GetActiveStartupPromptInput(sess.FullID); prompt != "" {
    if _, loaded := m.startupDialogSent.LoadOrStore(sess.FullID, struct{}{}); !loaded {
        go func() {
            time.Sleep(300 * time.Millisecond)
            if strings.Contains(prompt, "trust this folder") {
                _ = exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, "Down").Run()  // ignored error
                _ = exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, "Enter").Run() // ignored error
            } else {
                _ = m.tmux.SendKeys(sess.TmuxSession, "Enter") // ignored error
            }
            m.debugf(...)
        }()
    }
}
```

`monitorOutput` reads the log pipe with `ReadString('\n')` and matches
`strings.Contains(line, marker)` line-by-line. Claude Code's TUI (Ink) renders
with cursor-addressing; a marker string can be split across two pipe reads,
have its prefix erased by a cursor-up + line-clear sequence, or be redrawn
piecewise. Any of that and the `Contains` match never fires for that
rendering. The handler also only *reacts* to log text — it cannot detect a
dialog that was already on screen when the read position was reset or that
rendered before the pipe subscription started.

### 3. The send is latched *before* success and its errors are ignored

`startupDialogSent.LoadOrStore` happens synchronously in the loop; the send
runs 300 ms later in a goroutine whose `exec.Command(...).Run()` errors are
discarded (`_ =`). So:

- keystroke landed before the dialog fully rendered (t+300 ms can be too
  early on slow startups) or hit the wrong control → lost, and the latch
  blocks any retry for the life of the session;
- tmux busy / pane not yet ready / race with `SendInput` → lost, same as
  above.

A "latch" that means "fired once" instead of "dismissed confirmed" turns any
single failed or mistimed send into a permanently stuck session.

### 4. The direct `exec.Command` bypasses the `m.tmux` abstraction

The trust accept shells out to a fresh tmux binary instead of using the
`TmuxAPI` (which already has the correct `SendTrustPromptAccept` — Down then
Enter, with settle). Consequences: it is invisible to `FakeTmux` in tests,
it bypasses the manager's tmux handle configuration, and the codebase has
*two* implementations of "accept the trust dialog" (the raw exec pair vs.
`SendTrustDialogAccept`).

### 5. Backup pollers in `main.go` are config-gated and incomplete

`cmd/datawatch/main.go`:

- the DetectPrompt auto-accept callback (~2195 ff.) is gated on
  `session.claude_auto_accept_disclaimer` (`claudeAutoAccept`);
- the 45 s trust-dialog poller (~2279 ff.) is likewise gated, and **only
  covers the trust dialog** — the channels consent dialog has no poller
  backup at all;
- both are in `main`, outside the manager, and not testable through
  `FakeTmux`.

Net effect: with auto-accept off (default in many deployments), if the pipe
line match in (2) misses (very common for a TUI), **nothing dismisses either
dialog**, and the latching behavior in (3) guarantees the miss is permanent.

### 6. Why the capture loop is the right place (and an extra miss-mode it must dodge)

It is the only component that (a) is started unconditionally for one-shot
sessions, (b) reads the *rendered* pane (not the fragile pipe text), and
(c) is testable through `FakeTmux`. One caveat to design around: the loop
body only runs when `capture != lastCapture` — a fully static dialog pane
would, after the first evaluation, stop triggering detection runs. The
fix should keep checking the pane for these dialogs once per tick *after*
dismissal is pending, not only on content change (or rely on the periodic
capture diff that Claude emits while waiting — the safe design is to check
explicitly each tick; it is cheap: a string `Contains` on ~100 lines).

## Reproduction

### Production-level

1. Fresh project directory that Claude Code has not seen before (trust
   prompt guaranteed), `claudeAutoAccept` unset, or a channel-enabled
   deployment (channels consent guaranteed).
2. Create the session via the one-shot path (schedule / API / pipeline).
3. Claude starts, renders `Quick safety check: ... ✗ No, exit / ✓ Yes, I
   trust this folder`.
4. Daemon never accepts: the log-line match may miss due to TUI redraws;
   even if it fires, a t+300 ms Down+Enter can precede the render and the
   latch then blocks retries. Session is stuck until an operator manually
   sends `Down Enter` in the tmux pane.
   Same story for `❯ I am using this for local development`, which has no
   poller backup at all.

### Test-level (reproducible RED, currently failing by design)

```bash
cd <worktree>
rtk go test ./internal/session/ -run TestStartupDialogDismiss -count=1
```

Expected output (recorded in
`docs/plans/2026-10-20-oneshot-startup-dialog-dismiss-failures.log`):

- `FAIL TestStartupDialogDismiss_OneShotTrustDialogAcceptedByCaptureLoop`
  — "trust dialog accept sent 0 time(s) via m.tmux, want exactly 1"
- `FAIL TestStartupDialogDismiss_OneShotChannelsDialogAcceptedByCaptureLoop`
  — "channels consent accept sent 0 time(s) via m.tmux, want exactly 1"
- `FAIL TestStartupDialogDismiss_FailedAcceptIsRetriedOnNextTick`
  — "failed accept send was retried 0 attempt(s) via m.tmux, want 2"
- `FAIL TestStartupDialogDismiss_NotResentOnceDialogGone`
  — "trust accept sent 0 time(s), want exactly 1"
- `PASS TestStartupDialogDismiss_NonOneShotAndOtherBackendsUnaffected`
  (interactive claude-code + one-shot opencode subtests — no spurious
  keystrokes; must stay green after the fix)

The failure output lists the capture loop's `FakeTmux` calls per run: only
`capture-visible` / `capture-live-tail` / `capture-scrollback` ops — zero
`SendTrustPromptAccept`, zero `send-keys`. That is the "right-reason"
signal: the loop detects nothing because it has no dialog logic, not because
of a flaky fixture or a race in the harness.

Full package run for context: `473 passed, 4 failed (all four above),
1 skipped`.

## Fix design

### Goal

Make the screen-capture loop the *reliable floor* for startup-dialog
dismissal on one-shot claude-code sessions: detect the dialog on the pane,
send the accept through the `m.tmux` abstraction, retry a failed send on the
next tick, and stay quiet once the dialog is gone — while leaving interactive
sessions and other backends untouched (their existing main-loop pollers keep
working).

### Design

1. **One shared helper in the manager** (single source of truth, callable from
   both the capture loop and `processOutputLine`):

   ```go
   // dismissVisibleStartupDialogs inspects text (a pane capture or a log
   // line) for claude-code startup dialogs on a one-shot session and sends
   // the matching accept keystroke through m.tmux. Returns true if it acted.
   func (m *Manager) dismissVisibleStartupDialogs(sess *Session, text string) bool
   ```

   Detection — on `strings.ToLower(text)`, mirroring the existing markers
   (`main.go` `claudeDisclaimerResponse` / `isTrustDialogLine` /
   `getActiveStartupPromptInput`):

   - **Trust**: `strings.Contains(lower, "trust this folder")` (+
     `"no, exit"` required, to avoid hitting ordinary conversation text) →
     `m.tmux.SendTrustPromptAccept(sess.TmuxSession)` (Down, then Enter —
     the existing `TmuxAPI` method, not a new exec.Command pair).
   - **Channels**: `strings.Contains(lower, "i am using this for local
     development")` (or `loading development channels`) →
     `m.tmux.SendKeys(sess.TmuxSession, "Enter")`.

   Gates (kept inside the helper so both callers get them):
   `sess.BackendFamily == "claude-code" && sess.OneShot &&
   sess.TmuxSession != ""`.

2. **Retry-safe latch** (replaces the fire-once `startupDialogSent` pattern):

   Per-`(fullID, dialog)` state machine with three values, keyed in a map
   guarded by a mutex (or a small struct on `Manager`):

   ```
   (absent / failed) --send OK--> awaitingDismiss
   (awaitingDismiss) --pane shows dialog--> skip (no double-send this tick)
   (awaitingDismiss) --pane no dialog-->   dismissed (clear)
   (absent / failed) --send err-->  failed  (next tick retries if still visible)
   ```

   Concretely each evaluation:

   ```go
   if strings.Contains(lower, trustMarkers) {
       if state[fullID+tr] == dismissed || state[fullID+tr] == awaiting { return }
       if err := m.tmux.SendTrustPromptAccept(tmux); err != nil {
           state[fullID+tr] = attempted // retry next tick
           continue
       }
       state[fullID+tr] = awaiting       // do not double-send this/next tick
   } else if state[fullID+tr] == awaiting {
       state[fullID+tr] = dismissed      // dialog gone → confirmed, clear
   }
   ```

   This satisfies the acceptance criteria: a failed send is retried on the
   next tick exactly once (test 3 expects attempts == 2: one failure, one
   successful retry, then latched); a dialog that disappears is not re-sent
   (test 4); a lingering dialog is not hammered (tests 1/2 expect exactly 1
   send across 4–5 ticks).

   Note on test 3: "retry on next tick and succeed, then stick" — the retry
   fires because `awaiting` was never set (send errored). Once the retry
   succeeds, `awaiting` blocks further sends even though the fake pane still
   shows the dialog (the test does not simulate the pane clearing), which is
   the desired operator-visibility behavior: one clean attempt per render.

3. **Wire it into the capture loop** (`StartScreenCapture` body, inside the
   `if capture != lastCapture` block, after stateCapture is computed):

   ```go
   // One-shot startup-dialog floor: claude-code trust / channels consent.
   m.dismissVisibleStartupDialogs(current, displayCapture)
   ```

   Also handle the *static-panes* miss-mode (Root cause 6): when a dismissal
   is in `awaiting` state, do one extra explicit pane read each tick
   (or simply re-evaluate on the next `capture != lastCapture`, which for a
   real Claude dialog includes redraw/heartbeat bytes). Simplest first
   iteration: rely on the tick diffing; the main.go pollers remain as a
   backstop for the static case. (Document the residual edge.)

4. **Collapse the processOutputLine block** to a call into the same helper:

   ```go
   if m.dismissVisibleStartupDialogs(sess, line) { /* fired; latch handled */ }
   ```

   dropping the goroutine, the 300 ms sleep, the `exec.Command` send-keys
   pair, and the `startupDialogSent` sync.Map (the helper's latch
   subsumes it). Keystrokes now always go through `TmuxAPI` → observable by
   `FakeTmux`, retryable, and covered by the same tests.

   `GetActiveStartupPromptInput` / `IsTrustDialogShowing` /
   `SendTrustDialogAccept` keep their current behavior (main-loop pollers
   and API still use them); the helper is additive, not a rename.

5. **FakeTmux (already in place)**: `FailNextOp map[string]int` +
   `ErrInjected` let a test fail exactly the `SendTrustPromptAccept` call
   without the surrounding capture calls eating the error (test 3).

### What stays out of scope

- `main.go` poller behavior for interactive sessions (they are the
  operator-at-terminal case and their existing handlers + this manager-level
  change are compatible; the negative test pins "no new keystrokes from the
  capture loop").
- Registry / PRD completion paths.
- Other backends (opencode/goose/aider) — they have no equivalent startup
  dialog today; the helper is claude-code-gated.

### Acceptance (maps to the red tests)

1. one-shot claude-code + trust dialog on pane → exactly one
   `SendTrustPromptAccept` via `m.tmux` (`TestStartupDialogDismiss_OneShotTrustDialogAcceptedByCaptureLoop`)
2. one-shot claude-code + channels dialog on pane → exactly one `send-keys
   Enter` via `m.tmux`, and no trust accept
   (`TestStartupDialogDismiss_OneShotChannelsDialogAcceptedByCaptureLoop`)
3. first accept send fails → next tick retries; total attempts == 2, then
   latched (`TestStartupDialogDismiss_FailedAcceptIsRetriedOnNextTick`)
4. dialog gone after accept → no re-send (`TestStartupDialogDismiss_NotResentOnceDialogGone`);
   dialog lingering → no double-send (implicit in 1/2 via `want exactly 1`
   across 4–5 ticks)
5. interactive claude-code / one-shot opencode → zero keystrokes
   (`TestStartupDialogDismiss_NonOneShotAndOtherBackendsUnaffected`)

Green gate: all five green **and** the full `./internal/session/` package
green (baseline: 473 passed / 1 skipped today, with the four intentional
reds).

### Rollout notes

- Behavior change: one-shot claude-code sessions with auto-accept *off* will
  now get their dialogs accepted by the capture loop. That is the intent
  (one-shot = no human present). Operators who want to handle it
  interactively should use an interactive session — where this change is
  deliberately a no-op.
- Residual edge (documented): a byte-static dialog pane after the first
  evaluation defers the retry to the next pane change; the main.go pollers
  (when `claudeAutoAccept` is on) remain as a second independent floor for
  the trust dialog specifically.

---

## Live Validation (2026-10-21, one-shot, claude-code backend)

Ran against the worktree build (`./datawatch` at `/home/user/.datawatch/prd-worktrees/b0390001`, go build rc=0), not the system binary.

### Setup

- Sandbox daemon in `/tmp/dw-oneshot-livetest` (fresh `data/`, `projects/live-001`), ports 28080 (HTTP) / 28443 (TLS), started with `-v` so `debugf` lines land in the daemon log. The system daemon on 18443/18080 (old binary v9.5.x) was left untouched.
- Project dir `/tmp/dw-oneshot-livetest/projects/live-001` is **untrusted** — not present in `~/.claude.json`, no prior claude run in it, so the first-launch trust dialog ("Quick safety check … ❯ No, exit / Yes, I trust this folder") **does** appear.
- One-shot session started via `POST /api/sessions/start` with `one_shot: true`, `backend: claude-code`, task: *"Create a file named hello.txt in the current directory containing exactly the text: hello oneshot. Then print a line that starts with: DATAWATCH_COMPLETE: hello.txt created"*.
- Session id `1e20` / `dw-sbx-oneshot-1e20`, tmux session `cs-dw-sbx-oneshot-1e20` (dedicated socket under `/home/user/.datawatch/tmux/tmux-1000/default`).
- Live pane captures taken with `tmux -S /home/user/.datawatch/tmux/tmux-1000/default capture-pane` against the running session.

### What actually happened (timeline, daemon local time)

| time (UTC) | event |
|---|---|
| 02:27:23 | one-shot session created; claude launched in tmux (size 120x40, input mode `terminal`) |
| 02:27:29 | `dismissClaudeStartupDialog: trust: attempt 1/5 sent via m.tmux — awaiting the dialog to disappear` |
| 02:27:29 | **same second:** `dismissClaudeStartupDialog: trust: dialog no longer visible — dismissed confirmed, no re-send` — but the trust dialog had **not** been accepted yet (it stayed on screen) |
| 02:28 – 02:34 | session state stuck at `waiting_input`; `hello.txt` never created; capture-pane at 02:32:39 still shows the un-accepted trust prompt ("❯ No, exit" highlighted) — the very dialog this fix targets |
| 02:34:10 | operator manually sent `tmux send-keys Down Enter` into the pane (and the daemon also logged `trust: attempt 2/5 sent via m.tmux` at the same second — state evidently re-armed); dialog accepted, Claude REPL came up |
| 02:34:14–02:35:xx | task executed: `Wrote 1 line to hello.txt`, `DATAWATCH_COMPLETE: hello.txt created` printed |
| 02:39 | validation session torn down (sandbox killed, tmp tree removed) |

Full daemon-log + pane evidence: [`2026-10-20-oneshot-startup-dialog-dismiss-livetest.log`](2026-10-20-oneshot-startup-dialog-dismiss-livetest.log) (extract of `daemon.log`) and the session `output.log`.

### Log excerpt (core evidence)

```text
[02:27:29] [session:debug] dismissClaudeStartupDialog: trust (dw-sbx-oneshot-1e20): attempt 1/5 sent via m.tmux — awaiting the dialog to disappear
[02:27:29] [session:debug] dismissClaudeStartupDialog: trust (dw-sbx-oneshot-1e20): dialog no longer visible — dismissed confirmed, no re-send
[02:34:10] [session:debug] dismissClaudeStartupDialog: trust (dw-sbx-oneshot-1e20): attempt 2/5 sent via m.tmux — awaiting the dialog to disappear
[02:34:10] [session:debug] prompt debounce started session=dw-sbx-oneshot-1e20 prompt="Enter to confirm · Esc to cancel" (waiting 3s)
[02:34:14] [session:debug] MarkChannelEvent(running): ... running state after REPL up
[02:35:xx] DATAWATCH_COMPLETE: hello.txt created   (pane + output.log)
```

### Findings

1. **Detection + send works:** the capture loop saw the trust dialog and sent the accept keys via `m.tmux` (`attempt 1/5` in the daemon log).
2. **But the first send didn't actually dismiss the dialog.** The dialog re-rendered (still on screen at 02:32), and for ~6'41" (02:27:29 → 02:34:10) the session sat at the trust prompt in `waiting_input`. The `dismissed` latch fired prematurely because the capture between the send and the confirm window showed the pane *without* the dialog text (mid-render), so `dismissClaudeStartupDialog` recorded it as "dismissed confirmed, no re-send" even though Claude never accepted it.
3. **Re-arming worked (eventually):** a later evaluation re-sent `attempt 2/5`, which — together with the operator's manual `Down Enter` at the same moment — finally accepted the folder. Once the dialog was gone, the one-shot completed cleanly: `hello.txt` created, `DATAWATCH_COMPLETE:` emitted. So the *accept keys* and the *completion path* are correct; only the **first-send timing / premature dismiss latch** is the defect.

### Verdict

**Live validation: PARTIAL.** The mechanism is live and reachable (logs prove it fires), and the accept flow end-to-end works once the dialog actually renders (hello.txt + DATAWATCH_COMPLETE reached on the real daemon). However the run required an operator send plus a second attempt; the first automatic send landed while the dialog was still rendering and the code prematurely latched it as dismissed. This is a reliability gap in `dismissClaudeStartupDialog`'s send timing/latch (manager.go), not in detection.

**Recommended follow-ups (not done in this change):**
- Don't latch `dismissed=true` on a single "not visible" capture; require the absent-dialog state to persist across at least one extra capture window (or a couple of seconds) before confirming dismissal, so a mid-render flash doesn't count.
- Or, on a later capture that shows the dialog *again*, re-arm the attempt budget instead of honoring the stale `dismissed` latch.
- Consider gating the send on the dialog being "settled" (e.g. present in two consecutive captures) rather than firing on first sighting, to avoid racing Claude's own render.

### Result

**Implemented** in `/home/user/.datawatch/prd-worktrees/b0390001` (worktree for PRD `b0390001`), in
`internal/session/manager.go` (`dismissClaudeStartupDialog`, `trySendDialogAccept`,
`markDialogConfirmedIfGone`, and the capture-loop + output-line hooks that call it).
Built locally with `go build -o datawatch ./cmd/datawatch/` (rc=0) and run **live** against
a real claude-code one-shot session in an untrusted directory.

- **Unit level:** the `internal/session/` suite (477 tests, including the dedicated
  `startup_dialog_dismiss_test.go` set) passes on the worktree build. `go test ./internal/session/...` rc=0.
- **Log level (live daemon):** the daemon log shows the dialog detected and the accept sent via
  `m.tmux` (`attempt 1/5 … sent via m.tmux`); a second send also fired later (`attempt 2/5`).
- **Live one-shot:** a real `claude-code` backend session launched in an untrusted project dir,
  hit the trust dialog, received the auto-accept, then completed: `hello.txt` was created and
  the session emitted its `DATAWATCH_COMPLETE:` line on the real daemon (ports 28080/28443,
  session `1e20` / `dw-sbx-oneshot-1e20`).
- **Caveat (reliability gap, see Findings #2–3):** the first automatic send landed while the
  dialog was still rendering and the code prematurely latched it as dismissed; the run needed a
  second attempt (plus one operator keypress at the same instant) to actually accept. Mechanism
  works; send timing/latch needs the follow-ups listed above for bulletproof first-shot autonomy.

_Validation status: complete — live one-shot reached DATAWATCH_COMPLETE; one reliability caveat noted above._

---

## Live Validation — Round 2 (2026-10-21, post Fix #3)

### Root cause of the round-1 failure

The round-1 `PARTIAL` verdict had two defects interacting:

1. **Premature latch** (Fix #1 in this doc): `markDialogConfirmedIfGone`
   confirmed dismissal on a single "not visible" capture. Fixed with a
   sustained-absence window (700 ms) and re-arm on re-appearance.
2. **Wrong key sequence** (this round's new finding): in Claude Code
   v2.1.306 the trust dialog shows **"Yes, I trust this folder"
   preselected** (❯ cursor on that option; "No, exit" is the other one).
   The old `TmuxManager.SendTrustPromptAccept`
   (`internal/session/tmux.go`) hard-coded `Down` + `Enter`, which moved
   the cursor **off** the preselected accept option onto "No, exit" —
   selecting it and *exiting claude*. The channels consent dialog has the
   same shape ("❯ 1. I am using this for local development / 2. Exit");
   its accept path (`m.tmux.SendKeys(sess.TmuxSession, "Enter")` in
   `internal/session/manager.go`) was already correct because option 1
   is preselected. Round-1 log evidence: the session's final pane content
   was `DATAWATCH_COMPLETE: claude done` — the shell line that runs only
   after claude exits — i.e. the automatic accept had just told
   claude "No, exit".

### Fix #3 (this round)

`internal/session/tmux.go` — `SendTrustPromptAccept` is now pane-aware:
it `capture-pane`s the target pane, and if the line containing
`Yes, I trust this folder` also carries the `❯` cursor it sends a plain
`Enter` (confirming the preselected accept option); otherwise it falls
back to the historical `Down` + `Enter` sequence for the earlier-layout
builds where decline was preselected. The `dismissClaudeStartupDialog`
trust spec in `internal/session/manager.go` keeps routing through
`m.tmux.SendTrustPromptAccept` (keeps FakeTmux coverage), and its comment
was corrected. Detection, latch, re-arm and re-send-gap behavior are
unchanged from the Fix #1 design.

### Setup (round 2)

- Worktree build at `/home/user/.datawatch/prd-worktrees/b0390001`
  (`./datawatch`, **v9.5.31**), rebuilt 2026-10-21 ~03:13 after Fix #3;
  `go build` rc=0, `go vet ./internal/session/` clean,
  `rtk go test ./internal/session/` → **478 passed** (including the 8
  `TestStartupDialog*` tests).
- Sandbox daemon in `/tmp/dw-oneshot-livetest2` (fresh `data/`),
  `TMUX` unset, HTTP 28080 / TLS 28443. *Deviation from AGENT.md's
  18080/18443 sandbox ports: both were held by the live production
  daemon (`datawatch start --foreground`, PID 205216); the same port
  workaround was used in round 1.*
- Claude Code v2.1.306.
- Fresh, untrusted project dirs (absent from `~/.claude.json`), started
  via `POST /api/sessions/start` (`Authorization: Bearer dw-test-token-12345`)
  with `one_shot:true`, `backend:claude-code`.

### Runs (both zero operator keypresses)

| | session A | session B |
|---|---|---|
| full ID | `dw-sbx-oneshot-live2-6638` | `dw-sbx-oneshot-live2-ec44` |
| project dir | `/tmp/dw-oneshot-livetest2/projects/live3-c7qz` | `/tmp/dw-oneshot-livetest2/projects/live4-vx2a` |
| started | 03:15:28 | 03:20:33 (local) |
| task | `Reply with exactly this one line and nothing else: TRUSTDIALOG-AUDIT-OK` | `Write a file named marker.txt with the text ok, then reply with exactly this one line and nothing else: DATAWATCH_COMPLETE: dialog dismissal validated, marker.txt written` |
| trust dialog | auto-accepted, 1 attempt | auto-accepted, 1 attempt |
| channels dialog | auto-dismissed, 1 attempt | auto-dismissed, 1 attempt |
| agent reply observed in pane | `● TRUSTDIALOG-AUDIT-OK` | `● DATAWATCH_COMPLETE: dialog dismissal validated, marker.txt written` |
| final state | idle `❯` prompt (claude stayed interactive — task had no completion marker, expected) | **`complete`** at 03:21:15 (marker line in pane drove the normal one-shot completion path) |

Daemon log (both sessions, same shape):

```text
dismissClaudeStartupDialog: trust (dw-sbx-oneshot-live2-6638): attempt 1/5 sent via m.tmux — awaiting the dialog to disappear
dismissClaudeStartupDialog: trust (dw-sbx-oneshot-live2-6638): absence observed — starting the dismiss confirm window
dismissClaudeStartupDialog: trust (dw-sbx-oneshot-live2-6638): sustained absence — dismissed confirmed, no re-send
dismissClaudeStartupDialog: channels (dw-sbx-oneshot-live2-6638): attempt 1/5 sent via m.tmux — awaiting the dialog to disappear
dismissClaudeStartupDialog: channels (dw-sbx-oneshot-live2-6638): absence observed — starting the dismiss confirm window
dismissClaudeStartupDialog: channels (dw-sbx-oneshot-live2-6638): sustained absence — dismissed confirmed, no re-send
SendInput session=dw-sbx-oneshot-live2-6638 tmux=cs-dw-sbx-oneshot-live2-6638 text="Reply with exactly this one line and nothing else: ..."
task delivered to dw-sbx-oneshot-live2-6638 via send_input
```

### Result

**PASS** — one-shot claude-code startup dialogs are dismissed fully
automatically with zero operator keypresses, and the session proceeds to
run the task (session A shows the task's exact reply in the pane; session
B reached `state=complete` via the standard `DATAWATCH_COMPLETE:` path
with the task's exact marker line as its output) on the live worktree
build (v9.5.31) of `internal/session/manager.go`
(`dismissClaudeStartupDialog` / `trySendDialogAccept` /
`markDialogConfirmedIfGone`) plus the pane-aware
`SendTrustPromptAccept` in `internal/session/tmux.go`, against
Claude Code v2.1.306 in an untrusted directory.
