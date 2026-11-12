package autonomous

import (
	"strings"
	"testing"
)

// verifierWaitTextStatus is a status with one working Claude lane and one
// Claude lane whose occupant waits on a verifier slot, plus one decision whose
// WaitReason names the wait that made the lane count available.
func verifierWaitTextStatus() LaneFillStatus {
	return LaneFillStatus{
		Mode: "on",
		Lanes: []LaneFillLaneStatus{
			{
				ID: "claude-1", Type: LaneTypeClaude, State: "occupied",
				Occupant: &LaneOccupant{PRDID: "p1", Title: "Add paging", Backend: "claude-code"},
			},
			{
				ID: "claude-2", Type: LaneTypeClaude, State: "waiting_verifier",
				Occupant: &LaneOccupant{
					PRDID: "p2", Title: "Fix login", Backend: "claude-code",
					WaitingOnVerifier: true, WaitReason: "CI still running the checks",
				},
			},
		},
		Decisions: []LaneFillDecision{
			{
				LaneID: "claude-2", Action: "start_prd", PRDID: "p3", Title: "Add export",
				Reason:     "the lane counts available",
				WaitReason: "CI still running the checks",
			},
		},
	}
}

// TestVerifierWaitText_WaitingLaneNamesTheWaitAndReason checks that a lane
// counted available by the verifier wait rule reads "waiting on verifier"
// followed by the reason in its line.
func TestVerifierWaitText_WaitingLaneNamesTheWaitAndReason(t *testing.T) {
	out := RenderLaneFillText(verifierWaitTextStatus(), 0)
	want := "claude-2: occupied by p2 \"Fix login\" (claude-code), waiting on verifier: CI still running the checks"
	if !strings.Contains(out, want) {
		t.Errorf("missing the verifier wait line:\n%s", out)
	}
}

// TestVerifierWaitText_WaitingLaneWithoutReasonStillNamesTheWait keeps the
// wording when the occupant reports no reason text.
func TestVerifierWaitText_WaitingLaneWithoutReasonStillNamesTheWait(t *testing.T) {
	st := verifierWaitTextStatus()
	st.Lanes[1].Occupant.WaitReason = ""
	out := RenderLaneFillText(st, 0)
	want := "claude-2: occupied by p2 \"Fix login\" (claude-code), waiting on verifier"
	if !strings.Contains(out, want) {
		t.Errorf("missing the verifier wait line:\n%s", out)
	}
}

// TestVerifierWaitText_WorkingLaneNamesNoWait checks that the working lane
// line stays plain: it must not read as waiting on a verifier slot.
func TestVerifierWaitText_WorkingLaneNamesNoWait(t *testing.T) {
	out := RenderLaneFillText(verifierWaitTextStatus(), 0)
	for _, lane := range strings.Split(out, "\n") {
		if strings.HasPrefix(lane, "claude-1:") {
			if strings.Contains(lane, "waiting on verifier") {
				t.Errorf("working lane must not say it waits on a verifier slot: %q", lane)
			}
			if want := "claude-1: occupied by p1 \"Add paging\" (claude-code)"; lane != want {
				t.Errorf("working lane line changed: got %q, want %q", lane, want)
			}
		}
	}
}

// TestVerifierWaitText_DecisionShowsWaitReason checks that a decision line
// carrying a WaitReason names it.
func TestVerifierWaitText_DecisionShowsWaitReason(t *testing.T) {
	out := RenderLaneFillText(verifierWaitTextStatus(), 0)
	for _, lane := range strings.Split(out, "\n") {
		if !strings.Contains(lane, "start_prd") {
			continue
		}
		if !strings.Contains(lane, "CI still running the checks") {
			t.Errorf("decision line must show the wait reason, got %q", lane)
		}
	}
}

// TestVerifierWaitText_DecisionWithoutWaitReasonStaysPlain checks that a
// decision with no WaitReason adds nothing about the verifier wait.
func TestVerifierWaitText_DecisionWithoutWaitReasonStaysPlain(t *testing.T) {
	st := verifierWaitTextStatus()
	st.Decisions[0].WaitReason = ""
	out := RenderLaneFillText(st, 0)
	decision := ""
	for _, lane := range strings.Split(out, "\n") {
		if strings.Contains(lane, "start_prd") {
			decision = lane
		}
	}
	if decision == "" {
		t.Fatalf("no decision line in output:\n%s", out)
	}
	if strings.Contains(decision, "verifier") {
		t.Errorf("decision without a wait reason must not mention the verifier wait: %q", decision)
	}
}
