# BL83: OpenCode-ACP Rich Chat Interface

**Date:** 2026-04-10
**Priority:** medium
**Effort:** 2-3 days
**Category:** chat / backends
**Source:** Inspired by [HackingDave/nightwire](https://github.com/HackingDave/nightwire) — multi-backend chat unification; extended from BL77 (Ollama chat mode) and BL80-82 (chat UI features)

---

## Overview

OpenCode-ACP currently outputs responses as amber "channel reply" lines via `OnChannelReply`, bypassing the rich chat UI entirely. When `output_mode=chat` is set, ACP sessions should render structured chat messages with the same rich UI as Ollama and OpenWebUI: avatars, timestamps, thinking overlays, memory commands, and conversation threads.

ACP is architecturally different from Ollama/OpenWebUI — it uses an SSE event stream with structured events (`message.part.delta`, `session.status`, `session.idle`, etc.) rather than HTTP request/response streaming. This makes it *better* suited for chat than the other backends because events are explicit and typed, but requires a different integration path.

## Current State

### How ACP works today

```
main.go → llm.Register(opencode.NewACP(...))
         ↓
Launch() → starts `opencode serve --port N` in tmux
         → waits for HTTP server ready (polls GET /session)
         → POST /session to create session
         → subscribes to GET /event (SSE stream)
         → sends initial task via POST /session/{id}/message
         ↓
streamEvents() → reads SSE events, writes status to logFile:
  session.status(busy)  → "[opencode-acp] processing..."
  message.part.delta    → accumulates text chunks
  message.part.updated(step-start)  → "[opencode-acp] thinking..."
  message.part.updated(step-finish) → flushes text → OnChannelReply(fullID, text)
  session.idle          → "[opencode-acp] awaiting input"
  session.error         → "[opencode-acp] error: ..."
  session.completed     → "DATAWATCH_COMPLETE: opencode-acp done"
```

### What's missing for chat

1. **No chat emitter** — `SetChatEmitter` is called for Ollama and OpenWebUI in main.go but not for ACP
2. **Responses go to OnChannelReply** — renders as amber channel reply lines, not chat bubbles
3. **No thinking overlay** — step-start/step-finish events are logged as text but not emitted as chat system messages
4. **No processing indicator** — `session.status(busy)` not emitted to chat
5. **No user message echo** — when input arrives via comm channel/API, it doesn't appear as a user bubble
6. **Prompt detection interference** — ACP uses explicit protocol messages (`[opencode-acp] awaiting input`) so tmux prompt detection is already skipped for structured channels, but chat-mode skip needs to be verified

## Architecture

### Chat event mapping

ACP SSE events map naturally to chat messages:

| ACP SSE Event | Chat Message | Role | Streaming |
|---------------|-------------|------|-----------|
| `session.status(busy)` | "Processing..." | system | false |
| `message.part.updated(step-start)` | "Thinking..." | system | false |
| `message.part.delta(text)` | text chunk | assistant | true |
| `message.part.updated(step-finish)` | "" (signal complete) | assistant | false |
| `session.idle` | "Ready for next message" | system | false |
| `session.error` | error text | system | false |
| User input (SendInput) | user text | user | false |

### Data flow (proposed)

```
streamEvents() SSE loop
  ├─ session.status(busy) → emitChat(session, "system", "Processing...", false)
  ├─ step-start → emitChat(session, "system", "Thinking...", false)
  ├─ message.part.delta → emitChat(session, "assistant", chunk, true)
  ├─ step-finish → emitChat(session, "assistant", "", false)  // signal done
  │                 + OnChannelReply(fullID, text)  // backward compat
  ├─ session.idle → emitChat(session, "system", "Ready", false)
  └─ session.error → emitChat(session, "system", "Error: ...", false)

SendInput() → manager emits user chat message (already wired for chat-mode)
           → SendMessageACP() posts to HTTP API
```

## Implementation Plan

### Phase 1: Wire chat emitter (30 min)

**File:** `internal/llm/backends/opencode/acpbackend.go`

1. Add chat emitter variable and setter (same pattern as ollama/openwebui):
   ```go
   var chatEmitter func(string, string, string, bool)
   
   func SetACPChatEmitter(fn func(sessionID, role, content string, streaming bool)) {
       chatEmitter = fn
   }
   
   func emitChat(tmuxSession, role, content string, streaming bool) {
       if chatEmitter != nil {
           sessionID := strings.TrimPrefix(tmuxSession, "cs-")
           chatEmitter(sessionID, role, content, streaming)
       }
   }
   ```

2. In `streamEvents()`, emit chat messages alongside existing log writes:
   - After `[opencode-acp] processing...` → `emitChat(tmuxSession, "system", "Processing...", false)`
   - After `[opencode-acp] thinking...` → `emitChat(tmuxSession, "system", "Thinking...", false)`
   - On `message.part.delta` text chunks → `emitChat(tmuxSession, "assistant", chunk, true)`
   - On `step-finish` text flush → `emitChat(tmuxSession, "assistant", "", false)` (signal complete)
   - After `[opencode-acp] awaiting input` → `emitChat(tmuxSession, "system", "Ready for next message", false)`
   - After `[opencode-acp] error:` → `emitChat(tmuxSession, "system", "Error: "+msg, false)`

3. Keep `OnChannelReply` call intact for backward compatibility with non-chat sessions.

**File:** `cmd/datawatch/main.go`

4. Add `opencode.SetACPChatEmitter(chatEmitFn)` alongside existing Ollama/OpenWebUI emitter wiring (around line 897).

### Phase 2: Config and output mode (30 min)

**File:** `internal/config/config.go`

1. Set ACP default `output_mode` to `"chat"` in `GetOutputMode()` (same as Ollama):
   ```go
   case "opencode-acp":
       mode = c.OpenCodeACP.OutputMode
       if mode == "" {
           mode = "chat"
       }
   ```

2. Add `output_mode` and `input_mode` to the API config response for opencode-acp section.

**File:** `internal/session/manager.go`

3. Verify chat-mode skip is working for ACP:
   - `sess.OutputMode != "chat"` checks in capture-pane and idle timeout paths
   - ACP already has `hasStructuredChannel()` which skips generic terminal detection
   - Confirm no double-skip interference

### Phase 3: User message echo (30 min)

The session manager already emits user chat messages for all chat-mode backends in `SendInput()` (line 1063-1067). Verify this works for ACP:

1. Create an ACP session with `output_mode=chat`
2. Send input via comm channel (`send <id>: hello`)
3. Confirm user bubble appears in chat UI

If the user message isn't appearing, check that `SendInput` reaches the chat emission before routing to `SendMessageACP()`.

### Phase 4: Memory commands in chat (15 min)

The manager's `chatMemoryFn` intercepts memory commands for all chat-mode sessions before routing to backends. Verify this works for ACP:

1. Send `remember: ACP test memory` to an ACP chat session
2. Confirm it's intercepted and stored (not sent to opencode as a prompt)
3. Send `recall: test` and verify response appears as system bubble

### Phase 5: Thinking overlay integration (1 hour)

The web UI's `renderChatMarkdown()` already supports thinking overlays for `<think>...</think>` blocks. ACP has explicit `step-start`/`step-finish` events that are richer.

**File:** `internal/server/web/app.js`

1. When a system message contains "Thinking..." from ACP, start the thinking overlay
2. When the next assistant message arrives (streaming=false, content=""), close the thinking overlay
3. Optionally: emit step-start as a system message with metadata that the JS can use to show an animated thinking indicator (not just static text)

### Phase 6: Handle ACP-specific chat features (1 hour)

**Tool execution visibility:**
ACP's `step-start`/`step-finish` events correspond to tool calls. In chat mode, these could render as:
- Collapsible "Tool: Read file.go" sections
- Show tool output in a code block within the collapse

**Implementation:** Parse `message.part.updated` events with `type=step-start` to extract tool name and render as a collapsible system message.

**Error handling:**
`session.error` events should render as red-bordered system messages in chat, not just text.

### Phase 7: Testing (1 hour)

Test matrix (same as Ollama/OpenWebUI validation):

| Test | Channel | Expected |
|------|---------|----------|
| Create ACP chat session | API `POST /api/sessions/start` | `output_mode=chat`, no false waiting_input |
| Send prompt | API test/message `send <id>: hello` | User bubble + assistant response |
| Send second prompt | Comm channel | Routed via SendMessageACP, response streams |
| Memory command | Comm channel `send <id>: remember: test` | Intercepted, stored, system bubble |
| Thinking overlay | Web UI | Thinking... appears, collapses when response starts |
| Error handling | Force error | Red system message |
| Processing indicator | Any | "Processing..." system message before response |
| Kill session | API | No orphaned SSE connections |
| Config change | API PUT | `output_mode` changeable at runtime |

### Phase 8: Documentation (30 min)

1. Update `docs/commands.md` — note ACP chat mode availability
2. Update `docs/config-reference.yaml` — add `opencode_acp.output_mode` with comment
3. Update `docs/llm-backends.md` — add chat mode column for ACP
4. Update `CHANGELOG.md` — BL83 feature entry

---

## Files to modify

| File | Changes |
|------|---------|
| `internal/llm/backends/opencode/acpbackend.go` | Add chatEmitter, emitChat calls in streamEvents |
| `cmd/datawatch/main.go` | Wire SetACPChatEmitter |
| `internal/config/config.go` | Default output_mode=chat for ACP |
| `internal/session/manager.go` | Verify chat-mode skip for ACP (may need no changes) |
| `internal/server/web/app.js` | ACP thinking overlay, tool execution collapsible |
| `docs/config-reference.yaml` | Add opencode_acp.output_mode |
| `docs/commands.md` | Note ACP chat mode |
| `docs/llm-backends.md` | Update backend comparison table |

## Risk assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| SSE stream already writes to logFile; double-emission to chat | Duplicate messages | Chat emitter calls should be parallel to log writes, not additive. Check JS side doesn't render raw log + chat. |
| ACP server starts slow (30s timeout) | Chat shows empty state | Show "Starting ACP server..." system message during waitForServer phase |
| Tool execution events may flood chat | UI clutter | Collapse tool steps by default, only show summary line |
| Backward compat with non-chat ACP sessions | Regression | Keep OnChannelReply for all sessions; chat emitter only fires when set |
| Config migration | Existing ACP sessions break | Default to chat only for new sessions; respect explicit output_mode config |

## Dependencies

- BL77 (Ollama chat mode) — **DONE** (v1.5.0)
- BL80-82 (Chat UI features) — **DONE** (v2.2.0)
- Chat-mode prompt detection skip — **DONE** (v2.2.6)
- Prompt debounce/cooldown — **DONE** (v2.2.4)

## Estimated effort

| Phase | Time |
|-------|------|
| Wire chat emitter | 30 min |
| Config and output mode | 30 min |
| User message echo | 30 min |
| Memory commands | 15 min |
| Thinking overlay | 1 hour |
| ACP-specific features | 1 hour |
| Testing | 1 hour |
| Documentation | 30 min |
| **Total** | **~5 hours** |
