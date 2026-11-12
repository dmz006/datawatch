package router

import (
	"net/http"
	"strings"
	"testing"
)

// laneFillReasonsCommBody is a lane fill status with a refusal decision, a
// decision that is not a refusal, lane counts from both sources and per-kind
// modes that differ from the base mode.
const laneFillReasonsCommBody = `{
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

// TestLaneFillComm_ReplyShowsReasonLaneCountsAndModes checks that the comm
// reply carries the same lines as the CLI: the reason code in front of a
// refusal's reason text, the lane counts line and the per-kind modes line.
func TestLaneFillComm_ReplyShowsReasonLaneCountsAndModes(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, laneFillReasonsCommBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), "\n")
	for _, want := range []string{
		"[stale] queued longer than 72 hours",
		"Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)",
		"Mode per lane kind: gpu on, claude shadow",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "start_prd abc") && strings.Contains(line, ": [") {
			t.Errorf("a decision that is not a refusal must show no reason code: %q", line)
		}
	}
}
