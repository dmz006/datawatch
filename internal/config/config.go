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
	// URL is the base URL of the remote server (e.g. "http://203.0.113.10:8080").
	URL string `yaml:"url"`
	// Token is the bearer token for authentication (matches server.token on the remote).
	Token string `yaml:"token"`
	// Enabled controls whether this server is active.
	Enabled bool `yaml:"enabled"`
}

// MemoryConfig controls the episodic memory system — vector-indexed project
// knowledge with semantic search and task learnings extraction.
type MemoryConfig struct {
	// Enabled activates the memory system (default false).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Backend selects the storage backend: "sqlite" (default, pure Go, no root needed)
	// or "postgres" (enterprise, requires PostgreSQL with pgvector extension).
	Backend string `yaml:"backend,omitempty"`
	// DBPath is the SQLite database file path (default: {data_dir}/memory.db).
	// Ignored when backend is "postgres".
	DBPath string `yaml:"db_path,omitempty"`
	// PostgresURL is the connection string for PostgreSQL backend (e.g. "postgres://user:pass@host/db").
	PostgresURL string `yaml:"postgres_url,omitempty"`
	// FallbackSQLite controls what happens when Backend is "postgres"
	// but the PostgresURL connection fails at startup. F10 S6.7 —
	// when true, the daemon logs a warning and falls back to the
	// SQLite store at DBPath. When false (default), the daemon
	// surfaces the error and disables memory entirely. Useful for
	// slim worker images that prefer "always have local memory" over
	// "always shared with the parent". Configurable via every channel.
	FallbackSQLite bool `yaml:"fallback_sqlite,omitempty"`
	// Embedder selects the embedding provider: "ollama" (default, free, local)
	// or "openai" (better quality, requires API key).
	Embedder string `yaml:"embedder,omitempty"`
	// EmbedderModel is the embedding model name (default: "nomic-embed-text" for ollama,
	// "text-embedding-3-small" for openai).
	EmbedderModel string `yaml:"embedder_model,omitempty"`
	// EmbedderHost is the embedder API URL. Defaults to ollama.host if empty.
	EmbedderHost string `yaml:"embedder_host,omitempty"`
	// OpenAIKey is the API key for OpenAI embeddings (only used when embedder=openai).
	OpenAIKey string `yaml:"openai_key,omitempty"`
	// Dimensions is the embedding vector dimensionality (auto-detected from model if 0).
	Dimensions int `yaml:"dimensions,omitempty"`
	// TopK is the number of results to return from similarity search (default 5).
	TopK int `yaml:"top_k,omitempty"`
	// AutoSave automatically saves session summaries on completion (default true when enabled).
	AutoSave *bool `yaml:"auto_save,omitempty"`
	// LearningsEnabled enables automatic task learnings extraction (default true when enabled).
	LearningsEnabled *bool `yaml:"learnings_enabled,omitempty"`
	// RetentionDays is how long memories are kept before auto-pruning (0 = forever, default).
	RetentionDays int `yaml:"retention_days,omitempty"`
	// StorageMode controls how session content is stored: "summary" (default, compact)
	// or "verbatim" (full prompt+response, higher retrieval accuracy).
	StorageMode string `yaml:"storage_mode,omitempty"`
	// EntityDetection enables automatic extraction of people/projects/tools from text
	// and populates the knowledge graph. (default false)
	EntityDetection bool `yaml:"entity_detection,omitempty"`
	// AutoHooks enables automatic Claude Code hook installation per-session.
	// When true, datawatch writes .claude/settings.local.json in the project dir
	// before launching Claude Code so memory hooks fire automatically.
	// Default: true when memory is enabled.
	AutoHooks *bool `yaml:"auto_hooks,omitempty"`
	// HookSaveInterval is how many human messages between auto-saves (default 15).
	HookSaveInterval int `yaml:"hook_save_interval,omitempty"`
	// SessionAwareness injects memory instructions into session guardrails (default true).
	SessionAwareness *bool `yaml:"session_awareness,omitempty"`
	// SessionBroadcast broadcasts session summaries to active sessions (default true).
	SessionBroadcast *bool `yaml:"session_broadcast,omitempty"`
	// RetentionSessionDays overrides retention for session summaries (0 = use RetentionDays).
	RetentionSessionDays int `yaml:"retention_session_days,omitempty"`
	// RetentionChunkDays overrides retention for output chunks (0 = use RetentionDays).
	RetentionChunkDays int `yaml:"retention_chunk_days,omitempty"`
}

// IsAutoHooks returns whether auto-hook installation is enabled (defaults to true).
func (m MemoryConfig) IsAutoHooks() bool {
	if m.AutoHooks == nil {
		return true
	}
	return *m.AutoHooks
}

// EffectiveHookInterval returns the hook save interval, defaulting to 15.
func (m MemoryConfig) EffectiveHookInterval() int {
	if m.HookSaveInterval <= 0 {
		return 15
	}
	return m.HookSaveInterval
}

// IsSessionAwareness returns whether memory awareness is injected into sessions (default true).
func (m MemoryConfig) IsSessionAwareness() bool {
	if m.SessionAwareness == nil { return true }
	return *m.SessionAwareness
}

// IsSessionBroadcast returns whether session summaries are broadcast (default true).
func (m MemoryConfig) IsSessionBroadcast() bool {
	if m.SessionBroadcast == nil { return true }
	return *m.SessionBroadcast
}

// EffectiveStorageMode returns the storage mode, defaulting to "summary".
func (m MemoryConfig) EffectiveStorageMode() string {
	if m.StorageMode == "verbatim" {
		return "verbatim"
	}
	return "summary"
}

// IsAutoSave returns whether auto-save is enabled (defaults to true).
func (m MemoryConfig) IsAutoSave() bool {
	if m.AutoSave == nil {
		return true
	}
	return *m.AutoSave
}

// IsLearningsEnabled returns whether learnings extraction is enabled (defaults to true).
func (m MemoryConfig) IsLearningsEnabled() bool {
	if m.LearningsEnabled == nil {
		return true
	}
	return *m.LearningsEnabled
}

// EffectiveBackend returns the storage backend, defaulting to "sqlite".
func (m MemoryConfig) EffectiveBackend() string {
	if m.Backend == "" {
		return "sqlite"
	}
	return m.Backend
}

// EffectiveEmbedder returns the embedding provider, defaulting to "ollama".
func (m MemoryConfig) EffectiveEmbedder() string {
	if m.Embedder == "" {
		return "ollama"
	}
	return m.Embedder
}

// EffectiveTopK returns the top-K value, defaulting to 5.
func (m MemoryConfig) EffectiveTopK() int {
	if m.TopK <= 0 {
		return 5
	}
	return m.TopK
}

// AgentsConfig controls the F10 ephemeral-agent spawn/bootstrap layer.
// All fields are optional; NewDockerDriver / NewManager pick sensible
// defaults when unset.
type AgentsConfig struct {
	// ImagePrefix is prepended to the Project Profile's image_pair.agent
	// when forming the full image reference. Overridable per-cluster via
	// ClusterProfile.ImageRegistry. Examples:
	//   "ghcr.io/your-org/datawatch"
	//   "harbor.example.com/datawatch"
	//   "registry.gitlab.com/your-group/datawatch"
	//   "localhost:5000/datawatch"  (local-dev fallback)
	// Empty = workers use the bare image name (assumes operator pre-
	// pulled or uses ClusterProfile.ImageRegistry per spawn).
	ImagePrefix string `yaml:"image_prefix,omitempty" json:"image_prefix,omitempty"`

	// ImageTag is the tag to pull. Defaults to "v" + daemon version so
	// operators running v2.4.5 get v2.4.5 worker images by default.
	// Override to pin workers to a specific release or use "latest" for
	// cutting-edge.
	ImageTag string `yaml:"image_tag,omitempty" json:"image_tag,omitempty"`

	// DockerBin is the binary the Docker driver shells out to.
	// Default "docker"; set to "podman" for rootless deploys.
	DockerBin string `yaml:"docker_bin,omitempty" json:"docker_bin,omitempty"`

	// KubectlBin is the binary the K8s driver shells out to.
	// Default "kubectl"; set to "oc" for OpenShift or a vendored path.
	KubectlBin string `yaml:"kubectl_bin,omitempty" json:"kubectl_bin,omitempty"`

	// CallbackURL overrides the parent URL workers dial for bootstrap.
	// Default: derived from Server.Host:Port. Use this when the parent
	// is reachable at a different address from inside containers than
	// the server's bind address (e.g. bind 0.0.0.0, workers dial
	// 198.51.100.10).
	CallbackURL string `yaml:"callback_url,omitempty" json:"callback_url,omitempty"`

	// BootstrapTokenTTLSeconds caps how long a bootstrap token stays
	// valid before the Manager's sweeper zeroes it out. Default 300.
	BootstrapTokenTTLSeconds int `yaml:"bootstrap_token_ttl_seconds,omitempty" json:"bootstrap_token_ttl_seconds,omitempty"`

	// WorkerBootstrapDeadlineSeconds is the total wall-clock budget the
	// worker has to complete its bootstrap call before exiting. Default
	// 60. Bump this on slow networks where docker bridge + parent
	// readiness can take longer to settle. Injected into the spawned
	// container as DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS.
	WorkerBootstrapDeadlineSeconds int `yaml:"worker_bootstrap_deadline_seconds,omitempty" json:"worker_bootstrap_deadline_seconds,omitempty"`

	// SecretsProvider (BL111) selects the secrets backend used to
	// resolve ClusterProfile.CredsRef. Defaults to "file" (single-host
	// dev). "env" reads from process environment. "k8s-secret",
	// "vault", and "csi" are stubbed for future hardening.
	SecretsProvider string `yaml:"secrets_provider,omitempty" json:"secrets_provider,omitempty"`

	// SecretsBaseDir (BL111) is the directory the file provider reads
	// from. Default <data_dir>/secrets. Ignored by the env / vault /
	// k8s-secret providers.
	SecretsBaseDir string `yaml:"secrets_base_dir,omitempty" json:"secrets_base_dir,omitempty"`

	// AuditPath (BL107 wire-up of S8.4) is the file path the agent
	// audit trail writes to. Empty disables agent auditing entirely.
	// Default: <data_dir>/audit/agents.jsonl. Format is JSON-lines;
	// CEF output is enabled per-deployment via AuditFormatCEF.
	AuditPath string `yaml:"audit_path,omitempty" json:"audit_path,omitempty"`

	// AuditFormatCEF, when true, writes the audit file in ArcSight
	// CEF format (single-line, syslog-friendly, every major SIEM
	// parses it). Default false → JSON-lines (jq-friendly + the
	// REST query handler can read it). Operators who need both can
	// run two FileAuditors via a custom main; the config-knob path
	// supports one or the other.
	AuditFormatCEF bool `yaml:"audit_format_cef,omitempty" json:"audit_format_cef,omitempty"`

	// IdleReaperIntervalSeconds (BL108) is the cadence of the idle-
	// reaper goroutine. Default 60. Set to 0 to disable the periodic
	// reaper entirely (workers still terminate via explicit operator
	// action). The reaper itself only acts on agents whose Project
	// Profile sets a non-zero idle_timeout.
	IdleReaperIntervalSeconds int `yaml:"idle_reaper_interval_seconds,omitempty" json:"idle_reaper_interval_seconds,omitempty"`

	// PQCBootstrap (BL95) opts spawned workers into the PQC envelope
	// bootstrap protocol (ML-KEM 768 + ML-DSA 65). Default false → the
	// legacy UUID token flow keeps working. When true the Manager
	// generates fresh KEM + signing keypairs at spawn time, retains
	// them on the in-memory Agent record, and the driver injects the
	// matching DATAWATCH_PQC_* env vars into the worker container.
	// ConsumeBootstrap accepts either a UUID (legacy) or an envelope
	// (PQC) based on which Agent record holds keys.
	PQCBootstrap bool `yaml:"pqc_bootstrap,omitempty" json:"pqc_bootstrap,omitempty"`
}

// ProxyConfig controls connection pooling, circuit breaker, and offline queuing
// for remote server communication. All fields are optional with sensible defaults.
type ProxyConfig struct {
	// Enabled activates proxy aggregation mode (default false).
	Enabled bool `yaml:"enabled,omitempty"`
	// HealthInterval is seconds between remote health checks (default 30).
	HealthInterval int `yaml:"health_interval,omitempty"`
	// RequestTimeout is seconds per remote request (default 10).
	RequestTimeout int `yaml:"request_timeout,omitempty"`
	// OfflineQueueSize is max queued commands per server (default 100).
	OfflineQueueSize int `yaml:"offline_queue_size,omitempty"`
	// CircuitBreakerThreshold is failures before marking server down (default 3).
	CircuitBreakerThreshold int `yaml:"circuit_breaker_threshold,omitempty"`
	// CircuitBreakerReset is seconds before retrying a downed server (default 30).
	CircuitBreakerReset int `yaml:"circuit_breaker_reset,omitempty"`
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
	OpenCode       OpenCodeConfig       `yaml:"opencode"`
	OpenCodeACP    OpenCodeACPConfig    `yaml:"opencode_acp"`
	OpenCodePrompt OpenCodePromptConfig `yaml:"opencode_prompt"`
	Shell     ShellBackendConfig `yaml:"shell_backend"`

	// DNSChannel holds DNS tunneling communication channel configuration.
	DNSChannel DNSChannelConfig `yaml:"dns_channel"`

	// Update controls automatic self-update behaviour.
	Update UpdateConfig `yaml:"update"`

	// Stats holds statistics collection configuration.
	Stats StatsConfig `yaml:"stats"`

	// RTK (Rust Token Killer) integration for token savings tracking.
	RTK RTKConfig `yaml:"rtk"`

	// Pipeline configuration for session chaining.
	Pipeline PipelineConfig `yaml:"pipeline"`

	// Whisper transcription for voice messages from messaging backends.
	Whisper WhisperConfig `yaml:"whisper"`

	// Named profiles for different accounts/API keys. Each profile overrides
	// the base backend config with custom env vars, binary, or model.
	Profiles map[string]ProfileConfig `yaml:"profiles,omitempty"`

	// Projects (BL27) — operator-registered named project directories.
	// Each entry is a quick alias for `new: @<project>: <task>` and the
	// REST start handler. Optional default_backend overrides
	// session.llm_backend for sessions launched into this project.
	Projects map[string]ProjectConfigEntry `yaml:"projects,omitempty"`

	// Templates (BL5) — named session-start bundles.
	Templates map[string]SessionTemplateEntry `yaml:"templates,omitempty"`

	// Servers is a list of remote datawatch instances to manage.
	// The implicit "local" entry (localhost:Server.Port) is always available.
	Servers []RemoteServerConfig `yaml:"servers,omitempty"`

	// Memory controls the episodic memory system — vector-indexed project knowledge.
	Memory MemoryConfig `yaml:"memory" json:"memory"`

	// Proxy controls connection pooling, circuit breaker, and offline queuing
	// for remote server communication.
	Proxy ProxyConfig `yaml:"proxy,omitempty"`

	// Agents controls the ephemeral container-spawned worker layer (F10).
	Agents AgentsConfig `yaml:"agents,omitempty" json:"agents,omitempty"`

	// Secrets (BL242) — centralized secrets manager. Backend defaults to "builtin".
	Secrets SecretsConfig `yaml:"secrets,omitempty" json:"secrets,omitempty"`

	// Tailscale (BL243) — k8s sidecar mesh networking.
	Tailscale TailscaleConfig `yaml:"tailscale,omitempty" json:"tailscale,omitempty"`

	// Autonomous (BL24+BL25, v3.10.0) — LLM-driven PRD → Stories →
	// Tasks decomposition with independent verification. Disabled by
	// default; opt-in via autonomous.enabled. All knobs are reachable
	// through every config channel per the parity rule.
	Autonomous AutonomousConfig `yaml:"autonomous,omitempty" json:"autonomous,omitempty"`

	// Plugins (BL33, v3.11.0) — subprocess plugin framework.
	// Disabled by default. See docs/api/plugins.md.
	Plugins PluginsConfig `yaml:"plugins,omitempty" json:"plugins,omitempty"`

	// Orchestrator (BL117, v4.0.0) — PRD-DAG orchestrator with
	// guardrail sub-agents (rules/security/release-readiness/docs).
	// Disabled by default. See docs/api/orchestrator.md.
	Orchestrator OrchestratorConfig `yaml:"orchestrator,omitempty" json:"orchestrator,omitempty"`

	// Observer (BL171, v4.1.0) — unified stats / process-tree /
	// sub-process monitoring. Ships in three shapes; this block
	// covers Shape A (in-process plugin). Enabled by default on
	// v4.1.0+. See docs/api/observer.md.
	Observer ObserverConfig `yaml:"observer,omitempty" json:"observer,omitempty"`

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

	// BL255 v6.7.0 — skill registries (PAI default + operator-added).
	Skills SkillsConfig `yaml:"skills,omitempty" json:"skills,omitempty"`
}

// SkillsConfig is the YAML side of the skills subsystem. Storage of
// registries themselves is in ~/.datawatch/skills.json (managed by
// internal/skills.Store); this config block lets operators seed
// registries via YAML in addition to the runtime CRUD surfaces.
//
// Fields are advisory: the runtime store is the source of truth; on
// daemon start we union the YAML registries with the on-disk store
// (existing names win) so YAML changes don't clobber operator edits.
type SkillsConfig struct {
	// Registries seeded from YAML. Each entry is upserted into the
	// runtime store on daemon start if a registry with that name is
	// not already present.
	Registries []SkillsRegistryConfig `yaml:"registries,omitempty" json:"registries,omitempty"`

	// AddDefaultOnStart, when true, calls AddDefault() on daemon start
	// to ensure the PAI registry is present (idempotent). Default false
	// — operator opts in once and the registry persists in the JSON store.
	AddDefaultOnStart bool `yaml:"add_default_on_start,omitempty" json:"add_default_on_start,omitempty"`

	// AutoIgnoreOnSessionStart, when true, calls EnsureSkillsIgnored on
	// every session start (parallel to BL219 GitignoreCheckOnStart).
	// Default true — drops `.datawatch/` into .gitignore so operator's
	// repo doesn't pick up injected skill files.
	AutoIgnoreOnSessionStart bool `yaml:"auto_ignore_on_session_start,omitempty" json:"auto_ignore_on_session_start,omitempty"`
}

// SkillsRegistryConfig is the YAML shape for a registry entry.
// `auth_secret_ref` MUST be a `${secret:name}` reference per the
// Secrets-Store Rule for any private repo.
type SkillsRegistryConfig struct {
	Name          string `yaml:"name" json:"name"`
	Kind          string `yaml:"kind,omitempty" json:"kind,omitempty"` // git
	URL           string `yaml:"url" json:"url"`
	Branch        string `yaml:"branch,omitempty" json:"branch,omitempty"`
	AuthSecretRef string `yaml:"auth_secret_ref,omitempty" json:"auth_secret_ref,omitempty"`
	Enabled       *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Description   string `yaml:"description,omitempty" json:"description,omitempty"`
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
	// PromptDebounce is the number of seconds to wait after detecting a prompt
	// before transitioning to waiting_input. During this window, if new output
	// arrives the timer resets. Prevents false positives during LLM thinking pauses.
	// Default: 3 seconds. Set to 0 to disable debouncing.
	PromptDebounce int `yaml:"prompt_debounce,omitempty"`
	// NotifyCooldown is the minimum seconds between repeated needs-input notifications
	// for the same session. Prevents notification floods. Default: 15 seconds.
	NotifyCooldown int `yaml:"notify_cooldown,omitempty"`
}

// ---- LLM backends ----

// OllamaConfig holds Ollama backend configuration.
type OllamaConfig struct {
	Enabled     bool            `yaml:"enabled"`
	Model       string          `yaml:"model"`
	Host        string          `yaml:"host"`
	Detection   DetectionConfig `yaml:"detection,omitempty"`
	ConsoleCols int             `yaml:"console_cols,omitempty"`
	ConsoleRows int             `yaml:"console_rows,omitempty"`
	OutputMode  string          `yaml:"output_mode,omitempty"` // "terminal" (default) or "log"
	InputMode   string          `yaml:"input_mode,omitempty"`  // "tmux" (default) or "none"
}

// OpenWebUIConfig holds OpenWebUI backend configuration.
type OpenWebUIConfig struct {
	Enabled     bool   `yaml:"enabled"`
	URL         string `yaml:"url"`
	Model       string `yaml:"model"`
	APIKey      string `yaml:"api_key"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
}

// AiderConfig holds aider LLM backend configuration.
type AiderConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
}

// GooseConfig holds goose LLM backend configuration.
type GooseConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
}

// GeminiConfig holds Gemini CLI LLM backend configuration.
type GeminiConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
}

// OpenCodeConfig holds opencode TUI backend configuration.
type OpenCodeConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
}

// OpenCodeACPConfig holds opencode ACP (headless server) backend configuration.
type OpenCodeACPConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Binary            string `yaml:"binary"`
	ACPStartupTimeout int    `yaml:"acp_startup_timeout"`
	ACPHealthInterval int    `yaml:"acp_health_interval"`
	ACPMessageTimeout int    `yaml:"acp_message_timeout"`
	ConsoleCols       int    `yaml:"console_cols,omitempty"`
	ConsoleRows       int    `yaml:"console_rows,omitempty"`
	OutputMode        string `yaml:"output_mode,omitempty"`
	InputMode         string `yaml:"input_mode,omitempty"`
}

// OpenCodePromptConfig holds opencode prompt-mode backend configuration.
type OpenCodePromptConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
}

// ShellBackendConfig holds shell script LLM backend configuration.
type ShellBackendConfig struct {
	Enabled     bool   `yaml:"enabled"`
	ScriptPath  string `yaml:"script_path"`
	ConsoleCols int    `yaml:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty"`
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

// SecretsConfig (BL242) — centralized secrets manager backend selection.
type SecretsConfig struct {
	// Backend selects the secrets store:
	//   "builtin"     — AES-256-GCM encrypted JSON (default)
	//   "keepass"     — KeePass database via keepassxc-cli (Phase 2)
	//   "onepassword" — 1Password vault via op CLI (Phase 3)
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`

	// KeePass backend (backend=keepass)
	KeePassDB       string `yaml:"keepass_db,omitempty" json:"keepass_db,omitempty"`
	KeePassPassword string `yaml:"keepass_password,omitempty" json:"keepass_password,omitempty"` // prefer DATAWATCH_KEEPASS_PASSWORD env
	KeePassBinary   string `yaml:"keepass_binary,omitempty" json:"keepass_binary,omitempty"`   // default: "keepassxc-cli"
	KeePassGroup    string `yaml:"keepass_group,omitempty" json:"keepass_group,omitempty"`     // optional group scope

	// 1Password backend (backend=onepassword)
	OPBinary string `yaml:"op_binary,omitempty" json:"op_binary,omitempty"` // default: "op"
	OPVault  string `yaml:"op_vault,omitempty" json:"op_vault,omitempty"`   // optional vault name/ID
	OPToken  string `yaml:"op_token,omitempty" json:"op_token,omitempty"`   // prefer DATAWATCH_OP_TOKEN env
}

// TailscaleConfig (BL243) — Tailscale k8s sidecar configuration.
// Fields that accept ${secret:name} references (AuthKey, APIKey) are
// resolved by secrets.ResolveConfig at daemon startup.
type TailscaleConfig struct {
	Enabled        bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	CoordinatorURL string   `yaml:"coordinator_url,omitempty" json:"coordinator_url,omitempty"`
	AuthKey        string   `yaml:"auth_key,omitempty" json:"auth_key,omitempty"`
	APIKey         string   `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Image          string   `yaml:"image,omitempty" json:"image,omitempty"`
	Tags           []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	ACL            struct {
		AllowedPeers []string `yaml:"allowed_peers,omitempty" json:"allowed_peers,omitempty"`
		ManagedTags  []string `yaml:"managed_tags,omitempty" json:"managed_tags,omitempty"`
	} `yaml:"acl,omitempty" json:"acl,omitempty"`
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

	// PublicURL is the externally-reachable URL of this datawatch
	// instance. Used by F10 sprint 4 K8s workers to call home to
	// the parent — Pods inside a cluster can rarely reach the
	// parent's bind address (0.0.0.0) directly. Examples:
	//   "https://datawatch.example.com"   (load balancer / Ingress)
	//   "http://198.51.100.10:8080"        (LAN address of dev box)
	// Resolution priority for the worker callback URL:
	//   1. ClusterProfile.ParentCallbackURL  (per-cluster override)
	//   2. AgentsConfig.CallbackURL          (operator-explicit, agents-only)
	//   3. Server.PublicURL                  (this field — global)
	//   4. http://<bind-host>:<port>         (best-effort fallback)
	PublicURL string `yaml:"public_url,omitempty"`

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

	// TLSPort is the HTTPS port. When TLS is enabled, HTTP on the main port
	// redirects to HTTPS on TLSPort. Default: 8443.
	TLSPort int `yaml:"tls_port"`

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

	// AllowSelfConfig (BL110) gates the `config_set` MCP tool. Default
	// false → AI sessions calling `config_set` get a permission-denied
	// response. Set true (per the BL110 vision: "datawatch will run
	// itself one day") to let an in-process AI mutate its own config.
	// Every approved mutation is logged to the daemon's stderr +
	// audit/config-self-modify.jsonl so an operator can review what
	// the AI changed. The setting itself can NOT be flipped via
	// config_set — bootstrap protection.
	AllowSelfConfig bool `yaml:"allow_self_config,omitempty"`

	// SelfConfigAuditPath (BL110) is the file the self-modify audit
	// stream writes to. Default <data_dir>/audit/config-self-modify.jsonl.
	// Empty disables the file sink (events still go to stderr).
	SelfConfigAuditPath string `yaml:"self_config_audit_path,omitempty"`
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

	// AlertContextLines is the number of non-empty output lines included
	// in prompt alerts sent to messaging channels. Default: 10.
	AlertContextLines int `yaml:"alert_context_lines"`

	// ClaudeEnabled controls whether claude-code backend is available.
	ClaudeEnabled bool `yaml:"claude_enabled"`

	// ClaudeBin is the path to the claude binary (claude-code backend only).
	ClaudeBin string `yaml:"claude_code_bin"`

	// LLMBackend selects which LLM backend to use. Default: "claude-code".
	LLMBackend string `yaml:"llm_backend"`

	// DefaultProjectDir is the working directory for sessions started via messaging
	// backends when no explicit directory is given. Defaults to the user's home directory.
	DefaultProjectDir string `yaml:"default_project_dir"`

	// WorkspaceRoot is the base directory under which session project_dirs are
	// resolved when relative. Set to "/workspace" in containers (NFS-mounted
	// or PVC); leave empty on bare metal to keep absolute paths as-is.
	// Used by the F10 ephemeral-agent containers so the same project_dir
	// string ("./datawatch") works on host and in the container.
	WorkspaceRoot string `yaml:"workspace_root"`

	// (resolver lives at the bottom of this struct, see ResolveProjectDir)

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

	// ClaudeAutoAcceptDisclaimer (v5.27.2) — when true, datawatch detects
	// claude-code's startup disclaimer / "trust this folder" / "Loading
	// development channels" prompts and auto-accepts them by sending the
	// confirmation key (Enter for trust prompts; "1\n" for numbered menus).
	// Mirrors the PRD-automation pattern: the prompt is recognised, marked
	// as `waiting_input`, then a brief debounce later the auto-response is
	// sent. Defaults to false so the operator opts in explicitly. Only
	// applies to the claude-code backend.
	ClaudeAutoAcceptDisclaimer bool `yaml:"claude_auto_accept_disclaimer"`

	// PermissionMode (v5.27.5) — passed to claude-code as
	// `--permission-mode <value>`. Valid values: "default", "plan",
	// "acceptEdits", "auto", "bypassPermissions", "dontAsk". Empty
	// (default) lets claude pick its own default. The "plan" mode is
	// the design-without-writing flavour useful for PRD decomposition
	// + design-review sessions. Overridden per-task / per-PRD via
	// the autonomous executor (Task.PermissionMode → PRD.PermissionMode
	// → this global). Only applies to the claude-code backend.
	PermissionMode string `yaml:"permission_mode"`

	// SecureTracking controls tracker file encryption when --secure is enabled.
	// "log_only" (default) encrypts only output.log; "full" also encrypts tracker .md files.
	SecureTracking string `yaml:"secure_tracking"`

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

	// ReconcileOnStartup (BL93) — when true the daemon scans
	// <data_dir>/sessions/<id>/session.json at startup and imports
	// any session not already in the registry. Default: false (dry-
	// run via REST/MCP/CLI is the safe default — operators opt in to
	// auto-import once they trust the orphan list).
	ReconcileOnStartup bool `yaml:"reconcile_on_startup"`

	// ConsoleCols and ConsoleRows set the default tmux terminal size for new sessions.
	// Per-LLM overrides take priority. Default: 80x24.
	ConsoleCols int `yaml:"console_cols"`
	ConsoleRows int `yaml:"console_rows"`

	// FallbackChain is an ordered list of profile names to try when the primary
	// backend hits a rate limit. Each entry must match a key in the top-level
	// profiles map. Empty = no fallback (default: pause and auto-resume).
	FallbackChain []string `yaml:"fallback_chain,omitempty"`

	// ScheduleSettleMs (B30) is the delay in milliseconds between sending
	// text and pressing Enter for scheduled commands. Fixes the 2nd-Enter
	// bug for TUIs that start accepting input slightly after their prompt
	// state transition fires. 0 disables (legacy single-call send-keys).
	// Default: 200.
	ScheduleSettleMs int `yaml:"schedule_settle_ms"`

	// DefaultEffort (BL41) is the per-session effort hint applied when
	// the operator doesn't pass one explicitly. One of "quick", "normal",
	// "thorough". Empty = "normal".
	DefaultEffort string `yaml:"default_effort,omitempty"`

	// StaleTimeoutSeconds (BL40) — sessions in StateRunning whose
	// UpdatedAt is older than this are reported as stale via
	// `GET /api/sessions/stale`. Default 1800 (30 min). 0 disables.
	StaleTimeoutSeconds int `yaml:"stale_timeout_seconds,omitempty"`

	// RateLimitGlobalPause (BL30) — when true, hitting a rate limit
	// on any backend pauses new session creation across all backends
	// until the cooldown elapses or the operator clears it.
	RateLimitGlobalPause bool `yaml:"rate_limit_global_pause,omitempty"`

	// CostRates (BL6) — operator-overridable per-backend USD rates
	// (per 1K tokens). Empty = use built-in DefaultCostRates. Each
	// entry is {in_per_k, out_per_k}.
	CostRates map[string]CostRateConfig `yaml:"cost_rates,omitempty"`

	// SplashLogoPath (BL69) — absolute path to a custom logo (PNG/SVG)
	// served at GET /api/splash/logo. Empty = use built-in favicon.svg.
	SplashLogoPath string `yaml:"splash_logo_path,omitempty"`

	// SplashTagline (BL69) — optional tagline rendered under the logo
	// on the splash screen.
	SplashTagline string `yaml:"splash_tagline,omitempty"`

	// AssistantBackend (BL42) — which backend the `assist:` command +
	// /api/assist endpoint use. Empty = "ollama".
	AssistantBackend string `yaml:"assistant_backend,omitempty"`

	// AssistantModel (BL42) — model override for the assistant.
	AssistantModel string `yaml:"assistant_model,omitempty"`

	// AssistantSystemPrompt (BL42) — optional system prompt prepended
	// to every assist call. Empty disables.
	AssistantSystemPrompt string `yaml:"assistant_system_prompt,omitempty"`

	// DeviceAliases (BL31) — map of operator-friendly aliases
	// ("prod", "dev") to remote-server names defined in `servers:`.
	// Used by router for `new: @<alias>: <task>` routing.
	DeviceAliases map[string]string `yaml:"device_aliases,omitempty"`

	// AlertsRichFormat (BL15) — when true, alert text containing
	// triple-backtick code blocks is sent with backend-specific
	// rich formatting (Telegram MarkdownV2, Slack mrkdwn, Signal
	// monospace). Default false (plain text).
	AlertsRichFormat bool `yaml:"alerts_rich_format,omitempty"`

	// RoutingRules (BL20) — ordered list of pattern→backend rules
	// applied to the task text on session start. First match wins;
	// no match falls through to req.Backend / session.llm_backend.
	// Each pattern is a Go regexp matched against the task text.
	RoutingRules []RoutingRule `yaml:"routing_rules,omitempty"`

	// QuickCommands (BL209, v5.27.7 — datawatch#28) — operator-editable
	// list of "system" quick commands surfaced in the PWA Quick
	// Commands panel + the Android bottom-sheet. Pre-v5.27.7 every
	// client hardcoded `yes` / `no` / `continue` / `skip` / `/exit` /
	// Esc / Ctrl-b / arrows / PgUp / PgDn / Tab / Enter — operators
	// who wanted to add a project-specific button or remove the
	// `/exit` shortcut had to wait for a client release. v5.27.7
	// serves the list from the daemon so a `datawatch config set
	// session.quick_commands` (or YAML edit + reload) takes effect
	// across every client immediately. Empty (default) → server
	// returns the v5.27.6-and-earlier hardcoded baseline.
	QuickCommands []QuickCommand `yaml:"quick_commands,omitempty"`

	// CleanupArtifactsOnEnd (BL219) — when true, removes ephemeral backend
	// files (aider cache dirs, goose session JSONL, etc.) from the project
	// directory after each session ends. Default: false (keep artifacts).
	CleanupArtifactsOnEnd bool `yaml:"cleanup_artifacts_on_end,omitempty"`

	// GitignoreArtifacts (BL219) — list of backend names whose artifact
	// patterns are appended to .gitignore (and .cfignore/.dockerignore if
	// present) on session start. Default: ["aider","goose","gemini"].
	// Set to [] to disable all ignore-file management.
	GitignoreArtifacts []string `yaml:"gitignore_artifacts,omitempty"`

	// GitignoreCheckOnStart (BL219) — when true, verifies and updates
	// ignore files on every session start. Default: true.
	GitignoreCheckOnStart bool `yaml:"gitignore_check_on_start,omitempty"`
}

// RoutingRule (BL20) — one entry in session.routing_rules.
type RoutingRule struct {
	Pattern     string `yaml:"pattern" json:"pattern"`
	Backend     string `yaml:"backend" json:"backend"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// QuickCommand (BL209, v5.27.7) — one entry in session.quick_commands.
// `label` is what the client renders on the button; `value` is what
// gets sent to the active session — either a literal text payload
// (e.g. "yes\n") or a tmux key shortcut prefixed with `key:` (e.g.
// `key:Up`, `key:C-b`, `key:Escape`). The client decides which
// transport to use based on the prefix. `category` is an optional
// grouping hint (`system` / `project` / etc.) so clients can
// section the panel.
type QuickCommand struct {
	Label    string `yaml:"label" json:"label"`
	Value    string `yaml:"value" json:"value"`
	Category string `yaml:"category,omitempty" json:"category,omitempty"`
}

// CostRateConfig (BL6) — YAML/JSON view of a per-backend rate.
type CostRateConfig struct {
	InPerK  float64 `yaml:"in_per_k" json:"in_per_k"`
	OutPerK float64 `yaml:"out_per_k" json:"out_per_k"`
}

// AutonomousConfig (BL24+BL25) — operator knobs for LLM-driven PRD
// decomposition. Mirrors internal/autonomous.Config; copied here so
// YAML loading + REST /api/config exposure don't pull in the package.
type AutonomousConfig struct {
	// Enabled gates the background loop. Off by default.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// PollIntervalSeconds is the loop tick (default 30).
	PollIntervalSeconds int `yaml:"poll_interval_seconds,omitempty" json:"poll_interval_seconds,omitempty"`
	// MaxParallelTasks is the per-PRD in-flight worker cap (default 3).
	MaxParallelTasks int `yaml:"max_parallel_tasks,omitempty" json:"max_parallel_tasks,omitempty"`
	// DecompositionBackend overrides session.llm_backend for the LLM
	// decomposition call. Empty = inherit.
	DecompositionBackend string `yaml:"decomposition_backend,omitempty" json:"decomposition_backend,omitempty"`
	// VerificationBackend selects the BL25 verifier backend. Empty =
	// inherit. For cross-backend independence, set this to a distinct
	// backend (e.g. ollama when decomposition is claude-code).
	VerificationBackend string `yaml:"verification_backend,omitempty" json:"verification_backend,omitempty"`
	// DecompositionEffort is the BL41 effort hint for the LLM call.
	DecompositionEffort string `yaml:"decomposition_effort,omitempty" json:"decomposition_effort,omitempty"`
	// VerificationEffort is the BL41 effort hint for the verifier.
	VerificationEffort string `yaml:"verification_effort,omitempty" json:"verification_effort,omitempty"`
	// v5.26.16 — operator-reported: decomposition + verification need
	// optional per-backend Model overrides like the per-PRD/per-task
	// LLM dropdowns. Empty = backend default.
	DecompositionModel string `yaml:"decomposition_model,omitempty" json:"decomposition_model,omitempty"`
	VerificationModel  string `yaml:"verification_model,omitempty" json:"verification_model,omitempty"`
	// StaleTaskSeconds — 0 inherits session.stale_timeout_seconds.
	StaleTaskSeconds int `yaml:"stale_task_seconds,omitempty" json:"stale_task_seconds,omitempty"`
	// AutoFixRetries — how many times to re-prompt on verifier failure.
	AutoFixRetries int `yaml:"auto_fix_retries,omitempty" json:"auto_fix_retries,omitempty"`
	// SecurityScan — when true, run the nightwire-port pattern scan
	// over modified files before marking a task complete.
	SecurityScan bool `yaml:"security_scan,omitempty" json:"security_scan,omitempty"`

	// BL191 Q4 (v5.9.0) — recursive child-PRDs. MaxRecursionDepth
	// caps the parent→child chain length; 0 disables recursion.
	// AutoApproveChildren skips operator review on spawned children
	// (otherwise every level hangs on /approve).
	MaxRecursionDepth   int  `yaml:"max_recursion_depth,omitempty" json:"max_recursion_depth,omitempty"`
	AutoApproveChildren bool `yaml:"auto_approve_children,omitempty" json:"auto_approve_children,omitempty"`

	// BL191 Q6 (v5.10.0) — guardrails at story + task level. Empty
	// list = disabled at that level. Names match the BL117 orchestrator:
	// rules, security, release-readiness, docs-diagrams-architecture.
	PerTaskGuardrails  []string `yaml:"per_task_guardrails,omitempty" json:"per_task_guardrails,omitempty"`
	PerStoryGuardrails []string `yaml:"per_story_guardrails,omitempty" json:"per_story_guardrails,omitempty"`

	// Phase 3 (v5.26.61) — per-story approval gate. Default false
	// (preserves the v5.26.x behavior where PRD approval auto-
	// approves every story). When true, PRD approval transitions
	// stories to "awaiting_approval"; the runner skips those until
	// the operator calls POST /api/autonomous/prds/{id}/approve_story
	// per story.
	PerStoryApproval bool `yaml:"per_story_approval,omitempty" json:"per_story_approval,omitempty"`
}

// OrchestratorConfig (BL117) — mirrors internal/orchestrator.Config;
// copied here so YAML loading + /api/config exposure don't pull in
// the package.
type OrchestratorConfig struct {
	// Enabled gates the orchestrator REST surface + runner.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// DefaultGuardrails is the ordered guardrail set applied to each
	// PRD when a graph doesn't override.
	DefaultGuardrails []string `yaml:"default_guardrails,omitempty" json:"default_guardrails,omitempty"`
	// GuardrailTimeoutMs caps each guardrail invocation (default 120000 = 2 min).
	GuardrailTimeoutMs int `yaml:"guardrail_timeout_ms,omitempty" json:"guardrail_timeout_ms,omitempty"`
	// GuardrailBackend selects the LLM backend the guardrail workers
	// run under. Empty = inherit session.llm_backend.
	GuardrailBackend string `yaml:"guardrail_backend,omitempty" json:"guardrail_backend,omitempty"`
	// v5.26.16 — optional per-backend Model override for guardrail
	// workers. Empty = backend default.
	GuardrailModel string `yaml:"guardrail_model,omitempty" json:"guardrail_model,omitempty"`
	// MaxParallelPRDs caps per-graph PRD parallelism (default 2).
	MaxParallelPRDs int `yaml:"max_parallel_prds,omitempty" json:"max_parallel_prds,omitempty"`
}

// ObserverConfig (BL171, v4.1.0) — mirrors
// internal/observer.Config; kept here so YAML loading + /api/config
// exposure stay independent of the implementation package. A zero
// value still collects — the ObserverConfig defaults are applied
// inside the daemon when the block is absent or empty.
type ObserverConfig struct {
	// PluginEnabled toggles Shape A. When false the observer REST
	// surface returns 503 and /api/stats falls back to the v1
	// statsCollector. Default: true.
	PluginEnabled *bool `yaml:"plugin_enabled,omitempty" json:"plugin_enabled,omitempty"`
	// TickIntervalMs between successive collections (default 1000).
	TickIntervalMs int `yaml:"tick_interval_ms,omitempty" json:"tick_interval_ms,omitempty"`
	// ProcessTreeEnabled toggles /proc walking. Set false on very
	// resource-constrained hosts; CPU/mem/disk/gpu host numbers
	// still report. Default: true.
	ProcessTreeEnabled *bool `yaml:"process_tree_enabled,omitempty" json:"process_tree_enabled,omitempty"`
	// TopNBroadcast caps how many top-CPU processes appear in the
	// WS broadcast tree per tick (default 200). Drill-downs via
	// /api/observer/envelope are uncapped.
	TopNBroadcast int `yaml:"top_n_broadcast,omitempty" json:"top_n_broadcast,omitempty"`
	// IncludeKthreads pulls kernel threads into the tree (default
	// false; noisy and rarely useful).
	IncludeKthreads bool `yaml:"include_kthreads,omitempty" json:"include_kthreads,omitempty"`
	// SessionAttribution groups processes under each tmux-pane
	// PID into a session envelope (default true).
	SessionAttribution *bool `yaml:"session_attribution,omitempty" json:"session_attribution,omitempty"`
	// BackendAttribution groups processes matching a known LLM
	// backend signature into a backend envelope (default true).
	BackendAttribution *bool `yaml:"backend_attribution,omitempty" json:"backend_attribution,omitempty"`
	// DockerDiscovery correlates processes to docker containers
	// via /proc/<pid>/cgroup (default true).
	DockerDiscovery *bool `yaml:"docker_discovery,omitempty" json:"docker_discovery,omitempty"`
	// GPUAttribution annotates envelopes with nvidia-smi per-process
	// GPU utilisation where available (default true).
	GPUAttribution *bool `yaml:"gpu_attribution,omitempty" json:"gpu_attribution,omitempty"`
	// EBPFEnabled controls per-process net eBPF capture on all three
	// shapes. "auto" (default) loads the programs when CAP_BPF is
	// present and silently degrades to /proc-only when not; "true"
	// fails the daemon boot if the kernel refuses the program;
	// "false" never attempts load. Shape C always has CAP_BPF;
	// Shape A + B light up when operator runs datawatch with ambient
	// CAP_BPF (systemd `AmbientCapabilities=CAP_BPF CAP_PERFMON`).
	// Note: YAML may represent as quoted string "true" or unquoted true;
	// this field normalizes both to the lowercase string form on load.
	EBPFEnabled string `yaml:"ebpf_enabled,omitempty" json:"ebpf_enabled,omitempty"`

	// Federation (S14a, v4.8.0) — turns this primary into a peer
	// of another root primary so operators with multiple clusters
	// see one pane of glass. Empty parent_url disables federation.
	Federation FederationConfig `yaml:"federation,omitempty" json:"federation,omitempty"`

	// OllamaTap (BL180 Phase 1, v4.9.1) — when Endpoint is set,
	// the observer polls ollama's /api/ps every 5 s and emits one
	// envelope per loaded model. Empty disables. Useful for
	// per-model GPU/RAM attribution on hosts that share one
	// ollama between multiple clients.
	OllamaTap ObserverOllamaTapConfig `yaml:"ollama_tap,omitempty" json:"ollama_tap,omitempty"`

	// ConnCorrelator (BL293, v5.6.1; bridge added v5.21.0) — opt-in
	// BL180 Phase 2 procfs userspace correlator. Disabled by default
	// after v5.6.0 OOM fallout. Pre-v5.21.0 this field existed in
	// internal/observer.Config but the operator-facing surface
	// (YAML + PUT /api/config + PWA) couldn't set it because it
	// wasn't bridged through internal/config.ObserverConfig.
	ConnCorrelator bool `yaml:"conn_correlator,omitempty" json:"conn_correlator,omitempty"`

	// Peers (BL172, v4.5.0; bridge added v5.21.0) — federated peer
	// registry knobs.
	Peers ObserverPeersConfig `yaml:"peers,omitempty" json:"peers,omitempty"`
}

// ObserverPeersConfig (v5.21.0 bridge) mirrors observer.PeersCfg.
type ObserverPeersConfig struct {
	// AllowRegister gates Shape B/C peers' POST /api/observer/peers.
	// When false the daemon refuses new peer registrations.
	AllowRegister bool `yaml:"allow_register,omitempty" json:"allow_register,omitempty"`
	// TokenRotationGraceS is the overlap window during peer-token
	// rotation; default 60.
	TokenRotationGraceS int `yaml:"token_ttl_rotation_grace_s,omitempty" json:"token_ttl_rotation_grace_s,omitempty"`
	// PushIntervalSeconds expected from peers; default 5.
	PushIntervalSeconds int `yaml:"push_interval_seconds,omitempty" json:"push_interval_seconds,omitempty"`
	// ListenAddr is the optional sidecar listen address Shape B peers
	// expose (e.g. ":9001").
	ListenAddr string `yaml:"listen_addr,omitempty" json:"listen_addr,omitempty"`
}

// ObserverOllamaTapConfig — BL180 Phase 1 — mirrors observer.OllamaTapCfg.
type ObserverOllamaTapConfig struct {
	// Endpoint is the ollama base URL (e.g. "http://localhost:11434").
	// Empty disables the tap.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
}

// FederationConfig (S14a) — cross-cluster federation push-out.
// Mirrors observer.FederationCfg; copied here so YAML/REST/MCP
// surfaces don't pull in the package.
type FederationConfig struct {
	// ParentURL is the root primary base URL (e.g.
	// "https://datawatch-root:8443"). Empty disables federation.
	ParentURL string `yaml:"parent_url,omitempty" json:"parent_url,omitempty"`
	// PeerName the local primary registers as on the root.
	// Defaults to the host name when empty.
	PeerName string `yaml:"peer_name,omitempty" json:"peer_name,omitempty"`
	// PushIntervalSeconds between federation pushes (default 10).
	PushIntervalSeconds int `yaml:"push_interval_seconds,omitempty" json:"push_interval_seconds,omitempty"`
	// TokenPath persists the registration token across restarts
	// (default <data_dir>/observer/federation.token).
	TokenPath string `yaml:"token_path,omitempty" json:"token_path,omitempty"`
	// Insecure skips TLS verify on the parent (dev / self-signed).
	Insecure bool `yaml:"insecure,omitempty" json:"insecure,omitempty"`
}

// PluginsConfig (BL33) — subprocess plugin framework. Mirrors
// internal/plugins.Config; copied here so YAML loading + REST
// /api/config exposure don't pull in the package.
type PluginsConfig struct {
	// Enabled gates plugin discovery + invocation. Off by default.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Dir is the discovery root. Defaults to <data_dir>/plugins.
	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`
	// TimeoutMs is the per-invocation wall-clock budget (default 2000).
	TimeoutMs int `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	// Disabled is the list of plugin names to skip at discovery.
	Disabled []string `yaml:"disabled,omitempty" json:"disabled,omitempty"`
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

// StatsConfig holds statistics collection configuration.
type StatsConfig struct {
	// EBPFEnabled enables per-session eBPF network/CPU tracing.
	// Requires CAP_BPF on the binary. Only configurable via CLI (datawatch setup ebpf).
	// NOT exposed in web UI or messaging for security.
	EBPFEnabled bool `yaml:"ebpf_enabled"`
}

// ProfileConfig defines a named backend profile with optional overrides.
// Profiles allow multiple accounts/API keys for the same backend type.
type ProfileConfig struct {
	Backend string            `yaml:"backend" json:"backend"`               // base backend name (e.g. "claude-code")
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`   // env var overrides (ANTHROPIC_API_KEY, etc.)
	Binary  string            `yaml:"binary,omitempty" json:"binary,omitempty"` // override binary path
	Model   string            `yaml:"model,omitempty" json:"model,omitempty"`   // override model name
}

// RTKConfig configures the RTK (Rust Token Killer) integration for token savings.
type RTKConfig struct {
	Enabled            bool   `yaml:"enabled"`              // enable RTK integration
	Binary             string `yaml:"binary"`               // path to rtk binary (default: "rtk")
	ShowSavings        bool   `yaml:"show_savings"`         // display token savings in stats dashboard
	AutoInit           bool   `yaml:"auto_init"`            // run 'rtk init -g' if hooks not installed
	DiscoverInterval   int    `yaml:"discover_interval"`    // seconds between discover checks (0 = disabled)
	AutoUpdate         bool   `yaml:"auto_update"`          // auto-update RTK binary when new version available
	UpdateCheckInterval int   `yaml:"update_check_interval"` // seconds between version checks (default: 86400 = daily, 0 = disabled)
}

// ProjectConfigEntry (BL27) — registered project directory alias.
type ProjectConfigEntry struct {
	Dir            string `yaml:"dir" json:"dir"`
	DefaultBackend string `yaml:"default_backend,omitempty" json:"default_backend,omitempty"`
	Description    string `yaml:"description,omitempty" json:"description,omitempty"`
}

// SessionTemplateEntry (BL5) — reusable bundle of session start params.
// Empty fields fall through to the operator's defaults.
type SessionTemplateEntry struct {
	ProjectDir     string            `yaml:"project_dir,omitempty" json:"project_dir,omitempty"`
	Backend        string            `yaml:"backend,omitempty" json:"backend,omitempty"`
	Profile        string            `yaml:"profile,omitempty" json:"profile,omitempty"`
	Effort         string            `yaml:"effort,omitempty" json:"effort,omitempty"` // BL41
	Env            map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	AutoGitCommit  *bool             `yaml:"auto_git_commit,omitempty" json:"auto_git_commit,omitempty"`
	AutoGitInit    *bool             `yaml:"auto_git_init,omitempty" json:"auto_git_init,omitempty"`
	Description    string            `yaml:"description,omitempty" json:"description,omitempty"`
}

// PipelineConfig configures session chaining (pipeline DAG executor).
type PipelineConfig struct {
	// MaxParallel is the max number of tasks running simultaneously (default: 3)
	MaxParallel int `yaml:"max_parallel"`
	// DefaultBackend overrides session.llm_backend for pipeline tasks (empty = use session default)
	DefaultBackend string `yaml:"default_backend,omitempty"`

	// QualityGates (BL28) — run test baseline before a task and
	// compare with a post-task run. Block completion on regression
	// when BlockOnRegression is true.
	QualityGates QualityGateConfig `yaml:"quality_gates,omitempty"`
}

// QualityGateConfig — BL28 runtime config. Mirrors pipeline.QualityGateConfig
// but lives in the top-level config struct for YAML round-trip without an
// import cycle.
type QualityGateConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`
	TestCommand       string `yaml:"test_command" json:"test_command"`
	Timeout           int    `yaml:"timeout" json:"timeout"`
	BlockOnRegression bool   `yaml:"block_on_regression" json:"block_on_regression"`
}

// WhisperConfig configures voice-to-text transcription. Three
// backend choices (BL189):
//
//   - "" / "whisper" (default) — local OpenAI-Whisper Python venv
//     (requires `pip install openai-whisper` in the venv).
//   - "openai" or "openai_compat" — POST to an OpenAI-compatible
//     `/v1/audio/transcriptions` endpoint. Works with the cloud
//     OpenAI API, OpenWebUI, faster-whisper-server, whisper.cpp's
//     server mode, and any other compatible host.
//
// Ollama doesn't ship native audio support — operators wanting an
// "ollama-flavoured" path point this at the OpenWebUI instance
// fronting their ollama (since OpenWebUI exposes the audio API).
type WhisperConfig struct {
	// Enabled controls whether voice message transcription is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Backend selects the transcribe path. "" or "whisper" = local
	// venv; "openai" / "openai_compat" = HTTP. Default: "whisper".
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`

	// Model — for `whisper` backend, the model size (tiny / base /
	// small / medium / large). For `openai` backend, the OpenAI model
	// name (whisper-1 by default).
	Model string `yaml:"model" json:"model"`

	// Language is the ISO 639-1 code for the expected spoken language
	// ("en", "es", "de", "ja", …). "" or "auto" enables detection.
	// See https://github.com/openai/whisper#available-models-and-languages
	Language string `yaml:"language" json:"language"`

	// VenvPath is the path to the Python virtualenv containing the
	// whisper CLI. Used only by the `whisper` backend. Default:
	// ".venv" (relative to datawatch working directory).
	VenvPath string `yaml:"venv_path" json:"venv_path"`

	// Endpoint is the base URL for the `openai` / `openai_compat`
	// backend (e.g. "https://api.openai.com/v1" or
	// "http://localhost:3000/api"). The full transcribe URL is
	// `<endpoint>/audio/transcriptions`. Required when `backend`
	// is `openai` / `openai_compat`.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`

	// APIKey is the bearer credential for the HTTP backend. Optional
	// for self-hosted OpenWebUI / whisper.cpp; required for the
	// upstream OpenAI API.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
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
				AlertContextLines:     10,
			ClaudeBin:             "claude",
			LLMBackend:            "claude-code",
			DefaultProjectDir:     home,
			AutoGitCommit:         true,
			AutoGitInit:           false,
			ClaudeEnabled:         true,
			ClaudeChannelEnabled:  true,
			ClaudeSkipPermissions: true,
			MCPMaxRetries:        5,
			ScheduleSettleMs:     200,
			DefaultEffort:        "normal",
			StaleTimeoutSeconds:  1800,
			// BL219 — tooling lifecycle defaults.
			GitignoreCheckOnStart: true,
			GitignoreArtifacts:    []string{"aider", "goose", "gemini"},
		},
		Server: ServerConfig{
			Enabled:              true,
			Host:                 "0.0.0.0",
			Port:                 8080,
			TLSPort:              8443,
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
		Whisper: WhisperConfig{
			Model:    "base",
			Language: "en",
			VenvPath: ".venv",
		},
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
	case "opencode":
		cols, rows = c.OpenCode.ConsoleCols, c.OpenCode.ConsoleRows
	case "opencode-acp":
		cols, rows = c.OpenCodeACP.ConsoleCols, c.OpenCodeACP.ConsoleRows
	case "opencode-prompt":
		cols, rows = c.OpenCodePrompt.ConsoleCols, c.OpenCodePrompt.ConsoleRows
	case "openwebui":
		cols, rows = c.OpenWebUI.ConsoleCols, c.OpenWebUI.ConsoleRows
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

// GetOutputMode returns the output display mode for a given LLM backend.
// "terminal" = show tmux capture-pane (interactive TUI apps, default)
// "log" = show output.log content (headless/ACP/prompt mode)
func (c *Config) GetOutputMode(backend string) string {
	var mode string
	switch backend {
	case "opencode":
		mode = c.OpenCode.OutputMode
	case "opencode-acp":
		mode = c.OpenCodeACP.OutputMode
		if mode == "" {
			return "chat" // ACP defaults to chat UI (BL83)
		}
	case "opencode-prompt":
		mode = c.OpenCodePrompt.OutputMode
	case "ollama":
		mode = c.Ollama.OutputMode
		if mode == "" {
			return "chat" // Ollama defaults to chat UI (BL77)
		}
	case "openwebui":
		mode = c.OpenWebUI.OutputMode
		if mode == "" {
			return "chat" // OpenWebUI defaults to chat UI
		}
	case "aider":
		mode = c.Aider.OutputMode
	case "goose":
		mode = c.Goose.OutputMode
	case "gemini":
		mode = c.Gemini.OutputMode
	case "shell":
		mode = c.Shell.OutputMode
	}
	if mode != "" {
		return mode
	}
	return "terminal"
}

// GetInputMode returns the input mode for a given LLM backend.
// "tmux" = send-keys works, show input bar (default)
// "none" = TUI handles its own input, hide input bar
func (c *Config) GetInputMode(backend string) string {
	var mode string
	switch backend {
	case "opencode":
		mode = c.OpenCode.InputMode
	case "opencode-acp":
		mode = c.OpenCodeACP.InputMode
	case "opencode-prompt":
		mode = c.OpenCodePrompt.InputMode
	case "ollama":
		mode = c.Ollama.InputMode
	case "openwebui":
		mode = c.OpenWebUI.InputMode
	case "aider":
		mode = c.Aider.InputMode
	case "goose":
		mode = c.Goose.InputMode
	case "gemini":
		mode = c.Gemini.InputMode
	case "shell":
		mode = c.Shell.InputMode
	}
	if mode != "" {
		return mode
	}
	return "tmux"
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
			// claude-code prompt (Unicode)
			"❯",
			// datawatch shell prompt
			"datawatch:",
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
	// Apply debounce/cooldown defaults (per-LLM overrides take precedence)
	if llmDet.PromptDebounce > 0 {
		base.PromptDebounce = llmDet.PromptDebounce
	}
	if llmDet.NotifyCooldown > 0 {
		base.NotifyCooldown = llmDet.NotifyCooldown
	}
	// Defaults if still zero
	if base.PromptDebounce == 0 {
		base.PromptDebounce = 3
	}
	if base.NotifyCooldown == 0 {
		base.NotifyCooldown = 15
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
	if cfg.Session.AlertContextLines == 0 {
		cfg.Session.AlertContextLines = 10
	}
	if cfg.Session.ScheduleSettleMs == 0 {
		cfg.Session.ScheduleSettleMs = 200
	}
	if cfg.Session.DefaultEffort == "" {
		cfg.Session.DefaultEffort = "normal"
	}
	if cfg.Session.StaleTimeoutSeconds == 0 {
		cfg.Session.StaleTimeoutSeconds = 1800
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
	if cfg.Session.LogLevel == "" {
		cfg.Session.LogLevel = "info"
	}
	if cfg.Session.RootPath == "" {
		if wd, err := os.Getwd(); err == nil {
			cfg.Session.RootPath = wd
		}
	}
}

// normalizeBooleanFields fixes YAML representation inconsistencies where
// boolean-like strings are quoted (e.g. "true" instead of true). This happens
// when a field is defined as string but written from a boolean value.
// Normalizes observer.ebpf_enabled and similar fields. v5.28.5 (datawatch#36).
func normalizeBooleanFields(cfg *Config) {
	// EBPFEnabled should be "auto", "true", or "false" (lowercase).
	// Normalize "True"/"False" and handle quoted forms.
	switch cfg.Observer.EBPFEnabled {
	case "true", "True", "TRUE":
		cfg.Observer.EBPFEnabled = "true"
	case "false", "False", "FALSE":
		cfg.Observer.EBPFEnabled = "false"
	case "auto", "Auto", "AUTO", "":
		cfg.Observer.EBPFEnabled = "auto"
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
	normalizeBooleanFields(cfg)
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
	normalizeBooleanFields(cfg)
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

// ResolveProjectDir maps a session-supplied project_dir to an absolute path.
//
// Rules (in order):
//  1. Empty input → DefaultProjectDir (or "" if also unset; caller decides).
//  2. Absolute path → returned unchanged. The caller already knows where they
//     want to be, and rewriting absolute paths under WorkspaceRoot would be
//     surprising on bare-metal hosts.
//  3. Relative path → joined under WorkspaceRoot when WorkspaceRoot is set
//     (container/PVC mode); otherwise filepath.Abs against the daemon's CWD
//     (bare-metal back-compat).
//
// The resolver does not stat the result. Caller is responsible for ensuring
// the directory exists or creating it.
func (s *SessionConfig) ResolveProjectDir(in string) string {
	if in == "" {
		return s.DefaultProjectDir
	}
	if filepath.IsAbs(in) {
		return in
	}
	if s.WorkspaceRoot != "" {
		return filepath.Join(s.WorkspaceRoot, in)
	}
	abs, err := filepath.Abs(in)
	if err != nil {
		return in
	}
	return abs
}
