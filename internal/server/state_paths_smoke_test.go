package server

// Smoke-runs storage must live under the configured data directory, not the
// operator's home directory: a decoy ~/.datawatch/smoke-runs/ with a
// sentinel run must be invisible to a Server whose data directory is set,
// and must stay untouched.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatePathSmokeRunsUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Decoy: the legacy home-rooted storage location already holds a
	// sentinel run that must never be read, written or deleted.
	decoyRun := filepath.Join(fakeHome, ".datawatch", "smoke-runs", "decoy.json")
	if err := os.MkdirAll(filepath.Dir(decoyRun), 0755); err != nil {
		t.Fatalf("setup decoy dir: %v", err)
	}
	decoyBody := `{"run_id":"decoy","pass":1}`
	if err := os.WriteFile(decoyRun, []byte(decoyBody), 0644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}

	// Same construction as the other smoke tests, plus an explicit data
	// directory.
	s := newTestServer(t, nil, nil)
	s.dataDir = dataDir

	// POST a progress record through the endpoint.
	rr := postSmokeProgress(t, s, map[string]any{"run_id": "real-run", "pass": float64(2)})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s, want 200", rr.Code, rr.Body.String())
	}

	// The record must be on disk under the data directory.
	wantPath := filepath.Join(dataDir, "smoke-runs", "real-run.json")
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("record not written under the data directory: %v", err)
	}
	var posted map[string]any
	if err := json.Unmarshal(data, &posted); err != nil {
		t.Fatalf("decode written record: %v", err)
	}
	if posted["run_id"] != "real-run" {
		t.Errorf("written run_id=%v, want real-run", posted["run_id"])
	}

	// GET must return only the data-directory records.
	getRR := httptest.NewRecorder()
	s.handleSmokeProgress(getRR, httptest.NewRequest(http.MethodGet, "/api/smoke/progress", nil))
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s, want 200", getRR.Code, getRR.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode GET list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("GET returned %d records, want exactly 1: %s", len(list), getRR.Body.String())
	}
	if list[0]["id"] != "real-run" {
		t.Errorf("GET[0].id=%v, want real-run", list[0]["id"])
	}
	for _, e := range list {
		if strings.Contains(e["id"].(string), "decoy") {
			t.Errorf("GET list leaked the decoy record: %v", e)
		}
	}

	// The decoy folder must be unchanged.
	decoyNow, err := os.ReadFile(decoyRun)
	if err != nil {
		t.Fatalf("decoy was removed: %v", err)
	}
	if string(decoyNow) != decoyBody {
		t.Errorf("decoy changed: %s, want %s", decoyNow, decoyBody)
	}
}
