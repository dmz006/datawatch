package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// testStillSame reports whether rec still identifies the same process: its
// pid must exist and carry the recorded start time.
func testStillSame(rec descendantProcess) bool {
	start, ok := readProcessStartTime(rec.PID)
	return ok && start == rec.StartTime
}

// startTestChild starts sh -c script as a child of the test process and makes
// sure it is killed and reaped on cleanup. It returns the script's pid.
func startTestChild(t *testing.T, sh, script string) int {
	t.Helper()
	cmd := exec.Command(sh, "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting child %q: %v", script, err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	return pid
}

// waitingChild returns a pid running a short sleep under sh, alive long enough
// for the test body.
func waitingChild(t *testing.T, sh string) int {
	return startTestChild(t, sh, "trap '' TERM; sleep 30")
}

func TestDescendantProcessRecordingKeepsPriorDroppedFromView(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("looking up sh: %v", err)
	}
	pid := waitingChild(t, sh)

	first := recordDescendants(map[int]bool{pid: true}, nil)
	rec, ok := first[pid]
	if !ok {
		t.Fatalf("recordDescendants: pid %d not recorded, got %v", pid, first)
	}
	if rec.StartTime == 0 {
		t.Fatal("recordDescendants: StartTime is 0 for a live process")
	}
	if rec.Line == "" {
		t.Fatal("recordDescendants: Line is empty for a live process")
	}

	// The child is no longer in the visible set (as if reparented out of the
	// worker's tree) but is still alive with the same start time, so the
	// prior entry must survive.
	kept := recordDescendants(nil, first)
	if got, ok := kept[pid]; !ok || got.StartTime != rec.StartTime || got.PID != pid {
		t.Fatalf("prior entry dropped out of view but should be kept: got %v, want %+v", kept[pid], rec)
	}

	// A forged prior entry, the pid now carrying a different start time, must
	// be dropped even if the pid is live (it is: the child's).
	forged := map[int]descendantProcess{pid: {PID: pid, StartTime: rec.StartTime + 1, Line: "forged"}}
	dropped := recordDescendants(nil, forged)
	if len(dropped) != 0 {
		t.Fatalf("prior entry with mismatched start time should be dropped, got %v", dropped)
	}
}

func TestDescendantProcessReusedPIDWithDifferentStartTimeIsNotSignalled(t *testing.T) {
	// The test process itself is a live pid at a known start time. Record an
	// entry for it whose start time does not match, as if the pid had been
	// reused by a different process. The mismatching entry must never be
	// signalled, whatever stillSame says about other entries.
	self := os.Getpid()
	selfStart, ok := readProcessStartTime(self)
	if !ok {
		t.Skipf("cannot read start time of pid %d: process is unobservable in this environment", self)
	}
	type sigCall struct {
		pid int
		sig syscall.Signal
	}
	var calls []sigCall
	signal := func(pid int, sig syscall.Signal) error {
		calls = append(calls, sigCall{pid: pid, sig: sig})
		return nil
	}
	recorded := map[int]descendantProcess{
		self: {PID: self, StartTime: selfStart + 12345, Line: "stale identity"},
	}
	ended, survivors := endRecordedDescendants(recorded, 50*time.Millisecond, signal, testStillSame)
	if len(calls) != 0 {
		t.Fatalf("signalled a mismatched identity: %v", calls)
	}
	if len(ended) != 0 {
		t.Fatalf("stale entry reported ended: %v", ended)
	}
	if len(survivors) != 1 || survivors[0].PID != self {
		t.Fatalf("stale entry should be accounted for as not ended, got %v", survivors)
	}
}

func TestDescendantProcessOnlyRecordedPIDsAreSignalled(t *testing.T) {
	// A live pid that never dies (the test process itself) is the only entry.
	// A fake signal must be invoked for exactly the recorded pid and for no
	// other pid, with SIGTERM first and SIGKILL after the grace period.
	self := os.Getpid()
	if _, ok := readProcessStartTime(self); !ok {
		t.Skipf("cannot read start time of pid %d", self)
	}
	type sigCall struct {
		pid int
		sig syscall.Signal
	}
	var calls []sigCall
	signal := func(pid int, sig syscall.Signal) error {
		calls = append(calls, sigCall{pid: pid, sig: sig})
		return nil
	}
	recorded := map[int]descendantProcess{
		self: {PID: self, StartTime: mustProcessStartTime(t, self), Line: "self"},
	}
	stillSame := func(descendantProcess) bool { return true }
	ended, survivors := endRecordedDescendants(recorded, 50*time.Millisecond, signal, stillSame)
	if len(ended) != 0 {
		t.Fatalf("nothing died under a non-delivering signal, so none may be ended: %v", ended)
	}
	if len(survivors) != 1 || survivors[0].PID != self {
		t.Fatalf("the recorded entry must surface as a survivor, got %v", survivors)
	}
	byPair := map[pair]int{}
	for _, c := range calls {
		byPair[pair{c.pid, c.sig}]++
	}
	for c := range byPair {
		if c.pid != self {
			t.Fatalf("signalled pid %d that is not the recorded entry", c.pid)
		}
	}
	if got, ok := byPair[pair{self, syscall.SIGTERM}]; !ok || got != 1 {
		t.Fatalf("SIGTERM calls for recorded pid = %d, want exactly 1 (calls: %v)", got, calls)
	}
	if got, ok := byPair[pair{self, syscall.SIGKILL}]; !ok || got != 1 {
		t.Fatalf("SIGKILL calls for recorded pid = %d, want exactly 1 (calls: %v)", got, calls)
	}
	if len(calls) != 2 {
		t.Fatalf("exactly the two signals for the recorded pid expected, got %v", calls)
	}
}

// pair is the pid/signal identity of one signal call, for call accounting.
type pair struct {
	pid int
	sig syscall.Signal
}

func mustProcessStartTime(t *testing.T, pid int) uint64 {
	t.Helper()
	start, ok := readProcessStartTime(pid)
	if !ok {
		t.Fatalf("cannot read start time of pid %d", pid)
	}
	return start
}

func TestDescendantProcessSIGKILLEscalationEndsChildThatIgnoresTERM(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("looking up sh: %v", err)
	}
	pid := startTestChild(t, sh, "trap '' TERM; while :; do sleep 0.02; done")
	// Synchronize on the child having set up its trap: TERM would otherwise
	// race its startup and kill it before the ignore is in effect.
	waitSigIgnored(t, pid, syscall.SIGTERM, 3*time.Second)

	recorded := recordDescendants(map[int]bool{pid: true}, nil)
	rec, ok := recorded[pid]
	if !ok {
		t.Fatalf("child pid %d not recorded: %v", pid, recorded)
	}
	var sent []syscall.Signal
	signal := func(_ int, sig syscall.Signal) error {
		sent = append(sent, sig)
		return syscall.Kill(rec.PID, sig)
	}
	ended, survivors := endRecordedDescendants(recorded, 300*time.Millisecond, signal, testStillSame)
	if len(survivors) != 0 {
		t.Fatalf("term-ignoring child survived SIGKILL: %v", survivors)
	}
	if len(ended) != 1 || ended[0].PID != pid {
		t.Fatalf("child should be reported ended, ended=%v", ended)
	}
	if !sawSignal(sent, syscall.SIGTERM) || !sawSignal(sent, syscall.SIGKILL) {
		t.Fatalf("expected SIGTERM then SIGKILL, got %v", sent)
	}
	if !processGone(pid) {
		t.Fatalf("pid %d still running after endRecordedDescendants", pid)
	}
}

func TestDescendantProcessChildExitingOnTERMEndsWithoutSIGKILL(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("looking up sh: %v", err)
	}
	pid := startTestChild(t, sh, "sleep 30")

	recorded := recordDescendants(map[int]bool{pid: true}, nil)
	rec, ok := recorded[pid]
	if !ok {
		t.Fatalf("child pid %d not recorded: %v", pid, recorded)
	}
	var sent []syscall.Signal
	signal := func(_ int, sig syscall.Signal) error {
		sent = append(sent, sig)
		return syscall.Kill(rec.PID, sig)
	}
	ended, survivors := endRecordedDescendants(recorded, 500*time.Millisecond, signal, testStillSame)
	if len(survivors) != 0 {
		t.Fatalf("child should have ended on SIGTERM: %v", survivors)
	}
	if len(ended) != 1 || ended[0].PID != pid {
		t.Fatalf("child should be reported ended, ended=%v", ended)
	}
	if sawSignal(sent, syscall.SIGKILL) {
		t.Fatalf("SIGKILL was sent even though the child exited on SIGTERM: %v", sent)
	}
}

// waitSigIgnored polls the child's /proc/<pid>/status until sig appears in
// the SigIgn mask, so the test does not race the child's signal setup, and
// fails the test if it never does within timeout.
func waitSigIgnored(t *testing.T, pid int, sig syscall.Signal, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	want := uint64(1) << uint(sig-1)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if mask, ok := parseSigMask(line, "SigIgn:"); ok && mask&want != 0 {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child pid %d never ignored signal %d (dead or still starting)", pid, int(sig))
}

// parseSigMask returns the hex mask of a /proc status signal line such as
// 'SigIgn:\t0000000000014000'.
func parseSigMask(line, prefix string) (uint64, bool) {
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}
	mask, err := strconv.ParseUint(strings.TrimSpace(line[len(prefix):]), 16, 64)
	return mask, err == nil
}

func sawSignal(calls []syscall.Signal, sig syscall.Signal) bool {
	for _, c := range calls {
		if c == sig {
			return true
		}
	}
	return false
}
