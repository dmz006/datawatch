package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// Shipped-source values (PRD.ShippedSource / Automation.ShippedSource). The
// empty value is unset: no shipped record exists yet.
const (
	// ShippedSourceMergeStep — the record was written by the merge step that
	// landed the work in the release.
	ShippedSourceMergeStep = "merge_step"
	// ShippedSourceBackfill — the record was written by a release-notes
	// backfill: the work shipped before automated recording existed.
	ShippedSourceBackfill = "backfill"
)

func validShippedSource(s string) bool {
	switch s {
	case ShippedSourceMergeStep, ShippedSourceBackfill:
		return true
	}
	return false
}

// ValidShippedSource reports whether s is one of the two shipped-source
// values (ShippedSourceMergeStep or ShippedSourceBackfill).
func ValidShippedSource(s string) bool { return validShippedSource(s) }

// IsShipped reports whether the PRD carries shipped state, i.e. a
// non-empty merge commit (see PRD.MergeCommit). A PRD that is shipped
// may still hold any lifecycle Status — shipped state is orthogonal to
// the PRD status and never changes it.
func (p *PRD) IsShipped() bool {
	return p != nil && strings.TrimSpace(p.MergeCommit) != ""
}

// IsShipped reports whether the Automation carries shipped state, i.e. a
// non-empty merge commit (see Automation.MergeCommit). It is orthogonal to
// the automation status, which recording shipped state never changes.
func (a *Automation) IsShipped() bool {
	return a != nil && strings.TrimSpace(a.MergeCommit) != ""
}

// setShippedFields applies the four shipped-state fields to a PRD from the
// record arguments. MergedAt stays nil when the time is zero.
func setPRDShipped(p *PRD, release string, mergedAt time.Time, commit, source string) {
	p.MergedInRelease = strings.TrimSpace(release)
	if mergedAt.IsZero() {
		p.MergedAt = nil
	} else {
		t := mergedAt
		p.MergedAt = &t
	}
	p.MergeCommit = commit
	p.ShippedSource = source
}

// setAutomationShippedFields applies the four shipped-state fields to an
// Automation from the record arguments. MergedAt stays nil when the time is
// zero.
func setAutomationShipped(a *Automation, release string, mergedAt time.Time, commit, source string) {
	a.MergedInRelease = strings.TrimSpace(release)
	if mergedAt.IsZero() {
		a.MergedAt = nil
	} else {
		t := mergedAt
		a.MergedAt = &t
	}
	a.MergeCommit = commit
	a.ShippedSource = source
}

// validateShippedRecordArgs checks the shared parts of the two record
// signatures: a non-empty merge commit and a known source.
func validateShippedRecordArgs(commit, source string) (string, error) {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return "", fmt.Errorf("shipped: merge commit must not be empty")
	}
	if !validShippedSource(source) {
		return "", fmt.Errorf("shipped: source must be %q or %q, got %q", ShippedSourceMergeStep, ShippedSourceBackfill, source)
	}
	return commit, nil
}

// RecordPRDShipped records that this PRD's work shipped in the named
// release. It stores the release name, the merge time (zero = not set),
// the merge commit, and where the record came from —
// ShippedSourceMergeStep or ShippedSourceBackfill — and saves the PRD
// through the store's normal save path.
//
// Status is never touched: a cancelled PRD is recorded, stays
// cancelled, and reads IsShipped as true. The call is idempotent per
// commit — a later call with the same merge commit is a no-op that
// leaves the stored fields, the revision and UpdatedAt alone — while a
// later call with a different commit (e.g. a merge-step record replacing
// a backfill one) overwrites all four fields.
func (m *Manager) RecordPRDShipped(id, release string, mergedAt time.Time, commit, source string) (*PRD, error) {
	c, err := validateShippedRecordArgs(commit, source)
	if err != nil {
		return nil, err
	}
	p, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	// p is the shared stored pointer (Store.GetPRD): check and write under the
	// store lock so the shipped fields are ordered against snapshot reads and
	// persist. SavePRD takes Store.mu too (non-reentrant), so it stays out.
	already := false
	m.store.WithLock(func() {
		if p.MergeCommit == c {
			already = true // idempotent: already recorded with this commit
			return
		}
		setPRDShipped(p, release, mergedAt, c, source)
	})
	if already {
		return p, nil
	}
	if err := m.store.SavePRD(p); err != nil {
		return nil, fmt.Errorf("shipped: save: %w", err)
	}
	out, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	return out, nil
}

// RecordAutomationShipped records that this Automation's work shipped in
// the named release. Same contract as RecordPRDShipped: the four
// shipped-state fields are stored, Status is never touched, a repeated
// call with the same commit is a no-op, and a call with a different
// commit overwrites all four fields. It saves through the automation
// store's Update path, so the automation's version and ownership rules
// keep applying (recording shipped state never blocks on them and never
// changes the status either).
func (m *Manager) RecordAutomationShipped(id, release string, mergedAt time.Time, commit, source string) (*Automation, error) {
	c, err := validateShippedRecordArgs(commit, source)
	if err != nil {
		return nil, err
	}
	if m.automations == nil {
		return nil, fmt.Errorf("automations store not configured")
	}
	a, ok := m.automations.Get(id)
	if !ok {
		return nil, fmt.Errorf("404: automation %q not found", id)
	}
	if a.MergeCommit == c {
		return a, nil // idempotent: already recorded with this commit
	}
	_, err = m.automations.Update(id, 0, func(a *Automation) error {
		setAutomationShipped(a, release, mergedAt, c, source)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("shipped: save: %w", err)
	}
	out, ok := m.automations.Get(id)
	if !ok {
		return nil, fmt.Errorf("automation %q not found", id)
	}
	// The record is on disk: run the merge-shadow comparison in the
	// background so the ship path never waits on it (no-op without a plan).
	m.startMergeShadowCompare(id)
	return out, nil
}
