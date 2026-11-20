package autonomous

import "testing"

func TestAttemptCategoryEndReasons(t *testing.T) {
	cases := []struct {
		name, reason, summary, want string
	}{
		{"verifier", attemptEndVerifierFailed, "", "code_or_test"},
		{"stalled", attemptEndStalled, "", "hung"},
		{"session died", attemptEndSessionDied, "", "infra"},
		{"launch refused", attemptEndLaunchRefused, "placement refused", "infra"},
		{"executor timeout", attemptEndExecutorError, "request timeout", "infra"},
		{"executor no output", attemptEndExecutorError, "worker produced no output", "infra"},
		{"adopted", attemptEndAdoptedThenFailed, "", "infra"},
		{"restart", attemptEndDaemonRestart, "", "infra"},
		{"cancelled", attemptEndCancelledByOperator, "", "none"},
		{"merge refused", attemptEndExecutorError, "Merge refused by guard", "policy"},
		{"commit failed", attemptEndExecutorError, "commit failed: hook", "policy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := attemptCategoryFor(c.reason, "failed", "", c.summary, "", nil); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestAttemptCategoryKeepsExisting(t *testing.T) {
	if got := attemptCategoryFor(attemptEndStalled, "stalled", "security", "x", "", nil); got != "security" {
		t.Fatalf("got %q", got)
	}
}

func TestAttemptCategoryNoReasonStaysEmpty(t *testing.T) {
	if got := attemptCategoryFor("", "failed", "", "", "", nil); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := attemptCategoryFor("", "failed", "", "", "bad", nil); got != "code_or_test" {
		t.Fatalf("got %q", got)
	}
	if got := attemptCategoryFor("", "failed", "", "", "", []string{"i"}); got != "code_or_test" {
		t.Fatalf("got %q", got)
	}
}
