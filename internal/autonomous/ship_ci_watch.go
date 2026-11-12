// Ship CI watch — runs after a successful ship step. Watches CI for two refs:
// the pushed commit and the tag. Reuses the ci-watch run listing and polling
// (ci_watch.go) with a ref and a workflow filter. Per the project's
// ShipPolicy.CIWatch mode: off skips, shadow records a failure only, on also
// raises an alert on the triage record.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const shipCIWatchTriageSource = "ship_ci_watch"

// CIWatchOutcome is what watching one ref found.
type CIWatchOutcome string

const (
	CIOutcomeSuccess CIWatchOutcome = "success"
	CIOutcomeFailure CIWatchOutcome = "failure"
	CIOutcomeRunning CIWatchOutcome = "still running"
	CIOutcomeTimeout CIWatchOutcome = "timeout"
)

// CIRunLister lists CI runs for a ref. The gh-backed implementation is the
// default; tests stub it.
type CIRunLister interface {
	ListRuns(ctx context.Context, ownerRepo string, ref CIRef) ([]CIWatchRun, error)
}

// ghRunLister lists runs through the gh CLI.
type ghRunLister struct{}

func (ghRunLister) ListRuns(ctx context.Context, ownerRepo string, ref CIRef) ([]CIWatchRun, error) {
	return defaultCIRunsForRef(ctx, ownerRepo, ref)
}

// ShipCIWatchOptions configures one watch.
type ShipCIWatchOptions struct {
	OwnerRepo string
	Commit    string
	Tag       string   // "" skips the tag watch
	Workflows []string // empty keeps every run
	Poll      time.Duration
	// Timeout bounds the wait for runs to finish. Zero checks once and
	// reports "still running" when they have not finished.
	Timeout time.Duration
}

// RefCIResult is the outcome for one watched ref.
type RefCIResult struct {
	Ref     CIRef
	Outcome CIWatchOutcome
	Runs    []CIWatchRun
	Failed  []CIWatchRun
	Err     error
}

// WatchShipCI watches CI for the pushed commit, then the tag.
func WatchShipCI(ctx context.Context, l CIRunLister, o ShipCIWatchOptions) []RefCIResult {
	refs := []CIRef{{Commit: o.Commit}}
	if o.Tag != "" {
		refs = append(refs, CIRef{Tag: o.Tag})
	}
	out := make([]RefCIResult, 0, len(refs))
	for _, ref := range refs {
		out = append(out, watchRef(ctx, l, o, ref))
	}
	return out
}

func watchRef(ctx context.Context, l CIRunLister, o ShipCIWatchOptions, ref CIRef) RefCIResult {
	res := RefCIResult{Ref: ref}
	if ref.Commit == "" && ref.Tag == "" || o.OwnerRepo == "" {
		res.Err = fmt.Errorf("ship ci-watch: no repository or ref to watch")
		res.Outcome = CIOutcomeFailure
		return res
	}
	d := ciWatchDeps{poll: o.Poll}
	if d.poll <= 0 {
		d.poll = defaultCIPollInterval
	}
	d.runs = func(ctx context.Context, ownerRepo, _ string) ([]CIWatchRun, error) {
		runs, err := l.ListRuns(ctx, ownerRepo, ref)
		if err != nil {
			return nil, err
		}
		return filterCIRunsByWorkflow(runs, o.Workflows), nil
	}
	deadline := time.Now().Add(o.Timeout)
	runs, settled, err := pollCIRuns(ctx, d, o.OwnerRepo, ref.String(), deadline)
	res.Runs = runs
	if err != nil {
		res.Err = err
		res.Outcome = CIOutcomeFailure
		return res
	}
	if !settled {
		if o.Timeout > 0 {
			res.Outcome = CIOutcomeTimeout
		} else {
			res.Outcome = CIOutcomeRunning
		}
		return res
	}
	failed, infra := classifyCIRuns(runs)
	res.Failed = append(failed, infra...)
	if len(res.Failed) > 0 {
		res.Outcome = CIOutcomeFailure
	} else {
		res.Outcome = CIOutcomeSuccess
	}
	return res
}

// runShipCIWatch is the completion-path hook that follows a ship step. It does
// nothing when the CI watch is off, the ship step failed, or nothing was pushed.
func (m *Manager) runShipCIWatch(prd *PRD, p ShipPolicy, ship *ShipResult, l CIRunLister) []RefCIResult {
	if prd == nil || ship == nil || ship.Err != nil || p.CIWatch == config.ChecksModeOff {
		return nil
	}
	if p.Push != config.ChecksModeOn && p.Tag != config.ChecksModeOn {
		return nil // nothing was actually pushed
	}
	if l == nil {
		l = ghRunLister{}
	}
	d := m.ciWatchDeps()
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout+time.Minute)
	defer cancel()
	sha, origin, _, err := d.git(ctx, prd.ProjectDir)
	ownerRepo := githubOwnerRepo(origin)
	if err != nil || ownerRepo == "" {
		m.recordShipCIWatch(prd, p, []RefCIResult{{Ref: CIRef{Commit: sha}, Outcome: CIOutcomeFailure,
			Err: fmt.Errorf("cannot resolve the GitHub repository for CI watch")}})
		return nil
	}
	o := ShipCIWatchOptions{OwnerRepo: ownerRepo, Commit: sha, Workflows: p.CIWorkflows, Poll: d.poll, Timeout: d.timeout}
	if p.Tag == config.ChecksModeOn {
		o.Tag = ship.Tag
	}
	res := WatchShipCI(ctx, l, o)
	m.recordShipCIWatch(prd, p, res)
	return res
}

func (m *Manager) recordShipCIWatch(prd *PRD, p ShipPolicy, results []RefCIResult) {
	failed := false
	lines := make([]string, 0, len(results))
	for _, r := range results {
		line := fmt.Sprintf("%s: %s", r.Ref, r.Outcome)
		if r.Err != nil {
			line += " (" + r.Err.Error() + ")"
		}
		if len(r.Failed) > 0 {
			line += " — " + strings.ReplaceAll(strings.TrimSpace(ciRunList(r.Failed)), "\n", "; ")
		}
		if r.Outcome == CIOutcomeFailure {
			failed = true
		}
		lines = append(lines, line)
	}
	label := "ship ci-watch"
	if failed {
		label = "ship ci-watch failed"
	}
	m.recordCheckFindings(prd, p.CIWatch, shipCIWatchTriageSource, label, lines, failed)
}
