package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestTaskAttemptLimitsRoundTripAndRejectNegative(t *testing.T) {
	// Own temp config file: the PUT persists, so it must never touch the
	// operator's real config.
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: filepath.Join(t.TempDir(), "config.yaml"),
	}
	put := func(body map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", body, rr.Code, rr.Body.String())
		}
	}
	get := func() (int, int) {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var out struct {
			Autonomous map[string]any `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		maxV, ok1 := out.Autonomous["task_max_attempt_seconds"].(float64)
		npV, ok2 := out.Autonomous["task_no_progress_seconds"].(float64)
		if !ok1 || !ok2 {
			t.Fatalf("limit keys missing from GET autonomous block: %v", out.Autonomous)
		}
		return int(maxV), int(npV)
	}

	put(map[string]any{
		"autonomous.task_max_attempt_seconds": 5400,
		"autonomous.task_no_progress_seconds": 1800,
	})
	if m, n := get(); m != 5400 || n != 1800 {
		t.Fatalf("after PUT, GET = %d/%d, want 5400/1800", m, n)
	}

	// Negative values are ignored: the previous values stay.
	put(map[string]any{
		"autonomous.task_max_attempt_seconds": -1,
		"autonomous.task_no_progress_seconds": -30,
	})
	if m, n := get(); m != 5400 || n != 1800 {
		t.Fatalf("after negative PUT, GET = %d/%d, want unchanged 5400/1800", m, n)
	}

	// Zero turns the limits off.
	put(map[string]any{
		"autonomous.task_max_attempt_seconds": 0,
		"autonomous.task_no_progress_seconds": 0,
	})
	if m, n := get(); m != 0 || n != 0 {
		t.Fatalf("after zero PUT, GET = %d/%d, want 0/0", m, n)
	}
}
