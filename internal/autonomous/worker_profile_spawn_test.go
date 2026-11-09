package autonomous

import (
	"context"
	"testing"
)

func launchAttempt(t *testing.T, m *Manager, prd *PRD, task *Task) (TaskAttempt, bool) {
	t.Helper()
	var run attemptRun
	_, applied := m.applyWorkerProfileToRun(prd, task, &run)
	run.Backend, run.Model = "pi-prompt", "qwen3"
	m.recordFailedAttempt(context.Background(), prd, task, run, attemptFailure{Outcome: "failed"})
	fresh, _ := m.Store().GetPRD(prd.ID)
	atts := fresh.Story[0].Tasks[0].Attempts
	if len(atts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(atts))
	}
	return atts[0], applied
}

func TestAppliedLaunchAttemptCarriesProfileAndStartupSize(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "minimal", "")
	a, applied := launchAttempt(t, m, prd, task)
	if !applied {
		t.Fatal("named built-in profile should be applied")
	}
	if a.WorkerProfile != "minimal" {
		t.Errorf("attempt profile = %q, want minimal", a.WorkerProfile)
	}
	if a.StartupTokens <= 0 {
		t.Errorf("attempt startup tokens = %d, want > 0", a.StartupTokens)
	}
}

func TestUnprofiledLaunchAttemptHasEmptyNameAndNoSize(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "", "")
	a, applied := launchAttempt(t, m, prd, task)
	if applied {
		t.Fatal("no configured profile: none should be applied")
	}
	if a.WorkerProfile != "" || a.StartupTokens != 0 {
		t.Errorf("attempt = profile %q tokens %d, want empty and 0", a.WorkerProfile, a.StartupTokens)
	}
}
