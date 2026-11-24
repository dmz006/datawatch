//go:build linux

// Probe-load regression tests. A daemon started with file capabilities
// is not dumpable, so opening /proc/self/mem is refused. The cilium
// library opens it to detect the kernel version for every kprobe
// program whose spec carries no version. These tests pin that the
// loader supplies the version itself and reports a clear message when
// the library still returns a permission error.

package ebpf

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	cebpf "github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

const nonDumpableChildEnv = "DATAWATCH_EBPF_NONDUMPABLE_CHILD"

// kprobeSpecs returns the program specs of Kprobe type in the prepared spec.
func kprobeSpecs(t *testing.T) map[string]*cebpf.ProgramSpec {
	t.Helper()
	spec, err := preparedNetprobeSpec()
	if err != nil {
		t.Fatalf("preparedNetprobeSpec: %v", err)
	}
	out := map[string]*cebpf.ProgramSpec{}
	for name, ps := range spec.Programs {
		if ps.Type == cebpf.Kprobe {
			out[name] = ps
		}
	}
	if len(out) == 0 {
		t.Fatal("no kprobe programs in the embedded spec")
	}
	return out
}

// TestPreparedSpec_KprobesCarryKernelVersion: every kprobe program has an
// explicit kernel version, so the library never needs /proc/self/mem.
func TestPreparedSpec_KprobesCarryKernelVersion(t *testing.T) {
	for name, ps := range kprobeSpecs(t) {
		if ps.KernelVersion == 0 || ps.KernelVersion == 0xFFFFFFFE {
			t.Errorf("program %s: KernelVersion=%#x, the library would read /proc/self/mem to detect it", name, ps.KernelVersion)
		}
	}
}

// TestNonDumpableProcess_NoKernelVersionDetection reproduces the defect in a
// child process made non-dumpable (what file capabilities do to the daemon):
// loading a kprobe program must not fail in kernel version detection. The
// child has no CAP_BPF in CI, so the load may still fail later; only the
// detection error is the defect.
func TestNonDumpableProcess_NoKernelVersionDetection(t *testing.T) {
	if os.Getenv(nonDumpableChildEnv) == "1" {
		nonDumpableChild(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNonDumpableProcess_NoKernelVersionDetection$", "-test.v")
	cmd.Env = append(os.Environ(), nonDumpableChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "SKIP") {
		t.Skipf("child skipped: %s", out)
	}
}

func nonDumpableChild(t *testing.T) {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		t.Skipf("cannot clear dumpable: %v", err)
	}
	if f, err := os.Open("/proc/self/mem"); err == nil {
		_ = f.Close()
		t.Skip("/proc/self/mem is readable although non-dumpable on this host; defect not reproducible here")
	}
	for name, ps := range kprobeSpecs(t) {
		_, err := cebpf.NewProgram(ps)
		if err != nil && (strings.Contains(err.Error(), "detecting kernel version") || strings.Contains(err.Error(), "/proc/self/mem")) {
			t.Errorf("program %s: %v", name, err)
		} else if err != nil {
			t.Logf("program %s: detection passed, later load error (expected without CAP_BPF): %v", name, err)
		}
	}
}

// TestLoadAndAttach_PermissionErrorSurfacesClearMessage: when the library
// returns the permission error, the probe reason explains cause and remedy.
func TestLoadAndAttach_PermissionErrorSurfacesClearMessage(t *testing.T) {
	orig, origMem := assignNetprobe, removeMemlock
	defer func() { assignNetprobe, removeMemlock = orig, origMem }()
	removeMemlock = func() error { return nil }
	assignNetprobe = func(*cebpf.CollectionSpec, *netprobeObjects, *cebpf.CollectionOptions) error {
		return errors.New("assign values: field KprobeTcpConnect: program kprobe_tcp_connect: detecting kernel version: opening mem: open /proc/self/mem: " + syscall.EACCES.Error())
	}
	p, err := loadAndAttach()
	if err != nil {
		t.Fatalf("loadAndAttach must degrade, not error: %v", err)
	}
	if p.Loaded() {
		t.Fatal("probe must not be loaded when the load failed")
	}
	r, ok := p.(interface{ Reason() string })
	if !ok {
		t.Fatal("noop probe must carry a Reason")
	}
	for _, want := range []string{"permission denied", "datawatch setup ebpf", "restart"} {
		if !strings.Contains(r.Reason(), want) {
			t.Errorf("reason %q lacks %q", r.Reason(), want)
		}
	}
}
