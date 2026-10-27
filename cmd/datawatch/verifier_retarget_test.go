package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// retargetHarness is a production-shaped verifier fixture over a real capacity
// ledger: the verifier admission vars are wired to the ledger exactly as
// wireCapacity does, minus the registry bookkeeping.
type retargetHarness struct {
	t      *testing.T
	led    *capacity.Ledger
	mgr    *autonomouspkg.Manager
	cfg    autonomouspkg.Config
	prd    *autonomouspkg.PRD
	taskID string
	holder string
	ctx    context.Context
	wg     sync.WaitGroup // background waits, joined before the package vars are restored
}

type retargetLeaseResult struct {
	lease verifierLease
	err   error
}

// retargetPools is the production fixture's pool map: pi-prompt draws on its
// own llm pool and the node pool, claude-code on its llm pool alone.
var retargetPools = map[string][]string{
	"pi-prompt":   {"llm:pi-prompt", "node:datawatch"},
	"claude-code": {"llm:claude-code"},
}

var retargetLimits = map[string]int{"llm:pi-prompt": 2, "llm:claude-code": 2, "node:datawatch": 2}

func newRetargetHarness(t *testing.T, policy config.VerifierPolicyConfig, pools map[string][]string, limits map[string]int) *retargetHarness {
	t.Helper()
	oldTick, oldWindow := retargetTick, verifierClaudeWindow
	oldAdmit, oldTry, oldFits, oldChanged, oldLoad := verifierCapacityAdmit, verifierCapacityTryAdmit, verifierCapacityFits, verifierCapacityChanged, verifierBackendLoad
	t.Cleanup(func() {
		retargetTick, verifierClaudeWindow = oldTick, oldWindow
		verifierCapacityAdmit, verifierCapacityTryAdmit, verifierCapacityFits, verifierCapacityChanged, verifierBackendLoad = oldAdmit, oldTry, oldFits, oldChanged, oldLoad
	})
	retargetTick = 20 * time.Millisecond
	verifierClaudeWindow = autonomouspkg.NewVerifierPolicyWindow(nil)
	verifierBackendLoad = nil

	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for p, n := range limits {
		led.SetLimit(p, n)
	}
	mgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetCapacity(led, func(backend, _ string) ([]string, string) { return pools[backend], "" })

	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomouspkg.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomouspkg.Story{{Title: "S", Tasks: []autonomouspkg.Task{{Title: "T", Spec: "spec"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	cur, _ := mgr.Store().GetPRD(prd.ID)
	taskID := cur.Story[0].Tasks[0].ID

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	h := &retargetHarness{t: t, led: led, mgr: mgr, prd: cur, taskID: taskID, holder: autonomouspkg.VerifyHolder(taskID), ctx: ctx}
	t.Cleanup(func() {
		cancel()
		h.wg.Wait()
	})
	h.cfg = autonomouspkg.DefaultConfig()
	h.cfg.VerifierPolicy = policy

	judge := func(backend, model string, wait time.Duration) autonomouspkg.JudgeAdmit {
		return autonomouspkg.JudgeAdmit{Holder: h.holder, WorkerHolder: taskID, PRDID: cur.ID, Backend: backend, Model: model, Wait: wait}
	}
	verifierCapacityAdmit = func(ctx context.Context, backend, model, _, _, _ string) (func(), string, error) {
		node, release, err := mgr.AdmitJudge(ctx, judge(backend, model, 30*time.Second))
		return release, node, err
	}
	verifierCapacityTryAdmit = func(ctx context.Context, backend, model, _, _, _ string) (func(), string, bool) {
		node, release, err := mgr.AdmitJudge(ctx, judge(backend, model, 150*time.Millisecond))
		if err != nil {
			return func() {}, "", false
		}
		return release, node, true
	}
	verifierCapacityFits = func(backend, model, _, _ string) bool {
		return mgr.JudgeFits(autonomouspkg.JudgeAdmit{Holder: h.holder, PRDID: cur.ID, Backend: backend, Model: model})
	}
	verifierCapacityChanged = mgr.CapacityChanged
	return h
}

// hold takes a long-running lease for holder on pools.
func (h *retargetHarness) hold(holder string, pools ...string) {
	h.t.Helper()
	req := capacity.Request{Holder: holder, Pools: pools}
	if err := h.led.Acquire(h.ctx, req, time.Second, func() bool { return false }, func(string) {}); err != nil {
		h.t.Fatalf("hold %s on %v: %v", holder, pools, err)
	}
}

// fill takes the production fixture's four long-running leases: two on
// pi-prompt (holders on node datawatch) and two on claude-code.
func (h *retargetHarness) fill() {
	h.hold("p1", retargetPools["pi-prompt"]...)
	h.hold("p2", retargetPools["pi-prompt"]...)
	h.hold("c1", retargetPools["claude-code"]...)
	h.hold("c2", retargetPools["claude-code"]...)
}

func retargetResolvers() (func(string) (string, string, error), func(string) bool, func(string) bool) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"pi-prompt": "pi-prompt", "claude-code": "claude-code", "ollama-b": "ollama"},
		map[string]string{"pi-prompt": "ollama/q", "claude-code": "opus", "ollama-b": "qwen-b"},
	)
	isSession := func(kind string) bool { return kind == "claude-code" || kind == "pi-prompt" }
	return resolve, compatible, isSession
}

// selection runs the policy the way the verify path does and returns the pick.
func (h *retargetHarness) selection() verifierSelection {
	h.t.Helper()
	resolve, compatible, isSession := retargetResolvers()
	cur, _ := h.mgr.Store().GetPRD(h.prd.ID)
	task := &cur.Story[0].Tasks[0]
	tryAdmit := func(backend, model string) (func(), string, bool) {
		return verifierCapacityTryAdmit(h.ctx, backend, model, h.taskID, h.prd.ID, "")
	}
	sel, ok := policyVerifierSelection(h.mgr, cur, task, autonomouspkg.VerificationLLM{Source: autonomouspkg.VerifierSourceGlobal},
		h.cfg, h.t.TempDir(), tryAdmit, resolve, compatible, isSession)
	if !ok {
		h.t.Fatalf("policy did not decide: policy %+v", h.cfg.VerifierPolicy)
	}
	return sel
}

// waitDeps is what main.go passes awaitVerifierLease. pin finishes a move
// target as the pinned-model guard does; nil pins the pi-family session model.
func (h *retargetHarness) waitDeps(pin func(*verifierSelection) *autonomouspkg.VerificationResult) verifierLeaseWait {
	if pin == nil {
		pin = func(s *verifierSelection) *autonomouspkg.VerificationResult {
			if s.UseSession && isPiFamilyKind(s.Kind) && s.Model == "" {
				s.Model = "ollama/q"
			}
			return nil
		}
	}
	return verifierLeaseWait{
		Admit: func(ctx context.Context, b, m string) (func(), string, error) {
			return verifierCapacityAdmit(ctx, b, m, h.taskID, h.prd.ID, "")
		},
		Take: func(b, m string) (func(), string, bool) {
			return verifierCapacityTryAdmit(h.ctx, b, m, h.taskID, h.prd.ID, "")
		},
		Changed: verifierCapacityChanged,
		Pin:     pin,
		Mgr:     h.mgr, PRDID: h.prd.ID, TaskID: h.taskID,
	}
}

// start runs awaitVerifierLease in the background and returns its result
// channel. The selection's model is filled in as main.go does before the wait.
func (h *retargetHarness) start(sel verifierSelection, w verifierLeaseWait) <-chan retargetLeaseResult {
	if sel.UseSession && isPiFamilyKind(sel.Kind) && sel.Model == "" {
		sel.Model = "ollama/q"
	}
	ch := make(chan retargetLeaseResult, 1)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		lease, err := awaitVerifierLease(h.ctx, sel, w)
		ch <- retargetLeaseResult{lease: lease, err: err}
	}()
	return ch
}

// waiters lists the ledger waiters for the verifier's holder.
func (h *retargetHarness) waiters() []capacity.Waiter {
	var out []capacity.Waiter
	for _, w := range h.led.Snapshot().Waiting {
		if w.Holder == h.holder {
			out = append(out, w)
		}
	}
	return out
}

// waitForWaiter blocks until the verifier is queued on pool.
func (h *retargetHarness) waitForWaiter(pool string) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range h.waiters() {
			for _, p := range w.Pools {
				if p == pool {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("verifier never queued on %s; waiters %+v", pool, h.waiters())
}

func (h *retargetHarness) result(ch <-chan retargetLeaseResult) retargetLeaseResult {
	h.t.Helper()
	select {
	case r := <-ch:
		if r.err != nil {
			h.t.Fatalf("awaitVerifierLease: %v", r.err)
		}
		return r
	case <-time.After(5 * time.Second):
		h.t.Fatalf("awaitVerifierLease did not finish; waiters %+v", h.waiters())
		return retargetLeaseResult{}
	}
}

// stays asserts the request is still queued, unmoved, after several ticks.
func (h *retargetHarness) stays(ch <-chan retargetLeaseResult, pool string) {
	h.t.Helper()
	select {
	case r := <-ch:
		h.t.Fatalf("request finished early: %+v (moved=%v backend=%s)", r.err, r.lease.Moved, r.lease.Sel.Backend)
	case <-time.After(10 * retargetTick):
	}
	ws := h.waiters()
	if len(ws) != 1 || !containsString(ws[0].Pools, pool) {
		h.t.Fatalf("waiters = %+v, want exactly one on %s", ws, pool)
	}
}

func (h *retargetHarness) leasePools() []string {
	if ls := h.led.LeaseOf(h.holder); ls != nil {
		return ls.Pools
	}
	return nil
}

func preferFree(backends ...string) config.VerifierPolicyConfig {
	return config.VerifierPolicyConfig{Mode: "prefer-free", Backends: backends}
}

// TestVerifierRetargetMovesWaiterToFreedBackend: a verifier queued on pi-prompt
// while both backends are full is admitted on claude-code once a claude-code
// lease is released, and the move is on record.
func TestVerifierRetargetMovesWaiterToFreedBackend(t *testing.T) {
	h := newRetargetHarness(t, preferFree("pi-prompt", "claude-code"), retargetPools, retargetLimits)
	h.fill()

	sel := h.selection()
	if sel.Backend != "pi-prompt" || sel.Release != nil || sel.Retarget == nil {
		t.Fatalf("pick = backend %q release %v retarget set %v, want a pi-prompt wait that can retarget", sel.Backend, sel.Release != nil, sel.Retarget != nil)
	}
	cur, _ := h.mgr.Store().GetPRD(h.prd.ID)
	if got := cur.Story[0].Tasks[0].VerifierPolicyChoice; got == nil || got.Reason != "prefer-free: all full, waiting on pi-prompt" {
		t.Fatalf("first choice = %+v", got)
	}

	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	r := h.result(ch)

	if !r.lease.Moved || r.lease.Sel.Backend != "claude-code" || r.lease.Release == nil {
		t.Fatalf("lease = moved %v backend %q release %v, want moved to claude-code", r.lease.Moved, r.lease.Sel.Backend, r.lease.Release != nil)
	}
	if !containsString(h.leasePools(), "llm:claude-code") {
		t.Fatalf("lease pools = %v, want llm:claude-code", h.leasePools())
	}
	if ws := h.waiters(); len(ws) != 0 {
		t.Fatalf("pi-prompt waiter left in the ledger: %+v", ws)
	}
	if r.lease.Sel.Source != autonomouspkg.VerifierSourcePolicy || r.lease.Sel.Retarget != nil {
		t.Fatalf("moved selection = %+v, want policy source and no further retarget", r.lease.Sel)
	}

	const reason = "moved from pi-prompt: prefer-free: claude-code has a free slot"
	cur, _ = h.mgr.Store().GetPRD(h.prd.ID)
	choice := cur.Story[0].Tasks[0].VerifierPolicyChoice
	if choice == nil || choice.Backend != "claude-code" || choice.Reason != reason {
		t.Fatalf("task choice = %+v, want claude-code with reason %q", choice, reason)
	}
	last := cur.Decisions[len(cur.Decisions)-1]
	if last.Backend != "claude-code" || !strings.Contains(last.Note, reason) {
		t.Fatalf("last decision = %+v, want it to name the move and its reason", last)
	}
	if n := verifierClaudeWindow.Count(time.Hour); n != 1 {
		t.Fatalf("claude window count = %d, want the move counted once", n)
	}
	r.lease.Release()
	if h.led.LeaseOf(h.holder) != nil {
		t.Fatal("release left the verifier lease behind")
	}
}

// TestVerifierRetargetPinnedWaiterStaysAndIsGrantedThere: an explicit
// per-task verification LLM carries no Retarget, so a freed claude-code slot
// does not move it; it is granted on pi-prompt when that frees.
func TestVerifierRetargetPinnedWaiterStaysAndIsGrantedThere(t *testing.T) {
	h := newRetargetHarness(t, preferFree("pi-prompt", "claude-code"), retargetPools, retargetLimits)
	h.fill()

	resolve, compatible, isSession := retargetResolvers()
	cur, _ := h.mgr.Store().GetPRD(h.prd.ID)
	_, decided := policyVerifierSelection(h.mgr, cur, &cur.Story[0].Tasks[0],
		autonomouspkg.VerificationLLM{Source: autonomouspkg.VerifierSourceTask, Backend: "pi-prompt"},
		h.cfg, h.t.TempDir(), nil, resolve, compatible, isSession)
	if decided {
		t.Fatal("an explicit per-task verification LLM must leave the policy out")
	}
	sel := verifierSelection{Backend: "pi-prompt", Kind: "pi-prompt", Model: "ollama/q", UseSession: true, Source: autonomouspkg.VerifierSourceTask}

	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	h.stays(ch, "llm:pi-prompt")
	if h.led.LeaseOf(h.holder) != nil {
		t.Fatalf("pinned waiter took a lease on %v", h.leasePools())
	}

	h.led.Release("p1")
	r := h.result(ch)
	if r.lease.Moved || r.lease.Sel.Backend != "pi-prompt" || !containsString(h.leasePools(), "llm:pi-prompt") {
		t.Fatalf("lease = moved %v backend %q pools %v, want granted on pi-prompt", r.lease.Moved, r.lease.Sel.Backend, h.leasePools())
	}
	r.lease.Release()
}

// TestVerifierRetargetDoesNotFlapBack: once the waiter moved to claude-code,
// freeing pi-prompt and filling claude-code again (a new holder takes the slot
// another one left) does not send it back: the lease stays, nothing queues, and
// no second move is recorded or counted.
func TestVerifierRetargetDoesNotFlapBack(t *testing.T) {
	h := newRetargetHarness(t, preferFree("pi-prompt", "claude-code"), retargetPools, retargetLimits)
	h.fill()
	sel := h.selection()
	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	r := h.result(ch)
	if !r.lease.Moved {
		t.Fatal("setup: the waiter did not move")
	}
	cur, _ := h.mgr.Store().GetPRD(h.prd.ID)
	decisions := len(cur.Decisions)

	// claude-code churns: c2 leaves and c3 takes its slot, so the pool is full
	// again (c3 and the verifier). pi-prompt empties out.
	h.led.Release("c2")
	h.hold("c3", retargetPools["claude-code"]...)
	h.led.Release("p1")
	h.led.Release("p2")
	time.Sleep(10 * retargetTick)

	if !containsString(h.leasePools(), "llm:claude-code") || containsString(h.leasePools(), "llm:pi-prompt") {
		t.Fatalf("lease pools = %v, want the claude-code lease kept", h.leasePools())
	}
	if ws := h.waiters(); len(ws) != 0 {
		t.Fatalf("waiters = %+v, want none", ws)
	}
	cur, _ = h.mgr.Store().GetPRD(h.prd.ID)
	if len(cur.Decisions) != decisions {
		t.Fatalf("decisions grew from %d to %d after the move", decisions, len(cur.Decisions))
	}
	if choice := cur.Story[0].Tasks[0].VerifierPolicyChoice; choice == nil || choice.Backend != "claude-code" {
		t.Fatalf("task choice = %+v, want it still on claude-code", choice)
	}
	if n := verifierClaudeWindow.Count(time.Hour); n != 1 {
		t.Fatalf("claude window count = %d, want 1", n)
	}

	// The policy itself also refuses to return to a backend the request left.
	if _, _, ok := sel.Retarget("claude-code", []string{"pi-prompt"}); ok {
		t.Fatal("Retarget named a backend in the left list")
	}
	r.lease.Release()
}

// TestVerifierRetargetFixedModeNeverMoves: fixed mode keeps its first backend
// whatever frees up elsewhere.
func TestVerifierRetargetFixedModeNeverMoves(t *testing.T) {
	policy := config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"pi-prompt", "claude-code"}}
	h := newRetargetHarness(t, policy, retargetPools, retargetLimits)
	h.fill()
	sel := h.selection()
	if sel.Backend != "pi-prompt" {
		t.Fatalf("fixed pick = %q, want pi-prompt", sel.Backend)
	}
	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:pi-prompt")
	h.led.Release("c1")
	h.stays(ch, "llm:pi-prompt")

	h.led.Release("p2")
	r := h.result(ch)
	if r.lease.Moved || !containsString(h.leasePools(), "llm:pi-prompt") {
		t.Fatalf("lease = moved %v pools %v, want granted on pi-prompt", r.lease.Moved, h.leasePools())
	}
	r.lease.Release()
}

// TestVerifierRetargetOneEnginePerNode: a candidate whose node pool is at its
// limit of 1 is not a move target although its llm pool has room.
func TestVerifierRetargetOneEnginePerNode(t *testing.T) {
	pools := map[string][]string{
		"pi-prompt": {"llm:pi-prompt", "node:datawatch"},
		"ollama-b":  {"llm:ollama-b", "node:gpu2"},
	}
	limits := map[string]int{"llm:pi-prompt": 2, "node:datawatch": 2, "llm:ollama-b": 2, "node:gpu2": 1}
	h := newRetargetHarness(t, preferFree("pi-prompt", "ollama-b"), pools, limits)
	h.hold("p1", pools["pi-prompt"]...)
	h.hold("p2", pools["pi-prompt"]...)
	h.hold("engine", "node:gpu2")

	sel := h.selection()
	if sel.Backend != "pi-prompt" || sel.Retarget == nil {
		t.Fatalf("pick = %q retarget %v, want a pi-prompt wait", sel.Backend, sel.Retarget != nil)
	}
	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:pi-prompt")
	h.stays(ch, "llm:pi-prompt")
	if h.led.LeaseOf(h.holder) != nil {
		t.Fatalf("verifier took a lease on %v with the node pool full", h.leasePools())
	}

	// Room on the node pool makes the same candidate a target.
	h.led.Release("engine")
	r := h.result(ch)
	if !r.lease.Moved || r.lease.Sel.Backend != "ollama-b" || !containsString(h.leasePools(), "node:gpu2") {
		t.Fatalf("lease = moved %v backend %q pools %v, want moved to ollama-b on node:gpu2", r.lease.Moved, r.lease.Sel.Backend, h.leasePools())
	}
	r.lease.Release()
}

// TestVerifierRetargetClaudeCapAndHoldBlockMove: a Claude backend that is
// capped or held is ineligible, exactly as for the first pick, so a freed
// claude-code slot does not move the waiter.
func TestVerifierRetargetClaudeCapAndHoldBlockMove(t *testing.T) {
	t.Run("cap", func(t *testing.T) {
		policy := preferFree("pi-prompt", "claude-code")
		policy.ClaudeSessionCap = 1
		h := newRetargetHarness(t, policy, retargetPools, retargetLimits)
		h.fill()
		sel := h.selection()
		ch := h.start(sel, h.waitDeps(nil))
		h.waitForWaiter("llm:pi-prompt")

		verifierClaudeWindow.Record() // the cap of 1 is now reached
		h.led.Release("c1")
		h.stays(ch, "llm:pi-prompt")
		if h.led.LeaseOf(h.holder) != nil {
			t.Fatalf("verifier moved past the Claude cap: %v", h.leasePools())
		}
	})
	t.Run("hold", func(t *testing.T) {
		h := newRetargetHarness(t, preferFree("pi-prompt", "claude-code"), retargetPools, retargetLimits)
		bo := autonomouspkg.NewBackoff()
		h.mgr.SetBackoff(bo)
		h.fill()
		sel := h.selection()
		ch := h.start(sel, h.waitDeps(nil))
		h.waitForWaiter("llm:pi-prompt")

		bo.Set("claude-code", time.Now().Add(time.Hour), "usage limit", "test")
		h.led.Release("c1")
		h.stays(ch, "llm:pi-prompt")
		if h.led.LeaseOf(h.holder) != nil {
			t.Fatalf("verifier moved onto a held Claude backend: %v", h.leasePools())
		}

		// Lifting the hold makes claude-code a target again (the ledger change
		// below wakes the waiter; the tick would find it anyway).
		bo.Clear("claude-code")
		r := h.result(ch)
		if !r.lease.Moved || r.lease.Sel.Backend != "claude-code" {
			t.Fatalf("after the hold lifted: moved %v backend %q, want claude-code", r.lease.Moved, r.lease.Sel.Backend)
		}
		r.lease.Release()
	})
}

// TestVerifierRetargetUnchangedKeepsOneWaiter: with nothing changing, many
// ticks leave the request queued once, never duplicated.
func TestVerifierRetargetUnchangedKeepsOneWaiter(t *testing.T) {
	h := newRetargetHarness(t, preferFree("pi-prompt", "claude-code"), retargetPools, retargetLimits)
	h.fill()
	sel := h.selection()
	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:pi-prompt")
	for i := 0; i < 5; i++ {
		h.stays(ch, "llm:pi-prompt")
	}
	if h.led.LeaseOf(h.holder) != nil {
		t.Fatalf("a lease appeared with no capacity free: %v", h.leasePools())
	}
	cur, _ := h.mgr.Store().GetPRD(h.prd.ID)
	if n := len(cur.Decisions); n != 1 {
		t.Fatalf("decisions = %d, want only the first pick (a waiting request writes no decision)", n)
	}
}

// TestVerifierRetargetRefusedMoveTargetReturnsSetupVerdict: a move target the
// pinned-model guard refuses ends the verification with the setup verdict, as
// the first pick would, and leaves no lease behind.
func TestVerifierRetargetRefusedMoveTargetReturnsSetupVerdict(t *testing.T) {
	// claude-code first: the waiter sits on claude-code, and pi-prompt is the
	// target that has no pinned model.
	h := newRetargetHarness(t, preferFree("claude-code", "pi-prompt"), retargetPools, retargetLimits)
	h.fill()
	sel := h.selection()
	if sel.Backend != "claude-code" {
		t.Fatalf("pick = %q, want claude-code", sel.Backend)
	}
	_, setup := guardPiFamilyVerifyModel("pi-prompt", "", "")
	if setup == nil {
		t.Fatal("setup: the guard accepted a pi-prompt verifier with no model configured")
	}
	refuse := func(s *verifierSelection) *autonomouspkg.VerificationResult {
		if s.UseSession && isPiFamilyKind(s.Kind) && s.Model == "" {
			_, v := guardPiFamilyVerifyModel(s.Backend, "", "")
			return v
		}
		return nil
	}
	ch := h.start(sel, h.waitDeps(refuse))
	h.waitForWaiter("llm:claude-code")
	h.led.Release("p1")
	r := h.result(ch)

	if r.lease.Setup == nil || r.lease.Setup.Category != "setup" || r.lease.Setup.OK {
		t.Fatalf("lease = %+v, want the setup verdict", r.lease)
	}
	if r.lease.Release != nil || h.led.LeaseOf(h.holder) != nil {
		t.Fatalf("a refused move left a lease on %v", h.leasePools())
	}
	if ws := h.waiters(); len(ws) != 0 {
		t.Fatalf("waiters = %+v, want none", ws)
	}
}

// TestVerifierRetargetFixedModeIgnoresClaudeHoldMidWait: a fixed waiter on
// claude-code stays when a Claude hold lands while it waits, though the
// selector would now name the next entry.
func TestVerifierRetargetFixedModeIgnoresClaudeHoldMidWait(t *testing.T) {
	policy := config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"claude-code", "pi-prompt"}}
	h := newRetargetHarness(t, policy, retargetPools, retargetLimits)
	bo := autonomouspkg.NewBackoff()
	h.mgr.SetBackoff(bo)
	h.fill()
	sel := h.selection()
	if sel.Backend != "claude-code" {
		t.Fatalf("fixed pick = %q, want claude-code", sel.Backend)
	}
	ch := h.start(sel, h.waitDeps(nil))
	h.waitForWaiter("llm:claude-code")

	bo.Set("claude-code", time.Now().Add(time.Hour), "usage limit", "test")
	h.led.Release("p1") // pi-prompt now has room
	h.stays(ch, "llm:claude-code")
	if h.led.LeaseOf(h.holder) != nil {
		t.Fatalf("fixed waiter moved to %v", h.leasePools())
	}
	if _, _, ok := sel.Retarget("claude-code", nil); ok {
		t.Fatal("Retarget moved a fixed waiter")
	}
}

// TestVerifierRetargetClaudeWindowCountsTheFinalBackend: a Claude first pick
// that has to wait is not counted until it holds a lease, so a move off
// claude-code leaves nothing counted, and a grant on claude-code counts once.
func TestVerifierRetargetClaudeWindowCountsTheFinalBackend(t *testing.T) {
	t.Run("moved off claude", func(t *testing.T) {
		policy := preferFree("claude-code", "pi-prompt")
		policy.ClaudeSessionCap = 1
		h := newRetargetHarness(t, policy, retargetPools, retargetLimits)
		h.fill()
		sel := h.selection()
		if sel.Backend != "claude-code" || sel.Release != nil {
			t.Fatalf("pick = %q release %v, want a claude-code wait", sel.Backend, sel.Release != nil)
		}
		if n := verifierClaudeWindow.Count(time.Hour); n != 0 {
			t.Fatalf("a waiting Claude pick counted %d sessions, want 0", n)
		}
		ch := h.start(sel, h.waitDeps(nil))
		h.waitForWaiter("llm:claude-code")
		h.led.Release("p1")
		r := h.result(ch)
		if !r.lease.Moved || r.lease.Sel.Backend != "pi-prompt" {
			t.Fatalf("lease = moved %v backend %q, want moved to pi-prompt", r.lease.Moved, r.lease.Sel.Backend)
		}
		if n := verifierClaudeWindow.Count(time.Hour); n != 0 {
			t.Fatalf("window count = %d after a move to pi-prompt, want 0", n)
		}
		r.lease.Release()
	})
	t.Run("granted on claude", func(t *testing.T) {
		policy := preferFree("claude-code", "pi-prompt")
		policy.ClaudeSessionCap = 1
		h := newRetargetHarness(t, policy, retargetPools, retargetLimits)
		h.fill()
		sel := h.selection()
		ch := h.start(sel, h.waitDeps(nil))
		h.waitForWaiter("llm:claude-code")
		// The pick's own pending session must not count against the cap: with
		// the cap at 1 the selector still reads claude-code as eligible.
		h.stays(ch, "llm:claude-code")
		h.led.Release("c1")
		r := h.result(ch)
		if r.lease.Moved || r.lease.Sel.Backend != "claude-code" {
			t.Fatalf("lease = moved %v backend %q, want granted on claude-code", r.lease.Moved, r.lease.Sel.Backend)
		}
		if n := verifierClaudeWindow.Count(time.Hour); n != 1 {
			t.Fatalf("window count = %d, want 1", n)
		}
		r.lease.Release()
	})
}

// TestVerifierTaskSession: the dry run takes its priority from the task's
// session.
func TestVerifierRetargetTaskSession(t *testing.T) {
	h := newRetargetHarness(t, preferFree("pi-prompt"), retargetPools, retargetLimits)
	if got := verifierTaskSession(h.mgr, h.taskID); got != "" {
		t.Fatalf("session of a task with none = %q", got)
	}
	tk, _ := h.mgr.Store().GetTask(h.taskID)
	h.mgr.Store().WithLock(func() { tk.SessionID = "sess-1" })
	if got := verifierTaskSession(h.mgr, h.taskID); got != "sess-1" {
		t.Fatalf("session = %q, want sess-1", got)
	}
	if got := verifierTaskSession(h.mgr, "no-such-task"); got != "" {
		t.Fatalf("session of an unknown task = %q", got)
	}
	if got := verifierTaskSession(nil, h.taskID); got != "" {
		t.Fatalf("session with no manager = %q", got)
	}
}

// TestVerifierRetargetBalanceMovesToLowestLoad: balance names the lowest load
// among the backends that fit, never the waiting one or one already left, and
// the closure itself takes no lease and writes no decision.
func TestVerifierRetargetBalanceMovesToLowestLoad(t *testing.T) {
	pools := map[string][]string{
		"pi-prompt":   {"llm:pi-prompt"},
		"claude-code": {"llm:claude-code"},
		"ollama-b":    {"llm:ollama-b"},
	}
	limits := map[string]int{"llm:pi-prompt": 1, "llm:claude-code": 4, "llm:ollama-b": 2}
	policy := config.VerifierPolicyConfig{Mode: "balance", Backends: []string{"pi-prompt", "claude-code", "ollama-b"}}
	h := newRetargetHarness(t, policy, pools, limits)
	h.hold("p1", pools["pi-prompt"]...)
	for _, c := range []string{"c1", "c2", "c3", "c4"} {
		h.hold(c, pools["claude-code"]...)
	}
	h.hold("o1", pools["ollama-b"]...)
	h.hold("o2", pools["ollama-b"]...)
	verifierBackendLoad = func(backend, _ string) float64 {
		for _, ps := range h.led.Snapshot().Pools {
			if ps.Name == pools[backend][0] && ps.Limit > 0 {
				return float64(ps.Held) / float64(ps.Limit)
			}
		}
		return 0
	}

	sel := h.selection()
	if sel.Backend != "pi-prompt" || sel.Retarget == nil {
		t.Fatalf("pick = %q retarget %v, want a pi-prompt wait (all full, list order)", sel.Backend, sel.Retarget != nil)
	}
	if _, _, ok := sel.Retarget("pi-prompt", nil); ok {
		t.Fatal("Retarget moved with every backend full")
	}

	// claude-code now reads 0.75 and ollama-b 0.50; both have room.
	h.led.Release("c4")
	h.led.Release("o2")
	before, _ := h.mgr.Store().GetPRD(h.prd.ID)
	ns, reason, ok := sel.Retarget("pi-prompt", nil)
	if !ok || ns.Backend != "ollama-b" || reason != "balance: ollama-b has the lowest load (0.50)" {
		t.Fatalf("Retarget = %q %q ok=%v, want ollama-b with the lowest load", ns.Backend, reason, ok)
	}
	if ns, _, ok := sel.Retarget("pi-prompt", []string{"ollama-b"}); !ok || ns.Backend != "claude-code" {
		t.Fatalf("Retarget with ollama-b left = %q ok=%v, want claude-code", ns.Backend, ok)
	}
	if ns, _, ok := sel.Retarget("ollama-b", []string{"claude-code"}); ok {
		t.Fatalf("Retarget with the only other fitting backend left = %q, want none", ns.Backend)
	}
	after, _ := h.mgr.Store().GetPRD(h.prd.ID)
	if len(after.Decisions) != len(before.Decisions) {
		t.Fatal("Retarget wrote to the decision log")
	}
	if h.led.LeaseOf(h.holder) != nil {
		t.Fatalf("Retarget took a lease: %v", h.leasePools())
	}
}
