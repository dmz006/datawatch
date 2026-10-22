package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// resumeFake models a manager where only the PRD id "parked" is parked.
type resumeFake struct {
	gh201FakeAutonomousForAudit
	gotActor, gotNote string
}

func (f *resumeFake) ResumeParked(id, actor, note string) (any, error) {
	if id != "parked" {
		return nil, errors.New("prd " + id + " is not parked")
	}
	f.gotActor, f.gotNote = actor, note
	return map[string]any{"id": id, "status": "running"}, nil
}

// Resume is the paused-PRD path; nothing in this fake is paused.
func (f *resumeFake) Resume(id, actor string) (any, error) {
	return nil, errors.New("prd " + id + " is not paused; nothing to resume")
}

func postResume(srv *Server, id string, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/resume", bytes.NewReader(b))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)
	return w
}

func TestAutonomousResume_ParkedSucceedsAndIsAudited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	fake := &resumeFake{}
	srv.autonomousMgr = fake

	w := postResume(srv, "parked", map[string]any{"actor": "tester", "note": "go"})
	if w.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", w.Code, w.Body.String())
	}
	if fake.gotActor != "tester" || fake.gotNote != "go" {
		t.Errorf("actor/note = %q/%q", fake.gotActor, fake.gotNote)
	}
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "resume" {
		t.Fatalf("audit entries = %+v, want one resume entry", entries)
	}
}

func TestAutonomousResume_NonParkedIsBadRequestAndNotAudited(t *testing.T) {
	srv, al := gh201Phase4Server(t)
	srv.autonomousMgr = &resumeFake{}

	w := postResume(srv, "running-one", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("resume non-parked: %d %s, want 400", w.Code, w.Body.String())
	}
	entries, _ := al.Read(audit.QueryFilter{})
	if len(entries) != 0 {
		t.Errorf("audit entries = %+v, want none", entries)
	}
}

func TestAutonomousResume_RejectsGet(t *testing.T) {
	srv, _ := gh201Phase4Server(t)
	srv.autonomousMgr = &resumeFake{}
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/parked/resume", nil)
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET resume: %d, want 405", w.Code)
	}
}
