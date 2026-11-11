// Unlanded-work read text: the CLI, the MCP result and the comm channel render
// the body of GET /api/autonomous/unlanded-work through this file, so no
// surface shows a different wording.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UnlandedWorkList is the body of GET /api/autonomous/unlanded-work.
type UnlandedWorkList struct {
	Branches []UnlandedBranch `json:"branches"`
}

// ParseUnlandedWorkList reads the body of GET /api/autonomous/unlanded-work.
func ParseUnlandedWorkList(body []byte) (UnlandedWorkList, error) {
	var l UnlandedWorkList
	if err := json.Unmarshal(body, &l); err != nil {
		return UnlandedWorkList{}, fmt.Errorf("reading the Automaton unlanded-work list: %w", err)
	}
	if l.Branches == nil {
		l.Branches = []UnlandedBranch{}
	}
	return l, nil
}

// RenderUnlandedWorkList formats the unlanded-work read: one block per
// archived Automaton branch whose commits are not on the default branch, with
// the title, the commit count, the last activity and the recommendation. It
// only describes; the operator keeps, archives or deletes a branch with the
// action commands.
func RenderUnlandedWorkList(l UnlandedWorkList) string {
	var b strings.Builder
	if len(l.Branches) == 0 {
		b.WriteString("No archived Automaton has work that is not on the default branch.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Automaton work not on the default branch (%d):\n", len(l.Branches))
	for _, u := range l.Branches {
		title := u.Title
		if title == "" {
			title = u.AutomatonID
		}
		fmt.Fprintf(&b, "  - %s (%s)\n", title, u.Branch)
		commits := "commits"
		if u.Commits == 1 {
			commits = "commit"
		}
		line := fmt.Sprintf("%d %s", u.Commits, commits)
		if !u.LastActivity.IsZero() {
			line += ", last activity " + u.LastActivity.UTC().Format("2006-01-02 15:04") + " UTC"
		}
		b.WriteString("      " + line + "\n")
		if u.Recommendation != "" {
			rec := "recommended: " + u.Recommendation
			if u.Reason != "" {
				rec += " (" + u.Reason + ")"
			}
			b.WriteString("      " + rec + "\n")
		}
	}
	return b.String()
}

// RenderUnlandedActionResult words the reply of one unlanded-work action
// (route is keep, archive-branch or delete-worktree) for the CLI and the chat
// channel. An unreadable body is shown as it came.
func RenderUnlandedActionResult(route string, body []byte) string {
	var r struct {
		Branch     string `json:"branch"`
		ArchiveRef string `json:"archive_ref"`
		BundlePath string `json:"bundle_path"`
		Worktree   string `json:"worktree_path"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Branch == "" {
		return strings.TrimSpace(string(body))
	}
	switch route {
	case "keep":
		return "Kept " + r.Branch + ": it is left alone."
	case "archive-branch":
		return "Archived " + r.Branch + " as " + r.ArchiveRef + "."
	default:
		s := "Deleted the worktree and branch " + r.Branch + "; backup bundle: " + r.BundlePath
		if r.Worktree != "" {
			s += " (worktree " + r.Worktree + ")"
		}
		return s + "."
	}
}
