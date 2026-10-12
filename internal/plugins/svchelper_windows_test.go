//go:build windows

package plugins

import (
	"net"
	"testing"
	"time"
)

// Windows stand-ins for the svchelper test helpers in supervisor_test.go
// (which is !windows): the managed-service registry tests in
// plugins_test.go compile everywhere but skip on Windows.

func buildSvcHelper(t *testing.T) string {
	t.Helper()
	t.Skip("svchelper-based service tests are POSIX-only")
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertGone(t *testing.T, pid int) { t.Helper() }

type eventLog struct{ events []ServiceEvent }

func (l *eventLog) add(ev ServiceEvent) { l.events = append(l.events, ev) }

func (l *eventLog) count(action string) int {
	n := 0
	for _, ev := range l.events {
		if ev.Action == action {
			n++
		}
	}
	return n
}
