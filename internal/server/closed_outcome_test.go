package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// coRESTServer builds a Server over a real Manager with the archive guard on.
func coRESTServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	cfg := autonomous.DefaultConfig()
	cfg.ClosedOutcomeArchive = autonomous.ClosedOutcomeOn
	m, err := autonomous.NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{autonomousMgr: autonomous.NewAPI(m)}, m
}

func coRESTPRD(t *testing.T, m *autonomous.Manager, status autonomous.PRDStatus) string {
	t.Helper()
	p, err := m.CreatePRD("spec", "/p", "claude", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Store().SetStories(p.ID, []autonomous.Story{{Title: "S", Tasks: []autonomous.Task{{Title: "T", Spec: "do"}}}}); err != nil {
		t.Fatal(err)
	}
	m.Store().MutatePRD(p.ID, func(q *autonomous.PRD) bool { q.Status = status; return true })
	return p.ID
}

func coPost(s *Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestClosedOutcomeREST_ArchiveRefusesThenAcceptsOutcome(t *testing.T) {
	s, m := coRESTServer(t)
	id := coRESTPRD(t, m, autonomous.PRDCompleted)
	rec := coPost(s, "/api/autonomous/prds/"+id+"/archive", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "outcome") {
		t.Fatalf("archive with no outcome: %d %s", rec.Code, rec.Body.String())
	}
	rec = coPost(s, "/api/autonomous/prds/"+id+"/archive", `{"outcome":"dropped","note":"probe"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive with outcome: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["status"] != "archived" || got["closed_outcome"] != "dropped" || got["closed_outcome_note"] != "probe" {
		t.Fatalf("reply = %v", got)
	}
}

func TestClosedOutcomeREST_SetOutcomeAndValidation(t *testing.T) {
	s, m := coRESTServer(t)
	id := coRESTPRD(t, m, autonomous.PRDArchived)
	rec := coPost(s, "/api/autonomous/prds/"+id+"/outcome", `{"outcome":"superseded"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "replaced") {
		t.Fatalf("superseded with no replacement must be 400: %d %s", rec.Code, rec.Body.String())
	}
	rec = coPost(s, "/api/autonomous/prds/"+id+"/outcome", `{"outcome":"dropped","note":"old"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"closed_outcome":"dropped"`) {
		t.Fatalf("set outcome: %d %s", rec.Code, rec.Body.String())
	}
	if rec := coPost(s, "/api/autonomous/prds/nosuch/outcome", `{"outcome":"dropped"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id must be 404, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+id+"/outcome", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET must be 405, got %d", rec.Code)
	}
}

func TestClosedOutcomeREST_CancelRecordsOutcome(t *testing.T) {
	s, m := coRESTServer(t)
	id := coRESTPRD(t, m, autonomous.PRDRunning)
	if rec := coPost(s, "/api/autonomous/prds/"+id+"/cancel", `{"outcome":"held","note":"until 10.0"}`); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}
	p, _ := m.Store().GetPRD(id)
	if p.Status != autonomous.PRDCancelled || p.ClosedOutcome != autonomous.OutcomeHeld {
		t.Fatalf("status=%s outcome=%q", p.Status, p.ClosedOutcome)
	}
}

func TestClosedOutcomeREST_OutcomeCheckReadAndRun(t *testing.T) {
	s, m := coRESTServer(t)
	id := coRESTPRD(t, m, autonomous.PRDArchived)
	rec := httptest.NewRecorder()
	s.handleOutcomeCheck(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/outcome-check", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	var rep struct {
		Mode     string `json:"mode"`
		Findings []struct {
			Automaton string `json:"automaton"`
			Kind      string `json:"kind"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Mode != "shadow" || len(rep.Findings) != 1 || rep.Findings[0].Automaton != id || rep.Findings[0].Kind != "closed_no_outcome" {
		t.Fatalf("report = %+v", rep)
	}
	rec = httptest.NewRecorder()
	s.handleOutcomeCheck(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/outcome-check", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"run_at"`) {
		t.Fatalf("POST run: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleOutcomeCheck(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/outcome-check", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT must be 405, got %d", rec.Code)
	}
}

func TestClosedOutcomeREST_DeployRecord(t *testing.T) {
	s, m := coRESTServer(t)
	id := coRESTPRD(t, m, autonomous.PRDCompleted)
	m.Store().MutatePRD(id, func(p *autonomous.PRD) bool { p.MergedInRelease, p.MergeCommit = "v9.1.0", "abc1234"; return true })
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleDeployRecord(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/deploy/record", strings.NewReader(body)))
		return rec
	}
	rec := post(`{"release":"v9.1.0","host":"h1","health":"passed","detail":"ok"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), id) {
		t.Fatalf("deploy record: %d %s", rec.Code, rec.Body.String())
	}
	p, _ := m.Store().GetPRD(id)
	if p.Deploy == nil || p.Deploy.Version != "v9.1.0" || p.Deploy.Host != "h1" {
		t.Fatalf("deploy = %+v", p.Deploy)
	}
	if rec := post(`{"release":"v9.1.0","health":"great"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad health must be 400, got %d", rec.Code)
	}
	if rec := post(`{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no release must be 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleDeployRecord(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/deploy/record", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET must be 405, got %d", rec.Code)
	}
}
