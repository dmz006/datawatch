package session

// Live, opt-in companion to the parked-PRD resume behavior: a PRD whose
// worker session survives a daemon restart must not be parked by the
// boot-resume gate just because its record looks stale (the live worker
// counts as activity), and when the gate DOES park it (resume_max_age_seconds
// = 0) the operator's resume action must re-adopt the same worker session
// rather than respawn it.
//
// It reuses the helpers and constants of TestDaemonRestartAdoptsLiveWorkerLive
// (restart_adopt_live_test.go) and TestBL462_DaemonRestartSurvivesLive
// (restart_safety_live_test.go): bl462LiveClient, bl462RestartCommand,
// bl462PollUntil, bl462TmuxHasSession, adoptLivePRD and the adoptLive* waits.
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
// The test changes autonomous.resume_max_age_seconds on the daemon under test
// and restores the original value on exit.
//
// Run with a timeout above the default 10 minute test timeout:
//
//	DW_LIVE_RESTART_TEST=1 DW_BASE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 30m -run TestParkedPRDResumeAdoptsLiveWorkerLive -v ./internal/session/

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// parkedResumeBootEntry is the subset of a boot-resume report entry read here.
type parkedResumeBootEntry struct {
	PRDID        string   `json:"prd_id"`
	Outcome      string   `json:"outcome"`
	Reason       string   `json:"reason"`
	LiveSessions []string `json:"live_sessions"`
}

// setResumeMaxAge writes autonomous.resume_max_age_seconds through the daemon.
func (c *bl462LiveClient) setResumeMaxAge(t *testing.T, seconds int) {
	t.Helper()
	status, err := c.do(http.MethodPut, "/api/autonomous/config", map[string]any{"resume_max_age_seconds": seconds}, nil)
	if err != nil || status >= 300 {
		t.Fatalf("set resume_max_age_seconds=%d: HTTP %d, err %v", seconds, status, err)
	}
}

// restartDaemon restarts the daemon the way the operator does and waits for a
// reset uptime.
func (c *bl462LiveClient) restartDaemon(t *testing.T) {
	t.Helper()
	preUptime, err := c.uptimeSeconds()
	if err != nil {
		t.Fatalf("read daemon uptime before restart: %v", err)
	}
	cmd, label := bl462RestartCommand(t)
	t.Logf("restart path: %s (Env is set by bl462RestartEnv: TMUX and TMUX_TMPDIR are stripped)", label)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart via %s failed: %v\n%s", label, err, out)
	}
	restored := bl462PollUntil(bl462LiveRestartWait, func() bool {
		up, err := c.uptimeSeconds()
		return err == nil && up < preUptime
	})
	if !restored {
		t.Fatalf("daemon did not come back with a reset uptime within %s (before: %.0fs)", bl462LiveRestartWait, preUptime)
	}
}

// bootResumeEntries reads the boot-resume report from the autonomous status.
func (c *bl462LiveClient) bootResumeEntries(t *testing.T) []parkedResumeBootEntry {
	t.Helper()
	var st struct {
		BootResume []parkedResumeBootEntry `json:"boot_resume"`
	}
	if status, err := c.do(http.MethodGet, "/api/autonomous/status", nil, &st); err != nil || status >= 300 {
		t.Fatalf("GET autonomous status: HTTP %d, err %v", status, err)
	}
	return st.BootResume
}

func TestParkedPRDResumeAdoptsLiveWorkerLive(t *testing.T) {
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

	// Remember the configured limit and restore it when done.
	var cfg struct {
		ResumeMaxAge *int `json:"resume_max_age_seconds"`
	}
	if status, err := c.do(http.MethodGet, "/api/autonomous/config", nil, &cfg); err != nil || status >= 300 {
		t.Fatalf("read autonomous config: HTTP %d, err %v", status, err)
	}
	origMaxAge := 3600
	if cfg.ResumeMaxAge != nil {
		origMaxAge = *cfg.ResumeMaxAge
	}
	t.Cleanup(func() {
		if status, err := c.do(http.MethodPut, "/api/autonomous/config", map[string]any{"resume_max_age_seconds": origMaxAge}, nil); err != nil || status >= 300 {
			t.Logf("cleanup: could not restore resume_max_age_seconds=%d (HTTP %d, err %v)", origMaxAge, status, err)
		}
	})

	token := fmt.Sprintf("parked-resume-live-%d", time.Now().UnixNano())
	// One long worker: ~5 minutes, long enough to span two daemon restarts.
	ticks := 150
	spec := fmt.Sprintf(
		"Probe PRD %s. The whole story is ONE task. That task: run this bash command exactly, wait for it to finish, then reply DATAWATCH_COMPLETE: done.\n"+
			"for i in $(seq 1 %d); do echo \"%s tick $i\"; sleep 2; done",
		token, ticks, token)
	startBody := map[string]any{"spec": spec}
	if dir := os.Getenv("DW_LIVE_RESTART_PROJECT_DIR"); dir != "" {
		startBody["project_dir"] = dir
	}

	// End the probe session via the daemon API — and verify the recorded
	// sandbox daemon pid — on every exit path (pass, failure, interrupt).
	// Registered before the first call that can create the PRD (and its
	// worker session).
	lc := newLiveTestCleanup(t, c, sandboxPID)

	// Create, plan, approve and run the probe PRD.
	var prd adoptLivePRD
	if status, err := c.do(http.MethodPost, "/api/autonomous/prds", startBody, &prd); err != nil || status >= 300 {
		t.Fatalf("create PRD: HTTP %d, err %v", status, err)
	}
	if prd.ID == "" {
		t.Fatalf("create PRD: empty id in response")
	}
	t.Logf("PRD %s created", prd.ID)

	if status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/plan", nil, nil); err != nil && status >= 500 {
		t.Fatalf("kick plan: HTTP %d, err %v", status, err)
	}
	planDone := bl462PollUntil(adoptLivePlanWait, func() bool {
		var st struct {
			Status string `json:"status"`
		}
		status, err := c.do(http.MethodGet, "/api/autonomous/prds/"+prd.ID+"/plan/status", nil, &st)
		return err == nil && status < 300 && st.Status == "no_active_job"
	})
	if !planDone {
		t.Fatalf("plan never finished within %s", adoptLivePlanWait)
	}
	prd, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prd.tasks()) == 0 {
		t.Fatalf("PRD %s has no tasks after planning (status=%q)", prd.ID, prd.Status)
	}

	approve := func() error {
		status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/approve", map[string]any{"actor": "operator", "note": "live parked-resume test"}, nil)
		if err != nil || status >= 300 {
			return fmt.Errorf("approve: HTTP %d, err %v", status, err)
		}
		return nil
	}
	if err := approve(); err != nil {
		ackBody := map[string]any{"actor": "operator", "note": "live parked-resume test: ack to proceed"}
		if ackStatus, ackErr := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/plan_coverage/acknowledge", ackBody, nil); ackErr == nil && ackStatus < 300 {
			if retryErr := approve(); retryErr != nil {
				t.Fatalf("approve (after plan-coverage ack): %v", retryErr)
			}
		} else {
			t.Fatalf("approve failed and plan-coverage ack was not possible (HTTP %d, err %v): %v", ackStatus, ackErr, err)
		}
	}
	if status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/run", nil, nil); err != nil || status >= 500 {
		t.Fatalf("run PRD: HTTP %d, err %v", status, err)
	}

	var taskSession string
	bl462PollUntil(adoptLiveRunWait, func() bool {
		p, err := c.prdByID(prd.ID)
		if err != nil {
			return false
		}
		for _, tk := range p.tasks() {
			if tk.SessionID != "" {
				taskSession = tk.SessionID
				return true
			}
		}
		return false
	})
	if taskSession == "" {
		p, _ := c.prdByID(prd.ID)
		t.Fatalf("no worker session started within %s (PRD status=%s)", adoptLiveRunWait, p.Status)
	}
	sess, ok := c.sessionByID(taskSession)
	if !ok {
		t.Fatalf("worker session %s not found in /api/sessions", taskSession)
	}
	lc.trackSession(sess)
	t.Logf("worker session %s running (state=%q tmux=%q)", sess.ID, sess.State, sess.TmuxSession)

	// assertSameSession fails if any task of the PRD points at another session.
	assertSameSession := func(phase string, p adoptLivePRD) {
		t.Helper()
		for _, tk := range p.tasks() {
			if tk.SessionID != "" && tk.SessionID != taskSession {
				t.Errorf("BUG (duplicate worker) %s: task %s has session %q; must keep %q", phase, tk.ID, tk.SessionID, taskSession)
			}
		}
		if n := countTaskSessions(p, taskSession); n > 1 {
			t.Errorf("BUG (duplicate worker) %s: %d tasks share session %s", phase, n, taskSession)
		}
		s, ok := c.sessionByID(taskSession)
		if !ok {
			t.Fatalf("%s: worker session %s vanished from the daemon", phase, taskSession)
		}
		lc.trackSession(s)
		if s.State == "failed" || s.State == "killed" {
			t.Errorf("BUG %s: worker session %s is %s; a live worker must never be killed", phase, s.ID, s.State)
		}
		if !bl462IsTerminal(s.State) && !bl462TmuxHasSession(s.TmuxSession) {
			t.Errorf("%s: worker session %s is %q but its tmux session %s is gone", phase, s.ID, s.State, s.TmuxSession)
		}
	}

	// Phase 1: stale record, live worker. A tiny positive limit makes the
	// record older than the limit by the time the daemon boots again, but the
	// live session counts as activity, so the PRD must not be parked.
	c.setResumeMaxAge(t, 1)
	time.Sleep(3 * time.Second)
	c.restartDaemon(t)
	t.Log("phase 1: daemon restarted with resume_max_age_seconds=1; checking the PRD is not parked")

	after, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status == "blocked" {
		t.Errorf("BUG phase 1: PRD %s is blocked (%q) although its worker session %s is alive", after.ID, after.Blocked, taskSession)
	}
	if after.Status == "cancelled" || after.Status == "failed" {
		t.Errorf("phase 1: PRD %s is %s (blocked_reason=%q)", after.ID, after.Status, after.Blocked)
	}
	for _, e := range c.bootResumeEntries(t) {
		if e.PRDID == prd.ID && e.Outcome == "parked" {
			t.Errorf("BUG phase 1: boot-resume report parked PRD %s: %s", prd.ID, e.Reason)
		}
	}
	assertSameSession("phase 1", after)

	// Phase 2: force the parked case. A limit of 0 disables boot-resume, so a
	// PRD left running is parked (blocked) even though the worker is alive.
	c.setResumeMaxAge(t, 0)
	c.restartDaemon(t)
	t.Log("phase 2: daemon restarted with resume_max_age_seconds=0; checking the PRD is parked with its live session")

	parked, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parked.Status != "blocked" {
		t.Fatalf("phase 2: PRD %s status is %q (blocked_reason=%q); want blocked with resume_max_age_seconds=0", parked.ID, parked.Status, parked.Blocked)
	}
	assertSameSession("phase 2 (parked)", parked)
	listed := false
	for _, e := range c.bootResumeEntries(t) {
		if e.PRDID != prd.ID || e.Outcome != "parked" {
			continue
		}
		listed = true
		has := false
		for _, id := range e.LiveSessions {
			if id == taskSession || id == lc.sessionFullID {
				has = true
			}
		}
		if !has {
			t.Errorf("phase 2: boot-resume entry for PRD %s lists live sessions %v; want %s", prd.ID, e.LiveSessions, taskSession)
		}
	}
	if !listed {
		t.Fatalf("phase 2: boot-resume report has no parked entry for PRD %s", prd.ID)
	}

	// Resume through the REST endpoint; the PRD must return to running and
	// re-adopt the same session.
	status, err := c.do(http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/resume", map[string]any{"actor": "operator", "note": "live parked-resume test"}, nil)
	if err != nil || status >= 300 {
		t.Fatalf("resume PRD: HTTP %d, err %v", status, err)
	}
	running := bl462PollUntil(adoptLiveSettleWait, func() bool {
		p, err := c.prdByID(prd.ID)
		return err == nil && p.Status == "running"
	})
	if !running {
		p, _ := c.prdByID(prd.ID)
		t.Fatalf("phase 2: PRD %s did not return to running after resume (status=%q blocked_reason=%q)", prd.ID, p.Status, p.Blocked)
	}
	resumed, err := c.prdByID(prd.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSameSession("phase 2 (resumed)", resumed)
	t.Logf("PRD %s resumed and re-adopted session %s", prd.ID, taskSession)
}
