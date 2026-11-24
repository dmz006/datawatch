package autonomous

import "context"

// ArchiveWithOutcome archives a finished Automaton, recording the outcome in
// the same step (see Manager.ArchiveWithOutcome), and emits the update. It is
// an operator surface, so the finished-work guard runs first
// (Manager.ArchiveGuardedWithOutcome).
func (a *API) ArchiveWithOutcome(id string, in OutcomeInput) (any, error) {
	out, err := a.M.ArchiveGuardedWithOutcome(id, "operator", in)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// SetClosedOutcome records the outcome of a finished Automaton.
func (a *API) SetClosedOutcome(id, actor string, in OutcomeInput) (any, error) {
	return a.M.SetClosedOutcome(id, actor, in)
}

// OutcomeCheck is the live read of the standing check of closed work.
func (a *API) OutcomeCheck() OutcomeCheckReport { return a.M.OutcomeCheck() }

// RunOutcomeCheck runs the standing check now, intent proof included.
func (a *API) RunOutcomeCheck(ctx context.Context) OutcomeCheckReport {
	return a.M.RunOutcomeCheck(ctx)
}

// RecordReleaseDeployed writes a deploy record onto every Automaton shipped in
// the record's release and returns the ids it changed.
func (a *API) RecordReleaseDeployed(rec DeployRecord) ([]string, error) {
	return a.M.RecordReleaseDeployed(rec)
}
