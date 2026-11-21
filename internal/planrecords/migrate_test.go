package planrecords

import "testing"

// fakeRegistry holds one registry row of every state the migration sees.
type fakeRegistry struct {
	s                                                       *Store
	plan                                                    Record
	shippedWith, shippedWithout, openWithout, closed, dated Record
}

func newFakeRegistry(t *testing.T) (*fakeRegistry, MigrateOptions) {
	t.Helper()
	s, _, _, _ := newTestStore(t, nil)
	f := &fakeRegistry{s: s}
	f.plan = mustCreate(t, s, Record{Kind: KindPlan, Project: "datawatch", Title: "Release 9", State: StatePlanned})
	if _, err := s.AttachAutomaton(f.plan.UID, "aaaa1111", LinkInfo{Title: "Built"}); err != nil {
		t.Fatal(err)
	}
	r := row("Shipped with an Automaton")
	r.State = StateShipped
	r.Links = []Link{{Kind: "automaton", Ref: "aaaa1111"}}
	f.shippedWith = mustCreate(t, s, r)
	r = row("Shipped without one")
	r.State = StateShipped
	f.shippedWithout = mustCreate(t, s, r)
	r = row("Open without one")
	r.LegacyNumber = 7
	f.openWithout = mustCreate(t, s, r)
	r = row("Dropped idea")
	r.State = StateCancelled
	f.closed = mustCreate(t, s, r)
	r = row("Faster search")
	r.Body = "Status: 2026-11-10 v9.5.1 shipped"
	f.dated = mustCreate(t, s, r)
	return f, MigrateOptions{Store: s}
}

func TestMigrateShippedWithAutomatonLinksReleaseItem(t *testing.T) {
	f, opts := newFakeRegistry(t)
	opts.Apply = true
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.s.Get(f.plan.UID)
	if err != nil || !hasLink(held.Links, LinkKindRegistryRow, f.shippedWith.UID) {
		t.Fatalf("shipped row not linked to the release plan: %v", err)
	}
	for _, it := range rep.WouldCreate {
		if it.SourceRef == f.shippedWith.ID {
			t.Fatalf("a plan was made for the shipped row: %+v", it)
		}
	}
	if len(rep.WouldLink) != 1 || rep.WouldLink[0].PlanID != f.plan.ID {
		t.Fatalf("links = %+v", rep.WouldLink)
	}
}

func TestMigrateShippedWithoutAutomatonHistoryGroup(t *testing.T) {
	f, opts := newFakeRegistry(t)
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range rep.HistoryWithoutAutomaton {
		if it.SourceRef == f.shippedWithout.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("shipped row missing from history: %+v", rep.HistoryWithoutAutomaton)
	}
	for _, it := range rep.WouldCreate {
		if it.SourceRef == f.shippedWithout.ID {
			t.Fatal("history row became a plan")
		}
	}
}

func TestMigrateOpenWithoutAutomatonBacklogPlan(t *testing.T) {
	f, opts := newFakeRegistry(t)
	opts.Apply = true
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	var got *MigrateItem
	for i, it := range rep.WouldCreate {
		if it.SourceRef == f.openWithout.ID {
			got = &rep.WouldCreate[i]
		}
	}
	if got == nil || got.Source != migrateSourceRowBacklog || got.Order != 7 || got.PlanID == "" {
		t.Fatalf("backlog item = %+v", got)
	}
}

func TestMigrateClosedRowAsHistory(t *testing.T) {
	f, opts := newFakeRegistry(t)
	opts.Apply = true
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range rep.HistoryWithoutAutomaton {
		if it.SourceRef == f.closed.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("closed row not in history: %+v", rep.HistoryWithoutAutomaton)
	}
	for _, it := range rep.WouldCreate {
		if it.SourceRef == f.closed.ID {
			t.Fatal("closed row became a plan")
		}
	}
}

func TestMigrateStatusTextDateAndVersionIgnored(t *testing.T) {
	f, opts := newFakeRegistry(t)
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range rep.WouldCreate {
		if it.SourceRef == f.dated.ID {
			if it.Title != "Plan: Faster search" || it.TitleSource != TitleDerived {
				t.Fatalf("title = %q (%s)", it.Title, it.TitleSource)
			}
			return
		}
	}
	t.Fatalf("dated row not planned: %+v", rep.Counts())
}

func TestMigrateSecondApplyChangesNothing(t *testing.T) {
	f, opts := newFakeRegistry(t)
	opts.Apply = true
	if _, err := Migrate(opts); err != nil {
		t.Fatal(err)
	}
	n := f.s.Len()
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.WouldCreate) != 0 || len(rep.WouldLink) != 0 || len(rep.Conflicts) != 0 || f.s.Len() != n {
		t.Fatalf("second apply changed things: %v len %d -> %d", rep.Counts(), n, f.s.Len())
	}
}

func TestMigrateNeverDeletesAnything(t *testing.T) {
	f, opts := newFakeRegistry(t)
	opts.Apply = true
	before := f.s.Len()
	if _, err := Migrate(opts); err != nil {
		t.Fatal(err)
	}
	if f.s.Len() < before {
		t.Fatalf("records fell from %d to %d", before, f.s.Len())
	}
	for _, r := range []Record{f.plan, f.shippedWith, f.shippedWithout, f.openWithout, f.closed, f.dated} {
		if _, err := f.s.Get(r.UID); err != nil {
			t.Fatalf("%s is gone: %v", r.ID, err)
		}
	}
}
