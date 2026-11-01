package shutdowncause

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// signalChildRole marks a re-executed test binary as the child that sends
// SIGTERM to its parent (this test process). Detected in TestMain before
// any testing machinery runs.
const signalChildRole = "signal-parent"

// runSignalParentRole is the child role: send SIGTERM to the exact PID
// given by the parent — nothing else, ever — then stay alive until the
// parent closes our stdin (EOF) or the 120 second safety cap runs out,
// so the parent's Capture can still find us in the process table.
func runSignalParentRole() int {
	raw := os.Getenv("DW_SHUTDOWNCAUSE_PARENT_PID")
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		fmt.Fprintf(os.Stderr, "signal-parent role: bad parent pid %q: %v\n", raw, err)
		return 2
	}
	if err := signalProc(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "signal-parent role: kill %d: %v\n", pid, err)
		return 3
	}
	// Stay alive only as long as the parent holds our stdin pipe open:
	// io.Copy returns on EOF, which happens when the parent closes the
	// pipe in its cleanup — or when it dies, so we can never outlive a
	// hung run as an orphan. The 120 second safety cap bounds us even if
	// the parent neither closes the pipe nor dies in time.
	time.AfterFunc(120*time.Second, func() { os.Exit(0) }) // safety cap
	_, _ = io.Copy(io.Discard, os.Stdin)                   // blocks until EOF from the parent
	return 0
}

// formatCandidates renders the procscan candidates for failure messages in
// a stable, readable 'pid=N cmdline="..."' form.
func formatCandidates(cands []Candidate) string {
	parts := make([]string, 0, len(cands))
	for _, c := range cands {
		parts = append(parts, fmt.Sprintf("pid=%d cmdline=%q", c.PID, c.Cmdline))
	}
	return strings.Join(parts, " ")
}

// TestSignalChildProcScan is a regression test for the signal-capture
// fallback: it re-executes this test binary (standard TestMain/helper-env
// pattern); the child sends SIGTERM to us by PID; the capture is done via
// signal.Notify + Capture. If the platform provided siginfo, the sender
// must be exactly the child's PID; otherwise (the /proc fallback) the
// child must appear among the procscan candidates and the captured
// cmdline must contain the test binary. The test signals only its own
// process, identified by the PID it passed to the child.
func TestSignalChildProcScan(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("siginfo/procscan signal capture requires Linux (/proc, kill(2)); skipping on %s", runtime.GOOS)
	}
	MarkAPIInitiated("")
	t.Cleanup(func() { MarkAPIInitiated("") })

	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	myPID := os.Getpid()

	// The child's /proc cmdline after start: binary, then "kill <our pid>".
	childCmdline := bin + " kill " + strconv.Itoa(myPID)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGTERM) })

	// Re-execute the test binary. The positional args are invisible to
	// test flag parsing (parsing stops at the first non-flag) and exist
	// only so the child's /proc/<pid>/cmdline is a recognizable
	// stop-sender line: "kill <our own pid>" (no other pid is ever used).
	// TestMain exits in helper mode before any test runs.
	cmd := exec.Command(bin, "kill", strconv.Itoa(myPID))
	cmd.Env = append(os.Environ(),
		"DW_SHUTDOWNCAUSE_ROLE="+signalChildRole,
		"DW_SHUTDOWNCAUSE_PARENT_PID="+strconv.Itoa(myPID),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Hold a stdin pipe to the child open for as long as we need it: the
	// child blocks on it (with its own safety cap), so it cannot exit
	// between sending SIGTERM and our procscan attribution check. The
	// cleanup below closes it first, which gives the child EOF.
	childStdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe for child: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("re-execute test binary: %v", err)
	}
	defer func() {
		_ = childStdin.Close() // EOF: releases the child promptly
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	var sig os.Signal
	select {
	case sig = <-sigCh:
	case <-time.After(30 * time.Second):
		t.Fatalf("waited 30s for SIGTERM from child pid %d, cmdline %q", cmd.Process.Pid, childCmdline)
	}

	c := Capture(sig)

	if c.Signal != syscall.SIGTERM.String() {
		t.Fatalf("Signal = %q, want %q", c.Signal, syscall.SIGTERM.String())
	}
	if c.Time.IsZero() {
		t.Fatal("Time must be set")
	}
	if !strings.HasPrefix(c.String(), "shutdown: signal=terminated") {
		t.Fatalf("String() = %q, want prefix %q", c.String(), "shutdown: signal=terminated")
	}

	base := filepath.Base(bin)
	switch c.Source {
	case SourceSignalInfo:
		// Platform provided siginfo: the sender is exactly the child.
		if c.SenderPID != cmd.Process.Pid {
			t.Fatalf("SenderPID = %d, want child pid %d (siginfo path)", c.SenderPID, cmd.Process.Pid)
		}
	default:
		// /proc fallback: the child must be among the candidates.
		cands := CandidatesFor(myPID)
		var childc *Candidate
		for i := range cands {
			if cands[i].PID == cmd.Process.Pid {
				childc = &cands[i]
				break
			}
		}
		if childc == nil {
			t.Fatalf("child pid %d not among procscan candidates: %s (cause: %s source=%q sender_pid=%d cmdline=%q)",
				cmd.Process.Pid, formatCandidates(cands), c.String(), c.Source, c.SenderPID, c.Cmdline)
		}
		if !strings.Contains(childc.Cmdline, base) {
			t.Fatalf("candidate cmdline %q does not contain test binary %s (cause: %s source=%q sender_pid=%d; candidates: %s)",
				childc.Cmdline, base, c.String(), c.Source, c.SenderPID, formatCandidates(cands))
		}
		// The captured cause must not point at some other process.
		if c.SenderPID != 0 && c.SenderPID != cmd.Process.Pid {
			t.Fatalf("SenderPID = %d, want child pid %d (procscan path); cause: %s source=%q sender_pid=%d cmdline=%q; candidates: %s",
				c.SenderPID, cmd.Process.Pid, c.String(), c.Source, c.SenderPID, c.Cmdline, formatCandidates(cands))
		}
		if c.Cmdline != "" && !strings.Contains(c.Cmdline, base) {
			t.Fatalf("Captured Cmdline %q does not contain test binary %s (cause: %s source=%q sender_pid=%d; candidates: %s)",
				c.Cmdline, base, c.String(), c.Source, c.SenderPID, formatCandidates(cands))
		}
	}
}

// TestPreviousShutdownLine covers the three startup-line forms plus the
// consumed-record behavior.
func TestPreviousShutdownLine(t *testing.T) {
	tm, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")

	t.Run("signal stop", func(t *testing.T) {
		dir := t.TempDir()
		c := Cause{
			Signal:    "terminated",
			SenderPID: 4242,
			Cmdline:   "systemctl --user stop datawatch", // service-scope-ok fixture string: expected-value test data, never executed
			Source:    SourceProcScan,
			Time:      tm,
		}
		if err := Write(dir, c); err != nil {
			t.Fatalf("Write: %v", err)
		}
		// service-scope-ok fixture string: expected previous-shutdown line text, never executed
		want := "previous shutdown: signal terminated from systemctl --user stop datawatch 4242 at 2006-01-02T15:04:05Z" // service-scope-ok
		if got := PreviousShutdownLine(dir); got != want {
			t.Fatalf("line = %q\nwant %q", got, want)
		}
		// The record must be consumed: it can no longer be read back, so a
		// crash after a later clean start is not misreported next startup.
		if _, ok := ReadLast(dir); ok {
			t.Fatal("record must be consumed after PreviousShutdownLine")
		}
		if got := PreviousShutdownLine(dir); got != "previous shutdown: unknown/crash" {
			t.Fatalf("second line (no record) = %q, want %q", got, "previous shutdown: unknown/crash")
		}
	})

	t.Run("api stop", func(t *testing.T) {
		dir := t.TempDir()
		c := Cause{
			Signal: "terminated",
			Source: SourceAPIStop,
			Via:    "stop request via daemon API",
			Time:   tm,
		}
		if err := Write(dir, c); err != nil {
			t.Fatalf("Write: %v", err)
		}
		want := "previous shutdown: stop request via daemon API at 2006-01-02T15:04:05Z"
		if got := PreviousShutdownLine(dir); got != want {
			t.Fatalf("line = %q\nwant %q", got, want)
		}
	})

	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		want := "previous shutdown: unknown/crash"
		if got := PreviousShutdownLine(dir); got != want {
			t.Fatalf("line = %q, want %q", got, want)
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600); err != nil {
			t.Fatalf("write corrupt: %v", err)
		}
		want := "previous shutdown: unknown/crash"
		if got := PreviousShutdownLine(dir); got != want {
			t.Fatalf("line = %q, want %q", got, want)
		}
		// A corrupt record must also be consumed so it cannot re-trigger.
		if got := PreviousShutdownLine(dir); got != want {
			t.Fatalf("second line = %q, want %q", got, want)
		}
	})
}
