// Configuration round-trip for server.allowed_project_roots on every
// surface the project keeps in parity: YAML, REST (GET and PUT, with a list or
// the comma string CLI, chat and MCP send), the Settings card and its five
// locale bundles, and the documentation.

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

	"github.com/dmz006/datawatch/internal/config"
)

func newAllowedRootsServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

func getAllowedRoots(t *testing.T, s *Server) []string {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var got struct {
		Server map[string]json.RawMessage `json:"server"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	raw, ok := got.Server["allowed_project_roots"]
	if !ok {
		t.Fatalf("GET /api/config server map lacks allowed_project_roots: %s", rr.Body.String())
	}
	var roots []string
	if err := json.Unmarshal(raw, &roots); err != nil {
		t.Fatalf("decode allowed_project_roots %s: %v", raw, err)
	}
	return roots
}

func TestAllowedProjectRoots_RESTRoundTrip(t *testing.T) {
	s, cfgPath := newAllowedRootsServer(t)
	if got := getAllowedRoots(t, s); len(got) != 0 {
		t.Fatalf("default must be empty, got %v", got)
	}

	// A JSON list (what the Settings card sends).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"server.allowed_project_roots": []string{"/srv/a", "/srv/b"}})
	if got := getAllowedRoots(t, s); strings.Join(got, ",") != "/srv/a,/srv/b" {
		t.Fatalf("after list PUT: %v", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "allowed_project_roots") || !strings.Contains(string(raw), "/srv/b") {
		t.Fatalf("config.yaml lacks the roots:\n%s", raw)
	}

	// A comma string (what datawatch config set, chat configure and MCP send).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"server.allowed_project_roots": "/srv/c, /srv/d"})
	if got := getAllowedRoots(t, s); strings.Join(got, ",") != "/srv/c,/srv/d" {
		t.Fatalf("after string PUT: %v", got)
	}

	// The configured roots are what the daemon enforces, next to the two that
	// are always allowed.
	roots := s.cfg.AllowedProjectRoots()
	if len(roots) < 3 || roots[len(roots)-2] != "/srv/c" || roots[len(roots)-1] != "/srv/d" {
		t.Fatalf("effective roots = %v", roots)
	}
	if roots[0] != config.DefaultPRDWorktreeDir() {
		t.Fatalf("the PRD worktree base must always be allowed: %v", roots)
	}

	// An empty value clears the extra roots; the two built-in ones stay.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"server.allowed_project_roots": []string{}})
	if got := getAllowedRoots(t, s); len(got) != 0 {
		t.Fatalf("after clear: %v", got)
	}
	if len(s.cfg.AllowedProjectRoots()) == 0 {
		t.Fatal("clearing the extras must not remove the always-allowed roots")
	}
}

func TestAllowedProjectRoots_PutRefusesBadValues(t *testing.T) {
	for name, val := range map[string]any{
		"relative path":   []string{"relative/dir"},
		"filesystem root": "/",
		"wrong type":      42,
		"NUL byte":        []string{"/srv/a\x00b"},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newAllowedRootsServer(t)
			b, _ := json.Marshal(map[string]any{"server.allowed_project_roots": val})
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			if len(s.cfg.Server.AllowedProjectRoots) != 0 {
				t.Fatalf("a refused value must not be applied: %v", s.cfg.Server.AllowedProjectRoots)
			}
		})
	}
}

func TestAllowedProjectRoots_AlwaysIncludesWorktreeBaseAndWorkspaceRoot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Autonomous.WorktreeDir = "/data/worktrees"
	cfg.Session.WorkspaceRoot = "/workspace"
	cfg.Server.AllowedProjectRoots = []string{"/srv/extra"}
	got := strings.Join(cfg.AllowedProjectRoots(), ",")
	if got != "/data/worktrees,/workspace,/srv/extra" {
		t.Fatalf("roots = %s", got)
	}
	cfg.Autonomous.WorktreeDir = ""
	cfg.Session.WorkspaceRoot = ""
	cfg.Server.AllowedProjectRoots = nil
	want := config.DefaultPRDWorktreeDir()
	if ws := config.DefaultWorkspaceRoot(); ws != "" {
		want += "," + ws
	}
	if got := strings.Join(cfg.AllowedProjectRoots(), ","); got != want {
		t.Fatalf("with nothing configured the worktree base and default workspace are allowed, got %s want %s", got, want)
	}
}

func TestAllowedProjectRoots_YAMLTemplate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.AllowedProjectRoots = []string{"/srv/a", "/srv/b"}
	out := config.GenerateAnnotatedConfig(cfg)
	if !strings.Contains(out, "allowed_project_roots: [/srv/a, /srv/b]") {
		t.Fatalf("config template lacks the key:\n%s", out)
	}
}

func TestAllowedProjectRoots_SettingsCardAndLocales(t *testing.T) {
	app, err := os.ReadFile(filepath.Join("web", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(app), "key: 'server.allowed_project_roots'") ||
		!strings.Contains(string(app), "labelKey: 'settings_allowed_project_roots'") ||
		!strings.Contains(string(app), "helpKey: 'settings_allowed_project_roots_help'") {
		t.Fatal("Settings card lacks server.allowed_project_roots with label and help keys")
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		raw, err := os.ReadFile(filepath.Join("web", "locales", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var bundle map[string]string
		if err := json.Unmarshal(raw, &bundle); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, k := range []string{"settings_allowed_project_roots", "settings_allowed_project_roots_help"} {
			if strings.TrimSpace(bundle[k]) == "" {
				t.Errorf("%s locale lacks %s", lang, k)
			}
		}
	}
}

func TestAllowedProjectRoots_Documented(t *testing.T) {
	for _, p := range []string{"../../docs/operations.md", "../../docs/config-reference.yaml"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "allowed_project_roots") {
			t.Errorf("%s does not document allowed_project_roots", p)
		}
	}
}
