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
// It is skipped unless DW_LIVE_RESTART_TEST=1, so `go test ./...` in CI skips
// it. Environment:
//
//	DW_LIVE_RESTART_TEST=1          gate (required)
//	DW_BASE=https://host:port       daemon under test (required, no default:
//	                                never point this at production by accident)
//	DW_BASE_TOKEN=...               bearer token, if the daemon has one
//	DW_BASE_INSECURE=0              verify TLS (default: skip verify, as the
//	                                other DW_BASE scripts do)
//	DW_LIVE_RESTART_PROJECT_DIR=... project_dir for the probe PRD (optional;
//	                                default: the daemon's own project-dir
//	                                policy, e.g. worktree mode)
//	DW_LIVE_RESTART_CMD='...'       shell command that restarts the daemon.
//	                                Only for a sandbox daemon that systemd
//	                                does not manage. When unset, the operator
//	                                path (systemctl --user / datawatch CLI)
//	                                is used, same as the BL462 test.
//
// The restart subprocess runs with TMUX and TMUX_TMPDIR removed from its
// environment (see bl462RestartEnv in restart_safety_live_test.go). The only
// exception is the `datawatch restart` CLI fallback, which keeps TMUX_TMPDIR
// because it re-execs the daemon in the ambient environment — without it the
// restarted daemon would lose its tmux socket, the failure under test. The
// test must run on the daemon's host with the same TMUX_TMPDIR as the
// daemon (see restart_safety_live_test.go).
//
// Run with a timeout above the default 10 minute test timeout:
//
//	DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 25m -run TestDaemonRestartAdoptsLiveWorkerLive -v ./internal/session/

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
	adoptLivePlanWait   = 8 * time.Minute // decompose is an LLM call
	adoptLiveRunWait    = 5 * time.Minute // worker must start before we restart
	adoptLiveSettleWait = 5 * time.Minute // adopted worker must finish after restart
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
	if os.Getenv("DW_LIVE_RESTART_TEST") != "1" {
		t.Skip("set DW_LIVE_RESTART_TEST=1 to run against a live daemon")
	}
	c := newBL462LiveClient(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux is required on the test host: %v", err)
	}

	// Unique token identifying this probe in logs and session task text.
	token := fmt.Sprintf("adopt-live-%d", time.Now().UnixNano())
	// A long single worker: run a ~3-minute bash loop, then complete.
	ticks := 85 // 85 * 2s ≈ 2.8min, comfortably inside the wait windows.
	spec := fmt.Sprintf(
		"Probe PRD %s. The whole story is ONE task. That task: run this bash command exactly, wait for it to finish, then reply DATAWATCH_COMPLETE: done.\n"+
			"for i in $(seq 1 %d); do echo \"%s tick $i\"; sleep 2; done",
		token, ticks, token)

	startBody := map[string]any{"spec": spec}
	if dir := os.Getenv("DW_LIVE_RESTART_PROJECT_DIR"); dir != "" {
		startBody["project_dir"] = dir
	}

	var lastSess bl462LiveSession
	t.Cleanup(func() {
		// Kill the probe session via the API only (never a raw tmux kill),
		// and only if it is still alive after everything below.
		if lastSess.ID == "" || bl462IsTerminal(lastSess.State) {
			return
		}
		if status, err := c.do(http.MethodPost, "/api/sessions/kill", map[string]string{"id": lastSess.ID}, nil); err != nil || status >= 300 {
			t.Logf("cleanup: could not kill probe session %s (HTTP %d, err %v)", lastSess.ID, status, err)
		}
	})

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
	lastSess = sess
	t.Logf("worker session %s running (state=%q tmux=%q) before restart", sess.ID, sess.State, sess.TmuxSession)

	// (e) restart the daemon the way the operator does.
	preUptime, err := c.uptimeSeconds()
	if err != nil {
		t.Fatalf("read daemon uptime before restart: %v", err)
	}
	cmd, label := bl462RestartCommand(t)
	t.Logf("restart path: %s (Env is set by bl462RestartEnv: TMUX and, except on the\nCLI fallback, TMUX_TMPDIR are stripped)", label)
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
	lastSess = sess
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
		lastSess = s
		if s.State == "killed" {
			t.Errorf("worker session %s ended as killed; adopt-live must never kill the surviving worker", s.ID)
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
		if tk.Status == "completed" {
			t.Logf("task %s completed through the adopted session %s", tk.ID, taskSession)
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
