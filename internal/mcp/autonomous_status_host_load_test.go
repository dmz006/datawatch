package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// TestAutonomousStatusToolIncludesHostLoad: the autonomous_status tool
// returns the GET /api/autonomous/status body verbatim (see
// internal/mcp/autonomous.go), so surfacing the host load check snapshot
// rides on the manager's provider: wire a HostLoadCheck-shaped snapshot
// through the same status read the daemon serves and assert the tool text
// carries it.
func TestAutonomousStatusToolIncludesHostLoad(t *testing.T) {
	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	want := autonomous.HostLoadStatus{Mode: "reap", LastLoad: 7, LastLimit: 6, Breached: true, OrphansReaped: 2}
	m.SetHostLoadProvider(func() autonomous.HostLoadStatus { return want })

	s := planTierWebServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(m.Status()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))

	res, err := s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("autonomous_status: err=%v res=%+v", err, res)
	}
	text := budgetResultText(t, res)
	var body struct {
		HostLoad *autonomous.HostLoadStatus `json:"host_load"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("unmarshal tool text: %v\n%s", err, text)
	}
	if body.HostLoad == nil {
		t.Fatalf("host_load missing from the autonomous_status body:\n%s", text)
	}
	if *body.HostLoad != want {
		t.Fatalf("host_load = %+v, want %+v", *body.HostLoad, want)
	}
}
