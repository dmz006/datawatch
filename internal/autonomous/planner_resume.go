package autonomous

// PlannerResumeAction is what a restart does with an Automaton that was in
// planning: it either re-attaches to the planner session or starts the
// decompose again. Both leave the Automaton in planning.
type PlannerResumeAction string

const (
	// PlannerResumeReattach re-attaches to the planner session that is still alive.
	PlannerResumeReattach PlannerResumeAction = "reattach"
	// PlannerResumeRestart starts the decompose again because the planner session is gone.
	PlannerResumeRestart PlannerResumeAction = "restart_decompose"
)

// ResolvePlannerResume decides how a restart resumes planning. If the planner
// session is still alive it re-attaches; if it is gone it starts the decompose
// again. Neither action counts a failure, records a failure or returns the
// Automaton to draft, so the caller never needs a failure path for a restart.
func ResolvePlannerResume(sessionAlive bool) (PlannerResumeAction, string) {
	if sessionAlive {
		return PlannerResumeReattach, "the planner session is still alive, so the Automaton stays in planning and is re-attached to it"
	}
	return PlannerResumeRestart, "the planner session is gone, so the Automaton stays in planning and the decompose is started again"
}
