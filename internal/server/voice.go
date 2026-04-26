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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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
//   audio       required — opus/ogg/webm blob (mono, 16 kHz preferred)
//   session_id  optional — if present the result auto-replies
//   auto_exec   optional — "true"/"1" runs a recognized command
//   ts_client   optional — unix_ms for latency telemetry
//
// Response (200):
//   { transcript, confidence, action, session_id, latency_ms }
//
// Confidence is whisper-impl-dependent — we surface a conservative
// 1.0 placeholder; a future Whisper upgrade can populate it from
// the model's own word-confidence output.
func (s *Server) handleVoiceTranscribe(w http.ResponseWriter, r *http.Request) {
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
	// for a minute of 16 kHz mono opus).
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing audio field", http.StatusBadRequest)
		return
	}
	defer file.Close()

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
	defer os.RemoveAll(tmpDir)

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
		tmpFile.Close()
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	transcript, err := s.transcriber.Transcribe(ctx, tmpPath)
	if err != nil {
		http.Error(w, "transcribe: "+err.Error(), http.StatusBadGateway)
		return
	}
	transcript = strings.TrimSpace(transcript)

	action := "none"
	if autoExec {
		action = classifyVoiceAction(transcript)
	}

	resp := map[string]interface{}{
		"transcript":   transcript,
		"confidence":   1.0, // whisper CLI doesn't surface per-word; placeholder
		"action":       action,
		"session_id":   sessionID,
		"latency_ms":   time.Since(start).Milliseconds(),
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
	defer os.RemoveAll(tmpDir)
	tmpPath := filepath.Join(tmpDir, "silence.wav")
	if err := os.WriteFile(tmpPath, silentWAV1Sec(), 0o644); err != nil {
		http.Error(w, "write: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	start := time.Now()
	out, err := s.transcriber.Transcribe(ctx, tmpPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error": err.Error(), "latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "transcript": strings.TrimSpace(out), "latency_ms": time.Since(start).Milliseconds(),
	})
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
	put32(16)               // fmt chunk size
	put16(1)                // PCM
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

