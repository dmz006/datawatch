package autonomous

import "testing"

// TestCleanRestartFlagSetByOperatorReset checks that an operator reset with a
// clean restart sets Task.CleanRestart, and that a plain reset leaves it false.
func TestCleanRestartFlagSetByOperatorReset(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	if _, err := m.ResetTaskWithOptions(prd.ID, "task-2b", "operator", false, true); err != nil {
		t.Fatalf("clean reset: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if !got.Story[1].Tasks[1].CleanRestart {
		t.Fatal("clean reset must set CleanRestart on the reset task")
	}

	m2, prd2 := bl382RunningPRD(t)
	if _, err := m2.ResetTask(prd2.ID, "task-2b", "operator", false); err != nil {
		t.Fatalf("plain reset: %v", err)
	}
	got2, _ := m2.Store().GetPRD(prd2.ID)
	if got2.Story[1].Tasks[1].CleanRestart {
		t.Fatal("plain reset must leave CleanRestart false")
	}
}
