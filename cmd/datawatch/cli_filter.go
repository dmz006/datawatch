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
		Short: "Add an output filter (action: alert|detect_prompt|send_input|schedule)",
		Long: `Add an output filter.

send_input and schedule type into sessions. They need --types-input, start
disabled, and type only while session.input_filters_enabled is on. Every line
they type is marked as typed by the filter, and they never answer a permission
or confirmation prompt: such patterns are refused.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action, _ := cmd.Flags().GetString("action")
			value, _ := cmd.Flags().GetString("value")
			if action == "" {
				action = "alert"
			}
			typesInput, _ := cmd.Flags().GetBool("types-input")
			body := map[string]any{
				"pattern": args[0],
				"action":  action,
				"value":   value,
			}
			if typesInput {
				body["allow_input"] = true
			}
			return daemonJSON(http.MethodPost, "/api/filters", body)
		},
	}
	addCmd.Flags().String("action", "alert", "Filter action: alert | detect_prompt | send_input | schedule")
	addCmd.Flags().String("value", "", "Alert body, or the line a send_input or schedule filter types")
	addCmd.Flags().Bool("types-input", false, "Confirm on purpose that this filter types into sessions (send_input, schedule)")
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
