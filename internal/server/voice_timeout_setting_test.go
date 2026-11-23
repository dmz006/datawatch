// whisper.timeout_seconds — fixed transcription deadline. 0 keeps the
// automatic deadline; a positive value replaces it. Guards against the
// setting being lost or changing the default behaviour.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestVoiceDeadline_DefaultIsAutomatic(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig()}
	if s.cfg.Whisper.TimeoutSeconds != 0 {
		t.Fatalf("default TimeoutSeconds = %d, want 0", s.cfg.Whisper.TimeoutSeconds)
	}
	if got := voiceDeadline(s, 3*time.Hour, true); got != 30*time.Minute {
		t.Errorf("default deadline for 3h clip = %v, want the 30m cap", got)
	}
	if got := voiceDeadline(nil, 10*time.Second, true); got != 5*time.Minute {
		t.Errorf("nil server deadline = %v, want 5m", got)
	}
}

func TestVoiceDeadline_FixedValueReplacesAutomatic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Whisper.TimeoutSeconds = 5400
	s := &Server{cfg: cfg}
	for _, audio := range []time.Duration{0, 10 * time.Second, 3 * time.Hour} {
		if got := voiceDeadline(s, audio, true); got != 90*time.Minute {
			t.Errorf("deadline for %v = %v, want 90m", audio, got)
		}
	}
}

func getWhisperTimeoutSeconds(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got struct {
		Whisper map[string]json.RawMessage `json:"whisper"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := got.Whisper["timeout_seconds"]
	if !ok {
		t.Fatalf("GET /api/config whisper has no timeout_seconds: %v", got.Whisper)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("timeout_seconds %s is not an int: %v", raw, err)
	}
	return n
}

func TestWhisperTimeoutSeconds_ConfigRoundTrip(t *testing.T) {
	s := newWhisperChunkServer(t)
	if got := getWhisperTimeoutSeconds(t, s); got != 0 {
		t.Fatalf("GET default = %d, want 0", got)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.timeout_seconds": 3600})
	if got := getWhisperTimeoutSeconds(t, s); got != 3600 {
		t.Fatalf("GET after PUT 3600 = %d", got)
	}
	for _, bad := range []int{1, 29, -5} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.timeout_seconds": bad})
		if got := getWhisperTimeoutSeconds(t, s); got != 3600 {
			t.Fatalf("GET after PUT %d = %d, want previous 3600", bad, got)
		}
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.timeout_seconds": 0})
	if got := getWhisperTimeoutSeconds(t, s); got != 0 {
		t.Fatalf("GET after PUT 0 = %d, want 0 (back to automatic)", got)
	}
}
