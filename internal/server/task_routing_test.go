package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// taskRoutingFixture is a server whose LLM registry knows pi-prompt and
// claude-code, over a real manager, behind a mux with the real route
// patterns so path values resolve.
func taskRoutingFixture(t *testing.T) (*Server, *autonomous.Manager, http.Handler) {
	t.Helper()
	cfg := autonomous.DefaultConfig()
	cfg.TaskRouting = config.DefaultTaskRouting()
	cfg.TaskRouting.Enabled = true
	mgr, err := autonomous.NewManager(t.TempDir(), cfg,
		func(autonomous.DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetTaskRoutingBackendCheck(func(b string) bool { return b == "pi-prompt" || b == "claude-code" })
	reg := inference.NewRegistry()
	for _, n := range []string{"pi-prompt", "claude-code"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: inference.KindOllama}); err != nil {
			t.Fatalf("registry add %s: %v", n, err)
		}
	}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: newTestAPI(mgr),
		inferenceReg:  reg,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/task-routing", s.handleTaskRouting)
	mux.HandleFunc("/api/autonomous/task-routing/profiles", s.handleTaskRoutingProfiles)
	mux.HandleFunc("/api/autonomous/task-routing/profiles/import", s.handleTaskRoutingProfileImport)
	mux.HandleFunc("/api/autonomous/task-routing/profiles/{name}", s.handleTaskRoutingProfile)
	mux.HandleFunc("/api/autonomous/task-routing/profiles/{name}/export", s.handleTaskRoutingProfileExport)
	mux.HandleFunc("/api/autonomous/task-routing/active-profile", s.handleTaskRoutingActiveProfile)
	mux.HandleFunc("/api/autonomous/prds/{id}/task-routing", s.handleAutomatonTaskRouting)
	mux.HandleFunc("/api/autonomous/prds/{id}/task-routing/dry-run", s.handleAutomatonTaskRouting)
	return s, mgr, mux
}

func taskRoutingDo(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decodeTaskRoutingView(t *testing.T, rr *httptest.ResponseRecorder) taskRoutingView {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var v taskRoutingView
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTaskRoutingRESTGetPutRoundTrip(t *testing.T) {
	_, mgr, h := taskRoutingFixture(t)
	v := decodeTaskRoutingView(t, taskRoutingDo(h, http.MethodGet, "/api/autonomous/task-routing", ""))
	if !v.Validation.Valid || v.ActiveProfile != "default" || len(v.Profiles["default"]) == 0 || len(v.TaskTypes) == 0 {
		t.Fatalf("GET view: %+v", v)
	}
	v.Enabled = false
	v.Profiles["default"][config.TaskTypeTests] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "haiku"}
	body, _ := json.Marshal(map[string]any{"enabled": v.Enabled, "active_profile": v.ActiveProfile, "profiles": v.Profiles})
	put := decodeTaskRoutingView(t, taskRoutingDo(h, http.MethodPut, "/api/autonomous/task-routing", string(body)))
	if put.Enabled || put.Profiles["default"][config.TaskTypeTests].WorkerModel != "haiku" {
		t.Fatalf("PUT answer: %+v", put)
	}
	live := mgr.Config().TaskRouting
	if live.Enabled || live.Profiles["default"][config.TaskTypeTests].WorkerBackend != "claude-code" {
		t.Errorf("live manager config not updated: %+v", live.Profiles["default"][config.TaskTypeTests])
	}
	again := decodeTaskRoutingView(t, taskRoutingDo(h, http.MethodGet, "/api/autonomous/task-routing", ""))
	if again.Profiles["default"][config.TaskTypeTests].WorkerModel != "haiku" {
		t.Errorf("GET after PUT: %+v", again.Profiles["default"][config.TaskTypeTests])
	}
}

func TestTaskRoutingRESTPutRejectsUnknownLLM(t *testing.T) {
	_, mgr, h := taskRoutingFixture(t)
	before := mgr.Config().TaskRouting
	body := `{"enabled":true,"active_profile":"default","profiles":{"default":{
	  "docs_set":{"worker_backend":"ghost-llm","worker_model":"m"},
	  "tests":{"worker_backend":"other-ghost","worker_model":"m"}}}}`
	rr := taskRoutingDo(h, http.MethodPut, "/api/autonomous/task-routing", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body.String())
	}
	var e struct {
		Error    string   `json:"error"`
		Problems []string `json:"problems"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &e)
	joined := strings.Join(e.Problems, "|")
	if len(e.Problems) != 2 || !strings.Contains(joined, "ghost-llm") || !strings.Contains(joined, "other-ghost") {
		t.Errorf("every problem must be listed: %+v", e)
	}
	if after := mgr.Config().TaskRouting; len(after.Profiles["default"]) != len(before.Profiles["default"]) {
		t.Error("a refused table changed the live config")
	}
}

func TestTaskRoutingRESTPutRejectsPremiumRowNotNamedExplicitly(t *testing.T) {
	_, _, h := taskRoutingFixture(t)
	mk := func(allow string) string {
		return `{"enabled":true,"active_profile":"default","profiles":{"default":{"final_gate":{"worker_backend":"claude-code","worker_model":"claude-opus-5-5"` + allow + `}}}}`
	}
	rr := taskRoutingDo(h, http.MethodPut, "/api/autonomous/task-routing", mk(""))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "allow_premium_model") {
		t.Fatalf("Opus row without the explicit flag: %d %s", rr.Code, rr.Body.String())
	}
	rr = taskRoutingDo(h, http.MethodPut, "/api/autonomous/task-routing", mk(`,"allow_premium_model":true`))
	if rr.Code != http.StatusOK {
		t.Fatalf("Opus row named explicitly must be accepted: %d %s", rr.Code, rr.Body.String())
	}
}

func TestTaskRoutingRESTProfileListSaveActivateDelete(t *testing.T) {
	_, mgr, h := taskRoutingFixture(t)
	list := func() (active string, names []string) {
		rr := taskRoutingDo(h, http.MethodGet, "/api/autonomous/task-routing/profiles", "")
		var o struct {
			Active   string   `json:"active_profile"`
			Profiles []string `json:"profiles"`
		}
		if rr.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &o)
		return o.Active, o.Profiles
	}
	if a, n := list(); a != "default" || len(n) != 1 {
		t.Fatalf("initial list: %s %v", a, n)
	}
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles", `{"name":"alt"}`); rr.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	if _, n := list(); len(n) != 2 || n[0] != "alt" {
		t.Fatalf("after save: %v", n)
	}
	if len(mgr.Config().TaskRouting.Profiles["alt"]) == 0 {
		t.Error("saved profile did not copy the current table")
	}
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles", `{"name":" "}`); rr.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rr.Code)
	}
	if rr := taskRoutingDo(h, http.MethodPut, "/api/autonomous/task-routing/active-profile", `{"name":"alt"}`); rr.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}
	if a, _ := list(); a != "alt" {
		t.Errorf("active = %s", a)
	}
	if rr := taskRoutingDo(h, http.MethodPut, "/api/autonomous/task-routing/active-profile", `{"name":"ghost"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("activate unknown: %d", rr.Code)
	}
	if rr := taskRoutingDo(h, http.MethodDelete, "/api/autonomous/task-routing/profiles/alt", ""); rr.Code != http.StatusBadRequest {
		t.Errorf("deleting the active profile: %d", rr.Code)
	}
	if rr := taskRoutingDo(h, http.MethodDelete, "/api/autonomous/task-routing/profiles/default", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete inactive: %d %s", rr.Code, rr.Body.String())
	}
	if rr := taskRoutingDo(h, http.MethodDelete, "/api/autonomous/task-routing/profiles/default", ""); rr.Code != http.StatusNotFound {
		t.Errorf("delete missing: %d", rr.Code)
	}
}

func TestTaskRoutingRESTExportThenImport(t *testing.T) {
	_, mgr, h := taskRoutingFixture(t)
	rr := taskRoutingDo(h, http.MethodGet, "/api/autonomous/task-routing/profiles/default/export", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rr.Code, rr.Body.String())
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "task-routing-default.json") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	doc := rr.Body.String()
	if rr := taskRoutingDo(h, http.MethodGet, "/api/autonomous/task-routing/profiles/ghost/export", ""); rr.Code != http.StatusNotFound {
		t.Errorf("export unknown: %d", rr.Code)
	}
	// Importing under its own name collides; overwrite or a new name works.
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles/import", doc); rr.Code != http.StatusBadRequest {
		t.Errorf("import over an existing name without overwrite: %d", rr.Code)
	}
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles/import?name=copy", doc); rr.Code != http.StatusOK {
		t.Fatalf("import as copy: %d %s", rr.Code, rr.Body.String())
	}
	live := mgr.Config().TaskRouting
	if len(live.Profiles["copy"]) != len(live.Profiles["default"]) {
		t.Errorf("imported profile differs: %d vs %d rows", len(live.Profiles["copy"]), len(live.Profiles["default"]))
	}
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles/import?overwrite=true", doc); rr.Code != http.StatusOK {
		t.Errorf("import with overwrite: %d %s", rr.Code, rr.Body.String())
	}
	bad := strings.Replace(doc, `"claude-code"`, `"ghost-llm"`, 1)
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles/import?name=bad", bad); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "ghost-llm") {
		t.Errorf("import with an unknown LLM: %d %s", rr.Code, rr.Body.String())
	}
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/task-routing/profiles/import", "not json"); rr.Code != http.StatusBadRequest {
		t.Errorf("import garbage: %d", rr.Code)
	}
}

func taskRoutingAutomaton(t *testing.T, mgr *autonomous.Manager) string {
	t.Helper()
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	mgr.Store().WithLock(func() { prd.Status = autonomous.PRDNeedsReview })
	if _, err := mgr.AddStory(prd.ID, "story", "desc", "op"); err != nil {
		t.Fatal(err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	for _, title := range []string{"Write unit tests", "Implement the engine"} {
		if _, err := mgr.AddTask(prd.ID, prd.Story[0].ID, title, "", "op", nil); err != nil {
			t.Fatal(err)
		}
	}
	return prd.ID
}

func TestTaskRoutingRESTAutomatonReadShape(t *testing.T) {
	_, mgr, h := taskRoutingFixture(t)
	id := taskRoutingAutomaton(t, mgr)
	rr := taskRoutingDo(h, http.MethodGet, "/api/autonomous/prds/"+id+"/task-routing", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rr.Code, rr.Body.String())
	}
	var o struct {
		AutomatonID string                       `json:"automaton_id"`
		Tasks       []autonomous.TaskRoutingView `json:"tasks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if o.AutomatonID != id || len(o.Tasks) != 2 {
		t.Fatalf("shape: %+v", o)
	}
	for _, v := range o.Tasks {
		if v.TaskID == "" || v.TaskType == "" || v.ClassifiedBy == "" {
			t.Errorf("task view incomplete: %+v", v)
		}
		if v.Applied && v.Row == "" {
			t.Errorf("an applied task must name its row: %+v", v)
		}
	}
	if rr := taskRoutingDo(h, http.MethodGet, "/api/autonomous/prds/nope/task-routing", ""); rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "Automaton") {
		t.Errorf("unknown Automaton: %d %s", rr.Code, rr.Body.String())
	}
}

func TestTaskRoutingRESTDryRunWritesNothing(t *testing.T) {
	s, mgr, h := taskRoutingFixture(t)
	id := taskRoutingAutomaton(t, mgr)
	before, _ := mgr.Store().SnapshotPRD(id)
	cfgBefore, _ := json.Marshal(mgr.Config().TaskRouting)
	rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/prds/"+id+"/task-routing/dry-run", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	var o struct {
		Entries []autonomous.TaskRoutingDryRunEntry `json:"entries"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &o)
	if len(o.Entries) != 2 || !o.Entries[0].WouldApply {
		t.Fatalf("entries: %+v", o.Entries)
	}
	after, _ := mgr.Store().SnapshotPRD(id)
	if len(after.Decisions) != len(before.Decisions) {
		t.Error("dry run appended a decision")
	}
	for ti := range after.Story[0].Tasks {
		a, b := after.Story[0].Tasks[ti], before.Story[0].Tasks[ti]
		if a.Backend != b.Backend || a.Model != b.Model || a.UpdatedAt != b.UpdatedAt {
			t.Error("dry run changed a task")
		}
	}
	if cfgAfter, _ := json.Marshal(mgr.Config().TaskRouting); string(cfgAfter) != string(cfgBefore) {
		t.Error("dry run changed the routing table")
	}
	if s.cfg.Autonomous.TaskRouting.ActiveProfile != "" {
		t.Error("dry run persisted config")
	}
	if rr := taskRoutingDo(h, http.MethodPost, "/api/autonomous/prds/"+id+"/task-routing/dry-run", `{"profile":"ghost"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown profile: %d %s", rr.Code, rr.Body.String())
	}
	if rr := taskRoutingDo(h, http.MethodGet, "/api/autonomous/prds/"+id+"/task-routing/dry-run", ""); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET dry-run: %d", rr.Code)
	}
}

// The three task routing settings written through PUT /api/config, the path
// the MCP config_set tool, the comm configure verb and CLI config set all
// send their value through, then read back through GET /api/config.

func newTaskRoutingConfigServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Autonomous.TaskRouting = config.DefaultTaskRouting()
	return &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
}

func taskRoutingPutConfig(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	return rr
}

func taskRoutingGetConfig(t *testing.T, s *Server) config.TaskRoutingConfig {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var top struct {
		Autonomous struct {
			TaskRouting config.TaskRoutingConfig `json:"task_routing"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return top.Autonomous.TaskRouting
}

func taskRoutingSkippedKeys(t *testing.T, rr *httptest.ResponseRecorder) ([]string, string) {
	t.Helper()
	var resp struct {
		Skipped       []string `json:"skipped"`
		SkippedReason string   `json:"skipped_reason"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode PUT reply: %v (%s)", err, rr.Body.String())
	}
	return resp.Skipped, resp.SkippedReason
}

func TestTaskRoutingConfigEnabledWriteReadBack(t *testing.T) {
	s := newTaskRoutingConfigServer(t)
	if rr := taskRoutingPutConfig(t, s, `{"autonomous.task_routing.enabled": true}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT enabled=true: %d %s", rr.Code, rr.Body.String())
	}
	if !taskRoutingGetConfig(t, s).Enabled {
		t.Fatal("enabled did not read back true")
	}
	if rr := taskRoutingPutConfig(t, s, `{"autonomous.task_routing.enabled": false}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT enabled=false: %d", rr.Code)
	}
	if taskRoutingGetConfig(t, s).Enabled {
		t.Fatal("enabled did not read back false")
	}
}

func TestTaskRoutingConfigActiveProfileWriteReadBackAndSkip(t *testing.T) {
	s := newTaskRoutingConfigServer(t)
	s.cfg.Autonomous.TaskRouting.Profiles["alt"] = s.cfg.Autonomous.TaskRouting.Profiles["default"]
	if rr := taskRoutingPutConfig(t, s, `{"autonomous.task_routing.active_profile": "alt"}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT active_profile=alt: %d %s", rr.Code, rr.Body.String())
	}
	if got := taskRoutingGetConfig(t, s).ActiveProfile; got != "alt" {
		t.Fatalf("active_profile = %q, want alt", got)
	}
	// A profile that does not exist is skipped and reported, not stored.
	rr := taskRoutingPutConfig(t, s, `{"autonomous.task_routing.active_profile": "no-such-profile"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT unknown profile: %d", rr.Code)
	}
	skipped, reason := taskRoutingSkippedKeys(t, rr)
	if len(skipped) != 1 || skipped[0] != "autonomous.task_routing.active_profile" {
		t.Fatalf("skipped = %v, want the active_profile key", skipped)
	}
	if strings.Contains(reason, "rotate-token") || reason == "" {
		t.Fatalf("skipped_reason should explain the routing skip, got %q", reason)
	}
	if got := taskRoutingGetConfig(t, s).ActiveProfile; got != "alt" {
		t.Fatalf("active_profile changed to %q after a skipped write", got)
	}
}

func TestTaskRoutingConfigBlockWriteReadBackAndSkip(t *testing.T) {
	s := newTaskRoutingConfigServer(t)
	good := config.DefaultTaskRouting()
	good.Enabled = true
	good.Profiles["lean"] = good.Profiles["default"]
	good.ActiveProfile = "lean"
	b, _ := json.Marshal(map[string]any{"autonomous.task_routing": good})
	if rr := taskRoutingPutConfig(t, s, string(b)); rr.Code != http.StatusOK {
		t.Fatalf("PUT block: %d %s", rr.Code, rr.Body.String())
	}
	got := taskRoutingGetConfig(t, s)
	if !got.Enabled || got.ActiveProfile != "lean" || len(got.Profiles["lean"]) == 0 {
		t.Fatalf("block did not read back: enabled=%v active=%q profiles=%d", got.Enabled, got.ActiveProfile, len(got.Profiles))
	}

	// A block that fails ValidateTaskRouting (a row for a task type that does
	// not exist) is skipped and reported; the stored block stays as it was.
	bad := config.TaskRoutingConfig{Enabled: true, ActiveProfile: "x", Profiles: map[string]config.TaskRoutingProfile{
		"x": {"not-a-task-type": config.TaskRoutingRow{WorkerBackend: "pi-prompt"}},
	}}
	b, _ = json.Marshal(map[string]any{"autonomous.task_routing": bad})
	rr := taskRoutingPutConfig(t, s, string(b))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT bad block: %d", rr.Code)
	}
	skipped, _ := taskRoutingSkippedKeys(t, rr)
	if len(skipped) != 1 || skipped[0] != "autonomous.task_routing" {
		t.Fatalf("skipped = %v, want the block key", skipped)
	}
	if after := taskRoutingGetConfig(t, s); after.ActiveProfile != "lean" || len(after.Profiles["x"]) != 0 {
		t.Fatalf("a refused block was stored: active=%q", after.ActiveProfile)
	}

	// The block as a JSON string (what a surface that does not parse it sends)
	// is skipped too, never half-applied.
	rr = taskRoutingPutConfig(t, s, `{"autonomous.task_routing": "{\"enabled\":true}"}`)
	if skipped, _ := taskRoutingSkippedKeys(t, rr); len(skipped) != 1 {
		t.Fatalf("string block should be skipped, got %v", skipped)
	}
}
