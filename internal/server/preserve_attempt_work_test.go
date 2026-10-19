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

func TestPreserveAttemptWorkSettingRoundTrips(t *testing.T) {
	// Own temp config file: the PUT persists, so it must never touch the
	// operator's real config.
	s := &Server{
		cfg:     &config.Config{},
		cfgPath: filepath.Join(t.TempDir(), "config.yaml"),
	}
	put := func(v bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]bool{"autonomous.preserve_attempt_work": v})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", v, rr.Code, rr.Body.String())
		}
	}
	get := func() bool {
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
		v, ok := out.Autonomous["preserve_attempt_work"].(bool)
		if !ok {
			t.Fatalf("preserve_attempt_work missing from GET autonomous block: %v", out.Autonomous)
		}
		return v
	}

	put(false)
	if got := get(); got {
		t.Fatal("after PUT false, GET must return false")
	}
	put(true)
	if got := get(); !got {
		t.Fatal("after PUT true, GET must return true")
	}
}
