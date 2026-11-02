# Release Notes — v6.11.14 (PWA channel tab visibility + chat-message inclusion)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.14

## Summary

Operator: "I'm not seeing any activity on the channel tab, in the mobile app I do. Also the details I see there would fix the running vs waiting detection, fix channel window and see if that fixes running detection if you use that."

This patch addresses the visibility half. The running-detection wiring is held pending operator clarification on which specific channel signals to use as the "active" indicator.

## Fixed — Bug A: channel tab hidden when channel not ready

`internal/server/web/app.js` `renderSessionDetail` had `showChannel = sessionMode === 'channel' && connReady`. The `connReady` gate hid the entire channel tab until the MCP channel server reported ready. After daemon restart, channel-ready takes a few seconds to re-assert; during that window the operator couldn't see channel history at all even though it existed in the ring buffer.

Fix: changed to `showChannel = sessionMode === 'channel'`. The tab is visible whenever the session uses MCP channel mode (claude / claude-code), regardless of channel-ready state. Matches what the mobile app does. Input is still gated on `connReady` (you can't send a message until the channel is ready), but the activity log is always visible.

## Fixed — Bug B: chat-mode messages didn't feed the channel tab

`internal/server/web/app.js` `chat_message` WS handler routed messages exclusively to the chat view. The channel tab only consumed `channel_reply` and `channel_notify` events, so chat-mode sessions with all activity flowing through the standard chat path saw an empty channel tab.

Fix: when a `chat_message` event arrives, additionally call `handleChannelReply` with the same content tagged by role (`[assistant]` / `[user]`). Channel tab now reflects every LLM-emitted message + every operator-sent message, matching the mobile app's behavior.

## Pending — running-vs-waiting detection

Operator hint: the channel events the mobile app shows "would fix the running vs waiting detection". Specific signals not yet identified — could be tool-call events, permission-relay notifications, message-content patterns, or all of the above. Once operator confirms channel tab is now showing activity in v6.11.14 and points at the specific signal, daemon-side state-transition wiring follows in a separate patch.

Per the recent feedback rule (saved memory: don't widen global default detection patterns to natural-language phrases without operator opt-in), I'm explicitly NOT speculating.

## Tests

1767 pass.

## Mobile parity

[`datawatch-app#68`](https://github.com/dmz006/datawatch-app/issues/68) filed — mobile already surfaces this; ticket tracks any divergence.

## See also

- CHANGELOG.md `[6.11.14]`
