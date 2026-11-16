// Project link over REST: set and clear, an unknown project refused, the
// effective project and its source on the read view, the backfill routes, the
// federation guard and the OpenAPI entries.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func newProjectLinkServer(t *testing.T) (*Server, *autonomous.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(sm)
	trackSessionManager(t, sm)
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "datawatch")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr.SetProjectLinkProjectsFn(func() map[string]autonomous.ProjectLinkProject {
		return map[string]autonomous.ProjectLinkProject{"datawatch": {Dir: repo}}
	})
	srv := NewServer(NewHub(), sm, "h", "", nil, &config.Config{}, filepath.Join(dir, "config.yaml"))
	srv.SetAutonomousAPI(newTestAPI(mgr))
	return srv, mgr, repo
}

func projectLinkPost(srv *Server, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return w
}

func TestProjectLinkRESTSetClearAndReadView(t *testing.T) {
	srv, mgr, _ := newProjectLinkServer(t)
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/autonomous/prds/" + prd.ID

	w := projectLinkPost(srv, base+"/set_project", map[string]string{"project": "datawatch"})
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["project"] != "datawatch" || got["project_source"] != "link" {
		t.Fatalf("set reply project=%v source=%v", got["project"], got["project_source"])
	}

	gw := httptest.NewRecorder()
	srv.handleAutonomousPRDs(gw, httptest.NewRequest(http.MethodGet, base, nil))
	var view map[string]any
	_ = json.Unmarshal(gw.Body.Bytes(), &view)
	if view["project"] != "datawatch" || view["project_source"] != "link" {
		t.Fatalf("read view project=%v source=%v body=%s", view["project"], view["project_source"], gw.Body.String())
	}

	w = projectLinkPost(srv, base+"/set_project", map[string]string{"project": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if p, _ := mgr.Store().GetPRD(prd.ID); p.Project != "" {
		t.Fatalf("link not cleared: %q", p.Project)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["project_source"] != "none" {
		t.Fatalf("after clear source = %v", got["project_source"])
	}
}

func TestProjectLinkRESTUnknownRefused(t *testing.T) {
	srv, mgr, _ := newProjectLinkServer(t)
	prd, _ := mgr.CreatePRD("spec", t.TempDir(), "", "", "")
	w := projectLinkPost(srv, "/api/autonomous/prds/"+prd.ID+"/set_project", map[string]string{"project": "nope"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "datawatch") {
		t.Fatalf("status %d body %q", w.Code, w.Body.String())
	}
	if p, _ := mgr.Store().GetPRD(prd.ID); p.Project != "" {
		t.Fatal("refused call changed the record")
	}
	if w := projectLinkPost(srv, "/api/autonomous/prds/missing/set_project", map[string]string{"project": "datawatch"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing Automaton: %d", w.Code)
	}
	gw := httptest.NewRecorder()
	srv.handleAutonomousPRDs(gw, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/"+prd.ID+"/set_project", nil))
	if gw.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET set_project: %d", gw.Code)
	}
}

func TestProjectLinkBackfillREST(t *testing.T) {
	srv, mgr, repo := newProjectLinkServer(t)
	prd, _ := mgr.CreatePRD("spec", repo, "", "", "")

	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.handleAutonomousProjectLinkBackfill(w, httptest.NewRequest(http.MethodGet, "/api/autonomous/project-link-backfill", nil))
		return w
	}
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.handleAutonomousProjectLinkBackfill(w, httptest.NewRequest(http.MethodPost, "/api/autonomous/project-link-backfill", strings.NewReader(body)))
		return w
	}
	if w := get(); w.Code != http.StatusNotFound {
		t.Fatalf("GET before any pass: %d", w.Code)
	}
	w := post(`{"apply":false}`)
	var rep autonomous.ProjectLinkBackfillReport
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rep) != nil || rep.Apply || rep.Counts.Matched != 1 || rep.Counts.Applied != 0 {
		t.Fatalf("dry run: %d %s", w.Code, w.Body.String())
	}
	if p, _ := mgr.Store().GetPRD(prd.ID); p.Project != "" {
		t.Fatal("dry run wrote a link")
	}
	if w := get(); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"matched":1`) {
		t.Fatalf("GET latest: %d %s", w.Code, w.Body.String())
	}
	w = post(`{"apply":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	if !rep.Apply || rep.Counts.Applied != 1 {
		t.Fatalf("apply report: %+v", rep)
	}
	if p, _ := mgr.Store().GetPRD(prd.ID); p.Project != "datawatch" {
		t.Fatalf("link = %q", p.Project)
	}
	if w := post(`{bad`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", w.Code)
	}
	w = httptest.NewRecorder()
	srv.handleAutonomousProjectLinkBackfill(w, httptest.NewRequest(http.MethodDelete, "/api/autonomous/project-link-backfill", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", w.Code)
	}
}

// The write routes need the autonomous write capability; the report read needs
// the read capability.
func TestProjectLinkFederationGuard(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	body := mustMarshal(map[string]any{"apply": false})
	pushCapTest(t, s, http.MethodPost, "/api/autonomous/project-link-backfill", body,
		s.handleAutonomousProjectLinkBackfill,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusForbidden, http.StatusForbidden)
	pushCapTest(t, s, http.MethodGet, "/api/autonomous/project-link-backfill", nil,
		s.handleAutonomousProjectLinkBackfill,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusForbidden)
	// With a manager wired, set_project answers 404 for an unknown Automaton
	// once the guard lets the call through.
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAutonomousAPI(newTestAPI(mgr))
	pushCapTest(t, s, http.MethodPost, "/api/autonomous/prds/p1/set_project", mustMarshal(map[string]string{"project": "x"}),
		s.handleAutonomousPRDs,
		http.StatusNotFound, http.StatusNotFound, http.StatusForbidden, http.StatusForbidden)
}

func TestProjectLinkOpenAPIListsRoutes(t *testing.T) {
	for _, p := range []string{"web/openapi.yaml", "../../docs/api/openapi.yaml"} {
		spec, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		for _, want := range []string{
			"/api/autonomous/prds/{id}/set_project:",
			"/api/autonomous/project-link-backfill:",
			"project_source:",
			"ProjectLinkBackfillReport:",
		} {
			if !strings.Contains(string(spec), want) {
				t.Errorf("%s must document %s", p, want)
			}
		}
	}
}
