// Tests for the judge verdict and final gate events in
// lifecycle_emit_judge.go.

package autonomous

import (
	"strings"
	"testing"
	"time"
)

var judgeKinds = []string{KindJudgeVerdictBlock, KindJudgeVerdictWarn, KindJudgeVerdictPass, KindFinalGateFailed}

func judgeManager(t *testing.T) (*Manager, *PRD, *collector) {
	t.Helper()
	m := newEmitManager(t, DefaultConfig(), nil)
	prd := newEmitPRD(t, m, PRDActive)
	col := &collector{}
	cancel := m.Subscribe(LifecycleFilter{Kinds: judgeKinds}, col.add)
	t.Cleanup(func() {
		cancel()
		m.lifecycleBus().Close()
	})
	return m, prd, col
}

func TestJudgeVerdictEvent(t *testing.T) {
	want := map[string]string{
		"block": KindJudgeVerdictBlock,
		"warn":  KindJudgeVerdictWarn,
		"pass":  KindJudgeVerdictPass,
	}
	for outcome, kind := range want {
		t.Run(outcome, func(t *testing.T) {
			m, prd, col := judgeManager(t)
			taskID := prd.Story[0].Tasks[0].ID
			v := GuardrailVerdict{Guardrail: "rules", Outcome: outcome, Summary: "s", Issues: []string{"i"}, VerdictAt: time.Now()}
			if err := m.recordGuardrailVerdict(prd, taskID, v); err != nil {
				t.Fatalf("recordGuardrailVerdict: %v", err)
			}
			time.Sleep(50 * time.Millisecond)
			evs := col.snapshot()
			if len(evs) != 1 {
				t.Fatalf("events = %d, want exactly 1: %+v", len(evs), evs)
			}
			ev := evs[0]
			if ev.Kind != kind || ev.Level != LevelTask || ev.TaskID != taskID || ev.PRDID != prd.ID {
				t.Errorf("event = %+v, want kind %s at task level for %s", ev, kind, taskID)
			}
			if ev.Verdict == nil || ev.Verdict.Outcome != outcome || ev.Decision == nil {
				t.Errorf("payload = verdict %+v decision %+v, want the recorded verdict and decision", ev.Verdict, ev.Decision)
			}
		})
	}
	t.Run("automaton level and unknown outcome", func(t *testing.T) {
		m, prd, col := judgeManager(t)
		if err := m.recordGuardrailVerdict(prd, prd.ID, GuardrailVerdict{Guardrail: "rules", Outcome: "block"}); err != nil {
			t.Fatal(err)
		}
		if err := m.recordGuardrailVerdict(prd, prd.ID, GuardrailVerdict{Guardrail: "rules", Outcome: "odd"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		evs := col.snapshot()
		if len(evs) != 1 || evs[0].Level != LevelPRD || evs[0].TaskID != "" {
			t.Fatalf("events = %+v, want one Automaton-level block event", evs)
		}
	})
}

func TestFinalGateFailedEvent(t *testing.T) {
	m, prd, col := judgeManager(t)
	task := &prd.Story[0].Tasks[0]
	m.finishScriptGateTask(prd, task, "", "gate ran", "build broke", false)
	time.Sleep(50 * time.Millisecond)
	evs := col.snapshot()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want exactly 1: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Kind != KindFinalGateFailed || ev.TaskID != task.ID || ev.PRDID != prd.ID || !strings.Contains(ev.Details, "build broke") {
		t.Errorf("event = %+v, want %s for %s naming the reason", ev, KindFinalGateFailed, task.ID)
	}

	m2, prd2, col2 := judgeManager(t)
	m2.finishScriptGateTask(prd2, &prd2.Story[0].Tasks[0], "", "gate ran", "", true)
	time.Sleep(50 * time.Millisecond)
	if n := len(col2.snapshot()); n != 0 {
		t.Errorf("a passing gate emitted %d events, want 0", n)
	}
}
