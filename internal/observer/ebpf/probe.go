// Package ebpf — per-process network byte counters via kernel kprobes.
//
// BL173 task 1. Public surface:
//
//	probe, err := ebpf.NewNetProbe()
//	if err != nil { /* degrade to /proc-only */ }
//	defer probe.Close()
//	stats := probe.Read()  // map[pid]ProcessNet
//
// On Linux ≥ 5.10 with CAP_BPF + CAP_PERFMON the Linux build attempts
// to load tcp_sendmsg / tcp_recvmsg / udp_sendmsg / udp_recvmsg
// kprobes via cilium/ebpf. Failure (missing capability, missing BTF,
// older kernel, non-Linux) returns a no-op probe that reports zero
// per-process bytes — callers should treat that as "/proc-only" mode
// and not error out.
//
// The actual eBPF object is generated from netprobe.bpf.c via
// `make ebpf-gen` (runs `go generate ./internal/observer/ebpf/...`
// → bpf2go). Without pre-generated objects the linuxKprobeProbe
// degrades to noop with a one-time log line.
package ebpf

// ProcessNet is the per-pid network counter snapshot reported by Read.
// Bytes are cumulative since process start (the eBPF map keeps the
// running total), not deltas — callers compute deltas across ticks.
type ProcessNet struct {
	PID     int    `json:"pid"`
	Comm    string `json:"comm,omitempty"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// NetProbe is the interface every backend implements. Read() must be
// cheap (≤1 ms on a 1k-process box); Close() must be idempotent.
type NetProbe interface {
	// Read returns a snapshot of per-pid counters. Empty slice when
	// the probe is in noop mode — never nil.
	Read() []ProcessNet
	// Loaded reports whether real kprobes are attached. False on
	// noop / degraded mode.
	Loaded() bool
	// Close detaches the probes (idempotent).
	Close() error
}

// noopProbe is the platform-agnostic fallback used on:
//   - non-Linux hosts
//   - Linux without CAP_BPF
//   - Linux where bpf2go-generated objects are absent
//   - any verifier rejection
//
// Its sole job is to keep the wire contract honest: callers can
// always Read() without nil-checking, and the observer's
// host.ebpf.kprobes_loaded field reflects reality via Loaded().
type noopProbe struct{ reason string }

func (n *noopProbe) Read() []ProcessNet { return nil }
func (n *noopProbe) Loaded() bool        { return false }
func (n *noopProbe) Close() error        { return nil }

// Reason returns the human-readable explanation for noop mode.
// Surfaced as observer.host.ebpf.message so operators see why
// kprobes_loaded is false.
func (n *noopProbe) Reason() string { return n.reason }

// NewNoopProbe constructs a noop probe with an explanation. Used
// internally by NewNetProbe when the platform can't load real
// programs; exported for tests + for callers that explicitly want
// to opt out (cfg.EBPFEnabled == "false").
func NewNoopProbe(reason string) NetProbe {
	return &noopProbe{reason: reason}
}
