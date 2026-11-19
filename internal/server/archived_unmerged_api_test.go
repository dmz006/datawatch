package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// aumFake is the AutonomousAPI double for the finder and carry-forward. Times
// come from its injected clock, never a calendar date.
type aumFake struct {
	fakeOrchAutonomous
	clock   func() time.Time
	entries []autonomous.ArchivedUnmergedEntry
	carried map[string]string
	calls   []string
	err     error
}

func (f *aumFake) ArchivedUnmerged(context.Context) []autonomous.ArchivedUnmergedEntry {
	return f.entries
}

func (f *aumFake) CarryForwardArchivedForced(id, actor string, force bool) (*autonomous.PRD, error) {
	f.calls = append(f.calls, id+"|"+actor)
	if f.err != nil {
		return nil, f.err
	}
	if _, done := f.carried[id]; done && !force {
		return nil, errors.New("carry forward: automaton \"" + id + "\" was already carried; pass force to carry again")
	}
	now := f.clock()
	d := &autonomous.PRD{ID: "draft-" + id, Title: "Parser rewrite", Status: autonomous.PRDDraft, CarriedFromID: id, CreatedAt: now, UpdatedAt: now}
	d.Git.Branch = "work/parser"
	f.carried[id] = d.ID
	return d, nil
}

func newAumFake() *aumFake {
	base := time.Now().UTC().Truncate(time.Second)
	f := &aumFake{clock: func() time.Time { return base }, carried: map[string]string{}}
	// A completed record that still holds unmerged commits.
	f.entries = []autonomous.ArchivedUnmergedEntry{{
		ID: "done-1", Title: "Parser rewrite", FormerStatus: "completed",
		Branch: "work/parser", BaseBranch: "main", Ahead: 3,
		ArchivedAt: base.Add(-time.Hour), State: autonomous.ArchivedUnmergedAhead,
	}}
	return f
}

func aumDo(s *Server, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleArchivedUnmerged(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestArchivedUnmergedListCompletedRecord(t *testing.T) {
	s := &Server{autonomousMgr: newAumFake()}
	rec := aumDo(s, http.MethodGet, "/api/autonomous/archived-unmerged", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Entries []autonomous.ArchivedUnmergedEntry `json:"archived_unmerged"`
		Total   int                                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Entries[0].FormerStatus != "completed" || got.Entries[0].Ahead != 3 || got.Entries[0].Title != "Parser rewrite" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestArchivedUnmergedListEmptyIsArray(t *testing.T) {
	f := newAumFake()
	f.entries = nil
	rec := aumDo(&Server{autonomousMgr: f}, http.MethodGet, "/api/autonomous/archived-unmerged", "")
	if !strings.Contains(rec.Body.String(), `"archived_unmerged":[]`) {
		t.Fatalf("empty list must be an array: %s", rec.Body.String())
	}
}

func TestArchivedUnmergedCarryForwardReturnsDraft(t *testing.T) {
	f := newAumFake()
	s := &Server{autonomousMgr: f}
	rec := aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged/done-1/carry-forward", `{"actor":"tester"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var d autonomous.PRD
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.ID != "draft-done-1" || d.Status != autonomous.PRDDraft || d.CarriedFromID != "done-1" || d.Git.Branch != "work/parser" {
		t.Fatalf("draft = %+v", d)
	}
	if len(f.calls) != 1 || f.calls[0] != "done-1|tester" {
		t.Fatalf("calls = %v", f.calls)
	}
	// Empty body falls back to the default actor.
	rec = aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged/done-1/carry-forward", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second carry without force: want 409, got %d", rec.Code)
	}
	if f.calls[1] != "done-1|operator" {
		t.Fatalf("default actor = %v", f.calls)
	}
	rec = aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged/done-1/carry-forward", `{"force":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("force: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestArchivedUnmergedRoutesRefuseBadRequests(t *testing.T) {
	f := newAumFake()
	s := &Server{autonomousMgr: f}
	f.err = errors.New(`carry forward: automaton "nope" not found`)
	if rec := aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged/nope/carry-forward", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d", rec.Code)
	}
	if rec := aumDo(s, http.MethodGet, "/api/autonomous/archived-unmerged/done-1/carry-forward", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET carry: want 405, got %d", rec.Code)
	}
	if rec := aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST list: want 405, got %d", rec.Code)
	}
	if rec := aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged/a/b/carry-forward", ""); rec.Code != http.StatusNotFound {
		t.Errorf("nested path: want 404, got %d", rec.Code)
	}
	if rec := aumDo(s, http.MethodPost, "/api/autonomous/archived-unmerged/done-1/carry-forward", "{not json"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: want 400, got %d", rec.Code)
	}
	if rec := aumDo(&Server{}, http.MethodGet, "/api/autonomous/archived-unmerged", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no manager: want 503, got %d", rec.Code)
	}
	if rec := aumDo(&Server{autonomousMgr: &fakeOrchAutonomous{}}, http.MethodGet, "/api/autonomous/archived-unmerged", ""); rec.Code != http.StatusNotImplemented {
		t.Errorf("no finder: want 501, got %d", rec.Code)
	}
}

func aumKeepGet(t *testing.T, s *Server) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(getAutonomousConfigMap(t, s)["archive_keep_unmerged"], &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestArchivedUnmergedKeepSettingRoundTrip(t *testing.T) {
	const key = "autonomous.archive_keep_unmerged"
	s, _ := newLoopServer(t)
	if g := aumKeepGet(t, s); g != true {
		t.Fatalf("GET default = %v, want true", g)
	}
	for _, v := range []bool{false, true} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: v})
		if g := aumKeepGet(t, s); g != v {
			t.Errorf("PUT %v, GET %v", v, g)
		}
	}
}

func TestArchivedUnmergedKeepSettingRefusesBadValue(t *testing.T) {
	const key = "autonomous.archive_keep_unmerged"
	for _, bad := range []any{"maybe", 1, 0.5, []string{"x"}} {
		s, _ := newLoopServer(t)
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: false})
		b, _ := json.Marshal(map[string]any{key: bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%v: want 400, got %d", bad, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), "archive_keep_unmerged") {
			t.Errorf("%v: error does not name the key: %q", bad, rr.Body.String())
		}
		if g := aumKeepGet(t, s); g != false {
			t.Errorf("%v: refused PUT changed the value to %v", bad, g)
		}
	}
}
