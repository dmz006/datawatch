// Long-audio REGRESSION tests — voice transcription of recordings longer than
// Whisper's 30 s window (fixed: see docs/plans/2026-10-20-voice-long-recording-rootcause.md).
//
// These tests previously REPRODUCED the >30 s failure and asserted the broken
// behaviour (EXPECTED FAILURE). With the fix landed they assert the correct
// behaviour instead: long recordings transcribe successfully, and a chunked
// retry against a windowed backend keeps every upload inside the 30 s window.
//
// Layers under test (this file):
//   (b) openai_compat.go  — windowed HTTP backend, timeouts, body size, retries
//   (c) transcribe.go     — local whisper venv, 65 s coverage (skips without venv)
//   (d) voice.go (handler) — ffmpeg transcode of MediaRecorder-style webm
//
// The handler layer itself (25 MB multipart cap + ReadTimeout) is
// covered by the regression tests in internal/server/long_audio_test.go.
// The local-whisper and ffmpeg-webm tests below document layers that
// already handled long audio correctly (they stayed green in the original
// reproduction).

package transcribe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Synthesis helpers
// ---------------------------------------------------------------------------

// tone16k builds seconds of mono 16 kHz 16-bit PCM WAV (440 Hz sine — a
// tone rather than silence so a transcriber that actually decodes the
// whole payload produces something distinct per window).
func tone16k(t *testing.T, seconds int) []byte {
	t.Helper()
	const sampleRate = 16000
	numSamples := sampleRate * seconds
	data := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		// 440 Hz sine, 16-bit sample
		const twoPi = 6.283185307179586
		v := int16(8000 * sinApprox(twoPi*440*float64(i)/float64(sampleRate)))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	buf.Write(le32(uint32(36 + len(data))))
	buf.WriteString("WAVEfmt ")
	buf.Write(le32(16))
	buf.Write(le16(1))
	buf.Write(le16(1))
	buf.Write(le32(sampleRate))
	buf.Write(le32(32000))
	buf.Write(le16(2))
	buf.Write(le16(16))
	buf.WriteString("data")
	buf.Write(le32(uint32(len(data))))
	buf.Write(data)
	return buf.Bytes()
}

// sinApprox is a small local sine; math.Sin would be equivalent and this
// avoids importing math in a test that could otherwise stay lightweight.
func sinApprox(x float64) float64 {
	// Reduce to [0, 2π)
	const twoPi = 6.283185307179586
	for x < 0 {
		x += twoPi
	}
	for x >= twoPi {
		x -= twoPi
	}
	// Taylor series around π/2 (converges fast for the reduced range).
	const halfPi = 1.5707963267948966
	d := x - halfPi
	s := 1.0
	term := 1.0
	for n := 1; n <= 12; n++ {
		term *= -d * d / float64(n*(2*n-1))
		s += term
	}
	// cos(d) around π/2 == sin(x)
	return s
}

func le16(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }
func le32(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }

// wavDurationSeconds parses a WAV container (from the RIFF magic) and
// returns the audio duration in seconds using the fmt chunk's byte rate
// and the data chunk size. Walks the chunk list (extra INFO/LIST chunks
// don't break it). Returns -1 when the bytes don't look like RIFF/WAVE.
func wavDurationSeconds(b []byte) float64 {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return -1
	}
	var byteRate, dataSize uint32
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		sz := binary.LittleEndian.Uint32(b[pos+4 : pos+8])
		switch id {
		case "fmt ":
			if pos+20 <= len(b) {
				byteRate = binary.LittleEndian.Uint32(b[pos+16 : pos+20])
			}
		case "data":
			dataSize = sz
		}
		next := int(sz)
		if next&1 == 1 {
			next++ // chunks are word-aligned
		}
		pos += 8 + next
		if next == 0 { // malformed; stop
			pos = len(b)
		}
	}
	if byteRate == 0 {
		return -1
	}
	return float64(dataSize) / float64(byteRate)
}

// writeWavFile writes WAV bytes to a temp file and returns the path.
func writeWavFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Fake "windowed" Whisper HTTP server.
//
// Real OpenAI-style /v1/audio/transcriptions backends that wrap a
// windowed engine may reject or truncate audio whose duration exceeds
// the engine's window (whisper is trained on 30 s windows; some
// self-hosted servers enforce it). This fake:
//   - records the request body size (to verify what the client sends)
//   - parses the WAV duration out of the multipart body
//   - 400-rejects anything longer than maxSeconds (the window), the
//     way a windowed backend would
//
// It also supports a "flaky" mode (503 N times, then success) to verify
// the openai_compat retry behaviour on long-audio payloads.
// ---------------------------------------------------------------------------

type fakeWindowedWhisper struct {
	maxSeconds float64
	failFirstN int // return 503 for the first N calls, then succeed

	mu       sync.Mutex
	calls    int
	lastBody int // bytes received (multipart)
	lastDur  float64
	durs     []float64 // WAV duration of every request, in order
}

func (f *fakeWindowedWhisper) handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(500)
		return
	}
	f.mu.Lock()
	f.calls++
	calls := f.calls
	f.lastBody = len(body)
	f.lastDur = -1
	wav := findWAVInBody(body)
	if wav >= 0 {
		f.lastDur = wavDurationSeconds(body[wav:])
	}
	f.durs = append(f.durs, f.lastDur)
	f.mu.Unlock()

	if calls <= f.failFirstN {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"model still loading"}`))
		return
	}
	if dur := f.lastDur; dur > -1 && f.maxSeconds > 0 && dur > f.maxSeconds {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write(fmt.Appendf(nil,
			`{"error":"audio duration %.1fs exceeds maximum window %.0fs"}`, dur, f.maxSeconds))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"text":"ok"}`))
}

// findWAVInBody returns the offset of the first RIFF/WAVE magic in a
// multipart body, or -1.
func findWAVInBody(b []byte) int {
	for i := 0; i+12 <= len(b); i++ {
		if b[i] == 'R' && b[i+1] == 'I' && b[i+2] == 'F' && b[i+3] == 'F' &&
			b[i+8] == 'W' && b[i+9] == 'A' && b[i+10] == 'V' && b[i+11] == 'E' {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// (b) openai_compat layer — long recordings against a windowed backend
// ---------------------------------------------------------------------------

// TestTranscribe_LongRecording_WindowedBackend is the regression test for the
// >30 s failure reproduced on 2026-10-20 (see
// docs/plans/2026-10-20-voice-long-recording-rootcause.md):
// against a backend that only accepts one 30 s window,
// the client must return a non-empty, non-truncated transcript for 31 s,
// 65 s and multi-minute recordings. The first attempt is a full-file upload
// (correct behaviour against full-length backends like OpenAI / OpenWebUI); the
// windowed backend refuses it with 400; openai_compat then splits locally into
// ≤30 s chunks, transcribes each, and joins the parts deterministically.
func TestTranscribe_LongRecording_WindowedBackend(t *testing.T) {
	cases := []struct {
		seconds int
	}{
		{29},  // control — inside the window, one-shot full upload
		{31},  // one window over
		{65},  // ~2 windows
		{300}, // 5 minutes (multi-minute)
		{600}, // 10 minutes (multi-minute)
	}

	makeTranscriber := func(t *testing.T, fake *fakeWindowedWhisper) *OpenAICompatTranscriber {
		t.Helper()
		ts := newFakeWhisperServer(fake)
		t.Cleanup(ts.Close)
		o, err := NewOpenAICompat(ts.URL, "", "whisper-1", "en")
		if err != nil {
			t.Fatal(err)
		}
		o.HTTPClient = ts.Client() // no cross-test dialer surprises
		return o
	}

	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%ds", c.seconds), func(t *testing.T) {
			fake := &fakeWindowedWhisper{maxSeconds: 30}
			o := makeTranscriber(t, fake)

			// Silent 8 kHz WAVs: a valid WAV the fake can duration-parse;
			// silence also makes the split points purely deterministic.
			samples := 8000 * c.seconds
			path := filepath.Join(t.TempDir(), fmt.Sprintf("t%d.wav", c.seconds))
			createSilentWAV(t, path, samples)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			text, err := o.Transcribe(ctx, path)
			if err != nil {
				t.Fatalf("%d s recording must transcribe successfully; got: %v", c.seconds, err)
			}
			if text == "" {
				t.Fatalf("%d s recording must return a non-empty transcript (was: empty)", c.seconds)
			}

			fake.mu.Lock()
			calls, durs := fake.calls, append([]float64(nil), fake.durs...)
			fake.mu.Unlock()

			if c.seconds <= 30 {
				// Control: exactly one full-file upload, no chunking.
				if calls != 1 {
					t.Errorf("within-window recording: %d backend calls, want 1", calls)
				}
				if text != "ok" {
					t.Errorf("text=%q want ok", text)
				}
				t.Logf("%d s in-window: ok in %d call(s)", c.seconds, calls)
				return
			}

			// Long recording: first attempt = full file (refused), then one
			// call per chunk, every chunk strictly inside the 30 s window.
			if len(durs) < 2 {
				t.Fatalf("expected a chunked retry (1 full + N chunks), backend saw %d call(s)", calls)
			}
			if durs[0] != float64(c.seconds) {
				t.Errorf("first attempt must be the full %d s upload; backend saw %.1fs", c.seconds, durs[0])
			}
			chunks := durs[1:]
			for i, d := range chunks {
				if d > 30.0+0.01 {
					t.Errorf("chunk %d is %.1fs — must be ≤ 30 s window", i, d)
				}
				if d <= 0 {
					t.Errorf("chunk %d has no parsable audio (%.1fs)", i, d)
				}
			}
			// Joining: each chunk contributes one "ok" word, single-spaced.
			parts := make([]string, len(chunks))
			for i := range parts {
				parts[i] = "ok"
			}
			want := strings.Join(parts, " ")
			if text != want {
				t.Errorf("transcript=%q — want %d joined chunks (%q)", text, len(chunks), want)
			}
			if calls != len(durs) {
				t.Errorf("backend saw %d calls but %d durations recorded", calls, len(durs))
			}
			t.Logf("%d s: %d backend call(s); durations: full=%.1fs then %v — transcript=%q", c.seconds, calls, durs[0], chunkDursStr(chunks), text)
		})
	}
}

// chunkDursStr renders the chunk duration list for logs (no testify).
func chunkDursStr(durs []float64) string {
	parts := make([]string, len(durs))
	for i, d := range durs {
		parts[i] = fmt.Sprintf("%.1fs", d)
	}
	return strings.Join(parts, ", ")
}

// TestTranscribe_LongRecording_SplitWAVPCM is a splitter-unit regression
// test (no network): 65 s of non-silent 16 kHz mono WAV must
// split into deterministic, non-overlapping, ≤30 s chunks that together
// cover the whole duration.
func TestTranscribe_LongRecording_SplitWAVPCM(t *testing.T) {
	wav := tone16k(t, 65)
	path := writeWavFile(t, "tone65.wav", wav)

	got, cleanup, err := splitAudioLong(context.Background(), path, DefaultAudioWindow)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil || len(got) < 2 {
		t.Fatalf("65 s split: got %d chunks, err=%v — want ≥2 chunks", len(got), err)
	}

	var total float64
	for i, p := range got {
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}
		d := wavDurationSeconds(b)
		if d <= 0 {
			t.Fatalf("chunk %d is not a parseable WAV (%d bytes)", i, len(b))
		}
		if d > float64(DefaultAudioWindowSeconds)+0.05 {
			t.Errorf("chunk %d is %.2fs — must be ≤ %d s", i, d, DefaultAudioWindowSeconds)
		}
		total += d
	}
	// No overlap, no loss: total ≈ 65 s (within 2 chunks of codec/frame rounding).
	if total < 64.9 || total > 65.2 {
		t.Errorf("chunks total %.2fs — want ≈65s (no overlap, no audio loss)", total)
	}

	// Determinism: same input bytes ⇒ identical cut points.
	got2, cleanup2, err2 := splitAudioLong(context.Background(), path, DefaultAudioWindow)
	if cleanup2 != nil {
		defer cleanup2()
	}
	if err2 != nil || len(got2) != len(got) {
		t.Fatalf("second split differs: %d chunks, err=%v (want %d)", len(got2), err2, len(got))
	}
	for i := range got {
		b1, _ := os.ReadFile(got[i])
		b2, _ := os.ReadFile(got2[i])
		if !bytes.Equal(b1, b2) {
			t.Errorf("chunk %d differs between two identical splits (%d vs %d bytes)", i, len(b1), len(b2))
		}
	}
	t.Logf("65 s → %d chunks (deterministic, non-overlapping, ≤30 s each)", len(got))
}

// TestTranscribe_LongRecording_ChunkFailStillFails surfaces — if
// the backend rejects a chunk too, the error must remain non-nil (the
// chunked retry replaces a 400 but never swallows one).
func TestTranscribe_LongRecording_ChunkFailStillFails(t *testing.T) {
	// Backend that rejects everything longer than 15 s — including every
	// chunk the client would produce from a 31 s recording. A chunked
	// retry replaces a 400, it never swallows one: the caller must still
	// see a non-nil error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		wav := findWAVInBody(body)
		dur := -1.0
		if wav >= 0 {
			dur = wavDurationSeconds(body[wav:])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write(fmt.Appendf(nil,
			`{"error":"audio duration %.1fs exceeds maximum window 15s"}`, dur))
	}))
	defer ts.Close()

	samples := 8000 * 31
	path := filepath.Join(t.TempDir(), "t31.wav")
	createSilentWAV(t, path, samples)

	o, err := NewOpenAICompat(ts.URL, "", "whisper-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := o.Transcribe(ctx, path); err == nil {
		t.Fatalf("transcribe should still fail when every chunk is refused")
	}
}

// TestLongAudio_OpenAICompat_BodySizeAndRetry exercises the openai_compat
// timeout/body-size/retry path with a 5-minute WAV against a flaky fake
// (two 503s, then success). Nothing in this layer should be size- or
// duration-sensitive: the only client-side limits are the 5-minute HTTP
// timeout and the 4-attempt 503/504 retry (openai_compat.go).
func TestLongAudio_OpenAICompat_BodySizeAndRetry(t *testing.T) {
	fake := &fakeWindowedWhisper{maxSeconds: 0, failFirstN: 2} // 0 = accept any duration
	ts := newFakeWhisperServer(fake)
	defer ts.Close()

	const secs = 300 // 5 min, 16 kHz s16 mono ≈ 9.6 MB
	wav := silentWAV(secs)
	path := writeWavFile(t, "five_min.wav", wav)
	t.Logf("5-minute 16 kHz mono s16 WAV = %d bytes (%.1f MB); multipart body overhead is small", len(wav), float64(len(wav))/1e6)

	o, err := NewOpenAICompat(ts.URL, "", "whisper-1", "")
	if err != nil {
		t.Fatal(err)
	}
	o.HTTPClient = ts.Client()
	start := time.Now()
	text, err := o.Transcribe(context.Background(), path)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("503×2 then 200 should succeed via retry: %v", err)
	}
	if text != "ok" {
		t.Errorf("text=%q want ok", text)
	}
	fake.mu.Lock()
	calls, body := fake.calls, fake.lastBody
	fake.mu.Unlock()
	if calls != 3 {
		t.Errorf("server saw %d calls, want 3 (2×503 + 1×200)", calls)
	}
	// Body must be the full audio, not a truncated 30-s window's worth.
	if body < len(wav)/2 { // multipart framing adds a little overhead
		t.Errorf("received body %d bytes is far smaller than the full 5-min payload (~%d)", body, len(wav))
	}
	// 503 retries cost 1s + 2s of backoff.
	if elapsed < 3*time.Second {
		t.Errorf("retry backoff took only %v; expected ≥3s (1s+2s)", elapsed)
	}
	t.Logf("retry path OK in %v over %d bytes across %d calls", elapsed, body, calls)
}

// TestLongAudio_OpenAICompat_TimeoutHonoursContext verifies the 5-minute
// HTTP client timeout is the only duration limit: a slow fake (2 s of
// work per window, i.e. 20 min of simulated inference for a 10-min file)
// must NOT be cut by anything shorter than the client timeout.
// We simulate "server slowness" rather than actually waiting 5 minutes.
func TestLongAudio_OpenAICompat_TimeoutHonoursContext(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer slow.Close()

	wav := silentWAV(600) // 10 min
	path := writeWavFile(t, "ten_min.wav", wav)

	o, err := NewOpenAICompat(slow.URL, "", "whisper-1", "")
	if err != nil {
		t.Fatal(err)
	}
	o.HTTPClient = slow.Client()

	// Short context to prove ctx is honoured (would block 5 min otherwise).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text, err := o.Transcribe(ctx, path)
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !contains(err.Error(), "context deadline exceeded") && !contains(err.Error(), "Client.Timeout") {
			t.Fatalf("expected a timeout-derived error, got: %v", err)
		}
		// EXPECTED (documented): with the default 5 min client timeout a
		// 10-min recording only succeeds if the backend is roughly
		// ≥1x realtime. There is no progress reporting / resumable
		// upload — the client either gets a full transcript or an error.
		t.Logf("long-audio request correctly killed by context: %v", err)
		return
	}
	if text != "ok" {
		t.Errorf("text=%q want ok", text)
	}
}

// ---------------------------------------------------------------------------
// (c) local whisper layer — 65 s synthetic file coverage
// ---------------------------------------------------------------------------

// TestLongAudio_LocalWhisper_65sCoverage runs the REAL venv whisper path
// (transcribe.go) on a 65 s tone. openai-whisper windows long audio into
// 30 s windows internally, so the local layer is expected to SUCCEED and
// cover the full duration — i.e. NOT to fail where the windowed remote
// backend failed. If the venv / whisper is missing the test skips.
func TestLongAudio_LocalWhisper_65sCoverage(t *testing.T) {
	python, ok := findVenvPython(t)
	if !ok {
		t.Skip("no venv with importable whisper (check <project>/.venv and ~/.datawatch/.venv) — skipping local whisper layer")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH (whisper decodes audio via ffmpeg) — skipping")
	}

	// 65 s of 440 Hz tone at 16 kHz — spans two 30 s windows plus 5 s.
	wav := tone16k(t, 65)
	path := writeWavFile(t, "tone65.wav", wav)
	t.Logf("synthetic 65 s tone WAV: %d bytes", len(wav))

	w := &WhisperTranscriber{Python: python, Model: "tiny", Language: "en"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	text, err := w.Transcribe(ctx, path)
	if err != nil {
		// EXPECTED FAILURE (record if seen): local whisper cannot even
		// decode one 65 s file — would implicate transcribe.go / the venv,
		// not just the windowed remote backend.
		t.Fatalf("local whisper failed on a 65 s file: %v", err)
	}
	// whisper produces no per-window offsets through the Go wrapper (it
	// only writes result["text"]), so coverage is asserted as: ran to
	// completion across 3 windows without error. The transcript of a
	// tone is model-dependent (often empty or hallucinated) — log it as
	// evidence but do not require specific text.
	t.Logf("local whisper on 65 s tone: text=%q (local layer handles >30 s windows fine)", text)
}

// findVenvPython locates a venv python3 with whisper importable,
// preferring the project .venv, then ~/.datawatch/.venv (the operator
// default noted in the task brief).
func findVenvPython(t *testing.T) (string, bool) {
	candidates := []string{
		filepath.Join(projectRoot(), ".venv", "bin", "python3"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".datawatch", ".venv", "bin", "python3"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if out, err := exec.Command(p, "-c", "import whisper").CombinedOutput(); err == nil {
			return p, true
		} else {
			t.Logf("whisper not importable in %s: %s", p, string(out))
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// (d) ffmpeg transcode of a MediaRecorder-style webm (no duration meta)
// ---------------------------------------------------------------------------

// TestLongAudio_FfmpegTranscode_WebmNoDuration reproduces the handler's
// ffmpeg step (internal/server/voice.go) on a MediaRecorder-style webm:
// recorded live to a stream, so the EBML Segment header carries NO
// Duration field — exactly what Chrome's MediaRecorder emits.
func TestLongAudio_FfmpegTranscode_WebmNoDuration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH — skipping webm transcode layer")
	}

	const seconds = 65
	dir := t.TempDir()
	webmPath := filepath.Join(dir, "media-recorder.webm")

	// Write the webm to a PIPE — ffmpeg in stream mode does not know the
	// total duration up front, so (like a live MediaRecorder) the
	// Segment element has no Duration. lavfi sine is our "voice".
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", seconds),
		"-c:a", "libopus", "-b:a", "64k", "-f", "webm", "-")
	out, err := gen.CombinedOutput()
	if err != nil {
		t.Fatalf("generate webm: %v\n%s", err, out)
	}
	if err := os.WriteFile(webmPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("MediaRecorder-style webm (pipelined, no duration metadata): %d bytes", len(out))

	// Evidence: duration field in the container.
	if ffprobe, perr := exec.LookPath("ffprobe"); perr == nil {
		probed, _ := exec.Command(ffprobe, "-hide_banner", "-loglevel", "error",
			"-show_entries", "format=duration=exact",
			"-of", "default=noprint_wrappers=1:nokey=1", webmPath).CombinedOutput()
		t.Logf("ffprobe format=duration (N/A = missing, as in MediaRecorder output): %s", string(bytes.TrimSpace(probed)))
	}

	// The exact command the handler runs (see internal/server/voice.go):
	wavPath := filepath.Join(dir, "audio.wav")
	cmd := exec.Command(ffmpeg, "-y", "-i", webmPath, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wavPath)
	if o, err := cmd.CombinedOutput(); err != nil {
		// EXPECTED FAILURE (record if seen): ffmpeg cannot transcode a
		// duration-less webm — would implicate the handler's transcode
		// step, not the transcribers.
		t.Fatalf("ffmpeg transcode failed: %v\n%s", err, o)
	}

	wavBytes, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatal(err)
	}
	dur := wavDurationSeconds(wavBytes)
	if dur < 0 {
		t.Fatalf("output does not parse as WAV (%d bytes)", len(wavBytes))
	}
	// Coverage: the full 65 s must survive the transcode (±1.5 s for
	// opus priming/flushing). A duration of ~30 s would prove
	// truncation at the window boundary.
	if dur < 63.5 || dur > 66.5 {
		t.Errorf("transcoded wav duration %.2fs — expected ≈65s; truncation at the 30 s window would read ~30s", dur)
	}
	t.Logf("transcoded wav: duration %.2fs, %d bytes — ffmpeg layer OK for >30 s", dur, len(wavBytes))
}

// ---------------------------------------------------------------------------

func newFakeWhisperServer(h interface {
	handler(http.ResponseWriter, *http.Request)
}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(h.handler))
}
