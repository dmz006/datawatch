package server

// The supervisor loop jobs run_merge_until_conflict and deploy_after_ship, and
// the retry of a deferred deploy at daemon start, are held by the merge run and
// deploy run switch and by the operator pause. These tests use the daemon's
// own hooks; only the final merge and deploy calls are replaced.

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/pause"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type holdClock struct{}

func (holdClock) Now() time.Time { return time.Now() }

type loopHoldRig struct {
	s               *Server
	mgr             *autonomous.Manager
	repo            string
	steps           *autonomous.SupervisorLoopSteps
	store           supervisorloop.Store
	log             *audit.Log
	merges, deploys atomic.Int32
}

var loopHeldJobs = map[string]string{"run_merge_until_conflict": mergeRunSubject, "deploy_after_ship": deployRunSubject}

func newLoopHoldRig(t *testing.T) *loopHoldRig {
	t.Helper()
	s, mgr, repo, _, _, log := switchServer(t)
	r := &loopHoldRig{s: s, mgr: mgr, repo: repo, log: log,
		store: supervisorloop.NewFileStore(filepath.Join(t.TempDir(), "runs.jsonl"), 100)}
	hooks := NewSupervisorLoopHooks(mgr, s.cfg, nil)
	hooks.MergeUntilConflict = func(context.Context) (autonomous.SupervisorMergeResult, error) {
		r.merges.Add(1)
		return autonomous.SupervisorMergeResult{}, nil
	}
	hooks.Deploy = func(context.Context) (string, error) { r.deploys.Add(1); return "deployed", nil }
	s.auditSupervisorRunRefusals(&hooks)
	engine := &supervisorloop.Engine{Settings: supervisorloop.DefaultSettings(), Clock: holdClock{}, Store: r.store}
	r.steps = autonomous.NewSupervisorLoopSteps(autonomous.NewCoordinator(mgr, noopStarter{}), engine, hooks)
	s.SetSupervisorLoop(NewSupervisorLoopControl(r.steps, nil, r.store))
	return r
}

func (r *loopHoldRig) run(job string) (int, string) {
	rr := liveDo(r.s.handleSupervisorLoopJobRun, http.MethodPost, "/api/supervisor-loop/jobs/"+job+"/run", "")
	return rr.Code, strings.TrimSpace(rr.Body.String())
}

func refusalEntries(t *testing.T, log *audit.Log) []audit.Entry {
	t.Helper()
	entries, err := log.Read(audit.QueryFilter{Action: audit.ActionMergeDeployRunRefused})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestSupervisorRunHoldREST_SwitchOffRefusesAHandRunAndAudits(t *testing.T) {
	r := newLoopHoldRig(t)
	// The step's own setting does not matter for a hand run.
	r.s.cfg.Autonomous.SupervisorLoop = config.SupervisorLoopConfig{"supervisor_loop.deploy_after_ship.enabled": false}
	before := switchHead(t, r.repo)
	for job := range loopHeldJobs {
		code, body := r.run(job)
		if code != http.StatusForbidden || body != config.MergeDeployRunRefusal {
			t.Errorf("%s: %d %q, want 403 with the refusal", job, code, body)
		}
	}
	if r.merges.Load() != 0 || r.deploys.Load() != 0 || switchHead(t, r.repo) != before {
		t.Errorf("a refused job acted: merges=%d deploys=%d", r.merges.Load(), r.deploys.Load())
	}
	if recs, _ := r.store.List(); len(recs) != 0 {
		t.Errorf("a refused hand run wrote run records: %+v", recs)
	}
	entries := refusalEntries(t, r.log)
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2: %+v", len(entries), entries)
	}
	for job, subject := range loopHeldJobs {
		found := false
		for _, e := range entries {
			if e.Details["job"] != job {
				continue
			}
			found = true
			if e.Subject != subject || e.Actor != "operator" || e.Details["setting"] != config.MergeDeployRunKey ||
				e.Details["outcome"] != "refused" || e.Details["trigger"] != "manual" ||
				e.Details["route"] != "/api/supervisor-loop/jobs/"+job+"/run" {
				t.Errorf("%s entry = %+v", job, e)
			}
		}
		if !found {
			t.Errorf("no audit entry for %s", job)
		}
	}
}

func TestSupervisorRunHoldREST_PauseRefusesAHandRunAndAudits(t *testing.T) {
	r := newLoopHoldRig(t)
	r.s.cfg.Autonomous.MergeDeployRunEnabled = true
	ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.mgr.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeProject, r.repo, "window closed", "operator"); err != nil {
		t.Fatal(err)
	}
	b, _ := ps.Blocked(r.repo)
	for job := range loopHeldJobs {
		if code, body := r.run(job); code != http.StatusConflict || body != b.Message() {
			t.Errorf("%s: %d %q, want 409 %q", job, code, body, b.Message())
		}
	}
	if r.merges.Load() != 0 || r.deploys.Load() != 0 {
		t.Errorf("a paused job acted: merges=%d deploys=%d", r.merges.Load(), r.deploys.Load())
	}
	entries := refusalEntries(t, r.log)
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if _, has := e.Details["setting"]; has {
			t.Errorf("a pause refusal must not name the switch: %+v", e)
		}
		if e.Details["held_by"] != "operator_pause" || e.Details["pause_scope"] != "project" ||
			e.Details["pause_project"] != r.repo || e.Details["pause_reason"] != "window closed" || e.Details["paused_by"] != "operator" {
			t.Errorf("entry = %+v", e)
		}
	}
}

func TestSupervisorRunHold_DaemonHooksReadTheSwitchLive(t *testing.T) {
	r := newLoopHoldRig(t)
	// Daemon passes while off: nothing acts, one audit entry per job.
	r.steps.RunPass(context.Background())
	r.steps.RunPass(context.Background())
	if r.merges.Load() != 0 || r.deploys.Load() != 0 {
		t.Fatalf("daemon pass while off: merges=%d deploys=%d", r.merges.Load(), r.deploys.Load())
	}
	entries := refusalEntries(t, r.log)
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Actor != "supervisor-loop" || e.Details["trigger"] != "daemon" || e.Details["route"] != nil {
			t.Errorf("daemon entry = %+v", e)
		}
	}
	// Switched on in the live config: the same hooks let the jobs run.
	r.s.cfg.Autonomous.MergeDeployRunEnabled = true
	if code, body := r.run("run_merge_until_conflict"); code != http.StatusOK || r.merges.Load() != 1 {
		t.Fatalf("switch on: %d %s merges=%d", code, body, r.merges.Load())
	}
	// The hooks the daemon builds always carry the switch.
	bare := &config.Config{}
	h := NewSupervisorLoopHooks(r.mgr, bare, nil)
	if h.MergeDeployRunEnabled == nil || h.MergeDeployRunEnabled() {
		t.Fatal("the daemon hooks must carry the switch, off by default")
	}
	bare.Autonomous.MergeDeployRunEnabled = true
	if !h.MergeDeployRunEnabled() {
		t.Fatal("the switch must be read live")
	}
}

// startHoldServer is a server with a deferred deploy waiting to be retried and
// a build command that leaves a marker.
func startHoldServer(t *testing.T, phase deploy.Phase) (s *Server, store deploy.FileStore, marker string, log *audit.Log) {
	t.Helper()
	s, _, _, marker, _, log = switchServer(t)
	data := s.statePath()
	if err := os.MkdirAll(filepath.Join(data, "deploy"), 0o700); err != nil {
		t.Fatal(err)
	}
	store = deploy.FileStore{Path: filepath.Join(data, "deploy", "state.json")}
	if err := store.Save(deploy.State{Version: deploy.StateVersion, Phase: phase, Reason: "final-gate tasks running", UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return s, store, marker, log
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestSupervisorRunHold_StartupDeferredRetrySwitchOff(t *testing.T) {
	s, store, marker, log := startHoldServer(t, deploy.PhaseDeferred)
	s.StartDeployResume(context.Background())
	waitFor(t, 10*time.Second, "the refusal or the retry", func() bool { return exists(marker) || len(refusalEntries(t, log)) > 0 })
	if exists(marker) {
		t.Fatal("the deferred deploy was retried while the switch is off")
	}
	e := refusalEntries(t, log)[0]
	if e.Subject != deployRunSubject || e.Actor != "daemon" || e.Details["trigger"] != "daemon_start" ||
		e.Details["setting"] != config.MergeDeployRunKey || e.Details["outcome"] != "refused" {
		t.Errorf("entry = %+v", e)
	}
	if st, _, err := store.Load(); err != nil || st.Phase != deploy.PhaseDeferred {
		t.Errorf("the deferred state must be kept: %+v %v", st, err)
	}
}

func TestSupervisorRunHold_StartupDeferredRetrySwitchOnRuns(t *testing.T) {
	s, _, marker, log := startHoldServer(t, deploy.PhaseDeferred)
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	s.StartDeployResume(context.Background())
	waitFor(t, 10*time.Second, "the retried build", func() bool { return exists(marker) })
	if n := len(refusalEntries(t, log)); n != 0 {
		t.Errorf("refusals with the switch on: %d", n)
	}
}

func TestSupervisorRunHold_StartupDeferredRetryPaused(t *testing.T) {
	s, _, marker, log := startHoldServer(t, deploy.PhaseDeferred)
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.pauseStore = ps
	s.StartDeployResume(context.Background())
	waitFor(t, 10*time.Second, "the refusal", func() bool { return exists(marker) || len(refusalEntries(t, log)) > 0 })
	if exists(marker) {
		t.Fatal("the deferred deploy was retried while paused")
	}
	if e := refusalEntries(t, log)[0]; e.Details["held_by"] != "operator_pause" || e.Details["trigger"] != "daemon_start" {
		t.Errorf("entry = %+v", e)
	}
}

// Finishing a deploy the restart interrupted is not a new deploy: it goes on
// with the switch off, and nothing is refused.
func TestSupervisorRunHold_StartupStillFinishesAnInterruptedDeploy(t *testing.T) {
	s, store, marker, log := startHoldServer(t, deploy.PhaseRestarting)
	s.StartDeployResume(context.Background())
	waitFor(t, 10*time.Second, "the interrupted deploy to finish", func() bool {
		st, _, err := store.Load()
		return err == nil && st.Phase == deploy.PhaseDone
	})
	if exists(marker) || len(refusalEntries(t, log)) != 0 {
		t.Errorf("built=%v refusals=%d", exists(marker), len(refusalEntries(t, log)))
	}
}

// Every attempt of a deferred retry asks again, so a switch turned off while
// the retry waits for running work stops the next attempt.
func TestSupervisorRunHold_DeferredRetryChecksEveryAttempt(t *testing.T) {
	s, _, repo, _, _, log := switchServer(t)
	s.cfg.Autonomous.MergeDeployRunEnabled = true
	calls := 0
	retry := s.heldDeferredRetry(repo, func(context.Context) (deploy.Result, error) {
		calls++
		return deploy.Result{Status: deploy.StatusDeferred}, nil
	})
	if res, err := retry(context.Background()); err != nil || res.Status != deploy.StatusDeferred || calls != 1 {
		t.Fatalf("first attempt: %+v %v calls=%d", res, err, calls)
	}
	s.cfg.Autonomous.MergeDeployRunEnabled = false
	res, err := retry(context.Background())
	if err != nil || res.Status != deploy.StatusSkipped || calls != 1 || !strings.Contains(res.Reason, config.MergeDeployRunKey) {
		t.Fatalf("after switching off: %+v %v calls=%d", res, err, calls)
	}
	if len(refusalEntries(t, log)) != 1 {
		t.Errorf("refusals = %d, want 1", len(refusalEntries(t, log)))
	}
}
