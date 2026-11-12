// datawatch autonomous merge and datawatch autonomous safe-to-delete — the
// merge reads over the CLI. The bodies of GET /api/autonomous/merge and
// GET /api/autonomous/safe-to-delete are rendered by the shared formatters
// (internal/autonomous/merge_text.go), the same text the comm verbs show.
// Both are reads: nothing is merged, deleted or pushed.

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousMergeCmd is the `datawatch autonomous merge` subcommand: the
// merge-pending mode, the alert threshold and the finished Automata whose
// work branch is not on main yet.
func newAutonomousMergeCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "merge",
		Short: "Show the merge state of finished Automata and the alert threshold",
		Long: "Show the merge-pending mode in effect, the alert threshold, and every " +
			"finished Automaton with a work branch, with how far ahead of main the " +
			"branch is. In shadow mode it also lists the decisions the check would " +
			"have acted on. A read: nothing is merged, deleted or pushed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/merge", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			ms, err := autonomous.ParseMergeStatus(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderMergeState(ms))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousSafeToDeleteCmd is the `datawatch autonomous safe-to-delete`
// subcommand: which Automaton branches and worktrees can be deleted and
// which are held back.
func newAutonomousSafeToDeleteCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "safe-to-delete",
		Short: "List the Automaton branches and worktrees that are safe to delete",
		Long: "Show which finished Automaton work branches and worktrees are safe to " +
			"delete and which are held back (not merged yet, or uncommitted changes). " +
			"A report only: nothing is deleted.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/safe-to-delete", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			r, err := autonomous.ParseSafeToDelete(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderSafeToDelete(r))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
