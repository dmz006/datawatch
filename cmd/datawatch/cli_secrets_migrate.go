// BL242 — secrets migrate: moves plaintext sensitive config fields into the
// centralized secrets store and rewrites the config file with ${secret:name}
// references.
//
//	datawatch secrets migrate [--dry-run] [--yes]

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/config"
)

// sensitiveField describes one config field that should be migrated to the
// secrets store.
type sensitiveField struct {
	secretName string
	desc       string
	tags       []string
	get        func(*config.Config) string
	set        func(*config.Config, string)
}

// sensitiveFields is the canonical list of all config fields that may contain
// plaintext credentials. Extend this list as new backends are added.
var sensitiveFields = []sensitiveField{
	// OpenWebUI
	{
		secretName: "openwebui-api-key",
		desc:       "OpenWebUI API key",
		tags:       []string{"openwebui", "llm"},
		get:        func(c *config.Config) string { return c.OpenWebUI.APIKey },
		set:        func(c *config.Config, v string) { c.OpenWebUI.APIKey = v },
	},
	// Memory OpenAI embedder key
	{
		secretName: "memory-openai-key",
		desc:       "OpenAI API key for memory embeddings",
		tags:       []string{"memory", "openai"},
		get:        func(c *config.Config) string { return c.Memory.OpenAIKey },
		set:        func(c *config.Config, v string) { c.Memory.OpenAIKey = v },
	},
	// DNS channel
	{
		secretName: "dns-channel-secret",
		desc:       "DNS channel HMAC-SHA256 shared secret",
		tags:       []string{"dns", "channel"},
		get:        func(c *config.Config) string { return c.DNSChannel.Secret },
		set:        func(c *config.Config, v string) { c.DNSChannel.Secret = v },
	},
	// Discord
	{
		secretName: "discord-token",
		desc:       "Discord bot token",
		tags:       []string{"discord", "messaging"},
		get:        func(c *config.Config) string { return c.Discord.Token },
		set:        func(c *config.Config, v string) { c.Discord.Token = v },
	},
	// Slack
	{
		secretName: "slack-token",
		desc:       "Slack bot token",
		tags:       []string{"slack", "messaging"},
		get:        func(c *config.Config) string { return c.Slack.Token },
		set:        func(c *config.Config, v string) { c.Slack.Token = v },
	},
	// Telegram
	{
		secretName: "telegram-token",
		desc:       "Telegram bot token",
		tags:       []string{"telegram", "messaging"},
		get:        func(c *config.Config) string { return c.Telegram.Token },
		set:        func(c *config.Config, v string) { c.Telegram.Token = v },
	},
	// Matrix
	{
		secretName: "matrix-access-token",
		desc:       "Matrix homeserver access token",
		tags:       []string{"matrix", "messaging"},
		get:        func(c *config.Config) string { return c.Matrix.AccessToken },
		set:        func(c *config.Config, v string) { c.Matrix.AccessToken = v },
	},
	// Twilio
	{
		secretName: "twilio-auth-token",
		desc:       "Twilio auth token",
		tags:       []string{"twilio", "messaging"},
		get:        func(c *config.Config) string { return c.Twilio.AuthToken },
		set:        func(c *config.Config, v string) { c.Twilio.AuthToken = v },
	},
	{
		secretName: "twilio-account-sid",
		desc:       "Twilio account SID",
		tags:       []string{"twilio", "messaging"},
		get:        func(c *config.Config) string { return c.Twilio.AccountSID },
		set:        func(c *config.Config, v string) { c.Twilio.AccountSID = v },
	},
	// Ntfy
	{
		secretName: "ntfy-token",
		desc:       "Ntfy push notification token",
		tags:       []string{"ntfy", "messaging"},
		get:        func(c *config.Config) string { return c.Ntfy.Token },
		set:        func(c *config.Config, v string) { c.Ntfy.Token = v },
	},
	// Email
	{
		secretName: "email-password",
		desc:       "SMTP email account password",
		tags:       []string{"email", "messaging"},
		get:        func(c *config.Config) string { return c.Email.Password },
		set:        func(c *config.Config, v string) { c.Email.Password = v },
	},
	// GitHub webhook
	{
		secretName: "github-webhook-secret",
		desc:       "GitHub webhook HMAC secret",
		tags:       []string{"github", "webhook"},
		get:        func(c *config.Config) string { return c.GitHubWebhook.Secret },
		set:        func(c *config.Config, v string) { c.GitHubWebhook.Secret = v },
	},
	// Generic webhook
	{
		secretName: "webhook-token",
		desc:       "Generic webhook bearer token",
		tags:       []string{"webhook"},
		get:        func(c *config.Config) string { return c.Webhook.Token },
		set:        func(c *config.Config, v string) { c.Webhook.Token = v },
	},
	// MCP SSE token
	{
		secretName: "mcp-sse-token",
		desc:       "MCP SSE server bearer token",
		tags:       []string{"mcp"},
		get:        func(c *config.Config) string { return c.MCP.Token },
		set:        func(c *config.Config, v string) { c.MCP.Token = v },
	},
	// KeePass master password (in secrets config itself)
	{
		secretName: "keepass-master-password",
		desc:       "KeePass database master password",
		tags:       []string{"keepass", "secrets"},
		get:        func(c *config.Config) string { return c.Secrets.KeePassPassword },
		set:        func(c *config.Config, v string) { c.Secrets.KeePassPassword = v },
	},
	// 1Password service account token
	{
		secretName: "op-service-token",
		desc:       "1Password service account token",
		tags:       []string{"onepassword", "secrets"},
		get:        func(c *config.Config) string { return c.Secrets.OPToken },
		set:        func(c *config.Config, v string) { c.Secrets.OPToken = v },
	},
}

func newSecretsMigrateCmd() *cobra.Command {
	var dryRun bool
	var yes bool

	c := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate plaintext config credentials into the secrets store",
		Long: `Scans the active config file for known plaintext sensitive fields
(tokens, passwords, API keys, webhook secrets), stores each value in the
centralized secrets store, and rewrites the config with ${secret:name}
references in their place.

The daemon must be running (secrets are stored via the REST API).
Restart the daemon after migration to activate the references.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSecretsMigrate(dryRun, yes)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be migrated without making any changes")
	c.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return c
}

// migrationEntry is one field selected for migration.
type migrationEntry struct {
	field sensitiveField
	value string
	ref   string // "${secret:name}"
}

func runSecretsMigrate(dryRun, yes bool) error {
	cfgPath := resolveConfigPath()
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build the migration plan: only fields with non-empty plaintext values.
	var plan []migrationEntry
	for _, f := range sensitiveFields {
		val := f.get(cfg)
		if val == "" || strings.HasPrefix(val, "${secret:") {
			continue
		}
		plan = append(plan, migrationEntry{
			field: f,
			value: val,
			ref:   "${secret:" + f.secretName + "}",
		})
	}

	// Also scan remote server tokens.
	for i, srv := range cfg.Servers {
		if srv.Token == "" || strings.HasPrefix(srv.Token, "${secret:") {
			continue
		}
		name := "remote-token-" + srv.Name
		idx := i // capture
		plan = append(plan, migrationEntry{
			field: sensitiveField{
				secretName: name,
				desc:       fmt.Sprintf("Remote server token (%s)", srv.Name),
				tags:       []string{"remote", "server"},
				get:        func(c *config.Config) string { return c.Servers[idx].Token },
				set:        func(c *config.Config, v string) { c.Servers[idx].Token = v },
			},
			value: srv.Token,
			ref:   "${secret:" + name + "}",
		})
	}

	if len(plan) == 0 {
		fmt.Println("No plaintext sensitive fields found in config. Nothing to migrate.")
		return nil
	}

	// Print the plan.
	fmt.Printf("Config: %s\n\n", cfgPath)
	fmt.Printf("Fields to migrate (%d):\n\n", len(plan))
	for _, e := range plan {
		masked := maskValue(e.value)
		fmt.Printf("  %-30s  %s  →  %s\n", e.field.secretName, masked, e.ref)
	}

	if dryRun {
		fmt.Println("\n[dry-run] No changes made.")
		return nil
	}

	if !yes {
		fmt.Printf("\nThis will:\n")
		fmt.Printf("  1. Store %d secret(s) in the daemon's secrets store\n", len(plan))
		fmt.Printf("  2. Rewrite %s with ${secret:name} references\n", cfgPath)
		fmt.Printf("  3. Require a daemon restart to activate\n\n")
		fmt.Print("Proceed? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Store secrets via REST API, tracking which ones succeeded.
	client := &http.Client{Timeout: 30 * time.Second}
	base := daemonURL()
	var stored []migrationEntry
	var failed []string

	for _, e := range plan {
		payload, _ := json.Marshal(map[string]any{
			"name":        e.field.secretName,
			"value":       e.value,
			"tags":        e.field.tags,
			"description": e.field.desc,
		})
		req, _ := http.NewRequest(http.MethodPost, base+"/api/secrets", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", e.field.secretName, err))
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			failed = append(failed, fmt.Sprintf("%s: HTTP %d %s", e.field.secretName, resp.StatusCode, string(body)))
			continue
		}
		stored = append(stored, e)
		fmt.Printf("  stored  %s\n", e.field.secretName)
	}

	if len(stored) == 0 {
		fmt.Println("\nNo secrets stored. Config not modified.")
		if len(failed) > 0 {
			fmt.Println("Errors:")
			for _, f := range failed {
				fmt.Println("  " + f)
			}
		}
		return nil
	}

	// Update the config struct and rewrite the file.
	for _, e := range stored {
		e.field.set(cfg, e.ref)
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		return fmt.Errorf("save config: %w (secrets already stored — re-run with --yes to retry config write)", err)
	}

	fmt.Printf("\n%d secret(s) stored, config rewritten: %s\n", len(stored), cfgPath)
	if len(failed) > 0 {
		fmt.Printf("%d failed (not written to config):\n", len(failed))
		for _, f := range failed {
			fmt.Println("  " + f)
		}
	}
	fmt.Println("\nRestart the daemon to activate the new references:")
	fmt.Println("  datawatch restart")
	return nil
}

// maskValue hides all but the first/last two characters of a credential.
// Strings of 5 or fewer characters are fully masked.
func maskValue(s string) string {
	if len(s) <= 5 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}
