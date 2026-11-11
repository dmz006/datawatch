// dependency_update_report.go — the outcome of one weekly dependency update
// run. The run's results step writes .datawatch/dependency-update/results.json
// (updated, reverted, skipped and failed items). This file reads that shape
// and turns it into one of three outcomes plus a one-line summary, and
// decides whether the run raises an alert. The Events source in the server
// package shows the outcome; this file holds no I/O of its own.
package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DependencyUpdateOutcome is how a weekly run ended.
type DependencyUpdateOutcome string

const (
	// DependencyUpdateUpdated: at least one dependency was updated and
	// nothing needs the operator.
	DependencyUpdateUpdated DependencyUpdateOutcome = "updated"
	// DependencyUpdateNothing: nothing was outdated or changed.
	DependencyUpdateNothing DependencyUpdateOutcome = "nothing_to_update"
	// DependencyUpdateNeedsAttention: an update was dropped or failed, a
	// major waits on a protected paths decision, or the run could not start.
	DependencyUpdateNeedsAttention DependencyUpdateOutcome = "needs_attention"
)

// dependencyUpdateResultsFile is the results step's output, in the project.
const dependencyUpdateResultsFile = ".datawatch/dependency-update/results.json"

// DependencyUpdateResults is the shape of the results file. Items may be
// plain strings or objects; only their count matters here.
type DependencyUpdateResults struct {
	Updated  []json.RawMessage `json:"updated"`
	Reverted []json.RawMessage `json:"reverted"`
	Skipped  []json.RawMessage `json:"skipped"`
	Failed   []json.RawMessage `json:"failed"`
}

// DependencyUpdateRunState carries what the results file cannot say.
type DependencyUpdateRunState struct {
	// WaitingProtectedPaths: a major is waiting on a protected paths decision.
	WaitingProtectedPaths bool
	// CouldNotStart: the run never started or ended before writing results.
	CouldNotStart bool
}

// DependencyUpdateReport is the outcome of one run and its one-line summary.
type DependencyUpdateReport struct {
	Outcome DependencyUpdateOutcome `json:"outcome"`
	Summary string                  `json:"summary"`
	Updated int                     `json:"updated"`
	Dropped int                     `json:"dropped"` // reverted plus failed
	Skipped int                     `json:"skipped"`
}

// DependencyUpdateReportFromResults reads the results file content and the
// run state. Empty data with no run problem reads as nothing to update; data
// that is not valid JSON is an error and a needs attention outcome.
func DependencyUpdateReportFromResults(data []byte, state DependencyUpdateRunState) (DependencyUpdateReport, error) {
	if state.CouldNotStart {
		return DependencyUpdateReport{
			Outcome: DependencyUpdateNeedsAttention,
			Summary: "the run could not start",
		}, nil
	}
	var res DependencyUpdateResults
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return DependencyUpdateReport{
				Outcome: DependencyUpdateNeedsAttention,
				Summary: "the results could not be read",
			}, fmt.Errorf("dependency update results: %w", err)
		}
	}
	r := DependencyUpdateReport{
		Updated: len(res.Updated),
		Dropped: len(res.Reverted) + len(res.Failed),
		Skipped: len(res.Skipped),
	}
	r.Summary = fmt.Sprintf("%d updated, %d dropped, %d skipped", r.Updated, r.Dropped, r.Skipped)
	switch {
	case r.Dropped > 0 || state.WaitingProtectedPaths:
		r.Outcome = DependencyUpdateNeedsAttention
		if state.WaitingProtectedPaths {
			r.Summary += ", a major waits on a protected paths decision"
		}
	case r.Updated > 0:
		r.Outcome = DependencyUpdateUpdated
	default:
		r.Outcome = DependencyUpdateNothing
	}
	return r, nil
}

// DependencyUpdateResultsPath is the repository-relative results file path.
func DependencyUpdateResultsPath() string { return dependencyUpdateResultsFile }

// DependencyUpdateShouldAlert reports whether a run's outcome raises an
// alert: needs attention always; updated only while the same Automaton is
// waiting for the operator; nothing to update never.
func DependencyUpdateShouldAlert(r DependencyUpdateReport, waitingForOperator bool) bool {
	switch r.Outcome {
	case DependencyUpdateNeedsAttention:
		return true
	case DependencyUpdateUpdated:
		return waitingForOperator
	}
	return false
}

// DependencyUpdateAlerter raises at most one alert per run, however many
// passes look at the same finished run.
type DependencyUpdateAlerter struct {
	mu     sync.Mutex
	raised map[string]bool
	// Alert is the existing alert path (the coordinator's alert method).
	Alert func(level, title, body string)
}

// Raise sends one alert for runKey when the outcome calls for it. It returns
// true only when an alert was sent now.
func (a *DependencyUpdateAlerter) Raise(runKey, runTitle string, r DependencyUpdateReport, waitingForOperator bool) bool {
	if a == nil || a.Alert == nil || !DependencyUpdateShouldAlert(r, waitingForOperator) {
		return false
	}
	a.mu.Lock()
	if a.raised == nil {
		a.raised = map[string]bool{}
	}
	if a.raised[runKey] {
		a.mu.Unlock()
		return false
	}
	a.raised[runKey] = true
	a.mu.Unlock()
	level := "warn"
	if r.Outcome == DependencyUpdateUpdated {
		level = "info"
	}
	a.Alert(level, "Dependency update: "+runTitle+" — "+r.Summary,
		fmt.Sprintf("The weekly dependency update Automaton %q finished with outcome %s (%s). Open it to review the change or answer its question.", runTitle, r.Outcome, r.Summary))
	return true
}

// DependencyUpdateRunReport is one reported weekly run, as stored and as the
// Events source reads it.
type DependencyUpdateRunReport struct {
	Title  string                 `json:"title"`
	At     time.Time              `json:"at"`
	Report DependencyUpdateReport `json:"report"`
}
