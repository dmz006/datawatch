package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pause"
)

// A gate repair that would change things and reset tasks, asked for while the
// operator paused new work, is held. Report mode changes nothing and still runs.
func TestRunGateRepair_OnModeIsHeldByTheOperatorPause(t *testing.T) {
	for _, mode := range []string{"on", "report"} {
		t.Run(mode, func(t *testing.T) {
			m, _, _, _ := apiFixture(t)
			prd, err := m.CreatePRD("spec", t.TempDir(), "opencode", "", EffortNormal)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "gate", Spec: "x"}}}}); err != nil {
				t.Fatal(err)
			}
			fresh, _ := m.Store().GetPRD(prd.ID)
			taskID := fresh.Story[0].Tasks[0].ID
			fresh.Story[0].Tasks[0].Status = TaskFailed
			if err := m.Store().SavePRD(fresh); err != nil {
				t.Fatal(err)
			}
			resets, reads := 0, 0
			m.SetGateRepairer(NewGateRepairer(GateRepairDeps{
				Mode: func() string { return mode },
				TaskGate: func(a, tk string) (GateRepairInput, bool) {
					reads++
					return GateRepairInput{AutomatonID: a, TaskID: tk}, true
				},
				ResetGateRun: func(string, string, string) error { resets++; return nil },
			}, filepath.Join(t.TempDir(), "budget.json")))
			ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, func() time.Time { return time.Unix(0, 0) })
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ps.Pause(pause.ScopeGlobal, "", "test pause", "operator"); err != nil {
				t.Fatal(err)
			}
			m.SetPauseGate(ps)

			one := m.RunGateRepair(context.Background(), prd.ID, taskID)
			all := m.RunGateRepairPass(context.Background())
			if len(all) != 1 {
				t.Fatalf("pass outcomes = %+v", all)
			}
			for _, o := range []GateRepairOutcome{one, all[0]} {
				held := strings.Contains(o.Err, "gate repair held") && strings.Contains(o.Err, "test pause")
				if held != (mode == "on") {
					t.Errorf("outcome %+v: held = %v in mode %q", o, held, mode)
				}
			}
			if mode == "on" && (reads != 0 || resets != 0) {
				t.Errorf("paused repair still read %d gates and reset %d", reads, resets)
			}
			if mode == "report" && reads != 2 {
				t.Errorf("report mode read %d gates, want 2", reads)
			}
		})
	}
}
