package resources

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const allocatorEnv = "DATAWATCH_SCOPE_KILL_ALLOCATOR"

// TestScopeKillAllocatorHelper is not a test on its own: the integration test
// re-executes the test binary with allocatorEnv set, and this function then
// allocates and touches memory until the kernel kills the process.
func TestScopeKillAllocatorHelper(t *testing.T) {
	if os.Getenv(allocatorEnv) != "1" {
		t.Skip("helper process only")
	}
	var held [][]byte
	for {
		b := make([]byte, 4<<20)
		for i := 0; i < len(b); i += 4096 {
			b[i] = 1
		}
		held = append(held, b)
		_ = len(held)
	}
}

func scopeUnitActive(unit string) bool {
	return exec.Command("systemctl", "--user", "is-active", "--quiet", unit+".scope").Run() == nil
}

func TestScopeKillIntegration(t *testing.T) {
	if !ScopeAvailable() {
		t.Skip("no systemd user scope on this host")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("itest-%d-%d", os.Getpid(), time.Now().UnixNano())
	allocSpec := ScopeSpec{ID: suffix + "-alloc", Kind: "scopekill", CeilingMB: 64}
	sibSpec := ScopeSpec{ID: suffix + "-sib", Kind: "scopekill", CeilingMB: 64}
	allocUnit := UnitName(allocSpec.ID, allocSpec.Kind)
	sibUnit := UnitName(sibSpec.ID, sibSpec.Kind)

	// Stop only the units this test created, by exact name.
	t.Cleanup(func() {
		for _, u := range []string{allocUnit, sibUnit} {
			_ = exec.Command("systemctl", "--user", "stop", u+".scope").Run()
			_ = exec.Command("systemctl", "--user", "reset-failed", u+".scope").Run()
		}
	})

	// Sibling scope running sleep.
	sibSpec.Command = []string{"sleep", "120"}
	bin, args, err := ScopeCommand(sibSpec)
	if err != nil {
		t.Fatal(err)
	}
	sib := exec.Command(bin, args...)
	if err := sib.Start(); err != nil {
		t.Logf("sibling start failed: %v", err)
		t.Skip("no systemd user scope on this host")
	}
	sibDone := make(chan error, 1)
	go func() { sibDone <- sib.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for !scopeUnitActive(sibUnit) {
		select {
		case err := <-sibDone:
			t.Logf("sibling exited: %v", err)
			t.Skip("no systemd user scope on this host")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("sibling scope never became active")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Allocator scope. systemd's default OOMPolicy=stop tears the whole scope
	// down on an OOM kill, removing the cgroup before it can be read, so the
	// test sets OOMPolicy=continue; a background sleep then keeps the cgroup
	// alive so memory.events and memory.peak stay readable.
	allocSpec.Command = []string{"sh", "-c", `sleep 120 </dev/null >/dev/null 2>&1 & exec "$0" -test.run=^TestScopeKillAllocatorHelper$`, exe}
	bin, args, err = ScopeCommand(allocSpec)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range args {
		if a == "MemoryAccounting=yes" {
			args = append(args[:i+1], append([]string{"-p", "OOMPolicy=continue"}, args[i+1:]...)...)
			break
		}
	}
	alloc := exec.Command(bin, args...)
	alloc.Env = append(os.Environ(), allocatorEnv+"=1")
	waitErr := make(chan error, 1)
	if err := alloc.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { waitErr <- alloc.Wait() }()

	select {
	case err := <-waitErr:
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("allocator exited without error: %v", err)
		}
		ws, ok := ee.Sys().(syscall.WaitStatus)
		if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
			t.Fatalf("allocator was not SIGKILLed: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("allocator was not killed within 60s")
	}

	out, err := exec.Command("systemctl", "--user", "show", "-p", "ControlGroup", "--value", allocUnit+".scope").Output()
	cg := strings.TrimSpace(string(out))
	if err != nil || cg == "" {
		t.Fatalf("cannot find cgroup of %s: %v %q", allocUnit, err, cg)
	}
	info := DetectScopeKill(nil, filepath.Join("/sys/fs/cgroup", cg))
	if !info.Killed {
		t.Fatalf("kill detection did not report Killed: %+v", info)
	}
	if info.PeakMB < 48 || info.PeakMB > 80 {
		t.Fatalf("peak %d MB is not near the 64 MB ceiling", info.PeakMB)
	}

	// Sibling still running.
	select {
	case err := <-sibDone:
		t.Fatalf("sibling scope ended: %v", err)
	default:
	}
	if !scopeUnitActive(sibUnit) {
		t.Fatal("sibling scope is no longer active")
	}
	// Test process (standing in for the daemon) is alive.
	if err := syscall.Kill(os.Getpid(), 0); err != nil {
		t.Fatalf("test process not alive: %v", err)
	}
}
