# Voice transcription flow

End-to-end path for the PWA mic button and the mobile voice
surface. Same endpoint serves both clients.

```
   ┌─── PWA / mobile ───────────────────────────────────────────────┐
   │                                                                │
   │  user taps 🎤  ──→  navigator.mediaDevices.getUserMedia        │
   │                       (mime auto: opus/webm → webm → ogg → mp4)│
   │                       │                                        │
   │                       ▼                                        │
   │                   MediaRecorder ── start ── stop                │
   │                       │                                        │
   │                       ▼                                        │
   │                   Blob (audio/*)                               │
   │                       │                                        │
   │                       ▼                                        │
   │   POST /api/voice/transcribe                                   │
   │   multipart/form-data                                          │
   │     audio       — blob                                         │
   │     session_id  — active session (optional)                    │
   │     auto_exec   — true → run command prefix                    │
   │     ts_client   — for latency telemetry                        │
   │                                                                │
   └────────────────────────────┬───────────────────────────────────┘
                                │ HTTPS
                                ▼
   ┌─── parent daemon (handleVoiceTranscribe) ─────────────────────┐
   │                                                                │
   │  503 ──→ if SetTranscriber(nil)                                │
   │                                                                │
   │  parse multipart (max 25 MB)                                   │
   │      │                                                         │
   │      ▼                                                         │
   │  spool blob to /tmp/dw-voice-XXXX/audio.<ext>                  │
   │      │                                                         │
   │      ▼                                                         │
   │  probe audio length (PCM WAV header, else ffprobe)             │
   │      │                                                         │
   │      ▼                                                         │
   │  deadline = max(5 min, 2 × audio length), capped at 30 min     │
   │  (5 min when the length is unknown)                            │
   │      │                                                         │
   │      ▼                                                         │
   │  transcriber.Transcribe(ctx, path)                             │
   │      │                                                         │
   │      ▼                                                         │
   │  ChunkedTranscriber (local whisper only, whisper.chunk_seconds)│
   │      • audio longer than chunk_seconds (default 120; 0 = off)  │
   │        is split: PCM WAV in pure Go, other formats via ffmpeg  │
   │      • no ffmpeg → whole file goes to whisper in one piece     │
   │      • slices transcribed in order, texts joined with a space  │
   │      │                                                         │
   │      ▼                                                         │
   │  internal/transcribe.WhisperCLI (one run per slice)            │
   │      • shells out to a venv-installed `whisper` (or            │
   │        whisper.cpp), language pinned via cfg                   │
   │      • returns plain transcript                                │
   │      │                                                         │
   │      ▼                                                         │
   │  failed or slower than 30 s → one `[voice] transcribe          │
   │  failed:` / `transcribe slow:` log line                        │
   │      │                                                         │
   │      ▼                                                         │
   │  classifyVoiceAction(transcript)                               │
   │      "new: …"     → action = "new"                             │
   │      "reply: …"   → action = "reply"                           │
   │      "status …"   → action = "status"                          │
   │      else         → action = "none"                            │
   │                                                                │
   │  rm -rf /tmp/dw-voice-XXXX                                     │
   │                                                                │
   │  ◀── 200 OK { transcript, confidence, action,                  │
   │              session_id, latency_ms }                          │
   │  ◀── 502 { error, cause, hint, detail, latency_ms }  on error  │
   │                                                                │
   └────────────────────────────┬───────────────────────────────────┘
                                │
                                ▼
   ┌─── PWA — voiceInputBtn handler ───────────────────────────────┐
   │                                                                │
   │  paste transcript into #sessionInput                           │
   │  re-enable input field                                         │
   │  on failure: keep the recording, show the cause message with   │
   │  Retry and Discard (the audio stays until you pick one)        │
   │  user reviews and presses send                                 │
   │                                                                │
   │  (auto_exec=true variant skips the review step and dispatches  │
   │  the action straight into the session — used by the future     │
   │  hands-free mobile flow)                                       │
   │                                                                │
   └────────────────────────────────────────────────────────────────┘
```

## Failure modes

A failed transcription is HTTP 502 with a JSON body:

```json
{
  "error": "transcribe: the whisper process was killed by SIGKILL, usually the kernel out-of-memory killer",
  "cause": "killed",
  "hint": "Use a smaller whisper.model such as small or base, or lower whisper.chunk_seconds.",
  "detail": "whisper failed: signal: killed",
  "latency_ms": 41873
}
```

`cause` is one of `timeout`, `killed`, `out_of_memory`, `ffmpeg_missing`,
`backend_error` or `unknown`. `detail` is the raw error cut to 500 bytes.
The PWA maps the cause to a localized message and keeps the recording so you
can retry it.

| Symptom | Likely cause | Fix |
|---|---|---|
| 503 "voice transcription not enabled" | Whisper not configured | `datawatch setup whisper` |
| 413 | Upload larger than 25 MB | Record a shorter clip |
| 502, `cause` `timeout` | Ran past the deadline (5 minutes, or twice the audio length, capped at 30 minutes) | Trim the recording, pick a smaller whisper model, or lower `whisper.chunk_seconds` |
| 502, `cause` `killed` | whisper ended by SIGKILL, usually the kernel out-of-memory killer | Use `small` or `base` on hosts with 8 GiB of RAM or less; lower `whisper.chunk_seconds` |
| 502, `cause` `out_of_memory` | whisper reported it ran out of memory | Same as `killed` |
| 502, `cause` `ffmpeg_missing` | `ffmpeg` is not installed or not on PATH | Install `ffmpeg` on the daemon host |
| 502, `cause` `backend_error` | Other non-zero exit, or an HTTP error from the backend; venv missing or model not present | Read `detail` and the log line; check `whisper --help` works under `cfg.Voice.VenvPath` |
| Transcript empty | Audio too quiet or wrong codec | check browser mime support; check whisper language config |

Every failure, and every success that took longer than 30 s, also writes one
daemon log line with the backend, upload size, audio length, elapsed time,
cause and exit status; see
[`howto/voice-input.md`](../howto/voice-input.md#reading-the-voice-log-lines).

## Related

- Endpoint spec: [`docs/api/openapi.yaml` → `/api/voice/transcribe`](../api/openapi.yaml)
- Mobile surface: [`docs/api/mobile-surface.md`](../api/mobile-surface.md)
- PWA mic handler: `internal/server/web/app.js` → `toggleVoiceInput()`
- Server handler: `internal/server/voice.go` → `handleVoiceTranscribe()`
- Transcriber backend: `internal/transcribe/`
- Chunking: `internal/transcribe/chunked.go` → `ChunkedTranscriber`
- Error causes: `internal/transcribe/errors.go` → `ClassifyError()`
