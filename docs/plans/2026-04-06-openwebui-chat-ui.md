# B26: OpenWebUI Interactive Chat UI

**Date:** 2026-04-06
**Priority:** medium
**Effort:** 4-6 hours
**Category:** UI / backends

---

## Problem

OpenWebUI interactive sessions display in the web UI as raw tmux terminal output.
The conversation backend (`conversation.go`) sends all text via `tmux send-keys`
with `echo` and `printf` commands, so the terminal shows shell commands rather than
a structured chat interface:

```
[openwebui] interactive mode — model: llama3
> What is 2+2?
printf '%s\n' 'The answer is 4.'
[openwebui] ready for next message
```

This is confusing and ugly — it should look like a chat application with distinct
user/assistant message blocks, similar to ChatGPT or the existing channel tab.

## Root Cause

The backend writes all output to tmux via `exec.Command("tmux", "send-keys")`,
then the web UI captures the tmux pane and displays it in xterm.js. There is no
structured message format — everything is raw shell text.

---

## Solution: Chat Output Mode

Add a third output mode `"chat"` alongside `"terminal"` and `"log"`. When a
session uses chat mode:

1. **Backend** emits structured chat messages via a new WS message type
   (`chat_message`) instead of writing to tmux
2. **Web UI** renders these as styled chat bubbles in a scrollable container
3. **tmux** is still used as a fallback/log but the primary display is the chat view

### Phase 1: WS chat message type and backend changes

**Files:** `ws.go`, `conversation.go`, `server.go`, `api.go`

1. Add `MsgChatMessage` WS message type with data:
   ```json
   {"session_id": "...", "role": "user|assistant|system", "content": "...", "streaming": false}
   ```
2. Add `NotifyChatMessage()` on HTTPServer that broadcasts to WS clients
3. Wire a callback from session manager: `onChatMessage func(sessionID, role, content string, streaming bool)`
4. Modify `conversation.go` to call the callback instead of (or in addition to)
   writing to tmux:
   - User message → callback with role=user
   - Each streamed chunk → callback with role=assistant, streaming=true
   - Final flush → callback with role=assistant, streaming=false

### Phase 2: Web UI chat renderer

**Files:** `app.js`, `style.css`

1. In `renderSessionDetail()`, when `output_mode === 'chat'`:
   - Create a chat container div instead of xterm.js
   - Style with CSS chat bubbles (user = blue left-border, assistant = green left-border)
2. Handle `chat_message` WS messages:
   - `streaming=true` → append to current assistant bubble (progressive rendering)
   - `streaming=false` → finalize the current bubble
   - `role=user` → add a new user bubble
   - `role=system` → add a muted system info line
3. Auto-scroll to bottom on new messages
4. Maintain chat history in `state.chatMessages[sessionId]`

### Phase 3: Config and defaults

**Files:** `config.go`

1. Default `OpenWebUI.OutputMode` to `"chat"` when interactive mode is used
2. Keep `"terminal"` as fallback for curl/script backend
3. tmux session still exists for the session lifecycle but primary display is chat

---

## Implementation Notes

- Reuse existing channel-reply-line / channel-send-line CSS patterns for chat bubbles
- Chat messages buffer in memory like `state.channelReplies` does currently
- The tmux pane still runs (needed for session lifecycle, completion detection)
  but the user sees the chat view by default
- Input bar already exists — just routes through `SendInput` → `SendMessageOWUI()`
- No changes needed to session manager state detection — completion patterns
  still work via tmux pane capture

---

## Files Summary

| File | Changes |
|------|---------|
| `internal/server/ws.go` | Add `MsgChatMessage` type, `ChatMessageData` struct |
| `internal/server/server.go` | Add `NotifyChatMessage()` method |
| `internal/server/api.go` | Wire chat message broadcast for WS `subscribe` |
| `internal/llm/backends/openwebui/conversation.go` | Emit chat messages via callback instead of tmux printf |
| `internal/session/manager.go` | Add `onChatMessage` callback, wire to OpenWebUI backend |
| `internal/server/web/app.js` | Chat renderer for `output_mode=chat`, handle `chat_message` WS type |
| `internal/server/web/style.css` | Chat bubble styles |
| `internal/config/config.go` | Default OpenWebUI output_mode to `"chat"` |
