package planrecords

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func plan(title string) Record {
	return Record{Kind: KindPlan, Project: "datawatch", Title: title}
}

func finding(title string) Record {
	return Record{Kind: KindFinding, Project: "datawatch", Title: title}
}

func TestPlanCreateAssignsOrderAndRejectsLinks(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("First plan"))
	b := mustCreate(t, s, plan("Second plan"))
	if a.Order != 1 || b.Order != 2 {
		t.Fatalf("orders = %d, %d, want 1, 2", a.Order, b.Order)
	}
	in := plan("Linked at birth")
	in.Automata = []string{"x1"}
	if _, err := s.Create(in); err == nil {
		t.Fatal("create with links must be refused")
	}
	bad := row("Not a plan")
	bad.Goal = "goal"
	if _, err := s.Create(bad); err == nil {
		t.Fatal("goal on a registry row must be refused")
	}
}

func TestAttachAutomatonAndPlanOf(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p1 := mustCreate(t, s, plan("Alpha"))
	p2 := mustCreate(t, s, plan("Beta"))
	got, err := s.AttachAutomaton(p1.ID, "aut-1", LinkInfo{Title: "Build the thing", State: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Automata) != 1 || got.Automata[0] != "aut-1" {
		t.Fatalf("automata = %v", got.Automata)
	}
	ref, ok := s.PlanOf("aut-1")
	if !ok || ref.ID != p1.ID || ref.Title != "Alpha" {
		t.Fatalf("PlanOf = %+v %v", ref, ok)
	}
	// Same plan again is idempotent and refreshes info.
	if _, err := s.AttachAutomaton(p1.ID, "aut-1", LinkInfo{Title: "Build the thing", State: "done"}); err != nil {
		t.Fatal(err)
	}
	links, _ := s.AutomataOf(p1.ID)
	if len(links) != 1 || links[0].State != "done" {
		t.Fatalf("links = %+v", links)
	}
	// A different plan is a typed conflict and changes nothing.
	_, err = s.AttachAutomaton(p2.ID, "aut-1", LinkInfo{})
	var lc *ErrLinkConflict
	if !errors.As(err, &lc) || lc.Holder != p1.ID || lc.Kind != "automaton" {
		t.Fatalf("err = %v", err)
	}
	if ref, _ := s.PlanOf("aut-1"); ref.ID != p1.ID {
		t.Fatal("link was overwritten")
	}
	p2r, _ := s.Get(p2.ID)
	if len(p2r.Automata) != 0 {
		t.Fatal("conflicting attach altered the other plan")
	}
	// Attaching to a non-plan is invalid.
	r := mustCreate(t, s, row("A row"))
	if _, err := s.AttachAutomaton(r.ID, "aut-2", LinkInfo{}); err == nil {
		t.Fatal("attach to a registry row must fail")
	}
}

func TestDetachFreesTheLink(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p1 := mustCreate(t, s, plan("Alpha"))
	p2 := mustCreate(t, s, plan("Beta"))
	mustAttach := func(ref, id string) {
		if _, err := s.AttachAutomaton(ref, id, LinkInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	mustAttach(p1.ID, "a")
	if _, err := s.DetachAutomaton("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.PlanOf("a"); ok {
		t.Fatal("still linked after detach")
	}
	mustAttach(p2.ID, "a")
	if ref, _ := s.PlanOf("a"); ref.ID != p2.ID {
		t.Fatal("not moved to second plan")
	}
	if _, err := s.DetachAutomaton("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAutomationLinks(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p1 := mustCreate(t, s, plan("Alpha"))
	p2 := mustCreate(t, s, plan("Beta"))
	if _, err := s.AttachAutomation(p1.ID, "auto-1", LinkInfo{Title: "Nightly"}); err != nil {
		t.Fatal(err)
	}
	if ref, ok := s.PlanOfAutomation("auto-1"); !ok || ref.ID != p1.ID {
		t.Fatal("automation reverse lookup failed")
	}
	var lc *ErrLinkConflict
	if _, err := s.AttachAutomation(p2.ID, "auto-1", LinkInfo{}); !errors.As(err, &lc) || lc.Kind != "automation" {
		t.Fatalf("err = %v", err)
	}
	// An Automaton and an Automation with the same id do not collide.
	if _, err := s.AttachAutomaton(p2.ID, "auto-1", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DetachAutomation("auto-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.PlanOfAutomation("auto-1"); ok {
		t.Fatal("automation still linked")
	}
	if ref, ok := s.PlanOf("auto-1"); !ok || ref.ID != p2.ID {
		t.Fatal("detaching the automation touched the Automaton link")
	}
}

func TestFindingAttach(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p1 := mustCreate(t, s, plan("Alpha"))
	p2 := mustCreate(t, s, plan("Beta"))
	f := mustCreate(t, s, finding("Crash on start"))
	got, err := s.AttachFinding(f.ID, Target{Kind: AttachPlan, Ref: p1.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.AttachKind != AttachPlan || got.AttachRef != p1.ID {
		t.Fatalf("finding = %+v", got)
	}
	pr, _ := s.Get(p1.ID)
	if len(pr.Findings) != 1 || pr.Findings[0] != f.ID {
		t.Fatalf("plan findings = %v", pr.Findings)
	}
	var lc *ErrLinkConflict
	if _, err := s.AttachFinding(f.ID, Target{Kind: AttachPlan, Ref: p2.ID}); !errors.As(err, &lc) || lc.Kind != "finding" {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.AttachFinding(f.ID, Target{Kind: AttachAutomaton, Ref: "aut-9"}); !errors.As(err, &lc) {
		t.Fatalf("err = %v", err)
	}
	// Same target again is a no-op.
	if _, err := s.AttachFinding(f.ID, Target{Kind: AttachPlan, Ref: p1.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DetachFinding(f.ID); err != nil {
		t.Fatal(err)
	}
	pr, _ = s.Get(p1.ID)
	if len(pr.Findings) != 0 {
		t.Fatalf("plan findings after detach = %v", pr.Findings)
	}
	g, err := s.AttachFinding(f.ID, Target{Kind: AttachAutomaton, Ref: "aut-9"})
	if err != nil || g.AttachKind != AttachAutomaton || g.AttachRef != "aut-9" {
		t.Fatalf("automaton attach: %+v %v", g, err)
	}
	// A registry row is not a finding.
	r := mustCreate(t, s, row("Row"))
	if _, err := s.AttachFinding(r.ID, Target{Kind: AttachPlan, Ref: p1.ID}); err == nil {
		t.Fatal("attaching a row as a finding must fail")
	}
	if _, err := s.AttachFinding(f.ID, Target{Kind: "x", Ref: "y"}); err == nil {
		t.Fatal("bad target kind must fail")
	}
}

func planIDs(ss []Summary) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}

func TestReorderPlans(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("Aaa"))
	b := mustCreate(t, s, plan("Bbb"))
	c := mustCreate(t, s, plan("Ccc"))
	d := mustCreate(t, s, plan("Ddd"))
	if err := s.ReorderPlans([]string{d.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	got := planIDs(s.Plans(""))
	want := []string{d.ID, b.ID, a.ID, c.ID}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if err := s.ReorderPlans([]string{a.ID, a.ID}); err == nil {
		t.Fatal("duplicate ref must fail")
	}
	r := mustCreate(t, s, row("Row"))
	before := planIDs(s.Plans(""))
	if err := s.ReorderPlans([]string{a.ID, r.ID}); err == nil {
		t.Fatal("non-plan ref must fail")
	}
	after := planIDs(s.Plans(""))
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("failed reorder changed the order")
		}
	}
}

func TestReorderAutomata(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.AttachAutomaton(p.ID, id, LinkInfo{Title: "T " + id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReorderAutomata(p.ID, []string{"c", "a", "b"}); err != nil {
		t.Fatal(err)
	}
	links, _ := s.AutomataOf(p.ID)
	if links[0].ID != "c" || links[1].ID != "a" || links[2].ID != "b" {
		t.Fatalf("links = %+v", links)
	}
	for _, bad := range [][]string{{"a", "b"}, {"a", "a", "b"}, {"a", "b", "z"}} {
		if _, err := s.ReorderAutomata(p.ID, bad); err == nil {
			t.Fatalf("%v must fail", bad)
		}
	}
	links, _ = s.AutomataOf(p.ID)
	if links[0].ID != "c" {
		t.Fatal("failed reorder changed the order")
	}
}

func TestBacklogIsPlansWithoutAutomaton(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("Aaa"))
	b := mustCreate(t, s, plan("Bbb"))
	mustCreate(t, s, row("A row is not a plan"))
	if got := planIDs(s.Backlog()); len(got) != 2 {
		t.Fatalf("backlog = %v", got)
	}
	if _, err := s.AttachAutomaton(a.ID, "x", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	if got := planIDs(s.Backlog()); len(got) != 1 || got[0] != b.ID {
		t.Fatalf("backlog = %v", got)
	}
	if _, err := s.DetachAutomaton("x"); err != nil {
		t.Fatal(err)
	}
	if len(s.Backlog()) != 2 {
		t.Fatal("plan did not return to the backlog")
	}
}

func TestUpdateGoalOrderKeepsIndexInSync(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("Aaa"))
	b := mustCreate(t, s, plan("Bbb"))
	if _, err := s.AttachAutomaton(b.ID, "x", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	goal, ord, st, title := "Ship it", 1, StateInProgress, "Bbb renamed"
	if _, err := s.Update(b.ID, Patch{Goal: &goal, Order: &ord, State: &st, Title: &title}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(b.ID)
	if got.Goal != goal || got.Order != 1 || len(got.Automata) != 1 {
		t.Fatalf("record = %+v", got)
	}
	ref, _ := s.PlanOf("x")
	if ref.Title != title || ref.State != StateInProgress {
		t.Fatalf("PlanOf not refreshed: %+v", ref)
	}
	_ = a
	long := string(make([]rune, MaxGoalRunes+1))
	bad := []rune(long)
	for i := range bad {
		bad[i] = 'x'
	}
	g := string(bad)
	if _, err := s.Update(a.ID, Patch{Goal: &g}); err == nil {
		t.Fatal("overlong goal must fail")
	}
}

func TestLinksSurviveReopenAndOldRecordsLoad(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	f := mustCreate(t, s, finding("Bug"))
	old := mustCreate(t, s, row("Old style row"))
	if _, err := s.AttachAutomaton(p.ID, "aut-1", LinkInfo{Title: "Title one", State: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachAutomation(p.ID, "auto-1", LinkInfo{Title: "Auto one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachFinding(f.ID, Target{Kind: AttachPlan, Ref: p.ID}); err != nil {
		t.Fatal(err)
	}
	// An old record written before the new fields: raw JSON without them.
	raw := `{"id":"legacy-one","uid":"11111111-1111-1111-1111-111111111111","kind":"registry_row","project":"datawatch","state":"open","title":"Legacy row","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	path := filepath.Join(dir, "plan-records", "registry_row", "11111111-1111-1111-1111-111111111111.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Skipped()) != 0 {
		t.Fatalf("skipped: %v", s2.Skipped())
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("reading rewrote an old record")
	}
	if ref, ok := s2.PlanOf("aut-1"); !ok || ref.ID != p.ID {
		t.Fatal("reverse index not rebuilt")
	}
	if ref, ok := s2.PlanOfAutomation("auto-1"); !ok || ref.ID != p.ID {
		t.Fatal("automation reverse index not rebuilt")
	}
	links, _ := s2.AutomataOf(p.ID)
	if len(links) != 1 || links[0].Title != "Title one" {
		t.Fatalf("links = %+v", links)
	}
	lr, err := s2.Get("legacy-one")
	if err != nil || lr.Title != "Legacy row" || lr.Order != 0 {
		t.Fatalf("legacy = %+v %v", lr, err)
	}
	_ = old
	if _, err := s2.AttachAutomaton(mustPlanID(t, s2, "Alpha"), "aut-1", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
}

func mustPlanID(t *testing.T, s *Store, title string) string {
	t.Helper()
	r, err := s.Get(title)
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func TestSealedStoreKeepsLinks(t *testing.T) {
	key := make([]byte, 32)
	sealer, err := KeySealer(key)
	if err != nil {
		t.Fatal(err)
	}
	s, dir, _, _ := newTestStore(t, sealer)
	p := mustCreate(t, s, plan("Alpha"))
	if _, err := s.AttachAutomaton(p.ID, "aut-1", LinkInfo{Title: "Secret title"}); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir, sealer)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.PlanOf("aut-1"); !ok {
		t.Fatal("link lost through a sealed reopen")
	}
}

func TestSearchTitlesCoversAllTypesWithStateFilter(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Search roadmap"))
	mustCreate(t, s, row("Search registry row"))
	f := mustCreate(t, s, finding("Search finding"))
	mustCreate(t, s, row("Unrelated"))
	if _, err := s.AttachAutomaton(p.ID, "aut-1", LinkInfo{Title: "Search Automaton", State: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachAutomaton(p.ID, "aut-2", LinkInfo{Title: "Search other Automaton", State: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachAutomation(p.ID, "auto-1", LinkInfo{Title: "Search Automation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachFinding(f.ID, Target{Kind: AttachPlan, Ref: p.ID}); err != nil {
		t.Fatal(err)
	}
	hits, total := s.SearchTitles("search", TitleFilter{})
	types := map[string]int{}
	for _, h := range hits {
		types[h.Type]++
	}
	if total != 6 || types[TypePlan] != 1 || types[TypeAutomaton] != 2 || types[TypeRegistryRow] != 1 ||
		types[TypeFinding] != 1 || types[TypeAutomation] != 1 {
		t.Fatalf("total=%d types=%v", total, types)
	}
	hits, total = s.SearchTitles("search", TitleFilter{Types: []string{TypeAutomaton}, State: "running"})
	if total != 1 || hits[0].Ref != "aut-1" || hits[0].PlanRef != p.ID {
		t.Fatalf("hits = %+v", hits)
	}
	hits, _ = s.SearchTitles("search", TitleFilter{State: string(StateOpen)})
	for _, h := range hits {
		if h.State != "open" {
			t.Fatalf("state filter leaked %+v", h)
		}
	}
	if _, total = s.SearchTitles("search roadmap", TitleFilter{}); total != 1 {
		t.Fatalf("multi-word total = %d", total)
	}
	hits, _ = s.SearchTitles("", TitleFilter{Types: []string{TypePlan}})
	if len(hits) != 1 {
		t.Fatalf("empty query plans = %d", len(hits))
	}
	if _, total = s.SearchTitles("search", TitleFilter{Limit: 2}); total != 6 {
		t.Fatal("total must be before paging")
	}
}

func TestListSummariesUsesIndexOnly(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("Aaa"))
	b := mustCreate(t, s, plan("Bbb"))
	mustCreate(t, s, row("A row"))
	if err := s.ReorderPlans([]string{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	// Remove every record file: summaries must still answer from the index.
	root := s.root
	if err := os.RemoveAll(filepath.Join(root, "plan")); err != nil {
		t.Fatal(err)
	}
	got, total := s.ListSummaries(SummaryFilter{Kind: KindPlan})
	if total != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Fatalf("got = %+v", got)
	}
	if _, total = s.ListSummaries(SummaryFilter{State: StateOpen}); total != 3 {
		t.Fatalf("total = %d", total)
	}
}

func TestAuditEventsForLinks(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	if _, err := s.AttachAutomaton(p.ID, "a", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DetachAutomaton("a"); err != nil {
		t.Fatal(err)
	}
	var linked, unlinked bool
	for _, e := range *evs {
		linked = linked || e.action == AuditLinked
		unlinked = unlinked || e.action == AuditUnlinked
	}
	if !linked || !unlinked {
		t.Fatalf("events = %+v", *evs)
	}
}
