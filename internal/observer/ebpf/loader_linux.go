//go:build linux

// BL173 task 1 — actual kprobe attach loader. Pre-v5.0.0 the
// `linuxKprobeProbe` constructor in probe_linux.go returned a noop
// with "not yet implemented"; this file wires bpf2go's generated
// loader (loadNetprobeObjects) plus the four kprobes
// (tcp/udp_sendmsg + tcp/udp_recvmsg-return) into a working probe.
//
// Like the v1 stats tracer (BL181), we pre-load kernel BTF via
// btf.LoadKernelSpec so the loader doesn't read /proc/self/mem
// (no CAP_SYS_PTRACE needed).
//
// Failure to attach any single kprobe is non-fatal — partial mode
// is still useful (e.g. tx-only). The Loaded() flag stays true as
// long as the program loaded; Read() iterates whatever maps are
// populated.

package ebpf

import (
	"fmt"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

// realLinuxKprobeProbe is the working implementation. It owns the
// loaded `netprobeObjects` + the kprobe links so Close detaches
// cleanly.
type realLinuxKprobeProbe struct {
	mu     sync.Mutex
	objs   *netprobeObjects
	links  []link.Link
	closed bool
}

func (r *realLinuxKprobeProbe) Loaded() bool { return !r.closed && r.objs != nil }

func (r *realLinuxKprobeProbe) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	for _, l := range r.links {
		_ = l.Close()
	}
	r.links = nil
	if r.objs != nil {
		_ = r.objs.Close()
		r.objs = nil
	}
	return nil
}

// Read returns one ProcessNet per pid that has either rx or tx
// bytes recorded in the eBPF maps. Cheap enough to run every
// observer tick.
func (r *realLinuxKprobeProbe) Read() []ProcessNet {
	r.mu.Lock()
	objs := r.objs
	r.mu.Unlock()
	if objs == nil {
		return nil
	}
	per := map[uint32]*ProcessNet{}
	collect := func(m *ebpf.Map, isTx bool) {
		if m == nil {
			return
		}
		var k uint32
		var v uint64
		it := m.Iterate()
		for it.Next(&k, &v) {
			pn, ok := per[k]
			if !ok {
				pn = &ProcessNet{PID: int(k)}
				per[k] = pn
			}
			if isTx {
				pn.TxBytes = v
			} else {
				pn.RxBytes = v
			}
		}
	}
	collect(objs.BytesRx, false)
	collect(objs.BytesTx, true)
	if len(per) == 0 {
		return nil
	}
	out := make([]ProcessNet, 0, len(per))
	for _, pn := range per {
		out = append(out, *pn)
	}
	return out
}

// loadAndAttach is the constructor that the probe_linux.go entry
// point delegates to once CAP_BPF + generated objects are present.
// On any error returns a NoopProbe with the reason — the daemon
// keeps running, just without per-pid net telemetry.
func loadAndAttach() (NetProbe, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return NewNoopProbe(fmt.Sprintf("ebpf rlimit.RemoveMemlock: %v", err)), nil
	}

	spec, err := loadNetprobe()
	if err != nil {
		return NewNoopProbe(fmt.Sprintf("loadNetprobe: %v", err)), nil
	}

	var collOpts ebpf.CollectionOptions
	if kspec, kerr := btf.LoadKernelSpec(); kerr == nil && kspec != nil {
		collOpts.Programs.KernelTypes = kspec
	}

	objs := &netprobeObjects{}
	if err := spec.LoadAndAssign(objs, &collOpts); err != nil {
		return NewNoopProbe(fmt.Sprintf("LoadAndAssign: %v", err)), nil
	}

	probe := &realLinuxKprobeProbe{objs: objs}

	// Attach each kprobe. Failures are logged in the noop fallback
	// path — but here we keep the partial set alive (tx-only is
	// still useful when only tcp_sendmsg attaches).
	attach := func(sym string, prog *ebpf.Program, ret bool) error {
		if prog == nil {
			return fmt.Errorf("nil program for %s", sym)
		}
		var l link.Link
		var err error
		if ret {
			l, err = link.Kretprobe(sym, prog, nil)
		} else {
			l, err = link.Kprobe(sym, prog, nil)
		}
		if err != nil {
			return err
		}
		probe.links = append(probe.links, l)
		return nil
	}

	var anyAttached bool
	if err := attach("tcp_sendmsg", objs.KprobeTcpSendmsg, false); err == nil {
		anyAttached = true
	}
	if err := attach("udp_sendmsg", objs.KprobeUdpSendmsg, false); err == nil {
		anyAttached = true
	}
	if err := attach("tcp_recvmsg", objs.KretprobeTcpRecvmsg, true); err == nil {
		anyAttached = true
	}
	if err := attach("udp_recvmsg", objs.KretprobeUdpRecvmsg, true); err == nil {
		anyAttached = true
	}
	// BL180 Phase 2 (v5.13.0) — new conn-attribution probes. Failures
	// here are non-fatal: byte counters above stay live; conn attr is
	// best-effort and degrades to procfs scan if the kernel doesn't
	// have a tcp_connect symbol or the verifier rejects.
	_ = attach("tcp_connect", objs.KprobeTcpConnect, false)
	_ = attach("inet_csk_accept", objs.KretprobeInetCskAccept, true)

	if !anyAttached {
		_ = probe.Close()
		return NewNoopProbe("no kprobes attached — kernel may have renamed tcp_sendmsg/udp_sendmsg or rejected verifier"), nil
	}
	return probe, nil
}

// ConnAttribution (BL180 Phase 2 v5.13.0) is one row from the
// kernel-side conn_attribution map. Sock is the kernel struct sock *
// pointer cast to uint64 — opaque to userspace; a future patch may
// resolve it back to the 4-tuple via tcp_connect kfunc.
type ConnAttribution struct {
	Sock  uint64
	PID   uint32
	TsNs  uint64
}

// ReadConnAttribution (BL180 Phase 2 v5.13.0) iterates the
// conn_attribution LRU map and returns one row per entry. Cheap
// enough to run every observer tick; the LRU eviction policy keeps
// the map bounded under heavy connection churn.
//
// PruneOlderThan walks the map and deletes entries with TsNs older
// than (now - ttl). Combined with the LRU map type, this gives both
// memory and freshness guarantees.
func (r *realLinuxKprobeProbe) ReadConnAttribution() []ConnAttribution {
	r.mu.Lock()
	objs := r.objs
	r.mu.Unlock()
	if objs == nil || objs.ConnAttribution == nil {
		return nil
	}
	type connAttrV struct {
		PID  uint32
		Pad  uint32
		TsNs uint64
	}
	var k uint64
	var v connAttrV
	out := make([]ConnAttribution, 0, 64)
	it := objs.ConnAttribution.Iterate()
	for it.Next(&k, &v) {
		out = append(out, ConnAttribution{Sock: k, PID: v.PID, TsNs: v.TsNs})
	}
	return out
}

// PruneConnAttribution (BL180 Phase 2 v5.13.0) deletes entries older
// than ttlNs nanoseconds. olderThanNs is typically (current_ktime_ns
// - 60_000_000_000) for a 60s TTL; the kernel-side LRU still bounds
// memory but the userspace pruner keeps the visible set fresh so
// stale (PID, sock) pairs don't outlive their conn.
//
// Returns the number of entries deleted.
func (r *realLinuxKprobeProbe) PruneConnAttribution(olderThanNs uint64) int {
	r.mu.Lock()
	objs := r.objs
	r.mu.Unlock()
	if objs == nil || objs.ConnAttribution == nil {
		return 0
	}
	type connAttrV struct {
		PID  uint32
		Pad  uint32
		TsNs uint64
	}
	var k uint64
	var v connAttrV
	stale := make([]uint64, 0, 16)
	it := objs.ConnAttribution.Iterate()
	for it.Next(&k, &v) {
		if v.TsNs < olderThanNs {
			stale = append(stale, k)
		}
	}
	for _, key := range stale {
		_ = objs.ConnAttribution.Delete(&key)
	}
	return len(stale)
}
