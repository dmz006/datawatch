package server

// REST-side tests over the shared plans fixture (the same JSON file the
// planrecords package loads): reads, the overview payload, reordering, the
// migration, paging on a large project inside the one-second budget, the old
// routes, and the rename in all five locales. No test here sleeps.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/planrecords"
)

type srvFixture struct {
	Project  string `json:"project"`
	Automata []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		State       string `json:"state"`
		CompletedAt string `json:"completed_at"`
	} `json:"automata"`
	Automations []struct {
		ID, Title, State string
	} `json:"automations"`
	Plans []struct {
		Key         string   `json:"key"`
		Title       string   `json:"title"`
		Goal        string   `json:"goal"`
		Order       int      `json:"order"`
		Automata    []string `json:"automata"`
		Automations []string `json:"automations"`
	} `json:"plans"`
	RunOrder map[string][]string `json:"run_order"`
	Next     map[string]string   `json:"next"`
	Arcs     []struct {
		AutomationID string `json:"automation_id"`
		Priority     int    `json:"priority"`
		State        string `json:"state"`
	} `json:"arcs"`
	Rows []struct {
		Title        string             `json:"title"`
		State        string             `json:"state"`
		LegacyNumber int                `json:"legacy_number"`
		Body         string             `json:"body"`
		Links        []planrecords.Link `json:"links"`
	} `json:"rows"`
	Resolve  map[string]planrecords.MigrateRef `json:"resolve"`
	Findings []struct {
		Title      string `json:"title"`
		State      string `json:"state"`
		AttachKind string `json:"attach_kind"`
		AttachRef  string `json:"attach_ref"`
	} `json:"findings"`
}

func loadSrvFixture(t *testing.T) *srvFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "planrecords", "testdata", "plans_fixture.json"))
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	var f srvFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse shared fixture: %v", err)
	}
	return &f
}

func (f *srvFixture) automaton(id string) planrecords.LinkInfo {
	for _, a := range f.Automata {
		if a.ID == id {
			return planrecords.LinkInfo{Title: a.Title, State: a.State, CompletedAt: a.CompletedAt}
		}
	}
	return planrecords.LinkInfo{}
}

// seedPlans fills the rig's store with the fixture plans, links, rows and
// findings. It returns the plan ids by fixture key.
func (f *srvFixture) seed(t *testing.T, st *planrecords.Store) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, p := range f.Plans {
		rec, err := st.Create(planrecords.Record{Kind: planrecords.KindPlan, Project: f.Project, Title: p.Title, Goal: p.Goal, Order: p.Order})
		if err != nil {
			t.Fatal(err)
		}
		ids[p.Key] = rec.ID
		for _, a := range p.Automata {
			if _, err := st.AttachAutomaton(rec.ID, a, f.automaton(a)); err != nil {
				t.Fatal(err)
			}
		}
		for _, id := range p.Automations {
			for _, au := range f.Automations {
				if au.ID == id {
					if _, err := st.AttachAutomation(rec.ID, id, planrecords.LinkInfo{Title: au.Title, State: au.State}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	for _, r := range f.Rows {
		rec := planrecords.Record{Kind: planrecords.KindRegistryRow, Project: f.Project, Title: r.Title, Body: r.Body,
			LegacyNumber: r.LegacyNumber, Links: r.Links, State: planrecords.State(r.State)}
		if _, err := st.Create(rec); err != nil {
			t.Fatal(err)
		}
	}
	for _, fd := range f.Findings {
		rec, err := st.Create(planrecords.Record{Kind: planrecords.KindFinding, Project: f.Project, Title: fd.Title, State: planrecords.State(fd.State)})
		if err != nil {
			t.Fatal(err)
		}
		switch fd.AttachKind {
		case planrecords.AttachPlan:
			_, err = st.AttachFinding(rec.ID, planrecords.Target{Kind: planrecords.AttachPlan, Ref: ids[fd.AttachRef]})
		case planrecords.AttachAutomaton:
			_, err = st.AttachFinding(rec.ID, planrecords.Target{Kind: planrecords.AttachAutomaton, Ref: fd.AttachRef})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func fxStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(map[string]any)["id"].(string))
	}
	return out
}

// fixtureRig is a write-capable rig whose store holds the fixture, but only
// plans, links, rows and findings (no migration has run).
func fixtureRig(t *testing.T) (*planRecordsRig, *http.ServeMux, *srvFixture, map[string]string) {
	t.Helper()
	f := loadSrvFixture(t)
	r, mux := writeRig(t)
	ids := f.seed(t, r.store)
	r.s.planItemSource = func(id string) (planrecords.MigrateRef, bool) {
		if v, ok := f.Resolve[id]; ok {
			return v, true
		}
		for _, a := range f.Automata {
			if a.ID == id {
				return planrecords.MigrateRef{Kind: "automaton", Title: a.Title, State: a.State}, true
			}
		}
		return planrecords.MigrateRef{}, false
	}
	return r, mux, f, ids
}

func TestFixturePlansListAndRunOrderOverREST(t *testing.T) {
	_, mux, f, ids := fixtureRig(t)
	code, body := plansGet(t, mux, "/api/projects/datawatch/plans")
	if code != 200 {
		t.Fatalf("plans: %d", code)
	}
	plans := body["plans"].([]any)
	if len(plans) != 2 || plans[0].(map[string]any)["title"] != "Plans browser" || plans[1].(map[string]any)["title"] != "Plans engine" {
		t.Fatalf("plans in priority order: %v", plans)
	}
	eng := plans[1].(map[string]any)
	prog := eng["progress"].(map[string]any)
	if prog["done"].(float64) != 3 || prog["total"].(float64) != 12 || prog["percent"].(float64) != 25 {
		t.Fatalf("engine progress = %v", prog)
	}
	back := body["backlog"].(map[string]any)["plans"].([]any)
	if len(back) != 2 || back[0].(map[string]any)["title"] != "Later idea" || back[1].(map[string]any)["title"] != "Another later idea" {
		t.Fatalf("backlog = %v", back)
	}
	for key, want := range f.RunOrder {
		code, body = plansGet(t, mux, "/api/plans/"+ids[key]+"/automata?limit=500")
		if code != 200 {
			t.Fatalf("automata %s: %d", key, code)
		}
		if got := fxStrings(body["automata"]); !reflect.DeepEqual(got, want) {
			t.Fatalf("plan %s run order = %v, want %v", key, got, want)
		}
		next, _ := body["next"].(map[string]any)
		got := ""
		if next != nil {
			got, _ = next["automaton_id"].(string)
		}
		if got != f.Next[key] {
			t.Fatalf("plan %s next = %q, want %q", key, got, f.Next[key])
		}
	}
	// Blocked and held rows are shown but skipped; the run class is spelled out.
	_, body = plansGet(t, mux, "/api/plans/"+ids["engine"]+"/automata?limit=500")
	runs := map[string]string{}
	skipped := map[string]bool{}
	for _, a := range body["automata"].([]any) {
		m := a.(map[string]any)
		runs[m["id"].(string)] = m["run"].(string)
		skipped[m["id"].(string)], _ = m["skipped"].(bool)
	}
	for id, run := range map[string]string{"aaaa0001": "completed", "aaaa0004": "running", "aaaa0007": "next",
		"aaaa0008": "blocked", "aaaa0009": "blocked", "aaaa000a": "held", "aaaa000c": "held"} {
		if runs[id] != run {
			t.Errorf("%s run = %q, want %q", id, runs[id], run)
		}
		if want := run == "blocked" || run == "held"; skipped[id] != want {
			t.Errorf("%s skipped = %v, want %v", id, skipped[id], want)
		}
	}
	// Paging: windows add up to the whole list and the next marker covers all of it.
	var paged []string
	for off := 0; off < 12; off += 5 {
		_, body = plansGet(t, mux, fmt.Sprintf("/api/plans/%s/automata?limit=5&offset=%d", ids["engine"], off))
		paged = append(paged, fxStrings(body["automata"])...)
		if body["page"].(map[string]any)["total"].(float64) != 12 || body["next"].(map[string]any)["automaton_id"] != "aaaa0007" {
			t.Fatalf("page %d: %v", off, body)
		}
	}
	if !reflect.DeepEqual(paged, f.RunOrder["engine"]) {
		t.Fatalf("pages = %v", paged)
	}
}

func TestFixtureOverviewSummaryPayload(t *testing.T) {
	r, mux, _, ids := fixtureRig(t)
	r.s.planPointsSource = func() map[string]float64 { return map[string]float64{"aaaa0001": 2, "aaaa0004": 3.5, "aaaa0102": 1} }
	code, body := plansGet(t, mux, "/api/projects/datawatch/overview-summary")
	if code != 200 {
		t.Fatalf("overview: %d", code)
	}
	if h := body["header"].(map[string]any); h["project"] != "datawatch" || h["generated_at"] == "" {
		t.Fatalf("header = %v", h)
	}
	prog := body["progress"].(map[string]any)
	au := prog["automata"].(map[string]any)
	if au["done"].(float64) != 4 || au["total"].(float64) != 14 || prog["points"].(float64) != 6.5 {
		t.Fatalf("progress = %v", prog)
	}
	lines := body["active_plans"].([]any)
	if len(lines) != 2 || lines[0].(map[string]any)["id"] != ids["browser"] || lines[1].(map[string]any)["id"] != ids["engine"] {
		t.Fatalf("active plan lines = %v", lines)
	}
	next := body["next"].(map[string]any)
	if next["plan_id"] != ids["engine"] || next["automaton_id"] != "aaaa0007" || next["automaton_title"] != "Search box" {
		t.Fatalf("next = %v (the plan without a waiting Automaton must be passed over)", next)
	}
	counts := body["counts"].(map[string]any)
	pl := counts["plans"].(map[string]any)
	byState := 0.0
	for _, n := range pl["by_state"].(map[string]any) {
		byState += n.(float64)
	}
	if pl["total"].(float64) != 4 || pl["backlog"].(float64) != 2 || byState != 4 {
		t.Fatalf("plan counts = %v", pl)
	}
	if rows := counts["rows"].(map[string]any); rows["total"].(float64) != 6 || rows["by_state"].(map[string]any)["shipped"].(float64) != 1 {
		t.Fatalf("row counts = %v", rows)
	}
	if fd := counts["findings"].(map[string]any); fd["total"].(float64) != 4 || fd["by_state"].(map[string]any)["triaged"].(float64) != 1 {
		t.Fatalf("finding counts = %v", fd)
	}
	if rec := counts["records"].(map[string]any); rec["total"].(float64) != 10 {
		t.Fatalf("record total = %v", rec)
	}
}

func TestFixtureReorderOverREST(t *testing.T) {
	_, mux, f, ids := fixtureRig(t)
	code, out := plansSend(t, mux, http.MethodPut, "/api/plans/order",
		map[string]any{"plans": []string{ids["later2"], ids["engine"]}})
	if code != 200 {
		t.Fatalf("reorder plans: %d %v", code, out)
	}
	rows := out["plans"].([]any)
	var titles []string
	for _, p := range rows {
		titles = append(titles, p.(map[string]any)["title"].(string))
	}
	if !reflect.DeepEqual(titles, []string{"Another later idea", "Plans engine", "Plans browser", "Later idea"}) {
		t.Fatalf("plan order = %v", titles)
	}
	_, body := plansGet(t, mux, "/api/projects/datawatch/plans")
	if first := body["plans"].([]any)[0].(map[string]any)["title"]; first != "Plans engine" {
		t.Fatalf("list after reorder starts with %v", first)
	}

	rev := append([]string{}, f.Plans[0].Automata...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	code, out = plansSend(t, mux, http.MethodPut, "/api/plans/"+ids["engine"]+"/automata/order", map[string]any{"automata": rev})
	if code != 200 {
		t.Fatalf("reorder Automata: %d %v", code, out)
	}
	// Reversed plan order: the groups stay, ties flip, and the next marker moves.
	want := []string{"aaaa0001", "aaaa0002", "aaaa0003", "aaaa0005", "aaaa0004", "aaaa0006", "aaaa0007",
		"aaaa0009", "aaaa0008", "aaaa000b", "aaaa000c", "aaaa000a"}
	if got := fxStrings(out["automata"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("run order after reversal = %v", got)
	}
	if out["next"].(map[string]any)["automaton_id"] != "aaaa0006" {
		t.Fatalf("next after reversal = %v", out["next"])
	}
	if code, _ = plansSend(t, mux, http.MethodPut, "/api/plans/"+ids["engine"]+"/automata/order", map[string]any{"automata": rev[1:]}); code != http.StatusBadRequest {
		t.Fatalf("partial list: %d", code)
	}
}

func TestFixtureLinkShownBothWaysOverREST(t *testing.T) {
	r, mux, _, ids := fixtureRig(t)
	ref, ok := r.store.PlanOf("aaaa0007")
	if !ok || ref.ID != ids["engine"] {
		t.Fatalf("Automaton shows %+v", ref)
	}
	code, out := plansSend(t, mux, http.MethodPost, "/api/plans/"+ids["browser"]+"/attach", map[string]any{"kind": "automaton", "id": "aaaa0007"})
	if code != http.StatusConflict {
		t.Fatalf("second plan for one Automaton: %d %v", code, out)
	}
	if ref, _ = r.store.PlanOf("aaaa0007"); ref.ID != ids["engine"] {
		t.Fatalf("link was overwritten: %+v", ref)
	}
	if code, out = plansSend(t, mux, http.MethodPost, "/api/plans/"+ids["engine"]+"/detach", map[string]any{"kind": "automaton", "id": "aaaa0007"}); code != 200 {
		t.Fatalf("detach: %d %v", code, out)
	}
	if _, ok = r.store.PlanOf("aaaa0007"); ok {
		t.Fatal("detached Automaton still shows a plan")
	}
}

func TestFixtureMigrationOverREST(t *testing.T) {
	f := loadSrvFixture(t)
	r := newPlanRecordsRig(t)
	var arcs []config.ProjectArc
	for _, a := range f.Arcs {
		arcs = append(arcs, config.ProjectArc{AutomationID: a.AutomationID, Priority: a.Priority, State: a.State})
	}
	r.s.cfg.Projects["datawatch"] = config.ProjectConfigEntry{Dir: r.dir, Arcs: arcs}
	r.s.planItemSource = func(id string) (planrecords.MigrateRef, bool) { v, ok := f.Resolve[id]; return v, ok }
	for _, row := range f.Rows {
		if _, err := r.store.Create(planrecords.Record{Kind: planrecords.KindRegistryRow, Project: f.Project, Title: row.Title,
			Body: row.Body, LegacyNumber: row.LegacyNumber, Links: row.Links, State: planrecords.State(row.State)}); err != nil {
			t.Fatal(err)
		}
	}
	start := r.store.Len()

	c := migrateCounts(t, migrateCall(t, r, http.MethodPost, nil))
	if r.store.Len() != start || c["would_create"] != 7 {
		t.Fatalf("dry run: counts %v, store %d -> %d", c, start, r.store.Len())
	}
	c = migrateCounts(t, migrateCall(t, r, http.MethodPost, map[string]any{"apply": true}))
	if c["would_create"] != 7 || r.store.Len() != start+7 {
		t.Fatalf("apply: counts %v, store %d -> %d", c, start, r.store.Len())
	}
	if p, ok := r.store.PlanOfAutomation("cccc3333"); !ok || p.State == "" {
		t.Fatalf("arc plan = %+v %v", p, ok)
	}
	if _, total := r.store.SearchTitles("ship the dashboard", planrecords.TitleFilter{Types: []string{planrecords.TypePlan}}); total != 1 {
		t.Fatalf("row plan not created: %d", total)
	}
	n := r.store.Len()
	c = migrateCounts(t, migrateCall(t, r, http.MethodPost, map[string]any{"apply": true}))
	if c["would_create"] != 0 || c["would_link"] != 0 || r.store.Len() != n {
		t.Fatalf("second apply changed things: %v, len %d -> %d", c, n, r.store.Len())
	}
}

func TestFixtureOldArcsRouteStillAnswers(t *testing.T) {
	f := loadSrvFixture(t)
	root := t.TempDir()
	s, _ := newAutomationRESTServer(t, t.TempDir())
	s.cfg.Projects = map[string]config.ProjectConfigEntry{f.Project: {Dir: root,
		Arcs: []config.ProjectArc{{AutomationID: "cccc3333", Priority: 1, State: "active"}}}}
	st, err := planrecords.Open(root, nil, planrecords.Options{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	f.seed(t, st)
	s.SetPlanRecords(st)
	code, body := plansGet(t, plansReadMux(s), "/api/projects/datawatch/arcs")
	if code != 200 {
		t.Fatalf("arcs route: %d %v", code, body)
	}
	if body["project"] != "datawatch" || body["deprecated"] == nil {
		t.Fatalf("arcs body = %v", body)
	}
	arcs, ok := body["arcs"].([]any)
	if !ok || len(arcs) != 1 {
		t.Fatalf("the old arcs list must stay in the answer: %v", body["arcs"])
	}
	plans, ok := body["plans"].([]any)
	if !ok || len(plans) != 4 {
		t.Fatalf("the alias must also carry the plans: %v", body["plans"])
	}
}

// TestFixtureLargeProjectPagesWithinBudget builds a large project from the
// fixture's Automata and checks the read routes page and answer inside the
// one-second budget.
func TestFixtureLargeProjectPagesWithinBudget(t *testing.T) {
	f := loadSrvFixture(t)
	const plansN, perPlan, rowsN, findingsN = 300, 20, 5000, 3000
	root := t.TempDir()
	dir := filepath.Join(root, "plan-records")
	for _, k := range []planrecords.Kind{planrecords.KindPlan, planrecords.KindRegistryRow, planrecords.KindFinding} {
		if err := os.MkdirAll(filepath.Join(dir, string(k)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const stamp = "2026-01-02T03:04:05Z"
	write := func(rec planrecords.Record) {
		b, _ := json.Marshal(rec)
		if err := os.WriteFile(filepath.Join(dir, string(rec.Kind), rec.UID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= plansN; i++ {
		rec := planrecords.Record{ID: fmt.Sprintf("plan-%04d", i), UID: fmt.Sprintf("uid-plan-%04d", i), Kind: planrecords.KindPlan,
			Project: f.Project, State: planrecords.StatePlanned, Title: fmt.Sprintf("Plan %04d", i), Order: i,
			CreatedAt: stamp, UpdatedAt: stamp, LinkInfo: map[string]planrecords.LinkInfo{}}
		for j := 0; j < perPlan; j++ {
			a := f.Automata[j%len(f.Automata)]
			id := fmt.Sprintf("a-%04d-%02d", i, j)
			rec.Automata = append(rec.Automata, id)
			rec.LinkInfo["automaton:"+id] = planrecords.LinkInfo{Title: a.Title + " " + id, State: a.State, CompletedAt: a.CompletedAt}
		}
		write(rec)
	}
	for i := 1; i <= rowsN; i++ {
		write(planrecords.Record{ID: fmt.Sprintf("row-%05d", i), UID: fmt.Sprintf("uid-row-%05d", i), Kind: planrecords.KindRegistryRow,
			Project: f.Project, State: planrecords.StateOpen, Title: fmt.Sprintf("Registry row %05d widget", i), LegacyNumber: i,
			CreatedAt: stamp, UpdatedAt: stamp})
	}
	for i := 1; i <= findingsN; i++ {
		write(planrecords.Record{ID: fmt.Sprintf("finding-%05d", i), UID: fmt.Sprintf("uid-finding-%05d", i), Kind: planrecords.KindFinding,
			Project: f.Project, State: planrecords.StateOpen, Title: fmt.Sprintf("Finding %05d", i), CreatedAt: stamp, UpdatedAt: stamp})
	}
	st, err := planrecords.Open(root, nil, planrecords.Options{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Projects = map[string]config.ProjectConfigEntry{f.Project: {Dir: root}}
	s := &Server{cfg: cfg}
	s.SetPlanRecords(st)
	s.planPointsSource = func() map[string]float64 { return map[string]float64{} }
	mux := plansReadMux(s)

	check := func(path string, verify func(map[string]any)) {
		t.Helper()
		start := time.Now()
		code, body := plansGet(t, mux, path)
		if d := time.Since(start); d > time.Second {
			t.Errorf("GET %s took %v, budget 1s", path, d)
		}
		if code != 200 {
			t.Errorf("GET %s: %d", path, code)
			return
		}
		verify(body)
	}
	check("/api/projects/datawatch/plans?limit=50&offset=100", func(b map[string]any) {
		pl := b["plans"].([]any)
		pg := b["page"].(map[string]any)
		if len(pl) != 50 || pg["total"].(float64) != plansN || pg["offset"].(float64) != 100 || pl[0].(map[string]any)["title"] != "Plan 0101" {
			t.Errorf("plans page = len %d %v", len(pl), pg)
		}
	})
	check("/api/projects/datawatch/plans?limit=50&offset=290", func(b map[string]any) {
		if n := len(b["plans"].([]any)); n != 10 {
			t.Errorf("last plans page holds %d, want 10", n)
		}
	})
	check("/api/projects/datawatch/overview-summary", func(b map[string]any) {
		c := b["counts"].(map[string]any)
		if c["plans"].(map[string]any)["total"].(float64) != plansN || c["rows"].(map[string]any)["total"].(float64) != rowsN ||
			c["findings"].(map[string]any)["total"].(float64) != findingsN {
			t.Errorf("counts = %v", c)
		}
	})
	check("/api/plans/plan-0150/automata?limit=7&offset=14", func(b map[string]any) {
		if len(b["automata"].([]any)) != 6 || b["page"].(map[string]any)["total"].(float64) != perPlan {
			t.Errorf("automata page = %v", b["page"])
		}
	})
	check("/api/plan-records/browse?q=widget&kind=registry_row&state=open&limit=500&offset=4900", func(b map[string]any) {
		if len(b["records"].([]any)) != 100 {
			t.Errorf("browse page keys: %v", b)
		}
	})
}

// ----- rename in all five locales -----------------------------------------

// plansRenameKeys are the person-facing labels the rename touched. Each must
// exist in every bundle, be non-empty, and be the plan word, not the old
// arc word, in the language of the bundle.
var plansRenameKeys = []string{
	"projects_page_arcs", "projects_page_plans", "projects_subtab_arcs", "projects_subtab_plans",
	"projects_page_loading_plans", "projects_event_no_plans", "projects_event_plan_empty",
}

var plansWord = map[string][]string{
	"en": {"plan"}, "de": {"plan", "pläne"}, "es": {"plan", "planes"}, "fr": {"plan"}, "ja": {"プラン"},
}

func TestFixturePlansRenameVisibleInAllFiveLocales(t *testing.T) {
	if len(requiredLocales) != 5 {
		t.Fatalf("expected five locales, got %v", requiredLocales)
	}
	arcWords := []string{"arc", "arcs", "bogen", "arco", "arcos", "arcs ", "アーク"}
	for _, lang := range requiredLocales {
		b := loadLocaleBundle(t, lang)
		for _, k := range plansRenameKeys {
			v, ok := b[k]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locale %s: key %s missing or empty", lang, k)
				continue
			}
			low := strings.ToLower(v)
			found := false
			for _, w := range plansWord[lang] {
				if strings.Contains(low, w) {
					found = true
				}
			}
			if !found {
				t.Errorf("locale %s: %s = %q does not use the plan word %v", lang, k, v, plansWord[lang])
			}
			for _, w := range arcWords {
				if lang == "fr" && w == "arcs " {
					continue
				}
				if strings.Contains(" "+low+" ", " "+strings.TrimSpace(w)+" ") {
					t.Errorf("locale %s: %s = %q still uses the old arc word %q", lang, k, v, w)
				}
			}
			if strings.Contains(v, "PRD") {
				t.Errorf("locale %s: %s = %q names a PRD", lang, k, v)
			}
		}
	}
	// The two labels that name the Automaton keep the shared translation.
	au := map[string]string{"en": "Automata", "de": "Automaten", "es": "autómatas", "fr": "automates", "ja": "オートマトン"}
	for _, lang := range requiredLocales {
		if v := loadLocaleBundle(t, lang)["projects_event_plan_empty"]; !strings.Contains(v, au[lang]) {
			t.Errorf("locale %s: projects_event_plan_empty = %q lacks %q", lang, v, au[lang])
		}
	}
}
