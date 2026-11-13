package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

var taskRoutingToolCaps = map[string]string{
	"task_routing_get":              federation.CapAutonomousRead,
	"task_routing_profile_list":     federation.CapAutonomousRead,
	"task_routing_profile_export":   federation.CapAutonomousRead,
	"task_routing_automaton":        federation.CapAutonomousRead,
	"task_routing_dry_run":          federation.CapAutonomousRead,
	"task_routing_set":              federation.CapConfigWrite,
	"task_routing_profile_save":     federation.CapConfigWrite,
	"task_routing_profile_activate": federation.CapConfigWrite,
	"task_routing_profile_delete":   federation.CapConfigWrite,
	"task_routing_profile_import":   federation.CapConfigWrite,
}

func TestTaskRoutingToolsRegisteredWithCaps(t *testing.T) {
	s := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, tl := range tools {
		listed[tl.Name] = tl.Description
	}
	for name, cap := range taskRoutingToolCaps {
		desc, ok := listed[name]
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		if len(desc) < 30 {
			t.Errorf("%s needs a clear description, got %q", name, desc)
		}
		if strings.Contains(strings.ToLower(desc), "prd") {
			t.Errorf("%s description must say Automaton, not PRD: %q", name, desc)
		}
		if got := federation.MCPToolCap[name]; got != cap {
			t.Errorf("MCPToolCap[%s] = %q, want %q", name, got, cap)
		}
		write := cap == federation.CapConfigWrite
		if federation.IsConfigWriteTool(name) != write {
			t.Errorf("%s config-write class = %v, want %v", name, !write, write)
		}
		if !write && !federation.IsNonConfigWriteTool(name) {
			t.Errorf("%s must be in the non-config class", name)
		}
	}
}

type trCall struct {
	method, path, query string
	body                string
}

func trServer(t *testing.T, reply string) (*Server, *[]trCall) {
	t.Helper()
	var calls []trCall
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, trCall{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		_, _ = w.Write([]byte(reply))
	})
	return s, &calls
}

func TestTaskRoutingReadToolsProxy(t *testing.T) {
	s, calls := trServer(t, `{"ok":true}`)
	ctx := context.Background()
	steps := []struct {
		run          func() (string, error)
		method, path string
	}{
		{func() (string, error) { return textOf(s.handleTaskRoutingGet(ctx, callReq(nil))) }, "GET", "/api/autonomous/task-routing"},
		{func() (string, error) { return textOf(s.handleTaskRoutingProfileList(ctx, callReq(nil))) }, "GET", "/api/autonomous/task-routing/profiles"},
		{func() (string, error) {
			return textOf(s.handleTaskRoutingProfileExport(ctx, callReq(map[string]any{"name": "my prof"})))
		}, "GET", "/api/autonomous/task-routing/profiles/my prof/export"},
		{func() (string, error) {
			return textOf(s.handleTaskRoutingAutomaton(ctx, callReq(map[string]any{"id": "abc"})))
		}, "GET", "/api/autonomous/prds/abc/task-routing"},
		{func() (string, error) {
			return textOf(s.handleTaskRoutingDryRun(ctx, callReq(map[string]any{"id": "abc", "profile": "alt"})))
		}, "POST", "/api/autonomous/prds/abc/task-routing/dry-run"},
	}
	for i, st := range steps {
		out, err := st.run()
		if err != nil || !strings.Contains(out, "ok") {
			t.Fatalf("step %d: %q %v", i, out, err)
		}
		c := (*calls)[i]
		if c.method != st.method || c.path != st.path {
			t.Errorf("step %d proxied %s %s, want %s %s", i, c.method, c.path, st.method, st.path)
		}
	}
	if !strings.Contains((*calls)[4].body, `"profile":"alt"`) {
		t.Errorf("dry run body: %s", (*calls)[4].body)
	}
	if _, err := s.handleTaskRoutingAutomaton(ctx, callReq(nil)); err == nil {
		t.Error("missing id must be an error")
	}
	if _, err := s.handleTaskRoutingProfileExport(ctx, callReq(nil)); err == nil {
		t.Error("missing name must be an error")
	}
}

// textOf returns a handler result's text, or its error.
func textOf(res *mcpsdk.CallToolResult, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if res == nil || len(res.Content) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		return "", io.ErrUnexpectedEOF
	}
	return tc.Text, nil
}

func TestTaskRoutingSetMergesOntoCurrent(t *testing.T) {
	var put map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &put)
		}
		_, _ = w.Write([]byte(`{"enabled":false,"active_profile":"default","profiles":{"default":{}},"task_types":["docs_set"],"validation":{"valid":true}}`))
	})
	profiles := `{"default":{"tests":{"worker_backend":"claude-code"}}}`
	res, err := s.handleTaskRoutingSet(context.Background(), callReq(map[string]any{"enabled": true, "profiles": profiles}))
	resultText(t, res, err)
	if put["enabled"] != true || put["active_profile"] != "default" {
		t.Errorf("merged body wrong: %v", put)
	}
	if _, ok := put["validation"]; ok {
		t.Error("read-only validation must not be sent back")
	}
	if _, ok := put["task_types"]; ok {
		t.Error("read-only task_types must not be sent back")
	}
	if p, _ := put["profiles"].(map[string]any); p["default"] == nil {
		t.Errorf("profiles not sent: %v", put["profiles"])
	}
	for name, args := range map[string]map[string]any{
		"enabled not bool": {"enabled": "yes"},
		"bad profiles":     {"profiles": "{"},
	} {
		if _, err := s.handleTaskRoutingSet(context.Background(), callReq(args)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestTaskRoutingSetSurfacesRESTRefusal(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			http.Error(w, `{"error":"the routing table is not valid","problems":["names LLM backend \"ghost\""]}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"enabled":false,"profiles":{}}`))
	})
	_, err := s.handleTaskRoutingSet(context.Background(), callReq(map[string]any{"enabled": true}))
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %v, want the REST problem list", err)
	}
}

func TestTaskRoutingProfileWriteToolsProxy(t *testing.T) {
	s, calls := trServer(t, `{"ok":true}`)
	ctx := context.Background()
	doc := `{"format_version":1,"name":"x","rows":{}}`
	mustOK := func(res string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustOK(textOf(s.handleTaskRoutingProfileSave(ctx, callReq(map[string]any{"name": "alt", "rows": `{"tests":{}}`}))))
	mustOK(textOf(s.handleTaskRoutingProfileActivate(ctx, callReq(map[string]any{"name": "alt"}))))
	mustOK(textOf(s.handleTaskRoutingProfileDelete(ctx, callReq(map[string]any{"name": "alt"}))))
	mustOK(textOf(s.handleTaskRoutingProfileImport(ctx, callReq(map[string]any{"document": doc, "name": "n2", "overwrite": true}))))
	want := []struct{ method, path, query, bodyHas string }{
		{"POST", "/api/autonomous/task-routing/profiles", "", `"name":"alt"`},
		{"PUT", "/api/autonomous/task-routing/active-profile", "", `"name":"alt"`},
		{"DELETE", "/api/autonomous/task-routing/profiles/alt", "", ""},
		{"POST", "/api/autonomous/task-routing/profiles/import", "name=n2&overwrite=true", `"format_version":1`},
	}
	if len(*calls) != len(want) {
		t.Fatalf("calls: %+v", *calls)
	}
	for i, w := range want {
		c := (*calls)[i]
		if c.method != w.method || c.path != w.path || c.query != w.query || !strings.Contains(c.body, w.bodyHas) {
			t.Errorf("call %d = %+v, want %+v", i, c, w)
		}
	}
	if !strings.Contains((*calls)[0].body, `"rows":{"tests":{}}`) {
		t.Errorf("save rows not forwarded: %s", (*calls)[0].body)
	}
	if _, err := s.handleTaskRoutingProfileImport(ctx, callReq(map[string]any{"document": "nope"})); err == nil {
		t.Error("a non-JSON document must be refused")
	}
	if _, err := s.handleTaskRoutingProfileSave(ctx, callReq(map[string]any{"name": "a", "rows": "{"})); err == nil {
		t.Error("bad rows must be refused")
	}
}
