package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
	dwserver "github.com/dmz006/datawatch/internal/server"
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

// config_set against the real /api/config handler: each of the three task
// routing keys is written and read back through get_config's source (GET).

func taskRoutingConfigMCP(t *testing.T) (*Server, func() config.TaskRoutingConfig) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Autonomous.TaskRouting = config.DefaultTaskRouting()
	cfg.Autonomous.TaskRouting.Profiles["alt"] = cfg.Autonomous.TaskRouting.Profiles["default"]
	api := dwserver.NewServer(dwserver.NewHub(), nil, "h", "", nil, cfg, filepath.Join(t.TempDir(), "config.yaml"))
	reg := inference.NewRegistry()
	for _, n := range []string{"pi-prompt", "claude-code"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: inference.KindOllama}); err != nil {
			t.Fatalf("registry add %s: %v", n, err)
		}
	}
	api.SetInference(reg, nil)
	ts := httptest.NewServer(api.ConfigHandler())
	t.Cleanup(ts.Close)
	s := &Server{webPort: bl427Port(t, ts), cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	read := func() config.TaskRoutingConfig {
		resp, err := http.Get(ts.URL + "/api/config")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		var top struct {
			Autonomous struct {
				TaskRouting config.TaskRoutingConfig `json:"task_routing"`
			} `json:"autonomous"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&top); err != nil {
			t.Fatal(err)
		}
		return top.Autonomous.TaskRouting
	}
	return s, read
}

func taskRoutingConfigSetMCP(t *testing.T, s *Server, key, value string) (string, bool) {
	t.Helper()
	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{"key": key, "value": value}))
	if err != nil {
		t.Fatalf("handleConfigSet: %v", err)
	}
	return automationText(t, res), res.IsError
}

func TestTaskRoutingConfigSetMCPEnabledAndActiveProfile(t *testing.T) {
	s, read := taskRoutingConfigMCP(t)
	if text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing.enabled", "true"); isErr {
		t.Fatalf("enabled: %s", text)
	}
	if !read().Enabled {
		t.Fatal("enabled did not read back true")
	}
	if text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing.active_profile", "alt"); isErr || !strings.Contains(text, "Set ") {
		t.Fatalf("active_profile: %s", text)
	}
	if got := read().ActiveProfile; got != "alt" {
		t.Fatalf("active_profile = %q, want alt", got)
	}
	// A profile that does not exist is skipped and reported, not stored.
	text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing.active_profile", "ghost")
	if !isErr || !strings.Contains(text, "not stored") || strings.Contains(text, "Set ") {
		t.Fatalf("unknown profile should be reported as not stored, got %q (isError=%v)", text, isErr)
	}
	if got := read().ActiveProfile; got != "alt" {
		t.Fatalf("active_profile changed to %q after a skipped write", got)
	}
}

func TestTaskRoutingConfigSetMCPBlock(t *testing.T) {
	s, read := taskRoutingConfigMCP(t)
	good := config.DefaultTaskRouting()
	good.Enabled = true
	good.Profiles["lean"] = good.Profiles["default"]
	good.ActiveProfile = "lean"
	b, _ := json.Marshal(good)
	if text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing", string(b)); isErr {
		t.Fatalf("block: %s", text)
	}
	if got := read(); !got.Enabled || got.ActiveProfile != "lean" || len(got.Profiles["lean"]) == 0 {
		t.Fatalf("block did not read back: %+v", got)
	}
	// A block that fails ValidateTaskRouting is skipped and reported.
	bad := `{"enabled":true,"active_profile":"x","profiles":{"x":{"not-a-task-type":{"worker_backend":"pi-prompt"}}}}`
	text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing", bad)
	if !isErr || !strings.Contains(text, "not stored") {
		t.Fatalf("invalid block should be reported as not stored, got %q (isError=%v)", text, isErr)
	}
	if got := read(); got.ActiveProfile != "lean" || len(got.Profiles["x"]) != 0 {
		t.Fatalf("a refused block was stored: %+v", got.ActiveProfile)
	}
}

// Unknown and premium rows refused through config_set (the real /api/config
// handler with the LLM registry wired): the message names the row and the
// stored block does not change.

const (
	taskRoutingBadUnknownBlock = `{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"ghost-llm"}}}}`
	taskRoutingBadModelBlock   = `{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"claude-code","worker_model":"ollama/qwen3.8:27b"}}}}`
	taskRoutingPremiumBlock    = `{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"claude-code","worker_model":"claude-opus-5-5"}}}}`
	taskRoutingPremiumOKBlock  = `{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"claude-code","worker_model":"claude-opus-5-5","allow_premium_model":true}}}}`
)

func TestTaskRoutingConfigSetMCPUnknownBackendAndModelNamesRow(t *testing.T) {
	for name, c := range map[string]struct{ block, want string }{
		"backend": {taskRoutingBadUnknownBlock, "ghost-llm"},
		"model":   {taskRoutingBadModelBlock, "ollama/qwen3.8:27b"},
	} {
		t.Run(name, func(t *testing.T) {
			s, read := taskRoutingConfigMCP(t)
			before := read()
			text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing", c.block)
			if !isErr || !strings.Contains(text, "not stored") {
				t.Fatalf("bad row should be refused, got %q (isError=%v)", text, isErr)
			}
			for _, w := range []string{`profile "x"`, `row "final_gate"`, "worker", c.want} {
				if !strings.Contains(text, w) {
					t.Errorf("message does not name %s: %s", w, text)
				}
			}
			if got := read(); got.ActiveProfile != before.ActiveProfile || len(got.Profiles["x"]) != 0 {
				t.Fatalf("a refused block was stored: %+v", got.ActiveProfile)
			}
		})
	}
}

func TestTaskRoutingConfigSetMCPPremiumModelNeedsFlag(t *testing.T) {
	s, read := taskRoutingConfigMCP(t)
	text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing", taskRoutingPremiumBlock)
	if !isErr || !strings.Contains(text, "allow_premium_model") || !strings.Contains(text, `row "final_gate"`) || !strings.Contains(text, `profile "x"`) {
		t.Fatalf("premium row without the flag should be refused naming the row, got %q (isError=%v)", text, isErr)
	}
	if got := read(); got.ActiveProfile == "x" || len(got.Profiles["x"]) != 0 {
		t.Fatalf("a refused premium block was stored: %+v", got.ActiveProfile)
	}
	if text, isErr := taskRoutingConfigSetMCP(t, s, "autonomous.task_routing", taskRoutingPremiumOKBlock); isErr {
		t.Fatalf("premium row with the flag refused: %s", text)
	}
	if got := read(); got.ActiveProfile != "x" || !got.Profiles["x"]["final_gate"].AllowPremiumModel {
		t.Fatalf("flagged premium block not stored: %+v", got.Profiles["x"])
	}
}
