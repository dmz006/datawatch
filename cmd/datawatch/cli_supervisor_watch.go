// datawatch autonomous supervisor-watch — the supervisor watch read over the
// CLI. The body of GET /api/autonomous/supervisor-watch is rendered as the
// mode, the last poll, one line per finding and the comparison summary;
// --json prints the body untouched.

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// supervisorWatchState is the GET /api/autonomous/supervisor-watch body: the
// mode in effect, the last poll instant, the stored findings (open first)
// and the comparison view against the supervisor script's record file.
type supervisorWatchState struct {
	Mode       string                                   `json:"mode"`
	LastPollAt time.Time                                `json:"last_poll_at"`
	Findings   []*autonomous.SupervisorWatchFinding     `json:"findings"`
	Comparison autonomous.SupervisorWatchComparisonView `json:"comparison"`
}

// supervisorWatchFindingState words a finding's standing for the operator:
// resolved, alerted (a real alert was raised), would alert (shadow marked it
// alert-due without raising one) or open.
func supervisorWatchFindingState(f *autonomous.SupervisorWatchFinding) string {
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

// renderSupervisorWatch renders the watch state body: the mode line, the last
// poll, one line per finding (kind, state, last seen, the operator line) or an
// explicit no-findings note, then the comparison summary.
func renderSupervisorWatch(w *strings.Builder, body *supervisorWatchState) {
	fmt.Fprintf(w, "Supervisor watch mode: %s\n", body.Mode)
	if body.LastPollAt.IsZero() {
		fmt.Fprintln(w, "Last poll: never")
	} else {
		fmt.Fprintf(w, "Last poll: %s\n", body.LastPollAt.Format("2006-01-02 15:04:05"))
	}

	if len(body.Findings) == 0 {
		fmt.Fprintln(w, "No supervisor watch findings.")
	} else {
		fmt.Fprintf(w, "Findings: %d\n", len(body.Findings))
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
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Kind, supervisorWatchFindingState(f), lastSeen, text)
		}
		tw.Flush() //nolint:errcheck
	}

	cmp := body.Comparison
	if cmp.Last == nil {
		fmt.Fprintf(w, "Comparison: none recorded yet (days of full agreement: %d)\n", cmp.AgreedDays)
		return
	}
	verdict := "agreed"
	if !cmp.Last.Agreed {
		verdict = "differs"
	}
	fmt.Fprintf(w, "Comparison: %s at %s (days of full agreement: %d)\n",
		verdict, cmp.Last.At.Format("2006-01-02 15:04:05"), cmp.AgreedDays)
	if len(cmp.OnlyWatch) > 0 {
		fmt.Fprintf(w, "  only the watch raised: %s\n", strings.Join(cmp.OnlyWatch, ", "))
	}
	if len(cmp.OnlyScript) > 0 {
		fmt.Fprintf(w, "  only the script raised: %s\n", strings.Join(cmp.OnlyScript, ", "))
	}
}

// newAutonomousSupervisorWatchCmd is the `datawatch autonomous
// supervisor-watch` subcommand: the supervisor watch mode, last poll, stored
// findings and the comparison against the supervisor script's output.
func newAutonomousSupervisorWatchCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "supervisor-watch",
		Short: "Read the supervisor watch: mode, last poll, findings and the script comparison",
		Long: "Show the supervisor watch state — the mode in effect (off, shadow or on; " +
			"shadow reports only and never acts), the instant the coordinator last " +
			"polled, one line per stored finding (kind, state, last seen, the " +
			"operator line), open findings first, and the comparison summary against " +
			"the supervisor script's output (the last comparison, the days of full " +
			"agreement and the one-sided differences). A read: nothing is written and " +
			"no alert is raised.\n\n" +
			"--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/supervisor-watch", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed supervisorWatchState
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			renderSupervisorWatch(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
