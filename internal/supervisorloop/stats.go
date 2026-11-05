package supervisorloop

import "time"

// Stats is the supervisor loop's observability snapshot: whether the loop is
// on, how many jobs it has and how many are enabled, and a count of the
// stored run records by outcome. LastRunAgeSeconds is -1 when no run has ever
// been recorded.
type Stats struct {
	Enabled           bool  `json:"enabled"`
	Jobs              int   `json:"jobs"`
	JobsEnabled       int   `json:"jobs_enabled"`
	Runs              int   `json:"runs"`
	RunsOK            int   `json:"runs_ok"`
	RunsValidatorFail int   `json:"runs_validator_failed"`
	RunsEscalated     int   `json:"runs_escalated"`
	RunsError         int   `json:"runs_error"`
	Retries           int   `json:"retries"`
	LastRunAgeSeconds int64 `json:"last_run_age_seconds"`
}

// ComputeStats builds the snapshot from the live settings and the stored run
// records (oldest first, as the store lists them).
func ComputeStats(set Settings, recs []RunRecord, now time.Time) Stats {
	st := Stats{Enabled: set.Enabled, LastRunAgeSeconds: -1}
	for _, n := range StepNames {
		st.Jobs++
		if set.Enabled && set.Steps[n] {
			st.JobsEnabled++
		}
	}
	for range ModelJobNames {
		st.Jobs++
		if set.Enabled {
			st.JobsEnabled++
		}
	}
	var last time.Time
	for _, r := range recs {
		st.Runs++
		switch r.Outcome {
		case OutcomeOK:
			st.RunsOK++
		case OutcomeValidatorFailed:
			st.RunsValidatorFail++
		case OutcomeEscalated:
			st.RunsEscalated++
		case OutcomeError:
			st.RunsError++
		}
		if r.Attempts > 1 {
			st.Retries += r.Attempts - 1
		}
		if r.Started.After(last) {
			last = r.Started
		}
	}
	if !last.IsZero() {
		age := int64(now.Sub(last).Seconds())
		if age < 0 {
			age = 0
		}
		st.LastRunAgeSeconds = age
	}
	return st
}
