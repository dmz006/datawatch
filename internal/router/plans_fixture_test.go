package router

// Chat-channel tests over the shared plans fixture (the same JSON file the
// planrecords package loads): reordering plans and Automata through the move
// verb, the old arcs verbs, and the project arcs command. A fake daemon serves
// the fixture the way the plans read API does and records what the router
// sends. Nothing here sleeps.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type routerFixture struct {
	Project  string `json:"project"`
	Automata []struct {
		ID, Title, State string
	} `json:"automata"`
	Plans []struct {
		Key      string   `json:"key"`
		Title    string   `json:"title"`
		Order    int      `json:"order"`
		Automata []string `json:"automata"`
	} `json:"plans"`
	RunOrder map[string][]string `json:"run_order"`
	Arcs     []struct {
		AutomationID string `json:"automation_id"`
		Priority     int    `json:"priority"`
		State        string `json:"state"`
	} `json:"arcs"`
}

func loadRouterFixture(t *testing.T) *routerFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "planrecords", "testdata", "plans_fixture.json"))
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	var f routerFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse shared fixture: %v", err)
	}
	return &f
}

func planID(title string) string {
	return strings.ReplaceAll(strings.ToLower(title), " ", "-")
}

// plansListJSON is the project plans answer the fixture would give.
func (f *routerFixture) plansListJSON() string {
	row := func(i int) map[string]any {
		p := f.Plans[i]
		return map[string]any{"id": planID(p.Title), "title": p.Title, "state": "open", "order": p.Order,
			"progress": map[string]any{"done": 0, "total": len(p.Automata)}}
	}
	var plans, back []map[string]any
	order := make([]int, len(f.Plans))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return f.Plans[order[a]].Order < f.Plans[order[b]].Order })
	for _, i := range order {
		p := f.Plans[i]
		if len(p.Automata) == 0 {
			back = append(back, row(i))
		} else {
			plans = append(plans, row(i))
		}
	}
	b, _ := json.Marshal(map[string]any{"plans": plans, "backlog": map[string]any{"plans": back}})
	return string(b)
}

// automataJSON is the plan's Automata answer: run order, with the position in
// the stored plan order on each row.
func (f *routerFixture) automataJSON(key string) string {
	var stored []string
	for _, p := range f.Plans {
		if p.Key == key {
			stored = p.Automata
		}
	}
	pos := map[string]int{}
	for i, id := range stored {
		pos[id] = i + 1
	}
	var rows []map[string]any
	for _, id := range f.RunOrder[key] {
		rows = append(rows, map[string]any{"id": id, "title": id, "run": "next", "position": pos[id]})
	}
	b, _ := json.Marshal(map[string]any{"automata": rows, "next": map[string]any{"automaton_id": "aaaa0007", "title": "Search box"}})
	return string(b)
}

func fixtureDaemon(t *testing.T, f *routerFixture) (*Router, *[]planCommReq) {
	t.Helper()
	routes := map[string]planCommRoute{
		"GET /api/projects/datawatch/plans":          {0, f.plansListJSON()},
		"GET /api/plans/plans-engine/automata":       {0, f.automataJSON("engine")},
		"PUT /api/plans/order":                       {0, `{"plans":[]}`},
		"PUT /api/plans/plans-engine/automata/order": {0, `{"next":{"automaton_id":"aaaa0006","title":"Records tab"},"automata":[]}`},
	}
	arcs, _ := json.Marshal(map[string]any{"arcs": f.Arcs})
	routes["GET /api/projects/datawatch"] = planCommRoute{0, string(arcs)}
	seen, port := planCommDaemon(t, routes)
	return setupPeerRouter(t, port), seen
}

func lastBody(t *testing.T, seen *[]planCommReq, method, path string) map[string]any {
	t.Helper()
	for i := len(*seen) - 1; i >= 0; i-- {
		if rq := (*seen)[i]; rq.method == method && rq.path == path {
			var out map[string]any
			if err := json.Unmarshal([]byte(rq.body), &out); err != nil {
				t.Fatalf("body %q: %v", rq.body, err)
			}
			return out
		}
	}
	t.Fatalf("no %s %s request in %+v", method, path, *seen)
	return nil
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestFixtureChatReorderPlans(t *testing.T) {
	f := loadRouterFixture(t)
	r, seen := fixtureDaemon(t, f)
	// Plan order from the fixture: browser(1) engine(2) later(3) later2(4).
	got := strings.Join(r.HandleTestMessage("autonomous plan move datawatch later-idea 1"), "\n")
	if !strings.Contains(got, "Moved plan") {
		t.Fatalf("reply = %q", got)
	}
	want := []string{"later-idea", "plans-browser", "plans-engine", "another-later-idea"}
	if ids := strs(lastBody(t, seen, "PUT", "/api/plans/order")["plans"]); !reflect.DeepEqual(ids, want) {
		t.Fatalf("order sent = %v, want %v", ids, want)
	}
	// By id and to the end (a position past the end clamps).
	*seen = nil
	r.HandleTestMessage("autonomous plan move datawatch plans-browser 99")
	want = []string{"plans-engine", "later-idea", "another-later-idea", "plans-browser"}
	if ids := strs(lastBody(t, seen, "PUT", "/api/plans/order")["plans"]); !reflect.DeepEqual(ids, want) {
		t.Fatalf("order sent = %v, want %v", ids, want)
	}
}

func TestFixtureChatReorderAutomata(t *testing.T) {
	f := loadRouterFixture(t)
	r, seen := fixtureDaemon(t, f)
	got := strings.Join(r.HandleTestMessage("autonomous plan move plans-engine automaton aaaa0006 1"), "\n")
	if !strings.Contains(got, "Moved Automaton aaaa0006 to position 1") || !strings.Contains(got, "Runs next: Records tab") {
		t.Fatalf("reply = %q", got)
	}
	// The stored plan order is sent, not the run order, with the moved one first.
	var stored []string
	for _, p := range f.Plans {
		if p.Key == "engine" {
			stored = append([]string{"aaaa0006"}, without(p.Automata, "aaaa0006")...)
		}
	}
	if ids := strs(lastBody(t, seen, "PUT", "/api/plans/plans-engine/automata/order")["automata"]); !reflect.DeepEqual(ids, stored) {
		t.Fatalf("Automata order sent = %v, want %v", ids, stored)
	}
	// An Automaton of another plan is refused and nothing is sent.
	*seen = nil
	got = strings.Join(r.HandleTestMessage("autonomous plan move plans-engine automaton aaaa0101 1"), "\n")
	if !strings.Contains(got, "is not linked to plan") {
		t.Fatalf("reply = %q", got)
	}
	for _, rq := range *seen {
		if rq.method == "PUT" {
			t.Fatalf("a refused move sent %+v", rq)
		}
	}
}

func without(ids []string, drop string) []string {
	var out []string
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

func TestFixtureChatArcsVerbIsTheOldNameForPlan(t *testing.T) {
	f := loadRouterFixture(t)
	r, seen := fixtureDaemon(t, f)
	plan := strings.Join(r.HandleTestMessage("autonomous plan list datawatch"), "\n")
	arcs := strings.Join(r.HandleTestMessage("autonomous arcs list datawatch"), "\n")
	if plan == "" || strings.Contains(plan, "failed") {
		t.Fatalf("plan list failed: %q", plan)
	}
	if strings.ReplaceAll(plan, "autonomous plan list", "") != strings.ReplaceAll(arcs, "autonomous plan list", "") {
		t.Fatalf("arcs verb answers differently:\nplan: %s\narcs: %s", plan, arcs)
	}
	lines := strings.Split(plan, "\n")
	var titles []string
	for _, l := range lines {
		if i := strings.Index(l, ". "); i >= 0 {
			titles = append(titles, strings.TrimSpace(l[i+2:strings.Index(l, " [")]))
		}
	}
	if !reflect.DeepEqual(titles, []string{"Plans browser", "Plans engine", "Later idea", "Another later idea"}) {
		t.Fatalf("listed plans = %v\n%s", titles, plan)
	}
	if !strings.Contains(plan, "backlog 1. Later idea") {
		t.Fatalf("backlog group missing:\n%s", plan)
	}
	*seen = nil
	r.HandleTestMessage("autonomous arcs move datawatch plans-engine 1")
	if ids := strs(lastBody(t, seen, "PUT", "/api/plans/order")["plans"]); ids[0] != "plans-engine" {
		t.Fatalf("arcs move sent %v", ids)
	}
}

func TestFixtureChatProjectArcsCommandStillWorks(t *testing.T) {
	f := loadRouterFixture(t)
	r, _ := fixtureDaemon(t, f)
	for _, verb := range []string{"project arcs datawatch", "project plans datawatch"} {
		got := strings.Join(r.HandleTestMessage(verb), "\n")
		if !strings.Contains(got, "Plans for project datawatch (2):") ||
			!strings.Contains(got, "1. cccc3333 priority=2 state=active") ||
			!strings.Contains(got, "2. dddd4444 priority=1 state=planned") {
			t.Fatalf("%q answered:\n%s", verb, got)
		}
	}
}

func TestFixtureChatMoveRefusesBadInput(t *testing.T) {
	f := loadRouterFixture(t)
	r, seen := fixtureDaemon(t, f)
	for _, msg := range []string{
		"autonomous plan move datawatch plans-engine 0",
		"autonomous plan move datawatch plans-engine first",
		"autonomous plan move datawatch no-such-plan 1",
		"autonomous plan move",
	} {
		got := strings.Join(r.HandleTestMessage(msg), "\n")
		if got == "" || !strings.Contains(got, "failed") {
			t.Errorf("%q answered %q, want a failure", msg, got)
		}
	}
	for _, rq := range *seen {
		if rq.method == "PUT" {
			t.Fatalf("refused input still sent %+v", rq)
		}
	}
}
