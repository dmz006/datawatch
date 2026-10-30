// Merge read text: the CLI, the MCP result and the comm channel render the
// bodies of GET /api/autonomous/merge and GET /api/autonomous/safe-to-delete
// through this file, so no surface shows a different wording. The merge-pending
// alert body is not rendered here; it comes from mergePendingAlertBody.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseMergeStatus reads the body of GET /api/autonomous/merge.
func ParseMergeStatus(body []byte) (MergeStatus, error) {
	var ms MergeStatus
	if err := json.Unmarshal(body, &ms); err != nil {
		return MergeStatus{}, fmt.Errorf("reading the Automaton merge status: %w", err)
	}
	return ms, nil
}

// ParseSafeToDelete reads the body of GET /api/autonomous/safe-to-delete.
func ParseSafeToDelete(body []byte) (SafeDeleteReport, error) {
	var r SafeDeleteReport
	if err := json.Unmarshal(body, &r); err != nil {
		return SafeDeleteReport{}, fmt.Errorf("reading the Automaton safe-to-delete report: %w", err)
	}
	return r, nil
}

func mergeThresholdText(minutes int) string {
	if minutes <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d min", minutes)
}

func mergeTimeText(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

// RenderMergeState formats the merge read: the mode in effect and the alert
// threshold, one line per finished Automaton with a work branch, and in shadow
// mode the decisions the pass would have acted on, newest first. It only
// describes; nothing it mentions has been merged, deleted or pushed.
func RenderMergeState(ms MergeStatus) string {
	var b strings.Builder
	mode := ms.Mode
	if mode == "" {
		mode = "off"
	}
	fmt.Fprintf(&b, "Automaton merge pending: mode %s (alert threshold: %s)\n", mode, mergeThresholdText(ms.AlertMinutes))
	if mode == "off" {
		b.WriteString("The merge-pending check is off, so no Automata are listed.\n")
		return b.String()
	}
	if len(ms.Automata) == 0 {
		b.WriteString("No finished Automata with a work branch.\n")
	} else {
		fmt.Fprintf(&b, "%d finished Automat%s with a work branch:\n", len(ms.Automata), pluralN(len(ms.Automata)))
		for _, e := range ms.Automata {
			name := e.Name
			if name == "" {
				name = e.AutomatonID
			}
			fmt.Fprintf(&b, "  Automaton %q (id %s): branch %s is %s, %d commit(s) ahead of %s",
				name, e.AutomatonID, e.Branch, e.State, e.CommitsAhead, mergeBaseText(e.Base))
			if e.PendingSince != nil {
				fmt.Fprintf(&b, ", merge pending since %s", mergeTimeText(e.PendingSince))
			}
			if e.AlertedAt != nil {
				fmt.Fprintf(&b, ", alerted at %s", mergeTimeText(e.AlertedAt))
			}
			if e.WouldAlert != nil && *e.WouldAlert {
				b.WriteString(", would alert")
			}
			b.WriteString("\n")
		}
	}
	if len(ms.ShadowDecisions) > 0 {
		fmt.Fprintf(&b, "Shadow decisions (nothing was sent), newest first:\n")
		for _, d := range ms.ShadowDecisions {
			name := d.Name
			if name == "" {
				name = d.AutomatonID
			}
			alert := "no alert"
			if d.WouldAlert {
				alert = "would alert"
			}
			fmt.Fprintf(&b, "  %s Automaton %q (id %s): branch %s %s, %d commit(s) ahead, %s",
				d.At.Format(time.RFC3339), name, d.AutomatonID, d.Branch, d.State, d.CommitsAhead, alert)
			if d.RepeatCount > 1 {
				fmt.Fprintf(&b, " (seen %d times, last %s)", d.RepeatCount, d.LastSeenAt.Format(time.RFC3339))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("Nothing has been merged, deleted or pushed.\n")
	return b.String()
}

// RenderSafeToDelete formats the safe-to-delete report: the Automaton
// branches and worktrees that can be deleted and those held back, with why.
// It is a report only; deleting stays a separate step the operator approves.
func RenderSafeToDelete(r SafeDeleteReport) string {
	var b strings.Builder
	if len(r.Safe) == 0 && len(r.Held) == 0 {
		b.WriteString("No finished Automaton branches or worktrees to review.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Automaton branches safe to delete: %d, held back: %d\n", len(r.Safe), len(r.Held))
	if len(r.Safe) > 0 {
		b.WriteString("Safe to delete:\n")
		for _, s := range r.Safe {
			fmt.Fprintf(&b, "  %s (%d commit(s)): %s\n", s.Branch, s.Commits, s.Reason)
			if s.Worktree != "" {
				fmt.Fprintf(&b, "    worktree: %s\n", s.Worktree)
			}
		}
	}
	if len(r.Held) > 0 {
		b.WriteString("Held back:\n")
		for _, h := range r.Held {
			fmt.Fprintf(&b, "  %s: %s", h.Branch, h.State)
			if h.Dirty {
				b.WriteString(", uncommitted changes")
			}
			if h.Note != "" {
				fmt.Fprintf(&b, " — %s", strings.ReplaceAll(h.Note, "\n", " "))
			}
			b.WriteString("\n")
			if h.Worktree != "" {
				fmt.Fprintf(&b, "    worktree: %s\n", h.Worktree)
			}
		}
	}
	if r.HeldDirty > 0 {
		fmt.Fprintf(&b, "%d branch(es) would otherwise be safe but have uncommitted changes in their worktree.\n", r.HeldDirty)
	}
	b.WriteString("This is a report only: nothing has been deleted.\n")
	return b.String()
}

// MergePendingAlertBody is the merge-pending alert body for one record, for
// the surfaces and tests that must show the alert text exactly as raised.
func MergePendingAlertBody(rec *TriageRecord, now time.Time) string {
	return mergePendingAlertBody(rec, now)
}
