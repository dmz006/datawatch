package autonomous

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Supervisor watch operator rendering — the one place the read view is
// worded for a person. The CLI (`datawatch autonomous supervisor-watch`)
// and the comm verb (`autonomous supervisor-watch`) both call
// RenderSupervisorWatch, so the two surfaces return the same lines.

// SupervisorWatchFindingState words a finding's standing for the
// operator: resolved, alerted (a real alert was raised), would alert
// (shadow marked it alert-due without raising one) or open.
func SupervisorWatchFindingState(f *SupervisorWatchFinding) string {
	switch {
	case f.Resolved:
		return "resolved"
	case f.AlertedAt != nil:
		return "alerted"
	case f.WouldAlert:
		return "would alert"
	}
	return "open"
}

// RenderSupervisorWatch writes the read view for an operator: the mode
// line, the last poll, one line per finding (kind, state, last seen, the
// operator line) or an explicit no-findings note, then the comparison
// summary.
func RenderSupervisorWatch(w io.Writer, body *SupervisorWatchStatus) {
	_, _ = fmt.Fprintf(w, "Supervisor watch mode: %s\n", body.Mode)
	if body.LastPollAt.IsZero() {
		_, _ = fmt.Fprintln(w, "Last poll: never")
	} else {
		_, _ = fmt.Fprintf(w, "Last poll: %s\n", body.LastPollAt.Format("2006-01-02 15:04:05"))
	}

	if len(body.Findings) == 0 {
		_, _ = fmt.Fprintln(w, "No supervisor watch findings.")
	} else {
		_, _ = fmt.Fprintf(w, "Findings: %d\n", len(body.Findings))
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, f := range body.Findings {
			lastSeen := "-"
			if !f.LastSeenAt.IsZero() {
				lastSeen = f.LastSeenAt.Format("2006-01-02 15:04:05")
			}
			text := f.Text
			if text == "" {
				text = f.Subject
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Kind, SupervisorWatchFindingState(f), lastSeen, text)
		}
		_ = tw.Flush()
	}

	cmp := body.Comparison
	if cmp.Last == nil {
		_, _ = fmt.Fprintf(w, "Comparison: none recorded yet (days of full agreement: %d)\n", cmp.AgreedDays)
		return
	}
	verdict := "agreed"
	if !cmp.Last.Agreed {
		verdict = "differs"
	}
	_, _ = fmt.Fprintf(w, "Comparison: %s at %s (days of full agreement: %d)\n",
		verdict, cmp.Last.At.Format("2006-01-02 15:04:05"), cmp.AgreedDays)
	if len(cmp.OnlyWatch) > 0 {
		_, _ = fmt.Fprintf(w, "  only the watch raised: %s\n", strings.Join(cmp.OnlyWatch, ", "))
	}
	if len(cmp.OnlyScript) > 0 {
		_, _ = fmt.Fprintf(w, "  only the script raised: %s\n", strings.Join(cmp.OnlyScript, ", "))
	}
}
