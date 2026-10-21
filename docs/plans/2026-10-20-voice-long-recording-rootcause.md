# Voice transcription fails for recordings longer than Whisper's 30 s window

Date: 2026-10-20
Task: root-cause voice transcription of recordings >30 s (5 min and 10 min included)
Status: reproduced; ranked root causes below

## Reproduction

All reproduction lives in two test files (both green — see Evidence):

1. `internal/transcribe/long_audio_test.go`
   - `TestLongAudio_OpenAICompat_WindowedBackend` — 29 s / 31 s / 65 s / 5 min / 10 min
     WAVs against a fake windowed OpenAI-compatible `/v1/audio/transcriptions`
     server (30 s window, like a bare `whisper.cpp`/`faster-whisper` server
     with no long-audio split).
   - `TestLongAudio_OpenAICompat_BodySizeAndRetry` — 5 min (9.6 MB) WAV against
     a flaky fake (two 503s, then 200); exercises size + retry, and verifies
     the FULL body is uploaded (not a 30-s fragment).
   - `TestLongAudio_OpenAICompat_TimeoutHonoursContext` — 10 min WAV with a
     slow server; context/client-5-min timeout honoured.
   - `TestLongAudio_LocalWhisper_65sCoverage` — 65 s 440 Hz tone through the
     REAL venv `whisper` (openai-whisper, `tiny`) — skips without venv/ffmpeg.
   - `TestLongAudio_FfmpegTranscode_WebmNoDuration` — MediaRecorder-style webm
     (pipelined, **no** EBML `Duration` metadata) run through ffmpeg, exactly
     as `internal/server/voice.go` does.

2. `internal/server/long_audio_test.go`
   - `TestVoice_LongUpload_25MBCapBoundary` — uploads 31 s / 65 s / 5 min /
     10 min WAVs through the real `handleVoiceTranscribe` with a fake
     transcriber; computes the cap boundary.
   - `TestVoice_LongUpload_SlowBody_ReadTimeout` — a real `http.Server` with
     `ReadTimeout` = 15 s (the same constant in `internal/server/server.go`)
     + a client that takes 17 s to deliver a 2 MB body.

Run:

```bash
go test ./internal/transcribe/ ./internal/server/ -run 'TestLongAudio|TestVoice_LongUpload' -v
```

Layer-by-layer result (all evidence in Evidence below):

| Layer                                             | 31 s | 65 s  | 5 min | 10 min |
| ------------------------------------------------- | ---- | ----- | ----- | ------ |
| (a) handler 25 MB multipart cap (`voice.go`)      | OK   | OK    | OK    | OK     |
| (a) handler `ReadTimeout` 15 s (server.go) slow up| —    | **FAIL** (upload >15 s) | **FAIL** (slow link) | **FAIL** (slow link) |
| (b) `openai_compat` vs windowed backend           | 400  | 400   | 400   | 400    |
| (b) `openai_compat` size / 5-min timeout / retry  | OK   | OK    | OK    | OK     |
| (c) local `whisper` venv                           | OK   | OK    | OK    | OK     |
| (d) `ffmpeg` webm→16 kHz WAV (no duration)        | OK   | OK    | OK    | OK     |

## Root cause

**Primary — `ReadTimeout: 15 s` applies to the whole voice-upload request.**
`internal/server/server.go` sets `ReadTimeout: 15*time.Second` server-wide. For
`POST /api/voice/transcribe` that bounds reading the entire multipart body
(`internal/server/voice.go` → `r.ParseMultipartForm`). Any recording whose body
takes >15 s to arrive — i.e. a long recording on a slow/throttled network, or
a large WAV-style file over a modest link — is aborted mid-read and the client
sees `400` `bad multipart: multipart: NextPart: read tcp … i/o timeout`.
Nothing in the code path has a wall-clock limit tied to how long the *upload*
takes; `WriteTimeout` is 0 (unbounded), so only the 15 s read timeout can kill
a request that is still transferring its body. Longer recording ⇒ more bytes ⇒
more time ⇒ more likely to cross 15 s. This is the only deterministic
length-correlated, wall-clock failure in the stack.

**Secondary — `openai_compat` uploads the WHOLE file and never chunks.**
`internal/transcribe/openai_compat.go` sends one multipart body per call and
has no audio-splitting (unlike (d) `ffmpeg` and (c) local `whisper`, which
window long audio internally and both PASS). If the operator's backend is a
bare windowed engine (e.g. a `whisper.cpp`/`faster-whisper` server with no
long-audio split, or a proxy that caps request size/duration), every >30 s
recording hard-fails with `400` and there is no retry path — retry only fires
on 503/504, and 400 is not retried. The `5 min` HTTP client timeout
(`openai_compat.go` `http.Client{Timeout: 5*time.Minute}`) is the only
duration ceiling and is not itself a problem.

**Not the cause (eliminated):**
- `voice.go` 25 MB cap: 16 kHz mono WAV crosses it at ~13.6 min; typical
  `webm/opus@64 kbps` at ~45 min. 5–10 min recordings are well under.
- Local `whisper` venv (c) and `ffmpeg` transcoding (d): both handle >30 s
  and even webm-with-no-duration fine.

## Evidence

Exact failing/succeeding lines from `go test -v` output (2026-10-20):

Handler-layer, slow upload (the PRIMARY failure) —
`internal/server/long_audio_test.go:211`:

```
REPRODUCED after 15.014505259s: status=400
body="bad multipart: multipart: NextPart: read tcp 127.0.0.1:42737->127.0.0.1:33130: i/o timeout\n"
```

- A ~2 MB body that the client deliberately delivers over 17 s is cut at the
  15 s `ReadTimeout`. Same failure shape happens for a genuinely slow upload
  of a long recording.
- `voice.go` 25 MB cap is cleared for 5 & 10 min (`TestVoice_LongUpload_25MBCapBoundary`
  all PASS): `600 s 16 kHz mono WAV = 19.20 MB (cap 26.21 MB)` → `OK in 52 ms`.
  Cap is reached at `16 kHz mono WAV ≈ 819 s (13.6 min); webm/opus@64kps ≈ 3277 s (54.6 min)`.

`openai_compat` against a windowed backend (the SECONDARY failure) —
`internal/transcribe/long_audio_test.go` (`TestLongAudio_OpenAICompat_WindowedBackend`):

```
31s:  fake received 496653 bytes, wav duration 31.0s  — error: transcribe(openai_compat): HTTP 400: {"error":"audio duration 31.0s exceeds maximum window 30s"}
65s:  fake received 1040653 bytes, wav duration 65.0s — error: transcribe(openai_compat): HTTP 400: …
300s: fake received 4800654 bytes, wav duration 300.0s — error: transcribe(openai_compat): HTTP 400: …
600s: fake received 9600654 bytes, wav duration 600.0s — error: transcribe(openai_compat): HTTP 400: …
```

- 29 s (inside the window) → `text=ok`. The client uploads the FULL 5 min
  (9.6 MB) body, gets a bare `400`, and `openai_compat.go` retries only on
  503/504 — so a `400` from a windowed backend is terminal (no chunking, no
  retry). This is the only layer that *cannot* represent >30 s content.

Local `whisper` (c) PASSES — `internal/transcribe/long_audio_test.go`
(`TestLongAudio_LocalWhisper_65sCoverage`):

```
local whisper on 65 s tone: text="" (local layer handles >30 s windows fine)   [9.15s]
```

`ffmpeg` webm (d) PASSES — (`TestLongAudio_FfmpegTranscode_WebmNoDuration`):

```
MediaRecorder-style webm (pipelined, no duration metadata): 685575 bytes
ffprobe format=duration (N/A = missing, as in MediaRecorder output): <empty>
transcoded wav: duration 65.00s, 2080078 bytes — ffmpeg layer OK for >30 s
```

## Fix outline

Ranked. Primary first.

1. **Do not let a long/slow upload trip the server-wide `ReadTimeout`.**
   - `internal/server/server.go` currently applies one `ReadTimeout` to all
     routes. For `POST /api/voice/transcribe` the body is the recording, so the
     real limit is *upload bandwidth*, not processing. Options (pick one):
     - Route the voice endpoint through a handler that reads the body with a
       generous per-request deadline (e.g. body read bounded by `context`
       deadline derived from content size / `WriteTimeout`-style budget), leaving
       `ReadTimeout` only to cover header + start of body. Concretely, in
       `internal/server/voice.go` replace `r.ParseMultipartForm(25<<20)` with a
       bounded stream (e.g. `http.MaxBytesReader` + `multipart.Reader` reading
       to `ioutil.TempFile`) and relax `ReadTimeout`/`ReadHeaderTimeout` for this
       route to `max(15s, size/bandwidth)`.
   - Minimal: raise `ReadTimeout` to cover realistic long-audio uploads (e.g.
     120–180 s) and add `http.MaxBytesReader(w, r.Body, 25<<20)` in `voice.go`
     so a slow but valid upload is accepted while the 25 MB hard cap still holds.

2. **Make `openai_compat` able to express >30 s content to a windowed backend.**
   - `internal/transcribe/openai_compat.go`: if the configured backend is a
     windowed engine, either (a) split the audio into ≤ `window` chunks on the
     client and concatenate transcripts, or (b) detect the window on the first
     `400`/`422` and split-and-then-retry each chunk. Keep the whole-file
     single request for full-length backends (OpenAI, OpenWebUI).
   - Alternatively route long audio to the local venv chain
     (`internal/transcribe/chain.go`) which windows correctly.

3. **Hardening (not the root cause, but adjacent):**
   - `voice.go`: the 25 MB cap and the "silently skip ffmpeg transcode if the
     upload is already `.wav`" path (`outPath == uploadPath` → ffmpeg
     "cannot edit existing files in-place", exit 234, then "using original .wav")
     should be logged, and the cap raised (or stream-to-disk) once long
     recordings are expected. Consider chunked upload.
   - `server.go`: `ReadHeaderTimeout` 10 s / `IdleTimeout` 60 s left as-is;
     add a `MaxBytesReader` guard independent of `ParseMultipartForm`'s
     in-memory limit.

## Tests that must flip (for the next task)

Marked with `EXPECTED FAILURE` comments (currently green because they *assert
the broken behaviour*) and should be rewritten once the fix lands:

- `internal/server/long_audio_test.go`
  `TestVoice_LongUpload_SlowBody_ReadTimeout` — currently asserts the 15 s
  `ReadTimeout` kills a slow upload. After fix 1, a slow-but-valid upload must
  complete and return a transcript.
- `internal/transcribe/long_audio_test.go`
  `TestLongAudio_OpenAICompat_WindowedBackend` (31 s / 65 s / 5 min / 10 min
  subtests) — currently asserts `400`. After fix 2, these must return a
  non-empty transcript (via chunking, or via routing to the local venv).

Files referenced: `internal/server/voice.go`, `internal/server/server.go`,
`internal/transcribe/openai_compat.go`, `internal/transcribe/transcribe.go`,
`internal/transcribe/chain.go`, `internal/transcribe/long_audio_test.go`,
`internal/server/long_audio_test.go`.
