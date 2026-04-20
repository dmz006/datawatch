# `POST /api/ask` — read-only LLM ask

**Shipped in v3.5.0 (BL34).** Sends a single-shot prompt to a chat
backend and returns the answer inline. No tmux session, no persistent
state — fast path for quick questions.

---

## When to use

- An operator wants a one-off answer ("what's the syntax for X?") without
  burning a session.
- An AI agent needs to consult an alternative model for a quick check.
- A messaging-channel listener wants to surface a cheap answer back to
  the user without spawning a worker.

If you need state across turns, threading, or tool use — start a session
instead (`POST /api/sessions`).

---

## Endpoint

```
POST /api/ask
Content-Type: application/json
```

### Request body

```json
{
  "question": "<required, non-empty>",
  "backend":  "ollama" | "openwebui",      // optional, default "ollama"
  "model":    "<optional model override>"  // optional
}
```

When `model` is omitted, the configured default is used:
- `ollama.model` for Ollama
- `openwebui.model` for OpenWebUI

### Response (200 OK)

```json
{
  "backend":      "ollama",
  "model":        "llama3.2:3b",
  "answer":       "...",
  "duration_ms":  812
}
```

### Error codes

| Code | Cause |
|------|-------|
| 400  | Empty question, unknown backend, missing model |
| 405  | Method other than POST |
| 500  | Backend returned an error or is not configured |

---

## Examples

### curl

```bash
curl -sS -X POST http://localhost:8080/api/ask \
  -H 'Content-Type: application/json' \
  -d '{"question":"What does GOMAXPROCS default to?"}' | jq .
```

### MCP client (deferred — adds in S2/S3)

The MCP `ask` tool will mirror the REST contract; until that ships use
the REST endpoint directly via your MCP client's HTTP-tool fallback.

### From a comm channel

The router's `ask:` command (planned in S2) will accept the same
question + optional backend in the same format. Today, route
ask-style prompts through the REST endpoint via your bot bridge.

---

## Configuration

`POST /api/ask` works as soon as the chosen backend is configured:

```yaml
ollama:
  enabled: true
  host:    http://localhost:11434
  model:   llama3.2:3b

openwebui:
  enabled: true
  url:     http://localhost:3000
  model:   gpt-oss:20b
  api_key: <token>
```

No new fields were added — `ask` consumes the existing backend config.
