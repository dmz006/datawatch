//go:build unix

package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hungSuiteScript stands in for a PWA test suite that hangs. It starts a
// background sleep far longer than any test timeout, records its pid in
// suite.pid in the gate's directory, and then waits on it, as a suite stuck on
// a browser or a server would.
const hungSuiteScript = `#!/bin/sh
sleep 600 &
echo $! > suite.pid
wait
`

// hungSuiteMakefile defines the test target the gate runs.
const hungSuiteMakefile = "test:\n\tsh ./fake-suite.sh\n"

// hungSuiteGateTimeout is the short gate timeout the test passes in. The
// suite sleeps for ten minutes, so the gate can only end by timing out.
const hungSuiteGateTimeout = 2 * time.Second

// hungSuiteBound is the longest the gate may take to return after its timeout
// started. It covers the timeout, the group kill and the pipe wait delay.
const hungSuiteBound = 30 * time.Second

// hungSuiteReapWait is how long the test polls for the suite's process to be
// gone after the gate returns.
const hungSuiteReapWait = 10 * time.Second

// TestGateTimeoutKillsHungSuiteProcessGroup runs a gate whose fake suite hangs
// and checks that the gate ends with status timeout within a bounded time and
// that no process the gate started remains afterwards.
//
// The test creates no load of its own, so the bounded-load rule (nice, a
// quarter of the cores, a hard timeout and an EXIT/INT/TERM trap) does not
// apply: the only process it starts is the fake suite, which the gate's own
// group kill and the cleanup below end.
func TestGateTimeoutKillsHungSuiteProcessGroup(t *testing.T) {
	dir := t.TempDir()
	writeHungSuiteFile(t, dir, "Makefile", hungSuiteMakefile, 0o644)
	writeHungSuiteFile(t, dir, "fake-suite.sh", hungSuiteScript, 0o755)

	ctx, cancel := context.WithTimeout(context.Background(), 2*hungSuiteBound)
	defer cancel()

	start := time.Now()
	res := RunGate(ctx, dir, "test", hungSuiteGateTimeout)
	elapsed := time.Since(start)

	pid := readSuitePid(t, dir)
	t.Cleanup(func() {
		// Safety net for a failing run: the recorded pid is ours to end.
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	if res.Status != GateTimeout {
		t.Fatalf("status = %q, want %q (detail %q)", res.Status, GateTimeout, res.Detail)
	}
	if elapsed > hungSuiteBound {
		t.Fatalf("gate took %s to end, want within %s", elapsed, hungSuiteBound)
	}

	deadline := time.Now().Add(hungSuiteReapWait)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("process %d started by the gate is still running after the gate timed out", pid)
	}
}

// writeHungSuiteFile writes name under dir with the given mode.
func writeHungSuiteFile(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// readSuitePid returns the pid the fake suite recorded. A missing record means
// the suite never started, so the test would prove nothing.
func readSuitePid(t *testing.T, dir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "suite.pid"))
	if err != nil {
		t.Fatalf("the fake suite never recorded its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("suite.pid holds %q, want a process id", data)
	}
	return pid
}

// processAlive reports whether pid names a running process. A zombie counts as
// not running: it has been killed and only awaits reaping.
func processAlive(pid int) bool {
	if data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
		// The state is the first field after the parenthesised command name.
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		if i < 0 || i+2 >= len(s) {
			return true
		}
		return s[i+2] != 'Z'
	}
	return syscall.Kill(pid, 0) == nil
}
