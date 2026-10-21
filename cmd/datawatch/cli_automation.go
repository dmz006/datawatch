// `datawatch automation ...` CLI subcommands. Currently: activate.
// Sibling of the automation_activate MCP tool and the "automation
// activate" comm verb; all call POST /api/automations/{id}/activate.

package main

import (
	"net/url"

	"github.com/spf13/cobra"
)

func newAutomationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "automation",
		Short: "Automation (chained Automatons) management",
		Long: `Manage Automations.

Subcommands:
  activate <id>   Start a draft Automation`,
	}
	cmd.AddCommand(newAutomationActivateCmd())
	return cmd
}

func newAutomationActivateCmd() *cobra.Command {
	var actor, reason string
	cmd := &cobra.Command{
		Use:   "activate <id>",
		Short: "Activate a draft Automation (starts its entry member)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if actor != "" {
				body["actor"] = actor
			}
			if reason != "" {
				body["reason"] = reason
			}
			return guardrailAPIPost("/api/automations/"+url.PathEscape(args[0])+"/activate", body)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "", "Who is activating (default operator)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason, recorded in the audit trail")
	return cmd
}
