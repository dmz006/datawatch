package main

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// The judge lifecycle tests drive the whole judge flow of one guardrail call
// through the production wiring: the routing chooser (judgeBackendChooserFor),
// the attempts and the hold (runJudgeAttempts), the time limit (runJudgeTimed),
// the capacity lease (judgeCapacityAdmit, judgeCapacityTryAdmit), the durable
// try log (judgeTryLogFor) and the alerts (emitJudgeAlert). Only the parts that
// need a daemon or a real session are faked:
//
//   - the lease pool is a real in-memory capacity.Ledger, one slot per backend;
//   - the judge session is judgeFakeSessions, an in-process stand-in for the
//     session manager. The one-shot runner's spawn and poll loop is not run: it
//     polls every three seconds. Its adopt-before-spawn guard is tested in
//     session_judgment_test.go, and the fake session follows the same contract
//     through the judgeLiveSession hook;
//   - the clock is judgeTestClock, advanced by a session that hits its limit;
//   - the alert sink is the autonomous Manager's sink, which emitJudgeAlert uses;
//   - the PRD store is a real autonomous Manager in a temp dir. A daemon restart
//     is a new Manager reading the same store file.
//
// Time limits are a few milliseconds of real time, so each test is fast.

// judgeLifecycleLimit is the judge time limit the lifecycle tests run with.
const judgeLifecycleLimit = 50 * time.Millisecond

// judgeFakeSessions stands in for the session manager. It counts the sessions
// started and killed per backend, and keeps the sessions still running by name.
// A session outlives the daemon that started it, as a tmux session does.
type judgeFakeSessions struct {
	mu     sync.Mutex
	next   int
	spawns map[string]int
	kills  map[string]int
	live   map[string]string // session name -> id of the session still running
}

func newJudgeFakeSessions() *judgeFakeSessions {
	return &judgeFakeSessions{spawns: map[string]int{}, kills: map[string]int{}, live: map[string]string{}}
}

// spawn starts a session named name on backend and returns its id.
func (s *judgeFakeSessions) spawn(backend, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := fmt.Sprintf("sess-%d", s.next)
	s.spawns[backend]++
	s.live[name] = id
	return id
}

// liveID returns the id of the running session named name, or "". It is the
// judgeLiveSession hook a restarted daemon consults before it starts a judge.
func (s *judgeFakeSessions) liveID(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live[name]
}

// kill ends the session named name, which ran on backend.
func (s *judgeFakeSessions) kill(backend, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.live, name)
	s.kills[backend]++
}

func (s *judgeFakeSessions) spawned(backend string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spawns[backend]
}

func (s *judgeFakeSessions) killed(backend string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills[backend]
}

func (s *judgeFakeSessions) running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.live)
}

// judgeLifecycle is one daemon's view of the judge flow: its lease ledger, its
// autonomous manager, and what its judge sessions did.
type judgeLifecycle struct {
	led      *capacity.Ledger
	mgr      *autonomouspkg.Manager
	sessions *judgeFakeSessions
	clock    *judgeTestClock
	limit    time.Duration
	hang     map[string]bool      // backends whose session runs until its time limit
	crash    chan struct{}        // closed to end this daemon mid-judge
	onEnter  func(backend string) // runs once the judge session is live, under its lease

	entered     []string
	budgetAtRun time.Duration // time left in the limit when the judge session started
	timeouts    []judgeTimeoutResult
	alerts      []alertRecord
}

// newJudgeLedger returns a lease ledger with one slot for each backend's pool.
func newJudgeLedger(backends ...string) *capacity.Ledger {
	led := capacity.New(capacity.Options{})
	for _, b := range backends {
		led.SetLimit(judgePoolName(b), 1)
	}
	return led
}

func judgePoolName(backend string) string { return "pool:" + backend }

// judgePoolPerBackend maps each backend to its own pool, with no node.
func judgePoolPerBackend(backend, _ string) ([]string, string) {
	return []string{judgePoolName(backend)}, ""
}

// reopenJudgeManager opens the autonomous manager over the store in dir. A
// second call over the same dir is a restarted daemon reading the same store.
func reopenJudgeManager(t *testing.T, dir string) *autonomouspkg.Manager {
	t.Helper()
	mgr, err := autonomouspkg.NewManager(dir, autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

// openJudgeManager opens the autonomous manager over dir and creates the PRD the
// judge calls run against.
func openJudgeManager(t *testing.T, dir string) (*autonomouspkg.Manager, string) {
	t.Helper()
	mgr := reopenJudgeManager(t, dir)
	prd, err := mgr.CreatePRD("judge lifecycle spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return mgr, prd.ID
}

// newJudgeLifecycle returns a daemon's judge view over led and mgr, and wires the
// judge hooks the daemon's wireCapacity installs over led. The hooks are restored
// when the test ends. The alert sink records every alert the manager raises.
func newJudgeLifecycle(t *testing.T, led *capacity.Ledger, mgr *autonomouspkg.Manager, sessions *judgeFakeSessions) *judgeLifecycle {
	t.Helper()
	lc := &judgeLifecycle{
		led: led, mgr: mgr, sessions: sessions,
		clock: &judgeTestClock{t: time.Unix(1000, 0)},
		limit: judgeLifecycleLimit,
		hang:  map[string]bool{},
	}
	mgr.SetAlertSink(func(level, title, body string) (id string) {
		lc.alerts = append(lc.alerts, alertRecord{level: level, title: title, body: body})
		return ""
	})

	admit, try, started := judgeAdmitters(led, newVerifierRegistry(), judgePoolPerBackend, 5*time.Second)
	origAdmit, origTry, origStarted := judgeCapacityAdmit, judgeCapacityTryAdmit, judgeSessionStarted
	origLive, origWindow := judgeLiveSession, judgeClaudeWindow
	t.Cleanup(func() {
		judgeCapacityAdmit, judgeCapacityTryAdmit, judgeSessionStarted = origAdmit, origTry, origStarted
		judgeLiveSession, judgeClaudeWindow = origLive, origWindow
	})
	judgeCapacityAdmit = func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
		release, err := admit(ctx, backend, model, unitID, prdID)
		return release, "", err
	}
	// The probe takes the lease and lets go at once, as judgeCapacityTryAdmit does.
	judgeCapacityTryAdmit = func(ctx context.Context, backend, model, unitID, prdID string) bool {
		release, ok := try(ctx, backend, model, unitID, prdID)
		if ok {
			release()
		}
		return ok
	}
	judgeSessionStarted = started
	judgeLiveSession = sessions.liveID
	judgeClaudeWindow = autonomouspkg.NewVerifierPolicyWindow(nil)
	return lc
}

// run is one judge call of the guardrail closure: the chooser over the judge
// policy list, the attempts under the time limit, and the hold. The verifier
// policy's backend list is the same list, as the daemon's config keeps them.
func (lc *judgeLifecycle) run(ctx context.Context, req autonomouspkg.GuardrailInvocation, mode string, policy ...string) (autonomouspkg.GuardrailVerdict, error) {
	cfg := judgeRouteConfig(mode, policy...)
	cfg.JudgePolicy = policy
	choose := judgeBackendChooserFor(ctx, lc.mgr, req, cfg, policy[0])
	alert := func(level, title, body string) { emitJudgeAlert(lc.mgr, level, title, body) }
	return runJudgeAttempts(req, choose, lc.limit, alert, judgeTryLogFor(lc.mgr, req), nil, nil, lc.attempt(ctx, req))
}

// attempt is one judge attempt as the guardrail closure runs it: the judge's
// lease first, then the time limit, then the judge session.
func (lc *judgeLifecycle) attempt(ctx context.Context, req autonomouspkg.GuardrailInvocation) judgeAttemptFn {
	return func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
		v, to, err := runJudgeTimed(ctx, judgeCapacityAdmit, att, "", req.UnitID, req.PRDID, lc.limit, lc.clock.now,
			func(runCtx context.Context, _ string) (autonomouspkg.GuardrailVerdict, error) {
				return lc.session(runCtx, backend)
			})
		if to != nil {
			lc.timeouts = append(lc.timeouts, *to)
		}
		return v, err
	}
}

// session is one judge session on backend, run under the judge's lease and the
// time limit in runCtx. A live session with the judge's name is adopted, as the
// one-shot runner does; otherwise one is started. A session that finishes is
// killed and passes. A session in hang runs until runCtx ends, which is its time
// limit: it is then killed and the clock has advanced by the limit. A closed
// crash channel ends the daemon: nothing after that point runs, so the session
// is left alive. runtime.Goexit still runs the deferred lease release.
func (lc *judgeLifecycle) session(runCtx context.Context, backend string) (autonomouspkg.GuardrailVerdict, error) {
	name := judgeSessionNamePrefix + ":u1"
	lc.entered = append(lc.entered, backend)
	if deadline, ok := runCtx.Deadline(); ok {
		lc.budgetAtRun = time.Until(deadline)
	}
	id := judgeLiveSession(name)
	if id == "" {
		id = lc.sessions.spawn(backend, name)
	}
	judgeSessionStarted(name, id)
	if lc.onEnter != nil {
		lc.onEnter(backend)
	}
	if !lc.hang[backend] {
		lc.sessions.kill(backend, name)
		return autonomouspkg.GuardrailVerdict{Outcome: "pass", VerdictAt: time.Now()}, nil
	}
	select {
	case <-runCtx.Done():
		lc.clock.advance(lc.limit)
		lc.sessions.kill(backend, name)
		return autonomouspkg.GuardrailVerdict{}, runCtx.Err()
	case <-lc.crash:
		runtime.Goexit()
	}
	return autonomouspkg.GuardrailVerdict{}, nil
}

// judgeOutcome is what one judge call returned.
type judgeOutcome struct {
	v   autonomouspkg.GuardrailVerdict
	err error
}

// awaitJudge waits for a judge call to return, failing the test if it hangs.
func awaitJudge(t *testing.T, done <-chan judgeOutcome) judgeOutcome {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(10 * time.Second):
		t.Fatal("judge call did not return")
	}
	return judgeOutcome{}
}

// judgeRun runs one judge call in the background, so the test can act while it
// waits.
func (lc *judgeLifecycle) judgeRun(req autonomouspkg.GuardrailInvocation, mode string, policy ...string) <-chan judgeOutcome {
	done := make(chan judgeOutcome, 1)
	go func() {
		v, err := lc.run(context.Background(), req, mode, policy...)
		done <- judgeOutcome{v: v, err: err}
	}()
	return done
}

// holdPool takes the one slot of pool for holder, as another workload would.
func holdPool(t *testing.T, led *capacity.Ledger, holder, pool string) {
	t.Helper()
	if err := led.Acquire(context.Background(), capacity.Request{Holder: holder, Pools: []string{pool}}, time.Second, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("hold %s for %s: %v", pool, holder, err)
	}
}

// judgeProbeWindow is how long the judge's routing probe waits for a slot before
// it gives up (judgeAdmitters.try).
const judgeProbeWindow = 150 * time.Millisecond

// judgeQueued reports whether holder is waiting for a capacity slot now.
func judgeQueued(led *capacity.Ledger, holder string) bool {
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == holder {
			return true
		}
	}
	return false
}

// awaitQueued blocks until holder is waiting for a capacity slot.
func awaitQueued(t *testing.T, led *capacity.Ledger, holder string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if judgeQueued(led, holder) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s never queued for a capacity slot", holder)
}

// judgeHolders returns the holders of pool, failing the test if it is absent.
func judgeHolders(t *testing.T, led *capacity.Ledger, pool string) []string {
	t.Helper()
	return judgePoolStatus(t, led, pool).Holders
}

// assertJudgePoolsFree fails unless every pool has no lease held.
func assertJudgePoolsFree(t *testing.T, led *capacity.Ledger, pools ...string) {
	t.Helper()
	for _, pool := range pools {
		if held := judgePoolStatus(t, led, pool).Held; held != 0 {
			t.Errorf("pool %s holds %d leases after the judge finished (%v), want 0", pool, held, judgeHolders(t, led, pool))
		}
	}
}

// judgePolicyDecisions returns the judge routing decisions the PRD recorded, in order.
func judgePolicyDecisions(t *testing.T, mgr *autonomouspkg.Manager, prdID string) []autonomouspkg.Decision {
	t.Helper()
	prd, ok := mgr.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	var out []autonomouspkg.Decision
	for _, d := range prd.Decisions {
		if d.Kind == "judge_policy" {
			out = append(out, d)
		}
	}
	return out
}

// judgeRequest is the guardrail invocation of one unit's judge call.
func judgeRequest(prdID string) autonomouspkg.GuardrailInvocation {
	return autonomouspkg.GuardrailInvocation{PRDID: prdID, UnitID: "u1", Guardrail: "rules"}
}

func TestJudgeLifecycleWaitsForFullPoolAndStartsWhenSlotFrees(t *testing.T) {
	led := newJudgeLedger("qwen")
	mgr, prdID := openJudgeManager(t, t.TempDir())
	lc := newJudgeLifecycle(t, led, mgr, newJudgeFakeSessions())

	holdPool(t, led, "peer", judgePoolName("qwen"))
	done := lc.judgeRun(judgeRequest(prdID), "prefer-free", "qwen")
	awaitQueued(t, led, autonomouspkg.JudgeHolder("u1"))
	// The routing probe waits a short window (judgeAdmitters.try, 150ms) and then
	// gives up. Only after that is the judge queued for the slot itself, so wait
	// past the window before checking that it is still waiting.
	time.Sleep(3 * judgeProbeWindow)
	if !judgeQueued(led, autonomouspkg.JudgeHolder("u1")) {
		t.Fatal("judge is no longer queued for the slot after the probe window: it did not wait for the slot")
	}

	// While the pool is full the judge waits: no session has started and the
	// slot is still the peer's alone.
	if n := lc.sessions.spawned("qwen"); n != 0 {
		t.Fatalf("judge session started %d times while the pool was full, want 0", n)
	}
	if got := judgeHolders(t, led, judgePoolName("qwen")); !reflect.DeepEqual(got, []string{"peer"}) {
		t.Fatalf("pool holders while waiting = %v, want the peer only", got)
	}

	led.Release("peer")
	out := awaitJudge(t, done)
	if out.err != nil || out.v.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want the judge's pass", out.v, out.err)
	}
	if !reflect.DeepEqual(lc.entered, []string{"qwen"}) {
		t.Fatalf("judge ran on %v, want [qwen]", lc.entered)
	}
	// The limit starts when the session starts, so the wait for the slot used
	// none of it: most of the limit was still left when the judge began.
	if lc.budgetAtRun <= judgeLifecycleLimit/2 {
		t.Fatalf("%s of the %s limit was left when the judge started: the wait for the slot was counted", lc.budgetAtRun, judgeLifecycleLimit)
	}
	if len(lc.timeouts) != 0 || len(lc.alerts) != 0 {
		t.Fatalf("timeouts = %+v, alerts = %+v; a judge that ran in time raises neither", lc.timeouts, lc.alerts)
	}
	if n := lc.sessions.spawned("qwen"); n != 1 {
		t.Fatalf("judge sessions started = %d, want 1", n)
	}
	assertJudgePoolsFree(t, led, judgePoolName("qwen"))
	if decisions := judgePolicyDecisions(t, mgr, prdID); len(decisions) != 1 || decisions[0].Backend != "qwen" {
		t.Fatalf("judge decisions = %+v, want the qwen pick", decisions)
	}
}

func TestJudgeLifecycleFollowsPolicyFallbackAndRecordsDecision(t *testing.T) {
	led := newJudgeLedger("claude-code", "qwen")
	mgr, prdID := openJudgeManager(t, t.TempDir())
	lc := newJudgeLifecycle(t, led, mgr, newJudgeFakeSessions())

	// The preferred backend's pool is full, so the judge takes the next backend
	// in the policy list instead of waiting on the preferred one.
	holdPool(t, led, "peer", judgePoolName("claude-code"))
	out := awaitJudge(t, lc.judgeRun(judgeRequest(prdID), "prefer-free", "claude-code", "qwen"))
	if out.err != nil || out.v.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want the fallback backend's pass", out.v, out.err)
	}
	if !reflect.DeepEqual(lc.entered, []string{"qwen"}) {
		t.Fatalf("judge ran on %v, want the fallback [qwen]", lc.entered)
	}
	if n := lc.sessions.spawned("claude-code"); n != 0 {
		t.Fatalf("judge session started on the full backend %d times, want 0", n)
	}
	if got := judgeHolders(t, led, judgePoolName("claude-code")); !reflect.DeepEqual(got, []string{"peer"}) {
		t.Fatalf("full pool holders = %v, want the peer only", got)
	}

	decisions := judgePolicyDecisions(t, mgr, prdID)
	if len(decisions) != 1 {
		t.Fatalf("judge decisions = %+v, want exactly one pick", decisions)
	}
	if d := decisions[0]; d.Backend != "qwen" || d.Actor != "autonomous" || !strings.Contains(d.Note, "role=judge") || !strings.Contains(d.Note, "backend=qwen") {
		t.Fatalf("decision = %+v, want the qwen pick recorded with the judge role", d)
	}
	// The peer still holds the claude-code slot; the judge's lease on qwen is gone.
	assertJudgePoolsFree(t, led, judgePoolName("qwen"))
}

func TestJudgeLifecycleTimeoutRetriesNextBackendWithWarning(t *testing.T) {
	led := newJudgeLedger("claude-code", "qwen")
	sessions := newJudgeFakeSessions()
	mgr, prdID := openJudgeManager(t, t.TempDir())
	lc := newJudgeLifecycle(t, led, mgr, sessions)
	lc.hang["claude-code"] = true

	req := judgeRequest(prdID)
	out := awaitJudge(t, lc.judgeRun(req, "prefer-free", "claude-code", "qwen"))
	if out.err != nil || out.v.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want the second backend's pass", out.v, out.err)
	}
	if !reflect.DeepEqual(lc.entered, []string{"claude-code", "qwen"}) {
		t.Fatalf("judge sessions ran on %v, want one per backend in order", lc.entered)
	}

	// The timed-out session was killed on its limit, and the time was read from
	// the fake clock: exactly the limit.
	if len(lc.timeouts) != 1 || lc.timeouts[0].Backend != "claude-code" || lc.timeouts[0].Elapsed != judgeLifecycleLimit {
		t.Fatalf("timeouts = %+v, want one claude-code timeout after the limit", lc.timeouts)
	}
	if sessions.spawned("claude-code") != 1 || sessions.killed("claude-code") != 1 {
		t.Fatalf("claude-code sessions spawned %d killed %d, want 1 and 1", sessions.spawned("claude-code"), sessions.killed("claude-code"))
	}
	if sessions.spawned("qwen") != 1 || sessions.killed("qwen") != 1 || sessions.running() != 0 {
		t.Fatalf("qwen spawned %d killed %d, running %d; want 1, 1 and none left", sessions.spawned("qwen"), sessions.killed("qwen"), sessions.running())
	}

	if len(lc.alerts) != 1 || lc.alerts[0].level != "warn" {
		t.Fatalf("alerts = %+v, want exactly one warning", lc.alerts)
	}
	warn := lc.alerts[0]
	if !strings.Contains(warn.title, prdID) || !strings.Contains(warn.body, "claude-code") || !strings.Contains(warn.body, "retrying on backend qwen") {
		t.Fatalf("warning = %+v, want the PRD, the timed-out backend and the next one", warn)
	}

	decisions := judgePolicyDecisions(t, mgr, prdID)
	if len(decisions) != 2 || decisions[0].Backend != "claude-code" || decisions[1].Backend != "qwen" {
		t.Fatalf("judge decisions = %+v, want a claude-code pick then a qwen pick", decisions)
	}
	if left := judgeTryLogFor(mgr, req).Tried(); len(left) != 0 {
		t.Fatalf("try log holds %+v after the judge returned, want it cleared", left)
	}
	assertJudgePoolsFree(t, led, judgePoolName("claude-code"), judgePoolName("qwen"))
}

func TestJudgeLifecycleEveryBackendTimedOutHoldsForReviewWithErrorAlert(t *testing.T) {
	led := newJudgeLedger("claude-code", "qwen")
	sessions := newJudgeFakeSessions()
	mgr, prdID := openJudgeManager(t, t.TempDir())
	lc := newJudgeLifecycle(t, led, mgr, sessions)
	lc.hang["claude-code"] = true
	lc.hang["qwen"] = true

	req := judgeRequest(prdID)
	out := awaitJudge(t, lc.judgeRun(req, "prefer-free", "claude-code", "qwen"))
	if out.err != nil {
		t.Fatalf("err = %v, want the hold as a verdict", out.err)
	}
	v := out.v
	if v.Outcome != "block" || !autonomouspkg.IsJudgeTimeoutHold(v) {
		t.Fatalf("verdict = %+v, want a judge-timeout hold that blocks: nothing passed silently", v)
	}
	if !strings.Contains(v.Summary, "needs review") || !strings.Contains(v.Summary, prdID) {
		t.Errorf("hold summary %q should say the Automaton needs review", v.Summary)
	}
	if len(v.JudgeAttempts) != 2 || v.JudgeAttempts[0].Backend != "claude-code" || v.JudgeAttempts[1].Backend != "qwen" {
		t.Fatalf("attempt summary = %+v, want one record per backend in order", v.JudgeAttempts)
	}
	for _, a := range v.JudgeAttempts {
		if a.Outcome != autonomouspkg.JudgeOutcomeTimedOut || a.Started.IsZero() {
			t.Errorf("attempt %+v, want a timed-out attempt with its start time", a)
		}
	}
	if len(v.Issues) != 2 {
		t.Errorf("issues = %v, want one line per attempt", v.Issues)
	}

	// Two warnings, one per timeout, then the error when the PRD is held.
	if len(lc.alerts) != 3 || lc.alerts[0].level != "warn" || lc.alerts[1].level != "warn" || lc.alerts[2].level != "error" {
		t.Fatalf("alerts = %+v, want warn, warn, error", lc.alerts)
	}
	if !strings.Contains(lc.alerts[1].body, "no backend is left") {
		t.Errorf("second warning %q should say no backend is left", lc.alerts[1].body)
	}
	hold := lc.alerts[2]
	for _, want := range []string{prdID, "claude-code, qwen", "in total", "Nothing was approved or passed"} {
		if !strings.Contains(hold.body, want) {
			t.Errorf("hold alert body %q lacks %q", hold.body, want)
		}
	}
	if !strings.Contains(hold.title, prdID) {
		t.Errorf("hold alert title %q lacks the PRD id", hold.title)
	}

	if !reflect.DeepEqual(lc.entered, []string{"claude-code", "qwen"}) {
		t.Fatalf("judge sessions ran on %v, want one per backend", lc.entered)
	}
	for _, b := range []string{"claude-code", "qwen"} {
		if sessions.spawned(b) != 1 || sessions.killed(b) != 1 {
			t.Errorf("%s sessions spawned %d killed %d, want 1 and 1", b, sessions.spawned(b), sessions.killed(b))
		}
	}
	if sessions.running() != 0 {
		t.Errorf("%d judge sessions still running after the hold", sessions.running())
	}
	if left := judgeTryLogFor(mgr, req).Tried(); len(left) != 0 {
		t.Errorf("try log holds %+v after the hold, want it cleared", left)
	}
	assertJudgePoolsFree(t, led, judgePoolName("claude-code"), judgePoolName("qwen"))
}

func TestJudgeLifecycleRestartMidJudgeKeepsOneLeaseOneSessionAndOneAttemptPerBackend(t *testing.T) {
	dir := t.TempDir()
	sessions := newJudgeFakeSessions()

	// Daemon one: claude-code times out, then the judge is on qwen when the
	// daemon dies. The qwen session is live, and the lease is held.
	mgr1, prdID := openJudgeManager(t, dir)
	req := judgeRequest(prdID)
	led1 := newJudgeLedger("claude-code", "qwen")
	lc1 := newJudgeLifecycle(t, led1, mgr1, sessions)
	lc1.hang["claude-code"] = true
	lc1.hang["qwen"] = true
	lc1.crash = make(chan struct{})
	lc1.onEnter = func(backend string) {
		if backend == "qwen" {
			close(lc1.crash)
		}
	}
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_, _ = lc1.run(context.Background(), req, "prefer-free", "claude-code", "qwen")
	}()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("daemon one did not stop at the restart point")
	}

	if !reflect.DeepEqual(lc1.entered, []string{"claude-code", "qwen"}) {
		t.Fatalf("daemon one ran judge sessions on %v, want claude-code then qwen", lc1.entered)
	}
	if tried := judgeTryLogFor(mgr1, req).Tried(); len(tried) != 1 || tried[0].Backend != "claude-code" {
		t.Fatalf("try log before the restart = %+v, want the claude-code timeout", tried)
	}
	if sessions.running() != 1 || sessions.spawned("qwen") != 1 {
		t.Fatalf("before the restart: running %d, qwen started %d; want the one live qwen session",
			sessions.running(), sessions.spawned("qwen"))
	}
	assertJudgePoolsFree(t, led1, judgePoolName("claude-code"), judgePoolName("qwen"))

	// Daemon two: a new ledger and a new manager on the same store. The live
	// judge session survives, and the timed-out claude-code attempt is on record.
	mgr2 := reopenJudgeManager(t, dir)
	led2 := newJudgeLedger("claude-code", "qwen")
	lc2 := newJudgeLifecycle(t, led2, mgr2, sessions)
	if tried := judgeTryLogFor(mgr2, req).Tried(); len(tried) != 1 || tried[0].Backend != "claude-code" {
		t.Fatalf("try log after the restart = %+v, want the claude-code timeout kept", tried)
	}

	var leaseAtStart int
	lc2.onEnter = func(backend string) {
		leaseAtStart = judgePoolStatus(t, led2, judgePoolName(backend)).Held
	}
	out := awaitJudge(t, lc2.judgeRun(req, "prefer-free", "claude-code", "qwen"))
	if out.err != nil || out.v.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want the adopted judge's pass", out.v, out.err)
	}

	// The judge ran on qwen only: claude-code is not tried again.
	if !reflect.DeepEqual(lc2.entered, []string{"qwen"}) {
		t.Fatalf("daemon two ran judge sessions on %v, want [qwen] only", lc2.entered)
	}
	// Exactly one lease: the restarted judge's own, on its pool.
	if leaseAtStart != 1 {
		t.Fatalf("qwen pool held %d leases while the judge ran, want 1", leaseAtStart)
	}
	// The live session was adopted, not started again.
	if sessions.spawned("qwen") != 1 || sessions.killed("qwen") != 1 {
		t.Fatalf("qwen sessions spawned %d killed %d, want 1 and 1: the restart must not start a second judge",
			sessions.spawned("qwen"), sessions.killed("qwen"))
	}
	if sessions.spawned("claude-code") != 1 || sessions.killed("claude-code") != 1 {
		t.Fatalf("claude-code sessions spawned %d killed %d, want 1 and 1: the timed-out attempt is not repeated",
			sessions.spawned("claude-code"), sessions.killed("claude-code"))
	}
	if sessions.running() != 0 {
		t.Errorf("%d judge sessions still running after the judge returned", sessions.running())
	}
	if len(lc2.alerts) != 0 {
		t.Errorf("alerts after the restart = %+v, a restored timeout must not warn again", lc2.alerts)
	}
	assertJudgePoolsFree(t, led2, judgePoolName("claude-code"), judgePoolName("qwen"))
	if left := judgeTryLogFor(mgr2, req).Tried(); len(left) != 0 {
		t.Errorf("try log holds %+v after the judge returned, want it cleared", left)
	}
}
