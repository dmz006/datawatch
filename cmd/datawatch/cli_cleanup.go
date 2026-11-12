// datawatch autonomous cleanup — the cleanup status read over the CLI.
// The body of GET /api/autonomous/cleanup is rendered by the shared
// formatter (internal/autonomous/cleanup_text.go), the same text the
// comm verb shows. A read: nothing is removed.

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousCleanupCmd is the `datawatch autonomous cleanup`
// subcommand: both cleanup passes, the two disk readings and the
// rate-limited disk alert.
func newAutonomousCleanupCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "cleanup",
		Short: "Show the Automaton cleanup status: both passes, the disk readings and the disk alert",
		Long: "Show both cleanup passes (the worktrees pass and the daemon " +
			"temp-file sweep) with their mode, last run, the removed / " +
			"would-remove / skipped / failed counts, the bytes freed, the last " +
			"error and the owner-vs-net split; the two disk readings the " +
			"cleanup guards; and the rate-limited disk alert (the threshold, " +
			"whether it is active and when it last raised). A read: nothing " +
			"is removed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/cleanup", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			st, err := autonomous.ParseCleanupStatus(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderCleanupStatus(st))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
