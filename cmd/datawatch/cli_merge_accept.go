// datawatch autonomous prd-landing-accept and merge-selection: the merge
// selection rule over the CLI.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousLandingAcceptCmd records the acceptance of one exact commit.
func newAutonomousLandingAcceptCmd() *cobra.Command {
	var commit, reason, actor string
	cmd := &cobra.Command{
		Use:   "prd-landing-accept <id>",
		Short: "Accept one exact commit of an Automaton's branch for merging",
		Long: "Record that the operator or the supervisor accepts one exact commit of the Automaton's branch for " +
			"merging, and why. It is for work whose review is pending or timed out, or that is needs_review. Failed, " +
			"blocked, cancelled, archived and review-blocked work cannot be accepted, and an acceptance never covers a " +
			"later commit. Nothing is merged or pushed.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, _ := json.Marshal(map[string]string{"actor": actor, "commit": commit, "reason": reason})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/landing/accept", body)
		},
	}
	cmd.Flags().StringVar(&commit, "commit", "", "the full commit id being accepted (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "why it is accepted (required)")
	cmd.Flags().StringVar(&actor, "actor", "operator", "who is accepting")
	return cmd
}

// newAutonomousMergeSelectionCmd shows what datawatch may merge and why not.
func newAutonomousMergeSelectionCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "merge-selection [id]",
		Short: "Show which Automaton branches datawatch may merge, and why the others are left out",
		Long: "Show the merge selection rule's answer for the branches waiting to merge, or for one Automaton. " +
			"A read: nothing is merged or pushed.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{"selection": {"true"}}
			if len(args) == 1 {
				q.Set("automaton", args[0])
			}
			body, err := progressGet("/api/autonomous/merge", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			sel, err := autonomous.ParseMergeSelection(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderMergeSelection(sel))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
