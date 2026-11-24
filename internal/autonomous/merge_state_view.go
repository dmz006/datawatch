// The derived, read-only merge_state of an Automaton view. A record that
// finished (completed, or archived since) while its work branch still carries
// commits that are not on the base branch shows "finished_not_merged", so work
// waiting to land is visible on every surface. The archive guard itself lives
// in the supervisor loop close step (supervisor_loop_archive_branch.go) and the
// archived-unmerged finder (archived_unmerged.go); this file only derives the
// value for views. It is computed on read and never persisted on the record.
package autonomous

import (
	"context"
	"time"
)

// MergeStateFinishedNotMerged is the derived merge_state value: the record
// finished (completed, or archived since) while its work branch still carries
// commits not on the base branch. Computed on read; never stored.
const MergeStateFinishedNotMerged = "finished_not_merged"

// mergeStateCacheWindowDefault is how long a derived value is cached per record
// so a list of finished Automata does not run git N+1 times per read.
const mergeStateCacheWindowDefault = time.Minute

type mergeStateCacheEntry struct {
	value    string
	computed time.Time
}

// SetMergeStateCacheWindow sets how long a derived merge_state is cached per
// record; a value of zero or less restores the one-minute default.
func (m *Manager) SetMergeStateCacheWindow(d time.Duration) {
	m.mu.Lock()
	m.mergeStateCacheWindow = d
	m.mu.Unlock()
}

// MergeStateCacheWindow returns the lifetime of a cached derived merge_state:
// the value set with SetMergeStateCacheWindow, else one minute. Views that
// cache a body holding merge_state use it as the longest age the body may
// reach while no record is written.
func (m *Manager) MergeStateCacheWindow() time.Duration {
	if m == nil {
		return mergeStateCacheWindowDefault
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mergeStateCacheWindow <= 0 {
		return mergeStateCacheWindowDefault
	}
	return m.mergeStateCacheWindow
}

// AutomatonMergeState returns the derived merge_state for views of p:
// MergeStateFinishedNotMerged when p is completed or archived and its branch
// is ahead of the base branch, "" otherwise (active records, no branch, a
// merged, empty or dropped branch, or a comparison that cannot be made: the
// field only surfaces work, it never hides a record). Read-only git queries,
// cached per record for a short window.
func (m *Manager) AutomatonMergeState(p *PRD) string {
	if p == nil {
		return ""
	}
	return m.AutomatonMergeStateOf(p.ID, p.Status, p.Git.Branch, p.Git.BaseBranch)
}

// AutomatonMergeStateOf is AutomatonMergeState from the fields it needs, so
// the list, roll-up and Gantt can derive it from a summary-index entry without
// reading the full record. An empty base resolves from the repository.
func (m *Manager) AutomatonMergeStateOf(id string, status PRDStatus, branch, base string) string {
	if m == nil || (status != PRDCompleted && status != PRDArchived) || branch == "" {
		return ""
	}
	key := id + "\x00" + string(status) + "\x00" + branch
	m.mu.Lock()
	window := m.mergeStateCacheWindow
	if window <= 0 {
		window = mergeStateCacheWindowDefault
	}
	if e, ok := m.mergeStateCache[key]; ok && time.Since(e.computed) < window {
		m.mu.Unlock()
		return e.value
	}
	repo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if repo == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), mergePendingGitTimeout)
	defer cancel()
	state, _, _ := checkBranchAhead(ctx, m.archivedUnmergedGitRunner(), repo, branch, base)
	value := ""
	if state == branchCheckAhead {
		value = MergeStateFinishedNotMerged
	}

	m.mu.Lock()
	if m.mergeStateCache == nil {
		m.mergeStateCache = map[string]mergeStateCacheEntry{}
	}
	m.mergeStateCache[key] = mergeStateCacheEntry{value: value, computed: time.Now()}
	m.mu.Unlock()
	return value
}
