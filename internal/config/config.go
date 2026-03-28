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

	// LLM backends
	Ollama    OllamaConfig    `yaml:"ollama"`
	OpenWebUI OpenWebUIConfig `yaml:"openwebui"`
	Aider     AiderConfig     `yaml:"aider"`
	Goose     GooseConfig     `yaml:"goose"`
	Gemini    GeminiConfig    `yaml:"gemini"`
	OpenCode  OpenCodeConfig  `yaml:"opencode"`
	Shell     ShellBackendConfig `yaml:"shell_backend"`

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

// ---- LLM backends ----

// OllamaConfig holds Ollama backend configuration.
type OllamaConfig struct {
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"`
	Host    string `yaml:"host"`
}

// OpenWebUIConfig holds OpenWebUI backend configuration.
type OpenWebUIConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Model   string `yaml:"model"`
	APIKey  string `yaml:"api_key"`
}

// AiderConfig holds aider LLM backend configuration.
type AiderConfig struct {
	Enabled bool   `yaml:"enabled"`
	Binary  string `yaml:"binary"`
}

// GooseConfig holds goose LLM backend configuration.
type GooseConfig struct {
	Enabled bool   `yaml:"enabled"`
	Binary  string `yaml:"binary"`
}

// GeminiConfig holds Gemini CLI LLM backend configuration.
type GeminiConfig struct {
	Enabled bool   `yaml:"enabled"`
	Binary  string `yaml:"binary"`
}

// OpenCodeConfig holds opencode LLM backend configuration.
type OpenCodeConfig struct {
	Enabled bool   `yaml:"enabled"`
	Binary  string `yaml:"binary"`
}

// ShellBackendConfig holds shell script LLM backend configuration.
type ShellBackendConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ScriptPath string `yaml:"script_path"`
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
			ClaudeChannelEnabled:  true,
			ClaudeSkipPermissions: true,
		},
		Server: ServerConfig{
			Enabled:         true,
			Host:            "0.0.0.0",
			Port:            8080,
			TLSAutoGenerate: true,
		},
		MCP: MCPConfig{
			Enabled:         true,
			SSEHost:         "0.0.0.0",
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
		GitHubWebhook: GitHubWebhookConfig{Addr: ":9001"},
		Webhook:       WebhookConfig{Addr: ":9002"},
		Twilio:        TwilioConfig{WebhookAddr: ":9003"},
		Aider:         AiderConfig{Binary: "aider"},
		Goose:         GooseConfig{Binary: "goose"},
		Gemini:        GeminiConfig{Binary: "gemini"},
		OpenCode:      OpenCodeConfig{Binary: "opencode"},
	}
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
		cfg.MCP.SSEHost = "0.0.0.0"
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
