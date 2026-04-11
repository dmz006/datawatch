# B3: LLM Session Reconnect on Daemon Restart

**Date:** 2026-04-11
**Priority:** medium
**Effort:** 2-3 days
**Category:** sessions / backends
**Source:** Inspired by [HackingDave/nightwire](https://github.com/HackingDave/nightwire) — nightwire persists worker state and reconnects on restart

---

## Problem

When the datawatch daemon restarts (manual restart, crash recovery, update), all in-memory LLM backend state is lost. Running sessions continue in their tmux panes, but datawatch can no longer:

- **ACP**: Route input via HTTP API (lost `acpStateMap` — port, session ID). Can't re-subscribe to SSE events. User sees "waiting on ACP" because `waitForServer` re-runs.
- **Ollama chat**: Route input via `/api/chat` (lost `conversations` sync.Map — message history). Falls back to tmux send-keys which doesn't work with chat mode.
- **OpenWebUI chat**: Route input via conversation manager (lost `InteractiveBackend.conversations` — message history). Same tmux fallback issue.

The tmux sessions survive the restart, and `monitorOutput` reconnects to the log files. But the conversation managers are gone.

### User impact
- ACP sessions: "waiting on ACP" message, input fails until session is killed and restarted
- Ollama/OpenWebUI chat sessions: input falls through to tmux send-keys which echoes raw text instead of routing through the API
- All chat-mode sessions: no more streaming chat messages (emitter callbacks lost)

---

## Architecture

### Current state (lost on restart)

```
Daemon memory (volatile):
  acpStateMap:       { "cs-xxx": { baseURL, sessionID, fullID } }
  conversations:     { "cs-xxx": { messages: [...] } }
  backendRegistry:   { "cs-xxx": *Backend }
  chatEmitter:       func(...)
```

### Proposed: persist session backend state

```
~/.datawatch/sessions/<fullID>/backend_state.json
{
  "backend": "opencode-acp",
  "acp_base_url": "http://127.0.0.1:54321",
  "acp_session_id": "sess-abc123",
  "ollama_host": "http://datawatch:11434",
  "ollama_model": "qwen3.5:35b",
  "conversation_history": [
    {"role": "user", "content": "what is 2+2?"},
    {"role": "assistant", "content": "4"}
  ]
}
```

---

## Implementation plan

### Phase 1: Persist backend state on creation (0.5 day)

**File:** `internal/session/manager.go`

1. Define `BackendState` struct:
   ```go
   type BackendState struct {
       Backend          string        `json:"backend"`
       ACPBaseURL       string        `json:"acp_base_url,omitempty"`
       ACPSessionID     string        `json:"acp_session_id,omitempty"`
       OllamaHost       string        `json:"ollama_host,omitempty"`
       OllamaModel      string        `json:"ollama_model,omitempty"`
       OpenWebUIBaseURL string        `json:"openwebui_base_url,omitempty"`
       OpenWebUIModel   string        `json:"openwebui_model,omitempty"`
       OpenWebUIAPIKey  string        `json:"openwebui_api_key,omitempty"`
       ConversationHistory []struct {
           Role    string `json:"role"`
           Content string `json:"content"`
       } `json:"conversation_history,omitempty"`
   }
   ```

2. Save `backend_state.json` to tracking dir:
   - ACP: after `createSession()` succeeds, write baseURL + sessionID
   - Ollama: after `LaunchChat()`, write host + model
   - OpenWebUI: after `Launch()`, write baseURL + model + apiKey
   - Conversation history: append on each `sendAndStream()` completion

**File:** `internal/llm/backends/opencode/acpbackend.go`

3. Add `SaveState(trackingDir string)` method that writes the ACP-specific fields.

**File:** `internal/llm/backends/ollama/conversation.go`

4. Add `SaveConversation(trackingDir string)` that persists message history.

### Phase 2: Reconnect on daemon startup (1 day)

**File:** `internal/session/manager.go`

1. In `monitorOutput()` start (or a new `reconnectBackends()` method called from `NewManager`):
   - For each session in store with `State == StateRunning || State == StateWaitingInput`:
     - Check if tmux session still exists
     - Read `backend_state.json` from tracking dir
     - Reconnect based on backend type

2. **ACP reconnect:**
   - Read `acp_base_url` and `acp_session_id`
   - Probe `GET /session` to verify server is still alive
   - If alive: store in `acpStateMap`, re-subscribe SSE via `streamEvents()`
   - If dead: mark session as failed or attempt restart

3. **Ollama reconnect:**
   - Read host + model + conversation history
   - Create new `Backend` instance, register in `backendRegistry`
   - Load conversation history into `conversationState`
   - Re-register chat emitter

4. **OpenWebUI reconnect:**
   - Read baseURL + model + apiKey + conversation history
   - Create new `InteractiveBackend`, set as `activeBackend`
   - Load conversation history

### Phase 3: Keep state updated (0.5 day)

1. **Conversation history**: append after each `sendAndStream()` in Ollama and OpenWebUI
2. **ACP state**: update on session ID changes (if ACP server restarts)
3. **Cleanup**: delete `backend_state.json` when session completes/kills
4. **Encryption**: if `--secure` mode, encrypt the state file (contains API keys)

### Phase 4: Testing (0.5 day)

| Test | Steps | Expected |
|------|-------|----------|
| ACP reconnect | Start ACP session → restart daemon → send input | Input routed via HTTP, SSE re-subscribed |
| Ollama reconnect | Start Ollama chat → send 2 prompts → restart → send 3rd | History preserved, 3rd prompt gets context from 1+2 |
| OpenWebUI reconnect | Same as Ollama | Same — history preserved |
| Dead server | Start ACP → kill opencode serve → restart daemon | Session marked failed, not stuck |
| Clean shutdown | Kill session → verify state file removed | No orphaned state files |

---

## Files to modify

| File | Changes |
|------|---------|
| `internal/session/manager.go` | BackendState struct, reconnectBackends(), state save/load |
| `internal/llm/backends/opencode/acpbackend.go` | SaveState, reconnect from state |
| `internal/llm/backends/ollama/conversation.go` | SaveConversation, load from state |
| `internal/llm/backends/openwebui/conversation.go` | SaveConversation, load from state |
| `cmd/datawatch/main.go` | Call reconnectBackends() after manager setup |

## Risk assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Stale state file | Connects to wrong port | Verify server alive before reconnecting |
| API key in state file | Security exposure | Encrypt with session key when --secure |
| Conversation history too large | Disk bloat | Cap at last 50 messages, rotate |
| Race with monitorOutput | Double monitoring | Check if monitor already running before reconnect |

## Dependencies

- Session tracking dirs (already exist)
- ACP state map architecture (already exists, just needs persistence)
- Conversation managers (already exist, just need save/load)
