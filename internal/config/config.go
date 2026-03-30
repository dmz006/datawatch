package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RemoteServerConfig holds connection details for a remote datawatch instance.
type RemoteServerConfig struct {
	// Name is a short identifier used with --server flag (e.g. "prod", "pi").
	Name string `yaml:"name"`
	// URL is the base URL of the remote server (e.g. "http://192.168.1.10:8080").
	URL string `yaml:"url"`
	// Token is the bearer token for authentication (matches server.token on the remote).
	Token string `yaml:"token"`
	// Enabled controls whether this server is active.
	Enabled bool `yaml:"enabled"`
}

// Config holds all datawatch configuration.
type Config struct {
	// Signal configuration
	Signal SignalConfig `yaml:"signal"`

	// Session configuration
	Session SessionConfig `yaml:"session"`

	// Server configuration for the PWA/WebSocket server
	Server ServerConfig `yaml:"server"`

	// MCP server configuration (for Cursor, Claude Desktop, VS Code integration)
	MCP MCPConfig `yaml:"mcp"`

	// Hostname is used to prefix messages and identify this machine.
	// Auto-detected from OS hostname if empty.
	Hostname string `yaml:"hostname"`

	// DataDir is where sessions, logs, and state are stored.
	DataDir string `yaml:"data_dir"`

	// Detection holds global patterns for prompt/completion/rate-limit detection.
	// Per-LLM overrides can be set in each backend's detection field.
	Detection DetectionConfig `yaml:"detection,omitempty"`

	// LLM backends
	Ollama    OllamaConfig    `yaml:"ollama"`
	OpenWebUI OpenWebUIConfig `yaml:"openwebui"`
	Aider     AiderConfig     `yaml:"aider"`
	Goose     GooseConfig     `yaml:"goose"`
	Gemini    GeminiConfig    `yaml:"gemini"`
	OpenCode  OpenCodeConfig  `yaml:"opencode"`
	Shell     ShellBackendConfig `yaml:"shell_backend"`

	// DNSChannel holds DNS tunneling communication channel configuration.
	DNSChannel DNSChannelConfig `yaml:"dns_channel"`

	// Update controls automatic self-update behaviour.
	Update UpdateConfig `yaml:"update"`

	// Servers is a list of remote datawatch instances to manage.
	// The implicit "local" entry (localhost:Server.Port) is always available.
	Servers []RemoteServerConfig `yaml:"servers,omitempty"`

	// Messaging backends
	Discord       DiscordConfig       `yaml:"discord"`
	Slack         SlackConfig         `yaml:"slack"`
	Telegram      TelegramConfig      `yaml:"telegram"`
	Matrix        MatrixConfig        `yaml:"matrix"`
	Twilio        TwilioConfig        `yaml:"twilio"`
	Ntfy          NtfyConfig          `yaml:"ntfy"`
	Email         EmailConfig         `yaml:"email"`
	GitHubWebhook GitHubWebhookConfig `yaml:"github_webhook"`
	Webhook       WebhookConfig       `yaml:"webhook"`
}

// DetectionConfig holds patterns for detecting session state transitions.
// Global defaults are merged with per-LLM overrides; the combined list is used by the monitor.
type DetectionConfig struct {
	// PromptPatterns are suffixes/substrings that indicate the LLM is waiting for input.
	PromptPatterns []string `yaml:"prompt_patterns,omitempty"`
	// CompletionPatterns indicate the session has completed.
	CompletionPatterns []string `yaml:"completion_patterns,omitempty"`
	// RateLimitPatterns indicate a rate limit has been hit.
	RateLimitPatterns []string `yaml:"rate_limit_patterns,omitempty"`
	// InputNeededPatterns are explicit "needs input" markers (e.g. DATAWATCH_NEEDS_INPUT:).
	InputNeededPatterns []string `yaml:"input_needed_patterns,omitempty"`
}

// ---- LLM backends ----

// OllamaConfig holds Ollama backend configuration.
type OllamaConfig struct {
	Enabled     bool            `yaml:"enabled"`
	Model       string          `yaml:"model"`
	Host        string          `yaml:"host"`
	Detection   DetectionConfig `yaml:"detection,omitempty"` // per-LLM pattern overrides
	ConsoleCols int             `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
}

// OpenWebUIConfig holds OpenWebUI backend configuration.
type OpenWebUIConfig struct {
	Enabled     bool   `yaml:"enabled"`
	URL         string `yaml:"url"`
	Model       string `yaml:"model"`
	APIKey      string `yaml:"api_key"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
}

// AiderConfig holds aider LLM backend configuration.
type AiderConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
}

// GooseConfig holds goose LLM backend configuration.
type GooseConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
}

// GeminiConfig holds Gemini CLI LLM backend configuration.
type GeminiConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
}

// OpenCodeConfig holds opencode LLM backend configuration.
type OpenCodeConfig struct {
	Enabled           bool   `yaml:"enabled"`
	ACPEnabled        bool   `yaml:"acp_enabled"`
	PromptEnabled     bool   `yaml:"prompt_enabled"`
	Binary            string `yaml:"binary"`
	ACPStartupTimeout int    `yaml:"acp_startup_timeout"`
	ACPHealthInterval int    `yaml:"acp_health_interval"`
	ACPMessageTimeout int    `yaml:"acp_message_timeout"`
	ConsoleCols       int    `yaml:"console_cols,omitempty"`
	ConsoleRows       int    `yaml:"console_rows,omitempty"`
}

// ShellBackendConfig holds shell script LLM backend configuration.
type ShellBackendConfig struct {
	Enabled     bool   `yaml:"enabled"`
	ScriptPath  string `yaml:"script_path"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
}

// DNSChannelConfig holds DNS tunneling communication channel configuration.
type DNSChannelConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Mode            string `yaml:"mode"`              // "server" or "client"
	Domain          string `yaml:"domain"`             // authoritative subdomain (e.g. "ctl.example.com")
	Listen          string `yaml:"listen"`             // server: UDP/TCP bind address (default ":53")
	Upstream        string `yaml:"upstream"`           // client: resolver address (e.g. "8.8.8.8:53")
	Secret          string `yaml:"secret"`             // HMAC-SHA256 shared secret
	TTL             int    `yaml:"ttl"`                // DNS response TTL in seconds (0 = non-cacheable)
	MaxResponseSize int    `yaml:"max_response_size"`  // max response bytes before truncation (default 512)
	PollInterval    string `yaml:"poll_interval"`      // client polling interval (default "5s")
	RateLimit       int    `yaml:"rate_limit"`         // max queries per IP per minute (default 30, 0 = unlimited)
}

// ---- Messaging backends ----

// DiscordConfig holds Discord messaging backend configuration.
type DiscordConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Token     string `yaml:"token"`
	ChannelID string `yaml:"channel_id"`
	// AutoManageChannel creates a channel named after hostname if ChannelID is empty.
	AutoManageChannel bool `yaml:"auto_manage_channel"`
}

// SlackConfig holds Slack messaging backend configuration.
type SlackConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Token     string `yaml:"token"`
	ChannelID string `yaml:"channel_id"`
	// AutoManageChannel creates a channel named after hostname if ChannelID is empty.
	AutoManageChannel bool `yaml:"auto_manage_channel"`
}

// TelegramConfig holds Telegram bot configuration.
type TelegramConfig struct {
	Enabled bool  `yaml:"enabled"`
	Token   string `yaml:"token"`
	ChatID  int64  `yaml:"chat_id"`
	// AutoManageGroup creates a group named after hostname if ChatID is zero.
	// Note: Telegram bots cannot create groups; manual setup is required.
	AutoManageGroup bool `yaml:"auto_manage_group"`
}

// MatrixConfig holds Matrix homeserver configuration.
type MatrixConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Homeserver  string `yaml:"homeserver"`
	UserID      string `yaml:"user_id"`
	AccessToken string `yaml:"access_token"`
	RoomID      string `yaml:"room_id"`
	// AutoManageRoom creates a room named after hostname if RoomID is empty.
	AutoManageRoom bool `yaml:"auto_manage_room"`
}

// TwilioConfig holds Twilio SMS backend configuration.
type TwilioConfig struct {
	Enabled     bool   `yaml:"enabled"`
	AccountSID  string `yaml:"account_sid"`
	AuthToken   string `yaml:"auth_token"`
	FromNumber  string `yaml:"from_number"`
	// ToNumber is the phone number to send messages to (e.g. +12125551234).
	ToNumber    string `yaml:"to_number"`
	// WebhookAddr is the address for the incoming SMS webhook (e.g. ":9003").
	WebhookAddr string `yaml:"webhook_addr"`
}

// NtfyConfig holds ntfy push notification configuration.
type NtfyConfig struct {
	Enabled   bool   `yaml:"enabled"`
	ServerURL string `yaml:"server_url"`
	Topic     string `yaml:"topic"`
	Token     string `yaml:"token"`
}

// EmailConfig holds SMTP email notification configuration.
type EmailConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	To       string `yaml:"to"`
}

// GitHubWebhookConfig holds GitHub webhook listener configuration.
type GitHubWebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
	Secret  string `yaml:"secret"`
}

// WebhookConfig holds generic webhook listener configuration.
type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"`
	Token   string `yaml:"token"`
}

// ---- Service configs ----

// ServerConfig holds configuration for the HTTP/WebSocket PWA server.
type ServerConfig struct {
	// Enabled controls whether the HTTP/WebSocket server starts.
	Enabled bool `yaml:"enabled"`

	// Host to bind to. Use "0.0.0.0" to listen on all interfaces.
	Host string `yaml:"host"`

	// Port to listen on (default: 8080).
	Port int `yaml:"port"`

	// Token is an optional bearer token for authentication.
	// If empty, no auth is required.
	Token string `yaml:"token"`

	// TLSEnabled enables TLS for the server.
	TLSEnabled bool `yaml:"tls_enabled"`

	// TLSAutoGenerate creates a self-signed cert in DataDir/tls/server/ if
	// TLSCert and TLSKey are empty. Enabled by default when TLSEnabled is true.
	TLSAutoGenerate bool `yaml:"tls_auto_generate"`

	// TLSCert and TLSKey are paths to PEM-encoded cert/key files.
	// Leave empty to use auto-generated self-signed cert (when TLSAutoGenerate is true).
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`

	// ChannelPort is the HTTP port of the datawatch MCP channel server
	// (channel/dist/index.js). datawatch posts messages to :ChannelPort/send.
	// Default: 7433.
	ChannelPort int `yaml:"channel_port"`

	// TLSPort is an optional separate port for TLS. When set, the main port stays
	// plain HTTP and TLS runs on TLSPort. When empty/0, TLS replaces the main port.
	TLSPort int `yaml:"tls_port,omitempty"`

	// AutoRestartOnConfig triggers a daemon restart when config is saved via the web UI.
	// Default: false. Skips restart if encrypted config has no DATAWATCH_SECURE_PASSWORD.
	AutoRestartOnConfig bool `yaml:"auto_restart_on_config"`

	// RecentSessionMinutes controls how long completed sessions show in the active list (default 5).
	RecentSessionMinutes int `yaml:"recent_session_minutes"`

	// SuppressActiveToasts hides toast notifications for the currently viewed session
	// (e.g. state change toasts while you're watching the output). Default: true.
	SuppressActiveToasts bool `yaml:"suppress_active_toasts"`
}

// MCPConfig holds MCP server configuration for IDE and remote AI integrations.
type MCPConfig struct {
	// Enabled controls whether the MCP server is available via `datawatch mcp`.
	Enabled bool `yaml:"enabled"`

	// SSEEnabled starts an HTTP/SSE MCP server for remote AI clients in addition
	// to the stdio transport used by local IDE clients (Cursor, Claude Desktop).
	SSEEnabled bool `yaml:"sse_enabled"`

	// SSEHost and SSEPort set the bind address for the SSE server (default: 0.0.0.0:8081).
	SSEHost string `yaml:"sse_host"`
	SSEPort int    `yaml:"sse_port"`

	// Token is a bearer token required for SSE connections.
	// Remote AI clients must pass "Authorization: Bearer <token>".
	Token string `yaml:"token"`

	// TLSEnabled enables TLS for the MCP SSE server.
	TLSEnabled bool `yaml:"tls_enabled"`

	// TLSAutoGenerate creates a self-signed cert in DataDir/tls/mcp/ if
	// TLSCert and TLSKey are empty and TLSEnabled is true.
	TLSAutoGenerate bool `yaml:"tls_auto_generate"`

	// TLSCert and TLSKey are paths to PEM-encoded cert/key files for the SSE server.
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`
}

// SignalConfig holds Signal-specific configuration.
type SignalConfig struct {
	// AccountNumber is the Signal phone number (e.g. +12125551234)
	AccountNumber string `yaml:"account_number"`

	// GroupID is the Signal group ID (base64) to listen on
	GroupID string `yaml:"group_id"`

	// ConfigDir is the signal-cli config directory
	ConfigDir string `yaml:"config_dir"`

	// DeviceName is the name shown in Signal for this linked device.
	// Defaults to hostname.
	DeviceName string `yaml:"device_name"`
}

// SessionConfig holds session management configuration.
type SessionConfig struct {
	// MaxSessions is the max number of concurrent AI sessions
	MaxSessions int `yaml:"max_sessions"`

	// InputIdleTimeout is how long to wait for idle output before
	// declaring a session is waiting for input (seconds)
	InputIdleTimeout int `yaml:"input_idle_timeout"`

	// TailLines is the default number of lines returned by tail command
	TailLines int `yaml:"tail_lines"`

	// ClaudeEnabled controls whether claude-code backend is available.
	ClaudeEnabled bool `yaml:"claude_enabled"`

	// ClaudeBin is the path to the claude binary (claude-code backend only).
	ClaudeBin string `yaml:"claude_code_bin"`

	// LLMBackend selects which LLM backend to use. Default: "claude-code".
	LLMBackend string `yaml:"llm_backend"`

	// DefaultProjectDir is the working directory for sessions started via messaging
	// backends when no explicit directory is given. Defaults to the user's home directory.
	DefaultProjectDir string `yaml:"default_project_dir"`

	// AutoGitCommit enables automatic git commits before and after each session.
	AutoGitCommit bool `yaml:"auto_git_commit"`

	// AutoGitInit initializes a git repo in the project dir if one doesn't exist.
	AutoGitInit bool `yaml:"auto_git_init"`

	// ClaudeSkipPermissions passes --dangerously-skip-permissions to claude-code,
	// bypassing interactive permission prompts within the session's project dir.
	// Only applies to the claude-code backend.
	ClaudeSkipPermissions bool `yaml:"skip_permissions"`

	// ClaudeChannelEnabled enables MCP channel mode for claude-code sessions.
	// Adds --channels server:datawatch --dangerously-load-development-channels
	// so Claude can receive messages and send replies via the datawatch channel server.
	// Only applies to the claude-code backend.
	ClaudeChannelEnabled bool `yaml:"channel_enabled"`

	// KillSessionsOnExit terminates all running sessions when the daemon exits.
	KillSessionsOnExit bool `yaml:"kill_sessions_on_exit"`

	// LogLevel sets verbosity for session activity logging: debug, info, warn, error.
	LogLevel string `yaml:"log_level"`

	// RootPath restricts the file browser to this directory and below.
	// Users cannot navigate above this path when choosing a project directory.
	// Defaults to the user's home directory.
	RootPath string `yaml:"root_path"`

	// MCPMaxRetries is the max number of times to auto-retry /mcp when
	// "MCP server failed" is detected in claude-code session output. Default: 5.
	MCPMaxRetries int `yaml:"mcp_max_retries"`

	// ConsoleCols and ConsoleRows set the default tmux terminal size for new sessions.
	// Per-LLM overrides take priority. Default: 80x24.
	ConsoleCols int `yaml:"console_cols"`
	ConsoleRows int `yaml:"console_rows"`
}

// UpdateConfig controls automatic self-update behaviour.
type UpdateConfig struct {
	// Enabled controls whether the daemon checks for and installs updates automatically.
	Enabled bool `yaml:"enabled"`

	// Schedule is how often to check: "hourly", "daily", or "weekly". Default: "daily".
	Schedule string `yaml:"schedule"`

	// TimeOfDay is the local time to perform the check in "HH:MM" format (24h). Default: "03:00".
	TimeOfDay string `yaml:"time_of_day"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	return &Config{
		Hostname: hostname,
		DataDir:  filepath.Join(home, ".datawatch"),
		Signal: SignalConfig{
			ConfigDir:  filepath.Join(home, ".local", "share", "signal-cli"),
			DeviceName: hostname,
		},
		Session: SessionConfig{
			MaxSessions:           10,
			InputIdleTimeout:      10,
			TailLines:             20,
			ClaudeBin:             "claude",
			LLMBackend:            "claude-code",
			DefaultProjectDir:     home,
			AutoGitCommit:         true,
			AutoGitInit:           false,
			ClaudeEnabled:         true,
			ClaudeChannelEnabled:  true,
			ClaudeSkipPermissions: true,
			MCPMaxRetries:        5,
		},
		Server: ServerConfig{
			Enabled:              true,
			Host:                 "0.0.0.0",
			Port:                 8080,
			TLSAutoGenerate:      true,
			RecentSessionMinutes: 5,
			SuppressActiveToasts: true,
		},
		MCP: MCPConfig{
			Enabled:         true,
			SSEHost:         "127.0.0.1",
			SSEPort:         8081,
			TLSAutoGenerate: true,
		},
		Ollama: OllamaConfig{
			Model: "llama3",
			Host:  "http://localhost:11434",
		},
		OpenWebUI: OpenWebUIConfig{
			URL:   "http://localhost:3000",
			Model: "llama3",
		},
		Ntfy:          NtfyConfig{ServerURL: "https://ntfy.sh"},
		Email:         EmailConfig{Port: 587},
		GitHubWebhook: GitHubWebhookConfig{Addr: "127.0.0.1:9001"},
		Webhook:       WebhookConfig{Addr: "127.0.0.1:9002"},
		Twilio:        TwilioConfig{WebhookAddr: "127.0.0.1:9003"},
		Aider:         AiderConfig{Binary: "aider"},
		Goose:         GooseConfig{Binary: "goose"},
		Gemini:        GeminiConfig{Binary: "gemini"},
		OpenCode:      OpenCodeConfig{Binary: "opencode"},
	}
}

// GetConsoleSize returns (cols, rows) for a given LLM backend.
// Per-LLM values override the global session default; 0 means use default 80x24.
func (c *Config) GetConsoleSize(backend string) (int, int) {
	defaultCols := c.Session.ConsoleCols
	defaultRows := c.Session.ConsoleRows
	if defaultCols <= 0 {
		defaultCols = 80
	}
	if defaultRows <= 0 {
		defaultRows = 24
	}

	var cols, rows int
	switch backend {
	case "claude-code":
		cols, rows = 120, 40 // claude works best at 100-120 cols
	case "aider":
		cols, rows = c.Aider.ConsoleCols, c.Aider.ConsoleRows
	case "goose":
		cols, rows = c.Goose.ConsoleCols, c.Goose.ConsoleRows
	case "gemini":
		cols, rows = c.Gemini.ConsoleCols, c.Gemini.ConsoleRows
	case "ollama":
		cols, rows = c.Ollama.ConsoleCols, c.Ollama.ConsoleRows
		if cols <= 0 { cols = 120 } // ollama interactive needs wider display
	case "opencode", "opencode-acp", "opencode-prompt":
		cols, rows = c.OpenCode.ConsoleCols, c.OpenCode.ConsoleRows
		if cols <= 0 { cols = 120 } // opencode TUI needs wider display
	case "openwebui":
		cols, rows = c.OpenWebUI.ConsoleCols, c.OpenWebUI.ConsoleRows
		if cols <= 0 { cols = 120 }
	case "shell":
		cols, rows = c.Shell.ConsoleCols, c.Shell.ConsoleRows
	}
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	return cols, rows
}

// DefaultDetection returns the built-in detection patterns.
// These are the same patterns that were previously hardcoded in manager.go.
func DefaultDetection() DetectionConfig {
	return DetectionConfig{
		PromptPatterns: []string{
			"? ", "> ", "$ ", "# ", "[y/N]", "[Y/n]", "(y/n)", "[yes/no]",
			"Do you want to", "Allow ", "Deny ", "Trust ", "trust the files",
			"(y/n/always)", "(yes/no/always)", "Allow this action",
			"Would you like", "Proceed?", "[A]llow", "[D]eny",
			"Yes, I trust", "No, exit", "trust this folder", "Quick safety check",
			"Is this a project", "1. Yes", "2. No",
			"❯ 1.", "❯ 2.",
			"Enter to confirm", "Esc to cancel",
			"I am using this for local development", "Loading development channels",
			"[opencode-acp] awaiting input", "[opencode-acp] ready",
			">>> ",
			"What do you want to do?",
			"Esc to back", "Esc to go back",
			"↑↓ to navigate",
			// opencode TUI prompt
			"Ask anything",
		},
		CompletionPatterns: []string{
			"DATAWATCH_COMPLETE:",
		},
		RateLimitPatterns: []string{
			"DATAWATCH_RATE_LIMITED:",
			"You've hit your limit",
			"rate limit exceeded",
			"quota exceeded",
		},
		InputNeededPatterns: []string{
			"DATAWATCH_NEEDS_INPUT:",
		},
	}
}

// GetDetection returns the merged detection config for a given LLM backend.
// Per-LLM patterns are appended to global patterns (not replaced).
func (c *Config) GetDetection(backend string) DetectionConfig {
	base := c.Detection
	defaults := DefaultDetection()

	// Start with built-in defaults, then merge global config overrides
	if len(base.PromptPatterns) == 0 {
		base.PromptPatterns = defaults.PromptPatterns
	}
	if len(base.CompletionPatterns) == 0 {
		base.CompletionPatterns = defaults.CompletionPatterns
	}
	if len(base.RateLimitPatterns) == 0 {
		base.RateLimitPatterns = defaults.RateLimitPatterns
	}
	if len(base.InputNeededPatterns) == 0 {
		base.InputNeededPatterns = defaults.InputNeededPatterns
	}

	// Merge per-LLM overrides (append to global, not replace)
	var llmDet DetectionConfig
	switch backend {
	case "ollama":
		llmDet = c.Ollama.Detection
	}
	if len(llmDet.PromptPatterns) > 0 {
		base.PromptPatterns = append(base.PromptPatterns, llmDet.PromptPatterns...)
	}
	if len(llmDet.CompletionPatterns) > 0 {
		base.CompletionPatterns = append(base.CompletionPatterns, llmDet.CompletionPatterns...)
	}
	if len(llmDet.RateLimitPatterns) > 0 {
		base.RateLimitPatterns = append(base.RateLimitPatterns, llmDet.RateLimitPatterns...)
	}
	if len(llmDet.InputNeededPatterns) > 0 {
		base.InputNeededPatterns = append(base.InputNeededPatterns, llmDet.InputNeededPatterns...)
	}
	return base
}

// applyDefaults fills zero-value fields with sensible defaults after unmarshalling.
func applyDefaults(cfg *Config) {
	if cfg.Hostname == "" {
		cfg.Hostname, _ = os.Hostname()
	}
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".datawatch")
	}
	if cfg.Signal.ConfigDir == "" {
		home, _ := os.UserHomeDir()
		cfg.Signal.ConfigDir = filepath.Join(home, ".local", "share", "signal-cli")
	}
	if cfg.Signal.DeviceName == "" {
		cfg.Signal.DeviceName = cfg.Hostname
	}
	if cfg.Session.MaxSessions == 0 {
		cfg.Session.MaxSessions = 10
	}
	if cfg.Session.InputIdleTimeout == 0 {
		cfg.Session.InputIdleTimeout = 10
	}
	if cfg.Session.TailLines == 0 {
		cfg.Session.TailLines = 20
	}
	if cfg.Session.ClaudeBin == "" {
		cfg.Session.ClaudeBin = "claude"
	}
	if cfg.Session.LLMBackend == "" {
		cfg.Session.LLMBackend = "claude-code"
	}
	if cfg.Session.DefaultProjectDir == "" {
		home, _ := os.UserHomeDir()
		cfg.Session.DefaultProjectDir = home
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.MCP.SSEPort == 0 {
		cfg.MCP.SSEPort = 8081
	}
	if cfg.MCP.SSEHost == "" {
		cfg.MCP.SSEHost = "127.0.0.1"
	}
}

// Load reads config from the given path, merging over defaults for missing fields.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

// LoadSecure reads config from path, decrypting if encrypted.
// If password is nil and the file is encrypted, returns an error.
func LoadSecure(path string, password []byte) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	if IsEncrypted(data) {
		if password == nil {
			return nil, fmt.Errorf("config file is encrypted — use --secure and provide a password")
		}
		data, err = Decrypt(data, password)
		if err != nil {
			return nil, err
		}
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyDefaults(cfg)
	return cfg, nil
}

// SaveSecure writes config to path, encrypting if password is non-nil.
func SaveSecure(cfg *Config, path string, password []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if password != nil {
		data, err = Encrypt(data, password)
		if err != nil {
			return fmt.Errorf("encrypt config: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// Save writes config to the given path, creating parent directories as needed.
// MarshalYAML serializes a Config to YAML bytes.
func MarshalYAML(cfg *Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".datawatch", "config.yaml")
}
