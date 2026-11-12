package session

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Live, opt-in companion to the BL462 restart audit
// (docs/plans/bl462-restart-audit-findings.md). The audit reproduced by hand
// that a claude-code task whose tmux session is live survives a daemon
// restart: the pane stays, output keeps reaching the session log, and the
// session record is not failed. This test repeats that sequence against a
// real daemon so it can be re-run after changes.
//
// It is skipped unless the full sandbox contract below holds, so `go test
// ./...` in CI skips it. Environment:
//
//	DW_LIVE_RESTART_TEST=1          gate (required)
//	DW_BASE=https://host:port       sandbox daemon under test (required, no
//	                                default: never point this at production by
//	                                accident)
//	DW_BASE_TOKEN=...               bearer token, if the daemon has one
//	DW_BASE_INSECURE=0              verify TLS (default: skip verify, as the
//	                                other DW_BASE scripts do)
//	DW_LIVE_RESTART_PROJECT_DIR=... project_dir for the probe session (optional)
//	DW_LIVE_SANDBOX_PID=<pid>       numeric pid of that sandbox daemon (required;
//	                                verified live and datawatch before any restart)
//	DW_LIVE_RESTART_CMD='...'       shell command that restarts exactly that pid.
//	                                Only for a sandbox daemon this suite owns.
//	                                Required — there is no default path.
//
// Production guard (docs/plans/2026-10-23-daemon-stop-cause.md, finding #3
// remediation): this test may only restart a sandbox daemon the operator
// started and identified by pid. The earlier defaults — service-manager
// restart of the datawatch unit, and the datawatch CLI stop/restart command
// — could have signalled the operator's production daemon, so they are
// removed. Without DW_LIVE_RESTART_CMD and DW_LIVE_SANDBOX_PID the test
// t.Skip()s with an explanatory message and never falls back to
// production (enforced again by scripts/check-no-production-service-control.sh).
//
// The test must run on the daemon's host with the same TMUX_TMPDIR as the
// daemon, because it checks the tmux session and reads the session log file.
//
// Restart path: the operator starts the sandbox daemon themselves (e.g.
// scripts/sec-assess/sandbox.sh) and passes both its pid and the restart
// command, e.g.
//
//	DW_LIVE_SANDBOX_PID=12345 \
//	DW_LIVE_RESTART_CMD='sh -c "kill 12345; exec /sb/bin/datawatch start --foreground --config /sb/config.yaml >/sb/daemon.log 2>&1 &"'
//
// The command may restart anything inside the sandbox it owns — as long as
// the pid it signals is the one passed in DW_LIVE_SANDBOX_PID. Before
// anything runs, bl462SandboxRestartContract verifies that pid is a live
// process and — on Linux, via /proc/<pid>/cmdline — a datawatch daemon, so
// the pid a restart would signal is a real sandbox datawatch process, not a
// recycled pid number.
//
// The test itself never calls a service manager, never signals a process by
// itself — it only runs the operator-supplied restart command from
// DW_LIVE_RESTART_CMD, which it refuses to run unless the pid contract above
// holds.
//
// Restart subprocess environment: per the restart-safety spec, the restart
// command's environment is built by bl462RestartEnv (TMUX and TMUX_TMPDIR
// stripped, no exceptions). An operator-supplied DW_LIVE_RESTART_CMD is
// responsible for giving the restarted daemon the socket dir it needs (e.g.
// export TMUX_TMPDIR inside the command).
//
// Run with a timeout above the default 10 minute test timeout:
//
//	DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 20m -run TestBL462_DaemonRestartSurvivesLive -v ./internal/session/
const (
	bl462LiveStartWait      = 3 * time.Minute
	bl462LiveRestartWait    = 2 * time.Minute
	bl462LiveLogWait        = 90 * time.Second
	bl462LiveCompletionWait = 4 * time.Minute
	bl462LiveTicks          = 240
	bl462LivePoll           = 2 * time.Second
)

// bl462LiveSession is the subset of the daemon's session JSON this test reads.
type bl462LiveSession struct {
	ID          string `json:"id"`
	FullID      string `json:"full_id"`
	Task        string `json:"task"`
	State       string `json:"state"`
	TmuxSession string `json:"tmux_session"`
	LogFile     string `json:"log_file"`
}

// bl462LiveClient is a minimal REST client for the daemon under test.
type bl462LiveClient struct {
	base  string
	token string
	http  *http.Client
}

func newBL462LiveClient(t *testing.T) *bl462LiveClient {
	t.Helper()
	base := strings.TrimRight(os.Getenv("DW_BASE"), "/")
	if base == "" {
		t.Fatal("DW_BASE must be set to the daemon under test (e.g. https://127.0.0.1:18443)")
	}
	tr := &http.Transport{}
	if strings.HasPrefix(base, "https://") && os.Getenv("DW_BASE_INSECURE") != "0" {
		// Matches the DW_BASE scripts: sandbox daemons use self-signed certs.
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	}
	return &bl462LiveClient{
		base:  base,
		token: os.Getenv("DW_BASE_TOKEN"),
		http:  &http.Client{Timeout: 15 * time.Second, Transport: tr},
	}
}

// do sends one request and decodes a JSON response into out when out is non-nil.
func (c *bl462LiveClient) do(method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// findSession returns the session whose task contains token.
func (c *bl462LiveClient) findSession(token string) (bl462LiveSession, bool) {
	var list []bl462LiveSession
	if status, err := c.do(http.MethodGet, "/api/sessions", nil, &list); err != nil || status >= 300 {
		return bl462LiveSession{}, false
	}
	for _, s := range list {
		if strings.Contains(s.Task, token) {
			return s, true
		}
	}
	return bl462LiveSession{}, false
}

// uptimeSeconds reads the daemon's uptime, used to prove a real restart happened.
func (c *bl462LiveClient) uptimeSeconds() (float64, error) {
	var h map[string]any
	status, err := c.do(http.MethodGet, "/api/health", nil, &h)
	if err != nil {
		return 0, err
	}
	if status >= 300 {
		return 0, fmt.Errorf("/api/health returned HTTP %d", status)
	}
	up, ok := h["uptime_seconds"].(float64)
	if !ok {
		return 0, fmt.Errorf("/api/health has no uptime_seconds field")
	}
	return up, nil
}

// bl462TmuxHasSession reports whether tmux knows the session. TMUX is removed
// from the environment so a test run from inside tmux still uses TMUX_TMPDIR.
func bl462TmuxHasSession(name string) bool {
	if name == "" {
		return false
	}
	cmd := exec.Command("tmux", "has-session", "-t", name) // #nosec G204 -- name comes from the daemon's own session record
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	return cmd.Run() == nil
}

// bl462RestartEnv returns the environment for the restart subprocess. The
// restart-safety spec requires restart subprocesses to run without TMUX or
// TMUX_TMPDIR: inheriting TMUX would point tmux clients at the parent test
// shell's server, and the restart command must not depend on ambient session
// variables. There is no exception: the CLI-fallback exception that kept
// TMUX_TMPDIR was removed with the fallback it served. An operator-supplied
// DW_LIVE_RESTART_CMD must export whatever its daemon needs inside the
// command itself.
func bl462RestartEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// bl462SandboxRestartContract enforces the production guard shared by all
// live-restart tests in this package and returns the sandbox daemon pid that
// is allowed to be restarted. It skips (rather than fails) when the operator
// has not supplied the sandbox contract, and fails hard when the pid a
// restart would signal cannot be verified as a live datawatch daemon — in
// that case a restart command would be signalling a pid this suite does not
// own.
func bl462SandboxRestartContract(t *testing.T) int {
	t.Helper()
	if os.Getenv("DW_LIVE_RESTART_TEST") != "1" {
		t.Skip("set DW_LIVE_RESTART_TEST=1 to run against a live daemon")
	}
	if os.Getenv("DW_BASE") == "" {
		t.Skip("DW_BASE must point at the sandbox daemon under test; these tests never fall back to a default URL or a production daemon")
	}
	if strings.TrimSpace(os.Getenv("DW_LIVE_RESTART_CMD")) == "" {
		t.Skip("DW_LIVE_RESTART_CMD is required, and it must restart only the sandbox daemon you started. There is no default: these tests do not restart a production datawatch unit and do not run the datawatch CLI stop/restart command. Set DW_BASE to the sandbox daemon URL, DW_LIVE_SANDBOX_PID to its pid, and DW_LIVE_RESTART_CMD to a command that restarts exactly that pid")
	}
	pidStr := strings.TrimSpace(os.Getenv("DW_LIVE_SANDBOX_PID"))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		t.Skipf("DW_LIVE_SANDBOX_PID must be the numeric pid of the sandbox daemon this test may restart (got %q); the test verifies the pid a restart would signal and refuses to run without it", pidStr)
	}
	if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
		t.Fatalf("DW_LIVE_SANDBOX_PID=%d is not a live process; refusing to run a restart command targeting a pid this suite does not own (the number may be recycled)", pid)
	}
	if b, rerr := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); rerr == nil {
		joined := strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
		if !strings.Contains(joined, "datawatch") {
			t.Fatalf("pid %d does not look like a datawatch daemon (cmdline=%q); refusing to let a restart command signal it — point DW_LIVE_SANDBOX_PID at the sandbox daemon this suite owns", pid, joined)
		}
		t.Logf("verified: pid %d is a live datawatch daemon (cmdline=%q); restart confined to this sandbox pid", pid, joined)
	} else {
		t.Logf("/proc/%d/cmdline not readable (%v): liveness verified only; DW_LIVE_RESTART_CMD must signal exactly pid %d", pid, rerr, pid)
	}
	return pid
}

// bl462RestartCommand returns the restart command and a label. The command's
// Env is built by bl462RestartEnv (TMUX and TMUX_TMPDIR stripped).
//
// Only DW_LIVE_RESTART_CMD is honored. The earlier defaults — a
// service-manager restart of the datawatch unit, then the datawatch CLI
// stop/restart command — were removed (docs/plans/
// 2026-10-23-daemon-stop-cause.md, finding #3): they could have signalled the
// operator's production daemon.
func bl462RestartCommand(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	pid := bl462SandboxRestartContract(t) // guard: skip/fatal unless the contract holds
	override := os.Getenv("DW_LIVE_RESTART_CMD")
	cmd := exec.Command("sh", "-c", override) // #nosec G204 -- operator-supplied restart command; contract above limits it to the sandbox pid
	cmd.Env = bl462RestartEnv()
	label := fmt.Sprintf("DW_LIVE_RESTART_CMD override (sandbox pid %d verified live at contract check), env: TMUX+TMUX_TMPDIR stripped", pid)
	t.Logf("restart env: TMUX=%q -> stripped, TMUX_TMPDIR=%q -> stripped", os.Getenv("TMUX"), os.Getenv("TMUX_TMPDIR"))
	return cmd, label
}

func bl462PollUntil(d time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if fn() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(bl462LivePoll)
	}
}

func bl462IsTerminal(state string) bool {
	return state == "complete" || state == "failed" || state == "killed"
}

// TestBL462_DaemonRestartSurvivesLive starts a long claude-code task session on
// a live daemon, restarts the daemon, and checks that the session survives.
//
// Live result: NOT RUN for this change. The gated path needs a daemon that the
// test may restart. The only daemon on this host is the operator's production
// daemon, which must not be restarted from here. No sandbox daemon was started,
// because the sandbox config would have to live outside the repo, and the write
// scope for this task is the repo. The skip path is verified by a plain run:
// `go test -run TestBL462_DaemonRestartSurvivesLive -v ./internal/session/`.
func TestBL462_DaemonRestartSurvivesLive(t *testing.T) {
	// Production guard: without the sandbox contract (DW_BASE +
	// DW_LIVE_SANDBOX_PID + DW_LIVE_RESTART_CMD) this test skips and never
	// falls back to a production unit.
	sandboxPID := bl462SandboxRestartContract(t)
	c := newBL462LiveClient(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux is required on the test host: %v", err)
	}

	// Unique token so this run's session can be found in the list.
	token := fmt.Sprintf("bl462-live-%d", time.Now().UnixNano())
	task := fmt.Sprintf("%s. Run this bash command exactly, wait for it to finish, then reply DATAWATCH_COMPLETE: done. "+
		"for i in $(seq 1 %d); do echo \"bl462 tick $i\"; sleep 2; done", token, bl462LiveTicks)
	startBody := map[string]any{"task": task, "backend": "claude-code"}
	if dir := os.Getenv("DW_LIVE_RESTART_PROJECT_DIR"); dir != "" {
		startBody["project_dir"] = dir
	}

	// End the probe session via the daemon API — and verify the recorded
	// sandbox daemon pid — on every exit path (pass, failure, interrupt).
	// Registered before the first call that can create the session.
	lc := newLiveTestCleanup(t, c, sandboxPID)

	// (a) start the probe session and wait for it to be running with a tmux session.
	if status, err := c.do(http.MethodPost, "/api/sessions/start", startBody, nil); err != nil || status >= 300 {
		t.Fatalf("start probe session: HTTP %d, err %v", status, err)
	}
	var sess bl462LiveSession
	if !bl462PollUntil(bl462LiveStartWait, func() bool {
		s, ok := c.findSession(token)
		if !ok {
			return false
		}
		sess = s
		lc.trackSession(s)
		return (s.State == "running" || s.State == "waiting_input") && bl462TmuxHasSession(s.TmuxSession)
	}) {
		t.Fatalf("probe session never reached running with a tmux session (last: id=%q state=%q tmux=%q)", sess.ID, sess.State, sess.TmuxSession)
	}
	t.Logf("probe session %s running in tmux %s", sess.ID, sess.TmuxSession)

	// The log must exist and be readable from this host before the restart.
	if !bl462PollUntil(bl462LiveStartWait, func() bool {
		fi, err := os.Stat(sess.LogFile)
		return err == nil && fi.Size() > 0
	}) {
		t.Fatalf("session log %q is missing or empty; the test must run on the daemon's host", sess.LogFile)
	}
	fi, err := os.Stat(sess.LogFile)
	if err != nil {
		t.Fatalf("stat session log before restart: %v", err)
	}
	preLogSize := fi.Size()
	preUptime, err := c.uptimeSeconds()
	if err != nil {
		t.Fatalf("read daemon uptime before restart: %v", err)
	}

	// (b) restart the daemon the way the operator does.
	cmd, label := bl462RestartCommand(t)
	t.Logf("restart path: %s", label)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart via %s failed: %v\n%s", label, err, out)
	}

	// (c) wait for the daemon to come back. A lower uptime than before proves it restarted.
	if !bl462PollUntil(bl462LiveRestartWait, func() bool {
		up, err := c.uptimeSeconds()
		return err == nil && up < preUptime
	}) {
		t.Fatalf("daemon did not come back with a reset uptime within %s (before: %.0fs)", bl462LiveRestartWait, preUptime)
	}

	// (d) assert survival.
	if !bl462TmuxHasSession(sess.TmuxSession) {
		t.Errorf("tmux session %s is gone after restart", sess.TmuxSession)
	}

	after, ok := c.findSession(token)
	if !ok {
		t.Fatalf("probe session %s is missing from the daemon after restart", sess.ID)
	}
	lc.trackSession(after)
	switch after.State {
	case "running", "waiting_input":
		// Expected: the session survived the restart.
	case "failed":
		t.Errorf("BUG (BL462 F1/F4): session %s is failed after restart while its tmux session should be live", after.ID)
	case "killed":
		t.Errorf("session %s is killed after restart; a live task should not be killed by a restart", after.ID)
	case "complete":
		t.Errorf("session %s reads complete right after restart; the %d-tick loop should still be running, so this is spurious",
			after.ID, bl462LiveTicks)
	default:
		t.Logf("session %s state after restart: %q", after.ID, after.State)
	}

	// The re-piped output must reach the same log file after the restart.
	if !bl462PollUntil(bl462LiveLogWait, func() bool {
		fi, err := os.Stat(sess.LogFile)
		return err == nil && fi.Size() > preLogSize
	}) {
		t.Errorf("session log %s did not grow after restart (size stayed at %d): output is no longer re-piped", sess.LogFile, preLogSize)
	}

	// Completion is a bonus check. The guarantee under test is survival.
	if !bl462PollUntil(bl462LiveCompletionWait, func() bool {
		s, ok := c.findSession(token)
		if !ok {
			return false
		}
		lc.trackSession(s)
		return bl462IsTerminal(s.State)
	}) {
		t.Logf("probe session still %q after %s; completion not asserted", lc.sessionState, bl462LiveCompletionWait)
		return
	}
	if lc.sessionState == "complete" {
		t.Logf("probe session reached complete")
		return
	}
	t.Errorf("probe session reached terminal state %q; expected complete", lc.sessionState)
}
