---
title: Streaming long-form voice input — near-live transcription with quick send
date: 2026-10-20
status: design (Planned)
baseline: docs/plans/2026-10-20-voice-long-recording-rootcause.md (shipped fix)
surfaces: REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone
---

# Streaming long-form voice input — near-live transcription with quick send

## Problem and goals

Today, voice input in this codebase is fully request/response: the PWA records the
entire utterance with `MediaRecorder` (`internal/server/web/app.js`), the operator
taps **Send** in the recording overlay, and only then does the blob travel to
`POST /api/voice/transcribe` in `internal/server/voice.go`. There is no visible text
while the person is still speaking, no way to send the words that are already stable
before the final word finishes decoding, and a long recording occupies the whole
pipeline: the 180 s upload read timeout and the 25 MB cap in
`internal/server/voice.go` together describe a "record everything, upload everything,
wait" model that gets worse as the utterance gets longer. For long-form dictation — a
five-minute status note, an hour-long meeting remark — the operator should see their
own words form in the UI within one or two seconds of speaking, be able to fire the
already-stable prefix to the destination (chat, command box, message) immediately, and
keep dictating or edit what was sent. This doc designs that: a streaming decode
pipeline, a chunk/stream transport, a partial/final/revise transcript protocol, quick
send, backend options, failure handling, and surface parity per AGENT.md. Goals, in
priority order:

1. **Near-live text.** First visible words within ≤ ~3 s of start on a reasonably
   sized local model; steady-state lag ≤ ~2 s with a rolling-window stable-prefix
   decoder.
2. **Quick send.** Send (or prefill and one-tap send) the stable prefix at any point
   without waiting for the final transcript or for the backend to finish.
3. **Long-form without the upload wall.** Multi-minute to multi-hour dictation that
   never re-enters the 25 MB / 180 s whole-blob path and never requires the final
   30 s-windowed whole-file transcription as a precondition.
4. **Edit and cancel.** Every dictation surface keeps the existing Cancel/Send
   semantics from the BL326 overlay in `internal/server/web/app.js`; the stable text
   must always be editable before it leaves the machine.
5. **Parity surface.** PWA, Android, iPhone, comm channels, REST, MCP, CLI, and
   YAML/config all behave consistently per the parity rules in AGENT.md; this plan's
   `## Parity surface` section lists intentional exclusions with reasons.
6. **No regressions.** The already-shipped long-recording fix (whole-upload-then-split
   fallback in `internal/transcribe/openai_compat.go` + `internal/transcribe/split.go`)
   remains the guaranteed-correct path; streaming is additive and strictly opt-in.

Non-goals: speaker identification, real-time translation, punctuation-quality
guarantees beyond what the chosen model gives, and sub-second lip-sync-grade latency.

## Current architecture

The single entry point is `POST /api/voice/transcribe` in
`internal/server/voice.go`: the handler enforces the 25 MB multipart cap
(`maxVoiceUploadBytes = 25 * 1024 * 1024`) and 180 s body-read timeout, runs the
payload through `ffmpeg` to 16 kHz mono WAV, then calls a `transcribeSurface`
implementation. That surface is either a direct `Transcriber`
(`internal/transcribe/transcribe.go`) or the v7.0.0-alpha.14
`ChainedTranscriber` (`internal/transcribe/chain.go`), which tries an
HTTP-shape primary (`openai`, `openai_compat`, `openwebui`, `ollama` — see
`internal/transcribe/openai_compat.go`) and, on failure or fast-fail signatures,
falls back to the local Python-venv `whisper` secondary. Long-audio correctness is
handled by splitting the WAV into ≤30 s silence-aware windows only when the primary
refuses the full file (`internal/transcribe/split.go`, `splitLongAudio`, pure-Go
PCM split with an ffmpeg fallback), joining the chunk texts without duplication.

The PWA side (`internal/server/web/app.js`, `toggleVoiceInput` and
`startGenericVoiceInput`) uses `getUserMedia` + `MediaRecorder` (webm/opus or
mp4), a five-bar waveform overlay with **Cancel**/**Send** (`BL326`), and on
**Send** posts the single blob and blocks the input field ("Transcribing…") until the
text is appended to the composer. Mobile (Android/iOS) and comm channels (Signal,
Telegram, Slack, Matrix — routed through `internal/router/router.go`) all reuse the
same transcriber path; the operator-facing behavior summary is in
`docs/howto/voice-input.md`.

Existing infrastructure this design reuses instead of inventing:

- **SSE hub.** `internal/server/sse.go` gives a `SSEHub` with per-topic subscriber
  sets, monotonic per-topic event IDs (so `Last-Event-ID`-style resume detection is
  possible), a non-blocking publish path, and `text/event-stream` framing. The
  project already ships SSE event endpoints (e.g.
  `GET /api/council/runs/{id}/events`, see `docs/api/council.md` referenced from
  `internal/server/sse.go`), so streaming-UI plumbing is an established pattern.
- **202 Accepted + polling.** Long-running operations elsewhere in the daemon use
  the "accept, then poll status" contract (see `internal/server/autonomous_decompose.go`
  and the BL328 polling fallback around `internal/server/autonomous.go`); that's the
  natural fallback transport for clients (comm channels, CLI, mobile on restrictive
  networks) where a persistent SSE stream is awkward.
- **Silence detection.** `internal/transcribe/split.go` already finds near-silence
  cut points with a ~1 % full-scale threshold and a silence-target search; the
  streaming pipeline's VAD can share that code rather than growing a second
  implementation.
- **Chain fallback + config.** `internal/config/config.go` (`WhisperConfig` struct,
  `whisper.*` YAML block) and `docs/config-reference.yaml` are where new
  `whisper.streaming.*` keys land.

In short: everything in this design slots into existing seams (SSE hub, chain,
config, parity rules) rather than standing up a new server.

## Whisper window constraint

Whisper-family models (the venv `openai-whisper` in
`internal/transcribe/transcribe.go`, and windowed OpenAI-compatible servers behind
`internal/transcribe/openai_compat.go`) are trained and served in a hard **30 s
window**. Two consequences dominate this design:

1. **Decoding is chunk-by-chunk with left context.** Each 30 s (or shorter) window is
   decoded using the *previous* window's transcript as prompt text. Within a single
   in-flight decode, the **tail** of the window is the least stable — the model
   hasn't "heard" the continuation yet, so the last one or two words routinely change
   when the next window is decoded. The head of a completed window is the most
   stable. This asymmetry is exactly what a stable-prefix protocol can exploit:
   commit the head, keep the tail tentative.
2. **Duration cap.** Windowed backends (bare `whisper.cpp` server, faster-whisper
   without long-audio support, duration-capping proxies) refuse > 30 s with HTTP 400.
   The shipped fix (`internal/transcribe/split.go`, tested in
   `internal/transcribe/long_audio_test.go` and `internal/server/long_audio_test.go`)
   sidesteps this for *finished* recordings by splitting into ≤30 s silence-aware
   windows and joining texts. Note deliberately **no overlap** in that path — overlap
   would duplicate words. That constraint does **not** apply to streaming: in a live
   pipeline the overlap is *required* (each new window re-decodes the trailing few
   seconds of already-accepted audio), and the duplication is removed by the
   stabilization/commit logic below instead of by avoiding overlap.

A further constraint: Whisper's per-window decode latency is proportional to window
length and model size (a few hundred ms for `base` on a modern CPU per 5 s of audio,
several seconds per 30 s for `medium`). A "decode the whole 30 s window" streaming
policy would therefore lag the speaker by up to 30 s + decode time — unacceptable.
Hence the rolling short-window policy in the next section: decode *short* trailing
windows repeatedly, and let the left-context trick (feeding the last committed text
as the prompt) keep accuracy close to full-window Whisper.

The sibling root-cause analysis
(`docs/plans/2026-10-20-voice-long-recording-rootcause.md`) documents the remaining
transport fragilities — the server-wide 15 s read-timeout interaction with slow
uploads and the absence of any resumable stream — which this design treats as the
motivation for moving audio off the single big-POST path entirely.

## Chunking and windowing

The streaming pipeline is a moving window over a bounded audio ring buffer, with the
decoder always working on the *tail* of the buffer. All times below are defaults;
every parameter is config-gated (see Configuration).

**Client → server audio framing.** The PWA already receives discrete
`ondataavailable` blobs from `MediaRecorder` (`internal/server/web/app.js`); it will
POST those blobs (or, if we add a Web Audio `ScriptProcessor`/`AudioWorklet` tap,
fixed 500 ms PCM-Opus frames) to a new chunk endpoint as they arrive, carrying a
monotonic per-stream chunk sequence number. Default frame size **5 s** (matches the
`chunk_seconds` budget and is small enough that a dropped frame costs ≤5 s of audio,
re-fetchable by the server-side ring buffer, which holds the last
`window_seconds + overlap_seconds` ≈ 15–20 s by default). Frames are compressed
(Opus) to keep bandwidth low even for long streams — 5 s of Opus audio is on the
order of 40–80 KB, versus a 5 s WAV slice that would be ~160 KB, so an hour of audio
streams in a few MB, never re-tripping the 25 MB `maxVoiceUploadBytes` cap in
`internal/server/voice.go` that the current design's ceiling is designed around.

**Decode windowing.** The server keeps a ring buffer of accepted PCM (16 kHz mono,
the same canonical format the venv Whisper transcriber expects, per
`internal/transcribe/transcribe.go`). On every new audio frame it:

1. appends to the ring (evicting anything older than `window_seconds` + `overlap_seconds`);
2. builds a decode window = the last `window_seconds` (default **12 s**) of audio;
3. decodes that window with the **last committed text as prompt context** (for
   Whisper-family backends this is the `initial_prompt`/left-context argument; for a
   streaming TDT backend the model is already streaming and this step is intrinsic);
4. runs the **stabilization** step below to decide what becomes committed text;
5. emits `partial`/`commit`/`revise` events (see the protocol section).

**Overlapping decode (rolling windows).** Consecutive decode windows overlap by
`overlap_seconds` (default **4 s**): window *n* covers seconds *[t₂₅, t₃₇]*, window
*n*+1 covers seconds *[t₃₉, t₅₁]*, etc. (defaults: 12 s window, 4 s overlap → 8 s
stride). Every region of audio is therefore decoded in **two consecutive windows**,
which is precisely the property the stabilization rule uses: a word that was
*present* in the same span in two successive decodes has survived the left-context
shift and is safe to commit. This is the deliberate, controlled counterpart to the
"no overlap" rule in the offline split path (`internal/transcribe/split.go`) —
there overlap would only duplicate text, here it is the stability mechanism itself.

**Why 12 s / 4 s and not shorter.** Going below ~8 s of window loses too much
left-context for Whisper to stay accurate on long utterances (numbers, proper nouns,
code-like tokens drift); going above ~16 s of window pushes first-word latency and
steady-state lag past the 3 s / 2 s budgets. Shorter is possible on fast TDT backends
(see Backend options) via the same config keys. **Why not 30 s.** A 30 s decode
window on a local `base`/`small` model is 1–3 s of wall-clock per window; at an 8 s
stride that's fine on GPU but borderline on CPU, and — worse — it doubles the
lag-before-stable, defeating "near-live".

**Stabilization rule.** After each decode:

- Let `prev_stable` be the committed text up to now, and `cur` the just-decoded text
  for the window (which re-encodes the overlap region plus new audio).
- Find the longest prefix of `cur` that, when appended to the already-committed text
  (i.e., the new words in the overlap-and-beyond), is **token-identical** across the
  last `stabilize_decodes` (default **2**, i.e., the previous and current decode) for
  the overlapping region.
- Words inside the overlap region that matched in both decodes → **commit** (append
  to the committed transcript, emit a `commit` event with the new stable substring
  and the updated full stable text).
- Words that appeared *only* in the current decode (the true tail) → **partial**
  (emit a `partial` event; render greyed in the UI).
- If a previously committed word changes (rare; happens when the tail of the next
  window re-orders a homophone or a number), emit a **`revise`** event carrying the
  corrected span and the offset it replaces (see Protocol for the exact envelope).

**VAD (voice activity detection).** Between phrases the decoder must not burn cycles
re-decoding silence (and must not emit a fresh `partial` every time it re-scores the
overlap). The server tracks a simple energy-based activity flag (RMS over a 200 ms
hop on the same 16 kHz PCM that `internal/transcribe/split.go`'s silence-threshold
code already operates on, ~1 % full-scale) and only triggers a decode window when
the ring's new-audio tail has been voiced for at least one `vad_min_voice_ms`
(default ~250 ms). During silence: no new decodes, the most recent `partial` is
retained and marked `stale` after `partial_stale_ms` (default ~1500 ms) so the UI can
dim it, and after `idle_timeout_seconds` (default **20 s**) of no voice the stream
auto-closes (or, in "keep-alive" mode, the stream sleeps but the decoder is parked —
useful for hour-long dictation where the speaker pauses). VAD is a coarse gate; the
stabilization rule above is the real correctness mechanism, so VAD false-negatives
(costs one extra decode later) are harmless and VAD false-positives (decoding into
silence) are bounded by the window-length cap on compute.

**Chunking trade-offs summary.** Every knob is a dial between three forces —
*latency* (window length and stride), *stability* (overlap and
`stabilize_decodes`), and *throughput* (decode cost per second of audio, which is
`1 / stride` decodes each of `window_seconds` length). The defaults above
(12 s / 4 s overlap / 200 ms energy VAD / 2-decode stabilization) were chosen so
that on an Apple-silicon-class CPU or any modern laptop with the `base` model,
steady-state first-word lag is ≈ 1.5–2.5 s, which meets the near-live goal without
assuming GPU. A `tiny` model halves decode cost at a small accuracy loss;
`small`/`medium` roughly double/4× decode cost and push the first-word budget to
~4 s on CPU — both supported but not the defaults.

## Transport

The design keeps **two independent channels** because they have opposite
characteristics: audio is upstream (client → server, small frames) and transcript
events are downstream (server → client, tiny JSON). Bundling them into one
WebSocket is possible but not the recommendation, and the project already has
working patterns for each direction separately.

**Recommended: SSE for transcript events + chunked POST for audio.**

- **Downstream (transcript events): SSE** via the existing `SSEHub` in
  `internal/server/sse.go`. A new endpoint
  `GET /api/voice/stream/{stream_id}/events` returns `text/event-stream` with the
  partial / commit / revise / error / close events (see Protocol), each carrying a
  monotonically increasing per-topic `id:` (the hub already assigns these
  per-topic). The client can (a) reconnect transparently on network blips, (b)
  resume with `Last-Event-ID` so it can detect gaps (id jump ⇒ re-fetch committed
  state via `GET /api/voice/stream/{stream_id}` — a cheap REST endpoint that returns
  the latest stable + partial snapshot), and (c) fall back to 1 Hz polling of that
  snapshot endpoint if SSE is unavailable (proxy stripping, restrictive mobile
  carrier, or the client simply prefers it). The hub's non-blocking publish
  (`internal/server/sse.go`) means a slow or dead subscriber can't stall the
  decoder, and the 30 s SSE keep-alive comment already covers idle-link probing.
- **Upstream (audio):** a short-lived `POST /api/voice/stream/{stream_id}/frames`
  per `MediaRecorder` blob (or a batched variant — multiple frames per POST is
  allowed and cheaper over chatty links; the server enforces the same total-size
  sanity cap as `internal/server/voice.go` does, just per-frame instead of
  per-utterance, so a dropped or slow frame is at most a few hundred KB, nowhere
  near the 25 MB ceiling). This is *not* one long-lived upload; it behaves like the
  already-shown `internal/server/voice.go` handler except per-frame, reusing the
  same auth, the same `ffmpeg`/16 kHz mono normalization, and the same error
  shapes so existing clients keep working.
- **Why not a single WebSocket for both directions.** The project already uses
  WebSockets for session I/O (`internal/server/ws.go`, `ws_session_stream.go`) and
  SSE for long-lived event feeds; adding a *third* bidirectional WebSocket protocol
  just for voice would be a new surface to test, auth, and document with no
  advantage, since (a) audio is one-directional and small (frames are
  self-describing POST bodies, no framing negotiation needed), (b) transcript
  events are one-directional and tiny (SSE + the hub's `Last-Event-ID` gap-detection
  is enough), and (c) the existing 202 / polling pattern in
  `internal/server/autonomous_decompose.go` (and the BL328 fallback noted in
  `internal/server/autonomous.go`) already covers the "persistent stream is
  inconvenient" case for comm-channel and CLI clients. A WebSocket would be the
  right call if we needed bidirectional *control* (e.g., the client driving
  decode-window size live); today that's overkill — the config keys drive it.
- **Fallback / degraded transport (project's 202 + polling pattern).** If SSE is
  unavailable, the client falls back to polling
  `GET /api/voice/stream/{stream_id}` (the snapsthot endpoint above); the response
  carries `last_event_id` so the client detects new events the same way as with SSE
  — this is the same contract as the 202-Accepted-then-poll pattern the daemon
  already uses for long operations (see `internal/server/autonomous_decompose.go`
  and the BL328 polling fallback in `internal/server/autonomous.go`). For comm
  channels (Signal/Telegram/Slack/Matrix) the "streaming UI" is naturally
  *absent* — the operator just sees the final text when the speaker stops or taps
  Send — so comm channels only need the REST endpoints
  (`open`, `frames`, `send`, `close`) plus the existing final-transcription
  fallback, not a live SSE subscription.

**Reconnect / backoff policy.** On a dropped SSE stream the client reconnects with
exponential backoff: **1 s, 2 s, 4 s, 8 s, 16 s, 30 s (cap)**, each retry with
`Last-Event-ID` set to the last id consumed, and on *any* successful reconnect it
also issues one `GET /api/voice/stream/{stream_id}` to reconcile the committed
snapshot (guards against a gap that `Last-Event-ID` alone would reveal only as an
id jump). Audio-frame POSTs are retried at the client with the same backoff, but
bounded by the ring-buffer window: if the gap since the last successfully
acknowledged frame exceeds `window_seconds + overlap_seconds`, the server signals
`event: gap` and the client re-fetches the tail of the stream state
(`GET /api/voice/stream/{stream_id}`) rather than attempting to re-send an unbounded
backlog — this is the streaming analogue of the existing "re-fetch state" step the
daemon uses elsewhere when a long op's status endpoint goes stale. The backoff and
cap are config-gated (`voice.stream.reconnect_backoff_ms`, `voice.stream.reconnect_max_ms`).

**Protocol shape.** Every downstream event is a small JSON envelope with a `type`
field (`partial` | `commit` | `revise` | `error` | `close`), a `seq` (per-stream
monotonic integer, mirroring the SSE hub's per-topic id), a `ts` (server
milliseconds), and the payload specific to the type. See the next section for
per-type required fields.

## Partial and final transcript protocol

The stream state machine, in the server's words:

```
OPEN → RUNNING ⇄ PARTIAL_IDLE* → (on commit) RUNNING → … → CLOSED
            * PARTIAL_IDLE = recent partial present, no new audio for a while
CLOSED can be reached from RUNNING (operator Send/Cancel, idle timeout, error,
       or server restart of the stream).
```

State transitions are observable via the event types; the snapshot REST endpoint
always reflects the current state regardless of transition path.

**Event envelope (all types).**

| Field | Type | Present on | Notes |
|---|---|---|---|
| `seq` | int | all | Per-stream monotonic; matches the SSE hub's per-topic id so `Last-Event-ID` resumes exactly. |
| `ts` | int (ms) | all | Server receive time of the triggering audio frame (for `partial`/`commit`/`revise`) or event time (for `error`/`close`). |
| `chunk_id` | string | `partial`, `commit`, `revise` | Identifier of the audio chunk(s) that produced this update; lets UI correlate a partial with the specific words it's derived from. |
| `type` | enum | all | See below. |

**`type = "partial"`** — tentative text the decoder produced for the current
window's tail. This is *not* committed; the UI renders it dimmed. Fields:
`text` (the full tentative tail, since the last commit), `stable_len` (int, how
many leading chars of `text` were already stable in the previous decode — lets a
UI diff-render), `confidence` (0–1, optional; Whisper-family backends don't emit
this, TDT backends may, UI shows a subtle indicator when < 0.6 to match the
existing "low confidence" UX in `docs/howto/voice-input.md`).

**`type = "commit"`** — one or more words were stable across
`stabilize_decodes` consecutive decodes and are now part of the committed
transcript. Fields: `text` (the newly committed substring — *not* the full
transcript, just the delta), `full_stable` (the full committed transcript up to
and including this commit; the server caps this at e.g. 64 KB to keep the SSE
frame small — the authoritative copy is always fetchable from the snapshot
endpoint or by accumulating deltas client-side), `stable_start` (char offset in the
full stream transcript where this commit's delta begins), `stable_end`. The client
appends `text` to its running committed buffer; it does *not* need to re-fetch
`full_stable` unless it detected a gap (see reconnection policy).

**`type = "revise"`** — a previously committed substring changed (rare;
happens when a homophone or a number was mis-resolved and the next window's
left-context fixes it). Fields: `replace_start`, `replace_end` (char offsets in the
pre-revision full transcript), `old_text` (the substring that was removed),
`new_text` (the replacement). The client applies a surgical
`splice(full, replace_start, replace_end, new_text)` and re-renders the affected
region; it does *not* re-fetch the whole stream. Offsets are stable across the
stream's life unless a `revise` changes them, in which case the server emits a
subsequent `commit` (or `revise` chain) that re-bases offsets — in practice
revisions are rare enough (typically < 1 per minute on clean audio) that the
client can simply rebuild its buffer from the snapshot endpoint if it ever loses
sync, rather than track a revision history.

**`type = "error"`** — non-fatal per-chunk failure (e.g., one decode window failed
on the primary and the chain fell back to the venv Whisper secondary
(`internal/transcribe/chain.go`), the decoder timed out, a single frame was
malformed). Fields: `code` (stable machine-readable string, e.g.
`E_PRIMARY_FALLBACK`, `E_DECODE_TIMEOUT`, `E_FRAME_MALFORMED`), `detail`
(human-readable, may be truncated), `retried` (bool), `stream_still_open` (bool —
if false this is effectively an `error` followed immediately by a `close`). The
protocol guarantees an `error` does *not* silently commit partial text: the
stream's committed buffer is only ever updated by `commit`/`revise` events, and
an `error` event leaves the committed buffer exactly as it was before the failing
decode window — the next successful decode after the error re-decodes the overlap
region and re-emits `commit`/`revise` as appropriate, so the operator never sees a
silent gap in the committed text without being told.

**`type = "close"`** — terminal; no further events. Fields: `reason` (one of
`operator_send`, `operator_cancel`, `idle_timeout`, `max_duration`, `server_restarted`,
`fatal_error`), `final_stable` (the full committed transcript at close — the
authoritative text that a "quick send" would have used, and the fallback a comm
channel would post as the final message), `final_partial` (the last uncommitted
tail, for UIs that want to present "you were about to say …"), `audio_dur_s` (total
seconds of audio accepted into the ring), `decode_count` (total decode windows run —
useful telemetry). After `close` the server keeps the transcript
`transcript_retain_seconds` (default **300 s**) for the snapshot endpoint, then
drops it; it does *not* retain the raw audio (privacy — matching the existing
"transcribe and drop the source" posture the howto implies: the source blob in
`internal/server/voice.go` is transcode'd, transcribed, and never written to disk,
and this streaming path keeps the same property).

**Snapshot REST endpoint.** `GET /api/voice/stream/{stream_id}` returns, in one
call: current state, `last_seq`, `committed` (full text), `partial` (current
tentative tail, if any), `open` (bool), `audio_dur_s`, `backend` (which transcriber
leg is producing events — see Failure modes). This endpoint doubles as the
reconnect-reconciliation target and the quick-send payload source: a client that
taps **Send** calls `POST /api/voice/stream/{stream_id}/send` which (a) returns the
current committed text *and* any final uncommitted tail (so the operator can see
the exact text that's about to go out, and edit it if the UI surfaces a confirm
step), (b) closes the stream with `reason=operator_send`, and (c) for comm channels
is what `internal/router/router.go` posts as the resulting message body. The
endpoint is rate-limited to the stream's own session (the stream id itself is a
capability — an unguessable 128-bit token, matching the daemon's existing
secret-handling posture in `internal/server/voice.go`'s auth), so a stolen stream
id is the only cross-session attack surface, and the id is scoped to one
`session_id` + one `operator_id` pair at creation time.

**Event ordering guarantee.** The SSE hub delivers per-topic events in publish
order (`internal/server/sse.go` assigns strictly-increasing per-topic ids and
broadcasts in that order); the decoder emits at most one of
`{partial, commit, revise}` per decode window in the order they were produced, so
a well-behaved client can process the event stream linearly and only needs the
snapshot endpoint to (a) recover from a detected gap (id jump over `last_seq`) or
(b) bootstrap before the first event on a mid-stream attach.

## Quick send

**Definition.** *Quick send* is the ability to deliver the **committed (stable)
prefix** of a live transcription to its destination immediately — before the last
few words finish decoding and before the backend would have finished a
whole-utterance non-streaming transcription. It is *not* the same as "send the raw
partial tail" (that would risk committing words that a revise would rewrite), and
it is *not* "wait for final and then send" (that's just the existing
request/response path this feature replaces).

**Semantics, per surface:**

- **PWA / session input (`internal/server/web/app.js`).** The recording overlay's
  existing **Cancel**/**Send** buttons (the BL326 modal in
  `internal/server/web/app.js`) are repurposed: while a stream is open, **Send**
  calls `POST /api/voice/stream/{id}/send`, which returns the committed text; the
  client inserts it into `#sessionInput` (the same `inputEl.value = ...` append
  the non-streaming path uses today) and *optionally* auto-submits if the
  operator enables "auto-send on stable prefix" (off by default — the
  "editable before it leaves the machine" goal means the operator still taps a
  real Send in the input bar, not the overlay). A new "Send what's stable so far,
  keep recording" affordance (a secondary quick-action, keyboard shortcut
  available) is what delivers the stable prefix *and* leaves the stream running so
  the operator can keep dictating the next sentence. **Cancel** closes the stream
  with `reason=operator_cancel` and discards both committed and partial text.
- **Comm channels (Signal/Telegram/Slack/Matrix, `internal/router/router.go`).**
  A "quick send" mid-utterance is a send of the committed prefix as a regular
  chat message at the moment the operator taps Send in the *source* app's own
  voice-note UI — but comm channels don't have a live overlay; the equivalent is
  "finish the voice note, and the *stable portion* arrives immediately while the
  *tail* arrives as a corrected follow-up on `revise`". Concretely: the router
  posts the committed text as the first message once the stream closes
  (`reason=operator_send`) or, if the operator explicitly quick-sends from a
  surface that has a stream id, immediately upon the `send` call — and any
  subsequent `revise` is delivered as a *new* short message with a
  "(correction)" prefix, never as a silent rewrite (message platforms don't
  support in-place rewrite reliably across all five adapters, so we don't rely on
  it — this is an intentional, reason-logged exclusion in the Parity surface
  section below). Low-confidence (< 0.6, per the existing rule in
  `docs/howto/voice-input.md`) committed text gets the existing
  `[transcribed]?` suffix on quick send so the operator can spot-correct, matching
  today's behavior for the final path.
- **MCP (a new `voice_stream_*` tool family).** A session's LLM can
  `voice_stream_open` (returns `stream_id`), `voice_stream_send` (returns the
  committed text — the LLM can then act on it immediately rather than waiting for
  a whole-file transcription), and `voice_stream_close`. This is the
  `voice_transcribe` MCP tool (`docs/howto/voice-input.md` lists it) extended from
  "one blob, one answer" to "open, stream, quick-send, close" — the same
  capability an in-session LLM operator would want when dictating a long
  multi-step task. The MCP tool surface is REST-backed (the daemon's MCP server
  already proxies to REST endpoints, per the existing
  `voice_transcribe` tool), so no second implementation is required: the MCP tools
  are thin wrappers over the same
  `POST/GET /api/voice/stream/...` endpoints.
- **CLI (`datawatch voice stream`).** Opens a stream from `stdin` (e.g.,
  `sox -r -y -e signed-integer -b 16 -c 1 mic -t raw - | datawatch voice stream`)
  or a local file (chunked into the same 5 s frames client-side in the CLI
  process, reusing the same `ffmpeg`-via-`exec` normalization path
  `internal/server/voice.go` uses), prints committed text to stdout as
  `commit` events arrive (with `partial` text on a separate line, suffixed with
  `…`, so a human reader can see the tentative tail without it being mistaken
  for final text), and exits `0` / non-zero per the final `close` event's
  `reason`. This gives the operator a way to test the streaming path without the
  PWA, and to wire it into shell pipelines (`datawatch voice stream --file in.wav
  | tee out.txt`), which is the natural "quick send to a file" on the CLI
  surface and is what the Testing plan's live smoke will exercise first.
- **Android / iPhone.** The Compose Multiplatform app reuses the same REST
  endpoints (open/frames/send/close) and either subscribes to the SSE events
  endpoint (Android: OkHttp SSE; iOS: URLSession `bytes`) or polls the snapshot
  endpoint on a 500 ms cadence (the fallback path, also fine given the
  steady-state event rate is a few events/sec). The OS-native audio recorders
  (the same ones `toggleVoiceInput`'s PWA counterpart uses, per the howto's
  § 5a) produce the same blob/frame sequence the PWA will, so there is no
  second audio format to support — the `ffmpeg` 16 kHz mono normalization in
  `internal/server/voice.go` accepts whatever container the OS produced, as long
  as it's a container `ffmpeg` can read (opus/webm/mp4/caf are all in scope).
  The parity requirement here is *capability* parity (quick send, edit, cancel,
  live text all reachable), not *implementation* parity (each platform's native
  event loop and recorder stay the idiomatic choice), per AGENT.md's
  Mobile-Parity Rule ("capability parity is required; implementation parity is
  not — each platform uses idiomatic delivery").

**Invariants the whole feature must hold** (these are the contractual guarantees
the test plan enforces):

1. **Committed text is monotonic except via an explicit `revise`.** A client that
   appends every `commit.text` in `seq` order and applies every `revise` as
   described reconstructs exactly the server's committed buffer at any `seq`
   boundary — there is no other way the committed text changes.
2. **Quick send never sends a word the server hasn't committed.** The
   `POST /send` response's text is a snapshot of the committed buffer at the
   moment of the call; any subsequent `revise` is a *separate*, explicitly
   prefixed correction (comm channels) or a visible splice (PWA) — never a silent
   swap-out of text that already left the client.
3. **Every `close` carries a `final_stable` that equals the committed buffer at
   close time** (the client can assert this against its own accumulated buffer as
   a live smoke check).
4. **No raw audio is stored** (matches `internal/server/voice.go`'s
   transcode-then-transcribe posture) beyond the in-memory `window + overlap`
   ring per open stream.
5. **Stream ids are single-capability tokens** (128-bit, tied to one session +
   operator at creation) — there is no "list all open streams" endpoint and no
   cross-operator access path.

**Auto-close bounds.** A stream auto-closes on:
- `idle_timeout_seconds` (default 20 s) of no voiced audio past the last
  `commit` (the VAD gate from Chunking and windowing drives this), *unless*
  `keep_alive=true` was set at open (CLI long-dictation mode) — in keep-alive the
  stream parks the decoder but the committed buffer persists and a later `commit`
  resumes event emission;
- `max_stream_seconds` (default **3600**; hard ceiling **14400** s) total elapsed
  stream time, regardless of voice activity, to bound the memory ceiling of an
  abandoned-but-not-cancelled stream;
- `POST /close` (operator Cancel) or `POST /send` (operator Send — send *closes*
  the stream unless the client set `keep_open=true` for the "send stable prefix,
  keep dictating" affordance, in which case the server returns the committed text
  and continues running).

## Backend options

The streaming pipeline is backend-agnostic by design — the
`transcribeSurface`/`Transcriber` seam in
`internal/transcribe/transcribe.go` and
`internal/transcribe/chain.go` already abstracts "give me a window of PCM, give
me text back" — but the *stability* and *latency* characteristics of the
underlying model are exactly what determine the chunking / windowing defaults,
so the choice of streaming backend is a first-class part of this feature.

**Option A — Whisper (existing, `internal/transcribe/transcribe.go` + the
OpenAI-compatible server behind `internal/transcribe/openai_compat.go`).** The
family the daemon already ships: offline `base`/`small`/`medium` (venv, 30 s
window, `internal/transcribe/split.go`'s split path), or a hosted
OpenAI-compatible endpoint. For *streaming*, this is the "rolling-window
stable-prefix" policy from Chunking and windowing: each 12 s window is fed with
the last committed text as left-context prompt, the
`stabilize_decodes=2` overlap rule makes the head stable, and the tail is
tentative. Advantages: already integrated, no new model download, the chain
fallback (`internal/transcribe/chain.go`) means a failing streaming leg degrades
to the offline transcriber transparently. Disadvantages: per-window decode cost
scales with model size (a `medium` on CPU is ~4× the wall-clock of `base`,
eating the 2 s steady-state lag budget), and the stabilization rule is a
*heuristic* over a non-streaming model — it works well in practice on clean
speech but is not guaranteed by the model, so revisions are a real (if rare)
case the protocol must handle.

**Option B — NVIDIA Parakeet / NeMo TDT (streaming-first).** Parakeet TDT (and
the related NeMo TDT models) are trained *natively* on streaming
chunk-by-chunk input with low-latency targets (single-digit ms of
model-computed latency per audio chunk on GPU, sub-100 ms on modern CPU for the
smaller check-point sizes), and do *not* have a 30 s hard window — they process
arbitrary-length input in streaming chunks. This removes the need for the
rolling-window stabilization heuristic entirely: the model's own streaming
segmenter decides where words commit, and the daemon's role reduces to
(a) feeding the ring buffer's new frames, (b) relaying the model's
`token_commit` / `token_revise` emissions as the daemon's `commit` / `revise`
events. Latency is *inherently* low (the model doesn't have to re-decode a
12 s window every ~8 s stride — it decodes each ~200 ms of new audio exactly
once), and throughput on long streams is therefore dramatically better than
Option A on CPU hardware. Disadvantages: a second runtime to ship (a NeMo
container or a `python` venv with `nemo` + a model file, roughly 500 MB–2 GB
depending on the check-point), a new configuration surface (`whisper.backend:
parakeet` or a new `streaming.backend: parakeet` key), and it's *not* a drop-in
replacement for the existing `whisper` family — it's an *additional* backend
leg in the chain (`internal/transcribe/chain.go` can list it as the streaming-
preferred leg, with Option A as the offline/fallback leg, so the daemon still
has a guaranteed-correct path if Parakeet is unavailable).

**Option C — Moonshine (Boson, small CPU real-time model).** Moonshine and
similar small real-time CPU STT models target *on-device* (phone) real-time
transcription at very low compute cost (a few % of a modern phone core, ~1×
realtime on a laptop CPU); accuracy is below Whisper `base` on noisy or
specialized-speech audio, and on-domain vocabulary/code-like tokens are where
the gap is largest. For this product's operator demographic (who already
tolerates `base`-model Whisper for the offline path, per the existing
configuration defaults in `docs/howto/voice-input.md`), Option C is not
competitive as the *primary* — but it is a reasonable *secondary on-device*
option for mobile surfaces (Android/iPhone) where a 12 s window round-trip to
the daemon over the network is the dominant latency term rather than decode
cost; in that case the mobile app can run a tiny local model for the *first*
tentative characters (to give immediate visual feedback) and let the daemon's
streaming leg commit the authoritative text, with the existing
stabilization / revise protocol absorbing the discrepancy. That's a nice-to-have
for a later phase, not a blocker.

**Recommendation.** *Primary streaming backend for the daemon: Option B
(Parakeet/NeMo TDT streaming model) on GPU hardware, falling back to Option A
(rolling-window stable-prefix over the existing Whisper family, `base` model on
CPU, `small` on faster hardware) where the streaming model isn't installed.*
The two are not mutually exclusive in the daemon — `internal/transcribe/chain.go`
already supports a primary + secondary leg, so the daemon can run Option B as the
streaming leg and Option A as the offline/finalization leg behind it, and the
existing fast-fail detection (`ctranslate2` / `cuda` / `engine-not` signatures,
see `internal/transcribe/openai_compat.go`) means a broken streaming leg
degrades to the offline leg without any protocol-level change from the client's
point of view (same `commit`/`partial`/`revise` events, just with higher
steady-state latency and a higher `revise` rate). *Why not Option C as primary:*
the operator-facing accuracy floor this codebase has already established —
`base` model as the default in `docs/config-reference.yaml`
(`WhisperConfig.Model` default `"base"` in
`internal/config/config.go`) — is higher than what Option C reliably delivers on
the operator's actual speech (code-adjacent vocabulary, technical terms, the
operational language this product's user base uses), and the latency
advantage Option C offers over Option A-on-CPU-parked-on-GPU is small enough that
accuracy is the right trade. *Why Option B before Option A as the streaming
default:* Option B's per-chunk decode cost is O(new audio) rather than
O(window length per stride), so on the hardware class this product targets
(single-node daemons, often a laptop or a Jetson-class ARM box — see the
ARM/Thor CTranslate2 caveats in `docs/howto/voice-input.md`) Option B gives a
better steady-state lag *and* a lower CPU footprint over a long multi-hour
dictation, while remaining a drop-in leg behind the existing chain.

**Model-size / accuracy matrix (for the config reference).**

| Backend leg | Model | CPU (modern laptop) steady-state lag | GPU (any) lag | Notes |
|---|---|---|---|---|
| Option A (Whisper, rolling) | `tiny` | ~1 s | < 0.5 s | Accuracy floor — not the default |
| Option A | `base` | ~2 s | < 1 s | Current default; recommended CPU default for streaming |
| Option A | `small` | ~3–4 s | ~1.5 s | Faster hardware only |
| Option A | `medium` | > 6 s (exceeds budget) | ~3 s | GPU only, not recommended as a streaming default |
| Option B (Parakeet TDT) | small check-point | ~0.5–1 s | < 0.3 s | Recommended primary streaming leg; no 30 s window |
| Option B | large check-point | ~1–2 s | < 0.5 s | Higher accuracy if the hardware supports it |
| Option C (Moonshine-class) | on-device | N/A (runs on the phone) | N/A | Mobile-surface early-feedback only, not a daemon default |

The daemon exposes *which leg produced which event* in the `error` event's
`detail` and in the snapshot endpoint's `backend` field (see
Protocol) so the operator can see in real time that a `revise` burst came after
a streaming-leg fallback to the offline leg, rather than guessing.

## Failure modes

The streaming path's failure surface is larger than today's one-shot path (which is
a single POST, a single response), so each mode below is paired with the recovery
that keeps the operator's committed text intact and the stream either resumable or
cleanly closed (per the invariants in Quick send).

**F1 — Audio frame dropped or out-of-order on the wire.** The chunked-POST audio
path is per-frame
(`POST /api/voice/stream/{id}/frames`), so a dropped frame is one 5 s of
audio, not the whole utterance. Each frame POST is idempotent by `chunk_id`
(server dedupes on `(stream_id, chunk_id)`), the client retries a failed POST
at the same backoff as the SSE reconnect (1 s → 30 s cap), and if the gap since
the last *accepted* frame's `chunk_id` grows past `window_seconds +
overlap_seconds` (the ring-buffer retention from Chunking and windowing), the
server emits `event: gap` (a kind of `error` event with `code=E_GAP`) and the
client stops re-sending the old tail — at that point the overlap buffer can
no longer re-cover the missing region, so the honest recovery is the snapshot
reconcile step (client re-fetches
`GET /api/voice/stream/{id}`, the committed buffer is unchanged, the partial
is gone until the next voiced region is decoded and the next `commit` arrives).
This reuses the same "re-fetch state on gap" pattern the
SSE transport section describes for downstream events; the two gaps (audio
upstream, events downstream) share one reconciliation primitive on the client.

**F2 — Streaming backend leg fails mid-stream (model OOM, GPU drop,
`ctranslate2` load failure, 503/504).** The
`ChainedTranscriber` in `internal/transcribe/chain.go` already handles
primary→secondary fallback for a *single* decode window; for streaming the
rule is that a failed decode window in window *n* does **not** fail the stream
— the server emits an `error` event (`code=E_PRIMARY_FALLBACK`,
`retried=true`, `stream_still_open=true`) and the *next* window uses whichever
leg is currently healthy, and the committed buffer is exactly the
`commit`/`revise`-derived state it was before window *n* (invariant 1 in
Quick send). The transition is visible in two ways: the snapshot's
`backend` field flips from the streaming leg to the offline leg, and if the
offline leg produces a different tail for the overlapping region than the
streaming leg did just before, that difference surfaces as a `revise`
event — which is *desirable*: the operator sees the corrected text, not a
silent divergence between the two legs. A hard failure of *both* legs
(OOM + offline leg missing, or offline leg's venv path invalid per
`internal/config/config.go`'s
`WhisperConfig.VenvPath`) closes the stream with
`reason=fatal_error` and a `final_stable` equal to everything committed up to
the last good window — the operator never loses committed text to a backend
failure, they lose at most the last uncommitted tail.

**F3 — Server restart / daemon crash with open streams.** The stream's state
(live ring buffer of accepted PCM) is in-process memory, so a daemon restart
loses the *in-flight* window and the *partial* tail, but the *committed
transcript* for a stream that had a `commit` event in flight at the moment of
crash is recoverable from the client's own accumulated buffer (the client is
the source of truth for what it already rendered; the server's job on
recovery is to let the client re-attach). Concretely: on startup the daemon
re-publishes a `close` event with `reason=server_restarted` on any pre-existing
stream topic (the SSE hub's per-topic subscriber set —
`internal/server/sse.go` — re-subscribes on reconnect and the last
`seq`-based `Last-Event-ID` check will reveal the stream is now closed), and the
snapshot endpoint returns the last-persisted committed text (the daemon writes
committed text to a per-stream spool file, e.g.,
`~/.datawatch/voice-streams/{id}.committed`, on every `commit`/`revise` event
— this is a deliberately small write, a few KB per minute of dictation, and it
gives both the `final_stable` invariance and a crash-recovery path without a
full audio retention policy). The client, on seeing
`reason=server_restarted`, offers the operator a "re-open with the committed
text pre-filled" affordance (the PWA can re-open the stream with
`POST /api/voice/stream` carrying
`pre_commit_text=<client's committed buffer>`; the daemon appends that to its
own fresh committed buffer and continues from there) — that's the
"crash-tolerant resume" path, and it's the *only* place the client's
accumulated buffer is fed back to the server, which keeps the server's
committed buffer authoritative in all other cases.

**F4 — VAD false-negative (server thinks nobody's speaking, decoder parked).**
The energy gate in Chunking and windowing uses a ~1 % full-scale RMS threshold
(the same threshold `internal/transcribe/split.go`'s silence search uses);
a very quiet speaker or a heavily compressed low-bitrate Opus frame can dip
under it, delaying the next decode window by up to `vad_min_voice_ms` plus the
stride. The cost of this is bounded by construction: the committed buffer is
unchanged (invariant 1), the *partial* tail is retained and just goes
`stale` after `partial_stale_ms`, and the next voiced region above the
threshold re-triggers the decod
er and re-decodes the overlap region — worst case the operator waits one
extra stride (~8 s with defaults) before the next word commits, and no text
is lost, only delayed. A very quiet operator can lower the threshold via
`voice.stream.vad_rms_threshold` (default 0.01, matching
`splitSilenceTarget`'s threshold in `internal/transcribe/split.go`) if they
find commits are stalling; that's a config knob, not a code fix, because the
threshold is genuinely workload-dependent and hard to auto-tune without a
reference signal.

**F5 — VAD false-positive (silence decoded as if it were voiced).** The
decoder runs one extra 12 s window on pure silence; Whisper-family backends
either emit nothing new (common) or a spurious homoph
one or filler word for the overlap region (rare, and *only* the overlap
region — the true tail of a pure-silence window is itself silence, so it
produces no new committed word on its own). If a spurious word does appear
in the overlap region it goes through the normal
`stabilize_decodes` gate: the next window (which re-decodes that same
overlap region for real, now with actual voiced context on either side)
either confirms it (in which case it was probably a real word the VAD
mis-scored as silence — fine to commit) or refutes it (in which case a
`revise` event retracts it and the client splices it out). Either way the
committed buffer never carries a word that two consecutive decodes didn't
both support for the same audio span, so a VAD false-positive costs at most
one wasted decode and, in the worst case, one visible `revise` event — it
cannot corrupt the committed text, by the stabilization rule's
construction.

**F6 — Client disconnects mid-dictation (network drop, tab closed).**
The SSE hub's per-topic subscriber set
(`internal/server/sse.go`) goes empty; the keep-alive comment and the
`publish` path's non-blocking send both handle a
dead subscriber by dropping it, so the decoder keeps running and the committed
buffer keeps growing for `idle_timeout_seconds` past the last *voiced* region
(the hub's `unsubscribe` on disconnect is
what starts the idle timer running — the decoder doesn't know the client is
gone, so the stream behaves exactly as if the client had just walked away
from a still-open stream, which is the same as F3's "client walked away"
branch rather than the "daemon crashed" branch). When the reconnect-with-
`Last-Event-ID` path succeeds (common case, brief network blip), the client
reconciles via the snapshot endpoint as in the Transport section and resumes
rendering from `last_seq`; when the idle timer fires first (long disconnect,
tab actually closed), the stream is closed with `reason=idle_timeout` and the
committed buffer is spooled to disk (F3's spool) so a later
`GET /api/voice/stream/{id}` within `transcript_retain_seconds` still returns
it, and the client can offer to re-open with the committed text pre-filled
(same affordance as F3's recovery path).

**F7 — Ring-buffer memory ceiling (abandoned long streams).** Each open stream
holds `window_seconds + overlap_seconds` ≈ 16 s of 16 kHz mono PCM ≈
~ 1 MB (16 kHz × 2 bytes × 16 s) in the ring, plus the committed-text spool
on disk (bounded by `max_stream_seconds` × average speech rate, a few KB/s at
most). A pathological operator (or a malicious client holding a stream id
open while streaming silence) is bounded by
`max_stream_seconds` (default 3600 s, hard ceiling 14400 s) after which the
stream auto-closes with `reason=max_duration` (even in `keep_alive` mode the
*duration* ceiling still applies — keep-alive parks the *decoder*, it doesn't
waive the memory/duration bounding); the committed buffer at that point is
spooled and retrievable as in F3, so "I accidentally left a stream open and
it hit the duration cap" is recoverable, not data-losing. The daemon also
caps the *concurrent* open streams per operator
(`voice.stream.max_concurrent_per_operator`, default 4) so a stuck
client-side retry loop in `internal/server/web/app.js` can't open
unbounded decoders; the 4th concurrent
`POST /api/voice/stream` from the same operator returns `409` with
`code=E_MAX_CONCURRENT_STREAMS` and the existing overlay's error path (the
`showError` call the non-streaming path in
`internal/server/web/app.js` uses for a bad `/api/voice/transcribe`
response) renders it.

**F8 — `revise` storm (backend disagreement, model boundary, a genuinely
ambiguous word the two legs keep flip-flopping on).** If the
`stabilize_decodes` gate is set to 2 (default) but the two legs (or two
consecutive windows on the same leg) keep disagreeing on a particular
overlap-region word, each
agreement→`commit`, each later
disagreement→`revise`, and the committed buffer
oscillates. The protocol handles this *correctly* (invariant 1 still holds —
every change is an explicit `commit` or `revise`, never a silent one) but it's
bad for the operator. The bounded response: (a) the client can
*debounce* its rendering of a `revise` that reverts a `commit` made in the
last `revise_debounce_ms` (default 3 s) by showing the word as
`[unstable: "…"]` rather than ch
asing the flip-flop (a
pure client-side rendering concern — the *committed buffer* itself still
records every change faithfully, so the final `final_stable` is unaffected
by the debounce, the debounce only
affects what the operator sees *while* it's happening); (b) the daemon
counts `revise`-per-`commit` ratio per stream and if it exceeds
`voice.stream.revise_warn_ratio` (default 0.25) sustained over
`revise_warn_window` (default 30 s) it logs a
`warning: stream {id} is revision-heavy; consider a larger overlap_seconds or a different backend leg` to the
operator log and
emits one `error` event (`code=E_REVISION_HEAVY_ADVISORY`)
so a UI can show a "transcription is being a bit wobbly on this stretch"
toast — it does *not* change the committed buffer, it's an advisory so the
operator knows the text they're about to quick-send
may still shift, which is exactly the information they need at the quick-send
moment.

**F9 — `ffmpeg` normalization failure (a frame's container is something
`ffmpeg` on this box can't read — the ARM/Thor
CTranslate2 / `ffmpeg`-variant caveats in
`docs/howto/voice-input.md` apply equally to the streaming path's per-frame
normalization).** A single frame's
`ffmpeg` failure is a per-frame `error`
event (`code=E_FRAME_MALFORMED`, `retried=false`)
that skips that one 5 s of audio into the
ring (the ring's continuity is
preserved — the
*next* frame's `chunk_id` just has a 5 s hole in it that the
overlap-decode
stabilization rule
cannot recover from, so the words in that 5 s are
*lost from the committed transcript* with a visible `error` event naming
them by
`chunk_id` — the PWA can render "
(skipped 5 s — format error)" inline, which is
honest and matches the operator's expectation
of "something went
wrong with that bit, not with my whole dictation"); if *every* frame fails
normalization (a bad `ffmpeg` entirely, or a
container the daemon's `ffmpeg` simply can't
parse), the stream closes with
`reason=fatal_error` and the
error footer naming the `ffmpeg`
failure — matching the
existing "
`ffmpeg` missing" pitfall behavior
in `docs/howto/voice-input.md`, just per-stream instead of per-utterance,
which is strictly less destructive (an
operator mid-dictation
who hits a bad frame doesn't lose the whole multi-minute recording, they
lose 5 s of it plus whatever
`revise`-visible
instability the following overlap-decode
produces while the missing 5 s' context is absent
from the left-context
prompt)

**F10 — Auth / stream-id scope violation (a client tries to act on a stream
it does not own).** The stream id is a single-capability 128-bit token tied
to one `session_id` + operator pair at creation (see Partial / final
transcript protocol, snapshot endpoint above), and the `session_id` is frozen
at stream creation the same way `internal/server/voice.go`'s existing
`session_id` form-field is trusted for one request today — a stream can not
be re-pointed at another session mid-life, so the worst case of a stolen id
is that the stealer can read the committed text and trigger one `send`/`close`
on that one stream, exactly as much as any other bearer credential in this
codebase grants (the daemon's auth posture — `Authorization: Bearer` on the
voice handler, secret storage for any external key per
`docs/howto/secrets-manager.md` — applies unchanged). There is no
"list open streams" endpoint, no cross-operator stream access, and `send`
cannot target a different `session_id` than the one frozen at creation, so
the attack surface of the streaming path is deliberately no larger than the
attack surface of today's one-shot path (`internal/server/voice.go`):
single-stream, single-session, bearer-credential-scoped.

**F11 — Protocol/version disagreement between daemon and an older client.**
An older PWA/mobile build (pre-streaming) simply never calls the new
endpoints — it keeps using `POST /api/voice/transcribe` exactly as before,
so the only way the two paths interact is if an operator mixes a new client
(streaming) and an old client (one-shot) against the same daemon in one
session, which is fine by construction: the one-shot path is untouched and
stateless (`internal/server/voice.go`'s handler does not read or write any
stream state), the streaming path is self-contained in its per-stream spool,
and the only shared resource is the transcriber itself (`internal/transcribe/
chain.go`). A concurrent one-shot transcription and one streaming decode on
the same backend leg is exactly the situation `ChainedTranscriber`'s
existing per-call independence already handles (each call is its own
`Transcribe(ctx, wav)` invocation, no shared mutable state inside
`internal/transcribe/transcribe.go`'s transcribers beyond the
subprocess/client they each spawn per call), so mixing clients against one
daemon degrades to "the backend is a bit busier", not a protocol error.
The only real versioning risk is the reverse direction — a *new* client
talking to an *older* daemon that doesn't have the streaming endpoints yet —
and that's a plain 404 from the router (`internal/server/router.go`-level
route matching, i.e. `404 Not Found` with the daemon's standard
`{"error": "not found"}` body that
`internal/server/voice.go`'s existing handlers already emit for unknown
sub-paths), which the new client's error handler (the same
`showError` path in `internal/server/web/app.js` used when
`POST /api/voice/transcribe` returns a non-200) renders as
"streaming not available on this daemon — use one-shot mode", and the
client then falls back to its existing one-shot `toggleVoiceInput` flow,
so the feature degrades to *today's* behavior rather than breaking it.

**F12 — A `commit` event lands but the SSE connection is dead at that
exact instant.** The SSE hub's `publish` in `internal/server/sse.go` sends
to each subscriber on a non-blocking basis (a slow or dead connection is
dropped without stalling the decoder), so if the client's socket is gone
the moment a `commit` fires, that client will not get the event over SSE
this session — but the committed buffer itself is unaffected (invariant 1
is about the *server's* committed buffer, not about any one client's
view), the spool file (F3) has already recorded it, and the client's
reconnect path (Last-Event-ID gap-detection + the snapshot
reconcile `GET /api/voice/stream/{id}`) recovers the full committed text
on reconnect — "lost in transit, recoverable by design", the same
guarantee the daemon's existing SSE endpoints give to their subscribers
today (see `internal/server/sse.go`'s non-blocking publish contract). A
client that *polls* instead of using SSE (the fallback transport, or a
comm-channel/CLI consumer) is entirely unaffected by this failure mode
because polling re-reads the committed buffer from the snapshot endpoint
every poll — it can never be "one commit behind" for longer than one poll
interval, which at the default 500 ms cadence is far below the operator's
perceptual threshold for "did my word register".

## Parity surface

Per the Mobile-Parity Rule and Federation-Parity-adjacent checklist
conventions in AGENT.md (the full parity-surface set being `REST`, `MCP`,
`CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS`),
here is how each surface behaves in the streaming design, and what is
intentionally not streaming on each:

| Surface | Streaming support | Quick send | Edit before send | Cancel | Notes / code path |
|---|---|---|---|---|---|
| **PWA** | Full (SSE + chunked frame POST) | Full (`POST /api/voice/stream/{id}/send`, plus the "send stable prefix, keep dictating" affordance) | Full (transcript lands in `#sessionInput`, the same field `toggleVoiceInput` in `internal/server/web/app.js` writes into today) | Full (`POST /api/voice/stream/{id}/close`, `reason=operator_cancel`) | Primary surface; `MediaRecorder` blob framing reused from the existing overlay in `internal/server/web/app.js` (BL326) |
| **Android** | Full, via the same REST endpoints (SSE or the polling fallback, the client's choice) | Full (same `send` endpoint semantics) | Full | Full | Capability parity with the PWA per AGENT.md's Mobile-Parity Rule; no *implementation* parity required — the OS-native recorder + the platform's idiomatic event transport (OkHttp SSE or polling, per Quick send) are the correct choice, not a copy of the PWA's `MediaRecorder` loop |
| **iPhone/iOS** | Full, via the same REST endpoints (URLSession `bytes`-based SSE or polling) | Full | Full | Full | Same Mobile-Parity-exception logic as Android; APNs/FCM push are irrelevant here (voice is a foreground, in-app interaction, not a push-triggered one), so no push-surface claim is made for this feature |
| **Comm channel (Signal/Telegram/Slack/Discord/Matrix)** | The *daemon-side* streaming pipeline is fully available (a voice note arriving on a comm channel could, if the operator has a live PWA session for the same `session_id`, surface partials there — an operator-facing "watch a comm-channel voice note being transcribed live in the PWA" nicety, *not* a comm-channel-UI feature, because comm-channel *senders* have no live UI to attach to); the comm-channel *receiver* only ever sees the final text, delivered by `internal/router/router.go` exactly as it delivers transcribed text today, plus an explicit `(correction)` follow-up message on any `revise` after a `send` | Limited to "send the committed text as soon as the stream closes" (there is no mid-dictation quick-send *within* a comm channel, because a comm-channel voice note is by definition one fixed recording, not an open-ended dictation session — the "quick send" concept only applies to a surface that is still recording, which a comm-channel voice note never is); this is an intentional, stated scope limitation, not an implementation gap | N/A (comm channels don't support rich inline editing of an in-flight message across all five adapters the daemon supports, so the "edit before send" affordance is a PWA/CLI/mobile-app capability, not comm-channel; the operator's existing "reply with a corrected version" workflow on the comm channel is the comm-channel equivalent, unchanged by this feature) | N/A same reason | The only *new* comm-channel-visible behavior is the `(correction)` follow-up message on a post-`send` `revise`; everything else (low-confidence `[transcribed]?` suffix per `docs/howto/voice-input.md`, the `[transcribed]` prefix convention the router already uses) is unchanged |
| **REST** | Full (this *is* the primary API: `POST /api/voice/stream`, `POST /api/voice/stream/{id}/frames`, `GET /api/voice/stream/{id}`, `GET /api/voice/stream/{id}/events`, `POST /api/voice/stream/{id}/send`, `POST /api/voice/stream/{id}/close`, all under the same `Authorization: Bearer` requirement `internal/server/voice.go` imposes today) | Full | N/A conceptually (REST is the *transport*, "edit" is a client-app concept; the REST surface simply returns the committed text and lets the caller decide what to do with it) | Full | The `testing-tracker` Rule (AGENT.md) applies: these are new endpoints, so `docs/testing-tracker.md` needs a new section (recorded in the Rollout phases checklist below) |
| **MCP** | Full (a `voice_stream_open` / `voice_stream_frames` / `voice_stream_send` / `voice_stream_close` tool family, thin REST proxies — the same pattern the existing `voice_transcribe` MCP tool already uses per `docs/howto/voice-input.md`'s MCP section, so no second implementation, only new tool definitions over the new REST endpoints) | Full (an in-session LLM can quick-send a committed prefix into its own context immediately, which is the LLM-facing analogue of "act on the stable text now") | N/A same reason as REST above | Full | Capability parity per AGENT.md's operator-interface surfaces list |
| **CLI** | Full (`datawatch voice stream --file <path>` or piped `stdin`, printing committed text to stdout as it commits and a dimmed/bracketed partial tail as described in Quick send's CLI row) | Full (`--send-to <target>` on close, or piping stdout to another command is the shell-native "quick send") | Full (an operator piping into `$EDITOR` is the idiomatic CLI edit step) | Full (`Ctrl-C` closes the stream with `reason=operator_cancel`, same semantics as the PWA's Cancel button) | The CLI is also the fastest way to smoke-test the streaming daemon without a live mic, which is why the Test plan below starts there |
| **YAML/config** | Full (every knob in the Configuration section below is a `voice.stream.*` YAML key in the same `whisper:` block the existing `WhisperConfig` in `internal/config/config.go` lives in, so `datawatch config set voice.stream.enabled true` + `datawatch reload` is the entire "turn on streaming" workflow, matching the existing `datawatch config set whisper.backend ...` ergonomics in `docs/howto/voice-input.md`) | N/A (config doesn't "send", it configures) | N/A | N/A | The Configuration Accessibility Rule (AGENT.md) is satisfied because every new behavior here is reachable without a code change: an operator who wants streaming on *only* the PWA, or *only* one specific comm channel's associated session, does so purely with `voice.stream.*` keys, no feature-flag server restart mid-stream required (the existing `datawatch reload` hot-reload path in
`docs/howto/voice-input.md` covers this unchanged) |

**Reasons an intentionally *excluded* capability appears as "N/A" above,
summarized:** comm channels cannot mid-message-edit across all five adapters
the daemon supports (a hard technical fact of the platforms, not a choice);
`edit` is a client-app concept with no REST/MCP/CLI analogue to *require*
(the REST surface's job is to return text, not to render it); push delivery
(APNs/FCM) is orthogonal to a foreground recording interaction. No surface
in the set is excluded from *the core capability* (near-live text, quick
send of stable text, edit-before-leave, cancel) without an explicit
reason above, which is the bar AGENT.md's Mobile-Parity Rule and
Configuration Accessibility Rule both set.

## Configuration

New keys land in the existing `whisper:` block alongside
`WhisperConfig`'s current fields (`internal/config/config.go`: `backend`,
`model`, `language`, `venv_path`, `endpoint`, `api_key`, `timeout`,
`window_seconds`), exposed in `docs/config-reference.yaml` under a new
`streaming:` sub-block so the YAML shape is `whisper.streaming.*` — this
keeps `config_set whisper.model` and any existing `whisper.backend` value
working exactly as before (the existing one-shot path reads the same
top-level keys, untouched), while streaming-specific behavior is a distinct,
discovered sub-tree, matching how the existing `whisper.venv_path`,
`whisper.endpoint`, etc. are documented in `docs/config-reference.yaml` and
`docs/howto/voice-input.md`'s "Configuration" section.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `whisper.streaming.enabled` | bool | `false` | Master gate. `false` = the daemon exposes none of the new `/api/voice/stream*` endpoints at all (a client trying them gets the 404 + "streaming not available on this daemon — use one-shot mode" error described in Failure mode F11), and no streaming leg is loaded even if one is installed. `true` = endpoints exposed, streaming leg loaded at startup (validated once, not per-request, so a broken `Parakeet`/`venv` install is caught at boot and reported, matching how `internal/transcribe/transcribe.go`'s `New` validates the venv path today, just at daemon-startup time rather than first-request time). |
| `whisper.streaming.backend` | string | `"auto"` | Which leg is the *streaming* (first) leg among the streaming-capable ones installed. Values: `"auto"` (use a Parakeet/NeMo TDT leg if installed, otherwise the rolling-window Whisper leg over whichever `whisper.backend` value is already set), `"whisper_rolling"` (force the rolling-window leg, the one that reuses the existing `whisper.model`/`whisper.model`'s venv or endpoint), `"parakeet"` (force the Parakeet/NeMo TDT leg; errors at startup if not installed, so an operator who explicitly chooses a missing leg gets a startup failure, not a silent fallback — this is an intentional, operator-facing loud failure, different from `auto`'s silent fallback, and documented here so the two behaviors aren't conflated). The *offline fallback* leg is always the existing `whisper.backend` value from the top-level block, per
`internal/transcribe/chain.go`'s existing primary/secondary split, unchanged. |
| `whisper.streaming.model` | string | `"base"` | Model id for the rolling-window leg (Whisper-family: `tiny`/`base`/`small`/`medium`, matching `WhisperConfig.Model`'s existing default in `internal/config/config.go`) and the Parakeet check-point size descriptor for that leg (`small`/`large` per the Backend options matrix). `""` inherits the top-level `whisper.model` value. |
| `whisper.streaming.vad_rms_threshold` | float | `0.01` | The energy gate threshold (fraction of full-scale) that decides "is this frame voiced" (see VAD in Chunking and windowing). Lower it if a very quiet speaker's commits are stalling (Failure mode F4); raise it if silent regions are triggering extra decodes (Failure mode F5). Matches the ~1 % threshold `internal/transcribe/split.go` already uses for its near-silence search, so the two code paths share one mental model of "what counts as speech on this mic". |
| `whisper.streaming.vad_min_voice_ms` | int (ms) | `250` | Minimum sustained voiced duration before a decode window is triggered (Failure modes F4/F5 trade-off knob). |
| `whisper.streaming.window_seconds` | int | `12` | Decoder window length (Chunking and windowing). |
| `whisper.streaming.overlap_seconds` | int | `4` | Overlap between consecutive decode windows (Chunking and windowing). Must be less than `window_seconds`; the daemon validates this at startup (with `enabled: true`) and logs a clear config error if not, rather than
silently accepting an impossible windowing schedule. |
| `whisper.streaming.stabilize_decodes` | int | `2` | Number of consecutive agreeing decodes required before a word commits (Chunking and windowing stabilization rule). `1` = commit on first decode (faster, more `revise` events); `3` = extra-conservative (slower first-word, fewer `revise`s). |
| `whisper.streaming.idle_timeout_seconds` | int | `20` | Seconds of no voiced audio after which `keep_alive=false` streams auto-close (`reason=idle_timeout`; the Quick send Auto-close bounds list). |
| `whisper.streaming.keep_alive_default` | bool | `false` | Default for new streams; an explicit per-stream `keep_alive` in the `POST /api/voice/stream` body always overrides this (the CLI's long-dictation mode sets it per-stream, not via this key). |
| `whisper.streaming.max_stream_seconds` | int | `3600` | Hard per-stream duration cap (Quick send Auto-close bounds; hard ceiling **14400 s** even if the operator sets this higher — the daemon clamps and logs the clamp at startup, so an operator who asks for 24 h of one stream is told, not silently granted, and not silently granted *less* either — the 4 h default is what they actually get by default, and the clamp only matters if they explicitly ask for more than 4 h). |
| `whisper.streaming.max_concurrent_per_operator` | int | `4` | Max simultaneously-open streams per operator (Failure mode F7's mitigation for a stuck retry loop); the daemon returns `409` + `code=E_MAX_CONCURRENT_STREAMS` past this. |
| `whisper.streaming.transcript_retain_seconds` | int | `300` | How long a closed stream's committed transcript stays available via the snapshot endpoint (Partial / final transcript protocol); after this the spool file is deleted (see F3's spool path) and the raw-audio-never-stored invariant (Quick send invariants) is fully enforced — this key bounds *text* retention only, not audio, and the two must not be conflated in a UI label ("we don't keep your audio" and "we keep the transcript for 5 min" are both true and both must be shown in the PWA's recording overlay, per the existing howto's posture in `docs/howto/voice-input.md`). |
| `whisper.streaming.reconnect_backoff_ms` | int | `1000` | Base for the client-side SSE reconnection/backoff series (Transport section: 1 s → 2 s → 4 s → 8 s → 16 s → 30 s cap). |
| `whisper.streaming.reconnect_max_ms` | int | `30000` | Cap for that series. |
| `whisper.streaming.revise_warn_ratio` | float | `0.25` | `revise`/`commit` ratio that triggers the `E_REVISION_HEAVY_ADVISORY` log + event (Failure mode F8). |
| `whisper.streaming.revise_warn_window` | int (seconds) | `30` | Window over which the ratio in the previous key is measured. |
| `whisper.streaming.frame_seconds` | int | `5` | Duration of an upstream audio frame (Chunking and windowing's "Client → server audio framing"). Changing this is the main knob for tuning bandwidth vs. granularity on a constrained uplink; the server's `maxVoiceUploadBytes`-derived per-frame cap (`internal/server/voice.go`'s existing 25 MB ceiling, reapplied per-frame rather than per-utterance in this path) is never the binding constraint below ~60 fps of `frame_seconds` on a sane model, so this key is safe to leave at its default in every case the daemon's own hardware tests are tuned for (see Test plan). |

**Validation and reload behavior.** Every key above is read at daemon
startup (whenever `streaming.enabled` is `true`) and on `datawatch reload`,
and validated as a group: a bad value (a negative number,
`overlap_seconds >= window_seconds`, `stabilize_decodes < 1`, or
`max_stream_seconds` above the 14400 s hard ceiling) is rejected with a
startup/reload error naming the offending key and value, matching the
existing validate-at-boundary posture in `internal/config/config.go`
rather than deferring to first use. Reload never changes the backend leg of
an *already-open* stream: already-open streams keep running on whatever leg
they were started with (the same "config changes affect new invocations,
not in-flight ones" contract the top-level `whisper.*` keys follow today,
per
`internal/config/config.go`'s `WhisperConfig` and
`docs/howto/voice-input.md`'s "Configuration"/hot-reload sections), and
only *newly opened* streams pick up a changed `streaming.backend` value.

## Test plan

Testing follows the same structure as the sibling long-recording fix
(`docs/plans/2026-10-20-voice-long-recording-rootcause.md`'s "Test
infrastructure" and "Validation" sections, and its test files
`internal/transcribe/long_audio_test.go` /
`internal/server/long_audio_test.go`), and reuses the same discipline of
"synthesize audio in Go, transcribe with the real backend where a real
backend is available, assert on the *text* not the wall clock":

**1. Unit tests — protocol and state machine (pure Go, no model needed).**
New file `internal/server/voice_stream_test.go` (mirrors
`internal/server/long_audio_test.go`'s shape, which already stands up an
in-process daemon with `httptest` and drives it over HTTP, the same setup
the long-recording tests use). Coverage:
- `TestVoiceStream_OpenCloseLifecycle` — open a stream, POST two frames,
  receive a synthesized-decoder-driven `commit` event (a
  scripted/mock `Transcriber`, injected the same way `internal/transcribe/
  chain.go`'s `ChainedTranscriber` is constructed with real
  `TranscriptionFunc` values in its own tests), assert `final_stable` equals the
  accumulated `commit` deltas, assert `close`
  reason is `operator_send` / `operator_cancel` as driven from the test.
- `TestVoiceStream_ReviseAppliedConsistently` — script the mock transcriber to
  "commit" a word, then "revise" it, then commit a third word; reconstruct the
  client-side buffer exactly as the client would (append `commit.text`,
  `splice` on `revise`) and assert it equals the daemon's
  `committed` snapshot at `close` time — this is invariant 1 from
  Quick send, tested as an actual wire-level round trip, not just an
  in-memory assertion.
- `TestVoiceStream_SeenSeqGapReconcilesViaSnapshot` — drop a `commit` event
  from the client's view (simulated by the test client deliberately not
  reading an SSE event), then assert the reconciled snapshot endpoint
  returns the daemon's true committed buffer and the client's post-reconcile
  buffer matches it exactly (invariant 1 under the failure conditions F1/F12
  both describe).
- `TestVoiceStream_MaxConcurrentPerOperatorEnforced` — open
  `voice.stream.max_concurrent_per_operator`+1 streams from one
  operator-scoped credential, assert the last one gets `409` +
  `code=E_MAX_CONCURRENT_STREAMS` (Failure mode F7) and no daemon-side
  decoder state was allocated for the rejected stream (checked via a
  daemon-internal test hook on the test build, the same pattern
  `internal/transcribe/chain_test.go` uses to inspect
  `ChainedTranscriber`'s post-call state).
- `TestVoiceStream_StreamIDScopedToOneSession` — a second, unrelated
  `session_id`'s credential cannot `send` or `close` the first stream
  (Failure mode F10's positive assertion), and the first stream's
  `session_id` is unchanged by any `frames`/`send`/`close` call after the
  `POST /api/voice/stream` that created it (asserted by comparing the
  snapshot endpoint's `session_id` field before/after).

**2. Integration tests — real windowing, real backend where available.**
- `TestVoiceStream_RollingWindowStabilizesAcrossTwoDecodes` (analogous in
  spirit to `TestLongAudio_LocalWhisper_65sCoverage` in
  `internal/server/long_audio_test.go`): synthesize a fixed, known
  speech-like waveform (the same synthesized-audio approach
  `TestLongAudio_WavTranscodePipeline` in
  `internal/transcribe/long_audio_test.go` uses, but split into the
  5 s frames the streaming path expects), with `stabilize_decodes=2` and
  the real `base` Whisper venv if one is available (guarded by the same
  `TestMain`-style venv discovery the existing tests use), assert:
  (a) the first `commit` event arrives within ~3 s of the first voiced
  frame (the near-live goal's testable core, wall-clock-asserted with a
  generous 2× margin like the existing tests' timeouts), (b) every word in
  the final `final_stable` was present in at least two consecutive decodes
  of overlapping windows (the test inspects the daemon's per-decision
  audit log, a new `t.Logf`-level tracing hook behind a test-only flag, not
  a production feature), and (c) zero `revise` events on this controlled
  input (a known-stable synthesized waveform should never trigger a
  revision; if one does, the test fails and the trace shows exactly which
  window disagreed — this is the "the stabilization rule should be
  near-deterministic on a known input" check the whole Chunking and
  windowing section depends on).
- `TestVoiceStream_FallsBackToOfflineLegOnPrimaryFailure` (analogous to
  `TestLongAudio_SplitFallbackOnShortBackend` in
  `internal/server/long_audio_test.go`): stand up the same pair of mock
  transcribers `ChainedTranscriber` uses in its own tests, make the
  streaming leg fail every Nth decode window, assert the stream never
  closes (a per-window failure is F2's "retried=true,
  stream_still_open=true" case), the committed buffer is exactly the
  pre-failure committed text plus the post-failure commits (no lost
  commits, no phantom commits from the failing window), and the snapshot
  endpoint's `backend` field reflects the fallback leg for the duration of
  the fallback.
- `TestVoiceStream_IdleTimeoutAutoClose` — open a stream with
  `keep_alive=false` and the default `idle_timeout_seconds=20`, feed a
  voiced frame then feed nothing for `idle_timeout_seconds`+margin, assert
  a `close` event with `reason=idle_timeout` arrives and the
  transcript remains available via the snapshot endpoint for
  `transcript_retain_seconds` (the test uses a short test-value
  for both, say 60 s and 120 s, scaled down proportionally rather than
  waiting on the real 20 s/300 s defaults, keeping the test fast while
  exercising the same code path the defaults exercise in production).

**3. PWA tests.** New `internal/server/web/app_voice_stream.test.mjs`
(same naming pattern as the existing `*.test.mjs` files in
`internal/server/web/`, run under `node --test` exactly as
`internal/server/web/app_voice_input.test.mjs` — the long-recording
fix's plan
(`docs/plans/2026-10-20-voice-long-recording-rootcause.md`) references —
is run, per AGENT.md's PWA testing rule). Coverage:
- The existing `toggleVoiceInput` / `startGenericVoiceInput`
  code path in `internal/server/web/app.js` is refactored to a shared
  "start recording" helper that takes a "transport strategy" (the old
  one-shot `POST /api/voice/transcribe`, the new streaming
  `POST /api/voice/stream*` set) and the test asserts both strategies go
  through it, with a fake `MediaRecorder` + fake `fetch` injected the
  same way the existing `app_voice_input.test.mjs`-style tests do (per
  AGENT.md's guidance to test the existing helper's shape rather than
  reaching into `state.voice.recorder` internals for this new branch).
- The overlay's **Send** button, under the streaming strategy, calls
  `send` on the stream (not the one-shot endpoint), and under the
  one-shot strategy still calls `POST /api/voice/transcribe` — asserted
  by counting calls to a stubbed `fetch` and inspecting the
  `sessionInput` field's value afterwards (the "editable before it
  leaves the machine" invariant, in the browser's own terms, per the
  existing overlay's behavior in
  `internal/server/web/app.js`).
- The "send stable prefix, keep dictating" affordance (a new button in
  the overlay, `voiceRecordingSendPrefix`, sibling to the existing
  `voiceRecordingCancel`/`voiceRecordingSend` in
  `internal/server/web/app.js`) inserts the committed text into
  `sessionInput`, submits it, and — critically — does NOT close the
  stream (asserted by the stubbed stream's `open` state after the
  click); "Cancel" still closes it (asserted the same way).

**4. CLI tests.** `datawatch voice stream --file <synthesized.wav>` against
  the test daemon (the same `httptest`-backed daemon the Go integration
  tests use), asserting committed text appears on stdout before the
  process exits (the "streaming is observable as a live process, not just
  a final result" check, which is the single most user-visible guarantee
  of this whole feature and the one most at risk of being broken by a
  test that only checks the exit status — so it's listed under section 4
  but is the *first* test a developer runs by hand, per AGENT.md's
  Testing Tracker expectation to reproduce locally before trusting the
  automated suite).

**5. Failure-mode tests.** One per F-code (F1–F12 above), each a small
test in `internal/server/voice_stream_test.go` for daemon-side behavior,
or in the PWA test file for client-side recovery, following the shape
of `TestLongAudio_OpenAICompat_BodySizeAndRetry` in
`internal/server/long_audio_test.go` (a controllable bad-behavior peer on
the other end of the connection under test, plus an assertion on the
*observed* client-side recovery, not just the daemon's own bookkeeping).
The most important of these — F1 (dropped audio frame), F2 (streaming-leg
mid-stream failure), F3 (daemon restart), F6 (client disconnect) and F9
(`ffmpeg` per-frame failure) — are the ones an operator is most likely to
actually hit in the field, and they also map 1:1 onto the failure modes
listed in the long-recording fix's own failure-surface section in
`docs/plans/2026-10-20-voice-long-recording-rootcause.md`, so the two
fixes' test suites deliberately share a naming convention
(`TestVoiceStream_{F-mode}`) so a future operator can grep one name
pattern and find the coverage for *both* the old one-shot bug class and
the new streaming bug class in one pass.

## Rollout phases

Phases are ordered so that no operator-facing surface is ever *less*
functional than today before it is *more* functional — each phase is
either a pure addition of unused code, or a gate-controlled flip that
can be reverted by setting one config key back to its default (which is
always "off"), so rolling back any phase is a `datawatch config set` +
`datawatch reload`, never a hotfix:

**Phase 0 — Baseline hardening (no new operator-facing behavior).**
Ship the config key surface (`whisper.streaming.*`, all default-off, per
the Configuration section), the `voiceTranscriber`-style
`transcribeSurface` seam extended so a `streaming` leg is
*representable* in `internal/transcribe/chain.go` (a new
`StreamingLeg` interface alongside the existing `TranscriptionFunc`
leg, so `ChainedTranscriber` gains a third, initially-unused slot), and
the spool-file infrastructure from Failure mode F3 as a standalone,
already-tested unit (the "write a committed buffer to
`~/.datawatch/voice-streams/{id}.committed` on every commit/revise"
helper, with its own test, before any endpoint actually calls it). This
phase adds no new HTTP endpoint and no new PWA button, so its rollback
is simply "don't merge it" — it cannot break an existing operator.
The Testing Tracker (per AGENT.md's Tracking Rule
in `docs/testing-tracker.md`) gets its first entry: the new
`internal/server/voice_stream_test.go` unit-test file from section 1 of
the Test plan, all passing.

**Phase 1 — Daemon endpoints, no client yet.** Wire the six
`/api/voice/stream*` endpoints (Transport section) against the Phase-0
seam, gated on `streaming.enabled: true` (default `false`, so an
operator who never touches the config key sees exactly today's daemon).
Ship the CLI's `datawatch voice stream` command (Test plan section 4)
*disabled by default in the CLI's own feature flag* but present, so an
operator with a dev build can exercise it before the PWA or mobile apps
ever do — this is deliberately the *first* surface an operator can
actually touch, because it's the fastest to test by hand (per the Test
plan's own ordering rationale) and the cheapest to roll back (`datawatch
voice stream --help` simply won't list the subcommand on a config where
`streaming.enabled` is off, matching the
`datawatch config set`/`reload`-driven feature-
visibility posture the existing `whisper` config block already implies
in `docs/howto/voice-input.md`). Rollback: set `streaming.enabled: false`,
reload; the endpoints 404 and the CLI subcommand disappears, and nothing
else in the daemon is touched because Phase 0 already made the
streaming leg a third, independently-validated slot rather than a
rewrite of the two existing legs.

**Phase 2 — PWA live capture + quick send.** Add the `voiceRecordingSendPrefix`
button and the streaming transport strategy to `toggleVoiceInput`/
`startGenericVoiceInput` in `internal/server/web/app.js`, behind the
*same* `streaming.enabled` flag (the PWA checks
`GET /api/voice/stream`-equivalent capability discovery once at startup;
if the daemon 404s it, the PWA silently uses the one-shot strategy
exactly as it does today — so a PWA ahead of its daemon degrades to
today's behavior, and a daemon ahead of its PWA is simply unused,
covering both mismatch orderings, per Failure mode F11). The PWA test
plan (section 3) ships with this phase. Rollback: operator
re-clicking the record button always gets the one-shot overlay first
(there's no separate "streaming mode" toggle *in the PWA itself* — the
PWA picks the strategy by probing the daemon once, so the PWA needs no
config of its own, and rolling back is a daemon-side config change
affecting all future sessions, matching the existing
`whisper.*` reload semantics in
`docs/howto/voice-input.md`).

**Phase 3 — Mobile (Android, iOS) + comm-channel corrections.** Same
REST endpoints, platform-native event transport per the Parity surface section; the
`(correction)` follow-up message behavior in
`internal/router/router.go` ships here too (comm channels were never
blocked on Phases 1–2, they just didn't have a live-streaming leg to
correction-flow into until now). Mobile-Parity Rule (AGENT.md) requires
both apps land in the same release wave, and both apps' build/test
pipelines (`datawatch-app`'s Compose Multiplatform setup, per AGENT.md's
project structure) get their `toggleVoiceInput`-equivalent test updates
in that wave. Rollback: same config key, same reload; the mobile apps
probe the daemon exactly like the PWA does, so a rolled-back daemon
silently reverts every app to one-shot mode with no app update needed.

**Phase 4 — MCP tools + docs + operator-facing enablement.** The
`voice_stream_*` MCP tool family (Parity surface, MCP row), the
`docs/config-reference.yaml` and `docs/howto/voice-input.md` updates
(a new "Streaming" section, parallel to the existing howto's
"Configuration/Verification" sections, documenting
`streaming.enabled`, the per-surface behavior table from Surface
parity, and the recommended
`datawatch config set whisper.streaming.enabled true` +
`datawatch voice stream --help` first-run smoke), and — only after all
four prior phases' rollback gates have been exercised at least once in
a staging environment — the *announcement* of the feature to operators,
which is what actually flips `streaming.enabled: true` in anyone's
production config for the first time. This ordering (docs + announcement
last, not first) is deliberate: the config key has existed since
Phase 0, the endpoints since Phase 1, but no operator is told about it
until Phase 4, so the blast radius of any Phase 1–3 bug is bounded by
"only operators who explicitly opted in during testing", which is the
same blast-radius control the existing `whisper.*` feature
config-gating (see `docs/config-reference.yaml`) has always
provided, just applied one explicit "announcement" phase later instead
of all at once.

**Per-phase gate (must all be true before the next phase starts).**
(1) All tests in the Test plan's sections 1–2 (unit + integration,
daemon-side) pass on at least one CPU-only reference machine and one
GPU reference machine in CI (mirroring the two-hardware-class coverage
the Long Recording Fix's Validation section in
`docs/plans/2026-10-20-voice-long-recording-rootcause.md` requires);
(2) the rollback procedure for that phase was exercised at least once
end-to-end (set the key, reload, confirm the
operator-facing surface
matches the previous phase exactly, confirm
the one-shot path in
`internal/server/voice.go`
is byte-for-byte
unaffected — checked by running the existing
`TestVoice_*` suite from
`internal/server/long_audio_test.go`
unmodified, which must pass identically before and after the rollback,
proving the streaming work never altered the shared one-shot code
path);
(3) the AGENT.md checklist items relevant to that phase (Testing Tracker
entry added if new endpoints shipped, Mobile-Parity Rule issue filed if
the PWA or either mobile app was touched, the "no breaking changes to
existing API" clause from AGENT.md's compatibility rules verified by a
diff of `internal/server/router.go`'s route table showing the new
routes are purely additive) are all completed and linked in the
phase's rollout checklist in `docs/testing-tracker.md`.
