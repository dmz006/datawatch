---
docs:
  index: true
  topics: [voice, transcription, accessibility]
exec_params:
  - {name: backend, required: true, description: "whisper-local, whisper-server, openai, or none"}
  - {name: model, required: false, default: "", description: "Optional model override (whisper.cpp model file)"}
exec_steps:
  - tool: config_set
    description: Set the transcription backend
    args:
      key: "transcribe.backend"
      value: "{{params.backend}}"
    read_only: false
  - tool: config_set
    description: Set the transcription model (no-op if model unset)
    args:
      key: "transcribe.model"
      value: "{{params.model}}"
    read_only: false
  - tool: reload
    description: Apply the transcribe config without restarting the daemon
    args: {}
    read_only: false
---
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

### OpenWebUI (your existing OpenWebUI fronts the GPU)

Datawatch reuses the URL + API key from the `openwebui:` config block; you don't enter them twice.

```sh
datawatch config set whisper.enabled true
datawatch config set whisper.backend openwebui
datawatch config set whisper.model whisper-1   # or whatever your OpenWebUI engine accepts
datawatch reload
```

The daemon resolves the audio endpoint to `<openwebui-url>/api/v1/audio/transcriptions` (note the `/api/v1` path — OpenWebUI's audio API is **not** at `/v1`).

### Ollama (transparently routed through OpenWebUI)

Bare Ollama has **no audio endpoint**. When you set `whisper.backend: ollama`, datawatch transparently routes the audio request through your configured `openwebui:` block (because OpenWebUI is the only thing that fronts Ollama with an audio API). You'll see this on startup:

```
[voice] note: whisper.backend=ollama transparently routed through configured OpenWebUI at http://<host>:3000/api/v1
```

The fallback chain (see below) takes over if OpenWebUI's whisper engine isn't reachable.

## OpenWebUI configuration

OpenWebUI's audio API is at `/api/v1/audio/transcriptions` (the daemon handles this for you). What it does on the server side depends on **OpenWebUI Settings → Audio → Speech-to-Text Engine**. Pick one:

| Engine | Pros | Cons |
|---|---|---|
| `local` (CTranslate2 + faster-whisper) | Fast on CUDA x86_64 | **Broken on ARM unless CTranslate2 is built with CUDA-arm64.** Default install often returns `Error transcribing chunk: This CTranslate2 package was not compiled with CUDA support`. |
| `openai` | Uses OpenAI's API | Requires API key + sends audio to OpenAI |
| `whisper.cpp` (via the OpenWebUI extension) | Pure CPU works on any arch | Slower than CUDA, faster than venv |

### NVIDIA Jetson AGX Thor / other ARM-with-GPU

CTranslate2's published wheels are x86_64-only. On an ARM box (Jetson AGX Thor, NVIDIA Grace Hopper, generic Orin), the OpenWebUI default `local` engine will return "CTranslate2 not compiled with CUDA support" because the wheel pip pulled is a CPU-only ARM build.

Fix one of:

1. **Build CTranslate2 from source with CUDA-arm64**. Heavy; needs the JetPack CUDA toolchain. See the [CTranslate2 build docs](https://opennmt.net/CTranslate2/installation.html#install-from-sources). Pin a known-working commit.
2. **Switch OpenWebUI's audio engine to `whisper.cpp`** in Settings → Audio. whisper.cpp has good ARM CUDA support and the daemon doesn't care which engine the server picks.
3. **Switch OpenWebUI's audio engine to `openai`** if you have a key — sends audio off-box, defeats local-GPU goal but unblocks transcription.
4. **Skip OpenWebUI entirely on Thor and use the local whisper venv directly** (`whisper.backend: whisper`). The Python venv's `whisper` package builds against PyTorch, which has native ARM-CUDA wheels; works without CTranslate2.

The daemon **fast-fails** on CTranslate2/CUDA/engine-not-loaded errors and falls through the chain to the local whisper venv (if configured), so voice keeps working even when the server engine is broken — you just don't get the bigger GPU. Look for this footer on the transcript:

```
_(transcribe: openai-compat (...) failed (...); fell back to local-whisper (...))_
```

When you see that footer, fix the OpenWebUI engine — the chain saves you in the meantime.

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

## Auto-fallback chain (v7.0.0+)

When `whisper.backend` is HTTP-shape (`openai`, `openai_compat`, `openwebui`, `ollama`) **and** the local whisper venv is also installed, the daemon builds a **chained transcriber** at startup:

```
primary   = openai-compat (e.g. OpenWebUI)
secondary = local whisper venv      (last-resort)
```

What happens at runtime:

1. Browser-mic blob (webm/opus) arrives at `/api/voice/transcribe`.
2. Daemon transcodes to 16-kHz mono WAV via `ffmpeg` (must be on PATH).
3. POST to primary. On 503/504 retry with exponential backoff up to 4×.
4. On 404 (model not found), the openai-compat client tries a hardcoded chain of known whisper names (`whisper-1` → `whisper` → `large-v3` → … → `tiny`) before giving up.
5. On primary failure (or fast-fail signatures: `ctranslate2` / `cuda` / `engine-not` / `import-error`), fall through to the local whisper venv.
6. Result returns with a footer telling you which leg of the chain handled it.

You can see the chain wiring at startup:

```
[voice] enabled (backend=ollama, model=base, language=en)
[voice] fall-back: local whisper venv (~/.datawatch/venv, model=base) chained as last resort
```

Disable the auto-chain by leaving `whisper.venv_path` empty (only the primary path remains; failures bubble up).

## Startup preflight

The daemon actively probes the configured backend on startup by POSTing a 1-second silent WAV. Outcomes logged:

- Silent success — model is reachable + loaded.
- `preflight: HTTP 400 (...format/audio/file...)` — endpoint is reachable but rejected the synthetic probe; treated as success since real audio works fine.
- `preflight: model "X" not found` — runtime will use the fallback chain. Fix your `whisper.model` value.
- `preflight: HTTP 503/504` — server is loading the model; not an error.

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
- **OpenWebUI returns 400 "file format not supported".** Almost always actually masks a server-side engine error. Hit the endpoint with `curl` + your API key to see the real response. On ARM/Thor: it'll be the CTranslate2/CUDA error described in the OpenWebUI section above.
- **`ffmpeg` missing.** Daemon falls back to sending the original webm/opus blob to the primary; OpenWebUI usually rejects this. Install ffmpeg (`apt install ffmpeg`) so the WAV-transcode step runs.

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

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/comm-channels](comm-channels.md)
- [howto/chat-and-llm-quickstart](chat-and-llm-quickstart.md)
- [api/voice](../api/voice.md)
