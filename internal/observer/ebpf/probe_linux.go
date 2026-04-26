//go:build linux

package ebpf

import (
	"fmt"
	"os"
	"sync"
)

// NewNetProbe attempts to attach real kprobes on Linux. Without the
// bpf2go-generated objects (the netprobeObjects + loadNetprobeObjects
// symbols are emitted by `make ebpf-gen`), this falls back to a
// noop probe with a clear reason so the operator sees what's missing.
//
// The actual attach path is wired in linuxKprobeProbe.attach() once
// the generated code lands. Until then this file is a clean,
// degrade-safe stub.
func NewNetProbe() (NetProbe, error) {
	if !hasCapBPF() {
		return NewNoopProbe("CAP_BPF not granted on this binary — run `datawatch setup ebpf`"), nil
	}
	if !generatedAvailable() {
		return NewNoopProbe("eBPF objects not pre-generated — run `make ebpf-gen` and rebuild"), nil
	}
	// Real attach lands when bpf2go output is present. See
	// netprobe.bpf.c + the //go:generate directive in netprobe.go.
	return newLinuxKprobeProbe()
}

// hasCapBPF reads /proc/self/status and inspects CapEff bit 39
// (CAP_BPF). The same probe lives in internal/observer/cap_linux.go
// for the StatsResponse v2 host.ebpf.capability field; we duplicate
// the read here to avoid the import cycle.
func hasCapBPF() bool {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	const marker = "CapEff:\t"
	idx := indexOf(string(data), marker)
	if idx < 0 {
		return false
	}
	tail := string(data[idx+len(marker):])
	end := indexOf(tail, "\n")
	if end < 0 {
		return false
	}
	hex := tail[:end]
	bits, err := parseCapHex(hex)
	if err != nil {
		return false
	}
	return bits&(1<<39) != 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func parseCapHex(s string) (uint64, error) {
	var out uint64
	for _, c := range s {
		var v uint64
		switch {
		case c >= '0' && c <= '9':
			v = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			v = uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = uint64(c-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex char %q", c)
		}
		out = (out << 4) | v
	}
	return out, nil
}

// generatedAvailable reports whether the bpf2go-emitted objects are
// linked into the binary. v4.8.0 committed the amd64 artifacts;
// v4.8.22 added arm64. With both arches present, the symbol always
// exists at compile time on linux/amd64+arm64.
var generatedAvailable = func() bool { return true }

// newLinuxKprobeProbe delegates to the real loader (loader_linux.go).
// Kept as a small adapter so tests in this file can swap in a mock
// without touching loader_linux.go.
func newLinuxKprobeProbe() (NetProbe, error) {
	return loadAndAttach()
}

// Legacy stub kept for the test file's import; unused by NewNetProbe.
type linuxKprobeProbe struct {
	mu     sync.Mutex
	closed bool
}

func (l *linuxKprobeProbe) Read() []ProcessNet { return nil }
func (l *linuxKprobeProbe) Loaded() bool       { return false }
func (l *linuxKprobeProbe) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}
