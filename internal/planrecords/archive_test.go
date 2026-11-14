package planrecords

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Tests for Archive and Restore. They run on the shared plans fixture (plans
// with and without Automata, registry rows, findings) and on the fake clock and
// audit sink newTestStore provides.

const archivedPlanTitle = "Plans browser"

func titlesOf(ss []Summary) []string {
	out := make([]string, 0, len(ss))
	for _, sm := range ss {
		out = append(out, sm.Title)
	}
	return out
}

func hasTitle(ss []Summary, title string) bool {
	for _, sm := range ss {
		if sm.Title == title {
			return true
		}
	}
	return false
}

func actionsOf(evs *[]auditEv, action string) []auditEv {
	var out []auditEv
	for _, e := range *evs {
		if e.action == action {
			out = append(out, e)
		}
	}
	return out
}

// planWithAutomata returns a fixture plan that holds Automata.
func planWithAutomata(t *testing.T, s *Store, plans map[string]Record) Record {
	t.Helper()
	for _, p := range plans {
		if p.Title == archivedPlanTitle {
			// The seeded record predates the links; read the stored one.
			got, err := s.Get(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
	}
	t.Fatalf("fixture has no plan %q", archivedPlanTitle)
	return Record{}
}

// storeState is every record (archived included) and every link target the
// store knows, read back through the public API.
type storeState struct {
	Records map[string]Record
	Files   []string
	// Targets maps "automaton:<id>" / "automation:<id>" to the plan id that
	// holds it; Automata maps a plan id to its Automata in order.
	Targets  map[string]string
	Automata map[string][]string
}

func captureState(t *testing.T, s *Store) storeState {
	t.Helper()
	st := storeState{Records: map[string]Record{}, Targets: map[string]string{}, Automata: map[string][]string{}}
	all, total := s.List(Filter{IncludeArchived: true, Limit: MaxLimit})
	if total != len(all) {
		t.Fatalf("state capture is paged: %d of %d", len(all), total)
	}
	for _, r := range all {
		st.Records[r.UID] = r
		if r.Kind != KindPlan {
			continue
		}
		links, err := s.AutomataOf(r.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range links {
			st.Automata[r.ID] = append(st.Automata[r.ID], l.ID)
			if p, ok := s.PlanOf(l.ID); ok {
				st.Targets["automaton:"+l.ID] = p.ID
			}
		}
		for _, id := range r.Automations {
			if p, ok := s.PlanOfAutomation(id); ok {
				st.Targets["automation:"+id] = p.ID
			}
		}
	}
	for _, kind := range allKinds {
		entries, err := os.ReadDir(filepath.Join(s.root, string(kind)))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			st.Files = append(st.Files, string(kind)+"/"+e.Name())
		}
	}
	sort.Strings(st.Files)
	return st
}

// withoutArchiveMark blanks the fields Archive and Restore are allowed to
// change on a plan, so two states can be compared for everything else.
func withoutArchiveMark(m map[string]Record) map[string]Record {
	out := map[string]Record{}
	for k, r := range m {
		r.Archived, r.ArchivedAt, r.ArchiveReason, r.UpdatedAt = false, "", "", ""
		out[k] = r
	}
	return out
}

func TestArchiveHidesPlanFromListsSearchAndCountsAndKeepsFile(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	target := planWithAutomata(t, s, plans)
	backlogPlan := s.Backlog()[0]

	before := s.Overview(f.Project, 0)
	res, err := s.Archive(target.ID, "superseded", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.Plan.Archived || res.Plan.ArchivedAt == "" || res.Plan.ArchiveReason != "superseded" {
		t.Fatalf("archive result = %+v", res)
	}
	if res.AutomataHeld != len(target.Automata) || res.AutomataHeld == 0 {
		t.Fatalf("automata held = %d, want %d", res.AutomataHeld, len(target.Automata))
	}
	if _, err := os.Stat(s.path(KindPlan, target.UID)); err != nil {
		t.Fatalf("the plan file must stay on disk: %v", err)
	}

	// Default plan lists.
	if hasTitle(s.Plans(""), target.Title) {
		t.Error("Plans still lists the archived plan")
	}
	if got, total := s.ListSummaries(SummaryFilter{Kind: KindPlan}); hasTitle(got, target.Title) || total != 3 {
		t.Errorf("ListSummaries = %v (%d)", titlesOf(got), total)
	}
	if got, _ := s.List(Filter{Kind: KindPlan}); len(got) != 3 {
		t.Errorf("List returned %d plans, want 3", len(got))
	}
	view := s.ProjectPlans(f.Project, Page{}, Page{})
	for _, d := range append(append([]PlanDigest{}, view.Plans...), view.Backlog...) {
		if d.Summary.Title == target.Title {
			t.Error("ProjectPlans still shows the archived plan")
		}
	}
	if view.PlansTotal != 1 || view.BacklogTotal != 2 {
		t.Errorf("plans/backlog totals = %d/%d", view.PlansTotal, view.BacklogTotal)
	}
	// Search.
	if hits, _ := s.Search("browser", Filter{}); func() bool {
		for _, h := range hits {
			if h.Record.UID == target.UID {
				return true
			}
		}
		return false
	}() {
		t.Error("Search returns the archived plan")
	}
	for _, h := range func() []TitleHit {
		h, _ := s.SearchTitles(archivedPlanTitle, TitleFilter{Types: []string{TypePlan}})
		return h
	}() {
		t.Errorf("SearchTitles returns the archived plan: %+v", h)
	}
	// Overview counts.
	after := s.Overview(f.Project, 0)
	if after.PlansTotal != before.PlansTotal-1 || after.ArchivedPlans != 1 || after.Total != before.Total-len(target.Automata) {
		t.Errorf("overview before %+v after %+v", before, after)
	}
	for _, d := range after.Active {
		if d.Summary.Title == target.Title {
			t.Error("Overview.Active shows the archived plan")
		}
	}

	// A backlog plan leaves the backlog the same way.
	if _, err := s.Archive(backlogPlan.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if hasTitle(s.Backlog(), backlogPlan.Title) || len(s.Backlog()) != 1 {
		t.Errorf("backlog = %v", titlesOf(s.Backlog()))
	}
	if ov := s.Overview(f.Project, 0); ov.Backlog != 1 || ov.ArchivedPlans != 2 {
		t.Errorf("overview backlog = %d archived = %d", ov.Backlog, ov.ArchivedPlans)
	}

	// An archived plan still resolves, with its links in place.
	got, err := s.Get(target.ID)
	if err != nil || !got.Archived || !reflect.DeepEqual(got.Automata, target.Automata) {
		t.Fatalf("get archived = %+v, %v", got, err)
	}
	if p, ok := s.PlanOf(target.Automata[0]); !ok || p.ID != target.ID {
		t.Errorf("PlanOf = %+v, %v", p, ok)
	}
}

func TestArchivedPlansAreListedOnRequest(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	target := planWithAutomata(t, s, plans)
	if _, err := s.Archive(target.ID, "", ""); err != nil {
		t.Fatal(err)
	}

	only, total := s.ListSummaries(SummaryFilter{ArchivedOnly: true})
	if total != 1 || only[0].ID != target.ID || !only[0].Archived || only[0].ArchivedAt == "" {
		t.Errorf("ArchivedOnly summaries = %+v (%d)", only, total)
	}
	if _, total := s.ListSummaries(SummaryFilter{Kind: KindPlan, IncludeArchived: true}); total != 4 {
		t.Errorf("IncludeArchived plan total = %d, want 4", total)
	}
	// ArchivedOnly wins over IncludeArchived.
	if _, total := s.ListSummaries(SummaryFilter{IncludeArchived: true, ArchivedOnly: true}); total != 1 {
		t.Errorf("both flags total = %d, want 1", total)
	}
	if got, total := s.List(Filter{ArchivedOnly: true}); total != 1 || got[0].UID != target.UID {
		t.Errorf("List archived only = %d", total)
	}
	if _, total := s.List(Filter{Kind: KindPlan, IncludeArchived: true}); total != 4 {
		t.Errorf("List include archived = %d", total)
	}
	if hits, total := s.Search("browser", Filter{ArchivedOnly: true}); total != 1 || hits[0].Record.UID != target.UID {
		t.Errorf("Search archived only = %d", total)
	}
	_, defTotal := s.Search("browser", Filter{})
	if _, total := s.Search("browser", Filter{IncludeArchived: true}); total != defTotal+1 {
		t.Errorf("Search include archived = %d, default %d", total, defTotal)
	}
	if hits, _ := s.SearchTitles(archivedPlanTitle, TitleFilter{Types: []string{TypePlan}, IncludeArchived: true}); len(hits) != 1 {
		t.Errorf("SearchTitles include archived = %+v", hits)
	}
	if hits, _ := s.SearchTitles("", TitleFilter{Types: []string{TypePlan}, ArchivedOnly: true}); len(hits) != 1 || hits[0].Ref != target.ID {
		t.Errorf("SearchTitles archived only = %+v", hits)
	}
	// The Automata of an archived plan are found by title either way; archived
	// only narrows them to that plan's.
	auto := target.Automata[0]
	autoTitle := f.automaton(auto).Title
	if hits, _ := s.SearchTitles(autoTitle, TitleFilter{Types: []string{TypeAutomaton}}); len(hits) != 1 || hits[0].PlanRef != target.ID {
		t.Errorf("Automaton search default = %+v", hits)
	}
	if hits, _ := s.SearchTitles(autoTitle, TitleFilter{Types: []string{TypeAutomaton}, ArchivedOnly: true}); len(hits) != 1 {
		t.Errorf("Automaton search archived only = %+v", hits)
	}
	if hits, _ := s.SearchTitles("", TitleFilter{Types: []string{TypeAutomaton}, ArchivedOnly: true}); len(hits) != len(target.Automata) {
		t.Errorf("archived-only Automata = %d, want %d", len(hits), len(target.Automata))
	}
	if got := s.PlansIn("", ArchiveScope{ArchivedOnly: true}); len(got) != 1 || got[0].ID != target.ID {
		t.Errorf("PlansIn archived only = %v", titlesOf(got))
	}
	if got := s.PlansIn("", ArchiveScope{IncludeArchived: true}); len(got) != 4 {
		t.Errorf("PlansIn include = %v", titlesOf(got))
	}
	if got := s.BacklogIn(ArchiveScope{ArchivedOnly: true}); len(got) != 0 {
		t.Errorf("BacklogIn archived only = %v", titlesOf(got))
	}
	view := s.ProjectPlansIn(f.Project, Page{}, Page{}, ArchiveScope{ArchivedOnly: true})
	if view.PlansTotal != 1 || view.BacklogTotal != 0 {
		t.Errorf("ProjectPlansIn archived only = %d/%d", view.PlansTotal, view.BacklogTotal)
	}
	all := s.OverviewIn(f.Project, 0, ArchiveScope{IncludeArchived: true})
	if all.PlansTotal != 4 || all.Total != 14 || all.ArchivedPlans != 1 {
		t.Errorf("OverviewIn include = %+v", all)
	}
}

func TestArchiveScopeAllows(t *testing.T) {
	cases := []struct {
		scope    ArchiveScope
		archived bool
		want     bool
	}{
		{ArchiveScope{}, false, true},
		{ArchiveScope{}, true, false},
		{ArchiveScope{IncludeArchived: true}, false, true},
		{ArchiveScope{IncludeArchived: true}, true, true},
		{ArchiveScope{ArchivedOnly: true}, false, false},
		{ArchiveScope{ArchivedOnly: true}, true, true},
		{ArchiveScope{IncludeArchived: true, ArchivedOnly: true}, false, false},
		{ArchiveScope{IncludeArchived: true, ArchivedOnly: true}, true, true},
	}
	for _, c := range cases {
		if got := c.scope.allows(c.archived); got != c.want {
			t.Errorf("%+v allows(%v) = %v, want %v", c.scope, c.archived, got, c.want)
		}
	}
}

func TestRestoreBringsPlanBackInTheSameOrder(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	target := planWithAutomata(t, s, plans)
	order := titlesOf(s.Plans(""))
	orders := map[string]int{}
	for _, sm := range s.Plans("") {
		orders[sm.ID] = sm.Order
	}
	for _, id := range []string{target.ID} {
		if _, err := s.Archive(id, "tidy", "operator"); err != nil {
			t.Fatal(err)
		}
	}
	if hasTitle(s.Plans(""), target.Title) {
		t.Fatal("plan should be hidden")
	}
	got, err := s.Restore(target.ID, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if got.Archived || got.ArchivedAt != "" || got.ArchiveReason != "" {
		t.Fatalf("restored plan still marked: %+v", got)
	}
	if after := titlesOf(s.Plans("")); !reflect.DeepEqual(after, order) {
		t.Fatalf("order after restore = %v, want %v", after, order)
	}
	for _, sm := range s.Plans("") {
		if sm.Order != orders[sm.ID] {
			t.Errorf("%s order %d, want %d", sm.ID, sm.Order, orders[sm.ID])
		}
	}
	if len(s.Plans("")) != 4 || s.Overview(f.Project, 0).PlansTotal != 4 || s.Overview(f.Project, 0).ArchivedPlans != 0 {
		t.Error("counts did not return")
	}
	if hits, _ := s.SearchTitles(archivedPlanTitle, TitleFilter{Types: []string{TypePlan}}); len(hits) != 1 {
		t.Errorf("search after restore = %+v", hits)
	}
}

func TestArchivedPlanKeepsItsSlotWhenOthersAreReordered(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("Alpha"))
	b := mustCreate(t, s, plan("Beta"))
	c := mustCreate(t, s, plan("Gamma"))
	if _, err := s.Archive(b.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderPlans([]string{c.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	if got := planIDs(s.Plans("")); !reflect.DeepEqual(got, []string{c.ID, a.ID}) {
		t.Fatalf("visible order = %v", got)
	}
	if _, err := s.Restore(b.ID, ""); err != nil {
		t.Fatal(err)
	}
	got := planIDs(s.Plans(""))
	if !reflect.DeepEqual(got, []string{c.ID, a.ID, b.ID}) {
		t.Fatalf("order after restore = %v", got)
	}
	seen := map[int]bool{}
	for _, sm := range s.Plans("") {
		if seen[sm.Order] {
			t.Fatalf("two plans share order %d", sm.Order)
		}
		seen[sm.Order] = true
	}
}

func TestArchiveAndRestoreAreIdempotent(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	if _, err := s.AttachAutomaton(p.ID, "auto-1", LinkInfo{Title: "One", State: "running"}); err != nil {
		t.Fatal(err)
	}

	// Restoring a plan that was never archived changes nothing.
	same, err := s.Restore(p.ID, "operator")
	if err != nil || same.Archived {
		t.Fatalf("restore of a live plan = %+v, %v", same, err)
	}
	if n := len(actionsOf(evs, AuditRestored)); n != 0 {
		t.Fatalf("restore of a live plan emitted %d events", n)
	}

	first, err := s.Archive(p.ID, "first reason", "operator")
	if err != nil || !first.Changed {
		t.Fatalf("first archive = %+v, %v", first, err)
	}
	filesAfterFirst := captureState(t, s)
	second, err := s.Archive(p.ID, "a different reason", "someone-else")
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Error("second archive reports a change")
	}
	if !reflect.DeepEqual(second.Plan, first.Plan) {
		t.Errorf("second archive changed the plan:\n got %+v\nwant %+v", second.Plan, first.Plan)
	}
	if second.AutomataHeld != 1 {
		t.Errorf("second archive automata held = %d", second.AutomataHeld)
	}
	if !reflect.DeepEqual(captureState(t, s), filesAfterFirst) {
		t.Error("second archive changed the store")
	}

	restored, err := s.Restore(p.ID, "operator")
	if err != nil {
		t.Fatal(err)
	}
	againState := captureState(t, s)
	again, err := s.Restore(p.ID, "someone-else")
	if err != nil || !reflect.DeepEqual(again, restored) {
		t.Fatalf("second restore = %+v, %v", again, err)
	}
	if !reflect.DeepEqual(captureState(t, s), againState) {
		t.Error("second restore changed the store")
	}

	if n := len(actionsOf(evs, AuditArchived)); n != 1 {
		t.Errorf("archive events = %d, want 1", n)
	}
	if n := len(actionsOf(evs, AuditRestored)); n != 1 {
		t.Errorf("restore events = %d, want 1", n)
	}
}

func TestArchiveAndRestoreAuditOncePerRealChange(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	for _, id := range []string{"a1", "a2", "a3"} {
		if _, err := s.AttachAutomaton(p.ID, id, LinkInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	*evs = nil

	if _, err := s.Archive(p.ID, "done with it", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(p.ID, "again", "alice"); err != nil {
		t.Fatal(err)
	}
	if len(*evs) != 1 || (*evs)[0].action != AuditArchived || (*evs)[0].subject != "Alpha" {
		t.Fatalf("events after archive = %+v", *evs)
	}
	d := (*evs)[0].details
	if d["actor"] != "alice" || d["reason"] != "done with it" || d["automata_held"] != 3 ||
		d["id"] != p.ID || d["uid"] != p.UID || d["kind"] != string(KindPlan) || d["project"] != "datawatch" {
		t.Fatalf("archive details = %+v", d)
	}

	if _, err := s.Restore(p.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(p.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	if len(*evs) != 2 || (*evs)[1].action != AuditRestored || (*evs)[1].details["actor"] != "bob" {
		t.Fatalf("events after restore = %+v", *evs)
	}

	// No actor means the system, like every other write.
	if _, err := s.Archive(p.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := (*evs)[2].details["actor"]; got != "system" {
		t.Errorf("actor = %v, want system", got)
	}

	// A refused or failed call is not audited.
	n := len(*evs)
	if _, err := s.Archive("no such plan", "", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Restore("no such plan", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(*evs) != n {
		t.Errorf("failed calls emitted %d events", len(*evs)-n)
	}
	for _, e := range *evs {
		if e.action == AuditUpdated {
			t.Errorf("archive must use its own action names, saw %s", e.action)
		}
	}
}

func TestArchiveThenRestoreDeletesAndChangesNothingElse(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	target := planWithAutomata(t, s, plans)
	before := captureState(t, s)
	if len(before.Targets) == 0 || len(before.Automata[target.ID]) == 0 {
		t.Fatal("fixture should carry link targets")
	}

	if _, err := s.Archive(target.ID, "why not", "operator"); err != nil {
		t.Fatal(err)
	}
	mid := captureState(t, s)
	// While archived: same records, same files, same link targets; only the
	// archived plan itself differs, and only by the archive mark.
	if !reflect.DeepEqual(mid.Files, before.Files) {
		t.Errorf("files changed:\n%v\n%v", before.Files, mid.Files)
	}
	if !reflect.DeepEqual(mid.Targets, before.Targets) || !reflect.DeepEqual(mid.Automata, before.Automata) {
		t.Error("link targets changed while archived")
	}
	if !reflect.DeepEqual(withoutArchiveMark(mid.Records), withoutArchiveMark(before.Records)) {
		t.Error("a record changed beyond the archive mark")
	}
	for uid, rec := range mid.Records {
		if uid != target.UID && !reflect.DeepEqual(rec, before.Records[uid]) {
			t.Errorf("record %s (%s) changed by archiving another plan", rec.ID, rec.Kind)
		}
	}

	if _, err := s.Restore(target.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	after := captureState(t, s)
	if !reflect.DeepEqual(after.Files, before.Files) {
		t.Error("file set differs after restore")
	}
	if !reflect.DeepEqual(after.Targets, before.Targets) || !reflect.DeepEqual(after.Automata, before.Automata) {
		t.Error("link targets differ after restore")
	}
	if len(after.Records) != len(before.Records) {
		t.Fatalf("records %d -> %d", len(before.Records), len(after.Records))
	}
	for uid, rec := range after.Records {
		want := before.Records[uid]
		if uid == target.UID {
			// Only the touched timestamp may differ on the plan itself.
			rec.UpdatedAt, want.UpdatedAt = "", ""
		}
		if !reflect.DeepEqual(rec, want) {
			t.Errorf("record %s (%s) differs after the round trip:\n got %+v\nwant %+v", rec.ID, rec.Kind, rec, want)
		}
	}
	// Findings and registry rows are still reachable from the plan and the rows.
	if got := len(s.Overview(f.Project, 0).Findings); got == 0 {
		t.Error("findings vanished from the overview")
	}
}

func TestArchiveKeepsRowsFindingsAndTheirPlanLinks(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	target := planWithAutomata(t, s, plans)
	rowsBefore, _ := s.Browse(BrowseFilter{})
	if _, err := s.Archive(target.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	rowsAfter, _ := s.Browse(BrowseFilter{})
	if !reflect.DeepEqual(rowsBefore, rowsAfter) {
		t.Error("registry rows and findings changed or lost their plan name")
	}
	ov := s.Overview(f.Project, 0)
	if ov.RowsTotal != 6 || ov.FindingsTotal != 4 {
		t.Errorf("rows %d findings %d after archive", ov.RowsTotal, ov.FindingsTotal)
	}
}

func TestArchiveProtectedPlan(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	rel := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Release 11", PlanKind: PlanKindRelease, Release: "11.0.0"})
	other := mustCreate(t, s, plan("Side plan"))
	empty := mustCreate(t, s, plan("Empty plan"))
	for _, id := range []string{"r1", "r2"} {
		if _, err := s.AttachAutomaton(rel.ID, id, LinkInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AttachAutomaton(other.ID, "o1", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	*evs = nil

	_, err := s.Archive(rel.Title, "", "operator", WithProtectedPlan(rel.ID))
	var prot *ErrProtectedPlan
	if !errors.As(err, &prot) || prot.Automata != 2 || prot.Plan.ID != rel.ID {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "2 Automata") {
		t.Errorf("message = %q", err)
	}
	if got, _ := s.Get(rel.ID); got.Archived {
		t.Fatal("protected plan was archived")
	}
	if len(*evs) != 0 {
		t.Fatalf("a refused archive emitted %+v", *evs)
	}
	// The protected plan may be named by any reference form.
	if _, err := s.Archive(rel.ID, "", "operator", WithProtectedPlan("Release 11")); !errors.As(err, &prot) {
		t.Fatalf("title reference err = %v", err)
	}

	// A different plan is archived and reports the Automata it held.
	res, err := s.Archive(other.ID, "x", "operator", WithProtectedPlan(rel.ID))
	if err != nil || !res.Changed || res.AutomataHeld != 1 {
		t.Fatalf("archive of another plan = %+v, %v", res, err)
	}
	// The protected plan with no Automata is not blocked.
	if res, err := s.Archive(empty.ID, "", "operator", WithProtectedPlan(empty.ID)); err != nil || !res.Changed || res.AutomataHeld != 0 {
		t.Fatalf("archive of an empty protected plan = %+v, %v", res, err)
	}
	// Without the option the release plan can be archived.
	if res, err := s.Archive(rel.ID, "", "operator"); err != nil || res.AutomataHeld != 2 {
		t.Fatalf("unprotected archive = %+v, %v", res, err)
	}
	// An archived plan is a no-op even when named protected.
	if res, err := s.Archive(rel.ID, "", "operator", WithProtectedPlan(rel.ID)); err != nil || res.Changed {
		t.Fatalf("repeat archive = %+v, %v", res, err)
	}
	// A protected reference that names nothing fails the call.
	live := mustCreate(t, s, plan("Live plan"))
	if _, err := s.Archive(live.ID, "", "operator", WithProtectedPlan("nothing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown protected plan err = %v", err)
	}
	// A protected reference must name a plan.
	r := mustCreate(t, s, row("A row"))
	if _, err := s.Archive(live.ID, "", "", WithProtectedPlan(r.ID)); err == nil {
		t.Fatal("a row cannot be the protected plan")
	}
	if got, _ := s.Get(live.ID); got.Archived {
		t.Fatal("a failed protected lookup must not archive")
	}
}

func TestArchiveValidation(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	r := mustCreate(t, s, row("A row"))
	fi := mustCreate(t, s, finding("A finding"))
	*evs = nil

	var inv *ErrInvalid
	for _, ref := range []string{r.ID, fi.ID} {
		if _, err := s.Archive(ref, "", ""); !errors.As(err, &inv) {
			t.Errorf("archiving %s: err = %v", ref, err)
		}
		if _, err := s.Restore(ref, ""); !errors.As(err, &inv) {
			t.Errorf("restoring %s: err = %v", ref, err)
		}
	}

	long := strings.Repeat("é", MaxArchiveReasonRunes+1)
	if _, err := s.Archive(p.ID, long, ""); !errors.As(err, &inv) {
		t.Fatalf("over-long reason err = %v", err)
	}
	if got, _ := s.Get(p.ID); got.Archived {
		t.Fatal("a refused archive left the plan archived")
	}
	exact := strings.Repeat("é", MaxArchiveReasonRunes)
	res, err := s.Archive(p.ID, "  "+exact+"  ", "")
	if err != nil || res.Plan.ArchiveReason != exact {
		t.Fatalf("reason at the cap: %v", err)
	}
	if len(*evs) != 1 {
		t.Errorf("events = %d", len(*evs))
	}

	// Archive fields cannot be set at creation or on other kinds.
	for _, in := range []Record{
		{Kind: KindPlan, Project: "datawatch", Title: "Born archived", Archived: true},
		{Kind: KindPlan, Project: "datawatch", Title: "Born with a reason", ArchiveReason: "x"},
	} {
		if _, err := s.Create(in); !errors.As(err, &inv) {
			t.Errorf("create %q: err = %v", in.Title, err)
		}
	}
	bad := Record{ID: "x", UID: "u", Kind: KindRegistryRow, Project: "datawatch", State: StateOpen, Title: "t", Archived: true}
	if err := bad.validate(); err == nil {
		t.Error("an archived registry row must not validate")
	}
	bad = Record{ID: "x", UID: "u", Kind: KindPlan, Project: "datawatch", State: StateOpen, Title: "t", ArchivedAt: "2026-01-01T00:00:00Z"}
	if err := bad.validate(); err == nil {
		t.Error("archived_at without archived must not validate")
	}
}

func TestArchivedPlanKeepsTitleAndReleaseReserved(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	rel := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Release 11", PlanKind: PlanKindRelease, Release: "11.0.0"})
	if got := s.ReleasePlans("datawatch"); len(got) != 1 {
		t.Fatalf("release plans = %v", titlesOf(got))
	}
	if _, err := s.Archive(rel.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.ReleasePlans("datawatch"); len(got) != 0 {
		t.Errorf("archived release plan listed: %v", titlesOf(got))
	}
	if got := s.ReleasePlansIn("datawatch", ArchiveScope{ArchivedOnly: true}); len(got) != 1 {
		t.Errorf("ReleasePlansIn archived only = %v", titlesOf(got))
	}
	// Restoring must never meet a conflict, so the title and the release stay taken.
	var tc *ErrTitleConflict
	if _, err := s.Create(plan("Release 11")); !errors.As(err, &tc) {
		t.Fatalf("title err = %v", err)
	}
	var rc *ErrReleaseConflict
	if _, err := s.Create(Record{Kind: KindPlan, Project: "datawatch", Title: "Another", PlanKind: PlanKindRelease, Release: "11.0.0"}); !errors.As(err, &rc) {
		t.Fatalf("release err = %v", err)
	}
	if _, err := s.Restore(rel.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := s.ReleasePlans("datawatch"); len(got) != 1 {
		t.Errorf("release plans after restore = %v", titlesOf(got))
	}
}

func TestArchiveSurvivesReopenAndSealedStore(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealer, err := KeySealer(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, sl := range map[string]Sealer{"plain": nil, "sealed": sealer} {
		t.Run(name, func(t *testing.T) {
			s, dir, _, _ := newTestStore(t, sl)
			p := mustCreate(t, s, plan("Alpha"))
			keep := mustCreate(t, s, plan("Beta"))
			if _, err := s.AttachAutomaton(p.ID, "a1", LinkInfo{Title: "One", State: "running"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Archive(p.ID, "later", "operator"); err != nil {
				t.Fatal(err)
			}
			re, err := Open(dir, sl)
			if err != nil {
				t.Fatal(err)
			}
			if got := planIDs(re.Plans("")); !reflect.DeepEqual(got, []string{keep.ID}) {
				t.Fatalf("plans after reopen = %v", got)
			}
			got, err := re.Get(p.ID)
			if err != nil || !got.Archived || got.ArchiveReason != "later" || got.ArchivedAt == "" {
				t.Fatalf("archived plan after reopen = %+v, %v", got, err)
			}
			if pl, ok := re.PlanOf("a1"); !ok || pl.ID != p.ID {
				t.Fatalf("link lost on reopen: %+v %v", pl, ok)
			}
			if _, err := re.Restore(p.ID, ""); err != nil {
				t.Fatal(err)
			}
			again, err := Open(dir, sl)
			if err != nil {
				t.Fatal(err)
			}
			if got := planIDs(again.Plans("")); !reflect.DeepEqual(got, []string{p.ID, keep.ID}) {
				t.Fatalf("plans after restore and reopen = %v", got)
			}
		})
	}
}

func TestOldPlanFilesLoadWithoutArchiveFields(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	raw, err := os.ReadFile(s.path(KindPlan, p.UID))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"archived", "archived_at", "archive_reason"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("a live plan file carries %q; the fields must be omitempty", field)
		}
	}
	re, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := re.Get(p.ID); got.Archived || len(re.Plans("")) != 1 {
		t.Fatalf("old file loaded as %+v", got)
	}
}

func TestUpdateOfAnArchivedPlanKeepsTheMark(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Alpha"))
	if _, err := s.Archive(p.ID, "later", ""); err != nil {
		t.Fatal(err)
	}
	goal := "a new goal"
	got, err := s.Update(p.ID, Patch{Goal: &goal})
	if err != nil || !got.Archived || got.ArchiveReason != "later" || got.Goal != goal {
		t.Fatalf("update = %+v, %v", got, err)
	}
	if len(s.Plans("")) != 0 {
		t.Error("an update un-hid the archived plan")
	}
}
