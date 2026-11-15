//go:build linux

package shutdowncause

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// readLineWithTimeout reads one line from r, giving up after d. It never
// sleeps: it returns as soon as the line (or an error) arrives.
func readLineWithTimeout(r io.Reader, d time.Duration) (string, error) {
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		ch <- res{strings.TrimRight(line, "\r\n"), err}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case got := <-ch:
		if got.err != nil && got.line == "" {
			return "", got.err
		}
		return got.line, nil
	case <-timer.C:
		return "", fmt.Errorf("no line within %s", d)
	}
}

// readProcCmdline reads /proc/<pid>/cmdline as a space-joined string.
func readProcCmdline(pid int) (string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return "", err
	}
	return strings.Trim(strings.ReplaceAll(string(raw), "\x00", " "), " "), nil
}

// waitForCmdline polls read(pid) until its result contains want, returning
// early on success and failing once the deadline passes. The poll interval
// only bounds how often the check runs; correctness never depends on it.
func waitForCmdline(pid int, want string, deadline time.Duration, read func(int) (string, error)) (string, error) {
	end := time.Now().Add(deadline)
	var last string
	var lastErr error
	for {
		got, err := read(pid)
		if err == nil && strings.Contains(got, want) {
			return got, nil
		}
		last, lastErr = got, err
		if !time.Now().Before(end) {
			return last, fmt.Errorf("pid %d cmdline %q (err %v) did not contain %q within %s", pid, last, lastErr, want, deadline)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// awaitChildReadyWith waits until the child has announced its start on
// stdout (bounded by lineTimeout) and read(pid) shows a cmdline
// containing want (bounded by cmdTimeout, early exit). After the
// announcement the rest of stdout is drained so the child never blocks.
func awaitChildReadyWith(stdout io.Reader, pid int, want string, lineTimeout, cmdTimeout time.Duration, read func(int) (string, error)) error {
	line, err := readLineWithTimeout(stdout, lineTimeout)
	if err != nil || line != childStartedLine {
		return fmt.Errorf("child pid %d did not announce start: line=%q err=%v", pid, line, err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if got, err := waitForCmdline(pid, want, cmdTimeout, read); err != nil {
		return fmt.Errorf("child cmdline not visible: %v (last %q)", err, got)
	}
	return nil
}

// awaitChildReady is awaitChildReadyWith against the live /proc with the
// test's bounds (30s for the announcement, 10s for the cmdline).
func awaitChildReady(t *testing.T, stdout io.Reader, pid int, want string) {
	t.Helper()
	if err := awaitChildReadyWith(stdout, pid, want, 30*time.Second, 10*time.Second, readProcCmdline); err != nil {
		t.Fatal(err)
	}
}
