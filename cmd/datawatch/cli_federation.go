// BL316 S2 — CLI for the federation peer and group registry.

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newFederationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "federation",
		Short: "Manage federation peers and capability groups (BL316 S2)",
	}

	peerCmd := &cobra.Command{
		Use:   "peer",
		Short: "Manage federation peers",
	}

	peerCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all federation peers",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/peers") },
	})

	peerAddCmd := &cobra.Command{
		Use:   "add --name <name> --url <url> [--token <token>] [--capabilities <caps>]",
		Short: "Register a new federation peer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			url, _ := cmd.Flags().GetString("url")
			token, _ := cmd.Flags().GetString("token")
			capsStr, _ := cmd.Flags().GetString("capabilities")

			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if url == "" {
				return fmt.Errorf("--url is required")
			}

			body := map[string]any{
				"name": name,
				"url":  url,
			}
			if token != "" {
				body["token"] = token
			}
			if capsStr != "" {
				var caps []string
				for _, c := range strings.Split(capsStr, ",") {
					if t := strings.TrimSpace(c); t != "" {
						caps = append(caps, t)
					}
				}
				body["capabilities"] = caps
			}
			return daemonJSON(http.MethodPost, "/api/federation/peers", body)
		},
	}
	peerAddCmd.Flags().String("name", "", "peer identifier (required)")
	peerAddCmd.Flags().String("url", "", "base URL of the remote instance (required)")
	peerAddCmd.Flags().String("token", "", "bearer token for authentication (optional)")
	peerAddCmd.Flags().String("capabilities", "", "comma-separated capabilities or group names (optional)")
	peerCmd.AddCommand(peerAddCmd)

	peerCmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one federation peer by name",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/federation/peers/" + args[0]) },
	})

	peerUpdateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an existing federation peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, _ := cmd.Flags().GetString("url")
			token, _ := cmd.Flags().GetString("token")
			capsStr, _ := cmd.Flags().GetString("capabilities")

			body := map[string]any{
				"name": args[0],
			}
			if url != "" {
				body["url"] = url
			}
			if token != "" {
				body["token"] = token
			}
			if capsStr != "" {
				var caps []string
				for _, c := range strings.Split(capsStr, ",") {
					if t := strings.TrimSpace(c); t != "" {
						caps = append(caps, t)
					}
				}
				body["capabilities"] = caps
			}
			return daemonJSON(http.MethodPut, "/api/federation/peers/"+args[0], body)
		},
	}
	peerUpdateCmd.Flags().String("url", "", "new base URL")
	peerUpdateCmd.Flags().String("token", "", "new bearer token")
	peerUpdateCmd.Flags().String("capabilities", "", "comma-separated capabilities or group names")
	peerCmd.AddCommand(peerUpdateCmd)

	peerCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a federation peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/federation/peers/"+args[0], nil)
		},
	})

	peerCmd.AddCommand(&cobra.Command{
		Use:   "test <name>",
		Short: "Test connectivity to a federation peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/federation/peers/"+args[0]+"/test", nil)
		},
	})

	cmd.AddCommand(peerCmd)

	groupCmd := &cobra.Command{
		Use:   "group",
		Short: "Manage federation capability groups",
	}

	groupCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all capability groups",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/groups") },
	})

	groupCmd.AddCommand(&cobra.Command{
		Use:   "builtins",
		Short: "List built-in capability groups",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/groups/builtins") },
	})

	groupAddCmd := &cobra.Command{
		Use:   "add --name <name> --caps <caps> [--description <desc>]",
		Short: "Create a new capability group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			capsStr, _ := cmd.Flags().GetString("caps")
			description, _ := cmd.Flags().GetString("description")

			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if capsStr == "" {
				return fmt.Errorf("--caps is required")
			}

			var caps []string
			for _, c := range strings.Split(capsStr, ",") {
				if t := strings.TrimSpace(c); t != "" {
					caps = append(caps, t)
				}
			}

			body := map[string]any{
				"name": name,
				"caps": caps,
			}
			if description != "" {
				body["description"] = description
			}
			return daemonJSON(http.MethodPost, "/api/federation/groups", body)
		},
	}
	groupAddCmd.Flags().String("name", "", "group identifier (required)")
	groupAddCmd.Flags().String("caps", "", "comma-separated capabilities (required)")
	groupAddCmd.Flags().String("description", "", "human-readable description (optional)")
	groupCmd.AddCommand(groupAddCmd)

	groupCmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one capability group by name",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/federation/groups/" + args[0]) },
	})

	groupUpdateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an existing capability group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capsStr, _ := cmd.Flags().GetString("caps")
			description, _ := cmd.Flags().GetString("description")

			if capsStr == "" {
				return fmt.Errorf("--caps is required")
			}

			var caps []string
			for _, c := range strings.Split(capsStr, ",") {
				if t := strings.TrimSpace(c); t != "" {
					caps = append(caps, t)
				}
			}

			body := map[string]any{
				"name": args[0],
				"caps": caps,
			}
			if description != "" {
				body["description"] = description
			}
			return daemonJSON(http.MethodPut, "/api/federation/groups/"+args[0], body)
		},
	}
	groupUpdateCmd.Flags().String("caps", "", "comma-separated capabilities (required)")
	groupUpdateCmd.Flags().String("description", "", "human-readable description (optional)")
	groupCmd.AddCommand(groupUpdateCmd)

	groupCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a capability group",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/federation/groups/"+args[0], nil)
		},
	})

	cmd.AddCommand(groupCmd)

	return cmd
}
