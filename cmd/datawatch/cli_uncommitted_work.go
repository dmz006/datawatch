// datawatch autonomous uncommitted-work — the uncommitted-work status read
// over the CLI. The body of GET /api/autonomous/uncommitted-work is rendered
// by the shared formatter (internal/autonomous/uncommitted_work_text.go), the
// same text the comm verb shows. A read: nothing is committed or discarded.

package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousUncommittedWorkCmd is the `datawatch autonomous
// uncommitted-work` subcommand: the policy and scope in effect and the tasks
// and Automata held because work was left uncommitted.
func newAutonomousUncommittedWorkCmd() *cobra.Command {
	var automaton string
	var asJSON bool
	c := &cobra.Command{
		Use:   "uncommitted-work",
		Short: "Show the Automaton uncommitted-work status: the policy, the scope and what is held",
		Long: "Show the uncommitted-work policy and scope in effect and every " +
			"task or Automaton held because work was left uncommitted in its " +
			"worktree: the files, the files outside the task's declared " +
			"files, the reason and the question waiting for an answer " +
			"(commit, discard or retry). A read: nothing is committed or " +
			"discarded.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if automaton != "" {
				q.Set("automaton", automaton)
			}
			body, err := progressGet("/api/autonomous/uncommitted-work", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			st, err := autonomous.ParseUncommittedWorkStatus(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderUncommittedWorkStatus(st))
			return err
		},
	}
	c.Flags().StringVar(&automaton, "automaton", "", "narrow the holds to one Automaton ID")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
