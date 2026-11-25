package autonomous

// deploy_after_ship and run_merge_until_conflict are held by the merge run and
// deploy run switch and by the operator pause, by hand and in the daemon's own
// passes. A held job is not a failure.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pause"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

var heldJobs = []string{stepDeployAfterShip, stepRunMergeUntilConflict}

type runHoldRig struct {
	h                     *coordHarness
	steps                 *SupervisorLoopSteps
	store                 *slMemStore
	on                    atomic.Bool
	deploys, merges, good atomic.Int32

	mu       sync.Mutex
	refusals []SupervisorRunHold
	byHand   []bool
}

// newRunHoldRig has one shipped release, so deploy_after_ship has work to do.
func newRunHoldRig(t *testing.T, set *supervisorloop.Settings) *runHoldRig {
	t.Helper()
	r := &runHoldRig{}
	hooks := SupervisorLoopHooks{
		Deploy: func(context.Context) (string, error) { r.deploys.Add(1); return "restarted", nil },
		DeployHealth: func(context.Context) (string, string) {
			r.good.Add(1)
			return DeployHealthPassed, ""
		},
		MergeUntilConflict: func(context.Context) (SupervisorMergeResult, error) {
			r.merges.Add(1)
			return SupervisorMergeResult{Merged: []string{"b1"}}, nil
		},
		MergeDeployRunEnabled: r.on.Load,
		RunRefused: func(_ context.Context, hold SupervisorRunHold, byHand bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.refusals = append(r.refusals, hold)
			r.byHand = append(r.byHand, byHand)
		},
	}
	if set != nil {
		hooks.Settings = func() supervisorloop.Settings { return *set }
	}
	r.h, r.steps, r.store = newStepsRig(t, hooks)
	if _, err := r.h.m.RecordPRDShipped(r.h.prds[0].ID, "v1.2.3", time.Now(), "abc", ShippedSourceBackfill); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *runHoldRig) refused(job string) (n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.refusals {
		if h.Job == job {
			n++
		}
	}
	return n
}

func (r *runHoldRig) assertNothingRan(t *testing.T) {
	t.Helper()
	if d, m, g := r.deploys.Load(), r.merges.Load(), r.good.Load(); d != 0 || m != 0 || g != 0 {
		t.Fatalf("a held job acted: deploys=%d merges=%d health checks=%d", d, m, g)
	}
}

// heldPass runs one daemon pass and checks both jobs report the hold without
// an error.
func (r *runHoldRig) heldPass(t *testing.T, want ...string) {
	t.Helper()
	rs := r.steps.RunPass(context.Background())
	for _, job := range heldJobs {
		res := resultFor(t, rs, job)
		if res.Err != nil {
			t.Errorf("%s: a hold is not a failure, got error %v", job, res.Err)
		}
		for _, w := range append([]string{"held: "}, want...) {
			if !strings.Contains(res.Detail, w) {
				t.Errorf("%s: detail %q lacks %q", job, res.Detail, w)
			}
		}
	}
}

func TestSupervisorRunHold_SwitchOffHoldsBothJobsInDaemonPasses(t *testing.T) {
	r := newRunHoldRig(t, nil)
	for i := 0; i < 3; i++ {
		r.heldPass(t, config.MergeDeployRunKey)
	}
	r.assertNothingRan(t)
	for _, job := range heldJobs {
		if n := r.refused(job); n != 1 {
			t.Errorf("%s: %d refusals recorded over 3 passes, want 1", job, n)
		}
		recs := r.store.byJob(job)
		if len(recs) != 3 {
			t.Fatalf("%s: %d run records, want 3", job, len(recs))
		}
		for _, rec := range recs {
			if rec.Outcome != supervisorloop.OutcomeOK || len(rec.ValidatorErrors) != 0 {
				t.Errorf("%s: a hold was recorded as a failure: %+v", job, rec)
			}
		}
	}
	for i, h := range r.refusals {
		if r.byHand[i] || h.Setting != config.MergeDeployRunKey || h.Pause != nil || h.Message != config.MergeDeployRunRefusal {
			t.Errorf("refusal %d = %+v byHand=%v", i, h, r.byHand[i])
		}
	}
	if len(r.refusals) != 2 {
		t.Errorf("refusals = %d, want only the two merge and deploy jobs", len(r.refusals))
	}
	// The scheduled entry (the idle tick) is held the same way.
	r.steps.RunScheduled(context.Background())
	r.assertNothingRan(t)
}

func TestSupervisorRunHold_HandRunRefusedWhateverTheStepSetting(t *testing.T) {
	for _, stepOn := range []bool{true, false} {
		set := supervisorloop.DefaultSettings()
		for _, job := range heldJobs {
			if err := set.Set("supervisor_loop."+job+".enabled", stepOn); err != nil {
				t.Fatal(err)
			}
		}
		r := newRunHoldRig(t, &set)
		for _, job := range heldJobs {
			for i := 0; i < 2; i++ {
				_, found, err := r.steps.RunStep(context.Background(), job)
				var held *SupervisorRunHeldError
				if !found || !errors.As(err, &held) {
					t.Fatalf("step on=%v %s: found=%v err=%v, want a held refusal", stepOn, job, found, err)
				}
				if held.Hold.Job != job || !strings.Contains(err.Error(), config.MergeDeployRunKey) {
					t.Errorf("%s: hold = %+v", job, held.Hold)
				}
			}
			if n := r.refused(job); n != 2 {
				t.Errorf("%s: every hand run is recorded, got %d of 2", job, n)
			}
			if n := len(r.store.byJob(job)); n != 0 {
				t.Errorf("%s: a refused hand run wrote %d run records", job, n)
			}
		}
		r.assertNothingRan(t)
		for i := range r.byHand {
			if !r.byHand[i] {
				t.Errorf("refusal %d not marked as by hand", i)
			}
		}
	}
}

func TestSupervisorRunHold_SwitchOnRunsBothJobs(t *testing.T) {
	r := newRunHoldRig(t, nil)
	r.on.Store(true)
	r.steps.RunPass(context.Background())
	if r.deploys.Load() != 1 || r.merges.Load() != 1 {
		t.Fatalf("deploys=%d merges=%d, want 1 and 1", r.deploys.Load(), r.merges.Load())
	}
	if _, _, err := r.steps.RunStep(context.Background(), stepRunMergeUntilConflict); err != nil || r.merges.Load() != 2 {
		t.Fatalf("hand run with the switch on: %v merges=%d", err, r.merges.Load())
	}
	if len(r.refusals) != 0 {
		t.Errorf("refusals with the switch on: %+v", r.refusals)
	}
}

func TestSupervisorRunHold_OperatorPauseHoldsBothJobs(t *testing.T) {
	for _, scope := range []pause.Scope{pause.ScopeGlobal, pause.ScopeProject} {
		r := newRunHoldRig(t, nil)
		r.on.Store(true)
		base := t.TempDir()
		cfg := r.h.m.Config()
		cfg.WorktreeBaseRepo = base
		r.h.m.SetConfig(cfg)
		ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.h.m.SetPauseGate(ps)
		project := ""
		if scope == pause.ScopeProject {
			project = base
		}
		if _, err := ps.Pause(scope, project, "window closed", "operator"); err != nil {
			t.Fatal(err)
		}
		r.heldPass(t, "paused by operator", "window closed")
		r.heldPass(t, "window closed")
		for _, job := range heldJobs {
			_, _, err := r.steps.RunStep(context.Background(), job)
			var held *SupervisorRunHeldError
			if !errors.As(err, &held) || held.Hold.Pause == nil || held.Hold.Setting != "" || held.Hold.Pause.Scope != scope {
				t.Fatalf("%s %s: err = %v", scope, job, err)
			}
			if n := r.refused(job); n != 2 { // one for the daemon, one by hand
				t.Errorf("%s %s: refusals = %d, want 2", scope, job, n)
			}
		}
		r.assertNothingRan(t)
		if _, err := ps.Resume(scope, project, "go", "operator"); err != nil {
			t.Fatal(err)
		}
		r.steps.RunPass(context.Background())
		if r.deploys.Load() != 1 || r.merges.Load() != 1 {
			t.Fatalf("%s: after resume deploys=%d merges=%d", scope, r.deploys.Load(), r.merges.Load())
		}
	}
}

// With both holding, the switch is named first, as the run routes do. A new
// hold is recorded again: a changed holder, or a hold after a job could run.
func TestSupervisorRunHold_EachNewHoldIsRecordedOnce(t *testing.T) {
	r := newRunHoldRig(t, nil)
	ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.h.m.SetPauseGate(ps)
	if _, err := ps.Pause(pause.ScopeGlobal, "", "first", "operator"); err != nil {
		t.Fatal(err)
	}
	passes := func(want int, why string) {
		t.Helper()
		r.steps.RunPass(context.Background())
		r.steps.RunPass(context.Background())
		for _, job := range heldJobs {
			if n := r.refused(job); n != want {
				t.Fatalf("%s: %s refusals = %d, want %d", why, job, n, want)
			}
		}
	}
	passes(1, "switch off and paused")
	if r.refusals[0].Setting != config.MergeDeployRunKey {
		t.Errorf("first refusal = %+v, want the switch", r.refusals[0])
	}
	r.on.Store(true)
	passes(2, "switch on, still paused")
	if _, err := ps.Pause(pause.ScopeGlobal, "", "second", "operator"); err != nil {
		t.Fatal(err)
	}
	passes(3, "a new pause reason")
	r.assertNothingRan(t)
	if _, err := ps.Resume(pause.ScopeGlobal, "", "go", "operator"); err != nil {
		t.Fatal(err)
	}
	passes(3, "free")
	r.on.Store(false)
	passes(4, "switched off again")
}

// A host whose deploy job is held does not deploy after ship, so the closed
// work check must not report shipped work as never deployed.
func TestSupervisorRunHold_HeldHostDoesNotDeployAfterShip(t *testing.T) {
	r := newRunHoldRig(t, nil)
	if r.steps.DeploysAfterShip() {
		t.Error("switch off: DeploysAfterShip must be false")
	}
	r.on.Store(true)
	if !r.steps.DeploysAfterShip() {
		t.Error("switch on: DeploysAfterShip must be true")
	}
	ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.h.m.SetPauseGate(ps)
	if r.steps.DeploysAfterShip() {
		t.Error("paused: DeploysAfterShip must be false")
	}
	if len(r.refusals) != 0 {
		t.Errorf("reading DeploysAfterShip is not a refusal: %+v", r.refusals)
	}
}

// A job with no hook has nothing to hold: it still says it is not configured.
func TestSupervisorRunHold_NoHookStaysNotConfigured(t *testing.T) {
	off := func() bool { return false }
	_, steps, _ := newStepsRig(t, SupervisorLoopHooks{MergeDeployRunEnabled: off})
	for _, job := range heldJobs {
		detail, _, err := steps.RunStep(context.Background(), job)
		if err != nil || detail != stepNotConfigured {
			t.Errorf("%s: detail %q err %v", job, detail, err)
		}
	}
}
