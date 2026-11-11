package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

// finishRun marks the Automaton of the last run with a status and project
// directory holding the given results file content (nil writes no file).
func finishDependencyRun(t *testing.T, h *duHarness, status PRDStatus, results []byte) {
	t.Helper()
	id := h.c.dependencyUpdateRun.AutomatonID
	prd, ok := h.m.store.GetPRD(id)
	if !ok {
		t.Fatal("no run Automaton")
	}
	dir := t.TempDir()
	if results != nil {
		p := filepath.Join(dir, DependencyUpdateResultsPath())
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, results, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.m.mu.Lock()
	prd.Status, prd.ProjectDir = status, dir
	h.m.mu.Unlock()
}

func startedDependencyRun(t *testing.T) (*duHarness, *[]string) {
	h := newDUHarness(t, t.TempDir())
	var alerts []string
	h.c.alertFn = func(level, title, body string) string { alerts = append(alerts, level+"|"+title); return "" }
	h.tick("2026-11-06 12:00")
	h.tick("2026-11-09 05:01")
	if h.c.dependencyUpdateRun.AutomatonID == "" {
		t.Fatal("run did not start")
	}
	return h, &alerts
}

func TestDependencyUpdateReportPassNeedsAttentionAlertsOnce(t *testing.T) {
	h, alerts := startedDependencyRun(t)
	finishDependencyRun(t, h, PRDCompleted, []byte(`{"updated":["a"],"failed":["b"]}`))
	for _, at := range []string{"2026-11-09 05:05", "2026-11-09 05:10", "2026-11-09 05:15"} {
		h.tick(at)
	}
	if len(*alerts) != 1 {
		t.Fatalf("want one alert per run, got %v", *alerts)
	}
	reps := h.c.DependencyUpdateReports()
	if len(reps) != 1 || reps[0].Report.Outcome != DependencyUpdateNeedsAttention {
		t.Fatalf("reports=%+v", reps)
	}
}

func TestDependencyUpdateReportPassUpdatedIsQuiet(t *testing.T) {
	h, alerts := startedDependencyRun(t)
	finishDependencyRun(t, h, PRDCompleted, []byte(`{"updated":["a","b"]}`))
	h.tick("2026-11-09 05:05")
	if len(*alerts) != 0 {
		t.Fatalf("updated run alerted: %v", *alerts)
	}
	reps := h.c.DependencyUpdateReports()
	if len(reps) != 1 || reps[0].Report.Outcome != DependencyUpdateUpdated {
		t.Fatalf("reports=%+v", reps)
	}
}

func TestDependencyUpdateReportPassUpdatedWaitingAlertsOnce(t *testing.T) {
	h, alerts := startedDependencyRun(t)
	finishDependencyRun(t, h, PRDBlocked, []byte(`{"updated":["a"]}`))
	h.tick("2026-11-09 05:05")
	h.tick("2026-11-09 05:10")
	if len(*alerts) != 1 {
		t.Fatalf("want one alert while waiting, got %v", *alerts)
	}
	if len(h.c.DependencyUpdateReports()) != 0 {
		t.Fatal("unfinished run was reported")
	}
}

func TestDependencyUpdateReportPassNothingSilent(t *testing.T) {
	h, alerts := startedDependencyRun(t)
	finishDependencyRun(t, h, PRDCompleted, []byte(`{}`))
	h.tick("2026-11-09 05:05")
	if len(*alerts) != 0 {
		t.Fatalf("nothing run alerted: %v", *alerts)
	}
	reps := h.c.DependencyUpdateReports()
	if len(reps) != 1 || reps[0].Report.Outcome != DependencyUpdateNothing {
		t.Fatalf("reports=%+v", reps)
	}
}

func TestDependencyUpdateReportPassCouldNotStart(t *testing.T) {
	h, alerts := startedDependencyRun(t)
	finishDependencyRun(t, h, PRDFailed, nil)
	h.tick("2026-11-09 05:05")
	if len(*alerts) != 1 {
		t.Fatalf("want one alert, got %v", *alerts)
	}
}
