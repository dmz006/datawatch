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

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type rulesContextSpy struct {
	fakeOrchAutonomous
	prds map[string]*autonomous.PRD
}

func (s *rulesContextSpy) GetPRD(id string) (any, bool) {
	p, ok := s.prds[id]
	return p, ok
}

func newRulesContextServer(t *testing.T) *Server {
	t.Helper()
	t.Cleanup(func() { autonomous.SetRulesContextConfig(config.AutonomousRulesContextConfig{}) })
	autonomous.SetRulesContextConfig(config.AutonomousRulesContextConfig{})
	return &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func rulesContextDo(t *testing.T, s *Server, method, url string, body any) (int, rulesContextView, string) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousRulesContext(rr, httptest.NewRequest(method, url, rd))
	var v rulesContextView
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode: %v (%s)", err, rr.Body.String())
		}
	}
	return rr.Code, v, rr.Body.String()
}

func TestRulesContextSettingsRESTGetDefault(t *testing.T) {
	s := newRulesContextServer(t)
	code, v, body := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context", nil)
	if code != 200 {
		t.Fatalf("GET = %d %s", code, body)
	}
	if v.Policy.RulesFile != "AGENT.md" || len(v.Policy.ContextFiles) != 1 || v.Policy.ContextFiles[0] != "CONTEXT.md" ||
		v.Policy.DigestMode != "no_tools" || v.Policy.DigestMaxBytes != 4096 || v.Record != nil {
		t.Fatalf("default policy = %+v record=%v", v.Policy, v.Record)
	}
	if code, _, _ := rulesContextDo(t, s, http.MethodPost, "/api/autonomous/rules-context", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", code)
	}
}

func TestRulesContextSettingsRESTGetResolvedProjectRenamedContext(t *testing.T) {
	s := newRulesContextServer(t)
	dir := t.TempDir()
	cfg := map[string]any{"projects": []map[string]any{{"project_dir": dir, "context_files": []string{"STATE.md"}, "rules_file": "RULES.md"}}}
	if code, _, body := rulesContextDo(t, s, http.MethodPut, "/api/autonomous/rules-context", cfg); code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	_, v, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?project_dir="+dir, nil)
	if v.Policy.RulesFile != "RULES.md" || len(v.Policy.ContextFiles) != 1 || v.Policy.ContextFiles[0] != "STATE.md" {
		t.Fatalf("project policy = %+v", v.Policy)
	}
	_, other, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?project_dir=/elsewhere", nil)
	if other.Policy.ContextFiles[0] != "CONTEXT.md" {
		t.Fatalf("other project must follow global: %+v", other.Policy)
	}
}

func TestRulesContextSettingsRESTGetPRDRecord(t *testing.T) {
	s := newRulesContextServer(t)
	s.autonomousMgr = &rulesContextSpy{prds: map[string]*autonomous.PRD{
		"p1": {RulesContext: &autonomous.RulesContextRecord{
			Files: []autonomous.RulesContextFileRef{
				{Name: "AGENT.md", Kind: "rules", Present: true, SHA256: "abc", Bytes: 12},
				{Name: "STATE.md", Kind: "context"},
			},
			Missing: []string{"STATE.md"}, Notes: []string{"STATE.md is missing"}, DigestMode: "no_tools",
		}},
		"p2": {},
	}}
	code, v, body := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?prd=p1", nil)
	if code != 200 || v.Record == nil {
		t.Fatalf("GET = %d %s", code, body)
	}
	r := v.Record
	if !r.Recorded || r.FileCount != 2 || r.PresentCount != 1 || r.MissingCount != 1 || len(r.Notes) != 1 ||
		!strings.Contains(r.Notes[0], "missing") || r.Files[0].SHA256 != "abc" || r.Files[0].Bytes != 12 {
		t.Fatalf("record = %+v", r)
	}
	_, v2, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?prd=p2", nil)
	if v2.Record == nil || v2.Record.Recorded || v2.Record.FileCount != 0 {
		t.Fatalf("unrecorded = %+v", v2.Record)
	}
	if code, _, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?prd=nope", nil); code != http.StatusNotFound {
		t.Fatalf("unknown prd = %d, want 404", code)
	}
}

func TestRulesContextSettingsRESTPutValidPersists(t *testing.T) {
	s := newRulesContextServer(t)
	body := map[string]any{"rules_file": "RULES.md", "context_files": []string{"A.md", "docs/B.md"}, "digest_mode": "ALL", "digest_max_bytes": 2048}
	code, v, raw := rulesContextDo(t, s, http.MethodPut, "/api/autonomous/rules-context", body)
	if code != 200 {
		t.Fatalf("PUT = %d %s", code, raw)
	}
	if v.Config.DigestMode != "all" || v.Policy.RulesFile != "RULES.md" || v.Policy.DigestMaxBytes != 2048 {
		t.Fatalf("PUT result = %+v", v)
	}
	_, got, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context", nil)
	if got.Config.RulesFile != "RULES.md" || len(got.Config.ContextFiles) != 2 || got.Policy.DigestMode != "all" {
		t.Fatalf("read back = %+v", got)
	}
	if p := autonomous.RulesContextFor(""); p.RulesFile != "RULES.md" || p.DigestMaxBytes != 2048 {
		t.Fatalf("in-memory policy = %+v", p)
	}
	disk, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Autonomous.RulesContext.RulesFile != "RULES.md" || disk.Autonomous.RulesContext.DigestMode != "all" {
		t.Fatalf("file config = %+v", disk.Autonomous.RulesContext)
	}
}

func TestRulesContextSettingsRESTPutInvalid(t *testing.T) {
	s := newRulesContextServer(t)
	for name, body := range map[string]any{
		"absolute path":    map[string]any{"rules_file": "/etc/passwd"},
		"absolute context": map[string]any{"context_files": []string{"/x.md"}},
		"escape":           map[string]any{"rules_file": "../x.md"},
		"bad digest mode":  map[string]any{"digest_mode": "sometimes"},
		"cap too small":    map[string]any{"digest_max_bytes": 10},
		"cap too large":    map[string]any{"digest_max_bytes": 1 << 20},
		"project no dir":   map[string]any{"projects": []map[string]any{{"rules_file": "A.md"}}},
		"project bad mode": map[string]any{"projects": []map[string]any{{"project_dir": "/p", "digest_mode": "x"}}},
		"unknown field":    map[string]any{"bogus": 1},
	} {
		code, _, msg := rulesContextDo(t, s, http.MethodPut, "/api/autonomous/rules-context", body)
		if code != http.StatusBadRequest || strings.TrimSpace(msg) == "" {
			t.Errorf("%s: code=%d msg=%q, want 400 with a message", name, code, msg)
		}
	}
	if _, err := os.Stat(s.cfgPath); err == nil {
		t.Fatal("a refused PUT must not write the config file")
	}
	if p := autonomous.RulesContextFor(""); p.RulesFile != "AGENT.md" {
		t.Fatalf("a refused PUT changed the policy: %+v", p)
	}
}

func TestRulesContextSettingsRESTProjectOverrideRoundTrip(t *testing.T) {
	s := newRulesContextServer(t)
	body := map[string]any{
		"digest_mode": "off",
		"projects":    []map[string]any{{"project_dir": "/work/a", "rules_file": "A-RULES.md", "context_files": []string{"A-STATE.md"}, "digest_mode": "all", "digest_max_bytes": 1024}},
	}
	if code, _, raw := rulesContextDo(t, s, http.MethodPut, "/api/autonomous/rules-context", body); code != 200 {
		t.Fatalf("PUT = %d %s", code, raw)
	}
	_, v, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?project_dir=/work/a", nil)
	if len(v.Config.Projects) != 1 || v.Config.Projects[0].ProjectDir != "/work/a" {
		t.Fatalf("projects = %+v", v.Config.Projects)
	}
	if v.Policy.RulesFile != "A-RULES.md" || v.Policy.DigestMode != "all" || v.Policy.DigestMaxBytes != 1024 {
		t.Fatalf("override policy = %+v", v.Policy)
	}
	_, g, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context", nil)
	if g.Policy.DigestMode != "off" || g.Policy.RulesFile != "AGENT.md" {
		t.Fatalf("global policy = %+v", g.Policy)
	}
	// Clearing the projects list removes the override.
	if code, _, _ := rulesContextDo(t, s, http.MethodPut, "/api/autonomous/rules-context", map[string]any{"digest_mode": "off"}); code != 200 {
		t.Fatal("clear PUT failed")
	}
	_, c, _ := rulesContextDo(t, s, http.MethodGet, "/api/autonomous/rules-context?project_dir=/work/a", nil)
	if c.Policy.RulesFile != "AGENT.md" || len(c.Config.Projects) != 0 {
		t.Fatalf("after clear = %+v", c)
	}
}
