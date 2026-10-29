package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file reads the lane fill status body and formats it as text. The CLI
// and the comm channel both render through it, so the two surfaces show the
// same lines.

// ParseLaneFillStatus decodes the body GET /api/autonomous/lane-fill returns.
func ParseLaneFillStatus(body []byte) (LaneFillStatus, error) {
	var st LaneFillStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return LaneFillStatus{}, fmt.Errorf("reading the lane fill status: %w", err)
	}
	return st, nil
}

// RenderLaneFillText formats the status: the mode, one line per lane, the
// queued eligible counts, any pool evidence error, then the newest decisions.
// maxDecisions caps how many decisions are shown; 0 or less shows all of them.
func RenderLaneFillText(st LaneFillStatus, maxDecisions int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lane fill: mode %s\n", st.Mode)
	for _, l := range st.Lanes {
		fmt.Fprintf(&b, "%s: %s\n", l.ID, laneFillLaneText(l))
	}
	fmt.Fprintf(&b, "Queued eligible: gpu %d, claude %d\n", st.QueuedEligible.GPU, st.QueuedEligible.Claude)
	if st.Evidence.Error != "" {
		fmt.Fprintf(&b, "Pool evidence error: %s\n", st.Evidence.Error)
	}
	decisions := st.Decisions
	if maxDecisions > 0 && len(decisions) > maxDecisions {
		decisions = decisions[:maxDecisions]
	}
	if len(decisions) == 0 {
		b.WriteString("No decisions yet.\n")
		return b.String()
	}
	b.WriteString("Newest decisions:\n")
	for _, d := range decisions {
		fmt.Fprintf(&b, "  %s\n", laneFillDecisionText(d))
	}
	return b.String()
}

// laneFillLaneText says what a lane is running, or why it is idle.
func laneFillLaneText(l LaneFillLaneStatus) string {
	var s string
	switch {
	case l.Occupant != nil && l.Occupant.PRDID == "":
		s = "occupied by a pool session outside any Automaton"
	case l.Occupant != nil:
		s = fmt.Sprintf("occupied by %s %q (%s)", l.Occupant.PRDID, l.Occupant.Title, l.Occupant.Backend)
		if l.Occupant.WaitingOnVerifier {
			// A lane that works and a lane that waits must read differently.
			if r := strings.TrimSpace(l.Occupant.WaitReason); r != "" {
				s += fmt.Sprintf(", waiting on a verifier slot: %s", r)
			} else {
				s += ", waiting on a verifier slot"
			}
		}
	case l.Blocker != "":
		s = "idle, " + l.Blocker
	default:
		s = "idle"
	}
	if l.EligibleWaiting > 0 {
		s += fmt.Sprintf(", %d eligible waiting", l.EligibleWaiting)
	}
	return s
}

// laneFillDecisionText is one decision on one line, with what became of it.
func laneFillDecisionText(d LaneFillDecision) string {
	s := fmt.Sprintf("%s %s %s", d.At.Format("15:04:05"), d.LaneID, d.Action)
	if d.PRDID != "" {
		s += " " + d.PRDID
	}
	if d.Reason != "" {
		s += ": " + d.Reason
	}
	if d.Blocker != "" {
		s += " (blocked: " + d.Blocker + ")"
	}
	if d.WaitReason != "" {
		s += fmt.Sprintf(", lane counted available by the verifier wait: %s", d.WaitReason)
	}
	switch {
	case d.Error != "":
		s += " [error: " + d.Error + "]"
	case d.Executed:
		s += " [executed]"
	default:
		s += " [not executed]"
	}
	if d.RepeatCount > 1 {
		s += fmt.Sprintf(" (seen %d times)", d.RepeatCount)
	}
	return s
}
