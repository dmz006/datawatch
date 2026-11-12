// Package stats collects system and session metrics for the dashboard.
// Metrics are held in a ring buffer (1 hour at 5s intervals) — no persistence.
package stats

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemStats holds a snapshot of system metrics.
type SystemStats struct {
	Timestamp time.Time `json:"timestamp"`

	// CPU
	CPULoadAvg1  float64 `json:"cpu_load_avg_1"`
	CPULoadAvg5  float64 `json:"cpu_load_avg_5"`
	CPULoadAvg15 float64 `json:"cpu_load_avg_15"`
	CPUCores     int     `json:"cpu_cores"`

	// Memory (bytes)
	MemTotal     uint64 `json:"mem_total"`
	MemUsed      uint64 `json:"mem_used"`
	MemAvailable uint64 `json:"mem_available"`
	SwapTotal    uint64 `json:"swap_total"`
	SwapUsed     uint64 `json:"swap_used"`

	// Disk (bytes) — data_dir partition
	DiskTotal uint64 `json:"disk_total"`
	DiskUsed  uint64 `json:"disk_used"`
	DiskFree  uint64 `json:"disk_free"`

	// GPU (optional — empty if no GPU detected)
	GPUName       string `json:"gpu_name,omitempty"`
	GPUTemp       int    `json:"gpu_temp,omitempty"`     // Celsius
	GPUUtilPct    int    `json:"gpu_util_pct,omitempty"` // 0-100
	GPUMemUsedMB  int    `json:"gpu_mem_used_mb,omitempty"`
	GPUMemTotalMB int    `json:"gpu_mem_total_mb,omitempty"`
	// GPUError explains why the GPU fields above are empty when nvidia-smi
	// exists but failed (e.g. a driver/library version mismatch after an
	// unattended driver update, before a reboot) — as opposed to being empty
	// because no GPU/nvidia-smi was found at all, which leaves this "".
	GPUError string `json:"gpu_error,omitempty"`

	// Process
	DaemonRSSBytes uint64 `json:"daemon_rss_bytes"`
	Goroutines     int    `json:"goroutines"`
	OpenFDs        int    `json:"open_fds"`

	// Sessions (filled externally by the server)
	ActiveSessions int `json:"active_sessions"`
	TotalSessions  int `json:"total_sessions"`

	// Tmux
	TmuxSessions  int      `json:"tmux_sessions"`           // total tmux sessions with cs- prefix
	OrphanedTmux  []string `json:"orphaned_tmux,omitempty"` // tmux sessions with no matching datawatch session
	UptimeSeconds int      `json:"uptime_seconds"`

	// eBPF status
	EBPFEnabled bool   `json:"ebpf_enabled"`
	EBPFActive  bool   `json:"ebpf_active"`            // true if BPF programs are loaded
	EBPFMessage string `json:"ebpf_message,omitempty"` // status/warning message

	// Network
	BoundInterfaces []string `json:"bound_interfaces,omitempty"`
	NetRxBytes      uint64   `json:"net_rx_bytes"` // total received bytes (all interfaces)
	NetTxBytes      uint64   `json:"net_tx_bytes"` // total transmitted bytes

	// Server interfaces (for infrastructure card)
	WebPort    int    `json:"web_port,omitempty"`
	TLSEnabled bool   `json:"tls_enabled,omitempty"`
	TLSPort    int    `json:"tls_port,omitempty"`
	MCPSSEHost string `json:"mcp_sse_host,omitempty"`
	MCPSSEPort int    `json:"mcp_sse_port,omitempty"`
	// GH#192 D78a — Observer's Daemon/Infrastructure cards had neither
	// hostname nor the running daemon version anywhere. Populated via
	// SetServerIdentity, the same pattern as SetServerInterfaces below.
	Hostname      string `json:"hostname,omitempty"`
	DaemonVersion string `json:"daemon_version,omitempty"`

	// RTK (Rust Token Killer) integration stats
	RTKInstalled       bool    `json:"rtk_installed,omitempty"`
	RTKVersion         string  `json:"rtk_version,omitempty"`
	RTKHooksActive     bool    `json:"rtk_hooks_active,omitempty"`
	RTKTotalSaved      int     `json:"rtk_total_saved,omitempty"`     // total tokens saved
	RTKAvgSavings      float64 `json:"rtk_avg_savings_pct,omitempty"` // average savings percentage
	RTKTotalCmds       int     `json:"rtk_total_commands,omitempty"`
	RTKLatestVersion   string  `json:"rtk_latest_version,omitempty"`   // latest available version from GitHub
	RTKUpdateAvailable bool    `json:"rtk_update_available,omitempty"` // true if newer version exists

	// Episodic Memory stats
	MemoryEnabled       bool   `json:"memory_enabled,omitempty"`
	MemoryBackend       string `json:"memory_backend,omitempty"`     // "sqlite" or "postgres"
	MemoryEmbedder      string `json:"memory_embedder,omitempty"`    // e.g. "ollama:nomic-embed-text"
	MemoryTotalCount    int    `json:"memory_total_count,omitempty"` // total memories across all projects
	MemoryManualCount   int    `json:"memory_manual_count,omitempty"`
	MemorySessionCount  int    `json:"memory_session_count,omitempty"`
	MemoryLearningCount int    `json:"memory_learning_count,omitempty"`
	MemoryChunkCount    int    `json:"memory_chunk_count,omitempty"`
	MemoryDBSizeBytes   int64  `json:"memory_db_size_bytes,omitempty"`
	MemoryEncrypted     bool   `json:"memory_encrypted,omitempty"`
	MemoryKeyFP         string `json:"memory_key_fingerprint,omitempty"`

	// Ollama server stats (BL71)
	OllamaStats *OllamaStats `json:"ollama_stats,omitempty"`

	// Per-session stats (filled by orphan detect callback)
	SessionStats []SessionStat `json:"session_stats,omitempty"`

	// Communication channel stats
	CommStats []CommChannelStat `json:"comm_stats,omitempty"`

	// MCP extended stats (BL302 S1)
	MCPStats *MCPStatsBlock `json:"mcp_stats,omitempty"`

	// BL367 — autonomous quality gate aggregate stats.
	QualityGateRuns int `json:"quality_gate_runs,omitempty"`
	QualityGatePass int `json:"quality_gate_pass,omitempty"`
	QualityGateFail int `json:"quality_gate_fail,omitempty"`

	// BL382 — cancel/requeue lifecycle operation totals.
	StoriesCancelled int64 `json:"stories_cancelled,omitempty"`
	TasksCancelled   int64 `json:"tasks_cancelled,omitempty"`
	TasksRequeued    int64 `json:"tasks_requeued,omitempty"`

	// Worker questions (the DATAWATCH_QUESTION: marker), counted since daemon
	// start: lines raised as questions, repeats of an open question, lines
	// refused, tasks parked on a blocking question and tasks resumed.
	WorkerQuestionsRaised    int64 `json:"worker_questions_raised,omitempty"`
	WorkerQuestionsDuplicate int64 `json:"worker_questions_duplicate,omitempty"`
	WorkerQuestionsRefused   int64 `json:"worker_questions_refused,omitempty"`
	WorkerQuestionsParked    int64 `json:"worker_questions_parked,omitempty"`
	WorkerQuestionsResumed   int64 `json:"worker_questions_resumed,omitempty"`

	// Safe-to-delete report (autonomous manager): duration in
	// milliseconds of the most recent safe-to-delete computation, and the
	// totals of computations over the warning threshold and of failed
	// computations. Populated in the autonomous stats callback.
	SafeDeleteLastMS          int64  `json:"safe_delete_last_ms,omitempty"`
	SafeDeleteSlowReports     uint64 `json:"safe_delete_slow_reports,omitempty"`
	SafeDeleteRefreshFailures uint64 `json:"safe_delete_refresh_failures,omitempty"`

	// Branch refresh (autonomous.branch_refresh_enabled): Automata by the
	// result of their latest refresh, and how many are stopped by a conflict.
	BranchRefreshClean    int `json:"branch_refresh_clean,omitempty"`
	BranchRefreshConflict int `json:"branch_refresh_conflict,omitempty"`
	BranchRefreshError    int `json:"branch_refresh_error,omitempty"`
	BranchRefreshStopped  int `json:"branch_refresh_stopped,omitempty"`

	// Agent tracing (tracing.* settings): store totals and write counters.
	// Present only while tracing is on. Populated by the trace stats callback.
	TraceEnabled      bool   `json:"trace_enabled,omitempty"`
	TraceSessions     int    `json:"trace_sessions,omitempty"`
	TraceEvents       int    `json:"trace_events,omitempty"`
	TraceErrorEvents  int    `json:"trace_error_events,omitempty"`
	TraceAppended     uint64 `json:"trace_appended_total,omitempty"`
	TraceAppendErrors uint64 `json:"trace_append_errors_total,omitempty"`
	TracePruned       uint64 `json:"trace_pruned_total,omitempty"`
	TraceLastEventAge int64  `json:"trace_last_event_age_seconds,omitempty"`

	// UI prompt interstitials (ui_prompt.* settings): the daemon-side
	// dismissal outcome counters for recognised interstitial prompts, since
	// daemon start (in-memory only — reset on restart). Filled by the
	// session manager through SetUIPromptsFunc; nil when the daemon never
	// wired it.
	UIPrompts *UIPromptStats `json:"ui_prompts,omitempty"`

	// Supervisor watch: the mode in effect, seconds since the coordinator
	// last polled (-1 when it never has), the unresolved findings (and
	// how many of them are would-alert or alerted) and the days of full
	// agreement with the supervisor script.
	SupervisorWatchMode               string `json:"supervisor_watch_mode,omitempty"`
	SupervisorWatchLastPollAgeSeconds int64  `json:"supervisor_watch_last_poll_age_seconds,omitempty"`
	SupervisorWatchFindingsOpen       int    `json:"supervisor_watch_findings_open,omitempty"`
	SupervisorWatchFindingsWouldAlert int    `json:"supervisor_watch_findings_would_alert,omitempty"`
	SupervisorWatchFindingsAlerted    int    `json:"supervisor_watch_findings_alerted,omitempty"`
	SupervisorWatchAgreedDays         int    `json:"supervisor_watch_agreed_days,omitempty"`

	// Supervisor loop: whether it is on, its job counts, the stored run
	// counts by outcome, the retries used and the seconds since the last run
	// (-1 when none has run).
	SupervisorLoopEnabled           bool  `json:"supervisor_loop_enabled,omitempty"`
	SupervisorLoopJobs              int   `json:"supervisor_loop_jobs,omitempty"`
	SupervisorLoopJobsEnabled       int   `json:"supervisor_loop_jobs_enabled,omitempty"`
	SupervisorLoopRuns              int   `json:"supervisor_loop_runs,omitempty"`
	SupervisorLoopRunsOK            int   `json:"supervisor_loop_runs_ok,omitempty"`
	SupervisorLoopRunsValidatorFail int   `json:"supervisor_loop_runs_validator_failed,omitempty"`
	SupervisorLoopRunsEscalated     int   `json:"supervisor_loop_runs_escalated,omitempty"`
	SupervisorLoopRunsError         int   `json:"supervisor_loop_runs_error,omitempty"`
	SupervisorLoopRetries           int   `json:"supervisor_loop_retries,omitempty"`
	SupervisorLoopLastRunAgeSeconds int64 `json:"supervisor_loop_last_run_age_seconds,omitempty"`

	// Host load check (autonomous.host_load_alert): the effective mode,
	// the last successful load reading against its threshold (multiple x
	// core count), whether that reading is over the threshold (a breach
	// streak in progress), when the last reading was, and how many
	// leftover processes the reaper has ended for ended sessions since
	// the check started (reap mode; the alert fires on the same tick as
	// the reap). Populated in the autonomous stats callback from the
	// check's Status (); zero/empty until the first successful reading
	// after startup, and Mode stays "off" while disabled.
	HostLoadMode          string    `json:"host_load_mode,omitempty"`
	HostLoadLast          float64   `json:"host_load_last,omitempty"`
	HostLoadLimit         float64   `json:"host_load_limit,omitempty"`
	HostLoadBreached      bool      `json:"host_load_breached"`
	HostLoadLastCheckAt   time.Time `json:"host_load_last_check_at,omitempty"`
	HostLoadOrphansReaped int       `json:"host_load_orphans_reaped,omitempty"`

	// Burn guard (usage.burn_guard): ladder state, projection and counters.
	BurnGuardState         string  `json:"burn_guard_state,omitempty"`
	BurnGuardLevel         int     `json:"burn_guard_level,omitempty"`
	BurnGuardProjected     float64 `json:"burn_guard_projected_percent,omitempty"`
	BurnGuardTarget        float64 `json:"burn_guard_target_percent,omitempty"`
	BurnGuardOverBy        float64 `json:"burn_guard_over_by_points,omitempty"`
	BurnGuardPointsPerHour float64 `json:"burn_guard_points_per_hour,omitempty"`
	BurnGuardLastTickAge   int64   `json:"burn_guard_last_tick_age_seconds,omitempty"`
	BurnGuardTicks         int64   `json:"burn_guard_ticks,omitempty"`
	BurnGuardClimbs        int64   `json:"burn_guard_climbs,omitempty"`
	BurnGuardStepBacks     int64   `json:"burn_guard_step_backs,omitempty"`
	BurnGuardRefits        int64   `json:"burn_guard_refits,omitempty"`
	BurnGuardErrors        int64   `json:"burn_guard_errors,omitempty"`

	// BL372 — web search MCP injection stats.
	WebSearchEnabled      bool      `json:"web_search_enabled,omitempty"`
	WebSearchProvider     string    `json:"web_search_provider,omitempty"`
	WebSearchURL          string    `json:"web_search_url,omitempty"`
	WebSearchQueriesTotal int64     `json:"web_search_queries_total,omitempty"`
	WebSearchErrorsTotal  int64     `json:"web_search_errors_total,omitempty"`
	WebSearchLastQueryAt  time.Time `json:"web_search_last_query_at,omitempty"`

	// GH#201 — HTTP access / WS lifecycle / auth-failure log. Counts are
	// since daemon start (in-process, reset on restart); the durable,
	// cross-restart totals are the datawatch_access_log_events_total
	// Prometheus counter.
	AccessLogEnabled           bool  `json:"access_log_enabled,omitempty"`
	AccessLogEventsTotal       int64 `json:"access_log_events_total,omitempty"`
	AccessLogAuthFailuresTotal int64 `json:"access_log_auth_failures_total,omitempty"`

	// Pre-push secret-scan counters (runs, findings, shadow reports,
	// refusals, last-run/last-refusal times), since daemon start. Filled
	// by the git package's counters through SetSecretScanStatsFunc; nil
	// (and therefore omitted from JSON) when the daemon never scanned.
	SecretScan *SecretScanStats `json:"secret_scan,omitempty"`

	// WorkerStartup is the average startup token size per worker profile
	// and per role (see WorkerStartupStats). Always present; both maps are
	// empty when no attempt carries a startup size.
	WorkerStartup WorkerStartupStats `json:"worker_startup"`
}

// CommChannelStat holds detailed stats for a communication channel or LLM backend.
type CommChannelStat struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "messaging", "llm", "infra"
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint,omitempty"` // connection endpoint (group, channel, URL)

	// Message counters (messaging + infra)
	MsgSent  int   `json:"msg_sent"`
	MsgRecv  int   `json:"msg_recv"`
	Errors   int   `json:"errors"`
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`

	// Connection info
	Connections int   `json:"connections,omitempty"` // active connections (WS clients, MCP clients)
	LastActive  int64 `json:"last_active,omitempty"` // unix timestamp

	// LLM-specific stats
	TotalSessions  int     `json:"total_sessions,omitempty"`
	ActiveSessions int     `json:"active_sessions,omitempty"`
	AvgDurationSec float64 `json:"avg_duration_sec,omitempty"` // average session duration in seconds
	AvgPrompts     float64 `json:"avg_prompts,omitempty"`      // average prompts per session
	AvgMessages    float64 `json:"avg_messages,omitempty"`     // average messages per session
}

// SessionStat holds resource usage for a single session.
// UIPromptKindStats is one dismissal outcome row for interstitial UI
// prompts (see UIPromptStats).
type UIPromptKindStats struct {
	// Dismissed counts prompt occurrences whose safe dismiss key cleared the
	// prompt (the daemon pressed it and the screen got back to work).
	Dismissed int `json:"dismissed"`
	// NotCleared counts occurrences where the attempts cap was used up with
	// the prompt still on screen (the daemon gave up on keys for it).
	NotCleared int `json:"not_cleared"`
	// Refused counts dismiss keys the send-site guard refused to send
	// (safety events: the guard worked, nothing was sent).
	Refused int `json:"refused"`
}

// UIPromptStats is the daemon-side in-memory dismissal outcome snapshot for
// interstitial UI prompts: totals plus one row per prompt kind. All fields
// are counted since daemon start and reset on restart.
type UIPromptStats struct {
	// Dismissed is the total across kinds.
	Dismissed int `json:"dismissed"`
	// NotCleared is the total across kinds.
	NotCleared int `json:"not_cleared"`
	// Refused is the total across kinds.
	Refused int `json:"refused"`
	// ByKind breaks the totals down per interstitial prompt kind (e.g.
	// autocompact).
	ByKind map[string]UIPromptKindStats `json:"by_kind,omitempty"`
}

type SessionStat struct {
	SessionID  string  `json:"session_id"`
	Name       string  `json:"name"`
	Backend    string  `json:"backend"`
	State      string  `json:"state"`
	PanePID    int     `json:"pane_pid"`
	RSSBytes   uint64  `json:"rss_bytes"` // resident set size of process tree
	CPUPercent float64 `json:"cpu_percent"`
	Uptime     string  `json:"uptime"`       // elapsed time
	NetTxBytes uint64  `json:"net_tx_bytes"` // per-session TCP TX (eBPF, 0 if disabled)
	NetRxBytes uint64  `json:"net_rx_bytes"` // per-session TCP RX (eBPF, 0 if disabled)
	// RTK token savings for this session's project
	RTKSavedTokens int     `json:"rtk_saved_tokens,omitempty"`
	RTKSavingsPct  float64 `json:"rtk_savings_pct,omitempty"`
	RTKCommands    int     `json:"rtk_commands,omitempty"`
}

// SecretScanStats is the pre-push secret-scan counter block surfaced in
// SystemStats.SecretScan: how many scans executed, how many findings they
// produced, how many shadow reports and enforce refusals, plus the times
// of the last of each. Mirrors the git package's SecretScanStats json tags
// (the two packages cannot import each other, so the shape is duplicated
// here and the wiring callback copies field by field). Counts are since
// daemon start and reset on restart.
type SecretScanStats struct {
	Runs          int64     `json:"runs"`
	Findings      int64     `json:"findings"`
	ShadowReports int64     `json:"shadow_reports"`
	Refusals      int64     `json:"refusals"`
	LastRunAt     time.Time `json:"last_run_at"`
	LastRefusalAt time.Time `json:"last_refusal_at"`
}

// Collector periodically samples system metrics and stores them in a ring buffer.
type Collector struct {
	mu      sync.RWMutex
	ring    []SystemStats
	maxSize int
	idx     int
	full    bool
	dataDir string

	// sessionCountFn returns (active, total) session counts.
	sessionCountFn func() (int, int)

	// orphanDetectFn returns (tmux_count, orphaned_names)
	orphanDetectFn func() (int, []string)

	// sessionStatsFn returns per-session resource stats
	sessionStatsFn func() []SessionStat

	// commStatsFn returns communication channel statistics
	commStatsFn func() []CommChannelStat

	// onCollect is called after each collection with the latest stats (for WS broadcast)
	onCollect func(SystemStats)

	// boundInterfaces returns the list of bound interface addresses
	boundInterfaces []string

	startTime   time.Time
	ebpfEnabled bool
	ebpfActive  bool
	ebpfMessage string

	// daemonNetFn returns (tx, rx) bytes for the daemon process tree via eBPF
	daemonNetFn func() (uint64, uint64)

	// rtkFn populates RTK fields on a stats snapshot
	rtkFn func(*SystemStats)

	// memoryStatsFn populates episodic memory metrics
	memoryStatsFn func(*SystemStats)

	// burnGuardStatsFn populates the burn guard fields.
	burnGuardStatsFn func(*SystemStats)
	// supervisorLoopStatsFn populates the supervisor loop fields.
	supervisorLoopStatsFn func(*SystemStats)

	// traceStatsFn populates the agent tracing fields.
	traceStatsFn func(*SystemStats)

	// accessLogStatsFn (GH#201) populates HTTP access / WS lifecycle /
	// auth-failure log fields.
	accessLogStatsFn func(*SystemStats)

	// autonomousStatsFn populates autonomous quality gate metrics
	autonomousStatsFn func(*SystemStats)

	// webSearchStatsFn populates web search stats (BL372)
	webSearchStatsFn func(*SystemStats)

	// secretScanStatsFn populates the pre-push secret-scan counters
	// (SecretScan field).
	secretScanStatsFn func(*SystemStats)

	// workerStartupStatsFn populates the worker_startup block (WorkerStartup
	// field) from the attempt startup records.
	workerStartupStatsFn func(*SystemStats)

	// uiPromptsFn returns the daemon-side in-memory interstitial UI-prompt
	// dismissal outcome counters (from the session manager).
	uiPromptsFn func() *UIPromptStats

	// ollamaHost is the Ollama API URL for stats polling
	ollamaHost string

	// Server interface config
	webPort    int
	tlsEnabled bool
	tlsPort    int
	mcpSSEHost string
	mcpSSEPort int

	// GH#192 D78a — server identity for the Daemon/Infrastructure cards.
	hostname      string
	daemonVersion string
}

// NewCollector creates a new metrics collector.
// dataDir is used to determine which disk partition to monitor.
func NewCollector(dataDir string) *Collector {
	return &Collector{
		maxSize:   720, // 1 hour at 5s intervals
		ring:      make([]SystemStats, 720),
		dataDir:   dataDir,
		startTime: time.Now(),
	}
}

// SetSessionCountFunc sets the callback for session counts.
func (c *Collector) SetSessionCountFunc(fn func() (int, int)) {
	c.sessionCountFn = fn
}

// SetOrphanDetectFunc sets the callback for detecting orphaned tmux sessions.
func (c *Collector) SetOrphanDetectFunc(fn func() (int, []string)) {
	c.orphanDetectFn = fn
}

// SetBoundInterfaces sets the list of interfaces the server is bound to.
func (c *Collector) SetBoundInterfaces(ifaces []string) {
	c.boundInterfaces = ifaces
}

// SetSessionStatsFunc sets the callback for per-session resource stats.
func (c *Collector) SetSessionStatsFunc(fn func() []SessionStat) {
	c.sessionStatsFn = fn
}

// SetUIPromptsFunc sets the callback that returns the daemon-side in-memory
// interstitial UI-prompt dismissal outcome counters (dismissed /
// not_cleared / refused, with a by-kind breakdown). It is snapshotted into
// the ui_prompts field of every collected SystemStats, so it is visible in
// GET /api/stats and the get_stats MCP tool.
func (c *Collector) SetUIPromptsFunc(fn func() *UIPromptStats) {
	c.uiPromptsFn = fn
}

// SetEBPFStatus sets the eBPF status for display in the dashboard.
func (c *Collector) SetEBPFStatus(enabled, active bool, message string) {
	c.ebpfEnabled = enabled
	c.ebpfActive = active
	c.ebpfMessage = message
}

// SetCommStatsFunc sets the callback for communication channel stats.
func (c *Collector) SetCommStatsFunc(fn func() []CommChannelStat) {
	c.commStatsFn = fn
}

// SetServerInterfaces sets the server interface config for the infrastructure card.
func (c *Collector) SetServerInterfaces(webPort int, tlsEnabled bool, tlsPort int, mcpSSEHost string, mcpSSEPort int) {
	c.webPort = webPort
	c.tlsEnabled = tlsEnabled
	c.tlsPort = tlsPort
	c.mcpSSEHost = mcpSSEHost
	c.mcpSSEPort = mcpSSEPort
}

// SetServerIdentity sets the hostname + running daemon version surfaced on
// the Daemon/Infrastructure cards (GH#192 D78a).
func (c *Collector) SetServerIdentity(hostname, version string) {
	c.hostname = hostname
	c.daemonVersion = version
}

// SetDaemonNetFunc sets a callback that returns per-process (tx, rx) bytes for the daemon.
func (c *Collector) SetDaemonNetFunc(fn func() (uint64, uint64)) {
	c.daemonNetFn = fn
}

// SetRTKFunc sets a callback that populates RTK fields on each stats snapshot.
func (c *Collector) SetRTKFunc(fn func(*SystemStats)) {
	c.rtkFn = fn
}

// SetAccessLogStatsFunc (GH#201) sets a callback that populates the
// access-log fields on each stats snapshot.
func (c *Collector) SetAccessLogStatsFunc(fn func(*SystemStats)) {
	c.accessLogStatsFn = fn
}

// SetTraceStatsFunc sets a callback that populates the agent tracing fields on each snapshot.
func (c *Collector) SetTraceStatsFunc(fn func(*SystemStats)) {
	c.traceStatsFn = fn
}

// SetBurnGuardStatsFunc sets a callback that populates the burn guard fields on each snapshot.
func (c *Collector) SetBurnGuardStatsFunc(fn func(*SystemStats)) {
	c.burnGuardStatsFn = fn
}

// SetSupervisorLoopStatsFunc sets a callback that populates the supervisor loop fields on each snapshot.
func (c *Collector) SetSupervisorLoopStatsFunc(fn func(*SystemStats)) {
	c.supervisorLoopStatsFn = fn
}

// SetMemoryStatsFunc sets a callback that populates episodic memory stats on each snapshot.
func (c *Collector) SetMemoryStatsFunc(fn func(*SystemStats)) {
	c.memoryStatsFn = fn
}

// SetAutonomousStatsFunc sets a callback that populates autonomous quality gate stats on each snapshot.
func (c *Collector) SetAutonomousStatsFunc(fn func(*SystemStats)) {
	c.autonomousStatsFn = fn
}

// SetWebSearchStatsFunc sets a callback that populates web search stats on each snapshot (BL372).
func (c *Collector) SetWebSearchStatsFunc(fn func(*SystemStats)) {
	c.webSearchStatsFn = fn
}

// SetWorkerStartupStatsFunc sets a callback that populates the worker
// startup averages (SystemStats.WorkerStartup) on each snapshot.
func (c *Collector) SetWorkerStartupStatsFunc(fn func(*SystemStats)) {
	c.workerStartupStatsFn = fn
}

// SetSecretScanStatsFunc sets a callback that populates the pre-push
// secret-scan counters (SystemStats.SecretScan) on each snapshot. The
// callback is expected to leave the field nil when the daemon has never
// scanned, so the key is omitted from the JSON output entirely.
func (c *Collector) SetSecretScanStatsFunc(fn func(*SystemStats)) {
	c.secretScanStatsFn = fn
}

// SetOllamaHost sets the Ollama API URL for stats polling.
func (c *Collector) SetOllamaHost(host string) {
	c.ollamaHost = host
}

// SetOnCollect sets a callback invoked after each collection (for real-time WS broadcast).
func (c *Collector) SetOnCollect(fn func(SystemStats)) {
	c.onCollect = fn
}

// GetOnCollect returns the current onCollect callback (for chaining).
func (c *Collector) GetOnCollect() func(SystemStats) {
	return c.onCollect
}

// Start begins collecting metrics every 5 seconds. Blocks until ctx is cancelled.
func (c *Collector) Start(ctx context.Context) {
	// Collect immediately on start
	c.collect()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

// Latest returns the most recent stats snapshot.
func (c *Collector) Latest() SystemStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.full && c.idx == 0 {
		return SystemStats{}
	}
	prev := c.idx - 1
	if prev < 0 {
		prev = c.maxSize - 1
	}
	return c.ring[prev]
}

// History returns up to the last N minutes of stats.
func (c *Collector) History(minutes int) []SystemStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	maxEntries := minutes * 12 // 12 entries per minute at 5s intervals
	var count int
	if c.full {
		count = c.maxSize
	} else {
		count = c.idx
	}
	if maxEntries > count {
		maxEntries = count
	}

	result := make([]SystemStats, maxEntries)
	start := c.idx - maxEntries
	if start < 0 {
		start += c.maxSize
	}
	for i := 0; i < maxEntries; i++ {
		result[i] = c.ring[(start+i)%c.maxSize]
	}
	return result
}

func (c *Collector) collect() {
	s := SystemStats{
		Timestamp:  time.Now(),
		CPUCores:   runtime.NumCPU(),
		Goroutines: runtime.NumGoroutine(),
	}

	c.readLoadAvg(&s)
	c.readMemInfo(&s)
	c.readDiskUsage(&s)
	c.readGPU(&s)
	c.readProcessStats(&s)
	// Use per-process network if eBPF available, otherwise system-wide
	if c.daemonNetFn != nil {
		s.NetTxBytes, s.NetRxBytes = c.daemonNetFn()
	} else {
		c.readNetworkStats(&s)
	}

	if c.sessionCountFn != nil {
		s.ActiveSessions, s.TotalSessions = c.sessionCountFn()
	}

	if c.orphanDetectFn != nil {
		s.TmuxSessions, s.OrphanedTmux = c.orphanDetectFn()
	}

	s.UptimeSeconds = int(time.Since(c.startTime).Seconds())
	s.BoundInterfaces = c.boundInterfaces
	s.EBPFEnabled = c.ebpfEnabled
	s.EBPFActive = c.ebpfActive
	s.EBPFMessage = c.ebpfMessage
	s.WebPort = c.webPort
	s.TLSEnabled = c.tlsEnabled
	s.TLSPort = c.tlsPort
	s.MCPSSEHost = c.mcpSSEHost
	s.MCPSSEPort = c.mcpSSEPort
	s.Hostname = c.hostname
	s.DaemonVersion = c.daemonVersion

	if c.sessionStatsFn != nil {
		s.SessionStats = c.sessionStatsFn()
	}
	if c.commStatsFn != nil {
		s.CommStats = c.commStatsFn()
	}
	if c.uiPromptsFn != nil {
		s.UIPrompts = c.uiPromptsFn()
	}
	if c.accessLogStatsFn != nil {
		c.accessLogStatsFn(&s)
	}

	// RTK integration stats
	if c.rtkFn != nil {
		c.rtkFn(&s)
	}

	// Episodic memory stats
	if c.supervisorLoopStatsFn != nil {
		c.supervisorLoopStatsFn(&s)
	}
	if c.burnGuardStatsFn != nil {
		c.burnGuardStatsFn(&s)
	}
	if c.traceStatsFn != nil {
		c.traceStatsFn(&s)
	}
	if c.memoryStatsFn != nil {
		c.memoryStatsFn(&s)
	}

	// BL367 — autonomous quality gate stats
	if c.autonomousStatsFn != nil {
		c.autonomousStatsFn(&s)
	}

	// BL372 — web search stats
	if c.webSearchStatsFn != nil {
		c.webSearchStatsFn(&s)
	}

	// Pre-push secret-scan counters
	if c.secretScanStatsFn != nil {
		c.secretScanStatsFn(&s)
	}

	// Worker startup averages per profile and role: the block always
	// carries empty maps, never nil ones, so its JSON shape is stable.
	s.WorkerStartup = BuildWorkerStartupStats(nil)
	if c.workerStartupStatsFn != nil {
		c.workerStartupStatsFn(&s)
	}

	// Ollama server stats (BL71) — poll every collection cycle
	if c.ollamaHost != "" {
		ollamaStats := FetchOllamaStats(c.ollamaHost)
		s.OllamaStats = &ollamaStats
	}

	c.mu.Lock()
	c.ring[c.idx] = s
	c.idx++
	if c.idx >= c.maxSize {
		c.idx = 0
		c.full = true
	}
	c.mu.Unlock()

	// Real-time broadcast to WebSocket clients
	if c.onCollect != nil {
		c.onCollect(s)
	}
}

func (c *Collector) readLoadAvg(s *SystemStats) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	parts := strings.Fields(string(data))
	if len(parts) >= 3 {
		s.CPULoadAvg1, _ = strconv.ParseFloat(parts[0], 64)
		s.CPULoadAvg5, _ = strconv.ParseFloat(parts[1], 64)
		s.CPULoadAvg15, _ = strconv.ParseFloat(parts[2], 64)
	}
}

func (c *Collector) readMemInfo(s *SystemStats) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	fields := map[string]*uint64{
		"MemTotal:":     &s.MemTotal,
		"MemAvailable:": &s.MemAvailable,
		"SwapTotal:":    &s.SwapTotal,
		"SwapFree:":     nil, // need to compute SwapUsed
	}
	var swapFree uint64
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if ptr, ok := fields[parts[0]]; ok && ptr != nil {
			val, _ := strconv.ParseUint(parts[1], 10, 64)
			*ptr = val * 1024 // kB to bytes
		}
		if parts[0] == "SwapFree:" {
			val, _ := strconv.ParseUint(parts[1], 10, 64)
			swapFree = val * 1024
		}
	}
	s.MemUsed = s.MemTotal - s.MemAvailable
	s.SwapUsed = s.SwapTotal - swapFree
}

func (c *Collector) readDiskUsage(s *SystemStats) {
	dir := c.dataDir
	if dir == "" {
		dir = "/"
	}
	// Use syscall.Statfs via os
	var stat syscallStatfs
	if err := statfs(dir, &stat); err != nil {
		return
	}
	s.DiskTotal = stat.Blocks * stat.Bsize
	s.DiskFree = stat.Bavail * stat.Bsize
	s.DiskUsed = s.DiskTotal - s.DiskFree
}

func (c *Collector) readGPU(s *SystemStats) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		// No nvidia-smi at all — leave everything empty, no error. This is
		// the normal "no NVIDIA GPU on this host" case.
		return
	}
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=name,temperature.gpu,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// nvidia-smi exists but failed — e.g. "Failed to initialize NVML:
		// Driver/library version mismatch" after an unattended driver update
		// with no reboot yet. Surface the real reason rather than silently
		// looking identical to "no GPU present".
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			s.GPUError = msg
		} else {
			s.GPUError = err.Error()
		}
		return
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ", ")
	if len(parts) >= 5 {
		s.GPUName = strings.TrimSpace(parts[0])
		s.GPUTemp, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		s.GPUUtilPct, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
		s.GPUMemUsedMB, _ = strconv.Atoi(strings.TrimSpace(parts[3]))
		s.GPUMemTotalMB, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
	}
	// Could add rocm-smi support here for AMD GPUs
}

func (c *Collector) readNetworkStats(s *SystemStats) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") || strings.HasPrefix(line, "Inter") || strings.HasPrefix(line, " face") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 10 {
			continue
		}
		// parts[0] is "iface:", parts[1] is rx_bytes, parts[9] is tx_bytes
		rx, _ := strconv.ParseUint(parts[1], 10, 64)
		tx, _ := strconv.ParseUint(parts[9], 10, 64)
		s.NetRxBytes += rx
		s.NetTxBytes += tx
	}
}

func (c *Collector) readProcessStats(s *SystemStats) {
	pid := os.Getpid()
	// RSS from /proc/self/statm (in pages)
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 2 {
			rssPages, _ := strconv.ParseUint(parts[1], 10, 64)
			s.DaemonRSSBytes = rssPages * 4096 // assuming 4KB pages
		}
	}
	// Open FDs
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err == nil {
		s.OpenFDs = len(entries)
	}
}
