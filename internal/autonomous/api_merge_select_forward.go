// The daemon hands the server *API, not *Manager: these forwarders keep the
// merge selection rule, the acceptance and the confirmed landing reachable.
package autonomous

import (
	"context"
	"time"
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
