// whisper.chunk_seconds — GET/PUT /api/config round-trip. Values of 0
// (chunking off) and >= 30 are stored; anything else is ignored and the
// previous value stays.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newWhisperChunkServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.DefaultConfig(), cfgPath: t.TempDir() + "/config.yaml"}
}

func getWhisperChunkSeconds(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Whisper map[string]json.RawMessage `json:"whisper"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := got.Whisper["chunk_seconds"]
	if !ok {
		t.Fatalf("GET /api/config whisper has no chunk_seconds: %v", got.Whisper)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("chunk_seconds %s is not an int: %v", raw, err)
	}
	return n
}

func TestWhisperChunkSeconds_GetConfigDefaultIs120(t *testing.T) {
	s := newWhisperChunkServer(t)
	if got := getWhisperChunkSeconds(t, s); got != 120 {
		t.Fatalf("GET chunk_seconds = %d, want 120", got)
	}
}

func TestWhisperChunkSeconds_PutRoundTrip(t *testing.T) {
	s := newWhisperChunkServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.chunk_seconds": 60})
	if got := getWhisperChunkSeconds(t, s); got != 60 {
		t.Fatalf("GET after PUT 60 = %d, want 60", got)
	}
}

func TestWhisperChunkSeconds_PutZeroRoundTrips(t *testing.T) {
	s := newWhisperChunkServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.chunk_seconds": 0})
	if got := getWhisperChunkSeconds(t, s); got != 0 {
		t.Fatalf("GET after PUT 0 = %d, want 0", got)
	}
}

func TestWhisperChunkSeconds_PutInvalidIsIgnored(t *testing.T) {
	s := newWhisperChunkServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.chunk_seconds": 60})
	for _, bad := range []int{10, -5, 29} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.chunk_seconds": bad})
		if got := getWhisperChunkSeconds(t, s); got != 60 {
			t.Fatalf("GET after PUT %d = %d, want previous value 60", bad, got)
		}
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"whisper.chunk_seconds": 30})
	if got := getWhisperChunkSeconds(t, s); got != 30 {
		t.Fatalf("GET after PUT 30 = %d, want 30 (boundary is valid)", got)
	}
}
