// datawatch autonomous housekeeping-repairs — the housekeeping repairs read
// over the CLI: per group the mode, what it would do and what it did.
// --json prints the body untouched. A group's mode changes only through the
// existing config set paths.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newAutonomousHousekeepingRepairsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "housekeeping-repairs",
		Short: "Read the housekeeping repairs: per group the mode, what it would do and what it did",
		Long: "Show the housekeeping repairs the daemon makes on stuck Automata and tasks: per " +
			"group (task_chain, advance_judge, lane_fill, leftovers, planning) the mode in effect (off, " +
			"report or on), what a report-mode pass would do, what an on-mode pass did, and " +
			"the cases that failed or were skipped as unsafe. A read: nothing is repaired, " +
			"no alert is raised and no mode changes. Change a mode with config set, key " +
			"autonomous.housekeeping_repairs.<group>.\n\n" +
			"--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/housekeeping-repairs", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed autonomous.HousekeepingRepairsView
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			autonomous.RenderHousekeepingRepairs(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
