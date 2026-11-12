package router

import (
	"net/http"
	"strings"
	"testing"
)

// verifierWaitCommBody is a lane fill status with one working Claude lane and
// one Claude lane counted available by the verifier-wait rule, plus a
// decision carrying the wait reason.
const verifierWaitCommBody = `{
  "mode": "on",
  "lanes": [
    {"id": "claude-1", "type": "claude", "state": "occupied",
     "occupant": {"prd_id": "p1", "title": "Add paging", "backend": "claude-code"},
     "blocker": "", "eligible_waiting": 0},
    {"id": "claude-2", "type": "claude", "state": "waiting_verifier",
     "occupant": {"prd_id": "p2", "title": "Fix login", "backend": "claude-code",
                  "waiting_on_verifier": true, "wait_reason": "CI still running the checks"},
     "blocker": "", "eligible_waiting": 1}
  ],
  "queued_eligible": {"gpu": 0, "claude": 1},
  "evidence": {"error": ""},
  "decisions": [
    {"at": "2026-10-24T12:01:03Z", "lane_id": "claude-2", "action": "start_prd",
     "prd_id": "p3", "title": "Add export", "reason": "the lane counts available",
     "wait_reason": "CI still running the checks", "executed": false, "repeat_count": 1}
  ],
  "alerts": []
}`

// TestVerifierWaitComm_ReplyNamesTheVerifierWait checks that the comm reply
// line for a lane counted available by the verifier wait rule reads
// "waiting on verifier" followed by the reason, while the working lane line
// does not.
func TestVerifierWaitComm_ReplyNamesTheVerifierWait(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, verifierWaitCommBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), " ")
	want := "claude-2: occupied by p2 \"Fix login\" (claude-code), waiting on verifier: CI still running the checks, 1 eligible waiting"
	if !strings.Contains(got, want) {
		t.Errorf("reply lacks the verifier wait line:\n%s", got)
	}
	working := ""
	for _, lane := range strings.Split(got, "\n") {
		if strings.HasPrefix(lane, "claude-1:") {
			working = lane
		}
	}
	if working == "" {
		t.Fatalf("reply lacks the working lane line:\n%s", got)
	}
	if strings.Contains(working, "waiting on verifier") {
		t.Errorf("working lane line must not name the verifier wait: %q", working)
	}
}

// TestVerifierWaitComm_DecisionShowsWaitReason checks that the reply's
// decision line shows the wait reason when the record carries one.
func TestVerifierWaitComm_DecisionShowsWaitReason(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, verifierWaitCommBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), " ")
	if strings.Contains(got, "start_prd p3") && !strings.Contains(got, "CI still running the checks") {
		t.Errorf("decision line must show the wait reason:\n%s", got)
	}
}
