package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// completedUnmergedSpy is the fake manager for the completed-unmerged read.
type completedUnmergedSpy struct {
	fakeOrchAutonomous
	list  []autonomous.CompletedUnmerged
	calls int
}

func (s *completedUnmergedSpy) CompletedUnmerged(context.Context) []autonomous.CompletedUnmerged {
	s.calls++
	return s.list
}

func completedUnmergedGet(s *Server, method string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousCompletedUnmerged(rec, httptest.NewRequest(method, "/api/autonomous/completed-unmerged", nil))
	return rec
}

func TestCompletedUnmergedRESTListsEntries(t *testing.T) {
	spy := &completedUnmergedSpy{list: []autonomous.CompletedUnmerged{{ID: "a1", Branch: "automaton/a1", Detail: "not an ancestor"}}}
	rec := completedUnmergedGet(&Server{autonomousMgr: spy}, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Items []autonomous.CompletedUnmerged `json:"completed_unmerged"`
		Count int                            `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 1 || len(got.Items) != 1 || got.Items[0].ID != "a1" || got.Items[0].Branch != "automaton/a1" || got.Items[0].Detail != "not an ancestor" {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestCompletedUnmergedRESTEmptyIsArray(t *testing.T) {
	rec := completedUnmergedGet(&Server{autonomousMgr: &completedUnmergedSpy{}}, http.MethodGet)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["completed_unmerged"]) != "[]" || string(raw["count"]) != "0" {
		t.Errorf("empty body = %s", rec.Body)
	}
}

func TestCompletedUnmergedRESTReadOnlyAndGated(t *testing.T) {
	spy := &completedUnmergedSpy{}
	s := &Server{autonomousMgr: spy}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := completedUnmergedGet(s, m)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
			t.Errorf("%s: status=%d allow=%q", m, rec.Code, rec.Header().Get("Allow"))
		}
	}
	if spy.calls != 0 {
		t.Errorf("a refused method reached the manager %d times", spy.calls)
	}
	if rec := completedUnmergedGet(&Server{}, http.MethodGet); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled autonomous: status = %d", rec.Code)
	}
	if rec := completedUnmergedGet(&Server{autonomousMgr: &fakeOrchAutonomous{}}, http.MethodGet); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("manager without the read: status = %d", rec.Code)
	}
}
