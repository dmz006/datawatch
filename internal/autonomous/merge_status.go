// Automaton merge status: the read model behind GET /api/autonomous/merge.
// It lists the completed Automata that have a work branch, with the state the
// merge-pending pass last compared them at. Every state shown comes from the
// Coordinator's in-memory comparison cache, so a read never runs git and
// nothing here adds a persisted field to an Automaton.
package autonomous

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// ErrMergeNoBaseRepo is returned by the safe-to-delete read when no worktree
// base repo is configured for Automata, so there is no repository to inspect.
var ErrMergeNoBaseRepo = errors.New("no worktree base repo is configured for Automata")

// MergeStatusEntry is one completed Automaton with a work branch.
type MergeStatusEntry struct {
	AutomatonID string `json:"automaton_id"`
	Name        string `json:"name"`
	Branch      string `json:"branch"`
	// State is the cached merge comparison state; unknown until the pass has
	// compared the branch.
	State        string `json:"state"`
	CommitsAhead int    `json:"commits_ahead"`
	// Base is the default branch the comparison used; empty until compared.
	Base string `json:"base,omitempty"`
	// PendingSince is when the Automaton was first seen merge pending; null
	// when it is not pending or the pass has not seen it yet.
	PendingSince *time.Time `json:"pending_since"`
	// AlertedAt is when the merge-pending alert fired; null until it does.
	AlertedAt *time.Time `json:"alerted_at"`
	// WouldAlert is the shadow-mode flag; absent outside shadow mode.
	WouldAlert *bool `json:"would_alert,omitempty"`
}

// MergeStatus is the body of GET /api/autonomous/merge.
type MergeStatus struct {
	// Mode is the merge_pending_mode in effect: off, shadow or on.
	Mode         string `json:"mode"`
	AlertMinutes int    `json:"alert_minutes"`
	// Automata is empty when the mode is off.
	Automata []MergeStatusEntry `json:"automata"`
	// ShadowDecisions is newest first; empty unless the mode is shadow.
	ShadowDecisions []MergeShadowDecision `json:"shadow_decisions"`
	// PipelineRuns lists the merge pipeline runs, newest first, each with the
	// steps it ran. It is filled whatever the merge-pending mode is.
	PipelineRuns []MergePipelineRun `json:"pipeline_runs"`
	// MergeLock is the lock held on the merge base repository by a running
	// merge; nil when nothing is held. It is filled whatever the mode is.
	MergeLock *MergeLockStatus `json:"merge_lock,omitempty"`
	// LastValidator is the release-bump validator's result from the newest
	// run that has one; nil when no run has one, so nothing is shown.
	LastValidator *MergeValidatorResult `json:"last_validator,omitempty"`
}

// latestMergeValidator returns a copy of the validator result of the first
// run, in the newest-first order of runs, that has one. It returns nil when
// no run has a validator result.
func latestMergeValidator(runs []MergePipelineRun) *MergeValidatorResult {
	for i := range runs {
		if runs[i].Validator != nil {
			v := *runs[i].Validator
			return &v
		}
	}
	return nil
}

// heldMergeLock returns the lock on the merge base repository, or nil when
// no repository is configured, the store cannot be read, or nothing is held.
// A lock entry that names no run and no Automaton holds nothing, so it is not
// reported.
func (m *Manager) heldMergeLock() *MergeLockStatus {
	repo := m.MergeBaseRepo()
	if repo == "" {
		return nil
	}
	locks, err := m.MergeLocks()
	if err != nil {
		return nil
	}
	st, ok := locks.Status(repo)
	if !ok || (st.RunID == "" && st.AutomatonID == "") {
		return nil
	}
	return &st
}

// mergeCached returns the cached comparison for branch, without running git.
func (c *Coordinator) mergeCached(branch string) (MergeCompare, bool) {
	s := &c.mp
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[branch]
	return e.cmp, ok
}

// mergeSince returns when the shadow episode of the Automaton began.
func (c *Coordinator) mergeSince(prdID string) (time.Time, bool) {
	s := &c.mp
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.since[prdID]
	return t, ok
}

// MergeStatus assembles the merge status from the stored Automata and the
// Coordinator's in-memory cache. Before the Coordinator has started, or while
// the mode is off, the Automaton list is empty.
func (m *Manager) MergeStatus() MergeStatus {
	cfg := m.Config()
	mode := cfg.Automation.EffectiveMergePendingMode()
	out := MergeStatus{
		Mode:            mode,
		AlertMinutes:    cfg.Automation.EffectiveMergePendingAlertMinutes(),
		Automata:        []MergeStatusEntry{},
		ShadowDecisions: []MergeShadowDecision{},
		PipelineRuns:    m.MergeRuns(0),
		MergeLock:       m.heldMergeLock(),
	}
	out.LastValidator = latestMergeValidator(out.PipelineRuns)
	if mode != config.MergePendingModeShadow && mode != config.MergePendingModeOn {
		return out
	}
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil {
		return out
	}

	for _, prd := range m.store.ListPRDs() {
		if prd.Status != PRDCompleted || prd.Git.Branch == "" {
			continue
		}
		name := prd.Title
		if name == "" {
			name = prd.ID
		}
		e := MergeStatusEntry{AutomatonID: prd.ID, Name: name, Branch: prd.Git.Branch, State: MergeStateUnknown}
		if cmp, ok := c.mergeCached(prd.Git.Branch); ok {
			e.State, e.CommitsAhead, e.Base = cmp.State, cmp.Ahead, cmp.Base
		}
		pending := e.State == MergeStatePending
		if mode == config.MergePendingModeShadow {
			wouldAlert := false
			if since, ok := c.mergeSince(prd.ID); ok && pending {
				t := since
				e.PendingSince = &t
				wouldAlert = out.AlertMinutes > 0 && !c.now().Before(since.Add(time.Duration(out.AlertMinutes)*time.Minute))
			}
			e.WouldAlert = &wouldAlert
		} else if t := prd.Triage; pending && t != nil && t.Status == TriageStatusMergePending {
			first := t.FirstSeenAt
			e.PendingSince = &first
			e.AlertedAt = t.AlertedAt
		}
		out.Automata = append(out.Automata, e)
	}
	sort.SliceStable(out.Automata, func(i, j int) bool { return out.Automata[i].AutomatonID < out.Automata[j].AutomatonID })

	if mode == config.MergePendingModeShadow {
		out.ShadowDecisions = c.MergeShadowDecisions(0)
	}
	return out
}

// MergeSafeToDelete reports which Automaton branches and worktrees are safe to
// delete, over the configured worktree base repo. It only reads. The report
// is served through a short-lived in-memory cache (safeDeleteCache) keyed by
// the repository path, so repeated reads do not re-run the git work.
func (m *Manager) MergeSafeToDelete(ctx context.Context) (SafeDeleteReport, error) {
	repo := m.Config().WorktreeBaseRepo
	if repo == "" {
		return SafeDeleteReport{Safe: []SafeDeleteEntry{}, Held: []SafeDeleteHeld{}}, ErrMergeNoBaseRepo
	}
	c := m.safeDeleteCacheOrInit()
	return c.get(ctx, repo)
}

// safeDeleteCacheOrInit returns the manager's safe-to-delete report cache,
// creating it (and wiring its compute to the real git-backed report) on
// first use. createSafeDeleteCache can be replaced in tests.
func (m *Manager) safeDeleteCacheOrInit() *safeDeleteCache {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.safeDeleteCache == nil {
		c := newSafeDeleteCache()
		c.compute = m.safeDeleteCompute
		m.safeDeleteCache = c
	}
	return m.safeDeleteCache
}

// safeDeleteCompute is the production compute behind the safe-to-delete
// cache: it resolves the base branch and runs the read-only git report.
func (m *Manager) safeDeleteCompute(ctx context.Context, repo string) (SafeDeleteReport, error) {
	git := NewMergeGitRunner()
	probe := &managerWorktreeProbe{m: m}
	return SafeToDeleteReport(ctx, git, repo, resolveMergeBase(ctx, git, repo), SafeDeleteOptions{
		// Reuse the same running rule the worktree-cleanup probe's
		// WorktreeInUse check applies: the Automaton in the automation
		// store is in one of the running states.
		IsRunning: func(branch string) bool {
			prd := probe.prdFor(branch)
			return prd != nil && (prd.Status == PRDRunning || prd.Status == PRDActive)
		},
	})
}
