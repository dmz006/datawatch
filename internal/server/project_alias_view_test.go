package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type aliasFleet struct {
	fakeOrchAutonomous
	web, other string
}

func (f aliasFleet) ListPRDs() []any {
	return []any{
		&autonomous.PRD{ID: "p-web", Status: autonomous.PRDRunning, ProjectDir: filepath.Join(f.web, "sub")},
		&autonomous.PRD{ID: "p-prof", Status: autonomous.PRDRunning, ProjectProfile: "other"},
		&autonomous.PRD{ID: "p-none", Status: autonomous.PRDRunning, ProjectDir: "/nowhere/else"},
	}
}

func (f aliasFleet) ListAutomations() []any {
	return []any{
		&autonomous.Automation{ID: "a-web", Status: autonomous.AutomationActive, ProjectDir: f.web},
		&autonomous.Automation{ID: "a-none", Status: autonomous.AutomationActive},
	}
}

func (f aliasFleet) GetAutomation(id string) (any, bool) {
	for _, x := range f.ListAutomations() {
		if a := x.(*autonomous.Automation); a.ID == id {
			return a, true
		}
	}
	return nil, false
}

func newAliasServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	web, other := filepath.Join(root, "web"), filepath.Join(root, "other")
	for _, d := range []string{filepath.Join(web, "sub"), other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Projects = map[string]config.ProjectConfigEntry{"web": {Dir: web}, "other": {Dir: other}}
	return &Server{autonomousMgr: &aliasFleet{web: web, other: other}, cfg: cfg}
}

func aliasGET(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if len(path) > 15 && path[:15] == "/api/automation" && path[:16] != "/api/autonomous/" {
		s.handleAutomations(rec, req)
	} else {
		s.handleAutonomousPRDs(rec, req)
	}
	return rec
}

func TestProjectAliasPRDListField(t *testing.T) {
	s := newAliasServer(t)
	rec := aliasGET(t, s, "/api/autonomous/prds")
	var body struct {
		PRDs []struct{ ID, Project string } `json:"prds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range body.PRDs {
		got[p.ID] = p.Project
	}
	want := map[string]string{"p-web": "web", "p-prof": "other", "p-none": ""}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("project of %s = %q, want %q (%v)", id, got[id], w, got)
		}
	}
}

func TestProjectAliasPRDListFilter(t *testing.T) {
	s := newAliasServer(t)
	for alias, wantID := range map[string]string{"web": "p-web", "other": "p-prof"} {
		rec := aliasGET(t, s, "/api/autonomous/prds?project="+alias)
		var body struct {
			PRDs []struct{ ID string } `json:"prds"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.PRDs) != 1 || body.PRDs[0].ID != wantID {
			t.Fatalf("project=%s -> %+v, want only %s", alias, body.PRDs, wantID)
		}
	}
	rec := aliasGET(t, s, "/api/autonomous/prds?project=ghost")
	var empty struct {
		PRDs []struct{ ID string } `json:"prds"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &empty)
	if len(empty.PRDs) != 0 {
		t.Fatalf("unknown alias matched %+v", empty.PRDs)
	}
}

func TestProjectAliasAutomationListGetAndFilter(t *testing.T) {
	s := newAliasServer(t)
	rec := aliasGET(t, s, "/api/automations")
	var all []struct{ ID, Project string }
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Project != "web" || all[1].Project != "" {
		t.Fatalf("automation list = %+v", all)
	}
	rec = aliasGET(t, s, "/api/automations?project=web")
	var filtered []struct{ ID, Project string }
	_ = json.Unmarshal(rec.Body.Bytes(), &filtered)
	if len(filtered) != 1 || filtered[0].ID != "a-web" || filtered[0].Project != "web" {
		t.Fatalf("filtered automations = %+v", filtered)
	}
	rec = aliasGET(t, s, "/api/automations/a-web")
	var one struct{ ID, Project string }
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	if one.Project != "web" {
		t.Fatalf("get project = %q: %s", one.Project, rec.Body.String())
	}
}

func TestProjectAliasReadLeavesStoredFilesUnchanged(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "prd.json")
	orig := []byte(`{"id":"x","project_dir":"/tmp/x","status":"running"}`)
	if err := os.WriteFile(rec, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	// Deriving a view of a decoded record must not touch the stored bytes
	// and the derived field must not appear on the record struct's JSON.
	var p autonomous.PRD
	if err := json.Unmarshal(orig, &p); err != nil {
		t.Fatal(err)
	}
	_ = withProjectField(&p, autonomous.ProjectAliasOfPRD(&p, map[string]string{"x": "/tmp/x"}))
	b, _ := json.Marshal(&p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, has := m["project"]; has {
		t.Fatal("stored PRD JSON gained a project field")
	}
	after, err := os.ReadFile(rec)
	if err != nil || string(after) != string(orig) {
		t.Fatalf("stored file changed: %q err=%v", after, err)
	}
}
