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
| `WaitingInput` | LLM is paused, awaiting your next message. Reversible — you type, it goes back to Running. | Type. |
| `RateLimited` | Backend hit its limit. Daemon will auto-resume after the cool-down. | Wait, or rotate to a different backend. |
| `Complete` | LLM finished its task. **Terminal** — won't resume on its own. | Read the output; start a new session if you want to continue. |
| `Failed` | tmux died unexpectedly. Terminal. | Investigate; start a new session. |
| `Killed` | You (or another operator) explicitly killed it. Terminal. | None. |

## Three signals, one priority chain

State is driven by a small priority chain:

### 1. Structured backend events (authoritative)

opencode-acp emits typed SSE events:

| Event | → State |
|---|---|
| `session.status: busy`, `step-start`, `message.part.delta`, `message.part.updated` | Running |
| `session.status: idle`, `session.idle` | WaitingInput |
| `session.completed`, `message.completed` | Complete |

When these arrive, they drive state directly. No NLP guessing for
this backend. **Authoritative — overrides everything else.**

claude-code MCP doesn't emit structural events (only reply text), so
its state derives from signals 2 + 3 below.

### 2. Channel reply / chat-message text (advisory)

Natural-language patterns in incoming channel messages can promote a
state change but only conservatively:

- **Input-needed patterns** (e.g. *"should i proceed?"*, trailing
  `?` on short message) → can promote to WaitingInput.
- **Completion patterns** (e.g. *"task complete"*, *"done!"*) → **NOT**
  promoted to Complete. Removed in v6.11.25 because multi-sentence
  wraps like *"Done. Let me know if anything else."* false-fired into
  the sticky Complete state.

The advisory layer never overrides a structural signal.

### 3. Universal 15 s gap watcher (fallback)

The daemon runs a 1-second-tick goroutine that scans every Running
session. If `LastChannelEventAt` is older than 15 seconds, the session
flips to WaitingInput.

The watcher has a **30 s warm-up grace period** after daemon start
to avoid flipping resumed sessions before they have a chance to
produce activity.

## What bumps `LastChannelEventAt`?

- A structured ACP event arrives.
- A channel reply arrives (MCP / ACP / chat_message).
- The tmux pane content changes (StartScreenCapture goroutine — runs
  while a WS client is subscribed to the session).
- The session log file gets a new line (every backend; works without
  any WS subscription — this is the most reliable signal).
- The operator types into the input bar.
- ResumeMonitors confirms tmux is alive after a daemon restart
  (positive evidence — LCE reset to "now" so the watcher gets a clean
  baseline).

## When state seems wrong — diagnostic table

| Symptom | Likely cause | Where to look |
|---|---|---|
| Stuck `WaitingInput`, output is flowing | Output-arrival path not reaching the LCE bump | `monitorOutput` → `processOutputLine` — for structured-channel backends ensure the bump fires at the TOP of the structured-channel branch (the early-return at the end of that branch will bypass the bump path otherwise) |
| Stuck `Running`, session is genuinely idle | LCE keeps getting bumped by something | `tail -f ~/.datawatch/daemon.log \| grep MarkChannel` to see what's bumping. Could be a stuck `tmux capture-pane` showing a status timer that updates every second |
| Flips Running ↔ WaitingInput rapidly | Backend emitting ambiguous events | ACP backend: `tail` the log + check the SSE event stream for unmatched `session.status` swings |
| Says `Complete` while session is alive | NLP false-positive (rare post-v6.11.25) OR DATAWATCH_COMPLETE marker fired | `grep DATAWATCH_COMPLETE ~/.datawatch/sessions/<id>/output.log` to confirm the marker was real |
| Says `Failed` after restart | tmux session was killed during daemon downtime | `tmux ls` to confirm the cs-`<id>` session is gone; check kernel `dmesg` for OOM kills |
| Says `RateLimited` but the cool-down already passed | Backend's reset hint was wrong, or scheduler missed the wake-up | `datawatch schedule list \| grep <id>` for pending resume; manually `datawatch sessions resume <id>` |

## Step-by-step debug walk

A session is in WaitingInput, you're typing, it goes Running, then
flips back to WaitingInput within seconds. What's happening?

```sh
SID=ralfthewise-abcd

# 1. Confirm output IS arriving on the log file:
tail -F ~/.datawatch/sessions/$SID/output.log
# (Type into the session in another tab; you should see new lines.)

# 2. Watch the API state + LCE in real time:
while true; do
  curl -sk https://localhost:8443/api/sessions \
    | jq -r --arg s "$SID" '.sessions[]|select(.full_id==$s)|"\(.state) lce=\(.last_channel_event_at)"'
  sleep 1
done

# 3. Watch the daemon log for state-engine activity:
tail -F ~/.datawatch/daemon.log | grep -E 'MarkChannel|ChannelStateWatcher|monitorOutput'
```

If you see output arriving (1), state oscillating (2), but no
MarkChannelEvent log lines (3): the output-arrival path isn't
bumping LCE. The fix is in `processOutputLine` — confirm the bump
fires at the top of the structured-channel branch, BEFORE the early
return.

If you see MarkChannelEvent lines but state still oscillates (2): a
legacy reverter is undoing the watcher's transition. Check for
"flip back to Running" branches in `manager.go` that run on a periodic
monitor — they should be guarded by an `lceFresh` check (LCE within
the watcher's gap window) so they don't fight the watcher.

## Stale-comms indicator (PWA)

The PWA renders an amber pulsing dot adjacent to the state badge after
**2 s** of LCE silence. Pure visual; no state change. Gives early
warning before the 15 s gap-watcher flip. Useful for alerting
integrations:

- Card view: dot appears next to the state badge whenever a Running
  session has been silent >2 s.
- Detail view: same dot on the header badge.
- Externally: any tooling reading `last_channel_event_at` from
  `/api/sessions` can apply the same 2 s rule.

## Tuning the gap (operator override)

In `~/.datawatch/datawatch.yaml`:

```yaml
session:
  channel_state_watcher:
    enabled: true                         # default true
    tick_interval: 1s                     # how often the watcher runs
    gap: 15s                              # silence → flip to WaitingInput
    warmup: 30s                           # post-restart grace
```

**Don't shorten the gap below 8 s** — claude tool calls can pause the
log file for 5+ seconds during a tool invocation; a too-tight gap
will flip mid-tool-call. **Don't lengthen above 60 s** — operator
won't get the WaitingInput hint reliably.

## CLI / API

The state field appears on every `/api/sessions` response. The
`last_channel_event_at` timestamp is broadcast alongside, so external
tooling can compute its own staleness.

There is no operator-facing override of session state — fix the
underlying signal source instead. (The daemon refuses
`PATCH /api/sessions/<id> {state: ...}` for exactly this reason.)

## Common pitfalls

- **Treating WaitingInput as "session is idle".** It means "no signal
  in 15 s". Could be the LLM's tool call genuinely taking 16 s; could
  be the LLM done; could be a backend bug. Read the last few lines of
  output before deciding.
- **Adding new state-change paths without bumping LCE.** Any code
  that flips state between Running and WaitingInput must also bump
  LCE — otherwise the watcher will fight you within 15 s. The pattern
  is `current.LastChannelEventAt = time.Now()` AFTER every state
  assignment.
- **Watching `UpdatedAt` instead of `LastChannelEventAt`.** UpdatedAt
  is bumped by daemon-internal housekeeping (rate-limit timer,
  reconcile, etc.), not just real activity. Always use LCE for
  staleness.

## Linked references

- Sessions deep-dive: [`sessions-deep-dive.md`](sessions-deep-dive.md)
- Architecture: `architecture-overview.md`
- Plan: BL266 channel-driven state engine arc
- Daemon log location: `~/.datawatch/daemon.log`

## All channels reference

State is observable everywhere; transitions are NOT operator-overridable
(by design — fix the underlying signal, not the symptom).

| Channel | How |
|---|---|
| **PWA** | State badge on every session card + the stale-comms amber dot. |
| **Mobile** | Same. |
| **REST** | `state` + `last_channel_event_at` fields on every `/api/sessions` response. |
| **MCP** | `session_get` returns the same fields. |
| **CLI** | `datawatch sessions get <id>` (jq into `.state`, `.last_channel_event_at`). |
| **Comm** | `state <id>` returns the current state. |
| **YAML** | `~/.datawatch/sessions/<id>/state.json` is the on-disk truth. |

## Screenshots needed (operator weekend pass)

- [ ] State badges across the 6 states (running / waiting_input / rate_limited / complete / failed / killed)
- [ ] Stale-comms amber dot on a Running badge
- [ ] Daemon log showing MarkChannelEvent + ChannelStateWatcher entries during a debug walk
