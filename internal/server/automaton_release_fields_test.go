package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// Release size and breaking-change setters on /api/autonomous/prds/{id}/...
// They run against the real autonomous manager so the stored record, the
// decision timeline and the operator audit log are all checked end to end.

func postReleaseField(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func auditEntriesFor(t *testing.T, al *audit.Log, action string) []audit.Entry {
	t.Helper()
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var out []audit.Entry
	for _, e := range entries {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

func TestReleaseSize_SetClearAndAudit(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	prd, err := mgr.CreatePRD("release size spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	path := "/api/autonomous/prds/" + prd.ID + "/set_release_size"

	if rec := postReleaseField(s, path, `{"release_size":"minor"}`); rec.Code != http.StatusOK {
		t.Fatalf("set minor: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := mgr.Store().GetPRD(prd.ID)
	if got.ReleaseSize != autonomous.ReleaseSizeMinor {
		t.Fatalf("ReleaseSize = %q, want minor", got.ReleaseSize)
	}

	if rec := postReleaseField(s, path, `{"release_size":" MAJOR "}`); rec.Code != http.StatusOK {
		t.Fatalf("set major (case and space): want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got, _ = mgr.Store().GetPRD(prd.ID); got.ReleaseSize != autonomous.ReleaseSizeMajor {
		t.Fatalf("ReleaseSize = %q, want major", got.ReleaseSize)
	}

	if rec := postReleaseField(s, path, `{"release_size":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clear: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got, _ = mgr.Store().GetPRD(prd.ID); got.ReleaseSize != "" {
		t.Fatalf("empty size must clear, got %q", got.ReleaseSize)
	}

	entries := auditEntriesFor(t, al, "set_release_size")
	if len(entries) != 3 {
		t.Fatalf("want 3 set_release_size audit entries, got %d", len(entries))
	}
	// Read returns newest first: the clear is entries[0], the first set is the last.
	clear := entries[0]
	if clear.Details["resource_type"] != "automaton" || clear.Details["resource_id"] != prd.ID {
		t.Errorf("audit identity wrong: %+v", clear.Details)
	}
	if v, ok := clear.Details["release_size"]; !ok || v != "" {
		t.Errorf("clear audit must record an empty release_size, got %v", v)
	}
	if first := entries[len(entries)-1]; first.Details["release_size"] != "minor" {
		t.Errorf("first audit release_size = %v, want minor", first.Details["release_size"])
	}
}

func TestReleaseSize_InvalidRefusedAndNotAudited(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	prd, err := mgr.CreatePRD("invalid size spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	path := "/api/autonomous/prds/" + prd.ID + "/set_release_size"

	rec := postReleaseField(s, path, `{"release_size":"huge"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid size: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "patch, minor, major or none") {
		t.Errorf("refusal must name the valid sizes: %s", rec.Body.String())
	}
	if got, _ := mgr.Store().GetPRD(prd.ID); got.ReleaseSize != "" {
		t.Errorf("refused size must not be stored, got %q", got.ReleaseSize)
	}
	if n := len(auditEntriesFor(t, al, "set_release_size")); n != 0 {
		t.Errorf("a refused size must not be audited, got %d entries", n)
	}
	if rec := postReleaseField(s, path, `nope`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: want 400, got %d", rec.Code)
	}
	if rec := postReleaseField(s, "/api/autonomous/prds/missing/set_release_size", `{"release_size":"patch"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d", rec.Code)
	}
	get := httptest.NewRecorder()
	s.handleAutonomousPRDs(get, httptest.NewRequest(http.MethodGet, path, nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: want 405, got %d", get.Code)
	}
}

func TestBreakingChange_SetClearAndAudit(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	prd, err := mgr.CreatePRD("breaking spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	path := "/api/autonomous/prds/" + prd.ID + "/set_breaking_change"

	if rec := postReleaseField(s, path, `{"breaking_change":true}`); rec.Code != http.StatusOK {
		t.Fatalf("set: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got, _ := mgr.Store().GetPRD(prd.ID); !got.BreakingChange {
		t.Fatal("breaking_change not stored")
	}
	if rec := postReleaseField(s, path, `{"breaking_change":false}`); rec.Code != http.StatusOK {
		t.Fatalf("clear: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got, _ := mgr.Store().GetPRD(prd.ID); got.BreakingChange {
		t.Fatal("breaking_change not cleared")
	}

	entries := auditEntriesFor(t, al, "set_breaking_change")
	if len(entries) != 2 {
		t.Fatalf("want 2 set_breaking_change audit entries, got %d", len(entries))
	}
	// Newest first: the clear is entries[0], the set is entries[1].
	if entries[0].Details["breaking_change"] != false || entries[1].Details["breaking_change"] != true {
		t.Errorf("audit values wrong: %+v / %+v", entries[0].Details, entries[1].Details)
	}
	if entries[1].Details["resource_id"] != prd.ID {
		t.Errorf("audit resource_id = %v, want %s", entries[1].Details["resource_id"], prd.ID)
	}
}

func TestBreakingChange_MissingOrInvalidRefused(t *testing.T) {
	s, mgr, al := newAutomationAuditServer(t)
	prd, err := mgr.CreatePRD("breaking refusal spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	path := "/api/autonomous/prds/" + prd.ID + "/set_breaking_change"

	// An omitted flag must not silently clear it, and a string is not a bool.
	for _, body := range []string{`{}`, `{"breaking_change":"yes"}`, `nope`} {
		if rec := postReleaseField(s, path, body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: want 400, got %d", body, rec.Code)
		}
	}
	if rec := postReleaseField(s, "/api/autonomous/prds/missing/set_breaking_change", `{"breaking_change":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d", rec.Code)
	}
	if n := len(auditEntriesFor(t, al, "set_breaking_change")); n != 0 {
		t.Errorf("refused calls must not be audited, got %d", n)
	}
}

// releaseSpy records the two setters and stands in for the manager so the
// federation gate can be checked without the real record.
type releaseSpy struct {
	*kindSpy
	sizeCalls     int
	breakingCalls int
}

func (r *releaseSpy) SetPRDReleaseSize(id, size string) (any, error) {
	r.sizeCalls++
	if !r.known[id] {
		return nil, errors.New("prd not found")
	}
	return map[string]any{"id": id, "release_size": size}, nil
}

func (r *releaseSpy) SetPRDBreakingChange(id string, _ bool) (any, error) {
	r.breakingCalls++
	if !r.known[id] {
		return nil, errors.New("prd not found")
	}
	return map[string]any{"id": id}, nil
}

func TestReleaseFields_FederationDenial(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	spy := &releaseSpy{kindSpy: newKindSpy("abc")}
	s.autonomousMgr = spy
	capTest(t, s,
		http.MethodPost, "/api/autonomous/prds/abc/set_release_size", []byte(`{"release_size":"patch"}`),
		s.handleAutonomousPRDs,
		http.StatusOK,
		http.StatusOK,
		http.StatusForbidden,
	)
	capTest(t, s,
		http.MethodPost, "/api/autonomous/prds/abc/set_breaking_change", []byte(`{"breaking_change":true}`),
		s.handleAutonomousPRDs,
		http.StatusOK,
		http.StatusOK,
		http.StatusForbidden,
	)
	if spy.sizeCalls != 2 || spy.breakingCalls != 2 {
		t.Fatalf("only admin and full-control may reach the manager: size=%d breaking=%d", spy.sizeCalls, spy.breakingCalls)
	}
}
