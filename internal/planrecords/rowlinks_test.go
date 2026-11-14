package planrecords

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func planRefIDs(ps []PlanRef) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func rowItemTitles(items []RowItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestAttachRowAndListRows(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	engine, browser := plans["engine"], plans["browser"]
	dash, err := s.Get("Ship the dashboard")
	if err != nil {
		t.Fatal(err)
	}
	faster, _ := s.Get("Faster search")

	got, err := s.AttachRow(engine.ID, dash.UID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasLink(got.Links, LinkKindRegistryRow, dash.UID) {
		t.Fatalf("link not stored by uid: %+v", got.Links)
	}
	// By id and by title resolve to the same row; relinking changes nothing.
	again, err := s.AttachRow(engine.ID, "ship the dashboard")
	if err != nil || again.UpdatedAt != got.UpdatedAt || len(again.Links) != 1 {
		t.Fatalf("relink: %+v %v", again, err)
	}
	if _, err := s.AttachRow(engine.ID, faster.ID); err != nil {
		t.Fatal(err)
	}
	// A row may be linked from more than one plan.
	if _, err := s.AttachRow(browser.ID, dash.UID); err != nil {
		t.Fatalf("a row linked from a second plan: %v", err)
	}

	rows, err := s.RowsOf(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Plan.ID != engine.ID || !reflect.DeepEqual(rowItemTitles(rows.Rows), []string{"Ship the dashboard", "Faster search"}) || len(rows.Backlog) != 0 {
		t.Fatalf("rows of the plan: %+v", rows)
	}
	if it := rows.Rows[0]; it.Link != LinkKindRegistryRow || it.UID != dash.UID || it.ID != dash.ID || it.Kind != KindRegistryRow || it.State != StateOpen || it.Project != f.Project {
		t.Fatalf("row item: %+v", it)
	}

	// The row is found from its plans, in priority order, by any reference.
	for _, ref := range []string{dash.UID, dash.ID, "Ship the dashboard"} {
		holders, err := s.PlansOfRow(ref)
		if err != nil || !reflect.DeepEqual(planRefIDs(holders), []string{browser.ID, engine.ID}) {
			t.Fatalf("PlansOfRow(%q): %v %v", ref, planRefIDs(holders), err)
		}
	}
	if holders, _ := s.PlansOfRow("Tidy the docs"); len(holders) != 0 {
		t.Fatalf("an unlinked row has no plans: %+v", holders)
	}

	// Detach.
	after, err := s.DetachRow(engine.ID, dash.ID)
	if err != nil || hasLink(after.Links, LinkKindRegistryRow, dash.UID) || len(after.Links) != 1 {
		t.Fatalf("detach: %+v %v", after, err)
	}
	if holders, _ := s.PlansOfRow(dash.ID); !reflect.DeepEqual(planRefIDs(holders), []string{browser.ID}) {
		t.Fatalf("index after detach: %v", planRefIDs(holders))
	}
	if _, err := s.DetachRow(engine.ID, dash.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("detaching a row that is not linked: %v", err)
	}
	// Detaching the last link leaves a plan with no links at all.
	if last, err := s.DetachRow(engine.ID, faster.ID); err != nil || last.Links != nil {
		t.Fatalf("last detach: %+v %v", last, err)
	}
	// The rows themselves are untouched.
	if again, _ := s.Get(dash.ID); again.UpdatedAt != dash.UpdatedAt {
		t.Fatal("linking a row must not change the row")
	}
}

func TestAttachRowRefusals(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	engine := plans["engine"]
	fin := mustCreate(t, s, Record{Kind: KindFinding, Project: f.Project, Title: "A finding"})
	for name, tc := range map[string]struct{ plan, ref string }{
		"a finding is not a row":     {engine.ID, fin.ID},
		"a plan is not a row":        {engine.ID, plans["browser"].ID},
		"a row is not a plan":        {"Ship the dashboard", "Faster search"},
		"unknown row":                {engine.ID, "no such row"},
		"unknown plan":               {"no such plan", "Faster search"},
		"blank row reference":        {engine.ID, " "},
		"registry row as plan owner": {"Old and shipped", "Faster search"},
	} {
		if _, err := s.AttachRow(tc.plan, tc.ref); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := s.AttachRow(engine.ID, fin.ID); !isInvalid(err) {
		t.Fatalf("a finding as a row link: %v", err)
	}
	if _, err := s.AttachRow(engine.ID, "no such row"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown row: %v", err)
	}
	if got, _ := s.Get(engine.ID); len(got.Links) != 0 {
		t.Fatalf("a refused link must not be stored: %+v", got.Links)
	}
}

func TestAddBacklogItemAndList(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	engine, browser := plans["engine"], plans["browser"]
	docs, _ := s.Get("Tidy the docs")
	nav, _ := s.Get("Rework the navigation")
	fin := mustCreate(t, s, Record{Kind: KindFinding, Project: f.Project, Title: "Loose finding"})

	for _, ref := range []string{docs.UID, fin.ID} {
		if _, err := s.AddBacklogItem(engine.ID, ref); err != nil {
			t.Fatalf("add backlog item %q: %v", ref, err)
		}
	}
	// Same item again is a no-op; the same row in a second plan is allowed.
	cur, _ := s.Get(engine.ID)
	if again, err := s.AddBacklogItem(engine.ID, docs.ID); err != nil || again.UpdatedAt != cur.UpdatedAt {
		t.Fatalf("re-adding: %v", err)
	}
	if _, err := s.AddBacklogItem(browser.ID, docs.ID); err != nil {
		t.Fatalf("a row held as a backlog item by a second plan: %v", err)
	}
	// A row can be a row link in one plan and a backlog item in another.
	if _, err := s.AttachRow(browser.ID, nav.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBacklogItem(engine.ID, nav.ID); err != nil {
		t.Fatalf("a row linked by one plan, held as backlog by another: %v", err)
	}

	list, err := s.RowsOf(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rowItemTitles(list.Backlog), []string{"Tidy the docs", "Loose finding", "Rework the navigation"}) || len(list.Rows) != 0 {
		t.Fatalf("backlog of the plan: %+v", list)
	}
	if it := list.Backlog[1]; it.Link != LinkKindBacklogItem || it.Kind != KindFinding || it.UID != fin.UID {
		t.Fatalf("finding item: %+v", it)
	}
	if holders, _ := s.PlansOfBacklogItem(docs.ID); !reflect.DeepEqual(planRefIDs(holders), []string{browser.ID, engine.ID}) {
		t.Fatalf("backlog holders: %v", planRefIDs(holders))
	}
	if holders, _ := s.PlansOfRow(docs.ID); len(holders) != 0 {
		t.Fatalf("a backlog item is not a row link: %v", planRefIDs(holders))
	}

	after, err := s.RemoveBacklogItem(engine.ID, "loose finding")
	if err != nil || hasLink(after.Links, LinkKindBacklogItem, fin.UID) {
		t.Fatalf("remove backlog item: %+v %v", after, err)
	}
	if _, err := s.RemoveBacklogItem(engine.ID, fin.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing twice: %v", err)
	}
	if holders, _ := s.PlansOfBacklogItem(fin.ID); len(holders) != 0 {
		t.Fatalf("index after remove: %v", planRefIDs(holders))
	}
}

func TestBacklogItemRefusals(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	engine, browser := plans["engine"], plans["browser"]
	docs, _ := s.Get("Tidy the docs")

	// A row that already names an Automaton is not backlog.
	named := mustCreate(t, s, Record{Kind: KindRegistryRow, Project: f.Project, Title: "Row with an Automaton",
		Links: []Link{{Kind: linkAutomaton, Ref: "aaaa0001"}}})
	if _, err := s.AddBacklogItem(engine.ID, named.ID); !isInvalid(err) {
		t.Fatalf("a row with an Automaton: %v", err)
	}
	// A row that names only an Automation still has no Automaton.
	faster, _ := s.Get("Faster search")
	if _, err := s.AddBacklogItem(engine.ID, faster.ID); err != nil {
		t.Fatalf("a row naming an Automation only: %v", err)
	}

	// Findings: attached to an Automaton -> refused; attached to a plan ->
	// allowed there, refused as another plan's backlog (the existing finding
	// link rule); unattached -> allowed.
	onAutomaton := mustCreate(t, s, Record{Kind: KindFinding, Project: f.Project, Title: "On an Automaton"})
	if _, err := s.AttachFinding(onAutomaton.ID, Target{Kind: AttachAutomaton, Ref: "aaaa0001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBacklogItem(engine.ID, onAutomaton.ID); !isInvalid(err) {
		t.Fatalf("a finding attached to an Automaton: %v", err)
	}
	onPlan := mustCreate(t, s, Record{Kind: KindFinding, Project: f.Project, Title: "On the engine plan"})
	if _, err := s.AttachFinding(onPlan.ID, Target{Kind: AttachPlan, Ref: engine.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBacklogItem(engine.ID, onPlan.ID); err != nil {
		t.Fatalf("a finding attached to this same plan: %v", err)
	}
	var lc *ErrLinkConflict
	if _, err := s.AddBacklogItem(browser.ID, onPlan.ID); !errors.As(err, &lc) || lc.Kind != "finding" || lc.Holder != "plan:"+engine.ID {
		t.Fatalf("a finding attached to another plan: %v", err)
	}

	// Only rows and findings are backlog items.
	if _, err := s.AddBacklogItem(engine.ID, browser.ID); !isInvalid(err) {
		t.Fatalf("a plan as a backlog item: %v", err)
	}
	// Row link and backlog item are one or the other inside a plan.
	if _, err := s.AttachRow(engine.ID, docs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBacklogItem(engine.ID, docs.ID); !isInvalid(err) {
		t.Fatalf("row already linked, now as backlog: %v", err)
	}
	if _, err := s.AddBacklogItem("no such plan", docs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown plan: %v", err)
	}
	if _, err := s.AddBacklogItem(engine.ID, "no such item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown item: %v", err)
	}
	if _, err := s.RemoveBacklogItem("no such plan", docs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove on an unknown plan: %v", err)
	}
	if _, err := s.RowsOf("no such plan"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rows of an unknown plan: %v", err)
	}
	if _, err := s.RowsOf(docs.ID); !isInvalid(err) {
		t.Fatalf("rows of a row: %v", err)
	}
	if _, err := s.PlansOfRow("no such row"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("plans of an unknown row: %v", err)
	}
}

func TestRowLinksSurviveReopenAndPatch(t *testing.T) {
	f := loadPlansFixture(t)
	s, plans := f.fullStore(t)
	engine := plans["engine"]
	dash, _ := s.Get("Ship the dashboard")
	docs, _ := s.Get("Tidy the docs")
	if _, err := s.AttachRow(engine.ID, dash.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBacklogItem(engine.ID, docs.ID); err != nil {
		t.Fatal(err)
	}
	// The reverse index is rebuilt from the files on open.
	re, err := Open(filepath.Dir(s.root), nil)
	if err != nil || len(re.Skipped()) != 0 {
		t.Fatalf("reopen: %v %v", err, re.Skipped())
	}
	if h, _ := re.PlansOfRow(dash.ID); !reflect.DeepEqual(planRefIDs(h), []string{engine.ID}) {
		t.Fatalf("row holders after reopen: %v", planRefIDs(h))
	}
	if h, _ := re.PlansOfBacklogItem(docs.ID); !reflect.DeepEqual(planRefIDs(h), []string{engine.ID}) {
		t.Fatalf("backlog holders after reopen: %v", planRefIDs(h))
	}

	// A patch that replaces the Links list keeps the index in step.
	keep := []Link{{Kind: "doc", Ref: "docs/x.md"}, {Kind: LinkKindBacklogItem, Ref: docs.UID}}
	if _, err := s.Update(engine.ID, Patch{Links: &keep}); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.PlansOfRow(dash.ID); len(h) != 0 {
		t.Fatalf("patched-out row link still indexed: %v", planRefIDs(h))
	}
	if h, _ := s.PlansOfBacklogItem(docs.ID); len(h) != 1 {
		t.Fatalf("kept backlog link lost from the index: %v", planRefIDs(h))
	}
	// A patch cannot list a row twice, or as both kinds.
	for name, links := range map[string][]Link{
		"twice":     {{Kind: LinkKindRegistryRow, Ref: dash.UID}, {Kind: LinkKindRegistryRow, Ref: dash.UID}},
		"both kind": {{Kind: LinkKindRegistryRow, Ref: dash.UID}, {Kind: LinkKindBacklogItem, Ref: dash.UID}},
	} {
		l := links
		if _, err := s.Update(engine.ID, Patch{Links: &l}); !isInvalid(err) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	// The rule is for plans; a registry row's own links are not looked at.
	dup := []Link{{Kind: LinkKindRegistryRow, Ref: "x"}, {Kind: LinkKindRegistryRow, Ref: "x"}}
	if _, err := s.Update(dash.ID, Patch{Links: &dup}); err != nil {
		t.Fatalf("a registry row's own links: %v", err)
	}
}

func TestRowLinksFollowThePlanLinkCap(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Big plan"})
	var last Record
	var err error
	for i := 0; i < MaxLinks; i++ {
		r := mustCreate(t, s, row(fmt.Sprintf("Row %03d", i)))
		if last, err = s.AttachRow(p.ID, r.ID); err != nil {
			t.Fatalf("row %d: %v", i, err)
		}
	}
	if len(last.Links) != MaxLinks {
		t.Fatalf("links: %d", len(last.Links))
	}
	over := mustCreate(t, s, row("One too many"))
	if _, err := s.AttachRow(p.ID, over.ID); !isInvalid(err) || !strings.Contains(err.Error(), "too many links") {
		t.Fatalf("row link over the cap must be rejected, not dropped: %v", err)
	}
	if _, err := s.AddBacklogItem(p.ID, over.ID); !isInvalid(err) {
		t.Fatalf("backlog item over the cap: %v", err)
	}
	if got, _ := s.Get(p.ID); len(got.Links) != MaxLinks {
		t.Fatalf("a refused link was stored: %d", len(got.Links))
	}
}

func TestRowLinkAuditEvents(t *testing.T) {
	f := loadPlansFixture(t)
	s, _, evs, _ := newTestStore(t, nil)
	plans := f.seedPlans(t, s)
	f.seedRows(t, s)
	engine := plans["engine"]
	dash, _ := s.Get("Ship the dashboard")
	docs, _ := s.Get("Tidy the docs")
	n := len(*evs)
	if _, err := s.AttachRow(engine.ID, dash.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBacklogItem(engine.ID, docs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DetachRow(engine.ID, dash.ID); err != nil {
		t.Fatal(err)
	}
	got := (*evs)[n:]
	if len(got) != 3 || got[0].action != AuditLinked || got[1].action != AuditLinked || got[2].action != AuditUnlinked {
		t.Fatalf("audit events: %+v", got)
	}
	if got[0].details["link"] != LinkKindRegistryRow || got[0].details["link_id"] != dash.UID ||
		got[1].details["link"] != LinkKindBacklogItem || got[0].details["actor"] != "system" {
		t.Fatalf("audit details: %+v", got)
	}
	// A no-op write sends no event.
	if _, err := s.AddBacklogItem(engine.ID, docs.ID); err != nil {
		t.Fatal(err)
	}
	if len(*evs) != n+3 {
		t.Fatalf("a no-op sent an event: %d", len(*evs)-n)
	}
}
