package main

// CLI tests over the shared plans fixture (the same JSON file the planrecords
// package loads): reorder of plans and of a plan's Automata, and the old
// `projects arcs` command. A fake daemon serves the fixture and records what
// the CLI sends. Nothing here sleeps.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type cliFixture struct {
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
}

func loadCLIFixture(t *testing.T) *cliFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "planrecords", "testdata", "plans_fixture.json"))
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	var f cliFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse shared fixture: %v", err)
	}
	return &f
}

// fixtureCLIDaemon records each request and answers the plans routes.
type fixtureCLIDaemon struct {
	mu   sync.Mutex
	reqs []string
	body map[string]any
	f    *cliFixture
}

func (d *fixtureCLIDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reqs = append(d.reqs, r.Method+" "+r.URL.Path)
	d.body = nil
	_ = json.NewDecoder(r.Body).Decode(&d.body)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/api/plans/order":
		var rows []map[string]any
		for i, ref := range d.body["plans"].([]any) {
			rows = append(rows, map[string]any{"title": ref, "id": ref, "order": i + 1})
		}
		b, _ := json.Marshal(map[string]any{"plans": rows})
		_, _ = w.Write(b)
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/automata/order"):
		var rows []map[string]any
		for _, id := range d.body["automata"].([]any) {
			rows = append(rows, map[string]any{"title": id, "id": id, "run": "next"})
		}
		b, _ := json.Marshal(map[string]any{"plan": map[string]any{"title": "Plans engine", "id": "plans-engine"},
			"next": map[string]any{"automaton_id": "aaaa0006", "title": "aaaa0006"}, "automata": rows})
		_, _ = w.Write(b)
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/datawatch/plans":
		_, _ = w.Write([]byte(`{"project":"datawatch","plans":[],"backlog":{"plans":[]}}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/datawatch":
		arcs, _ := json.Marshal(d.f.Arcs)
		_, _ = w.Write([]byte(`{"name":"datawatch","dir":"/srv/datawatch","arcs":` + string(arcs) + `}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func withFixtureDaemon(t *testing.T) *fixtureCLIDaemon {
	t.Helper()
	d := &fixtureCLIDaemon{f: loadCLIFixture(t)}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	return d
}

func TestFixtureCLIReorderPlans(t *testing.T) {
	d := withFixtureDaemon(t)
	cmd := newPlansCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"reorder", "Later idea", "Plans engine"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.reqs, []string{"PUT /api/plans/order"}) {
		t.Fatalf("requests = %v", d.reqs)
	}
	var got []string
	for _, v := range d.body["plans"].([]any) {
		got = append(got, v.(string))
	}
	if !reflect.DeepEqual(got, []string{"Later idea", "Plans engine"}) {
		t.Fatalf("order sent = %v", got)
	}
	if !strings.Contains(out.String(), "1. Later idea") || !strings.Contains(out.String(), "2. Plans engine") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestFixtureCLIReorderAutomata(t *testing.T) {
	d := withFixtureDaemon(t)
	var ids []string
	for _, p := range d.f.Plans {
		if p.Key == "engine" {
			ids = append(ids, p.Automata...)
		}
	}
	ids[0], ids[len(ids)-1] = ids[len(ids)-1], ids[0]
	cmd := newPlansCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(append([]string{"reorder-automata", "plans-engine"}, ids...))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.reqs, []string{"PUT /api/plans/plans-engine/automata/order"}) {
		t.Fatalf("requests = %v", d.reqs)
	}
	var got []string
	for _, v := range d.body["automata"].([]any) {
		got = append(got, v.(string))
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("order sent = %v, want %v", got, ids)
	}
	if !strings.Contains(out.String(), "Plan Plans engine") || !strings.Contains(out.String(), "aaaa0006") {
		t.Fatalf("run view = %q", out.String())
	}
	// reorder-automata needs the plan and at least one Automaton.
	cmd = newPlansCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"reorder-automata", "plans-engine"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("reorder-automata with no Automaton must fail")
	}
}

func TestFixtureCLIProjectsArcsStillWorks(t *testing.T) {
	d := withFixtureDaemon(t)
	cmd := newProjectsCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"arcs", "datawatch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("projects arcs: %v", err)
	}
	if !reflect.DeepEqual(d.reqs, []string{"GET /api/projects/datawatch/plans"}) {
		t.Fatalf("projects arcs must show the plans list, got %v", d.reqs)
	}
	// --set keeps the old arc list form and the order given.
	d.reqs = nil
	var spec []string
	for _, a := range d.f.Arcs {
		spec = append(spec, a.AutomationID+":"+strconv.Itoa(a.Priority)+":"+a.State)
	}
	cmd = newProjectsCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"arcs", "datawatch", "--set", strings.Join(spec, ",")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("projects arcs --set: %v", err)
	}
	raw, _ := json.Marshal(d.body["arcs"])
	var got []projectArcArg
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(d.f.Arcs) {
		t.Fatalf("arcs sent = %+v", got)
	}
	for i, a := range d.f.Arcs {
		if got[i].AutomationID != a.AutomationID || got[i].Priority != a.Priority {
			t.Errorf("arc %d = %+v, want %+v", i, got[i], a)
		}
	}
}
