// Pure tests for the cancelled-to-Completed migration HTTP surface. A small
// in-memory fake stands in for the manager: it holds one cancelled merged
// record and one cancelled unmerged record, and moves only the merged one on
// a real run, so the handler's pass-through of dry_run, limit and id, its
// status mapping, its audit entry and its capability gate are all checked.

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

type completedMigrateCall struct {
	dryRun bool
	limit  int
	id     string
}

type completedMigrateFake struct {
	fakeOrchAutonomous

	calls  []completedMigrateCall
	err    error
	status map[string]string // id -> status
	merged map[string]bool   // id -> work merged
}

func newCompletedMigrateFake() *completedMigrateFake {
	return &completedMigrateFake{
		status: map[string]string{"aaaa0001": "cancelled", "bbbb0002": "cancelled"},
		merged: map[string]bool{"aaaa0001": true, "bbbb0002": false},
	}
}

func (f *completedMigrateFake) MigrateCompleted(dryRun bool, limit int, id string) (any, error) {
	f.calls = append(f.calls, completedMigrateCall{dryRun, limit, id})
	if f.err != nil {
		return nil, f.err
	}
	rep := autonomous.CompletedMigrationReport{DryRun: dryRun}
	for _, rid := range []string{"aaaa0001", "bbbb0002"} {
		if id != "" && id != rid {
			continue
		}
		if f.status[rid] != "cancelled" {
			continue
		}
		rep.Considered++
		if !f.merged[rid] {
			continue
		}
		item := autonomous.CompletedMigrationItem{ID: rid, Kind: "automaton", From: "cancelled", To: "completed"}
		if dryRun {
			rep.WouldComplete = append(rep.WouldComplete, item)
			continue
		}
		f.status[rid] = "completed"
		rep.Changed++
		rep.Completed = append(rep.Completed, item)
	}
	return rep, nil
}

func postMigrate(s *Server, path string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(http.MethodPost, path, nil)
	}
	s.handleAutonomousCompletedMigrate(rec, req)
	return rec
}

func TestCompletedMigrationREST_DryRunWritesNothing(t *testing.T) {
	f := newCompletedMigrateFake()
	rec := postMigrate(&Server{autonomousMgr: f}, "/api/autonomous/completed/migrate?dry_run=true", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(f.calls) != 1 || !f.calls[0].dryRun {
		t.Fatalf("calls = %+v, want one dry run", f.calls)
	}
	var rep autonomous.CompletedMigrationReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.WouldComplete) != 1 || rep.WouldComplete[0].ID != "aaaa0001" || rep.Changed != 0 {
		t.Errorf("report = %+v, want aaaa0001 listed and nothing changed", rep)
	}
	if f.status["aaaa0001"] != "cancelled" {
		t.Errorf("dry run wrote: status = %q", f.status["aaaa0001"])
	}
}

func TestCompletedMigrationREST_RealRunChangesMergedRecordOnly(t *testing.T) {
	f := newCompletedMigrateFake()
	rec := postMigrate(&Server{autonomousMgr: f}, "/api/autonomous/completed/migrate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if f.status["aaaa0001"] != "completed" {
		t.Errorf("merged record status = %q, want completed", f.status["aaaa0001"])
	}
	if f.status["bbbb0002"] != "cancelled" {
		t.Errorf("unmerged record status = %q, want cancelled", f.status["bbbb0002"])
	}
	var rep autonomous.CompletedMigrationReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Changed != 1 || len(rep.Completed) != 1 {
		t.Errorf("report = %+v, want one change", rep)
	}
}

func TestCompletedMigrationREST_ParametersFromQueryAndBody(t *testing.T) {
	f := newCompletedMigrateFake()
	s := &Server{autonomousMgr: f}
	if rec := postMigrate(s, "/api/autonomous/completed/migrate?dry_run=true&limit=3&id=aaaa0001", nil); rec.Code != http.StatusOK {
		t.Fatalf("query: %d %s", rec.Code, rec.Body.String())
	}
	body := []byte(`{"dry_run":true,"limit":5,"id":"bbbb0002"}`)
	if rec := postMigrate(s, "/api/autonomous/completed/migrate", body); rec.Code != http.StatusOK {
		t.Fatalf("body: %d %s", rec.Code, rec.Body.String())
	}
	want := []completedMigrateCall{{true, 3, "aaaa0001"}, {true, 5, "bbbb0002"}}
	if len(f.calls) != 2 || f.calls[0] != want[0] || f.calls[1] != want[1] {
		t.Fatalf("calls = %+v, want %+v", f.calls, want)
	}
}

func TestCompletedMigrationREST_BadParametersAre400(t *testing.T) {
	for _, path := range []string{
		"/api/autonomous/completed/migrate?dry_run=bogus",
		"/api/autonomous/completed/migrate?limit=-1",
		"/api/autonomous/completed/migrate?limit=abc",
	} {
		f := newCompletedMigrateFake()
		rec := postMigrate(&Server{autonomousMgr: f}, path, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", path, rec.Code)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: manager called %v", path, f.calls)
		}
	}
	f := newCompletedMigrateFake()
	if rec := postMigrate(&Server{autonomousMgr: f}, "/api/autonomous/completed/migrate", []byte(`{"limit":-2}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("body limit -2: want 400, got %d", rec.Code)
	}
	if rec := postMigrate(&Server{autonomousMgr: f}, "/api/autonomous/completed/migrate", []byte(`{not json`)); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: want 400, got %d", rec.Code)
	}
}

func TestCompletedMigrationREST_BusyIs409(t *testing.T) {
	f := newCompletedMigrateFake()
	f.err = autonomous.ErrCompletedMigrationBusy
	rec := postMigrate(&Server{autonomousMgr: f}, "/api/autonomous/completed/migrate", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), autonomous.ErrCompletedMigrationBusy.Error()) {
		t.Errorf("body %q lacks the plain message", rec.Body.String())
	}
	f2 := newCompletedMigrateFake()
	f2.err = errors.New("no base repository configured")
	if rec := postMigrate(&Server{autonomousMgr: f2}, "/api/autonomous/completed/migrate", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("other failure: want 400, got %d", rec.Code)
	}
}

func TestCompletedMigrationREST_MethodCheck(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		f := newCompletedMigrateFake()
		rec := httptest.NewRecorder()
		(&Server{autonomousMgr: f}).handleAutonomousCompletedMigrate(rec, httptest.NewRequest(m, "/api/autonomous/completed/migrate", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", m, rec.Code)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s ran the migration", m)
		}
	}
}

func TestCompletedMigrationREST_AuditEntry(t *testing.T) {
	dir := t.TempDir()
	al, err := audit.NewAt(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	f := newCompletedMigrateFake()
	s := &Server{autonomousMgr: f, auditLog: al}
	if rec := postMigrate(s, "/api/autonomous/completed/migrate?dry_run=true&limit=4", nil); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Action == "completed_migrate" {
			found = true
			if e.Details["dry_run"] != true || e.Details["limit"] != float64(4) || e.Details["report"] == nil {
				t.Errorf("audit details = %+v", e.Details)
			}
		}
	}
	if !found {
		t.Fatalf("no completed_migrate audit entry in %+v", entries)
	}
}

func TestCompletedMigrationREST_CapabilityGate(t *testing.T) {
	s, fullTok, roTok := fedCapFixture(t)
	f := newCompletedMigrateFake()
	s.autonomousMgr = f
	_ = fullTok
	_ = roTok
	// admin 200, full-control peer 200, read-only peer 403.
	capTest(t, s, http.MethodPost, "/api/autonomous/completed/migrate?dry_run=true", nil,
		s.handleAutonomousCompletedMigrate, http.StatusOK, http.StatusOK, http.StatusForbidden)
}
