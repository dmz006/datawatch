// Merged-member text: the CLI and the comm channel both render the body of
// GET /api/automations/{id}/merged-members through this file, so no surface
// shows a different wording.
package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseMergedMembers reads the body of GET /api/automations/{id}/merged-members.
func ParseMergedMembers(body []byte) ([]MergedMemberRow, error) {
	var b struct {
		MergedMembers []MergedMemberRow `json:"merged_members"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("reading the merged-member decisions: %w", err)
	}
	if b.MergedMembers == nil {
		b.MergedMembers = []MergedMemberRow{}
	}
	return b.MergedMembers, nil
}

// RenderMergedMemberRow is one decision on one line: the Automaton, the
// member, the outcome, the branch with the default branch it was proved merged
// into, the proving commit, the policy and the time.
func RenderMergedMemberRow(r MergedMemberRow) string {
	who := r.AutomationName
	if who == "" {
		who = r.AutomationID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s | member %s %s | branch %s on %s | commit %s | policy %s", who, r.Member, r.Outcome, r.Branch, r.DefaultBranch, r.ProvingCommit, r.Policy)
	if !r.At.IsZero() {
		b.WriteString(" | " + r.At.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// RenderMergedMembers lists the decisions, newest first, one line each. max
// caps the lines (0 means all). An empty list says so.
func RenderMergedMembers(rows []MergedMemberRow, max int) string {
	if len(rows) == 0 {
		return "No merged-member decisions.\n"
	}
	var b strings.Builder
	n := 0
	for i := len(rows) - 1; i >= 0; i-- {
		if max > 0 && n >= max {
			break
		}
		b.WriteString(RenderMergedMemberRow(rows[i]) + "\n")
		n++
	}
	return b.String()
}
