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
- `openai`: API key stored as a secret (`${secret:OPENAI_API_KEY}`). See [secrets-manager.md](secrets-manager.md) for secret storage.
- `openai_compat`: HTTPS endpoint that speaks the OpenAI
  `/v1/audio/transcriptions` shape.
- `openwebui` / `ollama`: the parent LLM backend already configured and registered. See [llm-registry.md](llm-registry.md) for LLM backend registration and [compute-nodes.md](compute-nodes.md) for compute node setup.

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

### 4c. Recording overlay (v8.1.0)

When you tap the microphone icon the PWA shows a modal overlay rather
than an inline recording indicator:

- **Animated waveform** — five bars bounce with staggered timing,
  making it obvious the mic is live.
- **Cancel** — discards the recording and closes the overlay without
  sending anything.
- **Send** — stops recording immediately and submits audio for
  transcription exactly as before.

The overlay also closes (and discards) if you click outside it. This
is a PWA-only UX feature; the mobile app uses the OS native audio
recorder (see § 5a).

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
#  → {"transcript":"Refactor the payment module...", ...}
#  On failure: HTTP 502 {"error","cause","hint","detail","latency_ms"}
#  (see "When transcription fails: error causes" below).
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
  chunk_seconds: 120                     # local whisper: slice longer audio (0 = off, else >= 30)
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

## Long recordings (longer than 30 s)

Whisper is trained on 30 s windows, and some OpenAI-compatible backends
(windowed whisper.cpp / faster-whisper servers, duration-capping proxies)
refuse anything longer with HTTP 400. Since v9.5.32 this is handled
transparently: the daemon uploads the full recording first (correct for
full-length backends like OpenAI and OpenWebUI), and only if the backend
refuses does it split the audio locally into ≤30 s chunks — cutting at
near-silence points inside the last quarter of each window so words are
not split mid-syllable — transcribe each chunk in order, and join the
texts without duplication or loss. The split is deterministic for a
given input. If a chunk is still refused by the backend, the error is
surfaced (chunked retry never swallows a refusal).

**Upload size and time limits** (enforced by the daemon, in
`internal/server/voice.go` / `internal/transcribe/openai_compat.go`):

- **Upload size cap: 25 MB** — larger voice uploads are rejected with
  HTTP 413 (the Whisper API's own cap + headroom for a minute or more of
  16 kHz mono opus).
- **Server read timeout for voice uploads: 180 s** — covers a 25 MB
  body arriving over a slow link; a slow-but-valid upload still
  completes and transcribes.
- **Audio duration cap: configurable** — `max_audio_seconds` (default
  120 s) rejects longer files at intake; raise it for longer notes.
- **Transcription deadline: scales with the audio.** The request gets 5
  minutes, or twice the audio length when that is longer, capped at 30
  minutes. A clip whose length cannot be read gets the 5-minute base.
  The ffmpeg transcode has its own flat 5-minute budget. HTTP backends
  also retry 503/504 up to 4 times (1 s / 2 s backoff). There is no
  resumable upload, so either the full transcript comes back or the
  caller gets an error (see "When transcription fails" above).
- **PWA mic recordings:** Chrome's MediaRecorder emits webm with no
duration metadata; the handler's ffmpeg transcode to 16 kHz mono WAV
  handles arbitrarily long streams (verified with a 65 s
  MediaRecorder-style file).

## Long recordings and memory (local whisper)

The local `whisper` backend loads the whole model and the whole clip into
memory for each run. A long recording on a small host can push the machine
into swap or get the whisper process killed. To keep memory flat, the
daemon splits long audio before it reaches whisper.

Audio longer than `whisper.chunk_seconds` (default **120**, `0` disables) is
split into slices of at most that length. Each slice is transcribed in turn
and the slice texts are joined with a single space, so you still get one
transcript back. Audio that already fits in one slice is sent as is.

How the split is done:

- **PCM WAV** is split in pure Go, with no external tool.
- **Any other format** (webm/opus from the browser microphone, ogg, m4a, mp3)
  is split with `ffmpeg`. If `ffmpeg` is not on PATH the split is skipped, the
  daemon logs `[voice] chunking skipped: ...; sending the whole file`, and the
  whole file goes to whisper in one piece. That still works for short clips
  but brings back the memory problem for long ones, so install `ffmpeg`.
- A slice that fails stops the whole request. The error names the slice, for
  example `slice 3 of 5 failed`, and the cause is classified as described below.

Chunking applies only to the local `whisper` backend, including when it is the
fallback leg of the chain described above. The HTTP backends (`openai`,
`openai_compat`, `openwebui`, `ollama`) ignore it.

`whisper.chunk_seconds` must be `0` or at least `30`, because whisper's own
window is 30 s. A value from 1 to 29 sent through the API is ignored and the
old value stays.

### Memory envelope

Model size decides how much RAM a run needs. `medium` needs about 1.5 GiB for
the weights plus several GiB at peak while it decodes. `small` and `base`
need far less.

**A host with 8 GiB of RAM or less should use `small` or `base`.** On such a
host `medium` can get the whisper process killed by the kernel (cause
`killed`, below) once a long clip is under way. Shorter slices lower the
peak too, so on a tight host you can also lower `whisper.chunk_seconds`
(for example to 60); do not set it below 30.

### The five ways to set `whisper.chunk_seconds`

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `whisper:` → `chunk_seconds: 120` |
| **CLI** | `datawatch config set whisper.chunk_seconds 120`, then `datawatch reload` |
| **Web UI** | Settings → General → **Voice Input (Whisper)** card → *Longest audio slice per local whisper run* |
| **REST API** | `PUT /api/config` with `{"whisper.chunk_seconds": 120}`; `GET /api/config` returns it under `whisper.chunk_seconds` |
| **Comm channel** | `configure whisper.chunk_seconds=120` |

The value is read when the daemon builds the transcriber, so reload (or
restart) after changing it. At startup the daemon logs
`[voice] chunking: local whisper slices audio longer than 120s` when it is on.
During a long request it logs `[voice] chunked: 5 slices of <=120s`.

### When transcription fails: error causes

`POST /api/voice/transcribe` no longer returns a bare error string. A failure
is HTTP 502 with a JSON body:

```json
{
  "error": "transcribe: the whisper process was killed by SIGKILL, usually the kernel out-of-memory killer",
  "cause": "killed",
  "hint": "Use a smaller whisper.model such as small or base, or lower whisper.chunk_seconds.",
  "detail": "whisper failed: signal: killed",
  "latency_ms": 41873
}
```

`cause` is one of these values. The PWA shows a plain-language message for
each; the table gives what it means and what to do.

| `cause` | What it means | Fix |
|---|---|---|
| `timeout` | The request ran past its deadline, or the client went away. The deadline is 5 minutes, or twice the audio length when that is longer, capped at 30 minutes. | Record a shorter clip, use a smaller `whisper.model`, or lower `whisper.chunk_seconds`. |
| `killed` | The whisper process ended by SIGKILL. On Linux this is almost always the kernel out-of-memory killer; `dmesg` shows `Out of memory: Killed process`. | Use `small` or `base`, lower `whisper.chunk_seconds`, or add RAM. |
| `out_of_memory` | Whisper itself reported it ran out of memory (a Python `MemoryError`, `Cannot allocate memory`, or a GPU out-of-memory message). | Same as `killed`. |
| `ffmpeg_missing` | `ffmpeg` is not installed or not on the daemon's PATH, and whisper needs it to read the audio. | `apt install ffmpeg` (or your package manager's equivalent), then retry. |
| `backend_error` | Any other non-zero exit, or an HTTP error from a remote backend. | Read `detail` and the `[voice] transcribe failed:` log line, fix the backend, retry. |
| `unknown` | The error matched nothing above. | Read the daemon log for the full error. |

`hint` is one sentence of advice for that cause and `detail` is the raw backend
error cut to 500 bytes. A recording over the upload limit is a separate case:
HTTP 413 before any transcription starts.

### Reading the voice log lines

A failed request, and any request slower than 30 s even when it succeeds, writes
one line to the daemon log. The transcript text is never logged.

```
[voice] transcribe failed: backend=whisper bytes=1843201 audio_seconds=312.4 elapsed_ms=41873 cause=killed exit=killed err=whisper failed: signal: killed
[voice] transcribe slow: backend=whisper bytes=402112 audio_seconds=48.0 elapsed_ms=36120 cause=none exit=n/a err=
```

| Field | Meaning |
|---|---|
| `failed` / `slow` | `failed` means the request returned an error. `slow` means it succeeded but took longer than 30 s. |
| `backend` | The configured `whisper.backend`. |
| `bytes` | Size of the uploaded audio file. |
| `audio_seconds` | Length of the audio, or `unknown` when it could not be read (not a PCM WAV and no `ffprobe`). |
| `elapsed_ms` | Time spent in the transcriber only, not upload or ffmpeg transcode. |
| `cause` | One of the causes above, or `none` on a slow success. |
| `exit` | The whisper process exit status, the signal name (`killed`) when it was stopped by a signal, or `n/a` when there was no child process. |
| `ffmpeg=missing` | Present only when the transcode step could not find `ffmpeg`. |
| `err` | The first line of the error text. |

A rule of thumb: if `elapsed_ms` is much larger than `audio_seconds` times 1000,
the host is slower than realtime for this model and long clips will time out;
pick a smaller model.

### Failed recordings are kept in the PWA

When a transcription fails in the PWA (the session input bar, any other
microphone-enabled text field, and the Test transcription button in Settings),
the audio is
**kept in the browser** and a notice appears beside the input with the message
for the cause, the line "Recording kept, nothing is lost.", and two buttons:

- **Retry transcription** sends the same recording again, without re-speaking.
  The button shows "Retrying transcription…" while it runs. If it fails again
  the notice stays and you can retry as many times as you like.
- **Discard recording** throws the audio away.

The recording stays until you press one of them. It is held in the page's memory,
so reloading or closing the tab loses it; retry before you leave. A retry after
fixing the cause on the daemon (for example installing `ffmpeg` or choosing a
smaller model and reloading) works with no new recording.

## Common pitfalls

- **Whisper venv path wrong.** `datawatch voice test` returns
  "venv not found"; fix `whisper.venv_path` and reload.
- **Wrong language detection.** Set `whisper.language` explicitly if
  `auto` keeps misclassifying.
- **`openai` backend without API key in secrets.** Will fail with
  401. Confirm with `datawatch secrets get OPENAI_API_KEY`.
- **Audio too long.** Default cap is 120s. Raise `max_audio_seconds`
  for longer notes. Recordings longer than 30 s are transcribed in ≤30 s
  chunks automatically now — see "Long recordings (longer than 30 s)"
  above — so a windowed backend is no longer a failure mode. Local whisper
  also slices at `whisper.chunk_seconds`; see "Long recordings and memory".
- **Whisper process killed or out of memory.** The error cause is `killed` or
  `out_of_memory`. Use `whisper.model` `small` or `base` on hosts with 8 GiB of
  RAM or less, or lower `whisper.chunk_seconds`.
- **PWA mic permission denied.** Browser blocks mic access on
  non-HTTPS origins. Use `https://` (the daemon's default) — not
  `http://` redirect.
- **OpenWebUI returns 400 "file format not supported".** Almost always actually masks a server-side engine error. Hit the endpoint with `curl` + your API key to see the real response. On ARM/Thor: it'll be the CTranslate2/CUDA error described in the OpenWebUI section above.
- **`ffmpeg` missing.** Daemon falls back to sending the original webm/opus blob to the primary; OpenWebUI usually rejects this. Install ffmpeg (`apt install ffmpeg`) so the WAV-transcode step runs. Without it,
  long non-WAV recordings also cannot be sliced, and a request whisper cannot read
  fails with cause `ffmpeg_missing`.

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

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/comm-channels](comm-channels.md)
- [howto/chat-and-llm-quickstart](chat-and-llm-quickstart.md)
- [api/voice](../api/voice.md)
