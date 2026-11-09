package main

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// guardReportMgr returns a fixed interruption report.
type guardReportMgr struct {
	rep autonomous.RestartInterruption
}

func (g *guardReportMgr) RestartInterruptions() autonomous.RestartInterruption { return g.rep }
func (g *guardReportMgr) SetLaunchDraining(bool)                               {}

// forcedReport is a report that needs force: a planning Automaton, a verifying
// task, a running judge and a live session, none covered.
func forcedReport() autonomous.RestartInterruption {
	return autonomous.RestartInterruption{
		Policy:           config.RestartPolicyImmediate,
		QuietTimeoutSec:  600,
		OnTimeout:        config.RestartOnTimeoutProceed,
		PlanningAutomata: []autonomous.PlanningAutomaton{{ID: "auto-plan-1", Title: "Planner"}},
		VerifyingTasks:   []autonomous.VerifyingTask{{PRDID: "prd-v", TaskID: "task-v", SessionID: "sess-w", VerifierSessionID: "sess-ver"}},
		RunningJudges:    []autonomous.RunningJudge{{SessionID: "sess-judge", UnitID: "unit-j"}},
		LiveSessions:     []string{"sess-a", "sess-b"},
		LiveSessionCnt:   2,
		PlanningCnt:      1,
		VerifyingTaskCnt: 1,
		RunningJudgeCnt:  1,
		NeedsForce:       true,
	}
}

func guardWith(rep autonomous.RestartInterruption, restart func(bool)) func(bool) {
	mgr := &guardReportMgr{rep: rep}
	return guardRestartForce(restart,
		func() restartPolicyManager { return mgr },
		func() restartPolicySettings { return restartSettingsFrom(config.AutonomousConfig{}) })
}

func TestRestartGuardReportListsSessions(t *testing.T) {
	out := restartGuardReport(forcedReport())
	for _, want := range []string{
		"auto-plan-1", "Planner",
		"prd-v/task-v", "sess-w", "sess-ver",
		"sess-judge", "unit-j",
		"sess-a, sess-b",
		"immediate", "force",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestRestartGuardRefusesWithoutForce(t *testing.T) {
	calls := 0
	g := guardWith(forcedReport(), func(bool) { calls++ })
	g(false)
	if calls != 0 {
		t.Fatalf("restart called %d times without force; want 0", calls)
	}
}

func TestRestartGuardProceedsWithForce(t *testing.T) {
	var gotForce []bool
	g := guardWith(forcedReport(), func(f bool) { gotForce = append(gotForce, f) })
	g(true)
	if len(gotForce) != 1 || !gotForce[0] {
		t.Fatalf("restart calls with force = %v; want one call with force=true", gotForce)
	}
}

func TestRestartGuardProceedsWithNoForceWhenNotNeeded(t *testing.T) {
	rep := autonomous.RestartInterruption{Policy: config.RestartPolicyImmediate, NeedsForce: false}
	var gotForce []bool
	g := guardWith(rep, func(f bool) { gotForce = append(gotForce, f) })
	g(false)
	if len(gotForce) != 1 || gotForce[0] {
		t.Fatalf("restart calls = %v; want one call with force=false", gotForce)
	}
}

func TestRestartGuardLegacyWrapperRefusesWhenNeedsForce(t *testing.T) {
	calls := 0
	mgr := &guardReportMgr{rep: forcedReport()}
	g := guardRestart(func() { calls++ }, func() restartPolicyManager { return mgr },
		func() restartPolicySettings { return restartSettingsFrom(config.AutonomousConfig{}) })
	g()
	if calls != 0 {
		t.Fatalf("legacy guardRestart ran restart %d times with NeedsForce; want 0", calls)
	}
}
