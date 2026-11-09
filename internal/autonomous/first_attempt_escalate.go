package autonomous

// autonomous.first_attempt escalation — when the first-attempt pair stops
// applying to a task.
//
// The first-attempt pair runs a task's first attempts on the cheaper worker.
// Once the task has failed FirstAttemptEscalateAfterFailures times, the pair
// no longer applies and the existing worker choice, including the failure
// escalation ladder, takes over unchanged.

// firstAttemptEscalated reports whether a task that has recorded
// failedAttempts failed attempts has reached the escalation point, so the
// first-attempt pair stops applying. escalateAfter below 1 is treated as 1,
// so the first failure always escalates when the setting is unset or zero.
func firstAttemptEscalated(failedAttempts, escalateAfter int) bool {
	if escalateAfter < 1 {
		escalateAfter = 1
	}
	return failedAttempts >= escalateAfter
}
