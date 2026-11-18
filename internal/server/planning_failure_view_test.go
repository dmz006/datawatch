package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// planningFailureServer returns a server over a real manager holding two
// Automata: one whose planning failed and one that has no failure.
func planningFailureServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.Config{PlanningBackend: "ollama", ExecutionBackend: "claude-code"},
		func(autonomous.DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := newTestAPI(mgr)
	for _, spec := range []string{"first spec", "second spec"} {
		if _, err := mgr.CreatePRD(spec, "", "", "", ""); err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
	}
	return &Server{cfg: &config.Config{}, autonomousMgr: api}
}

func planningFailureIDs(t *testing.T, s *Server) (failed, plain string) {
	t.Helper()
	list := s.autonomousMgr.ListPRDs()
	if len(list) != 2 {
		t.Fatalf("want 2 Automata, got %d", len(list))
	}
	a, _ := toPRD(list[0])
	b, _ := toPRD(list[1])
	return a.ID, b.ID
}

func markPlanningFailed(t *testing.T, s *Server, id string) {
	t.Helper()
	mgr := s.autonomousMgr.(*autonomous.API).M
	prd, ok := mgr.Store().GetPRD(id)
	if !ok {
		t.Fatalf("no Automaton %s", id)
	}
	mgr.Store().WithLock(func() {
		prd.LastDecomposeFailure = &autonomous.FailureReason{
			Category: "content", Detail: `fixed version number "v9.1.0"`, Source: "release-version-fixed",
			Phrases: []string{"v9.1.0"}, Retryable: false, OccurredAt: time.Now(),
		}
		prd.RetryCountsByCategory = map[string]int{"content": 1}
	})
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
}

func getJSON(t *testing.T, s *Server, path string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleAutonomousPRDs(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestPlanningFailureRESTGetCarriesObject(t *testing.T) {
	s := planningFailureServer(t)
	failed, plain := planningFailureIDs(t, s)
	markPlanningFailed(t, s, failed)
	for _, detail := range []string{"", "summary", "tasks"} {
		path := "/api/autonomous/prds/" + failed
		if detail != "" {
			path += "?detail=" + detail
		}
		body := getJSON(t, s, path)
		pf, ok := body["planning_failure"].(map[string]any)
		if !ok {
			t.Fatalf("detail=%q: no planning_failure in %v", detail, body)
		}
		if pf["reason"] != `fixed version number "v9.1.0"` || pf["category"] != "content" || pf["source"] != "release-version-fixed" || pf["retryable"] != false {
			t.Errorf("detail=%q: wrong object %v", detail, pf)
		}
		if ph, _ := pf["phrases"].([]any); len(ph) != 1 || ph[0] != "v9.1.0" {
			t.Errorf("detail=%q: phrases %v", detail, pf["phrases"])
		}
		if rc, _ := pf["retry_counts"].(map[string]any); rc["content"] != float64(1) {
			t.Errorf("detail=%q: retry_counts %v", detail, pf["retry_counts"])
		}
		if _, ok := pf["retry_budget"]; !ok {
			t.Errorf("detail=%q: no retry_budget", detail)
		}
	}
	if _, has := getJSON(t, s, "/api/autonomous/prds/"+plain)["planning_failure"]; has {
		t.Error("an Automaton with no failure must omit planning_failure")
	}
}

func TestPlanningFailureRESTListCarriesObject(t *testing.T) {
	s := planningFailureServer(t)
	failed, plain := planningFailureIDs(t, s)
	markPlanningFailed(t, s, failed)
	for _, q := range []string{"?full=1", ""} {
		body := getJSON(t, s, "/api/autonomous/prds"+q)
		rows, _ := body["prds"].([]any)
		if len(rows) != 2 {
			t.Fatalf("q=%q: want 2 rows, got %d", q, len(rows))
		}
		for _, r := range rows {
			row := r.(map[string]any)
			_, has := row["planning_failure"]
			switch row["id"] {
			case failed:
				if !has {
					t.Errorf("q=%q: failed row lacks planning_failure", q)
				}
			case plain:
				if has {
					t.Errorf("q=%q: plain row must omit planning_failure", q)
				}
			}
		}
	}
}
