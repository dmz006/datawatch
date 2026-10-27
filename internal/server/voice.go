// Server handler for POST /api/voice/transcribe — closes #2.
//
// Generic voice transcription endpoint. Mobile clients (and any
// future HTTP caller) POST an audio blob and get back the Whisper
// transcript + an optional action dispatch when the transcript
// starts with a recognised command prefix ("new:", "reply:",
// "status").
//
// Implements option (a) from the issue — clean endpoint separation
// rather than fake-Telegram-context. Telegram's voice flow continues
// to work through its existing backend; this endpoint is additive.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/transcribe"
)

// voiceLogf writes one voice diagnostics line. A variable so tests can
// capture lines.
var voiceLogf = func(format string, args ...any) { fmt.Printf(format, args...) }

// voiceSlowThreshold is the elapsed time past which a successful
// transcription is still logged, so slow requests are visible before
// they start timing out. A variable so tests can shrink it.
var voiceSlowThreshold = 30 * time.Second

// voiceDetailMaxBytes caps the raw error text returned in the JSON body.
const voiceDetailMaxBytes = 500

// maxVoiceUploadBytes is the hard cap for POST /api/voice/transcribe
// uploads (25 MB — the Whisper API's own cap + headroom for a couple of
// minutes of 16 kHz mono opus). Enforced on the wire via
// http.MaxBytesReader (explicit 413) and again as the multipart memory
// budget; see docs/plans/2026-10-20-voice-long-recording-rootcause.md.
const maxVoiceUploadBytes int64 = 25 << 20

// voiceUploadReadTimeout is the server-wide http.Server.ReadTimeout
// (set in server.go). ReadTimeout bounds reading the ENTIRE request
// body, so it must cover a 25 MB voice blob arriving over a slow
// mobile link — the old flat 15 s aborted in-flight long-recording
// uploads ("bad multipart: … i/o timeout") before the handler ran.
// 180 s comfortably covers 25 MB at a realistic worst-case link
// (~115 kbps) while still bounding pathological clients.
const voiceUploadReadTimeout = 180 * time.Second

// Transcription deadline: voiceBaseTimeout covers the ffmpeg transcode and
// any clip of unknown or short length; a known clip gets
// voiceTimeoutPerAudioSecond seconds of allowance per second of audio, up to
// voiceMaxTimeout.
const (
	voiceBaseTimeout           = 5 * time.Minute
	voiceTimeoutPerAudioSecond = 2
	voiceMaxTimeout            = 30 * time.Minute
)

// voiceTranscribeTimeout returns how long a transcription may run for a clip
// of the given length. known is false when the length could not be
// determined, which gets the base timeout.
func voiceTranscribeTimeout(audio time.Duration, known bool) time.Duration {
	if !known {
		return voiceBaseTimeout
	}
	d := audio * voiceTimeoutPerAudioSecond
	if d < voiceBaseTimeout {
		d = voiceBaseTimeout
	}
	if d > voiceMaxTimeout {
		d = voiceMaxTimeout
	}
	return d
}

// transcribeSurface is the narrow interface the handler needs; the
// full transcribe.Transcriber implements it. Defined here so server
// tests can plug a fake without importing whisper/venv machinery.
type transcribeSurface interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

// SetTranscriber wires the Whisper transcriber for /api/voice/transcribe.
// Nil disables the endpoint (503).
func (s *Server) SetTranscriber(t transcribeSurface) { s.transcriber = t }

// handleVoiceTranscribe implements POST /api/voice/transcribe.
//
// Accepts multipart/form-data with fields:
//
//	audio       required — opus/ogg/webm blob (mono, 16 kHz preferred)
//	session_id  optional — if present the result auto-replies
//	auto_exec   optional — "true"/"1" runs a recognized command
//	ts_client   optional — unix_ms for latency telemetry
//
// Response (200):
//
//	{ transcript, confidence, action, session_id, latency_ms }
//
// Confidence is whisper-impl-dependent — we surface a conservative
// 1.0 placeholder; a future Whisper upgrade can populate it from
// the model's own word-confidence output.
func (s *Server) handleVoiceTranscribe(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.transcriber == nil {
		http.Error(w, "voice transcription not enabled", http.StatusServiceUnavailable)
		return
	}

	start := time.Now()
	// Limit uploads to 25 MB (Whisper API's own cap + enough headroom
	// for a minute of 16 kHz mono opus). http.MaxBytesReader enforces
	// the cap on the wire with an explicit 413 (the old sole guard,
	// ParseMultipartForm(25<<20), only bound in-memory spooling — a
	// valid >25 MB body was spooled to disk and accepted). See
	// docs/plans/2026-10-20-voice-long-recording-rootcause.md.
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxVoiceUploadBytes)
	}
	if err := r.ParseMultipartForm(maxVoiceUploadBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = fmt.Fprintf(w, "audio upload exceeds the %d MB limit — split the recording or lower the quality\n", maxVoiceUploadBytes>>20)
			return
		}
		http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing audio field", http.StatusBadRequest)
		return
	}
	defer file.Close() //nolint:errcheck

	sessionID := r.FormValue("session_id")
	autoExec := r.FormValue("auto_exec") == "true" || r.FormValue("auto_exec") == "1"
	var clientTs int64
	if raw := r.FormValue("ts_client"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			clientTs = v
		}
	}
	_ = clientTs // reserved for future telemetry

	// Persist the blob to a temp file — Whisper shells out so needs
	// a real path. Cleaned up after Transcribe returns.
	tmpDir, err := os.MkdirTemp("", "dw-voice-")
	if err != nil {
		http.Error(w, "temp: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".ogg"
	}
	tmpPath := filepath.Join(tmpDir, "audio"+ext)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "temp create: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(tmpFile, file); err != nil {
		_ = tmpFile.Close()
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmpFile.Close()

	// The transcode keeps the flat base budget; transcription gets its own
	// deadline sized from the clip length below.
	ffmpegCtx, ffmpegCancel := context.WithTimeout(r.Context(), voiceBaseTimeout)
	defer ffmpegCancel()

	// v7.0.0-alpha.14 (#237) — transcode the browser-mic blob (typically
	// webm/opus) to 16-kHz mono WAV before handing off to the
	// transcriber. OpenWebUI's audio API rejects webm/opus with HTTP
	// 400 ("file format not supported"); local whisper venv is more
	// permissive but slower. WAV works everywhere and triggers the
	// faster primary path. ffmpeg is invoked only when present;
	// missing-ffmpeg silently keeps the original file (chain fallback
	// then takes over as before).
	ffmpegMissing := false
	if ffmpegBin, ferr := exec.LookPath("ffmpeg"); ferr != nil {
		ffmpegMissing = true
	} else {
		ffmpegStart := time.Now()
		wavPath := filepath.Join(tmpDir, "audio.wav")
		// -y overwrite, -i input, -ac 1 mono, -ar 16000 16kHz, -c:a pcm_s16le 16-bit PCM
		cmd := exec.CommandContext(ffmpegCtx, ffmpegBin, "-y", "-i", tmpPath, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wavPath)
		if out, fferr := cmd.CombinedOutput(); fferr != nil {
			// Transcode failed — keep original; chain fallback handles.
			voiceLogf("[voice] ffmpeg transcode failed (%v); using original %s. bytes=%d elapsed_ms=%d ffmpeg said: %s\n",
				fferr, ext, header.Size, time.Since(ffmpegStart).Milliseconds(), strings.TrimSpace(string(out)))
		} else {
			tmpPath = wavPath
		}
	}

	// Size the transcription deadline from the (possibly transcoded) clip so
	// a long recording on a slow host is not cut off by a flat budget.
	audioLen, audioKnown := transcribe.AudioDuration(r.Context(), tmpPath)
	ctx, cancel := context.WithTimeout(r.Context(), voiceTranscribeTimeout(audioLen, audioKnown))
	defer cancel()

	transcribeStart := time.Now()
	transcript, err := s.transcriber.Transcribe(ctx, tmpPath)
	elapsed := time.Since(transcribeStart)
	failure := transcribe.ClassifyError(err)
	if err != nil || elapsed > voiceSlowThreshold {
		s.logVoiceTranscribe(tmpPath, header.Size, elapsed, err, failure, ffmpegMissing)
	}
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":      "transcribe: " + failure.Message,
			"cause":      failure.Cause,
			"hint":       failure.Hint,
			"detail":     truncateBytes(err.Error(), voiceDetailMaxBytes),
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	transcript = strings.TrimSpace(transcript)

	action := "none"
	if autoExec {
		action = classifyVoiceAction(transcript)
	}

	resp := map[string]interface{}{
		"transcript": transcript,
		"confidence": 1.0, // whisper CLI doesn't surface per-word; placeholder
		"action":     action,
		"session_id": sessionID,
		"latency_ms": time.Since(start).Milliseconds(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleVoiceTest probes the configured transcriber by feeding it a
// 1 KB silent WAV. Returns 200 on success or 502 with the underlying
// error so the PWA "Test" button can refuse to enable when the
// configured backend (whisper venv / openai / openwebui / ollama)
// isn't reachable.
func (s *Server) handleVoiceTest(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapLLMsList) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.transcriber == nil {
		http.Error(w, "voice transcription not enabled", http.StatusServiceUnavailable)
		return
	}
	tmpDir, err := os.MkdirTemp("", "dw-voicetest-")
	if err != nil {
		http.Error(w, "temp: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck
	tmpPath := filepath.Join(tmpDir, "silence.wav")
	if err := os.WriteFile(tmpPath, silentWAV1Sec(), 0o644); err != nil {
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	start := time.Now()
	out, err := s.transcriber.Transcribe(ctx, tmpPath)
	if err != nil {
		failure := transcribe.ClassifyError(err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error": err.Error(), "cause": failure.Cause, "hint": failure.Hint,
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "transcript": strings.TrimSpace(out), "latency_ms": time.Since(start).Milliseconds(),
	})
}

// voiceBackend returns the configured whisper backend, "whisper" when unset.
// Safe on a Server built without config.
func (s *Server) voiceBackend() string {
	if s.cfg != nil && s.cfg.Whisper.Backend != "" {
		return s.cfg.Whisper.Backend
	}
	return "whisper"
}

// logVoiceTranscribe emits the one-line diagnostics record for a failed or
// slow transcription. It never includes the transcript text.
func (s *Server) logVoiceTranscribe(audioPath string, uploadBytes int64, elapsed time.Duration, err error, f transcribe.Failure, ffmpegMissing bool) {
	label, cause, exit, errText := "slow", "none", "n/a", ""
	if err != nil {
		label, cause, errText = "failed", f.Cause, firstErrLine(err.Error())
		switch {
		case f.Signal != "":
			exit = f.Signal
		case f.ExitCode >= 0:
			exit = strconv.Itoa(f.ExitCode)
		}
	}
	audioSeconds := "unknown"
	probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if d, ok := transcribe.AudioDuration(probeCtx, audioPath); ok {
		audioSeconds = strconv.FormatFloat(d.Seconds(), 'f', 1, 64)
	}
	note := ""
	if err != nil && ffmpegMissing {
		note = " ffmpeg=missing"
	}
	voiceLogf("[voice] transcribe %s: backend=%s bytes=%d audio_seconds=%s elapsed_ms=%d cause=%s exit=%s%s err=%s\n",
		label, s.voiceBackend(), uploadBytes, audioSeconds, elapsed.Milliseconds(), cause, exit, note, errText)
}

// firstErrLine returns the first line of an error message.
func firstErrLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// truncateBytes shortens s to at most max bytes without splitting a rune.
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// silentWAV1Sec returns ~1 KB of valid mono 16-bit 8 kHz WAV (silence).
// Hand-built so we don't pull in a sound library for one byte sequence.
func silentWAV1Sec() []byte {
	const sampleRate = 8000
	const numSamples = sampleRate / 8 // 0.125 s — small enough to keep the test cheap
	const bitsPerSample = 16
	const numChannels = 1
	const byteRate = sampleRate * numChannels * bitsPerSample / 8
	const blockAlign = numChannels * bitsPerSample / 8
	dataSize := numSamples * numChannels * bitsPerSample / 8
	buf := make([]byte, 0, 44+dataSize)
	put32 := func(v uint32) { buf = append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	put16 := func(v uint16) { buf = append(buf, byte(v), byte(v>>8)) }
	buf = append(buf, 'R', 'I', 'F', 'F')
	put32(uint32(36 + dataSize))
	buf = append(buf, 'W', 'A', 'V', 'E', 'f', 'm', 't', ' ')
	put32(16) // fmt chunk size
	put16(1)  // PCM
	put16(numChannels)
	put32(sampleRate)
	put32(byteRate)
	put16(blockAlign)
	put16(bitsPerSample)
	buf = append(buf, 'd', 'a', 't', 'a')
	put32(uint32(dataSize))
	buf = append(buf, make([]byte, dataSize)...) // silence
	return buf
}

// classifyVoiceAction inspects the transcript for a recognised
// command prefix and returns one of the values documented in #2.
// Extracted so tests can exercise the prefix matrix without the
// Whisper plumbing.
func classifyVoiceAction(transcript string) string {
	lower := strings.ToLower(strings.TrimSpace(transcript))
	switch {
	case strings.HasPrefix(lower, "new:"), strings.HasPrefix(lower, "new "):
		return "new"
	case strings.HasPrefix(lower, "reply:"), strings.HasPrefix(lower, "reply "):
		return "reply"
	case strings.HasPrefix(lower, "status"):
		return "status"
	}
	return "none"
}
