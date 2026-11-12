# BL467 — MCP channel bridge registration is first-come, not owner-bound

**Status:** needs_review · **Date:** 2026-10-16 · **Tracker:** BL467
**Scope:** design only. No code changes.

## The problem

Investigating a real `datawatch` MCP server `CONNECTION_CLOSED` error tonight
(root cause: a stale, fixed-port project MCP entry left over from a sandbox
daemon run — already self-healed by `channel.RemoveProjectMCPEntry`; a
separately-stale installed `datawatch-channel` binary, predating a TLS fix,
was also found and reinstalled) surfaced a second, separate, real gap that
was out of scope to fix in that pass.

The daemon's own journal shows, live, on this exact host tonight:

```
[channel] ignoring ready for local-node-ab22 on port 39381 — existing port 36777 is still alive (likely a stray duplicate bridge)
```

This log line is itself the *intended* dedup guard (`internal/server/api.go`'s
`ready` handler, ~line 8581) — not a bug on its own. The real issue is what
it's guarding against: **registration is first-come-first-served, not
owner-bound.** Every `datawatch-channel` bridge process on the host — one per
live Claude Code session/worktree — registers itself against the daemon by
session ID. MCP server entries are written at **user scope**
(`claude mcp add --scope user`), so every Claude process on the box loads the
*same* set of per-session server definitions, not just the one it actually
belongs to. Concretely, tonight: the port the daemon has registered for
session `ab22` (port `36777`) is actually owned by a child process of the
`9245` Claude session, not `ab22`'s own process — because `9245`'s bridge
happened to report `ready` first. `api.go` (~line 8540-8600) has no concept
of "this session's bridge is only allowed to come from this session's own
process tree" — it just accepts whichever bridge claims a given session ID
first and ignores every later claimant as a "stray duplicate."

## Why this matters

Today this is mostly harmless — the "wrong" bridge still proxies to the same
daemon and the same tool set, so tool calls still work. But it means:

- A session's MCP tool calls are, in principle, being served by a different
  Claude process's bridge than the one that's supposed to own that session —
  an attribution/isolation gap, not just log noise.
- If the "winning" bridge's parent Claude process exits or is killed, the
  session whose ID it won stops working even though *that session's own*
  Claude process and bridge are still alive and healthy — the daemon has no
  way to know it should fail over to the rightful owner.
- Debugging "why is my MCP tool list stale/wrong" for any given session
  requires manually cross-referencing `pgrep -f datawatch-channel` parent
  PIDs against live Claude processes, exactly as done in tonight's
  investigation — there's no daemon-side visibility into which bridge
  process actually owns which session's registration.

## What would need to change

1. **Scope MCP server entries away from user-scope**, or
2. **Make the daemon's `ready` handler owner-bound** — verify the reporting
   bridge's session ID against some proof of ownership (e.g. a session-scoped
   token minted at session creation and passed to the bridge via env, not
   just a self-reported session ID string) before accepting a `ready` claim,
   rejecting (not silently ignoring) a claim that doesn't match.

Neither is a small change — (1) touches how Claude Code itself is configured
system-wide for this operator, (2) touches the channel-bridge handshake
protocol (`internal/channel/`, `cmd/datawatch-channel/`, and the daemon's
`ready`/registration path in `internal/server/api.go`) and needs a real
look at backward compatibility with already-registered bridges.

## Not yet done

No code. No decision on which of the two directions (or another) to take.
Filed so this doesn't get lost after tonight's investigation — not urgent
(nothing is currently broken by it), but a real correctness gap worth fixing
deliberately rather than rediscovering cold next time a stray-bridge log line
shows up.
