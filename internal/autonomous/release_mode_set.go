package autonomous

// Release approval mode changes go through SetReleaseApprovalMode, which emits
// one operator audit event per successful change, so the Events view shows
// each switch next to the decision record that holds its readiness evidence.
// A no-op change, or a change whose apply step fails, emits nothing.

import (
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// ReleaseModeRecorder stores the decision record built for a change and returns
// the stored record's ID. The caller supplies it, so the setter does not need
// the decision record store wired.
type ReleaseModeRecorder func(rec DecisionRecord) (string, error)

// SetReleaseApprovalMode forwards a release approval mode change to the Manager.
// The REST layer holds the API wrapper rather than the Manager, so the wrapper
// carries this method too; see Manager.SetReleaseApprovalMode for the contract.
func (a *API) SetReleaseApprovalMode(change ReleaseModeChange, evidence ReleaseReadiness, apply func(mode string) error, record ReleaseModeRecorder) (string, error) {
	if a == nil || a.M == nil {
		return "", fmt.Errorf("release approval mode: no autonomous manager")
	}
	return a.M.SetReleaseApprovalMode(change, evidence, apply, record)
}

// SetReleaseApprovalMode applies one release approval mode change for
// change.Size and emits exactly one audit.ActionReleaseModeChanged event for it.
//
// The change is refused with no effect when the size is not patch, minor or
// major or the new mode is not an accepted approval mode. A change whose old
// and new modes are equal is a no-op: apply, record and event are all skipped
// and the result is an empty ID with no error.
//
// apply persists the new mode and must return an error if it did not; then
// nothing is recorded or emitted. After a successful apply, record stores the
// decision record for the change and its ID is returned and written to the
// event. The event is emitted even when record fails, with an empty
// decision_record_id, and that record error is returned alongside the event,
// because the mode has already changed and must not go unreported. A nil record
// skips the decision record and leaves the ID empty.
func (m *Manager) SetReleaseApprovalMode(change ReleaseModeChange, evidence ReleaseReadiness, apply func(mode string) error, record ReleaseModeRecorder) (string, error) {
	switch change.Size {
	case ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor:
	default:
		return "", fmt.Errorf("release size %q is invalid; use %q, %q or %q", change.Size, ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor)
	}
	if err := config.ValidateReleaseApprovalMode(change.NewMode); err != nil {
		return "", err
	}
	if change.OldMode == change.NewMode {
		return "", nil
	}
	if apply == nil {
		return "", fmt.Errorf("release approval mode: no apply function for %s", change.Size)
	}
	if err := apply(change.NewMode); err != nil {
		return "", err
	}

	recordID, recordErr := "", error(nil)
	if record != nil {
		recordID, recordErr = record(NewReleaseModeChangeDecision(change, evidence, time.Now()))
	}

	headline := fmt.Sprintf("%s release approval mode changed from %s to %s", change.Size, change.OldMode, change.NewMode)
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn(audit.ActionReleaseModeChanged, map[string]any{
			"headline":           headline,
			"size":               change.Size,
			"old_mode":           change.OldMode,
			"new_mode":           change.NewMode,
			"actor":              change.Actor,
			"surface":            change.Surface,
			"decision_record_id": recordID,
		})
	}
	return recordID, recordErr
}
