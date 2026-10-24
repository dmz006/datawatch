package autonomous

import (
	"strings"
	"testing"
)

// TestEndAttributionResetEndsTaskSessionsWithPRDAndTaskReason checks the reset
// path: a task reset on daemon restart ends the task's other live session
// through the session killer, and the reason names the reset, the PRD and the
// task.
func TestEndAttributionResetEndsTaskSessionsWithPRDAndTaskReason(t *testing.T) {
	prd := &PRD{ID: "prd-reset", Story: []Story{{Tasks: []Task{{ID: "task-reset"}}}}}
	task := &prd.Story[0].Tasks[0]

	dead := map[string]bool{}
	m := &Manager{}
	m.SetSessionListerFn(func() []SessionInfo {
		return []SessionInfo{{ID: "sess-old", TaskID: task.ID, PRDID: prd.ID, Alive: !dead["sess-old"]}}
	})
	type call struct{ id, caller, reason string }
	var calls []call
	m.SetSessionKillerFn(func(id, caller, reason string) error {
		calls = append(calls, call{id, caller, reason})
		dead[id] = true
		return nil
	})

	ended := m.endLiveSessionsOfTask(prd, task, "", "autonomous.boot-reconcile.reset", taskResetEndReason(prd.ID, task.ID))

	if len(ended) != 1 || ended[0] != "sess-old" {
		t.Fatalf("ended = %v, want [sess-old]", ended)
	}
	if len(calls) != 1 {
		t.Fatalf("killer calls = %+v, want one", calls)
	}
	if calls[0].caller != "autonomous.boot-reconcile.reset" {
		t.Errorf("caller = %q, want the reset caller", calls[0].caller)
	}
	for _, want := range []string{"reset", "prd-reset", "task-reset"} {
		if !strings.Contains(calls[0].reason, want) {
			t.Errorf("reason %q does not name %q", calls[0].reason, want)
		}
	}
}
