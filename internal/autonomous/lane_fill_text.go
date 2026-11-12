package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
// lane counts, the queued eligible counts, any pool evidence error, then the newest decisions.
// maxDecisions caps how many decisions are shown; 0 or less shows all of them.
func RenderLaneFillText(st LaneFillStatus, maxDecisions int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lane fill: mode %s\n", st.Mode)
	if (st.Modes.GPU != "" && st.Modes.GPU != st.Mode) || (st.Modes.Claude != "" && st.Modes.Claude != st.Mode) {
		fmt.Fprintf(&b, "Mode per lane kind: gpu %s, claude %s\n", st.Modes.GPU, st.Modes.Claude)
	}
	for _, l := range st.Lanes {
		fmt.Fprintf(&b, "%s: %s\n", l.ID, laneFillLaneText(l))
	}
	if line := laneFillCountsText(st.LaneCounts); line != "" {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "Queued eligible: gpu %d, claude %d\n", st.QueuedEligible.GPU, st.QueuedEligible.Claude)
	if st.Evidence.Error != "" {
		fmt.Fprintf(&b, "Pool evidence error: %s\n", st.Evidence.Error)
	}
	if len(st.Evidence.Degraded) > 0 {
		b.WriteString("Degraded hosts (not blocking the lanes):\n")
		for _, d := range st.Evidence.Degraded {
			fmt.Fprintf(&b, "  %s: %s (%s)\n", d.Host, d.Error, laneFillDegradedFor(d.Since))
		}
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

// laneFillCountsText is the one lane counts line: for each lane kind the
// effective count, the configured count and where the effective one came from,
// as "Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)". A status
// with no source for either kind (an older daemon) gives no line.
func laneFillCountsText(c LaneFillLaneCounts) string {
	if c.GPU.Source == "" && c.Claude.Source == "" {
		return ""
	}
	return fmt.Sprintf("Lane counts: gpu %s, claude %s", laneFillCountText(c.GPU), laneFillCountText(c.Claude))
}

// laneFillCountText is "effective of configured (source)".
func laneFillCountText(c LaneFillLaneCount) string {
	src := c.Source
	switch src {
	case LaneCountSourceCaps:
		src = "LLM caps"
	case "":
		src = "unknown"
	}
	return fmt.Sprintf("%d of %d (%s)", c.Effective, c.Configured, src)
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
				s += fmt.Sprintf(", waiting on verifier: %s", r)
			} else {
				s += ", waiting on verifier"
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
	// A refusal shows its reason code in brackets in front of the reason text.
	switch {
	case d.EligibilityReason != "" && d.Reason != "":
		s += ": [" + d.EligibilityReason + "] " + d.Reason
	case d.EligibilityReason != "":
		s += ": [" + d.EligibilityReason + "]"
	case d.Reason != "":
		s += ": " + d.Reason
	}
	if d.Blocker != "" && d.Blocker != d.Reason {
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

// laneFillDegradedFor says how long a host has been unreachable, from the time
// it first failed.
func laneFillDegradedFor(since time.Time) string {
	if since.IsZero() {
		return "unreachable"
	}
	d := time.Since(since)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("unreachable for %d min", int(d/time.Minute))
}
