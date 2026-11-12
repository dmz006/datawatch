package supervisorloop

import (
	"testing"
	"time"
)

func TestComputeStats(t *testing.T) {
	set := DefaultSettings()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	recs := []RunRecord{
		{Job: "draft_spec", Outcome: OutcomeOK, Attempts: 1, Started: now.Add(-time.Hour)},
		{Job: "pick_backend", Outcome: OutcomeValidatorFailed, Attempts: 3, Started: now.Add(-time.Minute)},
		{Job: "pick_backend", Outcome: OutcomeEscalated, Attempts: 2, Started: now.Add(-2 * time.Hour)},
		{Job: "deploy_after_ship", Outcome: OutcomeError, Started: now.Add(-3 * time.Hour)},
	}
	st := ComputeStats(set, recs, now)
	if !st.Enabled || st.Jobs != len(StepNames)+len(ModelJobNames) || st.JobsEnabled != st.Jobs {
		t.Errorf("jobs: %+v", st)
	}
	if st.Runs != 4 || st.RunsOK != 1 || st.RunsValidatorFail != 1 || st.RunsEscalated != 1 || st.RunsError != 1 {
		t.Errorf("counts: %+v", st)
	}
	if st.Retries != 3 {
		t.Errorf("retries = %d, want 3", st.Retries)
	}
	if st.LastRunAgeSeconds != 60 {
		t.Errorf("age = %d, want 60", st.LastRunAgeSeconds)
	}
}

func TestComputeStatsEmptyAndDisabled(t *testing.T) {
	set := DefaultSettings()
	set.Enabled = false
	st := ComputeStats(set, nil, time.Now())
	if st.Enabled || st.JobsEnabled != 0 || st.Runs != 0 || st.LastRunAgeSeconds != -1 {
		t.Errorf("%+v", st)
	}
}
