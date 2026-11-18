package autonomous

// Dependents that failed behind a dependency — "dependency <id> failed" or
// "dependency <id> was cancelled" — with nothing of their own wrong, return
// to pending the moment that dependency is in a state from which it can still
// complete. This recovery is unconditional: it applies to every failure class
// of the dependency, not just infrastructure, and it does not read
// autonomous.infra_retry.retry_dependents.

import (
	"strings"
	"testing"
	"time"
)

const (
	drRootID  = "dr-1-root"
	drChildID = "dr-2-child"
	drGrandID = "dr-3-grand"
)

// drPrd builds an approved Automaton of root → child → grand and seeds the
// tasks by ID (a missing ID leaves the task pending). The PRD status is set
// after the seeds when prdStatus is non-empty.
func drPrd(t *testing.T, m *Manager, seeds map[string]func(*Task), prdStatus PRDStatus) *PRD {
	t.Helper()
	mk := func(id, title string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: "do " + title, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	base := newHoldPRD(t, m, []Task{
		mk(drRootID, "dr root"),
		mk(drChildID, "dr child", "dr root"),
		mk(drGrandID, "dr grand", "dr child"),
	}, nil)
	p, ok := m.Store().GetPRD(base.ID)
	if !ok {
		t.Fatalf("PRD %s not found", base.ID)
	}
	for id, f := range seeds {
		tk := lookupTask(p, id)
		if tk == nil {
			t.Fatalf("task %s not found", id)
		}
		f(tk)
	}
	if prdStatus != "" {
		p.Status = prdStatus
	}
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return p
}

// drTasks is the raw four-task shape: root → child → grand, plus an extra
// task that "two" also depends on.
func drTasks(t *testing.T) []Task {
	mk := func(id, title string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: "do " + title, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	return []Task{
		mk(drRootID, "dr root"),
		mk(drChildID, "dr child", "dr root"),
		mk(drGrandID, "dr grand", "dr child"),
		mk("dr-extra", "dr extra"),
		mk("dr-two", "dr two", "dr child", "dr extra"),
	}
}

// drDecisions lists the decisions of kind made on the Automaton.
func drDecisions(t *testing.T, m *Manager, prdID, kind string) []Decision {
	t.Helper()
	p, ok := m.Store().SnapshotPRD(prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", prdID)
	}
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func contentRootReason() FailureReason {
	return FailureReason{
		Category: "content", Source: "executor",
		Detail:     "the work is incomplete",
		OccurredAt: time.Now(),
	}
}

func ownFailureReason() FailureReason {
	return FailureReason{
		Category: "content", Source: "verifier",
		Detail:     "the verification failed on the task's own work",
		OccurredAt: time.Now(),
	}
}

// A content/scope failure of the dependency must not keep the dependents down
// any longer than an infrastructure one: resetting the failed root releases
// the whole chain for every failure class, the same way the operator's reset
// restarts the run.
func TestDependencyRecovery_ContentDependentRecover(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, nil, failedWith(contentRootReason()))
			child := r.task(t, depChildID)
			if want := "dependency " + depRootID + " failed"; child.Error != want {
				t.Fatalf("child error = %q, want %q", child.Error, want)
			}
			if child.FailureReason != nil {
				t.Fatalf("child failure reason = %+v, want none for a content root", child.FailureReason)
			}
			// Keep the history a reset must not clear.
			r.edit(t, depChildID, func(tk *Task) {
				tk.Attempts = []TaskAttempt{{}}
				tk.InfraRetries = []InfraRetryRecord{{Number: 1, Class: "spawn_error"}}
				tk.InfraRetryCount = 1
			})

			if _, err := r.api.ResetTask(r.prdID, depRootID, "operator", false); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}
			if got := drDecisions(t, r.m, r.prdID, dependentsRecoveredKind); len(got) != 1 {
				t.Fatalf("dependents_recovered decisions = %d, want 1", len(got))
			} else if !strings.Contains(got[0].Note, "root="+depRootID) ||
				!strings.Contains(got[0].Note, depChildID) || !strings.Contains(got[0].Note, depGrandID) {
				t.Errorf("decision note = %q, want the root and both reset tasks", got[0].Note)
			}
			if got := drDecisions(t, r.m, r.prdID, infraDependentsRecoveredKind); len(got) != 0 {
				t.Errorf("infra_dependents_recovered = %d, want none for a content root", len(got))
			}
			waitForPRDTerminal(t, r.m, r.prdID)
			r.api.execWG.Wait()

			for _, id := range []string{depRootID, depChildID, depGrandID} {
				if tk := r.task(t, id); tk.Status != TaskCompleted {
					t.Errorf("task %s status = %q, want completed (error %q)", id, tk.Status, tk.Error)
				}
			}
			child = r.task(t, depChildID)
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

// A dependency that ended cancelled releases its dependents through the "was
// cancelled" wording too; a cancelled task only resets when forced, and the
// chain still completes.
func TestDependencyRecovery_CancelledDependentRecover(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, nil, func(tk *Task) {
				tk.Status = TaskCancelled
				tk.Error = "cancelled by operator"
			})
			child := r.task(t, depChildID)
			// The propagation wording the rig left on the child must name the
			// cancelled root; either of the two dependency wordings counts.
			wording := "dependency " + depRootID + " was cancelled"
			if child.Error != wording && child.Error != "dependency "+depRootID+" failed" {
				t.Fatalf("child error = %q, want the dependency wording naming %s", child.Error, depRootID)
			}
			if child.FailureReason != nil {
				t.Fatalf("child failure reason = %+v, want none for a cancelled root", child.FailureReason)
			}

			// A cancelled task only resets when forced.
			if _, err := r.api.ResetTask(r.prdID, depRootID, "operator", true); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}
			if got := drDecisions(t, r.m, r.prdID, dependentsRecoveredKind); len(got) != 1 {
				t.Fatalf("dependents_recovered decisions = %d, want 1", len(got))
			}
			waitForPRDTerminal(t, r.m, r.prdID)
			r.api.execWG.Wait()

			for _, id := range []string{depRootID, depChildID, depGrandID} {
				if tk := r.task(t, id); tk.Status != TaskCompleted {
					t.Errorf("task %s status = %q, want completed (error %q)", id, tk.Status, tk.Error)
				}
			}
			if got := r.prd(t).Status; got != PRDCompleted {
				t.Errorf("Automaton status = %q, want completed", got)
			}
		})
	}
}

// No reset at all: the dependency itself is ready to run, and its dependents
// stand failed behind it from an earlier pass. The run releases the whole
// chain and completes it for every failure class, not just infrastructure.
func TestDependencyRecovery_CompletionReleasesChain(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			m, api, spawn, verify := apiFixture(t)
			mk := func(id, title string, deps ...string) Task {
				return Task{ID: id, Title: title, Spec: "do " + title, ProjectDir: t.TempDir(), DependsOn: deps}
			}
			prd := newHoldPRD(t, m, []Task{
				mk(depRootID, "Root task"),
				mk(depChildID, "Child task", "Root task"),
				mk(depGrandID, "Grandchild task", "Child task"),
			}, func(p *PRD) { p.MaxConcurrentTasks = mode.concurrency })
			m.Store().WithLock(func() {
				tk := lookupTask(prd, depChildID)
				tk.Status = TaskFailed
				tk.Error = "dependency " + depRootID + " failed"
				tk = lookupTask(prd, depGrandID)
				tk.Status = TaskFailed
				tk.Error = "dependency " + depChildID + " failed"
			})
			if err := m.Store().SavePRD(prd); err != nil {
				t.Fatalf("SavePRD: %v", err)
			}
			api.SetExecutors(spawn, verify)
			// The boot-resume scan SetExecutors starts must be over first.
			api.execWG.Wait()
			if err := api.Run(prd.ID); err != nil {
				t.Fatalf("Run: %v", err)
			}
			waitForPRDTerminal(t, m, prd.ID)
			api.execWG.Wait()

			p, ok := m.Store().SnapshotPRD(prd.ID)
			if !ok {
				t.Fatalf("Automaton %s not found", prd.ID)
			}
			for _, id := range []string{depRootID, depChildID, depGrandID} {
				if tk := lookupTask(p, id); tk.Status != TaskCompleted {
					t.Errorf("task %s status = %q, want completed (error %q)", id, tk.Status, tk.Error)
				}
			}
			if p.Status != PRDCompleted {
				t.Errorf("Automaton status = %q, want completed", p.Status)
			}
			if got := drDecisions(t, m, prd.ID, dependentsRecoveredKind); len(got) != 1 {
				t.Errorf("dependents_recovered decisions = %d, want 1", len(got))
			}
			if got := drDecisions(t, m, prd.ID, infraDependentsRecoveredKind); len(got) != 0 {
				t.Errorf("infra decisions = %d, want none", len(got))
			}
		})
	}
}

// Unit: the pass releases the whole chain in dependency order — a content
// failure of the root is no different from an infrastructure one; the
// infrastructure retry budget is cleared but nothing else is lost; a second
// pass resets nothing; and with no failed task left the Automaton that ended
// failed runs again.
func TestDependencyRecovery_ChainInDependencyOrder(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	p := drPrd(t, m, map[string]func(*Task){
		drRootID: func(tk *Task) { tk.Status = TaskCompleted },
		drChildID: func(tk *Task) {
			tk.Status = TaskFailed
			tk.Error = "dependency " + drRootID + " failed"
			tk.Verification = &VerificationResult{}
			tk.SessionID = "sess-old"
			tk.InfraRetryCount = 2
		},
		drGrandID: func(tk *Task) {
			tk.Status = TaskFailed
			tk.Error = "dependency " + drChildID + " failed"
		},
	}, PRDFailed)

	got := m.recoverDependencyDependents(p)
	if want := []string{drChildID, drGrandID}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recovered = %v, want %v in dependency order", got, want)
	}
	for _, id := range []string{drChildID, drGrandID} {
		tk := lookupTask(p, id)
		if normTaskStatus(tk.Status) != TaskPending || tk.Error != "" || tk.FailureReason != nil || tk.Verification != nil || tk.SessionID != "" {
			t.Errorf("task %s = status %q error %q reason %+v, want a clean pending task", id, tk.Status, tk.Error, tk.FailureReason)
		}
	}
	if tk := lookupTask(p, drChildID); tk.InfraRetryCount != 0 {
		t.Errorf("child InfraRetryCount = %d, want 0 — the retry budget resets with the task", tk.InfraRetryCount)
	}
	if again := m.recoverDependencyDependents(p); len(again) != 0 {
		t.Errorf("second pass reset %v, want nothing", again)
	}
	if got := drDecisions(t, m, p.ID, dependentsRecoveredKind); len(got) != 1 {
		t.Errorf("decisions = %d, want one for the pass", len(got))
	}
	if got := drDecisions(t, m, p.ID, infraDependentsRecoveredKind); len(got) != 0 {
		t.Errorf("infra decisions = %d, want none", len(got))
	}
	// Nothing failed remains, so the Automaton that ended failed runs again.
	if got := p.Status; got != PRDRunning {
		t.Errorf("Automaton status = %q, want running", got)
	}
}

// Unit: the recovery ignores autonomous.infra_retry.retry_dependents — with
// the key off it still releases a dependent whose only failure is its
// dependency.
func TestDependencyRecovery_IndependentOfRetryDependents(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	m.mu.Lock()
	off := false
	m.cfg.InfraRetry.RetryDependents = &off
	m.mu.Unlock()
	p := drPrd(t, m, map[string]func(*Task){
		drRootID:  func(tk *Task) { tk.Status = TaskPending },
		drChildID: func(tk *Task) { tk.Status = TaskFailed; tk.Error = "dependency " + drRootID + " failed" },
	}, "")
	if got := m.recoverDependencyDependents(p); len(got) != 1 || got[0] != drChildID {
		t.Fatalf("recovered = %v, want %v despite retry_dependents off", got, drChildID)
	}
	if tk := lookupTask(p, drChildID); normTaskStatus(tk.Status) != TaskPending || tk.Error != "" {
		t.Errorf("child = status %q error %q, want clean pending", tk.Status, tk.Error)
	}
}

// Unit: a dependent with its own content/scope failure of its work is not
// released when its dependency recovers — its work is wrong, and it keeps
// failing again until the operator deals with it.
func TestDependencyRecovery_OwnFailureNotReleased(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	own := func(text string) func(*Task) {
		reason := ownFailureReason()
		return func(tk *Task) {
			tk.Status = TaskFailed
			tk.Error = text
			tk.FailureReason = &reason
		}
	}
	p := drPrd(t, m, map[string]func(*Task){
		drRootID:  func(tk *Task) { tk.Status = TaskCompleted },
		drChildID: own("the work is incomplete"),
	}, "")
	if got := m.recoverDependencyDependents(p); len(got) != 0 {
		t.Fatalf("recovered = %v, want nothing — the child failed on its own work", got)
	}
	if tk := lookupTask(p, drChildID); tk.Status != TaskFailed {
		t.Errorf("child status = %q, want kept failed", tk.Status)
	}
	if got := drDecisions(t, m, p.ID, dependentsRecoveredKind); len(got) != 0 {
		t.Errorf("decisions = %d, want none", len(got))
	}

	// Nor is a content reason that happened to ride the dependency wording.
	m2, _, _, _ := apiFixture(t)
	p2 := drPrd(t, m2, map[string]func(*Task){
		drRootID:  func(tk *Task) { tk.Status = TaskCompleted },
		drChildID: own("dependency " + drRootID + " failed"),
	}, "")
	if got := m2.recoverDependencyDependents(p2); len(got) != 0 {
		t.Fatalf("recovered = %v, want nothing — the child's own content reason wins", got)
	}
}

// Unit: a dependent still waiting on another dependency that has not itself
// recovered stays failed, and is released when the last one clears — even if
// its failure wording names the other dependency.
func TestDependencyRecovery_OtherUnmetDependencyStaysFailed(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	failed := func(text string) func(*Task) {
		return func(tk *Task) { tk.Status = TaskFailed; tk.Error = text }
	}
	p := newHoldPRD(t, m, drTasks(t), nil)
	p, ok := m.Store().GetPRD(p.ID)
	if !ok {
		t.Fatalf("PRD not found")
	}
	for id, f := range map[string]func(*Task){
		drRootID:   func(tk *Task) { tk.Status = TaskCompleted },
		drChildID:  failed("dependency " + drRootID + " failed"),
		"dr-extra": failed("the extra work failed"),
		"dr-two":   failed("dependency " + drChildID + " failed"),
	} {
		tk := lookupTask(p, id)
		if tk == nil {
			t.Fatalf("task %s not found", id)
		}
		f(tk)
	}
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	// The root and the child's dependency cleared, but "dr two" still stands
	// behind its failed extra dependency: only the chain releases.
	if got := m.recoverDependencyDependents(p); len(got) != 1 || got[0] != drChildID {
		t.Fatalf("recovered = %v, want %v only", got, drChildID)
	}
	if tk := lookupTask(p, "dr-two"); normTaskStatus(tk.Status) != TaskFailed {
		t.Errorf("two status = %q, want failed behind its extra dependency", tk.Status)
	}

	// The extra task completes: the last blocker is gone, "dr two" releases.
	m.Store().WithLock(func() { lookupTask(p, "dr-extra").Status = TaskCompleted })
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if got := m.recoverDependencyDependents(p); len(got) != 1 || got[0] != "dr-two" {
		t.Fatalf("recovered = %v, want dr-two", got)
	}
	if tk := lookupTask(p, "dr-two"); normTaskStatus(tk.Status) != TaskPending {
		t.Errorf("two status = %q, want pending", tk.Status)
	}
}

// Unit: nothing is released while the named dependency itself is still
// failed, or still cancelled — and a task that failed behind a dependency
// that is not a task of this Automaton is never reset by this recovery.
func TestDependencyRecovery_DependentsStillFailed(t *testing.T) {
	for name, root := range map[string]func(*Task){
		"stillFailed": func(tk *Task) {
			tk.Status = TaskFailed
			tk.Error = "the work is incomplete"
			reason := contentRootReason()
			tk.FailureReason = &reason
		},
		"stillCancelled": func(tk *Task) {
			tk.Status = TaskCancelled
			tk.Error = "cancelled by operator"
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, _, _, _ := apiFixture(t)
			p := drPrd(t, m, map[string]func(*Task){
				drRootID:  root,
				drChildID: func(tk *Task) { tk.Status = TaskFailed; tk.Error = "dependency " + drRootID + " failed" },
			}, PRDFailed)
			if got := m.recoverDependencyDependents(p); len(got) != 0 {
				t.Fatalf("recovered = %v, want nothing", got)
			}
			if tk := lookupTask(p, drChildID); tk.Status != TaskFailed {
				t.Errorf("child status = %q, want kept failed", tk.Status)
			}
			if got := drDecisions(t, m, p.ID, dependentsRecoveredKind); len(got) != 0 {
				t.Errorf("decisions = %d, want none", len(got))
			}
			if got := p.Status; got != PRDFailed {
				t.Errorf("Automaton status = %q, want still failed", got)
			}
		})
	}

	// The error names a dependency outside this Automaton: it is never reset
	// here on someone else's completion.
	m, _, _, _ := apiFixture(t)
	mk := func(id, title string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: "do " + title, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	prd := newHoldPRD(t, m, []Task{
		mk("dr-ok", "dr ok"),
		mk("dr-orphan", "dr orphan"),
	}, nil)
	p, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("PRD %s not found", prd.ID)
	}
	if lookupTask(p, "dr-ok") == nil || lookupTask(p, "dr-orphan") == nil {
		t.Fatalf("tasks not found")
	}
	lookupTask(p, "dr-ok").Status = TaskCompleted
	orphan := lookupTask(p, "dr-orphan")
	orphan.Status = TaskFailed
	orphan.Error = "dependency dr-foreign failed"
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if got := m.recoverDependencyDependents(p); len(got) != 0 {
		t.Fatalf("recovered = %v, want nothing for a foreign dependency", got)
	}
}

// Unit: a held Automaton is not touched by the recovery at all — the operator
// holds it on purpose, and the dependents wait for the hold to be released.
func TestDependencyRecovery_HeldAutomatonNotTouched(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	p := drPrd(t, m, map[string]func(*Task){
		drRootID:  func(tk *Task) { tk.Status = TaskCompleted },
		drChildID: func(tk *Task) { tk.Status = TaskFailed; tk.Error = "dependency " + drRootID + " failed" },
	}, PRDHeld)
	if got := m.recoverDependencyDependents(p); len(got) != 0 {
		t.Fatalf("recovered = %v, want nothing while held", got)
	}
	if tk := lookupTask(p, drChildID); tk.Status != TaskFailed {
		t.Errorf("child status = %q, want still failed", tk.Status)
	}
	if got := p.Status; got != PRDHeld {
		t.Errorf("Automaton status = %q, want held", got)
	}
	if got := drDecisions(t, m, p.ID, dependentsRecoveredKind); len(got) != 0 {
		t.Errorf("decisions = %d, want none", len(got))
	}
}
