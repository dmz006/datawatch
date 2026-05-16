// BL242 Phase 2 — specialized auth import commands for test environments.
//
// Imports external authentication credentials into the centralized secrets store
// for use by test daemons and agents.
//
//	datawatch secrets import kubectl --context=<name> [--name=<secret-name>]
//	datawatch secrets import claude --from-env ANTHROPIC_API_KEY [--name=claude-test-api-key]
//	datawatch secrets import github --from-env GITHUB_TOKEN [--name=github-test-pat]
//	datawatch secrets import ssh --key-path ~/.ssh/id_rsa.pub [--name=ssh-test-pubkey]

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newSecretsImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import external credentials into secrets store (Phase 2)",
	}
	cmd.AddCommand(newSecretsImportKubectlCmd())
	cmd.AddCommand(newSecretsImportClaudeCmd())
	cmd.AddCommand(newSecretsImportGithubCmd())
	cmd.AddCommand(newSecretsImportSSHCmd())
	return cmd
}

// newSecretsImportKubectlCmd imports kubectl context into secrets.
//
// Usage:
//	datawatch secrets import kubectl --context=testing
//	datawatch secrets import kubectl --context=prod --name=k8s-prod-context
func newSecretsImportKubectlCmd() *cobra.Command {
	var context string
	var secretName string

	c := &cobra.Command{
		Use:   "kubectl",
		Short: "Import kubectl context config (flatten to JSON)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if context == "" {
				return fmt.Errorf("--context is required")
			}

			// Run kubectl config view --context=<context> --flatten
			cmd := exec.Command("kubectl", "config", "view",
				fmt.Sprintf("--context=%s", context), "--flatten")
			out, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("kubectl config view failed: %w", err)
			}

			// Determine secret name: use provided --name or derive from context
			if secretName == "" {
				secretName = fmt.Sprintf("k8s-context-%s", context)
			}

			// Store in secrets via daemon API
			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        secretName,
				"value":       string(out),
				"tags":        []string{"test", "k8s", "kubectl", context},
				"description": fmt.Sprintf("Kubernetes context: %s (imported via kubectl config view --flatten)", context),
			})
		},
	}

	c.Flags().StringVar(&context, "context", "", "kubectl context name to import (required)")
	c.Flags().StringVar(&secretName, "name", "", "Secret name in store (default: k8s-context-<context>)")
	return c
}

// newSecretsImportClaudeCmd imports Claude API key from environment.
//
// Usage:
//	export ANTHROPIC_API_KEY=sk-ant-...
//	datawatch secrets import claude --from-env ANTHROPIC_API_KEY
//	datawatch secrets import claude --from-env ANTHROPIC_API_KEY --name claude-prod-key
func newSecretsImportClaudeCmd() *cobra.Command {
	var fromEnv string
	var secretName string

	c := &cobra.Command{
		Use:   "claude",
		Short: "Import Claude API key from environment variable",
		RunE: func(_ *cobra.Command, _ []string) error {
			if fromEnv == "" {
				return fmt.Errorf("--from-env is required (e.g. ANTHROPIC_API_KEY)")
			}

			// Get value from environment
			apiKey := os.Getenv(fromEnv)
			if apiKey == "" {
				return fmt.Errorf("environment variable %s is not set", fromEnv)
			}

			// Determine secret name
			if secretName == "" {
				secretName = "claude-test-api-key"
			}

			// Store in secrets via daemon API
			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        secretName,
				"value":       apiKey,
				"tags":        []string{"test", "claude", "llm", "anthropic"},
				"description": fmt.Sprintf("Claude API key from $%s (imported for test environment)", fromEnv),
			})
		},
	}

	c.Flags().StringVar(&fromEnv, "from-env", "", "Environment variable containing API key (required)")
	c.Flags().StringVar(&secretName, "name", "", "Secret name in store (default: claude-test-api-key)")
	return c
}

// newSecretsImportGithubCmd imports GitHub PAT from environment.
//
// Usage:
//	export GITHUB_TOKEN=ghp_...
//	datawatch secrets import github --from-env GITHUB_TOKEN
//	datawatch secrets import github --from-env GITHUB_TOKEN --name gh-org-token
func newSecretsImportGithubCmd() *cobra.Command {
	var fromEnv string
	var secretName string

	c := &cobra.Command{
		Use:   "github",
		Short: "Import GitHub PAT from environment variable",
		RunE: func(_ *cobra.Command, _ []string) error {
			if fromEnv == "" {
				return fmt.Errorf("--from-env is required (e.g. GITHUB_TOKEN)")
			}

			// Get value from environment
			token := os.Getenv(fromEnv)
			if token == "" {
				return fmt.Errorf("environment variable %s is not set", fromEnv)
			}

			// Determine secret name
			if secretName == "" {
				secretName = "test-github-pat"
			}

			// Store in secrets via daemon API
			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        secretName,
				"value":       token,
				"tags":        []string{"test", "github", "git", "vcs"},
				"description": fmt.Sprintf("GitHub Personal Access Token from $%s (imported for test environment)", fromEnv),
			})
		},
	}

	c.Flags().StringVar(&fromEnv, "from-env", "", "Environment variable containing PAT (required)")
	c.Flags().StringVar(&secretName, "name", "", "Secret name in store (default: test-github-pat)")
	return c
}

// newSecretsImportSSHCmd imports SSH public key from file.
//
// Usage:
//	datawatch secrets import ssh --key-path ~/.ssh/id_rsa.pub
//	datawatch secrets import ssh --key-path ~/.ssh/id_ed25519.pub --name ssh-prod-pubkey
func newSecretsImportSSHCmd() *cobra.Command {
	var keyPath string
	var secretName string

	c := &cobra.Command{
		Use:   "ssh",
		Short: "Import SSH public key from file",
		RunE: func(_ *cobra.Command, _ []string) error {
			if keyPath == "" {
				return fmt.Errorf("--key-path is required (e.g. ~/.ssh/id_rsa.pub)")
			}

			// Expand ~ in path
			if strings.HasPrefix(keyPath, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("could not determine home directory: %w", err)
				}
				keyPath = home + keyPath[1:]
			}

			// Read SSH public key from file
			pubKeyBytes, err := os.ReadFile(keyPath)
			if err != nil {
				return fmt.Errorf("could not read SSH public key at %s: %w", keyPath, err)
			}

			pubKey := strings.TrimSpace(string(pubKeyBytes))

			// Determine secret name
			if secretName == "" {
				secretName = "ssh-test-pubkey"
			}

			// Store in secrets via daemon API
			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        secretName,
				"value":       pubKey,
				"tags":        []string{"test", "ssh", "auth"},
				"description": fmt.Sprintf("SSH public key imported from %s (test environment)", keyPath),
			})
		},
	}

	c.Flags().StringVar(&keyPath, "key-path", "", "Path to SSH public key file (required, e.g. ~/.ssh/id_rsa.pub)")
	c.Flags().StringVar(&secretName, "name", "", "Secret name in store (default: ssh-test-pubkey)")
	return c
}
