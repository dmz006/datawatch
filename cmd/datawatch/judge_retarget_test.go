package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// judgeRetargetHarness is a production-shaped judge fixture over a real
// capacity ledger: the judge admission vars are set by wireJudgeAdmission, the
// function wireCapacity calls. llm:pi-prompt and llm:claude-code hold two slots
// each; pi-prompt also draws on node:datawatch.
type judgeRetargetHarness struct {
	t      *testing.T
	led    *capacity.Ledger
	mgr    *autonomouspkg.Manager
	cfg    autonomouspkg.Config
	req    autonomouspkg.GuardrailInvocation
	holder string
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // background judges, joined before the package vars are restored

	mu         sync.Mutex
	admits     map[string]int // calls to the lease admission, by backend
	ran        []string       // backends the judge body ran on
	attempts   []judgeAttempt // every attempt as run returned it
	heldInBody []bool         // whether the judge held a lease when its body ran
}

type judgeRetargetResult struct {
	verdict  autonomouspkg.GuardrailVerdict
	err      error
	panicked any
	alerts   []alertRecord
}

var judgeRetargetPools = map[string][]string{
	"pi-prompt":   {"llm:pi-prompt", "node:datawatch"},
	"claude-code": {"llm:claude-code"},
}

var judgeRetargetLimits = map[string]int{"llm:pi-prompt": 2, "llm:claude-code": 2, "node:datawatch": 2}

func newJudgeRetargetHarness(t *testing.T, policy config.VerifierPolicyConfig) *judgeRetargetHarness {
	t.Helper()
	oldTick, oldWindow := retargetTick, judgeClaudeWindow
	oldAdmit, oldTry, oldFits, oldChanged, oldLoad := judgeCapacityAdmit, judgeCapacityTryAdmit, judgeCapacityFits, judgeCapacityChanged, verifierBackendLoad
	t.Cleanup(func() {
		retargetTick, judgeClaudeWindow = oldTick, oldWindow
		judgeCapacityAdmit, judgeCapacityTryAdmit, judgeCapacityFits, judgeCapacityChanged, verifierBackendLoad = oldAdmit, oldTry, oldFits, oldChanged, oldLoad
	})
	retargetTick = 20 * time.Millisecond
	judgeClaudeWindow = autonomouspkg.NewVerifierPolicyWindow(nil)
	verifierBackendLoad = nil

	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for p, n := range judgeRetargetLimits {
		led.SetLimit(p, n)
	}
	mgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetCapacity(led, func(backend, _ string) ([]string, string) { return judgeRetargetPools[backend], "" })

	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomouspkg.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomouspkg.Story{{Title: "S", Tasks: []autonomouspkg.Task{{Title: "T", Spec: "spec"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	cur, _ := mgr.Store().GetPRD(prd.ID)
	unitID := cur.Story[0].Tasks[0].ID

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	h := &judgeRetargetHarness{
		t: t, led: led, mgr: mgr, ctx: ctx, cancel: cancel, holder: autonomouspkg.JudgeHolder(unitID),
		req: autonomouspkg.GuardrailInvocation{PRDID: prd.ID, Level: "task", UnitID: unitID, Guardrail: "g"},
	}
	t.Cleanup(func() {
		cancel()
		h.wg.Wait()
	})
	h.cfg = autonomouspkg.DefaultConfig()
	h.cfg.VerifierPolicy = policy

	wireJudgeAdmission(mgr, newVerifierRegistry())
	return h
}

func judgeRetargetPreferFree() config.VerifierPolicyConfig {
	return config.VerifierPolicyConfig{Mode: config.VerifierPolicyPreferFree, Backends: []string{"pi-prompt", "claude-code"}}
}

// hold takes a long-running lease for holder on pools.
func (h *judgeRetargetHarness) hold(holder string, pools ...string) {
	h.t.Helper()
	req := capacity.Request{Holder: holder, Pools: pools}
	if err := h.led.Acquire(h.ctx, req, time.Second, func() bool { return false }, func(string) {}); err != nil {
		h.t.Fatalf("hold %s on %v: %v", holder, pools, err)
	}
}

// fill takes the fixture's four long-running leases: two on each backend.
func (h *judgeRetargetHarness) fill() {
	h.hold("p1", judgeRetargetPools["pi-prompt"]...)
	h.hold("p2", judgeRetargetPools["pi-prompt"]...)
	h.hold("c1", judgeRetargetPools["claude-code"]...)
	h.hold("c2", judgeRetargetPools["claude-code"]...)
}

// attempt is one judge attempt as the guardrail closure runs it: the lease,
// then body. block, when set, holds the body open until it is closed.
func (h *judgeRetargetHarness) attempt(admit judgeLeaseAdmit, body func(backend string) (autonomouspkg.GuardrailVerdict, error)) judgeAttemptFn {
	counted := func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
		h.mu.Lock()
		if h.admits == nil {
			h.admits = map[string]int{}
		}
		h.admits[backend]++
		h.mu.Unlock()
		return admit(ctx, backend, model, unitID, prdID)
	}
	return func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
		v, err := runJudgeLeased(h.ctx, counted, att, "", h.req.UnitID, h.req.PRDID,
			func(string) (autonomouspkg.GuardrailVerdict, error) {
				h.mu.Lock()
				h.ran = append(h.ran, backend)
				h.heldInBody = append(h.heldInBody, h.led.Held(h.holder))
				h.mu.Unlock()
				return body(backend)
			})
		h.mu.Lock()
		h.attempts = append(h.attempts, *att)
		h.mu.Unlock()
		return v, err
	}
}

func judgeRetargetPass(string) (autonomouspkg.GuardrailVerdict, error) {
	return autonomouspkg.GuardrailVerdict{Outcome: "pass", VerdictAt: time.Now()}, nil
}

// start runs the judge the way main.go does: the route for the request, then
// runJudgeAttempts with the route's retarget function and decision-log writer.
func (h *judgeRetargetHarness) start(req autonomouspkg.GuardrailInvocation, run judgeAttemptFn) <-chan judgeRetargetResult {
	route := judgeRouteForSize(h.ctx, h.mgr, req, h.cfg, "pi-prompt", judgeSize{})
	ch := make(chan judgeRetargetResult, 1)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		var res judgeRetargetResult
		defer func() {
			res.panicked = recover()
			ch <- res
		}()
		res.verdict, res.err = runJudgeAttempts(req, route.Choose, time.Minute, recordAlerts(&res.alerts), nil, route.Retarget, route.Pick, run)
	}()
	return ch
}

func (h *judgeRetargetHarness) waiters() []capacity.Waiter {
	var out []capacity.Waiter
	for _, w := range h.led.Snapshot().Waiting {
		if w.Holder == h.holder {
			out = append(out, w)
		}
	}
	return out
}

func (h *judgeRetargetHarness) queuedOn(pool string) bool {
	for _, w := range h.waiters() {
		for _, p := range w.Pools {
			if p == pool {
				return true
			}
		}
	}
	return false
}

// waitForWaiter blocks until the judge is queued on pool. It first waits for
// the lease admission to start on that pool's backend: before that the judge's
// non-blocking probes queue for a moment on every backend, and a queue place
// seen then is not the wait.
func (h *judgeRetargetHarness) waitForWaiter(pool string) {
	h.t.Helper()
	backend := strings.TrimPrefix(pool, "llm:")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		n := h.admits[backend]
		h.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		if h.queuedOn(pool) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("judge never queued on %s; waiters %+v", pool, h.waiters())
}

// result waits for the judge to finish.
func (h *judgeRetargetHarness) result(ch <-chan judgeRetargetResult) judgeRetargetResult {
	h.t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		h.t.Fatalf("judge did not finish; waiters %+v, ran %v", h.waiters(), h.ranList())
		return judgeRetargetResult{}
	}
}

// staysQueued fails if the judge finishes within d, and returns once d is up.
func (h *judgeRetargetHarness) staysQueued(ch <-chan judgeRetargetResult, d time.Duration) {
	h.t.Helper()
	select {
	case r := <-ch:
		h.t.Fatalf("judge finished while it should have waited: %+v (ran %v)", r, h.ranList())
	case <-time.After(d):
	}
}

func (h *judgeRetargetHarness) ranList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.ran...)
}

// judgeLines returns the notes of the PRD's judge decision-log lines.
func (h *judgeRetargetHarness) judgeLines() []string {
	cur, ok := h.mgr.Store().GetPRD(h.req.PRDID)
	if !ok {
		h.t.Fatalf("PRD %s is gone", h.req.PRDID)
	}
	var out []string
	for _, d := range cur.Decisions {
		if strings.Contains(d.Note, "role=judge") {
			out = append(out, d.Note)
		}
	}
	return out
}

func (h *judgeRetargetHarness) movedLines() []string {
	var out []string
	for _, l := range h.judgeLines() {
		if strings.Contains(l, "moved from") {
			out = append(out, l)
		}
	}
	return out
}

// settled fails unless the judge holds no lease and waits in no queue.
func (h *judgeRetargetHarness) settled() {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !h.led.Held(h.holder) && len(h.waiters()) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("judge lease leaked: held=%v waiters=%+v", h.led.Held(h.holder), h.waiters())
}

func TestJudgeRetargetMovesToFreeBackend(t *testing.T) {
	h := newJudgeRetargetHarness(t, judgeRetargetPreferFree())
	h.fill()
	release := make(chan struct{})
	ch := h.start(h.req, h.attempt(judgeCapacityAdmit, func(string) (autonomouspkg.GuardrailVerdict, error) {
		<-release
		return judgeRetargetPass("")
	}))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")

	// The judge leaves pi-prompt and takes the freed claude-code slot.
	deadline := time.Now().Add(5 * time.Second)
	for len(h.ranList()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := h.ranList(); len(got) != 1 || got[0] != "claude-code" {
		t.Fatalf("judge ran on %v, want [claude-code]", got)
	}
	if h.queuedOn("llm:pi-prompt") {
		t.Fatalf("judge still queued on pi-prompt after the move: %+v", h.waiters())
	}
	if !h.led.Held(h.holder) {
		t.Fatal("moved judge holds no lease while its body runs")
	}
	close(release)
	res := h.result(ch)
	if res.err != nil || res.verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want the moved judge's pass", res.verdict, res.err)
	}
	if len(res.alerts) != 0 {
		t.Fatalf("alerts = %+v; a move raises none", res.alerts)
	}
	h.settled()

	moved := h.movedLines()
	if len(moved) != 1 {
		t.Fatalf("moved lines = %q, want exactly one", moved)
	}
	for _, want := range []string{"role=judge", "backend=claude-code", "moved from pi-prompt:", "prefer-free: claude-code has a free slot"} {
		if !strings.Contains(moved[0], want) {
			t.Fatalf("move line %q lacks %q", moved[0], want)
		}
	}
	if n := len(h.judgeLines()); n != 2 {
		t.Fatalf("judge decision lines = %d, want 2 (first pick, move): %q", n, h.judgeLines())
	}

	// The move is no timeout and no slot shortage; the first attempt carries the target.
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.attempts) != 2 {
		t.Fatalf("attempts = %+v, want the move and the run", h.attempts)
	}
	if a := h.attempts[0]; a.Backend != "pi-prompt" || a.MoveTo != "claude-code" || a.NoSlot || a.TimedOut {
		t.Fatalf("first attempt = %+v, want a plain move to claude-code", a)
	}
	if a := h.attempts[1]; a.Backend != "claude-code" || a.MoveTo != "" || a.NoSlot || a.TimedOut {
		t.Fatalf("second attempt = %+v, want a plain run on claude-code", a)
	}
}

func TestJudgeRetargetCountsOnlyGrantedClaudeLeases(t *testing.T) {
	h := newJudgeRetargetHarness(t, judgeRetargetPreferFree())
	h.fill()
	ch := h.start(h.req, h.attempt(judgeCapacityAdmit, judgeRetargetPass))
	h.waitForWaiter("llm:pi-prompt")
	if n := judgeClaudeWindow.Count(time.Hour); n != 0 {
		t.Fatalf("claude window = %d while the judge only waits", n)
	}
	h.led.Release("c1")
	if res := h.result(ch); res.err != nil {
		t.Fatalf("err = %v", res.err)
	}
	if n := judgeClaudeWindow.Count(time.Hour); n != 1 {
		t.Fatalf("claude window = %d, want 1: the move itself is not a session, the granted lease is", n)
	}
}

func TestJudgeRetargetPinnedJudgeStays(t *testing.T) {
	h := newJudgeRetargetHarness(t, judgeRetargetPreferFree())
	h.fill()
	req := h.req
	req.GuardrailPinned, req.GuardrailBackend = true, "pi-prompt"
	ch := h.start(req, h.attempt(judgeCapacityAdmit, judgeRetargetPass))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	h.staysQueued(ch, 300*time.Millisecond)
	if !h.queuedOn("llm:pi-prompt") {
		t.Fatalf("pinned judge left its queue: %+v", h.waiters())
	}
	h.led.Release("p1")
	res := h.result(ch)
	if res.err != nil || res.verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v", res.verdict, res.err)
	}
	if got := h.ranList(); len(got) != 1 || got[0] != "pi-prompt" {
		t.Fatalf("pinned judge ran on %v, want [pi-prompt]", got)
	}
	if moved := h.movedLines(); len(moved) != 0 {
		t.Fatalf("pinned judge logged a move: %q", moved)
	}
	h.settled()
}

func TestJudgeRetargetNoFlapping(t *testing.T) {
	h := newJudgeRetargetHarness(t, judgeRetargetPreferFree())
	h.fill()
	// Another holder takes the freed claude-code slot just as the judge moves
	// there, so the judge ends up waiting on claude-code with pi-prompt left.
	var once sync.Once
	admit := func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
		if backend == "claude-code" {
			once.Do(func() { h.hold("steal", judgeRetargetPools["claude-code"]...) })
		}
		return judgeCapacityAdmit(ctx, backend, model, unitID, prdID)
	}
	ch := h.start(h.req, h.attempt(admit, judgeRetargetPass))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	h.waitForWaiter("llm:claude-code")

	// Two release/fill cycles on pi-prompt: the judge must not go back.
	for i, holder := range []string{"p1", "p2"} {
		h.led.Release(holder)
		h.staysQueued(ch, 250*time.Millisecond)
		if !h.queuedOn("llm:claude-code") || h.queuedOn("llm:pi-prompt") {
			t.Fatalf("cycle %d: judge flapped; waiters %+v", i, h.waiters())
		}
		h.hold(holder+"-again", judgeRetargetPools["pi-prompt"]...)
	}
	if got := h.ranList(); len(got) != 0 {
		t.Fatalf("judge ran on %v while it waited", got)
	}

	h.led.Release("steal")
	res := h.result(ch)
	if res.err != nil || res.verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v", res.verdict, res.err)
	}
	if got := h.ranList(); len(got) != 1 || got[0] != "claude-code" {
		t.Fatalf("judge ran on %v, want [claude-code]", got)
	}
	if moved := h.movedLines(); len(moved) != 1 {
		t.Fatalf("moved lines = %q, want exactly one", moved)
	}
	h.settled()
}

func TestJudgeRetargetFixedModeDoesNotMove(t *testing.T) {
	policy := config.VerifierPolicyConfig{Mode: config.VerifierPolicyFixed, Backends: []string{"pi-prompt", "claude-code"}}
	h := newJudgeRetargetHarness(t, policy)
	h.fill()
	ch := h.start(h.req, h.attempt(judgeCapacityAdmit, judgeRetargetPass))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	h.staysQueued(ch, 300*time.Millisecond)
	if !h.queuedOn("llm:pi-prompt") {
		t.Fatalf("fixed-mode judge left its queue: %+v", h.waiters())
	}
	h.led.Release("p1")
	res := h.result(ch)
	if res.err != nil || res.verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v", res.verdict, res.err)
	}
	if got := h.ranList(); len(got) != 1 || got[0] != "pi-prompt" {
		t.Fatalf("fixed judge ran on %v, want [pi-prompt]", got)
	}
	if moved := h.movedLines(); len(moved) != 0 {
		t.Fatalf("fixed judge logged a move: %q", moved)
	}
}

func TestJudgeRetargetUnitBackendFallbackDoesNotMove(t *testing.T) {
	// The only listed backend is claude-code and its session cap is used up, so
	// the first pick falls back to the unit backend (pi-prompt). A fallback pick
	// is not a policy pick: it never moves.
	policy := config.VerifierPolicyConfig{
		Mode: config.VerifierPolicyPreferFree, Backends: []string{"claude-code"}, ClaudeSessionCap: 1,
	}
	h := newJudgeRetargetHarness(t, policy)
	// The session window runs on a fake clock so the cap can clear while the
	// judge waits: the policy then names claude-code, and the judge must still
	// stay on its fallback pick.
	var clockMu sync.Mutex
	clock := time.Now()
	judgeClaudeWindow = autonomouspkg.NewVerifierPolicyWindow(func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	})
	judgeClaudeWindow.Record()
	h.fill()
	ch := h.start(h.req, h.attempt(judgeCapacityAdmit, judgeRetargetPass))
	h.waitForWaiter("llm:pi-prompt")
	lines := h.judgeLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "using the unit backend pi-prompt") {
		t.Fatalf("first pick = %q, want the unit-backend fallback", lines)
	}
	clockMu.Lock()
	clock = clock.Add(48 * time.Hour)
	clockMu.Unlock()
	if n := judgeClaudeWindow.Count(time.Duration(config.NormalizeVerifierPolicyClaudeCapWindow(0)) * time.Minute); n != 0 {
		t.Fatalf("claude window = %d after the clock moved; the cap should have cleared", n)
	}
	h.led.Release("c1")
	h.staysQueued(ch, 300*time.Millisecond)
	if !h.queuedOn("llm:pi-prompt") {
		t.Fatalf("fallback judge left its queue: %+v", h.waiters())
	}
	h.led.Release("p1")
	res := h.result(ch)
	if res.err != nil || res.verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v", res.verdict, res.err)
	}
	if got := h.ranList(); len(got) != 1 || got[0] != "pi-prompt" {
		t.Fatalf("fallback judge ran on %v, want [pi-prompt]", got)
	}
	if moved := h.movedLines(); len(moved) != 0 {
		t.Fatalf("fallback judge logged a move: %q", moved)
	}
}

func TestJudgeRetargetReleasesTheMovedLeaseOnEveryPath(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		body func(string) (autonomouspkg.GuardrailVerdict, error)
		// cancelWhileWaiting cancels the judge while it waits on the move target.
		cancelWhileWaiting bool
		check              func(t *testing.T, res judgeRetargetResult)
	}{
		{name: "success", body: judgeRetargetPass, check: func(t *testing.T, res judgeRetargetResult) {
			if res.err != nil || res.verdict.Outcome != "pass" {
				t.Fatalf("verdict = %+v, err = %v", res.verdict, res.err)
			}
		}},
		{name: "error", body: func(string) (autonomouspkg.GuardrailVerdict, error) {
			return autonomouspkg.GuardrailVerdict{}, boom
		}, check: func(t *testing.T, res judgeRetargetResult) {
			if !errors.Is(res.err, boom) {
				t.Fatalf("err = %v, want boom", res.err)
			}
		}},
		{name: "panic", body: func(string) (autonomouspkg.GuardrailVerdict, error) {
			panic("judge body panicked")
		}, check: func(t *testing.T, res judgeRetargetResult) {
			if res.panicked == nil {
				t.Fatal("the panic was swallowed")
			}
		}},
		{name: "cancelled while waiting on the target", cancelWhileWaiting: true, check: func(t *testing.T, res judgeRetargetResult) {
			if !errors.Is(res.err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", res.err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newJudgeRetargetHarness(t, judgeRetargetPreferFree())
			h.fill()
			admit := judgeCapacityAdmit
			if c.cancelWhileWaiting {
				var once sync.Once
				admit = func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
					if backend == "claude-code" {
						once.Do(func() { h.hold("steal", judgeRetargetPools["claude-code"]...) })
					}
					return judgeCapacityAdmit(ctx, backend, model, unitID, prdID)
				}
			}
			ch := h.start(h.req, h.attempt(admit, c.body))
			h.waitForWaiter("llm:pi-prompt")
			h.led.Release("c1")
			if c.cancelWhileWaiting {
				h.waitForWaiter("llm:claude-code")
				if h.queuedOn("llm:pi-prompt") || h.led.Held(h.holder) {
					t.Fatalf("judge holds a place on the backend it left: held=%v waiters=%+v", h.led.Held(h.holder), h.waiters())
				}
				h.cancel()
			}
			res := h.result(ch)
			c.check(t, res)
			h.settled()
			snap := h.led.Snapshot()
			for _, l := range snap.Leases {
				if l.Holder == h.holder {
					t.Fatalf("lease %+v outlived the judge", l)
				}
			}
		})
	}
}

func TestJudgeRetargetDoesNotWriteTheLogOrHoldALease(t *testing.T) {
	h := newJudgeRetargetHarness(t, judgeRetargetPreferFree())
	h.fill()
	h.led.Release("c1")
	route := judgeRouteForSize(h.ctx, h.mgr, h.req, h.cfg, "pi-prompt", judgeSize{})
	before := len(h.judgeLines())
	backend, _, reason, ok := route.Retarget("pi-prompt", nil)
	if !ok || backend != "claude-code" || !strings.Contains(reason, "claude-code has a free slot") {
		t.Fatalf("retarget = %q %q %v, want claude-code with a free slot", backend, reason, ok)
	}
	if got := len(h.judgeLines()); got != before {
		t.Fatalf("retarget wrote %d decision lines", got-before)
	}
	if h.led.Held(h.holder) || len(h.waiters()) != 0 {
		t.Fatalf("retarget touched the ledger: held=%v waiters=%+v", h.led.Held(h.holder), h.waiters())
	}
	// A backend that was left or tried is never named again.
	if b, _, _, ok := route.Retarget("pi-prompt", []string{"claude-code"}); ok {
		t.Fatalf("retarget named %s, which the judge already left", b)
	}
	// The waiting backend is never named either.
	if b, _, _, ok := route.Retarget("claude-code", nil); ok {
		t.Fatalf("retarget named %s while the judge waits on it, with pi-prompt full", b)
	}
}

func TestJudgeRetargetBalanceMovesToLowestLoad(t *testing.T) {
	oldLoad := verifierBackendLoad
	t.Cleanup(func() { verifierBackendLoad = oldLoad })
	loads := map[string]float64{"a": 1, "b": 0.5, "c": 0.25}
	verifierBackendLoad = func(backend, _ string) float64 { return loads[backend] }
	cfg := judgeRouteConfig("balance", "a", "b", "c")
	fits := func(backend, _ string) bool { return backend != "c" }
	route := judgeVerifierRouteSized(nil, cfg, "a", nil, fits, nopPick, judgeSize{})

	backend, _, reason, ok := route.Retarget("a", nil)
	if !ok || backend != "b" || !strings.Contains(reason, "balance: b has the lowest load") {
		t.Fatalf("retarget = %q %q %v, want b: the lowest load among the backends that fit", backend, reason, ok)
	}
	if b, _, _, ok := route.Retarget("a", []string{"b"}); ok {
		t.Fatalf("retarget named %s, want none: b was left and c does not fit", b)
	}
}

func TestJudgeRetargetNilFitsMeansEverythingFits(t *testing.T) {
	route := judgeVerifierRouteSized(nil, judgeRouteConfig("prefer-free", "a", "b"), "a", nil, nil, nopPick, judgeSize{})
	if b, _, _, ok := route.Retarget("a", nil); !ok || b != "b" {
		t.Fatalf("retarget = %q %v, want b", b, ok)
	}
}

func TestJudgeRetargetAttemptLoopMovesWithoutAskingChoose(t *testing.T) {
	// a's wait moves to b; b has no slot within its wait; the chooser is asked
	// for the next backend only then. The move counts as neither a timeout nor a
	// missing slot, and the chooser sees only b as tried.
	var chooseTried [][]string
	choose := func(tried []string) (string, bool) {
		chooseTried = append(chooseTried, append([]string(nil), tried...))
		return judgeChooserFromList([]string{"a", "b"})(tried)
	}
	var picks []string
	pick := func(backend, model, reason string) { picks = append(picks, backend+"|"+model+"|"+reason) }
	var seenExcluded [][]string
	retarget := func(waiting string, left []string) (string, string, string, bool) {
		seenExcluded = append(seenExcluded, append([]string(nil), left...))
		return "b", "m", "b has room", true
	}
	var ran []string
	var alerts []alertRecord
	verdict, err := runJudgeAttempts(autonomouspkg.GuardrailInvocation{PRDID: "p", UnitID: "u"}, choose, time.Minute,
		recordAlerts(&alerts), nil, retarget, pick,
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			ran = append(ran, backend)
			switch len(ran) {
			case 1:
				if _, _, _, ok := att.Retarget("a", nil); !ok {
					t.Fatal("attempt has no working Retarget")
				}
				att.MoveTo, att.MoveModel, att.MoveReason = "b", "m", "b has room"
			case 2:
				att.NoSlot = true
			default:
				return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
			}
			return autonomouspkg.GuardrailVerdict{Category: "infra"}, nil
		})
	if err != nil || verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v", verdict, err)
	}
	if got := strings.Join(ran, ","); got != "a,b,a" {
		t.Fatalf("ran = %s, want a,b,a", got)
	}
	if len(chooseTried) != 2 || len(chooseTried[0]) != 0 || strings.Join(chooseTried[1], ",") != "b" {
		t.Fatalf("choose saw tried %v, want [] then [b]: the move is not a tried backend and does not call choose", chooseTried)
	}
	if len(picks) != 1 || picks[0] != "b|m|moved from a: b has room" {
		t.Fatalf("picks = %q, want one move line", picks)
	}
	if len(alerts) != 0 {
		t.Fatalf("alerts = %+v, want none", alerts)
	}
	if len(seenExcluded) != 1 || len(seenExcluded[0]) != 0 {
		t.Fatalf("first attempt's retarget excluded %v, want nothing", seenExcluded)
	}
}

func TestJudgeRetargetNilRetargetLeavesAttemptsPlain(t *testing.T) {
	var got judgeRetargetFn
	_, _ = runJudgeAttempts(autonomouspkg.GuardrailInvocation{PRDID: "p", UnitID: "u"}, judgeChooserFromList([]string{"a"}), time.Minute,
		recordAlerts(new([]alertRecord)), nil, nil, nil,
		func(_ string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			got = att.Retarget
			return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
		})
	if got != nil {
		t.Fatal("a nil retarget gave the attempt a Retarget")
	}
}
