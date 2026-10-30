// datawatch autonomous merged-members — the merged-member decisions over the
// CLI. The body of GET /api/automations/{id}/merged-members is rendered by the
// shared formatter (internal/autonomous/merged_member_text.go), the same text
// the comm verb shows. A read: nothing is written.

package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousMergedMembersCmd is the `datawatch autonomous merged-members`
// subcommand: the decisions the coordinator made on members whose branch was
// already merged.
func newAutonomousMergedMembersCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "merged-members [automation-id]",
		Short: "List the merged-member decisions of an Automaton, newest first",
		Long: "Show the decisions the Automation coordinator made on members whose branch " +
			"was already merged on the default branch: one line each with the Automaton, " +
			"the member, the outcome (advanced or blocked), the branch and the default " +
			"branch it was proved merged into, the proving commit, the policy in force " +
			"and the time. Without an id, every Automaton's decisions are listed. A read: " +
			"nothing is written.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/api/automations/merged-members"
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				path = "/api/automations/" + url.PathEscape(strings.TrimSpace(args[0])) + "/merged-members"
			}
			body, err := progressGet(path, nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			rows, err := autonomous.ParseMergedMembers(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderMergedMembers(rows, 0))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
