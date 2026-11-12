package session

// Shared "end everything this test started" cleanup for the DW_BASE-live
// tests in this package, plus unit tests for that helper.
//
// The helper reuses bl462LiveClient (defined in restart_safety_live_test.go)
// so it can only ever reach the daemon through its HTTP API; a unit test
// points base at an httptest.NewServer instead of a real daemon.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// liveDaemonMarker is the substring these tests recorded with a live daemon
// pid: the daemon binary name as it appears in its cmdline. It is used for a
// read-only "is this still the daemon we recorded" check — never as a kill
// justification. A recorded pid may be signalled only by the tooling that
// recorded it (the sandbox start/wipe), after re-verifying its cmdline still
// carries the recorded marker.
const liveDaemonMarker = "datawatch"

// liveKillPath is the daemon API that ends a session; the only teardown
// channel this helper uses.
const liveKillPath = "/api/sessions/kill"

// liveSessionEnded reports whether there is nothing left to end for a
// session in this state: the store's terminal states (bl462IsTerminal),
// plus the observed tmux pane-gone states terminated/timed_out (killing
// those would only fail at the daemon).
func liveSessionEnded(state string) bool {
	return bl462IsTerminal(state) || state == "terminated" || state == "timed_out"
}

// liveTestCleanup centralizes the "end everything this test started" duty on
// every exit path (pass, failure and interrupt) for the DW_BASE-live tests in
// this package.
//
// Register it before the first call that can create a resource — the probe
// session start, the PRD create that spawns a worker — and feed it what the
// test learns as it goes:
//
//   - trackSession: the probe or worker session, each time a poll loop
//     observes it. The id is often only known after a list call
//     (polling the session list, or the PRD task's session id); once tracked,
//     every later exit path — including a t.Fatalf after it was learned —
//     ends it.
//
//   - the recorded daemon pids: the sandbox's recorded pid comes from the
//     restart contract (sandboxPID); the replacement pid a restart command's
//     daemon gets is usually unlearnable (the process is re-parented), but if
//     a test ever learns one it records it with recordDaemonPID.
//
// What the cleanup does — and only this:
//
//  1. Ends the tracked session through the daemon API only
//     (POST /api/sessions/kill) when its id is known and its last observed
//     state is not already ended. No tmux session stop, no kill-by-name: the
//     daemon's API is the only channel, and the daemon owns its sessions.
//
//  2. Prints a verification log line for each recorded daemon pid: either
//     the pid is gone (it ended — nothing to do) or it is still the recorded
//     sandbox pid and its cmdline still carries the datawatch marker — a
//     suite-owned sandbox, which has a max lifetime and its own wipe command
//     that own its end, so it is left running. A pid whose cmdline no longer
//     carries the marker may be a recycled number; it is logged and left
//     untouched.
//
// The helper never signals any process. Killing by name is forbidden, and
// sandbox daemons are suite-owned (max lifetime and wipe).
type liveTestCleanup struct {
	c             *bl462LiveClient
	sessionID     string
	sessionFullID string
	sessionState  string
	sandboxPID    int
	daemonPIDs    []int
	procRoot      string
	logf          func(format string, args ...any)
}

// newLiveTestCleanup registers the cleanup with the test (t.Cleanup functions
// run in last-in-first-out order) and returns the tracker. sandboxPID is the
// pid the sandbox restart contract records for the daemon under test, or 0
// when the contract is not in effect.
func newLiveTestCleanup(t *testing.T, c *bl462LiveClient, sandboxPID int) *liveTestCleanup {
	t.Helper()
	lc := &liveTestCleanup{
		c:          c,
		sandboxPID: sandboxPID,
		procRoot:   "/proc",
		logf:       t.Logf,
	}
	t.Cleanup(lc.run)
	return lc
}

// trackSession records the last observed probe/worker session.
func (lc *liveTestCleanup) trackSession(s bl462LiveSession) {
	lc.sessionID = s.ID
	lc.sessionFullID = s.FullID
	lc.sessionState = s.State
}

// recordDaemonPID records a daemon pid the test learned about — e.g. the new
// pid a restart command's daemon got when that command surfaces it. The
// cleanup verifies it like the sandbox pid; it never signals it.
func (lc *liveTestCleanup) recordDaemonPID(pid int) {
	lc.daemonPIDs = append(lc.daemonPIDs, pid)
}

// run is the cleanup body: end the tracked session via the daemon API (if
// any), then verify the recorded daemon pids in log lines.
func (lc *liveTestCleanup) run() {
	lc.endTrackedSession()
	lc.verifyDaemonPIDs()
}

func (lc *liveTestCleanup) endTrackedSession() {
	if lc.sessionID == "" {
		lc.logf("cleanup: no session was ever observed (its id is only known after a list call) — nothing to end")
		return
	}
	if liveSessionEnded(lc.sessionState) {
		lc.logf("cleanup: session %s is already %s — nothing to end", lc.sessionID, lc.sessionState)
		return
	}
	status, err := lc.c.do(http.MethodPost, liveKillPath, map[string]any{"id": lc.sessionID}, nil)
	if err != nil {
		lc.logf("cleanup: could not end session %s via the daemon API: %v — end it manually if it is still alive", lc.sessionID, err)
		return
	}
	if status >= http.StatusBadRequest {
		lc.logf("cleanup: the daemon API refused to end session %s (status %d) — check it manually if it is still alive", lc.sessionID, status)
		return
	}
	lc.logf("cleanup: ended session %s via the daemon API (last observed state %q)", lc.sessionID, lc.sessionState)
}

func (lc *liveTestCleanup) verifyDaemonPIDs() {
	if lc.sandboxPID <= 0 && len(lc.daemonPIDs) == 0 {
		lc.logf("cleanup: no daemon pid was recorded for this test — nothing to verify")
		return
	}
	if lc.sandboxPID > 0 {
		lc.verifyOneDaemonPID(lc.sandboxPID, true)
	}
	for _, pid := range lc.daemonPIDs {
		lc.verifyOneDaemonPID(pid, false)
	}
}

func (lc *liveTestCleanup) verifyOneDaemonPID(pid int, isSandboxRecorded bool) {
	cmdline, alive := liveProcCmdline(lc.procRoot, pid)
	switch {
	case !alive:
		lc.logf("cleanup: daemon pid %d is gone (it ended) — nothing to do", pid)
	case strings.Contains(cmdline, liveDaemonMarker):
		if isSandboxRecorded {
			lc.logf("cleanup: daemon pid %d is still the recorded sandbox pid and its cmdline still carries the datawatch marker — suite-owned sandbox (max lifetime and its own wipe command own its end); left running", pid)
		} else {
			lc.logf("cleanup: daemon pid %d is still alive and still carries the datawatch marker — suite-owned (max lifetime and the sandbox wipe command own its end); left running", pid)
		}
	default:
		lc.logf("cleanup: daemon pid %d no longer carries the datawatch marker in its cmdline (pid may be recycled) — left untouched", pid)
	}
}

// liveProcCmdline reads <procRoot>/<pid>/cmdline read-only, turning NUL
// separators into spaces. A process that vanished in the meantime reports
// as not alive. This is best-effort observation only — it never signals
// anything, and the marker check above is not a kill justification.
func liveProcCmdline(procRoot string, pid int) (string, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", false
	}
	return strings.ReplaceAll(string(data), "\x00", " "), true
}

// --- unit tests: no real daemon, no tmux, no real /proc --------------------

// liveCleanupForTest builds the helper directly (no t.Cleanup registration
// needed — run() is called explicitly) with a log buffer for assertions.
func liveCleanupForTest(base string, sandboxPID int, procRoot string, buf *strings.Builder) *liveTestCleanup {
	return &liveTestCleanup{
		c: &bl462LiveClient{
			base: base,
			http: &http.Client{Timeout: 5 * time.Second},
		},
		sandboxPID: sandboxPID,
		procRoot:   procRoot,
		logf: func(format string, args ...any) {
			buf.WriteString(fmt.Sprintf(format, args...) + "\n")
		},
	}
}

func writeFakeProcPID(t *testing.T, procRoot string, pid int, cmdline string) {
	t.Helper()
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making fake proc dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatalf("writing fake proc cmdline: %v", err)
	}
}

func TestLiveTestCleanupEndsTrackedSessionViaAPI(t *testing.T) {
	var killCalls []map[string]any
	var killErr bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != liveKillPath || r.Method != http.MethodPost {
			killErr = true
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			killErr = true
		} else {
			killCalls = append(killCalls, body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var buf strings.Builder
	lc := liveCleanupForTest(srv.URL, 0, t.TempDir(), &buf)
	lc.trackSession(bl462LiveSession{ID: "sess-probe", State: "running"})
	lc.run()

	if killErr {
		t.Fatalf("kill request was malformed: %v", killCalls)
	}
	if len(killCalls) != 1 {
		t.Fatalf("expected exactly one kill call via the daemon API, got %d; log:\n%s", len(killCalls), buf.String())
	}
	if killCalls[0]["id"] != "sess-probe" {
		t.Errorf("kill call body = %v; want id sess-probe", killCalls[0])
	}
	if !strings.Contains(buf.String(), "ended session sess-probe via the daemon API") {
		t.Errorf("log line missing the API-kill confirmation:\n%s", buf.String())
	}
}

func TestLiveTestCleanupSkipsAlreadyEndedSession(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	var buf strings.Builder
	lc := liveCleanupForTest(srv.URL, 0, t.TempDir(), &buf)
	lc.trackSession(bl462LiveSession{ID: "sess-probe", State: "terminated"})
	lc.run()

	if called {
		t.Errorf("session was already terminated; the cleanup must not call the daemon")
	}
	if !strings.Contains(buf.String(), "already terminated") {
		t.Errorf("log line missing the already-ended note:\n%s", buf.String())
	}
}

func TestLiveTestCleanupMakesNoCallWithoutSession(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	var buf strings.Builder
	lc := liveCleanupForTest(srv.URL, 0, t.TempDir(), &buf)
	lc.run()
	if called {
		t.Errorf("no session was ever observed; the cleanup must not call the daemon")
	}
	if !strings.Contains(buf.String(), "only known after a list call") {
		t.Errorf("log line missing the not-observed note:\n%s", buf.String())
	}
}

func TestLiveTestCleanupLogsSandboxPidGone(t *testing.T) {
	var buf strings.Builder
	lc := liveCleanupForTest("", 4242, t.TempDir(), &buf)
	lc.run()
	if !strings.Contains(buf.String(), "daemon pid 4242 is gone") {
		t.Errorf("expected the 'pid gone' verification line:\n%s", buf.String())
	}
}

func TestLiveTestCleanupLogsSandboxPidStillRecorded(t *testing.T) {
	proc := t.TempDir()
	writeFakeProcPID(t, proc, 4242, "/opt/dw/bin/datawatch\x00start\x00--foreground\x00")

	var buf strings.Builder
	lc := liveCleanupForTest("", 4242, proc, &buf)
	lc.run()
	if !strings.Contains(buf.String(), "still the recorded sandbox pid") ||
		!strings.Contains(buf.String(), "left running") {
		t.Errorf("expected 'still the recorded sandbox pid / left running' verification line:\n%s", buf.String())
	}
}

func TestLiveTestCleanupLogsRecordedReplacementPidGone(t *testing.T) {
	var buf strings.Builder
	lc := liveCleanupForTest("", 4242, t.TempDir(), &buf)
	lc.recordDaemonPID(7777)
	lc.run()
	if !strings.Contains(buf.String(), "daemon pid 7777 is gone") {
		t.Errorf("expected the replacement pid 'gone' verification line:\n%s", buf.String())
	}
}

func TestLiveTestCleanupIgnoresRecycledPid(t *testing.T) {
	proc := t.TempDir()
	writeFakeProcPID(t, proc, 4242, "/usr/lib/postgresql/bin/postgres\x00-D\x00/data")

	var buf strings.Builder
	lc := liveCleanupForTest("", 4242, proc, &buf)
	lc.run()
	if !strings.Contains(buf.String(), "may be recycled") ||
		!strings.Contains(buf.String(), "left untouched") {
		t.Errorf("expected the recycled-pid 'left untouched' line:\n%s", buf.String())
	}
}

func TestLiveTestCleanupSurvivesDaemonUnreachable(t *testing.T) {
	// base points at a closed port: the cleanup must log, not panic, and
	// still verify the recorded pid.
	var buf strings.Builder
	lc := liveCleanupForTest("http://127.0.0.1:1", 4242, t.TempDir(), &buf)
	lc.trackSession(bl462LiveSession{ID: "sess-probe", State: "running"})
	lc.run()
	if !strings.Contains(buf.String(), "could not end session sess-probe via the daemon API") {
		t.Errorf("expected the unreachable-API note:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "daemon pid 4242 is gone") {
		t.Errorf("pid verification must still run after an API failure:\n%s", buf.String())
	}
}
