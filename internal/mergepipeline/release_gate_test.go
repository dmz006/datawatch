package mergepipeline

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDecideRelease(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		checksPassed bool
		wantMode     string
		wantAction   ReleaseGateAction
		wantReason   string
	}{
		{"auto passing ships", config.ReleaseApprovalAuto, true, config.ReleaseApprovalAuto, ReleaseGateShip, "checks passed; shipping automatically"},
		{"auto failing holds for person", config.ReleaseApprovalAuto, false, config.ReleaseApprovalAuto, ReleaseGateHoldForPerson, "checks failed; a person needs to look before this ships"},
		{"ask passing holds for decision", config.ReleaseApprovalAsk, true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"ask failing holds for decision", config.ReleaseApprovalAsk, false, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"manual passing holds for person", config.ReleaseApprovalManual, true, config.ReleaseApprovalManual, ReleaseGateHoldForPerson, "prepared; a person ships this release"},
		{"manual failing holds for person", config.ReleaseApprovalManual, false, config.ReleaseApprovalManual, ReleaseGateHoldForPerson, "prepared; a person ships this release"},
		{"unknown mode never ships", "yolo", true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"empty mode never ships", "", true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"case-mismatched mode never ships", "AUTO", true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideRelease(tc.mode, "minor", tc.checksPassed)
			if got.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, tc.wantAction)
			}
			if got.Mode != tc.wantMode {
				t.Errorf("Mode = %q, want %q", got.Mode, tc.wantMode)
			}
			if got.Size != "minor" {
				t.Errorf("Size = %q, want %q", got.Size, "minor")
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

func TestDecideReleaseActionValues(t *testing.T) {
	if string(ReleaseGateShip) != "ship" ||
		string(ReleaseGateHoldForDecision) != "hold_for_decision" ||
		string(ReleaseGateHoldForPerson) != "hold_for_person" {
		t.Fatal("ReleaseGateAction string values changed")
	}
}
