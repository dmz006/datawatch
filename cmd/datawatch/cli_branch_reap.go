// datawatch autonomous branch-reap, branch-reap-run and branch-reap-restore —
// the branch reaper over the CLI. The bodies of the three routes are rendered
// by the shared formatters (internal/autonomous/reap_text.go), the same text
// the comm verbs show. branch-reap is a read: it lists what would be reaped
// and holds nothing; branch-reap-run backs up each listed branch and then
// deletes it; branch-reap-restore recreates one reaped branch from its
// newest backup ref.

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousBranchReapCmd is the `datawatch autonomous branch-reap`
// subcommand: the read-side pass — the task sub-branches that would be
// reaped, the ones held, and every backup ref that exists.
func newAutonomousBranchReapCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "branch-reap",
		Short: "Show the branch reap report: what would be reaped, what is held, the backup refs",
		Long: "Show which task sub-branches a pass would reap (settled, contained, backed up), " +
			"which are held (still landing or uncontained), and every backup ref under " +
			"refs/datawatch/reap-backup/ with the branch and tip it holds. A report only: " +
			"nothing is backed up, deleted or restored.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/branch-reap", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			rep, err := autonomous.ParseReapSurfaceReport(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderReapReport(rep))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousBranchReapRunCmd performs one reap pass: every listed branch is
// backed up first and then deleted with git branch -D.
func newAutonomousBranchReapRunCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "branch-reap-run",
		Short: "Perform one branch reap pass: each listed branch is backed up first, then deleted",
		Long: "For every Automaton whose landing state has settled, each task sub-branch whose " +
			"commits are all on the base is backed up to refs/datawatch/reap-backup/ first and " +
			"then deleted. Automata still landing, uncontained branches and a merge in progress " +
			"are held and listed. A reap already in progress answers a conflict, not a silent skip.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asJSON {
				body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/branch-reap/run", nil)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(append(body, '\n'))
				return err
			}
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/branch-reap/run", nil)
			if err != nil {
				return err
			}
			rep, err := autonomous.ParseReapReport(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderReapRun(rep))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousBranchReapRestoreCmd recreates one reaped branch from its
// newest backup ref.
func newAutonomousBranchReapRestoreCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "branch-reap-restore <branch>",
		Short: "Recreate one reaped branch from its newest backup ref",
		Long: "Re-point the branch at the newest tip a previous pass backed up under " +
			"refs/datawatch/reap-backup/. The branch name is the task sub-branch from the " +
			"branch-reap report. A branch that was never reaped answers not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/branch-reap/restore", map[string]string{"branch": args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(append(body, '\n'))
				return err
			}
			res, err := autonomous.ParseReapRestoreResult(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderReapRestore(res))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
