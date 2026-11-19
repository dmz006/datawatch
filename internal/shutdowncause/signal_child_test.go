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
// a stable, readable 'pid=N cmdline="..."' form. It is deliberately
// truncated — at most 5 candidates, each cmdline at most 200 characters,
// with a "(N more)" marker — so a wave of misclassified agent/editor
// sessions with multi-kilobyte command lines cannot blow up the failure
// output.
func formatCandidates(cands []Candidate) string {
	const maxShown = 5
	const maxCmdline = 200
	parts := make([]string, 0, maxShown+1)
	for i, c := range cands {
		if i >= maxShown {
			parts = append(parts, fmt.Sprintf("(%d more)", len(cands)-maxShown))
			break
		}
		cmdline := c.Cmdline
		if len(cmdline) > maxCmdline {
			cmdline = cmdline[:maxCmdline] + "..."
		}
		parts = append(parts, fmt.Sprintf("pid=%d cmdline=%q", c.PID, cmdline))
	}
	return strings.Join(parts, " ")
}

// TestSignalChildProcScan is a regression test for the signal-capture
// /proc fallback: while a live process shaped exactly like the datawatch
// stop-sender is visible in the process table, the test delivers a real
// SIGTERM and checks that Capture attributes the stop to that candidate
// process — and never to some unrelated live process (for example the
// agent session running this test) whose command line merely mentions the
// unit name or the stop/restart words.
func TestSignalChildProcScan(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("procscan signal capture requires Linux (/proc, kill(2)); skipping on %s", runtime.GOOS)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available; cannot stand up a live stop-sender-shaped process")
	}
	MarkAPIInitiated("")
	t.Cleanup(func() { MarkAPIInitiated("") })

	myPID := os.Getpid()

	// A live process whose /proc cmdline is exactly the systemctl stop of
	// the datawatch unit: `exec -a` fakes the argv, and the process stays
	// alive long enough to be visible to the procscan. No unit or service
	// manager is ever invoked.
	// service-scope-ok: fake argv on a child of this test; never touches a real unit.
	fake := exec.Command(bash, "-c", `exec -a "systemctl --user stop datawatch" sleep 300`) // service-scope-ok fake argv, never executed by a service manager
	if err := fake.Start(); err != nil {
		t.Fatalf("start fake stop-sender: %v", err)
	}
	defer func() {
		_ = fake.Process.Kill()
		_ = fake.Wait()
	}()
	time.Sleep(150 * time.Millisecond) // let it settle so /proc sees a stable cmdline

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGTERM) })

	// Deliver a real SIGTERM to this test process. With signal.Notify
	// installed the Go runtime routes it to us instead of dying. No
	// siginfo capture is installed for this signal here, so Capture must
	// attribute via the procscan fallback.
	if err := syscall.Kill(myPID, syscall.SIGTERM); err != nil {
		t.Fatalf("kill(SIGTERM) to own pid %d: %v", myPID, err)
	}

	var sig os.Signal
	select {
	case sig = <-sigCh:
	case <-time.After(30 * time.Second):
		t.Fatalf("waited 30s for our own SIGTERM; candidates at that moment: %s", formatCandidates(CandidatesFor(myPID)))
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

	switch c.Source {
	case SourceSignalInfo:
		// A platform-provided siginfo names the real sender; with no
		// siginfo capture installed that is this test process itself.
		if c.SenderPID != 0 && c.SenderPID != myPID {
			t.Fatalf("SenderPID = %d, want 0 or own pid %d (siginfo path); candidates: %s",
				c.SenderPID, myPID, formatCandidates(CandidatesFor(myPID)))
		}
	default:
		// /proc fallback: the live stop-sender-shaped process must be
		// among the candidates, and the captured cause must point at one
		// of them — never at an unrelated process.
		cands := CandidatesFor(myPID)
		var fakec *Candidate
		for i := range cands {
			if cands[i].PID == fake.Process.Pid {
				fakec = &cands[i]
				break
			}
		}
		if fakec == nil {
			t.Fatalf("fake stop-sender pid %d not among procscan candidates: %s (cause: %s source=%q)",
				fake.Process.Pid, formatCandidates(cands), c.String(), c.Source)
		}
		if !strings.Contains(fakec.Cmdline, "systemctl") || !strings.Contains(fakec.Cmdline, daemonUnit) {
			t.Fatalf("fake candidate cmdline = %q, want the systemctl stop of %s (cause: %s)",
				fakec.Cmdline, daemonUnit, c.String())
		}
		if c.SenderPID != 0 {
			inCands := false
			for i := range cands {
				if cands[i].PID == c.SenderPID {
					inCands = true
					break
				}
			}
			if !inCands {
				t.Fatalf("SenderPID = %d is not a procscan candidate (the cause blames an unrelated process): %s (cause: %s)",
					c.SenderPID, formatCandidates(cands), c.String())
			}
		}
		if c.Cmdline != "" {
			inCands := false
			for i := range cands {
				if cands[i].Cmdline == c.Cmdline {
					inCands = true
					break
				}
			}
			if !inCands {
				t.Fatalf("captured Cmdline %q is not any procscan candidate's (the cause blames an unrelated process): %s",
					c.Cmdline, formatCandidates(cands))
			}
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
