package autonomous

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Operator review decisions made while the Automaton's executor is still live.
// Every scenario runs against the fakes of the failure escalation tests, in
// both modes: a single task slot (the sequential walk) and two task slots (the
// concurrent pool). The held task is held the way a run holds it, by failing
// its verifier until the escalation limit; a second task is blocked inside its
// fake spawn (or its fake verify) so the executor is provably live while the
// operator acts. Fakes are coordinated with channels, never fixed sleeps.

const (
	liveHeldTitle   = "Held task"
	liveBusyTitle   = "Busy task"
	liveFillerTitle = "Filler task"
	liveFollowTitle = "Follower task"

	// Explicit IDs keep the run order independent of ID generation: the order
	// of a pass is the sorted order of the IDs that have no dependency.
	liveHeldID   = "live-1-held"
	liveBusyID   = "live-2-busy"
	liveFillerID = "live-3-filler"
	liveFollowID = "live-4-follower"

	// liveReviewWait bounds every wait. It is a failure timeout only; a passing
	// run never waits for it.
	liveReviewWait = 10 * time.Second
	// liveReviewMaxDecision is the most a review decision may take to return.
	// The executor slot used to be waited for during five seconds.
	liveReviewMaxDecision = 2 * time.Second
)

type liveReviewMode struct {
	name        string
	concurrency int
}

func (m liveReviewMode) concurrent() bool { return m.concurrency > 1 }

var liveReviewModes = []liveReviewMode{
	{name: "sequential", concurrency: 1},
	{name: "concurrent", concurrency: 2},
}

// liveReviewOpts shapes one rig.
type liveReviewOpts struct {
	mode liveReviewMode
	// follower adds a task that depends on the held task.
	follower bool
	// blockInVerify makes the busy task block in its verifier instead of its
	// spawn, so its worker has already run when the operator acts.
	blockInVerify bool
	// busyOpen lets the busy task run through without blocking.
	busyOpen bool
}

// liveReviewRig is one Automaton run against the fakes. The held task fails
// verification until healed is set. In a concurrent pass a filler task joins
// the pass: it can only start once the held task has given up its slot, so its
// spawn shows that the coordinator has taken the hold.
type liveReviewRig struct {
	m             *Manager
	api           *API
	mode          liveReviewMode
	prdID         string
	spawns        escalationSpawnLog
	healed        atomic.Bool
	blockInVerify bool

	// entered reports that the busy task is blocked; gate releases it.
	entered  chan struct{}
	gate     chan struct{}
	gateOnce sync.Once

	// executors holds the review wake channel each spawn ran under. A channel
	// belongs to one executor lifetime, so one entry means one executor.
	mu        sync.Mutex
	executors map[<-chan struct{}]bool
}

func newLiveReviewRig(t *testing.T, o liveReviewOpts) *liveReviewRig {
	t.Helper()
	// The task directories are made before the fixture registers its drain, so
	// they outlive it.
	task := func(id, title string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: "do the " + title, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	tasks := []Task{task(liveHeldID, liveHeldTitle), task(liveBusyID, liveBusyTitle)}
	if o.mode.concurrent() && !o.busyOpen {
		tasks = append(tasks, task(liveFillerID, liveFillerTitle))
	}
	if o.follower {
		tasks = append(tasks, task(liveFollowID, liveFollowTitle, liveHeldTitle))
	}
	m, api := newFailureEscalationFixture(t, t.TempDir())
	r := &liveReviewRig{
		m:             m,
		api:           api,
		mode:          o.mode,
		blockInVerify: o.blockInVerify,
		entered:       make(chan struct{}, 1),
		gate:          make(chan struct{}),
		executors:     map[<-chan struct{}]bool{},
	}
	r.prdID = newHoldPRD(t, m, tasks, func(p *PRD) { p.MaxConcurrentTasks = o.mode.concurrency }).ID
	api.SetExecutors(r.spawn, r.verify)
	// Cleanups run last in first out: the busy task is released before the
	// fixture's drain waits for the executor.
	t.Cleanup(r.releaseBusy)
	if o.busyOpen {
		r.releaseBusy()
	}
	return r
}

func (r *liveReviewRig) releaseBusy() { r.gateOnce.Do(func() { close(r.gate) }) }

// blockBusy reports that the busy task is blocked, then holds it until the gate
// opens or the run is cancelled.
func (r *liveReviewRig) blockBusy(ctx context.Context) error {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-r.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *liveReviewRig) spawn(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
	r.mu.Lock()
	r.executors[reviewWakeFrom(ctx)] = true
	r.mu.Unlock()
	res, err := r.spawns.spawn(ctx, req)
	if req.Title == liveBusyTitle && !r.blockInVerify {
		if berr := r.blockBusy(ctx); berr != nil {
			return SpawnResult{}, berr
		}
	}
	return res, err
}

func (r *liveReviewRig) verify(ctx context.Context, _ *PRD, task *Task) (VerificationResult, error) {
	switch {
	case task.Title == liveHeldTitle && !r.healed.Load():
		return VerificationResult{OK: false, Summary: "tests still fail"}, nil
	case task.Title == liveBusyTitle && r.blockInVerify:
		if err := r.blockBusy(ctx); err != nil {
			return VerificationResult{}, err
		}
	}
	return VerificationResult{OK: true, Summary: "tests pass"}, nil
}

// startUntilBusy starts the run and waits until the busy task is blocked.
func (r *liveReviewRig) startUntilBusy(t *testing.T) {
	t.Helper()
	if err := r.api.Run(r.prdID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-r.entered:
	case <-time.After(liveReviewWait):
		t.Fatalf("the busy task did not block within %s", liveReviewWait)
	}
}

// snapshot returns a copy of the task, so a test reads it without racing the
// executor.
func (r *liveReviewRig) snapshot(t *testing.T, id string) Task {
	t.Helper()
	prd, ok := r.m.Store().SnapshotPRD(r.prdID)
	if !ok {
		t.Fatalf("PRD %s not found", r.prdID)
	}
	task := lookupTask(prd, id)
	if task == nil {
		t.Fatalf("task %s not found in PRD %s", id, r.prdID)
	}
	return *task
}

// awaitHold waits until the held task is held for review. In a concurrent pass
// it also waits for the filler to start, which is the coordinator's reaction to
// the slot the hold freed.
func (r *liveReviewRig) awaitHold(t *testing.T) {
	t.Helper()
	pollUntil(t, liveReviewWait, "the held task to be held for review", func() bool {
		prd, ok := r.m.Store().SnapshotPRD(r.prdID)
		if !ok {
			return false
		}
		task := lookupTask(prd, liveHeldID)
		return task != nil && task.Status == TaskReviewNeeded
	})
	if r.mode.concurrent() {
		pollUntil(t, liveReviewWait, "the filler task to start in the freed slot", func() bool {
			return r.spawns.spawnsFor(liveFillerID) > 0
		})
	}
	if n := r.spawns.spawnsFor(liveHeldID); n != defaultFailureEscalationMaxFailures {
		t.Fatalf("held task spawns before the hold = %d, want %d", n, defaultFailureEscalationMaxFailures)
	}
}

// holdBeforeRun holds the held task before the run starts, the way a hold left
// by an earlier pass is found by the next one.
func (r *liveReviewRig) holdBeforeRun(t *testing.T) {
	t.Helper()
	prd, ok := r.m.Store().GetPRD(r.prdID)
	if !ok {
		t.Fatalf("PRD %s not found", r.prdID)
	}
	held := lookupTask(prd, liveHeldID)
	r.m.Store().WithLock(func() { held.EscalationFailures = defaultFailureEscalationMaxFailures })
	if err := r.m.holdTaskForReview(prd, held, "verification", "tests still fail"); err != nil {
		t.Fatalf("holdTaskForReview: %v", err)
	}
}

// resolve is the operator's decision on the held task through the API. It must
// return well under the old wait for the executor slot.
func (r *liveReviewRig) resolve(t *testing.T, action string) {
	t.Helper()
	start := time.Now()
	_, err := r.api.ResolveTaskReview(r.prdID, liveHeldID, ReviewResolution{Action: action}, "operator-test")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ResolveTaskReview %s with a live executor: %v", action, err)
	}
	if elapsed >= liveReviewMaxDecision {
		t.Fatalf("ResolveTaskReview %s took %s, want under %s: it must not wait for the executor", action, elapsed, liveReviewMaxDecision)
	}
}

// liveSlots counts the executor slots held for the Automaton.
func (r *liveReviewRig) liveSlots() int {
	r.api.runMu.Lock()
	defer r.api.runMu.Unlock()
	return len(r.api.runCancels)
}

func (r *liveReviewRig) assertLiveSlots(t *testing.T, want int) {
	t.Helper()
	if got := r.liveSlots(); got != want {
		t.Fatalf("executor slots = %d, want %d", got, want)
	}
}

// assertExecutors checks how many executor lifetimes the spawns ran under.
func (r *liveReviewRig) assertExecutors(t *testing.T, want int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.executors[nil] {
		t.Fatal("a spawn ran outside a live executor: its context carries no review wake")
	}
	if got := len(r.executors); got != want {
		t.Fatalf("spawns ran under %d executors, want %d", got, want)
	}
}

// runPasses counts the Run passes the Automaton has had, from its run
// decisions.
func (r *liveReviewRig) runPasses(t *testing.T) int {
	t.Helper()
	prd, ok := r.m.Store().SnapshotPRD(r.prdID)
	if !ok {
		t.Fatalf("PRD %s not found", r.prdID)
	}
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == "run" {
			n++
		}
	}
	return n
}

// finish releases the busy task and waits until the executor has exited and
// every goroutine of the run is gone.
func (r *liveReviewRig) finish(t *testing.T) {
	t.Helper()
	r.releaseBusy()
	waitExecutorGone(t, r.api, r.prdID, liveReviewWait)
	if err := r.api.WaitIdle(liveReviewWait); err != nil {
		t.Fatalf("executor goroutines outlived the run: %v", err)
	}
	r.assertLiveSlots(t, 0)
}

func (r *liveReviewRig) assertTask(t *testing.T, id string, want TaskStatus) {
	t.Helper()
	// An empty status reads as pending.
	if got := r.snapshot(t, id); normTaskStatus(got.Status) != normTaskStatus(want) {
		t.Fatalf("task %s status = %q, want %q", id, got.Status, want)
	}
}

func (r *liveReviewRig) assertPRD(t *testing.T, want PRDStatus) {
	t.Helper()
	prd, ok := r.m.Store().SnapshotPRD(r.prdID)
	if !ok {
		t.Fatalf("PRD %s not found", r.prdID)
	}
	if prd.Status != want {
		t.Fatalf("Automaton status = %q, want %q", prd.Status, want)
	}
}

func (r *liveReviewRig) assertSpawns(t *testing.T, id string, want int) {
	t.Helper()
	if got := r.spawns.spawnsFor(id); got != want {
		t.Fatalf("task %s spawns = %d, want %d", id, got, want)
	}
}

// A retry of a held task, made while another task is blocked in its spawn,
// returns at once and does not start a second executor. The retried task is
// spawned again by the executor that is already live: at the next task boundary
// in the sequential walk, and immediately in the concurrent pool.
func TestLiveReview_RetryWhileTaskRunsDoesNotWaitForExecutor(t *testing.T) {
	for _, mode := range liveReviewModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newLiveReviewRig(t, liveReviewOpts{mode: mode})
			r.startUntilBusy(t)
			r.awaitHold(t)
			r.assertLiveSlots(t, 1)

			r.healed.Store(true)
			r.resolve(t, reviewActionRetry)
			r.assertLiveSlots(t, 1)

			held := defaultFailureEscalationMaxFailures
			if mode.concurrent() {
				// A free slot takes the retried task while the other still runs.
				pollUntil(t, liveReviewWait, "the retried task to start in the same executor", func() bool {
					return r.spawns.spawnsFor(liveHeldID) == held+1
				})
			} else {
				// The only slot is busy: nothing else can have been spawned.
				r.assertSpawns(t, liveHeldID, held)
			}
			r.assertSpawns(t, liveBusyID, 1)
			r.assertPRD(t, PRDRunning)

			r.finish(t)
			r.assertSpawns(t, liveHeldID, held+1)
			r.assertSpawns(t, liveBusyID, 1)
			r.assertTask(t, liveHeldID, TaskCompleted)
			r.assertTask(t, liveBusyID, TaskCompleted)
			r.assertPRD(t, PRDCompleted)
			r.assertExecutors(t, 1)
			if n := r.runPasses(t); n != 1 {
				t.Fatalf("run passes = %d, want 1: the decision must not start another Run", n)
			}
		})
	}
}

// A task that was already held when the pass began is not in the pass's order.
// A retry made while another task runs still reaches it.
func TestLiveReview_RetryOfTaskHeldBeforeThePass(t *testing.T) {
	for _, mode := range liveReviewModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newLiveReviewRig(t, liveReviewOpts{mode: mode})
			r.holdBeforeRun(t)
			r.healed.Store(true)
			r.startUntilBusy(t)
			r.assertTask(t, liveHeldID, TaskReviewNeeded)

			r.resolve(t, reviewActionRetry)
			r.assertLiveSlots(t, 1)
			if mode.concurrent() {
				pollUntil(t, liveReviewWait, "the retried task to start while the busy task runs", func() bool {
					return r.spawns.spawnsFor(liveHeldID) == 1
				})
			} else {
				r.assertSpawns(t, liveHeldID, 0)
			}

			r.finish(t)
			r.assertSpawns(t, liveHeldID, 1)
			r.assertTask(t, liveHeldID, TaskCompleted)
			r.assertPRD(t, PRDCompleted)
			r.assertExecutors(t, 1)
		})
	}
}

// A cancel of a held task, made while another task runs, returns at once and
// ends the held task cancelled. Its dependent fails as it does for any
// cancelled dependency and is never spawned. The executor exits cleanly once
// the live task finishes.
func TestLiveReview_CancelWhileTaskRunsFailsDependents(t *testing.T) {
	for _, mode := range liveReviewModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newLiveReviewRig(t, liveReviewOpts{mode: mode, follower: true})
			r.startUntilBusy(t)
			r.awaitHold(t)
			r.assertTask(t, liveFollowID, TaskPending)

			r.resolve(t, reviewActionCancel)
			r.assertTask(t, liveHeldID, TaskCancelled)
			r.assertLiveSlots(t, 1)

			r.finish(t)
			r.assertTask(t, liveHeldID, TaskCancelled)
			r.assertTask(t, liveBusyID, TaskCompleted)
			follower := r.snapshot(t, liveFollowID)
			if follower.Status != TaskFailed || !strings.Contains(follower.Error, liveHeldID) {
				t.Fatalf("follower status %q error %q, want failed because task %s was cancelled", follower.Status, follower.Error, liveHeldID)
			}
			r.assertSpawns(t, liveFollowID, 0)
			r.assertSpawns(t, liveHeldID, defaultFailureEscalationMaxFailures)
			// The failed dependent takes the Automaton down, as it would for any
			// failed task; the cancel itself is not a failure.
			r.assertPRD(t, PRDFailed)
			r.assertExecutors(t, 1)
			if !prdHasDecisionKind(r.m, r.prdID, reviewDecisionKind) {
				t.Fatal("the operator's cancel left no review_resolved decision")
			}
		})
	}
}

// A decision made while the last live task is in its verifier, so that it lands
// as the executor is about to exit, is not lost: the retried task still runs.
// Which side of the exit the decision reaches depends on the mode and on timing;
// the outcome does not.
func TestReviewWhileRunning_DecisionBeforeLastTaskFinishesIsNotLost(t *testing.T) {
	for _, mode := range liveReviewModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newLiveReviewRig(t, liveReviewOpts{mode: mode, blockInVerify: true})
			r.startUntilBusy(t)
			r.awaitHold(t)
			r.assertSpawns(t, liveBusyID, 1)

			r.healed.Store(true)
			r.resolve(t, reviewActionRetry)

			// The verifier holds until the decision is applied; it finishes now.
			r.finish(t)
			r.assertSpawns(t, liveHeldID, defaultFailureEscalationMaxFailures+1)
			r.assertTask(t, liveHeldID, TaskCompleted)
			r.assertTask(t, liveBusyID, TaskCompleted)
			r.assertPRD(t, PRDCompleted)
			r.assertExecutors(t, 1)
		})
	}
}

// A decision whose nudge the pass never sees leaves only the pending flag. The
// executor goroutine reads it before it gives up its slot, so the retried task
// runs in a second pass under the same slot. Driven from inside the package: the
// decision is recorded through the Manager, which signals nothing, and the flag
// is set the way a signal sets it.
func TestLiveReview_PendingDecisionAtExitRunsInSecondPass(t *testing.T) {
	for _, mode := range liveReviewModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newLiveReviewRig(t, liveReviewOpts{mode: mode})
			r.startUntilBusy(t)
			r.awaitHold(t)

			r.healed.Store(true)
			if _, err := r.m.ResolveTaskReview(r.prdID, liveHeldID, ReviewResolution{Action: reviewActionRetry}, "operator-test"); err != nil {
				t.Fatalf("ResolveTaskReview: %v", err)
			}
			r.api.reviewWake.mu.Lock()
			r.api.reviewWake.pending[r.prdID] = true
			r.api.reviewWake.mu.Unlock()

			r.finish(t)
			r.assertSpawns(t, liveHeldID, defaultFailureEscalationMaxFailures+1)
			r.assertTask(t, liveHeldID, TaskCompleted)
			r.assertPRD(t, PRDCompleted)
			// One slot held through both passes: the retry did not wait for the
			// slot to free, and no second executor was started.
			r.assertExecutors(t, 1)
			if n := r.runPasses(t); n != 2 {
				t.Fatalf("run passes = %d, want 2: the pending decision must run a second pass", n)
			}
		})
	}
}

// With no executor live, a retry relaunches the run through the existing path,
// under a new executor, and the Automaton runs to completion.
func TestLiveReview_NoLiveExecutorRetryRelaunchesRun(t *testing.T) {
	for _, mode := range liveReviewModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newLiveReviewRig(t, liveReviewOpts{mode: mode, busyOpen: true})
			if err := r.api.Run(r.prdID); err != nil {
				t.Fatalf("Run: %v", err)
			}
			pollEscalationSettled(t, r.m, r.prdID, liveReviewWait)
			waitExecutorGone(t, r.api, r.prdID, liveReviewWait)
			r.assertTask(t, liveHeldID, TaskReviewNeeded)
			r.assertTask(t, liveBusyID, TaskCompleted)
			r.assertPRD(t, PRDBlocked)
			r.assertLiveSlots(t, 0)
			r.assertExecutors(t, 1)

			r.healed.Store(true)
			r.resolve(t, reviewActionRetry)
			pollEscalationSettled(t, r.m, r.prdID, liveReviewWait)

			r.finish(t)
			r.assertSpawns(t, liveHeldID, defaultFailureEscalationMaxFailures+1)
			r.assertSpawns(t, liveBusyID, 1)
			r.assertTask(t, liveHeldID, TaskCompleted)
			r.assertPRD(t, PRDCompleted)
			r.assertExecutors(t, 2)
			if n := r.runPasses(t); n != 2 {
				t.Fatalf("run passes = %d, want 2: the relaunch is a second Run", n)
			}
		})
	}
}
