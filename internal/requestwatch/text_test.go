package requestwatch

import "testing"

func TestSummary(t *testing.T) {
	cases := []struct {
		name string
		o    Observation
		want string
	}{
		{"generating", Observation{State: StateGenerating}, "waiting on a model request: generating"},
		{"generating ignores age", Observation{State: StateGenerating, NoProgressSeconds: 900}, "waiting on a model request: generating"},
		{"idle zero", Observation{State: StateIdle}, "waiting on a model request: no progress for less than a minute"},
		{"idle 59s", Observation{State: StateIdle, NoProgressSeconds: 59}, "waiting on a model request: no progress for less than a minute"},
		{"idle 60s", Observation{State: StateIdle, NoProgressSeconds: 60}, "waiting on a model request: no progress for 1 minute"},
		{"idle 119s floors", Observation{State: StateIdle, NoProgressSeconds: 119}, "waiting on a model request: no progress for 1 minute"},
		{"idle 120s", Observation{State: StateIdle, NoProgressSeconds: 120}, "waiting on a model request: no progress for 2 minutes"},
		{"idle 31m", Observation{State: StateIdle, NoProgressSeconds: 31*60 + 30}, "waiting on a model request: no progress for 31 minutes"},
		{"unreachable zero", Observation{State: StateUnreachable}, "waiting on a model request: node unreachable, no progress for less than a minute"},
		{"unreachable 1m", Observation{State: StateUnreachable, NoProgressSeconds: 61}, "waiting on a model request: node unreachable, no progress for 1 minute"},
		{"unreachable 5m", Observation{State: StateUnreachable, NoProgressSeconds: 300}, "waiting on a model request: node unreachable, no progress for 5 minutes"},
		{"unknown", Observation{State: StateUnknown}, "waiting on a model request: no reading yet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Summary(c.o); got != c.want {
				t.Fatalf("Summary = %q, want %q", got, c.want)
			}
		})
	}
}

func TestExplainEveryState(t *testing.T) {
	want := map[State]string{
		StateGenerating:  ExplainGenerating,
		StateIdle:        ExplainIdle,
		StateUnreachable: ExplainUnreachable,
		StateUnknown:     ExplainUnknown,
	}
	seen := map[string]bool{}
	for s, w := range want {
		got := Explain(s)
		if got == "" || got != w {
			t.Fatalf("Explain(%q) = %q, want %q", s, got, w)
		}
		if seen[got] {
			t.Fatalf("explanation for %q repeats another state", s)
		}
		seen[got] = true
	}
}
