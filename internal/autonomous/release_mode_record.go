package autonomous

// Release approval mode changes are recorded as decision records so the
// operator sees the readiness evidence next to the mode switch they approve.

import (
	"fmt"
	"strings"
	"time"
)

// ReleaseModeChangeSource is the DecisionRecord.Source for a record built from
// a release approval mode change.
const ReleaseModeChangeSource = "release_mode_change"

// ReleaseModeChange describes one requested change of the release approval mode.
type ReleaseModeChange struct {
	// Size is the release size the mode applies to: "patch", "minor" or "major".
	Size string `json:"size"`
	// OldMode is the approval mode in force before the change.
	OldMode string `json:"old_mode"`
	// NewMode is the approval mode requested by the change.
	NewMode string `json:"new_mode"`
	// Actor is the person, daemon or supervisor that made the change.
	Actor string `json:"actor"`
	// Surface is where the change was made: "pwa", "rest", "mcp", "comm", "cli" or "config".
	Surface string `json:"surface"`
}

// NewReleaseModeChangeDecision builds an open decision record for change. The
// situation carries the readiness counts, the missing items and the ready flag
// from evidence, plus the actor and surface. The options are the old and new
// modes, and the default is the old mode, so the status quo holds if no answer
// is given. The record is not validated or stored here.
func NewReleaseModeChangeDecision(change ReleaseModeChange, evidence ReleaseReadiness, at time.Time) DecisionRecord {
	missing := "none"
	if len(evidence.Missing) > 0 {
		missing = strings.Join(evidence.Missing, "; ")
	}
	status := "not ready"
	if evidence.Ready {
		status = "ready"
	}
	situation := fmt.Sprintf(
		"Release readiness for %s: %d plan-equal-or-better records (bar %d), %d conflict records (bar %d), %d PWA-change records (bar %d), %d blocking defects. Status: %s. Missing: %s. Requested by %s via %s.",
		change.Size,
		evidence.Records, evidence.Required.Records,
		evidence.Conflicts, evidence.Required.Conflicts,
		evidence.PWAChanges, evidence.Required.PWAChanges,
		evidence.BlockingDefects,
		status, missing,
		change.Actor, change.Surface,
	)
	created := at
	return DecisionRecord{
		ID:        NewDecisionRecordID(),
		Title:     fmt.Sprintf("Change %s release approval mode from %s to %s", change.Size, change.OldMode, change.NewMode),
		Situation: situation,
		Deciding:  fmt.Sprintf("Whether the %s release approval mode changes from %s to %s.", change.Size, change.OldMode, change.NewMode),
		Options: []DecisionOption{
			{ID: change.OldMode, Label: change.OldMode},
			{ID: change.NewMode, Label: change.NewMode},
		},
		Default:   change.OldMode,
		State:     DecisionRecordOpen,
		Source:    ReleaseModeChangeSource,
		CreatedAt: &created,
	}
}
