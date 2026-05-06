# B30 — Scheduled command needs a 2nd Enter to activate

**Status:** open, research complete, fix not started
**Opened:** 2026-04-19
**Research requested by:** operator (user report)

---

## Symptom

When a scheduled command fires (time-based or on-`waiting_input`
trigger), the command's text lands in the target session's TUI
prompt **but does not execute**. An operator-typed Enter from the
web UI / CLI / comm channel after the scheduled fire is required to
actually submit it.

Observed with claude-code sessions (TUI-based). Likely reproducible
with any backend that uses a React-for-terminals rendering library
(ink-based TUIs) or bracketed-paste input handling.

---

## Is this a regression of a prior fix?

**No — new bug.** Research below:

### Prior schedule/tmux fixes in the codebase

| Ref | Symptom | Root cause | Fix |
|---|---|---|---|
| `CHANGELOG.md` L1475 — "Scheduler NeedsInputHandler bug fix" | `runScheduler` silently overwrote the combined NeedsInputHandler set in `runStart`; on-input schedules never fired | Goroutine registered a single handler, later code re-registered | Extracted `fireInputSchedules` as a standalone helper; the combined handler in `runStart` calls it. |
| `CHANGELOG.md` L314 — "Schedule bar not refreshing" | UI stale, not input-related | WebSocket broadcast didn't re-emit on state change | Added schedule refresh on state-change |
| `CHANGELOG.md` L662 — "Orphan scheduled commands on session delete" | Scheduled entries leaked past session termination | `Manager.Kill`/`Delete` didn't cancel pending schedules | Added `ScheduleStore.CancelBySession` + call-sites |

None of these match the current symptom. Grep of all source,
CHANGELOG, and `docs/plans/*.md` for patterns like "double enter",
"2nd enter", "needs enter", "bracketed paste", "tmux.*Enter.*Enter"
returns only unrelated matches (empty-string-as-Enter-only in
the saved-command router, and the existing `SendKeys` that already
appends one Enter per call).

### Conclusion

This is a **new bug**. File it fresh as **B30**.

---

## Code flow reconstruction

1. **`runScheduler`** (goroutine in `cmd/datawatch/main.go:5169`)
   ticks every 10s and walks `ScheduleStore.DuePending(t)`. For
   each due entry it calls:
   ```go
   mgr.SendInput(sess.FullID, sc.Command, "schedule")
   ```
2. **`fireInputSchedules`** (`cmd/datawatch/main.go:5144`) is the
   sibling path for on-`waiting_input` schedules. Identical to the
   above: `mgr.SendInput(sess.FullID, sc.Command, "schedule")`.
3. **`Manager.SendInput`** (`internal/session/manager.go:1056`)
   for the tmux-backed backends (every backend except opencode-acp
   / openwebui / ollama-chat — claude-code falls in this bucket)
   ends with:
   ```go
   m.tmux.SendKeys(sess.TmuxSession, input)
   ```
4. **`TmuxManager.SendKeys`** (`internal/session/tmux.go:50`):
   ```go
   exec.Command("tmux", "send-keys", "-t", session, keys, "Enter").Run()
   ```
   — one `tmux send-keys` call that emits `<keys>` followed by a
   literal `Enter`.

The web-UI, comm-channel, CLI, and MCP send paths all terminate at
the same `SendKeys` call, so the tmux command the scheduler issues
is **byte-for-byte identical** to what a user's interactive send
produces.

---

## Why web-UI works but scheduler doesn't (hypothesis)

The tmux command is identical, so the difference is **timing**, not
content. Three candidate root causes, in descending likelihood:

### H1. Render-settle race (most likely)

`fireInputSchedules` is invoked from the `NeedsInputHandler`
callback chain, which fires the instant the parent sees the
`waiting_input` state transition. That transition is detected from
output-line pattern-matching — i.e., the moment claude-code *starts*
printing its prompt widget.

An ink / React-for-terminals TUI renders the prompt in phases:

1. Print the prompt text
2. Switch to raw-input mode + set cursor style
3. Enable bracketed-paste
4. Begin accepting stdin characters

Between phase 1 (state transition fires) and phase 4 (input usable)
is ~50–200 ms. If the scheduler beats phase 4, tmux queues the
bytes and the TUI drops the first Enter as "part of setup" — the
text sits in the buffer but the submit doesn't fire until a
subsequent Enter re-triggers the buffer flush.

A human operator who opens the web UI naturally waits multiple
seconds before typing, giving phase 4 room.

### H2. Bracketed-paste swallow

Claude Code's prompt likely enables bracketed-paste (`ESC [ ? 2004 h`).
When multiple bytes arrive fast, the terminal wraps them in
`\e[200~ ... \e[201~` and the TUI receives them as a single paste
event. If the TUI treats pasted content as "text only — no Enter
submit", the trailing Enter becomes part of the pasted payload
instead of a submit.

`tmux send-keys <keys> Enter` sends the keys and the Enter back-to-
back without any `-l` (literal) flag, so bracketed-paste wrapping
is up to the terminal, not us. A `-l` for keys followed by a
separate tmux call for Enter would defeat bracketed-paste on a
per-segment basis — see "possible fixes" below.

### H3. `tmux` buffer flush under load

Less likely. If the tmux server is under load when both `<keys>`
and `Enter` are in the same call, the `Enter` key-name lookup
happens synchronously and should not race. Ruled out for now but
noted for completeness.

---

## Repro plan (for fix validation — not execution yet)

1. Start a claude-code session (`datawatch session new …`).
2. Schedule a command that fires in 30s:
   ```
   datawatch session schedule add <id> "just say hello"
   ```
3. Wait 30s + observe:
   - Does the text land in the prompt but not submit? → B30 reproduced
   - Does the submit fire normally? → operator's observation was
     transient (investigate logs instead)
4. If reproduced, run the same text through the web UI's input bar
   — expect: submit fires. Confirms the problem is scheduler-side,
   not SendKeys-side.

---

## Possible fixes (research only — do NOT implement yet)

### Fix A — two-step send with a settle delay (preferred)

```go
// internal/session/tmux.go
func (t *TmuxManager) SendKeysWithSettle(session, keys string, settle time.Duration) error {
    // Phase 1: push the text literally (no Enter).
    if err := exec.Command("tmux", "send-keys", "-t", session, "-l", keys).Run(); err != nil {
        return err
    }
    // Phase 2: wait for the TUI to exit bracketed-paste / settle.
    time.Sleep(settle)
    // Phase 3: submit.
    return exec.Command("tmux", "send-keys", "-t", session, "Enter").Run()
}
```

Wire `fireInputSchedules` and `runScheduler` to call the settle
variant with a configurable delay (`session.schedule_settle_ms`,
default 200 ms, 0 = legacy one-shot). Per AGENT.md Configuration
Accessibility Rule, expose via YAML + REST + MCP + comm + CLI.

**Pros:** surgical; other SendInput call-sites unchanged; the
operator-observed fix ("I hit Enter manually and it went through")
matches the hypothesis.

**Cons:** a mandatory 200 ms delay on every scheduled command.

### Fix B — wait for output idle before firing

Watch the output-line stream for N ms of silence before
`fireInputSchedules` calls SendInput. Detects that the TUI has
finished rendering the prompt. More complex + has its own edge
cases (TUIs that keep printing throughout input).

### Fix C — always use two-step (drop legacy one-shot)

Apply the settle delay universally. Catches the same bug when it
surfaces through comm channels / MCP. Behaviour change for the
web UI path (where interactive typing already worked, the delay
adds 200 ms of latency).

---

## Acceptance criteria (for fix PR)

- [ ] Test: mocked tmux fake records argv — asserts the fix emits
      two `send-keys` calls (or settle-variant) for scheduled fires.
- [ ] Test: unchanged call for the legacy one-shot path (web UI,
      comm, MCP, CLI) when Fix A is picked.
- [ ] Test: settle-delay config field round-trips (YAML, REST,
      MCP `config_set`, comm `configure`).
- [ ] Manual: scheduled command against a real claude-code session
      fires the command AND submits it on a single fire event.
- [ ] AGENT.md release-testing rule applied — verified against the
      operator's `testing` K8s cluster before shipping.
- [ ] Documented in CHANGELOG under Fixed section with the B30 ID.

---

## References

- `internal/session/tmux.go` — `SendKeys` (current one-shot)
- `internal/session/manager.go` — `SendInput` (shared send path)
- `cmd/datawatch/main.go:5142` — `fireInputSchedules`
- `cmd/datawatch/main.go:5169` — `runScheduler`
- `CHANGELOG.md:1475` — prior schedule fix (unrelated symptom)

---

**Do not start implementation until this plan is reviewed.**
