package autonomous

import (
	"strings"
	"testing"
	"time"
)

// The idle detector's stall reason and error carry the session's nudge history
// when the daemon read it, and nothing extra when it did not.
func TestSessionIdle_NudgeHistoryIsNamedInReasonAndError(t *testing.T) {
	o := SessionIdleObservation{
		SessionID: "idle-sess",
		Backend:   "claude-code",
		Model:     "sonnet",
		State:     "waiting_input",
		IdleFor:   20 * time.Minute,
		Limit:     15 * time.Minute,
		Nudge:     "nudge 1 sent at 2026-10-25T15:03:00Z, outcome sent",
	}
	const clause = ", nudge 1 sent at 2026-10-25T15:03:00Z, outcome sent)"

	stalled, reason := o.Stalled()
	if !stalled {
		t.Fatal("Stalled() = false, want the idle session stalled")
	}
	if !strings.HasSuffix(reason, clause) {
		t.Errorf("stall reason %q does not end with the nudge history", reason)
	}
	if err := SessionIdleError(o); !strings.Contains(err.Error(), clause) {
		t.Errorf("SessionIdleError %q does not name the nudge history", err.Error())
	}

	o.Nudge = ""
	if _, reason := o.Stalled(); strings.Contains(reason, "nudge") {
		t.Errorf("reason %q names a nudge the caller never read", reason)
	}
	if err := SessionIdleError(o); strings.Contains(err.Error(), "nudge") {
		t.Errorf("error %q names a nudge the caller never read", err.Error())
	}
}
