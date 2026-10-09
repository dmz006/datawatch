// BL242 Phase 1 — CLI subcommands for the centralized secrets manager.
//
//   datawatch secrets list
//   datawatch secrets get <name>
//   datawatch secrets set <name> <value> [--tags t1,t2] [--desc "..."] [--scope agent:x]
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
	cmd.AddCommand(newSecretsMigrateCmd())
	cmd.AddCommand(newSecretsImportCmd())
	cmd.AddCommand(newSecretsVaultCmd())
	cmd.AddCommand(newSecretsMintServiceTokenCmd())
	cmd.AddCommand(newSecretsListServiceTokensCmd())
	cmd.AddCommand(newSecretsRevokeServiceTokenCmd())
	return cmd
}

// GH#203 — external-service tokens: a persistent, operator-minted
// credential for an independent service (not a spawned F10 agent, not a
// federation peer — e.g. imap-mcp running as its own systemd service) to
// resolve a specific, scoped set of secrets via GET /api/external/secrets/{name}.
//
// IMPORTANT: run `mint-service-token` yourself, directly in a real
// terminal — not by asking an agent to run it, and not via Claude
// Code's `!` shell-passthrough prompt either, since that still lands
// the output in the conversation transcript. The printed token is the
// only place its plaintext value is ever shown; anywhere an LLM can see
// that output defeats the whole point of minting it this way.
func newSecretsMintServiceTokenCmd() *cobra.Command {
	var desc string
	c := &cobra.Command{
		Use:   "mint-service-token <name>",
		Short: "Mint a persistent token for an external service (run directly, never via an agent)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/secrets/service-tokens", map[string]any{
				"name":        args[0],
				"description": desc,
			})
		},
	}
	c.Flags().StringVar(&desc, "desc", "", "Human-readable description")
	return c
}

func newSecretsListServiceTokensCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-service-tokens",
		Short: "List provisioned external-service tokens (names only, never the token values)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/secrets/service-tokens") },
	}
}

func newSecretsRevokeServiceTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke-service-token <name>",
		Short: "Revoke an external-service token",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/secrets/service-tokens/"+args[0], nil)
		},
	}
}

// BL267 (v6.15.0) — `datawatch secrets vault status` for the Vault backend.
func newSecretsVaultCmd() *cobra.Command {
	v := &cobra.Command{
		Use:   "vault",
		Short: "HashiCorp Vault / OpenBao backend (BL267)",
	}
	v.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show Vault backend connectivity + last success / error",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/secrets/vault/status") },
	})
	return v
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
	var scopes []string
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
				"scopes":      scopes,
				"description": desc,
			})
		},
	}
	c.Flags().StringVar(&tags, "tags", "", "Comma-separated tags (e.g. git,cloud)")
	c.Flags().StringVar(&desc, "desc", "", "Human-readable description")
	c.Flags().StringArrayVar(&scopes, "scope", nil, "Access scope (repeatable: --scope agent:ci-runner --scope plugin:gh-hooks)")
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
