package session

// Live, opt-in companion to the boot-resume live-session adoption behavior:
// an autonomous PRD whose worker session survives a daemon restart must be
// ADOPTED by the restarted daemon, not reset. The restarted daemon must NOT
// kill the live session, must NOT spawn a second worker (no duplicate), and
// must let the surviving session finish and drive the task to verification /
// completion.
//
// This is the PRD-level counterpart of TestBL462_DaemonRestartSurvivesLive
// (restart_safety_live_test.go, session-level survival): it reuses that file's
// helpers (bl462LiveClient, bl462RestartCommand, bl462PollUntil,
// bl462TmuxHasSession) and the same operator/sandbox restart paths.
//
// It is skipped unless the full sandbox contract holds (see below), so
// `go test ./...` in CI skips it. Environment:
//
//	DW_LIVE_RESTART_TEST=1          gate (required)
//	DW_BASE=https://host:port       sandbox daemon under test (required, no
//	                                default: never point this at production by
//	                                accident)
//	DW_BASE_TOKEN=...               bearer token, if the daemon has one
//	DW_BASE_INSECURE=0              verify TLS (default: skip verify, as the
//	                                other DW_BASE scripts do)
//	DW_LIVE_RESTART_PROJECT_DIR=... project_dir for the probe PRD (optional;
//	                                default: the daemon's own project-dir
//	                                policy, e.g. worktree mode)
//	DW_LIVE_RESTART_BACKEND=...     worker backend for the claude-code variant
//	                                (TestDaemonRestartAdoptsLiveClaudeCodeWorkerLive);
//	                                default claude-code. Ignored by the default-
//	                                backend test.
//	DW_LIVE_SANDBOX_PID=<pid>       numeric pid of that sandbox daemon (required;
//	                                verified live and datawatch before any restart)
//	DW_LIVE_RESTART_CMD='...'       shell command that restarts exactly that pid.
//	                                Only for a sandbox daemon this suite owns.
//	                                Required — there is no default path (see
//	                                restart_safety_live_test.go for the
//	                                production guard).
//
// The restart subprocess runs with TMUX and TMUX_TMPDIR removed from its
// environment (see bl462RestartEnv in restart_safety_live_test.go), no
// exceptions. The test itself never stops a tmux server and never restarts a
// production datawatch unit or runs the datawatch CLI stop/restart command.
// The test must run on the daemon's host with the same TMUX_TMPDIR as the
// daemon (see restart_safety_live_test.go).
//
// Run with a timeout above the default 10 minute test timeout:
//
//	DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 25m -run TestDaemonRestartAdoptsLiveWorkerLive -v ./internal/session/
//
// The claude-code variant reproduces the field failure (a long-delivered
// claude-code worker falsely completed/killed after a restart):
//
//	DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 \
//	  env -u TMUX -u TMUX_TMPDIR go test -count=1 -timeout 25m \
//	  -run TestDaemonRestartAdoptsLiveClaudeCodeWorkerLive -v ./internal/session/
//
// Cleanup only ever goes through the API; no test here stops a tmux server.

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	adoptLivePlanWait   = 8 * time.Minute  // decompose is an LLM call
	adoptLiveRunWait    = 5 * time.Minute  // worker must start before we restart
	adoptLiveSettleWait = 5 * time.Minute  // adopted worker must finish after restart
	adoptLiveEarlyWatch = 60 * time.Second // post-restart window of per-poll state checks
	adoptLiveMinRunning = 45 * time.Second // claude-code: session must run this long before restart
	adoptLiveTicks      = 85               // 85 * 2s ≈ 2.8min worker loop
)

// adoptLiveTask is one entry of the PRD's task tree as this test reads it.
type adoptLiveTask struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// adoptLivePRD is the subset of the daemon's PRD JSON this test reads.
type adoptLivePRD struct {
	ID         string `json:"id"`
	Spec       string `json:"spec"`
	Status     string `json:"status"`
	ProjectDir string `json:"project_dir"`
	Blocked    string `json:"blocked_reason"`
	Stories    []struct {
		ID    string          `json:"id"`
		Tasks []adoptLiveTask `json:"tasks"`
	} `json:"stories"`
}

func (p *adoptLivePRD) tasks() []adoptLiveTask {
	var out []adoptLiveTask
	for _, s := range p.Stories {
		out = append(out, s.Tasks...)
	}
	return out
}

// sessionByID returns the session whose id or full_id equals id.
func (c *bl462LiveClient) sessionByID(id string) (bl462LiveSession, bool) {
	var list []bl462LiveSession
	if status, err := c.do(http.MethodGet, "/api/sessions", nil, &list); err != nil || status >= 300 {
		return bl462LiveSession{}, false
	}
	for _, s := range list {
		if s.ID == id || s.FullID == id || strings.Contains(s.FullID, id) {
			return s, true
		}
	}
	return bl462LiveSession{}, false
}

// prdByID fetches one PRD (full view).
func (c *bl462LiveClient) prdByID(id string) (adoptLivePRD, error) {
	var p adoptLivePRD
	status, err := c.do(http.MethodGet, "/api/autonomous/prds/"+id, nil, &p)
	if err != nil || status >= 300 {
		return p, fmt.Errorf("GET PRD: HTTP %d, err %v", status, err)
	}
	return p, nil
}

func TestDaemonRestartAdoptsLiveWorkerLive(t *testing.T) {
	runDaemonRestartAdoptsLiveWorker(t, "", 0)
}

// TestDaemonRestartAdoptsLiveClaudeCodeWorkerLive runs the same scenario with
// a claude-code worker that has been running well past the first 30s before
// the restart, then also asserts the session is never marked complete/killed
// while the worker is still running.
func TestDaemonRestartAdoptsLiveClaudeCodeWorkerLive(t *testing.T) {
	backend := os.Getenv("DW_LIVE_RESTART_BACKEND")
	if backend == "" {
		backend = "claude-code"
	}
	runDaemonRestartAdoptsLiveWorker(t, backend, adoptLiveMinRunning)
}

// sessionTimelineHasKilled reports whether the session's timeline records a
// 'killed' entry.
func (c *bl462LiveClient) sessionTimelineHasKilled(id string) (bool, error) {
	var tl struct {
		Lines []string `json:"lines"`
	}
	status, err := c.do(http.MethodGet, "/api/sessions/timeline?id="+id, nil, &tl)
	if err != nil || status >= 300 {
		return false, fmt.Errorf("GET timeline: HTTP %d, err %v", status, err)
	}
	for _, l := range tl.Lines {
		if strings.Contains(strings.ToLower(l), "killed") {
			return true, nil
		}
	}
	return false, nil
}

// runDaemonRestartAdoptsLiveWorker is the shared body. backend "" uses the
// daemon default; minRunning > 0 makes the test wait until the worker session
// has been running that long before restarting (and enables the strict
// post-restart watch for a premature complete/killed state).
func runDaemonRestartAdoptsLiveWorker(t *testing.T, backend string, minRunning time.Duration) {
	if os.Getenv("DW_LIVE_RESTART_TEST") != "1" {
		t.Skip("set DW_LIVE_RESTART_TEST=1 to run against a live daemon")
	}
	// Production guard: without the full sandbox contract the shared
	// bl462RestartCommand path skips; check it here so the test does not
	// set up a probe PRD before discovering it cannot restart the daemon.
	sandboxPID := bl462SandboxRestartContract(t)
	c := newBL462LiveClient(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux is required on the test host: %v", err)
	}

	// Unique token identifying this probe in logs and session task text.
	token := fmt.Sprintf("adopt-live-%d", time.Now().UnixNano())
	// A long single worker: run a ~3-minute bash loop, then complete.
	ticks := adoptLiveTicks // ≈ 2.8min, comfortably inside the wait windows.
	spec := fmt.Sprintf(
		"Probe PRD %s. The whole story is ONE task. That task: run this bash command exactly, wait for it to finish, then reply DATAWATCH_COMPLETE: done.\n"+
			"for i in $(seq 1 %d); do echo \"%s tick $i\"; sleep 2; done",
		token, ticks, token)

	startBody := map[string]any{"spec": spec}
	if backend != "" {
		startBody["backend"] = backend
	}
	if dir := os.Getenv("DW_LIVE_RESTART_PROJECT_DIR"); dir != "" {
		startBody["project_dir"] = dir
	}

	// End the probe session via the daemon API — and verify the recorded
	// sandbox daemon pid — on every exit path (pass, failure, interrupt).
	// Registered before the first call that can create the PRD (and its
	// worker session).
	lc := newLiveTestCleanup(t, c, sandboxPID)

	// (a) create the PRD.
	var prd adoptLivePRD
	if status, err := c.do(http.MethodPost, "/api/autonomous/prds", startBody, &prd); err != nil || status >= 300 {
		t.Fatalf("create PRD: HTTP %d, err %v", status, err)
	}
	if prd.ID == "" {
		t.Fatalf("create PRD: empty id in response")
	}
	t.Logf("PRD %s created (status=%q project_dir=%q)", prd.ID, prd.Status, prd.ProjectDir)

	// (b) plan (decompose) and wait for it to finish.
	if status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/plan", nil, nil); err != nil && status >= 500 {
		t.Fatalf("kick plan: HTTP %d, err %v", status, err)
	}
	planDone := false
	bl462PollUntil(adoptLivePlanWait, func() bool {
		var st struct {
			Status string `json:"status"`
		}
		status, err := c.do(http.MethodGet, "/api/autonomous/prds/"+prd.ID+"/plan/status", nil, &st)
		if err != nil || status >= 300 {
			return false
		}
		planDone = st.Status == "no_active_job"
		return planDone
	})
	if !planDone {
		t.Fatalf("plan never finished within %s", adoptLivePlanWait)
	}
	prd, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prd.tasks()) == 0 {
		t.Fatalf("PRD %s has no tasks after planning (status=%q spec=%q)", prd.ID, prd.Status, prd.Spec)
	}
	t.Logf("PRD %s planned (status=%q, %d task(s))", prd.ID, prd.Status, len(prd.tasks()))

	// (c) approve. Some daemons gate approval on a plan-coverage ack; if the
	// first approve fails, acknowledge once and retry.
	approve := func() error {
		status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/approve", map[string]any{"actor": "operator", "note": "live adopt test"}, nil)
		if err != nil || status >= 300 {
			return fmt.Errorf("approve: HTTP %d, err %v", status, err)
		}
		return nil
	}
	if err := approve(); err != nil {
		ackBody := map[string]any{"actor": "operator", "note": "live adopt test: ack to proceed"}
		if ackStatus, ackErr := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/plan_coverage/acknowledge", ackBody, nil); ackErr == nil && ackStatus < 300 {
			if retryErr := approve(); retryErr != nil {
				t.Fatalf("approve (after plan-coverage ack): %v", retryErr)
			}
		} else {
			t.Fatalf("approve failed and plan-coverage ack was not possible (HTTP %d, err %v): %v", ackStatus, ackErr, err)
		}
	}
	prd, err = c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prd.Status != "approved" && prd.Status != "running" {
		t.Fatalf("PRD status after approve is %q (blocked_reason=%q); want approved", prd.Status, prd.Blocked)
	}

	// (d) run and wait for the worker session to be live.
	if status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/run", nil, nil); err != nil || status >= 500 {
		t.Fatalf("run PRD: HTTP %d, err %v", status, err)
	}
	var taskSession string
	found := false
	bl462PollUntil(adoptLiveRunWait, func() bool {
		p, err := c.prdByID(prd.ID)
		if err != nil {
			return false
		}
		for _, tk := range p.tasks() {
			if tk.SessionID != "" {
				taskSession = tk.SessionID
				found = true
				return true
			}
			if tk.Status == "completed" || tk.Status == "failed" {
				t.Logf("watch: task %s already %s before session was captured", tk.ID, tk.Status)
			}
		}
		return false
	})
	if !found {
		p, _ := c.prdByID(prd.ID)
		for _, tk := range p.tasks() {
			t.Logf("task %s: status=%q session=%q error=%q", tk.ID, tk.Status, tk.SessionID, tk.Error)
		}
		t.Fatalf("no worker session started within %s (PRD status=%s)", adoptLiveRunWait, p.Status)
	}
	sess, ok := c.sessionByID(taskSession)
	if !ok {
		t.Fatalf("worker session %s not found in /api/sessions", taskSession)
	}
	lc.trackSession(sess)
	t.Logf("worker session %s running (state=%q tmux=%q) before restart", sess.ID, sess.State, sess.TmuxSession)
	capturedAt := time.Now()
	if minRunning > 0 {
		// Let the delivered task run well past the first 30s so the restart
		// lands mid-work, as in the field failure.
		if time.Since(capturedAt) < minRunning {
			time.Sleep(minRunning)
		}
		sess, ok = c.sessionByID(taskSession)
		if !ok {
			t.Fatalf("worker session %s vanished before restart", taskSession)
		}
		lc.trackSession(sess)
		if sess.State != "running" && sess.State != "waiting_input" {
			t.Fatalf("worker session %s is %q before restart; want running/waiting_input (worker ended too early to test adoption)", sess.ID, sess.State)
		}
	}
	// A worker whose loop needs ticks*2s cannot truly finish before this.
	earliestFinish := capturedAt.Add(time.Duration(ticks)*2*time.Second - 30*time.Second)

	// (e) restart the daemon the way the operator does.
	preUptime, err := c.uptimeSeconds()
	if err != nil {
		t.Fatalf("read daemon uptime before restart: %v", err)
	}
	cmd, label := bl462RestartCommand(t)
	t.Logf("restart path: %s (Env is set by bl462RestartEnv: TMUX and TMUX_TMPDIR are stripped)", label)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart via %s failed: %v\n%s", label, err, out)
	}

	// (f) wait for the daemon to come back. A lower uptime proves the restart.
	restored := false
	bl462PollUntil(bl462LiveRestartWait, func() bool {
		up, err := c.uptimeSeconds()
		restored = err == nil && up < preUptime
		return restored
	})
	if !restored {
		t.Fatalf("daemon did not come back with a reset uptime within %s (before: %.0fs)", bl462LiveRestartWait, preUptime)
	}
	t.Log("daemon restarted; checking adoption")

	// (g) immediate post-restart assertions: the live session must not have
	// been killed, and the task must still point at the SAME session (no
	// duplicate worker, no reset to pending).
	after, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range after.tasks() {
		if tk.SessionID != "" && tk.SessionID != taskSession {
			t.Errorf("BUG (duplicate worker): task %s was re-spawned with session %q; must keep the adopted session %q",
				tk.ID, tk.SessionID, taskSession)
		}
		if tk.SessionID == "" && (tk.Status == "pending" || tk.Status == "queued") {
			t.Logf("watch: task %s reset to %q after restart; the live session %s must be adopted, not re-queued", tk.ID, tk.Status, taskSession)
		}
	}
	if after.Status == "cancelled" || after.Status == "failed" {
		t.Errorf("PRD %s is %s right after restart (blocked_reason=%q); a live worker should be adopted", after.ID, after.Status, after.Blocked)
	}
	sess, ok = c.sessionByID(taskSession)
	if !ok {
		t.Fatalf("worker session %s vanished from the daemon after restart", taskSession)
	}
	lc.trackSession(sess)
	switch sess.State {
	case "running", "waiting_input", "complete":
		// Expected: session survived (or finished in the restart window).
	case "failed":
		t.Errorf("BUG: worker session %s is failed after restart while its tmux session should be live", sess.ID)
	case "killed":
		t.Errorf("BUG: worker session %s is killed after restart; adopt-live must never kill a live worker", sess.ID)
	default:
		t.Logf("worker session %s state after restart: %q", sess.ID, sess.State)
	}
	if !bl462IsTerminal(sess.State) && !bl462TmuxHasSession(sess.TmuxSession) {
		t.Errorf("worker session %s is %q but its tmux session %s is gone; the pane was lost in the restart", sess.ID, sess.State, sess.TmuxSession)
	}
	if n := countTaskSessions(after, taskSession); n > 1 {
		t.Errorf("BUG (duplicate worker): %d tasks share session %s; one PRD task should own it", n, taskSession)
	}

	// (g2) strict watch (minRunning > 0): for ~60s after restart the session
	// must stay running/waiting_input and keep the same task->session link; a
	// complete/killed state before the worker's loop could have ended is the
	// false-completion regression.
	if minRunning > 0 {
		bl462PollUntil(adoptLiveEarlyWatch, func() bool {
			s, ok := c.sessionByID(taskSession)
			if !ok {
				t.Errorf("worker session %s vanished after restart", taskSession)
				return true
			}
			lc.trackSession(s)
			if time.Now().Before(earliestFinish) && s.State != "running" && s.State != "waiting_input" {
				t.Errorf("BUG: worker session %s is %q only %s after delivery; the worker is still running", s.ID, s.State, time.Since(capturedAt).Round(time.Second))
				return true
			}
			if p, err := c.prdByID(prd.ID); err == nil {
				if n := countTaskSessions(p, taskSession); n > 1 {
					t.Errorf("BUG (duplicate worker): %d tasks share session %s", n, taskSession)
				}
				for _, tk := range p.tasks() {
					if tk.SessionID != "" && tk.SessionID != taskSession {
						t.Errorf("task %s re-assigned to session %q after restart (duplicate worker)", tk.ID, tk.SessionID)
					}
				}
			}
			return false
		})
	}

	// (h) the adopted worker must finish and drive the task to verification /
	// completion (never killed, never re-spawned).
	deadline := false
	bl462PollUntil(adoptLiveSettleWait, func() bool {
		p, err := c.prdByID(prd.ID)
		if err != nil {
			return false
		}
		for _, tk := range p.tasks() {
			if tk.SessionID != taskSession && tk.SessionID != "" {
				t.Errorf("task %s re-assigned to session %q after restart (duplicate worker)", tk.ID, tk.SessionID)
			}
		}
		done := false
		for _, tk := range p.tasks() {
			if tk.Status == "completed" || tk.Status == "failed" || tk.Status == "cancelled" {
				done = true
			}
		}
		deadline = done
		return done
	})
	if s, ok := c.sessionByID(taskSession); ok {
		lc.trackSession(s)
		if s.State == "killed" {
			t.Errorf("worker session %s ended as killed; adopt-live must never kill the surviving worker", s.ID)
		}
	}
	if minRunning > 0 {
		if killed, err := c.sessionTimelineHasKilled(taskSession); err != nil {
			t.Logf("could not read timeline for %s: %v", taskSession, err)
		} else if killed {
			t.Errorf("BUG: session %s timeline has a 'killed' entry; the worker must never be killed", taskSession)
		}
	}
	if !deadline {
		p, _ := c.prdByID(prd.ID)
		for _, tk := range p.tasks() {
			t.Logf("task %s: status=%q session=%q error=%q (settle window elapsed)", tk.ID, tk.Status, tk.SessionID, tk.Error)
		}
		// Survival+no-duplicate above are the hard assertions; reaching a
		// terminal state within the window is a bonus check like BL462's.
		t.Logf("PRD task did not reach a terminal state within %s (PRD status=%s); adoption checks still applied", adoptLiveSettleWait, p.Status)
		return
	}
	p, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range p.tasks() {
		if tk.Status == "completed" || (minRunning > 0 && tk.Status == "verifying") {
			t.Logf("task %s reached %s through the adopted session %s", tk.ID, tk.Status, taskSession)
			return
		}
	}
	t.Errorf("task reached a terminal state that was not completed (see task logs above)")
}

// countTaskSessions counts how many of the PRD's tasks point at the given
// session id; more than one means the same worker identity was re-issued
// to multiple task slots (a duplicate-spawn symptom within the PRD tree).
func countTaskSessions(p adoptLivePRD, want string) int {
	n := 0
	for _, tk := range p.tasks() {
		if tk.SessionID == want {
			n++
		}
	}
	return n
}
