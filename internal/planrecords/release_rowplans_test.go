package planrecords

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Tests for ListRowPlans and ConvertRowPlans. The row-plans are made by the
// real plans migration from fake rows, and the release migration's work (the
// Automata leaving a row-plan for a release plan) is done with Detach and
// Attach, so the fixture is the state the conversion meets in practice.

type rowPlanFixture struct {
	s      *Store
	evs    *[]auditEv
	r10    Record
	r11    Record
	rows   map[string]Record
	conv   []map[string]any
	rowIDs map[string]string
}

func releasePlanRecord(label string) Record {
	return Record{
		Kind: KindPlan, Project: "datawatch", State: StatePlanned, Title: "Release " + label,
		PlanKind: PlanKindRelease, Release: label,
	}
}

func newRowPlanFixture(t *testing.T) *rowPlanFixture {
	t.Helper()
	s, _, evs, _ := newTestStore(t, nil)
	f := &rowPlanFixture{s: s, evs: evs, rows: map[string]Record{}}
	f.r10 = mustCreate(t, s, releasePlanRecord("10.0"))
	f.r11 = mustCreate(t, s, releasePlanRecord("11.0"))

	mk := func(key, title string, n int, links ...Link) {
		r := row(title)
		r.LegacyNumber, r.Links = n, links
		f.rows[key] = mustCreate(t, s, r)
	}
	mk("dash", "Ship the dashboard", 1, Link{Kind: "automaton", Ref: "aaaa1111"})
	mk("search", "Faster search", 2, Link{Kind: "automaton", Ref: "bbbb2222"}, Link{Kind: "automaton", Ref: "cccc3333"})
	mk("docs", "Tidy the docs", 3)
	mk("wait", "Waiting for the move", 4, Link{Kind: "automaton", Ref: "dddd4444"})
	mk("auto", "Chain by Automation", 5, Link{Kind: "automation", Ref: "eeee5555"})
	mk("autowait", "Automation not filed", 6, Link{Kind: "automation", Ref: "ffff6666"})

	rep, err := Migrate(MigrateOptions{Store: s, Apply: true, Actor: "tester"})
	if err != nil || len(rep.Conflicts) != 0 {
		t.Fatalf("plans migration: %v %+v", err, rep)
	}
	// An ordinary plan with the title form but no marker, a plan with an arc
	// marker, and a plan with a row marker but another title form: none is a
	// row-plan.
	mustCreate(t, s, plan("Plan: written by hand"))
	arc := plan("Plan: made from an arc")
	arc.Links = []Link{{Kind: LinkMigratedFrom, Ref: "arc:datawatch:99990000"}}
	mustCreate(t, s, arc)
	odd := plan("Odd title")
	odd.Links = []Link{{Kind: LinkMigratedFrom, Ref: "row:" + f.rows["dash"].UID}}
	mustCreate(t, s, odd)
	mustCreate(t, s, plan("Release train"))

	// The release migration's work: Automata and Automations leave the
	// row-plan for a release plan. "wait" and "autowait" stay where they are.
	f.move(t, "automaton", "aaaa1111", f.r10)
	f.move(t, "automaton", "bbbb2222", f.r10)
	f.move(t, "automaton", "cccc3333", f.r11)
	f.move(t, "automation", "eeee5555", f.r10)
	return f
}

func (f *rowPlanFixture) move(t *testing.T, kind, id string, to Record) {
	t.Helper()
	var err error
	if kind == "automation" {
		if _, err = f.s.DetachAutomation(id); err == nil {
			_, err = f.s.AttachAutomation(to.ID, id, LinkInfo{})
		}
	} else {
		if _, err = f.s.DetachAutomaton(id); err == nil {
			_, err = f.s.AttachAutomaton(to.ID, id, LinkInfo{})
		}
	}
	if err != nil {
		t.Fatalf("move %s %s: %v", kind, id, err)
	}
}

func (f *rowPlanFixture) opts() ConvertRowPlansOptions {
	return ConvertRowPlansOptions{
		Store: f.s, Actor: "tester",
		Audit: func(action, subject string, d map[string]any) {
			if action != AuditRowPlansConverted {
				panic("unexpected audit action " + action)
			}
			f.conv = append(f.conv, d)
		},
	}
}

func (f *rowPlanFixture) preview(t *testing.T) ConvertRowPlansReport {
	t.Helper()
	rep, err := ConvertRowPlans(f.opts())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func (f *rowPlanFixture) apply(t *testing.T) ConvertRowPlansReport {
	t.Helper()
	pre := f.preview(t)
	o := f.opts()
	o.Apply, o.ApprovalToken = true, pre.PreviewID
	rep, err := ConvertRowPlans(o)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return rep
}

func (f *rowPlanFixture) rowPlan(t *testing.T, key string) Record {
	t.Helper()
	p, err := f.s.Get("Plan: " + f.rows[key].Title)
	if err != nil {
		t.Fatalf("row-plan of %q: %v", key, err)
	}
	return p
}

func changeIDs(l []RowPlanChange) []string {
	out := []string{}
	for _, c := range l {
		out = append(out, c.RowTitle)
	}
	return out
}

func findChange(l []RowPlanChange, rowTitle string) (RowPlanChange, bool) {
	for _, c := range l {
		if c.RowTitle == rowTitle {
			return c, true
		}
	}
	return RowPlanChange{}, false
}

func TestListRowPlansFindsRowPlansAndIgnoresOrdinaryPlans(t *testing.T) {
	f := newRowPlanFixture(t)
	got, err := ListRowPlans(f.s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("listed %d row-plans, want 6: %+v", len(got), got)
	}
	byRow := map[string]RowPlan{}
	for _, rp := range got {
		if !strings.HasPrefix(rp.Title, "Plan: ") || rp.RowID == "" || !rp.RowFound {
			t.Fatalf("not a row-plan with its row: %+v", rp)
		}
		byRow[rp.RowTitle] = rp
	}
	for _, name := range []string{"Plan: written by hand", "Plan: made from an arc", "Odd title", "Release train"} {
		for _, rp := range got {
			if rp.Title == name {
				t.Fatalf("ordinary plan %q was listed as a row-plan", name)
			}
		}
	}

	dash := byRow["Ship the dashboard"]
	if dash.Source != "registry-row" || !reflect.DeepEqual(dash.Automata, []string{"aaaa1111"}) ||
		dash.ReleasePlanID != f.r10.ID || len(dash.Unheld) != 0 || dash.RowID != f.rows["dash"].ID {
		t.Fatalf("dashboard row-plan = %+v", dash)
	}
	search := byRow["Faster search"]
	if !reflect.DeepEqual(search.Automata, []string{"bbbb2222", "cccc3333"}) || search.ReleasePlanID != f.r10.ID ||
		!reflect.DeepEqual(search.OtherReleasePlans, []string{f.r11.ID}) {
		t.Fatalf("search row-plan = %+v", search)
	}
	if docs := byRow["Tidy the docs"]; docs.Source != "registry-row-backlog" || len(docs.Automata)+len(docs.Automations) != 0 || docs.ReleasePlanID != "" {
		t.Fatalf("docs row-plan = %+v", docs)
	}
	if w := byRow["Waiting for the move"]; !reflect.DeepEqual(w.Automata, []string{"dddd4444"}) || !reflect.DeepEqual(w.Unheld, []string{"automaton:dddd4444"}) || w.ReleasePlanID != "" {
		t.Fatalf("waiting row-plan = %+v", w)
	}
	if a := byRow["Chain by Automation"]; !reflect.DeepEqual(a.Automations, []string{"eeee5555"}) || len(a.Unheld) != 0 {
		t.Fatalf("automation row-plan = %+v", a)
	}

	if _, err := ListRowPlans(nil); err == nil {
		t.Fatal("expected an error without a store")
	}
}

func TestListRowPlansIncludesArchivedAndOrdersByPlan(t *testing.T) {
	f := newRowPlanFixture(t)
	before, _ := ListRowPlans(f.s)
	f.apply(t)
	after, err := ListRowPlans(f.s)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("archived row-plans dropped from the list: %d -> %d", len(before), len(after))
	}
	archived := 0
	for i, rp := range after {
		if rp.PlanID != before[i].PlanID {
			t.Fatalf("order changed at %d", i)
		}
		if rp.Archived {
			archived++
		}
	}
	if archived != 4 {
		t.Fatalf("%d archived row-plans, want 4", archived)
	}
}

func TestListRowPlansReadsRowTextWithResolver(t *testing.T) {
	f := newRowPlanFixture(t)
	r := row("Text names an id")
	r.Body = "Built by 99aa77bb as part of the chain."
	row := mustCreate(t, f.s, r)
	if _, err := Migrate(MigrateOptions{Store: f.s, Apply: true}); err != nil {
		t.Fatal(err)
	}
	// Without a resolver the row-plan is a backlog row-plan; with one the id is
	// an Automaton the row names.
	plain, _ := ListRowPlans(f.s)
	for _, rp := range plain {
		if rp.RowID == row.ID && rp.Source != "registry-row-backlog" {
			t.Fatalf("no resolver, source = %q", rp.Source)
		}
	}
	got := listRowPlans(f.s, "", func(id string) (MigrateRef, bool) {
		return MigrateRef{Kind: "automaton"}, id == "99aa77bb"
	})
	for _, rp := range got {
		if rp.RowID != row.ID {
			continue
		}
		if rp.Source != "registry-row" || !reflect.DeepEqual(rp.Automata, []string{"99aa77bb"}) || !reflect.DeepEqual(rp.Unheld, []string{"automaton:99aa77bb"}) {
			t.Fatalf("resolved row-plan = %+v", rp)
		}
		return
	}
	t.Fatal("the row-plan was not listed")
}

func TestConvertRowPlansPreviewWritesNothing(t *testing.T) {
	f := newRowPlanFixture(t)
	before := captureState(t, f.s)
	nEvents := len(*f.evs)
	rep := f.preview(t)
	if rep.Applied || rep.PreviewID == "" {
		t.Fatalf("preview = %+v", rep)
	}
	if !reflect.DeepEqual(before, captureState(t, f.s)) {
		t.Fatal("the preview changed the store")
	}
	if len(*f.evs) != nEvents || len(f.conv) != 0 {
		t.Fatalf("the preview wrote audit entries: store %d -> %d, converter %d", nEvents, len(*f.evs), len(f.conv))
	}
	want := map[string]int{"link_rows": 2, "backlog_items": 2, "waiting": 2, "conflicts": 0, "unchanged": 0}
	if !reflect.DeepEqual(rep.Counts(), want) || rep.Changes() != 4 {
		t.Fatalf("counts = %v changes = %d", rep.Counts(), rep.Changes())
	}
	// The preview id is stable while nothing changes.
	if again := f.preview(t); again.PreviewID != rep.PreviewID {
		t.Fatalf("preview id moved: %s -> %s", rep.PreviewID, again.PreviewID)
	}
}

func TestConvertRowPlansWrittenRunNeedsMatchingPreviewID(t *testing.T) {
	f := newRowPlanFixture(t)
	before := captureState(t, f.s)

	o := f.opts()
	o.Apply = true
	if _, err := ConvertRowPlans(o); !errors.Is(err, ErrRowPlansPreviewMissing) {
		t.Fatalf("no id: %v", err)
	}
	o.ApprovalToken = "not-the-preview-id"
	if _, err := ConvertRowPlans(o); !errors.Is(err, ErrRowPlansPreviewStale) {
		t.Fatalf("wrong id: %v", err)
	}
	// An id from before the data changed is stale.
	pre := f.preview(t)
	f.move(t, "automaton", "dddd4444", f.r10)
	o.ApprovalToken = pre.PreviewID
	if _, err := ConvertRowPlans(o); !errors.Is(err, ErrRowPlansPreviewStale) {
		t.Fatalf("stale id: %v", err)
	}
	after := captureState(t, f.s)
	// Only the move above is allowed to differ.
	if len(before.Records) != len(after.Records) || len(f.conv) != 0 {
		t.Fatalf("a refused run wrote: records %d -> %d, audit %d", len(before.Records), len(after.Records), len(f.conv))
	}
	for _, r := range after.Records {
		if r.Archived {
			t.Fatalf("a refused run archived %q", r.Title)
		}
	}
}

func TestConvertRowPlansAutomataBecomeRegistryRowLinkOnTheReleasePlan(t *testing.T) {
	f := newRowPlanFixture(t)
	rep := f.apply(t)
	if !rep.Applied || len(rep.Conflicts) != 0 {
		t.Fatalf("apply = %+v", rep)
	}

	dash, ok := findChange(rep.LinkRows, "Ship the dashboard")
	if !ok || dash.Op != RowPlanOpLinkRow || dash.ReleasePlanID != f.r10.ID || dash.ReleasePlanTitle != "Release 10.0" {
		t.Fatalf("dashboard change = %+v (%v)", dash, ok)
	}
	holders, err := f.s.PlansOfRow(f.rows["dash"].ID)
	if err != nil || len(holders) != 1 || holders[0].ID != f.r10.ID {
		t.Fatalf("dashboard row holders = %+v %v", holders, err)
	}
	rp := f.rowPlan(t, "dash")
	if !rp.Archived || !strings.Contains(rp.ArchiveReason, "converted") || !strings.Contains(rp.ArchiveReason, "Release 10.0") {
		t.Fatalf("row-plan after = archived %v reason %q", rp.Archived, rp.ArchiveReason)
	}
	// The Automaton is still on the release plan.
	if p, ok := f.s.PlanOf("aaaa1111"); !ok || p.ID != f.r10.ID {
		t.Fatalf("Automaton moved to %+v", p)
	}

	// Automata on two release plans: the first by plan order gets the row, and
	// the other is reported.
	search, ok := findChange(rep.LinkRows, "Faster search")
	if !ok || search.ReleasePlanID != f.r10.ID || !reflect.DeepEqual(search.OtherReleasePlans, []string{f.r11.ID}) ||
		!strings.Contains(search.Reason, "2 release plans") {
		t.Fatalf("search change = %+v", search)
	}
	holders, _ = f.s.PlansOfRow(f.rows["search"].ID)
	if len(holders) != 1 || holders[0].ID != f.r10.ID {
		t.Fatalf("search row holders = %+v", holders)
	}
	if p, _ := f.s.PlanOf("cccc3333"); p.ID != f.r11.ID {
		t.Fatalf("an Automaton moved off its release plan: %+v", p)
	}
}

func TestConvertRowPlansNoAutomatonBecomesBacklogItemOfTheCurrentReleasePlan(t *testing.T) {
	f := newRowPlanFixture(t)
	rep := f.apply(t)

	docs, ok := findChange(rep.BacklogItems, "Tidy the docs")
	if !ok || docs.Op != RowPlanOpBacklogItem || docs.ReleasePlanID != f.r10.ID {
		t.Fatalf("docs change = %+v (%v)", docs, ok)
	}
	holders, err := f.s.PlansOfBacklogItem(f.rows["docs"].ID)
	if err != nil || len(holders) != 1 || holders[0].ID != f.r10.ID {
		t.Fatalf("docs backlog holders = %+v %v", holders, err)
	}
	if rows, _ := f.s.PlansOfRow(f.rows["docs"].ID); len(rows) != 0 {
		t.Fatalf("a backlog row was also linked as a registry row: %+v", rows)
	}
	if !f.rowPlan(t, "docs").Archived {
		t.Fatal("the backlog row-plan was not archived")
	}

	// A row-plan holding only an Automation that a release plan holds follows
	// the same rule.
	if _, ok := findChange(rep.BacklogItems, "Chain by Automation"); !ok {
		t.Fatalf("automation row-plan not converted: %v", changeIDs(rep.BacklogItems))
	}
	rec, _ := f.s.RowsOf(f.r10.ID)
	if len(rec.Backlog) != 2 || len(rec.Rows) != 2 {
		t.Fatalf("release plan holds %d backlog items and %d rows, want 2 and 2", len(rec.Backlog), len(rec.Rows))
	}
}

func TestConvertRowPlansWaitingRowPlanIsLeftAlone(t *testing.T) {
	f := newRowPlanFixture(t)
	before := f.rowPlan(t, "wait")
	rep := f.apply(t)

	for _, name := range []string{"Waiting for the move", "Automation not filed"} {
		w, ok := findChange(rep.Waiting, name)
		if !ok || !strings.Contains(w.Reason, "not held by a release plan") || w.Op != "" {
			t.Fatalf("%s change = %+v (%v)", name, w, ok)
		}
		if _, ok := findChange(append(rep.LinkRows, rep.BacklogItems...), name); ok {
			t.Fatalf("%s was converted", name)
		}
	}
	after := f.rowPlan(t, "wait")
	if after.Archived || !reflect.DeepEqual(before, after) {
		t.Fatalf("the waiting row-plan changed: %+v -> %+v", before, after)
	}
	if p, ok := f.s.PlanOf("dddd4444"); !ok || p.ID != after.ID {
		t.Fatalf("the waiting Automaton left its plan: %+v", p)
	}
	if holders, _ := f.s.PlansOfRow(f.rows["wait"].ID); len(holders) != 0 {
		t.Fatalf("the waiting row got a link: %+v", holders)
	}
	if !hasTitle(f.s.Plans(""), after.Title) {
		t.Fatal("the waiting row-plan left the plan list")
	}

	// Once the release migration has filed the Automaton the row-plan converts.
	f.move(t, "automaton", "dddd4444", f.r11)
	rep = f.apply(t)
	if c, ok := findChange(rep.LinkRows, "Waiting for the move"); !ok || c.ReleasePlanID != f.r11.ID {
		t.Fatalf("after the move: %+v (%v)", c, ok)
	}
	if !f.rowPlan(t, "wait").Archived {
		t.Fatal("the row-plan was not archived once its Automaton was filed")
	}
}

func TestConvertRowPlansRemovesFromPlanListWithoutDeletingAnyRecord(t *testing.T) {
	f := newRowPlanFixture(t)
	before := captureState(t, f.s)
	listedBefore := titlesOf(f.s.Plans(""))
	rep := f.apply(t)
	after := captureState(t, f.s)

	// The same records, the same files, the same Automata on the same plans.
	if len(before.Records) != len(after.Records) || !reflect.DeepEqual(before.Files, after.Files) {
		t.Fatalf("records %d -> %d, files differ: %v", len(before.Records), len(after.Records), !reflect.DeepEqual(before.Files, after.Files))
	}
	if !reflect.DeepEqual(before.Targets, after.Targets) || !reflect.DeepEqual(before.Automata, after.Automata) {
		t.Fatalf("Automata or targets changed:\n%v\n%v\n%v\n%v", before.Targets, after.Targets, before.Automata, after.Automata)
	}
	archived := map[string]bool{}
	for _, c := range append(append([]RowPlanChange{}, rep.LinkRows...), rep.BacklogItems...) {
		archived[c.PlanID] = true
	}
	for uid, was := range before.Records {
		now, ok := after.Records[uid]
		if !ok {
			t.Fatalf("record %q (%s) is gone", was.Title, was.Kind)
		}
		switch {
		case archived[was.ID]:
			if !now.Archived || was.Archived {
				t.Fatalf("%q: archived %v -> %v", was.Title, was.Archived, now.Archived)
			}
			// Only the archive mark and the update time differ.
			was.Archived, was.ArchivedAt, was.ArchiveReason, was.UpdatedAt = true, now.ArchivedAt, now.ArchiveReason, now.UpdatedAt
			if !reflect.DeepEqual(was, now) {
				t.Fatalf("%q changed beyond the archive mark:\n%+v\n%+v", was.Title, was, now)
			}
		case was.ID == f.r10.ID:
			// The release plan only gained row links.
			for _, l := range was.Links {
				if !hasLink(now.Links, l.Kind, l.Ref) {
					t.Fatalf("release plan lost the link %+v", l)
				}
			}
			if len(now.Links) <= len(was.Links) || !reflect.DeepEqual(was.Automata, now.Automata) || !reflect.DeepEqual(was.Automations, now.Automations) {
				t.Fatalf("release plan after = %+v", now)
			}
		case was.ID == f.r11.ID:
			if len(now.Links) != len(was.Links) {
				t.Fatalf("release plan 11.0 gained links it should not have: %+v", now.Links)
			}
		default:
			if !reflect.DeepEqual(was, now) {
				t.Fatalf("%q (%s) changed:\n%+v\n%+v", was.Title, was.Kind, was, now)
			}
		}
	}
	// Every row is still a row, with the same content, and the row-plan's own
	// links and Automata links are intact.
	for key, r := range f.rows {
		if now := after.Records[r.UID]; !reflect.DeepEqual(r, now) {
			t.Fatalf("row %q changed", key)
		}
	}

	// The row-plans left the plan list; the waiting ones and the ordinary
	// plans stayed.
	listed := f.s.Plans("")
	for _, name := range []string{"Plan: Ship the dashboard", "Plan: Faster search", "Plan: Tidy the docs", "Plan: Chain by Automation"} {
		if hasTitle(listed, name) {
			t.Fatalf("%q is still in the plan list", name)
		}
		if !hasTitle(f.s.PlansIn("", ArchiveScope{ArchivedOnly: true}), name) {
			t.Fatalf("%q is not among the archived plans", name)
		}
	}
	for _, name := range []string{"Plan: Waiting for the move", "Plan: Automation not filed", "Plan: written by hand", "Plan: made from an arc", "Odd title", "Release train", "Release 10.0", "Release 11.0"} {
		if !hasTitle(listed, name) {
			t.Fatalf("%q left the plan list", name)
		}
	}
	if len(listed) != len(listedBefore)-4 {
		t.Fatalf("plan list %d -> %d, want 4 fewer", len(listedBefore), len(listed))
	}
	// A restore brings a row-plan straight back.
	if _, err := f.s.Restore("Plan: Tidy the docs", "tester"); err != nil || !hasTitle(f.s.Plans(""), "Plan: Tidy the docs") {
		t.Fatalf("restore: %v", err)
	}
}

func TestConvertRowPlansSecondRunChangesNothing(t *testing.T) {
	f := newRowPlanFixture(t)
	first := f.apply(t)
	if first.Changes() != 4 {
		t.Fatalf("first run changed %d row-plans", first.Changes())
	}
	state := captureState(t, f.s)
	nStore, nConv := len(*f.evs), len(f.conv)

	second := f.apply(t)
	if second.Changes() != 0 || len(second.Conflicts) != 0 {
		t.Fatalf("second run = %+v", second.Counts())
	}
	if len(second.Unchanged) != 4 || len(second.Waiting) != 2 {
		t.Fatalf("second run counts = %v", second.Counts())
	}
	for _, c := range second.Unchanged {
		if c.Reason != "already archived" {
			t.Fatalf("unchanged reason = %q", c.Reason)
		}
	}
	if !reflect.DeepEqual(state, captureState(t, f.s)) {
		t.Fatal("the second run changed the store")
	}
	if len(*f.evs) != nStore || len(f.conv) != nConv {
		t.Fatalf("the second run wrote audit entries: store %d -> %d, converter %d -> %d", nStore, len(*f.evs), nConv, len(f.conv))
	}
	// The first run's token is stale for the second state.
	o := f.opts()
	o.Apply, o.ApprovalToken = true, first.PreviewID
	if _, err := ConvertRowPlans(o); !errors.Is(err, ErrRowPlansPreviewStale) {
		t.Fatalf("reusing the first token: %v", err)
	}
}

func TestConvertRowPlansAuditsEveryWrittenChangeWithTheActor(t *testing.T) {
	f := newRowPlanFixture(t)
	f.apply(t)
	ops := map[string]int{}
	for _, d := range f.conv {
		if d["actor"] != "tester" || d["project"] != "datawatch" || d["plan_id"] == "" || d["row_id"] == "" || d["release_plan_id"] == "" {
			t.Fatalf("audit entry = %+v", d)
		}
		ops[d["op"].(string)]++
	}
	if ops["link_row"] != 2 || ops["add_backlog_item"] != 2 || ops["archive"] != 4 || len(f.conv) != 8 {
		t.Fatalf("converter audit ops = %v", ops)
	}
	archives := actionsOf(f.evs, AuditArchived)
	if len(archives) != 4 {
		t.Fatalf("%d archive events, want 4", len(archives))
	}
	for _, e := range archives {
		if e.details["actor"] != "tester" || !strings.Contains(e.details["reason"].(string), "converted") {
			t.Fatalf("archive event = %+v", e)
		}
	}

	// With no actor named the entries say "system".
	g := newRowPlanFixture(t)
	pre := g.preview(t)
	o := g.opts()
	o.Actor, o.Apply, o.ApprovalToken = "", true, pre.PreviewID
	if _, err := ConvertRowPlans(o); err != nil {
		t.Fatal(err)
	}
	for _, d := range g.conv {
		if d["actor"] != "system" {
			t.Fatalf("default actor = %v", d["actor"])
		}
	}
}

func TestConvertRowPlansFinishesAHalfDoneConversion(t *testing.T) {
	f := newRowPlanFixture(t)
	// An earlier run linked the row and stopped before the archive.
	if _, err := f.s.AttachRow(f.r10.ID, f.rows["dash"].UID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddBacklogItem(f.r10.ID, f.rows["docs"].UID); err != nil {
		t.Fatal(err)
	}
	pre := f.preview(t)
	for _, name := range []string{"Ship the dashboard", "Tidy the docs"} {
		c, ok := findChange(append(append([]RowPlanChange{}, pre.LinkRows...), pre.BacklogItems...), name)
		if !ok || !c.LinkPresent {
			t.Fatalf("%s: %+v (%v)", name, c, ok)
		}
	}
	f.apply(t)
	if !f.rowPlan(t, "dash").Archived || !f.rowPlan(t, "docs").Archived {
		t.Fatal("the half-done row-plans were not archived")
	}
	rows, _ := f.s.RowsOf(f.r10.ID)
	if len(rows.Rows) != 2 || len(rows.Backlog) != 2 {
		t.Fatalf("links doubled or lost: %d rows, %d backlog", len(rows.Rows), len(rows.Backlog))
	}
	// The audit entries list the archives but no second link for them.
	for _, d := range f.conv {
		if d["op"] != "archive" && (d["row_id"] == f.rows["dash"].ID || d["row_id"] == f.rows["docs"].ID) {
			t.Fatalf("a link that was already there was written again: %+v", d)
		}
	}
}

func TestConvertRowPlansWithoutACurrentReleasePlanTheBacklogRowPlansWait(t *testing.T) {
	f := newRowPlanFixture(t)
	o := f.opts()
	o.CurrentRelease = "12.0"
	rep, err := ConvertRowPlans(o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CurrentRelease != "12.0" || len(rep.BacklogItems) != 0 || len(rep.LinkRows) != 2 || len(rep.Waiting) != 4 {
		t.Fatalf("counts = %v", rep.Counts())
	}
	if w, ok := findChange(rep.Waiting, "Tidy the docs"); !ok || !strings.Contains(w.Reason, "no current release plan") {
		t.Fatalf("docs = %+v", w)
	}
	// An archived current release plan does not count either.
	if _, err := f.s.Archive(f.r10.ID, "retired", "tester"); err != nil {
		t.Fatal(err)
	}
	rep = f.preview(t)
	if len(rep.BacklogItems) != 0 {
		t.Fatalf("backlog items go to an archived release plan: %+v", rep.BacklogItems)
	}
}

func TestConvertRowPlansLeavesRowPlansWithOtherHeldWork(t *testing.T) {
	f := newRowPlanFixture(t)
	// A finding attached to a row-plan, and a row held by a row-plan, are held by
	// nothing else.
	fnd := mustCreate(t, f.s, finding("Found while reading"))
	dash := f.rowPlan(t, "dash")
	if _, err := f.s.AttachFinding(fnd.ID, Target{Kind: AttachPlan, Ref: dash.ID}); err != nil {
		t.Fatal(err)
	}
	other := mustCreate(t, f.s, row("Some other row"))
	docs := f.rowPlan(t, "docs")
	if _, err := f.s.AttachRow(docs.ID, other.UID); err != nil {
		t.Fatal(err)
	}
	rep := f.apply(t)
	for _, name := range []string{"Ship the dashboard", "Tidy the docs"} {
		w, ok := findChange(rep.Waiting, name)
		if !ok || !strings.Contains(w.Reason, "nothing else holds") {
			t.Fatalf("%s = %+v (%v)", name, w, ok)
		}
	}
	if f.rowPlan(t, "dash").Archived || f.rowPlan(t, "docs").Archived {
		t.Fatal("a row-plan holding other work was archived")
	}
	if got, _ := f.s.Get(fnd.ID); got.AttachRef != dash.ID {
		t.Fatalf("the finding moved: %+v", got)
	}
}

func TestConvertRowPlansConflicts(t *testing.T) {
	f := newRowPlanFixture(t)
	// A row-plan whose row is not in the store.
	ghost := plan("Plan: Ghost row")
	ghost.Links = []Link{{Kind: LinkMigratedFrom, Ref: "row:00000000-dead-beef-0000-000000000000"}}
	mustCreate(t, f.s, ghost)
	// The release plan holds the dashboard row as a backlog item already.
	if _, err := f.s.AddBacklogItem(f.r10.ID, f.rows["dash"].UID); err == nil {
		// The row names an Automaton, so this is refused; use a row that does not.
		t.Fatal("expected the backlog item of a row with an Automaton to be refused")
	}
	if _, err := f.s.AddBacklogItem(f.r10.ID, f.rows["docs"].UID); err != nil {
		t.Fatal(err)
	}
	rep := f.apply(t)
	g, ok := findChange(rep.Conflicts, "")
	if !ok || g.Title != "Plan: Ghost row" || !strings.Contains(g.Reason, "not in the store") {
		t.Fatalf("ghost = %+v (%v)", g, ok)
	}
	if got, _ := f.s.Get("Plan: Ghost row"); got.Archived {
		t.Fatal("the ghost row-plan was archived")
	}

	// The release plan holds the dashboard row as a backlog item: a registry-row
	// link would be refused, so the row-plan is a conflict and stays.
	h := newRowPlanFixture(t)
	h.s.mu.Lock()
	rec := *h.s.byUID[h.r10.UID]
	rec.Links = append(rec.Links, Link{Kind: LinkKindBacklogItem, Ref: h.rows["dash"].UID})
	next := rec.clone()
	if err := h.s.commit(next); err != nil {
		h.s.mu.Unlock()
		t.Fatal(err)
	}
	h.s.mu.Unlock()
	rep = h.apply(t)
	c, ok := findChange(rep.Conflicts, "Ship the dashboard")
	if !ok || !strings.Contains(c.Reason, "backlog item") || h.rowPlan(t, "dash").Archived {
		t.Fatalf("held-as-backlog = %+v (%v)", c, ok)
	}
}

func TestConvertRowPlansProjectFilterAndOptionErrors(t *testing.T) {
	f := newRowPlanFixture(t)
	o := f.opts()
	o.Project = "other-project"
	rep, err := ConvertRowPlans(o)
	if err != nil || rep.Changes() != 0 || len(rep.Waiting) != 0 || len(rep.Unchanged) != 0 {
		t.Fatalf("other project = %+v %v", rep.Counts(), err)
	}
	o.Project = "datawatch"
	if rep, err = ConvertRowPlans(o); err != nil || rep.Changes() != 4 {
		t.Fatalf("own project = %+v %v", rep.Counts(), err)
	}
	o.Project = "bad project!"
	if _, err = ConvertRowPlans(o); err == nil {
		t.Fatal("expected an error for a bad project name")
	}
	if _, err = ConvertRowPlans(ConvertRowPlansOptions{}); err == nil {
		t.Fatal("expected an error without a store")
	}
	// An empty report still has empty lists, never null.
	empty, _, _, _ := newTestStore(t, nil)
	rep, err = ConvertRowPlans(ConvertRowPlansOptions{Store: empty})
	if err != nil || rep.CurrentRelease != DefaultCurrentRelease || rep.LinkRows == nil || rep.BacklogItems == nil ||
		rep.Waiting == nil || rep.Conflicts == nil || rep.Unchanged == nil {
		t.Fatalf("empty report = %+v %v", rep, err)
	}
	if rep, err = ConvertRowPlans(ConvertRowPlansOptions{Store: empty, Apply: true, ApprovalToken: rep.PreviewID}); err != nil || !rep.Applied {
		t.Fatalf("empty apply = %+v %v", rep, err)
	}
}

func TestConvertRowPlansWriteStepReportsRefusedLinksAndArchives(t *testing.T) {
	f := newRowPlanFixture(t)
	dash := f.rowPlan(t, "dash")
	good := RowPlanChange{
		PlanID: dash.ID, Title: dash.Title, Project: "datawatch", RowID: f.rows["dash"].ID, Op: RowPlanOpLinkRow,
		ReleasePlanID: f.r10.ID, ReleasePlanTitle: f.r10.Title, uid: dash.UID, rowUID: f.rows["dash"].UID, releaseUID: f.r10.UID,
	}
	// The row is gone (a link to it is refused), then the row-plan is gone
	// (the archive is refused), as if another writer got in between.
	noRow, noPlan, backlog := good, good, good
	noRow.rowUID = "00000000-0000-0000-0000-000000000000"
	noPlan.uid = "00000000-0000-0000-0000-000000000001"
	backlog.Op, backlog.rowUID = RowPlanOpBacklogItem, "00000000-0000-0000-0000-000000000002"
	rep := ConvertRowPlansReport{
		LinkRows:     []RowPlanChange{noRow, noPlan},
		BacklogItems: []RowPlanChange{backlog},
	}
	executeRowPlans(f.opts(), &rep)
	if len(rep.LinkRows) != 0 || len(rep.BacklogItems) != 0 || len(rep.Conflicts) != 3 {
		t.Fatalf("report = %v", rep.Counts())
	}
	for i, want := range []string{"cannot link the row", "the row is linked but the plan was not archived", "cannot link the row"} {
		if !strings.Contains(rep.Conflicts[i].Reason, want) {
			t.Fatalf("conflict %d reason = %q, want %q", i, rep.Conflicts[i].Reason, want)
		}
	}
	// The refused link wrote nothing; the refused archive left the link that was
	// written, which a second run finishes.
	if got := f.rowPlan(t, "dash"); got.Archived {
		t.Fatal("a refused conversion archived the row-plan")
	}
	if holders, _ := f.s.PlansOfRow(f.rows["dash"].ID); len(holders) != 1 {
		t.Fatalf("holders = %+v", holders)
	}
}
