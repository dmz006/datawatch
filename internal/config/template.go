package config

import (
	"fmt"
	"strings"
)

// GenerateAnnotatedConfig returns a fully commented YAML config string with all
// fields present, defaults filled, and section headers for human readability.
// This is used by `datawatch config init` and `datawatch config generate` to produce
// a self-documenting config file.
func GenerateAnnotatedConfig(cfg *Config) string {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	var b strings.Builder

	section(&b, "Identity", "Machine identification and data storage paths.")
	field(&b, "hostname", cfg.Hostname, "Identifies this machine in messages and session IDs. Auto-detected if empty.")
	field(&b, "data_dir", cfg.DataDir, "Root directory for sessions.json, logs/, and state files.")
	b.WriteString("\n")

	section(&b, "Session Management", "Controls session creation, LLM defaults, and project git integration.")
	b.WriteString("session:\n")
	fieldi(&b, "llm_backend", cfg.Session.LLMBackend, "Active LLM backend. Options: claude-code, aider, goose, gemini, opencode, opencode-acp, opencode-prompt, ollama, openwebui, shell")
	fieldi(&b, "max_sessions", cfg.Session.MaxSessions, "Maximum concurrent active sessions (0 = unlimited)")
	fieldi(&b, "input_idle_timeout", cfg.Session.InputIdleTimeout, "Seconds of idle output before declaring a session is waiting for input")
	fieldi(&b, "tail_lines", cfg.Session.TailLines, "Default number of output lines returned by tail/status commands")
	fieldi(&b, "default_project_dir", cfg.Session.DefaultProjectDir, "Default working directory for new sessions")
	fieldi(&b, "root_path", cfg.Session.RootPath, "Restrict file browser to this directory tree (empty = no restriction)")
	fieldi(&b, "claude_code_bin", cfg.Session.ClaudeBin, "Path to the claude CLI binary")
	fieldi(&b, "claude_enabled", cfg.Session.ClaudeEnabled, "Enable the claude-code backend")
	fieldi(&b, "skip_permissions", cfg.Session.ClaudeSkipPermissions, "Pass --dangerously-skip-permissions to claude")
	fieldi(&b, "channel_enabled", cfg.Session.ClaudeChannelEnabled, "Enable per-session MCP channel for claude")
	fieldi(&b, "auto_git_init", cfg.Session.AutoGitInit, "Auto-initialize git repo in project directory")
	fieldi(&b, "auto_git_commit", cfg.Session.AutoGitCommit, "Auto-commit project changes after session completes")
	fieldi(&b, "kill_sessions_on_exit", cfg.Session.KillSessionsOnExit, "Kill all sessions when daemon stops")
	fieldi(&b, "mcp_max_retries", cfg.Session.MCPMaxRetries, "Max MCP channel restart attempts per session")
	fieldi(&b, "log_level", cfg.Session.LogLevel, "Log verbosity: info, debug, warn, error")
	b.WriteString("\n")

	section(&b, "Web Server", "HTTP/WebSocket server for the PWA, REST API, and browser access.")
	b.WriteString("server:\n")
	fieldi(&b, "enabled", cfg.Server.Enabled, "Enable the web server")
	fieldi(&b, "host", cfg.Server.Host, "Bind interface. Use 0.0.0.0 for all interfaces, 127.0.0.1 for localhost only")
	fieldi(&b, "port", cfg.Server.Port, "HTTP listen port")
	fieldi(&b, "token", cfg.Server.Token, "Bearer token for authentication (empty = no auth)")
	fieldi(&b, "tls_enabled", cfg.Server.TLSEnabled, "Enable HTTPS/TLS")
	fieldi(&b, "tls_auto_generate", cfg.Server.TLSAutoGenerate, "Auto-generate self-signed cert if tls_cert/tls_key are empty")
	fieldi(&b, "tls_cert", cfg.Server.TLSCert, "Path to TLS certificate PEM file")
	fieldi(&b, "tls_key", cfg.Server.TLSKey, "Path to TLS private key PEM file")
	fieldi(&b, "channel_port", cfg.Server.ChannelPort, "Per-session MCP channel port (0 = random)")
	fieldi(&b, "auto_restart_on_config", cfg.Server.AutoRestartOnConfig, "Auto-restart daemon when config is saved via web UI")
	fieldi(&b, "suppress_active_toasts", cfg.Server.SuppressActiveToasts, "Hide toast notifications for the currently viewed session")
	b.WriteString("\n")

	section(&b, "MCP Server", "Model Context Protocol server for IDE and AI agent integrations.")
	b.WriteString("mcp:\n")
	fieldi(&b, "enabled", cfg.MCP.Enabled, "Enable MCP stdio transport (Cursor, Claude Desktop, VS Code)")
	fieldi(&b, "sse_enabled", cfg.MCP.SSEEnabled, "Enable HTTP/SSE transport for remote AI clients")
	fieldi(&b, "sse_host", cfg.MCP.SSEHost, "SSE bind interface (127.0.0.1 for local, 0.0.0.0 for remote)")
	fieldi(&b, "sse_port", cfg.MCP.SSEPort, "SSE listen port")
	fieldi(&b, "token", cfg.MCP.Token, "Bearer token for SSE connections (empty = no auth)")
	fieldi(&b, "tls_enabled", cfg.MCP.TLSEnabled, "Enable TLS for SSE server")
	fieldi(&b, "tls_auto_generate", cfg.MCP.TLSAutoGenerate, "Auto-generate self-signed cert")
	fieldi(&b, "tls_cert", cfg.MCP.TLSCert, "Path to TLS certificate PEM file")
	fieldi(&b, "tls_key", cfg.MCP.TLSKey, "Path to TLS private key PEM file")
	b.WriteString("\n")

	section(&b, "Signal", "Signal messenger integration via signal-cli subprocess.")
	b.WriteString("signal:\n")
	fieldi(&b, "account_number", cfg.Signal.AccountNumber, "Signal phone number in E.164 format (e.g. +12125551234)")
	fieldi(&b, "group_id", cfg.Signal.GroupID, "Signal group ID in base64. Get via: signal-cli listGroups")
	fieldi(&b, "config_dir", cfg.Signal.ConfigDir, "signal-cli data directory")
	fieldi(&b, "device_name", cfg.Signal.DeviceName, "Name shown in Signal's Linked Devices list")
	b.WriteString("\n")

	section(&b, "Messaging Backends", "Communication channels for commands and notifications.")
	for _, mb := range []struct {
		name    string
		enabled bool
	}{
		{"telegram", cfg.Telegram.Enabled},
		{"discord", cfg.Discord.Enabled},
		{"slack", cfg.Slack.Enabled},
		{"matrix", cfg.Matrix.Enabled},
		{"twilio", cfg.Twilio.Enabled},
		{"ntfy", cfg.Ntfy.Enabled},
		{"email", cfg.Email.Enabled},
		{"github_webhook", cfg.GitHubWebhook.Enabled},
		{"webhook", cfg.Webhook.Enabled},
	} {
		b.WriteString(fmt.Sprintf("%s:\n  enabled: %v\n", mb.name, mb.enabled))
	}
	b.WriteString("\n")

	section(&b, "DNS Channel", "Covert command channel via DNS TXT queries with HMAC-SHA256 authentication.")
	b.WriteString("dns_channel:\n")
	fieldi(&b, "enabled", cfg.DNSChannel.Enabled, "Enable DNS channel")
	fieldi(&b, "mode", cfg.DNSChannel.Mode, "Operating mode: server (authoritative DNS) or client (query via resolver)")
	fieldi(&b, "domain", cfg.DNSChannel.Domain, "Domain name for DNS queries")
	fieldi(&b, "listen", cfg.DNSChannel.Listen, "Server bind address (e.g. :53 or 0.0.0.0:53)")
	fieldi(&b, "upstream", cfg.DNSChannel.Upstream, "Client upstream resolver (e.g. 8.8.8.8:53)")
	fieldi(&b, "secret", cfg.DNSChannel.Secret, "Shared HMAC-SHA256 secret for authentication")
	fieldi(&b, "rate_limit", cfg.DNSChannel.RateLimit, "Max queries per IP per minute (0 = unlimited)")
	fieldi(&b, "ttl", cfg.DNSChannel.TTL, "DNS response TTL in seconds (0 = non-cacheable)")
	fieldi(&b, "max_response_size", cfg.DNSChannel.MaxResponseSize, "Max response bytes before truncation")
	fieldi(&b, "poll_interval", cfg.DNSChannel.PollInterval, "Client polling interval (Go duration, e.g. 5s)")
	b.WriteString("\n")

	section(&b, "LLM Backends", "AI coding assistant backend configurations.")
	for _, lb := range []struct {
		name    string
		enabled bool
	}{
		{"ollama", cfg.Ollama.Enabled},
		{"openwebui", cfg.OpenWebUI.Enabled},
		{"aider", cfg.Aider.Enabled},
		{"goose", cfg.Goose.Enabled},
		{"gemini", cfg.Gemini.Enabled},
		{"opencode", cfg.OpenCode.Enabled},
		{"shell_backend", cfg.Shell.Enabled},
	} {
		b.WriteString(fmt.Sprintf("%s:\n  enabled: %v\n", lb.name, lb.enabled))
	}
	b.WriteString("\n")

	section(&b, "Auto-Update", "Automatic self-update configuration.")
	b.WriteString("update:\n")
	fieldi(&b, "enabled", cfg.Update.Enabled, "Enable automatic background updates")
	fieldi(&b, "schedule", cfg.Update.Schedule, "Check frequency: hourly, daily, weekly")
	fieldi(&b, "time_of_day", cfg.Update.TimeOfDay, "Time of day for daily/weekly checks (HH:MM)")

	return b.String()
}

func section(b *strings.Builder, title, desc string) {
	b.WriteString(fmt.Sprintf("# ── %s ──\n", title))
	if desc != "" {
		b.WriteString(fmt.Sprintf("# %s\n", desc))
	}
}

func field(b *strings.Builder, key string, val interface{}, comment string) {
	b.WriteString(fmt.Sprintf("# %s\n", comment))
	b.WriteString(fmt.Sprintf("%s: %s\n", key, yamlVal(val)))
}

func fieldi(b *strings.Builder, key string, val interface{}, comment string) {
	b.WriteString(fmt.Sprintf("  # %s\n", comment))
	b.WriteString(fmt.Sprintf("  %s: %s\n", key, yamlVal(val)))
}

func yamlVal(v interface{}) string {
	switch val := v.(type) {
	case string:
		if val == "" {
			return `""`
		}
		// Quote strings with special chars
		if strings.ContainsAny(val, ":{}[]!@#$%^&*|>< \t") {
			return fmt.Sprintf("%q", val)
		}
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}
