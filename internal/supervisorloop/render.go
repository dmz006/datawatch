// Shared text renderings for the supervisor loop's REST bodies, so every
// surface (the CLI's supervisor-loop reads, the comm verbs, the tests)
// renders the daemon's reply the same way, line for line.

package supervisorloop

import (
	"fmt"
	"io"
	"strings"
)

// JobView mirrors one job of the GET /api/supervisor-loop/jobs body.
type JobView struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Style       string `json:"style,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Model       string `json:"model,omitempty"`
	Enabled     bool   `json:"enabled"`
	LastOutcome string `json:"last_outcome"`
	LastRun     string `json:"last_run,omitempty"`
}

// RunView mirrors one run of the GET /api/supervisor-loop/runs body.
type RunView struct {
	Job               string   `json:"job"`
	Style             string   `json:"style,omitempty"`
	Backend           string   `json:"backend,omitempty"`
	Model             string   `json:"model,omitempty"`
	Outcome           string   `json:"outcome"`
	Result            string   `json:"result,omitempty"`
	ValidatorFailures []string `json:"validator_failures"`
	Retries           int      `json:"retries"`
	Escalations       int      `json:"escalations"`
	DurationMS        int64    `json:"duration_ms"`
	Started           string   `json:"started"`
}

// JobsBody is the GET /api/supervisor-loop/jobs body.
type JobsBody struct {
	Enabled bool      `json:"enabled"`
	Jobs    []JobView `json:"jobs"`
}

// RunsBody is the GET /api/supervisor-loop/runs body.
type RunsBody struct {
	Runs []RunView `json:"runs"`
}

// JobOnOff renders one job's enabled state.
func JobOnOff(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

// RenderJobs writes the jobs read: the loop's enabled state, then one line
// per job with its kind, the model job's style/backend/model, its enabled
// state and its last outcome with the run instant.
func RenderJobs(w io.Writer, b *JobsBody) {
	state := "on"
	if !b.Enabled {
		state = "off"
	}
	_, _ = fmt.Fprintf(w, "Supervisor loop %s; %d jobs.\n", state, len(b.Jobs))
	for _, j := range b.Jobs {
		line := fmt.Sprintf("%s: %s, %s", j.Name, j.Kind, JobOnOff(j.Enabled))
		if j.Style != "" {
			line += ", " + j.Style
		}
		if j.Backend != "" {
			line += ", " + j.Backend
		}
		if j.Model != "" {
			line += ", " + j.Model
		}
		if j.LastOutcome != "" {
			line += ", last " + j.LastOutcome
		}
		if j.LastRun != "" {
			line += " at " + j.LastRun
		}
		_, _ = fmt.Fprintln(w, line)
	}
}

// RenderRuns writes the runs read, newest first: one line per run with its
// outcome, result, validator failures, retries, escalations, duration and
// started instant.
func RenderRuns(w io.Writer, b *RunsBody) {
	if len(b.Runs) == 0 {
		_, _ = fmt.Fprintln(w, "No run records.")
		return
	}
	for i, r := range b.Runs {
		line := fmt.Sprintf("%d. %s %s", i+1, r.Job, r.Outcome)
		if r.Result != "" {
			line += ": " + r.Result
		}
		if len(r.ValidatorFailures) > 0 {
			line += " [validator: " + strings.Join(r.ValidatorFailures, "; ") + "]"
		}
		if r.Retries > 0 {
			line += fmt.Sprintf(" [%d retries]", r.Retries)
		}
		if r.Escalations > 0 {
			line += fmt.Sprintf(" [%d escalations]", r.Escalations)
		}
		if r.DurationMS > 0 {
			line += fmt.Sprintf(" (%d ms)", r.DurationMS)
		}
		if r.Started != "" {
			line += " at " + r.Started
		}
		_, _ = fmt.Fprintln(w, line)
	}
}

// RenderStats writes the stats snapshot as plain text, one fact per line.
func RenderStats(w io.Writer, st *Stats) {
	state := "on"
	if !st.Enabled {
		state = "off"
	}
	_, _ = fmt.Fprintf(w, "Supervisor loop %s; %d of %d jobs enabled.\n", state, st.JobsEnabled, st.Jobs)
	_, _ = fmt.Fprintf(w, "Runs stored: %d (ok %d, validator failed %d, escalated %d, error %d); retries used: %d.\n",
		st.Runs, st.RunsOK, st.RunsValidatorFail, st.RunsEscalated, st.RunsError, st.Retries)
	if st.LastRunAgeSeconds < 0 {
		_, _ = fmt.Fprintln(w, "No run yet.")
		return
	}
	_, _ = fmt.Fprintf(w, "Last run %d seconds ago.\n", st.LastRunAgeSeconds)
}
