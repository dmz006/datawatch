package planrecords

import (
	"errors"
	"strings"
	"testing"
)

func lastAudit(t *testing.T, evs *[]auditEv, action string) auditEv {
	t.Helper()
	for i := len(*evs) - 1; i >= 0; i-- {
		if (*evs)[i].action == action {
			return (*evs)[i]
		}
	}
	t.Fatalf("no %s audit event in %+v", action, *evs)
	return auditEv{}
}

func TestRenamePlanValid(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Old name"))
	got, err := s.RenamePlan(p.ID, "  New name  ", "dana")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New name" || !got.TitleSetByHand || got.UID != p.UID || got.ID != p.ID {
		t.Fatalf("renamed = %+v", got)
	}
	if got.UpdatedAt == p.UpdatedAt {
		t.Fatal("updated_at not moved")
	}
	// The old title still resolves, through the alias, and the new one resolves.
	if r, err := s.Get("Old name"); err != nil || r.UID != p.UID {
		t.Fatalf("old title: %+v %v", r, err)
	}
	if r, err := s.Get("new name"); err != nil || r.UID != p.UID {
		t.Fatalf("new title: %+v %v", r, err)
	}
	// The title index follows the rename: the old title is free, the new one is taken.
	if _, err := s.Create(plan("Old name")); err != nil {
		t.Fatalf("old title should be free: %v", err)
	}
	var tc *ErrTitleConflict
	if _, err := s.Create(plan("NEW   name")); !errors.As(err, &tc) || tc.Existing.UID != p.UID {
		t.Fatalf("new title should be taken: %v", err)
	}
	// It survives a reopen, flag included.
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s2.Get(p.ID); err != nil || r.Title != "New name" || !r.TitleSetByHand {
		t.Fatalf("reopened = %+v %v", r, err)
	}
}

func TestRenamePlanSameTitleIsNoOp(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Keep me"))
	n := len(*evs)
	got, err := s.RenamePlan(p.ID, " Keep me ", "dana")
	if err != nil {
		t.Fatalf("own title refused: %v", err)
	}
	if got.TitleSetByHand || got.UpdatedAt != p.UpdatedAt || len(got.Aliases) != 0 || len(*evs) != n {
		t.Fatalf("no-op changed things: %+v audit %d -> %d", got, n, len(*evs))
	}
}

func TestRenamePlanCaseOnlyChange(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("keep me"))
	got, err := s.RenamePlan(p.ID, "Keep Me", "dana")
	if err != nil || got.Title != "Keep Me" || !got.TitleSetByHand {
		t.Fatalf("case change: %+v %v", got, err)
	}
	if len(got.Aliases) != 0 {
		t.Fatalf("a case change should not add an alias: %v", got.Aliases)
	}
}

func TestRenamePlanRefusesMultiLine(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("One line"))
	n := len(*evs)
	for _, bad := range []string{"two\nlines", "two\r\nlines", "a b", "a\rb"} {
		_, err := s.RenamePlan(p.ID, bad, "dana")
		var inv *ErrInvalid
		if !errors.As(err, &inv) || !strings.Contains(inv.Reason, "one line") {
			t.Fatalf("%q: err = %v", bad, err)
		}
	}
	if r, _ := s.Get(p.ID); r.Title != "One line" || r.TitleSetByHand || len(*evs) != n {
		t.Fatalf("refused rename changed the plan: %+v", r)
	}
}

func TestRenamePlanTitleLength(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Short"))
	var inv *ErrInvalid
	if _, err := s.RenamePlan(p.ID, strings.Repeat("x", MaxPlanTitleRunes+1), "dana"); !errors.As(err, &inv) {
		t.Fatalf("101 characters accepted: %v", err)
	}
	ok := strings.Repeat("é", MaxPlanTitleRunes) // counts characters, not bytes
	got, err := s.RenamePlan(p.ID, ok, "dana")
	if err != nil || got.Title != ok {
		t.Fatalf("100 characters refused: %v", err)
	}
	if MaxPlanTitleRunes != 100 {
		t.Fatalf("the plan title limit is %d, want 100", MaxPlanTitleRunes)
	}
	if _, err := s.RenamePlan(p.ID, "   ", "dana"); !errors.As(err, &inv) {
		t.Fatalf("blank accepted: %v", err)
	}
}

func TestRenamePlanDuplicateInProjectNamesOtherPlan(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, plan("Alpha"))
	b := mustCreate(t, s, plan("Beta"))
	_, err := s.RenamePlan(b.ID, "  alpha ", "dana")
	var tc *ErrTitleConflict
	if !errors.As(err, &tc) {
		t.Fatalf("err = %v", err)
	}
	if tc.Existing.UID != a.UID || !strings.Contains(err.Error(), a.ID) || !strings.Contains(err.Error(), `"Alpha"`) {
		t.Fatalf("conflict does not name the other plan: %v", err)
	}
	if r, _ := s.Get(b.ID); r.Title != "Beta" || r.TitleSetByHand {
		t.Fatalf("refused rename changed the plan: %+v", r)
	}
	// A registry row holding the title in the project blocks it too.
	mustCreate(t, s, row("Taken by a row"))
	if _, err := s.RenamePlan(b.ID, "Taken by a row", "dana"); !errors.As(err, &tc) {
		t.Fatalf("row title not protected: %v", err)
	}
}

func TestRenamePlanSameTitleInOtherProjectAllowed(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	mustCreate(t, s, plan("Shared"))
	other := plan("Other")
	other.Project = "datawatch-app"
	o := mustCreate(t, s, other)
	got, err := s.RenamePlan(o.ID, "Shared", "dana")
	if err != nil || got.Title != "Shared" || got.Project != "datawatch-app" {
		t.Fatalf("other project: %+v %v", got, err)
	}
}

func TestRenamePlanWritesAuditLine(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Before"))
	if _, err := s.RenamePlan(p.ID, "After", "dana"); err != nil {
		t.Fatal(err)
	}
	e := lastAudit(t, evs, AuditPlanRenamed)
	if e.subject != "After" || e.details["old"] != "Before" || e.details["new"] != "After" ||
		e.details["actor"] != "dana" || e.details["id"] != p.ID || e.details["kind"] != "plan" {
		t.Fatalf("audit = %+v", e)
	}
	if _, err := s.RenamePlan(p.ID, "Again", ""); err != nil {
		t.Fatal(err)
	}
	if e := lastAudit(t, evs, AuditPlanRenamed); e.details["actor"] != "system" {
		t.Fatalf("empty actor should be system: %+v", e)
	}
}

func TestRenamePlanRefusesNonPlanAndUnknown(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	r := mustCreate(t, s, row("A row"))
	var inv *ErrInvalid
	if _, err := s.RenamePlan(r.ID, "New", "dana"); !errors.As(err, &inv) || !strings.Contains(inv.Reason, "not a plan") {
		t.Fatalf("row renamed: %v", err)
	}
	if _, err := s.EditPlanGoal(r.ID, "g", "dana"); !errors.As(err, &inv) {
		t.Fatalf("row goal edited: %v", err)
	}
	if _, err := s.RenamePlan("nope", "New", "dana"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown ref: %v", err)
	}
	if _, err := s.EditPlanGoal("nope", "g", "dana"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown ref: %v", err)
	}
}

func TestUpdateTitleOfPlanFollowsRenameRules(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	p := mustCreate(t, s, plan("Via update"))
	bad := "two\nlines"
	var inv *ErrInvalid
	if _, err := s.Update(p.ID, Patch{Title: &bad}); !errors.As(err, &inv) {
		t.Fatalf("multi-line title through update: %v", err)
	}
	good := "Renamed via update"
	got, err := s.Update(p.ID, Patch{Title: &good})
	if err != nil || !got.TitleSetByHand {
		t.Fatalf("update rename: %+v %v", got, err)
	}
	// A registry row keeps its older, wider title rules and gets no flag.
	r := mustCreate(t, s, row("Row"))
	long := strings.Repeat("y", MaxPlanTitleRunes+20)
	got, err = s.Update(r.ID, Patch{Title: &long})
	if err != nil || got.TitleSetByHand {
		t.Fatalf("row update: %+v %v", got, err)
	}
}

func TestEditPlanGoal(t *testing.T) {
	s, dir, evs, _ := newTestStore(t, nil)
	in := plan("Has a goal")
	in.Goal = "first goal"
	p := mustCreate(t, s, in)
	got, err := s.EditPlanGoal(p.ID, "  second goal  ", "dana")
	if err != nil || got.Goal != "second goal" || got.TitleSetByHand || got.Title != p.Title {
		t.Fatalf("goal edit: %+v %v", got, err)
	}
	e := lastAudit(t, evs, AuditPlanGoalEdited)
	if e.details["old"] != "first goal" || e.details["new"] != "second goal" || e.details["actor"] != "dana" {
		t.Fatalf("audit = %+v", e)
	}
	// Same goal: nothing changes, nothing is audited.
	n := len(*evs)
	same, err := s.EditPlanGoal(p.ID, "second goal", "dana")
	if err != nil || same.UpdatedAt != got.UpdatedAt || len(*evs) != n {
		t.Fatalf("no-op: %+v %v", same, err)
	}
	// Too long is refused and nothing changes.
	var inv *ErrInvalid
	if _, err := s.EditPlanGoal(p.ID, strings.Repeat("g", MaxGoalRunes+1), "dana"); !errors.As(err, &inv) {
		t.Fatalf("long goal accepted: %v", err)
	}
	// It can be cleared, and it persists.
	if cleared, err := s.EditPlanGoal(p.ID, " ", "dana"); err != nil || cleared.Goal != "" {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := s2.Get(p.ID); r.Goal != "" {
		t.Fatalf("reopened goal = %q", r.Goal)
	}
}

func TestRenamedPlanTitleSurvivesSecondMigrateApply(t *testing.T) {
	s, opts, _ := migrateFixture(t)
	opts.Apply = true
	if rep, err := Migrate(opts); err != nil || len(rep.Conflicts) != 0 {
		t.Fatalf("first apply: %v %+v", err, rep.Counts())
	}
	// One plan from an arc, one from a registry row.
	arcPlan, _ := s.PlanOfAutomation("cccc3333")
	rowPlan, _ := s.PlanOf("aaaa1111")
	arcNew, rowNew := "Plans tab, renamed", "Dashboard, renamed"
	if _, err := s.RenamePlan(arcPlan.ID, arcNew, "dana"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenamePlan(rowPlan.ID, rowNew, "dana"); err != nil {
		t.Fatal(err)
	}
	n := s.Len()
	for i := 0; i < 2; i++ {
		rep, err := Migrate(opts)
		if err != nil {
			t.Fatal(err)
		}
		if s.Len() != n || len(rep.WouldCreate) != 0 || len(rep.Conflicts) != 0 {
			t.Fatalf("apply %d changed things: len %d -> %d, %+v", i+2, n, s.Len(), rep.Counts())
		}
		for ref, want := range map[string]string{arcPlan.ID: arcNew, rowPlan.ID: rowNew} {
			r, err := s.Get(ref)
			if err != nil || r.Title != want || !r.TitleSetByHand {
				t.Fatalf("apply %d: plan %s = %q (by hand %v), want %q: %v", i+2, ref, r.Title, r.TitleSetByHand, want, err)
			}
		}
	}
}
