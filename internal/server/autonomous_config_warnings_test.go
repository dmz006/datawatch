package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// A config set that leaves a planning model unpaired with a non-finished
// PRD's decompose backend must return the warning in the response, so the
// caller sees it without polling the status endpoint.
func TestHandleAutonomousConfig_PUT_ReturnsPairingWarning(t *testing.T) {
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	t.Cleanup(sm.WaitBackground)
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	if _, err := mgr.CreatePRD("spec", t.TempDir(), "", "", ""); err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	srv := NewServer(NewHub(), sm, "h", "", nil, &config.Config{}, filepath.Join(dir, "config.yaml"))
	srv.SetAutonomousAPI(autonomous.NewAPI(mgr))

	body := `{"planning_backend":"claude-code","planning_model":"ollama/qwen3.8:27b"}`
	req := httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleAutonomousConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status field = %q, want ok", resp.Status)
	}
	if len(resp.Warnings) != 1 {
		t.Fatalf("want 1 warning in response, got %v", resp.Warnings)
	}
	for _, want := range []string{"ollama", "claude-code"} {
		if !strings.Contains(resp.Warnings[0], want) {
			t.Errorf("warning %q does not name %q", resp.Warnings[0], want)
		}
	}
}

// A manager without the optional pairing surface still gets a warnings
// field, as an empty array, so the response shape is stable.
func TestHandleAutonomousConfig_PUT_WarningsFieldIsEmptyArrayWithoutPairing(t *testing.T) {
	srv, _ := newAutonomousConfigSyncServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(`{"enabled":true}`))
	w := httptest.NewRecorder()
	srv.handleAutonomousConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"warnings":[]`) {
		t.Fatalf("want warnings as an empty array, body = %s", w.Body.String())
	}
}
