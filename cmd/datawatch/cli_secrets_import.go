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
		Short: "Import external credentials into secrets store",
	}
	cmd.AddCommand(newSecretsImportKubectlCmd())
	cmd.AddCommand(newSecretsImportClaudeCmd())
	cmd.AddCommand(newSecretsImportGithubCmd())
	cmd.AddCommand(newSecretsImportSSHCmd())
	cmd.AddCommand(newSecretsImportOpenAICmd())
	cmd.AddCommand(newSecretsImportGeminiCmd())
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

// newSecretsImportOpenAICmd imports an OpenAI-compatible API key from environment.
//
// Covers: memory embeddings (memory.openai_key), aider backend, any
// OpenAI-compat LLM configured with api_key_ref: ${secret:openai-api-key}.
//
// Usage:
//	export OPENAI_API_KEY=sk-...
//	datawatch secrets import openai --from-env OPENAI_API_KEY
//	datawatch secrets import openai --from-env GROQ_API_KEY --name groq-api-key
func newSecretsImportOpenAICmd() *cobra.Command {
	var fromEnv string
	var secretName string

	c := &cobra.Command{
		Use:   "openai",
		Short: "Import OpenAI-compatible API key from environment variable",
		Long: `Import an OpenAI-compatible API key into the secrets store.

Covers: memory embeddings (memory.openai_key), aider backend (OPENAI_API_KEY),
and any LLM registered with api_key_ref: ${secret:<name>}.

Also works for OpenAI-compatible providers (Groq, Together AI, Mistral, etc.)
by using --from-env with their respective env var and --name for a distinct key.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if fromEnv == "" {
				return fmt.Errorf("--from-env is required (e.g. OPENAI_API_KEY)")
			}

			apiKey := os.Getenv(fromEnv)
			if apiKey == "" {
				return fmt.Errorf("environment variable %s is not set", fromEnv)
			}

			if secretName == "" {
				secretName = "openai-api-key"
			}

			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        secretName,
				"value":       apiKey,
				"tags":        []string{"llm", "openai", "openai-compat"},
				"description": fmt.Sprintf("OpenAI-compatible API key from $%s", fromEnv),
			})
		},
	}

	c.Flags().StringVar(&fromEnv, "from-env", "", "Environment variable containing API key (required, e.g. OPENAI_API_KEY)")
	c.Flags().StringVar(&secretName, "name", "", "Secret name in store (default: openai-api-key)")
	return c
}

// newSecretsImportGeminiCmd imports a Gemini API key from environment.
//
// The Gemini CLI binary reads GEMINI_API_KEY (or GOOGLE_GENERATIVE_AI_API_KEY
// as an alias). Once stored, inject at session spawn via AgentSettings or by
// extending spawn env injection to include this secret.
//
// Usage:
//	export GEMINI_API_KEY=AIza...
//	datawatch secrets import gemini --from-env GEMINI_API_KEY
func newSecretsImportGeminiCmd() *cobra.Command {
	var fromEnv string
	var secretName string

	c := &cobra.Command{
		Use:   "gemini",
		Short: "Import Gemini API key from environment variable",
		Long: `Import a Gemini API key into the secrets store.

The Gemini CLI backend reads GEMINI_API_KEY (alias: GOOGLE_GENERATIVE_AI_API_KEY).
Once stored, reference it via ${secret:gemini-api-key} in config or inject it
at session spawn time through AgentSettings env extension.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if fromEnv == "" {
				return fmt.Errorf("--from-env is required (e.g. GEMINI_API_KEY or GOOGLE_GENERATIVE_AI_API_KEY)")
			}

			apiKey := os.Getenv(fromEnv)
			if apiKey == "" {
				return fmt.Errorf("environment variable %s is not set", fromEnv)
			}

			if secretName == "" {
				secretName = "gemini-api-key"
			}

			return daemonJSON(http.MethodPost, "/api/secrets", map[string]any{
				"name":        secretName,
				"value":       apiKey,
				"tags":        []string{"llm", "gemini", "google"},
				"description": fmt.Sprintf("Gemini API key from $%s (GEMINI_API_KEY / GOOGLE_GENERATIVE_AI_API_KEY)", fromEnv),
			})
		},
	}

	c.Flags().StringVar(&fromEnv, "from-env", "", "Environment variable containing API key (required, e.g. GEMINI_API_KEY)")
	c.Flags().StringVar(&secretName, "name", "", "Secret name in store (default: gemini-api-key)")
	return c
}
