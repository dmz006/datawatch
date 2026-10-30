package main

import (
	"net/http"
	"strings"
	"testing"
)

// verifierWaitStatusBody is a lane fill status with one working Claude lane
// and one Claude lane counted available by the verifier-wait rule.
const verifierWaitStatusBody = `{
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

// TestVerifierWaitCLI_WaitingLanePrintsTheWaitAndReason checks that the
// lane fill CLI line for a lane counted available by the verifier wait rule
// reads "waiting on verifier" followed by the reason.
func TestVerifierWaitCLI_WaitingLanePrintsTheWaitAndReason(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, verifierWaitStatusBody)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "claude-2: occupied by p2 \"Fix login\" (claude-code), waiting on verifier: CI still running the checks, 1 eligible waiting"
	if !strings.Contains(out, want) {
		t.Errorf("missing the verifier wait line:\n%s", out)
	}
	// The working lane line must not read as waiting.
	for _, lane := range strings.Split(out, "\n") {
		if strings.HasPrefix(lane, "claude-1:") && strings.Contains(lane, "waiting on verifier") {
			t.Errorf("working lane must not name the verifier wait: %q", lane)
		}
	}
}

// TestVerifierWaitCLI_DecisionShowsWaitReason checks that the CLI decision
// line shows the wait reason when the record carries one.
func TestVerifierWaitCLI_DecisionShowsWaitReason(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, verifierWaitStatusBody)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	decision := ""
	for _, lane := range strings.Split(out, "\n") {
		if strings.Contains(lane, "start_prd p3") {
			decision = lane
		}
	}
	if decision == "" {
		t.Fatalf("no decision line in the output:\n%s", out)
	}
	if !strings.Contains(decision, "CI still running the checks") {
		t.Errorf("decision line must show the wait reason, got %q", decision)
	}
}
