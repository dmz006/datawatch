package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// laneFillReasonsBody is a lane fill status with one refusal decision, one
// decision that is not a refusal, lane counts and per-kind modes.
const laneFillReasonsBody = `{
  "mode": "shadow",
  "modes": {"gpu": "on", "claude": "shadow"},
  "lane_counts": {
    "gpu": {"configured": 2, "effective": 2, "source": "config"},
    "claude": {"configured": 3, "effective": 1, "source": "llm_caps"}
  },
  "decisions": [
    {"lane_id": "gpu-1", "action": "blocked", "prd_id": "old1", "reason": "queued longer than 72 hours", "eligibility_reason": "stale"},
    {"lane_id": "gpu-1", "action": "start_prd", "prd_id": "abc", "reason": "lane idle"}
  ]
}`

// TestLaneFillStatusShowsTheRefusalReason checks that a refusal decision keeps
// its eligibility_reason through the tool and a non-refusal has none.
func TestLaneFillStatusShowsTheRefusalReason(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(laneFillReasonsBody))
	})
	res, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, `"eligibility_reason": "stale"`) {
		t.Errorf("the refusal reason was lost: %s", text)
	}
	if n := strings.Count(text, "eligibility_reason"); n != 1 {
		t.Errorf("only the refusal may carry eligibility_reason, found %d: %s", n, text)
	}
	for _, want := range []string{`"lane_counts"`, `"llm_caps"`, `"modes"`} {
		if !strings.Contains(text, want) {
			t.Errorf("result lost %s: %s", want, text)
		}
	}
}

// TestLaneFillStatusDescriptionMentionsReasonsAndLaneCounts checks that the
// tool description names the reason codes and the lane counts.
func TestLaneFillStatusDescriptionMentionsReasonsAndLaneCounts(t *testing.T) {
	desc := (&Server{}).toolAutonomousLaneFillStatus().Description
	for _, want := range []string{
		"eligibility_reason", "stale", "held", "sequenced_after_unshipped", "proposal_not_confirmed",
		"proposal_missing", "main_carries_work", "worktree_invalid", "lane_counts", "modes",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}
