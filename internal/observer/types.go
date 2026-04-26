// Package observer (BL171, Sprint S9 → v4.1.0) implements the
// unified stats / process-tree / sub-process monitoring subsystem.
// Produces StatsResponse v2: structured host/cpu/mem/disk/gpu/net/
// sessions/backends/processes/envelopes payload with the v1 flat
// fields preserved as aliases for back-compat.
//
// Design doc: docs/plans/2026-04-22-bl171-datawatch-observer.md.
// Three shapes share this contract:
//   Shape A  — in-process plugin (default, lives here in-tree)
//   Shape B  — standalone datawatch-stats daemon (Sprint S11)
//   Shape C  — cluster container with eBPF + DCGM (Sprint S12)

package observer

import "time"

// StatsResponse is the v2 wire contract served by GET /api/stats and
// broadcast over MsgStats. Every field is stable; additive changes
// are allowed, breaking changes require a v3 bump.
type StatsResponse struct {
	V           int         `json:"v"` // schema version, always 2
	Host        Host        `json:"host"`
	CPU         CPU         `json:"cpu"`
	Mem         Mem         `json:"mem"`
	Disk        []Disk      `json:"disk,omitempty"`
	GPU         []GPU       `json:"gpu,omitempty"`
	Net         Net         `json:"net"`
	Sessions    Sessions    `json:"sessions"`
	Backends    []Backend   `json:"backends,omitempty"`
	Processes   Processes   `json:"processes,omitempty"`
	Envelopes   []Envelope  `json:"envelopes,omitempty"`
	Cluster     *Cluster    `json:"cluster,omitempty"` // populated by Shape C only
	Peers       []Peer      `json:"peers,omitempty"`   // aggregator view only

	// v1 back-compat aliases — flat scalars that existed on the old
	// StatsDto. New consumers read the structured objects; old ones
	// keep working unchanged.
	CPUPctV1      float64 `json:"cpu_pct,omitempty"`
	MemPctV1      float64 `json:"mem_pct,omitempty"`
	DiskPctV1     float64 `json:"disk_pct,omitempty"`
	GPUPctV1      float64 `json:"gpu_pct,omitempty"`
	SessionsTotal int     `json:"sessions_total,omitempty"`
	SessionsRun   int     `json:"sessions_running,omitempty"`
	UptimeSeconds int64   `json:"uptime_seconds,omitempty"`

	SampledAtUnixMs int64 `json:"sampled_at_unix_ms"`
}

type Host struct {
	Name          string `json:"name"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	OS            string `json:"os"`
	Kernel        string `json:"kernel,omitempty"`
	Arch          string `json:"arch,omitempty"`
	// Shape is "plugin" (in-process), "daemon" (standalone), or
	// "cluster" (containerized). Lets consumers know the origin.
	Shape string `json:"shape"`
	// EBPF reports the eBPF status — configured (operator opted in
	// via observer.ebpf_enabled or `datawatch setup ebpf`),
	// capability granted on the binary, and whether the kprobes
	// actually loaded. v4.1.0 sets configured + capability but
	// kprobes_loaded stays false until the kprobe loader ships in
	// Sprint S12.
	EBPF EBPFStatus `json:"ebpf"`
}

// EBPFStatus surfaces the per-shape eBPF state so the PWA + mobile
// can render an honest "enabled / pending kernel probes / off" badge
// rather than a misleading boolean toggle. (v4.1.1)
type EBPFStatus struct {
	Configured     bool   `json:"configured"`
	Capability     bool   `json:"capability"`
	KprobesLoaded  bool   `json:"kprobes_loaded"`
	Message        string `json:"message,omitempty"`
}

type CPU struct {
	Pct        float64   `json:"pct"`
	Cores      int       `json:"cores"`
	Load1      float64   `json:"load1,omitempty"`
	Load5      float64   `json:"load5,omitempty"`
	Load15     float64   `json:"load15,omitempty"`
	PerCorePct []float64 `json:"per_core_pct,omitempty"`
}

type Mem struct {
	Pct            float64 `json:"pct"`
	UsedBytes      uint64  `json:"used_bytes"`
	TotalBytes     uint64  `json:"total_bytes"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes,omitempty"`
	SwapTotalBytes uint64  `json:"swap_total_bytes,omitempty"`
}

type Disk struct {
	Mount      string  `json:"mount"`
	Pct        float64 `json:"pct"`
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	FSType     string  `json:"fs_type,omitempty"`
}

type GPU struct {
	Name          string  `json:"name"`
	Vendor        string  `json:"vendor"` // nvidia | amd | intel
	UtilPct       float64 `json:"util_pct"`
	MemUsedBytes  uint64  `json:"mem_used_bytes,omitempty"`
	MemTotalBytes uint64  `json:"mem_total_bytes,omitempty"`
	PowerW        float64 `json:"power_w,omitempty"`
	TempC         float64 `json:"temp_c,omitempty"`
	ProcPIDs      []int   `json:"proc_pids,omitempty"`
}

type Net struct {
	RxBytesPerSec uint64        `json:"rx_bytes_per_sec"`
	TxBytesPerSec uint64        `json:"tx_bytes_per_sec"`
	PerProcess    []NetProc     `json:"per_process,omitempty"` // Shape C only (eBPF)
}

type NetProc struct {
	PID   int    `json:"pid"`
	Comm  string `json:"comm"`
	RxBps uint64 `json:"rx_bps"`
	TxBps uint64 `json:"tx_bps"`
}

type Sessions struct {
	Total       int            `json:"total"`
	Running     int            `json:"running"`
	Waiting     int            `json:"waiting"`
	RateLimited int            `json:"rate_limited,omitempty"`
	PerBackend  map[string]int `json:"per_backend,omitempty"`
}

type Backend struct {
	Name          string `json:"name"`
	Reachable     bool   `json:"reachable"`
	LastOkUnixMs  int64  `json:"last_ok_unix_ms,omitempty"`
	LatencyMs     int    `json:"latency_ms,omitempty"`
	ErrorMessage  string `json:"error,omitempty"`
}

// Processes is the full sub-process tree sampled per-tick. top_n
// controls how many top-CPU processes are included in each payload;
// the rest are summarised by the envelopes[] rollup.
type Processes struct {
	SampledAtUnixMs int64       `json:"sampled_at_unix_ms"`
	TotalTracked    int         `json:"total_tracked"`
	Tree            []ProcessNode `json:"tree,omitempty"`
}

// ProcessNode is one node in the process tree. CPU/RSS/etc. are
// per-process (not rolled up); envelopes[] carry the subtree rollups.
type ProcessNode struct {
	PID         int           `json:"pid"`
	PPID        int           `json:"ppid"`
	Comm        string        `json:"comm"`
	Cmdline     string        `json:"cmdline,omitempty"`
	CPUPct      float64       `json:"cpu_pct"`
	RSSBytes    uint64        `json:"rss_bytes"`
	Threads     int           `json:"threads,omitempty"`
	FDs         int           `json:"fds,omitempty"`
	StartTime   int64         `json:"start_unix_ms,omitempty"`
	Cgroup      string        `json:"cgroup,omitempty"`
	ContainerID string        `json:"container_id,omitempty"`
	GPUPct      float64       `json:"gpu_pct,omitempty"`
	GPUMemBytes uint64        `json:"gpu_mem_bytes,omitempty"`
	Children    []ProcessNode `json:"children,omitempty"`
}

// EnvelopeKind separates session envelopes from backend / container
// envelopes. Clients filter by kind.
type EnvelopeKind string

const (
	EnvelopeSession   EnvelopeKind = "session"
	EnvelopeBackend   EnvelopeKind = "backend"
	EnvelopeContainer EnvelopeKind = "container"
	EnvelopeSystem    EnvelopeKind = "system"
)

// Envelope is a logical grouping of processes with rolled-up
// metrics — the answer to "which session or backend is eating CPU?"
type Envelope struct {
	ID                 string       `json:"id"`    // stable: session:<full_id> | backend:<name>[-docker|-k8s] | container:<short_id>
	Kind               EnvelopeKind `json:"kind"`
	Label              string       `json:"label"`
	RootPID            int          `json:"root_pid,omitempty"`
	PIDs               []int        `json:"pids,omitempty"`
	CPUPct             float64      `json:"cpu_pct"`
	RSSBytes           uint64       `json:"rss_bytes"`
	Threads            int          `json:"threads,omitempty"`
	FDs                int          `json:"fds,omitempty"`
	NetRxBps           uint64       `json:"net_rx_bps,omitempty"`
	NetTxBps           uint64       `json:"net_tx_bps,omitempty"`
	GPUPct             float64      `json:"gpu_pct,omitempty"`
	GPUMemBytes        uint64       `json:"gpu_mem_bytes,omitempty"`
	ContainerID        string       `json:"container_id,omitempty"`
	Image              string       `json:"image,omitempty"`
	Pod                string       `json:"pod,omitempty"`
	Namespace          string       `json:"namespace,omitempty"`
	LastActivityUnixMs int64        `json:"last_activity_unix_ms,omitempty"`

	// Source is the federation attribution. Empty / "local" = produced
	// by this observer; "<peer-name>" = received from a Shape A/B/C
	// peer via /api/observer/peers/{name}/stats; "<primary-name>" =
	// flowed through cross-cluster federation (S14a). Roots fan
	// out child envelopes preserving Source so PWA can render a
	// `cluster: <primary-name>` group.
	Source string `json:"source,omitempty"`
}

type Cluster struct {
	Nodes []ClusterNode `json:"nodes"`
}

type ClusterNode struct {
	Name     string   `json:"name"`
	Ready    bool     `json:"ready"`
	CPUPct   float64  `json:"cpu_pct,omitempty"`
	MemPct   float64  `json:"mem_pct,omitempty"`
	PodCount int      `json:"pod_count,omitempty"`
	Pressure []string `json:"pressure,omitempty"` // memory | disk | pid | ...
}

type Peer struct {
	Name          string    `json:"name"`
	Shape         string    `json:"shape"` // daemon | cluster
	Reachable     bool      `json:"reachable"`
	LastPushUnixMs int64    `json:"last_push_unix_ms,omitempty"`
	Address       string    `json:"address,omitempty"`
	RegisteredAt  time.Time `json:"registered_at,omitempty"`
}

// Config mirrors the YAML `observer:` block. Defaults are sane; a
// zero-value Config runs the in-process plugin at 1 s with session +
// backend envelopes + docker discovery enabled.
type Config struct {
	PluginEnabled  bool           `json:"plugin_enabled"`
	TickIntervalMs int            `json:"tick_interval_ms,omitempty"`
	ProcessTree    ProcessTreeCfg `json:"process_tree,omitempty"`
	Envelopes      EnvelopesCfg   `json:"envelopes,omitempty"`
	Peers          PeersCfg       `json:"peers,omitempty"`
	Cluster        ClusterCfg     `json:"cluster,omitempty"`
	Federation     FederationCfg  `json:"federation,omitempty"`

	// EBPFEnabled controls per-process net capture via eBPF across
	// all three shapes. Values: "auto" (default — load if CAP_BPF
	// is present, silently skip otherwise), "true" (fail-boot if
	// the kernel refuses the program), "false" (never try).
	// Shape A + B pass the capability probe through when granted
	// (operator runs daemon with ambient CAP_BPF); Shape C always
	// has it. See §5 of the design doc.
	EBPFEnabled string `json:"ebpf_enabled,omitempty"`
}

type ProcessTreeCfg struct {
	Enabled         bool `json:"enabled"`
	TopNBroadcast   int  `json:"top_n_broadcast,omitempty"`
	IncludeKthreads bool `json:"include_kthreads,omitempty"`
}

type EnvelopesCfg struct {
	SessionAttribution bool                     `json:"session_attribution"`
	BackendAttribution bool                     `json:"backend_attribution"`
	DockerDiscovery    bool                     `json:"docker_discovery"`
	GPUAttribution     bool                     `json:"gpu_attribution"`
	BackendSignatures  map[string]BackendSig    `json:"backend_signatures,omitempty"`
}

type BackendSig struct {
	Exec  []string `json:"exec"`
	Track bool     `json:"track,omitempty"`
}

type PeersCfg struct {
	AllowRegister           bool   `json:"allow_register"`
	TokenRotationGraceS     int    `json:"token_ttl_rotation_grace_s,omitempty"`
	PushIntervalSeconds     int    `json:"push_interval_seconds,omitempty"`
	ListenAddr              string `json:"listen_addr,omitempty"`
}

type ClusterCfg struct {
	EBPFEnabled       bool   `json:"ebpf_enabled,omitempty"`
	K8sMetricsScrape  bool   `json:"k8s_metrics_scrape,omitempty"`
	DCGMEndpoint      string `json:"dcgm_endpoint,omitempty"`
}

// FederationCfg (S14a, v4.8.0) — turns this primary into a peer of
// another root primary, giving operators with multiple clusters one
// pane of glass. Empty ParentURL = federation off (default).
type FederationCfg struct {
	// ParentURL is the root primary base URL; e.g. "https://root:8443".
	// Empty disables federation entirely.
	ParentURL string `json:"parent_url,omitempty"`
	// PeerName is the name this primary registers as on the root.
	// Defaults to the host's name when empty.
	PeerName string `json:"peer_name,omitempty"`
	// PushIntervalSeconds between snapshot pushes to the root
	// (default 10 — the root sees aggregate, not raw process tree).
	PushIntervalSeconds int `json:"push_interval_seconds,omitempty"`
	// TokenPath persists the registration token across restarts
	// (default <data_dir>/observer/federation.token when empty).
	TokenPath string `json:"token_path,omitempty"`
	// Insecure skips TLS verify on the parent (dev / self-signed).
	Insecure bool `json:"insecure,omitempty"`
}

// DefaultConfig returns the "sane defaults" flavour — Shape A
// (plugin) enabled, 1 s tick, full envelope classification, Docker
// discovery on, no cluster/eBPF.
func DefaultConfig() Config {
	return Config{
		PluginEnabled:  true,
		TickIntervalMs: 1000,
		ProcessTree: ProcessTreeCfg{
			Enabled:       true,
			TopNBroadcast: 200,
		},
		Envelopes: EnvelopesCfg{
			SessionAttribution: true,
			BackendAttribution: true,
			DockerDiscovery:    true,
			GPUAttribution:     true,
			BackendSignatures: map[string]BackendSig{
				"claude":    {Exec: []string{"claude", "claude-code"}, Track: true},
				"ollama":    {Exec: []string{"ollama"}, Track: true},
				"openwebui": {Exec: []string{"open-webui"}, Track: true},
				"aider":     {Exec: []string{"aider"}, Track: true},
				"goose":     {Exec: []string{"goose"}, Track: true},
				"gemini":    {Exec: []string{"gemini"}, Track: true},
				"opencode":  {Exec: []string{"opencode"}, Track: true},
			},
		},
		Peers: PeersCfg{
			AllowRegister:       true,
			TokenRotationGraceS: 60,
			PushIntervalSeconds: 5,
			ListenAddr:          "0.0.0.0:9001",
		},
		EBPFEnabled: "auto",
	}
}
