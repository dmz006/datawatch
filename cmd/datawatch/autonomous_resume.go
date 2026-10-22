package main

import (
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"
)

// newAutonomousPRDResumeCmd — resume a paused PRD, or one the boot-resume
// gate parked. See POST /api/autonomous/prds/{id}/resume.
func newAutonomousPRDResumeCmd() *cobra.Command {
	var actor, note string
	cmd := &cobra.Command{
		Use:     "prd-resume <id>",
		Aliases: []string{"resume"},
		Short:   "Resume a paused or boot-parked PRD",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if actor == "" {
				actor = "operator"
			}
			body, _ := json.Marshal(map[string]string{"actor": actor, "note": note})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/resume", body)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "operator", "operator identity recorded on the audit entry")
	cmd.Flags().StringVar(&note, "note", "", "free-form note attached to the resume")
	return cmd
}
