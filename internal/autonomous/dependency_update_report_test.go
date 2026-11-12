package autonomous

import "testing"

func TestDependencyUpdateReportOutcomes(t *testing.T) {
	cases := []struct {
		name  string
		data  string
		state DependencyUpdateRunState
		want  DependencyUpdateOutcome
		sum   string
	}{
		{"updated", `{"updated":["a","b"],"skipped":["c"]}`, DependencyUpdateRunState{}, DependencyUpdateUpdated, "2 updated, 0 dropped, 1 skipped"},
		{"nothing", `{}`, DependencyUpdateRunState{}, DependencyUpdateNothing, "0 updated, 0 dropped, 0 skipped"},
		{"empty file", ``, DependencyUpdateRunState{}, DependencyUpdateNothing, "0 updated, 0 dropped, 0 skipped"},
		{"dropped", `{"updated":["a"],"reverted":["b"],"failed":[{"name":"c"}]}`, DependencyUpdateRunState{}, DependencyUpdateNeedsAttention, "1 updated, 2 dropped, 0 skipped"},
		{"protected", `{"updated":["a"]}`, DependencyUpdateRunState{WaitingProtectedPaths: true}, DependencyUpdateNeedsAttention, "1 updated, 0 dropped, 0 skipped, a major waits on a protected paths decision"},
		{"not started", ``, DependencyUpdateRunState{CouldNotStart: true}, DependencyUpdateNeedsAttention, "the run could not start"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := DependencyUpdateReportFromResults([]byte(c.data), c.state)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != c.want || r.Summary != c.sum {
				t.Fatalf("got %s %q, want %s %q", r.Outcome, r.Summary, c.want, c.sum)
			}
		})
	}
	r, err := DependencyUpdateReportFromResults([]byte(`{bad`), DependencyUpdateRunState{})
	if err == nil || r.Outcome != DependencyUpdateNeedsAttention {
		t.Fatalf("bad json: %v %s", err, r.Outcome)
	}
}

func TestDependencyUpdateAlertSilenceAndOncePerRun(t *testing.T) {
	var got []string
	a := &DependencyUpdateAlerter{Alert: func(level, title, body string) { got = append(got, level+"|"+title) }}
	nothing := DependencyUpdateReport{Outcome: DependencyUpdateNothing, Summary: "0 updated, 0 dropped, 0 skipped"}
	updated := DependencyUpdateReport{Outcome: DependencyUpdateUpdated, Summary: "1 updated, 0 dropped, 0 skipped"}
	attn := DependencyUpdateReport{Outcome: DependencyUpdateNeedsAttention, Summary: "0 updated, 1 dropped, 0 skipped"}

	if a.Raise("run1", "Weekly", nothing, true) || a.Raise("run1", "Weekly", updated, false) {
		t.Fatal("silent outcomes alerted")
	}
	if len(got) != 0 {
		t.Fatalf("alerts: %v", got)
	}
	if !a.Raise("run2", "Weekly", attn, false) {
		t.Fatal("needs attention did not alert")
	}
	for i := 0; i < 3; i++ { // later passes over the same run
		if a.Raise("run2", "Weekly", attn, false) {
			t.Fatal("second alert for the same run")
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one alert, got %v", got)
	}
	if !a.Raise("run3", "Weekly", updated, true) || len(got) != 2 {
		t.Fatalf("updated and waiting should alert once: %v", got)
	}
	if a.Raise("run4", "Weekly", attn, false) == false || len(got) != 3 {
		t.Fatal("a new run should alert again")
	}
}
