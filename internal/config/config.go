package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/trace"
	"github.com/dmz006/datawatch/internal/workerprofile"
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
	// BL316 S2 — federation peer extensions for YAML-seeded peers.
	// Federated marks this entry as a federation peer (enables CBAC enforcement).
	Federated bool `yaml:"federated,omitempty"`
	// AuthType is the authentication mechanism: "token" (default) or "spiffe" (future).
	AuthType string `yaml:"auth_type,omitempty"`
	// Capabilities is the CBAC grant list; mix of group names and surface:action strings.
	Capabilities []string `yaml:"capabilities,omitempty"`
	// TLSSkipVerify disables TLS cert validation for THIS peer only. Default
	// false. OK for a self-signed/local-dev peer on a trusted network only;
	// document the risk. Same convention as VaultConfig.TLSSkipVerify below.
	TLSSkipVerify bool `yaml:"tls_skip_verify,omitempty"`
	// FingerprintSync opts THIS peer into the resource-fingerprint exchange.
	// Opt-in per peer, default false — the operator decision recorded in
	// docs/plans/2026-10-14-concurrency-and-budget-design.md (open question
	// 10): control capability does not imply fingerprint trust.
	FingerprintSync bool `yaml:"fingerprint_sync,omitempty"`
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

// PlatformContextNoteMaxLen is the longest operator note, in bytes, the config
// surfaces accept for platform_context.note.
const PlatformContextNoteMaxLen = 2000

// PlatformContextConfig controls the platform context text delivered to sessions.
type PlatformContextConfig struct {
	// Enabled turns the platform context section on or off (default true; nil = true).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Note is optional operator text appended after the platform context text
	// inside the section. Appended only when non-empty.
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
}

// IsEnabled returns whether platform context is enabled (default true).
func (p PlatformContextConfig) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// EffectiveEnabled resolves the value for a session: the project override when
// set, otherwise the global value.
func (p PlatformContextConfig) EffectiveEnabled(project *ProjectPolicy) bool {
	if project != nil && project.PlatformContextEnabled != nil {
		return *project.PlatformContextEnabled
	}
	return p.IsEnabled()
}

// AppendNote returns text with the Note appended after it, only when Note is
// non-empty (after trimming whitespace).
func (p PlatformContextConfig) AppendNote(text string) string {
	note := strings.TrimSpace(p.Note)
	if note == "" {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n\n" + note
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
	if m.SessionAwareness == nil {
		return true
	}
	return *m.SessionAwareness
}

// IsSessionBroadcast returns whether session summaries are broadcast (default true).
func (m MemoryConfig) IsSessionBroadcast() bool {
	if m.SessionBroadcast == nil {
		return true
	}
	return *m.SessionBroadcast
}

// EffectiveStorageMode returns the storage mode, defaulting to "summary".
func (m MemoryConfig) EffectiveStorageMode() string {
	if m.StorageMode == "verbatim" {
		return "verbatim"
	}
	return "summary"
}

// CrossSessionConfig controls the Cross-Session Communication Rule —
// guidance injected into spawned claude-code sessions' CLAUDE.md
// steering them toward datawatch's own audited memory/discussion/
// reply MCP tools, instead of Claude Code's native cross-session
// messaging or any other non-datawatch persistence, for anything that
// should be audited or might run across a host/container boundary
// (operator policy, 2026-10-09). This only controls whether the
// GUIDANCE TEXT is injected — the underlying tools it points at
// (memory_recall, memory_discussion_write/recall, reply_to_parent,
// memory_handoff) work regardless of this setting.
type CrossSessionConfig struct {
	// Enabled injects the policy section into session guardrails
	// (default true — same instruction-injection-toggle shape as
	// Memory.SessionAwareness, not a subsystem toggle like
	// Memory.Enabled/RTK.Enabled, since there's no separate subsystem
	// here to turn on or off).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// IsEnabled returns whether the Cross-Session Communication Rule is
// injected into session guardrails (default true).
func (c CrossSessionConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
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

// Git push-on-complete policies (git.push_on_complete) — what datawatch
// does to the finished branch when a task completes. Defined here as plain
// string constants (instead of importing internal/git) to avoid an import
// cycle with the config package.
const (
	// GitPushNever never pushes on completion.
	GitPushNever = "never"
	// GitPushBranch pushes the finished work branch to the remote
	// (never force), with a visible "not pushed" warning when that
	// isn't possible.
	GitPushBranch = "branch"
	// GitPushDefaultBranch pushes the default branch.
	GitPushDefaultBranch = "default_branch"
)

// Git secret-scan modes (git.secret_scan.mode) — what datawatch does
// when the pre-push secret scan of about-to-be-pushed commits finds a
// match. Defined here as plain string constants (as with the push
// policies above, and mirroring the internal/git detector names)
// to avoid an import cycle with the config package.
const (
	// GitSecretScanOff disables the pre-push secret scan.
	GitSecretScanOff = "off"
	// GitSecretScanShadow runs the scan, logs and records findings, but
	// does not refuse the push.
	GitSecretScanShadow = "shadow"
	// GitSecretScanEnforce runs the scan and refuses the push when a
	// finding is not allow-listed.
	GitSecretScanEnforce = "enforce"
)

// GitSecretScanDefaultDetectors is the detector list in effect when
// git.secret_scan.detectors is unset: all built-in detectors (the names
// of internal/git's built-ins, kept here as plain strings to avoid the
// import cycle).
var GitSecretScanDefaultDetectors = []string{
	"private_key",
	"aws_access_key",
	"github_token",
	"generic_token",
	"slack_token",
}

// GitConfig holds git lifecycle settings for finished work.
type GitConfig struct {
	// PushOnComplete controls what gets pushed to the remote when work
	// completes. One of "never", "branch", "default_branch"; empty or
	// unknown values behave as "branch". Push is never forced.
	PushOnComplete string `yaml:"push_on_complete" json:"push_on_complete"`
	// PushRemote is the remote the finished branch is pushed to
	// (default "origin").
	PushRemote string `yaml:"push_remote" json:"push_remote"`
	// SecretScan is the pre-push secret-scan block. See
	// GitSecretScanConfig: the defaults (an unset or empty mode reads
	// shadow — the first-release default, matching the shadow-first
	// rollout convention of security_failure, supervisor_watch, lane
	// fill and create search — and an unset detector list reads all
	// built-in detectors) are applied only in the Effective* accessors,
	// and a mode other than off|shadow|enforce is refused on every
	// write path.
	SecretScan GitSecretScanConfig `yaml:"secret_scan" json:"secret_scan"`
	// ReleaseCIWindowMinutes is how many minutes the daemon waits for the
	// checks on a release to finish before it gives up and reports the
	// release as not confirmed (default 60).
	ReleaseCIWindowMinutes int `yaml:"release_ci_window_minutes" json:"release_ci_window_minutes"`
}

// GitSecretScanConfig is the git.secret_scan block: which detectors
// run, and what is allow-listed (path globs and fingerprint hashes).
type GitSecretScanConfig struct {
	// Mode is "off", "shadow" or "enforce"; empty or unknown reads as
	// shadow through EffectiveSecretScanMode.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// Detectors is the list of detector names to enable (see
	// GitSecretScanDefaultDetectors); empty reads as all built-ins.
	Detectors []string `yaml:"detectors,omitempty" json:"detectors,omitempty"`
	// AllowList is the allow-list: path globs and fingerprint hashes
	// that suppress findings.
	AllowList GitSecretAllowList `yaml:"allow_list" json:"allow_list"`
}

// GitSecretAllowList is the git.secret_scan.allow_list block. Paths are
// glob patterns matched against the repository-relative path (and a
// bare name like "vendor" matches any path inside that directory);
// fingerprints are the short 16-hex SHA-256 fingerprint a finding
// carries, or a full 64-hex SHA-256 digest.
type GitSecretAllowList struct {
	// Paths are path globs whose matches are skipped entirely.
	Paths []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	// Fingerprints are allow-listed fingerprint hashes.
	Fingerprints []string `yaml:"fingerprints,omitempty" json:"fingerprints,omitempty"`
}

// EffectivePushOnComplete returns PushOnComplete normalised to one of the
// three known policies; empty or unknown values fall back to "branch".
func (g GitConfig) EffectivePushOnComplete() string {
	switch g.PushOnComplete {
	case GitPushNever, GitPushBranch, GitPushDefaultBranch:
		return g.PushOnComplete
	default:
		return GitPushBranch
	}
}

// EffectivePushRemote returns PushRemote, defaulting to "origin" when empty.
func (g GitConfig) EffectivePushRemote() string {
	if g.PushRemote == "" {
		return "origin"
	}
	return g.PushRemote
}

// EffectiveSecretScanMode returns SecretScan.Mode normalised to one of
// off, shadow or enforce; empty or unknown values fall back to shadow —
// the first-release default, so a fresh configuration records findings
// without ever refusing a push.
func (g GitConfig) EffectiveSecretScanMode() string {
	switch g.SecretScan.Mode {
	case GitSecretScanOff, GitSecretScanShadow, GitSecretScanEnforce:
		return g.SecretScan.Mode
	default:
		return GitSecretScanShadow
	}
}

// ValidateGitSecretScanMode accepts the empty string (the default,
// shadow) and the three known modes; anything else is an error for the
// write paths.
func ValidateGitSecretScanMode(mode string) error {
	switch mode {
	case "", GitSecretScanOff, GitSecretScanShadow, GitSecretScanEnforce:
		return nil
	default:
		return fmt.Errorf("git.secret_scan.mode must be \"off\", \"shadow\" or \"enforce\", got %q", mode)
	}
}

// Release paths (autonomous.release_path).
const (
	ReleasePathDirectPush  = "direct_push"
	ReleasePathPullRequest = "pull_request"
)

// ValidateReleasePath requires autonomous.release_path to be direct_push or
// pull_request.
func ValidateReleasePath(v string) error {
	switch v {
	case "", ReleasePathDirectPush, ReleasePathPullRequest: // "" = unset, reads the default
		return nil
	default:
		return fmt.Errorf("autonomous.release_path must be \"direct_push\" or \"pull_request\", got %q", v)
	}
}

// ValidateReleaseCIWindowMinutes requires git.release_ci_window_minutes to be
// greater than zero.
func ValidateReleaseCIWindowMinutes(n int) error {
	if n <= 0 {
		return fmt.Errorf("git.release_ci_window_minutes must be greater than 0 (a whole number of minutes), got %d", n)
	}
	return nil
}

// ValidateGitSecretScanDetectors rejects a detector name that is not one of
// the built-ins (GitSecretScanDefaultDetectors). A misspelt name would make
// the push-time scan fail, which must never be possible to configure for a
// security gate. An empty list is valid (it resets to all built-ins).
func ValidateGitSecretScanDetectors(names []string) error {
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		known := false
		for _, d := range GitSecretScanDefaultDetectors {
			if n == d {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("git.secret_scan.detectors: unknown detector %q (use %s)", n, strings.Join(GitSecretScanDefaultDetectors, ", "))
		}
	}
	return nil
}

// EffectiveSecretScanDetectors returns the configured detector list, or
// all built-in detectors (GitSecretScanDefaultDetectors) when unset.
func (g GitConfig) EffectiveSecretScanDetectors() []string {
	if len(g.SecretScan.Detectors) == 0 {
		return append([]string(nil), GitSecretScanDefaultDetectors...)
	}
	return g.SecretScan.Detectors
}

// SecretScanRefusesPush reports whether the effective secret-scan mode
// is enforce: true only for enforce. Shadow (and off) never refuse a
// push — shadow logs and records findings only.
func (g GitConfig) SecretScanRefusesPush() bool {
	return g.EffectiveSecretScanMode() == GitSecretScanEnforce
}

// EffectiveSecretScanAllowList returns the configured allow-list; unset
// slices read as empty (nothing allow-listed).
func (g GitConfig) EffectiveSecretScanAllowList() GitSecretAllowList {
	if g.SecretScan.AllowList.Paths == nil {
		g.SecretScan.AllowList.Paths = []string{}
	}
	if g.SecretScan.AllowList.Fingerprints == nil {
		g.SecretScan.AllowList.Fingerprints = []string{}
	}
	return g.SecretScan.AllowList
}

// ResourceLimitsConfig holds the memory ceiling settings (the
// resource_limits.* section). Zero values read as unset and are replaced
// by the defaults when the config loads.
type ResourceLimitsConfig struct {
	// MemorySharePercent is the share of host memory, in percent, that
	// datawatch work may use. Default 50.
	MemorySharePercent int `yaml:"memory_share_percent" json:"memory_share_percent"`
	// MemoryFloorMB is the lowest memory ceiling in MB. Default 2048.
	MemoryFloorMB int `yaml:"memory_floor_mb" json:"memory_floor_mb"`
	// MemoryCapMB is the highest memory ceiling in MB. Default 16384.
	MemoryCapMB int `yaml:"memory_cap_mb" json:"memory_cap_mb"`
}

// DefaultResourceLimits returns the default memory ceiling settings.
func DefaultResourceLimits() ResourceLimitsConfig {
	return ResourceLimitsConfig{
		MemorySharePercent: 50,
		MemoryFloorMB:      2048,
		MemoryCapMB:        16384,
	}
}

// Config holds all datawatch configuration.
type Config struct {
	// DeprecatedKeys lists the deprecated keys (dotted YAML paths) set in
	// the loaded file, so the daemon can warn about them once at
	// startup. Populated by Load/LoadSecure; never serialized.
	DeprecatedKeys []string `yaml:"-" json:"-"`

	// Signal configuration
	Signal SignalConfig `yaml:"signal"`

	// Session configuration
	Session SessionConfig `yaml:"session"`

	// UI holds the top-level UI display settings (the ui.* section):
	// splash minimum-dwell seconds. Unset keys read their defaults.
	UI UIConfig `yaml:"ui" json:"ui"`

	// Git lifecycle settings: push-on-complete policies and push remote.
	Git GitConfig `yaml:"git" json:"git"`

	// ResourceLimits holds the memory ceiling settings.
	ResourceLimits ResourceLimitsConfig `yaml:"resource_limits" json:"resource_limits"`

	// Tracing holds the normalized agent trace settings (tracing.mode,
	// tracing.arg_cap_bytes, tracing.retention_days, tracing.backends).
	// Unset values read as the defaults (summary mode, every backend on)
	// through Tracing.WithDefaults.
	Tracing trace.Settings `yaml:"tracing" json:"tracing"`

	// Server configuration for the PWA/WebSocket server
	Server ServerConfig `yaml:"server"`

	// MCP server configuration (for Cursor, Claude Desktop, VS Code integration)
	MCP MCPConfig `yaml:"mcp"`

	// Hostname is used to prefix messages and identify this machine.
	// Auto-detected from OS hostname if empty.
	Hostname string `yaml:"hostname"`

	// DataDir is where sessions, logs, and state are stored.
	DataDir string `yaml:"data_dir"`

	// NotifyExclude (found 2026-10-10, operator-reported: email
	// notifications they hadn't expected once the imap_mcp outbound
	// bounce bug, B110, was fixed) names comm-channel backends
	// (matching each backend's own Name(), e.g. "imap_mcp", "signal",
	// "ntfy", "email") to skip when broadcasting session-lifecycle
	// bundled notifications. Every configured channel still receives
	// everything else (inbound commands still work, this only
	// suppresses the outbound notification-bundle broadcast); empty
	// (default) excludes nothing, preserving the existing behavior
	// exactly. General across every comm channel, not per-backend —
	// one list, not N new config fields.
	NotifyExclude []string `yaml:"notify_exclude,omitempty" json:"notify_exclude,omitempty"`

	// Detection holds global patterns for prompt/completion/rate-limit detection.
	// Per-LLM overrides can be set in each backend's detection field.
	Detection DetectionConfig `yaml:"detection,omitempty"`

	// LLM backends
	Ollama         OllamaConfig         `yaml:"ollama"`
	OpenWebUI      OpenWebUIConfig      `yaml:"openwebui"`
	Aider          AiderConfig          `yaml:"aider"`
	Pi             PiConfig             `yaml:"pi"`
	PiPrompt       PiPromptConfig       `yaml:"pi_prompt"`
	Goose          GooseConfig          `yaml:"goose"`
	Gemini         GeminiConfig         `yaml:"gemini"`
	OpenCode       OpenCodeConfig       `yaml:"opencode"`
	OpenCodeACP    OpenCodeACPConfig    `yaml:"opencode_acp"`
	OpenCodePrompt OpenCodePromptConfig `yaml:"opencode_prompt"`
	Shell          ShellBackendConfig   `yaml:"shell_backend"`

	// LSP holds the operator-defined Language Server Protocol server registry.
	// Each key is the language name shown in the session-creation UI ("go",
	// "typescript", "python", …). OpenCode sessions write the selected entry
	// to <projectDir>/opencode.json at launch time. Claude Code does not
	// support native LSP — use shell diagnostics (gopls check, tsc --noEmit)
	// as an alternative.
	LSP LSPConfig `yaml:"lsp"`

	// DNSChannel holds DNS tunneling communication channel configuration.
	DNSChannel DNSChannelConfig `yaml:"dns_channel"`

	// Acme holds the native ACME / Let's Encrypt subsystem configuration
	// (BL397). See docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md.
	Acme AcmeConfig `yaml:"acme,omitempty"`

	// Push controls outbound mobile push-endpoint validation (BL394 SSRF fix).
	Push PushConfig `yaml:"push,omitempty"`

	// Audit controls the HTTP access / WS lifecycle / auth-failure log
	// and retention for it and the operator audit log (GH#201).
	Audit AuditConfig `yaml:"audit,omitempty"`

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

	// Vision image description for image attachments from messaging backends.
	Vision VisionConfig `yaml:"vision"`

	// Named profiles for different accounts/API keys. Each profile overrides
	// the base backend config with custom env vars, binary, or model.
	Profiles map[string]ProfileConfig `yaml:"profiles,omitempty"`

	// OnboardingReport — switches for the Automation onboarding report
	// (what is configured, what is missing, what to try next). An unset key
	// reads its default. See OnboardingReportConfig.
	OnboardingReport OnboardingReportConfig `yaml:"onboarding_report,omitempty" json:"onboarding_report,omitempty"`

	// Projects (BL27) — operator-registered named project directories.
	// Each entry is a quick alias for `new: @<project>: <task>` and the
	// REST start handler. Optional default_backend overrides
	// session.llm_backend for sessions launched into this project.
	Projects map[string]ProjectConfigEntry `yaml:"projects,omitempty"`

	// ProjectsDefaults — daemon-level defaults a project inherits when it sets
	// none of its own (branch retention, weekly share, major trigger). Kept
	// beside Projects, not inside it, so a project named "defaults" cannot
	// collide. See projects_defaults.go.
	ProjectsDefaults ProjectsDefaultsConfig `yaml:"projects_defaults,omitempty" json:"projects_defaults,omitempty"`

	// Templates (BL5) — named session-start bundles.
	Templates map[string]SessionTemplateEntry `yaml:"templates,omitempty"`

	// Servers is a list of remote datawatch instances to manage.
	// The implicit "local" entry (localhost:Server.Port) is always available.
	Servers []RemoteServerConfig `yaml:"servers,omitempty"`

	// Memory controls the episodic memory system — vector-indexed project knowledge.
	Memory MemoryConfig `yaml:"memory" json:"memory"`

	// PlatformContext controls the platform context text delivered to sessions
	// (default enabled). A ProjectPolicy override takes precedence.
	PlatformContext PlatformContextConfig `yaml:"platform_context" json:"platform_context"`

	// CrossSession controls whether spawned claude-code sessions are
	// instructed to prefer datawatch's own audited memory/comm MCP
	// tools over Claude Code's native cross-session messaging for
	// anything that should be audited or might cross a host/container
	// boundary (operator policy, 2026-10-09).
	CrossSession CrossSessionConfig `yaml:"cross_session" json:"cross_session"`

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

	// Classifier holds the settings for the logprob classification judge.
	Classifier ClassifierConfig `yaml:"classifier,omitempty" json:"classifier,omitempty"`

	// Identity holds the operator identity settings (identity.*). The
	// top-level key is `identity`; the "Identity" banner in template.go is
	// only a comment heading about machine identification.
	Identity IdentityConfig `yaml:"identity" json:"identity"`

	// Usage holds the usage and cost calculator settings (usage.calculator.*).
	// All keys are reachable through every config channel per the parity rule.
	Usage UsageConfig `yaml:"usage,omitempty" json:"usage,omitempty"`

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
	Observer ObserverConfig `yaml:"observer" json:"observer"`

	// Messaging backends
	Discord       DiscordConfig       `yaml:"discord"`
	Slack         SlackConfig         `yaml:"slack"`
	Telegram      TelegramConfig      `yaml:"telegram"`
	Matrix        MatrixConfig        `yaml:"matrix"`
	Twilio        TwilioConfig        `yaml:"twilio"`
	Ntfy          NtfyConfig          `yaml:"ntfy"`
	Email         EmailConfig         `yaml:"email"`
	ImapMcp       ImapMcpConfig       `yaml:"imap_mcp"`
	GitHubWebhook GitHubWebhookConfig `yaml:"github_webhook"`
	Webhook       WebhookConfig       `yaml:"webhook"`

	// BL255 v6.7.0 — skill registries (PAI default + operator-added).
	Skills SkillsConfig `yaml:"skills,omitempty" json:"skills,omitempty"`

	// BL297 v6.22.3 — Council Mode "Add Persona" wizard storage retention.
	// Operator-decided 2026-05-08: SQLite-backed wizard sessions live
	// under ~/.datawatch/council/wizard-sessions.db; auto-GC after
	// `draft_retention_days` days; operator can purge selectively or
	// entirely via REST/MCP/CLI/comm/PWA.
	Council CouncilConfig `yaml:"council,omitempty" json:"council,omitempty"`

	// #54 — smoke-run cross-instance reporting. When ForwardURL is set,
	// the smoke-progress handler also POSTs each write to that URL so a
	// production dashboard can display test-daemon runs without sharing
	// the same data directory.
	Smoke SmokeConfig `yaml:"smoke,omitempty" json:"smoke,omitempty"`

	// v7.0.0 S1 — ComputeNode registry seed. Each entry becomes a
	// Node in the registry on daemon start (only if no JSON entry
	// already exists for that name — operator runtime edits win).
	// Schema: see internal/compute/node.go::Node.
	ComputeNodes []ComputeNodeYAML `yaml:"compute_nodes,omitempty" json:"compute_nodes,omitempty"`

	// PinCheckSeconds is how often the pin keeper re-verifies that each
	// compute node with a node_pinned_model is still serving that model.
	// 0 turns the check off. Written without omitempty so an explicit 0
	// survives a save/load round trip.
	PinCheckSeconds int `yaml:"pin_check_seconds" json:"pin_check_seconds"`

	// ComputePlacement holds the compute_placement placement-policy block.
	// An unset key reads its default; see ComputePlacementConfig.
	ComputePlacement ComputePlacementConfig `yaml:"compute_placement,omitempty" json:"compute_placement,omitempty"`

	// WebSearch (BL372) — web search MCP injection for opencode and goose sessions.
	// When enabled, auto-injects `datawatch mcp-search` as an MCP server so agents
	// have a `web_search` tool backed by the configured SearXNG instance.
	WebSearch WebSearchConfig `yaml:"web_search,omitempty" json:"web_search,omitempty"`

	// Dashboard holds the dashboard block (dashboard.*) — card rendering
	// switches. An unset key reads its default; see DashboardConfig.
	Dashboard DashboardConfig `yaml:"dashboard,omitempty" json:"dashboard,omitempty"`

	// Results — per-schedule result history store (scheduled jobs):
	// how many latest posted results each schedule name retains before
	// the oldest ones are evicted.
	Results ResultsConfig `yaml:"results,omitempty" json:"results,omitempty"`

	// CostReport — knobs for the cost report (internal/costreport), the
	// one view over recorded spend grouped by automaton, automation,
	// backend, release or week. Every key is a pointer so an absent key reads
	// its default while an explicit value sticks through saves and YAML
	// round trips. See the cost_report block in docs/config-reference.yaml.
	CostReport CostReportConfig `yaml:"cost_report,omitempty" json:"cost_report,omitempty"`
}

// CostReport defaults. Every one of them is read from the config struct:
// the report logic (internal/costreport) hard-codes none of them.
const (
	// DefaultCostReportGroupBy is the default group_by for the cost
	// report: automaton.
	DefaultCostReportGroupBy = "automaton"
	// DefaultCostReportWindowDays is the default cost report window in
	// days when a request does not carry its own since bound: 7.
	DefaultCostReportWindowDays = 7
	// DefaultCostReportWeekStart is the week start day for week-
	// grouped cost reports: monday.
	DefaultCostReportWeekStart = "monday"
)

// CostReportConfig is the cost_report block: how the cost report
// (internal/costreport) behaves when a request does not override a knob.
//
//	cost_report:
//	  default_group_by: automaton   # or automation, backend, release, week
//	  default_window_days: 7
//	  show_usd_estimates: true
//	  week_start: monday
//	  include_peer_spend: false
type CostReportConfig struct {
	// DefaultGroupBy — the grouping a request is missing its own
	// group_by under: automaton, automation, backend, release or week.
	// Default "automaton" (DefaultCostReportGroupBy); an absent key or a
	// value outside that vocabulary reads the default.
	DefaultGroupBy *string `yaml:"default_group_by,omitempty" json:"default_group_by,omitempty"`
	// DefaultWindowDays — the window, in days, the report falls back to
	// when a request has no since bound: until minus this many days.
	// Default 7 (DefaultCostReportWindowDays); a value below 1 reads
	// the default.
	DefaultWindowDays *int `yaml:"default_window_days,omitempty" json:"default_window_days,omitempty"`
	// ShowUSDEstimates — whether rows carry a usd_estimate at all (on
	// top of a rate being configured for the row's backends). Default
	// true; when false, USD never appears even where rates exist.
	ShowUSDEstimates *bool `yaml:"show_usd_estimates,omitempty" json:"show_usd_estimates,omitempty"`
	// WeekStart — the day that starts a week for week-grouped reports:
	// monday or sunday. Default "monday" (DefaultCostReportWeekStart);
	// an absent key or any other value reads the default.
	WeekStart *string `yaml:"week_start,omitempty" json:"week_start,omitempty"`
	// IncludePeerSpend — whether rows include spend recorded by
	// federation peers that opted into reporting to this daemon. Default
	// false: peer spend is excluded.
	IncludePeerSpend *bool `yaml:"include_peer_spend,omitempty" json:"include_peer_spend,omitempty"`
}

// EffectiveGroupBy returns the valid default grouping: automaton,
// automation, backend, release or week. An unset key, or a set key outside
// that vocabulary, reads the default "automaton"
// (DefaultCostReportGroupBy).
func (c CostReportConfig) EffectiveGroupBy() string {
	if c.DefaultGroupBy != nil {
		switch strings.ToLower(strings.TrimSpace(*c.DefaultGroupBy)) {
		case "automaton", "automation", "backend", "release", "week":
			return strings.ToLower(strings.TrimSpace(*c.DefaultGroupBy))
		}
	}
	return DefaultCostReportGroupBy
}

// EffectiveWindowDays returns the default report window in days: the
// configured value when it is at least 1, otherwise the default 7
// (DefaultCostReportWindowDays).
func (c CostReportConfig) EffectiveWindowDays() int {
	if c.DefaultWindowDays != nil && *c.DefaultWindowDays >= 1 {
		return *c.DefaultWindowDays
	}
	return DefaultCostReportWindowDays
}

// EffectiveShowUSDEstimates returns whether the report may carry USD
// estimates: true while the key is absent or unset to true.
func (c CostReportConfig) EffectiveShowUSDEstimates() bool {
	return boolOrDefault(c.ShowUSDEstimates, true)
}

// EffectiveWeekStart returns the week start day for week-grouped
// reports: monday or sunday. An unset key, or a set key with any other
// value, reads the default "monday" (DefaultCostReportWeekStart).
func (c CostReportConfig) EffectiveWeekStart() string {
	if c.WeekStart != nil {
		switch strings.ToLower(strings.TrimSpace(*c.WeekStart)) {
		case "monday", "sunday":
			return strings.ToLower(strings.TrimSpace(*c.WeekStart))
		}
	}
	return DefaultCostReportWeekStart
}

// EffectiveIncludePeerSpend returns whether the report includes peer
// spend: false while the key is absent (default false).
func (c CostReportConfig) EffectiveIncludePeerSpend() bool {
	return boolOrDefault(c.IncludePeerSpend, false)
}

// DashboardConfig is the dashboard block.
//
//	dashboard:
//	  hide_inactive_cards: false
//	  console:
//	    enabled: false
//
// An absent dashboard key, or an absent hide_inactive_cards key, or an
// absent console subkey, each reads its default, true.
type DashboardConfig struct {
	// HideInactiveCards controls whether the dashboard hides inactive
	// cards. A nil pointer (key absent) reads as the default, true.
	HideInactiveCards *bool `yaml:"hide_inactive_cards,omitempty" json:"hide_inactive_cards,omitempty"`
	// Console carries the operator console block (dashboard.console.*):
	// the switches gating the GET /api/dashboard/console one-glance
	// summary. Every subkey defaults to true (nil = key absent).
	Console DashboardConsoleConfig `yaml:"console,omitempty" json:"console,omitempty"`
}

// DashboardConsoleConfig is the operator console block under dashboard.*.
// enabled gates the whole endpoint; the show_* keys gate one section each.
// Each field is a pointer so that "key absent" reads the default, true,
// while an explicit false sticks through saves and YAML round trips.
type DashboardConsoleConfig struct {
	// Enabled gates the endpoint as a whole; when false every section
	// reports itself unavailable regardless of data.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// ShowQuestions gates the questions_waiting section (open operator
	// questions: the question channel, held questions and worker markers).
	ShowQuestions *bool `yaml:"show_questions,omitempty" json:"show_questions,omitempty"`
	// ShowHeldBlocked gates the held_blocked section (automata stuck in
	// failed/blocked triage states and uncommitted-work holds).
	ShowHeld *bool `yaml:"show_held_blocked,omitempty" json:"show_held_blocked,omitempty"`
	// ShowLanes gates the lanes section (lane fill status and the
	// capacity admission wait queue).
	ShowLanes *bool `yaml:"show_lanes,omitempty" json:"show_lanes,omitempty"`
	// ShowUsage gates the usage section (burn guard state and the usage
	// advice projection).
	ShowUsage *bool `yaml:"show_usage,omitempty" json:"show_usage,omitempty"`
	// ShowCIRelease gates the ci_release section (repo CI watch records
	// and the latest-release check).
	ShowCIRelease *bool `yaml:"show_ci_release,omitempty" json:"show_ci_release,omitempty"`
}

// EffectiveHideInactiveCards returns the effective dashboard.hide_inactive_cards
// value (true when the key is absent).
func (c DashboardConfig) EffectiveHideInactiveCards() bool {
	return boolOrDefault(c.HideInactiveCards, true)
}

// EffectiveEnabled returns the effective dashboard.console.enabled value
// (true when the key is absent).
func (c DashboardConsoleConfig) EffectiveEnabled() bool {
	return boolOrDefault(c.Enabled, true)
}

// EffectiveShowQuestions returns the effective dashboard.console.show_questions
// value (true when the key is absent).
func (c DashboardConsoleConfig) EffectiveShowQuestions() bool {
	return boolOrDefault(c.ShowQuestions, true)
}

// EffectiveShowHeld returns the effective dashboard.console.show_held_blocked
// value (true when the key is absent).
func (c DashboardConsoleConfig) EffectiveShowHeld() bool {
	return boolOrDefault(c.ShowHeld, true)
}

// EffectiveShowLanes returns the effective dashboard.console.show_lanes
// value (true when the key is absent).
func (c DashboardConsoleConfig) EffectiveShowLanes() bool {
	return boolOrDefault(c.ShowLanes, true)
}

// EffectiveShowUsage returns the effective dashboard.console.show_usage
// value (true when the key is absent).
func (c DashboardConsoleConfig) EffectiveShowUsage() bool {
	return boolOrDefault(c.ShowUsage, true)
}

// EffectiveShowCIRelease returns the effective dashboard.console.show_ci_release
// value (true when the key is absent).
func (c DashboardConsoleConfig) EffectiveShowCIRelease() bool {
	return boolOrDefault(c.ShowCIRelease, true)
}

// SmokeConfig controls smoke-run cross-instance reporting (#54).
type SmokeConfig struct {
	// ForwardURL, if set, receives a POST copy of every smoke-run write
	// so a remote production dashboard sees test-daemon progress.
	// Example: "https://prod.example.com:8443"
	ForwardURL string `yaml:"forward_url,omitempty" json:"forward_url,omitempty"`
	// ForwardToken is the bearer token sent with forwarded requests.
	ForwardToken string `yaml:"forward_token,omitempty" json:"forward_token,omitempty"`
}

// ComputeNodeYAML mirrors compute.Node for cfg-side seeding without
// importing the compute package (keeps cfg dep tree small). Daemon
// converts at startup.
type ComputeNodeYAML struct {
	Name               string   `yaml:"name" json:"name"`
	Kind               string   `yaml:"kind" json:"kind"` // local|ssh|docker|k8s|remote|remote-proxy
	Address            string   `yaml:"address,omitempty" json:"address,omitempty"`
	MonitoringEndpoint string   `yaml:"monitoring_endpoint,omitempty" json:"monitoring_endpoint,omitempty"`
	Tags               []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	CostPerHour        float64  `yaml:"cost_per_hour,omitempty" json:"cost_per_hour,omitempty"`
	SchedulingPriority int      `yaml:"scheduling_priority,omitempty" json:"scheduling_priority,omitempty"`
	DeclaredCapacity   struct {
		GPUs                int    `yaml:"gpus,omitempty" json:"gpus,omitempty"`
		GPUMemGB            int    `yaml:"gpu_mem_gb,omitempty" json:"gpu_mem_gb,omitempty"`
		RAMGB               int    `yaml:"ram_gb,omitempty" json:"ram_gb,omitempty"`
		MaxConcurrentModels int    `yaml:"max_concurrent_models,omitempty" json:"max_concurrent_models,omitempty"`
		GPUVendor           string `yaml:"gpu_vendor,omitempty" json:"gpu_vendor,omitempty"`
		GPUModel            string `yaml:"gpu_model,omitempty" json:"gpu_model,omitempty"`
	} `yaml:"declared_capacity,omitempty" json:"declared_capacity,omitempty"`
	Permissions struct {
		AllowedConsumers []string `yaml:"allowed_consumers,omitempty" json:"allowed_consumers,omitempty"`
		DeniedConsumers  []string `yaml:"denied_consumers,omitempty" json:"denied_consumers,omitempty"`
	} `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	// NodePinnedModel keeps this node on one model. Requests without an
	// explicit model run on it; an explicit request for another model is
	// refused unless AllowModelSwap is set. Empty = no pin.
	NodePinnedModel string `yaml:"node_pinned_model,omitempty" json:"node_pinned_model,omitempty"`
	// AllowModelSwap lets an explicit request replace the pinned model.
	AllowModelSwap bool `yaml:"allow_model_swap,omitempty" json:"allow_model_swap,omitempty"`
	// TLSInsecureSkipVerify turns off certificate verification for the
	// node usage probes and the supervisor watch probe. Default false =
	// verify. Cannot be combined with TLSCAFile or TLSCertFingerprint.
	TLSInsecureSkipVerify bool `yaml:"tls_insecure_skip_verify,omitempty" json:"tls_insecure_skip_verify,omitempty"`
	// VerifyTLS makes model pulls to this node verify its HTTPS certificate
	// against the system roots. Default false.
	VerifyTLS bool `yaml:"verify_tls" json:"verify_tls"`
	// TLSCAFile is the path to a PEM CA bundle trusted in addition to the
	// system roots when the node serves a certificate from a private CA.
	// Empty = system roots only.
	TLSCAFile string `yaml:"tls_ca_file,omitempty" json:"tls_ca_file,omitempty"`
	// TLSCertFingerprint pins the SHA-256 fingerprint of the node's leaf
	// certificate, for a single self-signed certificate. Empty = no pin.
	TLSCertFingerprint string `yaml:"tls_cert_fingerprint,omitempty" json:"tls_cert_fingerprint,omitempty"`
	// SessionSlots is the operator's worker count for this node (0 = derive
	// from observer GPU data). Must not be negative.
	SessionSlots int `yaml:"session_slots,omitempty" json:"session_slots,omitempty"`
	// VerifierSlots is the operator's verifier worker count for this node.
	// Nil (unset) = default; explicit 0 = verifier disabled on this node.
	// Must not be negative.
	VerifierSlots *int `yaml:"verifier_slots,omitempty" json:"verifier_slots,omitempty"`
	// VerifierMinFreeMemMB is the free GPU memory in MB a new verifier
	// needs on this node. Nil (unset) = default; explicit 0 turns the
	// headroom check off. Must not be negative.
	VerifierMinFreeMemMB *int `yaml:"verifier_min_free_mem_mb,omitempty" json:"verifier_min_free_mem_mb,omitempty"`
	// VerifierContextMB is the extra context memory in MB one parallel
	// verifier request needs on this node. 0 = unknown, so no suggestion
	// is shown. Must not be negative.
	VerifierContextMB int `yaml:"verifier_context_mb,omitempty" json:"verifier_context_mb,omitempty"`
}

// ComputePlacement — placement policy: what compute placement does when
// the model it is placing is missing on a candidate node, how much headroom
// to leave in the node's memory and disk, and how long an ask may sit
// unanswered before it resolves on its own.
//
// Placement reads the live block on every placement decision, so a runtime
// write (PUT /api/config, `configure compute_placement.<key>=<value>` or
// `datawatch config set compute_placement.<key> <value>`) applies with no
// daemon restart. Except where a comment says otherwise, an unset key reads
// its default.
type ComputePlacementConfig struct {
	// MissingModel decides what placement does when the requested model is
	// not resident on the candidate node:
	//
	//	ask        ask the operator what to do before proceeding
	//	auto_load  load the model onto the node without asking
	//	refuse     skip the node without loading the model
	//
	// Unset reads "ask" (the default) via EffectiveMissingModel.
	MissingModel string `yaml:"missing_model,omitempty" json:"missing_model,omitempty"`

	// MemHeadroomPct is the percentage of the node's total memory (0-100)
	// that placement keeps free for everything that is not a model it is
	// placing. Unset reads 10 (the default).
	MemHeadroomPct int `yaml:"mem_headroom_pct,omitempty" json:"mem_headroom_pct,omitempty"`

	// DiskHeadroomGB is the gigabytes of disk space placement keeps free
	// on the node. Unset reads 5 (the default).
	DiskHeadroomGB int `yaml:"disk_headroom_gb,omitempty" json:"disk_headroom_gb,omitempty"`

	// AskTimeoutMinutes is how many minutes an unanswered ask stays pending
	// before it resolves on its own. Unset reads 30 (the default).
	AskTimeoutMinutes int `yaml:"ask_timeout_minutes,omitempty" json:"ask_timeout_minutes,omitempty"`

	// AssumeModelSizeGB is the gigabyte size of a model that is already on
	// the node but whose resident model datawatch is not told about.
	// 0 means the size is unknown — that is the default, so an explicit 0
	// must survive a save/load round trip and applyDefaults never fills it.
	AssumeModelSizeGB int `yaml:"assume_model_size_gb,omitempty" json:"assume_model_size_gb,omitempty"`
}

// Compute placement defaults and the missing_model policy vocabulary. A
// changed decision is a deliberate edit of this const block and of the
// tests that pin it.
const (
	// The compute_placement.missing_model policy values.
	ComputePlacementAsk      = "ask"
	ComputePlacementAutoLoad = "auto_load"
	ComputePlacementRefuse   = "refuse"

	ComputePlacementDefaultMissingModel      = "ask"
	ComputePlacementDefaultMemHeadroomPct    = 10
	ComputePlacementDefaultDiskHeadroomGB    = 5
	ComputePlacementDefaultAskTimeoutMinutes = 30
	// assume_model_size_gb defaults to 0 = size unknown, so applyDefaults
	// deliberately leaves the field untouched.
	ComputePlacementDefaultAssumeModelSizeGB = 0
)

// ComputePlacementMissingModels lists the valid compute_placement
// .missing_model values in canonical order.
func ComputePlacementMissingModels() []string {
	return []string{ComputePlacementAsk, ComputePlacementAutoLoad, ComputePlacementRefuse}
}

// ComputePlacementValidMissingModel reports whether v is a usable
// missing_model value. The empty string is accepted too: unset reads the
// "ask" default.
func ComputePlacementValidMissingModel(v string) bool {
	if v == "" {
		return true
	}
	for _, m := range ComputePlacementMissingModels() {
		if v == m {
			return true
		}
	}
	return false
}

// EffectiveMissingModel returns the missing_model policy in effect: "ask"
// when unset.
func (c *ComputePlacementConfig) EffectiveMissingModel() string {
	if c == nil || c.MissingModel == "" {
		return ComputePlacementDefaultMissingModel
	}
	return c.MissingModel
}

// Validate rejects the bad compute_placement values: an unknown
// missing_model, a memory headroom outside 0-100, or a negative disk or
// ask-timeout or assumed size. The error names the key and the allowed
// values.
func (c *ComputePlacementConfig) Validate() error {
	if c == nil {
		return nil
	}
	if !ComputePlacementValidMissingModel(c.MissingModel) {
		return fmt.Errorf("compute_placement.missing_model: %q is invalid; valid values: %s (unset reads %q)",
			c.MissingModel, strings.Join(ComputePlacementMissingModels(), ", "), ComputePlacementDefaultMissingModel)
	}
	if c.MemHeadroomPct < 0 || c.MemHeadroomPct > 100 {
		return fmt.Errorf("compute_placement.mem_headroom_pct: %d is invalid; use a whole number from 0 to 100 (the default is %d)",
			c.MemHeadroomPct, ComputePlacementDefaultMemHeadroomPct)
	}
	if c.DiskHeadroomGB < 0 {
		return fmt.Errorf("compute_placement.disk_headroom_gb: %d is invalid; use a whole number of 0 or more (the default is %d)",
			c.DiskHeadroomGB, ComputePlacementDefaultDiskHeadroomGB)
	}
	if c.AskTimeoutMinutes < 0 {
		return fmt.Errorf("compute_placement.ask_timeout_minutes: %d is invalid; use a whole number of 0 or more (the default is %d)",
			c.AskTimeoutMinutes, ComputePlacementDefaultAskTimeoutMinutes)
	}
	if c.AssumeModelSizeGB < 0 {
		return fmt.Errorf("compute_placement.assume_model_size_gb: %d is invalid; use a whole number of 0 or more (0 = size unknown)",
			c.AssumeModelSizeGB)
	}
	return nil
}

// CouncilPersonaOverride allows operators to customize a persona's
// system_prompt (and optionally role) via YAML config instead of
// editing the on-disk YAML files directly. These overrides are applied
// at daemon start via council.Orchestrator.UpdatePersona; the on-disk
// file is also rewritten so the changes survive config removal.
//
// BL296 — YAML surface for persona prompt customization.
type CouncilPersonaOverride struct {
	Name         string `yaml:"name" json:"name"`
	Role         string `yaml:"role,omitempty" json:"role,omitempty"`
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
}

// CouncilConfig configures the Council Mode subsystem. v7.0.0 S3
// adds the LLM wiring fields. BL296 adds the Personas override slice.
type CouncilConfig struct {
	// DraftRetentionDays controls how long unsaved persona-wizard
	// drafts live in SQLite before auto-GC. 0 = disabled (drafts kept
	// forever; operator must purge manually). Default 7.
	DraftRetentionDays int `yaml:"draft_retention_days,omitempty" json:"draft_retention_days,omitempty"`
	// LLMRef names the LLM (from cfg.llms / runtime registry) the
	// council orchestrator should use for persona inference and
	// synthesis. Empty = council disabled (Run() returns honest 503).
	// Default "ollama" — matches the auto-migrated entry from
	// cfg.ollama.host.
	LLMRef string `yaml:"llm_ref,omitempty" json:"llm_ref,omitempty"`
	// Backends (v8.38.0, BL390) — the operator-selected subset of LLM
	// registry names (cfg.llms) available for council use: the
	// Settings UI's multi-select pool, and the set a persona's own
	// Backend override may be assigned from. LLMRef remains the actual
	// default a persona with no Backend override resolves to; this
	// list does not replace it. Empty = no pool configured yet (the
	// PWA falls back to listing every registry entry).
	Backends []string `yaml:"backends,omitempty" json:"backends,omitempty"`
	// MaxParallel is the per-round persona concurrency cap (BL295 Q2).
	// Default 2. 0 = serial.
	MaxParallel int `yaml:"max_parallel,omitempty" json:"max_parallel,omitempty"`
	// CommFirehose, when true, pushes EVERY persona_response to all
	// comm routers (Signal/Telegram/etc.) in addition to the always-on
	// milestones (run_started + round_completed + synthesis_started +
	// run_completed + run_cancelled). Default false = milestones only.
	// v7.0.0-alpha.13 (#199, operator-spec'd 2026-05-09).
	CommFirehose bool `yaml:"comm_firehose,omitempty" json:"comm_firehose,omitempty"`
	// Personas is an optional override slice. Each entry customizes a
	// persona's system_prompt (and optionally role) at daemon start.
	// Useful for fleet-wide persona tuning without editing per-host
	// YAML files. Example config.yaml fragment:
	//
	//   council:
	//     personas:
	//       - name: privacy
	//         system_prompt: "You are a privacy reviewer focused on GDPR/CCPA..."
	//
	// BL296 — YAML surface for persona customization.
	Personas []CouncilPersonaOverride `yaml:"personas,omitempty" json:"personas,omitempty"`

	// IncludeClaudeCode wires Claude Code (via MCP sampling) as an additional
	// deliberation participant alongside local LLM personas. When true, the
	// Council orchestrator calls SamplingDispatcher.Sample with trigger
	// "council_deliberation" before synthesizing the final answer.
	// Default false. BL302 S3 (D8).
	IncludeClaudeCode bool `yaml:"include_claude_code,omitempty" json:"include_claude_code,omitempty"`
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
	// The built-in default leaves this unset: when no operator list is
	// configured, the session Manager matches pane lines against its
	// per-backend anchored table (internal/session/rate_limit_lines.go)
	// keyed by BackendFamily. Entries set here — globally or per-LLM —
	// keep the classic flat-substring behavior.
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
	// AlertSettle is the seconds a session must stay in waiting_input before a
	// needs-input alert/push is sent; returning to running cancels it. Default: 45.
	AlertSettle int `yaml:"alert_settle,omitempty"`
	// AlertRepeat is the seconds an identical needs-input alert for the same
	// session is suppressed. Default: 300.
	AlertRepeat int `yaml:"alert_repeat,omitempty"`
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

// PiConfig holds pi (https://pi.dev) LLM backend configuration. 2026-10-16 —
// added as a lower-risk alternative to opencode-acp for open-model PRD
// execution: pi runs as one blocking CLI call (`pi --print`) instead of a
// long-lived server with an async completion signal, which sidesteps the
// completion-detection bug class found live in opencode-acp the same night.
// See docs/plans/2026-10-16-pi-opencode-evaluation.md (BL466).
type PiConfig struct {
	Enabled bool   `yaml:"enabled"`
	Binary  string `yaml:"binary"`
	Model   string `yaml:"model,omitempty"` // e.g. "ollama/qwen3.8:27b"; empty uses pi's own default
	// ThinkingLevel is the level passed to pi's own --thinking flag on
	// pi-prompt launches. "" = don't pass the flag (pi's model default).
	// Allowed: off, minimal, low, medium, high — see pi_thinking_level.go.
	ThinkingLevel string `yaml:"thinking_level,omitempty"`
	ConsoleCols   int    `yaml:"console_cols,omitempty"`
	ConsoleRows   int    `yaml:"console_rows,omitempty"`
	OutputMode    string `yaml:"output_mode,omitempty"`
	InputMode     string `yaml:"input_mode,omitempty"`
}

// PiPromptConfig holds pi-prompt (one-shot `pi --print`) LLM backend
// configuration — the non-interactive counterpart to PiConfig, mirroring
// the OpenCodeConfig/OpenCodePromptConfig split. Binary falls back to
// PiConfig.Binary when empty (see cmd/datawatch/main.go registration).
type PiPromptConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Binary      string `yaml:"binary,omitempty"`
	Model       string `yaml:"model,omitempty"`
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
	// T2 — provider/model/key injection (BL363)
	Provider  string `yaml:"provider,omitempty"`    // GOOSE_PROVIDER env var (e.g. "anthropic", "openai", "google", "ollama")
	Model     string `yaml:"model,omitempty"`       // GOOSE_MODEL env var
	APIKeyRef string `yaml:"api_key_ref,omitempty"` // literal key OR ${secret:name} — resolved at startup
	// T3 — MCP channel integration (BL363)
	ChannelEnabled bool `yaml:"channel_enabled,omitempty"` // inject GOOSE_MCP__DATAWATCH__* env vars at launch
}

// SearchProvider is one named, independently enabled/disabled web search
// backend (BL391). Multiple providers may be configured at once — the
// websearch.Registry tries them in Priority order (lowest first) and falls
// through to the next on error/empty results, so a free SearXNG instance
// can sit behind a paid Brave Search API as a fallback, or vice versa.
type SearchProvider struct {
	// Name is a unique, operator-chosen identifier (e.g. "primary-brave").
	Name string `yaml:"name" json:"name"`
	// Type selects the backend implementation: "searxng" or "brave".
	Type string `yaml:"type" json:"type"`
	// Enabled toggles this provider without deleting its configuration.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Priority orders providers when more than one is enabled — lowest
	// value is tried first. Ties break by registration order.
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`
	// URL is the SearXNG base URL (type=searxng only), e.g.
	// http://searxng.example.com:3001.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Engine is the SearXNG engine list (type=searxng only), comma-separated,
	// passed as ?engines=. Default "bing" — others are CAPTCHA/rate-limited.
	Engine string `yaml:"engine,omitempty" json:"engine,omitempty"`
	// APIKey is the provider's API key (type=brave only). Literal value or,
	// strongly preferred, a ${secret:name} reference resolved at startup
	// (internal/secrets/refs.go) — same convention as every other credential
	// field in this config (e.g. llm.api_key_ref, goose.api_key_ref).
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	// NumResults is the default number of results for this provider (1–20).
	// 0 falls back to WebSearchConfig's global default.
	NumResults int `yaml:"num_results,omitempty" json:"num_results,omitempty"`
	// CacheTTLSeconds overrides the global cache TTL for this provider's
	// results. 0 falls back to WebSearchConfig.CacheTTLSeconds.
	CacheTTLSeconds int `yaml:"cache_ttl_seconds,omitempty" json:"cache_ttl_seconds,omitempty"`
}

// WebSearchConfig holds web search MCP injection configuration (BL372,
// extended to a multi-provider registry in BL391). When Enabled is true,
// datawatch injects a `mcp-search` stdio MCP server into opencode and goose
// sessions, giving agents a `web_search` tool that proxies queries through
// the configured provider(s).
type WebSearchConfig struct {
	// Enabled turns on web search injection for opencode and goose sessions.
	Enabled bool `yaml:"enabled"`
	// Providers is the list of configured search backends. Tried in
	// Priority order; first enabled provider to return results wins.
	Providers []SearchProvider `yaml:"providers,omitempty" json:"providers,omitempty"`
	// CacheEnabled turns on the in-memory TTL result cache (BL391) — avoids
	// paying for/re-scraping an identical query within the TTL window.
	// Default true.
	CacheEnabled bool `yaml:"cache_enabled,omitempty" json:"cache_enabled,omitempty"`
	// CacheTTLSeconds is the default cache lifetime for a query result.
	// Default 900 (15 minutes). A provider may override via its own
	// CacheTTLSeconds.
	CacheTTLSeconds int `yaml:"cache_ttl_seconds,omitempty" json:"cache_ttl_seconds,omitempty"`

	// --- Legacy single-provider fields (pre-BL391) ---
	// Deprecated: set Providers instead. Kept so existing config.yaml files
	// keep working — applyDefaults migrates these into a single Providers[0]
	// entry named "default" the first time a legacy config is loaded with no
	// Providers list present. Never read directly once migration has run.
	Provider   string `yaml:"provider,omitempty" json:"provider,omitempty"`
	URL        string `yaml:"url,omitempty" json:"url,omitempty"`
	Engine     string `yaml:"engine,omitempty" json:"engine,omitempty"`
	NumResults int    `yaml:"num_results,omitempty" json:"num_results,omitempty"`
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

// LSPServerConfig describes a language server for OpenCode sessions.
// Fields map directly to the entries inside opencode.json's "lsp" object.
type LSPServerConfig struct {
	// Command is the executable + arguments to start the language server.
	Command []string `yaml:"command" json:"command"`
	// Extensions limits the server to files with these suffixes (e.g. ".go").
	// Omit to let OpenCode apply its own heuristic.
	Extensions []string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	// Env injects extra environment variables into the server process.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// LSPConfig is the top-level registry of Language Server Protocol presets.
// Operators add/remove entries freely; each key becomes one option in the
// session-creation "Language (LSP)" dropdown.
type LSPConfig struct {
	// Servers maps language name → LSP server definition.
	// Defaults seed common servers; none are enabled unless the operator
	// picks one in the session-creation form (or sets it per-session via API).
	Servers map[string]LSPServerConfig `yaml:"servers"`
}

// OpenCodeProvider holds the API key for a cloud provider accessible via opencode.
// The key is stored in the daemon secrets store under "opencode_provider_<name>".
type OpenCodeProvider struct {
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`
}

// OpenCodeConfig holds opencode TUI backend configuration.
type OpenCodeConfig struct {
	Enabled bool   `yaml:"enabled"`
	Binary  string `yaml:"binary"`
	// DefaultModel is the model written to opencode.json when a session is
	// started without an explicit model selection. Defaults to the first
	// free built-in model ("opencode/big-pickle"). Override with any model
	// ID returned by `opencode models` (or a paid provider model once
	// credentials are configured via `opencode providers login`).
	DefaultModel string `yaml:"default_model,omitempty"`
	ConsoleCols  int    `yaml:"console_cols,omitempty"`
	ConsoleRows  int    `yaml:"console_rows,omitempty"`
	OutputMode   string `yaml:"output_mode,omitempty"`
	InputMode    string `yaml:"input_mode,omitempty"`
	// Providers holds API keys for cloud providers (anthropic, openai, google).
	// Keys are stored in the daemon secrets store and never returned in plain text.
	Providers map[string]OpenCodeProvider `json:"providers,omitempty" yaml:"providers,omitempty"`

	// OllamaChunkTimeoutSec (B89, v8.33.x) — written into opencode.json's
	// provider.ollama.options.chunkTimeout (converted to ms). Max idle gap
	// between streamed SSE chunks before opencode aborts the request.
	// OpenCode's own default is 300s (5 min); observed qwen3 "thinking"
	// pauses on large local models ran 6m+ with zero streamed output,
	// tripping the default and aborting mid-generation with "SSE read
	// timed out" — the opencode session then hangs waiting for output that
	// will never arrive, since the underlying request was already killed.
	// 0 = leave opencode's built-in default in effect.
	OllamaChunkTimeoutSec int `yaml:"ollama_chunk_timeout_sec,omitempty"`

	// OllamaHeaderTimeoutSec (B89, v8.33.x) — written into opencode.json's
	// provider.ollama.options.headerTimeout (converted to ms). Max wait for
	// the first response header, i.e. how long Ollama is given to cold-load
	// a model into VRAM before opencode gives up. OpenCode's own default
	// (300s) is tight for the largest locally-hosted models. 0 = leave
	// opencode's built-in default in effect.
	OllamaHeaderTimeoutSec int `yaml:"ollama_header_timeout_sec,omitempty"`

	// BuildTemperature (2026-10-15, live reliability investigation) —
	// written into opencode.json's agent.build.temperature. Candidate fix
	// for small open models stalling at "awaiting_input" without ever
	// calling a tool, confirmed live on qwen3.8:27b at Ollama's default
	// temperature=1.0 for this model. 0 = leave the model/opencode default
	// in effect. Operator-set only for now (no REST/MCP/CLI surface yet —
	// this is a live experiment, not a certified feature); set directly
	// via a config-patch or this file while the hypothesis is tested.
	BuildTemperature float64 `yaml:"build_temperature,omitempty"`
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
	Domain          string `yaml:"domain"`            // authoritative subdomain (e.g. "ctl.example.com")
	Listen          string `yaml:"listen"`            // server: UDP/TCP bind address (default ":53")
	Upstream        string `yaml:"upstream"`          // client: resolver address (e.g. "8.8.8.8:53")
	Secret          string `yaml:"secret"`            // HMAC-SHA256 shared secret
	TTL             int    `yaml:"ttl"`               // DNS response TTL in seconds (0 = non-cacheable)
	MaxResponseSize int    `yaml:"max_response_size"` // max response bytes before truncation (default 512)
	PollInterval    string `yaml:"poll_interval"`     // client polling interval (default "5s")
	RateLimit       int    `yaml:"rate_limit"`        // max queries per IP per minute (default 30, 0 = unlimited)
}

// AcmeConfig controls the native ACME / Let's Encrypt subsystem (BL397).
// The daemon itself performs the ACME handshake and renewal — the
// operator never leaves datawatch for certificate management. See
// docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md.
type AcmeConfig struct {
	// Enabled is the master switch, mirroring server.tls_enabled's shape.
	Enabled bool `yaml:"enabled"`

	// Endpoint selects the ACME directory: "staging" (default, untrusted
	// certs, generous rate limits — use this first) or "production"
	// (browser-trusted certs, strict rate limits: 5 certs/7 days per exact
	// domain set, 5 auth-failures/hour). Flipping staging -> production is
	// a config save, not a code event.
	Endpoint string `yaml:"endpoint"`

	// Domains are the hostnames to request a single multi-SAN cert for
	// (one ACME order, not one order per name — e.g. the main server
	// hostname plus the proxy-sandbox hostname share one cert since the
	// sandbox listener reuses the main cert automatically).
	Domains []string `yaml:"domains"`

	// Method is the ACME validation method: "http01" (default, no DNS
	// management needed, wildcard not supported) or "dns01" (BL397 Phase
	// 2 — needed for wildcard domains or when inbound port 80 isn't
	// available; requires dns01.provider + dns01.token_secret).
	Method string `yaml:"method"`

	HTTP01 AcmeHTTP01Config `yaml:"http01,omitempty"`
	DNS01  AcmeDNS01Config  `yaml:"dns01,omitempty"`

	// RenewalDays triggers a renewal when the cert's remaining life drops
	// to this many days. Default 30 (90-day LE certs, ~certbot/Caddy
	// convention — NOT the 66 the originating research PRD proposed; 66
	// renews at only 24 days of cert age, far more aggressive than
	// standard practice).
	RenewalDays int `yaml:"renewal_days"`

	Retry AcmeRetryConfig `yaml:"retry"`
	Apply AcmeApplyConfig `yaml:"apply"`
}

// AcmeHTTP01Config holds HTTP-01-specific settings.
type AcmeHTTP01Config struct {
	// Listen is the optional bind address for the HTTP-01 challenge
	// responder. Empty means it's served through the daemon's existing
	// HTTP mux on the normal port-80-redirect listener — no second port.
	Listen string `yaml:"listen,omitempty"`
}

// AcmeDNS01Config holds DNS-01-specific settings (BL397 Phase 2).
// Only the B2 "scoped provider token" shape is implemented — a
// zone-scoped credential for one provider, resolved via the secrets
// manager. B1 (delegated-zone via dns_channel) is deferred; see the
// BL397 plan doc.
type AcmeDNS01Config struct {
	// Provider selects the DNS-01 provider plugin. Only "cloudflare" is
	// wired in this build (lego supports 180+; add more as needed).
	Provider string `yaml:"provider"`
	// TokenSecret is a ${secret:name} reference to a ZONE-SCOPED
	// credential (e.g. Cloudflare's "Zone > DNS > Edit" API token on one
	// zone) — never the account-global key. Resolved at Manager-
	// construction time and held only in memory; the config.yaml on disk
	// always keeps the unresolved ${secret:...} reference, even after
	// config.Save — see internal/acme's dns01TokenSecretRef handling.
	TokenSecret string `yaml:"token_secret,omitempty"`
	// ZoneID scopes the credential to one zone (Cloudflare's CF_Zone_ID
	// equivalent — required so a leaked token's blast radius is one
	// zone, not every zone the account token could otherwise reach).
	ZoneID string `yaml:"zone_id,omitempty"`
}

// AcmeRetryConfig controls retry/backoff behavior after a failed order.
type AcmeRetryConfig struct {
	// IntervalMinutes is the retry cadence after a failed attempt.
	// Default 30.
	IntervalMinutes int `yaml:"interval_minutes"`
	// MaxConsecutiveFailures pauses the renewal loop (alert + pause)
	// after this many consecutive failures, so a persistent misconfig
	// doesn't keep burning Let's Encrypt's failure-rate limits. Default 5
	// (at 30-minute spacing, 5 failures span 2.5h — safely under LE's
	// 5-auth-failures-per-hour cap).
	MaxConsecutiveFailures int `yaml:"max_consecutive_failures"`
}

// AcmeApplyConfig controls what happens after a cert is successfully
// issued or renewed.
type AcmeApplyConfig struct {
	// HotSwap selects the cert-apply mechanism. false (default): write
	// the PEM then restart the daemon via the existing restartFn so the
	// TLS listener re-loads it at startup — one short window per issue/
	// renewal. true (BL397 Phase 3): the listener's GetCertificate
	// callback (internal/tlsutil) already reloads the cert from disk on
	// mtime change at every handshake, so most renewals need no restart
	// at all — the one exception is the FIRST apply after switching to
	// ACME (or the very first issue), where the cert PATH itself is
	// changing and a running listener can't hot-reload a path it was
	// never watching; that one restarts regardless of this setting.
	HotSwap bool `yaml:"hot_swap"`
	// UpdateMCPCert also points mcp.tls_cert/tls_key at the same issued
	// cert. Default true — MCP's SSE endpoint binds to the same public
	// host as the main server (just a different port), and a TLS cert is
	// host-scoped, not port-scoped, so one cert legitimately covers both.
	UpdateMCPCert bool `yaml:"update_mcp_cert"`
}

// PushConfig controls validation of outbound mobile push-notification
// endpoints registered via POST /api/push/register (BL394 SSRF fix).
//
// Loopback, link-local, and cloud-metadata addresses are ALWAYS rejected
// regardless of these flags — there is no legitimate reason a push
// endpoint would ever need to be one of those. Both flags default false,
// which keeps today's working self-hosted-push setups (ntfy/Gotify on a
// LAN or Tailscale address, sometimes over plain http) working unchanged;
// operators who want a stricter posture opt in.
//
// See internal/server/push.go's top-of-file comment for the full design
// rationale and the two-registration-shape split this depends on.
type PushConfig struct {
	// AllowInsecureEndpoints, when false (default), rejects a registered
	// push endpoint whose scheme is not https://. Set true to allow plain
	// http:// (e.g. a self-hosted ntfy instance on a LAN with no TLS).
	AllowInsecureEndpoints bool `yaml:"allow_insecure_endpoints"`
	// BlockPrivateEndpoints, when true, additionally rejects endpoints
	// resolving to RFC1918 / Tailscale CGNAT (100.64.0.0/10) / IPv6 ULA
	// (fc00-:fdff range) addresses. Default false — that's exactly where most
	// real self-hosted push distributors live, so blocking it by default
	// would break the operator's own working setup.
	BlockPrivateEndpoints bool `yaml:"block_private_endpoints"`

	// APNs holds Apple Push Notification service dispatch settings
	// (BL397 Phase 4 / BL335). Separate from the UnifiedPush/ntfy webhook
	// mechanism above — APNs devices are registered via
	// POST /api/devices/register (kind=apns) and tracked in
	// internal/devices.Store, dispatched on alert fire.
	APNs APNsConfig `yaml:"apns,omitempty"`
}

// AuditConfig (GH#201) controls the HTTP access / WS lifecycle /
// auth-failure log (separate file from the operator action log, since
// access-log volume is much higher and less individually significant)
// and retention for both logs. Never includes the Authorization header
// or any token value — only request metadata and the resolved principal.
type AuditConfig struct {
	// AccessLogEnabled, default true, turns on logging of every HTTP
	// request/response (method, path, status, remote IP, user agent,
	// resolved principal), every auth failure, and every WebSocket
	// connect/disconnect to <data_dir>/access.log.
	AccessLogEnabled *bool `yaml:"access_log_enabled,omitempty"`
	// RetentionDays prunes entries older than this from both the
	// operator audit log and the access log. 0 = use the default (30).
	// Negative disables pruning (keep forever).
	RetentionDays int `yaml:"retention_days,omitempty"`
	// CEFMirrorEnabled (AGENT.md's Audit Logging Rule), default false,
	// additionally writes a CEF-formatted line (<file>.cef) alongside
	// every JSON-lines entry in both audit.log and access.log, for
	// forwarding to a SIEM (Splunk/QRadar/ArcSight/Sentinel). Opt-in —
	// most operators don't run a SIEM and don't need the second file.
	CEFMirrorEnabled bool `yaml:"cef_mirror_enabled,omitempty"`
}

// AccessLogEnabledOrDefault returns the effective access-log setting,
// defaulting to true when unset.
func (a AuditConfig) AccessLogEnabledOrDefault() bool {
	return a.AccessLogEnabled == nil || *a.AccessLogEnabled
}

// RetentionDaysOrDefault returns the effective retention window in days,
// defaulting to 30 when unset (0). A negative value means "never prune".
func (a AuditConfig) RetentionDaysOrDefault() int {
	if a.RetentionDays == 0 {
		return 30
	}
	return a.RetentionDays
}

// APNsConfig holds Apple Push Notification service provider-token
// credentials. See docs/howto/apns-push.md for setup.
type APNsConfig struct {
	// Enabled is the master switch.
	Enabled bool `yaml:"enabled"`
	// KeyID is the 10-character APNs Auth Key ID from the Apple Developer
	// portal (Certificates, Identifiers & Profiles -> Keys).
	KeyID string `yaml:"key_id"`
	// TeamID is the 10-character Apple Developer Team ID.
	TeamID string `yaml:"team_id"`
	// BundleID is the iOS app's bundle identifier — sent as the
	// apns-topic header on every push.
	BundleID string `yaml:"bundle_id"`
	// KeyPath is a filesystem path to the .p8 private key file Apple
	// issues for the Auth Key. Mutually exclusive with KeySecret —
	// KeySecret takes precedence if both are set.
	KeyPath string `yaml:"key_path,omitempty"`
	// KeySecret is a ${secret:name} reference to the .p8 key's PEM
	// contents, for operators who'd rather not have the raw key sitting
	// as a plaintext file (same posture as acme.dns01.token_secret).
	KeySecret string `yaml:"key_secret,omitempty"`
	// Sandbox sends to Apple's sandbox APNs environment
	// (api.sandbox.push.apple.com) instead of production
	// (api.push.apple.com) — matches the APNs environment a TestFlight
	// build's device token was issued for.
	Sandbox bool `yaml:"sandbox"`
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
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
	ChatID  int64  `yaml:"chat_id"`
	// AutoManageGroup creates a group named after hostname if ChatID is zero.
	// Note: Telegram bots cannot create groups; manual setup is required.
	AutoManageGroup bool `yaml:"auto_manage_group"`
}

// MatrixConfig holds Matrix homeserver configuration.
// AccessToken MUST be a ${secret:name} reference per the Secrets-Store Rule
// (BL241). Plaintext tokens are rejected at config-load time.
type MatrixConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Homeserver string `yaml:"homeserver" json:"homeserver"`
	UserID     string `yaml:"user_id" json:"user_id"`
	// AccessToken must use ${secret:matrix-access-token} syntax.
	AccessToken string `yaml:"access_token" json:"access_token,omitempty"`
	RoomID      string `yaml:"room_id" json:"room_id"`
	// AutoManageRoom creates a room named after hostname if RoomID is empty.
	AutoManageRoom bool                `yaml:"auto_manage_room" json:"auto_manage_room"`
	DeviceID       string              `yaml:"device_id,omitempty" json:"device_id,omitempty"`
	DeviceName     string              `yaml:"device_name,omitempty" json:"device_name,omitempty"`
	Encryption     MatrixEncryptionCfg `yaml:"encryption,omitempty" json:"encryption,omitempty"`
	AS             MatrixASCfg         `yaml:"application_service,omitempty" json:"application_service,omitempty"`
	// Bridges is reserved for v2 per-bridge ACL/policy config (BL241 out-of-scope).
	Bridges MatrixBridgesCfg `yaml:"bridges,omitempty" json:"bridges,omitempty"`
}

// MatrixEncryptionCfg controls E2EE behaviour (BL241 P2+).
type MatrixEncryptionCfg struct {
	// Mode: "cleartext" (default), "warn" (log warning + skip encrypted rooms),
	// or "required" (reject rooms without encryption — P2+ only).
	Mode        string `yaml:"mode,omitempty" json:"mode,omitempty"`
	CryptoStore string `yaml:"crypto_store,omitempty" json:"crypto_store,omitempty"`
}

// MatrixASCfg holds Application Service registration config.
type MatrixASCfg struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	RegistrationFile string `yaml:"registration_file,omitempty" json:"registration_file,omitempty"`
	// ASToken and HSToken must use ${secret:name} syntax per the Secrets-Store Rule.
	ASToken    string `yaml:"as_token,omitempty" json:"as_token,omitempty"`
	HSToken    string `yaml:"hs_token,omitempty" json:"hs_token,omitempty"`
	Namespace  string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	ListenAddr string `yaml:"listen_addr,omitempty" json:"listen_addr,omitempty"`
}

// MatrixBridgesCfg is reserved for v2 per-bridge policy (empty in v1).
type MatrixBridgesCfg struct{}

// SecretsConfig (BL242) — centralized secrets manager backend selection.
// BL267 (v6.15.0) — Vault backend added; per-secret backend via Secret.Backend
// field, so multiple backends are active simultaneously and any secret picks
// its own backend (operator chose this model; "Vault may only be used for a
// single agent, not all of datawatch").
type SecretsConfig struct {
	// Backend selects the DEFAULT secrets store for newly-created secrets
	// when the operator doesn't pick one explicitly:
	//   "builtin"     — AES-256-GCM encrypted JSON (default)
	//   "keepass"     — KeePass database via keepassxc-cli
	//   "onepassword" — 1Password vault via op CLI
	//   "vault"       — HashiCorp Vault / OpenBao (BL267, v6.15.0)
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`

	// KeePass backend (backend=keepass)
	KeePassDB       string `yaml:"keepass_db,omitempty" json:"keepass_db,omitempty"`
	KeePassPassword string `yaml:"keepass_password,omitempty" json:"keepass_password,omitempty"` // prefer DATAWATCH_KEEPASS_PASSWORD env
	KeePassBinary   string `yaml:"keepass_binary,omitempty" json:"keepass_binary,omitempty"`     // default: "keepassxc-cli"
	KeePassGroup    string `yaml:"keepass_group,omitempty" json:"keepass_group,omitempty"`       // optional group scope

	// 1Password backend (backend=onepassword)
	OPBinary string `yaml:"op_binary,omitempty" json:"op_binary,omitempty"` // default: "op"
	OPVault  string `yaml:"op_vault,omitempty" json:"op_vault,omitempty"`   // optional vault name/ID
	OPToken  string `yaml:"op_token,omitempty" json:"op_token,omitempty"`   // prefer DATAWATCH_OP_TOKEN env

	// Vault backend (backend=vault, BL267 v6.15.0). All fields are
	// settable via YAML; Token + Namespace also accept ${secret:name}
	// references resolved against the built-in store at startup so
	// operators can hold the Vault token in datawatch's own AES store.
	Vault VaultConfig `yaml:"vault,omitempty" json:"vault,omitempty"`
}

// VaultConfig (BL267, v6.15.0) — HashiCorp Vault / OpenBao backend settings.
// Design decisions captured in the BL267 interview (see CHANGELOG v6.15.0):
//   - Static-token auth only in v1; AppRole + Kubernetes deferred (BL281).
//   - KV v2 only.
//   - Flat path layout default; tag-aware opt-in (BL283 deferred for full
//     per-actor + Vault-side enforcement).
//   - Actor-aware write gate via the existing CallerCtx (operator/mcp/channel
//     may write; agents read-only).
//   - No cache (BL282 deferred for cache + invalidate API).
//   - Fail-closed on Vault unreachable (BL285 deferred for alerting + SRE).
//   - Defense-in-depth scoping: daemon CheckScope first, then Vault read.
type VaultConfig struct {
	// Address of the Vault server, e.g. "https://vault.internal:8200".
	Address string `yaml:"address,omitempty" json:"address,omitempty"`

	// Namespace is the Vault Enterprise namespace (X-Vault-Namespace
	// header). Empty = Vault Community / OpenBao.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// AuthMethod selects how the daemon authenticates to Vault.
	// v1 supports "token" only; "approle" / "kubernetes" land in BL281.
	AuthMethod string `yaml:"auth_method,omitempty" json:"auth_method,omitempty"`

	// Token is the Vault auth token (auth_method=token). Accepts a
	// ${secret:name} reference into the built-in store. Prefer the
	// DATAWATCH_VAULT_TOKEN env var when running headless.
	Token string `yaml:"token,omitempty" json:"token,omitempty"`

	// KVMount is the path the KV-v2 engine is mounted at (default "secret").
	KVMount string `yaml:"kv_mount,omitempty" json:"kv_mount,omitempty"`

	// PathPrefix is the sub-folder under the KV mount where datawatch
	// secrets live. Default "datawatch". Empty = secrets land directly
	// under the mount (operator's call).
	PathPrefix string `yaml:"path_prefix,omitempty" json:"path_prefix,omitempty"`

	// PathLayout: "flat" (default) puts every secret at <mount>/data/<prefix>/<name>;
	// "tag_aware" inserts the secret's first tag as a sub-folder so a
	// Vault policy can scope per tag (BL283 will land per-actor tokens).
	PathLayout string `yaml:"path_layout,omitempty" json:"path_layout,omitempty"`

	// WritePolicy: "operator" (default) lets actor=="operator" plus
	// channel:* / mcp write; agents are always read-only. "none" makes
	// the backend read-only for all actors. "any" allows writes
	// regardless of actor (operator-managed scripts; rare).
	WritePolicy string `yaml:"write_policy,omitempty" json:"write_policy,omitempty"`

	// TLSCAFile is the path to a PEM CA bundle to trust for the Vault
	// TLS handshake (additive to system CAs). Empty = system CAs only.
	TLSCAFile string `yaml:"tls_ca_file,omitempty" json:"tls_ca_file,omitempty"`

	// TLSSkipVerify disables TLS cert validation. Default false. OK for
	// local-dev bootstrap only; document the risk.
	TLSSkipVerify bool `yaml:"tls_skip_verify,omitempty" json:"tls_skip_verify,omitempty"`

	// RequestTimeout caps every Vault HTTP call. Default 10s.
	RequestTimeout time.Duration `yaml:"request_timeout,omitempty" json:"request_timeout,omitempty"`

	// UnreachableAlertAfterSeconds is how long (whole seconds) the Vault must
	// stay unreachable before the Vault-unreachable alert is raised. Unset
	// (nil) reads DefaultVaultUnreachableAlertAfterSeconds (120); an explicit
	// 0 turns the alert off. Negative values are refused.
	UnreachableAlertAfterSeconds *int `yaml:"unreachable_alert_after_seconds,omitempty" json:"unreachable_alert_after_seconds,omitempty"`
}

// DefaultVaultUnreachableAlertAfterSeconds is the default for
// secrets.vault.unreachable_alert_after_seconds.
const DefaultVaultUnreachableAlertAfterSeconds = 120

// EffectiveUnreachableAlertAfterSeconds returns the threshold in effect: unset
// reads the 120-second default, an explicit 0 disables the alert.
func (c VaultConfig) EffectiveUnreachableAlertAfterSeconds() int {
	if c.UnreachableAlertAfterSeconds == nil {
		return DefaultVaultUnreachableAlertAfterSeconds
	}
	return *c.UnreachableAlertAfterSeconds
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
	Enabled    bool   `yaml:"enabled"`
	AccountSID string `yaml:"account_sid"`
	AuthToken  string `yaml:"auth_token"`
	FromNumber string `yaml:"from_number"`
	// ToNumber is the phone number to send messages to (e.g. +12125551234).
	ToNumber string `yaml:"to_number"`
	// WebhookAddr is the address for the incoming SMS webhook (e.g. ":9003").
	WebhookAddr string `yaml:"webhook_addr"`
	// WebhookPublicURL (SEC-005) is the exact URL configured as this
	// number's "A MESSAGE COMES IN" webhook in the Twilio console (e.g.
	// "https://host.example.net/sms") — required to verify the X-Twilio-Signature
	// header, which Twilio computes over this exact URL plus the sorted
	// POST params. Left empty, the webhook accepts any POST with no
	// signature check at all (matches this codebase's github_webhook.secret
	// convention: unset is allowed, but loud about the risk).
	WebhookPublicURL string `yaml:"webhook_public_url"`
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

// ImapMcpConfig holds imap-mcp backend configuration for the email command channel.
// Operator must run imap-mcp serve independently; datawatch connects to it via HTTP.
type ImapMcpConfig struct {
	Enabled       bool   `yaml:"enabled"`
	URL           string `yaml:"url"`            // imap-mcp API base URL (e.g. http://localhost:8765)
	Account       string `yaml:"account"`        // imap-mcp account name; empty = use default
	SubjectPrefix string `yaml:"subject_prefix"` // prefix for outbound reply subjects; default "datawatch"
	// Token (GH#203) is the bearer token imap-mcp >= 0.5.3 requires on
	// every request except GET /api/health. Accepts a ${secret:name}
	// reference (resolved by secretspkg.ResolveConfig at startup) so the
	// value never lives in config.yaml in plaintext. Needs the "read"
	// and "send" scopes on the imap-mcp side. Empty = no Authorization
	// header sent (imap-mcp <= 0.5.2, or auth disabled there).
	Token string `yaml:"token,omitempty"`
	// To (found live 2026-10-10) is the fixed outbound recipient
	// address. Without it, every outbound send was addressed to the
	// literal string "imap_mcp" (the generic comm Router's groupID
	// label for this channel, which has no real meaning for email) and
	// SMTP correctly bounced it as 5.1.1 user unknown. Required for any
	// outbound reply/notification to actually deliver; empty leaves the
	// pre-fix bounce-prone behavior unchanged rather than hard-failing.
	To string `yaml:"to,omitempty"`
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
	// ImageDir (BL394 security fix) scopes the webhook's "image_url as a
	// local file path" attachment feature to one operator-designated
	// directory. Empty (the default) disables that feature entirely --
	// before this fix, any local path was readable with no restriction
	// at all, from a POST that may not even require a token (Token is
	// optional). A "data:<mime>;base64,..." image_url always works
	// regardless of this setting; it never touches the filesystem.
	ImageDir string `yaml:"image_dir,omitempty"`
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

	// AcknowledgeUnauthenticated silences the startup warning for
	// listeners reachable without authentication; it changes no
	// listener. Default: false.
	AcknowledgeUnauthenticated bool `yaml:"acknowledge_unauthenticated"`

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

	// SessionSummaryTerminalLimit caps how many terminal (complete, failed,
	// killed) sessions GET /api/sessions?view=summary returns, newest by
	// updated_at first; non-terminal sessions are never dropped. 0 = no cap.
	// Default: 50. Retention decision: the unfiltered GET /api/sessions list
	// is intentionally NOT capped so existing clients keep working; this
	// limit applies only to view=summary.
	SessionSummaryTerminalLimit int `yaml:"session_summary_terminal_limit"`

	// SessionSummaryTextChars is the character (rune) length that
	// GET /api/sessions?view=summary truncates task, prompt_context,
	// last_response and last_summary_long to. Default: 200, minimum 20.
	SessionSummaryTextChars int `yaml:"session_summary_text_chars"`

	// WSSessionListDefault is the WebSocket session-list mode for clients
	// that never send a hello message: "full" (the whole list on every
	// change) or "delta" (only per-session session_state /
	// session_removed frames; the client fetches the initial list over
	// REST). Default: "full". Read live at each session broadcast.
	WSSessionListDefault string `yaml:"ws_session_list_default"`

	// WSOutputScopeDefault is the WebSocket terminal-output mode for
	// clients that do not choose one in hello: "all" (every session's
	// output / raw_output / pane_capture / chat_message frames) or
	// "subscribed" (only sessions the client subscribed to). Default:
	// "all". Read live for every output frame.
	WSOutputScopeDefault string `yaml:"ws_output_scope_default"`

	// APIGzip compresses large JSON-ish /api/* responses with gzip when
	// the client sends Accept-Encoding: gzip. Default: true. Read on every
	// request, so a config change applies without a restart.
	APIGzip bool `yaml:"api_gzip"`

	// APIGzipMinBytes is the response size (bytes) a /api/* body must
	// exceed before it is compressed; smaller bodies are sent unchanged.
	// Default: 1024, minimum 0.
	APIGzipMinBytes int `yaml:"api_gzip_min_bytes"`

	// SuppressActiveToasts hides toast notifications for the currently viewed session
	// (e.g. state change toasts while you're watching the output). Default: true.
	SuppressActiveToasts bool `yaml:"suppress_active_toasts"`

	// AllowedProjectRoots lists extra directories a request-supplied project
	// directory may lie under, on top of the two that are always allowed: the
	// PRD worktree base (autonomous.worktree_dir) and session.workspace_root.
	// A project directory outside every root is refused at session start, PRD
	// creation and the pipeline test runner. Empty (the default) adds nothing.
	AllowedProjectRoots []string `yaml:"allowed_project_roots,omitempty" json:"allowed_project_roots,omitempty"`

	// ProxySandboxPort (BL394 security review, docs/plans/2026-10-03-
	// bl394-security-findings-review.md §6) — a second listener, bound
	// to this port on the same host/cert, that serves ONLY /remote/ (a
	// proxied federation peer's own PWA) and /api/proxy/ (the matching
	// REST+WS forwarding), under a distinct origin from the main PWA.
	// This is what actually stops a compromised/malicious peer's proxied
	// JS from reading the real operator's auth token out of the main
	// origin's localStorage — before this, /remote/ was served on the
	// SAME origin as the operator's own session. Default: Port+1.
	// Set to -1 to disable the sandbox listener (falls back to the old,
	// same-origin behavior — not recommended).
	//
	// Deliberately a port today, not a per-peer subdomain: a subdomain
	// needs real DNS/ingress the operator may not have, while a second
	// port works out of the box on any deployment. The design keeps
	// subdomains a drop-in upgrade for later (see proxySandboxOrigin in
	// internal/server/proxy_sandbox.go) — every caller already asks "what
	// origin should I use for proxying THIS peer" rather than hardcoding
	// a port, so switching the one function that answers that question is
	// the only change a future per-peer-subdomain scheme would need.
	ProxySandboxPort int `yaml:"proxy_sandbox_port,omitempty"`
}

// MCPResourcesConfig controls MCP resource exposure (BL302 S1).
type MCPResourcesConfig struct {
	// Enabled controls whether MCP resources (datawatch://) are registered and served.
	// Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// MCPSamplingTriggersConfig controls which built-in sampling triggers are active.
type MCPSamplingTriggersConfig struct {
	// AlertTriage enables sampling on alert creation (severity ≥ WARNING). Default false.
	AlertTriage bool `yaml:"alert_triage" json:"alert_triage"`
	// AnomalyAnalysis enables sampling when the gap-watcher fires. Default false.
	AnomalyAnalysis bool `yaml:"anomaly_analysis" json:"anomaly_analysis"`
	// MorningBriefing enables the daily briefing cron trigger. Default false.
	MorningBriefing bool `yaml:"morning_briefing" json:"morning_briefing"`
}

// MCPSamplingConfig controls daemon-initiated sampling (BL302 S3).
type MCPSamplingConfig struct {
	// Enabled controls whether sampling requests are dispatched. Default true.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// PluginTriggers lists plugin events that may trigger sampling
	// (e.g. "on_complete", "on_error"). Empty = no plugin sampling.
	PluginTriggers []string `yaml:"plugin_triggers,omitempty" json:"plugin_triggers,omitempty"`
	// CouncilEnabled wires Claude Code as a deliberation participant when
	// Council `include_claude_code: true` is set. Default false.
	CouncilEnabled bool `yaml:"council_enabled" json:"council_enabled"`
	// Triggers configures individual built-in sampling triggers.
	Triggers MCPSamplingTriggersConfig `yaml:"triggers" json:"triggers"`
}

// MCPElicitationConfig controls daemon-initiated elicitation (BL302 S3).
type MCPElicitationConfig struct {
	// Enabled controls whether elicitation requests are dispatched. Default true.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// MCPPromptsConfig controls MCP prompt exposure (BL302 S4).
type MCPPromptsConfig struct {
	// Enabled controls whether MCP prompts are registered and served. Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// MCPConfig holds MCP server configuration for IDE and remote AI integrations.
type MCPConfig struct {
	// Enabled controls whether the MCP server is available via `datawatch mcp`.
	Enabled bool `yaml:"enabled"`

	// Resources controls the MCP resources surface (BL302).
	Resources MCPResourcesConfig `yaml:"resources" json:"resources"`

	// Sampling controls daemon-initiated sampling (BL302 S3).
	Sampling MCPSamplingConfig `yaml:"sampling" json:"sampling"`

	// Elicitation controls daemon-initiated elicitation (BL302 S3).
	Elicitation MCPElicitationConfig `yaml:"elicitation" json:"elicitation"`

	// Prompts controls the MCP prompts surface (BL302 S4).
	Prompts MCPPromptsConfig `yaml:"prompts" json:"prompts"`

	// SSEEnabled starts an HTTP/SSE MCP server for remote AI clients in addition
	// to the stdio transport used by local IDE clients (Cursor, Claude Desktop).
	SSEEnabled bool `yaml:"sse_enabled"`

	// SSEHost and SSEPort set the bind address for the SSE server (default: 0.0.0.0:8081).
	SSEHost string `yaml:"sse_host"`
	SSEPort int    `yaml:"sse_port"`

	// SSEBindTimeout is how long ServeSSE retries a failed TCP bind with
	// exponential backoff before giving up (BL444; default 120s, zero or
	// negative is treated as the default).
	SSEBindTimeout time.Duration `yaml:"sse_bind_timeout"`

	// PublicURL is the externally-reachable base URL of the SSE server,
	// for example "https://host.tailnet.example.net:8081". When set, it is the
	// base URL advertised to clients in the SSE "endpoint" event instead
	// of the bind address, which is required when the SSE server is bound
	// to localhost behind a TLS-terminating reverse proxy (Tailscale
	// serve, Caddy, nginx). A trailing slash is ignored. Empty keeps the
	// default of <scheme>://<first sse_host>:<sse_port>. Mirrors
	// Server.PublicURL.
	PublicURL string `yaml:"public_url,omitempty"`

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

	// AllowSelfConfig (BL110; BL412 option C followup) is the alias for
	// the builtin "self-config" capability group (defined in
	// internal/federation/capabilities.go). Tri-state: nil (the key
	// absent from the YAML) and an explicit true both read as ENABLED —
	// that is the default; an explicit false disables it. Use
	// SelfConfigEnabled() rather than reading the field directly.
	//
	// With the alias enabled:
	//   - AI sessions calling the config_set MCP tools (and the named
	//     *_config_set tools that cover the same non-secret daemon
	//     settings — see federation.IsConfigWriteTool) are allowed;
	//     every mutation is still logged to the daemon's stderr +
	//     audit/config-self-modify.jsonl, and the setting itself
	//     remains bootstrap-protected (it cannot be flipped via
	//     config_set).
	//   - spawned sessions' scoped credentials are minted with the
	//     builtin "self-config" group added to the session-default
	//     grant, so the same tool set is also gated the capability way
	//     over the REST chokepoint (POST /api/mcp/call).
	//
	// Operators who want it off write allow_self_config: false; an
	// explicit grant (session.capabilities) can remove or narrow it.
	// Design: docs/plans/2026-10-29-bl412-self-config-gate-proposal.md;
	// see also docs/howto/federation-cbac.md.
	AllowSelfConfig *bool `yaml:"allow_self_config,omitempty"`

	// AllowSensitiveWrites controls the sensitive write tool class of the
	// MCP tool classification (tools that write secrets, credentials or
	// other sensitive state). Tri-state: nil (the key absent from the
	// YAML) and an explicit true both read as ENABLED — the default keeps
	// today's behaviour, sensitive tools allowed; an explicit false
	// disables the class. Use SensitiveWritesEnabled() rather than
	// reading the field directly. No default-flip migration is applied:
	// an absent key stays absent.
	AllowSensitiveWrites *bool `yaml:"allow_sensitive_writes,omitempty"`

	// SelfConfigAuditPath (BL110) is the file the self-modify audit
	// stream writes to. Default <data_dir>/audit/config-self-modify.jsonl.
	// Empty disables the file sink (events still go to stderr).
	SelfConfigAuditPath string `yaml:"self_config_audit_path,omitempty"`

	// SelfConfigAuditDenied (BL110 extension) controls whether DENIED
	// config-write attempts are recorded by the shared audit sink in
	// addition to allowed writes. Absent (nil) reads as true — an
	// attempt is an attempt; operators who want allowed-only logs write
	// self_config_audit_denied: false.
	SelfConfigAuditDenied *bool `yaml:"self_config_audit_denied,omitempty"`
}

// SelfConfigAuditDeniedOn resolves the self_config_audit_denied tri-state:
// an absent key (nil) reads as true (denied attempts ARE recorded), and
// an explicit false disables them.
func (m MCPConfig) SelfConfigAuditDeniedOn() bool {
	return m.SelfConfigAuditDenied == nil || *m.SelfConfigAuditDenied
}

// SelfConfigEnabled resolves the allow_self_config alias: an absent key
// (nil pointer) reads as ENABLED — the default since the BL412 option C
// followup (docs/plans/2026-10-29-bl412-self-config-gate-proposal.md);
// an explicit false disables it. All runtime gates should call this
// rather than dereferencing AllowSelfConfig directly.
func (m MCPConfig) SelfConfigEnabled() bool {
	return m.AllowSelfConfig == nil || *m.AllowSelfConfig
}

// SensitiveWritesEnabled resolves the allow_sensitive_writes setting: an
// absent key (nil pointer) reads as ENABLED, so the default keeps today's
// behaviour; an explicit false disables the sensitive write tool class.
// All runtime gates should call this rather than dereferencing
// AllowSensitiveWrites directly.
func (m MCPConfig) SensitiveWritesEnabled() bool {
	return m.AllowSensitiveWrites == nil || *m.AllowSensitiveWrites
}

// BoolPtr returns a pointer to b. Convenient for the tri-state bool
// config fields (e.g. MCPConfig.AllowSelfConfig), where nil carries
// meaning: a *bool field with yaml:"...,omitempty" stays absent when
// nil, while an explicit false or true round-trips through Save/Load.
func BoolPtr(b bool) *bool { return &b }

// selfConfigMigrationLogf carries the one-line notice raised when the
// allow_self_config default-flip migration (in applyDefaults) fills an
// absent key with the new default (enabled). Tests replace it to
// capture the output — same convention as usageCalculatorLogf.
var selfConfigMigrationLogf = func(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
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

// ExitHookConfigEntry is one exit hook from YAML config (BL356).
type ExitHookConfigEntry struct {
	Name            string `yaml:"name"`
	Action          string `yaml:"action"` // "restart" or "notify"
	NotifySession   string `yaml:"notify_session,omitempty"`
	NotifyMessage   string `yaml:"notify_message,omitempty"`
	CooldownSeconds int    `yaml:"cooldown_seconds,omitempty"` // default 300
}

// SessionConfig holds session management configuration.
type SessionConfig struct {
	// MaxSessions is the max number of concurrent AI sessions
	MaxSessions int `yaml:"max_sessions"`
	// ReservedInteractive holds back this many of max_sessions for operator
	// sessions so autonomous work never starves them (default 1 when
	// max_sessions >= 3, otherwise 0).
	ReservedInteractive *int `yaml:"reserved_interactive,omitempty" json:"reserved_interactive,omitempty"`

	// InputIdleTimeout is how long to wait for idle output before
	// declaring a session is waiting for input (seconds)
	InputIdleTimeout int `yaml:"input_idle_timeout"`

	// LivenessWindowSeconds is the liveness window (seconds) for print-mode
	// backends (family names ending in "-prompt"). Unset or 0 means the
	// built-in default (DefaultLivenessWindowSeconds, 120); callers must use
	// LivenessWindowSecondsEffective() rather than reading this raw.
	LivenessWindowSeconds int `yaml:"liveness_window_seconds,omitempty" json:"liveness_window_seconds,omitempty"`

	// LivenessIdleTimeoutOverrides overrides the input-idle timeout (seconds)
	// per backend. Keys are backend family names ("pi" matches "pi-prompt");
	// full backend names also match. An empty map means the global
	// session.input_idle_timeout applies. Callers must use
	// LivenessInputIdleTimeoutEffective(backend) rather than reading this raw:
	// print-mode backends default to off (0), interactive backends to the
	// global timeout (default 10).
	LivenessIdleTimeoutOverrides map[string]int `yaml:"liveness_idle_timeout_overrides,omitempty" json:"liveness_idle_timeout_overrides,omitempty"`

	// TailLines is the default number of lines returned by tail command
	TailLines int `yaml:"tail_lines"`

	// AlertContextLines is the number of non-empty output lines included
	// in prompt alerts sent to messaging channels. Default: 10.
	AlertContextLines int `yaml:"alert_context_lines"`

	// ClaudeEnabled controls whether claude-code backend is available.
	ClaudeEnabled bool `yaml:"claude_enabled"`

	// LLMBackend selects which LLM backend to use. Default: "claude-code".
	LLMBackend string `yaml:"llm_backend"`

	// CapacityWaitSeconds bounds how long an interactive (non-autonomous)
	// session start blocks waiting for a free node:/llm: capacity slot
	// before returning 503, when capacity is wired (default 8). Kept short
	// deliberately — this blocks a synchronous HTTP request, unlike the
	// much longer autonomous.capacity_wait_timeout_seconds default, which
	// bounds a background task retry loop instead.
	CapacityWaitSeconds int `yaml:"capacity_wait_seconds,omitempty" json:"capacity_wait_seconds,omitempty"`

	// TaskDeliveryWindowSeconds bounds how long one OneShot task delivery
	// attempt waits for the Claude main prompt before trying again
	// (default 60).
	TaskDeliveryWindowSeconds int `yaml:"task_delivery_window_seconds,omitempty" json:"task_delivery_window_seconds,omitempty"`

	// TaskDeliveryAttempts is how many delivery attempts DeliverOneShotTask
	// makes before giving up (default 3).
	TaskDeliveryAttempts int `yaml:"task_delivery_attempts,omitempty" json:"task_delivery_attempts,omitempty"`

	// DismissUIPrompts (session.dismiss_ui_prompts) switches UI prompt
	// dismissal on; absent reads as the default true. A pointer without
	// omitempty so absent and an explicit false differ, and an explicit
	// false survives a YAML/JSON round trip and a PUT /api/config write
	// (the identity.backups_to_keep rule).
	DismissUIPrompts *bool `yaml:"dismiss_ui_prompts" json:"dismiss_ui_prompts"`

	// UIPromptDismissDelaySeconds (session.ui_prompt_dismiss_delay_seconds) is
	// how many whole seconds to wait before sending the dismissal; absent
	// reads as the default 5, and an explicit 0 sends at once. Pointer as
	// above so an explicit 0 survives a round trip.
	UIPromptDismissDelaySeconds *int `yaml:"ui_prompt_dismiss_delay_seconds" json:"ui_prompt_dismiss_delay_seconds"`

	// UIPromptClearBoundSeconds (session.ui_prompt_clear_bound_seconds) is
	// the whole-seconds clear bound; a whole number >= 1, absent reads as
	// the default 30.
	UIPromptClearBoundSeconds *int `yaml:"ui_prompt_clear_bound_seconds" json:"ui_prompt_clear_bound_seconds"`

	// UIPromptMaxAttempts (session.ui_prompt_max_attempts) is how many
	// dismissal attempts are made; a whole number >= 1, absent reads as the
	// default 2.
	UIPromptMaxAttempts *int `yaml:"ui_prompt_max_attempts" json:"ui_prompt_max_attempts"`

	// StoreFlushInterval is how long the session store coalesces changes
	// before the background writer saves them, in milliseconds. Unset means
	// the 1000 ms default; 0 writes through on every change.
	StoreFlushInterval *int `yaml:"store_flush_interval,omitempty" json:"store_flush_interval,omitempty"`

	// StoreArchiveAfterDays is how many days after its last update a finished
	// session moves to the archive file. Unset means 14; 0 disables archiving.
	StoreArchiveAfterDays *int `yaml:"store_archive_after_days,omitempty" json:"store_archive_after_days,omitempty"`

	// StoreFieldCapKB is the largest session text field kept inline in the
	// session store, in KB; longer text is cut and the full text goes to a
	// side file (default 256).
	StoreFieldCapKB int `yaml:"store_field_cap_kb,omitempty" json:"store_field_cap_kb,omitempty"`

	// StoreSideFileCapKB is the length in KB above which an Automaton (PRD)
	// text field moves into a side file next to its record (default 64).
	StoreSideFileCapKB int `yaml:"store_side_file_cap_kb,omitempty" json:"store_side_file_cap_kb,omitempty"`

	// AutoInstallHooks (alpha.34a #202) — when true (default), the daemon
	// auto-writes .claude/sprint/{post-event.sh, .dw-env} into project_dir
	// at session spawn for claude-code sessions. The hook scripts then POST
	// Stop / PostToolUse / UserPromptSubmit events to the daemon's
	// /api/sessions/<id>/hook-event endpoint. Set false to opt out and
	// manage hook files yourself (see docs/howto/claude-hooks.md).
	AutoInstallHooks *bool `yaml:"auto_install_hooks,omitempty"`

	// HookScriptDir (alpha.34a #202) — when non-empty, AutoInstallHooks
	// uses this directory as the source of hook script templates instead
	// of the embedded defaults. Allows operators to customize hook payloads
	// without forking the daemon.
	HookScriptDir string `yaml:"hook_script_dir,omitempty"`

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

	// AutoGitCommit is deprecated and ignored: task sessions are committed
	// automatically now. The key is retained only so old config files with
	// auto_git_commit still load without error.
	AutoGitCommit bool `yaml:"auto_git_commit"`

	// AutoGitInit initializes a git repo in the project dir if one doesn't exist.
	AutoGitInit bool `yaml:"auto_git_init"`

	// GitDefault is the default for the `git` field on session start
	// requests: when a start request does not set `git` explicitly, the
	// session starts with git enabled iff GitDefault is true (pre-session
	// snapshot, checkpoint tag, completion commit). Default false keeps
	// existing flows unchanged.
	GitDefault bool `yaml:"git_default"`

	// SecureTracking controls tracker file encryption when --secure is enabled.
	// "log_only" (default) encrypts only output.log; "full" also encrypts tracker .md files.
	SecureTracking string `yaml:"secure_tracking"`

	// KillSessionsOnExit terminates all running sessions when the daemon exits.
	KillSessionsOnExit bool `yaml:"kill_sessions_on_exit"`

	// ExitHooks (BL356) — exit hooks loaded from config at startup.
	// These seed the persistent ExitHookStore; runtime changes via REST/MCP/CLI
	// are stored in exit_hooks.json and override config-seeded entries.
	ExitHooks []ExitHookConfigEntry `yaml:"exit_hooks,omitempty"`

	// LogLevel sets verbosity for session activity logging: debug, info, warn, error.
	LogLevel string `yaml:"log_level"`

	// RootPath restricts the file browser to this directory and below.
	// Users cannot navigate above this path when choosing a project directory.
	// Defaults to the user's home directory.
	RootPath string `yaml:"root_path"`

	// FileServiceRoot is the root directory for the federated file service (BL333).
	// Per-peer files land in <FileServiceRoot>/peers/<name>/.
	// Per-discussion files land in <FileServiceRoot>/discussions/<id>/.
	// If empty, falls back to session.root_path or the user home directory.
	FileServiceRoot string `yaml:"file_service_root"`

	// MCPMaxRetries is the max number of times to auto-retry /mcp when
	// "MCP server failed" is detected in claude-code session output. Default: 5.
	MCPMaxRetries int `yaml:"mcp_max_retries"`

	// ReconcileOnStartup (BL93) — retained for config compatibility only; it is
	// not read. The daemon always imports orphan session dirs at startup
	// (ReconcileSessions(true) in cmd/datawatch/main.go), so this key has no
	// effect (BL462 F7).
	ReconcileOnStartup bool `yaml:"reconcile_on_startup"`

	// ConsoleCols and ConsoleRows set the default tmux terminal size for new sessions.
	// Per-LLM overrides take priority. Default: 80x24.
	ConsoleCols int `yaml:"console_cols"`
	ConsoleRows int `yaml:"console_rows"`

	// ScheduleSettleMs (B30) is the delay in milliseconds between sending
	// text and pressing Enter for scheduled commands. Fixes the 2nd-Enter
	// bug for TUIs that start accepting input slightly after their prompt
	// state transition fires. 0 disables (legacy single-call send-keys).
	// Default: 200.
	ScheduleSettleMs int `yaml:"schedule_settle_ms"`

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

	// PersistTelemetryOnStop (BL303 S1) — when true, the daemon serializes
	// the session's structured telemetry (task list + timings + verdicts) to
	// episodic memory when a Stop event arrives. Requires memory.enabled=true.
	// Default: false (telemetry is ephemeral; wiped on session delete).
	PersistTelemetryOnStop bool `yaml:"persist_telemetry_on_stop,omitempty"`

	// OneShotSessions — global default for whether DATAWATCH_COMPLETE: in
	// pane output terminates a session (StateComplete) or leaves it alive for
	// more input (StateWaitingInput). Default false (interactive). Autonomous
	// task runners override this to true per-session via the start API
	// "one_shot" field regardless of this setting.
	OneShotSessions bool `yaml:"one_shot_sessions,omitempty"`

	// SessionRetentionDays — how long a finished session's folder under
	// <data_dir>/sessions/ is kept before it is deleted. The store record stays.
	// 0 means the default of 30 days; set a value to change the window.
	SessionRetentionDays int `yaml:"session_retention_days,omitempty"`

	// ResponseSummarizer — when Enabled, the last captured session
	// response is summarized via the referenced LLM before use in
	// alerts and TTS auto-play.
	Summarizer struct {
		Enabled bool   `yaml:"enabled" json:"enabled"`
		LLMRef  string `yaml:"llm_ref" json:"llm_ref"`
		Model   string `yaml:"model"   json:"model,omitempty"`
		Prompt  string `yaml:"prompt"  json:"prompt"`
	} `yaml:"summarizer" json:"summarizer"`

	// ExtraMCPServers (BL344 / GH#118) — additional MCP servers injected into
	// .mcp.json on every session spawn alongside the built-in "datawatch" entry.
	// Name is the map key in .mcp.json (e.g. "imap-mcp"). Command, Args, Env
	// match the stdio transport spec. Entries not present in this list that were
	// manually added to .mcp.json by the operator are preserved.
	ExtraMCPServers []ExtraMCPServer `yaml:"extra_mcp_servers,omitempty"`

	// Capabilities (Design A3, v8.39.25) — the capability group(s)/caps
	// minted into every spawned session's per-session scoped credential,
	// which replaces the admin token the session's bridge used to hold
	// (HLLM-001/002). Empty (the default) resolves to the builtin
	// "session-default" group. Accepts the same values as a federation
	// peer's Capabilities field (builtin group names, individual
	// "surface:action" strings). See docs/howto/federation-cbac.md.
	Capabilities []string `yaml:"capabilities,omitempty"`
}

// TaskDeliveryBackoff is the linear backoff step between OneShot task
// delivery attempts; DeliverOneShotTask sleeps TaskDeliveryBackoff * i
// after the i-th failed attempt before trying again.
const TaskDeliveryBackoff = 5 * time.Second

// EffectiveTaskDeliveryWindow returns TaskDeliveryWindowSeconds as a
// duration, falling back to the 60 s default when the value is <= 0.
func (s *SessionConfig) EffectiveTaskDeliveryWindow() time.Duration {
	if s.TaskDeliveryWindowSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(s.TaskDeliveryWindowSeconds) * time.Second
}

// EffectiveStoreFlushInterval returns the session store write-coalescing
// interval: the configured milliseconds, or 1 s when unset. Zero means
// write-through.
func (s *SessionConfig) EffectiveStoreFlushInterval() time.Duration {
	if s.StoreFlushInterval == nil {
		return time.Second
	}
	if *s.StoreFlushInterval < 0 {
		return 0
	}
	return time.Duration(*s.StoreFlushInterval) * time.Millisecond
}

// EffectiveStoreArchiveAfterDays returns the archive age in days: the
// configured value, or 14 when unset. Zero disables archiving.
func (s *SessionConfig) EffectiveStoreArchiveAfterDays() int {
	if s.StoreArchiveAfterDays == nil {
		return 14
	}
	if *s.StoreArchiveAfterDays < 0 {
		return 0
	}
	return *s.StoreArchiveAfterDays
}

// DefaultUIPrompt values: what the UI prompt handling keys read when the
// corresponding session.* key is absent.
const (
	// DefaultUIPromptEnabled is what dismiss_ui_prompts reads when absent.
	DefaultUIPromptEnabled = true
	// DefaultUIPromptDismissDelaySeconds is what
	// ui_prompt_dismiss_delay_seconds reads when absent (an explicit 0 =
	// send at once; the default is never 0).
	DefaultUIPromptDismissDelaySeconds = 5
	// DefaultUIPromptClearBoundSeconds is what ui_prompt_clear_bound_seconds
	// reads when absent.
	DefaultUIPromptClearBoundSeconds = 30
	// DefaultUIPromptMaxAttempts is what ui_prompt_max_attempts reads when
	// absent.
	DefaultUIPromptMaxAttempts = 2
)

// EffectiveDismissUIPrompts returns session.dismiss_ui_prompts: true when
// the key is absent.
func (s *SessionConfig) EffectiveDismissUIPrompts() bool {
	if s.DismissUIPrompts == nil {
		return DefaultUIPromptEnabled
	}
	return *s.DismissUIPrompts
}

// EffectiveUIPromptDismissDelaySeconds returns
// session.ui_prompt_dismiss_delay_seconds: 5 when the key is absent.
func (s *SessionConfig) EffectiveUIPromptDismissDelaySeconds() int {
	if s.UIPromptDismissDelaySeconds == nil {
		return DefaultUIPromptDismissDelaySeconds
	}
	return *s.UIPromptDismissDelaySeconds
}

// EffectiveUIPromptClearBoundSeconds returns
// session.ui_prompt_clear_bound_seconds: 30 when the key is absent.
func (s *SessionConfig) EffectiveUIPromptClearBoundSeconds() int {
	if s.UIPromptClearBoundSeconds == nil {
		return DefaultUIPromptClearBoundSeconds
	}
	return *s.UIPromptClearBoundSeconds
}

// EffectiveUIPromptMaxAttempts returns session.ui_prompt_max_attempts: 2
// when the key is absent.
func (s *SessionConfig) EffectiveUIPromptMaxAttempts() int {
	if s.UIPromptMaxAttempts == nil {
		return DefaultUIPromptMaxAttempts
	}
	return *s.UIPromptMaxAttempts
}

// ValidateUIPromptDismissDelaySeconds is the
// session.ui_prompt_dismiss_delay_seconds rule: a whole number >= 0 (0 =
// send at once); absent reads as the default 5. The config file load, PUT
// /api/config and every surface that rides it share this rule.
func ValidateUIPromptDismissDelaySeconds(n int) error {
	if n < 0 {
		return fmt.Errorf("session.ui_prompt_dismiss_delay_seconds: %d is invalid (use a whole number >= 0; 0 = send at once; default %d)",
			n, DefaultUIPromptDismissDelaySeconds)
	}
	return nil
}

// ValidateUIPromptClearBoundSeconds is the session.ui_prompt_clear_bound_seconds
// rule: a whole number >= 1; absent reads as the default 30.
func ValidateUIPromptClearBoundSeconds(n int) error {
	if n < 1 {
		return fmt.Errorf("session.ui_prompt_clear_bound_seconds: %d is invalid (use a whole number >= 1; default %d)",
			n, DefaultUIPromptClearBoundSeconds)
	}
	return nil
}

// ValidateUIPromptMaxAttempts is the session.ui_prompt_max_attempts rule:
// a whole number >= 1; absent reads as the default 2.
func ValidateUIPromptMaxAttempts(n int) error {
	if n < 1 {
		return fmt.Errorf("session.ui_prompt_max_attempts: %d is invalid (use a whole number >= 1; default %d)",
			n, DefaultUIPromptMaxAttempts)
	}
	return nil
}

// UIConfig holds the top-level UI display settings (the ui.* section).
type UIConfig struct {
	// SplashMinSeconds (ui.splash_min_seconds) is the minimum number of
	// whole seconds the UI splash stays on screen before it may dismiss.
	// Unset reads the default 6; an explicit 0 keeps the legacy timing
	// (no minimum dwell). A whole number from 0 to 60 inclusive.
	SplashMinSeconds *int `yaml:"splash_min_seconds" json:"splash_min_seconds"`

	// SplashMinSecondsNewVersion (ui.splash_min_seconds_new_version) is
	// the minimum dwell, in whole seconds, when the splash is shown for
	// a new version. Unset reads the default 9; an explicit 0 keeps the
	// legacy timing (no minimum dwell). A whole number from 0 to 60
	// inclusive.
	SplashMinSecondsNewVersion *int `yaml:"splash_min_seconds_new_version" json:"splash_min_seconds_new_version"`
}

// DefaultUISplash* values: what the ui.* splash keys read when absent.
const (
	// DefaultUISplashMinSeconds is what splash_min_seconds reads when
	// absent (an explicit 0 = keep the legacy timing; the default is
	// never 0).
	DefaultUISplashMinSeconds = 6
	// DefaultUISplashMinSecondsNewVersion is what
	// splash_min_seconds_new_version reads when absent.
	DefaultUISplashMinSecondsNewVersion = 9
	// MaxUISplashSeconds is the largest splash minimum dwell accepted.
	MaxUISplashSeconds = 60
)

// SplashMinSecondsEffective returns ui.splash_min_seconds: 6 when the
// key is absent. Callers use this instead of reading the pointer.
func (u *UIConfig) SplashMinSecondsEffective() int {
	if u == nil || u.SplashMinSeconds == nil {
		return DefaultUISplashMinSeconds
	}
	return *u.SplashMinSeconds
}

// SplashMinSecondsNewVersionEffective returns
// ui.splash_min_seconds_new_version: 9 when the key is absent.
func (u *UIConfig) SplashMinSecondsNewVersionEffective() int {
	if u == nil || u.SplashMinSecondsNewVersion == nil {
		return DefaultUISplashMinSecondsNewVersion
	}
	return *u.SplashMinSecondsNewVersion
}

// validateUISplashSeconds is the shared ui.* splash rule: a whole number
// from 0 to 60 inclusive (0 = keep the legacy timing). key is the dotted
// setting name the error carries.
func validateUISplashSeconds(key string, def int, n int) error {
	if n < 0 || n > MaxUISplashSeconds {
		return fmt.Errorf("%s: %d is invalid (use a whole number from 0 to %d; 0 = keep the legacy timing; default %d)",
			key, n, MaxUISplashSeconds, def)
	}
	return nil
}

// ValidateUISplashMinSeconds checks ui.splash_min_seconds: a whole number
// from 0 to 60 inclusive (0 = keep the legacy timing; unset reads 6).
func ValidateUISplashMinSeconds(n int) error {
	return validateUISplashSeconds("ui.splash_min_seconds", DefaultUISplashMinSeconds, n)
}

// ValidateUISplashMinSecondsNewVersion checks
// ui.splash_min_seconds_new_version: a whole number from 0 to 60 inclusive
// (0 = keep the legacy timing; unset reads 9).
func ValidateUISplashMinSecondsNewVersion(n int) error {
	return validateUISplashSeconds("ui.splash_min_seconds_new_version", DefaultUISplashMinSecondsNewVersion, n)
}

// validate checks the ui.* splash keys; it runs at config load.
func (u *UIConfig) validate() error {
	if u.SplashMinSeconds != nil {
		if err := validateUISplashSeconds("ui.splash_min_seconds", DefaultUISplashMinSeconds, *u.SplashMinSeconds); err != nil {
			return err
		}
	}
	if u.SplashMinSecondsNewVersion != nil {
		if err := validateUISplashSeconds("ui.splash_min_seconds_new_version", DefaultUISplashMinSecondsNewVersion, *u.SplashMinSecondsNewVersion); err != nil {
			return err
		}
	}
	return nil
}

// validate checks the UI prompt handling keys; it runs at config load.
func (s *SessionConfig) validate() error {
	if s.UIPromptDismissDelaySeconds != nil {
		if err := ValidateUIPromptDismissDelaySeconds(*s.UIPromptDismissDelaySeconds); err != nil {
			return err
		}
	}
	if s.UIPromptClearBoundSeconds != nil {
		if err := ValidateUIPromptClearBoundSeconds(*s.UIPromptClearBoundSeconds); err != nil {
			return err
		}
	}
	if s.UIPromptMaxAttempts != nil {
		if err := ValidateUIPromptMaxAttempts(*s.UIPromptMaxAttempts); err != nil {
			return err
		}
	}
	return nil
}

// EffectiveStoreFieldCapKB returns the session field cap in KB, falling back
// to 256 when the value is <= 0.
func (s *SessionConfig) EffectiveStoreFieldCapKB() int {
	if s.StoreFieldCapKB <= 0 {
		return 256
	}
	return s.StoreFieldCapKB
}

// EffectiveStoreSideFileCapKB returns the PRD side-file threshold in KB,
// falling back to 64 when the value is <= 0.
func (s *SessionConfig) EffectiveStoreSideFileCapKB() int {
	if s.StoreSideFileCapKB <= 0 {
		return 64
	}
	return s.StoreSideFileCapKB
}

// EffectiveTaskDeliveryAttempts returns TaskDeliveryAttempts, falling
// back to the 3-attempt default when the value is <= 0.
func (s *SessionConfig) EffectiveTaskDeliveryAttempts() int {
	if s.TaskDeliveryAttempts <= 0 {
		return 3
	}
	return s.TaskDeliveryAttempts
}

// TaskDeliveryBudgetTotal is the worst-case wall-clock time one OneShot
// task delivery takes: attempts * window plus the backoff sleeps
// DeliverOneShotTask performs between attempts (TaskDeliveryBackoff * 1,
// * 2, ..., * (attempts-1)). With the defaults this is 3*60 s + 15 s =
// 195 s.
func (s *SessionConfig) TaskDeliveryBudgetTotal() time.Duration {
	attempts := s.EffectiveTaskDeliveryAttempts()
	total := time.Duration(attempts) * s.EffectiveTaskDeliveryWindow()
	for i := 1; i < attempts; i++ {
		total += TaskDeliveryBackoff * time.Duration(i)
	}
	return total
}

// ExtraMCPServer is one entry in session.extra_mcp_servers (BL344 / GH#118).
type ExtraMCPServer struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
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

// DefaultSnapshotNeverStage — the path patterns the autonomous snapshot
// and automatic-staging paths must never stage by default (applied when
// snapshot_never_stage is unset): scratch and temp directories plus local
// data folders. An entry ending in "/" names a directory (everything under
// it is refused); any other entry is a shell-style glob matched against
// the candidate file name (see internal/gitguard's Keep patterns).
var DefaultSnapshotNeverStage = []string{
	"scratch/", "tmp/", "temp/", ".tmp/", "data/", ".data/", "*.tmp",
}

// AutonomousConfig (BL24+BL25) — operator knobs for LLM-driven Automata
// planning. Mirrors internal/autonomous.Config; copied here so
// YAML loading + REST /api/config exposure don't pull in the package.
type AutonomousConfig struct {
	// Enabled gates the background loop. Off by default.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// BranchRefreshEnabled turns on refreshing an Automaton branch that has
	// fallen behind its base. Off by default.
	BranchRefreshEnabled bool `yaml:"branch_refresh_enabled" json:"branch_refresh_enabled"`
	// BranchRefreshBehindLimit is how many commits behind the base a branch
	// may be before it is refreshed. Zero reads as 5; see BranchRefreshLimit.
	BranchRefreshBehindLimit int `yaml:"branch_refresh_behind_limit" json:"branch_refresh_behind_limit"`
	// PollIntervalSeconds is the loop tick (default 30).
	PollIntervalSeconds int `yaml:"poll_interval_seconds,omitempty" json:"poll_interval_seconds,omitempty"`
	// MaxParallelTasks is deprecated and read by nothing: it never
	// limited anything. It is accepted and ignored for one release (a
	// load-time warning + audit entry name the replacement), then
	// removed. Use MaxConcurrentTasks instead.
	MaxParallelTasks int `yaml:"max_parallel_tasks,omitempty" json:"max_parallel_tasks,omitempty"`
	// MaxConcurrentTasks is the daemon-wide default for how many
	// independent (no-dependency) tasks of one Automaton run at once.
	// An Automaton's own max_concurrent_tasks overrides it. 1 =
	// sequential (default).
	MaxConcurrentTasks int `yaml:"max_concurrent_tasks,omitempty" json:"max_concurrent_tasks,omitempty"`
	// ResumeMaxAgeSeconds is how old (in seconds) a completed task may be
	// before it is no longer eligible for resume. Default 3600.
	// NOTE: 0 is a meaningful value (never resume), so it must not be
	// normalised to the default in applyDefaults; Load merges over
	// DefaultConfig() so an absent key keeps 3600 while an explicit 0 or
	// -1 survives. No omitempty so 0 round-trips through Save/Load.
	ResumeMaxAgeSeconds int `yaml:"resume_max_age_seconds" json:"resume_max_age_seconds"`
	// FirstAttempt maps a task kind (implementation, docs, note,
	// final_gate) to the backend and model its first attempt runs on.
	// An absent or empty entry keeps today's choice. Validated by
	// ValidateAttemptCostSettings.
	FirstAttempt map[string]FirstAttemptChoice `yaml:"first_attempt,omitempty" json:"first_attempt,omitempty"`
	// FirstAttemptEscalateAfterFailures is how many failed attempts move
	// the next attempt to the stronger choice. Default 1: the first
	// failure escalates. Range 0..20. No omitempty so an explicit 0
	// round-trips through Save/Load.
	FirstAttemptEscalateAfterFailures int `yaml:"first_attempt_escalate_after_failures" json:"first_attempt_escalate_after_failures"`
	// RetryResumeMaxContextTokens caps the prior-context size (in tokens)
	// a retry may resume with. Default 150000. 0 is meaningful: every
	// retry starts a fresh session. Range 0..2000000. No omitempty so 0
	// round-trips through Save/Load.
	RetryResumeMaxContextTokens int `yaml:"retry_resume_max_context_tokens" json:"retry_resume_max_context_tokens"`
	// MaxEnginesPerNode is how many engine sessions one compute node
	// may run at once. 1 = one engine per node (default). Values below
	// 1 are clamped to 1 at load time.
	MaxEnginesPerNode int `yaml:"max_engines_per_node,omitempty" json:"max_engines_per_node,omitempty"`
	// ProjectWriteSerialization (BL428 §3a —
	// docs/plans/2026-10-14-concurrency-and-budget-design.md) selects
	// how Automaton task writes that target the same project tree are
	// serialized: "advisory" (default — the daemon enforces an
	// exclusive per-canonical-tree lock at admission; tasks on the
	// same tree queue) or "parallel" (reserved but DISABLED —
	// recognized and accepted until the real mechanism lands in the
	// follow-up implementation PRD; the daemon still enforces the lock
	// and logs a startup warning when it is set). Other values are a
	// config error and the daemon refuses to start.
	ProjectWriteSerialization string `yaml:"project_write_serialization,omitempty" json:"project_write_serialization,omitempty"`
	// RefuseDirtyTreeStart (BL428 §3a ii) makes the daemon hard-fail
	// task admission when the PRD's project tree has uncommitted
	// changes (git status --porcelain non-empty). Default false
	// (zero-value bool; not forced in applyDefaults — an omitted
	// bool in an existing config is indistinguishable from an
	// explicit false, the same convention as Apply.UpdateMCPCert
	// above).
	RefuseDirtyTreeStart bool `yaml:"refuse_dirty_tree_start" json:"refuse_dirty_tree_start"`
	// PlanningBackend overrides session.llm_backend for the LLM
	// planning call. Empty = inherit.
	// BL304: renamed from DecompositionBackend; old key still accepted on read.
	PlanningBackend string `yaml:"planning_backend,omitempty" json:"planning_backend,omitempty"`
	// decomposition_backend is the legacy YAML key for PlanningBackend;
	// accepted on read for back-compat, never written.
	DecompositionBackendLegacy string `yaml:"decomposition_backend,omitempty" json:"-"`
	// VerificationBackend selects the BL25 verifier backend. Empty =
	// inherit. For cross-backend independence, set this to a distinct
	// backend (e.g. ollama when planning is claude-code).
	VerificationBackend string `yaml:"verification_backend,omitempty" json:"verification_backend,omitempty"`
	// ExecutionBackend (found live 2026-10-11 with zero config surface
	// at all despite existing as a live internal/autonomous.Config
	// field since BL203) is the fallback backend used when a task and
	// its parent PRD both have an empty Backend field. Empty = fall
	// through to the global session.llm_backend default.
	ExecutionBackend string `yaml:"execution_backend,omitempty" json:"execution_backend,omitempty"`
	// PlanningEffort is the BL41 effort hint for the LLM call.
	// BL304: renamed from DecompositionEffort; old key still accepted on read.
	PlanningEffort string `yaml:"planning_effort,omitempty" json:"planning_effort,omitempty"`
	// decomposition_effort is the legacy YAML key for PlanningEffort;
	// accepted on read for back-compat, never written.
	DecompositionEffortLegacy string `yaml:"decomposition_effort,omitempty" json:"-"`
	// VerificationEffort is the BL41 effort hint for the verifier.
	VerificationEffort string `yaml:"verification_effort,omitempty" json:"verification_effort,omitempty"`
	// v5.26.16 — operator-reported: planning + verification need
	// optional per-backend Model overrides like the per-PRD/per-task
	// LLM dropdowns. Empty = backend default.
	// BL304: renamed from DecompositionModel; old key still accepted on read.
	PlanningModel string `yaml:"planning_model,omitempty" json:"planning_model,omitempty"`
	// decomposition_model is the legacy YAML key for PlanningModel;
	// accepted on read for back-compat, never written.
	DecompositionModelLegacy string `yaml:"decomposition_model,omitempty" json:"-"`
	// PlanningRouting (autonomous.planning_routing) routes the LLM
	// planning call to a stronger planner per rule; see
	// internal/config/planning_routing.go. Off by default (Enabled
	// false); Shadow computes/records the routed decision but keeps the
	// planner unchanged. Not applied by applyDefaults — an existing
	// config without the block loads unchanged.
	PlanningRouting   PlanningRoutingConfig `yaml:"planning_routing,omitempty" json:"planning_routing,omitempty"`
	VerificationModel string                `yaml:"verification_model,omitempty" json:"verification_model,omitempty"`
	// GuardrailModel (v9.10.18) — pinned model for the guardrail judge
	// sessions (rules-review, ci-watch, per-task/per-story guardrails,
	// rejudge). Tier 3 of ResolveGuardrailModel (see
	// internal/autonomous/guardrail_model.go):
	// PRD guardrail_model → task guardrail_model → GuardrailModel →
	// VerificationModel → PlanningModel. Empty = the tier falls through;
	// if no tier resolves a model the guardrail backend can serve, the
	// guardrail records a loud, non-retried infra-category block
	// instead of launching a judge on the backend's built-in default
	// model (which would evict the operator's pinned model).
	GuardrailModel string `yaml:"guardrail_model,omitempty" json:"guardrail_model,omitempty"`
	// VerificationBackends (v8.36.9) is an ordered list of LLM registry
	// names to load-balance the verifier's capacity requests across — e.g.
	// ["ollama-datawatch", "ollama-local-node"]. Found live: a
	// claude-code-backed PRD's verifier always fell back to a single
	// hardcoded "ollama" default, which needed the SAME exclusive
	// node: capacity slot a real local worker session on that node
	// needed — so a 100%-cloud PRD's progress could be blocked
	// indefinitely by unrelated local GPU contention. When set, the
	// verifier tries each entry in order and uses whichever currently
	// has free node capacity, falling back to the first entry's normal
	// (blocking) wait only when all are full. Each candidate uses its
	// own registry-default model — VerificationModel above is ignored
	// in multi-backend mode, since forcing one model across
	// heterogeneous nodes isn't safe. Empty = single-backend behavior
	// exactly as before (VerificationBackend / PRD backend / "ollama").
	//
	// B124 follow-up (2026-10-12) — no `omitempty`, deliberately, same
	// reason this file's Enabled field has none (see handleAutonomousConfig
	// in internal/server/autonomous.go): that handler round-trips the
	// manager's live config through json.Marshal + json.Unmarshal onto
	// this struct before persisting. omitempty on a slice elides it from
	// the marshal entirely when empty, so unmarshal then leaves this
	// field's PREVIOUS value untouched — an operator's explicit "clear
	// this list back to empty" silently never reached the saved
	// config.yaml, found live immediately after fixing the MCP tool
	// handler's own instance of the identical bug.
	VerificationBackends []string `yaml:"verification_backends" json:"verification_backends"`
	// VerifierPolicy chooses which verifier backend runs a verification:
	// the mode, the eligible backend list, the Claude session cap and the
	// stronger-verifier rules. The zero value keeps the existing behaviour
	// (mode fixed). See VerifierPolicyConfig. No omitempty, same round-trip
	// reason as VerificationBackends.
	VerifierPolicy VerifierPolicyConfig `yaml:"verifier_policy" json:"verifier_policy"`
	// HostLoadAlert (autonomous.host_load_alert.*) is the host load check:
	// mode off|alert|reap, the load-to-cores multiple and the minutes a breach
	// must last. The zero value reads as alert, 1.5 and 10. See
	// HostLoadAlertConfig.
	HostLoadAlert HostLoadAlertConfig `yaml:"host_load_alert" json:"host_load_alert"`
	// TaskOverrideStatuses (v9.5.5) lists the PRD lifecycle statuses that accept
	// task-level and story-level LLM override calls (set_task_llm, set_story_llm,
	// set_task_guardrail_llm, set_task_verification_llm, set_story_verification_llm).
	// Empty means the default [needs_review, revisions_asked, cancelled, blocked].
	// PRD-level overrides (set_llm and PRD-level guardrail/verification LLM) are
	// unaffected and stay locked to the pre-Run trio.
	// No omitempty on purpose: an explicit empty list must round-trip as a clear.
	TaskOverrideStatuses []string `yaml:"task_override_statuses" json:"task_override_statuses"`
	// OverrideQueuedTasks (queued task override): accept task-level and
	// story-level LLM override calls (set_task_llm, set_story_llm) while the
	// Automaton is running, i.e. at a PRD status that is NOT in
	// task_override_statuses (typically "running"). When enabled, the call
	// touches only unstarted tasks (pending, queued, waiting for a capacity
	// lease, waiting for a decision, awaiting approval): the new backend,
	// effort and model are written to the record, and an unstarted task
	// sitting in the capacity queue has its queued request retargeted in
	// place to the new backend's pool (capacity.Ledger.Retarget) so there is
	// exactly one live request for it, on the latest pool. Running and
	// finished tasks are never modified; they come back in the result's
	// left_alone list with a reason. When disabled, a locked status refuses
	// the call, the pre-change behavior.
	//
	// *bool because the default is ON and "not present" must stay
	// distinguishable from "explicit false": nil (unset) reads as true, an
	// explicit false reads as false. Same convention as
	// PlanCoverageSuppressCreatedFiles above. EffectiveOverrideQueuedTasks
	// (internal/autonomous) applies the rule.
	OverrideQueuedTasks *bool `yaml:"override_queued_tasks" json:"override_queued_tasks"`
	// PlanningTimeoutSeconds overrides the effort-scaled LLM call timeout
	// during PRD decomposition. 0 = use effort-scaled defaults
	// (120s quick, 300s normal, 900s high/max). Raise for slow Ollama.
	PlanningTimeoutSeconds int `yaml:"planning_timeout_seconds,omitempty" json:"planning_timeout_seconds,omitempty"`
	// PlanCoverageCheck sets how decomposition treats plan-requirement
	// coverage failures: "off" (no check), "warn" (report + surface,
	// proceed) or "block" (failing coverage stops decomposition).
	// Empty normalizes to "block" (the safe default) — see
	// NormalizePlanCoverageCheck. Unknown values are a config error.
	//
	// No `omitempty`, deliberately: handleAutonomousConfig
	// (internal/server/autonomous.go) round-trips the manager's live
	// config through json.Marshal + json.Unmarshal onto this struct
	// before persisting, and omitempty would elide an empty/explicit
	// value and leave the previous stored value in place — an
	// operator's explicit "off" (or a ratio changed to 0) must survive
	// the round-trip (same gotcha as the VerificationBackends comment
	// above).
	PlanCoverageCheck string `yaml:"plan_coverage_check" json:"plan_coverage_check"`
	// PlanCoverageMinRatio is the minimum covered/extractable-requirement
	// ratio a plan must meet for coverage to pass. Valid range (0,1];
	// zero or negative normalizes to 1.0 (require full coverage) and
	// values above 1 clamp to 1.0 — see NormalizePlanCoverageRatio.
	// No omitempty for the same round-trip reason as PlanCoverageCheck.
	PlanCoverageMinRatio float64 `yaml:"plan_coverage_min_ratio" json:"plan_coverage_min_ratio"`
	// PlanCoverageSuppressCreatedFiles controls whether the plan-coverage
	// check stops reporting a requirement as uncovered when a file the plan
	// itself creates already exists. On by default when the key is absent
	// (nil means on); set it explicitly to off to restore the old
	// behaviour. Pointer so an explicit off survives a YAML or JSON
	// round-trip (same pattern as DecisionGate). No omitempty, same
	// round-trip reason as PlanCoverageMinRatio.
	PlanCoverageSuppressCreatedFiles *bool `yaml:"plan_coverage_suppress_created_files" json:"plan_coverage_suppress_created_files"`
	// PlanCoverageScopedRequirements restricts requirement extraction to
	// the sections named by PlanCoverageRequirementSections instead of the
	// whole spec. On by default when the key is absent (nil means on);
	// set it explicitly to off to extract requirements from the entire
	// spec. Pointer so an explicit off survives a YAML or JSON round-trip.
	// No omitempty, same round-trip reason as PlanCoverageMinRatio.
	PlanCoverageScopedRequirements *bool `yaml:"plan_coverage_scoped_requirements" json:"plan_coverage_scoped_requirements"`
	// PlanCoverageRequirementSections is the comma-separated list of
	// section-heading words whose sentences count as requirements when
	// PlanCoverageScopedRequirements is on. Empty (unset) means
	// "scope,constraints" (see DefaultPlanCoverageRequirementSections).
	// No omitempty, same round-trip reason as PlanCoverageMinRatio.
	PlanCoverageRequirementSections string `yaml:"plan_coverage_requirement_sections" json:"plan_coverage_requirement_sections"`
	// PlanCoverageMinRequirementChars is the shortest sentence length, in
	// characters, that counts as a requirement. Zero or negative (unset)
	// means the default 30 (see
	// DefaultPlanCoverageMinRequirementChars). No omitempty, same
	// round-trip reason as PlanCoverageMinRatio.
	PlanCoverageMinRequirementChars int `yaml:"plan_coverage_min_requirement_chars" json:"plan_coverage_min_requirement_chars"`
	// GeneratedArtefacts switches the artefacts an Automaton generates from its
	// change record (gate report, changelog draft, parity draft, tracker rows,
	// registry row, combined docs task). Every switch is on when unset; see
	// EffectiveGeneratedArtefacts. No omitempty so an explicit off round-trips.
	GeneratedArtefacts GeneratedArtefacts `yaml:"generated_artefacts" json:"generated_artefacts"`
	// VerificationTimeoutSeconds bounds one verifier or guardrail one-shot
	// judgment session. Default 1800 seconds (30 minutes); zero or negative
	// values fall back to the default. Raise for slow local (Ollama /
	// OpenWebUI) models.
	VerificationTimeoutSeconds int `yaml:"verification_timeout_seconds,omitempty" json:"verification_timeout_seconds,omitempty"`
	// JudgePolicy is the ordered list of LLM backends the judges (the PRD
	// guardrail, plan-review and other autonomous judges) try, one attempt
	// per backend in order. Empty follows the verifier routing policy: the same
	// modes and backends the verifiers use. A verifier policy that does not
	// apply leaves each judge on its unit's guardrail backend. No
	// omitempty, same round-trip reason as VerificationBackends: an explicit
	// clear must reach the saved config.
	JudgePolicy []string `yaml:"judge_policy" json:"judge_policy"`
	// JudgeTimeoutSeconds bounds one judge run, counted from its start and
	// not from any wait for a capacity slot. Default 1800 seconds (30
	// minutes); zero or negative values fall back to the default (see
	// NormalizeJudgeTimeoutSeconds). No omitempty, for the same round-trip
	// reason as JudgePolicy.
	JudgeTimeoutSeconds int `yaml:"judge_timeout_seconds" json:"judge_timeout_seconds"`
	// JudgeSizeThresholdLines is the change size above which the judges route
	// size-aware. The size is the changed lines (added plus deleted) on the
	// Automaton branch measured against its merge base. Default 400 (see
	// JudgeSizeThresholdDefaultLines); 0 turns the rule off and is kept as 0,
	// never mapped to the default; negative values are rejected at load. No
	// omitempty, for the same round-trip reason as JudgePolicy: an explicit 0
	// must reach the saved config.
	JudgeSizeThresholdLines int `yaml:"judge_size_threshold_lines" json:"judge_size_threshold_lines"`
	// JudgePromptFileThresholdLines is the judge prompt length, in lines,
	// above which the prompt is handed to the judge as a file rather than
	// sent in full. Shorter prompts are sent inline. Default 300 (see
	// JudgePromptFileThresholdDefaultLines); 0 always sends the full prompt,
	// which disables the file route, and is kept as 0, never mapped to the
	// default. No omitempty, for the same round-trip reason as
	// JudgeSizeThresholdLines: an explicit 0 must reach the saved config.
	JudgePromptFileThresholdLines int `yaml:"judge_prompt_file_threshold_lines" json:"judge_prompt_file_threshold_lines"`
	// JudgeDeliveryWaitPerThousandLinesSeconds is the extra time, in
	// seconds, the daemon waits for the main prompt to be delivered for
	// every 1000 lines the prompt contains, on top of the base delivery
	// window. A longer prompt takes longer to type into the session, so
	// this stretches the wait with its size. Default 60; 0 adds nothing, so
	// the wait stays the same however long the prompt is, and is kept as 0,
	// never mapped to the default. No omitempty, so an explicit 0 reaches
	// the saved config.
	JudgeDeliveryWaitPerThousandLinesSeconds int `yaml:"judge_delivery_wait_per_thousand_lines_seconds" json:"judge_delivery_wait_per_thousand_lines_seconds"`
	// JudgeStrongBackend names the strong judge backend a large diff is
	// routed to first when the changed-line count exceeds
	// JudgeSizeThresholdLines (see config.JudgeSizeStrongFirst). Empty (the
	// default) resolves to the last entry of JudgePolicy; a value not in
	// the judge policy leaves the policy order unchanged. No omitempty,
	// for the same round-trip reason as JudgePolicy.
	JudgeStrongBackend string `yaml:"judge_strong_backend" json:"judge_strong_backend"`
	// JudgeVerdictCacheEnabled controls the per-commit judge verdict cache:
	// when on (the default), a judge that completes a pass or block verdict
	// on a (commit sha, judge policy, rules) triple replays that verdict
	// from the cache instead of running the judge again, and a judge that
	// timed out or otherwise did not finish is never cached. Unset (nil)
	// reads true — see NormalizeJudgeVerdictCacheEnabled; an explicit false
	// must round-trip, so the pointer is never collapsed.
	JudgeVerdictCacheEnabled *bool `yaml:"judge_verdict_cache_enabled,omitempty" json:"judge_verdict_cache_enabled,omitempty"`
	// SessionIdleTimeoutSeconds bounds one autonomous worker or verifier
	// session: a session sitting in waiting_input/idle with no live child
	// process for longer than this is an infra failure. 0 or unset => 900
	// (see NormalizeSessionIdleTimeoutSeconds).
	SessionIdleTimeoutSeconds int `yaml:"session_idle_timeout_seconds,omitempty" json:"session_idle_timeout_seconds,omitempty"`
	// DescendantStallSeconds bounds a pi-prompt worker whose only liveness is a
	// work child process: if it shows no output, no file change and no log
	// write for this long, it is stalled and is reaped as an infra failure. 0
	// or unset => 2700 (see NormalizeDescendantStallSeconds).
	DescendantStallSeconds int `yaml:"descendant_stall_seconds,omitempty" json:"descendant_stall_seconds,omitempty"`
	// VerifierInfraEscalationThreshold is the count of consecutive infra
	// verifier failures on one task after which its next verification runs on
	// FallbackVerificationBackend. 0 or unset => 2.
	VerifierInfraEscalationThreshold int `yaml:"verifier_infra_escalation_threshold,omitempty" json:"verifier_infra_escalation_threshold,omitempty"`
	// VerificationRecordScores keeps the classify-mode score vector and route
	// on every verification result. nil (unset) => true. false keeps only the
	// verifier mode.
	VerificationRecordScores *bool `yaml:"verification_record_scores,omitempty" json:"verification_record_scores,omitempty"`
	// ScopeBreadthEnabled turns on the scope-breadth classifier: on a
	// content-category task-verification failure it asks whether the task bundles
	// several separable units and sets FailureReason.scope_too_broad. Needs the
	// classifier capability. Default false.
	ScopeBreadthEnabled bool `yaml:"scope_breadth_enabled,omitempty" json:"scope_breadth_enabled,omitempty"`
	// FallbackVerificationBackend is the backend used for that one
	// verification. Empty => claude-code.
	FallbackVerificationBackend string `yaml:"fallback_verification_backend,omitempty" json:"fallback_verification_backend,omitempty"`
	// VerifierNoProgressTimeoutSeconds ends a verifier session that shows no
	// output file and no pane activity for this long. 0 or unset => 600
	// (see NormalizeVerifierNoProgressTimeoutSeconds).
	VerifierNoProgressTimeoutSeconds int `yaml:"verifier_no_progress_timeout_seconds,omitempty" json:"verifier_no_progress_timeout_seconds,omitempty"`
	// VerifierMaxRunSeconds caps the total wall-clock run of one verifier
	// session, counted from its start. Default 2700 seconds (45 minutes) (see
	// VerifierRunCapDefaultSeconds). 0 means no cap and is kept as 0, never
	// mapped to the default; negative values are rejected (see
	// ValidateVerifierRunCapSeconds). No omitempty, so an explicit 0 round-trips.
	VerifierMaxRunSeconds int `yaml:"verifier_max_run_seconds" json:"verifier_max_run_seconds"`
	// WorkerIdleNudgeSeconds is how long a PRD-owned claude-code worker may sit
	// idle with no output and no completion marker before it gets one nudge to
	// finish. 0 or unset => 180 (see NormalizeWorkerIdleNudgeSeconds).
	WorkerIdleNudgeSeconds int `yaml:"worker_idle_nudge_seconds,omitempty" json:"worker_idle_nudge_seconds,omitempty"`
	// WorkerIdleNudgeStallSeconds is how long, counted from the nudge, the
	// worker may stay silent before it is declared stalled and retried.
	// 0 or unset => 600 (see NormalizeWorkerIdleNudgeStallSeconds).
	WorkerIdleNudgeStallSeconds int `yaml:"worker_idle_nudge_stall_seconds,omitempty" json:"worker_idle_nudge_stall_seconds,omitempty"`
	// SessionStartConfirmSeconds bounds a claude-code autonomous session
	// that is still in waiting_input with no assistant output: a session
	// that produces no first assistant turn within this many seconds is a
	// "session never started" failure. 0 or unset => 120
	// (see NormalizeSessionStartConfirmSeconds).
	SessionStartConfirmSeconds int `yaml:"session_start_confirm_seconds,omitempty" json:"session_start_confirm_seconds,omitempty"`
	// OrphanGraceSeconds is how long a live task worker session may stay
	// unbound to any non-terminal task before the orphan sweep ends it. 0 or
	// unset => 300 (see NormalizeOrphanGraceSeconds).
	OrphanGraceSeconds int `yaml:"orphan_grace_seconds,omitempty" json:"orphan_grace_seconds,omitempty"`
	// DuplicateSessionKeep picks the survivor when two or more live sessions
	// match one task: "older" (default) keeps the older session until it ends,
	// "newer" keeps the newer one. The other sessions are ended through the
	// session killer, and only after their process exit is confirmed. Unset or
	// unrecognised => "older" (see NormalizeDuplicateSessionKeep).
	DuplicateSessionKeep string `yaml:"duplicate_session_keep,omitempty" json:"duplicate_session_keep,omitempty"`
	// RestartPolicy is what a requested daemon restart does about running
	// work: "immediate" (default) restarts at once; "wait_quiet" waits up to
	// RestartQuietTimeoutSeconds for zero running tasks; "drain" stops new
	// tasks starting, lets running ones finish, then restarts. Unset or
	// unrecognised => "immediate" (see NormalizeRestartPolicy).
	RestartPolicy string `yaml:"restart_policy,omitempty" json:"restart_policy,omitempty"`
	// PlanningRestartPolicy is what a daemon restart does to an Automaton
	// still in planning: "reattach" (default) keeps planning running across
	// the restart; "restart" restarts planning once and records a decision
	// saying so; "draft" resets the Automaton to draft, as before this
	// setting existed. Unset or unrecognised => "reattach" (see
	// NormalizePlanningRestartPolicy).
	PlanningRestartPolicy string `yaml:"planning_restart_policy,omitempty" json:"planning_restart_policy,omitempty"`
	// RestartQuietTimeoutSeconds bounds the wait_quiet wait. 0 or unset => 600
	// (see NormalizeRestartQuietTimeoutSeconds).
	RestartQuietTimeoutSeconds int `yaml:"restart_quiet_timeout_seconds,omitempty" json:"restart_quiet_timeout_seconds,omitempty"`
	// RestartOnTimeout is what wait_quiet does when the wait runs out with
	// tasks still running: "proceed" (default) restarts anyway, "abort"
	// cancels the restart (see NormalizeRestartOnTimeout).
	RestartOnTimeout string `yaml:"restart_on_timeout,omitempty" json:"restart_on_timeout,omitempty"`
	// LoopDetectTurns is the worker loop-detection window (how many
	// recent turns autonomous.DetectLoop compares). *int: nil = unset →
	// default 3; 0 = detector OFF; n>0 = window size. Negative values are
	// rejected — see loop_detect_turns.go.
	LoopDetectTurns *int `yaml:"loop_detect_turns,omitempty" json:"loop_detect_turns,omitempty"`
	// CapacityEnabled turns capacity-aware admission on (default true when
	// unset): tasks wait for a free host/node/LLM slot instead of failing.
	CapacityEnabled *bool `yaml:"capacity_enabled,omitempty" json:"capacity_enabled,omitempty"`
	// UsageBackoff controls automatic per-backend back-off after a rate or
	// usage limit is detected: "pause_backend" (default) holds new work for
	// the affected backend until its reset time; "off" disables it. An
	// unrecognized value normalizes to "off" with a one-time warning.
	UsageBackoff string `yaml:"usage_backoff,omitempty" json:"usage_backoff,omitempty"`
	// BackoffMaxWaitMinutes caps the back-off applied when a rate limit
	// carries no parseable reset time. Unset => 60; values < 1 clamp to 60.
	BackoffMaxWaitMinutes int `yaml:"backoff_max_wait_minutes,omitempty" json:"backoff_max_wait_minutes,omitempty"`
	// RoleScanIntervalSeconds is how often the daemon re-checks LLM role
	// tags for contention (two entries claiming one role, or two roles
	// converging on one entry). Unset or < 1 => 300. Warnings only.
	RoleScanIntervalSeconds int `yaml:"role_scan_interval_seconds,omitempty" json:"role_scan_interval_seconds,omitempty"`
	// CapacityWaitTimeoutSeconds bounds one capacity wait (default 14400).
	CapacityWaitTimeoutSeconds int `yaml:"capacity_wait_timeout_seconds,omitempty" json:"capacity_wait_timeout_seconds,omitempty"`
	// ArchiveGraceMinutes is how many whole minutes a cancelled Automaton
	// stays before the supervisor loop archives it. A pointer, so an explicit
	// 0 (archive at once) survives omitempty; unset reads as 60 through
	// ArchiveGraceMinutesOrDefault.
	ArchiveGraceMinutes *int `yaml:"archive_grace_minutes,omitempty" json:"archive_grace_minutes,omitempty"`
	// CapacityFallthrough: when a task's requested pool is full while another
	// allowed pool for the same task is idle, admit it on that other pool and
	// audit the fall-through. Default false: a task waits for its own pool.
	CapacityFallthrough bool `yaml:"capacity_fallthrough,omitempty" json:"capacity_fallthrough,omitempty"`
	// CapacityStrictNodeLimit: a compute node never runs more sessions than its
	// limit through admission — verifier and resumed (adopted) workers that land
	// on a full node are held outside its pool yet still count against it. Unset
	// = true. Off restores the earlier behaviour, where an adopted session was
	// added to the node pool whatever its use. Read live by the ledger.
	CapacityStrictNodeLimit *bool `yaml:"capacity_strict_node_limit,omitempty" json:"capacity_strict_node_limit,omitempty"`
	// CapacityCountPoolByNode: an llm: pool counts the sessions on each node up
	// to that node's own limit, so sessions piled on a busy node do not use up
	// slots a free node could take. Unset = true. Off counts every lease once.
	// Read live by the ledger.
	CapacityCountPoolByNode *bool `yaml:"capacity_count_pool_by_node,omitempty" json:"capacity_count_pool_by_node,omitempty"`
	// CapacityAdoptBeforeAdmit: after a daemon restart, new requests wait until
	// the work that survived the restart has been adopted (or
	// CapacityAdoptionWaitSeconds passes). Unset = true. Off lets a new request
	// take a slot at once. Read live by the ledger.
	CapacityAdoptBeforeAdmit *bool `yaml:"capacity_adopt_before_admit,omitempty" json:"capacity_adopt_before_admit,omitempty"`
	// CapacityAdoptionWaitSeconds bounds the restart wait above. 0 or unset =
	// 30. Read when the daemon starts.
	CapacityAdoptionWaitSeconds int `yaml:"capacity_adoption_wait_seconds,omitempty" json:"capacity_adoption_wait_seconds,omitempty"`
	// CapacityGPUUtilPct holds new tasks back while a node's busiest GPU is at
	// or above this utilisation percent (0 = off).
	CapacityGPUUtilPct int `yaml:"capacity_gpu_util_pct,omitempty" json:"capacity_gpu_util_pct,omitempty"`
	// CapacityRateRequests caps the lease grants each llm: pool may make in
	// one rolling window (CapacityRateWindowSeconds). 0 = no requests ceiling.
	CapacityRateRequests int `yaml:"capacity_rate_requests,omitempty" json:"capacity_rate_requests,omitempty"`
	// CapacityRateTokens caps the tokens each llm: pool may record in one
	// rolling window. 0 = no tokens ceiling.
	CapacityRateTokens int `yaml:"capacity_rate_tokens,omitempty" json:"capacity_rate_tokens,omitempty"`
	// CapacityRateWindowSeconds is the rolling window length for the two
	// ceilings above. 0 means the default of 3600 and matters only when a
	// ceiling is set.
	CapacityRateWindowSeconds int `yaml:"capacity_rate_window_seconds,omitempty" json:"capacity_rate_window_seconds,omitempty"`
	// PlanTierMaxInflight maps an inference.PlanTier (pro / max_5x /
	// max_20x / api) to the per-LLM inflight cap the capacity ledger
	// enforces when an LLM entry sets that plan_tier and no explicit
	// max_inflight. Starting table pro=1, max_5x=2, max_20x=4, api=0 is
	// OPERATOR POLICY, not a technical limit — override the map here.
	// api=0 (unlimited): the dollar budget is a later phase. Unknown
	// tier keys or negative values are a config error (validateLoaded).
	// No omitempty: an operator's explicit map edit (including clearing a
	// tier to 0) must survive the JSON round-trip through
	// handleAutonomousConfig, same gotcha as VerificationBackends above.
	PlanTierMaxInflight map[string]int `yaml:"plan_tier_max_inflight" json:"plan_tier_max_inflight"`
	// PlanTierAllocationUSD maps an inference.PlanTier (pro / max_5x /
	// max_20x / api) to the operator-declared notional dollar allocation
	// per budget window at API rates for that tier. Default is an empty
	// map (nothing declared); zero for a tier also means not declared. It
	// feeds only the estimated allocation_pct figure and never blocks or
	// gates anything. Unknown tier keys or negative values are a config
	// error (validateLoaded). No omitempty, for the same reason as
	// PlanTierMaxInflight: an explicit clear must survive the round trip.
	PlanTierAllocationUSD map[string]float64 `yaml:"plan_tier_allocation_usd" json:"plan_tier_allocation_usd"`
	// StaleTaskSeconds — default 900 seconds. A zero or negative value
	// inherits session.stale_timeout_seconds.
	StaleTaskSeconds int `yaml:"stale_task_seconds,omitempty" json:"stale_task_seconds,omitempty"`
	// StuckRunningSeconds — default 900 seconds. How long a PRD may sit in
	// running with every task terminal, no live session and no live executor
	// before the stuck-running watchdog re-fires the completion rollup once;
	// a second full bound in the same state moves it to blocked. A zero or
	// negative value uses the default (see NormalizeStuckRunningSeconds).
	StuckRunningSeconds int `yaml:"stuck_running_seconds,omitempty" json:"stuck_running_seconds,omitempty"`
	// TaskMaxAttemptSeconds is the wall-clock cap on one task attempt. 0 (the
	// default) means off; 5400 is recommended.
	TaskMaxAttemptSeconds int `yaml:"task_max_attempt_seconds,omitempty" json:"task_max_attempt_seconds,omitempty"`
	// TaskNoProgressSeconds is how many seconds one task attempt may go
	// without progress. 0 (the default) means off; 1800 is recommended.
	TaskNoProgressSeconds int `yaml:"task_no_progress_seconds,omitempty" json:"task_no_progress_seconds,omitempty"`
	// PreserveAttemptWork keeps a failed attempt's worktree changes around
	// so the next attempt can build on them. Nil (unset) means true; use
	// PreserveAttemptWorkEnabled rather than dereferencing the pointer.
	PreserveAttemptWork *bool `yaml:"preserve_attempt_work,omitempty" json:"preserve_attempt_work,omitempty"`
	// AutoFixRetries — the content auto-fix retries per Automaton (0 turns
	// content auto-fix off). Each retry is one corrective task created from
	// the completion review's findings; the retry budget is consumed only
	// after that task exists. When the completion review still blocks and
	// the retries are spent (or auto-fix is off), the Automaton is set to
	// needs_review with the findings attached — not blocked, not running —
	// so the operator or supervisor adds a corrective task with the normal
	// add-task call and approves it. The total retry ceiling
	// (RetryCeilingTotal) also applies on top of this budget.
	AutoFixRetries int `yaml:"auto_fix_retries,omitempty" json:"auto_fix_retries,omitempty"`
	// VerifierDiffMaxBytes caps the git diff injected into the verifier
	// prompt for evidence grounding (BL366). 0 = 8192. Raise for large
	// PRDs; lower to reduce token cost.
	VerifierDiffMaxBytes int `yaml:"verifier_diff_max_bytes,omitempty" json:"verifier_diff_max_bytes,omitempty"`
	// VerifierMinFreeMemMB is the free GPU memory (MB) a node must still
	// have, with the extra verifier slots running, before a verifier cap
	// above 1 is admitted and before a verifier_slots write above 1 is
	// accepted. Unset (nil) means the default of 1024 MB; an explicit 0
	// turns the floor off. Read through EffectiveVerifierMinFreeMB, which
	// reuses compute.EffectiveVerifierMinFreeMB — the only default.
	VerifierMinFreeMemMB *int `yaml:"verifier_min_free_mem_mb,omitempty" json:"verifier_min_free_mem_mb,omitempty"`
	// VerifierHeadroomMaxAgeHours is how old, in hours, a headroom
	// measurement may be and still count as fresh for the verifier cap
	// gates. 0 or unset means the default of 24 hours; a negative value is
	// a config error. Read through EffectiveVerifierHeadroomMaxAge.
	VerifierHeadroomMaxAgeHours int `yaml:"verifier_headroom_max_age_hours,omitempty" json:"verifier_headroom_max_age_hours,omitempty"`
	// SecurityScan — when true, run the nightwire-port pattern scan
	// over modified files before marking a task complete.
	SecurityScan bool `yaml:"security_scan,omitempty" json:"security_scan,omitempty"`
	// SnapshotNeverStage lists path patterns the autonomous snapshot and
	// automatic-staging paths (the refs/datawatch/attempts attempt snapshots
	// and the other automatic commits) must never stage: an entry ending in
	// "/" names a directory (every file under it is refused) while any other
	// entry is a shell-style glob matched against the candidate's file name
	// (e.g. "*.tmp"). The default list covers scratch, temp and local data
	// folders (see DefaultSnapshotNeverStage). Unset (nil) reads the
	// default; an explicitly empty list disables every never-stage rule,
	// so the nil and empty states must not be conflated
	// (see EffectiveSnapshotNeverStage). No `omitempty`, deliberately,
	// same round-trip reason as VerificationBackends above: handleAutonomousConfig
	// round-trips the live config through json.Marshal + json.Unmarshal
	// before persisting, and omitempty on a slice would elide an explicit
	// "clear this list" so the operator's intent to disable the rules never
	// reached the saved config.
	SnapshotNeverStage []string `yaml:"snapshot_never_stage" json:"snapshot_never_stage"`

	// BL191 Q4 (v5.9.0) — recursive child-PRDs. MaxRecursionDepth
	// caps the parent→child chain length; 0 disables recursion.
	// AutoApproveChildren skips operator review on spawned children
	// (otherwise every level hangs on /approve).
	MaxRecursionDepth int `yaml:"max_recursion_depth,omitempty" json:"max_recursion_depth,omitempty"`
	// JSON has no omitempty (YAML keeps it, for a concise on-disk file):
	// this struct is marshaled to JSON and merge-unmarshaled onto the live
	// internal/autonomous.Manager config on every autonomous.* config
	// write (internal/server/api.go's handlePutConfig); omitempty on a
	// bool means an explicit "set this back to false" is marshaled as an
	// absent key and silently never overwrites a previously-true value in
	// that merge — found live 2026-10-05 via this exact bug on
	// PerStoryApproval below, hanging every subsequent PRD approval.
	AutoApproveChildren bool `yaml:"auto_approve_children,omitempty" json:"auto_approve_children"`

	// BL191 Q6 (v5.10.0) — guardrails at story + task level. Empty
	// list = disabled at that level. Names match the BL117 orchestrator:
	// rules, security, release-readiness, docs-diagrams-architecture.
	PerTaskGuardrails  []string `yaml:"per_task_guardrails,omitempty" json:"per_task_guardrails,omitempty"`
	PerStoryGuardrails []string `yaml:"per_story_guardrails,omitempty" json:"per_story_guardrails,omitempty"`

	// BL418 — daemon-wide default for the "prd" level (PRD-completion
	// rules review opt-in). Empty = disabled, same as the two above.
	// Valid names: "rules-review", "ci-watch".
	PerPRDGuardrails []string `yaml:"per_prd_guardrails,omitempty" json:"per_prd_guardrails,omitempty"`

	// BL410 Phase 1 — per-category retry budgets. "content"/"policy"
	// fall back to AutoFixRetries above when absent (backward-compat
	// anchor). "infra"'s default (4) and "scope"'s (0) are seeded by
	// DefaultConfig, not hardcoded in the resolver.
	RetryPolicy map[string]int `yaml:"retry_policy,omitempty" json:"retry_policy,omitempty"`
	// RetryCeilingTotal caps auto-retries summed across every category
	// for one PRD's whole lifecycle (default 5 — a normal release PRD
	// failing more than ~5 times means the spec needs updating or
	// datawatch itself is broken, not a problem one more attempt
	// fixes). PRD.RetryCeilingTotal or a named RetryProfile can raise
	// this for genuinely iterative work (e.g. CVE test-and-fix PRDs).
	RetryCeilingTotal int `yaml:"retry_ceiling_total,omitempty" json:"retry_ceiling_total,omitempty"`
	// InfraRetryEscalateAfter — retries that keep the exact original
	// backend/model config before an infra-category retry falls back
	// to the global default (default 2).
	InfraRetryEscalateAfter int `yaml:"infra_retry_escalate_after,omitempty" json:"infra_retry_escalate_after,omitempty"`
	// CIWatchTimeoutSeconds bounds how long the ci-watch completion
	// guardrail (enabled by adding "ci-watch" to per_prd_guardrails)
	// waits for the CI runs of the commit an automaton pushed before
	// giving up (default 1500 = 25 minutes: the slowest real release
	// workflow run here took ~18 minutes, ordinary push workflows 1-11).
	CIWatchTimeoutSeconds int `yaml:"ci_watch_timeout_seconds,omitempty" json:"ci_watch_timeout_seconds,omitempty"`
	// DetailTextCapBytes caps each heavy output text field (memory report,
	// task error, verifier text, start-failure tail, verdicts) in the
	// default GET /api/autonomous/prds/{id} reply. The write-back fields —
	// Automaton spec, story description, task spec — are never capped, so a
	// capped reply can always be read and saved back verbatim. A capped
	// field contains a marker and is named in the record's "truncated"
	// list with the URL of the full text. The store always keeps the full
	// text; ?full=1 and the outputs sub-resource return it. 0 means the
	// default (4096); a negative value turns the cap off.
	DetailTextCapBytes int `yaml:"detail_text_cap_bytes,omitempty" json:"detail_text_cap_bytes,omitempty"`
	// InheritedTagsCacheSeconds is the lifetime of the shared cache of
	// inherited (system) tags per Automaton that the session manager
	// builds. 0 means the default (15 seconds); a negative value turns
	// the cache off (the index is rebuilt on every read).
	InheritedTagsCacheSeconds int `yaml:"inherited_tags_cache_seconds,omitempty" json:"inherited_tags_cache_seconds,omitempty"`
	// ContentAutoFixEnabled/AutoFixSkipApproval have no omitempty —
	// see AutoApproveChildren's comment below for why a bool config
	// field needs the explicit-clear discipline. Defaults: both true.
	ContentAutoFixEnabled bool `yaml:"content_auto_fix_enabled" json:"content_auto_fix_enabled"`
	AutoFixSkipApproval   bool `yaml:"auto_fix_skip_approval" json:"auto_fix_skip_approval"`
	// BlockOnFileDrift opts into strict foreign-file-drift enforcement:
	// a task that modified tracked files outside its own scope is
	// blocked instead of merely flagged. Default false. Overridable per
	// guardrail profile and per Automaton. No omitempty, same
	// explicit-clear reason as ContentAutoFixEnabled above.
	BlockOnFileDrift bool `yaml:"block_on_file_drift" json:"block_on_file_drift"`
	// DriftMode — pre-existing foreign-file-drift enforcement mode for
	// the autonomous executor: "block" (default) blocks a task whose
	// own commit swept in files that were already uncommitted before it
	// started and were not in its planned files; "warn" only flags the
	// task (recorded as foreign_file_drift, still completes). Replaces
	// the legacy block_on_file_drift boolean as the daemon-wide knob.
	// PUT /api/config and PUT /api/autonomous/config reject any value
	// other than "block" or "warn" (ValidateDriftMode). No omitempty —
	// the effective mode must survive YAML round-trips, matching the
	// executor's resolveDriftMode (empty/"block" → block).
	DriftMode string `yaml:"drift_mode" json:"drift_mode"`
	// UndeclaredEditMode — off | warn | block: what the executor does with
	// the worker's own edits outside a task's planned and generated files.
	// Default "warn". Independent of DriftMode. No omitempty so an explicit
	// value round-trips. Validated by ValidateUndeclaredEditMode.
	UndeclaredEditMode string `yaml:"undeclared_edit_mode" json:"undeclared_edit_mode"`
	// ScopeLint controls the pre-spawn scope lint that checks a task's
	// spec or planned files before the task runs. Modes:
	//   - "off" skips the scope lint entirely.
	//   - "warn" lints and records problems but never blocks a task.
	//   - "enforce" blocks a task whose spec or planned files fail the
	//     lint — today's behaviour, and the default.
	// Placeholder and quoted pattern text is never flagged in any mode.
	// An empty or unrecognised value resolves to "enforce" (see
	// ScopeLintMode), so an existing configuration keeps blocking.
	// No omitempty, same explicit-clear reason as BlockOnFileDrift above.
	ScopeLint string `yaml:"scope_lint" json:"scope_lint"`

	// Phase 3 (v5.26.61) — per-story approval gate. Default false
	// (preserves the v5.26.x behavior where PRD approval auto-
	// approves every story). When true, PRD approval transitions
	// stories to "awaiting_approval"; the runner skips those until
	// the operator calls POST /api/autonomous/prds/{id}/approve_story
	// per story.
	// No JSON omitempty — see AutoApproveChildren's comment above.
	PerStoryApproval bool `yaml:"per_story_approval,omitempty" json:"per_story_approval"`

	// ContinueOnStoryFailure: default false (halt). The moment a story
	// rolls up to "failed", the PRD stops (status -> blocked) instead of
	// the executor continuing into later, independent stories, so the
	// operator can re-edit/rerun the failed story first. Set true to
	// restore the old continue-regardless behavior. Per-PRD
	// continue_on_story_failure field overrides this default.
	// No JSON omitempty — see AutoApproveChildren's comment above.
	ContinueOnStoryFailure bool `yaml:"continue_on_story_failure,omitempty" json:"continue_on_story_failure"`

	// BL367 — default quality gate config applied to every PRD whose
	// quality_gates field is nil. Disabled (Enabled: false) by default
	// to preserve v8.15.x behavior. Per-PRD quality_gates field overrides.
	DefaultQualityGates QualityGateConfig `yaml:"default_quality_gates,omitempty" json:"default_quality_gates,omitempty"`
	// GateDisagreement — verifier/quality-gate disagreement policy. When a
	// task's verifier PASSES but the quality gate disagrees (a
	// valid-baseline regression, or an invalid gate), "warn" (default) lets
	// the task complete while recording the disagreement on the task
	// (gate_disagreement field), the PRD decision log, and a warning log
	// line; "block" fails the task with an error prefixed
	// "gate_disagreement: …" for a real regression from a VALID baseline
	// (an invalid gate never blocks under any policy). The per-PRD
	// quality-gates block_on_regression keeps its current meaning in the
	// no-disagreement case. Any other value is rejected on write and
	// treated as "warn" on read.
	GateDisagreement string `yaml:"gate_disagreement,omitempty" json:"gate_disagreement,omitempty"`
	// FailureEscalation — global default failure escalation policy: how many
	// failed attempts a task gets, which workers the later attempts use, and
	// when it is held for operator review. Automaton and task overrides win
	// per field. See FailureEscalationConfig.
	FailureEscalation FailureEscalationConfig `yaml:"failure_escalation" json:"failure_escalation"`
	// InfraRetry — retry policy for infrastructure failures (spawn error,
	// storage write, session killed before delivery, daemon restart): how
	// many tries, the backoff between them, and what happens when they run
	// out. An unset key reads its default. See InfraRetryConfig.
	InfraRetry InfraRetryConfig `yaml:"infra_retry,omitempty" json:"infra_retry,omitempty"`
	// MergePipeline — where a project's merge pipeline comes from (its own
	// file, the daemon's record, or the built-in default), what happens when
	// the file is bad, and how a merge conflict is handled; each key can be
	// overridden per project. An unset key reads its default. See
	// MergePipelineConfig.
	MergePipeline MergePipelineConfig `yaml:"merge_pipeline,omitempty" json:"merge_pipeline,omitempty"`
	// PushAndTag — whether the daemon publishes a finished release on its
	// own. Off (the default) means the daemon only prepares the release and
	// leaves pushing and tagging to the operator; on lets it push the
	// release commit and tag.
	PushAndTag bool `yaml:"push_and_tag" json:"push_and_tag"`
	// ReleasePath — how a prepared release reaches the remote: direct_push
	// (the default) pushes the release commit straight to the default
	// branch; pull_request opens a pull request instead and waits for it to
	// merge.
	ReleasePath string `yaml:"release_path" json:"release_path"`
	// RequestWatch — observation of the in-flight model request of a worker
	// or verifier whose model runs on a registered compute node, so a hung
	// request can be told from a slow one. Observation only: it never ends a
	// worker or retries. An unset key reads its default. See
	// RequestWatchConfig.
	RequestWatch RequestWatchConfig `yaml:"request_watch,omitempty" json:"request_watch,omitempty"`
	// ModelRouting — whether a worker pinned to a model that no registered
	// node serves is refused early, how long a node's served models are
	// cached, and how an unreadable node counts. An unset key reads its
	// default. See ModelRoutingConfig.
	ModelRouting ModelRoutingConfig `yaml:"model_routing,omitempty" json:"model_routing,omitempty"`

	// BL369 — prompt injection guard. When InjectionGuard is true, user-
	// supplied PRD/task specs are scanned for known injection phrases at
	// the API boundary. Findings are always logged; BlockOnInjection
	// causes the request to be rejected with 400 instead of warn-only.
	// No JSON omitempty on either — see AutoApproveChildren's comment above.
	InjectionGuard   bool `yaml:"injection_guard,omitempty" json:"injection_guard"`
	BlockOnInjection bool `yaml:"block_on_injection,omitempty" json:"block_on_injection"`

	// BL406 Phase 0 — scan sub-config, YAML-persisted (fixes B113: this
	// previously had no YAML field at all, so the daemon-startup bridge
	// in cmd/datawatch/main.go couldn't carry an operator's scan
	// settings across a restart, and the package's own all-on default
	// was never actually reached).
	Scan ScanConfig `yaml:"scan,omitempty" json:"scan,omitempty"`

	// BL406 — path to this project's prescriptive rules file (its
	// AGENT.md equivalent), read by the decomposer and the new
	// project-rules scanner. Relative to the PRD's project_dir. Empty
	// default "AGENT.md" is applied at read time, not stored, so an
	// operator without one isn't forced to create it.
	RulesFile string `yaml:"rules_file,omitempty" json:"rules_file,omitempty"`
	// ContextFile — path to this project's descriptive "state of the
	// project" doc (this repo's own DATAWATCH-CONTEXT.md is one example
	// of the convention; the name is never hardcoded). Explicitly
	// optional — not every project has one. Default "CONTEXT.md" is
	// applied at read time only when the file actually exists; an
	// operator with a differently-named file sets this instead of
	// renaming their file to match.
	ContextFile string `yaml:"context_file,omitempty" json:"context_file,omitempty"`
	// RulesContext — the per-project rules-file and context-file names
	// plus the rules/context digest sub-configuration
	// (autonomous.rules_context); the empty values above keep working as
	// the legacy fallback. See AutonomousRulesContextConfig.
	RulesContext AutonomousRulesContextConfig `yaml:"rules_context" json:"rules_context"`

	// Worker session profiles — what a worker session starts with (its MCP
	// servers, tools, skills, project context, model and effort). Built-in
	// profile names are minimal, digest, full and datawatch-tools; a user
	// profile may not reuse one of them. See internal/workerprofile.
	//
	// worker_profiles: user-defined profiles keyed by profile name. Each
	// value uses the workerprofile.WorkerProfile fields (name, mcp, tools,
	// skills, context, model, effort). A value's name, when set, must match
	// its key. Empty (default) = no user profiles; built-ins still apply.
	//
	// worker_profile_defaults: the profile each role and task kind starts
	// with when no profile is named. role keys: planner, worker, verifier,
	// judge. kind keys: implementation, docs, note, final_gate. Values are
	// built-in or user profile names. Empty (default) = built-in defaults.
	//
	// worker_profile: the global-layer profile name, used when no task,
	// automaton or project names one. Empty = the role default.
	//
	// Every setting is refused at load time by ValidateWorkerProfileConfig.
	WorkerProfiles        map[string]workerprofile.WorkerProfile `yaml:"worker_profiles" json:"worker_profiles"`
	WorkerProfileDefaults WorkerProfileDefaultsConfig            `yaml:"worker_profile_defaults" json:"worker_profile_defaults"`
	WorkerProfile         string                                 `yaml:"worker_profile" json:"worker_profile"`

	// UpstreamRepos — named targets a project-rules "file_upstream_issue"
	// action can reference, for gaps this repo's own PRD can't close
	// alone (e.g. a parity gap needing a companion-app issue).
	UpstreamRepos []UpstreamRepoConfig `yaml:"upstream_repos,omitempty" json:"upstream_repos,omitempty"`

	// BL407 Phase 1 — local git-worktree isolation. Empty (default) =
	// feature off, fully backward compatible. See
	// docs/plans/2026-10-10-prd-git-workflow.md.
	WorktreeBaseRepo string `yaml:"worktree_base_repo,omitempty" json:"worktree_base_repo,omitempty"`
	WorktreeDir      string `yaml:"worktree_dir,omitempty" json:"worktree_dir,omitempty"`

	// BL407 Phase 5 — daemon-wide default for a new PRD's Git.AutoPR
	// (push + open a PR automatically on completion), applied once at
	// PRD creation time. Per-PRD override via POST .../set_git takes
	// precedence after that. Default false (explicit opt-in), same as
	// the per-PRD field itself.
	DefaultAutoPR bool `yaml:"default_auto_pr,omitempty" json:"default_auto_pr,omitempty"`

	// Notify — lifecycle-notification knobs for the built-in lifecycle
	// sinks (comm fanout / alert paging).
	Notify AutonomousNotifyConfig `yaml:"notify" json:"notify"`

	// Questions — gate for the /api/questions endpoints (the Automaton
	// operator-question REST API). Ships off. See AutonomousQuestionsConfig
	// below.
	Questions AutonomousQuestionsConfig `yaml:"questions" json:"questions"`

	// Decisions — the decision record keys (autonomous.decisions.*). See
	// AutonomousDecisionsConfig below.
	Decisions AutonomousDecisionsConfig `yaml:"decisions" json:"decisions"`

	// MergeShadow — the shadow merge plan knobs (autonomous.merge_shadow).
	// See MergeShadowConfig in merge_shadow.go.
	MergeShadow MergeShadowConfig `yaml:"merge_shadow" json:"merge_shadow"`

	// Automation — knobs for Automation (multi-Automaton orchestration)
	// coordination. See AutonomousAutomationConfig.
	Automation AutonomousAutomationConfig `yaml:"automation" json:"automation"`

	// LaneFill — knobs for automatic lane filling (mode, lane counts,
	// idle alert, decompose-ahead, poll interval, lane backends). See
	// AutonomousLaneFillConfig.
	LaneFill AutonomousLaneFillConfig `yaml:"lane_fill" json:"lane_fill"`

	// Checks — mode of each automatic check (pre-start verifier, plan
	// rules, after-done report, registry README report) with optional
	// per-project overrides. See AutonomousChecksConfig.
	Checks AutonomousChecksConfig `yaml:"checks" json:"checks"`

	// Ship — policy for the push, tag and CI-watch steps after a merge
	// (modes, tag format, remote, CI workflows) with optional per-project
	// overrides. See AutonomousShipConfig.
	Ship AutonomousShipConfig `yaml:"ship" json:"ship"`
	// PriorityByKind — the priority assigned to an intake item by its kind
	// (bug / maintenance / feature / other). Shipped defaults are bug=80,
	// maintenance=70, feature=20, other=0; operator-supplied keys override
	// those defaults and unknown keys are kept. The map that is in effect is
	// returned by EffectivePriorityByKind. No omitempty: an operator's
	// explicit map edit (including clearing a kind to 0) must survive a JSON
	// round trip, same gotcha as PlanTierMaxInflight above.
	PriorityByKind map[string]int `yaml:"priority_by_kind" json:"priority_by_kind"`
	// MemoryLimitByKind — the per-task-kind memory ceiling override, keyed
	// by task kind. Each value is a positive MB number or the word "none";
	// a kind with no entry (or an empty value) uses the default. See
	// ValidateMemoryLimitByKind.
	MemoryLimitByKind map[string]string `yaml:"memory_limit_by_kind,omitempty" json:"memory_limit_by_kind,omitempty"`
	// DefectIntakeAutomation — the name of the Automaton this installation
	// takes defect intake on. Deliberately empty by default: each installation
	// chooses its own.
	DefectIntakeAutomation string `yaml:"defect_intake_automation,omitempty" json:"defect_intake_automation,omitempty"`

	// DecomposeLint — check over a freshly decomposed task graph
	// (autonomous.decompose_lint): mode off|shadow|enforce (default
	// shadow), the auto-retry budget (default 2, 0 allowed, cap 5) and
	// the lint rules. See DecomposeLintConfig.
	DecomposeLint DecomposeLintConfig `yaml:"decompose_lint" json:"decompose_lint"`
	// TaskTemplates — reusable task definitions offered at
	// decomposition time (autonomous.task_templates): enabled (default
	// true), the template list and per-project overrides. See
	// TaskTemplatesConfig.
	TaskTemplates TaskTemplatesConfig `yaml:"task_templates" json:"task_templates"`
	// DecomposeGuidance — literal guidance text or a file path handed to
	// the planner; empty (default) means the guidance is generated from
	// the configured decompose-lint rules (EffectiveDecomposeGuidance).
	DecomposeGuidance string `yaml:"decompose_guidance" json:"decompose_guidance"`
	// SupervisorLoop — the supervisor_loop settings as flat dotted keys
	// (for example "supervisor_loop.lane_count.max": 4,
	// "supervisor_loop.deploy_after_ship.enabled": false). Parsed by
	// supervisorloop.ParseSettings; absent keys take their defaults.
	SupervisorLoop SupervisorLoopConfig `yaml:"supervisor_loop,omitempty" json:"supervisor_loop,omitempty"`
	// Requirements — minimum backend version requirements. The shipped
	// table lives in code (internal/autonomous); this block holds only
	// operator overrides: autonomous.requirements.min_ollama.<capability>.
	// See AutonomousRequirementsConfig.
	Requirements AutonomousRequirementsConfig `yaml:"requirements" json:"requirements"`

	// Parallel — knobs for parallel task execution
	// (autonomous.parallel.*). See AutonomousParallelConfig.
	Parallel AutonomousParallelConfig `yaml:"parallel" json:"parallel"`

	// ScriptGateTimeoutSeconds caps how long a script gate may run before
	// it is stopped. Default DefaultScriptGateTimeoutSeconds (900). 0 means
	// no cap; a negative value is refused at load time. No omitempty, so an
	// explicit 0 survives a round trip. See EffectiveScriptGateTimeout in
	// script_gate.go.
	ScriptGateTimeoutSeconds int `yaml:"script_gate_timeout_seconds" json:"script_gate_timeout_seconds"`

	// ProjectGateCommand is the project's own build-and-test command the
	// gate runs (autonomous.project_gate_command). Empty means unset. It is
	// not tied to any language. Trimmed, at most 1024 bytes, no NUL or
	// newline. A project's policy block may override it. See
	// EffectiveProjectGateCommand in script_gate.go.
	ProjectGateCommand string `yaml:"project_gate_command,omitempty" json:"project_gate_command,omitempty"`

	// Progress — parameters of the progress metrics engine (lane count,
	// inactive-window gap, backlog file, start date, snapshot retention).
	// See AutonomousProgressConfig.
	Progress AutonomousProgressConfig `yaml:"progress" json:"progress"`

	// PlanReview (2026-10-17 pre-approval gate) — a one-shot LLM judge
	// over the freshly decomposed plan, run at the end of Decompose and
	// re-runnable via Manager.ReviewPlan. Internal wiring: see the
	// autonomous.Manager Config (internal/autonomous/manager.go) and
	// plan_review.go.
	// PlanReviewBackend / PlanReviewModel select the reviewer backend +
	// model (empty = daemon default backend chain).
	PlanReviewBackend string `yaml:"plan_review_backend,omitempty" json:"plan_review_backend,omitempty"`
	PlanReviewModel   string `yaml:"plan_review_model,omitempty" json:"plan_review_model,omitempty"`
	// PlanReviewMode: "" / "off" = disabled (default); "warn" = record
	// the verdict only; "block" = a negative verdict refuses
	// Manager.Approve until a new ok:true verdict lands. Any other
	// value is a config error (validateLoaded).
	PlanReviewMode string `yaml:"plan_review_mode,omitempty" json:"plan_review_mode,omitempty"`

	// SmokeVerificationBackend / SmokeVerificationModel — forward config
	// surface for the smoke-verification feature (an independent story
	// consumes these; default-off semantics).
	SmokeVerificationBackend string `yaml:"smoke_verification_backend,omitempty" json:"smoke_verification_backend,omitempty"`
	SmokeVerificationModel   string `yaml:"smoke_verification_model,omitempty" json:"smoke_verification_model,omitempty"`

	// Tags — the Automata tag browser settings (default view, attention
	// statuses, match mode, direct counts, shared registry keeper and sync
	// interval). See TagsConfig and ValidateTagSetting.
	Tags TagsConfig `yaml:"tags" json:"tags"`

	// AutoTags — the autonomous auto-tagging block (autonomous.auto_tags):
	// per-Automation, per-stage and per-kind tag name templates, and the
	// kind → tag-value labels. Unset templates and kind labels default, and
	// enabled is on unless set to false. See AutoTagsConfig and
	// ValidateAutoTagsSetting in auto_tags_settings.go.
	AutoTags AutoTagsConfig `yaml:"auto_tags" json:"auto_tags"`

	// HousekeepingRepairs — one off|report|on mode per repair group
	// (task_chain, advance_judge, lane_fill, leftovers; default report) and
	// the grace seconds. See housekeeping_repairs.go.
	HousekeepingRepairs HousekeepingRepairsConfig `yaml:"housekeeping_repairs" json:"housekeeping_repairs"`

	// Shipped — shipped-state surface for Automata (PRDs) and Automation:
	// master switch, chip display, default browser filter, separate shipped
	// counting and release backfill at startup. See ShippedConfig and
	// ValidateShippedSetting.
	Shipped ShippedConfig `yaml:"shipped" json:"shipped"`

	// DefectQueue — defect queue batching and tiny-fix criteria, with
	// per-project overrides. See DefectQueueConfig and
	// ValidateDefectQueueSetting.
	DefectQueue DefectQueueConfig `yaml:"defect_queue" json:"defect_queue"`
	// ModelPullConsent — the operator's model-pull consent
	// (autonomous.model_pull_consent): the models the Automation setup
	// needs, each with a pull allow flag, plus the publisher allow-list
	// (one entry allows every model of that publisher). Nothing is pulled
	// without a matching allow entry — the gate that reads this lives in
	// internal/autonomous (autonomous.ModelPullConsentCheck) and guards
	// the existing compute-node and marketplace pull paths. See
	// ModelPullConsent and ValidateModelPullConsent.
	ModelPullConsent ModelPullConsent `yaml:"model_pull_consent" json:"model_pull_consent"`
}

// ShippedSettingPrefix is the dotted key prefix shared by every
// autonomous.shipped.* setting.
const ShippedSettingPrefix = "autonomous.shipped."

// Legal values of autonomous.shipped.filter_default.
const (
	// ShippedFilterAll — the shipped-state filter shows everything
	// (shipped and unshipped).
	ShippedFilterAll = "all"
	// ShippedFilterShipped — the shipped-state filter shows only shipped
	// items.
	ShippedFilterShipped = "shipped"
	// ShippedFilterUnshipped — the shipped-state filter shows only items
	// that have not shipped yet.
	ShippedFilterUnshipped = "unshipped"
)

// Defaults of the autonomous.shipped block, used where a field is unset
// (a nil boolean pointer, an empty filter_default):
//
//	enabled           true  — shipped-state recording and display is on
//	show_chip         true  — the shipped chip renders on shipped items
//	filter_default    all   — the browser filter starts at "all"
//	count_separately  true  — shipped items count in their own group
//	backfill_on_start false — no release backfill runs at startup opt-out
//
// ShippedConfig is the autonomous.shipped block. Boolean fields are
// *bool with omitempty (the same convention as PreserveAttemptWork):
// nil = unset, which reads the default above — true and false both
// survive a save and a load.
type ShippedConfig struct {
	// Enabled is the master switch for shipped-state recording and
	// display. Unset (nil) reads true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// ShowChip renders the shipped chip on shipped items. Unset (nil)
	// reads true.
	ShowChip *bool `yaml:"show_chip,omitempty" json:"show_chip,omitempty"`
	// FilterDefault is the browser filter the shipped-state views open on:
	// ShippedFilterAll, ShippedFilterShipped or ShippedFilterUnshipped.
	// Empty reads ShippedFilterAll.
	FilterDefault string `yaml:"filter_default,omitempty" json:"filter_default,omitempty"`
	// CountSeparately puts shipped items in their own count group instead
	// of the active group. Unset (nil) reads true.
	CountSeparately *bool `yaml:"count_separately,omitempty" json:"count_separately,omitempty"`
	// BackfillOnStart runs the release-notes backfill (which records
	// shipped state with ShippedSourceBackfill) at daemon startup. Unset
	// (nil) reads false.
	BackfillOnStart *bool `yaml:"backfill_on_start,omitempty" json:"backfill_on_start,omitempty"`
}

// Effective reads of the shipped block: an unset field (nil pointer or
// empty string) returns its default; the stored value is not changed.
func (s ShippedConfig) EffectiveEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// EffectiveShowChip reports the in-force show-chip setting.
func (s ShippedConfig) EffectiveShowChip() bool {
	return s.ShowChip == nil || *s.ShowChip
}

// EffectiveFilterDefault reports the in-force default filter, canonical
// (lower-case, trimmed); empty reads ShippedFilterAll.
func (s ShippedConfig) EffectiveFilterDefault() string {
	f := strings.ToLower(strings.TrimSpace(s.FilterDefault))
	if f == "" {
		return ShippedFilterAll
	}
	return f
}

// EffectiveCountSeparately reports the in-force separate-count setting.
func (s ShippedConfig) EffectiveCountSeparately() bool {
	return s.CountSeparately == nil || *s.CountSeparately
}

// EffectiveBackfillOnStart reports the in-force startup backfill setting.
func (s ShippedConfig) EffectiveBackfillOnStart() bool {
	return s.BackfillOnStart != nil && *s.BackfillOnStart
}

// ValidateShippedSetting checks one autonomous.shipped.* setting. key is
// the full dotted key (for example autonomous.shipped.filter_default); a
// key outside the shipped block is not its concern and returns nil. value
// is what a config write carries (a bool, or the strings true, false,
// all, shipped, unshipped). The returned error text is the message every
// surface shows. A blank filter_default is legal and takes the default.
func ValidateShippedSetting(key string, value interface{}) error {
	name, ok := strings.CutPrefix(key, ShippedSettingPrefix)
	if !ok {
		return nil
	}
	_, err := parseShippedSetting(name, value)
	return err
}

// Set validates one setting and stores it on the block. name is the key
// without the autonomous.shipped. prefix. On error the block is unchanged.
func (s *ShippedConfig) Set(name string, value interface{}) error {
	v, err := parseShippedSetting(name, value)
	if err != nil {
		return err
	}
	switch name {
	case "enabled":
		s.Enabled = v.(*bool)
	case "show_chip":
		s.ShowChip = v.(*bool)
	case "filter_default":
		s.FilterDefault = v.(string)
	case "count_separately":
		s.CountSeparately = v.(*bool)
	case "backfill_on_start":
		s.BackfillOnStart = v.(*bool)
	}
	return nil
}

// shippedSettingErr builds the canonical error for one shipped setting.
func shippedSettingErr(name, reason string) error {
	return fmt.Errorf("%s%s: %s", ShippedSettingPrefix, name, reason)
}

// parseShippedSetting returns the stored form of one setting: a *bool or
// a canonical string. Unknown names are refused.
func parseShippedSetting(name string, value interface{}) (interface{}, error) {
	switch name {
	case "enabled", "show_chip", "count_separately", "backfill_on_start":
		b, err := parseShippedBool(name, value)
		if err != nil {
			return nil, err
		}
		return &b, nil
	case "filter_default":
		return parseShippedFilter(name, value)
	}
	return nil, shippedSettingErr(name, "unknown shipped setting")
}

// parseShippedBool accepts a JSON bool, or the strings true / false
// (case-insensitive).
func parseShippedBool(name string, value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, shippedSettingErr(name, "must be true or false")
}

// parseShippedFilter accepts a legal filter name (case-insensitive,
// trimmed); a blank value takes ShippedFilterAll.
func parseShippedFilter(name string, value interface{}) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", shippedSettingErr(name, fmt.Sprintf("must be %q, %q or %q", ShippedFilterAll, ShippedFilterShipped, ShippedFilterUnshipped))
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ShippedFilterAll, nil
	}
	switch s {
	case ShippedFilterAll, ShippedFilterShipped, ShippedFilterUnshipped:
		return s, nil
	}
	return "", shippedSettingErr(name, fmt.Sprintf("must be %q, %q or %q, got %q", ShippedFilterAll, ShippedFilterShipped, ShippedFilterUnshipped, value))
}

// loadShipped validates the shipped block at load time the way loadTags
// does for its block: a bad value in config.yaml refuses startup the same
// way every other config error does.
func loadShipped(cfg *Config) error {
	c := cfg.Autonomous.Shipped
	booleans := []struct {
		name string
		val  *bool
	}{
		{"enabled", c.Enabled},
		{"show_chip", c.ShowChip},
		{"count_separately", c.CountSeparately},
		{"backfill_on_start", c.BackfillOnStart},
	}
	for _, f := range booleans {
		if f.val == nil {
			continue // unset reads the default
		}
		if err := ValidateShippedSetting(ShippedSettingPrefix+f.name, *f.val); err != nil {
			return err
		}
	}
	return ValidateShippedSetting(ShippedSettingPrefix+"filter_default", c.FilterDefault)
}

// BranchRefreshLimit returns the behind-commit limit in effect: the
// configured value, or 5 when it is zero.
func (a AutonomousConfig) BranchRefreshLimit() int {
	if a.BranchRefreshBehindLimit == 0 {
		return 5
	}
	return a.BranchRefreshBehindLimit
}

// VerifierHeadroomMaxAgeHoursDefault is the default of
// autonomous.verifier_headroom_max_age_hours: a headroom measurement older
// than this many hours is stale for the verifier cap gates.
const VerifierHeadroomMaxAgeHoursDefault = 24

// EffectiveVerifierMinFreeMB is the free-memory floor (MB) the verifier cap
// gates use: autonomous.verifier_min_free_mem_mb when set (0 = check off),
// otherwise the default of 1024. It delegates to
// compute.EffectiveVerifierMinFreeMB, so the default lives in one place.
// The admission gate and the verifier_slots write gate both read this.
func (a AutonomousConfig) EffectiveVerifierMinFreeMB() int {
	return compute.EffectiveVerifierMinFreeMB(a.VerifierMinFreeMemMB)
}

// EffectiveVerifierHeadroomMaxAge is the headroom freshness bound the
// verifier cap gates use: autonomous.verifier_headroom_max_age_hours, with
// 0 or unset reading the default of 24 hours. The admission gate and the
// verifier_slots write gate both read this.
func (a AutonomousConfig) EffectiveVerifierHeadroomMaxAge() time.Duration {
	hours := a.VerifierHeadroomMaxAgeHours
	if hours <= 0 {
		hours = VerifierHeadroomMaxAgeHoursDefault
	}
	return time.Duration(hours) * time.Hour
}

// PreserveAttemptWorkEnabled reports whether failed-attempt work is kept.
// An unset (nil) PreserveAttemptWork means true.
func (a AutonomousConfig) PreserveAttemptWorkEnabled() bool {
	return a.PreserveAttemptWork == nil || *a.PreserveAttemptWork
}

// EffectiveSnapshotNeverStage returns the never-stage patterns in effect:
// the configured list (an explicitly empty list disables every never-stage
// rule), or DefaultSnapshotNeverStage when unset (nil). Every call site
// resolves the default through this accessor, so none hard-codes one.
func (a AutonomousConfig) EffectiveSnapshotNeverStage() []string {
	if a.SnapshotNeverStage == nil {
		return DefaultSnapshotNeverStage
	}
	return a.SnapshotNeverStage
}

// ValidateSnapshotNeverStage runs the one structural check every
// snapshot_never_stage entry must pass: non-empty after trimming, the same
// check the security_workflows load path performs. Unset (nil) and an
// explicit empty list are both legal; malformed glob syntax is not an
// error (a pattern that path.Match rejects is treated as simply never
// matching, exactly like gitguard's IgnorePatterns).
func ValidateSnapshotNeverStage(patterns []string) error {
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			return errors.New("autonomous.snapshot_never_stage: patterns must be non-empty")
		}
	}
	return nil
}

// ValidateDriftMode accepts the two legal autonomous.drift_mode values,
// "block" and "warn" (case-insensitive, trimmed — the executor's
// resolveDriftMode normalizes the same way), and rejects every other
// value with a clear error naming the accepted set. PUT /api/config
// and PUT /api/autonomous/config both run this before any write.
func ValidateDriftMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "block", "warn":
		return nil
	default:
		return fmt.Errorf("autonomous.drift_mode: %q is not a valid value (must be \"block\" or \"warn\")", mode)
	}
}

// ValidateUndeclaredEditMode accepts the three legal
// autonomous.undeclared_edit_mode values, "off", "warn" and "block"
// (case-insensitive, trimmed), and rejects every other value.
func ValidateUndeclaredEditMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off", "warn", "block":
		return nil
	default:
		return fmt.Errorf("autonomous.undeclared_edit_mode: %q is not a valid value (must be \"off\", \"warn\" or \"block\")", mode)
	}
}

// AutonomousAutomationConfig — autonomous.automation sub-config.
// IdleAlertMinutes is how long an active Automation may sit idle (a member
// finished and no next member started) before the coordinator raises one
// alert for that idle episode. 0 (the default) turns the alert off. Not
// omitempty, so an explicit 0 survives a round trip.
//
// AgentInsertPolicy decides what happens when an agent session inserts a member
// into an Automation: "allow" (the default) lets the insert land directly and
// "require_operator" holds it as a pending insert until an operator approves it.
// An empty value means "allow".
//
// TriageAlertMinutes is how long (whole minutes) an Automaton may sit failed or
// blocked before the coordinator's poll-time triage pass raises one triage
// alert for the episode. It is a pointer on purpose: an unset key
// (nil) reads the default of DefaultAutomationTriageAlertMinutes (30), while an
// explicit 0 (a pointer to zero) turns the triage alert off and must survive a
// round trip. Negative values are rejected with a 400 on every write path and
// with an error on config load.
const DefaultAutomationTriageAlertMinutes = 30

// MergePendingMode is the autonomous.automation.merge_pending_mode setting: it
// says whether the merge-pending check of finished Automaton branches is off,
// runs in shadow (records and reports only) or is on. An empty value reads as
// MergePendingModeOff. MergePendingAlertMinutes is how long (whole minutes) an
// Automaton branch may sit finished but unmerged before the merge-pending alert
// is raised. It is a pointer for the same reason as TriageAlertMinutes: unset
// (nil) reads DefaultAutomationMergePendingAlertMinutes (30), an explicit 0
// turns the merge-pending alert off and must survive a round trip, and a
// negative value is rejected on every write path and on config load.
const (
	MergePendingModeOff    = "off"
	MergePendingModeShadow = "shadow"
	MergePendingModeOn     = "on"

	DefaultAutomationMergePendingAlertMinutes = 30
)

// MergedMemberPolicy — the autonomous.automation.merged_member_policy
// setting: completed, manual or block. An empty value reads as
// MergedMemberPolicyCompleted.
const (
	MergedMemberPolicyCompleted = "completed"
	MergedMemberPolicyManual    = "manual"
	MergedMemberPolicyBlock     = "block"
)

type AutonomousAutomationConfig struct {
	IdleAlertMinutes   int  `yaml:"idle_alert_minutes" json:"idle_alert_minutes"`
	TriageAlertMinutes *int `yaml:"triage_alert_minutes,omitempty" json:"triage_alert_minutes,omitempty"`
	// RepoCIWatch — the repo CI watch knobs
	// (autonomous.automation.repo_ci_watch.*). See RepoCIWatchConfig: the
	// defaults are applied only in its Effective* accessors, and the bad
	// values (a mode other than off|shadow|on, delete_superseded other
	// than report|delete, a non-absolute repository path, a poll interval
	// below 60, a negative alert/tag threshold, a hung threshold below 1)
	// are refused on every write path.
	RepoCIWatch RepoCIWatchConfig `yaml:"repo_ci_watch" json:"repo_ci_watch"`
	// DependencyUpdate — the weekly dependency update Automation knobs
	// (autonomous.automation.dependency_update.*). See DependencyUpdateConfig.
	DependencyUpdate         DependencyUpdateConfig `yaml:"dependency_update" json:"dependency_update"`
	MergePendingMode         string                 `yaml:"merge_pending_mode,omitempty" json:"merge_pending_mode,omitempty"`
	MergePendingAlertMinutes *int                   `yaml:"merge_pending_alert_minutes,omitempty" json:"merge_pending_alert_minutes,omitempty"`
	MergedMemberPolicy       string                 `yaml:"merged_member_policy,omitempty" json:"merged_member_policy,omitempty"`
	AgentInsertPolicy        string                 `yaml:"agent_insert_policy" json:"agent_insert_policy"`
	// SecurityFailure — the security failure block
	// (autonomous.automation.security_failure.*). See
	// SecurityFailureConfig: the one default (an unset or empty mode reads
	// shadow, the first-release default — the feature is switched on in
	// shadow mode first, with no behaviour change) is applied only in its
	// Effective* accessor, and a mode other than off|shadow|on is refused
	// on every write path.
	SecurityFailure SecurityFailureConfig `yaml:"security_failure" json:"security_failure"`
	// SupervisorWatch — the supervisor watch block
	// (autonomous.automation.supervisor_watch.*). See
	// SupervisorWatchConfig: the defaults (an unset or empty mode reads
	// shadow, the release default — reports only, never acts; an unset
	// alert minutes reads the shared DefaultAutomationTriageAlertMinutes
	// (30) default; an unset silent minutes reads the 60-minute default)
	// are applied only in its Effective* accessors, a mode other than
	// off|shadow|on is refused on every write path, and a negative minute
	// threshold is refused on every write path.
	SupervisorWatch       SupervisorWatchConfig `yaml:"supervisor_watch" json:"supervisor_watch"`
	CreateSearchMode      string                `yaml:"create_search_mode" json:"create_search_mode"`
	CreateSearchThreshold *float64              `yaml:"create_search_threshold,omitempty" json:"create_search_threshold,omitempty"`
	// Cleanup — the safe-to-delete cleanup knobs (cleanup_mode,
	// cleanup_retention_hours, cleanup_max_removals_per_pass,
	// disk_alert_percent). The write path (internal/server/cleanup.go)
	// refuses a cleanup_mode outside off|report|on, retention hours and
	// per-pass removal caps below 1 or fractional, and a disk_alert_percent
	// outside 1..100 with a 400. The read side is still tolerant, for a
	// hand-edited config file: every read goes through the Effective*
	// accessors, so an unset key or an unrecognized cleanup_mode reads as
	// CleanupModeReport (record and report only), zero or negative retention
	// hours and removal caps read their defaults, and disk_alert_percent is
	// clamped to 1..100 (unset reads 85).
	CleanupMode               string `yaml:"cleanup_mode,omitempty" json:"cleanup_mode,omitempty"`
	CleanupRetentionHours     int    `yaml:"cleanup_retention_hours,omitempty" json:"cleanup_retention_hours,omitempty"`
	CleanupMaxRemovalsPerPass int    `yaml:"cleanup_max_removals_per_pass,omitempty" json:"cleanup_max_removals_per_pass,omitempty"`
	DiskAlertPercent          int    `yaml:"disk_alert_percent,omitempty" json:"disk_alert_percent,omitempty"`
	// UncommittedWorkPolicy decides what happens to a task whose work is not
	// committed after verification (hold|warn|off): hold = the task is held
	// with the reason and the files, and a question is raised; warn = record
	// and alert but let the task complete; off = no check and no commit
	// attempt. An unset key or an unknown word reads as hold (the safe
	// default) through EffectiveUncommittedWorkPolicy.
	UncommittedWorkPolicy string `yaml:"uncommitted_work_policy,omitempty" json:"uncommitted_work_policy,omitempty"`
	// UncommittedWorkScope sets which files the per-task uncommitted-work
	// check looks at (task_files|worktree): task_files = the files the task
	// itself changed; worktree = every uncommitted file in the worktree. This
	// scope governs the per-task check only; the check before an Automaton
	// is reported finished always covers the whole worktree. An unset key or
	// an unknown word reads as task_files through
	// EffectiveUncommittedWorkScope.
	UncommittedWorkScope string `yaml:"uncommitted_work_scope,omitempty" json:"uncommitted_work_scope,omitempty"`
	// ProtectedPaths lists the paths a task commit must not touch without the
	// operator. Each entry is a repository-relative exact file path, a
	// directory (trailing slash, matches everything below it) or a glob using
	// * ? [ ] (path.Match semantics, no **; an entry ending in /** is the same
	// as the directory form). An unset or empty list reads the default list
	// (DefaultProtectedPaths) through EffectiveProtectedPaths; an explicit
	// list replaces the default entirely.
	ProtectedPaths []string `yaml:"protected_paths,omitempty" json:"protected_paths,omitempty"`
	// ProtectedPathsAction decides what happens when a task commit touches a
	// protected path (hold|warn|block|off): hold = the task is held for the
	// operator; warn = record and alert but let the task complete; block =
	// refuse the commit; off = no check. An unset key or an unknown word
	// reads as hold (the safe default) through EffectiveProtectedPathsAction.
	ProtectedPathsAction string `yaml:"protected_paths_action,omitempty" json:"protected_paths_action,omitempty"`
	// RulesReviewSource sets where the rules review reads the project rules
	// from (base_commit|worktree): base_commit = the rules as committed on the
	// base the work branched from, so a task cannot change the rubric it is
	// judged by; worktree = the rules file in the task's working tree. An
	// unset key or an unknown word reads as base_commit through
	// EffectiveRulesReviewSource.
	RulesReviewSource string `yaml:"rules_review_source,omitempty" json:"rules_review_source,omitempty"`
	// PendingItemPolicy decides what an Automation does when its cursor
	// reaches a pending item (a named piece of work with no Automaton yet)
	// that is still waiting (hold|skip): hold = the Automation stays at the
	// item until an Automaton is attached; skip = the item is passed over
	// and the Automation moves on. An unset key or an unknown word reads as
	// hold (the safe default) through EffectivePendingItemPolicy.
	PendingItemPolicy string `yaml:"pending_item_policy,omitempty" json:"pending_item_policy,omitempty"`
	// PendingItemAutoResume controls whether an Automation that is holding
	// at a pending item resumes by itself once an Automaton is attached to
	// that item. A pointer so that unset (nil) reads true; an explicit false
	// keeps the Automation held until an operator resumes it. See
	// EffectivePendingItemAutoResume.
	PendingItemAutoResume *bool `yaml:"pending_item_auto_resume,omitempty" json:"pending_item_auto_resume,omitempty"`
	// PendingItemSource — the optional file registry the daemon reads
	// pending items from (autonomous.automation.pending_item_source.*). See
	// PendingItemSourceConfig: the defaults apply only in its Effective*
	// accessors, and the bad values are refused on every write path through
	// ValidatePendingItemKey.
	PendingItemSource PendingItemSourceConfig `yaml:"pending_item_source" json:"pending_item_source"`
}

// PendingItemSourceConfig is the autonomous.automation.pending_item_source
// block: where an optional file registry of pending items lives and how its
// rows map to pending items. Nothing in it assumes a numbering sequence, a
// file name or a repository convention. A column setting is a header name
// (matched case-insensitively) or a 1-based column number. Every default is
// applied only in the Effective* accessors, so an unset key stays unset in
// the file and in a config round trip.
type PendingItemSourceConfig struct {
	// Path is the registry file. Empty (the default) means no file registry.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// IDPattern is a Go regular expression a row's id must match to count as
	// a pending item. Unset reads PendingItemSourceDefaultIDPattern.
	IDPattern string `yaml:"id_pattern,omitempty" json:"id_pattern,omitempty"`
	// IDColumn names the column holding the item id: a header name or a
	// 1-based column number. Unset reads "1".
	IDColumn string `yaml:"id_column,omitempty" json:"id_column,omitempty"`
	// TitleColumn names the column holding the item title: a header name or
	// a 1-based column number. Unset reads "2".
	TitleColumn string `yaml:"title_column,omitempty" json:"title_column,omitempty"`
	// StateColumn names the column holding the item state: a header name or
	// a 1-based column number. Empty (the default) means the registry has no
	// state column.
	StateColumn string `yaml:"state_column,omitempty" json:"state_column,omitempty"`
	// DoneValues are the state-column values (compared case-insensitively)
	// that mark an item done. Unset or empty reads done, closed, shipped.
	DoneValues []string `yaml:"done_values,omitempty" json:"done_values,omitempty"`
	// OnMissingRow decides what happens to an item whose row has vanished
	// from the registry file (keep|drop). Unset or an unknown word reads
	// keep (the safe default).
	OnMissingRow string `yaml:"on_missing_row,omitempty" json:"on_missing_row,omitempty"`
}

// autonomous.automation pending-item values and defaults.
// PendingItemPolicy: hold (the default) keeps an Automation at a waiting
// pending item; skip passes over it. PendingItemSource.OnMissingRow: keep
// (the default) leaves an item in place when its registry row vanishes; drop
// removes it.
const (
	PendingItemPolicyHold = "hold"
	PendingItemPolicySkip = "skip"

	PendingItemOnMissingRowKeep = "keep"
	PendingItemOnMissingRowDrop = "drop"

	// DefaultPendingItemAutoResume is the auto-resume default (on).
	DefaultPendingItemAutoResume = true
	// PendingItemSourceDefaultIDPattern matches any non-blank token.
	PendingItemSourceDefaultIDPattern = `^\S+$`
	// PendingItemSourceDefaultIDColumn and ...TitleColumn are 1-based column
	// numbers.
	PendingItemSourceDefaultIDColumn    = "1"
	PendingItemSourceDefaultTitleColumn = "2"

	// PendingItemIDPatternMaxLen caps the id_pattern length.
	PendingItemIDPatternMaxLen = 512
)

// The full dotted names ValidatePendingItemKey accepts.
const (
	PendingItemKeyPolicy       = "autonomous.automation.pending_item_policy"
	PendingItemKeyAutoResume   = "autonomous.automation.pending_item_auto_resume"
	PendingItemKeyPath         = "autonomous.automation.pending_item_source.path"
	PendingItemKeyIDPattern    = "autonomous.automation.pending_item_source.id_pattern"
	PendingItemKeyIDColumn     = "autonomous.automation.pending_item_source.id_column"
	PendingItemKeyTitleColumn  = "autonomous.automation.pending_item_source.title_column"
	PendingItemKeyStateColumn  = "autonomous.automation.pending_item_source.state_column"
	PendingItemKeyDoneValues   = "autonomous.automation.pending_item_source.done_values"
	PendingItemKeyOnMissingRow = "autonomous.automation.pending_item_source.on_missing_row"
)

// DefaultPendingItemDoneValues returns a fresh copy of the default done
// values (done, closed, shipped).
func DefaultPendingItemDoneValues() []string {
	return []string{"done", "closed", "shipped"}
}

// EffectivePendingItemPolicy returns the pending-item policy in effect: skip
// is an explicit operator choice; an unset key (empty) and any unknown value
// read as PendingItemPolicyHold, the safe default.
func (c AutonomousAutomationConfig) EffectivePendingItemPolicy() string {
	if c.PendingItemPolicy == PendingItemPolicySkip {
		return PendingItemPolicySkip
	}
	return PendingItemPolicyHold
}

// EffectivePendingItemAutoResume returns whether a held Automation resumes
// by itself once an Automaton is attached: unset (nil) reads true.
func (c AutonomousAutomationConfig) EffectivePendingItemAutoResume() bool {
	if c.PendingItemAutoResume == nil {
		return DefaultPendingItemAutoResume
	}
	return *c.PendingItemAutoResume
}

// EffectivePath returns the registry file path; empty means no file registry.
func (s PendingItemSourceConfig) EffectivePath() string { return s.Path }

// EffectiveIDPattern returns the id pattern in effect: unset (blank) reads
// PendingItemSourceDefaultIDPattern.
func (s PendingItemSourceConfig) EffectiveIDPattern() string {
	if strings.TrimSpace(s.IDPattern) == "" {
		return PendingItemSourceDefaultIDPattern
	}
	return s.IDPattern
}

// EffectiveIDColumn returns the id column in effect: unset reads "1".
func (s PendingItemSourceConfig) EffectiveIDColumn() string {
	if strings.TrimSpace(s.IDColumn) == "" {
		return PendingItemSourceDefaultIDColumn
	}
	return strings.TrimSpace(s.IDColumn)
}

// EffectiveTitleColumn returns the title column in effect: unset reads "2".
func (s PendingItemSourceConfig) EffectiveTitleColumn() string {
	if strings.TrimSpace(s.TitleColumn) == "" {
		return PendingItemSourceDefaultTitleColumn
	}
	return strings.TrimSpace(s.TitleColumn)
}

// EffectiveStateColumn returns the state column in effect; empty means the
// registry has no state column.
func (s PendingItemSourceConfig) EffectiveStateColumn() string {
	return strings.TrimSpace(s.StateColumn)
}

// EffectiveDoneValues returns the done values in effect: unset or empty
// reads done, closed, shipped. The result is always a fresh slice.
func (s PendingItemSourceConfig) EffectiveDoneValues() []string {
	if len(s.DoneValues) == 0 {
		return DefaultPendingItemDoneValues()
	}
	return append([]string(nil), s.DoneValues...)
}

// EffectiveOnMissingRow returns the missing-row behavior in effect: drop is
// an explicit operator choice; an unset key and any unknown word read keep.
func (s PendingItemSourceConfig) EffectiveOnMissingRow() string {
	if s.OnMissingRow == PendingItemOnMissingRowDrop {
		return PendingItemOnMissingRowDrop
	}
	return PendingItemOnMissingRowKeep
}

// ValidatePendingItemKey checks one pending-item setting before it is
// written. key is the full dotted name (for example
// "autonomous.automation.pending_item_policy"); value is the new value as
// text (done_values is a comma-separated list). REST, MCP and the comm
// channel all call this one function. An empty value is accepted for every
// key except pending_item_auto_resume and means unset (the default).
func ValidatePendingItemKey(key, value string) error {
	switch key {
	case PendingItemKeyPolicy:
		switch value {
		case "", PendingItemPolicyHold, PendingItemPolicySkip:
			return nil
		}
		return fmt.Errorf("%s must be \"hold\" or \"skip\", got %q", key, value)
	case PendingItemKeyAutoResume:
		switch value {
		case "true", "false":
			return nil
		}
		return fmt.Errorf("%s must be \"true\" or \"false\", got %q", key, value)
	case PendingItemKeyOnMissingRow:
		switch value {
		case "", PendingItemOnMissingRowKeep, PendingItemOnMissingRowDrop:
			return nil
		}
		return fmt.Errorf("%s must be \"keep\" or \"drop\", got %q", key, value)
	case PendingItemKeyIDPattern:
		if len(value) > PendingItemIDPatternMaxLen {
			return fmt.Errorf("%s must be at most %d characters, got %d", key, PendingItemIDPatternMaxLen, len(value))
		}
		if value == "" {
			return nil
		}
		if _, err := regexp.Compile(value); err != nil {
			return fmt.Errorf("%s must be a valid regular expression: %v", key, err)
		}
		return nil
	case PendingItemKeyIDColumn, PendingItemKeyTitleColumn, PendingItemKeyStateColumn:
		if value != "" && strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must be a header name or a 1-based column number, not blank", key)
		}
		return nil
	case PendingItemKeyDoneValues:
		for _, v := range strings.Split(value, ",") {
			if value != "" && strings.TrimSpace(v) == "" {
				return fmt.Errorf("%s must be a comma-separated list of non-blank words", key)
			}
		}
		return nil
	case PendingItemKeyPath:
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%s must not contain a NUL byte", key)
		}
		return nil
	default:
		return fmt.Errorf("unknown pending item setting %q", key)
	}
}

// autonomous.automation.create_search_mode values and defaults.
const (
	CreateSearchModeOff               = "off"
	CreateSearchModeShadow            = "shadow"
	CreateSearchModeOn                = "on"
	DefaultAutomationCreateSearchMode = CreateSearchModeShadow

	DefaultAutomationCreateSearchThreshold = 0.5
)

// ValidateCreateSearchMode accepts the empty string (the default, shadow),
// off, shadow, and on; anything else is an error.
func ValidateCreateSearchMode(mode string) error {
	switch mode {
	case "", CreateSearchModeOff, CreateSearchModeShadow, CreateSearchModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.create_search_mode must be \"off\", \"shadow\" or \"on\", got %q", mode)
	}
}

// EffectiveCreateSearchMode returns the search-before-create mode in effect:
// an empty value reads as shadow.
func (c AutonomousAutomationConfig) EffectiveCreateSearchMode() string {
	if c.CreateSearchMode == "" {
		return CreateSearchModeShadow
	}
	return c.CreateSearchMode
}

// ValidateCreateSearchThreshold accepts a finite value in (0, 1]; zero,
// negative, non-finite, and above-1 values are errors.
func ValidateCreateSearchThreshold(threshold float64) error {
	if threshold != threshold || threshold == math.Inf(1) || threshold == math.Inf(-1) {
		return fmt.Errorf("autonomous.automation.create_search_threshold must be finite, got %g", threshold)
	}
	if threshold <= 0 || threshold > 1 {
		return fmt.Errorf("autonomous.automation.create_search_threshold must be in (0, 1], got %g", threshold)
	}
	return nil
}

// EffectiveCreateSearchThreshold returns the search-before-create threshold
// in effect: an unset key (nil) reads the default of
// DefaultAutomationCreateSearchThreshold (0.5).
func (c AutonomousAutomationConfig) EffectiveCreateSearchThreshold() float64 {
	if c.CreateSearchThreshold == nil {
		return DefaultAutomationCreateSearchThreshold
	}
	return *c.CreateSearchThreshold
}

// autonomous.automation cleanup values and defaults. CleanupMode decides
// whether the safe-to-delete cleanup pass is off, records and reports only
// (the default) or on. CleanupRetentionHours is how long a
// removal candidate may sit unused (whole hours) before it counts as
// retained for cleanup; CleanupMaxRemovalsPerPass bounds how many removals
// one pass may perform; DiskAlertPercent is the disk-usage percent at or
// above which a disk alert is raised.
const (
	CleanupModeOff    = "off"
	CleanupModeReport = "report"
	CleanupModeOn     = "on"

	DefaultAutomationCleanupRetentionHours     = 24
	DefaultAutomationCleanupMaxRemovalsPerPass = 10
	DefaultAutomationDiskAlertPercent          = 85
)

// EffectiveCleanupMode returns the cleanup mode in effect: an unset key
// (empty) and any unknown value read as CleanupModeReport; off and on are
// explicit operator choices. This is deliberately more forgiving than
// EffectiveMergePendingMode or EffectiveCreateSearchMode: unknown words are
// legal values that resolve to report, not write-path errors.
func (c AutonomousAutomationConfig) EffectiveCleanupMode() string {
	switch c.CleanupMode {
	case CleanupModeOff, CleanupModeOn:
		return c.CleanupMode
	default:
		return CleanupModeReport
	}
}

// autonomous.automation uncommitted-work values. UncommittedWorkPolicy
// decides what the per-task check does with work that is still uncommitted
// after verification: hold (the default) holds the task with the reason and
// files and raises a question; warn records and alerts but lets the task
// complete; off skips the check and any commit attempt.
// UncommittedWorkScope decides which files that check covers: task_files
// (the default) is the files the task itself changed, worktree is every
// uncommitted file in the worktree. The scope governs the per-task check
// only; the check before an Automaton is reported finished always covers the
// whole worktree.
const (
	UncommittedWorkPolicyHold = "hold"
	UncommittedWorkPolicyWarn = "warn"
	UncommittedWorkPolicyOff  = "off"

	UncommittedWorkScopeTaskFiles = "task_files"
	UncommittedWorkScopeWorktree  = "worktree"
)

// EffectiveUncommittedWorkPolicy returns the uncommitted-work policy in
// effect: warn and off are explicit operator choices; an unset key (empty)
// and any unknown value read as UncommittedWorkPolicyHold, the safe default.
func (c AutonomousAutomationConfig) EffectiveUncommittedWorkPolicy() string {
	switch c.UncommittedWorkPolicy {
	case UncommittedWorkPolicyWarn, UncommittedWorkPolicyOff:
		return c.UncommittedWorkPolicy
	default:
		return UncommittedWorkPolicyHold
	}
}

// EffectiveUncommittedWorkScope returns the uncommitted-work scope in
// effect: worktree is an explicit operator choice; an unset key (empty) and
// any unknown value read as UncommittedWorkScopeTaskFiles. The scope governs
// the per-task check only; the check before an Automaton is reported
// finished always covers the whole worktree.
func (c AutonomousAutomationConfig) EffectiveUncommittedWorkScope() string {
	if c.UncommittedWorkScope == UncommittedWorkScopeWorktree {
		return UncommittedWorkScopeWorktree
	}
	return UncommittedWorkScopeTaskFiles
}

// autonomous.automation protected-paths and rules-review values.
// ProtectedPathsAction decides what happens when a task commit touches a
// protected path: hold (the default) holds the task for the operator; warn
// records and alerts but lets the task complete; block refuses the commit;
// off skips the check. RulesReviewSource decides where the rules review reads
// the project rules: base_commit (the default) is the rules as committed on
// the base, worktree is the file in the task's working tree.
const (
	ProtectedPathsActionHold  = "hold"
	ProtectedPathsActionWarn  = "warn"
	ProtectedPathsActionBlock = "block"
	ProtectedPathsActionOff   = "off"

	RulesReviewSourceBaseCommit = "base_commit"
	RulesReviewSourceWorktree   = "worktree"
)

// defaultProtectedRulesFile is the rules file name used when
// autonomous.rules_file is unset.
const defaultProtectedRulesFile = "AGENT.md"

// DefaultProtectedPaths returns the protected-path list that applies when
// autonomous.automation.protected_paths is unset or empty: the effective
// project rules file (rulesFile, AGENT.md when empty), the supervisor rules
// document, CODEOWNERS and the workflows directory. The entries are
// normalised the same way as an explicit list.
func DefaultProtectedPaths(rulesFile string) []string {
	if strings.TrimSpace(rulesFile) == "" {
		rulesFile = defaultProtectedRulesFile
	}
	return NormalizeProtectedPaths([]string{
		rulesFile,
		"docs/plans/2026-10-17-supervisor-rules-and-lessons.md",
		"CODEOWNERS",
		".github/workflows/",
	})
}

// NormalizeProtectedPaths cleans a protected-path list: every entry is
// trimmed, a leading "./" or "/" is removed, a trailing "/**" becomes the
// directory form, the rest is cleaned with path.Clean (a trailing slash on a
// directory entry is kept). Empty entries and entries with a ".." segment are
// dropped, duplicates are removed and the order is kept.
func NormalizeProtectedPaths(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		e := strings.TrimSpace(raw)
		for {
			if strings.HasPrefix(e, "./") {
				e = e[2:]
			} else if strings.HasPrefix(e, "/") {
				e = e[1:]
			} else {
				break
			}
		}
		hasDotDot := false
		for _, seg := range strings.Split(e, "/") {
			if seg == ".." {
				hasDotDot = true
				break
			}
		}
		if hasDotDot {
			continue
		}
		dir := strings.HasSuffix(e, "/")
		if strings.HasSuffix(e, "/**") {
			e = strings.TrimSuffix(e, "**")
			dir = true
		}
		e = path.Clean(e)
		if e == "." || e == "" || e == "**" {
			continue
		}
		if dir {
			e += "/"
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// EffectiveProtectedPaths returns the protected-path list in effect: the
// normalised explicit list when it has at least one usable entry, otherwise
// DefaultProtectedPaths(rulesFile). rulesFile is the effective
// autonomous.rules_file value (empty reads AGENT.md).
func (c AutonomousAutomationConfig) EffectiveProtectedPaths(rulesFile string) []string {
	if len(c.ProtectedPaths) > 0 {
		if list := NormalizeProtectedPaths(c.ProtectedPaths); len(list) > 0 {
			return list
		}
	}
	return DefaultProtectedPaths(rulesFile)
}

// EffectiveProtectedPathsAction returns the protected-paths action in
// effect: warn, block and off are explicit operator choices; an unset key
// (empty) and any unknown value read as ProtectedPathsActionHold, the safe
// default.
func (c AutonomousAutomationConfig) EffectiveProtectedPathsAction() string {
	switch c.ProtectedPathsAction {
	case ProtectedPathsActionWarn, ProtectedPathsActionBlock, ProtectedPathsActionOff:
		return c.ProtectedPathsAction
	default:
		return ProtectedPathsActionHold
	}
}

// EffectiveRulesReviewSource returns where the rules review reads the
// project rules from: worktree is an explicit operator choice; an unset key
// (empty) and any unknown value read as RulesReviewSourceBaseCommit.
func (c AutonomousAutomationConfig) EffectiveRulesReviewSource() string {
	if c.RulesReviewSource == RulesReviewSourceWorktree {
		return RulesReviewSourceWorktree
	}
	return RulesReviewSourceBaseCommit
}

// EffectiveCleanupRetentionHours returns the cleanup retention window in
// whole hours: zero or a negative value reads the default of
// DefaultAutomationCleanupRetentionHours (24); an unset key and an explicit
// 0 are indistinguishable and both read the default.
func (c AutonomousAutomationConfig) EffectiveCleanupRetentionHours() int {
	if c.CleanupRetentionHours <= 0 {
		return DefaultAutomationCleanupRetentionHours
	}
	return c.CleanupRetentionHours
}

// EffectiveCleanupMaxRemovalsPerPass returns the per-pass removal cap: zero
// or a negative value reads the default of
// DefaultAutomationCleanupMaxRemovalsPerPass (10).
func (c AutonomousAutomationConfig) EffectiveCleanupMaxRemovalsPerPass() int {
	if c.CleanupMaxRemovalsPerPass <= 0 {
		return DefaultAutomationCleanupMaxRemovalsPerPass
	}
	return c.CleanupMaxRemovalsPerPass
}

// EffectiveDiskAlertPercent returns the disk-usage alert percent in effect:
// an unset key (zero) reads the default of DefaultAutomationDiskAlertPercent
// (85), and any other value is clamped into 1..100.
func (c AutonomousAutomationConfig) EffectiveDiskAlertPercent() int {
	p := c.DiskAlertPercent
	if p == 0 {
		return DefaultAutomationDiskAlertPercent
	}
	if p < 1 {
		p = 1
	}
	if p > 100 {
		p = 100
	}
	return p
}

// Shipped priority_by_kind table. Operator policy, not a technical limit:
// override any kind (or add kinds of your own) in
// autonomous.priority_by_kind; the effective map is built by
// DefaultPriorityByKindMap and merged by EffectivePriorityByKind.
const (
	DefaultPriorityByKindBug         = 80
	DefaultPriorityByKindMaintenance = 70
	DefaultPriorityByKindFeature     = 20
	DefaultPriorityByKindOther       = 0
)

// DefaultPriorityByKindMap returns a fresh copy of the shipped
// priority_by_kind table: bug=80, maintenance=70, feature=20, other=0.
func DefaultPriorityByKindMap() map[string]int {
	return map[string]int{
		"bug":         DefaultPriorityByKindBug,
		"maintenance": DefaultPriorityByKindMaintenance,
		"feature":     DefaultPriorityByKindFeature,
		"other":       DefaultPriorityByKindOther,
	}
}

// EffectivePriorityByKind returns the priority table that is in effect: the
// shipped defaults with the operator's autonomous.priority_by_kind entries
// layered on top. Operator-supplied keys override the defaults and unknown
// keys are kept; unset kinds fall back to their shipped value, so an empty
// config still yields the full table.
func (c AutonomousConfig) EffectivePriorityByKind() map[string]int {
	m := DefaultPriorityByKindMap()
	for k, v := range c.PriorityByKind {
		m[k] = v
	}
	return m
}

// autonomous.lane_fill.mode values and defaults.
const (
	LaneFillModeOff    = "off"
	LaneFillModeShadow = "shadow"
	LaneFillModeOn     = "on"

	DefaultLaneFillGPULanes          = 2
	DefaultLaneFillClaudeLanes       = 3
	DefaultLaneFillIdleAlertMinutes  = 30
	DefaultLaneFillDecomposeAheadMin = 2
	DefaultLaneFillPollSeconds       = 60
	MinLaneFillPollSeconds           = 5
	DefaultLaneFillGPUBackend        = "pi-prompt"
	DefaultLaneFillClaudeBackend     = "claude-code"
	DefaultLaneFillStaleAfterHours   = 72
	// DefaultLaneFillHostDownAfterMinutes is how long a pool host may stay
	// unreachable before lane fill treats it as down (0 = never).
	DefaultLaneFillHostDownAfterMinutes = 10
)

// AutonomousLaneFillConfig — autonomous.lane_fill sub-config: automatic lane
// filling. Mode is "off", "shadow" or "on"; empty means shadow, which computes
// and records every decision and starts nothing. GPULanes and ClaudeLanes are
// the lane counts (ClaudeLanes 0 means no Claude lane). IdleAlertMinutes is
// how long a lane may sit idle with eligible work queued before one alert is
// raised (0 = off). DecomposeAheadMin is the minimum number of decomposed
// Automata kept ready per lane. PollSeconds is the planner poll interval
// (minimum 5). GPUBackend, GPUModel and ClaudeBackend choose the backends the
// lanes run. The plain numbers are not omitempty, so an explicit zero survives
// a round trip; the pointer fields (CountVerifierWait, StaleAfterHours and the
// eligibility switches) are omitempty, so an absent key means the default
// while an explicit false or 0 survives. GPUMode and ClaudeMode override Mode
// for one lane kind; empty inherits Mode.
type AutonomousLaneFillConfig struct {
	Mode              string `yaml:"mode" json:"mode"`
	GPULanes          int    `yaml:"gpu_lanes" json:"gpu_lanes"`
	ClaudeLanes       int    `yaml:"claude_lanes" json:"claude_lanes"`
	IdleAlertMinutes  int    `yaml:"idle_alert_minutes" json:"idle_alert_minutes"`
	DecomposeAheadMin int    `yaml:"decompose_ahead_min" json:"decompose_ahead_min"`
	PollSeconds       int    `yaml:"poll_seconds" json:"poll_seconds"`
	GPUBackend        string `yaml:"gpu_backend" json:"gpu_backend"`
	GPUModel          string `yaml:"gpu_model" json:"gpu_model"`
	ClaudeBackend     string `yaml:"claude_backend" json:"claude_backend"`
	// CountVerifierWait: when true (the default, unset is nil) a Claude lane
	// whose Automata wait on a verifier slot counts as available; when false
	// the lane stays busy.
	CountVerifierWait *bool `yaml:"count_verifier_wait,omitempty" json:"count_verifier_wait,omitempty"`
	// StaleAfterHours: an Automaton that has sat queued this many hours is
	// treated as stale and is not started automatically. Nil (absent) means
	// 72; an explicit 0 turns the stale rule off.
	StaleAfterHours *int `yaml:"stale_after_hours,omitempty" json:"stale_after_hours,omitempty"`
	// HostDownAfterMinutes: a pool host whose loaded models cannot be read is
	// treated as down once it has been unreachable this many minutes. Nil
	// (absent) means 10; an explicit 0 never treats an unreachable host as
	// down, so every failed read blocks the lane builder.
	HostDownAfterMinutes *int `yaml:"host_down_after_minutes,omitempty" json:"host_down_after_minutes,omitempty"`
	// RequireConfirmedProposal: when true (the default, nil) an Automaton
	// created from a proposal is eligible only once the operator has confirmed
	// that proposal.
	RequireConfirmedProposal *bool `yaml:"require_confirmed_proposal,omitempty" json:"require_confirmed_proposal,omitempty"`
	// CheckMainCarries: when true (the default, nil) lane fill checks whether
	// the project default branch already carries an Automaton's work before it
	// counts as eligible.
	CheckMainCarries *bool `yaml:"check_main_carries,omitempty" json:"check_main_carries,omitempty"`
	// RequireWorktree: when true (the default, nil) an Automaton must have its
	// own worktree to be eligible.
	RequireWorktree *bool `yaml:"require_worktree,omitempty" json:"require_worktree,omitempty"`
	// LaneCountsFromCaps: when true (the default, nil) the lane counts follow
	// the in-flight caps; when false only gpu_lanes and claude_lanes apply.
	LaneCountsFromCaps *bool `yaml:"lane_counts_from_caps,omitempty" json:"lane_counts_from_caps,omitempty"`
	// GPUMode and ClaudeMode set the mode for one lane kind. Empty inherits
	// Mode; otherwise "off", "shadow" or "on".
	GPUMode    string `yaml:"gpu_mode" json:"gpu_mode"`
	ClaudeMode string `yaml:"claude_mode" json:"claude_mode"`
}

// DefaultAutonomousLaneFill returns the shipped lane fill settings: shadow
// mode, 2 GPU lanes, 3 Claude lanes, the pi-prompt and claude-code backends.
func DefaultAutonomousLaneFill() AutonomousLaneFillConfig {
	return AutonomousLaneFillConfig{
		Mode:              LaneFillModeShadow,
		GPULanes:          DefaultLaneFillGPULanes,
		ClaudeLanes:       DefaultLaneFillClaudeLanes,
		IdleAlertMinutes:  DefaultLaneFillIdleAlertMinutes,
		DecomposeAheadMin: DefaultLaneFillDecomposeAheadMin,
		PollSeconds:       DefaultLaneFillPollSeconds,
		GPUBackend:        DefaultLaneFillGPUBackend,
		ClaudeBackend:     DefaultLaneFillClaudeBackend,
	}
}

// AutonomousRequirementsConfig carries the operator overrides for the
// minimum backend version table. The table itself (capability → minimum
// Ollama version, e.g. the classification-verdict capability's
// /v1/systemone floor) ships in code in internal/autonomous; this block
// only lets an operator raise or lower a per-capability minimum under
// autonomous.requirements.min_ollama.<capability>. An absent key means
// "use the built-in table"; an empty value under a set key clears the
// built-in minimum for that capability (disable the floor).
type AutonomousRequirementsConfig struct {
	// MinOllama maps capability name → minimum Ollama version override.
	MinOllama map[string]string `yaml:"min_ollama,omitempty" json:"min_ollama,omitempty"`
}

// DefaultAutonomousRequirements returns the shipped requirements settings:
// no overrides — every capability reads its minimum from the in-code table
// in internal/autonomous.
func DefaultAutonomousRequirements() AutonomousRequirementsConfig {
	return AutonomousRequirementsConfig{MinOllama: map[string]string{}}
}

// MinOllamaOr returns the operator override for one capability. ok is
// false when the capability has no override (read the in-code table
// instead).
func (c AutonomousRequirementsConfig) MinOllamaOr(capability string) (v string, ok bool) {
	if c.MinOllama == nil {
		return "", false
	}
	v, ok = c.MinOllama[capability]
	return v, ok
}

// ValidateLaneFillMode accepts the legal autonomous.lane_fill.mode values,
// "", "off", "shadow" and "on" (case-insensitive, trimmed), and rejects every
// other value.
func ValidateLaneFillMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", LaneFillModeOff, LaneFillModeShadow, LaneFillModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.lane_fill.mode: %q is not a valid value (must be \"off\", \"shadow\" or \"on\")", mode)
	}
}

// ValidateLaneFillKindMode checks a per-kind mode, autonomous.lane_fill.gpu_mode
// or claude_mode, named by key. It accepts the same values as
// ValidateLaneFillMode, including empty (inherit the shared mode).
func ValidateLaneFillKindMode(key, mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", LaneFillModeOff, LaneFillModeShadow, LaneFillModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.lane_fill.%s: %q is not a valid value (must be \"off\", \"shadow\" or \"on\")", key, mode)
	}
}

// EffectiveMode returns the mode in effect, normalized: an empty value means
// shadow.
func (c AutonomousLaneFillConfig) EffectiveMode() string {
	m := strings.ToLower(strings.TrimSpace(c.Mode))
	if m == "" {
		return LaneFillModeShadow
	}
	return m
}

// EffectiveModeFor returns the mode in effect for one lane kind, "gpu" or
// "claude": the per-kind value when set, otherwise EffectiveMode. Any other
// kind reads the shared mode.
func (c AutonomousLaneFillConfig) EffectiveModeFor(kind string) string {
	var per string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "gpu":
		per = c.GPUMode
	case "claude":
		per = c.ClaudeMode
	}
	if m := strings.ToLower(strings.TrimSpace(per)); m != "" {
		return m
	}
	return c.EffectiveMode()
}

// EffectiveGPULanes returns the GPU lane count in effect: a negative value
// means 2 (an explicit 0 is kept).
func (c AutonomousLaneFillConfig) EffectiveGPULanes() int {
	if c.GPULanes < 0 {
		return DefaultLaneFillGPULanes
	}
	return c.GPULanes
}

// EffectiveClaudeLanes returns the Claude lane count in effect: a negative
// value means 3 (an explicit 0 means no Claude lane).
func (c AutonomousLaneFillConfig) EffectiveClaudeLanes() int {
	if c.ClaudeLanes < 0 {
		return DefaultLaneFillClaudeLanes
	}
	return c.ClaudeLanes
}

// EffectivePollSeconds returns the poll interval in effect: 0 (or less) means
// 60 and any value below 5 is raised to 5.
func (c AutonomousLaneFillConfig) EffectivePollSeconds() int {
	switch {
	case c.PollSeconds <= 0:
		return DefaultLaneFillPollSeconds
	case c.PollSeconds < MinLaneFillPollSeconds:
		return MinLaneFillPollSeconds
	}
	return c.PollSeconds
}

// EffectiveDecomposeAheadMin returns the decompose-ahead minimum in effect: a
// negative value means 2 (an explicit 0 is kept).
func (c AutonomousLaneFillConfig) EffectiveDecomposeAheadMin() int {
	if c.DecomposeAheadMin < 0 {
		return DefaultLaneFillDecomposeAheadMin
	}
	return c.DecomposeAheadMin
}

// EffectiveIdleAlertMinutes returns the idle alert threshold in effect: a
// negative value means 30 (an explicit 0 turns the alert off).
func (c AutonomousLaneFillConfig) EffectiveIdleAlertMinutes() int {
	if c.IdleAlertMinutes < 0 {
		return DefaultLaneFillIdleAlertMinutes
	}
	return c.IdleAlertMinutes
}

// EffectiveGPUBackend returns the GPU lane backend in effect: empty means
// pi-prompt.
func (c AutonomousLaneFillConfig) EffectiveGPUBackend() string {
	if strings.TrimSpace(c.GPUBackend) == "" {
		return DefaultLaneFillGPUBackend
	}
	return c.GPUBackend
}

// EffectiveClaudeBackend returns the Claude lane backend in effect: empty
// means claude-code.
func (c AutonomousLaneFillConfig) EffectiveClaudeBackend() string {
	if strings.TrimSpace(c.ClaudeBackend) == "" {
		return DefaultLaneFillClaudeBackend
	}
	return c.ClaudeBackend
}

// EffectiveCountVerifierWait returns the verifier-wait rule in effect: an
// unset (nil) value means true — a Claude lane whose Automata wait on a
// verifier slot counts as available; an explicit false keeps it busy.
func (c AutonomousLaneFillConfig) EffectiveCountVerifierWait() bool {
	if c.CountVerifierWait == nil {
		return true
	}
	return *c.CountVerifierWait
}

// EffectiveStaleAfterHours returns the stale threshold in effect: nil means
// 72 and an explicit 0 turns the stale rule off.
func (c AutonomousLaneFillConfig) EffectiveStaleAfterHours() int {
	if c.StaleAfterHours == nil || *c.StaleAfterHours < 0 {
		return DefaultLaneFillStaleAfterHours
	}
	return *c.StaleAfterHours
}

// EffectiveHostDownAfterMinutes returns the host-down threshold in effect: nil
// means 10 and an explicit 0 means an unreachable host is never treated as down.
func (c AutonomousLaneFillConfig) EffectiveHostDownAfterMinutes() int {
	if c.HostDownAfterMinutes == nil || *c.HostDownAfterMinutes < 0 {
		return DefaultLaneFillHostDownAfterMinutes
	}
	return *c.HostDownAfterMinutes
}

// laneFillBoolDefaultTrue reads a tri-state boolean: nil means true.
func laneFillBoolDefaultTrue(b *bool) bool { return b == nil || *b }

// EffectiveRequireConfirmedProposal returns the confirmed-proposal rule in
// effect: unset means true.
func (c AutonomousLaneFillConfig) EffectiveRequireConfirmedProposal() bool {
	return laneFillBoolDefaultTrue(c.RequireConfirmedProposal)
}

// EffectiveCheckMainCarries returns the main-carries check in effect: unset
// means true.
func (c AutonomousLaneFillConfig) EffectiveCheckMainCarries() bool {
	return laneFillBoolDefaultTrue(c.CheckMainCarries)
}

// EffectiveRequireWorktree returns the worktree rule in effect: unset means
// true.
func (c AutonomousLaneFillConfig) EffectiveRequireWorktree() bool {
	return laneFillBoolDefaultTrue(c.RequireWorktree)
}

// EffectiveLaneCountsFromCaps returns the lane-counts-from-caps rule in
// effect: unset means true.
func (c AutonomousLaneFillConfig) EffectiveLaneCountsFromCaps() bool {
	return laneFillBoolDefaultTrue(c.LaneCountsFromCaps)
}

// validate rejects negative numbers and an unknown mode.
func (c AutonomousLaneFillConfig) validate() error {
	if err := ValidateLaneFillMode(c.Mode); err != nil {
		return err
	}
	if err := ValidateLaneFillKindMode("gpu_mode", c.GPUMode); err != nil {
		return err
	}
	if err := ValidateLaneFillKindMode("claude_mode", c.ClaudeMode); err != nil {
		return err
	}
	if c.StaleAfterHours != nil && *c.StaleAfterHours < 0 {
		return fmt.Errorf("autonomous.lane_fill.stale_after_hours: %d must be >= 0", *c.StaleAfterHours)
	}
	if c.HostDownAfterMinutes != nil && *c.HostDownAfterMinutes < 0 {
		return fmt.Errorf("autonomous.lane_fill.host_down_after_minutes: %d must be >= 0", *c.HostDownAfterMinutes)
	}
	for k, v := range map[string]int{
		"gpu_lanes":           c.GPULanes,
		"claude_lanes":        c.ClaudeLanes,
		"idle_alert_minutes":  c.IdleAlertMinutes,
		"decompose_ahead_min": c.DecomposeAheadMin,
		"poll_seconds":        c.PollSeconds,
	} {
		if v < 0 {
			return fmt.Errorf("autonomous.lane_fill.%s: %d must be >= 0", k, v)
		}
	}
	return nil
}

// autonomous.progress defaults. A zero numeric value means "use the default".
const (
	DefaultProgressLanes                 = 5
	DefaultProgressInactiveGapHours      = 3.0
	DefaultProgressBacklogPath           = "docs/plans/README.md"
	DefaultProgressSnapshotRetentionDays = 90
	progressSinceLayout                  = "2006-01-02"
)

// AutonomousProgressConfig — autonomous.progress sub-config: every parameter
// of the progress metrics engine. Lanes is the number of parallel lanes used
// for the utilization figures (0 = 5). InactiveGapHours is the length of a gap
// between two activity events that counts as an inactive window (0 = 3).
// BacklogPath is the machine-readable backlog file; a relative path resolves
// against autonomous.worktree_base_repo when that is set, else the daemon's
// working directory. Since is the first day counted (YYYY-MM-DD; empty = the
// first record's creation date). SnapshotRetentionDays is how long saved
// progress snapshots are kept (0 = 90). None are omitempty, so an explicit
// zero survives a round trip.
type AutonomousProgressConfig struct {
	Lanes                 int     `yaml:"lanes" json:"lanes"`
	InactiveGapHours      float64 `yaml:"inactive_gap_hours" json:"inactive_gap_hours"`
	BacklogPath           string  `yaml:"backlog_path" json:"backlog_path"`
	Since                 string  `yaml:"since" json:"since"`
	SnapshotRetentionDays int     `yaml:"snapshot_retention_days" json:"snapshot_retention_days"`
}

// EffectiveLanes returns the lane count in effect: 0 (or less) means 5.
func (c AutonomousProgressConfig) EffectiveLanes() int {
	if c.Lanes <= 0 {
		return DefaultProgressLanes
	}
	return c.Lanes
}

// EffectiveInactiveGapHours returns the inactive-window gap in effect: 0 (or
// less) means 3 hours.
func (c AutonomousProgressConfig) EffectiveInactiveGapHours() float64 {
	if c.InactiveGapHours <= 0 {
		return DefaultProgressInactiveGapHours
	}
	return c.InactiveGapHours
}

// EffectiveSnapshotRetentionDays returns the snapshot retention in effect: 0
// (or less) means 90 days.
func (c AutonomousProgressConfig) EffectiveSnapshotRetentionDays() int {
	if c.SnapshotRetentionDays <= 0 {
		return DefaultProgressSnapshotRetentionDays
	}
	return c.SnapshotRetentionDays
}

// EffectiveBacklogPath returns the backlog path in effect: an empty value
// means docs/plans/README.md.
func (c AutonomousProgressConfig) EffectiveBacklogPath() string {
	if strings.TrimSpace(c.BacklogPath) == "" {
		return DefaultProgressBacklogPath
	}
	return c.BacklogPath
}

// ResolveBacklogPath returns the backlog file location: an absolute path as
// is, a relative one joined onto worktreeBaseRepo (autonomous.worktree_base_repo)
// when that is set, else made absolute against the daemon's working directory.
func (c AutonomousProgressConfig) ResolveBacklogPath(worktreeBaseRepo string) string {
	p := c.EffectiveBacklogPath()
	if filepath.IsAbs(p) {
		return p
	}
	if base := strings.TrimSpace(worktreeBaseRepo); base != "" {
		return filepath.Join(base, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// SinceTime parses Since. An empty value yields the zero time and ok=false,
// meaning the first record's creation date applies.
func (c AutonomousProgressConfig) SinceTime() (t time.Time, ok bool) {
	if strings.TrimSpace(c.Since) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(progressSinceLayout, strings.TrimSpace(c.Since))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ValidateProgressSince accepts the empty string (first record's creation
// date) and a YYYY-MM-DD date, and rejects every other value.
func ValidateProgressSince(since string) error {
	if strings.TrimSpace(since) == "" {
		return nil
	}
	if _, err := time.Parse(progressSinceLayout, strings.TrimSpace(since)); err != nil {
		return fmt.Errorf("autonomous.progress.since: %q is not a valid date (must be YYYY-MM-DD or empty)", since)
	}
	return nil
}

// ValidateProgressBacklogPath rejects a path containing a NUL byte. An empty
// value is accepted and means docs/plans/README.md.
func ValidateProgressBacklogPath(path string) error {
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("autonomous.progress.backlog_path: must not contain a NUL byte")
	}
	return nil
}

// validate checks every autonomous.progress value on the load paths.
func (c AutonomousProgressConfig) validate() error {
	if c.Lanes < 0 {
		return fmt.Errorf("autonomous.progress.lanes: %d must be >= 0 (0 = %d)", c.Lanes, DefaultProgressLanes)
	}
	if c.InactiveGapHours < 0 || math.IsNaN(c.InactiveGapHours) || math.IsInf(c.InactiveGapHours, 0) {
		return fmt.Errorf("autonomous.progress.inactive_gap_hours: %v must be a finite number >= 0 (0 = %v)", c.InactiveGapHours, DefaultProgressInactiveGapHours)
	}
	if c.SnapshotRetentionDays < 0 {
		return fmt.Errorf("autonomous.progress.snapshot_retention_days: %d must be >= 0 (0 = %d)", c.SnapshotRetentionDays, DefaultProgressSnapshotRetentionDays)
	}
	if err := ValidateProgressBacklogPath(c.BacklogPath); err != nil {
		return err
	}
	return ValidateProgressSince(c.Since)
}

// intPtr is the tiny pointer helper for pointer-to-int config fields.
func intPtr(v int) *int { return &v }

// Repo CI watch — the autonomous.automation.repo_ci_watch knobs. The
// constants and validation here mirror internal/autonomous
// (RepoCIWatchConfig) so the YAML/config-load surface and the REST write
// paths refuse the same bad values; the config package must not import
// that one (the daemon bridges it).
const (
	// RepoCIWatchModeOff/Shadow/On — the legal repo CI watch modes.
	RepoCIWatchModeOff    = "off"
	RepoCIWatchModeShadow = "shadow"
	RepoCIWatchModeOn     = "on"
	// RepoCIWatchDeleteSupersededReport/Delete — the legal
	// delete_superseded values.
	RepoCIWatchDeleteSupersededReport = "report"
	RepoCIWatchDeleteSupersededDelete = "delete"

	// RepoCIWatchDefaultPollSeconds / RepoCIWatchMinPollSeconds — the
	// poll interval default (unset) and minimum.
	RepoCIWatchDefaultPollSeconds = 300
	RepoCIWatchMinPollSeconds     = 60
	// RepoCIWatchDefaultTagCount — how many newest release tags are
	// watched by default (unset). An explicit 0 means none.
	RepoCIWatchDefaultTagCount = 5
	// RepoCIWatchDefaultHungAfterMinutes /
	// RepoCIWatchMinHungAfterMinutes — the hung-run threshold default
	// (unset) and minimum.
	RepoCIWatchDefaultHungAfterMinutes = 60
	RepoCIWatchMinHungAfterMinutes     = 1
)

// DefaultRepoCIWatchSecurityWorkflows — the default repo CI watch
// security workflow names (applies when security_workflows is unset).
var DefaultRepoCIWatchSecurityWorkflows = []string{
	"CodeQL",
	"dependency-review",
	"owasp-zap",
	"secret-scan",
	"security-scan",
}

// ValidateRepoCIWatchMode accepts the three legal repo CI watch modes
// (off, shadow, on; case-insensitive, trimmed; empty = unset = off) and
// rejects every other value.
func ValidateRepoCIWatchMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", RepoCIWatchModeOff, RepoCIWatchModeShadow, RepoCIWatchModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.repo_ci_watch.mode: %q is not a valid value (must be %q, %q or %q)",
			mode, RepoCIWatchModeOff, RepoCIWatchModeShadow, RepoCIWatchModeOn)
	}
}

// ValidateRepoCIWatchDeleteSuperseded accepts the two legal
// delete_superseded values (report, delete; case-insensitive, trimmed;
// empty = unset = report) and rejects every other value.
func ValidateRepoCIWatchDeleteSuperseded(v string) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", RepoCIWatchDeleteSupersededReport, RepoCIWatchDeleteSupersededDelete:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.repo_ci_watch.delete_superseded: %q is not a valid value (must be %q or %q)",
			v, RepoCIWatchDeleteSupersededReport, RepoCIWatchDeleteSupersededDelete)
	}
}

// RepoCIWatchConfig — mirror of autonomous.RepoCIWatchConfig
// (autonomous.automation.repo_ci_watch.*). AlertMinutes and TagCount are
// pointers on purpose: unset (nil) reads the 30-minute and 5-tag
// defaults, while an explicit 0 turns the alert off / watches no tags
// and survives a merge round trip. The Effective* accessors apply every
// default in one place, so no call site hard-codes one.
type RepoCIWatchConfig struct {
	Mode              string   `yaml:"mode" json:"mode"`
	Repos             []string `yaml:"repos" json:"repos"`
	PollSeconds       int      `yaml:"poll_seconds" json:"poll_seconds"`
	AlertMinutes      *int     `yaml:"alert_minutes,omitempty" json:"alert_minutes,omitempty"`
	TagCount          *int     `yaml:"tag_count,omitempty" json:"tag_count,omitempty"`
	HungAfterMinutes  int      `yaml:"hung_after_minutes" json:"hung_after_minutes"`
	SecurityWorkflows []string `yaml:"security_workflows" json:"security_workflows"`
	DeleteSuperseded  string   `yaml:"delete_superseded" json:"delete_superseded"`
}

// EffectiveMode returns the mode in effect; an empty value reads the
// off default. Bad values are refused on the write paths.
func (c RepoCIWatchConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return RepoCIWatchModeOff
	}
	return mode
}

// EffectiveRepos returns the watched repository paths: the configured
// list, or an empty slice when unset (nothing is watched). Repositories
// are never inferred from the working directory.
func (c RepoCIWatchConfig) EffectiveRepos() []string {
	if c.Repos == nil {
		return []string{}
	}
	return c.Repos
}

// EffectivePollSeconds returns the poll interval in effect; an unset (0)
// value reads the 300-second default. A value below 60 is refused on the
// write paths. (An unset read of 0 is normalized to the default.
// Sub-60 values are refused at the write path.)
//
// NOTE: an explicit 0 also reads the default — the minimum (60) makes
// 0 not a meaningful stored value.
func (c RepoCIWatchConfig) EffectivePollSeconds() int {
	if c.PollSeconds == 0 {
		return RepoCIWatchDefaultPollSeconds
	}
	return c.PollSeconds
}

// EffectiveAlertMinutes returns the alert threshold in effect: unset
// (nil) reads DefaultAutomationTriageAlertMinutes (30), an explicit 0
// turns the alert off. Negative values are refused on the write paths.
func (c RepoCIWatchConfig) EffectiveAlertMinutes() int {
	if c.AlertMinutes == nil {
		return DefaultAutomationTriageAlertMinutes
	}
	return *c.AlertMinutes
}

// EffectiveTagCount returns how many newest release tags to watch: unset
// (nil) reads the 5-tag default, an explicit 0 means none. Negative
// values are refused on the write paths.
func (c RepoCIWatchConfig) EffectiveTagCount() int {
	if c.TagCount == nil {
		return RepoCIWatchDefaultTagCount
	}
	return *c.TagCount
}

// EffectiveHungAfterMinutes returns the hung-run threshold in effect;
// an unset (0) value reads the 60-minute default. A value below 1 is
// refused on the write paths.
func (c RepoCIWatchConfig) EffectiveHungAfterMinutes() int {
	if c.HungAfterMinutes == 0 {
		return RepoCIWatchDefaultHungAfterMinutes
	}
	return c.HungAfterMinutes
}

// EffectiveSecurityWorkflows returns the security workflow names to
// watch: the configured list (an explicitly empty list means none), or
// DefaultRepoCIWatchSecurityWorkflows when unset (nil).
func (c RepoCIWatchConfig) EffectiveSecurityWorkflows() []string {
	if c.SecurityWorkflows == nil {
		return DefaultRepoCIWatchSecurityWorkflows
	}
	return c.SecurityWorkflows
}

// EffectiveDeleteSuperseded returns the superseded-run policy in effect;
// an empty value reads the report default. Bad values are refused on the
// write paths.
func (c RepoCIWatchConfig) EffectiveDeleteSuperseded() string {
	v := strings.ToLower(strings.TrimSpace(c.DeleteSuperseded))
	if v == "" {
		return RepoCIWatchDeleteSupersededReport
	}
	return v
}

// validateRepoCIWatch checks one RepoCIWatchConfig the same way the REST
// write paths do, so a config file load refuses the same bad values.
func validateRepoCIWatch(c RepoCIWatchConfig) error {
	if err := ValidateRepoCIWatchMode(c.Mode); err != nil {
		return err
	}
	for _, r := range c.Repos {
		path := strings.TrimSpace(r)
		if len(path) <= 1 || !strings.HasPrefix(path, "/") {
			return fmt.Errorf("autonomous.automation.repo_ci_watch.repos: %q must be an absolute path (repositories are never inferred from the working directory)", r)
		}
	}
	if c.PollSeconds > 0 && c.PollSeconds < RepoCIWatchMinPollSeconds {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.poll_seconds: %d must be a whole number >= %d (unset reads the %d-second default)",
			c.PollSeconds, RepoCIWatchMinPollSeconds, RepoCIWatchDefaultPollSeconds)
	}
	if c.AlertMinutes != nil && *c.AlertMinutes < 0 {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.alert_minutes: %d must be a whole number >= 0 (0 = alert off, unset reads the %d-minute default)",
			*c.AlertMinutes, DefaultAutomationTriageAlertMinutes)
	}
	if c.TagCount != nil && *c.TagCount < 0 {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.tag_count: %d must be a whole number >= 0 (0 = none, unset reads the %d default)",
			*c.TagCount, RepoCIWatchDefaultTagCount)
	}
	if c.HungAfterMinutes > 0 && c.HungAfterMinutes < RepoCIWatchMinHungAfterMinutes {
		return fmt.Errorf("autonomous.automation.repo_ci_watch.hung_after_minutes: %d must be a whole number >= %d (unset reads the %d-minute default)",
			c.HungAfterMinutes, RepoCIWatchMinHungAfterMinutes, RepoCIWatchDefaultHungAfterMinutes)
	}
	if err := ValidateRepoCIWatchDeleteSuperseded(c.DeleteSuperseded); err != nil {
		return err
	}
	for _, w := range c.SecurityWorkflows {
		if strings.TrimSpace(w) == "" {
			return errors.New("autonomous.automation.repo_ci_watch.security_workflows: workflow names must be non-empty")
		}
	}
	return nil
}

// Security failure — the autonomous.automation.security_failure knob
// (one key: mode). The constants and validation here mirror
// internal/autonomous (SecurityFailureConfig) so the YAML/config-load
// surface and the REST write paths refuse the same bad value; the config
// package must not import that one (the daemon bridges it)
const (
	// SecurityFailureModeOff/Shadow/On — the legal security failure modes.
	// SecurityFailureModeShadow is the first-release default: an unset or
	// empty mode reads shadow.
	SecurityFailureModeOff    = "off"
	SecurityFailureModeShadow = "shadow"
	SecurityFailureModeOn     = "on"
)

// SecurityFailureConfig — mirror of autonomous.SecurityFailureConfig
// (autonomous.automation.security_failure.mode). The one default (an
// unset or empty mode reads shadow, the first-release default) is applied
// only in EffectiveMode, so no call site hard-codes one. There is no
// other key: the security workflow set is read from
// autonomous.automation.repo_ci_watch.security_workflows, and the alert
// time is autonomous.automation.triage_alert_minutes.
type SecurityFailureConfig struct {
	Mode string `yaml:"mode" json:"mode"`
}

// EffectiveMode returns the security failure mode in effect: an empty
// (unset) value reads SecurityFailureModeShadow (the first-release
// default — shadow first, no behaviour change). Any value other than off,
// shadow or on is refused on the write paths, so this never has to guess
// for it.
func (c SecurityFailureConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return SecurityFailureModeShadow
	}
	return mode
}

// ValidateSecurityFailureMode accepts the three legal security failure
// modes (off, shadow, on; case-insensitive, trimmed; empty = unset =
// shadow) and rejects every other value.
func ValidateSecurityFailureMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SecurityFailureModeOff, SecurityFailureModeShadow, SecurityFailureModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.security_failure.mode: %q is not a valid value (must be %q, %q or %q)",
			mode, SecurityFailureModeOff, SecurityFailureModeShadow, SecurityFailureModeOn)
	}
}

// validateSecurityFailure checks one SecurityFailureConfig the same way
// the REST write paths do, so a bad mode is refused consistently on every
// write path (config load and PUT /api/config) rather than normalized
// silently.
func validateSecurityFailure(c SecurityFailureConfig) error {
	return ValidateSecurityFailureMode(c.Mode)
}

// Supervisor watch — the autonomous.automation.supervisor_watch knobs
// (mode, alert_minutes, silent_minutes, script_output_path,
// tmux_restart_baseline, main_checkout). Each knob mirrors
// internal/autonomous (SupervisorWatchConfig) so the YAML/config-load
// path keeps its own copy of the same rules — internal/config must not
// import internal/autonomous.
const (
	// SupervisorWatchModeOff/Shadow/On — the legal supervisor watch
	// modes. SupervisorWatchModeShadow is the release default: an
	// unset or empty mode reads shadow — reports only, never acts — so
	// the watch ships in shadow mode first.
	SupervisorWatchModeOff    = "off"
	SupervisorWatchModeShadow = "shadow"
	SupervisorWatchModeOn     = "on"

	// DefaultSupervisorWatchSilentMinutes — the supervisor watch
	// silent_minutes default: an Automaton session may go silent this
	// many minutes before the watch records a silence finding.
	DefaultSupervisorWatchSilentMinutes = 60

	// DefaultSupervisorWatchPollSeconds — the supervisor watch
	// poll_seconds default: the configured coordinator poll interval
	// (whole seconds) the script-record staleness window is measured
	// against (a record is stale at 3 times the effective interval).
	DefaultSupervisorWatchPollSeconds = 60

	// SupervisorWatchMinPollSeconds — the minimum configured poll
	// interval (whole seconds); an explicitly set value below it is
	// refused on every write path.
	SupervisorWatchMinPollSeconds = 60
)

// SupervisorWatchConfig — the supervisor watch knobs
// (autonomous.automation.supervisor_watch.*).
//
// Mode: off, shadow or on. An empty (unset) mode reads
// SupervisorWatchModeShadow (the release default — shadow first, reports
// only, never acts). Any other value is refused on every write path.
//
// AlertMinutes is a pointer on purpose: an unset (nil) value reads the
// shared DefaultAutomationTriageAlertMinutes (30) default — there is no
// second constant for 30 — while an explicit 0 (a pointer to zero) turns
// the alert off and must survive a round trip. A negative value is
// rejected with a 400 on every write path and with an error on config
// load.
//
// SilentMinutes is a pointer on purpose: an unset (nil) value reads
// DefaultSupervisorWatchSilentMinutes (60) while an explicit 0 must
// survive a round trip. A negative value is rejected the same way.
//
// PollSeconds is the configured coordinator poll interval (whole
// seconds) the script-record staleness window is measured against
// (stale at 3 times the effective interval). An unset value (0) reads
// DefaultSupervisorWatchPollSeconds (60) via EffectivePollSeconds(); an
// explicitly set value below SupervisorWatchMinPollSeconds (60), like a
// negative one, is refused on every write path.
//
// ScriptOutputPath is the optional path of the oracle script's record
// file; empty (the zero value) means the script has not published one.
// TmuxRestartBaseline is the optional baseline the tmux_restart finding
// compares against; empty means none. MainCheckout is the optional path
// of the main checkout; empty disables the dirty main-checkout finding
// altogether.
type SupervisorWatchConfig struct {
	Mode                string `yaml:"mode" json:"mode"`
	AlertMinutes        *int   `yaml:"alert_minutes,omitempty" json:"alert_minutes,omitempty"`
	SilentMinutes       *int   `yaml:"silent_minutes,omitempty" json:"silent_minutes,omitempty"`
	PollSeconds         int    `yaml:"poll_seconds,omitempty" json:"poll_seconds,omitempty"`
	ScriptOutputPath    string `yaml:"script_output_path,omitempty" json:"script_output_path,omitempty"`
	TmuxRestartBaseline string `yaml:"tmux_restart_baseline,omitempty" json:"tmux_restart_baseline,omitempty"`
	MainCheckout        string `yaml:"main_checkout,omitempty" json:"main_checkout,omitempty"`

	// WorkingModel is the model the watch expects to be resident on every
	// registered compute node. Empty (unset) turns the model_not_resident
	// check off; there is no default working model.
	WorkingModel string `yaml:"working_model,omitempty" json:"working_model,omitempty"`
}

// EffectiveMode returns the supervisor watch mode in effect: an empty (unset) value reads
// SupervisorWatchModeShadow (the release default). Any value other than
// off, shadow or on is refused on the write paths, so this never has to
// guess for it. This accessor is the only place the shadow default is
// applied; a caller never writes the literal.
func (c SupervisorWatchConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return SupervisorWatchModeShadow
	}
	return mode
}

// EffectiveAlertMinutes returns the supervisor watch alert threshold in
// effect: an unset (nil) value reads the shared
// DefaultAutomationTriageAlertMinutes (30) default, and an explicit 0
// keeps the alert off. Negative values are rejected on the write paths,
// so this never has to guess for them. This accessor is the only place
// the 30-minute default is applied.
func (c SupervisorWatchConfig) EffectiveAlertMinutes() int {
	if c.AlertMinutes == nil {
		return DefaultAutomationTriageAlertMinutes
	}
	return *c.AlertMinutes
}

// EffectiveSilentMinutes returns the supervisor watch silence window in
// effect: an unset (nil) value reads
// DefaultSupervisorWatchSilentMinutes (60), and an explicit 0 keeps the
// window at zero. Negative values are rejected on the write paths, so
// this never has to guess for them. This accessor is the only place the
// 60-minute default is applied.
func (c SupervisorWatchConfig) EffectiveSilentMinutes() int {
	if c.SilentMinutes == nil {
		return DefaultSupervisorWatchSilentMinutes
	}
	return *c.SilentMinutes
}

// EffectivePollSeconds returns the configured coordinator poll interval
// in effect (whole seconds the script-record staleness window is
// measured against): an unset value (0) reads the
// DefaultSupervisorWatchPollSeconds (60) default, while an explicit
// value below SupervisorWatchMinPollSeconds (60), like a negative one,
// is refused on the write paths, so this never has to guess for it. This
// accessor is the only place the 60-second default is applied; a caller
// never writes the literal.
func (c SupervisorWatchConfig) EffectivePollSeconds() int {
	if c.PollSeconds == 0 {
		return DefaultSupervisorWatchPollSeconds
	}
	return c.PollSeconds
}

// ValidateSupervisorWatchMode accepts the three legal supervisor watch
// modes (off, shadow, on; case-insensitive, trimmed; empty = unset =
// shadow) and rejects every other value with a clear error naming the
// accepted set.
func ValidateSupervisorWatchMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SupervisorWatchModeOff, SupervisorWatchModeShadow, SupervisorWatchModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.supervisor_watch.mode: %q is not a valid value (must be %q, %q or %q)",
			mode, SupervisorWatchModeOff, SupervisorWatchModeShadow, SupervisorWatchModeOn)
	}
}

// validateSupervisorWatchMinutes rejects a negative minute threshold with
// a clear error naming the key. Zero is legal (the operator said zero
// explicitly); an unset (nil) threshold is the accessors' business, not
// the validator's.
func validateSupervisorWatchMinutes(field string, minutes int) error {
	if minutes < 0 {
		return fmt.Errorf("autonomous.automation.supervisor_watch.%s: %d must not be negative (0 is legal; an unset key reads its default)",
			field, minutes)
	}
	return nil
}

// validateSupervisorWatch checks one SupervisorWatchConfig the same way
// the REST write paths do, so a bad mode or a negative threshold is
// refused consistently on every write path (config load and
// PUT /api/config) rather than normalized silently.
func validateSupervisorWatch(c SupervisorWatchConfig) error {
	if err := ValidateSupervisorWatchMode(c.Mode); err != nil {
		return err
	}
	if c.AlertMinutes != nil {
		if err := validateSupervisorWatchMinutes("alert_minutes", *c.AlertMinutes); err != nil {
			return err
		}
	}
	if c.SilentMinutes != nil {
		if err := validateSupervisorWatchMinutes("silent_minutes", *c.SilentMinutes); err != nil {
			return err
		}
	}
	if c.PollSeconds < 0 {
		return fmt.Errorf("autonomous.automation.supervisor_watch.poll_seconds: %d must not be negative (0 = unset, which reads the %d-second default)",
			c.PollSeconds, DefaultSupervisorWatchPollSeconds)
	}
	if c.PollSeconds > 0 && c.PollSeconds < SupervisorWatchMinPollSeconds {
		return fmt.Errorf("autonomous.automation.supervisor_watch.poll_seconds: %d must be a whole number >= %d (unset reads the %d-second default)",
			c.PollSeconds, SupervisorWatchMinPollSeconds, DefaultSupervisorWatchPollSeconds)
	}
	return nil
}

// float64Ptr returns a pointer to the float64 value v.
func float64Ptr(v float64) *float64 { return &v }

// autonomous.automation.agent_insert_policy values.
const (
	AgentInsertPolicyAllow           = "allow"
	AgentInsertPolicyRequireOperator = "require_operator"
)

// ValidateAgentInsertPolicy accepts the empty string (the default, allow),
// "allow" and "require_operator", and rejects every other value.
func ValidateAgentInsertPolicy(policy string) error {
	switch policy {
	case "", AgentInsertPolicyAllow, AgentInsertPolicyRequireOperator:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.agent_insert_policy: %q is not a valid value (must be %q or %q)",
			policy, AgentInsertPolicyAllow, AgentInsertPolicyRequireOperator)
	}
}

// EffectiveAgentInsertPolicy returns the policy in effect: an empty value
// means allow.
func (c AutonomousAutomationConfig) EffectiveAgentInsertPolicy() string {
	if c.AgentInsertPolicy == "" {
		return AgentInsertPolicyAllow
	}
	return c.AgentInsertPolicy
}

// Scope-lint modes for AutonomousConfig.ScopeLint. See the field's doc
// comment for what each mode does.
const (
	ScopeLintOff     = "off"
	ScopeLintWarn    = "warn"
	ScopeLintEnforce = "enforce"
)

// ScopeLintMode returns the normalised scope-lint mode: the value is
// trimmed and lower-cased, and anything other than off or warn —
// including an empty or garbage value — resolves to enforce, so an
// existing configuration keeps today's blocking behaviour.
func (a AutonomousConfig) ScopeLintMode() string {
	switch strings.ToLower(strings.TrimSpace(a.ScopeLint)) {
	case ScopeLintOff:
		return ScopeLintOff
	case ScopeLintWarn:
		return ScopeLintWarn
	default:
		return ScopeLintEnforce
	}
}

// ValidateScopeLint reports an error unless s is exactly one of the
// scope-lint modes. Write surfaces (REST, MCP, comm, CLI) call it so a
// typo is refused instead of being stored and quietly resolved to enforce
// by ScopeLintMode. Unlike ScopeLintMode it does not trim or fold case.
func ValidateScopeLint(s string) error {
	switch s {
	case ScopeLintOff, ScopeLintWarn, ScopeLintEnforce:
		return nil
	}
	return fmt.Errorf("scope_lint: unknown mode %q (use off, warn or enforce)", s)
}

// EffectiveTriageAlertMinutes returns the triage alert threshold in effect:
// an unset value (nil pointer) reads the default of
// DefaultAutomationTriageAlertMinutes (30), while an explicit 0 turns the
// triage alert off. Negative values are rejected on the write paths, so this
// never has to guess for them.
func (c AutonomousAutomationConfig) EffectiveTriageAlertMinutes() int {
	if c.TriageAlertMinutes == nil {
		return DefaultAutomationTriageAlertMinutes
	}
	return *c.TriageAlertMinutes
}

// ValidateMergePendingMode accepts the legal autonomous.automation.
// merge_pending_mode values: off, shadow and on. The empty string is accepted
// too, as "unset", and reads as off. Any other value is rejected.
func ValidateMergePendingMode(mode string) error {
	switch mode {
	case "", MergePendingModeOff, MergePendingModeShadow, MergePendingModeOn:
		return nil
	}
	return fmt.Errorf("autonomous.automation.merge_pending_mode: unknown mode %q (use %s, %s or %s)",
		mode, MergePendingModeOff, MergePendingModeShadow, MergePendingModeOn)
}

// EffectiveMergePendingMode returns the merge-pending mode in effect: an empty
// value reads as MergePendingModeOff.
func (c AutonomousAutomationConfig) EffectiveMergePendingMode() string {
	if c.MergePendingMode == "" {
		return MergePendingModeOff
	}
	return c.MergePendingMode
}

// ValidateMergedMemberPolicy accepts the legal autonomous.automation.
// merged_member_policy values: completed, manual and block. The empty string
// is accepted too, as "unset", and reads as completed. Any other value is
// rejected.
func ValidateMergedMemberPolicy(policy string) error {
	switch policy {
	case "", MergedMemberPolicyCompleted, MergedMemberPolicyManual, MergedMemberPolicyBlock:
		return nil
	}
	return fmt.Errorf("autonomous.automation.merged_member_policy: unknown policy %q (use %s, %s or %s)",
		policy, MergedMemberPolicyCompleted, MergedMemberPolicyManual, MergedMemberPolicyBlock)
}

// EffectiveMergedMemberPolicy returns the merged-member policy in effect: an
// empty value reads as MergedMemberPolicyCompleted.
func (c AutonomousAutomationConfig) EffectiveMergedMemberPolicy() string {
	if c.MergedMemberPolicy == "" {
		return MergedMemberPolicyCompleted
	}
	return c.MergedMemberPolicy
}

// EffectiveMergePendingAlertMinutes returns the merge-pending alert threshold
// in effect: an unset value (nil pointer) reads
// DefaultAutomationMergePendingAlertMinutes (30), while an explicit 0 turns
// the merge-pending alert off. Negative values are rejected on the write
// paths, so this never has to guess for them.
func (c AutonomousAutomationConfig) EffectiveMergePendingAlertMinutes() int {
	if c.MergePendingAlertMinutes == nil {
		return DefaultAutomationMergePendingAlertMinutes
	}
	return *c.MergePendingAlertMinutes
}

// AutonomousNotifyConfig —autonomous.notify sub-config. DefaultEvents
// is the global default set of lifecycle event kinds (see
// internal/autonomous/lifecycle_event.go: KindPRDUpdated
// "prd.updated", PRDStatusKind(prd.*) — e.g. "prd.completed",
// "prd.failed" — KindTaskSessionStarted "task.session_started",
// TaskStatusKind(task.*) — e.g. "task.completed", "task.failed").
//
// Resolution precedence (internal/autonomous.ResolveNotifyEvents):
// the Automaton's own notify_events list, then its assigned guardrail
// profile's notify_events list, then this global list. At the global
// tier the value distinguishes unset (nil — DefaultConfig() supplies
// the five-kind baseline: prd.needs_review, prd.blocked, prd.failed,
// prd.completed, prd.cancelled) from an explicit empty list
// (default_events: [] — the operator opted out, and the global tier
// resolves to no kinds). An operator's non-empty list replaces the
// baseline. The daemon bridge (cmd/datawatch) copies this slice into
// the autonomous Manager's NotifyDefaultEvents at boot and the live
// PUT /api/config path keeps it in sync without a restart.
// DefaultEvents has NO omitempty on either tag (the B124
// no-omitempty pattern already used for the autonomous.* fields in
// internal/server/api.go, see the comment block near api.go:5802):
// an operator's explicit empty list must survive a YAML/JSON
// round-trip and a PUT /api/config write instead of being elided on
// marshal and leaving the previous value untouched on unmarshal.
type AutonomousNotifyConfig struct {
	DefaultEvents []string `yaml:"default_events" json:"default_events"`
	// PendingEventsMax bounds each mcp_queue subscription sink's
	// in-memory pending-events queue; the oldest events are evicted
	// first. Values below 1 fall back to 500. No omitempty, same
	// reasoning as DefaultEvents.
	PendingEventsMax int `yaml:"pending_events_max" json:"pending_events_max"`
	// Webhook configures the outbound webhook sink. It ships disabled.
	// No omitempty, same reasoning as DefaultEvents.
	Webhook AutonomousNotifyWebhookConfig `yaml:"webhook" json:"webhook"`
	// Forwarding configures peer forwarding of lifecycle events. It ships
	// disabled. No omitempty, same reasoning as DefaultEvents.
	Forwarding AutonomousNotifyForwardingConfig `yaml:"forwarding" json:"forwarding"`
}

// AutonomousQuestionsConfig is autonomous.questions, the switch that
// gates the /api/questions endpoints (the Automaton operator-question
// REST API). It ships off (Enabled false): while it is false the
// questions endpoints refuse service. The field has NO omitempty on
// either tag, same reasoning as the AutonomousNotifyConfig fields above:
// an operator's explicit false must survive a YAML/JSON round trip and a
// PUT /api/config write instead of being elided on marshal and leaving
// the previous value untouched on unmarshal.
type AutonomousQuestionsConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`

	// The three toggles below default ON when the key is absent. Go's zero
	// value (false) is indistinguishable from an explicit false, so they
	// are pointers: nil means "key absent, use the ON default". None of
	// them carries omitempty, for the same reason Enabled does not: an
	// operator's explicit false must survive a YAML/JSON round trip and a
	// PUT /api/config write instead of being elided on marshal and leaving
	// the previous value untouched on unmarshal.
	//
	// NotifyMessaging is on when a question is asked and answered: the
	// operator is notified at both moments.
	NotifyMessaging *bool `yaml:"notify_messaging" json:"notify_messaging"`
	// NotifyAlert fires only for high priority questions; it is on by
	// default.
	NotifyAlert *bool `yaml:"notify_alert" json:"notify_alert"`
	// ReturnPath is on by default: the event plus polled GET return path
	// is always available, and the flag only PERMITS the per-question
	// opt-in session line on top of it.
	ReturnPath *bool `yaml:"return_path" json:"return_path"`

	// DecisionGate is on by default when the key is absent (nil): a task
	// spec that names an operator decision id waits until that decision
	// is answered. No omitempty, same reasoning as the toggles above.
	DecisionGate *bool `yaml:"decision_gate" json:"decision_gate"`
	// DecisionGateMode is "shadow" (the default when empty: the gate
	// reports what it would hold but holds nothing) or "on".
	DecisionGateMode string `yaml:"decision_gate_mode" json:"decision_gate_mode"`
	// HeldMode is "off", "shadow" (the default when empty: reports what it
	// would hold but holds nothing) or "on".
	HeldMode string `yaml:"held_mode" json:"held_mode"`
	// HeldStates is the list of Automaton task state ids a held question
	// covers. Absent (nil) means all five HeldState* ids are covered; an
	// explicit empty list means none. No omitempty on either tag, same
	// reasoning as the AutonomousNotifyConfig fields above: an operator's
	// explicit empty list must survive a YAML/JSON round trip and a
	// PUT /api/config write instead of being elided on marshal and leaving
	// the previous value untouched on unmarshal.
	HeldStates []string `yaml:"held_states" json:"held_states"`

	// WorkerMarker is off when the key is absent (nil). When it is on, and
	// Enabled is on too, the daemon acts on a question a worker raises in
	// its session output. The switch alone changes nothing. No omitempty,
	// same reasoning as the toggles above: an explicit true or false must
	// survive a YAML/JSON round trip and a PUT /api/config write.
	WorkerMarker *bool `yaml:"worker_marker" json:"worker_marker"`

	// The four keys below tune the question quality check and the queue row.
	// Each is a pointer: nil means the key is absent and the Effective*
	// helper returns the default. None carries omitempty, so an explicit
	// value (including 0 or an empty pattern) survives a YAML/JSON round
	// trip and a PUT /api/config write.
	//
	// QualityMode is off, warn or refuse (default warn).
	QualityMode *string `yaml:"quality_mode" json:"quality_mode"`
	// QualityMinTextChars is the fewest characters a question's text may
	// have before the check flags it; a whole number >= 0 (default 60).
	QualityMinTextChars *int `yaml:"quality_min_text_chars" json:"quality_min_text_chars"`
	// QualityTrackerIDPattern is a regular expression that spots an internal
	// tracker identifier in a question (default
	// DefaultQuestionQualityTrackerIDPattern).
	QualityTrackerIDPattern *string `yaml:"quality_tracker_id_pattern" json:"quality_tracker_id_pattern"`
	// QueuePreviewLines is how many lines of a question the queue row shows
	// before the expand control; a whole number >= 1 (default 3).
	QueuePreviewLines *int `yaml:"queue_preview_lines" json:"queue_preview_lines"`
}

// IdentityConfig holds the operator identity settings (identity.*).
type IdentityConfig struct {
	// BackupsToKeep is how many timestamped identity backups the daemon
	// keeps; a whole number >= 1 (default DefaultIdentityBackupsToKeep).
	// A pointer so an absent key and an explicit value differ; no
	// omitempty, so an explicit value survives a YAML/JSON round trip and
	// a PUT /api/config write.
	BackupsToKeep *int `yaml:"backups_to_keep" json:"backups_to_keep"`
}

// DefaultIdentityBackupsToKeep is the number of identity backups kept when
// identity.backups_to_keep is absent. It repeats DefaultBackupsToKeep in
// internal/identity (config cannot import that package).
const DefaultIdentityBackupsToKeep = 10

// EffectiveBackupsToKeep returns identity.backups_to_keep: 10 when the key
// is absent.
func (c IdentityConfig) EffectiveBackupsToKeep() int {
	if c.BackupsToKeep == nil {
		return DefaultIdentityBackupsToKeep
	}
	return *c.BackupsToKeep
}

// ValidateIdentityBackupsToKeep refuses a retention count below 1. The
// config file load, PUT /api/config and every surface that rides it share
// this rule.
func ValidateIdentityBackupsToKeep(n int) error {
	if n < 1 {
		return fmt.Errorf("identity.backups_to_keep: %d is invalid (use a whole number >= 1; default %d)",
			n, DefaultIdentityBackupsToKeep)
	}
	return nil
}

func (c IdentityConfig) validate() error {
	if c.BackupsToKeep != nil {
		return ValidateIdentityBackupsToKeep(*c.BackupsToKeep)
	}
	return nil
}

// Question quality modes (autonomous.questions.quality_mode).
const (
	QuestionQualityModeOff    = "off"
	QuestionQualityModeWarn   = "warn"
	QuestionQualityModeRefuse = "refuse"
)

// Defaults for the question quality and queue preview keys. The mode, the
// minimum and the pattern repeat the shipped defaults of the quality check in
// internal/autonomous/question_quality.go (config cannot import that package);
// a test in internal/server holds the two copies equal.
const (
	DefaultQuestionQualityMode             = QuestionQualityModeWarn
	DefaultQuestionQualityMinTextChars     = 60
	DefaultQuestionQualityTrackerIDPattern = `\b[A-Z]{1,3}[0-9]{2,4}\b`
	DefaultQuestionQueuePreviewLines       = 3
)

// EffectiveQualityMode returns the quality mode: warn when the key is absent.
func (q AutonomousQuestionsConfig) EffectiveQualityMode() string {
	if q.QualityMode == nil || *q.QualityMode == "" {
		return DefaultQuestionQualityMode
	}
	return *q.QualityMode
}

// EffectiveQualityMinTextChars returns the minimum question length: 60 when
// the key is absent. An explicit 0 stays 0 (no length check).
func (q AutonomousQuestionsConfig) EffectiveQualityMinTextChars() int {
	if q.QualityMinTextChars == nil {
		return DefaultQuestionQualityMinTextChars
	}
	return *q.QualityMinTextChars
}

// EffectiveQualityTrackerIDPattern returns the tracker identifier pattern:
// the default when the key is absent or empty.
func (q AutonomousQuestionsConfig) EffectiveQualityTrackerIDPattern() string {
	if q.QualityTrackerIDPattern == nil || *q.QualityTrackerIDPattern == "" {
		return DefaultQuestionQualityTrackerIDPattern
	}
	return *q.QualityTrackerIDPattern
}

// EffectiveQueuePreviewLines returns how many lines of a question the queue
// row shows before the expand control: 3 when the key is absent.
func (q AutonomousQuestionsConfig) EffectiveQueuePreviewLines() int {
	if q.QueuePreviewLines == nil {
		return DefaultQuestionQueuePreviewLines
	}
	return *q.QueuePreviewLines
}

// ValidateQuestionQualityMode refuses a quality_mode other than off, warn or
// refuse (empty reads as warn). The config file load, PUT /api/config and every surface that rides
// it share this rule.
func ValidateQuestionQualityMode(mode string) error {
	switch mode {
	case "", QuestionQualityModeOff, QuestionQualityModeWarn, QuestionQualityModeRefuse:
		return nil
	}
	return fmt.Errorf("autonomous.questions.quality_mode: invalid value %q (valid values: %q, %q, %q, or empty for %q)",
		mode, QuestionQualityModeOff, QuestionQualityModeWarn, QuestionQualityModeRefuse, DefaultQuestionQualityMode)
}

// ValidateQuestionQualityMinTextChars refuses a negative minimum.
func ValidateQuestionQualityMinTextChars(n int) error {
	if n < 0 {
		return fmt.Errorf("autonomous.questions.quality_min_text_chars: %d is invalid (use a whole number >= 0; default %d)",
			n, DefaultQuestionQualityMinTextChars)
	}
	return nil
}

// ValidateQuestionQualityTrackerIDPattern refuses a pattern that does not
// compile as a regular expression. Empty is valid and reads as the default.
func ValidateQuestionQualityTrackerIDPattern(pattern string) error {
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("autonomous.questions.quality_tracker_id_pattern: %q does not compile as a regular expression (%v); use a valid pattern, or empty for the default",
			pattern, err)
	}
	return nil
}

// ValidateQuestionQueuePreviewLines refuses a preview shorter than one line.
func ValidateQuestionQueuePreviewLines(n int) error {
	if n < 1 {
		return fmt.Errorf("autonomous.questions.queue_preview_lines: %d is invalid (use a whole number >= 1; default %d)",
			n, DefaultQuestionQueuePreviewLines)
	}
	return nil
}

// Decision gate modes (autonomous.questions.decision_gate_mode).
const (
	DecisionGateModeShadow = "shadow"
	DecisionGateModeOn     = "on"
)

// Held question modes (autonomous.questions.held_mode).
const (
	HeldModeOff    = "off"
	HeldModeShadow = "shadow"
	HeldModeOn     = "on"
)

// The five Automaton task state ids a held question may cover
// (autonomous.questions.held_states).
const (
	HeldStateBlocked           = "blocked"
	HeldStateReviewHeld        = "review_held"
	HeldStateRevisionRequested = "revision_requested"
	HeldStateAwaitingDecision  = "awaiting_decision"
	HeldStatePendingInsert     = "pending_insert"
)

// AllHeldStates is the five held state ids in order: an absent (nil)
// held_states list reads this, an explicit empty list means none.
var AllHeldStates = []string{
	HeldStateBlocked,
	HeldStateReviewHeld,
	HeldStateRevisionRequested,
	HeldStateAwaitingDecision,
	HeldStatePendingInsert,
}

// EffectiveDecisionGate reports whether the decision gate is enabled.
// Absent (nil) is on.
func (q AutonomousQuestionsConfig) EffectiveDecisionGate() bool {
	return q.DecisionGate == nil || *q.DecisionGate
}

// EffectiveDecisionGateMode returns the decision gate mode: shadow when
// the value is empty, otherwise the value as set.
func (q AutonomousQuestionsConfig) EffectiveDecisionGateMode() string {
	if q.DecisionGateMode == "" {
		return DecisionGateModeShadow
	}
	return q.DecisionGateMode
}

// ValidateDecisionGateMode refuses a decision_gate_mode other than shadow, on
// or empty (empty reads as shadow). It is the one rule the config file load,
// PUT /api/config and every surface that rides it share.
func ValidateDecisionGateMode(mode string) error {
	return AutonomousQuestionsConfig{DecisionGateMode: mode}.validate()
}

// validate refuses a decision_gate_mode other than shadow or on, a held_mode
// other than off, shadow or on, any unknown held state id, and any question
// quality or queue preview value outside its valid range.
func (q AutonomousQuestionsConfig) validate() error {
	switch q.DecisionGateMode {
	case "", DecisionGateModeShadow, DecisionGateModeOn:
		// ok
	default:
		return fmt.Errorf("autonomous.questions.decision_gate_mode: invalid value %q (valid values: %q, %q, or empty for %q)",
			q.DecisionGateMode, DecisionGateModeShadow, DecisionGateModeOn, DecisionGateModeShadow)
	}
	if q.QualityMode != nil {
		if err := ValidateQuestionQualityMode(*q.QualityMode); err != nil {
			return err
		}
	}
	if q.QualityMinTextChars != nil {
		if err := ValidateQuestionQualityMinTextChars(*q.QualityMinTextChars); err != nil {
			return err
		}
	}
	if q.QualityTrackerIDPattern != nil {
		if err := ValidateQuestionQualityTrackerIDPattern(*q.QualityTrackerIDPattern); err != nil {
			return err
		}
	}
	if q.QueuePreviewLines != nil {
		if err := ValidateQuestionQueuePreviewLines(*q.QueuePreviewLines); err != nil {
			return err
		}
	}
	if err := ValidateHeldMode(q.HeldMode); err != nil {
		return err
	}
	return ValidateHeldStates(q.HeldStates)
}

// EffectiveHeldMode returns the held question mode in effect: shadow when
// the value is empty.
func (q AutonomousQuestionsConfig) EffectiveHeldMode() string {
	if q.HeldMode == "" {
		return HeldModeShadow
	}
	return q.HeldMode
}

// EffectiveHeldStates returns the held state ids in effect: all five
// HeldState* ids when the list is absent (nil), the list itself
// (including an explicit empty list, which means none) otherwise.
func (q AutonomousQuestionsConfig) EffectiveHeldStates() []string {
	if q.HeldStates == nil {
		return AllHeldStates
	}
	return q.HeldStates
}

// HeldStateCovered reports whether a task in the given state is covered by
// the held state ids in effect: an absent list covers all five ids, an
// explicit empty list covers none.
func (q AutonomousQuestionsConfig) HeldStateCovered(state string) bool {
	for _, s := range q.EffectiveHeldStates() {
		if s == state {
			return true
		}
	}
	return false
}

// ValidateHeldMode refuses a held_mode other than off, shadow, on or empty
// (empty reads as shadow). It is the one rule the config file load,
// PUT /api/config and every surface that rides it share.
func ValidateHeldMode(mode string) error {
	switch mode {
	case "", HeldModeOff, HeldModeShadow, HeldModeOn:
		return nil
	}
	return fmt.Errorf("autonomous.questions.held_mode: invalid value %q (valid values: %q, %q, %q, or empty for %q)",
		mode, HeldModeOff, HeldModeShadow, HeldModeOn, HeldModeShadow)
}

// ValidHeldState reports whether id is one of the five HeldState* ids.
func ValidHeldState(id string) bool {
	switch id {
	case HeldStateBlocked, HeldStateReviewHeld, HeldStateRevisionRequested,
		HeldStateAwaitingDecision, HeldStatePendingInsert:
		return true
	}
	return false
}

// ValidateHeldStates refuses a held state id that is not one of the five
// HeldState* ids, naming the valid ids. An absent (nil) list and an
// explicit empty list are both valid: they read "all five" and "none"
// respectively.
func ValidateHeldStates(states []string) error {
	for _, s := range states {
		if !ValidHeldState(s) {
			return fmt.Errorf("autonomous.questions.held_states: invalid state id %q (valid ids: %s)",
				s, strings.Join(AllHeldStates, ", "))
		}
	}
	return nil
}

// EffectiveWorkerMarker reports whether the worker question marker is
// acted on. Absent (nil) is off.
func (q AutonomousQuestionsConfig) EffectiveWorkerMarker() bool {
	return q.WorkerMarker != nil && *q.WorkerMarker
}

// EffectiveNotifyMessaging reports whether the operator is notified when
// a question is asked and when it is answered. Absent (nil) is on.
func (q AutonomousQuestionsConfig) EffectiveNotifyMessaging() bool {
	return q.NotifyMessaging == nil || *q.NotifyMessaging
}

// EffectiveNotifyAlert reports whether alerts fire for high priority
// questions. Absent (nil) is on.
func (q AutonomousQuestionsConfig) EffectiveNotifyAlert() bool {
	return q.NotifyAlert == nil || *q.NotifyAlert
}

// EffectiveReturnPath reports whether the return path is permitted: the
// event plus polled GET is always there; when this is on the per-question
// session line may be opted in. Absent (nil) is on.
func (q AutonomousQuestionsConfig) EffectiveReturnPath() bool {
	return q.ReturnPath == nil || *q.ReturnPath
}

// AutonomousDecisionsConfig is autonomous.decisions, the keys that tune the
// decision record (a structured record of one operator decision: title,
// situation, options with their tradeoffs, a recommendation and a forced
// default). Every key ships with the operator's answer as its default and none
// is tied to a particular repository.
//
// A field whose default is not the zero value is a pointer: nil means the key
// is absent and the Effective* helper returns the default. No field carries
// omitempty on either tag, same reasoning as AutonomousQuestionsConfig: an
// explicit false or 0 must survive a YAML/JSON round trip and a PUT
// /api/config write instead of being elided on marshal and leaving the
// previous value untouched on unmarshal.
type AutonomousDecisionsConfig struct {
	// Enabled is the master switch for decision records (default true).
	Enabled *bool `yaml:"enabled" json:"enabled"`

	// Folder is a directory of decision files the daemon watches. Empty (the
	// zero value, and the default) means the folder watch is off.
	Folder string `yaml:"folder" json:"folder"`
	// FolderGlob picks the files in Folder that are read (default "*.md").
	FolderGlob *string `yaml:"folder_glob" json:"folder_glob"`
	// FrontMatterDelimiter is the line that opens and closes the front matter
	// block of a decision file (default "---"); it may not be empty.
	FrontMatterDelimiter *string `yaml:"front_matter_delimiter" json:"front_matter_delimiter"`
	// FrontMatterSeparator splits a front matter line into key and value
	// (default ":"); it may not be empty.
	FrontMatterSeparator *string `yaml:"front_matter_separator" json:"front_matter_separator"`
	// FrontMatterFieldSeparator separates the parts of an option or
	// recommendation line (default "|"); it may not be empty.
	FrontMatterFieldSeparator *string `yaml:"front_matter_field_separator" json:"front_matter_field_separator"`
	// FrontMatterKeys maps a canonical key name to the name a file uses for it.
	// Empty (the default) means files use the canonical names.
	FrontMatterKeys map[string]string `yaml:"front_matter_keys" json:"front_matter_keys"`
	// FolderWithdrawOnDelete withdraws the record when its file is deleted
	// (default true).
	FolderWithdrawOnDelete *bool `yaml:"folder_withdraw_on_delete" json:"folder_withdraw_on_delete"`

	// ClarityMode is off, warn or refuse (default warn): how a record is
	// checked for being readable before the operator sees it.
	ClarityMode *string `yaml:"clarity_mode" json:"clarity_mode"`
	// ClarityMinTextChars is the fewest characters the situation and the
	// deciding text may have; a whole number >= 0 (default 60).
	ClarityMinTextChars *int `yaml:"clarity_min_text_chars" json:"clarity_min_text_chars"`
	// ClarityRequireTradeoffs asks every option for a one-line tradeoff
	// (default true).
	ClarityRequireTradeoffs *bool `yaml:"clarity_require_tradeoffs" json:"clarity_require_tradeoffs"`
	// ClarityRequireLabel asks for a short label on the record (default true).
	ClarityRequireLabel *bool `yaml:"clarity_require_label" json:"clarity_require_label"`
	// ClarityTrackerIDPattern is a regular expression that spots an internal
	// tracker identifier in a record. Empty (or absent) means the built-in
	// pattern, DefaultQuestionQualityTrackerIDPattern.
	ClarityTrackerIDPattern *string `yaml:"clarity_tracker_id_pattern" json:"clarity_tracker_id_pattern"`

	// DocRoot is the directory document paths on a record resolve against and
	// must stay inside. Empty (the default) means the daemon's working
	// directory.
	DocRoot string `yaml:"doc_root" json:"doc_root"`
	// DocStatusMode is off, report or on (default report): whether the answer
	// is written back to the status line of the linked document.
	DocStatusMode *string `yaml:"doc_status_mode" json:"doc_status_mode"`
	// DocStatusPattern is a regular expression for the status line (default
	// DefaultDecisionDocStatusPattern).
	DocStatusPattern *string `yaml:"doc_status_pattern" json:"doc_status_pattern"`
	// DocStatusTemplate is the text the status line is replaced with. The
	// placeholders are {choice}, {choice_label}, {quote}, {actor}, {date} and
	// {decision_id} (default DefaultDecisionDocStatusTemplate).
	DocStatusTemplate *string `yaml:"doc_status_template" json:"doc_status_template"`

	// FollowUps lists the actions an answered record may request. Empty (the
	// default) means nothing runs unless the operator configures it.
	FollowUps []AutonomousDecisionFollowUp `yaml:"follow_ups" json:"follow_ups"`
}

// AutonomousDecisionFollowUp is one entry of autonomous.decisions.follow_ups.
type AutonomousDecisionFollowUp struct {
	// Name identifies the follow-up; it is unique within the list.
	Name string `yaml:"name" json:"name"`
	// Kind is start_template (Target is a template id) or open_automaton
	// (Target is an Automaton id).
	Kind string `yaml:"kind" json:"kind"`
	// Target is the template id or the Automaton id.
	Target string `yaml:"target" json:"target"`
}

// Follow-up kinds (autonomous.decisions.follow_ups[].kind).
const (
	DecisionFollowUpStartTemplate = "start_template"
	DecisionFollowUpOpenAutomaton = "open_automaton"
)

// Clarity modes (autonomous.decisions.clarity_mode) and document status modes
// (autonomous.decisions.doc_status_mode).
const (
	DecisionClarityModeOff      = "off"
	DecisionClarityModeWarn     = "warn"
	DecisionClarityModeRefuse   = "refuse"
	DecisionDocStatusModeOff    = "off"
	DecisionDocStatusModeReport = "report"
	DecisionDocStatusModeOn     = "on"
)

// Defaults for the autonomous.decisions keys. This block is the one place a
// changed default is made: the Effective* helpers read it, never a second copy.
const (
	DefaultDecisionsEnabled                  = true
	DefaultDecisionFolderGlob                = "*.md"
	DefaultDecisionFrontMatterDelimiter      = "---"
	DefaultDecisionFrontMatterSeparator      = ":"
	DefaultDecisionFrontMatterFieldSeparator = "|"
	DefaultDecisionFolderWithdrawOnDelete    = true
	DefaultDecisionClarityMode               = DecisionClarityModeWarn
	DefaultDecisionClarityMinTextChars       = 60
	DefaultDecisionClarityRequireTradeoffs   = true
	DefaultDecisionClarityRequireLabel       = true
	DefaultDecisionClarityTrackerIDPattern   = DefaultQuestionQualityTrackerIDPattern
	DefaultDecisionDocStatusMode             = DecisionDocStatusModeReport
	DefaultDecisionDocStatusPattern          = `^Confirmed:.*$`
	DefaultDecisionDocStatusTemplate         = `Confirmed: {choice} - "{quote}" ({actor}, {date})`
)

func decisionBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func decisionString(p *string, def string) string {
	if p == nil || *p == "" {
		return def
	}
	return *p
}

// EffectiveEnabled reports whether decision records are on. Absent is on.
func (d AutonomousDecisionsConfig) EffectiveEnabled() bool {
	return decisionBool(d.Enabled, DefaultDecisionsEnabled)
}

// EffectiveFolder returns the watched folder; empty means the watch is off.
func (d AutonomousDecisionsConfig) EffectiveFolder() string { return d.Folder }

// EffectiveFolderGlob returns the file pattern: "*.md" when absent.
func (d AutonomousDecisionsConfig) EffectiveFolderGlob() string {
	return decisionString(d.FolderGlob, DefaultDecisionFolderGlob)
}

// EffectiveFrontMatterDelimiter returns the front matter delimiter: "---" when
// absent.
func (d AutonomousDecisionsConfig) EffectiveFrontMatterDelimiter() string {
	return decisionString(d.FrontMatterDelimiter, DefaultDecisionFrontMatterDelimiter)
}

// EffectiveFrontMatterSeparator returns the key and value separator: ":" when
// absent.
func (d AutonomousDecisionsConfig) EffectiveFrontMatterSeparator() string {
	return decisionString(d.FrontMatterSeparator, DefaultDecisionFrontMatterSeparator)
}

// EffectiveFrontMatterFieldSeparator returns the separator between the parts of
// an option or recommendation line: "|" when absent.
func (d AutonomousDecisionsConfig) EffectiveFrontMatterFieldSeparator() string {
	return decisionString(d.FrontMatterFieldSeparator, DefaultDecisionFrontMatterFieldSeparator)
}

// EffectiveFrontMatterKeys returns a copy of the canonical-to-file key map;
// empty (never nil) means files use the canonical names.
func (d AutonomousDecisionsConfig) EffectiveFrontMatterKeys() map[string]string {
	out := make(map[string]string, len(d.FrontMatterKeys))
	for k, v := range d.FrontMatterKeys {
		out[k] = v
	}
	return out
}

// EffectiveFolderWithdrawOnDelete reports whether deleting a decision file
// withdraws its record. Absent is on.
func (d AutonomousDecisionsConfig) EffectiveFolderWithdrawOnDelete() bool {
	return decisionBool(d.FolderWithdrawOnDelete, DefaultDecisionFolderWithdrawOnDelete)
}

// EffectiveClarityMode returns the clarity mode: warn when absent or empty.
func (d AutonomousDecisionsConfig) EffectiveClarityMode() string {
	return decisionString(d.ClarityMode, DefaultDecisionClarityMode)
}

// EffectiveClarityMinTextChars returns the minimum text length: 60 when absent.
// An explicit 0 stays 0 (no length check).
func (d AutonomousDecisionsConfig) EffectiveClarityMinTextChars() int {
	if d.ClarityMinTextChars == nil {
		return DefaultDecisionClarityMinTextChars
	}
	return *d.ClarityMinTextChars
}

// EffectiveClarityRequireTradeoffs reports whether every option needs a
// tradeoff. Absent is on.
func (d AutonomousDecisionsConfig) EffectiveClarityRequireTradeoffs() bool {
	return decisionBool(d.ClarityRequireTradeoffs, DefaultDecisionClarityRequireTradeoffs)
}

// EffectiveClarityRequireLabel reports whether a record needs a label. Absent
// is on.
func (d AutonomousDecisionsConfig) EffectiveClarityRequireLabel() bool {
	return decisionBool(d.ClarityRequireLabel, DefaultDecisionClarityRequireLabel)
}

// EffectiveClarityTrackerIDPattern returns the tracker identifier pattern: the
// built-in pattern when the key is absent or empty.
func (d AutonomousDecisionsConfig) EffectiveClarityTrackerIDPattern() string {
	return decisionString(d.ClarityTrackerIDPattern, DefaultDecisionClarityTrackerIDPattern)
}

// EffectiveDocRoot returns the document root; empty means the daemon's working
// directory.
func (d AutonomousDecisionsConfig) EffectiveDocRoot() string { return d.DocRoot }

// EffectiveDocStatusMode returns the document status mode: report when absent
// or empty.
func (d AutonomousDecisionsConfig) EffectiveDocStatusMode() string {
	return decisionString(d.DocStatusMode, DefaultDecisionDocStatusMode)
}

// EffectiveDocStatusPattern returns the status line pattern: "^Confirmed:.*$"
// when absent or empty.
func (d AutonomousDecisionsConfig) EffectiveDocStatusPattern() string {
	return decisionString(d.DocStatusPattern, DefaultDecisionDocStatusPattern)
}

// EffectiveDocStatusTemplate returns the status line template: the default
// template when absent or empty.
func (d AutonomousDecisionsConfig) EffectiveDocStatusTemplate() string {
	return decisionString(d.DocStatusTemplate, DefaultDecisionDocStatusTemplate)
}

// EffectiveFollowUps returns a copy of the follow-up list; empty (never nil)
// means nothing is configured.
func (d AutonomousDecisionsConfig) EffectiveFollowUps() []AutonomousDecisionFollowUp {
	out := make([]AutonomousDecisionFollowUp, len(d.FollowUps))
	copy(out, d.FollowUps)
	return out
}

// ValidateDecisionClarityMode refuses a clarity_mode other than off, warn or
// refuse (empty reads as warn).
func ValidateDecisionClarityMode(mode string) error {
	switch mode {
	case "", DecisionClarityModeOff, DecisionClarityModeWarn, DecisionClarityModeRefuse:
		return nil
	}
	return fmt.Errorf("autonomous.decisions.clarity_mode: invalid value %q (valid values: %q, %q, %q, or empty for %q)",
		mode, DecisionClarityModeOff, DecisionClarityModeWarn, DecisionClarityModeRefuse, DefaultDecisionClarityMode)
}

// ValidateDecisionDocStatusMode refuses a doc_status_mode other than off,
// report or on (empty reads as report).
func ValidateDecisionDocStatusMode(mode string) error {
	switch mode {
	case "", DecisionDocStatusModeOff, DecisionDocStatusModeReport, DecisionDocStatusModeOn:
		return nil
	}
	return fmt.Errorf("autonomous.decisions.doc_status_mode: invalid value %q (valid values: %q, %q, %q, or empty for %q)",
		mode, DecisionDocStatusModeOff, DecisionDocStatusModeReport, DecisionDocStatusModeOn, DefaultDecisionDocStatusMode)
}

// ValidateDecisionClarityMinTextChars refuses a negative minimum.
func ValidateDecisionClarityMinTextChars(n int) error {
	if n < 0 {
		return fmt.Errorf("autonomous.decisions.clarity_min_text_chars: %d is invalid (use a whole number >= 0; default %d)",
			n, DefaultDecisionClarityMinTextChars)
	}
	return nil
}

// ValidateDecisionClarityTrackerIDPattern refuses a pattern that does not
// compile. Empty is valid and reads as the built-in pattern.
func ValidateDecisionClarityTrackerIDPattern(pattern string) error {
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("autonomous.decisions.clarity_tracker_id_pattern: %q does not compile as a regular expression (%v); use a valid pattern, or empty for the built-in pattern",
			pattern, err)
	}
	return nil
}

// ValidateDecisionDocStatusPattern refuses a pattern that does not compile.
// Empty is valid and reads as the default.
func ValidateDecisionDocStatusPattern(pattern string) error {
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("autonomous.decisions.doc_status_pattern: %q does not compile as a regular expression (%v); use a valid pattern, or empty for the default %q",
			pattern, err, DefaultDecisionDocStatusPattern)
	}
	return nil
}

// ValidateDecisionFrontMatterDelimiter refuses an empty delimiter.
func ValidateDecisionFrontMatterDelimiter(s string) error {
	return validateDecisionNonEmpty("front_matter_delimiter", s, DefaultDecisionFrontMatterDelimiter)
}

// ValidateDecisionFrontMatterSeparator refuses an empty separator.
func ValidateDecisionFrontMatterSeparator(s string) error {
	return validateDecisionNonEmpty("front_matter_separator", s, DefaultDecisionFrontMatterSeparator)
}

// ValidateDecisionFrontMatterFieldSeparator refuses an empty separator.
func ValidateDecisionFrontMatterFieldSeparator(s string) error {
	return validateDecisionNonEmpty("front_matter_field_separator", s, DefaultDecisionFrontMatterFieldSeparator)
}

func validateDecisionNonEmpty(key, s, def string) error {
	if s == "" {
		return fmt.Errorf("autonomous.decisions.%s: an empty value is invalid (use a non-empty text; default %q)", key, def)
	}
	return nil
}

// ValidateDecisionFollowUps refuses a follow-up list with an entry that has an
// empty name, a kind other than start_template or open_automaton, an empty
// target, or a name that an earlier entry already uses.
func ValidateDecisionFollowUps(list []AutonomousDecisionFollowUp) error {
	seen := make(map[string]bool, len(list))
	for i, f := range list {
		switch {
		case f.Name == "":
			return fmt.Errorf("autonomous.decisions.follow_ups[%d]: name is empty (give every follow-up a name)", i)
		case f.Kind != DecisionFollowUpStartTemplate && f.Kind != DecisionFollowUpOpenAutomaton:
			return fmt.Errorf("autonomous.decisions.follow_ups[%d] (%s): invalid kind %q (valid values: %q, %q)",
				i, f.Name, f.Kind, DecisionFollowUpStartTemplate, DecisionFollowUpOpenAutomaton)
		case f.Target == "":
			return fmt.Errorf("autonomous.decisions.follow_ups[%d] (%s): target is empty (name a template id or an Automaton id)", i, f.Name)
		case seen[f.Name]:
			return fmt.Errorf("autonomous.decisions.follow_ups[%d]: duplicate name %q (each follow-up needs its own name)", i, f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

// validate refuses any autonomous.decisions value outside its valid range. It
// is the one rule the config file load and every surface that rides it share.
func (d AutonomousDecisionsConfig) validate() error {
	if d.ClarityMode != nil {
		if err := ValidateDecisionClarityMode(*d.ClarityMode); err != nil {
			return err
		}
	}
	if d.DocStatusMode != nil {
		if err := ValidateDecisionDocStatusMode(*d.DocStatusMode); err != nil {
			return err
		}
	}
	if d.ClarityMinTextChars != nil {
		if err := ValidateDecisionClarityMinTextChars(*d.ClarityMinTextChars); err != nil {
			return err
		}
	}
	if d.ClarityTrackerIDPattern != nil {
		if err := ValidateDecisionClarityTrackerIDPattern(*d.ClarityTrackerIDPattern); err != nil {
			return err
		}
	}
	if d.DocStatusPattern != nil {
		if err := ValidateDecisionDocStatusPattern(*d.DocStatusPattern); err != nil {
			return err
		}
	}
	if d.FrontMatterDelimiter != nil {
		if err := ValidateDecisionFrontMatterDelimiter(*d.FrontMatterDelimiter); err != nil {
			return err
		}
	}
	if d.FrontMatterSeparator != nil {
		if err := ValidateDecisionFrontMatterSeparator(*d.FrontMatterSeparator); err != nil {
			return err
		}
	}
	if d.FrontMatterFieldSeparator != nil {
		if err := ValidateDecisionFrontMatterFieldSeparator(*d.FrontMatterFieldSeparator); err != nil {
			return err
		}
	}
	return ValidateDecisionFollowUps(d.FollowUps)
}

// DefaultPendingEventsMax is the pending_events_max fallback.
const DefaultPendingEventsMax = 500

// EffectivePendingEventsMax returns PendingEventsMax, or the default
// when it is below 1.
func (n AutonomousNotifyConfig) EffectivePendingEventsMax() int {
	if n.PendingEventsMax < 1 {
		return DefaultPendingEventsMax
	}
	return n.PendingEventsMax
}

// The defaults below are the recommended values in the outbound-webhook and
// forwarding proposal (docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md,
// section 2). Each one is pending operator confirmation. This block is the one
// place a changed decision is made: every Effective* helper below and the
// autonomous package read these, never a second copy. A zero or negative value
// in a config field means "use this default".
const (
	// DefaultNotifyMaxAttempts is the total attempts per delivery, the first
	// included. Pending operator confirmation.
	DefaultNotifyMaxAttempts = 5
	// DefaultNotifyJitterPercent is the +- spread applied to each retry delay.
	// Jitter cannot be turned off: 0 falls back to this value. Pending
	// operator confirmation.
	DefaultNotifyJitterPercent = 20
	// DefaultNotifyAttemptTimeoutSeconds bounds one attempt (connect, request
	// and response). Pending operator confirmation.
	DefaultNotifyAttemptTimeoutSeconds = 10
	// DefaultNotifyMaxBodyBytes caps the serialised event envelope. It is also
	// the size a forwarding receiver accepts. Pending operator confirmation.
	DefaultNotifyMaxBodyBytes = 64 * 1024
	// DefaultNotifySignatureToleranceSeconds is the replay window a receiver
	// should enforce on the signed timestamp. Pending operator confirmation.
	DefaultNotifySignatureToleranceSeconds = 300
	// DefaultNotifyMaxHops is the hop cap of a forwarded event. Pending
	// operator confirmation.
	DefaultNotifyMaxHops = 3
)

// DefaultNotifyBackoffSeconds returns the delay after each failed attempt:
// 1s, 4s, 16s, 64s. The last entry repeats for later attempts. Pending
// operator confirmation. A fresh slice each call, so callers cannot change
// the default.
func DefaultNotifyBackoffSeconds() []int {
	return []int{1, 4, 16, 64}
}

// AutonomousNotifyWebhookConfig is autonomous.notify.webhook, the outbound
// webhook sink. Every subscription with the webhook sink type is refused while
// Enabled is false, and any such subscription already stored delivers nothing.
// AllowHosts is the only list of hosts that may receive an event: an exact host
// or an exact host:port, and an empty list means nothing can receive.
// Plain YAML and JSON with no omitempty, so an explicit false or an empty list
// survives a round trip. A subscription's signing secret is never set here; it
// is a ${secret:name} reference on the subscription itself.
type AutonomousNotifyWebhookConfig struct {
	Enabled    bool     `yaml:"enabled" json:"enabled"`
	AllowHosts []string `yaml:"allow_hosts" json:"allow_hosts"`
	// MaxAttempts is the total attempts per delivery. Values below 1 mean the default.
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	// BackoffSeconds is the delay after each failed attempt; the last entry
	// repeats. An empty list means the default.
	BackoffSeconds []int `yaml:"backoff_seconds" json:"backoff_seconds"`
	// JitterPercent is the +- spread on each delay. Values below 1 mean the default.
	JitterPercent int `yaml:"jitter_percent" json:"jitter_percent"`
	// AttemptTimeoutSeconds bounds one attempt. Values below 1 mean the default.
	AttemptTimeoutSeconds int `yaml:"attempt_timeout_seconds" json:"attempt_timeout_seconds"`
	// MaxBodyBytes caps the serialised envelope. Values below 1 mean the default.
	MaxBodyBytes int `yaml:"max_body_bytes" json:"max_body_bytes"`
	// SignatureToleranceSeconds is the replay window a receiver enforces on the
	// signed timestamp. Values below 1 mean the default.
	SignatureToleranceSeconds int `yaml:"signature_tolerance_seconds" json:"signature_tolerance_seconds"`
}

// AutonomousNotifyForwardingConfig is autonomous.notify.forwarding, the peer
// forwarding of lifecycle events through the federation registry. It ships
// disabled: while Enabled is false, peer subscriptions are refused at creation
// and this daemon refuses to receive forwarded events. The retry fields are
// independent of the webhook sink's, with the same default values.
type AutonomousNotifyForwardingConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// MaxHops is the hop cap of a forwarded event. Values below 1 mean the default.
	MaxHops int `yaml:"max_hops" json:"max_hops"`
	// MaxAttempts is the total attempts per forwarded event. Values below 1 mean the default.
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	// BackoffSeconds is the delay after each failed attempt; the last entry
	// repeats. An empty list means the default.
	BackoffSeconds []int `yaml:"backoff_seconds" json:"backoff_seconds"`
	// JitterPercent is the +- spread on each delay. Values below 1 mean the default.
	JitterPercent int `yaml:"jitter_percent" json:"jitter_percent"`
	// AttemptTimeoutSeconds bounds one attempt. Values below 1 mean the default.
	AttemptTimeoutSeconds int `yaml:"attempt_timeout_seconds" json:"attempt_timeout_seconds"`
}

// effectiveNotifyInt returns v, or def when v is below 1.
func effectiveNotifyInt(v, def int) int {
	if v < 1 {
		return def
	}
	return v
}

// effectiveNotifyBackoff returns the positive entries of list, or the default
// schedule when none are positive. A fresh slice either way.
func effectiveNotifyBackoff(list []int) []int {
	out := make([]int, 0, len(list))
	for _, s := range list {
		if s >= 1 {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return DefaultNotifyBackoffSeconds()
	}
	return out
}

// effectiveNotifyJitter returns v, capped at 100, or the default when v is below 1.
func effectiveNotifyJitter(v int) int {
	if v < 1 {
		return DefaultNotifyJitterPercent
	}
	if v > 100 {
		return 100
	}
	return v
}

// EffectiveMaxAttempts returns MaxAttempts, or the default when it is below 1.
func (w AutonomousNotifyWebhookConfig) EffectiveMaxAttempts() int {
	return effectiveNotifyInt(w.MaxAttempts, DefaultNotifyMaxAttempts)
}

// EffectiveBackoffSeconds returns the positive entries of BackoffSeconds, or
// the default schedule when there are none.
func (w AutonomousNotifyWebhookConfig) EffectiveBackoffSeconds() []int {
	return effectiveNotifyBackoff(w.BackoffSeconds)
}

// EffectiveJitterPercent returns JitterPercent, capped at 100, or the default
// when it is below 1.
func (w AutonomousNotifyWebhookConfig) EffectiveJitterPercent() int {
	return effectiveNotifyJitter(w.JitterPercent)
}

// EffectiveAttemptTimeoutSeconds returns AttemptTimeoutSeconds, or the default
// when it is below 1.
func (w AutonomousNotifyWebhookConfig) EffectiveAttemptTimeoutSeconds() int {
	return effectiveNotifyInt(w.AttemptTimeoutSeconds, DefaultNotifyAttemptTimeoutSeconds)
}

// EffectiveMaxBodyBytes returns MaxBodyBytes, or the default when it is below 1.
func (w AutonomousNotifyWebhookConfig) EffectiveMaxBodyBytes() int {
	return effectiveNotifyInt(w.MaxBodyBytes, DefaultNotifyMaxBodyBytes)
}

// EffectiveSignatureToleranceSeconds returns SignatureToleranceSeconds, or the
// default when it is below 1.
func (w AutonomousNotifyWebhookConfig) EffectiveSignatureToleranceSeconds() int {
	return effectiveNotifyInt(w.SignatureToleranceSeconds, DefaultNotifySignatureToleranceSeconds)
}

// EffectiveMaxHops returns MaxHops, or the default when it is below 1.
func (f AutonomousNotifyForwardingConfig) EffectiveMaxHops() int {
	return effectiveNotifyInt(f.MaxHops, DefaultNotifyMaxHops)
}

// EffectiveMaxAttempts returns MaxAttempts, or the default when it is below 1.
func (f AutonomousNotifyForwardingConfig) EffectiveMaxAttempts() int {
	return effectiveNotifyInt(f.MaxAttempts, DefaultNotifyMaxAttempts)
}

// EffectiveBackoffSeconds returns the positive entries of BackoffSeconds, or
// the default schedule when there are none.
func (f AutonomousNotifyForwardingConfig) EffectiveBackoffSeconds() []int {
	return effectiveNotifyBackoff(f.BackoffSeconds)
}

// EffectiveJitterPercent returns JitterPercent, capped at 100, or the default
// when it is below 1.
func (f AutonomousNotifyForwardingConfig) EffectiveJitterPercent() int {
	return effectiveNotifyJitter(f.JitterPercent)
}

// EffectiveAttemptTimeoutSeconds returns AttemptTimeoutSeconds, or the default
// when it is below 1.
func (f AutonomousNotifyForwardingConfig) EffectiveAttemptTimeoutSeconds() int {
	return effectiveNotifyInt(f.AttemptTimeoutSeconds, DefaultNotifyAttemptTimeoutSeconds)
}

// ScanConfig — BL221 Phase 3 runtime config, mirrors
// internal/autonomous/scan.Config but lives in the top-level config
// struct for YAML round-trip without an import cycle (same pattern as
// QualityGateConfig above).
type ScanConfig struct {
	Enabled            bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	SASTEnabled        bool   `yaml:"sast_enabled,omitempty" json:"sast_enabled,omitempty"`
	SecretsEnabled     bool   `yaml:"secrets_enabled,omitempty" json:"secrets_enabled,omitempty"`
	DepsEnabled        bool   `yaml:"deps_enabled,omitempty" json:"deps_enabled,omitempty"`
	FailOnSeverity     string `yaml:"fail_on_severity,omitempty" json:"fail_on_severity,omitempty"`
	MaxFindings        int    `yaml:"max_findings,omitempty" json:"max_findings,omitempty"`
	RulesGraderEnabled bool   `yaml:"rules_grader_enabled,omitempty" json:"rules_grader_enabled,omitempty"`
	FixLoopEnabled     bool   `yaml:"fix_loop_enabled,omitempty" json:"fix_loop_enabled,omitempty"`
	FixLoopMaxRetries  int    `yaml:"fix_loop_max_retries,omitempty" json:"fix_loop_max_retries,omitempty"`

	// BL406 Phase 0/1 — the new fourth scan category. Data-driven,
	// operator-defined rules; mirrors internal/autonomous/scan.ProjectRule.
	ProjectRules []ProjectRuleConfig `yaml:"project_rules,omitempty" json:"project_rules,omitempty"`
}

// ProjectRuleConfig — BL406, mirrors scan.ProjectRule. One operator-
// defined rule for the project-rules scan category.
type ProjectRuleConfig struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
	// Type is one of: content | consistency | presence | parity.
	Type string `yaml:"type" json:"type"`
	// Granularity is one of: task_complete | story_complete | prd_complete.
	Granularity string `yaml:"granularity" json:"granularity"`
	// Pattern is the type-specific check — a regex for "content", a
	// pair of file paths for "consistency", a file/glob for "presence".
	// Unused for "parity" (built in, no pattern needed).
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	// Severity is one of: warning | error | critical (scan.Severity's
	// string values). "info" is valid but never fails a scan.
	Severity string `yaml:"severity,omitempty" json:"severity,omitempty"`
	// Action is one of: "" (finding only) | file_upstream_issue.
	Action string `yaml:"action,omitempty" json:"action,omitempty"`
	// UpstreamTarget names an UpstreamRepoConfig.Name — required when
	// Action is file_upstream_issue.
	UpstreamTarget string `yaml:"upstream_target,omitempty" json:"upstream_target,omitempty"`
}

// UpstreamRepoConfig — BL406. A named GitHub repo a project-rules
// "file_upstream_issue" action can file into.
type UpstreamRepoConfig struct {
	Name      string `yaml:"name" json:"name"`
	OwnerRepo string `yaml:"owner_repo" json:"owner_repo"` // e.g. "dmz006/datawatch-app"
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
	Peers ObserverPeersConfig `yaml:"peers" json:"peers"`

	// Pull is the observer pull protocol: instead of peers pushing
	// envelopes, the daemon pulls from a peer's observer endpoint on
	// a schedule. Off by default; per-peer overrides live on the
	// multiserver registry entry (observer_pull, observer_pull_interval_s,
	// observer_pull_timeout_s, where 0 = use these globals).
	Pull ObserverPullConfig `yaml:"pull,omitempty" json:"pull,omitempty"`
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
	// SilentAlertMinutes is how long a peer may stay silent before an
	// alert is raised; default 5, 0 disables. No omitempty so an explicit
	// 0 survives a YAML round trip instead of reverting to the default.
	SilentAlertMinutes int `yaml:"silent_alert_minutes" json:"silent_alert_minutes"`
}

// ObserverPullMinIntervalS is the floor for observer.pull.default_interval_s
// (and for any per-peer interval override): pulling faster than every 5
// seconds hammers the peer for little benefit.
const ObserverPullMinIntervalS = 5

// ObserverPullConfig — the observer pull protocol globals. Enabled gates
// the pull mode globally; per-peer opt-in lives on the multiserver
// registry entry. DefaultIntervalS / DefaultTimeoutS are the fallbacks a
// per-peer entry inherits when it leaves its own interval/timeout at 0.
type ObserverPullConfig struct {
	// Enabled turns on observer pull mode globally. Default: false.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// DefaultIntervalS is the default pull interval in seconds a peer
	// entry inherits when its own is 0. Default: 30; must be at least
	// ObserverPullMinIntervalS (5).
	DefaultIntervalS int `yaml:"default_interval_s" json:"default_interval_s"`
	// DefaultTimeoutS is the default per-pull request timeout in seconds
	// a peer entry inherits when its own is 0. Default: 5; must be >= 0.
	DefaultTimeoutS int `yaml:"default_timeout_s" json:"default_timeout_s"`
}

// DefaultObserverPullConfig returns the shipped observer.pull defaults:
// disabled, 30s interval, 5s timeout. The single source of truth for the
// block's defaults — a config that omits observer.pull entirely resolves
// to this value.
func DefaultObserverPullConfig() ObserverPullConfig {
	return ObserverPullConfig{
		Enabled:          false,
		DefaultIntervalS: 30,
		DefaultTimeoutS:  5,
	}
}

// ValidateObserverPullConfig rejects an interval below the 5-second floor
// (which also rejects a negative interval) and a negative timeout, naming
// the offending observer.pull.* key in each error.
func ValidateObserverPullConfig(p ObserverPullConfig) error {
	if p.DefaultIntervalS < 0 {
		return fmt.Errorf("observer.pull.default_interval_s: %d must be >= 0", p.DefaultIntervalS)
	}
	if p.DefaultIntervalS < ObserverPullMinIntervalS {
		return fmt.Errorf("observer.pull.default_interval_s: %d is below the floor of %d seconds", p.DefaultIntervalS, ObserverPullMinIntervalS)
	}
	if p.DefaultTimeoutS < 0 {
		return fmt.Errorf("observer.pull.default_timeout_s: %d must be >= 0", p.DefaultTimeoutS)
	}
	return nil
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
	// Managed names the kind:service plugins the operator allows datawatch
	// to start/stop; absent = never managed. Only honoured for plugins whose
	// manifest lifecycle.supported_modes declares "managed".
	Managed []string `yaml:"managed,omitempty" json:"managed,omitempty"`
	// Services holds the service-plugin watch settings (plugins.services):
	// the attach-only health-probe cadence, the enrichment-backlog growth
	// streak, the model-use thresholds, and the per-service lifecycle mode.
	// Absent fields read their defaults via the Effective* helpers, so an
	// operator setting one field does not have to set the rest.
	Services PluginServicesConfig `yaml:"services,omitempty" json:"services,omitempty"`
}

// PluginServicesConfig holds the plugins.services block — the settings for
// the service-kind plugins the daemon watches (attach-only health probes,
// enrichment-backlog growth, model-use findings) and the per-service
// lifecycle mode.
type PluginServicesConfig struct {
	// HealthIntervalSeconds is how often the daemon health-probes each
	// attach-only service plugin. Default DefaultPluginServicesHealthInterval;
	// minimum MinPluginServicesHealthInterval (a value below the minimum is
	// a config error on load).
	HealthIntervalSeconds int `yaml:"health_interval_seconds,omitempty" json:"health_interval_seconds,omitempty"`
	// BacklogGrowthPulls is how many consecutive rises of a plugin's
	// enrichment backlog (each at or above the minimum rise) the daemon
	// requires before raising the enrichment_backlog_growing condition.
	// Default DefaultPluginServicesBacklogGrowthPulls; must be >= 1.
	BacklogGrowthPulls int `yaml:"backlog_growth_pulls,omitempty" json:"backlog_growth_pulls,omitempty"`
	// NodeLoadRatio is the node-overload threshold for the model-use report:
	// a model's node counts as overloaded when it holds more than this many
	// times the model memory of another node able to serve the same model.
	// Default DefaultPluginServicesNodeLoadRatio; must be > 0.
	NodeLoadRatio float64 `yaml:"node_load_ratio,omitempty" json:"node_load_ratio,omitempty"`
	// SmallModelMaxParamsBillions is the small-model boundary in parameter
	// count (billions): a model at or above this is not a small model.
	// Default DefaultPluginServicesSmallModelMaxParamsBillions; must be > 0.
	SmallModelMaxParamsBillions float64 `yaml:"small_model_max_params_billions,omitempty" json:"small_model_max_params_billions,omitempty"`
	// SmallModelContextTokens is the context length (tokens) above which a
	// small model raises the small_model_large_context finding. Default
	// DefaultPluginServicesSmallModelContextTokens; must be >= 1.
	SmallModelContextTokens int `yaml:"small_model_context_tokens,omitempty" json:"small_model_context_tokens,omitempty"`
	// PerService holds one entry per plugin name — today only the lifecycle
	// mode. Absent entry = "" = the plugin's only supported mode (or
	// attach_only when it supports both).
	PerService map[string]PluginServiceOverride `yaml:"per_service,omitempty" json:"per_service,omitempty"`
}

// PluginServiceOverride is one per_service entry: the settings that vary
// per named plugin.
type PluginServiceOverride struct {
	// LifecycleMode is the named mode: "attach_only" or "managed" (or ""; see
	// LifecycleMode). Only honoured when the manifest declares the mode in
	// lifecycle.supported_modes.
	LifecycleMode string `yaml:"lifecycle_mode,omitempty" json:"lifecycle_mode,omitempty"`
}

const (
	// DefaultPluginServicesHealthInterval — attach-only probe cadence,
	// seconds; a minute keeps the probes quiet on shared endpoints.
	DefaultPluginServicesHealthInterval = 60
	// MinPluginServicesHealthInterval — below this (seconds) a config is
	// rejected at load; the probes would hammer the service.
	MinPluginServicesHealthInterval = 5
	// DefaultPluginServicesBacklogGrowthPulls — three consecutive backlog
	// rises before the daemon says the backlog is growing.
	DefaultPluginServicesBacklogGrowthPulls = 3
	// DefaultPluginServicesNodeLoadRatio — one node holding 1.5x another
	// candidate node's model memory is the overloaded threshold.
	DefaultPluginServicesNodeLoadRatio = 1.5
	// DefaultPluginServicesSmallModelMaxParamsBillions — the 8-billion
	// parameter small-model boundary.
	DefaultPluginServicesSmallModelMaxParamsBillions = 8.0
	// DefaultPluginServicesSmallModelContextTokens — a small model pinned
	// above an 8k context is the large-context finding.
	DefaultPluginServicesSmallModelContextTokens = 8192
)

// The two service lifecycle modes a per_service entry may name. They mirror
// the internal/plugins manifest modes; the config package does not import
// that one, so the names live here and both sides agree on the strings.
const (
	// PluginServiceLifecycleAttachOnly — datawatch observes the service it
	// attaches to and never owns or restarts that process.
	PluginServiceLifecycleAttachOnly = "attach_only"
	// PluginServiceLifecycleManaged — datawatch starts, stops and
	// restarts the process itself.
	PluginServiceLifecycleManaged = "managed"
)

// ValidLifecycleModes — the closed accepted set, in the order the errors
// that list them should print it.
var ValidLifecycleModes = []string{PluginServiceLifecycleAttachOnly, PluginServiceLifecycleManaged}

// EffectiveHealthInterval returns the attach-only probe cadence in seconds
// (stored when >= the minimum, otherwise the default).
func (s PluginServicesConfig) EffectiveHealthInterval() int {
	if s.HealthIntervalSeconds >= MinPluginServicesHealthInterval {
		return s.HealthIntervalSeconds
	}
	return DefaultPluginServicesHealthInterval
}

// EffectiveBacklogGrowthPulls returns the stored streak when >= 1, else
// the default.
func (s PluginServicesConfig) EffectiveBacklogGrowthPulls() int {
	if s.BacklogGrowthPulls >= 1 {
		return s.BacklogGrowthPulls
	}
	return DefaultPluginServicesBacklogGrowthPulls
}

// EffectiveNodeLoadRatio returns the stored ratio when > 0 and finite,
// else the default.
func (s PluginServicesConfig) EffectiveNodeLoadRatio() float64 {
	if s.NodeLoadRatio > 0 && !math.IsNaN(s.NodeLoadRatio) && !math.IsInf(s.NodeLoadRatio, 0) {
		return s.NodeLoadRatio
	}
	return DefaultPluginServicesNodeLoadRatio
}

// EffectiveSmallModelMaxParamsBillions returns the stored boundary when
// > 0 and finite, else the default.
func (s PluginServicesConfig) EffectiveSmallModelMaxParamsBillions() float64 {
	if s.SmallModelMaxParamsBillions > 0 && !math.IsNaN(s.SmallModelMaxParamsBillions) && !math.IsInf(s.SmallModelMaxParamsBillions, 0) {
		return s.SmallModelMaxParamsBillions
	}
	return DefaultPluginServicesSmallModelMaxParamsBillions
}

// EffectiveSmallModelContextTokens returns the stored context length when
// >= 1, else the default.
func (s PluginServicesConfig) EffectiveSmallModelContextTokens() int {
	if s.SmallModelContextTokens >= 1 {
		return s.SmallModelContextTokens
	}
	return DefaultPluginServicesSmallModelContextTokens
}

// LifecycleMode returns the operator's per-service mode for service —
// "" (unset), or one of ValidLifecycleModes, lower-cased and trimmed. A
// direct struct write that bypassed load and PUT and carried some other
// spelling normalizes to "" rather than a live mode, never to a typo.
func (s PluginServicesConfig) LifecycleMode(service string) string {
	mode := strings.ToLower(strings.TrimSpace(s.PerService[service].LifecycleMode))
	if mode == "" || mode == PluginServiceLifecycleAttachOnly || mode == PluginServiceLifecycleManaged {
		return mode
	}
	return ""
}

// validatePluginServices rejects the values the Effective* helpers would
// silently paper over. It runs at Load/LoadSecure time (via validateLoaded)
// and the PUT /api/config path reuses it, so a misconfiguration cannot land
// on disk and then break the next startup.
func validatePluginServices(s PluginServicesConfig, prefix string) error {
	if s.HealthIntervalSeconds != 0 && s.HealthIntervalSeconds < MinPluginServicesHealthInterval {
		return fmt.Errorf("%s.health_interval_seconds: %d must be a whole number >= %d (seconds; default %d)",
			prefix, s.HealthIntervalSeconds, MinPluginServicesHealthInterval, DefaultPluginServicesHealthInterval)
	}
	if s.BacklogGrowthPulls != 0 && s.BacklogGrowthPulls < 1 {
		return fmt.Errorf("%s.backlog_growth_pulls: %d must be a whole number >= 1 (default %d)",
			prefix, s.BacklogGrowthPulls, DefaultPluginServicesBacklogGrowthPulls)
	}
	if s.NodeLoadRatio != 0 && (s.NodeLoadRatio <= 0 || math.IsNaN(s.NodeLoadRatio) || math.IsInf(s.NodeLoadRatio, 0)) {
		return fmt.Errorf("%s.node_load_ratio: %v must be a number > 0 (default %v)",
			prefix, s.NodeLoadRatio, DefaultPluginServicesNodeLoadRatio)
	}
	if s.SmallModelMaxParamsBillions != 0 && (s.SmallModelMaxParamsBillions <= 0 || math.IsNaN(s.SmallModelMaxParamsBillions) || math.IsInf(s.SmallModelMaxParamsBillions, 0)) {
		return fmt.Errorf("%s.small_model_max_params_billions: %v must be a number > 0 (default %v)",
			prefix, s.SmallModelMaxParamsBillions, DefaultPluginServicesSmallModelMaxParamsBillions)
	}
	if s.SmallModelContextTokens != 0 && s.SmallModelContextTokens < 1 {
		return fmt.Errorf("%s.small_model_context_tokens: %d must be a whole number >= 1 (tokens; default %d)",
			prefix, s.SmallModelContextTokens, DefaultPluginServicesSmallModelContextTokens)
	}
	names := make([]string, 0, len(s.PerService))
	for name := range s.PerService {
		names = append(names, name)
	}
	sort.Strings(names) // Deterministic: the first error names one service in a stable order.
	for _, name := range names {
		if err := ValidatePluginServiceMode(name, s.PerService[name].LifecycleMode); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
	}
	return nil
}

// ValidatePluginServiceMode reports whether mode is an accepted per_service
// lifecycle_mode — "" (unset) or one of ValidLifecycleModes, case-insensitive
// — and, when not, an error naming the service and listing the valid values.
// Shared by the load-time check and the REST validation so both surfaces
// reject with the same message.
func ValidatePluginServiceMode(service, mode string) error {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return nil
	}
	for _, v := range ValidLifecycleModes {
		if m == v {
			return nil
		}
	}
	return fmt.Errorf("per_service.%s.lifecycle_mode: %q is not a valid mode (valid values: %s)",
		service, strings.TrimSpace(mode), strings.Join(ValidLifecycleModes, ", "))
}

// Validate reports the first misconfigured field in the block — the Load /
// LoadSecure path (via validateLoaded) and the REST PUT path both call it
// before anything sticks, so a misconfiguration cannot land on disk and
// then break the next startup.
func (s PluginServicesConfig) Validate() error {
	return validatePluginServices(s, "plugins.services")
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
	Backend string            `yaml:"backend" json:"backend"`                   // base backend name (e.g. "claude-code")
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`       // env var overrides (ANTHROPIC_API_KEY, etc.)
	Binary  string            `yaml:"binary,omitempty" json:"binary,omitempty"` // override binary path
	Model   string            `yaml:"model,omitempty" json:"model,omitempty"`   // override model name
}

// RTKConfig configures the RTK (Rust Token Killer) integration for token savings.
type RTKConfig struct {
	Enabled             bool   `yaml:"enabled"`               // enable RTK integration
	Binary              string `yaml:"binary"`                // path to rtk binary (default: "rtk")
	ShowSavings         bool   `yaml:"show_savings"`          // display token savings in stats dashboard
	AutoInit            bool   `yaml:"auto_init"`             // run 'rtk init -g' if hooks not installed
	DiscoverInterval    int    `yaml:"discover_interval"`     // seconds between discover checks (0 = disabled)
	AutoUpdate          bool   `yaml:"auto_update"`           // auto-update RTK binary when new version available
	UpdateCheckInterval int    `yaml:"update_check_interval"` // seconds between version checks (default: 86400 = daily, 0 = disabled)
}

// ProjectConfigEntry (BL27) — registered project directory alias.
type ProjectConfigEntry struct {
	Dir            string `yaml:"dir" json:"dir"`
	DefaultBackend string `yaml:"default_backend,omitempty" json:"default_backend,omitempty"`
	Description    string `yaml:"description,omitempty" json:"description,omitempty"`
	// Policy is the per-project autonomous policy override block: set fields
	// apply to every Automaton of this project, sitting above the daemon's
	// autonomous.* defaults and below the Automaton's own override fields.
	// Nil (key absent) = a plain project with no per-project overrides
	// (see ProjectPolicy and ResolveProjectPolicy).
	Policy *ProjectPolicy `yaml:"policy,omitempty" json:"policy,omitempty"`

	// Project record fields (additive, all optional). Every field is
	// omitempty and pointer/slice-typed where zero is a meaningful value, so
	// an entry that sets none of them loads and saves unchanged. See
	// project_record.go for the types and Validate rules.
	Owner               string               `yaml:"owner,omitempty" json:"owner,omitempty"`
	Repository          string               `yaml:"repository,omitempty" json:"repository,omitempty"`
	DefaultBranch       string               `yaml:"default_branch,omitempty" json:"default_branch,omitempty"`
	Arcs                []ProjectArc         `yaml:"arcs,omitempty" json:"arcs,omitempty"`
	Target              *ProjectTarget       `yaml:"target,omitempty" json:"target,omitempty"`
	LaneDefaults        *ProjectLaneDefaults `yaml:"lane_defaults,omitempty" json:"lane_defaults,omitempty"`
	WeeklyShare         *int                 `yaml:"weekly_share,omitempty" json:"weekly_share,omitempty"`
	BranchRetentionDays *int                 `yaml:"branch_retention_days,omitempty" json:"branch_retention_days,omitempty"`
	MajorTrigger        string               `yaml:"major_trigger,omitempty" json:"major_trigger,omitempty"`
}

// SessionTemplateEntry (BL5) — reusable bundle of session start params.
// Empty fields fall through to the operator's defaults.
type SessionTemplateEntry struct {
	ProjectDir    string            `yaml:"project_dir,omitempty" json:"project_dir,omitempty"`
	Backend       string            `yaml:"backend,omitempty" json:"backend,omitempty"`
	Profile       string            `yaml:"profile,omitempty" json:"profile,omitempty"`
	Effort        string            `yaml:"effort,omitempty" json:"effort,omitempty"` // BL41
	Env           map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	AutoGitCommit *bool             `yaml:"auto_git_commit,omitempty" json:"auto_git_commit,omitempty"` // deprecated/ignored: task sessions are committed automatically; retained for API compatibility
	AutoGitInit   *bool             `yaml:"auto_git_init,omitempty" json:"auto_git_init,omitempty"`
	Description   string            `yaml:"description,omitempty" json:"description,omitempty"`
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
	Enabled bool `yaml:"enabled" json:"enabled"`
	// TestCommand is ONE shell string executed via `sh -c` — compound
	// commands are fine, e.g.
	// "go test ./... && node --test internal/server/web/*.test.js".
	TestCommand       string `yaml:"test_command" json:"test_command"`
	Timeout           int    `yaml:"timeout" json:"timeout"`
	BlockOnRegression bool   `yaml:"block_on_regression" json:"block_on_regression"`
	// Scope is the gate scope (full | touched-plus-smoke | touched); empty
	// inherits the built-in default (full). Validated on the load paths
	// by validateGateScope.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
	// SmokePackages are the package patterns run as the smoke set under
	// the touched-plus-smoke scope; when unset the built-in default list
	// (DefaultGateSmokePackages) is applied in applyDefaults.
	SmokePackages []string `yaml:"smoke_packages,omitempty" json:"smoke_packages,omitempty"`
}

// DefaultGateSmokePackages is the built-in smoke set — the proposed smoke
// set from docs/plans/2026-10-23-quality-gate-scope.md. It is the single
// source of truth for the default smoke list: applyDefaults fills it in
// when autonomous.default_quality_gates.smoke_packages is unset, and
// pipeline.DefaultGateSmokePackages is an alias to it. The list lives in
// the config package (not the pipeline package) because the pipeline
// package transitively imports config (via session), so the reverse
// dependency would be an import cycle.
var DefaultGateSmokePackages = []string{
	"./internal/autonomous/...",
	"./internal/config",
	"./internal/pipeline",
	"./internal/server/...",
	"./internal/mcp/...",
	"./internal/observer/...",
	"./internal/audit",
	"./internal/metrics",
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

	// ChunkSeconds is the longest audio slice, in seconds, handed to the
	// local whisper process in one go. Longer recordings are split and
	// transcribed slice by slice so memory stays flat. Default 120; 0
	// disables chunking. Valid values are 0 or >= 30 (whisper's own
	// window is 30 s). Used only by the `whisper` backend.
	ChunkSeconds int `yaml:"chunk_seconds" json:"chunk_seconds"`

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

// VisionConfig configures the optional image-description subsystem (BL368).
// Supported backends: "ollama" (default) uses /api/generate with the images
// field; "openai" / "openai_compat" uses /v1/chat/completions with an
// image_url content part — covers OpenAI gpt-4o, Ollama openai-compat,
// LM Studio, and any other OpenAI-compatible server.
type VisionConfig struct {
	// Enabled controls whether image attachment description is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Backend selects the API format. "ollama" (default) or "openai" / "openai_compat".
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`

	// Endpoint is the base URL of the vision model server (required).
	// Ollama example: "http://localhost:11434"
	// OpenAI example: "https://api.openai.com"
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`

	// APIKey is the bearer credential. Optional for self-hosted; required for OpenAI.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`

	// Model is the vision model name (required). Examples: "llava", "moondream",
	// "gpt-4o", "llava-phi3".
	Model string `yaml:"model" json:"model"`

	// DefaultPrompt overrides the built-in prompt ("Describe this image concisely.")
	// when non-empty. Applied when callers pass an empty prompt.
	DefaultPrompt string `yaml:"default_prompt,omitempty" json:"default_prompt,omitempty"`

	// MaxImageBytes is the maximum allowed image size before rejection.
	// 0 defaults to 10 MB.
	MaxImageBytes int64 `yaml:"max_image_bytes,omitempty" json:"max_image_bytes,omitempty"`

	// DescribeTimeoutSeconds bounds a single image description request.
	// 0 or negative = default of 5 seconds (see EffectiveDescribeTimeout).
	DescribeTimeoutSeconds int `yaml:"describe_timeout_seconds,omitempty" json:"describe_timeout_seconds,omitempty"`
}

// DefaultVisionDescribeTimeout is the describe-request timeout applied when
// VisionConfig.DescribeTimeoutSeconds is unset (0) or negative.
const DefaultVisionDescribeTimeout = 5 * time.Second

// EffectiveDescribeTimeout returns the configured describe-request timeout:
// DescribeTimeoutSeconds as a duration when it is >0, otherwise the default
// of 5 seconds.
func (v VisionConfig) EffectiveDescribeTimeout() time.Duration {
	if v.DescribeTimeoutSeconds > 0 {
		return time.Duration(v.DescribeTimeoutSeconds) * time.Second
	}
	return DefaultVisionDescribeTimeout
}

// DefaultScheduleHistoryLimit is the per-schedule result history size
// used when results.schedule_history_limit is unset.
const DefaultScheduleHistoryLimit = 20

// ResultsConfig controls the per-schedule result history of scheduled
// jobs: every scheduled run posts a result payload, and each schedule
// name retains the most recent `schedule_history_limit` of them
// (oldest evicted first). The store is persisted under the data dir
// and survives daemon restarts, independently of the spawning session.
type ResultsConfig struct {
	// ScheduleHistoryLimit is the number of latest result entries
	// kept per schedule name. Unset (0) = 20; the effective value is
	// clamped so it never drops below 1.
	ScheduleHistoryLimit int `yaml:"schedule_history_limit,omitempty" json:"schedule_history_limit,omitempty"`
}

// EffectiveScheduleHistoryLimit returns the limit after defaulting and
// clamping: unset/zero/negative → 20, otherwise the configured value
// (minimum applied value is 1).
func (c ResultsConfig) EffectiveScheduleHistoryLimit() int {
	if c.ScheduleHistoryLimit < 1 {
		return DefaultScheduleHistoryLimit
	}
	return c.ScheduleHistoryLimit
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	// DefaultLoopDetectTurns is stored explicitly (not left nil) so new
	// config files written by this binary pin the loop-detection window; a
	// missing key in an existing file still resolves to the same default
	// via LoopDetectTurnsEffective.
	loopDetectTurnsDefault := DefaultLoopDetectTurns
	return &Config{
		Hostname:        hostname,
		Secrets:         SecretsConfig{Vault: VaultConfig{UnreachableAlertAfterSeconds: intPtr(DefaultVaultUnreachableAlertAfterSeconds)}},
		DataDir:         filepath.Join(home, ".datawatch"),
		PinCheckSeconds: 300,
		Observer: ObserverConfig{
			Peers: ObserverPeersConfig{SilentAlertMinutes: 5},
			Pull:  DefaultObserverPullConfig(),
		},
		Signal: SignalConfig{
			ConfigDir:  filepath.Join(home, ".local", "share", "signal-cli"),
			DeviceName: hostname,
		},
		Session: SessionConfig{
			MaxSessions:         10,
			InputIdleTimeout:    10,
			TailLines:           20,
			AlertContextLines:   10,
			LLMBackend:          "claude-code",
			DefaultProjectDir:   home,
			AutoGitCommit:       true,
			AutoGitInit:         false,
			GitDefault:          false,
			ClaudeEnabled:       true,
			MCPMaxRetries:       5,
			ScheduleSettleMs:    200,
			StaleTimeoutSeconds: 1800,
			// BL219 — tooling lifecycle defaults.
			GitignoreCheckOnStart: true,
			GitignoreArtifacts:    []string{"aider", "goose", "gemini"},
		},
		Git: GitConfig{
			PushOnComplete: GitPushBranch,
			PushRemote:     "origin",
			SecretScan: GitSecretScanConfig{
				Mode:      GitSecretScanShadow,
				Detectors: GitSecretScanDefaultDetectors,
			},
			ReleaseCIWindowMinutes: 60,
		},
		Server: ServerConfig{
			Enabled:              true,
			Host:                 "0.0.0.0",
			Port:                 8080,
			TLSPort:              8443,
			TLSAutoGenerate:      true,
			RecentSessionMinutes: 5,
			// BL432 — compact session-list view defaults.
			SessionSummaryTerminalLimit: 50,
			SessionSummaryTextChars:     200,
			WSSessionListDefault:        "full",
			WSOutputScopeDefault:        "all",
			// BL432 / GH#242 — /api/* gzip defaults.
			APIGzip:              true,
			APIGzipMinBytes:      1024,
			SuppressActiveToasts: true,
			ProxySandboxPort:     8444,
		},
		MCP: MCPConfig{
			Enabled:         true,
			SSEHost:         "127.0.0.1",
			SSEPort:         8081,
			TLSAutoGenerate: true,
			Resources:       MCPResourcesConfig{Enabled: true},
			Sampling:        MCPSamplingConfig{Enabled: true},
			Elicitation:     MCPElicitationConfig{Enabled: true},
			Prompts:         MCPPromptsConfig{Enabled: true},
		},
		Acme: AcmeConfig{
			Endpoint:    "staging",
			Method:      "http01",
			RenewalDays: 30,
			Retry:       AcmeRetryConfig{IntervalMinutes: 30, MaxConsecutiveFailures: 5},
			Apply:       AcmeApplyConfig{UpdateMCPCert: true},
		},
		// Backend addresses and binaries are intentionally empty in the default
		// config so fresh installs don't trigger spurious v6→v7 migration toasts.
		// Operators configure them explicitly via `datawatch setup llm <name>`.
		OpenCode: OpenCodeConfig{
			DefaultModel: "opencode/big-pickle",
			// B89 — 20 min / 15 min vs. opencode's built-in 5 min defaults;
			// sized for observed long-"thinking" pauses and cold-load times
			// on large locally-hosted models. Operator-overridable (0 = use
			// opencode's own default).
			OllamaChunkTimeoutSec:  1200,
			OllamaHeaderTimeoutSec: 900,
		},
		Usage:          UsageConfig{Calculator: DefaultUsageCalculator(), BurnGuard: DefaultBurnGuard()},
		ResourceLimits: DefaultResourceLimits(),
		Autonomous: AutonomousConfig{
			ReleasePath:                              "direct_push",
			ScriptGateTimeoutSeconds:                 DefaultScriptGateTimeoutSeconds,
			AutoFixRetries:                           1,
			MaxConcurrentTasks:                       1,
			UsageBackoff:                             UsageBackoffPauseBackend,
			BackoffMaxWaitMinutes:                    BackoffMaxWaitDefaultMinutes,
			RoleScanIntervalSeconds:                  RoleScanIntervalDefaultSeconds,
			ResumeMaxAgeSeconds:                      3600,
			FirstAttemptEscalateAfterFailures:        DefaultFirstAttemptEscalateAfterFailures,
			RetryResumeMaxContextTokens:              DefaultRetryResumeMaxContextTokens,
			MaxEnginesPerNode:                        1,
			JudgeSizeThresholdLines:                  JudgeSizeThresholdDefaultLines,
			JudgePromptFileThresholdLines:            JudgePromptFileThresholdDefaultLines,
			JudgeDeliveryWaitPerThousandLinesSeconds: 60,
			VerifierMaxRunSeconds:                    VerifierRunCapDefaultSeconds,
			GateDisagreement:                         autonomousGateDisagreementWarn,
			FailureEscalation:                        DefaultFailureEscalationConfig(),
			// Starting plan-tier table — operator policy, see the field
			// doc on PlanTierMaxInflight. api=0 = unlimited; the dollar
			// budget is a later phase.
			PlanTierMaxInflight: map[string]int{
				"pro":     1,
				"max_5x":  2,
				"max_20x": 4,
				"api":     0,
			},
			// Nothing declared until the operator sets a notional figure.
			PlanTierAllocationUSD: map[string]float64{},
			// Shipped priority-by-kind table — operator policy, see the field
			// doc on PriorityByKind. defect_intake_automation stays empty on
			// purpose: each installation chooses its own.
			PriorityByKind:      DefaultPriorityByKindMap(),
			LoopDetectTurns:     &loopDetectTurnsDefault,
			StaleTaskSeconds:    900,
			StuckRunningSeconds: StuckRunningDefaultSeconds,
			// BL410 Phase 1 — see AutonomousConfig.RetryPolicy's doc
			// comment for why "content"/"policy" are absent here.
			RetryPolicy:               map[string]int{"infra": 4, "scope": 0},
			RetryCeilingTotal:         5,
			InfraRetryEscalateAfter:   2,
			CIWatchTimeoutSeconds:     1500,
			DetailTextCapBytes:        4096,
			InheritedTagsCacheSeconds: 15,
			ContentAutoFixEnabled:     true,
			AutoFixSkipApproval:       true,
			// The never-stage list ships populated (scratch, temp and local
			// data folders). A nil list is saved as an empty list (the field
			// has no omitempty), and a reload reads an empty list as "never-
			// stage rules disabled" — only an operator's explicit empty list
			// may mean none, while an omitted key keeps this default.
			SnapshotNeverStage: append([]string(nil), DefaultSnapshotNeverStage...),
			BlockOnFileDrift:   false,   // opt-in strict drift enforcement
			DriftMode:          "block", // pre-existing drift blocks by default; "warn" for flag-only
			UndeclaredEditMode: "warn",
			ScopeLint:          ScopeLintEnforce,
			Automation: AutonomousAutomationConfig{
				AgentInsertPolicy:  AgentInsertPolicyAllow,
				TriageAlertMinutes: intPtr(DefaultAutomationTriageAlertMinutes), CreateSearchMode: CreateSearchModeShadow, CreateSearchThreshold: float64Ptr(DefaultAutomationCreateSearchThreshold), MergePendingMode: MergePendingModeOff, MergePendingAlertMinutes: intPtr(DefaultAutomationMergePendingAlertMinutes), MergedMemberPolicy: MergedMemberPolicyCompleted,
				// The repo CI watch's security workflow list ships populated.
				// A nil list is saved as an empty list (the field has no
				// omitempty), and a reload reads an empty list as "watch no
				// security workflows": the security failure pass then goes
				// silently off in every mode. Only an operator's explicit
				// empty list may mean none.
				RepoCIWatch: RepoCIWatchConfig{
					SecurityWorkflows: append([]string(nil), DefaultRepoCIWatchSecurityWorkflows...),
				},
			},
			LaneFill:      DefaultAutonomousLaneFill(),
			DecomposeLint: DefaultDecomposeLint(),
			TaskTemplates: DefaultTaskTemplates(),
			Requirements:  DefaultAutonomousRequirements(),
			Progress:      AutonomousProgressConfig{BacklogPath: DefaultProgressBacklogPath},
			// Notify — global baseline for the notify event kinds
			// (autonomous.notify.default_events). Written as literals,
			// not the internal/autonomous constants: the config package
			// must not import that one (the daemon bridges it). A YAML
			// file that omits the key keeps these five (config.Load merges
			// over this value); an explicit `default_events: []` is the
			// operator opt-out and loads as empty, non-nil — the global
			// tier then resolves to no kinds.
			Tags: TagsConfig{
				DefaultView:         TagDefaultViewAuto,
				AttentionStatuses:   append([]string(nil), DefaultTagAttentionStatuses...),
				DefaultMatchMode:    TagMatchAny,
				RegistrySyncSeconds: TagRegistrySyncDefaultSeconds,
				Placement:           DefaultPlacementConfig(),
			},
			Notify: AutonomousNotifyConfig{
				DefaultEvents: []string{
					"prd.needs_review",
					"prd.blocked",
					"prd.failed",
					"prd.completed",
					"prd.cancelled",
				},
			},
		},
		Ntfy:          NtfyConfig{ServerURL: "https://ntfy.sh"},
		Email:         EmailConfig{Port: 587},
		ImapMcp:       ImapMcpConfig{SubjectPrefix: "datawatch"},
		Results:       ResultsConfig{ScheduleHistoryLimit: DefaultScheduleHistoryLimit},
		GitHubWebhook: GitHubWebhookConfig{Addr: "127.0.0.1:9001"},
		Webhook:       WebhookConfig{Addr: "127.0.0.1:9002"},
		Twilio:        TwilioConfig{WebhookAddr: "127.0.0.1:9003"},
		Whisper: WhisperConfig{
			Model:        "base",
			Language:     "en",
			VenvPath:     ".venv",
			ChunkSeconds: 120,
		},
		LSP: LSPConfig{
			Servers: map[string]LSPServerConfig{
				"go": {
					Command:    []string{"gopls"},
					Extensions: []string{".go"},
				},
				"typescript": {
					Command:    []string{"typescript-language-server", "--stdio"},
					Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
				},
				"python": {
					Command:    []string{"pyright-langserver", "--stdio"},
					Extensions: []string{".py"},
				},
				"rust": {
					Command:    []string{"rust-analyzer"},
					Extensions: []string{".rs"},
				},
				"cpp": {
					Command:    []string{"clangd"},
					Extensions: []string{".cpp", ".cc", ".c", ".h", ".hpp"},
				},
			},
		},
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
		// RateLimitPatterns is intentionally left unset by the built-in
		// default. When no operator list is configured (here or per-LLM),
		// the session Manager falls back to its per-backend anchored table
		// (internal/session/rate_limit_lines.go), keyed by BackendFamily;
		// an operator-configured rate_limit_patterns keeps the classic
		// flat-substring behavior.
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
	if llmDet.AlertSettle > 0 {
		base.AlertSettle = llmDet.AlertSettle
	}
	if llmDet.AlertRepeat > 0 {
		base.AlertRepeat = llmDet.AlertRepeat
	}
	if base.AlertSettle == 0 {
		base.AlertSettle = 45
	}
	if base.AlertRepeat == 0 {
		base.AlertRepeat = 300
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
	if cfg.Session.StaleTimeoutSeconds == 0 {
		cfg.Session.StaleTimeoutSeconds = 1800
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
	if cfg.MCP.SSEBindTimeout <= 0 {
		cfg.MCP.SSEBindTimeout = 120 * time.Second
	}
	// BL412 option C followup (docs/plans/
	// 2026-10-29-bl412-self-config-gate-proposal.md) — the
	// allow_self_config default flip, isolated here as a migration so it
	// is testable in one place: allow_self_config is now the alias for
	// the builtin "self-config" capability group and defaults to
	// ENABLED. An explicit value (true OR false in the YAML) is
	// preserved as-written — this block only fills nil → true, so an
	// operator's explicit false never silently flips. Idempotent: once
	// filled, a second application is a no-op and logs nothing.
	if cfg.MCP.AllowSelfConfig == nil {
		cfg.MCP.AllowSelfConfig = BoolPtr(true)
		selfConfigMigrationLogf(
			"mcp.allow_self_config: key absent — applying new default (enabled), the alias for the builtin \"self-config\" capability group; set allow_self_config: false to disable")
	}
	// compute_placement: every unset key reads its default once. The 0 of
	// assume_model_size_gb means the size is unknown (the default), so it is
	// deliberately left untouched — an explicit 0 survives the round trip.
	if cfg.ComputePlacement.MissingModel == "" {
		cfg.ComputePlacement.MissingModel = ComputePlacementDefaultMissingModel
	}
	if cfg.ComputePlacement.MemHeadroomPct == 0 {
		cfg.ComputePlacement.MemHeadroomPct = ComputePlacementDefaultMemHeadroomPct
	}
	if cfg.ComputePlacement.DiskHeadroomGB == 0 {
		cfg.ComputePlacement.DiskHeadroomGB = ComputePlacementDefaultDiskHeadroomGB
	}
	if cfg.ComputePlacement.AskTimeoutMinutes == 0 {
		cfg.ComputePlacement.AskTimeoutMinutes = ComputePlacementDefaultAskTimeoutMinutes
	}
	if cfg.Session.LogLevel == "" {
		cfg.Session.LogLevel = "info"
	}
	if cfg.Session.RootPath == "" {
		if wd, err := os.Getwd(); err == nil {
			cfg.Session.RootPath = wd
		}
	}
	// BL391 — migrate legacy single-provider web_search config into a
	// Providers[0] entry. Runs before the BL372 defaults below so a legacy
	// config's Provider/Engine/NumResults are captured before being
	// defaulted; a config that already has a Providers list is left alone.
	if len(cfg.WebSearch.Providers) == 0 && (cfg.WebSearch.URL != "" || cfg.WebSearch.Provider != "") {
		legacyType := cfg.WebSearch.Provider
		if legacyType == "" {
			legacyType = "searxng"
		}
		cfg.WebSearch.Providers = []SearchProvider{{
			Name:       "default",
			Type:       legacyType,
			Enabled:    cfg.WebSearch.Enabled,
			URL:        cfg.WebSearch.URL,
			Engine:     cfg.WebSearch.Engine,
			NumResults: cfg.WebSearch.NumResults,
		}}
	}
	if !cfg.WebSearch.CacheEnabled && cfg.WebSearch.CacheTTLSeconds == 0 {
		cfg.WebSearch.CacheEnabled = true
	}
	if cfg.WebSearch.CacheTTLSeconds == 0 {
		cfg.WebSearch.CacheTTLSeconds = 900
	}
	for i := range cfg.WebSearch.Providers {
		p := &cfg.WebSearch.Providers[i]
		if p.Type == "searxng" && p.Engine == "" {
			p.Engine = "bing"
		}
		if p.NumResults == 0 {
			p.NumResults = 10
		}
	}
	// BL372 — legacy web search defaults (kept for the deprecated flat
	// fields themselves; superseded by per-provider fields above).
	if cfg.WebSearch.Provider == "" {
		cfg.WebSearch.Provider = "searxng"
	}
	if cfg.WebSearch.Engine == "" {
		cfg.WebSearch.Engine = "bing"
	}
	if cfg.WebSearch.NumResults == 0 {
		cfg.WebSearch.NumResults = 10
	}
	// BL304 — migrate legacy decomposition_* YAML keys → planning_* Go fields.
	// Old configs that set decomposition_backend/effort/model still work.
	if cfg.Autonomous.PlanningBackend == "" && cfg.Autonomous.DecompositionBackendLegacy != "" {
		cfg.Autonomous.PlanningBackend = cfg.Autonomous.DecompositionBackendLegacy
	}
	if cfg.Autonomous.PlanningEffort == "" && cfg.Autonomous.DecompositionEffortLegacy != "" {
		cfg.Autonomous.PlanningEffort = cfg.Autonomous.DecompositionEffortLegacy
	}
	if cfg.Autonomous.PlanningModel == "" && cfg.Autonomous.DecompositionModelLegacy != "" {
		cfg.Autonomous.PlanningModel = cfg.Autonomous.DecompositionModelLegacy
	}
	if cfg.Autonomous.MaxConcurrentTasks <= 0 {
		cfg.Autonomous.MaxConcurrentTasks = 1
	}
	cfg.Autonomous.UsageBackoff = normalizeUsageBackoff(cfg.Autonomous.UsageBackoff)
	cfg.Autonomous.BackoffMaxWaitMinutes = normalizeBackoffMaxWaitMinutes(cfg.Autonomous.BackoffMaxWaitMinutes)
	cfg.Usage.Calculator = normalizeUsageCalculator(cfg.Usage.Calculator, usageCalculatorLogf)
	cfg.Usage.BurnGuard = normalizeBurnGuard(cfg.Usage.BurnGuard, burnGuardLogf)
	// An unset planning_restart_policy stays unset; an invalid one is warned
	// about at startup and read as the default (see planning_restart_policy.go).
	if cfg.Autonomous.PlanningRestartPolicy != "" {
		cfg.Autonomous.PlanningRestartPolicy = normalizePlanningRestartPolicyLoaded(cfg.Autonomous.PlanningRestartPolicy, planningRestartPolicyLogf)
	}
	if cfg.Autonomous.RoleScanIntervalSeconds < 1 {
		cfg.Autonomous.RoleScanIntervalSeconds = RoleScanIntervalDefaultSeconds
	}
	if cfg.Autonomous.MaxEnginesPerNode < 1 {
		cfg.Autonomous.MaxEnginesPerNode = 1
	}
	// gate_disagreement defaults to "warn"; an unrecognized value is left
	// here and rejected by validateGateDisagreement on the load paths, or
	// treated as "warn" by the runtime readers.
	if cfg.Autonomous.GateDisagreement == "" {
		cfg.Autonomous.GateDisagreement = autonomousGateDisagreementWarn
	}
	// failure_escalation: an unset max_failures reads as 3. Negative values
	// are rejected by FailureEscalationConfig.Validate on the load paths.
	if cfg.Autonomous.FailureEscalation.MaxFailures == 0 {
		cfg.Autonomous.FailureEscalation.MaxFailures = FailureEscalationDefaultMaxFailures
	}
	// BL428 §3a — autonomous.project_write_serialization defaults to
	// "advisory"; "parallel" is reserved-but-disabled (recognized,
	// still enforced advisory, startup warning). validateLoaded
	// rejects any other value.
	if cfg.Autonomous.ProjectWriteSerialization == "" {
		cfg.Autonomous.ProjectWriteSerialization = autonomousProjectWriteSerializationAdvisory
	}
	// Gate scope: an empty scope deliberately inherits the built-in
	// default (full) and is not filled in here; an unrecognized value is
	// rejected by validateGateScope on the load paths. A non-empty valid
	// value is kept as written.
	// Gate smoke list: the built-in default list applies when the operator
	// has not set smoke_packages, so the global default is never empty.
	if len(cfg.Autonomous.DefaultQualityGates.SmokePackages) == 0 {
		cfg.Autonomous.DefaultQualityGates.SmokePackages = append([]string(nil), DefaultGateSmokePackages...)
	}
	if cfg.Autonomous.Automation.AgentInsertPolicy == "" {
		cfg.Autonomous.Automation.AgentInsertPolicy = AgentInsertPolicyAllow
	}
	if strings.TrimSpace(cfg.Autonomous.Progress.BacklogPath) == "" {
		cfg.Autonomous.Progress.BacklogPath = DefaultProgressBacklogPath
	}
	// BL397 — native ACME subsystem defaults.
	if cfg.Acme.Endpoint == "" {
		cfg.Acme.Endpoint = "staging"
	}
	if cfg.Acme.Method == "" {
		cfg.Acme.Method = "http01"
	}
	if cfg.Acme.RenewalDays == 0 {
		cfg.Acme.RenewalDays = 30
	}
	if cfg.Acme.Retry.IntervalMinutes == 0 {
		cfg.Acme.Retry.IntervalMinutes = 30
	}
	if cfg.Acme.Retry.MaxConsecutiveFailures == 0 {
		cfg.Acme.Retry.MaxConsecutiveFailures = 5
	}
	// Apply.UpdateMCPCert's true-default follows the same convention as
	// Server.TLSAutoGenerate: set only in DefaultConfig() for brand-new
	// configs, not forced here for loaded ones (an omitted bool in an
	// existing config.yaml is indistinguishable from an explicit false).
}

// Autonomous project-write serialization values (BL428 §3a —
// docs/plans/2026-10-14-concurrency-and-budget-design.md).
const (
	autonomousProjectWriteSerializationAdvisory = "advisory"
	// autonomousProjectWriteSerializationParallel is reserved but
	// DISABLED: recognized at load time, still advisory in practice.
	autonomousProjectWriteSerializationParallel = "parallel"
)

// gate_disagreement policy values (verifier pass vs. quality-gate
// disagreement; see AutonomousConfig.GateDisagreement).
const (
	autonomousGateDisagreementWarn  = "warn"
	autonomousGateDisagreementBlock = "block"
)

// validateLoaded rejects misconfigured values that applyDefaults left in
// place. Run after applyDefaults in Load and LoadSecure.
func validateLoaded(cfg *Config) error {
	if n := cfg.Secrets.Vault.UnreachableAlertAfterSeconds; n != nil && *n < 0 {
		return fmt.Errorf("secrets.vault.unreachable_alert_after_seconds: %d must be >= 0 (0 = off, default %d)", *n, DefaultVaultUnreachableAlertAfterSeconds)
	}
	if err := cfg.ComputePlacement.Validate(); err != nil {
		return err
	}
	for k, v := range map[string]int{
		"capacity_rate_requests":       cfg.Autonomous.CapacityRateRequests,
		"capacity_rate_tokens":         cfg.Autonomous.CapacityRateTokens,
		"capacity_rate_window_seconds": cfg.Autonomous.CapacityRateWindowSeconds,
	} {
		if v < 0 {
			return fmt.Errorf("autonomous.%s: %d must be >= 0", k, v)
		}
	}
	if cfg.Autonomous.CapacityAdoptionWaitSeconds < 0 {
		return fmt.Errorf("autonomous.capacity_adoption_wait_seconds: %d must be >= 0", cfg.Autonomous.CapacityAdoptionWaitSeconds)
	}
	if cfg.Autonomous.VerifierMinFreeMemMB != nil && *cfg.Autonomous.VerifierMinFreeMemMB < 0 {
		return fmt.Errorf("autonomous.verifier_min_free_mem_mb: %d must be >= 0 (0 turns the check off)", *cfg.Autonomous.VerifierMinFreeMemMB)
	}
	if cfg.Autonomous.VerifierHeadroomMaxAgeHours < 0 {
		return fmt.Errorf("autonomous.verifier_headroom_max_age_hours: %d must be >= 0 (0 or unset = default %d hours)", cfg.Autonomous.VerifierHeadroomMaxAgeHours, VerifierHeadroomMaxAgeHoursDefault)
	}
	if cfg.Autonomous.JudgeSizeThresholdLines < 0 {
		return fmt.Errorf("autonomous.judge_size_threshold_lines: %d must be >= 0", cfg.Autonomous.JudgeSizeThresholdLines)
	}
	for _, n := range cfg.ComputeNodes {
		if n.SessionSlots < 0 {
			return fmt.Errorf("compute_nodes[%s].session_slots: %d must be >= 0", n.Name, n.SessionSlots)
		}
		if n.VerifierSlots != nil && *n.VerifierSlots < 0 {
			return fmt.Errorf("compute_nodes[%s].verifier_slots: %d must be >= 0", n.Name, *n.VerifierSlots)
		}
		if n.VerifierMinFreeMemMB != nil && *n.VerifierMinFreeMemMB < 0 {
			return fmt.Errorf("compute_nodes[%s].verifier_min_free_mem_mb: %d must be >= 0", n.Name, *n.VerifierMinFreeMemMB)
		}
		if n.VerifierContextMB < 0 {
			return fmt.Errorf("compute_nodes[%s].verifier_context_mb: %d must be >= 0", n.Name, n.VerifierContextMB)
		}
	}
	switch cfg.Autonomous.ProjectWriteSerialization {
	case autonomousProjectWriteSerializationAdvisory, autonomousProjectWriteSerializationParallel:
		// ok
	default:
		return fmt.Errorf("autonomous.project_write_serialization: invalid value %q (valid values: %q, %q)",
			cfg.Autonomous.ProjectWriteSerialization,
			autonomousProjectWriteSerializationAdvisory, autonomousProjectWriteSerializationParallel)
	}
	if err := ValidateTracing(cfg.Tracing); err != nil {
		return err
	}
	if err := ValidateMergeConflictPolicy(cfg.Autonomous.Parallel.MergeConflictPolicy); err != nil {
		return err
	}
	if err := ValidateMergeRetrySeconds(cfg.Autonomous.Parallel.MergeRetrySeconds); err != nil {
		return err
	}
	// A fully-zero observer.pull block means "unset" (a zero-value Config
	// or an empty block) and keeps the shipped defaults; an explicit value
	// in the block is what ValidateObserverPullConfig checks.
	if p := cfg.Observer.Pull; p != (ObserverPullConfig{}) {
		if err := ValidateObserverPullConfig(p); err != nil {
			return err
		}
	}
	if err := ValidateAgentInsertPolicy(cfg.Autonomous.Automation.AgentInsertPolicy); err != nil {
		return err
	}
	if err := validateGateScope(cfg.Autonomous.DefaultQualityGates.Scope); err != nil {
		return err
	}
	if t := cfg.Autonomous.Automation.EffectiveTriageAlertMinutes(); t < 0 {
		return fmt.Errorf("autonomous.automation.triage_alert_minutes: %d must be a whole number >= 0 (0 = off, default %d)",
			t, DefaultAutomationTriageAlertMinutes)
	}
	if err := ValidateCreateSearchMode(cfg.Autonomous.Automation.EffectiveCreateSearchMode()); err != nil {
		return err
	}
	if err := ValidateCreateSearchThreshold(cfg.Autonomous.Automation.EffectiveCreateSearchThreshold()); err != nil {
		return err
	}
	if err := ValidateHostLoadAlertMode(cfg.Autonomous.HostLoadAlert.Mode); err != nil {
		return err
	}
	if err := ValidateHostLoadAlertMultiple(cfg.Autonomous.HostLoadAlert.Multiple); err != nil {
		return err
	}
	if err := ValidateHostLoadAlertMinutes(cfg.Autonomous.HostLoadAlert.Minutes); err != nil {
		return err
	}
	if err := ValidateMergePendingMode(cfg.Autonomous.Automation.MergePendingMode); err != nil {
		return err
	}
	if err := ValidateMergedMemberPolicy(cfg.Autonomous.Automation.MergedMemberPolicy); err != nil {
		return err
	}
	if t := cfg.Autonomous.Automation.EffectiveMergePendingAlertMinutes(); t < 0 {
		return fmt.Errorf("autonomous.automation.merge_pending_alert_minutes: %d must be a whole number >= 0 (0 = off, default %d)",
			t, DefaultAutomationMergePendingAlertMinutes)
	}
	if err := validateRepoCIWatch(cfg.Autonomous.Automation.RepoCIWatch); err != nil {
		return err
	}
	if err := cfg.Autonomous.Automation.DependencyUpdate.Validate(); err != nil {
		return err
	}
	if err := validatePluginServices(cfg.Plugins.Services, "plugins.services"); err != nil {
		return err
	}
	if err := ValidateReleasePath(cfg.Autonomous.ReleasePath); err != nil {
		return err
	}
	if err := ValidateReleaseCIWindowMinutes(cfg.Git.ReleaseCIWindowMinutes); err != nil {
		return err
	}
	if err := ValidateSnapshotNeverStage(cfg.Autonomous.SnapshotNeverStage); err != nil {
		return err
	}
	if err := validateSecurityFailure(cfg.Autonomous.Automation.SecurityFailure); err != nil {
		return err
	}
	if err := validateSupervisorWatch(cfg.Autonomous.Automation.SupervisorWatch); err != nil {
		return err
	}
	if err := ValidateModelPullConsent(cfg.Autonomous.ModelPullConsent); err != nil {
		return err
	}
	if err := cfg.Autonomous.LaneFill.validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.Checks.Validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.Ship.Validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.DecomposeLint.Validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.TaskTemplates.Validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.Progress.validate(); err != nil {
		return err
	}
	if err := validateScriptGateTimeout(cfg.Autonomous.ScriptGateTimeoutSeconds); err != nil {
		return err
	}
	if err := ValidateProjectGateCommand("autonomous", cfg.Autonomous.ProjectGateCommand); err != nil {
		return err
	}
	if err := cfg.Autonomous.Questions.validate(); err != nil {
		return err
	}
	if err := cfg.Session.validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.ModelRouting.Validate(); err != nil {
		return err
	}
	if err := cfg.UI.validate(); err != nil {
		return err
	}
	if err := cfg.Autonomous.Decisions.validate(); err != nil {
		return err
	}
	if err := cfg.Identity.validate(); err != nil {
		return err
	}
	// 2026-10-17 plan-review gate: mode must be empty/off/warn/block.
	switch cfg.Autonomous.PlanReviewMode {
	case "", "off", "warn", "block":
		// ok
	default:
		return fmt.Errorf("autonomous.plan_review_mode: invalid value %q (valid: empty, %q, %q, %q)",
			cfg.Autonomous.PlanReviewMode, "off", "warn", "block")
	}
	// plan_tier_max_inflight: every key must be a known tier and every
	// value a non-negative cap (0 = unlimited).
	for tier, cap := range cfg.Autonomous.PlanTierMaxInflight {
		if !inference.ValidPlanTier(inference.PlanTier(tier)) {
			return fmt.Errorf("autonomous.plan_tier_max_inflight: unknown tier %q (allowed: pro, max_5x, max_20x, api)", tier)
		}
		if cap < 0 {
			return fmt.Errorf("autonomous.plan_tier_max_inflight: tier %q cap %d must be >= 0", tier, cap)
		}
	}
	// plan_tier_allocation_usd: every key must be a known tier and every
	// value a finite, non-negative dollar figure (0 = not declared).
	for tier, usd := range cfg.Autonomous.PlanTierAllocationUSD {
		if !inference.ValidPlanTier(inference.PlanTier(tier)) {
			return fmt.Errorf("autonomous.plan_tier_allocation_usd: unknown tier %q (allowed: pro, max_5x, max_20x, api)", tier)
		}
		if usd < 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
			return fmt.Errorf("autonomous.plan_tier_allocation_usd: tier %q value %v must be a finite number >= 0", tier, usd)
		}
	}
	// usage.billing: per-provider billing profiles (billing_profile.go) —
	// unknown mode, negative or non-finite monthly_price, a bad reset
	// weekday or time, an unknown allowance unit and an empty provider key
	// are all refused at validation.
	if err := validateBillingProfiles(cfg); err != nil {
		return err
	}
	if err := cfg.Autonomous.VerifierPolicy.Validate(); err != nil {
		return fmt.Errorf("autonomous.%w", err)
	}
	if err := cfg.Autonomous.RequestWatch.Validate(); err != nil {
		return err
	}
	if err := ValidateWorkerProfileConfig(cfg); err != nil {
		return err
	}
	// The merge pipeline block (conflict policy, release approval modes and
	// the rest) is checked on load too, so a bad value in config.yaml is
	// refused the same way a bad PUT /api/config value is. Its messages
	// already name the full autonomous.merge_pipeline.* key.
	if err := cfg.Autonomous.MergePipeline.Validate(); err != nil {
		return err
	}
	if err := cfg.ProjectsDefaults.Validate(); err != nil {
		return err
	}
	return nil
}

// detectDeprecatedKeys reports the deprecated keys the just-parsed YAML
// set. It must run after yaml.Unmarshal and before applyDefaults so a
// default can never be mistaken for an operator-set value.
func detectDeprecatedKeys(cfg *Config) []string {
	var keys []string
	if cfg.Autonomous.MaxParallelTasks != 0 {
		keys = append(keys, "autonomous.max_parallel_tasks")
	}
	return keys
}

// validateGateDisagreement rejects an unrecognized
// autonomous.gate_disagreement value; run from the config load paths so
// anything but "warn"/"block" (or unset) is rejected. Runtime readers
// treat an unrecognized value as "warn" so an existing config can never
// wedge the daemon.
func validateGateDisagreement(v string) error {
	switch v {
	case autonomousGateDisagreementWarn, autonomousGateDisagreementBlock, "":
		return nil
	}
	return fmt.Errorf("autonomous.gate_disagreement: invalid value %q (valid values: %q, %q)",
		v, autonomousGateDisagreementWarn, autonomousGateDisagreementBlock)
}

// validateGateScope rejects an unrecognized
// autonomous.default_quality_gates.scope value; run from the config load
// paths (via validateLoaded) so anything but unset or the three valid
// values is rejected. An empty value means "inherit the built-in default
// (full)" and is valid.
func validateGateScope(v string) error {
	switch v {
	case "", "full", "touched-plus-smoke", "touched":
		return nil
	}
	return fmt.Errorf("autonomous.default_quality_gates.scope: invalid value %q (valid values: empty (inherit), \"full\", \"touched-plus-smoke\", \"touched\")", v)
}

// WorkerProfileDefaultsConfig maps a worker role or a task kind to the name of
// the worker profile it starts with when no profile is named at a more
// specific layer. Keys and values are validated by ValidateWorkerProfileConfig.
type WorkerProfileDefaultsConfig struct {
	// Role maps planner, worker, verifier or judge to a profile name.
	Role map[string]string `yaml:"role" json:"role"`
	// Kind maps implementation, docs, note or final_gate to a profile name.
	Kind map[string]string `yaml:"kind" json:"kind"`
}

// workerProfileRoleKeys lists the role keys worker_profile_defaults.role accepts.
var workerProfileRoleKeys = []string{"planner", "worker", "verifier", "judge"}

// workerProfileKindKeys lists the task-kind keys worker_profile_defaults.kind accepts.
var workerProfileKindKeys = []string{"implementation", "docs", "note", "final_gate"}

// ValidateWorkerProfileConfig checks the worker session profile settings in
// cfg.Autonomous and returns the first problem found, or nil. It refuses:
//   - a user profile whose name equals a built-in profile name;
//   - a user profile that workerprofile.Validate rejects;
//   - a worker_profile_defaults value, or the worker_profile global name, that
//     names no built-in and no user-defined profile;
//   - an unknown role key in worker_profile_defaults.role, or an unknown task
//     kind key in worker_profile_defaults.kind.
//
// The config load paths call it, and every write surface that changes these
// settings should call it before it persists them. Keys are checked in sorted
// order so the same bad config always reports the same error.
func ValidateWorkerProfileConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	a := cfg.Autonomous
	userNames := sortedProfileNames(a.WorkerProfiles)
	for _, name := range userNames {
		if _, builtin := workerprofile.Builtin(name); builtin {
			return fmt.Errorf("autonomous.worker_profiles.%s: %q is a built-in profile name; choose another name", name, name)
		}
		p := a.WorkerProfiles[name]
		if p.Name != "" && p.Name != name {
			return fmt.Errorf("autonomous.worker_profiles.%s: name %q must match its key %q or be empty", name, p.Name, name)
		}
		p.Name = name
		if err := workerprofile.Validate(p); err != nil {
			return fmt.Errorf("autonomous.worker_profiles.%s: %w", name, err)
		}
	}

	known := func(name string) bool {
		if _, builtin := workerprofile.Builtin(name); builtin {
			return true
		}
		_, user := a.WorkerProfiles[name]
		return user
	}
	if err := validateWorkerProfileKeys("autonomous.worker_profile_defaults.role", a.WorkerProfileDefaults.Role, workerProfileRoleKeys, known); err != nil {
		return err
	}
	if err := validateWorkerProfileKeys("autonomous.worker_profile_defaults.kind", a.WorkerProfileDefaults.Kind, workerProfileKindKeys, known); err != nil {
		return err
	}
	if a.WorkerProfile != "" && !known(a.WorkerProfile) {
		return fmt.Errorf("autonomous.worker_profile: %q names no built-in or user-defined profile", a.WorkerProfile)
	}
	return nil
}

// validateWorkerProfileKeys checks one defaults map: every key must be in
// allowed and every non-empty value must satisfy known. An empty value
// means no default and is accepted.
func validateWorkerProfileKeys(field string, m map[string]string, allowed []string, known func(string) bool) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !workerProfileKeyAllowed(allowed, k) {
			return fmt.Errorf("%s: unknown key %q (valid keys: %s)", field, k, strings.Join(allowed, ", "))
		}
		if v := m[k]; v != "" && !known(v) {
			return fmt.Errorf("%s.%s: %q names no built-in or user-defined profile", field, k, v)
		}
	}
	return nil
}

// sortedProfileNames returns the keys of profiles in ascending order.
func sortedProfileNames(profiles map[string]workerprofile.WorkerProfile) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// workerProfileKeyAllowed reports whether want is one of list.
func workerProfileKeyAllowed(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
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

	cfg.DeprecatedKeys = detectDeprecatedKeys(cfg)
	applyDefaults(cfg)
	if err := validateLoaded(cfg); err != nil {
		return nil, err
	}
	if err := validateGateDisagreement(cfg.Autonomous.GateDisagreement); err != nil {
		return nil, err
	}
	if err := ValidateVerifierRunCapSeconds(cfg.Autonomous.VerifierMaxRunSeconds); err != nil {
		return nil, err
	}
	if err := cfg.Autonomous.FailureEscalation.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateAttemptCostSettings(cfg.Autonomous); err != nil {
		return nil, err
	}
	if err := validateFailureEscalationYAML(data); err != nil {
		return nil, err
	}
	if err := loadShipped(cfg); err != nil {
		return nil, err
	}
	if err := loadTags(cfg); err != nil {
		return nil, err
	}
	if err := loadDefectQueue(cfg); err != nil {
		return nil, err
	}
	if err := loadAutoTags(cfg); err != nil {
		return nil, err
	}
	if err := loadHousekeepingRepairs(cfg); err != nil {
		return nil, err
	}
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
	cfg.DeprecatedKeys = detectDeprecatedKeys(cfg)
	applyDefaults(cfg)
	if err := validateLoaded(cfg); err != nil {
		return nil, err
	}
	if err := validateGateDisagreement(cfg.Autonomous.GateDisagreement); err != nil {
		return nil, err
	}
	if err := ValidateVerifierRunCapSeconds(cfg.Autonomous.VerifierMaxRunSeconds); err != nil {
		return nil, err
	}
	if err := cfg.Autonomous.FailureEscalation.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateAttemptCostSettings(cfg.Autonomous); err != nil {
		return nil, err
	}
	if err := validateFailureEscalationYAML(data); err != nil {
		return nil, err
	}
	if err := loadShipped(cfg); err != nil {
		return nil, err
	}
	if err := loadTags(cfg); err != nil {
		return nil, err
	}
	if err := loadDefectQueue(cfg); err != nil {
		return nil, err
	}
	if err := loadAutoTags(cfg); err != nil {
		return nil, err
	}
	if err := loadHousekeepingRepairs(cfg); err != nil {
		return nil, err
	}
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

// DefaultPRDWorktreeDir is the folder the daemon creates PRD worktrees in
// when autonomous.worktree_dir is not set.
func DefaultPRDWorktreeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".datawatch", "prd-worktrees")
	}
	return filepath.Join(home, ".datawatch", "prd-worktrees")
}

// DefaultPRDWorktreeDirFor is the PRD worktree folder (prd-worktrees) under
// the given data directory, with a leading "~" expanded to the home
// directory. An empty data directory falls back to the historical default
// location under the user's home (DefaultPRDWorktreeDir).
func DefaultPRDWorktreeDirFor(dataDir string) string {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return DefaultPRDWorktreeDir()
	}
	return filepath.Join(expandHome(dataDir), "prd-worktrees")
}

// DefaultWorkspaceRoot is the folder project directories may lie under when
// session.workspace_root is not set: "workspace" in the user's home
// directory. It is empty when the home directory cannot be determined.
func DefaultWorkspaceRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "workspace")
}

// AllowedProjectRoots returns every directory a request-supplied project
// directory may lie under: the PRD worktree base, the session workspace root
// (the default workspace folder when session.workspace_root is empty) and
// server.allowed_project_roots. The first two are always in the list,
// whatever else is configured, and whether or not an API token is set. A "~"
// prefix in a configured root is expanded to the home directory.
func (c *Config) AllowedProjectRoots() []string {
	if c == nil {
		roots := []string{DefaultPRDWorktreeDir()}
		if ws := DefaultWorkspaceRoot(); ws != "" {
			roots = append(roots, ws)
		}
		return roots
	}
	base := strings.TrimSpace(c.Autonomous.WorktreeDir)
	if base == "" {
		base = DefaultPRDWorktreeDirFor(c.DataDir)
	}
	roots := []string{expandHome(base)}
	if ws := strings.TrimSpace(c.Session.WorkspaceRoot); ws != "" {
		roots = append(roots, expandHome(ws))
	} else if ws := DefaultWorkspaceRoot(); ws != "" {
		roots = append(roots, ws)
	}
	for _, r := range c.Server.AllowedProjectRoots {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, expandHome(r))
		}
	}
	return roots
}

// ValidateAllowedProjectRoots checks server.allowed_project_roots: every entry
// must be an absolute path (a leading "~" is allowed) with no NUL byte, and
// none may be a filesystem root, which would allow every directory.
func ValidateAllowedProjectRoots(roots []string) error {
	for _, r := range roots {
		r = strings.TrimSpace(r)
		switch {
		case r == "":
			return errors.New("server.allowed_project_roots: empty entry")
		case strings.ContainsRune(r, 0):
			return fmt.Errorf("server.allowed_project_roots: %q contains a NUL byte", r)
		}
		e := expandHome(r)
		if !filepath.IsAbs(e) {
			return fmt.Errorf("server.allowed_project_roots: %q must be an absolute path", r)
		}
		if c := filepath.Clean(e); filepath.Dir(c) == c {
			return fmt.Errorf("server.allowed_project_roots: %q is a filesystem root and would allow every directory", r)
		}
	}
	return nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
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

// EffectiveReservedInteractive returns the number of max_sessions slots held
// back for operator sessions: the configured value, or 1 when max_sessions
// is at least 3 (otherwise 0). Never more than max_sessions-1.
func (c SessionConfig) EffectiveReservedInteractive() int {
	n := 0
	if c.ReservedInteractive != nil {
		n = *c.ReservedInteractive
	} else if c.MaxSessions >= 3 {
		n = 1
	}
	if c.MaxSessions > 0 && n > c.MaxSessions-1 {
		n = c.MaxSessions - 1
	}
	if n < 0 {
		n = 0
	}
	return n
}

// RoleScanIntervalDefaultSeconds is the default autonomous.role_scan_interval_seconds.
const RoleScanIntervalDefaultSeconds = 300

// AutonomousParallelConfig is autonomous.parallel, the knobs for running
// tasks in parallel. ReadOnlyTasks is a pointer: nil means the key is
// absent. The default is applied only by
// autonomous.EffectiveReadOnlyTasks. No omitempty, so an explicit false
// survives a YAML/JSON round trip and a PUT /api/config write.
type AutonomousParallelConfig struct {
	// ReadOnlyTasks: tasks flagged read_only skip the tree lock.
	ReadOnlyTasks *bool `yaml:"read_only_tasks" json:"read_only_tasks"`
	// WorktreePerTask: each parallel task runs in its own worktree.
	// Pointer so an explicit false survives; default applied by
	// autonomous.EffectiveWorktreePerTask.
	WorktreePerTask *bool `yaml:"worktree_per_task" json:"worktree_per_task"`
	// MergeConflictPolicy: additive_then_model | block | triage. Empty
	// means the default (see autonomous.EffectiveMergeConflictPolicy).
	MergeConflictPolicy string `yaml:"merge_conflict_policy" json:"merge_conflict_policy"`
	// MergeConflictModel: model that resolves a merge conflict. Empty
	// means the strongest configured planning model.
	MergeConflictModel string `yaml:"merge_conflict_model" json:"merge_conflict_model"`
	// MergeRetrySeconds: whole seconds between timed merge retry passes.
	// Pointer so an explicit 0 (timer off) survives; nil means the default
	// (see autonomous.EffectiveMergeRetrySeconds).
	MergeRetrySeconds *int `yaml:"merge_retry_seconds" json:"merge_retry_seconds"`
}

// DefaultMergeRetrySeconds is the merge retry interval read when
// autonomous.parallel.merge_retry_seconds is absent.
const DefaultMergeRetrySeconds = 60

// ValidateMergeRetrySeconds refuses a negative interval. Nil is valid (the
// key is absent and the default applies); 0 turns the timer off.
func ValidateMergeRetrySeconds(v *int) error {
	if v != nil && *v < 0 {
		return fmt.Errorf("autonomous.parallel.merge_retry_seconds: %d must be a whole number >= 0 (0 = timer off, default %d)",
			*v, DefaultMergeRetrySeconds)
	}
	return nil
}

// Valid values of autonomous.parallel.merge_conflict_policy.
const (
	MergeConflictPolicyAdditiveThenModel = "additive_then_model"
	MergeConflictPolicyBlock             = "block"
	MergeConflictPolicyTriage            = "triage"
)

// ValidateMergeConflictPolicy refuses an unknown policy. Empty is valid
// (the key is absent and the default applies).
func ValidateMergeConflictPolicy(v string) error {
	switch v {
	case "", MergeConflictPolicyAdditiveThenModel, MergeConflictPolicyBlock, MergeConflictPolicyTriage:
		return nil
	}
	return fmt.Errorf("autonomous.parallel.merge_conflict_policy: invalid value %q (valid values: %q, %q, %q)",
		v, MergeConflictPolicyAdditiveThenModel, MergeConflictPolicyBlock, MergeConflictPolicyTriage)
}

// ValidateTracing validates the tracing.* block for the write paths; an
// unset (zero) value is valid and reads as its default.
func ValidateTracing(t trace.Settings) error {
	return t.Validate()
}

// ClassifierConfig holds the classifier settings that live in the config file.
type ClassifierConfig struct {
	// PublisherAllowlist is the list of model publishers a classify-mode judge
	// model may come from (classifier.publisher_allowlist). Default empty: no
	// publisher is trusted until the operator adds it. Entries are added only
	// with explicit operator confirmation (REST, MCP, CLI and chat) and each
	// addition is written to the audit log.
	PublisherAllowlist []string `yaml:"publisher_allowlist,omitempty" json:"publisher_allowlist,omitempty"`

	// TimeoutSeconds bounds one classify call (classifier.timeout_seconds).
	// Default 30. A timeout is a block with an infra category.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	// TopLogprobs is top_logprobs on the generic path (classifier.top_logprobs). Default 10.
	TopLogprobs int `yaml:"top_logprobs,omitempty" json:"top_logprobs,omitempty"`
	// AbstainMargin is the least lead over the runner-up (classifier.abstain_margin). Default 0.10.
	AbstainMargin float64 `yaml:"abstain_margin,omitempty" json:"abstain_margin,omitempty"`
	// MinProbability is the least probability of the chosen option (classifier.min_probability). Default 0.50.
	MinProbability float64 `yaml:"min_probability,omitempty" json:"min_probability,omitempty"`
	// MinOptionMass is the least share of first-token probability held by the
	// option tokens (classifier.min_option_mass). Default 0.50.
	MinOptionMass float64 `yaml:"min_option_mass,omitempty" json:"min_option_mass,omitempty"`
	// RotateOptions rotates option order across calls (classifier.rotate_options). Default false.
	RotateOptions bool `yaml:"rotate_options,omitempty" json:"rotate_options,omitempty"`
	// ThinkingOffModels lists model-name substrings whose templates get
	// thinking disabled (classifier.thinking_off_models). Default ["qwen3"].
	ThinkingOffModels []string `yaml:"thinking_off_models,omitempty" json:"thinking_off_models,omitempty"`
	// ProbeTTLSeconds is how long a capability probe result is cached
	// (classifier.probe_ttl_seconds). Default 600. 0 reads the default.
	ProbeTTLSeconds int `yaml:"probe_ttl_seconds,omitempty" json:"probe_ttl_seconds,omitempty"`
	// ProbeTimeoutSeconds bounds one capability probe (classifier.probe_timeout_seconds).
	// Default 5. 0 reads the default.
	ProbeTimeoutSeconds int `yaml:"probe_timeout_seconds,omitempty" json:"probe_timeout_seconds,omitempty"`
	// Calibration holds the calibration tunables (classifier.calibration.*).
	Calibration ClassifierCalibrationConfig `yaml:"calibration,omitempty" json:"calibration,omitempty"`
}

// ClassifierCalibrationConfig holds the classifier.calibration.* keys. A zero
// value means the documented default applies.
type ClassifierCalibrationConfig struct {
	TestFraction          float64 `yaml:"test_fraction,omitempty" json:"test_fraction,omitempty"`
	SplitSeed             string  `yaml:"split_seed,omitempty" json:"split_seed,omitempty"`
	MinFitSamples         int     `yaml:"min_fit_samples,omitempty" json:"min_fit_samples,omitempty"`
	FalsePassWeight       float64 `yaml:"false_pass_weight,omitempty" json:"false_pass_weight,omitempty"`
	RotationBiasThreshold float64 `yaml:"rotation_bias_threshold,omitempty" json:"rotation_bias_threshold,omitempty"`
	RotationFlipThreshold float64 `yaml:"rotation_flip_threshold,omitempty" json:"rotation_flip_threshold,omitempty"`
}

// SupervisorLoopConfig is the supervisor_loop block: the settings as flat
// dotted keys (for example "supervisor_loop.lane_count.max": 4). Absent keys
// take the defaults in supervisorloop.DefaultSettings; supervisorloop.ParseSettings
// reads it and Settings.Set validates a change. It lives with the other
// autonomous blocks (Autonomous.SupervisorLoop, yaml key supervisor_loop).
type SupervisorLoopConfig map[string]any

// Clone returns an independent copy.
func (c SupervisorLoopConfig) Clone() SupervisorLoopConfig {
	if c == nil {
		return nil
	}
	out := make(SupervisorLoopConfig, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}

// DefaultArchiveGraceMinutes is the cancelled-Automaton archive grace when
// autonomous.archive_grace_minutes is unset.
const DefaultArchiveGraceMinutes = 60

// ArchiveGraceMinutesOrDefault resolves autonomous.archive_grace_minutes:
// unset is 60, an explicit 0 stays 0 (archive at once), and a negative stored
// value reads as the default.
func (a AutonomousConfig) ArchiveGraceMinutesOrDefault() int {
	if a.ArchiveGraceMinutes == nil || *a.ArchiveGraceMinutes < 0 {
		return DefaultArchiveGraceMinutes
	}
	return *a.ArchiveGraceMinutes
}

// ParseArchiveGraceMinutes validates a candidate autonomous.archive_grace_minutes
// value from a JSON number, an int or text. It must be a whole number >= 0.
func ParseArchiveGraceMinutes(v any) (int, error) {
	const key = "autonomous.archive_grace_minutes"
	switch x := v.(type) {
	case int:
		if x < 0 {
			return 0, fmt.Errorf("%s: must be a whole number of minutes >= 0, got %d", key, x)
		}
		return x, nil
	case float64:
		if x != float64(int(x)) {
			return 0, fmt.Errorf("%s: must be a whole number of minutes, got %v", key, x)
		}
		return ParseArchiveGraceMinutes(int(x))
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("%s: must be a whole number of minutes >= 0, got %q", key, x)
		}
		return ParseArchiveGraceMinutes(n)
	}
	return 0, fmt.Errorf("%s: must be a whole number of minutes >= 0, got %v", key, v)
}
