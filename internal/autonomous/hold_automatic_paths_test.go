package autonomous

// Every automatic path leaves a held Automaton, and its tasks, exactly as they
// are. Each test builds a held Automaton with tasks in several states, runs an
// automatic pass through the fakes the matching test files already use, and
// compares a JSON copy of the stored Automaton before and after.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

const (
	holdAutoDoneTask   = "hold-auto-done"
	holdAutoFailedTask = "hold-auto-failed"
	holdAutoReviewTask = "hold-auto-review"
	holdAutoQueuedTask = "hold-auto-queued"
)

// holdAutoRig is a coordinator harness with one held Automaton and one
// ordinary Automaton beside it, plus recorders on every outward call.
type holdAutoRig struct {
	t     *testing.T
	h     *coordHarness
	steps *SupervisorLoopSteps
	api   *API
	// heldID is the held Automaton; freeID is the ordinary one.
	heldID, freeID string
	// auto is the Automation a pass's setup joined the held Automaton to.
	auto *Automation

	mu      sync.Mutex
	refires []string
	audits  []string
	alerts  []string
	spawned []string
	now     time.Time
}

// holdAutoTasks is one task in each state the passes look at.
func holdAutoTasks(projectDir string) []Task {
	return []Task{
		{ID: holdAutoDoneTask, Title: "done task", Spec: "done", Status: TaskCompleted,
			WorktreeBranch: "task/hold-auto-done", ProjectDir: projectDir},
		{ID: holdAutoFailedTask, Title: "failed task", Spec: "failed", Status: TaskFailed,
			DependsOn: []string{"done task"},
			FailureReason: &FailureReason{Category: infraRetryFailureCategory, Source: dependencyInfraSource,
				DependsOnTaskID: holdAutoDoneTask, Retryable: true,
				Detail: "dependency " + holdAutoDoneTask + " failed for an infrastructure reason (network): timeout"}},
		{ID: holdAutoReviewTask, Title: "review task", Spec: "review", Status: TaskReviewNeeded,
			FailureReason: &FailureReason{Category: infraRetryFailureCategory, Source: infraRetryHoldSource, Retryable: true}},
		{ID: holdAutoQueuedTask, Title: "queued task", Spec: "queued", Status: TaskQueued},
	}
}

func newHoldAutoRig(t *testing.T) *holdAutoRig {
	t.Helper()
	h := newCoordHarness(t, 2)
	eng := &supervisorloop.Engine{Settings: supervisorloop.DefaultSettings(), Clock: slClock{}, Store: &slMemStore{}}
	r := &holdAutoRig{t: t, h: h, steps: NewSupervisorLoopSteps(h.c, eng, SupervisorLoopHooks{}), now: time.Unix(1_700_000_000, 0)}
	r.heldID, r.freeID = h.prds[0].ID, h.prds[1].ID
	return r
}

// holdAutoSnapshot is a JSON copy of the stored Automaton, tasks included.
func holdAutoSnapshot(t *testing.T, m *Manager, id string) string {
	t.Helper()
	p, ok := m.Store().SnapshotPRD(id)
	if !ok {
		t.Fatalf("Automaton %s not found", id)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	return string(b)
}

// holdAutoSeed stores tasks on the Automaton, then puts it on hold with its
// look-again time already in the past (an expired hold stays held).
func holdAutoSeed(t *testing.T, m *Manager, id string, tasks []Task, held bool) {
	t.Helper()
	if err := m.Store().SetStories(id, []Story{{Title: "Story", Tasks: tasks}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	ok := m.Store().MutatePRD(id, func(p *PRD) bool {
		p.ProjectDir = t.TempDir()
		for i := range p.Story[0].Tasks {
			p.Story[0].Tasks[i].ProjectDir = p.ProjectDir
		}
		if held {
			until := holdClockStart.Add(-time.Hour)
			expired := holdClockStart
			p.Status = PRDHeld
			p.Hold = &HoldState{Reason: "wait for the release", From: PRDFailed, Until: &until,
				HeldAt: holdClockStart.Add(-2 * time.Hour), ExpiredAt: &expired, PriorBlockedReason: "judge said block"}
			p.Triage = &TriageRecord{PRDID: id, Status: "failed", FirstSeenAt: holdClockStart.Add(-3 * time.Hour)}
		}
		return true
	})
	if !ok {
		t.Fatalf("Automaton %s not found", id)
	}
}

// build seeds the held Automaton (prds[0]) and installs the recorders.
func (r *holdAutoRig) build() *holdAutoRig {
	t, m := r.t, r.h.m
	holdAutoSeed(t, m, r.heldID, holdAutoTasks(""), true)
	m.mu.Lock()
	m.cfg.StuckRunningSeconds = 1
	m.mu.Unlock()
	m.orphanNow = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }
	m.SetSessionListerFn(func() []SessionInfo { return nil })
	m.SetStuckRunningHooks(func(id string) (bool, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.refires = append(r.refires, id)
		return true, nil
	}, func(string) bool { return false })
	m.SetAuditFn(func(action string, _ map[string]any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.audits = append(r.audits, action)
	})
	m.SetAlertFn(func(title, _ string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, title)
	})
	r.api = NewAPI(m)
	r.api.spawnFn = func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.spawned = append(r.spawned, req.TaskID)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	return r
}

func (r *holdAutoRig) advance(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

// assertNoOutwardCalls fails when a start, retry, requeue, rejudge, refire,
// alert or archive call reached a fake.
func (r *holdAutoRig) assertNoOutwardCalls(name string) {
	r.t.Helper()
	runs, resumes := r.h.fake.counts()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(runs) != 0 || len(resumes) != 0 {
		r.t.Errorf("%s: start calls reached the starter: runs=%v resumes=%v", name, runs, resumes)
	}
	if len(r.refires) != 0 || len(r.spawned) != 0 {
		r.t.Errorf("%s: refire or spawn reached a fake: refires=%v spawned=%v", name, r.refires, r.spawned)
	}
	if len(r.alerts) != 0 {
		r.t.Errorf("%s: alerts raised: %v", name, r.alerts)
	}
	for _, a := range r.audits {
		if strings.Contains(a, "archive") || strings.Contains(a, "retry") || strings.Contains(a, "requeue") ||
			strings.Contains(a, "rejudge") || strings.Contains(a, "stuck") || strings.Contains(a, "reconcile") {
			r.t.Errorf("%s: audit %q reached the sink", name, a)
		}
	}
	r.h.mu.Lock()
	defer r.h.mu.Unlock()
	if len(r.h.alerts) != 0 {
		r.t.Errorf("%s: coordinator alerts raised: %v", name, r.h.alerts)
	}
}

// fakes the reconcile hosts use; they record every end and adoption.
type holdAutoSessionHost struct {
	mu                sync.Mutex
	verifiers         []VerifierSession
	judges            []JudgeSession
	ended, boundLease []string
}

func (f *holdAutoSessionHost) LiveVerifiers() []VerifierSession { return f.verifiers }
func (f *holdAutoSessionHost) LiveJudges() []JudgeSession       { return f.judges }
func (f *holdAutoSessionHost) End(id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, id)
	return nil
}
func (f *holdAutoSessionHost) BindLease(holder, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boundLease = append(f.boundLease, holder+"="+id)
}

// holdAutoLoopSession is a worker session the loop rescue would write to.
type holdAutoLoopSession struct {
	input, stopped int
	accepts        bool
}

func (s *holdAutoLoopSession) ID() string             { return "hold-auto-session" }
func (s *holdAutoLoopSession) AcceptsInput() bool     { return s.accepts }
func (s *holdAutoLoopSession) SendInput(string) error { s.input++; return nil }
func (s *holdAutoLoopSession) Stop() error            { s.stopped++; return nil }

// holdAutoLeftovers is a leftovers lister whose Release is recorded.
type holdAutoLeftovers struct {
	views    []LeftoverView
	released []string
}

func (f *holdAutoLeftovers) Leftovers(context.Context) []LeftoverView { return f.views }
func (f *holdAutoLeftovers) Release(_ context.Context, kind, ref string) error {
	f.released = append(f.released, kind+" "+ref)
	return nil
}

func holdAutoDetectNone(t *testing.T, name string, repairs []Repair) {
	t.Helper()
	for i, rp := range repairs {
		if got := rp.Detect(context.Background()); len(got) != 0 {
			t.Errorf("%s: repair %d found %+v for a held Automaton", name, i, got)
		}
	}
}

func TestHoldAutomaticPassesLeaveHeldUntouched(t *testing.T) {
	enabled := true
	passes := []struct {
		name string
		// setup runs before the held Automaton is copied, for passes whose
		// fixture itself writes to the record (joining it to an Automation).
		setup func(t *testing.T, r *holdAutoRig)
		run   func(t *testing.T, r *holdAutoRig)
	}{
		{"housekeeping advance and judge repair", nil, func(t *testing.T, r *holdAutoRig) {
			ops := ManagerAdvanceOps{API: r.api}
			holdAutoDetectNone(t, "advance", NewAdvanceJudgeRepairs(ops))
			holdAutoDetectNone(t, "loop steps advance", []Repair{advanceJudgeRepair{s: r.steps}})
			for _, a := range ops.Automata(context.Background()) {
				if a.ID == r.heldID {
					t.Errorf("the advance listing includes the held Automaton")
				}
			}
		}},
		{"housekeeping planning repair", nil, func(t *testing.T, r *holdAutoRig) {
			ops := ManagerPlanningOps{API: r.api}
			holdAutoDetectNone(t, "planning", NewPlanningRepairs(ops))
			for _, d := range ops.Drafts(context.Background()) {
				if d.ID == r.heldID {
					t.Errorf("the planning listing includes the held Automaton")
				}
			}
		}},
		{"housekeeping task chain repair", nil, func(t *testing.T, r *holdAutoRig) {
			ops := ManagerChainOps{API: r.api}
			holdAutoDetectNone(t, "task chain", NewTaskChainRepairs(ops))
			for _, ct := range ops.ChainTasks(context.Background()) {
				if ct.AutomatonID == r.heldID {
					t.Errorf("the chain listing includes task %s of the held Automaton", ct.TaskID)
				}
			}
		}},
		{"housekeeping leftover release", nil, func(t *testing.T, r *holdAutoRig) {
			ops := &holdAutoLeftovers{views: []LeftoverView{
				{Kind: LeftoverWorktree, Ref: "/work/held", Automaton: "held", Finished: false},
				{Kind: LeftoverLease, Ref: "lease-held", Automaton: "held", Finished: false},
			}}
			holdAutoDetectNone(t, "leftovers", NewLeftoverReleaseRepairs(ops))
			if len(ops.released) != 0 {
				t.Errorf("released %v for a held Automaton", ops.released)
			}
		}},
		{"supervisor loop close finished records", nil, func(t *testing.T, r *holdAutoRig) {
			if _, err := r.steps.closeFinishedRecords(context.Background()); err != nil {
				t.Fatalf("closeFinishedRecords: %v", err)
			}
		}},
		{"supervisor loop retry failed infra", nil, func(t *testing.T, r *holdAutoRig) {
			detail, err := r.steps.retryFailedInfra(context.Background())
			if err != nil || !strings.Contains(detail, "0 of 0") {
				t.Fatalf("retryFailedInfra = %q, %v; want no held task retried", detail, err)
			}
		}},
		{"supervisor loop full pass", nil, func(t *testing.T, r *holdAutoRig) {
			for _, res := range r.steps.RunPass(context.Background()) {
				if res.Err != nil {
					t.Errorf("step %s: %v", res.Step, res.Err)
				}
			}
		}},
		{"loop rescue", nil, func(t *testing.T, r *holdAutoRig) {
			for _, accepts := range []bool{true, false} {
				s := &holdAutoLoopSession{accepts: accepts}
				act, err := r.h.m.RescueLoop(holdAutoFailedTask, s, LoopFinding{Kind: KindRepeat, Action: "read_file"})
				if err != nil || act != LoopRescueSkipped || s.input != 0 || s.stopped != 0 {
					t.Errorf("accepts=%v: action=%s err=%v input=%d stopped=%d; want skipped with no side effect",
						accepts, act, err, s.input, s.stopped)
				}
			}
		}},
		{"stuck running watchdog", nil, func(t *testing.T, r *holdAutoRig) {
			r.h.m.sweepStuckRunning()
			r.advance(time.Hour)
			r.h.m.sweepStuckRunning()
			r.advance(time.Hour)
			r.h.m.sweepStuckRunning()
		}},
		{"verifier and judge reconcile", nil, func(t *testing.T, r *holdAutoRig) {
			host := &holdAutoSessionHost{
				verifiers: []VerifierSession{{ID: "ver-1", TaskID: holdAutoDoneTask, Started: holdClockStart}},
				judges:    []JudgeSession{{ID: "judge-1", UnitID: holdAutoQueuedTask, Started: holdClockStart}},
			}
			if got := r.h.m.ReconcileVerifiers(host); len(got) != 0 {
				t.Errorf("verifier reconcile acted: %+v", got)
			}
			if got := r.h.m.ReconcileJudges(host); len(got) != 0 {
				t.Errorf("judge reconcile acted: %+v", got)
			}
			if len(host.ended) != 0 || len(host.boundLease) != 0 {
				t.Errorf("sessions touched: ended=%v bound=%v", host.ended, host.boundLease)
			}
		}},
		{"automation reconcile on boot", func(t *testing.T, r *holdAutoRig) {
			r.h.m.mu.Lock()
			r.h.m.coordinator = r.h.c
			r.h.m.mu.Unlock()
			r.auto = r.h.force(r.h.makeAuto(chainEdges(), 2))
		}, func(t *testing.T, r *holdAutoRig) {
			before := holdAutoSnapshotAutomation(t, r.h, r.auto)
			r.h.c.reconcile()
			if after := holdAutoSnapshotAutomation(t, r.h, r.auto); after != before {
				t.Errorf("the Automation changed:\nbefore %s\nafter  %s", before, after)
			}
		}},
		{"boot resume", nil, func(t *testing.T, r *holdAutoRig) {
			r.api.resumeRunningPRDs()
			r.api.execWG.Wait()
		}},
		{"retry policy", nil, func(t *testing.T, r *holdAutoRig) {
			prd, _ := r.h.m.Store().GetPRD(r.heldID)
			if r.h.m.retryBudgetRemaining(prd, "infra") {
				t.Errorf("a retry budget is offered to a held Automaton")
			}
			task := lookupTask(prd, holdAutoFailedTask)
			if held, err := r.h.m.holdAfterCeilingFailures(prd, task); held || err != nil {
				t.Errorf("holdAfterCeilingFailures = %v, %v; want no change", held, err)
			}
			if _, _, err := r.h.m.synthesizeCorrectiveWork(prd, GuardrailVerdict{Guardrail: "rules"}, ""); err == nil {
				t.Errorf("corrective work was added to a held Automaton")
			}
		}},
		{"infra retry dependents", nil, func(t *testing.T, r *holdAutoRig) {
			r.h.m.mu.Lock()
			r.h.m.cfg.InfraRetry.RetryDependents = &enabled
			r.h.m.mu.Unlock()
			prd, _ := r.h.m.Store().GetPRD(r.heldID)
			if ids := r.h.m.recoverInfraDependents(prd); len(ids) != 0 {
				t.Errorf("requeued %v for a held Automaton", ids)
			}
		}},
		{"rejudge", nil, func(t *testing.T, r *holdAutoRig) {
			if _, err := r.h.m.RejudgeCompletionGuardrail(context.Background(), r.heldID, "operator", ""); err == nil {
				t.Errorf("a held Automaton was rejudged")
			}
		}},
		{"merge retry and triage pass", nil, func(t *testing.T, r *holdAutoRig) {
			if n := r.h.m.MergeRetryPass(context.Background()); n != 0 {
				t.Errorf("MergeRetryPass merged %d tasks", n)
			}
			r.h.c.triagePass()
		}},
		{"completed migration", nil, func(t *testing.T, r *holdAutoRig) {
			for _, opts := range []CompletedMigrationOpts{{}, {DryRun: true}, {ID: r.heldID}} {
				rep, err := r.h.m.MigrateCompleted(context.Background(), opts)
				if err != nil {
					t.Fatalf("MigrateCompleted(%+v): %v", opts, err)
				}
				if rep.Changed != 0 || len(rep.Completed) != 0 || len(rep.WouldComplete) != 0 || rep.LeftOther < 1 {
					t.Errorf("MigrateCompleted(%+v) = %+v; want the held record counted as left alone", opts, rep)
				}
			}
		}},
		{"protected paths false hold release", nil, func(t *testing.T, r *holdAutoRig) {
			if got := r.h.m.ReleaseFalseProtectedPathHolds(context.Background()); len(got) != 0 {
				t.Errorf("released %v for a held Automaton", got)
			}
		}},
	}
	for _, p := range passes {
		t.Run(p.name, func(t *testing.T) {
			r := newHoldAutoRig(t).build()
			if p.setup != nil {
				p.setup(t, r)
			}
			before := holdAutoSnapshot(t, r.h.m, r.heldID)
			p.run(t, r)
			if after := holdAutoSnapshot(t, r.h.m, r.heldID); after != before {
				t.Errorf("the held Automaton or a task changed:\nbefore %s\nafter  %s", before, after)
			}
			r.assertNoOutwardCalls(p.name)
		})
	}
}

func holdAutoSnapshotAutomation(t *testing.T, h *coordHarness, a *Automation) string {
	t.Helper()
	b, err := json.Marshal(h.get(a))
	if err != nil {
		t.Fatalf("marshal Automation: %v", err)
	}
	return string(b)
}

func TestHoldHousekeepingRepairSkipsHeld(t *testing.T) {
	r := newHoldAutoRig(t).build()
	ctx := context.Background()
	// The ordinary Automaton beside it is running with every task terminal and
	// carries a failed dependent behind a recovered root: the repairs do find it.
	holdAutoSeed(t, r.h.m, r.freeID, []Task{
		{ID: "free-root", Title: "free root", Spec: "x", Status: TaskCompleted},
		{ID: "free-dep", Title: "free dependent", Spec: "x", Status: TaskFailed, DependsOn: []string{"free root"},
			FailureReason: &FailureReason{Category: infraRetryFailureCategory, Source: dependencyInfraSource, DependsOnTaskID: "free-root"}},
	}, false)
	r.h.m.Store().MutatePRD(r.freeID, func(p *PRD) bool { p.Status = PRDRunning; return true })
	before := holdAutoSnapshot(t, r.h.m, r.heldID)

	chain := ManagerChainOps{API: r.api}
	var heldTasks, freeTasks int
	for _, ct := range chain.ChainTasks(ctx) {
		switch ct.AutomatonID {
		case r.heldID:
			heldTasks++
		case r.freeID:
			freeTasks++
		}
	}
	if heldTasks != 0 || freeTasks != 2 {
		t.Errorf("chain listing: held tasks %d (want 0), free tasks %d (want 2)", heldTasks, freeTasks)
	}
	var heldSeen, freeSeen bool
	for _, a := range (ManagerAdvanceOps{API: r.api}).Automata(ctx) {
		heldSeen = heldSeen || a.ID == r.heldID
		freeSeen = freeSeen || a.ID == r.freeID
	}
	if heldSeen || !freeSeen {
		t.Errorf("advance listing: held seen %v (want false), free seen %v (want true)", heldSeen, freeSeen)
	}
	for _, f := range (advanceJudgeRepair{s: r.steps}).Detect(ctx) {
		if strings.Contains(f.Subject, r.heldID) {
			t.Errorf("finding %+v names the held Automaton", f)
		}
	}
	found := 0
	for i, rp := range NewTaskChainRepairs(chain) {
		for _, f := range rp.Detect(ctx) {
			if strings.Contains(f.Subject, r.heldID) {
				t.Errorf("task chain repair %d found %+v for the held Automaton", i, f)
			}
			if strings.Contains(f.Subject, r.freeID) {
				found++
			}
		}
	}
	if found == 0 {
		t.Errorf("no repair found the ordinary Automaton's dependent; the test cannot show anything")
	}
	if after := holdAutoSnapshot(t, r.h.m, r.heldID); after != before {
		t.Errorf("the held Automaton changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestHoldSupervisorLoopLeavesHeld(t *testing.T) {
	r := newHoldAutoRig(t).build()
	ctx := context.Background()
	before := holdAutoSnapshot(t, r.h.m, r.heldID)
	for i := 0; i < 3; i++ {
		for _, res := range r.steps.RunPass(ctx) {
			if res.Err != nil {
				t.Errorf("pass %d step %s: %v", i, res.Step, res.Err)
			}
		}
	}
	if detail, err := r.steps.retryFailedInfra(ctx); err != nil || !strings.Contains(detail, "0 of 0") {
		t.Errorf("retryFailedInfra = %q, %v", detail, err)
	}
	if _, err := r.steps.closeFinishedRecords(ctx); err != nil {
		t.Errorf("closeFinishedRecords: %v", err)
	}
	got, ok := r.h.m.Store().GetPRD(r.heldID)
	if !ok || got.Status == PRDArchived {
		t.Fatalf("the loop archived the held Automaton: %+v", got)
	}
	s := &holdAutoLoopSession{accepts: true}
	if act, _ := r.h.m.RescueLoop(holdAutoQueuedTask, s, LoopFinding{Kind: KindAnnounce, Action: "now i will write"}); act != LoopRescueSkipped || s.input != 0 {
		t.Errorf("the loop rescued a task of a held Automaton: %s input=%d", act, s.input)
	}
	if after := holdAutoSnapshot(t, r.h.m, r.heldID); after != before {
		t.Errorf("the held Automaton changed:\nbefore %s\nafter  %s", before, after)
	}
	r.assertNoOutwardCalls("supervisor loop")
}

func TestHoldStuckWatchdogIgnoresHeld(t *testing.T) {
	r := newHoldAutoRig(t).build()
	// Every task of the held Automaton is terminal, the shape the watchdog
	// re-fires and then blocks when the Automaton is running.
	holdAutoSeed(t, r.h.m, r.heldID, []Task{
		{ID: "all-done-1", Title: "done 1", Spec: "x", Status: TaskCompleted},
		{ID: "all-done-2", Title: "done 2", Spec: "x", Status: TaskCancelled},
	}, true)
	holdAutoSeed(t, r.h.m, r.freeID, []Task{{ID: "free-done", Title: "free done", Spec: "x", Status: TaskCompleted}}, false)
	r.h.m.Store().MutatePRD(r.freeID, func(p *PRD) bool { p.Status = PRDRunning; return true })
	before := holdAutoSnapshot(t, r.h.m, r.heldID)

	for i := 0; i < 4; i++ {
		r.h.m.sweepStuckRunning()
		r.advance(time.Hour)
	}

	r.mu.Lock()
	refires := append([]string(nil), r.refires...)
	audits := append([]string(nil), r.audits...)
	r.mu.Unlock()
	if len(refires) == 0 {
		t.Fatalf("the watchdog never re-fired the running control Automaton; the test cannot show anything")
	}
	for _, id := range refires {
		if id == r.heldID {
			t.Errorf("the watchdog re-fired the held Automaton")
		}
	}
	for _, a := range audits {
		if a == audit.ActionStuckRunningBlocked {
			if got, _ := r.h.m.Store().GetPRD(r.heldID); got.Status != PRDHeld {
				t.Errorf("the held Automaton was blocked as stuck: %s", got.Status)
			}
		}
	}
	if after := holdAutoSnapshot(t, r.h.m, r.heldID); after != before {
		t.Errorf("the held Automaton changed:\nbefore %s\nafter  %s", before, after)
	}
	r.h.m.mu.Lock()
	_, tracked := r.h.m.stuckSeen[r.heldID]
	r.h.m.mu.Unlock()
	if tracked {
		t.Errorf("the watchdog tracks the held Automaton as stuck")
	}
}

func TestHoldBootReconcileKeepsHeld(t *testing.T) {
	// A daemon restart: a second Manager opens the same data directory and the
	// boot passes run. The Automaton comes back held, with its hold record.
	dir := t.TempDir()
	m1, err := NewManager(dir, DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m1.CreatePRD("held spec", t.TempDir(), "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	holdAutoSeed(t, m1, prd.ID, holdAutoTasks(""), true)
	// A held record whose tasks look like the daemon killed them mid-run is the
	// shape boot-resume recovers when the Automaton is failed; held it stays.
	m1.Store().MutatePRD(prd.ID, func(p *PRD) bool {
		p.Story[0].Tasks[3].Status = TaskInProgress
		return true
	})
	want := holdAutoSnapshot(t, m1, prd.ID)

	m2, err := NewManager(dir, DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := holdAutoSnapshot(t, m2, prd.ID); got != want {
		t.Fatalf("the reopened record differs:\nwant %s\ngot  %s", want, got)
	}
	api := NewAPI(m2)
	var spawned []string
	api.spawnFn = func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned = append(spawned, req.TaskID)
		return SpawnResult{SessionID: "sess"}, nil
	}
	done := make(chan struct{})
	go func() { api.resumeRunningPRDs(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("resumeRunningPRDs did not return")
	}
	api.execWG.Wait()
	if got := holdAutoSnapshot(t, m2, prd.ID); got != want {
		t.Errorf("boot passes changed the held Automaton:\nwant %s\ngot  %s", want, got)
	}
	if len(spawned) != 0 {
		t.Errorf("boot passes started tasks %v of a held Automaton", spawned)
	}
	if got, _ := m2.Store().GetPRD(prd.ID); got.Status != PRDHeld || got.Hold == nil || got.Hold.ExpiredAt == nil {
		t.Errorf("after restart: status %s hold %+v; want held with its expiry mark", got.Status, got.Hold)
	}

	// The Automation reconcile leaves a running member whose Automaton is held.
	h, rs := newReconcileHarness(t, 2)
	holdAutoSeed(t, h.m, h.prds[0].ID, holdAutoTasks(""), true)
	a := h.force(h.makeAuto(chainEdges(), 2))
	beforeAuto := holdAutoSnapshotAutomation(t, h, a)
	beforePRD := holdAutoSnapshot(t, h.m, h.prds[0].ID)
	h.c.reconcile()
	if got := holdAutoSnapshotAutomation(t, h, a); got != beforeAuto {
		t.Errorf("the Automation changed on boot:\nbefore %s\nafter  %s", beforeAuto, got)
	}
	if got := holdAutoSnapshot(t, h.m, h.prds[0].ID); got != beforePRD {
		t.Errorf("the held member changed on boot:\nbefore %s\nafter  %s", beforePRD, got)
	}
	if runs, resumes := rs.counts(); len(runs) != 0 || len(resumes) != 0 {
		t.Errorf("boot reconcile started work: runs=%v resumes=%v", runs, resumes)
	}
}

func TestHoldInfraRetryDoesNotRequeueHeldTasks(t *testing.T) {
	r := newHoldAutoRig(t).build()
	enabled := true
	r.h.m.mu.Lock()
	r.h.m.cfg.InfraRetry.RetryDependents = &enabled
	r.h.m.mu.Unlock()

	// An ordinary Automaton with the same shape is recovered, so the test can
	// tell a guard from a recovery that never applies.
	holdAutoSeed(t, r.h.m, r.freeID, []Task{
		{ID: "free-root", Title: "free root", Spec: "x", Status: TaskCompleted},
		{ID: "free-dep", Title: "free dependent", Spec: "x", Status: TaskFailed, DependsOn: []string{"free root"},
			FailureReason: &FailureReason{Category: infraRetryFailureCategory, Source: dependencyInfraSource,
				DependsOnTaskID: "free-root", Retryable: true,
				Detail: "dependency free-root failed for an infrastructure reason (network): timeout"}},
	}, false)
	free, _ := r.h.m.Store().GetPRD(r.freeID)
	if ids := r.h.m.recoverInfraDependents(free); len(ids) != 1 || ids[0] != "free-dep" {
		t.Fatalf("control Automaton: recovered %v, want [free-dep]", ids)
	}

	before := holdAutoSnapshot(t, r.h.m, r.heldID)
	held, _ := r.h.m.Store().GetPRD(r.heldID)
	if ids := r.h.m.recoverInfraDependents(held); len(ids) != 0 {
		t.Errorf("recoverInfraDependents requeued %v of a held Automaton", ids)
	}
	if detail, err := r.steps.retryFailedInfra(context.Background()); err != nil || !strings.Contains(detail, "0 of 0") {
		t.Errorf("retryFailedInfra = %q, %v; want the held review task left alone", detail, err)
	}
	for _, id := range []string{holdAutoFailedTask, holdAutoReviewTask} {
		got, _ := r.h.m.Store().GetPRD(r.heldID)
		tk := lookupTask(got, id)
		if tk.Status == TaskPending || tk.Status == TaskQueued || tk.FailureReason == nil {
			t.Errorf("task %s was requeued: status %s", id, tk.Status)
		}
	}
	if after := holdAutoSnapshot(t, r.h.m, r.heldID); after != before {
		t.Errorf("the held Automaton changed:\nbefore %s\nafter  %s", before, after)
	}
	r.assertNoOutwardCalls("infra retry")
}
