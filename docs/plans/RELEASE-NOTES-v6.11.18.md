# Release Notes — v6.11.18 (BL264 — channel-based state detection)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.18

## Operator directive

> "Use the channel if available, like acp for opencode, it's a clear channel it's available for state detection, use it."

## What's new

`internal/session/manager.go` `MarkChannelActivity(fullID)` — when a channel-bearing backend (claude-code MCP, opencode-acp ACP) emits a reply or chat message, the daemon now treats that as authoritative evidence the session is actively running. Bumps state out of `WaitingInput` back to `Running`; respects terminal states and `RateLimited`.

Wired in:
- `internal/server/api.go` `BroadcastChannelReply` + `handleChannelReply` — fires after channel reply recorded.
- `internal/session/manager.go` `EmitChatMessage` — fires on assistant + user chat-message events. System-role messages skip the bump (transient "processing…" / "ready" indicators don't represent real activity).

## Why

Running-vs-waiting state has historically been driven by tmux pane pattern-matching. Pattern matchers can be fooled by stale prompt content (LLM finished a task but the previous prompt is still in the pane buffer). For backends with a structured channel, the channel itself is unambiguous evidence of activity. Using it eliminates the false-positive `WaitingInput` during long-running LLM responses.

## State-transition matrix

| Current | Channel activity | Result |
|---|---|---|
| WaitingInput | reply/chat | → Running (closes the false-positive case operator flagged) |
| Running | reply/chat | UpdatedAt touched, no transition |
| Complete / Failed / Killed | reply/chat | UpdatedAt touched, no transition (terminal states locked) |
| RateLimited | reply/chat | UpdatedAt touched, no transition (operator-visible pause) |

## Tests

1776 pass (1767 + 9 new BL264 cases). Coverage:
- Wake from WaitingInput
- No-op on Running
- No-op on Complete / Failed / Killed / RateLimited (4 subtests)
- Chat-message assistant bumps
- Chat-message system role does not bump

## Mobile parity

Not needed — daemon-internal; WS messages unchanged.

## See also

- CHANGELOG.md `[6.11.18]`
