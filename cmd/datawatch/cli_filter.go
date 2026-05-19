// filter CLI command — wraps /api/filters CRUD

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newFilterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filter",
		Short: "Manage session output filters",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all filters",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/filters") },
	})
	addCmd := &cobra.Command{
		Use:   "add <pattern>",
		Short: "Add a filter (action: redact|block|tag)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action, _ := cmd.Flags().GetString("action")
			value, _ := cmd.Flags().GetString("value")
			if action == "" {
				action = "redact"
			}
			return daemonJSON(http.MethodPost, "/api/filters", map[string]any{
				"pattern": args[0],
				"action":  action,
				"value":   value,
			})
		},
	}
	addCmd.Flags().String("action", "redact", "Filter action: redact | block | tag")
	addCmd.Flags().String("value", "", "Replacement value (for redact/tag)")
	cmd.AddCommand(addCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a filter by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/filters?id="+args[0], nil)
		},
	})
	toggleCmd := &cobra.Command{
		Use:   "toggle <id>",
		Short: "Enable or disable a filter",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			enable, _ := cmd.Flags().GetBool("enable")
			disable, _ := cmd.Flags().GetBool("disable")
			if !enable && !disable {
				return fmt.Errorf("pass --enable or --disable")
			}
			e := enable && !disable
			return daemonJSON(http.MethodPatch, "/api/filters", map[string]any{
				"id":      args[0],
				"enabled": e,
			})
		},
	}
	toggleCmd.Flags().Bool("enable", false, "Enable the filter")
	toggleCmd.Flags().Bool("disable", false, "Disable the filter")
	cmd.AddCommand(toggleCmd)
	return cmd
}
