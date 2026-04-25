// BL171 (S9) — default in-process collector. Builds a
// StatsResponse v2 from /proc + existing session / backend state on
// each tick. Pluggable via fn-indirections so tests and remote
// shapes can swap in fakes without touching /proc.

package observer

import (
	"context"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/observer/ebpf"
)

// SessionCountsFn returns the current session counts: total,
// running, waiting, rateLimited, per_backend.
type SessionCountsFn func() (total, running, waiting, rateLimited int, perBackend map[string]int)

// BackendHealthFn returns health for each registered LLM backend.
type BackendHealthFn func() []Backend

// ClusterNodesFn returns the current cluster.nodes snapshot. Wired
// from cmd/datawatch-stats Shape C with the K8sMetricsScraper output;
// nil on Shapes A/B → cluster.nodes stays empty in the snapshot.
type ClusterNodesFn func() []ClusterNode

// Collector is the default in-process collector. Holds the last
// snapshot; Latest() is O(1).
type Collector struct {
	cfg           Config
	shape         string
	startUnix     int64

	sessCounts    SessionCountsFn
	backendHealth BackendHealthFn
	clusterNodes  ClusterNodesFn

	mu     sync.RWMutex
	latest *StatsResponse

	stopCh chan struct{}

	// BL173 — eBPF kprobe loader. Lazy-init on first collect tick when
	// EBPFEnabled is on; degrades to noop when not loadable.
	ebpfMu    sync.Mutex
	ebpfProbe ebpf.NetProbe
}

// NewCollector returns a Collector with defaults filled in.
func NewCollector(cfg Config) *Collector {
	if cfg.TickIntervalMs <= 0 {
		cfg.TickIntervalMs = 1000
	}
	if cfg.ProcessTree.TopNBroadcast <= 0 {
		cfg.ProcessTree.TopNBroadcast = 200
	}
	return &Collector{
		cfg:       cfg,
		shape:     "plugin",
		startUnix: time.Now().Unix(),
		stopCh:    make(chan struct{}),
	}
}

// SetSessionCountsFn wires the daemon's session state.
func (c *Collector) SetSessionCountsFn(fn SessionCountsFn) { c.sessCounts = fn }

// SetBackendHealthFn wires per-backend health probes.
func (c *Collector) SetBackendHealthFn(fn BackendHealthFn) { c.backendHealth = fn }

// SetClusterNodesFn wires the Shape C k8s-metrics scraper output (or
// any other source) into snap.Cluster.Nodes. nil clears.
func (c *Collector) SetClusterNodesFn(fn ClusterNodesFn) { c.clusterNodes = fn }

// Start kicks a background goroutine that collects on every tick
// until Stop is called. Also runs one synchronous collection so
// Latest() isn't nil right after Start.
func (c *Collector) Start(ctx context.Context) {
	c.tick()
	go func() {
		t := time.NewTicker(time.Duration(c.cfg.TickIntervalMs) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-t.C:
				c.tick()
			}
		}
	}()
}

// Stop signals the background loop to exit.
func (c *Collector) Stop() {
	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}
}

// Latest returns the most recent snapshot. Never nil once Start has
// completed.
func (c *Collector) Latest() *StatsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// Config returns the active config.
func (c *Collector) Config() Config {
	return c.cfg
}

// SetConfig hot-swaps the config. Tick cadence change takes effect
// on the next tick; classifier rules are used immediately.
func (c *Collector) SetConfig(cfg Config) {
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

// ebpfProbeStatus lazily initialises the kprobe loader and returns
// whether real probes are attached + a human-readable message for
// the host.ebpf.message field. Always succeeds (probe degrades on
// any failure).
func (c *Collector) ebpfProbeStatus() (loaded bool, message string) {
	c.ebpfMu.Lock()
	defer c.ebpfMu.Unlock()
	if c.ebpfProbe == nil {
		p, err := ebpf.NewNetProbe()
		if err != nil || p == nil {
			c.ebpfProbe = ebpf.NewNoopProbe("probe init failed; falling back to /proc-only")
		} else {
			c.ebpfProbe = p
		}
	}
	if c.ebpfProbe.Loaded() {
		return true, "kprobes attached — per-process net live"
	}
	if r, ok := c.ebpfProbe.(interface{ Reason() string }); ok && r.Reason() != "" {
		return false, r.Reason()
	}
	return false, "kprobes not attached; running /proc-only"
}

// tick does one synchronous collection and stores it as Latest.
func (c *Collector) tick() {
	snap := c.collect()
	c.mu.Lock()
	c.latest = snap
	c.mu.Unlock()
}

func (c *Collector) collect() *StatsResponse {
	now := time.Now()
	hostName, _ := os.Hostname()
	os_, kern, arch := readHostFields()
	if os_ == "" {
		os_ = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	l1, l5, l15 := readLoadavg()

	procs, _ := walkProc()
	tree, totalProcCPU := buildTree(procs, c.cfg.ProcessTree.TopNBroadcast, c.cfg.ProcessTree.IncludeKthreads)
	envelopes, _ := classify(procs, c.cfg.Envelopes)

	mem := readMemInfo()
	disks := readDiskUsage()

	cpuPct := cpuOverall(totalProcCPU, runtimeNumCPU())

	// Sessions + backends from injected fns (may be nil in tests).
	var sessions Sessions
	if c.sessCounts != nil {
		t, r, w, rl, perB := c.sessCounts()
		sessions = Sessions{
			Total: t, Running: r, Waiting: w, RateLimited: rl,
			PerBackend: perB,
		}
	}
	var backends []Backend
	if c.backendHealth != nil {
		backends = c.backendHealth()
	}

	uptime := int64(now.Unix() - c.startUnix)
	if hostUp := readUptimeSeconds(); hostUp > 0 {
		uptime = hostUp
	}

	// v4.1.1 — surface eBPF status honestly. Configured = operator
	// opted in. Capability = setcap CAP_BPF granted on the running
	// binary. KprobesLoaded = the kprobe/tcp_* programs are
	// attached. v4.1.x ships configured+capability checks; the
	// loader itself lands in Sprint S12 alongside the cluster
	// container, so KprobesLoaded stays false until then.
	ebpfStatus := EBPFStatus{
		Configured: c.cfg.EBPFEnabled == "true" || c.cfg.EBPFEnabled == "auto",
	}
	if ebpfStatus.Configured {
		ebpfStatus.Capability = probeBPFCapability()
		// BL173 — ask the loader directly. The Linux build attempts
		// the real attach when CAP_BPF + bpf2go output are present;
		// otherwise it returns a noop probe with a Reason.
		probe, msg := c.ebpfProbeStatus()
		ebpfStatus.KprobesLoaded = probe
		ebpfStatus.Message = msg
	} else {
		ebpfStatus.Message = "off — set observer.ebpf_enabled=true to enable"
	}

	snap := &StatsResponse{
		V: 2,
		Host: Host{
			Name:          hostName,
			UptimeSeconds: uptime,
			OS:            os_,
			Kernel:        kern,
			Arch:          arch,
			Shape:         c.shape,
			EBPF:          ebpfStatus,
		},
		CPU: CPU{
			Pct:   round2(cpuPct),
			Cores: runtimeNumCPU(),
			Load1: l1, Load5: l5, Load15: l15,
		},
		Mem:  mem,
		Disk: disks,
		Net:  Net{}, // Shape C populates per-process; host-level rate TBD
		Sessions: sessions,
		Backends: backends,
		Processes: Processes{
			SampledAtUnixMs: now.UnixMilli(),
			TotalTracked:    len(procs),
			Tree:            tree,
		},
		Envelopes:       envelopes,
		SampledAtUnixMs: now.UnixMilli(),
	}

	// v1 aliases so old clients still parse.
	snap.CPUPctV1 = snap.CPU.Pct
	snap.MemPctV1 = snap.Mem.Pct
	if len(snap.Disk) > 0 {
		snap.DiskPctV1 = snap.Disk[0].Pct
	}
	if len(snap.GPU) > 0 {
		snap.GPUPctV1 = snap.GPU[0].UtilPct
	}
	snap.SessionsTotal = snap.Sessions.Total
	snap.SessionsRun = snap.Sessions.Running
	snap.UptimeSeconds = snap.Host.UptimeSeconds
	// BL173 — Shape C cluster.nodes from the K8sMetricsScraper
	// (or any future source). Nil-safe; empty slice means "no nodes
	// reported", and the PWA Cluster nodes card hides itself.
	if c.clusterNodes != nil {
		nodes := c.clusterNodes()
		if len(nodes) > 0 {
			snap.Cluster = &Cluster{Nodes: nodes}
		}
	}
	return snap
}

// cpuOverall approximates the host CPU pct by summing per-process
// CPU% and dividing by the number of cores. For a quiet box this
// under-counts kernel time, but it's a useful operator signal and
// avoids the /proc/stat delta book-keeping at this stage.
func cpuOverall(sumProcCPU float64, cores int) float64 {
	if cores <= 0 {
		cores = 1
	}
	pct := sumProcCPU / float64(cores)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// buildTree turns a flat process list into a nested tree rooted at
// pid 1. Only the top-N processes by CPU% are expanded with their
// children; the rest are elided from the tree payload to keep WS
// broadcasts small.
func buildTree(recs []ProcRecord, topN int, includeKthreads bool) ([]ProcessNode, float64) {
	var totalCPU float64
	byPID := map[int]*ProcRecord{}
	for i := range recs {
		r := &recs[i]
		if !includeKthreads && (r.PPID == 2 || r.PID == 2) {
			continue
		}
		byPID[r.PID] = r
		totalCPU += r.CPUPct
	}
	// Pick the top-N by CPU desc.
	type pair struct {
		pid int
		cpu float64
	}
	ranked := make([]pair, 0, len(byPID))
	for pid, r := range byPID {
		ranked = append(ranked, pair{pid, r.CPUPct})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].cpu > ranked[j].cpu })
	if topN > 0 && len(ranked) > topN {
		ranked = ranked[:topN]
	}
	keep := map[int]bool{}
	for _, p := range ranked {
		// Keep the process AND every ancestor so the tree is connected.
		pid := p.pid
		for pid > 1 {
			if keep[pid] {
				break
			}
			keep[pid] = true
			if r, ok := byPID[pid]; ok {
				pid = r.PPID
			} else {
				break
			}
		}
	}
	// Build the tree.
	children := map[int][]int{}
	for pid, r := range byPID {
		if !keep[pid] {
			continue
		}
		children[r.PPID] = append(children[r.PPID], pid)
	}
	var build func(pid int) ProcessNode
	build = func(pid int) ProcessNode {
		r := byPID[pid]
		node := ProcessNode{
			PID: r.PID, PPID: r.PPID, Comm: r.Comm, Cmdline: r.Cmdline,
			CPUPct: round2(r.CPUPct), RSSBytes: r.RSSBytes,
			Threads: r.Threads, FDs: r.FDs,
			Cgroup: r.Cgroup, ContainerID: r.ContainerID,
		}
		for _, c := range children[pid] {
			node.Children = append(node.Children, build(c))
		}
		return node
	}
	// Roots: keep-marked processes whose parent is NOT in keep
	// (typically PPID=1 or pid-namespace root).
	var roots []ProcessNode
	for pid := range keep {
		r := byPID[pid]
		if r == nil {
			continue
		}
		if _, parentKept := keep[r.PPID]; parentKept {
			continue
		}
		roots = append(roots, build(pid))
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].CPUPct > roots[j].CPUPct })
	return roots, totalCPU
}

// readDiskUsage returns a short list of mounted filesystems with
// their usage. Linux-specific statfs; non-linux returns empty.
func readDiskUsage() []Disk {
	return readDiskUsagePlatform()
}
