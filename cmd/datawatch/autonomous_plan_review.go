package main

import (
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"
)

// newAutonomousPRDPlanReviewCmd — re-run the pre-approval plan reviewer for
// a stored PRD. A passing re-run replaces the stored verdict, clearing a
// blocking one. See POST /api/autonomous/prds/{id}/plan-review.
func newAutonomousPRDPlanReviewCmd() *cobra.Command {
	var actor, note string
	cmd := &cobra.Command{
		Use:     "prd-plan-review <id>",
		Aliases: []string{"plan-review"},
		Short:   "Re-run the plan reviewer for a PRD; a passing verdict clears a blocking one",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if actor == "" {
				actor = "operator"
			}
			body, _ := json.Marshal(map[string]string{"actor": actor, "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/plan-review", body)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "operator", "operator identity recorded on the audit entry")
	cmd.Flags().StringVar(&note, "note", "", "free-form note attached to the re-run")
	return cmd
}
