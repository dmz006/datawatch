package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/transcribe"
)

// loudWAV builds a 16 kHz mono 16-bit PCM WAV of the given whole seconds
// filled with a constant, clearly audible sample. The splitter looks for
// near-silence to cut on, so a constant tone makes it cut exactly on the
// 30 s boundary instead of early.
func loudWAV(seconds int) []byte {
	const rate = 16000
	const sample = 8000
	dataSize := seconds * rate * 2
	b := make([]byte, 0, 44+dataSize)
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(36+dataSize))
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1) // PCM
	b = binary.LittleEndian.AppendUint16(b, 1) // mono
	b = binary.LittleEndian.AppendUint32(b, rate)
	b = binary.LittleEndian.AppendUint32(b, rate*2)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(dataSize))
	for i := 0; i < seconds*rate; i++ {
		b = binary.LittleEndian.AppendUint16(b, sample)
	}
	return b
}

// sliceRecorder is a stub inner transcriber that records every call and
// checks that calls never overlap.
type sliceRecorder struct {
	// reply decides the result of call n (1-based) for the file at path.
	reply func(n int, path string) (string, error)

	mu         sync.Mutex
	paths      []string
	seconds    []float64
	inFlight   int
	overlapped bool
}

func (r *sliceRecorder) Transcribe(_ context.Context, path string) (string, error) {
	r.mu.Lock()
	r.inFlight++
	if r.inFlight > 1 {
		r.overlapped = true
	}
	r.paths = append(r.paths, path)
	n := len(r.paths)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()

	if raw, err := os.ReadFile(path); err == nil && len(raw) >= 44 && string(raw[0:4]) == "RIFF" {
		r.mu.Lock()
		r.seconds = append(r.seconds, float64(len(raw)-44)/(16000*2))
		r.mu.Unlock()
	}
	return r.reply(n, path)
}

func (r *sliceRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.paths)
}

// chunkedVoiceServer wires a real 30 s ChunkedTranscriber over inner into a
// Server, sending its progress lines through voiceLogf.
func chunkedVoiceServer(inner transcribe.Transcriber) *Server {
	c := transcribe.NewChunked(inner, 30*time.Second)
	c.Logf = func(format string, args ...any) { voiceLogf(format, args...) }
	return &Server{transcriber: c}
}

// voiceUploadRequest builds a transcribe request carrying body as the audio
// part under the given file name.
func voiceUploadRequest(t *testing.T, body []byte, fileName string) *http.Request {
	t.Helper()
	payload, ctype := multipartWAVBody(t, body, "", fileName)
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(payload))
	req.Header.Set("Content-Type", ctype)
	return req
}

func joinedLog(lines []string) string { return strings.Join(lines, "") }

func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rr.Body.String())
	}
}

func TestVoiceChunkedEndToEnd_LongRecordingJoinsSlicesInOrder(t *testing.T) {
	logs := captureVoiceLog(t) // also keeps ffmpeg and ffprobe off PATH
	texts := []string{"alpha one", " bravo two ", "charlie three", "delta four\n"}
	inner := &sliceRecorder{reply: func(n int, _ string) (string, error) {
		return texts[n-1], nil
	}}
	s := chunkedVoiceServer(inner)

	rr := serveVoice(t, s, voiceUploadRequest(t, loudWAV(100), "audio.wav"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Transcript string `json:"transcript"`
	}
	decodeJSON(t, rr, &body)
	if want := "alpha one bravo two charlie three delta four"; body.Transcript != want {
		t.Errorf("transcript = %q, want %q", body.Transcript, want)
	}

	if got := inner.calls(); got != 4 {
		t.Fatalf("inner saw %d calls, want 4 slices", got)
	}
	if inner.overlapped {
		t.Error("slices were transcribed concurrently, want strictly sequential calls")
	}
	if len(inner.seconds) != 4 {
		t.Fatalf("recorded %d slice lengths, want 4", len(inner.seconds))
	}
	var total float64
	for i, sec := range inner.seconds {
		if sec > 30.0001 {
			t.Errorf("slice %d is %.2fs long, want at most 30s", i+1, sec)
		}
		total += sec
	}
	if total < 99.99 || total > 100.01 {
		t.Errorf("slices add up to %.2fs, want the full 100s", total)
	}
	if got := joinedLog(logs()); strings.Contains(got, "[voice] transcribe failed") {
		t.Errorf("successful request logged a failure line: %q", got)
	}
}

func TestVoiceChunkedEndToEnd_KilledSliceStopsAndReportsCause(t *testing.T) {
	logs := captureVoiceLog(t)
	killed := killedChildError(t)
	inner := &sliceRecorder{reply: func(n int, _ string) (string, error) {
		if n == 2 {
			return "", fmt.Errorf("whisper failed: %w", killed)
		}
		return fmt.Sprintf("slice %d", n), nil
	}}
	s := chunkedVoiceServer(inner)

	rr := serveVoice(t, s, voiceUploadRequest(t, loudWAV(100), "audio.wav"))
	body := decodeVoiceError(t, rr)
	if body["cause"] != "killed" {
		t.Errorf("cause = %v, want killed", body["cause"])
	}
	if hint, _ := body["hint"].(string); hint == "" {
		t.Error("hint is empty")
	}
	if msg, _ := body["detail"].(string); !strings.Contains(msg, "slice 2 of 4") {
		t.Errorf("detail = %q, want it to name the failing slice", msg)
	}
	if got := inner.calls(); got != 2 {
		t.Errorf("inner saw %d calls, want 2 (no slice after the failed one)", got)
	}

	var failed []string
	for _, line := range logs() {
		if strings.Contains(line, "[voice] transcribe failed:") {
			failed = append(failed, line)
		}
	}
	if len(failed) != 1 {
		t.Fatalf("got %d failure lines, want 1: %q", len(failed), logs())
	}
	for _, want := range []string{"backend=", "bytes=", "audio_seconds=100", "elapsed_ms=", "cause=killed", "exit=killed"} {
		if !strings.Contains(failed[0], want) {
			t.Errorf("failure line %q missing %q", failed[0], want)
		}
	}
}

func TestVoiceChunkedEndToEnd_DeadlineGivesTimeoutCause(t *testing.T) {
	logs := captureVoiceLog(t)
	inner := &sliceRecorder{reply: func(int, string) (string, error) {
		return "", context.DeadlineExceeded
	}}
	s := chunkedVoiceServer(inner)

	rr := serveVoice(t, s, voiceUploadRequest(t, loudWAV(100), "audio.wav"))
	body := decodeVoiceError(t, rr)
	if body["cause"] != "timeout" {
		t.Errorf("cause = %v, want timeout", body["cause"])
	}
	if hint, _ := body["hint"].(string); hint == "" {
		t.Error("hint is empty")
	}
	if got := inner.calls(); got != 1 {
		t.Errorf("inner saw %d calls, want 1", got)
	}
	if got := joinedLog(logs()); !strings.Contains(got, "cause=timeout") {
		t.Errorf("log %q missing cause=timeout", got)
	}
}

func TestVoiceChunkedEndToEnd_SlowSuccessIsLogged(t *testing.T) {
	logs := captureVoiceLog(t)
	orig := voiceSlowThreshold
	voiceSlowThreshold = time.Millisecond
	t.Cleanup(func() { voiceSlowThreshold = orig })

	inner := &sliceRecorder{reply: func(n int, _ string) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return fmt.Sprintf("slice %d", n), nil
	}}
	s := chunkedVoiceServer(inner)

	rr := serveVoice(t, s, voiceUploadRequest(t, loudWAV(100), "audio.wav"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	got := joinedLog(logs())
	if !strings.Contains(got, "transcribe slow:") {
		t.Errorf("log %q has no slow line", got)
	}
	if strings.Contains(got, "transcribe failed") {
		t.Errorf("successful request logged a failure line: %q", got)
	}
}

func TestVoiceChunkedEndToEnd_NonWAVWithoutFFmpegGoesWhole(t *testing.T) {
	logs := captureVoiceLog(t)
	garbage := []byte("these bytes are not any audio container at all")
	var gotName string
	var gotBytes []byte
	inner := &sliceRecorder{reply: func(_ int, path string) (string, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		gotBytes = raw
		gotName = path
		return "whole clip", nil
	}}
	s := chunkedVoiceServer(inner)

	rr := serveVoice(t, s, voiceUploadRequest(t, garbage, "clip.webm"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Transcript string `json:"transcript"`
	}
	decodeJSON(t, rr, &body)
	if body.Transcript != "whole clip" {
		t.Errorf("transcript = %q, want %q", body.Transcript, "whole clip")
	}
	if got := inner.calls(); got != 1 {
		t.Fatalf("inner saw %d calls, want 1", got)
	}
	if !bytes.Equal(gotBytes, garbage) {
		t.Errorf("inner got %d bytes from %s, want the original %d upload bytes", len(gotBytes), gotName, len(garbage))
	}
	if got := joinedLog(logs()); !strings.Contains(got, "chunking skipped") {
		t.Errorf("log %q has no chunking skipped line", got)
	}
}
