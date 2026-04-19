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
	fieldi(&b, "alert_context_lines", cfg.Session.AlertContextLines, "Number of non-empty output lines included in prompt alerts (default 10)")
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
	fieldi(&b, "schedule_settle_ms", cfg.Session.ScheduleSettleMs, "B30: ms to wait between text and Enter for scheduled commands (0 = legacy single send-keys; 200 default fixes the 2nd-Enter bug for TUIs like claude-code)")
	fieldi(&b, "log_level", cfg.Session.LogLevel, "Log verbosity: info, debug, warn, error")
	fieldi(&b, "console_cols", cfg.Session.ConsoleCols, "Default terminal width (cols) for new sessions (0 = 80)")
	fieldi(&b, "console_rows", cfg.Session.ConsoleRows, "Default terminal height (rows) for new sessions (0 = 24)")
	b.WriteString("\n")

	section(&b, "Web Server", "HTTP/WebSocket server for the PWA, REST API, and browser access.")
	b.WriteString("server:\n")
	fieldi(&b, "enabled", cfg.Server.Enabled, "Enable the web server")
	fieldi(&b, "host", cfg.Server.Host, "Bind interface. Use 0.0.0.0 for all interfaces, 127.0.0.1 for localhost only")
	fieldi(&b, "port", cfg.Server.Port, "HTTP listen port")
	fieldi(&b, "token", cfg.Server.Token, "Bearer token for authentication (empty = no auth)")
	fieldi(&b, "tls_enabled", cfg.Server.TLSEnabled, "Enable HTTPS/TLS")
	fieldi(&b, "tls_port", cfg.Server.TLSPort, "Separate TLS port (0 = TLS replaces main port)")
	fieldi(&b, "tls_auto_generate", cfg.Server.TLSAutoGenerate, "Auto-generate self-signed cert if tls_cert/tls_key are empty")
	fieldi(&b, "tls_cert", cfg.Server.TLSCert, "Path to TLS certificate PEM file")
	fieldi(&b, "tls_key", cfg.Server.TLSKey, "Path to TLS private key PEM file")
	fieldi(&b, "channel_port", cfg.Server.ChannelPort, "Per-session MCP channel port (0 = random)")
	fieldi(&b, "recent_session_minutes", cfg.Server.RecentSessionMinutes, "How long completed sessions show in active list (minutes)")
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

	b.WriteString("telegram:\n")
	fieldi(&b, "enabled", cfg.Telegram.Enabled, "Enable Telegram bot")
	fieldi(&b, "token", cfg.Telegram.Token, "Bot token from @BotFather")
	fieldi(&b, "chat_id", cfg.Telegram.ChatID, "Chat/group ID")
	fieldi(&b, "auto_manage_group", cfg.Telegram.AutoManageGroup, "Auto-manage group membership")
	b.WriteString("discord:\n")
	fieldi(&b, "enabled", cfg.Discord.Enabled, "Enable Discord bot")
	fieldi(&b, "token", cfg.Discord.Token, "Bot token")
	fieldi(&b, "channel_id", cfg.Discord.ChannelID, "Channel ID")
	fieldi(&b, "auto_manage_channel", cfg.Discord.AutoManageChannel, "Auto-manage channel")
	b.WriteString("slack:\n")
	fieldi(&b, "enabled", cfg.Slack.Enabled, "Enable Slack bot")
	fieldi(&b, "token", cfg.Slack.Token, "OAuth bot token")
	fieldi(&b, "channel_id", cfg.Slack.ChannelID, "Channel ID")
	fieldi(&b, "auto_manage_channel", cfg.Slack.AutoManageChannel, "Auto-manage channel")
	b.WriteString("matrix:\n")
	fieldi(&b, "enabled", cfg.Matrix.Enabled, "Enable Matrix homeserver")
	fieldi(&b, "homeserver", cfg.Matrix.Homeserver, "Homeserver URL")
	fieldi(&b, "user_id", cfg.Matrix.UserID, "Bot user ID (@bot:host)")
	fieldi(&b, "access_token", cfg.Matrix.AccessToken, "Access token")
	fieldi(&b, "room_id", cfg.Matrix.RoomID, "Room ID")
	fieldi(&b, "auto_manage_room", cfg.Matrix.AutoManageRoom, "Auto-manage room")
	b.WriteString("twilio:\n")
	fieldi(&b, "enabled", cfg.Twilio.Enabled, "Enable Twilio SMS")
	fieldi(&b, "account_sid", cfg.Twilio.AccountSID, "Account SID")
	fieldi(&b, "auth_token", cfg.Twilio.AuthToken, "Auth token")
	fieldi(&b, "from_number", cfg.Twilio.FromNumber, "From phone number")
	fieldi(&b, "to_number", cfg.Twilio.ToNumber, "To phone number")
	fieldi(&b, "webhook_addr", cfg.Twilio.WebhookAddr, "Webhook listen address")
	b.WriteString("ntfy:\n")
	fieldi(&b, "enabled", cfg.Ntfy.Enabled, "Enable ntfy push notifications")
	fieldi(&b, "server_url", cfg.Ntfy.ServerURL, "ntfy server URL")
	fieldi(&b, "topic", cfg.Ntfy.Topic, "Topic name")
	fieldi(&b, "token", cfg.Ntfy.Token, "Auth token (optional)")
	b.WriteString("email:\n")
	fieldi(&b, "enabled", cfg.Email.Enabled, "Enable email notifications")
	fieldi(&b, "host", cfg.Email.Host, "SMTP host")
	fieldi(&b, "port", cfg.Email.Port, "SMTP port")
	fieldi(&b, "username", cfg.Email.Username, "SMTP username")
	fieldi(&b, "password", cfg.Email.Password, "SMTP password")
	fieldi(&b, "from", cfg.Email.From, "From address")
	fieldi(&b, "to", cfg.Email.To, "To address")
	b.WriteString("github_webhook:\n")
	fieldi(&b, "enabled", cfg.GitHubWebhook.Enabled, "Enable GitHub webhook")
	fieldi(&b, "addr", cfg.GitHubWebhook.Addr, "Listen address")
	fieldi(&b, "secret", cfg.GitHubWebhook.Secret, "HMAC webhook secret")
	b.WriteString("webhook:\n")
	fieldi(&b, "enabled", cfg.Webhook.Enabled, "Enable generic webhook")
	fieldi(&b, "addr", cfg.Webhook.Addr, "Listen address")
	fieldi(&b, "token", cfg.Webhook.Token, "Bearer token (optional)")
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

	section(&b, "LLM Backends", "AI coding assistant backend configurations. Each has optional console_cols/console_rows for terminal size.")

	b.WriteString("ollama:\n")
	fieldi(&b, "enabled", cfg.Ollama.Enabled, "Enable Ollama backend")
	fieldi(&b, "model", cfg.Ollama.Model, "Model name (e.g. llama3, codellama)")
	fieldi(&b, "host", cfg.Ollama.Host, "Ollama API URL")
	fieldi(&b, "console_cols", cfg.Ollama.ConsoleCols, "Terminal width (0 = use session default)")
	fieldi(&b, "console_rows", cfg.Ollama.ConsoleRows, "Terminal height (0 = use session default)")

	b.WriteString("openwebui:\n")
	fieldi(&b, "enabled", cfg.OpenWebUI.Enabled, "Enable OpenWebUI backend")
	fieldi(&b, "url", cfg.OpenWebUI.URL, "OpenWebUI server URL")
	fieldi(&b, "model", cfg.OpenWebUI.Model, "Model name")
	fieldi(&b, "api_key", cfg.OpenWebUI.APIKey, "API key")
	fieldi(&b, "console_cols", cfg.OpenWebUI.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.OpenWebUI.ConsoleRows, "Terminal height")

	b.WriteString("aider:\n")
	fieldi(&b, "enabled", cfg.Aider.Enabled, "Enable aider backend")
	fieldi(&b, "binary", cfg.Aider.Binary, "Path to aider binary")
	fieldi(&b, "console_cols", cfg.Aider.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.Aider.ConsoleRows, "Terminal height")

	b.WriteString("goose:\n")
	fieldi(&b, "enabled", cfg.Goose.Enabled, "Enable goose backend")
	fieldi(&b, "binary", cfg.Goose.Binary, "Path to goose binary")
	fieldi(&b, "console_cols", cfg.Goose.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.Goose.ConsoleRows, "Terminal height")

	b.WriteString("gemini:\n")
	fieldi(&b, "enabled", cfg.Gemini.Enabled, "Enable Gemini CLI backend")
	fieldi(&b, "binary", cfg.Gemini.Binary, "Path to gemini binary")
	fieldi(&b, "console_cols", cfg.Gemini.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.Gemini.ConsoleRows, "Terminal height")

	b.WriteString("opencode:\n")
	fieldi(&b, "enabled", cfg.OpenCode.Enabled, "Enable opencode TUI backend")
	fieldi(&b, "binary", cfg.OpenCode.Binary, "Path to opencode binary")
	fieldi(&b, "console_cols", cfg.OpenCode.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.OpenCode.ConsoleRows, "Terminal height")
	fieldi(&b, "output_mode", cfg.OpenCode.OutputMode, "Output display mode: terminal (default) or log")

	b.WriteString("opencode_acp:\n")
	fieldi(&b, "enabled", cfg.OpenCodeACP.Enabled, "Enable opencode ACP (headless server) backend")
	fieldi(&b, "binary", cfg.OpenCodeACP.Binary, "Path to opencode binary (defaults to opencode.binary)")
	fieldi(&b, "acp_startup_timeout", cfg.OpenCodeACP.ACPStartupTimeout, "ACP startup timeout (seconds)")
	fieldi(&b, "acp_health_interval", cfg.OpenCodeACP.ACPHealthInterval, "ACP health check interval (seconds)")
	fieldi(&b, "acp_message_timeout", cfg.OpenCodeACP.ACPMessageTimeout, "ACP message timeout (seconds)")
	fieldi(&b, "console_cols", cfg.OpenCodeACP.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.OpenCodeACP.ConsoleRows, "Terminal height")
	fieldi(&b, "output_mode", cfg.OpenCodeACP.OutputMode, "Output display mode: terminal or log (default)")

	b.WriteString("opencode_prompt:\n")
	fieldi(&b, "enabled", cfg.OpenCodePrompt.Enabled, "Enable opencode prompt-mode backend")
	fieldi(&b, "binary", cfg.OpenCodePrompt.Binary, "Path to opencode binary (defaults to opencode.binary)")
	fieldi(&b, "console_cols", cfg.OpenCodePrompt.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.OpenCodePrompt.ConsoleRows, "Terminal height")
	fieldi(&b, "output_mode", cfg.OpenCodePrompt.OutputMode, "Output display mode: terminal (default) or log")

	b.WriteString("shell_backend:\n")
	fieldi(&b, "enabled", cfg.Shell.Enabled, "Enable shell script backend")
	fieldi(&b, "script_path", cfg.Shell.ScriptPath, "Script path (empty = interactive shell)")
	fieldi(&b, "console_cols", cfg.Shell.ConsoleCols, "Terminal width")
	fieldi(&b, "console_rows", cfg.Shell.ConsoleRows, "Terminal height")
	b.WriteString("\n")

	section(&b, "Detection Filters", "Configurable patterns for session state detection. One pattern per line.")
	b.WriteString("detection:\n")
	fieldi(&b, "prompt_patterns", "[]", "Patterns indicating LLM is waiting for input (empty = use built-in defaults)")
	fieldi(&b, "completion_patterns", "[]", "Patterns indicating session completed")
	fieldi(&b, "rate_limit_patterns", "[]", "Patterns indicating rate limit hit")
	fieldi(&b, "input_needed_patterns", "[]", "Explicit protocol markers for input needed")
	b.WriteString("\n")

	section(&b, "Episodic Memory", "Vector-indexed project knowledge with semantic search and task learnings.")
	b.WriteString("memory:\n")
	fieldi(&b, "enabled", cfg.Memory.Enabled, "Enable episodic memory system")
	fieldi(&b, "backend", "sqlite", "Storage backend: sqlite (default, pure Go) or postgres (enterprise, pgvector)")
	fieldi(&b, "db_path", "", "SQLite database path (default: {data_dir}/memory.db)")
	fieldi(&b, "postgres_url", "", "PostgreSQL connection string (only for backend=postgres)")
	fieldi(&b, "embedder", "ollama", "Embedding provider: ollama (free, local) or openai (better quality)")
	fieldi(&b, "embedder_model", "nomic-embed-text", "Embedding model name")
	fieldi(&b, "embedder_host", "", "Embedder API URL (default: same as ollama.host)")
	fieldi(&b, "openai_key", "", "OpenAI API key (only for embedder=openai)")
	fieldi(&b, "dimensions", 0, "Vector dimensions (0 = auto-detect from model)")
	fieldi(&b, "top_k", 5, "Number of results for similarity search")
	fieldi(&b, "auto_save", true, "Save session summaries on completion")
	fieldi(&b, "learnings_enabled", true, "Extract task learnings on completion")
	fieldi(&b, "retention_days", 0, "Auto-prune memories older than N days (0 = keep forever)")
	b.WriteString("\n")

	section(&b, "Proxy", "Connection pooling, circuit breaker, and offline queuing for remote servers.")
	b.WriteString("proxy:\n")
	fieldi(&b, "enabled", cfg.Proxy.Enabled, "Enable proxy aggregation mode")
	fieldi(&b, "health_interval", cfg.Proxy.HealthInterval, "Seconds between remote health checks (default 30)")
	fieldi(&b, "request_timeout", cfg.Proxy.RequestTimeout, "Seconds per remote request (default 10)")
	fieldi(&b, "offline_queue_size", cfg.Proxy.OfflineQueueSize, "Max queued commands per server (default 100)")
	fieldi(&b, "circuit_breaker_threshold", cfg.Proxy.CircuitBreakerThreshold, "Failures before marking server down (default 3)")
	fieldi(&b, "circuit_breaker_reset", cfg.Proxy.CircuitBreakerReset, "Seconds before retrying downed server (default 30)")
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
