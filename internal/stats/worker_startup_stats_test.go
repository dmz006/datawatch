package stats

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/costreport"
)

func TestBuildWorkerStartupStats_AveragesByProfileAndRole(t *testing.T) {
	attempts := []costreport.Attempt{
		{Profile: "small", Role: "coder", StartupTokens: 100, Source: costreport.StartupSourceEstimated},
		{Profile: "small", Role: "coder", StartupTokens: 300, Source: costreport.StartupSourceReported},
		{Profile: "big", Role: "reviewer", StartupTokens: 1000, Source: costreport.StartupSourceEstimated},
	}
	got := BuildWorkerStartupStats(attempts)

	if v, ok := got.AverageStartupTokensByProfile["small"]; !ok || v != 200 {
		t.Errorf("profile small average = %v (present %v), want 200", v, ok)
	}
	if v, ok := got.AverageStartupTokensByProfile["big"]; !ok || v != 1000 {
		t.Errorf("profile big average = %v (present %v), want 1000", v, ok)
	}
	if v, ok := got.AverageStartupTokensByRole["coder"]; !ok || v != 200 {
		t.Errorf("role coder average = %v (present %v), want 200", v, ok)
	}
	if v, ok := got.AverageStartupTokensByRole["reviewer"]; !ok || v != 1000 {
		t.Errorf("role reviewer average = %v (present %v), want 1000", v, ok)
	}
	if len(got.AverageStartupTokensByProfile) != 2 || len(got.AverageStartupTokensByRole) != 2 {
		t.Errorf("unexpected map sizes: profile=%d role=%d",
			len(got.AverageStartupTokensByProfile), len(got.AverageStartupTokensByRole))
	}
}

func TestBuildWorkerStartupStats_ZeroTokenAttemptsStayOutOfAverage(t *testing.T) {
	attempts := []costreport.Attempt{
		{Profile: "small", Role: "coder", StartupTokens: 400, Source: costreport.StartupSourceEstimated},
		// Recorded before startup sizes existed: counts as an attempt, not an average.
		{Profile: "small", Role: "coder"},
	}
	got := BuildWorkerStartupStats(attempts)
	if v := got.AverageStartupTokensByProfile["small"]; v != 400 {
		t.Errorf("zero-token attempt leaked into average: got %v, want 400", v)
	}
}

func TestBuildWorkerStartupStats_KeyWithOnlyNoDataIsOmitted(t *testing.T) {
	attempts := []costreport.Attempt{
		{Profile: "legacy", Role: "", StartupTokens: 0},
		{Profile: "", Role: "", StartupTokens: 50, Source: costreport.StartupSourceReported},
	}
	got := BuildWorkerStartupStats(attempts)
	if _, ok := got.AverageStartupTokensByProfile["legacy"]; ok {
		t.Error("profile with no sized attempt must be omitted, not reported as zero")
	}
	if v := got.AverageStartupTokensByProfile[costreport.UnprofiledKey]; v != 50 {
		t.Errorf("unprofiled average = %v, want 50", v)
	}
	// Both attempts ran with no role; only the sized one enters the average.
	if v := got.AverageStartupTokensByRole[costreport.NoRoleKey]; v != 50 {
		t.Errorf("no-role average = %v, want 50 (zero-token attempt excluded)", v)
	}
}

func TestBuildWorkerStartupStats_NoAttemptsIsSafe(t *testing.T) {
	for name, attempts := range map[string][]costreport.Attempt{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			got := BuildWorkerStartupStats(attempts)
			if got.AverageStartupTokensByProfile == nil || got.AverageStartupTokensByRole == nil {
				t.Fatal("maps must be non-nil so the JSON block is {} not null")
			}
			if len(got.AverageStartupTokensByProfile) != 0 || len(got.AverageStartupTokensByRole) != 0 {
				t.Errorf("expected empty maps, got %+v", got)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			want := `{"average_startup_tokens_by_profile":{},"average_startup_tokens_by_role":{}}`
			if string(raw) != want {
				t.Errorf("json = %s, want %s", raw, want)
			}
			text := WorkerStartupText(got)
			if !strings.Contains(text, "no attempts with a startup size") {
				t.Errorf("empty text missing placeholder:\n%s", text)
			}
		})
	}
}

func TestWorkerStartupText_RendersSortedAverages(t *testing.T) {
	got := WorkerStartupStats{
		AverageStartupTokensByProfile: map[string]float64{"zeta": 10, "alpha": 2.5},
		AverageStartupTokensByRole:    map[string]float64{"coder": 200},
	}
	text := WorkerStartupText(got)
	if strings.Index(text, "alpha") > strings.Index(text, "zeta") {
		t.Errorf("profile keys not sorted:\n%s", text)
	}
	for _, want := range []string{"by profile", "alpha", "2.5", "zeta", "by role", "coder", "200"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
}

func TestWorkerStartupText_ZeroValueDoesNotPanic(t *testing.T) {
	text := WorkerStartupText(WorkerStartupStats{})
	if !strings.Contains(text, "worker startup") {
		t.Errorf("unexpected text for zero value:\n%s", text)
	}
}

func TestCollectorSnapshotCarriesWorkerStartupBlock(t *testing.T) {
	c := &Collector{}
	// No hook wired: the block is still present with empty maps.
	var s SystemStats
	s.WorkerStartup = BuildWorkerStartupStats(nil)
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"worker_startup":{"average_startup_tokens_by_profile":{},"average_startup_tokens_by_role":{}}`) {
		t.Errorf("snapshot JSON missing worker_startup block: %s", raw)
	}

	c.SetWorkerStartupStatsFunc(func(st *SystemStats) {
		st.WorkerStartup = BuildWorkerStartupStats([]costreport.Attempt{
			{Profile: "p", Role: "r", StartupTokens: 42, Source: costreport.StartupSourceEstimated},
		})
	})
	var hooked SystemStats
	c.workerStartupStatsFn(&hooked)
	if hooked.WorkerStartup.AverageStartupTokensByProfile["p"] != 42 {
		t.Errorf("hook did not populate profile average: %+v", hooked.WorkerStartup)
	}
}
