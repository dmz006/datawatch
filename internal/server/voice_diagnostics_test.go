package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"encoding/json"

	"github.com/dmz006/datawatch/internal/config"
)

// funcTranscriber adapts a function to the transcribeSurface interface.
type funcTranscriber func(ctx context.Context, path string) (string, error)

func (f funcTranscriber) Transcribe(ctx context.Context, path string) (string, error) {
	return f(ctx, path)
}

// captureVoiceLog swaps voiceLogf for a recorder and keeps ffmpeg off PATH
// so the handler never transcodes with a real binary.
func captureVoiceLog(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	orig := voiceLogf
	voiceLogf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { voiceLogf = orig })
	t.Setenv("PATH", t.TempDir())
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lines...)
	}
}

// killedChildError runs a real child that SIGKILLs itself and returns the
// resulting *exec.ExitError.
func killedChildError(t *testing.T) error {
	t.Helper()
	err := exec.Command("/bin/sh", "-c", "kill -9 $$").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError from killed child, got %v", err)
	}
	return err
}

// pcmWAV builds a mono 16-bit 16 kHz WAV of the given whole seconds.
func pcmWAV(seconds int) []byte {
	const rate = 16000
	dataSize := seconds * rate * 2
	b := make([]byte, 0, 44+dataSize)
	le32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	le16 := func(v uint16) { b = binary.LittleEndian.AppendUint16(b, v) }
	b = append(b, "RIFF"...)
	le32(uint32(36 + dataSize))
	b = append(b, "WAVEfmt "...)
	le32(16)
	le16(1)
	le16(1)
	le32(rate)
	le32(rate * 2)
	le16(2)
	le16(16)
	b = append(b, "data"...)
	le32(uint32(dataSize))
	return append(b, make([]byte, dataSize)...)
}

func serveVoice(t *testing.T, s *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleVoiceTranscribe(rr, req)
	return rr
}

func decodeVoiceError(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rr.Body.String())
	}
	return body
}

func TestVoiceDiagnostics_KilledChildLogsAndJSON(t *testing.T) {
	logs := captureVoiceLog(t)
	killed := killedChildError(t)
	s := &Server{transcriber: funcTranscriber(func(context.Context, string) (string, error) {
		return "", fmt.Errorf("whisper failed: %w", killed)
	})}
	rr := serveVoice(t, s, newAudioRequest(t, "fake-opus", nil))
	body := decodeVoiceError(t, rr)
	if body["cause"] != "killed" {
		t.Errorf("cause = %v, want killed", body["cause"])
	}
	if msg, _ := body["error"].(string); !strings.HasPrefix(msg, "transcribe: ") {
		t.Errorf("error = %q, want prefix %q", msg, "transcribe: ")
	}
	for _, k := range []string{"hint", "detail"} {
		if v, _ := body[k].(string); v == "" {
			t.Errorf("%s is empty", k)
		}
	}
	if _, ok := body["latency_ms"]; !ok {
		t.Error("latency_ms missing")
	}
	got := logs()
	if len(got) != 1 {
		t.Fatalf("got %d log lines, want 1: %q", len(got), got)
	}
	for _, want := range []string{"[voice] transcribe failed:", "backend=whisper", "bytes=9", "audio_seconds=unknown", "elapsed_ms=", "cause=killed", "exit=killed", "ffmpeg=missing", "err="} {
		if !strings.Contains(got[0], want) {
			t.Errorf("log line %q missing %q", got[0], want)
		}
	}
}

func TestVoiceDiagnostics_Causes(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		cause string
	}{
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"ffmpeg_missing", errors.New("FileNotFoundError: [Errno 2] No such file or directory: 'ffmpeg'"), "ffmpeg_missing"},
		{"backend_error", errors.New("whisper failed: exit status 2"), "backend_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureVoiceLog(t)
			s := &Server{transcriber: &fakeTranscriber{err: tc.err}}
			body := decodeVoiceError(t, serveVoice(t, s, newAudioRequest(t, "fake-opus", nil)))
			if body["cause"] != tc.cause {
				t.Errorf("cause = %v, want %s", body["cause"], tc.cause)
			}
			got := logs()
			if len(got) != 1 || !strings.Contains(got[0], "cause="+tc.cause) {
				t.Errorf("log lines = %q, want one with cause=%s", got, tc.cause)
			}
		})
	}
}

func TestVoiceDiagnostics_DetailTruncated(t *testing.T) {
	captureVoiceLog(t)
	long := "whisper failed: " + strings.Repeat("é", 600)
	s := &Server{transcriber: &fakeTranscriber{err: errors.New(long)}}
	body := decodeVoiceError(t, serveVoice(t, s, newAudioRequest(t, "fake-opus", nil)))
	detail, _ := body["detail"].(string)
	if len(detail) == 0 || len(detail) > voiceDetailMaxBytes {
		t.Errorf("detail length = %d, want 1..%d", len(detail), voiceDetailMaxBytes)
	}
}

func TestVoiceDiagnostics_LogsAudioSecondsAndBackend(t *testing.T) {
	logs := captureVoiceLog(t)
	s := &Server{
		cfg:         &config.Config{},
		transcriber: &fakeTranscriber{err: errors.New("whisper failed: exit status 1")},
	}
	s.cfg.Whisper.Backend = "openwebui"
	serveVoice(t, s, newAudioRequest(t, string(pcmWAV(3)), nil))
	got := logs()
	if len(got) != 1 {
		t.Fatalf("got %d log lines, want 1: %q", len(got), got)
	}
	for _, want := range []string{"audio_seconds=3.0", "backend=openwebui"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("log line %q missing %q", got[0], want)
		}
	}
}

func TestVoiceDiagnostics_SlowSuccessLogsOnce(t *testing.T) {
	logs := captureVoiceLog(t)
	orig := voiceSlowThreshold
	voiceSlowThreshold = 10 * time.Millisecond
	t.Cleanup(func() { voiceSlowThreshold = orig })
	s := &Server{transcriber: funcTranscriber(func(context.Context, string) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "secret transcript words", nil
	})}
	rr := serveVoice(t, s, newAudioRequest(t, "fake-opus", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	got := logs()
	if len(got) != 1 {
		t.Fatalf("got %d log lines, want 1: %q", len(got), got)
	}
	if !strings.Contains(got[0], "transcribe slow:") || !strings.Contains(got[0], "cause=none") || !strings.Contains(got[0], "exit=n/a") {
		t.Errorf("unexpected slow line: %q", got[0])
	}
	if strings.Contains(got[0], "secret transcript") {
		t.Errorf("log line leaks transcript text: %q", got[0])
	}
}

func TestVoiceDiagnostics_FastSuccessLogsNothing(t *testing.T) {
	logs := captureVoiceLog(t)
	s := &Server{transcriber: &fakeTranscriber{out: "hello"}}
	rr := serveVoice(t, s, newAudioRequest(t, "fake-opus", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := logs(); len(got) != 0 {
		t.Errorf("fast success logged %q, want nothing", got)
	}
}

func TestVoiceDiagnostics_NilConfigDoesNotPanic(t *testing.T) {
	logs := captureVoiceLog(t)
	s := &Server{cfg: nil, transcriber: &fakeTranscriber{err: errors.New("whisper failed")}}
	serveVoice(t, s, newAudioRequest(t, "fake-opus", nil))
	got := logs()
	if len(got) != 1 || !strings.Contains(got[0], "backend=whisper") {
		t.Errorf("log lines = %q, want one with backend=whisper", got)
	}
}

func TestVoiceDiagnostics_TestEndpointCarriesCauseAndHint(t *testing.T) {
	captureVoiceLog(t)
	s := &Server{transcriber: &fakeTranscriber{err: context.DeadlineExceeded}}
	rr := httptest.NewRecorder()
	s.handleVoiceTest(rr, httptest.NewRequest(http.MethodPost, "/api/voice/test", nil))
	body := decodeVoiceError(t, rr)
	if body["ok"] != false {
		t.Errorf("ok = %v, want false", body["ok"])
	}
	if body["cause"] != "timeout" {
		t.Errorf("cause = %v, want timeout", body["cause"])
	}
	if v, _ := body["hint"].(string); v == "" {
		t.Error("hint is empty")
	}
	if _, ok := body["error"]; !ok {
		t.Error("error missing")
	}
}
