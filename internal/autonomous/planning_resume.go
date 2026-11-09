package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// PlanningResumeAction is what the daemon does with an Automaton that was
// still in planning (decompose running) when the daemon restarted.
type PlanningResumeAction string

const (
	// ActionAttachReader re-attaches a reader to the planner session that is
	// still alive after the restart.
	ActionAttachReader PlanningResumeAction = "attach_reader"
	// ActionIngestOutput stores the finished planner output and moves on
	// without spawning a new planner.
	ActionIngestOutput PlanningResumeAction = "ingest_output"
	// ActionRestartPlanning starts planning again from the beginning.
	ActionRestartPlanning PlanningResumeAction = "restart_planning"
	// ActionResetToDraft returns the Automaton to draft.
	ActionResetToDraft PlanningResumeAction = "reset_to_draft"
)

// OutputState classifies what the planner left in its output file.
type OutputState string

const (
	// OutputNone means there is no output at all (or only whitespace).
	OutputNone OutputState = "none"
	// OutputPartial means output exists but looks cut off mid-document.
	OutputPartial OutputState = "partial"
	// OutputCompleteParsable means the output is a whole document that
	// parses against the decomposition schema.
	OutputCompleteParsable OutputState = "complete_parsable"
	// OutputCompleteUnparsable means the output looks whole but does not
	// parse against the decomposition schema.
	OutputCompleteUnparsable OutputState = "complete_unparsable"
)

// PlanningResumeInput is the state the daemon gathers after a restart.
type PlanningResumeInput struct {
	// Policy is the normalized autonomous.planning_restart_policy value
	// (reattach, restart or draft).
	Policy string
	// HasPlannerRecord reports whether the Automaton has a recorded planner
	// session id. Without one there is nothing to re-attach to or to read.
	HasPlannerRecord bool
	// SessionAlive reports whether the recorded planner session survived the
	// restart.
	SessionAlive bool
	// OutputState is the classification of the planner's output file.
	OutputState OutputState
	// AlreadyRestartedOnce reports whether planning was already restarted
	// automatically for this Automaton.
	AlreadyRestartedOnce bool
}

// DecidePlanningResume returns the action to take for an Automaton still in
// planning after a daemon restart. It is pure: it reads only its input and
// returns the action plus a Reason string for the decision record.
func DecidePlanningResume(in PlanningResumeInput) PlanningResumeAction {
	action, _ := decidePlanningResume(in)
	return action
}

// DecidePlanningResumeWithReason returns the action and its human-readable reason, for
// the decision record. DecidePlanningResume returns the same action.
func DecidePlanningResumeWithReason(in PlanningResumeInput) (PlanningResumeAction, string) {
	return decidePlanningResume(in)
}

func decidePlanningResume(in PlanningResumeInput) (PlanningResumeAction, string) {
	switch config.NormalizePlanningRestartPolicy(in.Policy) {
	case config.PlanningRestartPolicyDraft:
		return ActionResetToDraft, "planning_restart_policy is draft: planning is reset to draft on restart"
	case config.PlanningRestartPolicyRestart:
		return ActionRestartPlanning, "planning_restart_policy is restart: planning is restarted once after the daemon restart"
	}

	// Policy is reattach (the default for unset or unknown values).
	if !in.HasPlannerRecord {
		// Nothing identifies the planner, so neither a live session nor its
		// output can be used.
		return restartOrReset(in.AlreadyRestartedOnce, "reattach: no planner session is recorded")
	}
	if in.SessionAlive {
		return ActionAttachReader, "reattach: the planner session is still alive, so a reader is attached to it"
	}
	if in.OutputState == OutputCompleteParsable {
		return ActionIngestOutput, "reattach: the planner session ended and its output is complete and parses, so the output is ingested"
	}
	return restartOrReset(in.AlreadyRestartedOnce, "reattach: the planner session ended and left no usable output ("+string(in.OutputState)+")")
}

// restartOrReset restarts planning when the automatic restart has not been
// used yet, and resets to draft once it has.
func restartOrReset(alreadyRestarted bool, why string) (PlanningResumeAction, string) {
	if alreadyRestarted {
		return ActionResetToDraft, why + "; the one automatic restart was already used, so the Automaton is reset to draft"
	}
	return ActionRestartPlanning, why + "; planning is restarted once automatically"
}

// ClassifyPlannerOutput classifies the raw bytes of a planner output file.
//
// It returns OutputCompleteParsable only when the content parses with
// ParsePlanning (the decomposition parser the manager uses) and passes the
// same plan check the manager applies before storing stories. Otherwise it
// returns OutputPartial when the content does not end like a complete JSON
// document (it looks cut off), and OutputCompleteUnparsable when it does.
func ClassifyPlannerOutput(raw []byte) OutputState {
	if strings.TrimSpace(string(raw)) == "" {
		return OutputNone
	}
	_, stories, err := ParsePlanning(string(raw))
	if err == nil && validateParsedPlan(stories) == nil {
		return OutputCompleteParsable
	}
	cleaned := cleanLLMJSON(string(raw))
	if strings.HasSuffix(cleaned, "}") || strings.HasSuffix(cleaned, "]") {
		return OutputCompleteUnparsable
	}
	return OutputPartial
}
