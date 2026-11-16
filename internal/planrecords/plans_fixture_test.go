package planrecords

// Shared plans fixture: testdata/plans_fixture.json holds plans (with a
// backlog), project arcs, registry rows that name Automata, findings, and an
// Automaton in each state. The loader here seeds a store from it; the tests
// below cover the migration, the run order, the link in both directions, the
// reorder calls, the title search, the overview counts and paging on a large
// project. The server, router, mcp and cli packages read the same JSON file.
// Nothing here sleeps or depends on load.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

type fxAutomaton struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	State       string `json:"state"`
	CompletedAt string `json:"completed_at"`
}

type fxPlan struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Goal        string   `json:"goal"`
	Order       int      `json:"order"`
	Automata    []string `json:"automata"`
	Automations []string `json:"automations"`
}

type fxRow struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	State        string `json:"state"`
	LegacyNumber int    `json:"legacy_number"`
	Body         string `json:"body"`
	Links        []Link `json:"links"`
}

type fxFinding struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	State      string `json:"state"`
	AttachKind string `json:"attach_kind"`
	AttachRef  string `json:"attach_ref"`
}

type plansFixture struct {
	Project     string                `json:"project"`
	Automata    []fxAutomaton         `json:"automata"`
	Automations []fxAutomaton         `json:"automations"`
	Plans       []fxPlan              `json:"plans"`
	RunOrder    map[string][]string   `json:"run_order"`
	Next        map[string]string     `json:"next"`
	Arcs        []MigrateArc          `json:"arcs"`
	Rows        []fxRow               `json:"rows"`
	Resolve     map[string]MigrateRef `json:"resolve"`
	Findings    []fxFinding           `json:"findings"`
	Expected    struct {
		BacklogTitles []string `json:"backlog_titles_by_row"`
		BacklogOrders []int    `json:"backlog_orders"`
	} `json:"expected"`
}

func loadPlansFixture(t *testing.T) *plansFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "plans_fixture.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f plansFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return &f
}

func (f *plansFixture) automaton(id string) fxAutomaton {
	for _, a := range f.Automata {
		if a.ID == id {
			return a
		}
	}
	return fxAutomaton{ID: id}
}

func (f *plansFixture) info(id string) LinkInfo {
	a := f.automaton(id)
	return LinkInfo{Title: a.Title, State: a.State, CompletedAt: a.CompletedAt}
}

// seedPlans creates the fixture plans and links their Automata and
// Automations in the plan order the fixture lists. It returns the plans by key.
func (f *plansFixture) seedPlans(t *testing.T, s *Store) map[string]Record {
	t.Helper()
	out := map[string]Record{}
	for _, p := range f.Plans {
		rec := mustCreate(t, s, Record{Kind: KindPlan, Project: f.Project, Title: p.Title, Goal: p.Goal, Order: p.Order})
		for _, id := range p.Automata {
			if _, err := s.AttachAutomaton(rec.ID, id, f.info(id)); err != nil {
				t.Fatalf("attach %s: %v", id, err)
			}
		}
		for _, id := range p.Automations {
			var info LinkInfo
			for _, a := range f.Automations {
				if a.ID == id {
					info = LinkInfo{Title: a.Title, State: a.State}
				}
			}
			if _, err := s.AttachAutomation(rec.ID, id, info); err != nil {
				t.Fatalf("attach automation %s: %v", id, err)
			}
		}
		out[p.Key] = rec
	}
	return out
}

func (f *plansFixture) seedRows(t *testing.T, s *Store) {
	t.Helper()
	for _, r := range f.Rows {
		rec := Record{Kind: KindRegistryRow, Project: f.Project, Title: r.Title, Body: r.Body,
			LegacyNumber: r.LegacyNumber, Links: r.Links}
		if r.State != "" {
			rec.State = State(r.State)
		}
		mustCreate(t, s, rec)
	}
}

// seedFindings creates the findings and attaches the ones that name a plan key
// or an Automaton id.
func (f *plansFixture) seedFindings(t *testing.T, s *Store, plans map[string]Record) {
	t.Helper()
	for _, fd := range f.Findings {
		rec := mustCreate(t, s, Record{Kind: KindFinding, Project: f.Project, Title: fd.Title, State: State(fd.State)})
		switch fd.AttachKind {
		case AttachPlan:
			if _, err := s.AttachFinding(rec.ID, Target{Kind: AttachPlan, Ref: plans[fd.AttachRef].ID}); err != nil {
				t.Fatal(err)
			}
		case AttachAutomaton:
			if _, err := s.AttachFinding(rec.ID, Target{Kind: AttachAutomaton, Ref: fd.AttachRef}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// fullStore is a store holding every fixture record kind.
func (f *plansFixture) fullStore(t *testing.T) (*Store, map[string]Record) {
	t.Helper()
	s, _, _, _ := newTestStore(t, nil)
	plans := f.seedPlans(t, s)
	f.seedRows(t, s)
	f.seedFindings(t, s, plans)
	return s, plans
}

// migrationStore holds only the registry rows; the migration builds the plans.
func (f *plansFixture) migrationStore(t *testing.T) (*Store, string, MigrateOptions) {
	t.Helper()
	s, dir, _, _ := newTestStore(t, nil)
	f.seedRows(t, s)
	return s, dir, MigrateOptions{
		Store: s,
		Arcs:  f.Arcs,
		Resolve: func(id string) (MigrateRef, bool) {
			r, ok := f.Resolve[id]
			return r, ok
		},
	}
}

func ids(as []PlanAutomaton) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ID
	}
	return out
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func snapshotSummaries(s *Store) string {
	all, _ := s.ListSummaries(SummaryFilter{Limit: MaxLimit})
	sort.Slice(all, func(i, j int) bool { return all[i].UID < all[j].UID })
	b, _ := json.Marshal(all)
	return string(b)
}

// ----- migration -----------------------------------------------------------

func TestFixtureMigrationDryRunWritesNothing(t *testing.T) {
	f := loadPlansFixture(t)
	s, dir, opts := f.migrationStore(t)
	files, before := countFiles(t, dir), snapshotSummaries(s)
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Applied {
		t.Fatal("a run without Apply must be a dry run")
	}
	if countFiles(t, dir) != files || snapshotSummaries(s) != before {
		t.Fatal("dry run changed the store")
	}
	// 2 arcs, 2 rows naming an id, 3 backlog rows; the shipped row is left alone.
	if len(rep.WouldCreate) != 7 || len(rep.WouldLink) != 4 || len(rep.SkippedExisting) != 1 || len(rep.Conflicts) != 0 {
		t.Fatalf("dry run report = %v", rep.Counts())
	}
}

func TestFixtureMigrationApplyCreatesPlansAndLinks(t *testing.T) {
	f := loadPlansFixture(t)
	s, _, opts := f.migrationStore(t)
	opts.Apply = true
	rep, err := Migrate(opts)
	if err != nil || len(rep.Conflicts) != 0 {
		t.Fatalf("apply: %v %v", err, rep.Counts())
	}
	for _, a := range f.Arcs {
		p, ok := s.PlanOfAutomation(a.AutomationID)
		if !ok || p.Title != a.Title && a.Title != "" {
			t.Fatalf("arc %s plan = %+v %v", a.AutomationID, p, ok)
		}
	}
	if p, ok := s.PlanOf("eeee0001"); !ok || p.Title != "Plan: Ship the dashboard" {
		t.Fatalf("row naming an Automaton: %+v %v", p, ok)
	}
	if p, ok := s.PlanOfAutomation("eeee0002"); !ok || p.Title != "Plan: Faster search" {
		t.Fatalf("row linking an Automation: %+v %v", p, ok)
	}
	var titles []string
	var orders []int
	for _, b := range s.Backlog() {
		if s.rowPlanIs(b.UID) {
			titles = append(titles, b.Title)
			orders = append(orders, b.Order)
		}
	}
	if !reflect.DeepEqual(titles, f.Expected.BacklogTitles) || !reflect.DeepEqual(orders, f.Expected.BacklogOrders) {
		t.Fatalf("backlog = %v %v, want %v %v", titles, orders, f.Expected.BacklogTitles, f.Expected.BacklogOrders)
	}
}

// rowPlanIs reports whether the plan with this uid was made from a backlog row.
func (s *Store) rowPlanIs(uid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.li.rowPlan {
		if p == uid {
			return true
		}
	}
	return false
}

func TestFixtureMigrationSecondApplyIsNoOp(t *testing.T) {
	f := loadPlansFixture(t)
	s, dir, opts := f.migrationStore(t)
	opts.Apply = true
	if _, err := Migrate(opts); err != nil {
		t.Fatal(err)
	}
	files, before := countFiles(t, dir), snapshotSummaries(s)
	again, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.WouldCreate) != 0 || len(again.WouldLink) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("second apply report = %v", again.Counts())
	}
	if countFiles(t, dir) != files || snapshotSummaries(s) != before {
		t.Fatal("second apply changed the store (a record, an order or a timestamp moved)")
	}
}

func TestFixtureMigrationNeverOverwritesExistingLinks(t *testing.T) {
	f := loadPlansFixture(t)
	s, _, opts := f.migrationStore(t)
	mine := mustCreate(t, s, Record{Kind: KindPlan, Project: f.Project, Title: "Hand made"})
	if _, err := s.AttachAutomation(mine.ID, "cccc3333", LinkInfo{Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachAutomaton(mine.ID, "eeee0001", LinkInfo{Title: "Mine too"}); err != nil {
		t.Fatal(err)
	}
	opts.Apply = true
	if _, err := Migrate(opts); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ kind, id string }{{"automation", "cccc3333"}, {"automaton", "eeee0001"}} {
		var p PlanRef
		var ok bool
		if c.kind == "automation" {
			p, ok = s.PlanOfAutomation(c.id)
		} else {
			p, ok = s.PlanOf(c.id)
		}
		if !ok || p.ID != mine.ID {
			t.Fatalf("%s %s moved from the hand-made plan to %+v", c.kind, c.id, p)
		}
	}
	got, err := s.Get(mine.ID)
	if err != nil || got.LinkInfo["automaton:eeee0001"].Title != "Mine too" {
		t.Fatalf("existing link info rewritten: %+v %v", got.LinkInfo, err)
	}
}

// ----- run order -----------------------------------------------------------

func TestFixtureRunOrderAndNextMarker(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	for key, want := range f.RunOrder {
		d, page, total, err := s.PlanAutomataPage(plans[key].ID, Page{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if total != len(want) || !reflect.DeepEqual(ids(page), want) {
			t.Fatalf("plan %s run order = %v, want %v", key, ids(page), want)
		}
		got := ""
		if d.Next != nil {
			got = d.Next.ID
		}
		if got != f.Next[key] {
			t.Fatalf("plan %s next = %q, want %q", key, got, f.Next[key])
		}
	}
}

func TestFixtureRunClassOfEveryState(t *testing.T) {
	f := loadPlansFixture(t)
	want := map[string]string{
		"completed": RunCompleted, "archived": RunCompleted, "running": RunRunning, "planning": RunRunning,
		"approved": RunNext, "draft": RunNext, "blocked": RunBlocked, "failed": RunBlocked,
		"paused": RunHeld, "cancelled": RunHeld, "rejected": RunHeld,
	}
	seen := map[string]bool{}
	for _, a := range f.Automata {
		seen[a.State] = true
		if got := RunClass(a.State); got != want[a.State] {
			t.Errorf("RunClass(%q) = %q, want %q", a.State, got, want[a.State])
		}
	}
	for st := range want {
		if !seen[st] {
			t.Errorf("fixture has no Automaton in state %q", st)
		}
	}
}

func TestFixtureNextSkipsBlockedAndHeldAndFindsNone(t *testing.T) {
	f := loadPlansFixture(t)
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, Record{Kind: KindPlan, Project: f.Project, Title: "Only stuck work"})
	for _, id := range []string{"aaaa0008", "aaaa0009", "aaaa000a", "aaaa000b"} {
		if _, err := s.AttachAutomaton(p.ID, id, f.info(id)); err != nil {
			t.Fatal(err)
		}
	}
	d, page, _, err := s.PlanAutomataPage(p.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Next != nil {
		t.Fatalf("next = %+v, want none when everything is blocked or held", d.Next)
	}
	if got := ids(page); !reflect.DeepEqual(got, []string{"aaaa0008", "aaaa0009", "aaaa000a", "aaaa000b"}) {
		t.Fatalf("blocked then held, plan order inside each group: %v", got)
	}
}

func TestFixtureCompletedUnknownTimeSortsAfterKnown(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	_, page, _, err := s.PlanAutomataPage(plans["engine"].ID, Page{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page); !reflect.DeepEqual(got, []string{"aaaa0001", "aaaa0002", "aaaa0003"}) {
		t.Fatalf("completed by completion time, unknown last: %v", got)
	}
}

// ----- backlog -------------------------------------------------------------

func TestFixtureBacklogGroupFollowsPlanOrder(t *testing.T) {
	f := loadPlansFixture(t)
	s, _ := f.fullStore(t)
	v := s.ProjectPlans(f.Project, Page{Limit: 500}, Page{Limit: 500})
	if v.PlansTotal != 2 || v.BacklogTotal != 2 {
		t.Fatalf("plans %d backlog %d", v.PlansTotal, v.BacklogTotal)
	}
	if v.Plans[0].Summary.Title != "Plans browser" || v.Plans[1].Summary.Title != "Plans engine" {
		t.Fatalf("plans in priority order: %s, %s", v.Plans[0].Summary.Title, v.Plans[1].Summary.Title)
	}
	if v.Backlog[0].Summary.Title != "Later idea" || v.Backlog[1].Summary.Title != "Another later idea" {
		t.Fatalf("backlog order: %s, %s", v.Backlog[0].Summary.Title, v.Backlog[1].Summary.Title)
	}
}

// ----- links both ways -----------------------------------------------------

func TestFixtureLinkBothWays(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	for _, p := range f.Plans {
		links, err := s.AutomataOf(plans[p.Key].ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != len(p.Automata) {
			t.Fatalf("plan %s lists %d Automata, want %d", p.Key, len(links), len(p.Automata))
		}
		for _, id := range p.Automata {
			ref, ok := s.PlanOf(id)
			if !ok || ref.ID != plans[p.Key].ID {
				t.Fatalf("Automaton %s shows plan %+v, want %s", id, ref, p.Key)
			}
		}
		for _, id := range p.Automations {
			if ref, ok := s.PlanOfAutomation(id); !ok || ref.ID != plans[p.Key].ID {
				t.Fatalf("Automation %s shows plan %+v", id, ref)
			}
		}
	}
	if _, ok := s.PlanOf("eeee0001"); ok {
		t.Fatal("an Automaton in no plan must show no plan")
	}
	// A second attach to another plan is refused and the first link stays.
	if _, err := s.AttachAutomaton(plans["browser"].ID, "aaaa0007", LinkInfo{}); err == nil {
		t.Fatal("attach to a second plan must be refused")
	}
	if ref, _ := s.PlanOf("aaaa0007"); ref.ID != plans["engine"].ID {
		t.Fatalf("link moved to %+v", ref)
	}
	// Detach frees it both ways.
	if _, err := s.DetachAutomaton("aaaa0007"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.PlanOf("aaaa0007"); ok {
		t.Fatal("detached Automaton still shows a plan")
	}
	links, _ := s.AutomataOf(plans["engine"].ID)
	for _, l := range links {
		if l.ID == "aaaa0007" {
			t.Fatal("detached Automaton still listed by its plan")
		}
	}
}

func TestFixtureFindingsShowTheirPlan(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	rows, total := s.Browse(BrowseFilter{Kind: KindFinding, Page: Page{Limit: 50}})
	if total != len(f.Findings) {
		t.Fatalf("findings = %d, want %d", total, len(f.Findings))
	}
	byTitle := map[string]BrowseRow{}
	for _, r := range rows {
		byTitle[r.Summary.Title] = r
	}
	if r := byTitle["Order lost after restart"]; r.PlanID != plans["engine"].ID {
		t.Fatalf("plan finding shows plan %q", r.PlanID)
	}
	if r := byTitle["Search misses short titles"]; r.PlanID != plans["engine"].ID || len(r.Automata) != 1 || r.Automata[0] != "aaaa0007" {
		t.Fatalf("Automaton finding shows %+v", r)
	}
	if r := byTitle["Unattached finding about search"]; r.PlanID != "" {
		t.Fatalf("unattached finding shows plan %q", r.PlanID)
	}
}

// ----- reorder -------------------------------------------------------------

func TestFixtureReorderPlansAndAutomata(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	order := []string{plans["later2"].ID, plans["engine"].ID}
	if err := s.ReorderPlans(order); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range s.Plans("") {
		got = append(got, p.Title)
	}
	want := []string{"Another later idea", "Plans engine", "Plans browser", "Later idea"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan order = %v, want %v", got, want)
	}
	if err := s.ReorderPlans([]string{plans["engine"].ID, plans["engine"].ID}); err == nil {
		t.Fatal("a plan listed twice must fail")
	}
	if err := s.ReorderPlans([]string{"no-such-plan"}); err == nil {
		t.Fatal("an unknown plan must fail")
	}
	after := snapshotSummaries(s)
	_ = s.ReorderPlans([]string{"no-such-plan"})
	if snapshotSummaries(s) != after {
		t.Fatal("a failed reorder changed something")
	}

	// Automata: reversing the plan order moves the ties, not the classes.
	eng := f.Plans[0]
	rev := make([]string, len(eng.Automata))
	for i, id := range eng.Automata {
		rev[len(rev)-1-i] = id
	}
	if _, err := s.ReorderAutomata(plans["engine"].ID, rev); err != nil {
		t.Fatal(err)
	}
	d, page, _, _ := s.PlanAutomataPage(plans["engine"].ID, Page{Limit: 500})
	wantRun := []string{"aaaa0001", "aaaa0002", "aaaa0003", "aaaa0005", "aaaa0004", "aaaa0006", "aaaa0007",
		"aaaa0009", "aaaa0008", "aaaa000b", "aaaa000c", "aaaa000a"}
	if !reflect.DeepEqual(ids(page), wantRun) {
		t.Fatalf("run order after reversal = %v, want %v", ids(page), wantRun)
	}
	if d.Next == nil || d.Next.ID != "aaaa0006" {
		t.Fatalf("next after reversal = %+v, want aaaa0006", d.Next)
	}
	if _, err := s.ReorderAutomata(plans["engine"].ID, rev[1:]); err == nil {
		t.Fatal("a partial Automata list must fail")
	}
	if _, err := s.ReorderAutomata(plans["engine"].ID, append([]string{"ffff0000"}, rev[1:]...)); err == nil {
		t.Fatal("an Automaton of another plan must fail")
	}
}

func TestFixtureReorderSurvivesReopen(t *testing.T) {
	f := loadPlansFixture(t)
	s, dir, _, _ := newTestStore(t, nil)
	plans := f.seedPlans(t, s)
	if err := s.ReorderPlans([]string{plans["later"].ID}); err != nil {
		t.Fatal(err)
	}
	re, err := Open(dir, nil, Options{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	if got := re.Plans("")[0].Title; got != "Later idea" {
		t.Fatalf("first plan after reopen = %q", got)
	}
	if snapshotSummaries(re) != snapshotSummaries(s) {
		t.Fatal("reopened store differs")
	}
}

// ----- search --------------------------------------------------------------

func TestFixtureSearchOverTheFourTitleKinds(t *testing.T) {
	f := loadPlansFixture(t)
	s, _ := f.fullStore(t)
	// "search" appears in a plan-linked Automation, an Automaton, a registry
	// row and a finding title; the plan titles carry no such word.
	hits, total := s.SearchTitles("search", TitleFilter{})
	kinds := map[string]int{}
	for _, h := range hits {
		kinds[h.Type]++
	}
	if kinds[TypeAutomation] != 1 || kinds[TypeAutomaton] != 1 || kinds[TypeRegistryRow] != 1 || kinds[TypeFinding] != 2 || total != 5 {
		t.Fatalf("search kinds = %v total %d (%+v)", kinds, total, hits)
	}
	plansHit, _ := s.SearchTitles("plans", TitleFilter{Types: []string{TypePlan}})
	if len(plansHit) != 2 || plansHit[0].Title != "Plans browser" {
		t.Fatalf("plan hits = %+v", plansHit)
	}
	// State filter.
	hits, total = s.SearchTitles("search", TitleFilter{State: "open"})
	for _, h := range hits {
		if h.State != "open" {
			t.Fatalf("state filter leaked %+v", h)
		}
	}
	if total != 2 { // the registry row and the unattached finding are open
		t.Fatalf("open hits = %d (%+v)", total, hits)
	}
	hits, total = s.SearchTitles("", TitleFilter{Types: []string{TypeAutomaton}, State: "draft"})
	if total != 1 || hits[0].Ref != "aaaa0007" || hits[0].PlanRef == "" {
		t.Fatalf("draft Automata = %+v", hits)
	}
	if _, total = s.SearchTitles("search", TitleFilter{Limit: 1}); total != 5 {
		t.Fatal("total must be counted before paging")
	}
	if _, total = s.SearchTitles("no such words anywhere", TitleFilter{}); total != 0 {
		t.Fatalf("unrelated query matched %d", total)
	}
}

// ----- overview ------------------------------------------------------------

func TestFixtureOverviewCounts(t *testing.T) {
	f := loadPlansFixture(t)
	s, _ := f.fullStore(t)
	ov := s.Overview(f.Project, 0)
	if ov.PlansTotal != 4 || ov.Backlog != 2 || ov.Total != 14 || ov.Done != 4 {
		t.Fatalf("plans %d backlog %d automata %d done %d", ov.PlansTotal, ov.Backlog, ov.Total, ov.Done)
	}
	if ov.RowsTotal != 6 || ov.Rows[StateOpen] != 5 || ov.Rows[StateShipped] != 1 {
		t.Fatalf("rows = %d %v", ov.RowsTotal, ov.Rows)
	}
	if ov.FindingsTotal != 4 || ov.Findings[StateOpen] != 2 || ov.Findings[StateTriaged] != 1 || ov.Findings[StateFixed] != 1 {
		t.Fatalf("findings = %d %v", ov.FindingsTotal, ov.Findings)
	}
	if len(ov.Active) != 2 || ov.Active[0].Summary.Title != "Plans browser" {
		t.Fatalf("active plans = %+v", ov.Active)
	}
	if one := s.Overview(f.Project, 1); len(one.Active) != 1 {
		t.Fatalf("maxActive 1 gave %d lines", len(one.Active))
	}
	if other := s.Overview("elsewhere", 0); other.PlansTotal != 0 || other.RowsTotal != 0 {
		t.Fatalf("another project shows %+v", other)
	}
}

// ----- large project: read paths page and stay inside the budget ----------

// writeLargeStore writes plan, row and finding files straight into a store
// directory (as a long-lived project would have them) and opens it.
func writeLargeStore(t *testing.T, f *plansFixture, plans, perPlan, rows, findings int) *Store {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "plan-records")
	for _, k := range []Kind{KindPlan, KindRegistryRow, KindFinding} {
		if err := os.MkdirAll(filepath.Join(dir, string(k)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const stamp = "2026-01-02T03:04:05Z"
	write := func(r Record) {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, string(r.Kind), r.UID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= plans; i++ {
		rec := Record{ID: fmt.Sprintf("plan-%04d", i), UID: fmt.Sprintf("uid-plan-%04d", i), Kind: KindPlan,
			Project: f.Project, State: StatePlanned, Title: fmt.Sprintf("Plan %04d", i), Order: i,
			CreatedAt: stamp, UpdatedAt: stamp, LinkInfo: map[string]LinkInfo{}}
		for j := 0; j < perPlan; j++ {
			a := f.Automata[j%len(f.Automata)]
			id := fmt.Sprintf("a-%04d-%02d", i, j)
			rec.Automata = append(rec.Automata, id)
			rec.LinkInfo["automaton:"+id] = LinkInfo{Title: a.Title + " " + id, State: a.State, CompletedAt: a.CompletedAt}
		}
		write(rec)
	}
	for i := 1; i <= rows; i++ {
		write(Record{ID: fmt.Sprintf("row-%05d", i), UID: fmt.Sprintf("uid-row-%05d", i), Kind: KindRegistryRow,
			Project: f.Project, State: StateOpen, Title: fmt.Sprintf("Registry row %05d widget", i),
			LegacyNumber: i, CreatedAt: stamp, UpdatedAt: stamp})
	}
	for i := 1; i <= findings; i++ {
		write(Record{ID: fmt.Sprintf("finding-%05d", i), UID: fmt.Sprintf("uid-finding-%05d", i), Kind: KindFinding,
			Project: f.Project, State: StateOpen, Title: fmt.Sprintf("Finding %05d", i), CreatedAt: stamp, UpdatedAt: stamp})
	}
	s, err := Open(root, nil, Options{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.Skipped()); n != 0 {
		t.Fatalf("%d fixture files skipped", n)
	}
	return s
}

func TestFixtureLargeProjectReadsPageAndStayUnderOneSecond(t *testing.T) {
	f := loadPlansFixture(t)
	const plansN, perPlan, rowsN, findingsN = 300, 20, 5000, 3000
	s := writeLargeStore(t, f, plansN, perPlan, rowsN, findingsN)
	const budget = time.Second
	within := func(name string, fn func()) {
		t.Helper()
		start := time.Now()
		fn()
		if d := time.Since(start); d > budget {
			t.Errorf("%s took %v, budget %v", name, d, budget)
		}
	}
	within("project plans page", func() {
		v := s.ProjectPlans(f.Project, Page{Limit: 50, Offset: 100}, Page{Limit: 50})
		if v.PlansTotal != plansN || len(v.Plans) != 50 || v.Plans[0].Summary.Order != 101 {
			t.Errorf("plans page: total %d len %d", v.PlansTotal, len(v.Plans))
		}
		if v.BacklogTotal != 0 || len(v.Backlog) != 0 {
			t.Errorf("backlog = %d", v.BacklogTotal)
		}
	})
	within("last plans page", func() {
		v := s.ProjectPlans(f.Project, Page{Limit: 50, Offset: 280}, Page{})
		if len(v.Plans) != 20 {
			t.Errorf("last page holds %d, want 20", len(v.Plans))
		}
	})
	within("plan Automata page", func() {
		_, page, total, err := s.PlanAutomataPage("plan-0150", Page{Limit: 7, Offset: 14})
		if err != nil || total != perPlan || len(page) != 6 {
			t.Errorf("Automata page: %v total %d len %d", err, total, len(page))
		}
	})
	within("overview", func() {
		ov := s.Overview(f.Project, 10)
		if ov.PlansTotal != plansN || ov.Total != plansN*perPlan || ov.RowsTotal != rowsN || ov.FindingsTotal != findingsN || len(ov.Active) != 10 {
			t.Errorf("overview = %d plans %d automata %d rows %d findings %d active",
				ov.PlansTotal, ov.Total, ov.RowsTotal, ov.FindingsTotal, len(ov.Active))
		}
	})
	within("browse", func() {
		rows, total := s.Browse(BrowseFilter{Query: "widget", Kind: KindRegistryRow, State: StateOpen, Page: Page{Limit: 500}})
		if total != rowsN || len(rows) != 500 {
			t.Errorf("browse: total %d len %d", total, len(rows))
		}
		rows, total = s.Browse(BrowseFilter{Query: "finding 00042"})
		if total != 1 || len(rows) != 1 {
			t.Errorf("browse one finding: total %d", total)
		}
	})
	within("title search", func() {
		hits, total := s.SearchTitles("widget", TitleFilter{Limit: 100, Offset: 4950})
		if total != rowsN || len(hits) != 50 {
			t.Errorf("search: total %d len %d", total, len(hits))
		}
	})
}
