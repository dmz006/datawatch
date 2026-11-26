package mergeshadow

import (
	"strings"
	"testing"
	"time"
)

// matchedReplay is a replay in which all seven results match.
func matchedReplay(id string, at time.Time) *Replay {
	return &Replay{
		Key: id + "-aaaaaaaaaaaa", AutomatonID: id, Project: "proj", Kind: "bug", State: ReplayDone,
		SettingsHash: "s1", ChecksHash: "c1", CreatedAt: at,
		Selection: &PlanSelection{Eligible: true, Basis: "review_passed", Tip: "t"},
		Hand:      HandMerge{Commit: "h", Base: "b", Tip: "t", At: at, Version: "1.2.4", Size: ReleaseSizePatch, Conflicts: []string{}},
		Daemon: DaemonMerge{Result: "ok", Version: "1.2.4", Size: ReleaseSizePatch, Candidate: "c",
			Push: []ReplayPushRef{{Ref: "refs/heads/main", New: "c", Commits: 3}}, Conflicts: []string{},
			Checks: []ReplayCheck{{Name: "lint", Kind: "lint", Result: "ok"}, {Name: "go-test", Kind: "test", Result: "ok"}}},
		Content: ReplayContent{Compared: true, Differences: []string{}},
		Leak: ReplayLeak{Evaluated: true, ProjectCheckRan: true, ProjectCheckPassed: true,
			ProjectCheckOutput: "clean (3 private pattern(s) loaded)", PrivatePatterns: 3, SecretScanRan: true},
		SideEffects: ReplaySideEffects{Checked: true, Found: []string{}},
		HandChecks:  map[string]string{"lint": "pass", "go-test": "pass"},
	}
}

func rowOf(t *testing.T, r *Replay, n int) Row {
	t.Helper()
	rows := r.Rows()
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7: %+v", len(rows), rows)
	}
	return rows[n-1]
}

func TestReplayAllSevenMatch(t *testing.T) {
	r := matchedReplay("a", time.Now())
	for n := 1; n <= 7; n++ {
		if got := rowOf(t, r, n); got.Result != RowMatch || got.Number != n {
			t.Errorf("row %d = %+v", n, got)
		}
	}
	if v := r.Verdict(); v != VerdictMatch {
		t.Errorf("verdict = %q", v)
	}
}

// Every difference class fails the match, in its own row.
func TestReplayEachDifferenceFailsTheMatch(t *testing.T) {
	for name, c := range map[string]struct {
		row  int
		why  string
		edit func(r *Replay)
	}{
		"work not eligible": {RowSelection, "review is pending", func(r *Replay) {
			r.Selection = &PlanSelection{Reasons: []string{"its review is pending"}}
		}},
		"no selection answer": {RowSelection, "", func(r *Replay) { r.Selection = nil }},
		"trees differ":        {RowContent, "main.go", func(r *Replay) { r.Content.Differences = []string{"main.go"} }},
		"trees not compared":  {RowContent, "", func(r *Replay) { r.Content.Compared = false }},
		"another number":      {RowVersion, "1.3.0", func(r *Replay) { r.Daemon.Version = "1.3.0" }},
		"another size":        {RowVersion, "minor", func(r *Replay) { r.Daemon.Size = ReleaseSizeMinor }},
		"no hand version":     {RowVersion, "", func(r *Replay) { r.Hand.Version = "" }},
		"a tag in the push": {RowVersion, "tag", func(r *Replay) {
			r.Daemon.Push = append(r.Daemon.Push, ReplayPushRef{Ref: "refs/tags/v1.2.4", New: "c"})
		}},
		"the hand merge tagged":       {RowVersion, "v1.2.4", func(r *Replay) { r.Hand.Tags = []string{"v1.2.4"} }},
		"daemon fail, hand pass":      {RowChecks, "go-test", func(r *Replay) { r.Daemon.Checks[1].Result = "failed" }},
		"daemon pass, hand fail":      {RowChecks, "serious", func(r *Replay) { r.HandChecks["go-test"] = "fail" }},
		"daemon skipped":              {RowChecks, "skipped", func(r *Replay) { r.Daemon.Checks[0].Result = "skipped"; r.HandChecks["lint"] = "skip" }},
		"daemon not run":              {RowChecks, "not_run", func(r *Replay) { r.Daemon.Checks[0].Result = "not_run" }},
		"hand skipped":                {RowChecks, "skip", func(r *Replay) { r.HandChecks["lint"] = "skip" }},
		"hand result missing":         {RowChecks, "go-test", func(r *Replay) { delete(r.HandChecks, "go-test") }},
		"hand check the daemon lacks": {RowChecks, "release-smoke", func(r *Replay) { r.HandChecks["release-smoke"] = "pass" }},
		"no test step in the pipeline": {RowChecks, "test step", func(r *Replay) {
			r.Daemon.Checks = r.Daemon.Checks[:1]
			delete(r.HandChecks, "go-test")
		}},
		"conflict files differ":    {RowConflicts, "x.go", func(r *Replay) { r.Hand.Conflicts = []string{"x.go"} }},
		"no push list":             {RowLeak, "", func(r *Replay) { r.Leak = ReplayLeak{PrivatePatterns: -1} }},
		"push moves another ref":   {RowLeak, "refs/heads/other", func(r *Replay) { r.Daemon.Push[0].Ref = "refs/heads/other" }},
		"no project check":         {RowLeak, "local checks", func(r *Replay) { r.Leak.ProjectCheckRan = false }},
		"project check refused":    {RowLeak, "address", func(r *Replay) { r.Leak.ProjectCheckPassed = false; r.Leak.Refusal = "address found" }},
		"private list not counted": {RowLeak, "private", func(r *Replay) { r.Leak.PrivatePatterns = -1 }},
		"private list empty":       {RowLeak, "private", func(r *Replay) { r.Leak.PrivatePatterns = 0 }},
		"secret scan off":          {RowLeak, "secret scan", func(r *Replay) { r.Leak.SecretScanRan = false }},
		"secret found":             {RowLeak, "1 secret", func(r *Replay) { r.Leak.SecretFindings = 1 }},
		"side effect":              {RowSideEffects, "pushed", func(r *Replay) { r.SideEffects.Found = []string{"a check pushed to the remote"} }},
		"side effects unchecked":   {RowSideEffects, "", func(r *Replay) { r.SideEffects.Checked = false }},
	} {
		t.Run(name, func(t *testing.T) {
			r := matchedReplay("a", time.Now())
			c.edit(r)
			got := rowOf(t, r, c.row)
			if got.Result != RowDiffers && got.Result != RowNotCompared {
				t.Errorf("row %d = %+v, want it not to match", c.row, got)
			}
			if !strings.Contains(got.Detail, c.why) {
				t.Errorf("row %d detail %q does not mention %q", c.row, got.Detail, c.why)
			}
			if v := r.Verdict(); v != VerdictDiffers {
				t.Errorf("verdict = %q, want differs", v)
			}
		})
	}
}

func TestReplayWithoutHandCheckResultsWaits(t *testing.T) {
	r := matchedReplay("a", time.Now())
	r.HandChecks = nil
	if got := rowOf(t, r, RowChecks); got.Result != RowWaiting {
		t.Errorf("checks row = %+v", got)
	}
	if v := r.Verdict(); v != VerdictWaiting {
		t.Errorf("verdict = %q", v)
	}
	r.Content.Differences = []string{"main.go"}
	if v := r.Verdict(); v != VerdictDiffers {
		t.Errorf("a difference elsewhere must not wait: %q", v)
	}
}

// askedReplay is a run that stopped because a conflict would be asked about.
func askedReplay(id string, at time.Time) *Replay {
	r := matchedReplay(id, at)
	r.Hand.Conflicts = []string{"x.go"}
	r.Daemon = DaemonMerge{Result: "failed", Asked: true, AskNote: "would ask about x.go", Conflicts: []string{"x.go"},
		Checks: []ReplayCheck{{Name: "lint", Kind: "lint", Result: "not_run"}}}
	r.Content, r.Leak = ReplayContent{}, ReplayLeak{PrivatePatterns: -1}
	return r
}

func TestReplayWouldAskNeitherCountsNorBreaks(t *testing.T) {
	r := askedReplay("q", time.Now())
	if got := rowOf(t, r, RowConflicts); got.Result != RowAsked {
		t.Errorf("conflicts row = %+v", got)
	}
	for _, n := range []int{RowContent, RowVersion, RowChecks, RowLeak} {
		if got := rowOf(t, r, n); got.Result != RowNotCompared {
			t.Errorf("row %d = %+v, want not compared", n, got)
		}
	}
	if v := r.Verdict(); v != VerdictAsked {
		t.Errorf("verdict = %q", v)
	}
	// A side effect or ineligible work is still a miss.
	r.SideEffects.Found = []string{"a tag appeared"}
	if v := r.Verdict(); v != VerdictDiffers {
		t.Errorf("verdict with a side effect = %q", v)
	}
}

func TestReplayThatCouldNotRunIsAMissWithItsReason(t *testing.T) {
	r := &Replay{State: ReplayError, Error: "the hand merge has no merge commit"}
	for _, row := range r.Rows() {
		if row.Result != RowNotCompared || !strings.Contains(row.Detail, "no merge commit") {
			t.Errorf("row %+v", row)
		}
	}
	if v := r.Verdict(); v != VerdictDiffers {
		t.Errorf("verdict = %q", v)
	}
	if v := (&Replay{State: ReplayRunning}).Verdict(); v != VerdictRunning {
		t.Errorf("running verdict = %q", v)
	}
}

func TestReplayStreak(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	same := func([]string) string { return "c1" }
	one := matchedReplay("a", at)
	two := matchedReplay("b", at.Add(time.Hour))
	two.Kind, two.Hand.Conflicts, two.Daemon.Conflicts = "feature", []string{"x"}, []string{"x"}
	if s := ReplayStreak([]*Replay{two, one}, "proj", "s1", same); s.Length != 2 || s.Total != 2 || s.Bugs != 1 || s.Features != 1 || s.WithConflict != 1 {
		t.Fatalf("two Automata of one project: %+v", s)
	}
	// "Would ask" is passed over.
	ask := askedReplay("q", at.Add(2*time.Hour))
	if s := ReplayStreak([]*Replay{one, two, ask}, "proj", "s1", same); s.Length != 2 || s.Asked != 1 {
		t.Errorf("after a would-ask: %+v", s)
	}
	for name, miss := range map[string]func(r *Replay){
		"a planted leak":           func(r *Replay) { r.Leak.SecretFindings = 1 },
		"a side effect":            func(r *Replay) { r.SideEffects.Found = []string{"x"} },
		"a shadow that failed":     func(r *Replay) { r.State, r.Error = ReplayError, "boom" },
		"still running":            func(r *Replay) { r.State = ReplayRunning },
		"waiting for hand results": func(r *Replay) { r.HandChecks = nil },
	} {
		bad := matchedReplay("z", at.Add(3*time.Hour))
		miss(bad)
		if s := ReplayStreak([]*Replay{one, two, bad}, "proj", "s1", same); s.Length != 0 || s.StoppedBy == "" {
			t.Errorf("%s: %+v, want a streak of 0", name, s)
		}
		// A later match counts again, from the miss on.
		after := matchedReplay("y", at.Add(4*time.Hour))
		if s := ReplayStreak([]*Replay{one, two, bad, after}, "proj", "s1", same); s.Length != 1 {
			t.Errorf("%s, then a match: %+v", name, s)
		}
	}
	// A pipeline or conflict setting change, and a check file change, reset it.
	if s := ReplayStreak([]*Replay{one, two}, "proj", "s2", same); s.Length != 0 || !strings.Contains(s.StoppedBy, "settings") {
		t.Errorf("settings changed: %+v", s)
	}
	if s := ReplayStreak([]*Replay{one, two}, "proj", "s1", func([]string) string { return "c2" }); s.Length != 0 || !strings.Contains(s.StoppedBy, "check") {
		t.Errorf("check files changed: %+v", s)
	}
	older := matchedReplay("o", at.Add(-time.Hour))
	older.ChecksHash = "c0"
	if s := ReplayStreak([]*Replay{older, one, two}, "proj", "s1", same); s.Length != 2 {
		t.Errorf("a match before a check change must not count: %+v", s)
	}
	other := matchedReplay("p", at.Add(5*time.Hour))
	other.Project = "elsewhere"
	other.Content.Differences = []string{"x"}
	if s := ReplayStreak([]*Replay{one, two, other}, "proj", "s1", same); s.Length != 2 || s.Total != 2 {
		t.Errorf("another project's miss: %+v", s)
	}
}
