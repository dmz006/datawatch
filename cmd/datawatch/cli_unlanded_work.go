// datawatch autonomous unlanded-work — the Housekeeping list of archived
// Automaton branches whose work is not on the default branch, and the actions
// on one listed branch. The list body of GET /api/autonomous/unlanded-work is
// rendered by the shared formatter (internal/autonomous/unlanded_work_text.go),
// the same text the comm verb shows.

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousUnlandedWorkCmd is the `datawatch autonomous unlanded-work`
// command. With no subcommand it lists; keep, archive-branch and
// delete-worktree act on one listed branch.
func newAutonomousUnlandedWorkCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "unlanded-work",
		Short: "List archived Automaton branches with work not on the default branch, and act on them",
		Long: "List the archived Automaton branches whose commits are not on the " +
			"default branch, with the recommendation for each. The subcommands " +
			"keep (leave a branch alone), archive-branch (move it under the " +
			"archive refs) and delete-worktree (back it up as a bundle, then " +
			"delete the worktree and branch; needs --confirm) act on one listed " +
			"branch.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/unlanded-work", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			l, err := autonomous.ParseUnlandedWorkList(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderUnlandedWorkList(l))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.AddCommand(unlandedActionCmd("keep", "Leave a listed branch alone", "keep", false),
		unlandedActionCmd("archive-branch", "Move a listed branch under the archive refs", "archive-branch", false),
		unlandedActionCmd("delete-worktree", "Back up a listed branch as a bundle, then delete its worktree and branch", "delete-worktree", true))
	return c
}

// unlandedActionCmd builds one action subcommand. confirmRequired makes
// --confirm mandatory (the delete is not undone without the bundle).
func unlandedActionCmd(use, short, route string, confirmRequired bool) *cobra.Command {
	var note string
	var confirm bool
	c := &cobra.Command{
		Use:   use + " <branch>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := strings.TrimSpace(args[0])
			if branch == "" {
				return fmt.Errorf("branch is required")
			}
			if confirmRequired && !confirm {
				return fmt.Errorf("--confirm is required to delete a worktree")
			}
			req := map[string]any{"branch": branch}
			if note != "" {
				req["note"] = note
			}
			if confirmRequired {
				req["confirm"] = true
			}
			body, err := daemonJSONBody("POST", "/api/autonomous/unlanded-work/"+route, req)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), autonomous.RenderUnlandedActionResult(route, body))
			return err
		},
	}
	if route == "keep" {
		c.Flags().StringVar(&note, "note", "", "why the branch is left alone")
	}
	if confirmRequired {
		c.Flags().BoolVar(&confirm, "confirm", false, "confirm the delete")
	}
	return c
}
