// datawatch autonomous supervisor-watch — the supervisor watch read over the
// CLI. The body of GET /api/autonomous/supervisor-watch is rendered as the
// mode, the last poll, one line per finding and the comparison summary;
// --json prints the body untouched.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

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
			var parsed autonomous.SupervisorWatchStatus
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			autonomous.RenderSupervisorWatch(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
