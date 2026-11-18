package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	dwserver "github.com/dmz006/datawatch/internal/server"
)

const taskRoutingCLIView = `{"enabled":true,"active_profile":"default","profiles":{"default":{` +
	`"docs_set":{"worker_backend":"pi-prompt","worker_model":"ollama/qwen3.8:27b","verification_backend":"claude-code","verification_model":"sonnet","effort":"low","second_choice":{"backend":"claude-code","model":"sonnet"}},` +
	`"tests":{"worker_backend":"claude-code","worker_model":"sonnet"}}},"validation":{"valid":true}}`

type trCall struct{ method, path, query, body string }

// taskRoutingFake answers by "METHOD path" and records every call.
type taskRoutingFake struct {
	mu    sync.Mutex
	calls []trCall
}

func newTaskRoutingFake(t *testing.T, replies map[string]string) *taskRoutingFake {
	t.Helper()
	f := &taskRoutingFake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, trCall{r.Method, r.URL.Path, r.URL.RawQuery, string(raw)})
		f.mu.Unlock()
		rep, ok := replies[r.Method+" "+r.URL.Path]
		if !ok {
			http.Error(w, "no fake reply", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(rep))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return f
}

func (f *taskRoutingFake) last() trCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func TestTaskRoutingCLIRegisteredUnderAutonomous(t *testing.T) {
	var group bool
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() != "task-routing" {
			continue
		}
		group = true
		names := map[string]bool{}
		for _, sub := range c.Commands() {
			names[sub.Name()] = true
		}
		for _, want := range []string{"show", "set", "profile", "automaton", "dry-run"} {
			if !names[want] {
				t.Errorf("task-routing lacks %s", want)
			}
		}
	}
	if !group {
		t.Error("task-routing is not registered under autonomous")
	}
}

func TestTaskRoutingCLIShowPrintsTable(t *testing.T) {
	f := newTaskRoutingFake(t, map[string]string{"GET /api/autonomous/task-routing": taskRoutingCLIView})
	out, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"show"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Task routing: enabled=true profile=default (valid)", "TYPE", "docs_set",
		"pi-prompt/ollama/qwen3.8:27b", "claude-code/sonnet", "low"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if f.last().path != "/api/autonomous/task-routing" {
		t.Errorf("path = %s", f.last().path)
	}
	js, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"show", "--json"})
	if err != nil || js != taskRoutingCLIView {
		t.Errorf("--json must print the body untouched, got %q (%v)", js, err)
	}
}

func TestTaskRoutingCLISetOneRowSendsMergedTable(t *testing.T) {
	f := newTaskRoutingFake(t, map[string]string{
		"GET /api/autonomous/task-routing": taskRoutingCLIView,
		"PUT /api/autonomous/task-routing": taskRoutingCLIView,
	})
	_, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"set", "--type", "tests",
		"--worker-backend", "pi-prompt", "--worker-model", "ollama/qwen3.8:27b", "--effort", "medium",
		"--second-choice", "claude-code/sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	c := f.last()
	if c.method != "PUT" {
		t.Fatalf("last call = %s %s", c.method, c.path)
	}
	for _, want := range []string{`"tests":{"worker_backend":"pi-prompt","worker_model":"ollama/qwen3.8:27b"`,
		`"effort":"medium"`, `"second_choice":{"backend":"claude-code","model":"sonnet"}`, `"docs_set"`} {
		if !strings.Contains(c.body, want) {
			t.Errorf("PUT body lacks %q: %s", want, c.body)
		}
	}
	if _, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"set", "--type", "bogus", "--effort", "low"}); err == nil {
		t.Error("an unknown task type must be rejected")
	}
	if _, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"set"}); err == nil {
		t.Error("set with no file and no type must fail")
	}
}

func TestTaskRoutingCLISetFromYAMLFileReplacesActiveProfile(t *testing.T) {
	f := newTaskRoutingFake(t, map[string]string{
		"GET /api/autonomous/task-routing": taskRoutingCLIView,
		"PUT /api/autonomous/task-routing": taskRoutingCLIView,
	})
	p := filepath.Join(t.TempDir(), "rows.yaml")
	if err := os.WriteFile(p, []byte("docs_set:\n  worker_backend: claude-code\n  worker_model: haiku\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"set", "--file", p}); err != nil {
		t.Fatal(err)
	}
	body := f.last().body
	if !strings.Contains(body, `"worker_model":"haiku"`) || strings.Contains(body, `"tests"`) {
		t.Errorf("active profile must be replaced by the file rows: %s", body)
	}
}

func TestTaskRoutingCLIProfileSubcommands(t *testing.T) {
	f := newTaskRoutingFake(t, map[string]string{
		"GET /api/autonomous/task-routing/profiles":          `{"active_profile":"default","profiles":["default","cheap"]}`,
		"POST /api/autonomous/task-routing/profiles":         `{"saved":"cheap","profiles":["cheap","default"]}`,
		"PUT /api/autonomous/task-routing/active-profile":    `{"active_profile":"cheap"}`,
		"DELETE /api/autonomous/task-routing/profiles/cheap": `{"deleted":"cheap","profiles":["default"]}`,
	})
	out, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "list"})
	if err != nil || !strings.Contains(out, "* default") || !strings.Contains(out, "  cheap") {
		t.Errorf("list: %q (%v)", out, err)
	}
	out, err = runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "save", "cheap"})
	if err != nil || !strings.Contains(out, "Saved profile cheap") || f.last().body != `{"name":"cheap"}` {
		t.Errorf("save: %q body=%s (%v)", out, f.last().body, err)
	}
	out, err = runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "activate", "cheap"})
	if err != nil || !strings.Contains(out, "Active profile is now cheap") || f.last().body != `{"name":"cheap"}` {
		t.Errorf("activate: %q (%v)", out, err)
	}
	out, err = runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "delete", "cheap"})
	if err != nil || !strings.Contains(out, "Deleted profile cheap") || f.last().method != "DELETE" {
		t.Errorf("delete: %q (%v)", out, err)
	}
}

func TestTaskRoutingCLIExportWritesFileAndImportReadsItBack(t *testing.T) {
	const doc = `{"name":"cheap","rows":{"tests":{"worker_backend":"pi-prompt"}}}`
	f := newTaskRoutingFake(t, map[string]string{
		"GET /api/autonomous/task-routing/profiles/cheap/export": doc,
		"POST /api/autonomous/task-routing/profiles/import":      `{"profiles":["cheap2","default"]}`,
	})
	file := filepath.Join(t.TempDir(), "cheap.json")
	out, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "export", "cheap", "--out", file})
	if err != nil || !strings.Contains(out, "Exported profile cheap") {
		t.Fatalf("export: %q (%v)", out, err)
	}
	if got, _ := os.ReadFile(file); string(got) != doc {
		t.Fatalf("exported file = %q", got)
	}
	out, err = runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "import", file, "--name", "cheap2", "--overwrite"})
	if err != nil {
		t.Fatal(err)
	}
	c := f.last()
	if c.body != doc || !strings.Contains(c.query, "name=cheap2") || !strings.Contains(c.query, "overwrite=true") {
		t.Errorf("import sent body=%q query=%q", c.body, c.query)
	}
	if !strings.Contains(out, "cheap2") {
		t.Errorf("import output: %q", out)
	}
	// Without --out the document goes to stdout.
	out, _ = runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"profile", "export", "cheap"})
	if out != doc {
		t.Errorf("stdout export = %q", out)
	}
}

func TestTaskRoutingCLIAutomatonAndDryRun(t *testing.T) {
	f := newTaskRoutingFake(t, map[string]string{
		"GET /api/autonomous/prds/a1/task-routing": `{"automaton_id":"a1","tasks":[` +
			`{"task_id":"t1","title":"Write docs","task_type":"docs_set","classified_by":"title_spec","row":"docs_set","applied":true,"effective":{"backend":"pi-prompt","model":"qwen","source":"task"}},` +
			`{"task_id":"t2","title":"Fix bug","task_type":"daemon_backend_code","classified_by":"default","reason":"routing disabled","applied":false,"effective":{"backend":"claude-code","model":"sonnet","source":"global"}}]}`,
		"POST /api/autonomous/prds/a1/task-routing/dry-run": `{"automaton_id":"a1","entries":[` +
			`{"task_id":"t1","title":"Write docs","task_type":"docs_set","classified_by":"title_spec","profile":"cheap","row":"docs_set","would_apply":true,"result":{"worker_backend":"pi-prompt","worker_model":"qwen"}}]}`,
	})
	out, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"automaton", "a1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Task routing for Automaton a1", "Write docs", "docs_set", "pi-prompt/qwen", "routed", "routing disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("automaton output lacks %q:\n%s", want, out)
		}
	}
	out, err = runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"dry-run", "a1", "--profile", "cheap"})
	if err != nil {
		t.Fatal(err)
	}
	if f.last().body != `{"profile":"cheap"}` {
		t.Errorf("dry-run body = %s", f.last().body)
	}
	for _, want := range []string{"Dry run for Automaton a1 (nothing written)", "Write docs", "pi-prompt/qwen"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output lacks %q:\n%s", want, out)
		}
	}
}

func TestTaskRoutingCLIErrorFromDaemonIsReturned(t *testing.T) {
	newTaskRoutingFake(t, map[string]string{})
	if _, err := runLaneFillCmd(t, newAutonomousTaskRoutingCmd(), []string{"show"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}

// `datawatch config set` and the comm `configure` verb against the real
// /api/config handler: each of the three task routing keys is written and
// read back.

func taskRoutingConfigDaemon(t *testing.T) (*config.Config, func() config.TaskRoutingConfig) {
	t.Helper()
	dcfg := config.DefaultConfig()
	dcfg.Autonomous.TaskRouting = config.DefaultTaskRouting()
	dcfg.Autonomous.TaskRouting.Profiles["alt"] = dcfg.Autonomous.TaskRouting.Profiles["default"]
	api := dwserver.NewServer(dwserver.NewHub(), nil, "h", "", nil, dcfg, filepath.Join(t.TempDir(), "config.yaml"))
	reg := inference.NewRegistry()
	for _, n := range []string{"pi-prompt", "claude-code"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: inference.KindOllama}); err != nil {
			t.Fatalf("registry add %s: %v", n, err)
		}
	}
	api.SetInference(reg, nil)
	ts := httptest.NewServer(api.ConfigHandler())
	t.Cleanup(ts.Close)
	old := serverURL
	serverURL = ts.URL
	t.Cleanup(func() { serverURL = old })
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
	return config.DefaultConfig(), read
}

const taskRoutingBadBlock = `{"enabled":true,"active_profile":"x","profiles":{"x":{"not-a-task-type":{"worker_backend":"pi-prompt"}}}}`

func taskRoutingGoodBlock(t *testing.T) string {
	t.Helper()
	good := config.DefaultTaskRouting()
	good.Enabled = true
	good.Profiles["lean"] = good.Profiles["default"]
	good.ActiveProfile = "lean"
	b, _ := json.Marshal(good)
	return string(b)
}

// taskRoutingSurface writes through one surface and reports its error.
type taskRoutingSurface struct {
	name string
	set  func(cfg *config.Config, key, value string) error
}

func taskRoutingSurfaces() []taskRoutingSurface {
	return []taskRoutingSurface{
		{"cli config set", runConfigSet},
		{"comm configure", func(cfg *config.Config, key, value string) error {
			return commConfigureFunc(cfg)(key, value)
		}},
	}
}

func TestTaskRoutingConfigEnabledAndActiveProfileCLIAndComm(t *testing.T) {
	for _, surf := range taskRoutingSurfaces() {
		t.Run(surf.name, func(t *testing.T) {
			cfg, read := taskRoutingConfigDaemon(t)
			if err := surf.set(cfg, "autonomous.task_routing.enabled", "true"); err != nil {
				t.Fatalf("enabled: %v", err)
			}
			if !read().Enabled {
				t.Fatal("enabled did not read back true")
			}
			if err := surf.set(cfg, "autonomous.task_routing.enabled", "false"); err != nil || read().Enabled {
				t.Fatalf("enabled=false: err=%v enabled=%v", err, read().Enabled)
			}
			if err := surf.set(cfg, "autonomous.task_routing.active_profile", "alt"); err != nil {
				t.Fatalf("active_profile: %v", err)
			}
			if got := read().ActiveProfile; got != "alt" {
				t.Fatalf("active_profile = %q, want alt", got)
			}
			// A profile that does not exist is skipped and reported, not stored.
			err := surf.set(cfg, "autonomous.task_routing.active_profile", "ghost")
			if err == nil || !strings.Contains(err.Error(), "not stored") {
				t.Fatalf("unknown profile should be reported as not stored, got %v", err)
			}
			if got := read().ActiveProfile; got != "alt" {
				t.Fatalf("active_profile changed to %q after a skipped write", got)
			}
		})
	}
}

func TestTaskRoutingConfigBlockCLIAndComm(t *testing.T) {
	for _, surf := range taskRoutingSurfaces() {
		t.Run(surf.name, func(t *testing.T) {
			cfg, read := taskRoutingConfigDaemon(t)
			if err := surf.set(cfg, "autonomous.task_routing", taskRoutingGoodBlock(t)); err != nil {
				t.Fatalf("block: %v", err)
			}
			if got := read(); !got.Enabled || got.ActiveProfile != "lean" || len(got.Profiles["lean"]) == 0 {
				t.Fatalf("block did not read back: active=%q enabled=%v", got.ActiveProfile, got.Enabled)
			}
			// A block that fails ValidateTaskRouting is skipped and reported.
			err := surf.set(cfg, "autonomous.task_routing", taskRoutingBadBlock)
			if err == nil || !strings.Contains(err.Error(), "not stored") {
				t.Fatalf("invalid block should be reported as not stored, got %v", err)
			}
			if got := read(); got.ActiveProfile != "lean" || len(got.Profiles["x"]) != 0 {
				t.Fatalf("a refused block was stored: active=%q", got.ActiveProfile)
			}
		})
	}
}

func TestTaskRoutingConfigRequestBodySendsBlockAsObject(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(configureRequestBody("autonomous.task_routing", taskRoutingGoodBlock(t))), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["autonomous.task_routing"].(map[string]any); !ok {
		t.Fatalf("the block must go out as a JSON object, got %T", m["autonomous.task_routing"])
	}
	if err := json.Unmarshal([]byte(configureRequestBody("autonomous.task_routing.active_profile", "123")), &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["autonomous.task_routing.active_profile"].(string); !ok || v != "123" {
		t.Fatalf("a profile name must go out as a string, got %#v", m["autonomous.task_routing.active_profile"])
	}
}

// Unknown and premium rows refused through `datawatch config set` and the
// comm configure verb: the error names the row and nothing is stored.

func TestTaskRoutingConfigUnknownBackendAndModelCLIAndComm(t *testing.T) {
	blocks := map[string]struct{ block, want string }{
		"backend": {`{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"ghost-llm"}}}}`, "ghost-llm"},
		"model":   {`{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"claude-code","worker_model":"ollama/qwen3.8:27b"}}}}`, "ollama/qwen3.8:27b"},
	}
	for _, surf := range taskRoutingSurfaces() {
		for name, c := range blocks {
			t.Run(surf.name+"/"+name, func(t *testing.T) {
				cfg, read := taskRoutingConfigDaemon(t)
				before := read()
				err := surf.set(cfg, "autonomous.task_routing", c.block)
				if err == nil || !strings.Contains(err.Error(), "not stored") {
					t.Fatalf("bad row should be refused, got %v", err)
				}
				for _, w := range []string{`profile "x"`, `row "final_gate"`, "worker", c.want} {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("message does not name %s: %v", w, err)
					}
				}
				if got := read(); got.ActiveProfile != before.ActiveProfile || len(got.Profiles["x"]) != 0 {
					t.Fatalf("a refused block was stored: active=%q", got.ActiveProfile)
				}
			})
		}
	}
}

func TestTaskRoutingConfigPremiumModelNeedsFlagCLIAndComm(t *testing.T) {
	const noFlag = `{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"claude-code","worker_model":"claude-opus-5-5"}}}}`
	const withFlag = `{"enabled":true,"active_profile":"x","profiles":{"x":{"final_gate":{"worker_backend":"claude-code","worker_model":"claude-opus-5-5","allow_premium_model":true}}}}`
	for _, surf := range taskRoutingSurfaces() {
		t.Run(surf.name, func(t *testing.T) {
			cfg, read := taskRoutingConfigDaemon(t)
			err := surf.set(cfg, "autonomous.task_routing", noFlag)
			if err == nil || !strings.Contains(err.Error(), "allow_premium_model") ||
				!strings.Contains(err.Error(), `row "final_gate"`) || !strings.Contains(err.Error(), `profile "x"`) {
				t.Fatalf("premium row without the flag should be refused naming the row, got %v", err)
			}
			if got := read(); got.ActiveProfile == "x" || len(got.Profiles["x"]) != 0 {
				t.Fatalf("a refused premium block was stored: active=%q", got.ActiveProfile)
			}
			if err := surf.set(cfg, "autonomous.task_routing", withFlag); err != nil {
				t.Fatalf("premium row with the flag refused: %v", err)
			}
			if got := read(); got.ActiveProfile != "x" || !got.Profiles["x"]["final_gate"].AllowPremiumModel {
				t.Fatalf("flagged premium block not stored: %+v", got.Profiles["x"])
			}
		})
	}
}
