package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestRepoCIWatchMCPToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tl := range tools {
		have[tl.Name] = true
	}
	if !have["autonomous_repo_ci_watch"] {
		t.Error("tool \"autonomous_repo_ci_watch\" is not registered")
	}
}

func TestRepoCIWatchMCPSchema(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousRepoCIWatch()
	if tool.Name != "autonomous_repo_ci_watch" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("autonomous_repo_ci_watch must be marked read-only")
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("all parameters are optional, got required %v", tool.InputSchema.Required)
	}
	for _, name := range []string{"repo", "status"} {
		if _, ok := tool.InputSchema.Properties[name]; !ok {
			t.Errorf("lacks the %s parameter", name)
		}
	}
	desc := tool.Description
	for _, word := range []string{"alert_minutes", "superseded", "last_poll_at", "repos"} {
		if !strings.Contains(desc, word) {
			t.Errorf("description does not mention %q: %s", word, desc)
		}
	}
}

func TestRepoCIWatchMCPProxiesGETAndPassesFiltersThrough(t *testing.T) {
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(`{"mode":"off","alert_minutes":0,"repos":[],"records":[]}`))
	})
	res, err := s.handleAutonomousRepoCIWatch(context.Background(), callReq(map[string]any{
		"repo": "dmz006/datawatch", "status": "failing",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/repo-ci-watch" {
		t.Errorf("proxied %s %s", method, path)
	}
	for key, want := range map[string]string{"repo": "dmz006/datawatch", "status": "failing"} {
		if got := query[key]; len(got) != 1 || got[0] != want {
			t.Errorf("query %s = %v, want [%s]", key, got, want)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, `"alert_minutes":0`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestRepoCIWatchMCPSendsNoQueryWhenAbsent(t *testing.T) {
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousRepoCIWatch(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if len(query) != 0 {
		t.Errorf("query sent without any argument: %v", query)
	}
}

func TestRepoCIWatchMCPSurfacesValidationError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `status must be one of green, failing, pending, hung, rerunning, superseded`, http.StatusBadRequest)
	})
	res, err := s.handleAutonomousRepoCIWatch(context.Background(), callReq(map[string]any{"status": "bogus"}))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, `status must be one of`) {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestRepoCIWatchMCPCapabilityMapping(t *testing.T) {
	if got := federation.MCPToolCap["autonomous_repo_ci_watch"]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_repo_ci_watch = %q, want %q", got, federation.CapAutonomousRead)
	}
}
