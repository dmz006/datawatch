//go:build linux

package stats

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

//go:embed bpf/net_track.o
var bpfObject embed.FS

// EBPFCollector tracks per-PID TCP network bytes using eBPF kprobes.
type EBPFCollector struct {
	mu      sync.Mutex
	coll    *ebpf.Collection
	txMap   *ebpf.Map
	rxMap   *ebpf.Map
	links   []link.Link
	closed  bool
}

// NewEBPFCollector loads BPF programs and attaches kprobes.
func NewEBPFCollector() (*EBPFCollector, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock: %w", err)
	}

	// Read embedded BPF object
	data, err := bpfObject.ReadFile("bpf/net_track.o")
	if err != nil {
		return nil, fmt.Errorf("read BPF object: %w", err)
	}

	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("load BPF spec: %w", err)
	}

	// BL181: pre-load kernel BTF from /sys/kernel/btf/vmlinux so the
	// cilium/ebpf default detection path (which reads /proc/self/mem,
	// requires CAP_SYS_PTRACE) is bypassed. /sys/kernel/btf/vmlinux
	// is world-readable on every modern kernel that ships BTF, which
	// is the only place we attach kprobes anyway. If the kernel
	// doesn't ship BTF we fall back to the default detection and let
	// the original error path emit the same warning the operator
	// already knows about.
	var collOpts ebpf.CollectionOptions
	if kspec, kerr := btf.LoadKernelSpec(); kerr == nil && kspec != nil {
		collOpts.Programs.KernelTypes = kspec
	}

	coll, err := ebpf.NewCollectionWithOptions(spec, collOpts)
	if err != nil {
		return nil, fmt.Errorf("create BPF collection: %w", err)
	}

	c := &EBPFCollector{
		coll:  coll,
		txMap: coll.Maps["tx_bytes"],
		rxMap: coll.Maps["rx_bytes"],
	}

	// Attach kprobes
	if prog, ok := coll.Programs["trace_tcp_sendmsg"]; ok {
		l, err := link.Kprobe("tcp_sendmsg", prog, nil)
		if err != nil {
			coll.Close()
			return nil, fmt.Errorf("attach kprobe tcp_sendmsg: %w", err)
		}
		c.links = append(c.links, l)
	}

	if prog, ok := coll.Programs["trace_tcp_recvmsg"]; ok {
		l, err := link.Kprobe("tcp_recvmsg", prog, nil)
		if err != nil {
			// Non-fatal — TX tracking still works
			fmt.Printf("[ebpf] kprobe tcp_recvmsg failed: %v (TX-only mode)\n", err)
		} else {
			c.links = append(c.links, l)
		}
	}

	if prog, ok := coll.Programs["trace_tcp_recvmsg_ret"]; ok {
		l, err := link.Kretprobe("tcp_recvmsg", prog, nil)
		if err != nil {
			fmt.Printf("[ebpf] kretprobe tcp_recvmsg failed: %v (RX tracking unavailable)\n", err)
		} else {
			c.links = append(c.links, l)
		}
	}

	fmt.Printf("[ebpf] Attached %d probes for per-PID TCP tracking\n", len(c.links))
	fmt.Printf("[ebpf] TX map: %v, RX map: %v\n", c.txMap, c.rxMap)
	return c, nil
}

// ReadPIDBytes returns cumulative TX and RX bytes for a PID.
func (c *EBPFCollector) ReadPIDBytes(pid uint32) (tx, rx uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}

	if c.txMap != nil {
		var val uint64
		if err := c.txMap.Lookup(pid, &val); err == nil {
			tx = val
		}
	}
	if c.rxMap != nil {
		var val uint64
		if err := c.rxMap.Lookup(pid, &val); err == nil {
			rx = val
		}
	}
	return
}

// ReadPIDTreeBytes sums TX/RX for a PID and all its descendant processes.
func (c *EBPFCollector) ReadPIDTreeBytes(pid uint32) (tx, rx uint64) {
	tx, rx = c.ReadPIDBytes(pid)
	// Sum children
	out, err := exec.Command("pgrep", "-P", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var cpid uint32
		if _, err := fmt.Sscanf(line, "%d", &cpid); err == nil && cpid > 0 {
			ctxVal, crxVal := c.ReadPIDBytes(cpid)
			tx += ctxVal
			rx += crxVal
			// Recurse one more level (grandchildren)
			gout, _ := exec.Command("pgrep", "-P", fmt.Sprintf("%d", cpid)).Output()
			for _, gline := range strings.Split(strings.TrimSpace(string(gout)), "\n") {
				if gline == "" { continue }
				var gpid uint32
				if _, e := fmt.Sscanf(gline, "%d", &gpid); e == nil && gpid > 0 {
					gtx, grx := c.ReadPIDBytes(gpid)
					tx += gtx
					rx += grx
				}
			}
		}
	}
	return
}

// DumpStats returns the count of entries in BPF maps (for debugging).
func (c *EBPFCollector) DumpStats() (txEntries, rxEntries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.txMap != nil {
		var key uint32
		var val uint64
		iter := c.txMap.Iterate()
		for iter.Next(&key, &val) {
			txEntries++
		}
	}
	if c.rxMap != nil {
		var key uint32
		var val uint64
		iter := c.rxMap.Iterate()
		for iter.Next(&key, &val) {
			rxEntries++
		}
	}
	return
}

// PurgeDeadPIDs removes BPF map entries for PIDs that no longer exist.
// Returns the number of entries deleted from each map.
func (c *EBPFCollector) PurgeDeadPIDs() (txDeleted, rxDeleted int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	purge := func(m *ebpf.Map) int {
		if m == nil {
			return 0
		}
		var key uint32
		var val uint64
		var dead []uint32
		iter := m.Iterate()
		for iter.Next(&key, &val) {
			// /proc/<pid> is the cheapest liveness check available without syscalls.
			if _, err := os.Stat(fmt.Sprintf("/proc/%d", key)); err != nil {
				dead = append(dead, key)
			}
		}
		n := 0
		for _, k := range dead {
			if err := m.Delete(k); err == nil {
				n++
			}
		}
		return n
	}
	txDeleted = purge(c.txMap)
	rxDeleted = purge(c.rxMap)
	return
}

// Close detaches probes and closes maps.
func (c *EBPFCollector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, l := range c.links {
		l.Close()
	}
	if c.coll != nil {
		c.coll.Close()
	}
}

