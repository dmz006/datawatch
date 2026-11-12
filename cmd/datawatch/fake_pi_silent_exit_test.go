// FakePiSilentExit tests: an end-to-end stand-in for a silent `pi --print`
// exit. A tiny fake `pi` shell script is exec'd to a session-log file (the
// fake-pane command it would have filled), then the real decision code —
// exitSilenceTick + autonomouspkg.ExitSilenceTracker — is driven with a
// fake clock and fake runner-alive state. No tmux is involved at all: the
// script is a plain shell file and everything else is in-process.
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// writeFakePi writes a stand-in `pi` executable into dir whose body (between
// the shebang and `exit 0`) is scriptBody. Same fixture style as the fake
// tmux in internal/llm/backends/pi/backend_test.go.
func writeFakePi(t *testing.T, dir, scriptBody string) string {
	t.Helper()
	piPath := filepath.Join(dir, "pi")
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	if scriptBody != "" {
		sb.WriteString(scriptBody)
	}
	sb.WriteString("exit 0\n")
	if err := os.WriteFile(piPath, []byte(sb.String()), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake pi: %v", err)
	}
	return piPath
}

// runFakePi exec's the fake pi exactly the way a wait loop observes its pane
// command: stdout and stderr go to logPath, then we Wait and assert the
// process is gone and exited cleanly.
func runFakePi(t *testing.T, piPath, logPath string) {
	t.Helper()
	logf, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}
	cmd := exec.Command(piPath)
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake pi: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		_ = logf.Close()
		t.Fatalf("fake pi must exit 0: %v", err)
	}
	if err := logf.Close(); err != nil {
		t.Fatalf("close log file: %v", err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatalf("fake pi process must be gone and have exited cleanly (state=%v)", cmd.ProcessState)
	}
}

// shellSingleQuote quotes a path for the sh fixture, escaping any embedded
// single quotes.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Fake clock base: a future instant (a day past real now) so the log file's
// real-wall-clock mtime always lands before the fake clock and can never
// accidentally move the tracker's lastActivity.
func fakeClockBase(t *testing.T) time.Time {
	t.Helper()
	return time.Now().Add(24 * time.Hour).Truncate(time.Second)
}

func TestFakePiSilentExit_StalledAfterLimit(t *testing.T) {
	dir := t.TempDir()
	piPath := writeFakePi(t, dir, "") // silent: prints nothing, exit 0

	logPath := filepath.Join(dir, "session.log")
	runFakePi(t, piPath, logPath)

	// The silent exit: the log the fake pane command would have filled is
	// empty, and its process is gone (both asserted above and here).
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("silent fake pi must leave an empty log, got %q", data)
	}
	logMtime := time.Time{}
	if st, err := os.Stat(logPath); err == nil {
		logMtime = st.ModTime()
	}

	limit := 2 * time.Minute
	start := fakeClockBase(t)
	tr := autonomouspkg.NewExitSilenceTracker(start, limit)
	runnerAlive := true
	tickAt := func(now time.Time) (bool, string) {
		return exitSilenceTick(tr, "pi-prompt", now,
			func() bool { return runnerAlive },
			func() time.Time { return logMtime }) // the empty log file's mtime
	}

	// While the runner is alive it is never stalled — not even once the
	// fake clock has sailed past the limit.
	runnerAlive = true
	for _, offset := range []time.Duration{0, 5 * time.Minute} {
		if stalled, reason := tickAt(start.Add(offset)); stalled {
			t.Fatalf("runner alive at +%.0fs must never be stalled, got reason=%q", offset.Seconds(), reason)
		}
	}

	// The runner stops. Before the silence limit has elapsed: quiet.
	runnerAlive = false
	stoppedAt := start.Add(5 * time.Minute) // last tick saw the runner alive here
	if stalled, reason := tickAt(stoppedAt.Add(90 * time.Second)); stalled {
		t.Fatalf("90s of post-exit silence (limit %s) must not stall, got reason=%q", limit, reason)
	}
	if stalled, reason := tickAt(stoppedAt.Add(limit - time.Second)); stalled {
		t.Fatalf("just under the limit must not stall, got reason=%q", reason)
	}

	// The fake clock passes the limit: stalled, with the real error type.
	stalled, reason := tickAt(stoppedAt.Add(limit + 30*time.Second))
	if !stalled {
		t.Fatal("expected stalled once the fake clock passes the limit after the runner stopped")
	}
	if !strings.Contains(reason, "exited without a completion or verdict") {
		t.Errorf("reason %q should name the silent-exit condition", reason)
	}
	err = autonomouspkg.ExitSilenceError(reason)
	if !errors.Is(err, autonomouspkg.ErrWorkerStalled) {
		t.Fatalf("ExitSilenceError must satisfy errors.Is(_, ErrWorkerStalled), got %v", err)
	}
}

func TestFakePiSilentExit_NonPiFamily_NeverStalled(t *testing.T) {
	dir := t.TempDir()
	piPath := writeFakePi(t, dir, "") // same silent fake pi
	logPath := filepath.Join(dir, "session.log")
	runFakePi(t, piPath, logPath)
	if data, _ := os.ReadFile(logPath); len(data) != 0 {
		t.Fatalf("silent fake pi must leave an empty log, got %q", data)
	}

	start := fakeClockBase(t)
	limit := 2 * time.Minute
	tr := autonomouspkg.NewExitSilenceTracker(start, limit)
	probes := 0
	stalled, reason := exitSilenceTick(tr, "claude-code", start.Add(10*time.Minute),
		func() bool { probes++; return false },
		func() time.Time { probes++; return time.Time{} })
	if stalled || reason != "" {
		t.Fatalf("non-pi backend family must never be flagged, got stalled=%v reason=%q", stalled, reason)
	}
	if probes != 0 {
		t.Fatalf("non-pi family must not call the probes (got %d calls)", probes)
	}
}

func TestFakePiSilentExit_LogLineResetsSilenceClock(t *testing.T) {
	dir := t.TempDir()

	logPath := filepath.Join(dir, "session.log")
	// Fake pi that writes exactly one line to the log just before exiting.
	body := "printf 'final line before exit\\n' > " + shellSingleQuote(logPath) + "\n"
	piPath := writeFakePi(t, dir, body)

	runFakePi(t, piPath, logPath)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(data) == 0 || !strings.Contains(string(data), "final line before exit") {
		t.Fatalf("fake pi must have written one line to the log, got %q", data)
	}

	start := fakeClockBase(t)
	limit := 2 * time.Minute
	tr := autonomouspkg.NewExitSilenceTracker(start, limit)
	runnerAlive := false
	// Log mtime reads the real file, exactly like the wait loop's probe.
	logModTime := func() time.Time {
		st, err := os.Stat(logPath)
		if err != nil {
			t.Fatalf("stat log: %v", err)
		}
		return st.ModTime()
	}
	tickAt := func(now time.Time) (bool, string) {
		return exitSilenceTick(tr, "pi-prompt", now,
			func() bool { return runnerAlive }, logModTime)
	}

	// Silence accumulates with no log line visible yet (the real mtime of
	// the log file lands before the fake clock, so it does not move the
	// tracker): past the limit the silent exit would be flagged.
	lineArrivingAt := start.Add(limit + 30 * time.Second)
	if stalled, reason := tickAt(start.Add(limit + 30 * time.Second)); !stalled {
		t.Fatalf("before the log line is delivered, %s of silence past the limit must stall (reason=%q)", limit+30*time.Second, reason)
	}

	// The one line arrives just before exit, at fake time lineArrivingAt.
	if err := os.Chtimes(logPath, lineArrivingAt, lineArrivingAt); err != nil {
		t.Fatalf("chtimes log: %v", err)
	}
	// Now the very instant that looked stalled is the instant the line
	// landed — the silence clock resets, so it is no longer stalled.
	if stalled, reason := tickAt(lineArrivingAt); stalled {
		t.Fatalf("the log line landing at %s must reset the clock, got stalled with reason=%q", lineArrivingAt.Format(time.RFC3339), reason)
	}
	if stalled, reason := tickAt(lineArrivingAt.Add(limit - time.Second)); stalled {
		t.Fatalf("under a full limit after the line's mtime must not stall, got reason=%q", reason)
	}
	// And the clock keeps counting from the line, not from the start.
	if stalled, reason := tickAt(lineArrivingAt.Add(limit + 30*time.Second)); !stalled {
		t.Fatalf("a full limit of silence after the line should stall again, reason=%q", reason)
	}
}
