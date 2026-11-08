package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// TestAutonomousStatusIncludesHostLoad — the autonomous status read (GET
// /api/autonomous/status, which the autonomous_status MCP tool returns
// verbatim) carries the host load check snapshot when the provider is wired,
// and omits the field while none is wired — the same shape the daemon's main
// wires in from HostLoadCheck.Status.
func TestAutonomousStatusIncludesHostLoad(t *testing.T) {
	statusBody := func(t *testing.T, m *autonomous.Manager) []byte {
		t.Helper()
		s := &Server{autonomousMgr: newTestAPI(m)}
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/status", nil)
		rec := httptest.NewRecorder()
		s.handleAutonomousStatus(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/autonomous/status = %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}

	// No provider wired: the field is omitted entirely.
	m0, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	var body0 map[string]json.RawMessage
	if err := json.Unmarshal(statusBody(t, m0), &body0); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body0["host_load"]; ok {
		t.Fatal("host_load must be omitted while no host load check is wired")
	}

	// Wired: the snapshot is carried through verbatim.
	want := autonomous.HostLoadStatus{Mode: "reap", LastLoad: 7, LastLimit: 6, Breached: true, OrphansReaped: 2}
	m1, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m1.SetHostLoadProvider(func() autonomous.HostLoadStatus { return want })
	var body1 struct {
		HostLoad *autonomous.HostLoadStatus `json:"host_load"`
	}
	if err := json.Unmarshal(statusBody(t, m1), &body1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body1.HostLoad == nil {
		t.Fatal("host_load missing from the status body")
	}
	if *body1.HostLoad != want {
		t.Fatalf("host_load = %+v, want %+v", *body1.HostLoad, want)
	}
}
