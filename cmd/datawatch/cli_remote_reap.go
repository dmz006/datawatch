// datawatch autonomous remote-reap, remote-reap-run and remote-reap-restore —
// the remote branch reaper over the CLI. The bodies of the three routes are
// rendered by the shared formatters (internal/autonomous/remote_reap_text.go),
// the same text the comm verbs show. remote-reap is a read: it lists which
// Automaton work branches on origin would be reaped and which are held, and
// touches nothing on origin; remote-reap-run deletes nothing while
// autonomous.remote_reap_enabled is false or unset (the default) — the same
// evaluation runs and the report says report_only; remote-reap-restore
// recreates one reaped branch from its newest backup ref.

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousRemoteReapCmd is the `datawatch autonomous remote-reap`
// subcommand: the read-side pass on origin — the Automaton work branches
// that would be reaped, the ones held, and every backup ref that exists
// under refs/datawatch/remote-reap-backup/.
func newAutonomousRemoteReapCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "remote-reap",
		Short: "Show the remote reap report on origin: what would be reaped, what is held, the backup refs",
		Long: "Show which Automaton work branches a pass would delete from origin (fully merged " +
			"into the project's default branch and at least the retention window old, whole days), " +
			"which are held (not merged, still inside the retention window, or no remote ref), and " +
			"every backup ref under refs/datawatch/remote-reap-backup/ with the branch and tip it " +
			"holds. A report only: no ref is written and nothing on origin is touched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/remote-reap", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			rep, err := autonomous.ParseRemoteReapSurfaceReport(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderRemoteReapReport(rep))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousRemoteReapRunCmd performs one remote reap pass: each
// qualifying branch is backed up first, the backup ref is verified to point
// at the branch tip, and only then is the branch deleted from origin.
func newAutonomousRemoteReapRunCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "remote-reap-run",
		Short: "Perform one remote reap pass: each qualifying branch is backed up, verified, then deleted on origin",
		Long: "Each Automaton work branch that is fully merged into the project's default branch " +
			"and at least the retention window old is backed up to " +
			"refs/datawatch/remote-reap-backup/<branch>/<unix-ts>, the backup ref is verified to " +
			"point at the tip, and only then is the branch deleted from origin. While " +
			"autonomous.remote_reap_enabled is false or unset (the default) the same evaluation " +
			"runs but nothing is deleted and the report says report_only. A pass already in " +
			"progress answers a conflict, not a silent skip.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/remote-reap/run", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(append(body, '\n'))
				return err
			}
			rep, err := autonomous.ParseRemoteReapReport(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderRemoteReapRun(rep))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousRemoteReapRestoreCmd recreates one remotely reaped branch
// from its newest backup ref.
func newAutonomousRemoteReapRestoreCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "remote-reap-restore <branch>",
		Short: "Recreate one remotely reaped branch from its newest backup ref",
		Long: "Re-point the branch at the newest tip a previous remote reap pass backed up under " +
			"refs/datawatch/remote-reap-backup/. The branch name is the Automaton work branch from " +
			"the remote-reap report. A branch that was never remotely reaped (no backup ref) " +
			"answers not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/remote-reap/restore", map[string]string{"branch": args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(append(body, '\n'))
				return err
			}
			res, err := autonomous.ParseRemoteReapRestoreResult(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderRemoteReapRestore(res))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
