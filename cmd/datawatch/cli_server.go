// BL312 S1 — CLI for the multi-server registry.

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage remote datawatch server registry (BL312 S1)",
		Long: `Register, view, and remove remote datawatch instances.
Runtime entries are persisted to <data-dir>/servers.json.
YAML-seeded entries (cfg.servers) are read-only seeds visible in the list.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all registered servers (YAML seeds + runtime entries)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/servers") },
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one server by name",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/servers/" + args[0]) },
	})

	addCmd := &cobra.Command{
		Use:   "add --name <name> --url <url> [--token <token>] [--label <label>]",
		Short: "Register a new remote server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			url, _ := cmd.Flags().GetString("url")
			token, _ := cmd.Flags().GetString("token")
			label, _ := cmd.Flags().GetString("label")
			enabled, _ := cmd.Flags().GetBool("enabled")

			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if url == "" {
				return fmt.Errorf("--url is required")
			}

			body := map[string]any{
				"name":    name,
				"url":     url,
				"enabled": enabled,
			}
			if token != "" {
				body["token"] = token
			}
			if label != "" {
				body["label"] = label
			}
			return daemonJSON(http.MethodPost, "/api/servers", body)
		},
	}
	addCmd.Flags().String("name", "", "short identifier (required)")
	addCmd.Flags().String("url", "", "base URL of the remote instance (required)")
	addCmd.Flags().String("token", "", "bearer token for authentication (optional)")
	addCmd.Flags().String("label", "", "human-readable label (optional)")
	addCmd.Flags().Bool("enabled", true, "whether the server is active")
	cmd.AddCommand(addCmd)

	updateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Replace an existing runtime server entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, _ := cmd.Flags().GetString("url")
			token, _ := cmd.Flags().GetString("token")
			label, _ := cmd.Flags().GetString("label")

			body := map[string]any{
				"name": args[0],
			}
			if url != "" {
				body["url"] = url
			}
			if token != "" {
				body["token"] = token
			}
			if label != "" {
				body["label"] = label
			}
			if cmd.Flags().Changed("enabled") {
				enabled, _ := cmd.Flags().GetBool("enabled")
				body["enabled"] = enabled
			}
			return daemonJSON(http.MethodPut, "/api/servers/"+args[0], body)
		},
	}
	updateCmd.Flags().String("url", "", "new base URL")
	updateCmd.Flags().String("token", "", "new bearer token")
	updateCmd.Flags().String("label", "", "new human-readable label")
	updateCmd.Flags().Bool("enabled", true, "enable or disable the server")
	cmd.AddCommand(updateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a runtime server entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/servers/"+args[0], nil)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "test <name>",
		Short: "Ping the named server's /api/health endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/servers/"+args[0]+"/test", nil)
		},
	})

	return cmd
}
