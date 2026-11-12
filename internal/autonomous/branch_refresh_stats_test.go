package autonomous

import "testing"

func TestSummarizeBranchRefresh(t *testing.T) {
	on, off := true, false
	prds := []*PRD{
		nil,
		{ID: "a"},
		{ID: "b", BranchRefresh: &BranchRefresh{LastResult: BranchRefreshClean}},
		{ID: "c", BranchRefresh: &BranchRefresh{LastResult: BranchRefreshConflict, Stopped: true}, BranchRefreshOverride: &on},
		{ID: "d", BranchRefresh: &BranchRefresh{LastResult: BranchRefreshError}, BranchRefreshOverride: &off},
		{ID: "e", BranchRefresh: &BranchRefresh{LastResult: BranchRefreshSkipped}},
	}
	got := SummarizeBranchRefresh(prds, true, 0)
	want := BranchRefreshStats{Enabled: true, BehindLimit: 5, Tracked: 4, Clean: 1, Conflict: 1, Skipped: 1, Error: 1, Stopped: 1, OverrideOn: 1, OverrideOff: 1}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if g := SummarizeBranchRefresh(nil, false, 9); g.BehindLimit != 9 || g.Enabled || g.Tracked != 0 {
		t.Fatalf("empty: %+v", g)
	}
}

func TestManagerBranchRefreshStats(t *testing.T) {
	m, _, _ := brManager(t, true, brGit("0\n"))
	p, _ := m.store.GetPRD("p1")
	p.BranchRefresh = &BranchRefresh{LastResult: BranchRefreshConflict, Stopped: true}
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	s := m.BranchRefreshStats()
	if !s.Enabled || s.Conflict != 1 || s.Stopped != 1 || s.BehindLimit != 5 {
		t.Fatalf("stats: %+v", s)
	}
}
