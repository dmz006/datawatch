package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// kindSpy records the kind setter and the create call. Only the ids in known
// exist.
type kindSpy struct {
	fakeOrchAutonomous
	known      map[string]bool
	setID      string
	setKind    string
	setCalls   int
	createCall int
	deleted    []string
}

func newKindSpy(ids ...string) *kindSpy {
	k := &kindSpy{known: map[string]bool{}}
	for _, id := range ids {
		k.known[id] = true
	}
	return k
}

func (k *kindSpy) SetPRDKind(id, kind string) (any, error) {
	k.setCalls++
	norm, ok := autonomous.NormalizePRDKind(kind)
	if !ok {
		return nil, errors.New("unknown kind")
	}
	if !k.known[id] {
		return nil, errors.New("prd not found")
	}
	k.setID, k.setKind = id, norm
	return map[string]any{"id": id, "kind": norm}, nil
}

func (k *kindSpy) GetPRD(id string) (any, bool) {
	if !k.known[id] {
		return nil, false
	}
	return map[string]any{"id": id, "kind": k.setKind}, true
}

func (k *kindSpy) CreatePRD(string, string, string, string, string) (any, error) {
	k.createCall++
	k.known["new1"] = true
	return map[string]any{"id": "new1"}, nil
}

func (k *kindSpy) DeletePRD(id string) error {
	k.deleted = append(k.deleted, id)
	return nil
}

func postKind(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestProgressKind_SetValid(t *testing.T) {
	for _, kind := range []string{"feature", "bug", "other", " Bug "} {
		spy := newKindSpy("abc")
		s := &Server{autonomousMgr: spy}
		rec := postKind(s, "/api/autonomous/prds/abc/set_kind", `{"kind":"`+kind+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("kind %q: want 200, got %d: %s", kind, rec.Code, rec.Body.String())
		}
		want, _ := autonomous.NormalizePRDKind(kind)
		if spy.setID != "abc" || spy.setKind != want {
			t.Fatalf("kind %q not applied: id=%q kind=%q", kind, spy.setID, spy.setKind)
		}
	}
}

func TestProgressKind_SetEmptyClears(t *testing.T) {
	spy := newKindSpy("abc")
	spy.setKind = "bug"
	s := &Server{autonomousMgr: spy}
	rec := postKind(s, "/api/autonomous/prds/abc/set_kind", `{"kind":""}`)
	if rec.Code != http.StatusOK || spy.setCalls != 1 || spy.setKind != "" {
		t.Fatalf("empty kind must clear: code=%d calls=%d kind=%q", rec.Code, spy.setCalls, spy.setKind)
	}
}

func TestProgressKind_SetInvalid(t *testing.T) {
	spy := newKindSpy("abc")
	s := &Server{autonomousMgr: spy}
	rec := postKind(s, "/api/autonomous/prds/abc/set_kind", `{"kind":"chore"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind must be 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "feature, bug or other") {
		t.Fatalf("error must name the valid kinds: %s", rec.Body.String())
	}
	if spy.setCalls != 0 {
		t.Fatalf("an unknown kind must not reach the manager, got %d calls", spy.setCalls)
	}
	if rec := postKind(s, "/api/autonomous/prds/abc/set_kind", `nope`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json must be 400, got %d", rec.Code)
	}
}

func TestProgressKind_SetUnknownID(t *testing.T) {
	spy := newKindSpy("abc")
	s := &Server{autonomousMgr: spy}
	rec := postKind(s, "/api/autonomous/prds/missing/set_kind", `{"kind":"bug"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id must be 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProgressKind_SetMethodGuard(t *testing.T) {
	s := &Server{autonomousMgr: newKindSpy("abc")}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/abc/set_kind", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET must be 405, got %d", rec.Code)
	}
}

func TestProgressKind_FederationDenial(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	spy := newKindSpy("abc")
	s.autonomousMgr = spy
	capTest(t, s,
		http.MethodPost, "/api/autonomous/prds/abc/set_kind", []byte(`{"kind":"bug"}`),
		s.handleAutonomousPRDs,
		http.StatusOK,        // admin: unrestricted
		http.StatusOK,        // full-control: holds the autonomous write capability
		http.StatusForbidden, // read-only peer: lacks it
	)
	if spy.setCalls != 2 {
		t.Fatalf("only the admin and full-control calls may reach the manager, got %d", spy.setCalls)
	}
}

func TestProgressKind_CreateWithKind(t *testing.T) {
	spy := newKindSpy()
	s := &Server{autonomousMgr: spy}
	rec := postKind(s, "/api/autonomous/prds", `{"spec":"fix the leak","project_dir":"/srv/x","kind":"bug"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create with kind: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if spy.createCall != 1 || spy.setID != "new1" || spy.setKind != "bug" {
		t.Fatalf("kind not applied to the new Automaton: creates=%d id=%q kind=%q", spy.createCall, spy.setID, spy.setKind)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["kind"] != "bug" {
		t.Fatalf("reply must carry the kind: %v %s", err, rec.Body.String())
	}
}

func TestProgressKind_CreateWithoutKindLeavesItUnset(t *testing.T) {
	spy := newKindSpy()
	s := &Server{autonomousMgr: spy}
	rec := postKind(s, "/api/autonomous/prds", `{"spec":"add paging","project_dir":"/srv/x"}`)
	if rec.Code != http.StatusOK || spy.createCall != 1 || spy.setCalls != 0 {
		t.Fatalf("code=%d creates=%d kind calls=%d", rec.Code, spy.createCall, spy.setCalls)
	}
}

func TestProgressKind_CreateWithInvalidKindCreatesNothing(t *testing.T) {
	spy := newKindSpy()
	s := &Server{autonomousMgr: spy}
	rec := postKind(s, "/api/autonomous/prds", `{"spec":"x","project_dir":"/srv/x","kind":"chore"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind on create must be 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if spy.createCall != 0 {
		t.Fatalf("nothing may be created for an unknown kind, got %d creates", spy.createCall)
	}
}
