// BL242 Phase 1 — CLI subcommands for the centralized secrets manager.
//
//   datawatch secrets list
//   datawatch secrets get <name>
//   datawatch secrets set <name> <value> [--tags t1,t2] [--desc "..."]
//   datawatch secrets delete <name>

package main

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage centralized secrets",
	}
	cmd.AddCommand(newSecretsListCmd())
	cmd.AddCommand(newSecretsGetCmd())
	cmd.AddCommand(newSecretsSetCmd())
	cmd.AddCommand(newSecretsDeleteCmd())
	return cmd
}

func newSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all secrets (no values)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/secrets") },
	}
}

func newSecretsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a secret value (access is audit-logged)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/secrets/" + args[0])
		},
	}
}

func newSecretsSetCmd() *cobra.Command {
	var tags string
	var desc string
	c := &cobra.Command{
		Use:   "set <name> <value>",
		Short: "Create or update a secret",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			var tagList []string
			for _, t := range strings.Split(tags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tagList = append(tagList, t)
				}
			}
			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        args[0],
				"value":       args[1],
				"tags":        tagList,
				"description": desc,
			})
		},
	}
	c.Flags().StringVar(&tags, "tags", "", "Comma-separated tags (e.g. git,cloud)")
	c.Flags().StringVar(&desc, "desc", "", "Human-readable description")
	return c
}

func newSecretsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/secrets/"+args[0], nil)
		},
	}
}
