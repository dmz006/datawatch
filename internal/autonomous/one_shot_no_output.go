package autonomous

// OneShotNoOutputError is the no-output family returned by a one-shot
// decompose session that timed out, or ended in a terminal state, without
// writing its plan output file. The cmd layer's one-shot runner produces
// it; the decompose loops in Manager match it with errors.As to count the
// failure toward decomposeSessionStopThreshold.
type OneShotNoOutputError struct {
	Msg string
	// TimedOut is set when the session hit its time limit, as opposed to a
	// no-progress stall. A judge uses it to move on to its next backend.
	TimedOut bool
}

func (e *OneShotNoOutputError) Error() string { return e.Msg }
