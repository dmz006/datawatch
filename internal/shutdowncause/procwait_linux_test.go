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

// awaitChildReady blocks until the re-executed signal-parent child has
// announced its start on stdout (read through the pipe with a bounded
// timeout) and its own /proc entry shows a cmdline containing want
// (polled with a bounded deadline and early exit). After the
// announcement the rest of stdout is drained so the child never blocks.
func awaitChildReady(t *testing.T, stdout io.Reader, pid int, want string) {
	t.Helper()
	if line, err := readLineWithTimeout(stdout, 30*time.Second); err != nil || line != childStartedLine {
		t.Fatalf("child pid %d did not announce start: line=%q err=%v", pid, line, err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if got, err := waitForCmdline(pid, want, 10*time.Second, readProcCmdline); err != nil {
		t.Fatalf("child cmdline not visible: %v (last %q)", err, got)
	}
}
