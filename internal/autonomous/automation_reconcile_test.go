package autonomous

import (
	"sync"
	"testing"
	"time"
)

// runningStarter records starts and moves the PRD to running like the real
// API.Run, so a second reconcile sees a live member and not an errored one.
type runningStarter struct {
	fakeStarter
}

func (r *runningStarter) Run(id string) error {
	if err := r.fakeStarter.Run(id); err != nil {
		return err
	}
	r.flip(id)
	return nil
}

func (r *runningStarter) Resume(id, actor string) (any, error) {
	out, err := r.fakeStarter.Resume(id, actor)
	if err == nil {
		r.flip(id)
	}
	return out, err
}

func (r *runningStarter) flip(id string) {
	if p, ok := r.m.store.GetPRD(id); ok {
		r.m.store.WithLock(func() { p.Status = PRDRunning })
	}
}

// reconcileHarness is a coordinator harness whose starter flips PRDs to
// running and whose coordinator is registered on the Manager (as Start does).
func newReconcileHarness(t *testing.T, nPRDs int) (*coordHarness, *runningStarter) {
	t.Helper()
	h := newCoordHarness(t, nPRDs)
	rs := &runningStarter{fakeStarter{m: h.m}}
	h.c.starter = rs
	h.m.mu.Lock()
	h.m.coordinator = h.c
	h.m.mu.Unlock()
	return h, rs
}

func chainEdges() [][3]string { return [][3]string{{"m1", "", ""}} }

func TestAutomationReconcile_TerminalCursorAdvances(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	h.setStatus(0, PRDCompleted) // completed while the daemon was down

	h.c.reconcile()

	got := h.get(a)
	if got.CurrentMemberID != "m1" || got.Members[0].Status != MemberCompleted || got.Members[1].Status != MemberRunning {
		t.Fatalf("cursor=%q members=%+v", got.CurrentMemberID, got.Members)
	}
	if runs, _ := rs.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs = %v, want next member once", runs)
	}
	if !h.hasAudit("automation_reconcile") {
		t.Fatalf("audits = %v, want automation_reconcile", h.audits)
	}
	if !h.c.Ready() {
		t.Fatal("reconcile did not ready the coordinator")
	}
}

func TestAutomationReconcile_FailedCursorRoutesOnlyAfterReady(t *testing.T) {
	h, rs := newReconcileHarness(t, 3)
	a := h.force(h.makeAuto([][3]string{{"m1", "", "m2"}}, 3))
	h.setStatus(0, PRDFailed)

	// A live failed event before boot recovery finished is deferred.
	h.event(0, PRDFailed)
	if got := h.get(a); got.CurrentMemberID != "m0" {
		t.Fatalf("failed member routed before ready: cursor=%q", got.CurrentMemberID)
	}
	h.c.reconcile()
	got := h.get(a)
	if got.CurrentMemberID != "m2" || got.Members[0].EdgeTaken != edgeOnFailed {
		t.Fatalf("cursor=%q edge=%q", got.CurrentMemberID, got.Members[0].EdgeTaken)
	}
	if runs, _ := rs.counts(); len(runs) != 1 || runs[0] != h.prds[2].ID {
		t.Fatalf("runs = %v, want on_failed target once", runs)
	}
}

func TestAutomationReconcile_RunningCursorUntouched(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	h.setStatus(0, PRDRunning)
	before := h.get(a)

	h.c.reconcile()

	got := h.get(a)
	if got.Version != before.Version || got.CurrentMemberID != "m0" {
		t.Fatalf("running cursor changed: version %d->%d cursor=%q", before.Version, got.Version, got.CurrentMemberID)
	}
	if runs, resumes := rs.counts(); len(runs)+len(resumes) != 0 {
		t.Fatalf("started %v %v for a running member", runs, resumes)
	}
}

func TestAutomationReconcile_PendingCursorRetriesStart(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.makeAuto(chainEdges(), 2)
	a, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		a.CurrentMemberID = a.EntryMemberID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	h.c.reconcile()

	got := h.get(a)
	if got.Status != AutomationActive || got.Members[0].Status != MemberRunning || got.Members[0].StartedAt == nil {
		t.Fatalf("status=%s member=%+v", got.Status, got.Members[0])
	}
	if runs, _ := rs.counts(); len(runs) != 1 || runs[0] != h.prds[0].ID {
		t.Fatalf("runs = %v, want entry once", runs)
	}
}

func TestAutomationReconcile_PendingCursorGateAndPause(t *testing.T) {
	t.Run("gate closed blocks", func(t *testing.T) {
		h, rs := newReconcileHarness(t, 1)
		a := h.makeAuto(nil, 1)
		h.setStatus(0, PRDNeedsReview)
		a, _ = h.m.automations.Update(a.ID, 0, func(a *Automation) error {
			a.Status = AutomationActive
			a.CurrentMemberID = a.EntryMemberID
			return nil
		})
		h.c.reconcile()
		got := h.get(a)
		if got.Status != AutomationBlocked || !h.hasAudit("automation_blocked") {
			t.Fatalf("status=%s audits=%v", got.Status, h.audits)
		}
		if runs, _ := rs.counts(); len(runs) != 0 {
			t.Fatalf("started %v through a closed gate", runs)
		}
	})
	t.Run("paused does not start", func(t *testing.T) {
		h, rs := newReconcileHarness(t, 1)
		a := h.makeAuto(nil, 1)
		a, _ = h.m.automations.Update(a.ID, 0, func(a *Automation) error {
			a.Status = AutomationPaused
			a.CurrentMemberID = a.EntryMemberID
			return nil
		})
		before := h.get(a)
		h.c.reconcile()
		if got := h.get(a); got.Version != before.Version {
			t.Fatalf("paused automation changed: %+v", got)
		}
		if runs, _ := rs.counts(); len(runs) != 0 {
			t.Fatalf("started %v for a paused automation", runs)
		}
	})
}

func TestAutomationReconcile_StartedButApprovedBlocks(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	// force marked m0 running with StartedAt set; the PRD is still approved.

	h.c.reconcile()

	got := h.get(a)
	if got.Status != AutomationBlocked || got.Members[0].Status != MemberBlocked {
		t.Fatalf("status=%s member=%+v", got.Status, got.Members[0])
	}
	if last := got.Decisions[len(got.Decisions)-1]; last.Kind != "blocked" || last.Note != "member run errored" {
		t.Fatalf("decision = %+v", last)
	}
	if h.alertCount("warn|") != 1 {
		t.Fatalf("alerts = %v", h.alerts)
	}
	if runs, _ := rs.counts(); len(runs) != 0 {
		t.Fatalf("started %v for an errored member", runs)
	}
}

func TestAutomationReconcile_Idempotent(t *testing.T) {
	h, rs := newReconcileHarness(t, 3)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}, {"m2", "", ""}}, 3))
	h.setStatus(0, PRDCompleted)

	h.c.reconcile()
	first := h.get(a)
	h.c.reconcile()
	h.c.reconcile()

	got := h.get(a)
	if got.Version != first.Version || got.CurrentMemberID != "m1" || got.Status != AutomationActive {
		t.Fatalf("second reconcile changed state: version %d->%d cursor=%q status=%s", first.Version, got.Version, got.CurrentMemberID, got.Status)
	}
	if runs, _ := rs.counts(); len(runs) != 1 {
		t.Fatalf("runs = %v, want exactly one start", runs)
	}
}

func TestAutomationReconcile_PendingStartIdempotent(t *testing.T) {
	h, rs := newReconcileHarness(t, 1)
	a := h.makeAuto(nil, 1)
	h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		a.CurrentMemberID = a.EntryMemberID
		return nil
	})
	h.c.reconcile()
	h.c.reconcile()
	if runs, _ := rs.counts(); len(runs) != 1 {
		t.Fatalf("runs = %v, want one start across two reconciles", runs)
	}
}

func TestAutomationReconcile_LiveEventRacesReconcile(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	h.setStatus(0, PRDCompleted)
	h.c.SetReady()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			h.c.reconcile()
		}()
		go func() {
			defer wg.Done()
			<-start
			h.event(0, PRDCompleted)
		}()
	}
	close(start)
	wg.Wait()

	got := h.get(a)
	if got.CurrentMemberID != "m1" || got.Members[0].Status != MemberCompleted || got.Status != AutomationActive {
		t.Fatalf("cursor=%q status=%s members=%+v", got.CurrentMemberID, got.Status, got.Members)
	}
	if runs, _ := rs.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs = %v, want next member started exactly once", runs)
	}
}

// A restart while the member is mid-run: reconcile leaves it to boot-resume,
// and the later live completion still advances the Automation.
func TestAutomationReconcile_RestartMidRun(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	h.setStatus(0, PRDRunning)

	h.c.reconcile()
	if got := h.get(a); got.CurrentMemberID != "m0" || got.Members[0].Status != MemberRunning {
		t.Fatalf("mid-run member disturbed: %+v", got)
	}

	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	if got := h.get(a); got.CurrentMemberID != "m1" {
		t.Fatalf("live completion after reconcile did not advance: cursor=%q", got.CurrentMemberID)
	}
	if runs, _ := rs.counts(); len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
}

// A restart after the member completed while the daemon was down: no event
// will ever arrive, so only reconcile can advance it.
func TestAutomationReconcile_RestartAfterCompletedWhileDown(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	h.setStatus(0, PRDCompleted)

	h.c.reconcile()

	if got := h.get(a); got.CurrentMemberID != "m1" || got.Members[1].Status != MemberRunning {
		t.Fatalf("cursor=%q members=%+v", got.CurrentMemberID, got.Members)
	}
	if runs, _ := rs.counts(); len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
}

// resumeRunningPRDs calls reconcile last, through the Manager's coordinator.
func TestAutomationReconcile_RunsFromBootResume(t *testing.T) {
	h, rs := newReconcileHarness(t, 2)
	a := h.force(h.makeAuto(chainEdges(), 2))
	h.setStatus(0, PRDCompleted)
	api := NewAPI(h.m)

	if h.c.Ready() {
		t.Fatal("coordinator ready before boot-resume")
	}
	done := make(chan struct{})
	go func() { api.resumeRunningPRDs(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("resumeRunningPRDs did not return")
	}

	if !h.c.Ready() {
		t.Fatal("boot-resume did not ready the coordinator")
	}
	if got := h.get(a); got.CurrentMemberID != "m1" {
		t.Fatalf("cursor=%q, want m1", got.CurrentMemberID)
	}
	if runs, _ := rs.counts(); len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
}

func TestAutomationReconcile_NilCoordinator(t *testing.T) {
	var c *Coordinator
	c.reconcile() // must not panic

	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(m)
	api.resumeRunningPRDs() // no coordinator configured
}
