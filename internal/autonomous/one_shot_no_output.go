package autonomous

import (
	"errors"
	"strings"
	"time"
)

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
	// RunCap is set when the verifier run-time cap
	// (autonomous.verifier_max_run_seconds) ended the session, as opposed to
	// the no-progress watchdog or the session timeout.
	RunCap bool
	// RunCapElapsed and RunCapSeconds are the accumulated run time and the
	// cap in seconds when RunCap is set; they feed the decision record.
	RunCapElapsed time.Duration
	RunCapSeconds int
	// OperatorEndedBy is the end caller (see OperatorEndCaller) when the
	// operator ended the session. A planner ended this way is not started again.
	OperatorEndedBy string
}

func (e *OneShotNoOutputError) Error() string { return e.Msg }

// operatorEndCallers are the end callers a session record names when a person,
// or a client acting for one, asked for the end (see session.EndReason). Ends
// the daemon makes itself (daemon.*, autonomous.*, a gone-session finalise) are
// not listed, so their planner is retried as before.
var operatorEndCallers = map[string]bool{
	"cli.session-kill":      true,
	"cli.session-stop-all":  true,
	"command.kill":          true,
	"comm.kill":             true,
	"mcp.kill-session":      true,
	"mcp.stop-all-sessions": true,
	"rest.session-kill":     true,
	"rest.autonomous-prd":   true,
	"rest.llm-force-delete": true,
}

// OperatorEndCaller returns the operator caller named by a session's end note
// ("killed by <caller>: <reason>"), or "" when the note does not say the
// operator ended the session.
func OperatorEndCaller(endNote string) string {
	rest, ok := strings.CutPrefix(endNote, "killed by ")
	if !ok {
		return ""
	}
	caller, _, _ := strings.Cut(rest, ":")
	if !operatorEndCallers[caller] {
		return ""
	}
	return caller
}

// operatorEndedBy returns the operator caller a decompose error carries, or "".
func operatorEndedBy(err error) string {
	var noOutput *OneShotNoOutputError
	if errors.As(err, &noOutput) {
		return noOutput.OperatorEndedBy
	}
	return ""
}
