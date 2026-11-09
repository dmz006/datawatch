package config

import "strings"

// Values of autonomous.planning_restart_policy: what a daemon restart does to
// an Automaton that is still in planning (decompose running).
const (
	// PlanningRestartPolicyReattach keeps planning running across a restart:
	// the decompose is re-attached to the restarted daemon. The default, and
	// what an unset or unrecognised value reads as.
	PlanningRestartPolicyReattach = "reattach"
	// PlanningRestartPolicyRestart restarts planning once after the restart
	// and records a decision saying so.
	PlanningRestartPolicyRestart = "restart"
	// PlanningRestartPolicyDraft resets the Automaton to draft, as the daemon
	// did before this setting existed.
	PlanningRestartPolicyDraft = "draft"
)

// NormalizePlanningRestartPolicy returns the effective planning restart
// policy; unset or unrecognised reads as reattach.
func NormalizePlanningRestartPolicy(v string) string {
	switch s := strings.ToLower(strings.TrimSpace(v)); s {
	case PlanningRestartPolicyRestart, PlanningRestartPolicyDraft:
		return s
	}
	return PlanningRestartPolicyReattach
}

// ValidPlanningRestartPolicy reports whether v is one of the three recognised
// policy values. The empty string is not valid here: it reads as unset on
// load (see NormalizePlanningRestartPolicy), but write paths reject it.
func ValidPlanningRestartPolicy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case PlanningRestartPolicyReattach, PlanningRestartPolicyRestart, PlanningRestartPolicyDraft:
		return true
	}
	return false
}
