# How-to: Voice input

Datawatch transcribes voice messages so you can speak into Signal,
Telegram, Slack, etc., or hit the PWA microphone, instead of typing.
Four supported transcription backends; pick whichever matches what
you already have running.

## What it is

Inbound voice notes (or PWA microphone audio) get transcribed by a
configured backend and routed into the session as if you'd typed the
text. Backends:

| Backend | Where it runs | Privacy | One-line summary |
|---|---|---|---|
| `whisper` | Local Python venv | private | Default; needs Python + the `whisper` model files. |
| `openai` | Cloud (api.openai.com) | sends audio to OpenAI | Easiest if you already pay for an OpenAI key. |
| `openai_compat` | Any OpenAI-compatible HTTPS endpoint | depends | faster-whisper-server, vLLM, custom. |
| `openwebui` | Your already-configured OpenWebUI | private | Reuses whatever model OpenWebUI is fronting. |
| `ollama` | Your already-configured Ollama | private | Reuses Ollama's transcribe endpoint. |

Non-local backends (`openai`, `openai_compat`, `openwebui`, `ollama`)
**inherit endpoint + API key from the matching LLM-backend config
block** — you don't enter them twice.

## Base requirements

Per-backend:

- `whisper`: Python 3.10+, `pip install openai-whisper`, model files
  on disk.
- `openai`: API key in secrets (`${secret:OPENAI_API_KEY}`).
- `openai_compat`: HTTPS endpoint that speaks the OpenAI
  `/v1/audio/transcriptions` shape.
- `openwebui` / `ollama`: the parent LLM backend already configured.

## Setup

### Local whisper (default, best privacy)

```sh
python3 -m venv ~/.datawatch/venv
~/.datawatch/venv/bin/pip install -U openai-whisper

datawatch config set whisper.enabled true
datawatch config set whisper.backend whisper
datawatch config set whisper.venv_path ~/.datawatch/venv
datawatch config set whisper.model base       # tiny / base / small / medium / large
datawatch config set whisper.language en      # or "auto"
datawatch reload
```

### OpenAI cloud

```sh
datawatch secrets set OPENAI_API_KEY "sk-..."
datawatch config set whisper.enabled true
datawatch config set whisper.backend openai
datawatch reload
```

### OpenAI-compatible (faster-whisper-server, vLLM, etc.)

```sh
datawatch config set whisper.enabled true
datawatch config set whisper.backend openai_compat
datawatch config set whisper.openai_compat_url https://my-stt.local/v1
datawatch config set whisper.openai_compat_key '${secret:STT_KEY}'
datawatch reload
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Test the configured backend.
datawatch voice test
#  → ok; transcript_test_phrase recognized; latency_ms=423

# 2. Transcribe a file ad-hoc.
datawatch voice transcribe ~/recordings/note.ogg
#  → text: "Refactor the payment module to use the new auth"

# 3. Tail voice activity (across all channels).
datawatch voice tail -f
# (Send a voice note from Signal — appears here as transcribed text.)
```

### 4b. Happy path — PWA

1. Settings → General → **Voice Input** card. Pick backend → **Test
   transcription endpoint**. Toast confirms success.
2. (If failure: card auto-disables `whisper.enabled` so the daemon
   stops trying. Fix config and re-test.)
3. Now in any session detail: tap the **🎤 microphone** icon next to
   the input bar.
4. Speak; the icon glows red while recording. Tap again to stop.
5. The transcribed text appears in the input bar; edit if needed and
   press Enter to send.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Settings → General → Voice Input card. Microphone icon in the
session input bar uses the OS audio recorder. Round-trip parity with
the PWA.

### 5b. REST

```sh
# Test.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" $BASE/api/voice/test

# Transcribe a file.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -F "audio=@/path/to/note.ogg" \
  $BASE/api/voice/transcribe
#  → {"text":"Refactor the payment module..."}
```

### 5c. MCP

Tool: `voice_transcribe` accepting `{ "audio_b64": "<base64>" }` and
returning `{ "text": "..." }`. Useful when an LLM in a session wants
to process an audio attachment a user dropped into chat.

### 5d. Comm channel

Send a voice note to any voice-capable channel adapter (Signal,
Telegram, Slack, Discord, Matrix). The adapter detects the audio
attachment, calls the transcription backend, and treats the result
as session input — exactly as if you'd typed it.

```
(You send a voice note in Signal: "Refactor the payment module")
Bot: [transcribed] Refactor the payment module
Bot: <LLM's response>
```

If the transcription confidence is low (< 0.6), the bot replies with
the transcript + a `?` so you can correct it before it acts.

### 5e. YAML

```yaml
whisper:
  enabled: true
  backend: whisper                      # or openai / openai_compat / openwebui / ollama
  venv_path: ~/.datawatch/venv
  model: base                            # tiny / base / small / medium / large
  language: en                           # or "auto"

  # Backend-specific:
  openai_compat_url: ""
  openai_compat_key: ""
  ollama_model: whisper

  # Common:
  min_confidence: 0.6                    # below = ask operator to confirm
  max_audio_seconds: 120                 # reject longer files
```

`datawatch reload` picks up changes without restart.

## Diagram

```
   Signal/Telegram/PWA mic    REST upload
            │                     │
            └──────────┬──────────┘
                       │
                       ▼
           ┌──────────────────────┐
           │ Voice transcribe API │
           └──────────┬───────────┘
                      │ pick configured backend
                      ▼
   ┌──────┬──────────┬─────────────┬──────────┐
   │whisper│ openai   │ openai_compat│ openwebui │
   │(local)│ (cloud)  │ (custom HTTPS)│ / ollama  │
   └──┬────┴────┬─────┴──────┬──────┴────┬──────┘
      └─────────┴────────────┴───────────┘
                      │
                      ▼
              session input
```

## Common pitfalls

- **Whisper venv path wrong.** `datawatch voice test` returns
  "venv not found"; fix `whisper.venv_path` and reload.
- **Wrong language detection.** Set `whisper.language` explicitly if
  `auto` keeps misclassifying.
- **`openai` backend without API key in secrets.** Will fail with
  401. Confirm with `datawatch secrets get OPENAI_API_KEY`.
- **Audio too long.** Default cap is 120s. Raise `max_audio_seconds`
  for longer notes; very long audio is better split.
- **PWA mic permission denied.** Browser blocks mic access on
  non-HTTPS origins. Use `https://` (the daemon's default) — not
  `http://` redirect.

## Linked references

- See also: [`comm-channels.md`](comm-channels.md) — channel-specific voice handling.
- See also: [`secrets-manager.md`](secrets-manager.md) — store API keys.
- Architecture: `../architecture-overview.md` § Voice transcribe.

## Screenshots needed (operator weekend pass)

- [ ] Settings → General → Voice Input card configured
- [ ] Test transcription endpoint result toast
- [ ] PWA session input bar with mic icon (idle + recording state)
- [ ] Voice note round-trip on Signal showing the [transcribed] prefix
- [ ] Low-confidence transcript with the `?` confirm
