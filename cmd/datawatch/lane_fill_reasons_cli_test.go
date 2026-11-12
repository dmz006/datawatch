package main

import (
	"net/http"
	"strings"
	"testing"
)

// laneFillReasonsCLIBody is a lane fill status with a refusal decision, a
// decision that is not a refusal, lane counts from both sources and per-kind
// modes that differ from the base mode.
const laneFillReasonsCLIBody = `{
  "mode": "shadow",
  "modes": {"gpu": "on", "claude": "shadow"},
  "lane_counts": {
    "gpu": {"configured": 2, "effective": 2, "source": "config"},
    "claude": {"configured": 3, "effective": 1, "source": "llm_caps"}
  },
  "lanes": [
    {"id": "gpu-1", "type": "gpu", "state": "idle", "occupant": null, "blocker": "", "eligible_waiting": 0}
  ],
  "queued_eligible": {"gpu": 0, "claude": 0},
  "evidence": {"error": ""},
  "decisions": [
    {"at": "2026-10-24T12:01:03Z", "lane_id": "gpu-1", "action": "blocked", "prd_id": "old1",
     "reason": "queued longer than 72 hours", "blocker": "queued longer than 72 hours",
     "eligibility_reason": "stale", "executed": false, "repeat_count": 1},
    {"at": "2026-10-24T12:01:04Z", "lane_id": "gpu-1", "action": "start_prd", "prd_id": "abc",
     "reason": "lane idle", "executed": false, "repeat_count": 1}
  ],
  "alerts": []
}`

// TestLaneFillReasonsCLIShowsReasonLaneCountsAndModes checks that the CLI
// prints the reason code in front of a refusal's reason text, the lane counts
// line under the lane lines and the per-kind modes line.
func TestLaneFillReasonsCLIShowsReasonLaneCountsAndModes(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, laneFillReasonsCLIBody)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[stale] queued longer than 72 hours",
		"Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)",
		"Mode per lane kind: gpu on, claude shadow",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "start_prd abc") && strings.Contains(line, ": [") {
			t.Errorf("a decision that is not a refusal must show no reason code: %q", line)
		}
	}
}

// TestLaneFillReasonsCLIJSONReturnsTheRawBody checks that --json prints the
// body with the reason, lane counts and modes untouched.
func TestLaneFillReasonsCLIJSONReturnsTheRawBody(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, laneFillReasonsCLIBody)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != laneFillReasonsCLIBody {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, laneFillReasonsCLIBody)
	}
}
