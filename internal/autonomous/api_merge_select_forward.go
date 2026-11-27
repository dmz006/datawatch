// The daemon hands the server *API, not *Manager: these forwarders keep the
// merge selection rule, the acceptance and the confirmed landing reachable.
package autonomous

import (
	"context"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// MergeSelection forwards the selection read.
func (a *API) MergeSelection(ctx context.Context, id string) MergeSelection {
	return a.M.MergeSelection(ctx, id)
}

// SelectMergeRun forwards the rule for a merge run request.
func (a *API) SelectMergeRun(ctx context.Context, branches []string, automatonID string) (MergeSelectionEntry, error) {
	return a.M.SelectMergeRun(ctx, branches, automatonID)
}

// AcceptForMerge forwards the acceptance of an exact commit.
func (a *API) AcceptForMerge(ctx context.Context, id, commit, actor, reason string) (*PRD, error) {
	return a.M.AcceptForMerge(ctx, id, commit, actor, reason)
}

// RecordMergeLanding forwards the confirmed landing record.
func (a *API) RecordMergeLanding(ctx context.Context, id, release, commit string, at time.Time) (*PRD, error) {
	return a.M.RecordMergeLanding(ctx, id, release, commit, at)
}

// MergeReleaseFacts forwards the release facts of one Automaton.
func (a *API) MergeReleaseFacts(id string, kindMap map[string]string, majorTrigger string) (MergeReleaseFacts, bool) {
	return a.M.MergeReleaseFacts(id, kindMap, majorTrigger)
}

// MergeModelLookup forwards the merge conflict model registry.
func (a *API) MergeModelLookup() config.LLMLookup { return a.M.MergeModelLookup() }

// MergePauseMessage forwards the operator pause check of a merge run.
func (a *API) MergePauseMessage() (string, bool) { return a.M.MergePauseMessage() }

// RecordMergeLandingOfPushedTip forwards the landing record of a repaired push
// whose branch changed afterwards.
func (a *API) RecordMergeLandingOfPushedTip(ctx context.Context, id, release, commit, tip string, at time.Time) (*PRD, error) {
	return a.M.RecordMergeLandingOfPushedTip(ctx, id, release, commit, tip, at)
}

// MergeShadowSubject forwards what the shadow of a hand merge reads about its
// Automaton.
func (a *API) MergeShadowSubject(ctx context.Context, id string) (ShadowSubject, bool) {
	return a.M.MergeShadowSubject(ctx, id)
}

// MergeEligibilityForHandMerge forwards the selection rule as of a hand merge.
func (a *API) MergeEligibilityForHandMerge(ctx context.Context, id, tip string, mergedAt time.Time) MergeSelectionEntry {
	return a.M.MergeEligibilityForHandMerge(ctx, id, tip, mergedAt)
}
