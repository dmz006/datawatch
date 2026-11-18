package planrecords

import "testing"

func migrateFixture(t *testing.T) (*Store, MigrateOptions, *[]auditEv) {
	t.Helper()
	s, _, evs, _ := newTestStore(t, nil)
	r1 := row("Ship the dashboard")
	r1.LegacyNumber = 12
	r1.Body = "Built by aaaa1111 as part of the chain."
	mustCreate(t, s, r1)
	r2 := row("Tidy the docs")
	r2.LegacyNumber = 5
	mustCreate(t, s, r2)
	r3 := row("Faster search")
	r3.LegacyNumber = 9
	r3.Links = []Link{{Kind: "automation", Ref: "bbbb2222"}}
	mustCreate(t, s, r3)
	r4 := row("Old and shipped")
	r4.State = StateShipped
	mustCreate(t, s, r4)
	opts := MigrateOptions{
		Store: s,
		Arcs: []MigrateArc{
			{Project: "datawatch", AutomationID: "cccc3333", Priority: 2, State: "active", Title: "Plans tab", Goal: "Show plans"},
			{Project: "datawatch", AutomationID: "dddd4444", Priority: 1, Title: "Registry import"},
		},
		Resolve: func(id string) (MigrateRef, bool) {
			switch id {
			case "aaaa1111":
				return MigrateRef{Kind: "automaton", Title: "Dashboard build"}, true
			case "bbbb2222":
				return MigrateRef{Kind: "automation", Title: "Search chain"}, true
			}
			return MigrateRef{}, false
		},
	}
	return s, opts, evs
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	s, opts, _ := migrateFixture(t)
	before := s.Len()
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Applied || s.Len() != before {
		t.Fatalf("dry run wrote: applied=%v len %d -> %d", rep.Applied, before, s.Len())
	}
	// 2 arcs + 2 linked rows + 1 backlog row (the shipped row is skipped).
	if len(rep.WouldCreate) != 5 || len(rep.WouldLink) != 4 || len(rep.SkippedExisting) != 1 || len(rep.Conflicts) != 0 {
		t.Fatalf("report = %+v", rep.Counts())
	}
}

func TestMigrateApplyIsIdempotent(t *testing.T) {
	s, opts, evs := migrateFixture(t)
	opts.Apply = true
	opts.Actor = "tester"
	opts.Audit = func(action, subject string, d map[string]any) {
		*evs = append(*evs, auditEv{action: action, subject: subject, details: d})
	}
	rep, err := Migrate(opts)
	if err != nil || len(rep.Conflicts) != 0 {
		t.Fatalf("apply: %v %+v", err, rep)
	}
	if len(rep.WouldCreate) != 5 || len(rep.WouldLink) != 4 {
		t.Fatalf("applied counts = %+v", rep.Counts())
	}
	if p, ok := s.PlanOfAutomation("cccc3333"); !ok || p.Title != "Plans tab" {
		t.Fatalf("arc plan = %+v %v", p, ok)
	}
	if p, ok := s.PlanOf("aaaa1111"); !ok || p.Title != "Plan: Ship the dashboard" {
		t.Fatalf("row plan = %+v %v", p, ok)
	}
	// Backlog plan carries the old row number as its order.
	var backlog *Summary
	for _, b := range s.Backlog() {
		b := b
		if b.Title == "Plan: Tidy the docs" {
			backlog = &b
		}
	}
	if backlog == nil || backlog.Order != 5 {
		t.Fatalf("backlog = %+v", backlog)
	}
	got := false
	for _, e := range *evs {
		if e.action == AuditMigrated {
			got = true
		}
	}
	if !got {
		t.Fatal("apply wrote no audit entry")
	}
	n := s.Len()
	again, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != n || len(again.WouldCreate) != 0 || len(again.WouldLink) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("second apply changed things: len %d -> %d, %+v", n, s.Len(), again.Counts())
	}
}

func TestMigrateNeverOverwritesExistingLinkOrPlan(t *testing.T) {
	s, opts, _ := migrateFixture(t)
	mine := mustCreate(t, s, plan("Mine"))
	if _, err := s.AttachAutomation(mine.ID, "cccc3333", LinkInfo{}); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, plan("Registry import")) // same title as an arc, made earlier
	opts.Apply = true
	rep, err := Migrate(opts)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PlanOfAutomation("cccc3333"); p.ID != mine.ID {
		t.Fatalf("link moved to %+v", p)
	}
	if len(rep.Conflicts) != 1 || rep.Conflicts[0].LinkID != "dddd4444" {
		t.Fatalf("conflicts = %+v", rep.Conflicts)
	}
	if len(rep.SkippedExisting) < 1 {
		t.Fatalf("expected a skipped-existing item, got %+v", rep.Counts())
	}
}

func TestMigrateNeedsStore(t *testing.T) {
	if _, err := Migrate(MigrateOptions{}); err == nil {
		t.Fatal("expected an error without a store")
	}
}
