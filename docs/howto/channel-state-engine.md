# How-to: Channel-driven session state engine

Why a session is currently `Running` / `WaitingInput` / `Complete`,
and what to look at when the state disagrees with reality. This is
the lowest-level diagnostic doc for sessions — start here when state
is wrong, then drop into [`sessions-deep-dive.md`](sessions-deep-dive.md)
for the rest.

## What state means

| State | Meaning | Operator action |
|---|---|---|
| `Running` | LLM is actively processing. Output is flowing or imminent. | Wait. |
| `WaitingInput` | LLM is paused, awaiting your next message. Reversible — type and it goes back to Running. | Type. |
| `RateLimited` | Backend hit its limit. Daemon will auto-resume after the cool-down. | Wait, or rotate to a different backend. |
| `Complete` | LLM finished its task. **Terminal** — won't resume on its own. | Read the output; start a new session. |
| `Failed` | tmux died unexpectedly. Terminal. | Investigate; start a new session. |
| `Killed` | You (or another operator) explicitly killed it. Terminal. | None. |

## Three signals, one priority chain

### 1. Structured backend events (authoritative)

opencode-acp emits typed SSE events:

| Event | → State |
|---|---|
| `session.status: busy`, `step-start`, `message.part.delta`, `message.part.updated` | Running |
| `session.status: idle`, `session.idle` | WaitingInput |
| `session.completed`, `message.completed` | Complete |

These drive state directly. **Authoritative — overrides everything else.**
claude-code MCP doesn't emit structural events, so its state derives
from signals 2 + 3.

### 2. Channel reply / chat-message text (advisory)

Natural-language patterns in incoming channel messages can
conservatively promote:

- **Input-needed patterns** (e.g. *"should i proceed?"*, trailing `?`
  on short message) → can promote to WaitingInput.
- **Completion patterns** (e.g. *"task complete"*, *"done!"*) → **NOT**
  promoted to Complete. Removed because multi-sentence wraps like
  *"Done. Let me know if anything else."* false-fired into the sticky
  Complete state.

The advisory layer never overrides a structural signal.

### 3. Universal 15 s gap watcher (fallback)

A 1-second-tick goroutine scans every Running session. If
`LastChannelEventAt` is older than 15 seconds, the session flips to
WaitingInput. **30 s warm-up grace** after daemon start so resumed
sessions get a clean baseline.

## What bumps `LastChannelEventAt`?

- Structured ACP event arrives.
- Channel reply arrives (MCP / ACP / chat_message).
- tmux pane content changes (StartScreenCapture goroutine — runs
  while a WS client is subscribed).
- Session log file gets a new line (every backend; works without WS).
- Operator types into the input bar.
- ResumeMonitors confirms tmux is alive after a daemon restart
  (positive evidence — LCE reset to "now").

## Base requirements

- `datawatch start` — daemon up.
- A session in any state.

## Setup

The state engine runs by default. Tunable via:

```yaml
# ~/.datawatch/datawatch.yaml
session:
  channel_state_watcher:
    enabled: true
    tick_interval: 1s
    gap: 15s
    warmup: 30s
```

**Don't shorten `gap` below 8 s** — claude tool calls can pause the
log file for 5+ seconds; a too-tight gap will flip mid-tool-call.
**Don't lengthen above 60 s** — the WaitingInput hint becomes
unreliable.

## Two happy paths

### 4a. Happy path — CLI (debug-walk)

A session is in WaitingInput, you're typing, it goes Running, then
flips back to WaitingInput within seconds. Walk:

```sh
SID=ralfthewise-abcd

# Pane 1: confirm output IS arriving on the log file.
tail -F ~/.datawatch/sessions/$SID/output.log
# Type into the session in another tab; new lines should appear here.

# Pane 2: watch state + LCE in real time.
while true; do
  curl -sk -H "Authorization: Bearer $(cat ~/.datawatch/token)" \
    https://localhost:8443/api/sessions \
    | jq -r --arg s "$SID" '.sessions[] | select(.full_id==$s) | "\(.state) lce=\(.last_channel_event_at) upd=\(.updated_at)"'
  sleep 1
done

# Pane 3: state-engine activity in the daemon log.
tail -F ~/.datawatch/daemon.log \
  | grep -E 'MarkChannel|ChannelStateWatcher|monitorOutput'
```

Read the panes together:

- Output arrives (pane 1) but no `MarkChannelEvent` lines (pane 3) →
  the output-arrival path isn't bumping LCE. Check
  `processOutputLine` in `internal/session/manager.go` — for
  structured-channel backends the bump must happen at the TOP of the
  branch (not after the early return).
- `MarkChannelEvent` lines fire (pane 3) but state still oscillates
  (pane 2) → a legacy reverter is undoing the watcher's transition.
  Look for "flip back to Running" branches in `manager.go` periodic
  monitors — they should be guarded by an `lceFresh` check.

### 4b. Happy path — PWA

1. PWA → Sessions list.
2. State badge on each card. Click into a session.
3. Header carries the same badge + the **stale-comms amber dot**
   when LCE > 2 s. Color cues:
   - Green dot = LCE < 2 s
   - Amber pulsing dot = LCE 2-15 s (early warning)
   - Badge flips to `waiting_input` = LCE ≥ 15 s
4. To debug a stuck session: open browser dev console,
   `console.log(state.sessions.find(s => s.full_id === '<id>'))` —
   inspect `state` + `last_channel_event_at` directly.

There's no operator-facing state-override in the PWA (by design — fix
the underlying signal, not the symptom).

## Other channels

### 5a. Mobile (Compose Multiplatform)

State badges + stale-comms dot parity with PWA. Mobile is observer-only
for state — no override; same rationale.

### 5b. REST

The state field appears on every `/api/sessions` response. The
`last_channel_event_at` timestamp is broadcast alongside, so external
tooling can compute its own staleness:

```sh
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/sessions \
  | jq '.sessions[] | {full_id, state, last_channel_event_at, updated_at}'
```

There is no `PATCH /api/sessions/<id> {state: ...}` — state is
read-only via the API. Fix signals to fix state.

### 5c. MCP

`session_get` returns the same fields. Useful for an autonomous LLM
to check the state of its sibling sessions before deciding what to do
next.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `state <id>` | Returns one-line state + LCE age. |
| `state list` | Compact one-line-per-session. |
| `health` | Includes per-session state distribution. |

### 5e. YAML

`~/.datawatch/sessions/<id>/state.json` is the on-disk truth — but
managed by the daemon. Don't edit by hand. The daemon serializes
writes and recovers on restart from this file.

To inspect:

```sh
cat ~/.datawatch/sessions/$SID/state.json | jq
#  → { "state": "running", "last_channel_event_at": "...",
#      "updated_at": "...", ... }
```

## Diagnostic table

| Symptom | Likely cause | Where to look |
|---|---|---|
| Stuck `WaitingInput`, output flowing | Output-arrival path not bumping LCE | `processOutputLine` for structured-channel backends; bump must be at top of branch |
| Stuck `Running`, session genuinely idle | LCE keeps getting bumped | `tail -f daemon.log \| grep MarkChannel`; could be a stuck pane status timer |
| Flips Running ↔ WaitingInput rapidly | Backend emitting ambiguous events | ACP backend: tail log + check SSE event stream for unmatched `session.status` swings |
| Says `Complete` while alive | DATAWATCH_COMPLETE marker fired | `grep DATAWATCH_COMPLETE output.log` |
| Says `Failed` after restart | tmux session was killed during downtime | `tmux ls`; check `dmesg` for OOM kills |
| Stuck `RateLimited` past cool-down | Backend reset hint wrong, or scheduler missed wake-up | `datawatch schedule list \| grep <id>`; manually `datawatch sessions resume <id>` |

## Diagram

```
        ACP structural events ──┐
                                 │
        Channel text (advisory) ─┤
                                 │   priority chain
        Pane content / log line ─┤   ▼
        Operator input ──────────┤
                                 │
        Daemon restart (LCE=now) ┘
                                 │
                                 ▼
                       LastChannelEventAt
                                 │
                  ┌──────────────┼──────────────┐
                  │              │              │
              < 2 s         2-15 s          ≥ 15 s
                  │              │              │
                  ▼              ▼              ▼
              Green dot      Amber dot     Watcher flip
              (live)         (stale; UI    (Running →
                              early-warn)   WaitingInput)
```

## Stale-comms indicator (PWA)

Pure visual; no state change. Useful for alerting integrations:

- Card view: dot appears next to badge whenever a Running session has
  been silent >2 s.
- Detail view: same dot on the header badge.
- Externally: any tooling reading `last_channel_event_at` can apply
  the same 2 s rule.

## Common pitfalls

- **Treating WaitingInput as "session is idle".** It means "no signal
  in 15 s". Could be a real 16 s tool call. Read the last few lines
  of output before deciding.
- **Adding new state-change paths without bumping LCE.** Any code
  that flips state between Running and WaitingInput must also bump
  LCE — otherwise the watcher will fight you within 15 s.
- **Watching `UpdatedAt` instead of `LastChannelEventAt`.** UpdatedAt
  is bumped by housekeeping; LCE is real activity. Always use LCE
  for staleness.

## Linked references

- Sessions deep-dive: [`sessions-deep-dive.md`](sessions-deep-dive.md)
- Architecture: `../architecture-overview.md`
- Daemon log location: `~/.datawatch/daemon.log`

## Screenshots needed (operator weekend pass)

- [ ] State badges across the 6 states
- [ ] Stale-comms amber dot on a Running badge
- [ ] Daemon log showing MarkChannelEvent + ChannelStateWatcher entries during a debug walk
- [ ] 3-pane debug walk (output / API state poll / daemon log) — terminal screenshot

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [howto/comm-channels](comm-channels.md)
- [architecture](../architecture.md)
- [api/channel](../api/channel.md)
