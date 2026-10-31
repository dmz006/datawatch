package autonomous

// Dependents failed behind a dependency that failed for an infrastructure
// reason are marked so, on both the sequential and the concurrent path. The
// dependency is seeded as already failed, so only the propagation runs. They
// return to pending when that dependency recovers.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	depRootID  = "dep-1-root"
	depChildID = "dep-2-child"
	depGrandID = "dep-3-grandchild"
)

// depPathModes are the two executor paths: sequential and a pool of two.
var depPathModes = []struct {
	name        string
	concurrency int
}{
	{"sequential", 0},
	{"concurrent", 2},
}

// depRig is an Automaton of a seeded failed root task and two tasks behind it,
// the second behind the first.
type depRig struct {
	m     *Manager
	api   *API
	prdID string
}

// newDepRig builds the Automaton. seed edits the root task (its status and
// FailureReason); retryDependents, when non-nil, sets the config key.
func newDepRig(t *testing.T, concurrency int, retryDependents *bool, seed func(*Task)) *depRig {
	t.Helper()
	m, api, spawn, verify := apiFixture(t)
	if retryDependents != nil {
		m.mu.Lock()
		m.cfg.InfraRetry.RetryDependents = retryDependents
		m.mu.Unlock()
	}
	mk := func(id, title string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: "do " + title, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	root := mk(depRootID, "Root task")
	seed(&root)
	prd := newHoldPRD(t, m, []Task{
		root,
		mk(depChildID, "Child task", "Root task"),
		mk(depGrandID, "Grandchild task", "Child task"),
	}, func(p *PRD) { p.MaxConcurrentTasks = concurrency })
	api.SetExecutors(spawn, verify)
	// The boot-resume scan SetExecutors starts resets a finished all-failed
	// Automaton to pending, so it must be over before this one runs.
	api.execWG.Wait()
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitForPRDTerminal(t, m, prd.ID)
	api.execWG.Wait()
	return &depRig{m: m, api: api, prdID: prd.ID}
}

func (r *depRig) task(t *testing.T, id string) Task {
	t.Helper()
	p, ok := r.m.Store().SnapshotPRD(r.prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prdID)
	}
	if tk := lookupTask(p, id); tk != nil {
		return *tk
	}
	t.Fatalf("task %s not found", id)
	return Task{}
}

// failedWith seeds a failed root task with the reason.
func failedWith(reason FailureReason) func(*Task) {
	return func(t *Task) {
		t.Status = TaskFailed
		t.Error = reason.Detail
		t.FailureReason = &reason
	}
}

func infraRootReason() FailureReason {
	return FailureReason{
		Category: "infra", Source: "executor",
		Detail:     "worker spawn failed: no worker slot",
		OccurredAt: time.Now(),
	}
}

// assertNotMarked checks that the task failed behind its dependency with the
// unchanged error text and no failure reason.
func assertNotMarked(t *testing.T, tk Task, depID, verb string) {
	t.Helper()
	if tk.Status != TaskFailed {
		t.Fatalf("task %s status = %q, want failed", tk.ID, tk.Status)
	}
	if want := "dependency " + depID + " " + verb; tk.Error != want {
		t.Errorf("task %s error = %q, want %q", tk.ID, tk.Error, want)
	}
	if tk.FailureReason != nil {
		t.Errorf("task %s failure reason = %+v, want none", tk.ID, tk.FailureReason)
	}
}

func TestInfraRetry_DependentMarkedAsInfra(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, nil, failedWith(infraRootReason()))
			child := r.task(t, depChildID)
			if child.Status != TaskFailed {
				t.Fatalf("child status = %q, want failed", child.Status)
			}
			if want := "dependency " + depRootID + " failed"; child.Error != want {
				t.Errorf("child error = %q, want %q", child.Error, want)
			}
			fr := child.FailureReason
			if fr == nil {
				t.Fatal("child has no failure reason")
			}
			if fr.Category != "infra" || fr.Source != "dependency-infra" {
				t.Errorf("category/source = %q/%q, want infra/dependency-infra", fr.Category, fr.Source)
			}
			if fr.DependsOnTaskID != depRootID {
				t.Errorf("DependsOnTaskID = %q, want %q", fr.DependsOnTaskID, depRootID)
			}
			if !fr.Retryable || fr.OccurredAt.IsZero() {
				t.Errorf("Retryable = %v, OccurredAt = %v, want true and set", fr.Retryable, fr.OccurredAt)
			}
			want := "dependency " + depRootID + " failed for an infrastructure reason (spawn_error): worker spawn failed: no worker slot"
			if fr.Detail != want {
				t.Errorf("detail = %q, want %q", fr.Detail, want)
			}
		})
	}
}

func TestInfraRetry_DependentChainMarkedWithRoot(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, nil, failedWith(infraRootReason()))
			grand := r.task(t, depGrandID)
			if grand.Status != TaskFailed {
				t.Fatalf("grandchild status = %q, want failed", grand.Status)
			}
			if want := "dependency " + depChildID + " failed"; grand.Error != want {
				t.Errorf("grandchild error = %q, want %q", grand.Error, want)
			}
			fr := grand.FailureReason
			if fr == nil {
				t.Fatal("grandchild has no failure reason")
			}
			if fr.Source != "dependency-infra" || fr.Category != "infra" {
				t.Errorf("category/source = %q/%q, want infra/dependency-infra", fr.Category, fr.Source)
			}
			if fr.DependsOnTaskID != depRootID {
				t.Errorf("DependsOnTaskID = %q, want the original root %q", fr.DependsOnTaskID, depRootID)
			}
			want := "dependency " + depChildID + " failed for an infrastructure reason (spawn_error): worker spawn failed: no worker slot"
			if fr.Detail != want {
				t.Errorf("detail = %q, want %q (no nested sentence)", fr.Detail, want)
			}
		})
	}
}

func TestInfraRetry_DependentOfContentFailureNotMarked(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			// The detail would classify as infra by its text; the category
			// says the judgment was about content, and that decides.
			r := newDepRig(t, mode.concurrency, nil, failedWith(FailureReason{
				Category: "content", Source: "verifier-ask",
				Detail:     "worker spawn failed in the code under test",
				OccurredAt: time.Now(),
			}))
			assertNotMarked(t, r.task(t, depChildID), depRootID, "failed")
			assertNotMarked(t, r.task(t, depGrandID), depChildID, "failed")

			scope := newDepRig(t, mode.concurrency, nil, failedWith(FailureReason{
				Category: "scope", Source: "verification-script-scope",
				Detail: "the spec writes outside its directory", OccurredAt: time.Now(),
			}))
			assertNotMarked(t, scope.task(t, depChildID), depRootID, "failed")

			bare := newDepRig(t, mode.concurrency, nil, func(tk *Task) { tk.Status = TaskFailed; tk.Error = "boom" })
			assertNotMarked(t, bare.task(t, depChildID), depRootID, "failed")
		})
	}
}

func TestInfraRetry_DependentOfCancelledNotMarked(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			reason := infraRootReason()
			r := newDepRig(t, mode.concurrency, nil, func(tk *Task) {
				tk.Status = TaskCancelled
				tk.FailureReason = &reason
			})
			child := r.task(t, depChildID)
			if child.Status != TaskFailed {
				t.Fatalf("child status = %q, want failed", child.Status)
			}
			if child.FailureReason != nil {
				t.Errorf("child failure reason = %+v, want none", child.FailureReason)
			}
			if !strings.HasPrefix(child.Error, "dependency "+depRootID) {
				t.Errorf("child error = %q, want it to name %s", child.Error, depRootID)
			}
			if g := r.task(t, depGrandID); g.FailureReason != nil {
				t.Errorf("grandchild failure reason = %+v, want none", g.FailureReason)
			}
		})
	}
}

func TestInfraRetry_DependentMarkingDisabledByConfig(t *testing.T) {
	off := false
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, &off, failedWith(infraRootReason()))
			assertNotMarked(t, r.task(t, depChildID), depRootID, "failed")
			assertNotMarked(t, r.task(t, depGrandID), depChildID, "failed")
		})
	}
}

// edit changes a stored task of the rig's Automaton under the store lock.
func (r *depRig) edit(t *testing.T, id string, f func(*Task)) {
	t.Helper()
	p, ok := r.m.Store().GetPRD(r.prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prdID)
	}
	tk := lookupTask(p, id)
	if tk == nil {
		t.Fatalf("task %s not found", id)
	}
	r.m.Store().WithLock(func() { f(tk) })
	if err := r.m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
}

// recoveredDecisions counts the infra_dependents_recovered decisions.
func (r *depRig) recoveredDecisions(t *testing.T) []Decision {
	t.Helper()
	p, _ := r.m.Store().SnapshotPRD(r.prdID)
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == infraDependentsRecoveredKind {
			out = append(out, d)
		}
	}
	return out
}

func (r *depRig) prd(t *testing.T) *PRD {
	t.Helper()
	p, ok := r.m.Store().GetPRD(r.prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prdID)
	}
	return p
}

func TestInfraRetry_DependentsRecover(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, nil, failedWith(infraRootReason()))
			if c := r.task(t, depChildID); c.FailureReason == nil || c.FailureReason.Source != dependencyInfraSource {
				t.Fatalf("child = %+v, want it marked dependency-infra before the reset", c.FailureReason)
			}
			// Keep the history a reset must not clear.
			r.edit(t, depChildID, func(tk *Task) {
				tk.Attempts = []TaskAttempt{{}}
				tk.InfraRetries = []InfraRetryRecord{{Number: 1, Class: "spawn_error"}}
				tk.InfraRetryCount = 1
			})

			// Retrying the dependency releases the dependents and, because the
			// Automaton had ended failed, restarts the run with no second call.
			if _, err := r.api.ResetTask(r.prdID, depRootID, "operator", false); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}
			if got := r.recoveredDecisions(t); len(got) != 1 {
				t.Fatalf("infra_dependents_recovered decisions = %d, want 1", len(got))
			} else if !strings.Contains(got[0].Note, "root="+depRootID) ||
				!strings.Contains(got[0].Note, depChildID) || !strings.Contains(got[0].Note, depGrandID) {
				t.Errorf("decision note = %q, want the root and both reset tasks", got[0].Note)
			}
			waitForPRDTerminal(t, r.m, r.prdID)
			r.api.execWG.Wait()

			for _, id := range []string{depRootID, depChildID, depGrandID} {
				if tk := r.task(t, id); tk.Status != TaskCompleted {
					t.Errorf("task %s status = %q, want completed (error %q)", id, tk.Status, tk.Error)
				}
			}
			child := r.task(t, depChildID)
			if child.FailureReason != nil || child.Error != "" {
				t.Errorf("child kept error %q / reason %+v after the reset", child.Error, child.FailureReason)
			}
			if len(child.InfraRetries) != 1 || len(child.Attempts) == 0 {
				t.Errorf("child history: InfraRetries=%d Attempts=%d, want the history kept", len(child.InfraRetries), len(child.Attempts))
			}
			if got := r.prd(t).Status; got != PRDCompleted {
				t.Errorf("Automaton status = %q, want completed", got)
			}
		})
	}
}

func TestInfraRetry_DependentsRecoverChain(t *testing.T) {
	r := newDepRig(t, 0, nil, failedWith(infraRootReason()))
	// The dependency completes outside any reset; one pass releases the whole
	// chain, in dependency order.
	r.edit(t, depRootID, func(tk *Task) { tk.Status = TaskCompleted })
	r.edit(t, depChildID, func(tk *Task) { tk.InfraRetryCount = 2 })

	got := r.m.recoverInfraDependents(r.prd(t))
	if want := []string{depChildID, depGrandID}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recovered = %v, want %v", got, want)
	}
	for _, id := range []string{depChildID, depGrandID} {
		tk := r.task(t, id)
		if normTaskStatus(tk.Status) != TaskPending || tk.Error != "" || tk.FailureReason != nil || tk.Verification != nil {
			t.Errorf("task %s = status %q error %q reason %+v, want a clean pending task", id, tk.Status, tk.Error, tk.FailureReason)
		}
	}
	if c := r.task(t, depChildID); c.InfraRetryCount != 0 {
		t.Errorf("child InfraRetryCount = %d, want 0", c.InfraRetryCount)
	}
	if again := r.m.recoverInfraDependents(r.prd(t)); len(again) != 0 {
		t.Errorf("second pass reset %v, want nothing", again)
	}
	if got := r.recoveredDecisions(t); len(got) != 1 {
		t.Errorf("decisions = %d, want one for the pass", len(got))
	}
	// Nothing failed remains, so the Automaton that ended failed runs again.
	if got := r.prd(t).Status; got != PRDRunning {
		t.Errorf("Automaton status = %q, want running", got)
	}
}

func TestInfraRetry_DependentWithOtherFailureStaysFailed(t *testing.T) {
	r := newDepRig(t, 0, nil, failedWith(infraRootReason()))
	// The child ran and failed on content after it was marked.
	r.edit(t, depChildID, func(tk *Task) {
		tk.Error = "verification failed"
		tk.FailureReason = &FailureReason{Category: "content", Source: "verifier-ask", Detail: "the work is incomplete", OccurredAt: time.Now()}
	})
	if _, err := r.m.ResetTask(r.prdID, depRootID, "operator", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	child := r.task(t, depChildID)
	if child.Status != TaskFailed || child.FailureReason == nil || child.FailureReason.Category != "content" {
		t.Errorf("child = status %q reason %+v, want it failed on content", child.Status, child.FailureReason)
	}
	// The grandchild waits behind a child that is still failed.
	if g := r.task(t, depGrandID); g.Status != TaskFailed || g.FailureReason == nil {
		t.Errorf("grandchild = status %q reason %+v, want it still failed behind the child", g.Status, g.FailureReason)
	}
	if got := r.recoveredDecisions(t); len(got) != 0 {
		t.Errorf("decisions = %d, want none", len(got))
	}
}

func TestInfraRetry_DependentsNotResetWhileDependencyFailed(t *testing.T) {
	r := newDepRig(t, 0, nil, failedWith(infraRootReason()))
	for _, st := range []TaskStatus{TaskFailed, TaskCancelled, TaskBlocked, TaskReviewNeeded} {
		r.edit(t, depRootID, func(tk *Task) { tk.Status = st })
		if got := r.m.recoverInfraDependents(r.prd(t)); len(got) != 0 {
			t.Errorf("root %s: reset %v, want nothing", st, got)
		}
		for _, id := range []string{depChildID, depGrandID} {
			if tk := r.task(t, id); tk.Status != TaskFailed || tk.FailureReason == nil || tk.FailureReason.Source != dependencyInfraSource {
				t.Errorf("root %s: task %s = status %q reason %+v, want it still failed behind the root", st, id, tk.Status, tk.FailureReason)
			}
		}
	}
	if got := r.recoveredDecisions(t); len(got) != 0 {
		t.Errorf("decisions = %d, want none", len(got))
	}
}

func TestInfraRetry_DependentsRecoverDisabledByConfig(t *testing.T) {
	r := newDepRig(t, 0, nil, failedWith(infraRootReason()))
	// The dependents were marked while the key was on; turning it off stops
	// the recovery.
	off := false
	r.m.mu.Lock()
	r.m.cfg.InfraRetry.RetryDependents = &off
	r.m.mu.Unlock()
	if _, err := r.api.ResetTask(r.prdID, depRootID, "operator", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	if got := r.m.recoverInfraDependents(r.prd(t)); len(got) != 0 {
		t.Errorf("recovered %v, want nothing with retry_dependents off", got)
	}
	for _, id := range []string{depChildID, depGrandID} {
		if tk := r.task(t, id); tk.Status != TaskFailed {
			t.Errorf("task %s status = %q, want failed", id, tk.Status)
		}
	}
	if got := r.recoveredDecisions(t); len(got) != 0 {
		t.Errorf("decisions = %d, want none", len(got))
	}
}

// A task whose worker spawn keeps failing, with the count action, fails on the
// ordinary path carrying the infrastructure reason, so the task behind it is
// marked and returns to pending when the first task is retried. This runs the
// real executor from the spawn error, where the other tests seed the failed
// root by hand.
func TestInfraRetry_CountedSpawnFailureMarksDependents(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			m, api, _, verify := apiFixture(t)
			m.mu.Lock()
			m.cfg.InfraRetry.ExhaustedAction = config.ExhaustedActionCount
			m.mu.Unlock()
			var failSpawn atomic.Bool
			failSpawn.Store(true)
			spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
				if failSpawn.Load() {
					return SpawnResult{}, errors.New(holdTestCause)
				}
				return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
			}
			mk := func(id, title string, deps ...string) Task {
				return Task{ID: id, Title: title, Spec: "do " + title, ProjectDir: t.TempDir(), DependsOn: deps}
			}
			prd := newHoldPRD(t, m, []Task{
				mk(depRootID, "Root task"),
				mk(depChildID, "Child task", "Root task"),
			}, func(p *PRD) { p.MaxConcurrentTasks = mode.concurrency })
			api.SetExecutors(spawn, verify)
			api.execWG.Wait()
			if err := api.Run(prd.ID); err != nil {
				t.Fatalf("Run: %v", err)
			}
			waitForPRDTerminal(t, m, prd.ID)
			api.execWG.Wait()
			r := &depRig{m: m, api: api, prdID: prd.ID}

			root := r.task(t, depRootID)
			if root.Status != TaskFailed {
				t.Fatalf("root status = %q (error %q), want failed", root.Status, root.Error)
			}
			if len(root.InfraRetries) != config.InfraRetryDefaultMaxTries {
				t.Errorf("root retries = %d, want %d", len(root.InfraRetries), config.InfraRetryDefaultMaxTries)
			}
			fr := root.FailureReason
			if fr == nil || fr.Category != infraRetryFailureCategory || fr.Source != infraRetryCountSource {
				t.Fatalf("root failure reason = %+v, want category infra, source %s", fr, infraRetryCountSource)
			}
			if class, ok := ClassifyInfraText(fr.Detail); !ok || class != config.InfraRetryClassSpawnError || !strings.Contains(fr.Detail, holdTestCause) {
				t.Errorf("root failure detail %q classifies as %q (%v), want spawn_error with the cause", fr.Detail, class, ok)
			}
			child := r.task(t, depChildID)
			if child.Status != TaskFailed || child.FailureReason == nil ||
				child.FailureReason.Source != dependencyInfraSource || child.FailureReason.DependsOnTaskID != depRootID {
				t.Fatalf("child = status %q reason %+v, want it failed behind the root as dependency-infra", child.Status, child.FailureReason)
			}

			failSpawn.Store(false)
			if _, err := api.ResetTask(prd.ID, depRootID, "operator", false); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}
			if got := r.recoveredDecisions(t); len(got) != 1 || !strings.Contains(got[0].Note, depChildID) {
				t.Fatalf("infra_dependents_recovered decisions = %+v, want one naming the child", got)
			}
			waitForPRDTerminal(t, m, prd.ID)
			api.execWG.Wait()
			for _, id := range []string{depRootID, depChildID} {
				if tk := r.task(t, id); tk.Status != TaskCompleted {
					t.Errorf("task %s status = %q (error %q), want completed", id, tk.Status, tk.Error)
				}
			}
		})
	}
}
