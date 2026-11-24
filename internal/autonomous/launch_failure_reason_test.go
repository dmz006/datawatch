// A launch that ended because its task never reached the prompt names the
// delivery failure; it does not read as "worker stalled".
package autonomous

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestLaunchFailureErrorWithReason_NamesTheReasonNotStalled(t *testing.T) {
	reason := "task delivery failed: main prompt never appeared within 1m0s across 3 attempts; last screen: the input box already holds text, so nothing more was typed"
	err := LaunchFailureErrorWithReason("s1", "claude-code", "killed", reason)
	msg := err.Error()
	if !strings.Contains(msg, reason) {
		t.Errorf("message %q does not carry the delivery reason", msg)
	}
	if strings.Contains(strings.ToLower(msg), "stalled") {
		t.Errorf("message %q still says stalled", msg)
	}
	if !strings.Contains(msg, LaunchFailureReason) {
		t.Errorf("message %q lost LaunchFailureReason, the infra classifier keys on it", msg)
	}
	if !errors.Is(err, ErrSessionKilledBeforeDelivery) || !errors.Is(err, ErrWorkerStalled) {
		t.Errorf("must unwrap to both sentinels so the executor retries it: %v", err)
	}
	if class, ok := ClassifyInfraText(msg); !ok || class != config.InfraRetryClassSessionKilled {
		t.Errorf("text classification = %q, %v; want session_killed", class, ok)
	}
}

func TestLaunchFailureErrorWithReason_EmptyReasonKeepsTheOldMessage(t *testing.T) {
	got := LaunchFailureErrorWithReason("s1", "claude-code", "killed", "").Error()
	want := LaunchFailureError("s1", "claude-code", "killed").Error()
	if got != want {
		t.Errorf("empty reason message = %q, want %q", got, want)
	}
}
