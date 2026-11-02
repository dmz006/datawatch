# Release Notes — v6.11.16 (PWA channel tab mirrors mobile event stream)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.16

## Operator report

> "Channel tab still has no data."
> "Check what data is being shown in the datawatch-app."

## What I found by reading the mobile source

`composeApp/src/androidMain/kotlin/com/dmzs/datawatchclient/ui/sessions/SessionDetailScreen.kt:520` —

```kotlin
} else if (chatMode) {
    // User's view-mode toggle (Terminal vs Chat-style bubbles
    // for any non-chat session). Renders the event stream as
    // bubbles but is different from server-side chat mode.
    ChatEventList(
        events = state.events,
        ...
    )
}
```

When the operator toggles the mobile app to its "channel" view (`chatMode = true`) on a non-server-chat session, it renders `ChatEventList(events = state.events, ...)`. **`state.events` is the full session event stream** — output frames, pane captures, state changes, everything — formatted as chat-style bubbles.

PWA's channel tab was only consuming `channel_reply` / `channel_notify` / `chat_message` (v6.11.14 added the latter). For terminal-mode claude-code sessions, none of those flow. So the PWA channel tab was empty even when activity was streaming over WS via the `output` channel.

## Fixed

`internal/server/web/app.js` `output` WS handler — also routes the cleaned output lines into `handleChannelReply` so the channel tab shows them. Each output line becomes an incoming channel entry. This brings the PWA in line with mobile's channel-view behavior on terminal-mode sessions.

## Iteration history (channel tab arc)

| | |
|---|---|
| v6.11.14 | Channel tab visible whenever sessionMode=='channel' (was gated on connReady) + chat_message events feed channel | Helped chat-mode sessions; terminal-mode sessions still empty |
| v6.11.16 | `output` WS events also feed channel | Now matches mobile behavior on terminal-mode sessions |

## Tests

1767 pass.

## Mobile parity

Already shipped on mobile (this is the side that needed to catch up). [`datawatch-app#69`](https://github.com/dmz006/datawatch-app/issues/69) opens for divergence tracking only.

## See also

- CHANGELOG.md `[6.11.16]`
