# Voice transcription — operator reference

The PWA mic button + the mobile voice surface both POST audio
captures to a single transcribe endpoint, which routes the audio to
a Whisper-compatible backend (cloud, local, or — when configured — a
local LLM with audio input).

The flow diagram lives at
[`docs/flow/voice-transcribe-flow.md`](../flow/voice-transcribe-flow.md).

## Surface

### REST

| Endpoint | Purpose |
|---|---|
| `POST /api/voice/transcribe` | Multipart form with the audio blob; returns `{text}`. |
| `GET /api/voice/config` | Returns the active transcribe backend + model. |
| `PUT /api/voice/config` | Operator updates backend / API key / model. |

### MCP

`voice_transcribe` (file path or base64 audio) — see
[`docs/api-mcp-mapping.md`](../api-mcp-mapping.md).

### CLI

```bash
datawatch voice transcribe path/to/audio.wav
datawatch voice config get
datawatch voice config set --backend whisper-local --model base
```

### Chat / messaging

Voice messages received over Signal / Telegram (when the backend
supports voice attachments) are auto-routed through the same
transcribe endpoint and dispatched as text commands.

## Configuration

```yaml
whisper:
  enabled:  true
  backend:  whisper       # whisper (local Python venv) | openai | openai_compat
  model:    base          # whisper venv: tiny|base|small|medium|large
                          # openai: model name (default whisper-1)
  language: en            # ISO 639-1, "" or "auto" for detection
  venv_path: ".venv"      # used by `whisper` backend only

  # Used by openai / openai_compat backends:
  endpoint: ""            # base URL — request hits <endpoint>/audio/transcriptions
                          # OpenAI:    https://api.openai.com/v1
                          # OpenWebUI: http://<host>/api/v1
                          # whisper.cpp server: http://localhost:8080/v1
  api_key:  ""            # bearer; required for cloud OpenAI, optional for self-hosted
```

**Backend reach:** `whisper` runs the local OpenAI-Whisper CLI from
a Python venv. `openai` / `openai_compat` post the audio over HTTPS
to any OpenAI-compatible `/audio/transcriptions` endpoint (cloud
OpenAI, OpenWebUI fronting your ollama/local models, faster-whisper-
server, whisper.cpp server-mode). Bare ollama doesn't ship audio —
operators wanting the "ollama path" point this at OpenWebUI.

## See also

- [`docs/flow/voice-transcribe-flow.md`](../flow/voice-transcribe-flow.md) — request → backend → text → command flow
- [`docs/setup.md`](../setup.md) — initial Whisper install guide
- BL189 (open) — Whisper local + ollama / openwebui integration in
  the backlog at [`docs/plans/README.md`](../plans/README.md)
