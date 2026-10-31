package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

// A task held after its infrastructure retries carries the cause in the
// review_needed row, and any other held task leaves the field out.
func TestReviewNeededEntry_CarriesInfraHoldCause(t *testing.T) {
	story := &Story{ID: "s1"}
	held := &Task{ID: "t1", Status: TaskReviewNeeded, FailureReason: &FailureReason{
		Category: infraRetryFailureCategory, Source: infraRetryHoldSource, Detail: "spawn_error: tmux spawn failed",
	}}
	if got := reviewNeededEntry(story, held).HeldCause; got != "spawn_error: tmux spawn failed" {
		t.Errorf("HeldCause = %q", got)
	}

	other := &Task{ID: "t2", Status: TaskReviewNeeded, FailureReason: &FailureReason{Category: "content", Source: "verifier", Detail: "tests fail"}}
	e := reviewNeededEntry(story, other)
	if e.HeldCause != "" {
		t.Errorf("a content hold must have no HeldCause, got %q", e.HeldCause)
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "held_cause") {
		t.Errorf("held_cause must be omitted when empty: %s", raw)
	}
}
