package autonomous

import (
	"errors"
	"strings"
)

// NextVerifierRunCapBackend decides whether a verifier session that ended at
// the run cap (autonomous.verifier_max_run_seconds) is retried, and on which
// backend. order is the verifier backend order the policy defines; cut is the
// backend the capped session ran on; retried reports whether this verification
// already spent its one run-cap retry.
//
// It returns the first backend after cut in order that is not cut itself and
// not blank. When cut is not in order, the search starts at the top of the
// list. It returns ("", false) when the retry was already spent or no next
// backend exists, so a cap end never retries more than once and a
// single-backend list gets no retry at all.
func NextVerifierRunCapBackend(order []string, cut string, retried bool) (string, bool) {
	if retried {
		return "", false
	}
	cut = strings.TrimSpace(cut)
	start := 0
	for i, b := range order {
		if strings.TrimSpace(b) == cut {
			start = i + 1
			break
		}
	}
	for _, b := range order[start:] {
		b = strings.TrimSpace(b)
		if b != "" && b != cut {
			return b, true
		}
	}
	return "", false
}

// IsVerifierRunCapEnd reports whether err is the no-output error a verifier
// session returns when the run cap ended it. Such an end is verifier
// infrastructure trouble, not a judgment about the task.
func IsVerifierRunCapEnd(err error) bool {
	var noOutput *OneShotNoOutputError
	return errors.As(err, &noOutput) && noOutput.RunCap
}
