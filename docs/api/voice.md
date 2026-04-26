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
voice:
  backend: whisper        # whisper | openai | local | ollama (BL189 — pending)
  api_key: ""             # cloud Whisper / OpenAI
  model: small.en         # whisper-* model id
  endpoint: ""            # for local servers
```

## See also

- [`docs/flow/voice-transcribe-flow.md`](../flow/voice-transcribe-flow.md) — request → backend → text → command flow
- [`docs/setup.md`](../setup.md) — initial Whisper install guide
- BL189 (open) — Whisper local + ollama / openwebui integration in
  the backlog at [`docs/plans/README.md`](../plans/README.md)
