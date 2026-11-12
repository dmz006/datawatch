package autonomous

// overlap_text.go renders the overlap read for operators. The CLI and the
// comm verb both print the same line for each warning, so an operator sees
// one wording wherever they ask. The read is GET /api/autonomous/overlap.

import (
	"encoding/json"
	"strconv"
	"strings"
)

// overlapNoneText is the one line printed when the read finds no warnings.
const overlapNoneText = "No overlaps found."

// ParseOverlapWarnings reads the warnings out of a GET /api/autonomous/overlap body.
func ParseOverlapWarnings(body []byte) ([]OverlapWarning, error) {
	var reply struct {
		Warnings []OverlapWarning `json:"warnings"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, err
	}
	return reply.Warnings, nil
}

// RenderOverlapText renders one line per warning, each ending in a newline,
// or the none-found line when there are no warnings.
func RenderOverlapText(ws []OverlapWarning) string {
	if len(ws) == 0 {
		return overlapNoneText + "\n"
	}
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(renderOverlapLine(w))
		b.WriteString("\n")
	}
	return b.String()
}

// renderOverlapLine is one warning: the Automaton id and status, or the
// registry row, or the branch, then the score and the reason. An unavailable
// warning is not scored, so its line is the reason alone.
func renderOverlapLine(w OverlapWarning) string {
	score := strconv.FormatFloat(w.Score, 'f', 2, 64)
	switch w.Kind {
	case warningKindAutomaton:
		return "automaton " + w.ID + " status " + w.Status + " score " + score + ": " + w.Reason
	case warningKindRegistry:
		return "registry row " + strconv.Itoa(w.Row) + " score " + score + ": " + w.Reason
	case warningKindBranch:
		return "branch " + w.Branch + " score " + score + ": " + w.Reason
	default:
		return "unavailable: " + w.Reason
	}
}
