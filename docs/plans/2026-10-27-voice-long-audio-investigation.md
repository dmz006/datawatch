# Voice long-audio (>30 s) transcription failure — investigation

Date: 2026-10-27 · Investigated on host `local-node` (48-core AMD64, 125 GB RAM), live daemon data at `~/.datawatch`
Related (prior, already fixed): `docs/plans/2026-10-20-voice-long-recording-rootcause.md` (15 s `ReadTimeout` upload abort; windowed `openai_compat` backend 30 s input limit). Both fixes are in tree.

## Symptom

- Operator-reported 2026-10-15: voice transcription fails for recordings longer than ~30 s; recordings under 30 s succeed.
- Reproduced per operator report on both the web PWA and the mobile app, on a daemon with live config `voice: backend=whisper, model=medium, language=en` (see `[voice] enabled (backend=whisper, model=medium, language=en)` lines in `~/.datawatch/daemon.log`).
- Failure is user-visible as "transcription broken / no transcript" (exact client-side text not available in retained logs).
- No source-code change is part of this investigation; this document records what was checked, what reproduced, what it did not, and what remains unknown.

## Reproduction

Attempted on 2026-10-27, on the **exact** live stack:

- Transcriber: `transcribe.WhisperTranscriber` (`internal/transcribe/transcribe.go`) — the real one, same `venv_path`, `model`, `language` as the live daemon.
- Invocation: throwaway Go harness `go run ./.voice-harness` (kept out of the repo: `.voice-harness/` is in `.git/info/exclude`; file `main.go` documents itself as throwaway). The harness calls `w.Transcribe(ctx, path)` with a 5-minute context — identical ceiling to `handleVoiceTranscribe` in `internal/server/voice.go`.
- Test clips: 30 s / 60 s / 120 s / 180 s, 16 kHz mono PCM WAV, generated with ffmpeg (no TTS tooling — espeak/flite/pico2wave — exists on this host; clips are a 440 Hz tone, i.e. non-speech. Regenerate with `ffmpeg -f lavfi -i "sine=frequency=440:duration=30" -ac 1 -ar 16000 out.wav`).
- venv: `~/.datawatch/.venv` — `whisper 20250625`, `python 3.14`, model files cached in `~/.cache/whisper`.
- Memory context at run time: 125 GB total, 59 GB available.

**Result: no failure at any clip length.**

| clip | result | wall time (incl. model load) |
|---|---|---|
| 30 s  | OK, transcript `"you"` (tone → near-empty, expected) | 6.3 s |
| 60 s  | OK, transcript `"you"` | 7.3 s |
| 120 s | OK, transcript `""` | 9.5 s |
| 180 s | OK, transcript `""` | 12.3 s |

Direct OS-level probe of the 180 s clip (`/usr/bin/time -v <python> -c <whisper-script>`, the child the Go transcriber launches): **Exit status 0**, no signal, `Signals delivered: 0`, peak RSS 5,060,348 kB (~4.8 GiB), no Python traceback (only the benign `UserWarning: Performing inference on CPU when CUDA is available`).

So, on this host with the current tree and current venv, whisper `medium` handles 180 s of audio in ~10 s with zero OOM headroom risk. The >30 s failure did **not** reproduce here.

## Evidence

### 1. Reproduction data (above)
No error text, exit code was 0, no traceback, no kill, peak RSS 4.8 GiB on a 125 GB host. The Go-side `Transcribe` path returns success for every tested length. Whatever failed in the field did not fail here under the identical code + model + venv.

### 2. Daemon logs around the 2026-10-15 incident (this host)
- `~/.datawatch/daemon.log`: 75 `[voice] …` lines, **all** `[voice] enabled (…)`; zero `[voice]` error/warn lines (no `transcribe failed (502)`, no `signal: killed`, no `context deadline exceeded`).
- `~/.datawatch/access.log`: every `/api/voice/transcribe` request — 248 total, spanning 2026-10-15 → 2026-10-21, including the Oct-15 window (01:24–01:28 and 07:34–08:14 ET, Android/Chrome mobile UA) — has `"status":200`. **No 4xx/5xx on the voice path at all.**
- `~/.datawatch/daemon-crash.log`: no voice/whisper entries.
- `journalctl` (this host): Oct 15 06:07:19 `systemd … OOM killer killed some processes in … user-1000.slice` — a genuine OOM-killer activation on the incident day. The corresponding kernel `Out of memory: Killed process … (…)` line is not in retained journal, so the killed process name is **unknown**. (Retained kernel OOM lines near that window are from Oct 13–14 and are about `git` session-repo operations, unrelated to voice.)
- `~/.datawatch/config.yaml.bak-pi64k` (a different machine's backup config): no `voice:` section at all — voice config was not set there.

Implication: the daemon instance *on this host* never logged a voice-transcription failure, and its access log shows every transcription request on Oct 15 succeeding with 200. If the operator experienced failures on Oct 15, they either (a) happened on a different daemon instance/host whose logs are not here, or (b) were client-side perception of a request the operator considers "failed" — but (b) is inconsistent with the "no transcript" description given server-side 200s.

### 3. Code inspection of all three candidate timeout layers
- `internal/transcribe/transcribe.go`: `exec.CommandContext(ctx, python, "-c", whisperScript)` + `CombinedOutput()`. No built-in timeout; the ctx passed in governs. A context deadline here surfaces as Go error `context deadline exceeded`; an OOM kill surfaces as `signal: killed`. Whisper's own script has no length guard.
- `internal/server/voice.go`: upload `ReadTimeout` 180 s; `MaxBytesReader` 25 MB; transcribe ctx **5 minutes** (production endpoint `handleVoiceTranscribe`) and **3 minutes** (test endpoint `handleVoiceTest`); ffmpeg transcode of webm/m4a before transcription (error paths distinct: "transcode" vs "transcribe").
- `internal/server/web/app.js`: three `fetch('/api/voice/transcribe', …)` sites (≈lines 6972, 7176, 17919) — bare `fetch`, **no** `AbortController`, **no** client-side timeout.

## Root cause

**Not conclusively determined from available evidence.** What the evidence does establish:

1. It is **not** reproducible on the current tree/venv/host, at any length up to 180 s, on the exact production code path.
2. The Oct-15 incident day's daemon logs on this host show **no failure** (all 200, no `[voice]` error lines) — so the reported failures were not produced by this daemon instance, or the failure signature was never logged (e.g. daemon died mid-request, whose only trace would be a connection reset on the client, or a client-side error before the request completed).
3. There **was** an OOM-killer activation on the incident day (2026-10-15 06:07:19, user-1000.slice) on this host; the killed process is unknown.

Ranked hypotheses, each consistent with the >30 s boundary:

- **(H1, strongest) OOM kill of the whisper subprocess on the field host.** Whisper `medium` peaks at ~4.8 GiB RSS even for 180 s of trivial audio on a big host, and **model load is ~1.5 GiB of weights alone regardless of clip length**. On a RAM-constrained daemon host, the kernel OOM-kills the python child exactly once it crosses the machine's budget — which grows with clip length because whisper's memory scales with the number of 30 s windows it holds. That is the only mechanism in this stack that (a) produces a failure, (b) is length-dependent near ~30 s on a memory-tight box, and (c) does not leave a Python traceback. The Go-side error text would be `transcribe: whisper failed: signal: killed` — and the handler's 502 body would say exactly that. This is **not** confirmable from retained logs (the kill line is not in journal; the Oct-15 daemon access log shows 200s, which points at a different daemon host being the one that actually served the failing requests).
- **(H2) Context-timeout on a slow (few-core) host.** Transcription time scales with clip length (windows of 30 s). On a 1–2 core ARM host, whisper `medium` decode of a 2–3 min clip can exceed the handler's 3-min (test endpoint) or 5-min (production) ctx, surfacing as `context deadline exceeded` → 502. Same "works ≤30 s, fails above" signature, only visible on underpowered hosts. Also not confirmable without the field host's daemon log.
- **(H3) Client-side display layer** (PWA/mobile app shows the 200 response as failed) — contradicted by the operator's "no transcript" description and by the server 200s; weakest.

The **distinguishing evidence** (H1 vs H2) is a one-line difference in the daemon log at failure time — `signal: killed` (H1) vs `context deadline exceeded` (H2) — and that line is not present in any retained log I can see. If the operator can point at the field daemon host's `daemon.log` around 2026-10-15, the root cause resolves to a definitive line.

## Ruled out

- **15-second `ReadTimeout` aborting >~200 KB uploads** — was real, was the documented 2026-10-20 root cause, **already fixed** to 180 s (verified in `voice.go`). Not the current bug.
- **`ReadTimeout` in general** — current value 180 s; a 3-min mobile webm/opus clip is well under 25 MB and well under 180 s to upload on any realistic link. Ruled out.
- **25 MB `MaxBytesReader` cap** — 3-min opus-at-16 kHz ≈ 0.5–1 MB. Ruled out for the reported clip lengths.
- **Client-side 30-second hard timeout** — `app.js` uses bare `fetch` at all three `/api/voice/transcribe` sites; no `AbortController`, no timeout, no `setTimeout`. Ruled out in code. (Browser network stack can still drop a stalled request after ~30 s of *inactivity* — but the server responds within seconds on this host, and the mobile case would then be browser-specific, which does not fit "both PWA and mobile".)
- **30-second windowed `openai_compat` backend** — prior doc's second root cause; **no `openai_compat` backend exists in the live config** (`voice.backend=whisper`), so it cannot be the current bug.
- **Local (this host's) OOM during transcription** — 125 GB RAM, peak RSS 4.8 GiB, 59 GB free at run time. Ruled out *on this host*. (Does **not** rule out OOM on a constrained field host — that is H1.)
- **ffmpeg transcode failure on long clips** — no `[voice]`/transcode error in daemon log; transcode path is separate from the whisper call and would log a distinct error. No evidence of it. Ruled out for absence of evidence, not for correctness.
- **whisper `medium` model load failure on long clips** — load is length-independent; observed working repeatedly in this session. Ruled out.
- **whisper `medium` CPU-arch / venv incompatibility** — `whisper 20250625` under `python 3.14` in the exact live venv runs and produces output (garbage on a tone, as expected) at all tested lengths. Ruled out for this stack.

## Proposed fix

No source change in this task. Ordered by cost/benefit:

1. **Get the field daemon's log line from around 2026-10-15** (`daemon.log` grep `transcribe failed`). One line resolves H1 vs H2 vs "never failed server-side". Ask the operator which host served the failing requests; the Oct-15 access log on *this* host is all 200.
2. **Raise voice-transcription visibility cheaply**: in `handleVoiceTranscribe` / `handleVoiceTest`, when the transcriber returns an error, include clip duration, model, name, and (when available) the child's peak RSS in the 502 body and `[voice]` log line. This turns H1/H2 from "one line in the log" into "one line in the log **that says which**". ~10 lines of Go.
3. **Add a client-visible, operator-actionable error string** for the two expected failure modes: `signal: killed` → "host out of memory — reduce `voice.model` to `small` or `base`"; `context deadline exceeded` → "transcription timed out — reduce `voice.model` or clip length". No behavior change, just wording.
4. **Document the memory envelope** in the voice config reference: `medium` ≈ 4.8 GiB RSS even on 180 s of trivial audio + 1.5 GiB of fixed weight load. Any field daemon on ≤ 8 GiB RAM is one long clip away from the kernel OOM killer. Recommend `small` or `base` for constrained hosts.
5. **(Deferred, if H2 is confirmed)** Align the test endpoint's 3-min ctx with the production 5-min, or better, make both a single operator-configurable value.
6. **(Deferred, if H1 is confirmed)** Two viable follow-ups: (a) `exec.CommandContext` with a per-model RSS budget check before spawn, refused with a clear 502 instead of an OOM kill; (b) switch the default whisper model to `small` on RAM-constrained hosts and let operators opt into `medium`.

## Unknowns / gaps to close before declaring done

- **Which daemon host actually served the Oct-15 failing requests** — its `daemon.log` is the single missing artifact. Everything on *this* host contradicts the reported failure.
- **The exact client-side error string** the operator saw on PWA and mobile ("no transcript" is the description; the UI's wording would confirm server-502 vs client-error).
- **Tone-vs-speech timing**: test clips are a 440 Hz tone (no TTS tooling on the host). Whisper's decoder can be pathologically slow on specific speech content even when a tone of the same length is fast; a real-speech 180 s clip on the *field* host was not testable here because TTS is unavailable. This does not change the H1/H2 ranking (both are about length + host resources, not content) but it does mean "reproduction = no failure" is a *lower bound* on real-speech timing, not a proof.
- **The Oct-15 06:07:19 OOM kill on this host** is genuine but the killed process is not in retained journal. If that killed process *was* a whisper child, it would mean a different daemon on this same host failed that afternoon — but the access log says otherwise. Unresolvable from retained evidence.

---

## Appendix — reproduction harness (kept out of the repo)

`.voice-harness/main.go` (in `.git/info/exclude`, **not** part of the repo):

```go
// Run: go run ./.voice-harness  (from module root)
w, _ := transcribe.New(venv, "medium", "en")
for _, name := range []string{"speech30.wav","speech60.wav","speech120.wav","speech180.wav"} {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    text, err := w.Transcribe(ctx, filepath.Join(".voice-harness", name))
    cancel()
    // print name, err, elapsed
}
```

Clips: 16 kHz mono PCM WAV, 440 Hz tone, 30/60/120/180 s. Regenerate:
`ffmpeg -f lavfi -i "sine=frequency=440:duration=30" -ac 1 -ar 16000 speech30.wav` (etc.).

Harness and clips were removed/retained per the "throwaway" instruction; the Go file stays in `.voice-harness/` (excluded from git) for one re-run, and can be deleted after the follow-up in *Proposed fix* step 1 resolves the field-host question.

## Operator decisions (2026-10-27)

- About 12:30, operator, quoted: "Good the fix i will get an example in a few hours".
- About 12:35, operator, quoted: "Hold, not good". The supervisor had read the first line as putting the fix on hold; the operator wants the fix to go ahead. Decision: the fix is filed as a normal worktree PRD (eca00001) that hardens long-recording transcription (visible failure logging and clear errors, keep the recording or draft in the PWA on failure, bounded chunking with ffmpeg, tests, docs), and the operator's real failing example, when it arrives, is used to confirm it or to find the remaining cause.
