package autonomous

import (
	"reflect"
	"testing"
)

func pgFixture(t *testing.T) []*PRD {
	a := pePRD(t, "a1", PRDCompleted, "2026-10-12T00:00:00Z",
		peDone(t, "2026-10-12T01:00:00Z", "2026-10-12T03:00:00Z"))
	a.ProjectProfile, a.AutomationID = "alpha", "arc1"
	b := pePRD(t, "b1", PRDRunning, "2026-10-13T00:00:00Z",
		peDone(t, "2026-10-13T01:00:00Z", "2026-10-13T02:00:00Z"),
		peTask(t, TaskPending, "", ""))
	b.ProjectDir, b.AutomationID = "/w/beta", "arc1"
	c := pePRD(t, "c1", PRDBlocked, "2026-10-14T00:00:00Z", peTask(t, TaskPending, "", ""))
	c.ProjectProfile = "alpha"
	d := pePRD(t, "d1", PRDApproved, "2026-10-15T00:00:00Z", peTask(t, TaskPending, "", ""))
	// archived with every task completed is finished work, no project
	e := pePRD(t, "e1", PRDArchived, "2026-10-16T00:00:00Z",
		peDone(t, "2026-10-16T01:00:00Z", "2026-10-16T04:00:00Z"))
	return []*PRD{a, b, c, d, e}
}

func pgSelect(t *testing.T, prds []*PRD) []progressRec {
	return selectProgressRecords(prds, peOpts(t), nil)
}

func sumGroups(m map[string]ProgressGroup) ProgressGroup {
	var s ProgressGroup
	for _, g := range m {
		s.PRDs += g.PRDs
		s.Done += g.Done
		s.Running += g.Running
		s.Waiting += g.Waiting
		s.Blocked += g.Blocked
		s.Tasks += g.Tasks
		s.TasksDone += g.TasksDone
		s.TasksLeft += g.TasksLeft
		s.WorkHours += g.WorkHours
	}
	return s
}

func TestProgressGroupTotalsSumsMatch(t *testing.T) {
	g := ProgressGroupTotals(pgSelect(t, pgFixture(t)))
	for name, m := range map[string]map[string]ProgressGroup{
		"project": g.ByProject, "automation": g.ByAutomation, "automaton": g.ByAutomaton,
	} {
		s := sumGroups(m)
		want := g.Totals
		if s.PRDs != want.PRDs || s.Done != want.Done || s.Running != want.Running ||
			s.Waiting != want.Waiting || s.Blocked != want.Blocked || s.Tasks != want.Tasks ||
			s.TasksDone != want.TasksDone || s.TasksLeft != want.TasksLeft {
			t.Errorf("%s: sum %+v != totals %+v", name, s, want)
		}
		peNear(t, name+" work hours", s.WorkHours, want.WorkHours)
	}
	if g.Totals.PRDs != 5 || g.Totals.Done != 2 || g.Totals.Running != 1 ||
		g.Totals.Blocked != 1 || g.Totals.Waiting != 1 {
		t.Errorf("totals = %+v", g.Totals)
	}
	if g.ByProject["alpha"].PRDs != 2 || g.ByProject["/w/beta"].PRDs != 1 {
		t.Errorf("by project = %+v", g.ByProject)
	}
	if g.ByAutomation["arc1"].PRDs != 2 || g.ByAutomation["(none)"].PRDs != 3 {
		t.Errorf("by automation = %+v", g.ByAutomation)
	}
	if len(g.ByAutomaton) != 5 || g.ByAutomaton["b1"].TasksLeft != 1 {
		t.Errorf("by automaton = %+v", g.ByAutomaton)
	}
}

func TestProgressGroupTotalsMatchesComputeProgress(t *testing.T) {
	prds := pgFixture(t)
	m := ComputeProgress(ProgressInput{PRDs: prds, Options: peOpts(t)})
	if !reflect.DeepEqual(m.Grouped.Totals, m.Totals) {
		t.Errorf("grouped totals %+v != totals %+v", m.Grouped.Totals, m.Totals)
	}
}

func TestProgressGroupTotalsNoProjectReported(t *testing.T) {
	g := ProgressGroupTotals(pgSelect(t, pgFixture(t)))
	if g.NoProject.Count != 2 || !reflect.DeepEqual(g.NoProject.IDs, []string{"d1", "e1"}) {
		t.Errorf("no project = %+v", g.NoProject)
	}
	if g.ByProject[ProjectKeyNone].PRDs != 2 {
		t.Errorf("(none) bucket = %+v", g.ByProject[ProjectKeyNone])
	}
}

func TestProgressGroupTotalsArchivedFinishedCounted(t *testing.T) {
	g := ProgressGroupTotals(pgSelect(t, pgFixture(t)))
	e := g.ByAutomaton["e1"]
	if e.Done != 1 || e.TasksDone != 1 || e.WorkHours < 2.99 || e.WorkHours > 3.01 {
		t.Errorf("archived finished work = %+v", e)
	}
	// an archived record with unfinished tasks stays excluded
	open := pePRD(t, "x1", PRDArchived, "2026-10-16T00:00:00Z", peTask(t, TaskPending, "", ""))
	g = ProgressGroupTotals(pgSelect(t, []*PRD{open}))
	if g.Totals.PRDs != 0 {
		t.Errorf("unfinished archived counted: %+v", g.Totals)
	}
}

func TestProgressGroupTotalsEmpty(t *testing.T) {
	g := ProgressGroupTotals(nil)
	if g.Totals.PRDs != 0 || g.Totals.Tasks != 0 || g.NoProject.Count != 0 {
		t.Errorf("empty totals = %+v", g)
	}
	if g.ByProject == nil || g.ByAutomation == nil || g.ByAutomaton == nil ||
		len(g.ByProject)+len(g.ByAutomation)+len(g.ByAutomaton) != 0 || g.NoProject.IDs == nil {
		t.Errorf("empty groups must be empty, non-nil: %+v", g)
	}
}
