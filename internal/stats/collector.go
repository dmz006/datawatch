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
	GPUName       string  `json:"gpu_name,omitempty"`
	GPUTemp       int     `json:"gpu_temp,omitempty"`       // Celsius
	GPUUtilPct    int     `json:"gpu_util_pct,omitempty"`   // 0-100
	GPUMemUsedMB  int     `json:"gpu_mem_used_mb,omitempty"`
	GPUMemTotalMB int     `json:"gpu_mem_total_mb,omitempty"`

	// Process
	DaemonRSSBytes uint64 `json:"daemon_rss_bytes"`
	Goroutines     int    `json:"goroutines"`
	OpenFDs        int    `json:"open_fds"`

	// Sessions (filled externally by the server)
	ActiveSessions int `json:"active_sessions"`
	TotalSessions  int `json:"total_sessions"`

	// Tmux
	TmuxSessions    int      `json:"tmux_sessions"`              // total tmux sessions with cs- prefix
	OrphanedTmux    []string `json:"orphaned_tmux,omitempty"`    // tmux sessions with no matching datawatch session
	UptimeSeconds   int      `json:"uptime_seconds"`

	// eBPF status
	EBPFEnabled  bool   `json:"ebpf_enabled"`
	EBPFActive   bool   `json:"ebpf_active"`    // true if BPF programs are loaded
	EBPFMessage  string `json:"ebpf_message,omitempty"` // status/warning message

	// Network
	BoundInterfaces []string `json:"bound_interfaces,omitempty"`
	NetRxBytes      uint64   `json:"net_rx_bytes"`  // total received bytes (all interfaces)
	NetTxBytes      uint64   `json:"net_tx_bytes"`  // total transmitted bytes

	// Server interfaces (for infrastructure card)
	WebPort     int    `json:"web_port,omitempty"`
	TLSEnabled  bool   `json:"tls_enabled,omitempty"`
	TLSPort     int    `json:"tls_port,omitempty"`
	MCPSSEHost  string `json:"mcp_sse_host,omitempty"`
	MCPSSEPort  int    `json:"mcp_sse_port,omitempty"`

	// RTK (Rust Token Killer) integration stats
	RTKInstalled    bool    `json:"rtk_installed,omitempty"`
	RTKVersion      string  `json:"rtk_version,omitempty"`
	RTKHooksActive  bool    `json:"rtk_hooks_active,omitempty"`
	RTKTotalSaved   int     `json:"rtk_total_saved,omitempty"`     // total tokens saved
	RTKAvgSavings   float64 `json:"rtk_avg_savings_pct,omitempty"` // average savings percentage
	RTKTotalCmds    int     `json:"rtk_total_commands,omitempty"`

	// Per-session stats (filled by orphan detect callback)
	SessionStats []SessionStat `json:"session_stats,omitempty"`

	// Communication channel stats
	CommStats []CommChannelStat `json:"comm_stats,omitempty"`
}

// CommChannelStat holds detailed stats for a communication channel or LLM backend.
type CommChannelStat struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // "messaging", "llm", "infra"
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
type SessionStat struct {
	SessionID  string  `json:"session_id"`
	Name       string  `json:"name"`
	Backend    string  `json:"backend"`
	State      string  `json:"state"`
	PanePID    int     `json:"pane_pid"`
	RSSBytes   uint64  `json:"rss_bytes"`       // resident set size of process tree
	CPUPercent float64 `json:"cpu_percent"`
	Uptime     string  `json:"uptime"`           // elapsed time
	NetTxBytes uint64  `json:"net_tx_bytes"`     // per-session TCP TX (eBPF, 0 if disabled)
	NetRxBytes uint64  `json:"net_rx_bytes"`     // per-session TCP RX (eBPF, 0 if disabled)
	// RTK token savings for this session's project
	RTKSavedTokens int     `json:"rtk_saved_tokens,omitempty"`
	RTKSavingsPct  float64 `json:"rtk_savings_pct,omitempty"`
	RTKCommands    int     `json:"rtk_commands,omitempty"`
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

	startTime    time.Time
	ebpfEnabled  bool
	ebpfActive   bool
	ebpfMessage  string

	// daemonNetFn returns (tx, rx) bytes for the daemon process tree via eBPF
	daemonNetFn func() (uint64, uint64)

	// rtkFn populates RTK fields on a stats snapshot
	rtkFn func(*SystemStats)

	// Server interface config
	webPort    int
	tlsEnabled bool
	tlsPort    int
	mcpSSEHost string
	mcpSSEPort int
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

// SetDaemonNetFunc sets a callback that returns per-process (tx, rx) bytes for the daemon.
func (c *Collector) SetDaemonNetFunc(fn func() (uint64, uint64)) {
	c.daemonNetFn = fn
}

// SetRTKFunc sets a callback that populates RTK fields on each stats snapshot.
func (c *Collector) SetRTKFunc(fn func(*SystemStats)) {
	c.rtkFn = fn
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
		Timestamp: time.Now(),
		CPUCores:  runtime.NumCPU(),
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

	if c.sessionStatsFn != nil {
		s.SessionStats = c.sessionStatsFn()
	}
	if c.commStatsFn != nil {
		s.CommStats = c.commStatsFn()
	}

	// RTK integration stats
	if c.rtkFn != nil {
		c.rtkFn(&s)
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
	// Try nvidia-smi first
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,temperature.gpu,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), ", ")
		if len(parts) >= 5 {
			s.GPUName = strings.TrimSpace(parts[0])
			s.GPUTemp, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			s.GPUUtilPct, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
			s.GPUMemUsedMB, _ = strconv.Atoi(strings.TrimSpace(parts[3]))
			s.GPUMemTotalMB, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		}
		return
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
