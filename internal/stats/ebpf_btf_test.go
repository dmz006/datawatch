//go:build linux

// BL181 — verify the BTF-from-/sys/kernel/btf/vmlinux discovery
// path that NewEBPFCollector switched to (so the loader doesn't
// need CAP_SYS_PTRACE for /proc/self/mem). Skip cleanly on
// kernels that don't ship BTF.

package stats

import (
	"os"
	"testing"

	"github.com/cilium/ebpf/btf"
)

func TestKernelBTFLoadable(t *testing.T) {
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); os.IsNotExist(err) {
		t.Skip("/sys/kernel/btf/vmlinux not present on this kernel")
	}
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		t.Fatalf("btf.LoadKernelSpec: %v — BL181's fix relies on this not requiring CAP_SYS_PTRACE", err)
	}
	if spec == nil {
		t.Fatal("btf.LoadKernelSpec returned nil spec without error")
	}
	// Sanity: every BTF-shipping kernel has at least pt_regs.
	if _, err := spec.AnyTypeByName("pt_regs"); err != nil {
		t.Errorf("kernel BTF missing pt_regs (BPF won't work): %v", err)
	}
}
