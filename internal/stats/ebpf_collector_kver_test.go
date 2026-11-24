//go:build linux

package stats

import (
	"testing"

	"github.com/cilium/ebpf"
)

// TestLoadNetTrackSpec_KprobesCarryKernelVersion: a kprobe program without a
// version makes the cilium library read /proc/self/mem, which a daemon with
// file capabilities (not dumpable) is refused.
func TestLoadNetTrackSpec_KprobesCarryKernelVersion(t *testing.T) {
	spec, err := loadNetTrackSpec()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for name, ps := range spec.Programs {
		if ps.Type != ebpf.Kprobe {
			continue
		}
		n++
		if ps.KernelVersion == 0 || ps.KernelVersion == 0xFFFFFFFE {
			t.Errorf("program %s: KernelVersion=%#x", name, ps.KernelVersion)
		}
	}
	if n == 0 {
		t.Fatal("no kprobe programs in net_track.o")
	}
}
