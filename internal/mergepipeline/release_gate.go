package mergepipeline

import "github.com/dmz006/datawatch/internal/config"

// ReleaseGateAction is what the release gate does with a prepared release.
type ReleaseGateAction string

const (
	// ReleaseGateShip ships the release now.
	ReleaseGateShip ReleaseGateAction = "ship"
	// ReleaseGateHoldForDecision holds the release until the operator decides.
	ReleaseGateHoldForDecision ReleaseGateAction = "hold_for_decision"
	// ReleaseGateHoldForPerson holds the release until a person ships it or
	// looks at a failed check.
	ReleaseGateHoldForPerson ReleaseGateAction = "hold_for_person"
)

// ReleaseGateOutcome is the decision DecideRelease returns for one release.
// Mode is the approval mode the decision was made under: an unknown input
// mode is reported as config.ReleaseApprovalAsk.
type ReleaseGateOutcome struct {
	Mode   string
	Size   string
	Action ReleaseGateAction
	Reason string
}

// DecideRelease maps a release's approval mode and whether its checks passed
// to the gate action. It is pure: it performs no I/O.
//
//   - auto with passing checks ships.
//   - auto with failing checks holds for a person.
//   - ask holds for a decision.
//   - manual holds for a person.
//   - any other mode is treated as ask, so an unknown mode never ships.
func DecideRelease(mode, size string, checksPassed bool) ReleaseGateOutcome {
	switch mode {
	case config.ReleaseApprovalAuto:
		if checksPassed {
			return ReleaseGateOutcome{
				Mode:   mode,
				Size:   size,
				Action: ReleaseGateShip,
				Reason: "checks passed; shipping automatically",
			}
		}
		return ReleaseGateOutcome{
			Mode:   mode,
			Size:   size,
			Action: ReleaseGateHoldForPerson,
			Reason: "checks failed; a person needs to look before this ships",
		}
	case config.ReleaseApprovalManual:
		return ReleaseGateOutcome{
			Mode:   mode,
			Size:   size,
			Action: ReleaseGateHoldForPerson,
			Reason: "prepared; a person ships this release",
		}
	default:
		// config.ReleaseApprovalAsk, and any unrecognised mode.
		return ReleaseGateOutcome{
			Mode:   config.ReleaseApprovalAsk,
			Size:   size,
			Action: ReleaseGateHoldForDecision,
			Reason: "prepared; waiting for your decision",
		}
	}
}
