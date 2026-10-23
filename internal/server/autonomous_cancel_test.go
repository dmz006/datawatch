// The PRD cancel / hard-delete response (and its audit entry) must name
// every session ID a kill was attempted on (killed_session_ids), so an
// operator can verify no unrelated session was stopped. killed_sessions
// keeps its original len(SessionIDsForPRD) semantics for existing clients.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionSpyAutonomous is a spyAutonomous whose SessionIDsForPRD returns
// a fixed list (the base spy returns nil).
type sessionSpyAutonomous struct {
	spyAutonomous
	ids []string
}

func (s *sessionSpyAutonomous) SessionIDsForPRD(string) []string { return s.ids }

func newCancelTestServer(t *testing.T, ids []string) (*Server, *audit.Log) {
	t.Helper()
	// A real manager with an empty store: Kill on these IDs returns
	// "not found" without touching tmux, which is all the handler needs.
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(sm.WaitBackground)
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	return &Server{autonomousMgr: &sessionSpyAutonomous{ids: ids}, manager: sm, auditLog: al}, al
}

func doPRDDelete(t *testing.T, s *Server, url string) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodDelete, url, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE %s: status=%d body=%s", url, rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	return body
}

func assertKilled(t *testing.T, m map[string]any, wantCount float64, wantIDs []any) {
	t.Helper()
	if got, ok := m["killed_sessions"].(float64); !ok || got != wantCount {
		t.Errorf("killed_sessions = %#v, want %v", m["killed_sessions"], wantCount)
	}
	raw, present := m["killed_session_ids"]
	if !present {
		t.Fatalf("killed_session_ids missing: %#v", m)
	}
	ids, ok := raw.([]any)
	if !ok {
		t.Fatalf("killed_session_ids = %#v, want JSON array (not null)", raw)
	}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Errorf("killed_session_ids = %#v, want %#v", ids, wantIDs)
	}
}

func lastAuditDetails(t *testing.T, al *audit.Log) map[string]any {
	t.Helper()
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil || len(entries) == 0 {
		t.Fatalf("audit read: %v (%d entries)", err, len(entries))
	}
	// Round-trip through JSON so slices compare like the HTTP body.
	b, _ := json.Marshal(entries[0].Details)
	var d map[string]any
	_ = json.Unmarshal(b, &d)
	return d
}

func TestAutonomousPRDCancel_NamesKilledSessionIDs(t *testing.T) {
	s, al := newCancelTestServer(t, []string{"ab95", "", "cd12"})
	body := doPRDDelete(t, s, "/api/autonomous/prds/prd-c1")
	if body["status"] != "cancelled" || body["id"] != "prd-c1" {
		t.Errorf("status/id = %v/%v", body["status"], body["id"])
	}
	// count keeps len(SessionIDsForPRD) (3, including the empty slot);
	// the empty ID is never killed so it's omitted from the array.
	assertKilled(t, body, 3, []any{"ab95", "cd12"})
	assertKilled(t, lastAuditDetails(t, al), 3, []any{"ab95", "cd12"})
}

func TestAutonomousPRDHardDelete_NamesKilledSessionIDs(t *testing.T) {
	s, al := newCancelTestServer(t, []string{"ab95", "", "cd12"})
	body := doPRDDelete(t, s, "/api/autonomous/prds/prd-d1?hard=true")
	if body["status"] != "deleted" || body["id"] != "prd-d1" {
		t.Errorf("status/id = %v/%v", body["status"], body["id"])
	}
	assertKilled(t, body, 3, []any{"ab95", "cd12"})
	assertKilled(t, lastAuditDetails(t, al), 3, []any{"ab95", "cd12"})
}

func TestAutonomousPRDCancel_NoSessionsIsEmptyArray(t *testing.T) {
	s, al := newCancelTestServer(t, nil)
	body := doPRDDelete(t, s, "/api/autonomous/prds/prd-c0")
	assertKilled(t, body, 0, []any{})
	assertKilled(t, lastAuditDetails(t, al), 0, []any{})
	if _, ok := body["kill_errors"]; ok {
		t.Errorf("kill_errors should be omitted when no kill failed: %#v", body)
	}
}
