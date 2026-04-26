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
   │  transcriber.Transcribe(ctx, path)  ── 60 s timeout            │
   │      │                                                         │
   │      ▼                                                         │
   │  internal/transcribe.WhisperCLI                                │
   │      • shells out to a venv-installed `whisper` (or            │
   │        whisper.cpp), language pinned via cfg                   │
   │      • returns plain transcript                                │
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
   │                                                                │
   └────────────────────────────┬───────────────────────────────────┘
                                │
                                ▼
   ┌─── PWA — voiceInputBtn handler ───────────────────────────────┐
   │                                                                │
   │  paste transcript into #sessionInput                           │
   │  re-enable input field                                         │
   │  user reviews and presses send                                 │
   │                                                                │
   │  (auto_exec=true variant skips the review step and dispatches  │
   │  the action straight into the session — used by the future     │
   │  hands-free mobile flow)                                       │
   │                                                                │
   └────────────────────────────────────────────────────────────────┘
```

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| 503 "voice transcription not enabled" | Whisper not configured | `datawatch setup whisper` |
| 502 "transcribe: ..." | venv missing or model not present | check `whisper --help` works under `cfg.Voice.VenvPath` |
| Transcript empty | Audio too quiet or wrong codec | check browser mime support; check whisper language config |
| 60 s timeout | Audio too long for the model | trim recording or pick a faster whisper model |

## Related

- Endpoint spec: [`docs/api/openapi.yaml` → `/api/voice/transcribe`](../api/openapi.yaml)
- Mobile surface: [`docs/api/mobile-surface.md`](../api/mobile-surface.md)
- PWA mic handler: `internal/server/web/app.js` → `toggleVoiceInput()`
- Server handler: `internal/server/voice.go` → `handleVoiceTranscribe()`
- Transcriber backend: `internal/transcribe/`
