package mcp

// MCP-surface tests over the shared plans fixture (the same JSON file the
// planrecords package loads): the reorder tools send the order the fixture
// gives them, the old project_set_arcs tool still works, and search forwards
// its state filter. A stub daemon records what each tool proxies. Nothing here
// sleeps.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

type mcpFixture struct {
	Project string `json:"project"`
	Plans   []struct {
		Key      string   `json:"key"`
		Title    string   `json:"title"`
		Order    int      `json:"order"`
		Automata []string `json:"automata"`
	} `json:"plans"`
	Arcs []struct {
		AutomationID string `json:"automation_id"`
		Priority     int    `json:"priority"`
		State        string `json:"state"`
	} `json:"arcs"`
	Findings []struct {
		Title string `json:"title"`
		State string `json:"state"`
	} `json:"findings"`
}

func loadMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "planrecords", "testdata", "plans_fixture.json"))
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	var f mcpFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse shared fixture: %v", err)
	}
	return &f
}

func (f *mcpFixture) plan(key string) (title string, automata []string) {
	for _, p := range f.Plans {
		if p.Key == key {
			return p.Title, p.Automata
		}
	}
	return "", nil
}

func anyStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestFixtureMCPToolsAreRegistered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]string{}
	for _, tl := range tools {
		have[tl.Name] = tl.Description
	}
	for _, name := range []string{"plan_reorder", "plan_reorder_automata", "plan_attach", "plan_detach",
		"plan_record_search", "plan_rollup", "project_set_arcs"} {
		if _, ok := have[name]; !ok {
			t.Errorf("tool %q is not registered", name)
		}
	}
	if d := have["project_set_arcs"]; !strings.Contains(strings.ToLower(d), "deprecated") || !strings.Contains(d, "plan_reorder") {
		t.Errorf("project_set_arcs must say it is the old name and point at plan_reorder: %q", d)
	}
}

func TestFixtureMCPReorderPlans(t *testing.T) {
	f := loadMCPFixture(t)
	d := newPlanRecordTestDaemon(t, 0, `{"plans":[]}`)
	later, _ := f.plan("later")
	engine, _ := f.plan("engine")
	res := d.call(t, d.s.handlePlanReorder, map[string]any{"plans": []any{later, engine}})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPut || d.path != "/api/plans/order" {
		t.Fatalf("proxied %s %s", d.method, d.path)
	}
	if got := anyStrings(d.bodyMap(t)["plans"]); !reflect.DeepEqual(got, []string{later, engine}) {
		t.Fatalf("order sent = %v", got)
	}
	// A JSON array string works the same.
	res = d.call(t, d.s.handlePlanReorder, map[string]any{"plans": `["Plans browser","Plans engine"]`})
	if res.IsError {
		t.Fatalf("string list: %s", planResultText(res))
	}
	if got := anyStrings(d.bodyMap(t)["plans"]); !reflect.DeepEqual(got, []string{"Plans browser", "Plans engine"}) {
		t.Fatalf("order sent = %v", got)
	}
}

func TestFixtureMCPReorderAutomata(t *testing.T) {
	f := loadMCPFixture(t)
	d := newPlanRecordTestDaemon(t, 0, `{"automata":[],"next":{"automaton_id":"aaaa0006"}}`)
	title, ids := f.plan("engine")
	rev := make([]string, len(ids))
	for i, id := range ids {
		rev[len(ids)-1-i] = id
	}
	args := make([]any, len(rev))
	for i, id := range rev {
		args[i] = id
	}
	res := d.call(t, d.s.handlePlanReorderAutomata, map[string]any{"plan": title, "automata": args})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	if d.method != http.MethodPut || d.rawPath != "/api/plans/Plans%20engine/automata/order" {
		t.Fatalf("proxied %s %s", d.method, d.rawPath)
	}
	if got := anyStrings(d.bodyMap(t)["automata"]); !reflect.DeepEqual(got, rev) {
		t.Fatalf("order sent = %v", got)
	}
	if !strings.Contains(planResultText(res), "aaaa0006") {
		t.Fatalf("the daemon's run view must come back: %s", planResultText(res))
	}
}

func TestFixtureMCPReorderRefusesBadInputBeforeTheDaemon(t *testing.T) {
	f := loadMCPFixture(t)
	d := newPlanRecordTestDaemon(t, 0, `{}`)
	title, _ := f.plan("engine")
	for name, c := range map[string]struct{ args map[string]any }{
		"plans missing":      {map[string]any{}},
		"plans empty":        {map[string]any{"plans": []any{}}},
		"plans blank entry":  {map[string]any{"plans": []any{title, " "}}},
		"plans not text":     {map[string]any{"plans": []any{float64(1)}}},
		"automata no plan":   {map[string]any{"automata": []any{"aaaa0001"}}},
		"automata missing":   {map[string]any{"plan": title}},
		"automata blank one": {map[string]any{"plan": title, "automata": []any{""}}},
	} {
		d.method = ""
		var res bool
		if strings.HasPrefix(name, "plans") {
			res = d.call(t, d.s.handlePlanReorder, c.args).IsError
		} else {
			res = d.call(t, d.s.handlePlanReorderAutomata, c.args).IsError
		}
		if !res {
			t.Errorf("%s: expected a tool error", name)
		}
		if d.method != "" {
			t.Errorf("%s: the refused call reached the daemon (%s %s)", name, d.method, d.path)
		}
	}
}

func TestFixtureMCPDaemonRefusalSurfaces(t *testing.T) {
	f := loadMCPFixture(t)
	d := newPlanRecordTestDaemon(t, http.StatusBadRequest, `{"error":"list every Automaton of the plan exactly once (12 linked, 1 given)"}`)
	title, _ := f.plan("engine")
	res := d.call(t, d.s.handlePlanReorderAutomata, map[string]any{"plan": title, "automata": []any{"aaaa0001"}})
	if !res.IsError || !strings.Contains(planResultText(res), "exactly once") {
		t.Fatalf("daemon refusal lost: %v %s", res.IsError, planResultText(res))
	}
}

func TestFixtureMCPProjectSetArcsStillWorks(t *testing.T) {
	f := loadMCPFixture(t)
	cap, s, done := newRecordServer(t, `{"name":"datawatch"}`)
	defer done()
	arcs := make([]any, 0, len(f.Arcs))
	for _, a := range f.Arcs {
		arcs = append(arcs, map[string]any{"automation_id": a.AutomationID, "priority": float64(a.Priority), "state": a.State})
	}
	res := callTool(t, s.handleProjectSetArcs, map[string]any{"name": f.Project, "arcs": arcs})
	if res.IsError {
		t.Fatalf("project_set_arcs: %s", resContentText(t, res))
	}
	if cap.method != http.MethodPut || cap.path != "/api/projects/datawatch/arcs" {
		t.Fatalf("proxied %s %s", cap.method, cap.path)
	}
	got, _ := cap.body["arcs"].([]any)
	if len(got) != len(f.Arcs) {
		t.Fatalf("arcs sent = %v", cap.body["arcs"])
	}
	for i, a := range f.Arcs {
		if got[i].(map[string]any)["automation_id"] != a.AutomationID {
			t.Errorf("array order not kept at %d: %v", i, got[i])
		}
	}
}

func TestFixtureMCPSearchForwardsStateFilter(t *testing.T) {
	f := loadMCPFixture(t)
	d := newPlanRecordTestDaemon(t, 0, `{"hits":[]}`)
	state := f.Findings[0].State
	res := d.call(t, d.s.handlePlanRecordSearch, map[string]any{"q": "search", "project": f.Project, "state": state, "limit": 20, "offset": 40})
	if res.IsError {
		t.Fatalf("tool error: %s", planResultText(res))
	}
	for k, want := range map[string]string{"q": "search", "project": f.Project, "state": state, "limit": "20", "offset": "40"} {
		if q := d.query[k]; len(q) != 1 || q[0] != want {
			t.Errorf("query %s = %v, want %s", k, q, want)
		}
	}
}
