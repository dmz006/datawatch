package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// plansReadMux registers the plans read routes the way server.go does.
func plansReadMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects/{name}/plans", s.handleProjectPlans)
	mux.HandleFunc("GET /api/projects/{name}/overview-summary", s.handleProjectOverviewSummary)
	mux.HandleFunc("GET /api/projects/{name}/arcs", s.handleProjectArcs)
	mux.HandleFunc("GET /api/plans/{ref}/automata", s.handlePlanAutomata)
	mux.HandleFunc("GET /api/plans/{ref}/tasks", s.handlePlanTasks)
	mux.HandleFunc("/api/plan-records/", s.handlePlanRecords)
	return mux
}

func plansGet(t *testing.T, mux *http.ServeMux, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func mustPlan(t *testing.T, st *planrecords.Store, title string, order int, goal string) planrecords.Record {
	t.Helper()
	p, err := st.Create(planrecords.Record{Kind: planrecords.KindPlan, Project: "datawatch", Title: title, Goal: goal, Order: order})
	if err != nil {
		t.Fatalf("create plan %q: %v", title, err)
	}
	return p
}

func attach(t *testing.T, st *planrecords.Store, plan, id string, info planrecords.LinkInfo) {
	t.Helper()
	if _, err := st.AttachAutomaton(plan, id, info); err != nil {
		t.Fatalf("attach %s: %v", id, err)
	}
}

func TestPlansReadAPIRunOrderAndNextMarker(t *testing.T) {
	r := newPlanRecordsRig(t)
	r.s.planPointsSource = func() map[string]float64 { return map[string]float64{"a-done": 2, "a-run": 3.5} }
	p := mustPlan(t, r.store, "Ship the thing", 1, "Make it real")
	if _, err := r.store.AttachAutomation(p.ID, "auto-1", planrecords.LinkInfo{Title: "Chain", State: "active"}); err != nil {
		t.Fatal(err)
	}
	attach(t, r.store, p.ID, "a-held", planrecords.LinkInfo{Title: "Held one", State: "paused"})
	attach(t, r.store, p.ID, "a-next", planrecords.LinkInfo{Title: "Next one", State: "draft"})
	attach(t, r.store, p.ID, "a-block", planrecords.LinkInfo{Title: "Blocked one", State: "blocked"})
	attach(t, r.store, p.ID, "a-run", planrecords.LinkInfo{Title: "Running one", State: "running"})
	attach(t, r.store, p.ID, "a-done2", planrecords.LinkInfo{Title: "Done later", State: "completed", CompletedAt: "2026-11-02T00:00:00Z"})
	attach(t, r.store, p.ID, "a-done", planrecords.LinkInfo{Title: "Done first", State: "completed", CompletedAt: "2026-11-01T00:00:00Z"})
	mustPlan(t, r.store, "Later idea", 2, "")
	mux := plansReadMux(r.s)

	code, body := plansGet(t, mux, "/api/plans/"+p.ID+"/automata")
	if code != 200 {
		t.Fatalf("automata: %d", code)
	}
	var got []string
	for _, a := range body["automata"].([]any) {
		got = append(got, a.(map[string]any)["id"].(string))
	}
	want := []string{"a-done", "a-done2", "a-run", "a-next", "a-block", "a-held"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("run order = %v, want %v", got, want)
	}
	if n := body["next"].(map[string]any)["automaton_id"]; n != "a-next" {
		t.Fatalf("next marker = %v, want a-next (blocked and held are skipped)", n)
	}
	rows := body["automata"].([]any)
	if rows[4].(map[string]any)["skipped"] != true || rows[5].(map[string]any)["skipped"] != true {
		t.Fatalf("blocked and held rows must be marked skipped: %v", rows)
	}
	if rows[3].(map[string]any)["is_next"] != true {
		t.Fatalf("a-next row must carry is_next")
	}

	// Paged.
	_, body = plansGet(t, mux, "/api/plans/"+p.ID+"/automata?limit=2&offset=2")
	if len(body["automata"].([]any)) != 2 || body["page"].(map[string]any)["total"].(float64) != 6 {
		t.Fatalf("paging: %v", body)
	}
	if body["next"].(map[string]any)["automaton_id"] != "a-next" {
		t.Fatalf("next marker must cover the whole list, not the page")
	}

	// Plans list: progress, points, Backlog group.
	code, body = plansGet(t, mux, "/api/projects/datawatch/plans")
	if code != 200 {
		t.Fatalf("plans: %d", code)
	}
	plans := body["plans"].([]any)
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1 (the other plan has no Automaton)", len(plans))
	}
	row := plans[0].(map[string]any)
	prog := row["progress"].(map[string]any)
	if prog["done"].(float64) != 2 || prog["total"].(float64) != 6 || prog["percent"].(float64) != 33 {
		t.Fatalf("progress = %v", prog)
	}
	if row["points"].(float64) != 5.5 || row["goal"] != "Make it real" {
		t.Fatalf("points/goal = %v / %v", row["points"], row["goal"])
	}
	if ids := row["automation_ids"].([]any); len(ids) != 1 || ids[0] != "auto-1" {
		t.Fatalf("automation_ids = %v", ids)
	}
	if row["next"].(map[string]any)["automaton_id"] != "a-next" {
		t.Fatalf("plan row next = %v", row["next"])
	}
	back := body["backlog"].(map[string]any)["plans"].([]any)
	if len(back) != 1 || back[0].(map[string]any)["title"] != "Later idea" {
		t.Fatalf("backlog = %v", back)
	}

	// Overview summary.
	code, body = plansGet(t, mux, "/api/projects/datawatch/overview-summary")
	if code != 200 {
		t.Fatalf("overview: %d", code)
	}
	lines := body["active_plans"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["progress"].(map[string]any)["percent"].(float64) != 33 {
		t.Fatalf("active plan lines = %v", lines)
	}
	if body["next"].(map[string]any)["automaton_id"] != "a-next" {
		t.Fatalf("overview next = %v", body["next"])
	}
	counts := body["counts"].(map[string]any)
	if counts["plans"].(map[string]any)["total"].(float64) != 2 || counts["plans"].(map[string]any)["backlog"].(float64) != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestPlansReadAPITasksOnExpand(t *testing.T) {
	r := newPlanRecordsRig(t)
	p := mustPlan(t, r.store, "Plan with tasks", 1, "")
	attach(t, r.store, p.ID, "a1", planrecords.LinkInfo{Title: "One", State: "running"})
	attach(t, r.store, p.ID, "a2", planrecords.LinkInfo{Title: "Two", State: "draft"})
	reads := 0
	r.s.planTasksSource = func(id string) ([]planTaskRow, bool) {
		reads++
		return []planTaskRow{{Title: "t1 of " + id, ID: id + "-t1", Status: "pending", AutomatonID: id}, {Title: "t2 of " + id, ID: id + "-t2", Status: "completed", AutomatonID: id}}, true
	}
	mux := plansReadMux(r.s)
	plansGet(t, mux, "/api/projects/datawatch/plans")
	plansGet(t, mux, "/api/plans/"+p.ID+"/automata")
	if reads != 0 {
		t.Fatalf("the plans and Automata lists must not read tasks (read %d times)", reads)
	}
	_, body := plansGet(t, mux, "/api/plans/"+p.ID+"/tasks?limit=3")
	if len(body["tasks"].([]any)) != 3 || body["page"].(map[string]any)["total"].(float64) != 4 {
		t.Fatalf("tasks page = %v", body)
	}
}

func TestPlansReadAPIBrowse(t *testing.T) {
	r := newPlanRecordsRig(t)
	p := mustPlan(t, r.store, "Plan one", 1, "")
	attach(t, r.store, p.ID, "a1", planrecords.LinkInfo{Title: "One", State: "running"})
	row, err := r.store.Create(planrecords.Record{Kind: planrecords.KindRegistryRow, Project: "datawatch", Title: "Fix the widget",
		Links: []planrecords.Link{{Kind: "automaton", Ref: "a1"}}})
	if err != nil {
		t.Fatal(err)
	}
	fin, err := r.store.Create(planrecords.Record{Kind: planrecords.KindFinding, Project: "datawatch", Title: "Widget leaks"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.AttachFinding(fin.ID, planrecords.Target{Kind: planrecords.AttachPlan, Ref: p.ID}); err != nil {
		t.Fatal(err)
	}
	mux := plansReadMux(r.s)
	_, body := plansGet(t, mux, "/api/plan-records/browse")
	if body["page"].(map[string]any)["total"].(float64) != 2 {
		t.Fatalf("browse total = %v", body["page"])
	}
	_, body = plansGet(t, mux, "/api/plan-records/browse?q=widget&kind=finding")
	recs := body["records"].([]any)
	if len(recs) != 1 || recs[0].(map[string]any)["plan_id"] != p.ID {
		t.Fatalf("finding browse = %v", recs)
	}
	_, body = plansGet(t, mux, "/api/plan-records/browse?kind=registry_row&state=open")
	recs = body["records"].([]any)
	if len(recs) != 1 || recs[0].(map[string]any)["id"] != row.ID ||
		fmt.Sprint(recs[0].(map[string]any)["automaton_ids"]) != "[a1]" {
		t.Fatalf("row browse = %v", recs)
	}
	if code, _ := plansGet(t, mux, "/api/plan-records/browse?kind=plan"); code != 400 {
		t.Fatalf("kind=plan = %d, want 400", code)
	}
	if code, _ := plansGet(t, mux, "/api/plan-records/browse?state=bogus"); code != 400 {
		t.Fatalf("state=bogus = %d, want 400", code)
	}
}

func TestPlansReadAPIGuards(t *testing.T) {
	r := newPlanRecordsRig(t)
	mux := plansReadMux(r.s)
	if code, _ := plansGet(t, mux, "/api/projects/nope/plans"); code != 404 {
		t.Fatalf("unknown project = %d", code)
	}
	if code, _ := plansGet(t, mux, "/api/projects/datawatch/plans?limit=0"); code != 400 {
		t.Fatalf("limit=0 = %d", code)
	}
	if code, _ := plansGet(t, mux, "/api/plans/does-not-exist/automata"); code != 404 {
		t.Fatalf("unknown plan = %d", code)
	}
	off := false
	r.s.cfg.Autonomous.PlanRecords.Enabled = &off
	if code, _ := plansGet(t, mux, "/api/projects/datawatch/plans"); code != 404 {
		t.Fatalf("plan records off = %d", code)
	}
	_ = federation.CapAutonomousRead
}

// TestPlansReadAPIDeprecatedArcsAlias: the old arcs route and the roll-up arcs
// field keep their data and gain plans.
func TestPlansReadAPIDeprecatedArcsAlias(t *testing.T) {
	r := newPlanRecordsRig(t)
	p := mustPlan(t, r.store, "Plan one", 1, "")
	attach(t, r.store, p.ID, "a1", planrecords.LinkInfo{Title: "One", State: "running"})
	rows := r.s.plansForAlias("datawatch")
	if len(rows) != 1 || rows[0].Title != "Plan one" {
		t.Fatalf("alias plans = %+v", rows)
	}
	roll := buildProjectRollup(rollupInput{Project: "datawatch", WindowDays: 7, Now: time.Now()})
	if roll.Plans == nil || roll.Arcs == nil {
		t.Fatalf("roll-up must keep arcs and carry plans (non-nil lists): %+v", roll)
	}
}

// TestPlansReadAPIOneSecondBudget documents the budget: every read answers from
// the summary index in about a second for a large project (hundreds of plans,
// thousands of Automata, rows and findings).
func TestPlansReadAPIOneSecondBudget(t *testing.T) {
	const (
		plans         = 300
		automataEach  = 20
		registryRows  = 5000
		findings      = 3000
		budget        = time.Second
		fixtureRFC    = "2026-01-02T03:04:05Z"
		projectAlias  = "datawatch"
		firstPlanName = "Plan 0001"
	)
	root := t.TempDir()
	dir := filepath.Join(root, "plan-records")
	for _, k := range []planrecords.Kind{planrecords.KindPlan, planrecords.KindRegistryRow, planrecords.KindFinding} {
		if err := os.MkdirAll(filepath.Join(dir, string(k)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rec planrecords.Record) {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, string(rec.Kind), rec.UID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	states := []string{"completed", "running", "draft", "blocked", "paused"}
	for i := 1; i <= plans; i++ {
		rec := planrecords.Record{
			ID: fmt.Sprintf("plan-%04d", i), UID: fmt.Sprintf("uid-plan-%04d", i), Kind: planrecords.KindPlan,
			Project: projectAlias, State: planrecords.StatePlanned, Title: fmt.Sprintf("Plan %04d", i),
			Goal: "goal", Order: i, CreatedAt: fixtureRFC, UpdatedAt: fixtureRFC,
			Automations: []string{fmt.Sprintf("auto-%04d", i)}, LinkInfo: map[string]planrecords.LinkInfo{},
		}
		for j := 0; j < automataEach; j++ {
			id := fmt.Sprintf("a-%04d-%02d", i, j)
			rec.Automata = append(rec.Automata, id)
			rec.LinkInfo["automaton:"+id] = planrecords.LinkInfo{Title: "Automaton " + id, State: states[j%len(states)]}
		}
		write(rec)
	}
	for i := 1; i <= registryRows; i++ {
		write(planrecords.Record{
			ID: fmt.Sprintf("row-%05d", i), UID: fmt.Sprintf("uid-row-%05d", i), Kind: planrecords.KindRegistryRow,
			Project: projectAlias, State: planrecords.StateOpen, Title: fmt.Sprintf("Registry row %05d widget", i),
			LegacyNumber: i, CreatedAt: fixtureRFC, UpdatedAt: fixtureRFC,
		})
	}
	for i := 1; i <= findings; i++ {
		write(planrecords.Record{
			ID: fmt.Sprintf("finding-%05d", i), UID: fmt.Sprintf("uid-finding-%05d", i), Kind: planrecords.KindFinding,
			Project: projectAlias, State: planrecords.StateOpen, Title: fmt.Sprintf("Finding %05d", i),
			CreatedAt: fixtureRFC, UpdatedAt: fixtureRFC,
		})
	}
	st, err := planrecords.Open(root, nil, planrecords.Options{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := len(st.Skipped()); got != 0 {
		t.Fatalf("fixture files skipped: %d", got)
	}
	cfg := config.DefaultConfig()
	cfg.Projects = map[string]config.ProjectConfigEntry{projectAlias: {Dir: root}}
	s := &Server{cfg: cfg}
	s.SetPlanRecords(st)
	s.planPointsSource = func() map[string]float64 { return map[string]float64{} }
	mux := plansReadMux(s)

	paths := []string{
		"/api/projects/datawatch/plans",
		"/api/projects/datawatch/plans?limit=500&offset=100",
		"/api/projects/datawatch/overview-summary",
		"/api/plans/plan-0150/automata",
		"/api/plan-records/browse",
		"/api/plan-records/browse?q=widget&kind=registry_row&state=open&limit=500",
		"/api/plan-records/browse?q=finding+00042",
	}
	for _, p := range paths {
		start := time.Now()
		code, _ := plansGet(t, mux, p)
		if code != http.StatusOK {
			t.Fatalf("%s = %d", p, code)
		}
		if el := time.Since(start); el > budget {
			t.Errorf("%s took %v, over the %v budget", p, el, budget)
		}
	}
	_, body := plansGet(t, mux, "/api/projects/datawatch/plans?limit=1")
	if body["plans"].([]any)[0].(map[string]any)["title"] != firstPlanName {
		t.Fatalf("first plan = %v", body["plans"])
	}
}
