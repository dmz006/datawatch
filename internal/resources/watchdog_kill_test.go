package resources

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

const watchdogAllocEnv = "DATAWATCH_WATCHDOG_KILL_ALLOCATOR"

// TestWatchdogKillAllocatorHelper is not a test on its own: the kill test
// re-executes the test binary with watchdogAllocEnv set, and this function
// then holds touched memory well past the ceiling until it is killed.
func TestWatchdogKillAllocatorHelper(t *testing.T) {
	if os.Getenv(watchdogAllocEnv) != "1" {
		t.Skip("helper process only")
	}
	var held [][]byte
	for i := 0; i < 50; i++ { // 50 x 4 MB = 200 MB, bounded as a safety net
		b := make([]byte, 4<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = 1
		}
		held = append(held, b)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(30 * time.Second)
	_ = held
}

// TestWatchdogKillsGroupOverCeiling runs on every host: it uses the /proc
// watchdog directly and never needs systemd.
func TestWatchdogKillsGroupOverCeiling(t *testing.T) {
	if !(ProcFS{Root: "/proc"}).Exists() {
		t.Skip("no /proc on this host; the no-ceiling path is covered separately")
	}
	const ceilingMB = 64
	cmd := exec.Command(os.Args[0], "-test.run=^TestWatchdogKillAllocatorHelper$")
	cmd.Env = append(os.Environ(), watchdogAllocEnv+"=1")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, _ := Watch(ctx, cmd, ceilingMB)
	if ctx.Err() != nil {
		t.Fatalf("watchdog did not stop the child before the timeout: %s", res)
	}
	if !res.Killed {
		t.Fatalf("child group was not killed: %s", res)
	}
	if res.Status != StatusWatchdog {
		t.Errorf("status = %s, want %s", res.Status, StatusWatchdog)
	}
	if res.PeakKB <= int64(ceilingMB)*1024 {
		t.Errorf("peak %d KB does not exceed the %d MB ceiling", res.PeakKB, ceilingMB)
	}

	info := DetectWatchdogKill(res)
	if !info.Killed {
		t.Fatalf("DetectWatchdogKill did not report a kill: %+v", info)
	}
	if KillReason != "the process used more memory than the limit" {
		t.Errorf("kill reason = %q", KillReason)
	}

	// The child group is gone: signal 0 to the group must fail with ESRCH.
	pgid := cmd.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(-pgid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-pgid, 0); err != syscall.ESRCH {
		t.Errorf("child group %d still alive after the kill: %v", pgid, err)
	}

	// The test process itself is alive.
	if err := syscall.Kill(os.Getpid(), 0); err != nil {
		t.Errorf("test process not alive: %v", err)
	}
}

// TestWatchdogNoCeilingWithoutScopeOrProc uses an injected reader that has
// no /proc and a host with no user scope.
func TestWatchdogNoCeilingWithoutScopeOrProc(t *testing.T) {
	r := &fakeProc{exists: false}
	st := SelectStatus(false, r.Exists())
	if st != StatusNone {
		t.Fatalf("status = %s, want %s", st, StatusNone)
	}
	if !st.NeedsEvent() {
		t.Error("no-ceiling status must raise an Events entry")
	}
	if st.Message() != NoCeilingMessage {
		t.Errorf("message = %q, want %q", st.Message(), NoCeilingMessage)
	}
	if ErrNoCeiling.Error() != NoCeilingMessage {
		t.Errorf("ErrNoCeiling = %q", ErrNoCeiling)
	}
}
