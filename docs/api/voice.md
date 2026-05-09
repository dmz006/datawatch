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

**v7.0.0+ chained fallback + auto-routing.** When `backend: ollama`
or `backend: openwebui` is set and the local whisper venv is also
installed, the daemon builds a chain at startup (primary HTTP →
secondary local venv) so a server outage / model-not-loaded / engine
misconfig falls through silently. The daemon also:

- Resolves the OpenWebUI endpoint to `<url>/api/v1` (their audio API
  is at `/api/v1/audio/transcriptions`, **not** `/v1`).
- For `backend: ollama`, transparently routes through the configured
  OpenWebUI (bare Ollama has no audio endpoint).
- Transcodes the browser-mic blob (typically webm/opus) to 16-kHz
  mono WAV via `ffmpeg` before posting to the primary, so OpenWebUI
  / faster-whisper-server / whisper.cpp all accept the upload.
- 503/504 retries up to 4× with exponential backoff (handles "model
  is loading").
- Fast-fails on server-engine-misconfig signatures (`ctranslate2` /
  `cuda` / `engine-not` / `import-error`) so the chain secondary
  takes over without burning 60 seconds of retry.

See [`howto/voice-input.md`](../howto/voice-input.md#openwebui-configuration)
for OpenWebUI-on-ARM / NVIDIA Jetson Thor specifics, including the
`CTranslate2 not compiled with CUDA support` failure mode and four
ways to fix it.

## See also

- [`docs/flow/voice-transcribe-flow.md`](../flow/voice-transcribe-flow.md) — request → backend → text → command flow
- [`docs/setup.md`](../setup.md) — initial Whisper install guide
- BL189 (open) — Whisper local + ollama / openwebui integration in
  the backlog at [`docs/plans/README.md`](../plans/README.md)

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/voice-input](../howto/voice-input.md)
- [howto/comm-channels](../howto/comm-channels.md)
