# BL448 — voice transcription fails on long recordings: reproduction and root cause

**Date:** 2026-10-19 · **Status:** Reproduced: yes · **Root cause: 300 s context deadline (SIGKILL), not a whisper bug and not OOM**

> **Recovered 2026-11-26** from a branch that was never merged. A dated record of the fault before
> it was fixed: main now transcribes long clips in chunks and sizes the deadline from the clip length
> (see `internal/transcribe/chunked.go` and `whisper.timeout_seconds`). Line numbers are those of
> 2026-10-19. Local paths are replaced by placeholders, and the `scratch/` harness and logs it names
> were not kept.

Operator record: voice message transcription fails when processing exceeds roughly 30
seconds; backend is openai-whisper `medium` on CPU.

---

## Repro setup

- **Host:** the daemon host, whisper from the datawatch Python environment (`<venv>`)
  (Python 3.14, `openai.whisper` in site-packages), model `medium`, `device='cpu'`,
  `fp16=False`, language `en` — identical values to what `cmd/datawatch/main.go` builds via
  `transcribe.New(venv, model, language)`.
- **Path exercised (option a from the task):** a Go harness, `scratch/bl448/repro_main.go`,
  that instantiates the exact production transcriber (`transcribe.New` +
  `Transcribe(ctx, path)`) and runs each clip under
  `context.WithTimeout(context.Background(), 300*time.Second)` — the same value
  `handleVoiceTranscribe` applies at `internal/server/voice.go:122`
  (`5*time.Minute`). For every clip it captured verbatim: wall-clock time, the Go error
  string (the handler would prefix it with `transcribe: ` and answer HTTP 502), and the
  subprocess CombinedOutput text.
- **Control (proves the kill source):** `scratch/bl448/raw_run.py` run directly,
  `time timeout -s KILL 900 <venv>/bin/python3 raw_run.py audio/speech240.wav`
  — same venv/model/clip, **no Go context**, 900 s hard guard so only a true hang would kill it.
- **Clips** (`scratch/bl448/audio/`, all mono 16-bit 16 kHz WAV):
  | file | length | method |
  |------|--------|--------|
  | `clip40.wav`, `clip90.wav`, `clip180.wav` | 40/90/180 s | synthesized pink noise (sanity check only — noise does not load whisper's decoder path; transcription returned ~empty text in 7–11 s) |
  | `speech40.wav` (1.28 MB) | 40 s | repeated real speech (JFK "Ask not…" clip looped) |
  | `speech90.wav` (2.88 MB) | 90 s | same |
  | `speech180.wav` (5.76 MB) | 3 min | same |
  | `speech240.wav` (7.68 MB) | 4 min | same — the failing clip |

  Real speech is essential to load the decoder; the synthetic-noise clips transcribe near
  instantly and were kept only to confirm the pipeline works end to end.

## Evidence

### Run matrix (Go harness, 300 s context — the production `handleVoiceTranscribe` budget)

| clip | audio len | wall clock | result |
|------|-----------|-----------|--------|
| `speech40` | 40 s | **46.13 s** | OK — transcript correct |
| `speech90` | 90 s | **2 m 9.558 s** | OK — transcript correct |
| `speech180` | 180 s | **4 m 50.714 s** | OK — transcript correct |
| `speech240` | 240 s | **301 s** | **FAIL** — Go error below |
| `speech90` (re-run while a 240 s transcription was in flight) | 90 s | **302 s** | **FAIL** — same Go error (CPU contention pushed a 90 s clip past the limit) |

### Verbatim Go error string (exactly what `Transcribe` returns; handler sends HTTP 502 `transcribe: <this>`)

```
transcribe: whisper failed: signal: killed
output: <venv>/lib/python3.14/site-packages/whisper/transcribe.py:130: UserWarning: Performing inference on CPU when CUDA is available
  warnings.warn("Performing inference on CPU when CUDA is available")
<python-lib>/multiprocessing/resource_tracker.py:396: UserWarning: resource_tracker: There appear to be 1 leaked semaphore objects to clean up at shutdown: {'<semaphore>'}
  warnings.warn(
```
(exit code reported by Go: `signal: killed`, i.e. SIGKILL; harness elapsed=301 s / 302 s)

The same text in both failures (speech240.log, slow90.log). Whisper's **stderr is only
UserWarnings — no Python traceback in any run**, i.e. the process never raised; it was
killed externally.

### Decisive control run: same clip, same venv/model, **no Go context** (900 s guard)

```
	Command being timed: "timeout -s KILL 900 <venv>/bin/python3 raw_run.py audio/speech240.wav"
	User time (seconds): 7607.08
	System time (seconds): 85.70
	Percent of CPU this job got: 2212%
	Elapsed (wall clock) time (h:mm:ss or m:ss): 5:47.65          ← 347.65 s, past the 301 s kill point
	Maximum resident set size (kbytes): 5056560                    ← ~4.8 GiB peak RSS
	Swaps: 0                                                       ← no memory pressure
	Signals delivered: 0                                           ← no SIGKILL/SIGSEGV
	Exit status: 0                                                 ← SUCCESS
```

stderr for this run: the same two UserWarnings, no traceback → `RAW_TRANSCRIPT_LEN:` printed
(normal completion). **The identical 240 s clip that dies at 301 s under the Go context
finishes in 347.65 s with exit 0 when the only change is removing the 300 s deadline.**

### Code under test

- `internal/server/voice.go:122` (handleVoiceTranscribe): `ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)` — this ctx is **shared with the ffmpeg transcode before whisper**, so effective inference budget is just under 300 s.
- `internal/transcribe/transcribe.go:96`: `cmd := exec.CommandContext(ctx, w.Python, "-c", whisperScript, ...)` — per the os/exec docs, cancelling `ctx` after it is created kills the process tree; the default kill signal is `os.Kill` (SIGKILL). `CombinedOutput` then reports `exit status: signal: killed` → wrapped as `transcribe: whisper failed: %w\noutput: %s`.
- `internal/transcribe/transcribe.go:82`: nil-ctx default is the same `5*time.Minute`.
- `internal/server/voice.go:196` (handleVoiceTest): `3*time.Minute` — same latent mechanism, smaller budget.
- Client side (`internal/server/web/app.js` lines 5712–5713, 5916–5917, 15546): plain
  `fetch('/api/voice/transcribe', …)` with **no AbortController** — the browser waits
  indefinitely; there is no 30 s client timeout. No 30–60 s reverse-proxy read timeout exists
  anywhere in this repo's configs (the only nginx conf, `testdata/mock-opencode/nginx.conf`,
  is a test mock for opencode, not the datawatch deployment).

## Root cause

**The hardcoded 5-minute (300 s) context deadline in `handleVoiceTranscribe`
(`internal/server/voice.go:122`) expires during whisper inference and Go's
`exec.CommandContext` sends `os.Kill` (SIGKILL) to the whisper Python child. Failure
threshold ≈ any clip whose CPU inference time exceeds 300 s wall-clock.**

Mechanism, end to end:

1. Operator sends a voice message long enough (or concurrent transcriptions load the CPU
   enough) that `medium`-on-CPU inference needs > 300 s.
2. At t = 300 s the `context.WithTimeout` derived from `r.Context()` cancels.
3. `os/exec` (command started via `exec.CommandContext`) kills the python process —
   `signal: killed`. No python exception, no partial transcript, no traceback; the
   `whisper-result-*.txt` output file is only written at the very end, so nothing is
   recoverable.
4. `Transcribe` returns `transcribe: whisper failed: signal: killed\noutput: <UserWarnings>`;
   `handleVoiceTranscribe` answers **HTTP 502** `transcribe: whisper failed: signal: killed…`.

**A timeout value IS the cause — specifically the 5-minute value at `internal/server/voice.go:122`
(and the identical 5-minute default fallback at `internal/transcribe/transcribe.go:82`).**
Not a whisper exception (stderr shows only warnings in every run). Not OOM: peak RSS
~4.8 GiB, `Swaps: 0`, and the control run exited 0 on the same clip. The 3-minute value in
`handleVoiceTest` is the same hazard class but only matters for the availability probe.

Relation to the operator's "~30 seconds": on **this** host `medium`/CPU runs ~1.2–1.6×
realtime on real speech (40→46 s, 90→130 s, 180→291 s, 240→348 s), so the failure point
here is ~300 s of processing (~240 s+ of audio, or much less under CPU contention — the
contention run killed a 90 s clip). A production failure point near 30 s is consistent with
the same 300 s ceiling observed from the *request-latency* side on a machine whose CPU is
roughly an order of magnitude slower than this host; the mechanism (deadline → SIGKILL) is
hardware-independent and is what the code deterministically enforces.

## Confidence / open questions

- **High** (proven by A/B on this host): the kill is the 300 s context deadline; same clip,
  same venv/model — dies at 301 s with it, succeeds at 347.65 s / exit 0 without it.
- **High**: not OOM (4.8 GiB peak RSS, 0 swaps, exit 0 control), not a whisper exception
  (UserWarnings only, no traceback in any captured stderr).
- **High**: no 30 s/60 s client or in-repo proxy timeout exists to explain the operator's
  figure; the PWA fetch has no AbortController.
- **Open**: the production machine's inference rate was not measured (out of scope for this
  work). If production fails at ~30 s of processing,
  confirm on the production host: run `raw_run.py` on the operator's actual failed
  recording and time it; expect ~30 s×N where N is the slowdown factor vs. this host.
- **Open (risk amplifier, observed live)**: concurrent transcriptions share one CPU — two
  whisper `medium` processes pushed even a 90 s clip past 300 s. There is no per-caller
  concurrency limit or queue for voice transcription.
- Minor: the error string `signal: killed` leaks no timing context to the operator; it is
  indistinguishable from an external kill without the harness evidence above.

## Recommended next step for the fix

1. **Stop binding transcription to request lifetime**: make `handleVoiceTranscribe`
   enqueue the clip onto a server-side job (or worker) and return **202 + job id**; expose
   `GET /api/voice/transcribe/progress?job=` for the PWA to poll (the PWA already polls
   nothing and its fetch has no AbortController, so a long-lived HTTP response is the
   fragile piece). This removes any fixed deadline from the critical path entirely.
   If keeping the synchronous API, at minimum make the deadline a **configurable
   `transcribe.timeout_seconds`** (default far above worst-case, e.g. 20 min) instead of
   the hardcoded `5*time.Minute` at `internal/server/voice.go:122` and the identical
   default at `internal/transcribe/transcribe.go:82` — and exclude the ffmpeg transcode
   phase from the inference budget.
2. **Surface a real error**: in `Transcribe`, detect `ctx.Err() == context.DeadlineExceeded`
   and return `transcribe: timed out after 300s (audio longer/larger than budget; model=medium)`
   instead of the misleading `signal: killed`, and include the clip length and elapsed time.
3. **Bound concurrency**: serialize (or cap at N) whisper subprocesses on the daemon so one
   long clip cannot push shorter clips past the deadline (contention failure reproduced
   above), and log per-transcription wall time to make the rate measurable in production.
4. **Verify on production**: time one real operator-failed recording with `raw_run.py`
   style (no ctx) to measure that host's inference rate and confirm the 300 s ceiling is
   what was crossed.

---

### Artifacts (all under `scratch/bl448/`, temporary, no source files modified)

- `repro_main.go` — Go harness (production `transcribe.New` + 300 s ctx)
- `raw_run.py` — whisper-only control driver; `raw240.stderr` — its `time` output (exit 0 @ 347.65 s)
- `out/speech{40,90,180,240}.log`, `out/slow90.log`, `out/clip180.log` — per-run verbatim logs
- `audio/*.wav` — generated clips
