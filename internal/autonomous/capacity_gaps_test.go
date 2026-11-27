// The known limits of the worker-slot fix: a resume still queued when its
// session ends, a resumed worker whose lease names no session, a resume that
// takes a slot without waiting, and two writers of one tree that block each
// other through a one-slot pool. Fake sessions, temp dirs, no daemon.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/pipeline"
)

// gapSessions is a switchable session-liveness fake.
type gapSessions struct {
	mu   sync.Mutex
	dead map[string]bool
}

func newGapSessions(m *Manager) *gapSessions {
	s := &gapSessions{dead: map[string]bool{}}
	m.SetSessionAliveFn(func(id string) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.dead[id]
	})
	return s
}

func (s *gapSessions) end(id string) {
	s.mu.Lock()
	s.dead[id] = true
	s.mu.Unlock()
}

func gapQueued(led *capacity.Ledger, holder string) bool {
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			return true
		}
	}
	return false
}

func gapTake(t *testing.T, led *capacity.Ledger, holder string) {
	t.Helper()
	req := capacity.Request{Holder: holder, PRDID: "other", Pools: []string{"node:n1"}, Node: "n1"}
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("take slot for %s: %v", holder, err)
	}
}

// gapParkedForResume is a task parked on an answered question while another
// holder has the only slot, so its resume is queued.
func gapParkedForResume(t *testing.T) (*tickFixture, *gapSessions, string) {
	t.Helper()
	f := newTickFixture(t)
	s := newGapSessions(f.m)
	gapTake(t, f.led, "other-hold")
	f.answer(t, Answer{ChosenOptionID: "second", Actor: "operator", Surface: "rest"})
	f.m.QuestionTick(context.Background())
	id := f.task(t).ID
	pollUntil(t, 3*time.Second, "resume queued", func() bool { return gapQueued(f.led, id) })
	return f, s, id
}

func gapResumesDone(t *testing.T, m *Manager, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() { m.waitQuestionResumes(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: the resume is still waiting", what)
	}
}

// A queued resume leaves the queue when its session ends: it neither waits on
// nor takes the slot, and the finished task's verifier gets it.
func TestCapacityGap_QueuedResumeIsDroppedWhenItsSessionEnds(t *testing.T) {
	f, s, id := gapParkedForResume(t)

	s.end("worker-sess")
	pollUntil(t, 3*time.Second, "dead session's resume leaves the queue", func() bool { return !gapQueued(f.led, id) })
	gapResumesDone(t, f.m, "session ended")

	f.led.Release("other-hold")
	if ls := f.led.LeaseOf(id); ls != nil {
		t.Fatalf("the ended session's task holds a slot: %+v", *ls)
	}
	gapTake(t, f.led, VerifyHolder(id))
	if n := len(f.sent()); n != 0 {
		t.Fatalf("prompted an ended session %d time(s)", n)
	}
	got := f.task(t)
	if IsAwaitDecisionWaitReason(got.WaitReason) {
		t.Fatalf("task still reads as queued: %s %q", got.Status, got.WaitReason)
	}
	if got.RetryCount != 2 {
		t.Fatalf("RetryCount = %d, want 2", got.RetryCount)
	}
	f.tick()
	if gapQueued(f.led, id) || f.led.Held(id) {
		t.Fatal("a later tick queued the ended session's resume again")
	}
}

// The slot frees in the same instant the session ends, before any wait pass
// sees it: the grant is given back at once.
func TestCapacityGap_GrantForAnEndedSessionIsReleased(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Hour})
	led.SetLimit("node:n1", 1)
	m.SetCapacity(led, func(string, string) ([]string, string) { return []string{"node:n1"}, "n1" })
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status, tk.SessionID = TaskInProgress, "worker-sess"
	_ = m.Store().SaveTask(tk)
	s := newGapSessions(m)
	var prompts int32
	m.SetSessionContinueFn(func(string) error { atomic.AddInt32(&prompts, 1); return nil })
	gapTake(t, led, "other-hold")

	errCh := make(chan error, 1)
	go func() { errCh <- m.AwaitExternal(context.Background(), cur, tk, "CI run #1") }()
	pollUntil(t, 3*time.Second, "re-admission queued", func() bool { return gapQueued(led, tk.ID) })
	s.end("worker-sess")
	led.Release("other-hold")
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("AwaitExternal for an ended session = %v, want nil so the wait loop sees the session end", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AwaitExternal did not return")
	}
	if ls := led.LeaseOf(tk.ID); ls != nil {
		t.Fatalf("the ended session's task kept the slot: %+v", *ls)
	}
	if n := atomic.LoadInt32(&prompts); n != 0 {
		t.Fatalf("prompted an ended session %d time(s)", n)
	}
	got := testTaskAt(t, m, prd.ID, 0)
	if got.Status != TaskInProgress || got.WaitReason != "" {
		t.Fatalf("task = %s %q, want in_progress with no wait reason", got.Status, got.WaitReason)
	}
}

// A worker resumed after a question holds a lease that names its session, so
// the reaper judges it by the session and not by the spawn grace.
func TestCapacityGap_ResumedQuestionWorkerLeaseNamesItsSession(t *testing.T) {
	f := newTickFixture(t)
	newGapSessions(f.m)
	f.answer(t, Answer{ChosenOptionID: "second", Actor: "operator", Surface: "rest"})
	f.tick()
	ls := f.led.LeaseOf(f.task(t).ID)
	if ls == nil || ls.SessionID != "worker-sess" {
		t.Fatalf("lease = %+v, want it bound to worker-sess", ls)
	}
}

// The same for a worker resumed after an outside wait.
func TestCapacityGap_ResumedOutsideWaitWorkerLeaseNamesItsSession(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status, tk.SessionID = TaskInProgress, "worker-sess"
	_ = m.Store().SaveTask(tk)
	newGapSessions(m)
	if err := m.AwaitExternal(context.Background(), cur, tk, "CI run #2"); err != nil {
		t.Fatalf("AwaitExternal: %v", err)
	}
	ls := led.LeaseOf(tk.ID)
	if ls == nil || ls.SessionID != "worker-sess" {
		t.Fatalf("lease = %+v, want it bound to worker-sess", ls)
	}
}

// gapRetry drives one failed verification followed by a retry that resumes the
// same session, with another holder taking the freed slot before the retry.
type gapRetry struct {
	m      *Manager
	led    *capacity.Ledger
	sess   *gapSessions
	taskID string
	sends  int32
	spawns int32
	verify int32
	atSend atomic.Value // capacity.Lease of the task when the line was sent
	held   atomic.Int32 // node:n1 leases when the line was sent
	done   chan error
}

func startGapRetry(t *testing.T) *gapRetry {
	t.Helper()
	m, _, led := capFixture(t, 1)
	m.mu.Lock()
	m.cfg.RetryResumeMaxContextTokens = 150000
	m.mu.Unlock()
	dir := t.TempDir()
	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "task", Backend: "claude-code"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]

	r := &gapRetry{m: m, led: led, sess: newGapSessions(m), taskID: task.ID, done: make(chan error, 1)}
	m.SetSessionContextTokensFn(func(string) int { return 5000 })
	m.SetSessionContinueLineFn(func(string, string) error {
		atomic.AddInt32(&r.sends, 1)
		if ls := led.LeaseOf(task.ID); ls != nil {
			r.atSend.Store(*ls)
		}
		r.held.Store(int32(len(led.Leases())))
		return nil
	})
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: fmt.Sprintf("sess-%d", atomic.AddInt32(&r.spawns, 1))}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		if atomic.AddInt32(&r.verify, 1) == 1 {
			// The verifier frees the worker slot; another holder takes it.
			led.Release(task.ID)
			gapTake(t, led, "other-hold")
			return VerificationResult{OK: false, Summary: "unit tests failed"}, nil
		}
		return VerificationResult{OK: true, VerifiedAt: time.Now()}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { r.done <- m.executeOne(ctx, prd, task, spawn, verify, 2, pipeline.QualityGateConfig{}, nil) }()
	return r
}

func (r *gapRetry) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("executeOne: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeOne did not finish")
	}
}

// A retry that resumes the same session waits for a slot like any new work.
func TestCapacityGap_RetryResumeWaitsForASlot(t *testing.T) {
	r := startGapRetry(t)
	pollUntil(t, 5*time.Second, "retry queued for the full pool, or resumed over it", func() bool {
		return gapQueued(r.led, r.taskID) || atomic.LoadInt32(&r.sends) > 0
	})
	if n := atomic.LoadInt32(&r.sends); n != 0 {
		t.Fatalf("the session was resumed while another holder has the one slot (%d lease(s) at the send, none of them the task's)", r.held.Load())
	}
	if r.led.Held(r.taskID) {
		t.Fatal("the retry holds a lease while the pool is full")
	}
	r.led.Release("other-hold")
	r.wait(t)
	if atomic.LoadInt32(&r.sends) != 1 || atomic.LoadInt32(&r.spawns) != 1 {
		t.Fatalf("sends=%d spawns=%d, want one resume of the one session", r.sends, r.spawns)
	}
	ls, ok := r.atSend.Load().(capacity.Lease)
	if !ok || ls.SessionID != "sess-1" {
		t.Fatalf("lease at resume = %+v, want one bound to sess-1", ls)
	}
}

// A session that ends while its retry resume is queued starts a fresh attempt.
func TestCapacityGap_RetryResumeSessionEndsWhileQueued(t *testing.T) {
	r := startGapRetry(t)
	pollUntil(t, 5*time.Second, "retry queued", func() bool {
		return gapQueued(r.led, r.taskID) || atomic.LoadInt32(&r.sends) > 0
	})
	r.sess.end("sess-1")
	time.Sleep(100 * time.Millisecond)
	r.led.Release("other-hold")
	r.wait(t)
	if n := atomic.LoadInt32(&r.sends); n != 0 {
		t.Fatalf("sent %d line(s) into an ended session", n)
	}
	if n := atomic.LoadInt32(&r.spawns); n != 2 {
		t.Fatalf("spawns = %d, want a fresh second attempt", n)
	}
}

// A task parked on an outside wait holds no slot. After a restart into a full
// pool it is not prompted until it gets one.
func TestCapacityGap_BootResumeOfParkedTaskWaitsWhenPoolIsFull(t *testing.T) {
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	_ = m.Store().SavePRD(cur)
	tk := &cur.Story[0].Tasks[0]
	tk.Status, tk.SessionID = TaskWaitingCapacity, "parked-sess"
	tk.WaitReason = "await_external: CI run #42"
	_ = m.Store().SaveTask(tk)
	newGapSessions(m)
	var prompts int32
	var leaseAtPrompt atomic.Value
	m.SetSessionContinueFn(func(string) error {
		if ls := led.LeaseOf(tk.ID); ls != nil {
			leaseAtPrompt.Store(*ls)
		}
		atomic.AddInt32(&prompts, 1)
		return nil
	})
	m.SetSessionWaitFn(func(context.Context, AdoptedWait) error { return nil })
	gapTake(t, led, "live-worker")

	m.resetInProgressTasksForResume(cur)
	if led.Held(tk.ID) {
		t.Fatalf("restart put the pool over its limit: %d leases on 1 slot", len(led.Leases()))
	}
	if n := atomic.LoadInt32(&prompts); n != 0 {
		t.Fatal("the parked session was prompted with no slot")
	}

	var spawns int32
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawns, 1)
		return SpawnResult{SessionID: "new"}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.executeOne(ctx, cur, tk, spawn, verify, 0, pipeline.QualityGateConfig{}, nil) }()
	pollUntil(t, 3*time.Second, "parked task queued", func() bool { return gapQueued(led, tk.ID) })
	if n := atomic.LoadInt32(&prompts); n != 0 {
		t.Fatal("prompted while queued")
	}
	if got := testTaskAt(t, m, prd.ID, 0); !IsAwaitExternalWaitReason(got.WaitReason) {
		t.Fatalf("queued task reads %s %q, want the outside-wait reason", got.Status, got.WaitReason)
	}
	led.Release("live-worker")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeOne: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeOne did not finish")
	}
	if atomic.LoadInt32(&prompts) != 1 || atomic.LoadInt32(&spawns) != 0 {
		t.Fatalf("prompts=%d spawns=%d, want one prompt and no new session", prompts, spawns)
	}
	if ls, ok := leaseAtPrompt.Load().(capacity.Lease); !ok || ls.SessionID != "parked-sess" {
		t.Fatalf("lease at prompt = %+v, want one bound to parked-sess", ls)
	}
}

// Two writers of one tree on a one-slot pool: the one waiting for the tree
// must not hold the slot the other one's verifier needs.
func TestCapacityGap_SameTreeWriterHoldsNoSlotWhileWaiting(t *testing.T) {
	fastTreeLockPolling(t)
	m, _, led := capFixture(t, 1)
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 6
	m.mu.Unlock()
	dir := t.TempDir()
	prd, _ := m.CreatePRD("s", dir, "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "T1", Spec: "write docs/a.md"}, {Title: "T2", Spec: "write docs/b.md"}}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status, cur.MaxConcurrentTasks = PRDApproved, 2
	_ = m.Store().SavePRD(cur)

	var mu sync.Mutex
	var verifierFaults []string
	var first sync.Once
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(ctx context.Context, _ *PRD, tk *Task) (VerificationResult, error) {
		// Let the sibling queue for the slot first, as a queued worker does.
		first.Do(func() {
			deadline := time.Now().Add(2 * time.Second)
			for len(led.Snapshot().Waiting) == 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
		})
		led.Release(tk.ID)
		holder := VerifyHolder(tk.ID)
		req := capacity.Request{Holder: holder, PRDID: prd.ID, Pools: []string{"node:n1"}, Node: "n1", Role: capacity.RoleVerifier}
		if err := led.Acquire(ctx, req, 3*time.Second, nil, nil); err != nil {
			mu.Lock()
			verifierFaults = append(verifierFaults, err.Error())
			mu.Unlock()
			return VerificationResult{OK: false, Summary: "verifier: capacity: " + err.Error()}, nil
		}
		led.Release(holder)
		return VerificationResult{OK: true}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := m.Run(ctx, prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(verifierFaults) != 0 {
		t.Errorf("verifier waited behind a writer queued for the tree: %s", strings.Join(verifierFaults, "; "))
	}
	out, _ := m.Store().SnapshotPRD(prd.ID)
	for _, tk := range out.Story[0].Tasks {
		if tk.Status != TaskCompleted {
			t.Errorf("task %s = %s (%s), want completed", tk.Title, tk.Status, tk.Error)
		}
	}
	if n := len(led.Leases()); n != 0 {
		t.Errorf("%d lease(s) left", n)
	}
}

// The same rule on resume: a resumed worker whose tree another writer now
// holds gives its slot up while it waits for the tree.
func TestCapacityGap_ResumedWorkerHoldsNoSlotWhileWaitingForTree(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepo(t, true)
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	a, b := &cur.Story[0].Tasks[0], &cur.Story[0].Tasks[1]
	for _, tk := range []*Task{a, b} {
		tk.ProjectDir, tk.Status = repo, TaskInProgress
		_ = m.Store().SaveTask(tk)
	}
	a.SessionID = "worker-sess"
	_ = m.Store().SaveTask(a)
	newGapSessions(m)
	relA, err := m.acquireTreeWritersLock(context.Background(), cur, a)
	if err != nil {
		t.Fatalf("tree lock a: %v", err)
	}
	boxA := relA
	m.attachTreeRelease(a.ID, &boxA)
	t.Cleanup(func() { m.detachTreeRelease(a.ID) })
	gapTake(t, led, b.ID)

	errCh := make(chan error, 1)
	go func() { errCh <- m.AwaitExternal(context.Background(), cur, a, "CI run #3") }()
	pollUntil(t, 3*time.Second, "a queued", func() bool { return gapQueued(led, a.ID) })
	// b takes the released tree, then frees its slot for its verifier.
	relB, err := m.acquireTreeWritersLock(context.Background(), cur, b)
	if err != nil {
		t.Fatalf("tree lock b: %v", err)
	}
	led.Release(b.ID)

	verifier := capacity.Request{Holder: VerifyHolder(b.ID), PRDID: prd.ID, Pools: []string{"node:n1"}, Node: "n1", Role: capacity.RoleVerifier}
	if err := led.Acquire(context.Background(), verifier, 3*time.Second, nil, nil); err != nil {
		relB()
		t.Logf("a, after b's tree freed: %v", <-errCh)
		t.Fatalf("b's verifier waits behind a, which waits for b's tree: %v", err)
	}
	led.Release(VerifyHolder(b.ID))
	relB()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("AwaitExternal: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a did not resume after the tree freed")
	}
	lockState(t, testLockPath(t, repo), a.ID)
	ls := led.LeaseOf(a.ID)
	if ls == nil || ls.SessionID != "worker-sess" {
		t.Fatalf("lease = %+v, want a resumed with a bound slot", ls)
	}
}

// The daemon's own order after a restart: the ledger holds every new request
// until the surviving work is adopted, then the boot pass ends the hold. A
// parked worker takes that path, in the sequential and the parallel walk.
func TestCapacityGap_BootPassResumesParkedTaskAfterAdoption(t *testing.T) {
	for _, conc := range []int{1, 2} {
		t.Run(fmt.Sprintf("concurrency %d", conc), func(t *testing.T) {
			m, api, led := capFixture(t, 1)
			prd := newPRDWithTasks(t, m, 1)
			cur, _ := m.Store().GetPRD(prd.ID)
			cur.Status, cur.ProjectDir, cur.MaxConcurrentTasks = PRDRunning, t.TempDir(), conc
			_ = m.Store().SavePRD(cur)
			tk := &cur.Story[0].Tasks[0]
			tk.Status, tk.SessionID = TaskWaitingCapacity, "parked-sess"
			tk.WaitReason = "await_external: CI run #42"
			_ = m.Store().SaveTask(tk)
			newGapSessions(m)
			var prompts, spawns int32
			var leaseAtPrompt atomic.Value
			m.SetSessionContinueFn(func(string) error {
				if ls := led.LeaseOf(tk.ID); ls != nil {
					leaseAtPrompt.Store(*ls)
				}
				atomic.AddInt32(&prompts, 1)
				return nil
			})
			m.SetSessionWaitFn(func(context.Context, AdoptedWait) error { return nil })
			led.BeginAdoption()
			api.SetExecutors(func(context.Context, SpawnRequest) (SpawnResult, error) {
				atomic.AddInt32(&spawns, 1)
				return SpawnResult{SessionID: "new"}, nil
			}, func(context.Context, *PRD, *Task) (VerificationResult, error) {
				return VerificationResult{OK: true}, nil
			})
			pollPRDTerminal(t, m, cur.ID, 10*time.Second)
			if err := api.WaitIdle(10 * time.Second); err != nil {
				t.Fatal(err)
			}
			if p, s := atomic.LoadInt32(&prompts), atomic.LoadInt32(&spawns); p != 1 || s != 0 {
				t.Fatalf("prompts=%d spawns=%d, want one prompt and no new session", p, s)
			}
			if ls, ok := leaseAtPrompt.Load().(capacity.Lease); !ok || ls.SessionID != "parked-sess" {
				t.Fatalf("lease at prompt = %+v, want one bound to parked-sess", ls)
			}
			if got := testTaskAt(t, m, prd.ID, 0); got.Status != TaskCompleted {
				t.Fatalf("task = %s (%s), want completed", got.Status, got.Error)
			}
			if n := len(led.Leases()); n != 0 {
				t.Fatalf("%d lease(s) left", n)
			}
		})
	}
}
