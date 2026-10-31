package autonomous

// Dependents failed behind a dependency that failed for an infrastructure
// reason are marked so, on both the sequential and the concurrent path. The
// dependency is seeded as already failed, so only the propagation runs.

import (
	"strings"
	"testing"
	"time"
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
	return &depRig{m: m, prdID: prd.ID}
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
