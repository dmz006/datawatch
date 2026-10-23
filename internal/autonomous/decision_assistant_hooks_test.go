package autonomous

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hookCase is one hook point wired to a scripted fake model. run triggers the
// hook; acted reports whether the assistant's pick took effect; today reports
// that the behaviour without an assistant happened instead; auto reads the
// Automation under test.
type hookCase struct {
	hook  string
	calls *atomic.Int32
	run   func()
	acted func() bool
	today func() bool
	auto  func() *Automation
}

type hookBuilder func(t *testing.T, mode string, model modelScript) hookCase

// modelScript is the fake model's behaviour: the confidence it answers with, a
// raw reply that replaces the JSON answer, or a hang until its context ends.
type modelScript struct {
	conf float64
	raw  string
	hang bool
}

func answerJSON(option string, confidence float64) string {
	return fmt.Sprintf(`{"option":%q,"confidence":%v,"reason":"scripted answer"}`, option, confidence)
}

// wireAssistant enables the hook on Automation a with a fake model that picks
// option as scripted, and returns the model call counter.
func wireAssistant(t *testing.T, m *Manager, a *Automation, mode, hook, option string, model modelScript) *atomic.Int32 {
	t.Helper()
	da := validAssistant()
	da.Mode = mode
	da.Hooks = []string{hook}
	if _, err := m.SetDecisionAssistant(a.ID, "", "alice", da, 0); err != nil {
		t.Fatal(err)
	}
	reply := model.raw
	if reply == "" {
		reply = answerJSON(option, model.conf)
	}
	calls := new(atomic.Int32)
	m.SetDecisionAssistantTimeout(40 * time.Millisecond)
	m.SetDecisionAssistantModelFn(func(ctx context.Context, backend, mdl, prompt string) (string, error) {
		calls.Add(1)
		if model.hang {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return reply, nil
	})
	return calls
}

// lastAssistantDecision returns the newest Decision of the hook's kind.
func lastAssistantDecision(t *testing.T, a *Automation, hook string) Decision {
	t.Helper()
	for i := len(a.Decisions) - 1; i >= 0; i-- {
		if a.Decisions[i].Kind == controlAssistantDecisionPrefix+hook {
			return a.Decisions[i]
		}
	}
	t.Fatalf("no %s Decision recorded in %+v", controlAssistantDecisionPrefix+hook, a.Decisions)
	return Decision{}
}

// runHookMatrix drives one hook through the five outcomes: a confident pick
// acts, low confidence, garbage and a timeout escalate (today's behaviour), and
// shadow mode records without acting.
func runHookMatrix(t *testing.T, build hookBuilder) {
	cases := []struct {
		name    string
		mode    string
		model   modelScript
		acts    bool
		outcome string
	}{
		{"confident pick acts", DecisionModeLive, modelScript{conf: 0.99}, true, "outcome=decided"},
		{"low confidence escalates", DecisionModeLive, modelScript{conf: 0.4}, false, "outcome=escalate"},
		{"garbage escalates", DecisionModeLive, modelScript{raw: "I would just skip it, honestly."}, false, "outcome=escalate"},
		{"timeout escalates", DecisionModeLive, modelScript{hang: true}, false, "outcome=escalate"},
		{"shadow records without acting", DecisionModeShadow, modelScript{conf: 0.99}, false, "outcome=would_decide"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hc := build(t, tc.mode, tc.model)
			hc.run()
			if hc.calls.Load() == 0 {
				t.Fatal("the model was never consulted")
			}
			if got := hc.acted(); got != tc.acts {
				t.Fatalf("acted = %t, want %t", got, tc.acts)
			}
			if !tc.acts && !hc.today() {
				t.Fatal("the existing behaviour did not happen")
			}
			d := lastAssistantDecision(t, hc.auto(), hc.hook)
			if !strings.HasPrefix(d.Actor, "assistant:") || !strings.Contains(d.Note, tc.outcome) {
				t.Fatalf("decision = %+v, want an assistant actor and %q", d, tc.outcome)
			}
			if tc.mode == DecisionModeShadow && !strings.Contains(d.Note, "SHADOW") {
				t.Fatalf("shadow decision is not marked: %q", d.Note)
			}
		})
	}
}

// emptyEdgeRig is a chain m0 -> m1 whose m0 has no blocked or failed edge, with
// the assistant on the empty_edge hook.
func emptyEdgeRig(t *testing.T, mode, option string, model modelScript) (*coordHarness, *Automation, *atomic.Int32) {
	t.Helper()
	h, _ := newControlHarness(t, 2)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	calls := wireAssistant(t, h.m, a, mode, DecisionHookEmptyEdge, option, model)
	h.c.SetReady()
	return h, a, calls
}

func TestDecisionAssistantHookEmptyEdge(t *testing.T) {
	runHookMatrix(t, func(t *testing.T, mode string, model modelScript) hookCase {
		h, a, calls := emptyEdgeRig(t, mode, assistOptionSkipPrefix+edgeOnComplete, model)
		return hookCase{
			hook:  DecisionHookEmptyEdge,
			calls: calls,
			run: func() {
				h.setStatus(0, PRDFailed)
				h.event(0, PRDFailed)
			},
			acted: func() bool { return h.get(a).Members[0].Status == MemberSkipped },
			today: func() bool {
				got := h.get(a)
				return got.Status == AutomationBlocked && got.Members[0].Status == MemberFailed
			},
			auto: func() *Automation { return h.get(a) },
		}
	})
}

func TestAssistantHookEmptyEdgePausesAutomation(t *testing.T) {
	h, a, _ := emptyEdgeRig(t, DecisionModeLive, assistOptionPause, modelScript{conf: 0.99})
	h.setStatus(0, PRDFailed)
	h.event(0, PRDFailed)
	got := h.get(a)
	if got.Status != AutomationPaused {
		t.Fatalf("status = %s, want paused", got.Status)
	}
	if got.Members[0].Status == MemberSkipped {
		t.Fatal("pausing must not skip the member")
	}
}

func TestAssistantHookEmptyEdgeNeedsOperatorStillBlocks(t *testing.T) {
	h, a, _ := emptyEdgeRig(t, DecisionModeLive, assistOptionNeedsOperator, modelScript{conf: 0.99})
	h.setStatus(0, PRDFailed)
	h.event(0, PRDFailed)
	if got := h.get(a); got.Status != AutomationBlocked || got.Members[0].Status != MemberFailed {
		t.Fatalf("status=%s member=%s, want the existing stop-and-alert", got.Status, got.Members[0].Status)
	}
	if h.alertCount("warn|") != 1 {
		t.Fatalf("alerts = %v", h.alerts)
	}
}

func TestAssistantHookEmptyEdgeSkipsOnlyExistingEdges(t *testing.T) {
	h, _ := newControlHarness(t, 2)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	mem := automationMember(a, "m0")
	if got := skipEdgeOptions(mem, edgeOnFailed); len(got) != 1 || got[0] != assistOptionSkipPrefix+edgeOnComplete {
		t.Fatalf("options = %v, want only on_complete (blocked and failed name no member)", got)
	}
	// An edge that names no member is not offered, so the engine refuses the pick.
	eh, ea, _ := emptyEdgeRig(t, DecisionModeLive, assistOptionSkipPrefix+edgeOnBlocked, modelScript{conf: 0.99})
	eh.setStatus(0, PRDFailed)
	eh.event(0, PRDFailed)
	if got := eh.get(ea); got.Status != AutomationBlocked || got.Members[0].Status != MemberFailed {
		t.Fatalf("status=%s member=%s, want the existing block", got.Status, got.Members[0].Status)
	}
}

func TestAssistantHookDisabledNeverConsultsModel(t *testing.T) {
	h, a, calls := emptyEdgeRig(t, DecisionModeLive, assistOptionPause, modelScript{conf: 0.99})
	da := validAssistant()
	da.Hooks = []string{DecisionHookBudgetBreach}
	if _, err := h.m.SetDecisionAssistant(a.ID, "", "alice", da, 0); err != nil {
		t.Fatal(err)
	}
	h.setStatus(0, PRDFailed)
	h.event(0, PRDFailed)
	if calls.Load() != 0 {
		t.Fatalf("model called %d times with the hook disabled", calls.Load())
	}
	if got := h.get(a); got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want the existing block", got.Status)
	}
}

func TestDecisionAssistantHookSkipMember(t *testing.T) {
	runHookMatrix(t, func(t *testing.T, mode string, model modelScript) hookCase {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		calls := wireAssistant(t, h.m, a, mode, DecisionHookSkipMember, assistOptionSkipPrefix+edgeOnComplete, model)
		return hookCase{
			hook:  DecisionHookSkipMember,
			calls: calls,
			run: func() {
				if _, _, err := h.m.AssistSkipStuckMember(context.Background(), a.ID, "", "no output for 30 minutes"); err != nil {
					t.Fatal(err)
				}
			},
			acted: func() bool { return h.get(a).Members[0].Status == MemberSkipped },
			today: func() bool { return h.get(a).Members[0].Status == MemberRunning },
			auto:  func() *Automation { return h.get(a) },
		}
	})
}

func TestAssistantHookSkipMemberKeepWaitingLeavesMember(t *testing.T) {
	h, _ := newControlHarness(t, 2)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	wireAssistant(t, h.m, a, DecisionModeLive, DecisionHookSkipMember, assistOptionKeepWaiting, modelScript{conf: 0.99})
	res, _, err := h.m.AssistSkipStuckMember(context.Background(), a.ID, "", "slow")
	if err != nil || !res.Actionable() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if h.get(a).Members[0].Status != MemberRunning {
		t.Fatal("keep_waiting skipped the member")
	}
}

func insertRig(t *testing.T, mode string, model modelScript) (*coordHarness, *Automation, *atomic.Int32, []AssistInsertCandidate) {
	t.Helper()
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	cands := []AssistInsertCandidate{{PRDID: h.prds[2].ID, Name: "fix-up"}}
	calls := wireAssistant(t, h.m, a, mode, DecisionHookInsertMember, assistOptionInsertPrefix+cands[0].PRDID, model)
	return h, a, calls, cands
}

func TestDecisionAssistantHookInsertMember(t *testing.T) {
	runHookMatrix(t, func(t *testing.T, mode string, model modelScript) hookCase {
		h, a, calls, cands := insertRig(t, mode, model)
		return hookCase{
			hook:  DecisionHookInsertMember,
			calls: calls,
			run: func() {
				if _, _, err := h.m.AssistInsertTriage(context.Background(), a.ID, "m0", cands); err != nil {
					t.Fatal(err)
				}
			},
			acted: func() bool { return len(h.get(a).PendingInserts) == 1 },
			today: func() bool { return len(h.get(a).PendingInserts) == 0 },
			auto:  func() *Automation { return h.get(a) },
		}
	})
}

func TestAssistantHookInsertStaysPendingApproval(t *testing.T) {
	h, a, _, cands := insertRig(t, DecisionModeLive, modelScript{conf: 0.99})
	if _, _, err := h.m.AssistInsertTriage(context.Background(), a.ID, "m0", cands); err != nil {
		t.Fatal(err)
	}
	got := h.get(a)
	if len(got.PendingInserts) != 1 || len(got.Members) != 2 {
		t.Fatalf("pending=%d members=%d, want the insert held and the graph unchanged", len(got.PendingInserts), len(got.Members))
	}
	p := got.PendingInserts[0]
	if p.Actor != "assistant:qwen3.8:27b" || p.Reason != "scripted answer" || p.PRDID != cands[0].PRDID || p.AfterMemberID != "m0" {
		t.Fatalf("pending insert = %+v", p)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("a member was started before approval: %v", runs)
	}
	if _, err := h.m.ApproveInsertedMember(a.ID, p.ID, "assistant:qwen3.8:27b", h.get(a).Version); err == nil {
		t.Fatal("the assistant approved its own insert")
	}
	approved, err := h.m.ApproveInsertedMember(a.ID, p.ID, operatorActor, h.get(a).Version)
	if err != nil {
		t.Fatal(err)
	}
	var landed *AutomationMember
	for i := range approved.Members {
		if approved.Members[i].ID == p.ID {
			landed = &approved.Members[i]
		}
	}
	if landed == nil || landed.AddedBy != "assistant:qwen3.8:27b" || landed.AddedReason != "scripted answer" || landed.Status != MemberPending {
		t.Fatalf("approved member = %+v", landed)
	}
}

func TestAssistantHookInsertRefusesDraft(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a, err := h.m.CreateAutomation(AutomationInput{Name: "draft", EntryMemberID: "m0", Members: []AutomationMember{h.cm(0, "", "", "")}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	wireAssistant(t, h.m, a, DecisionModeLive, DecisionHookInsertMember, assistOptionInsertPrefix+h.prds[2].ID, modelScript{conf: 0.99})
	if _, _, err := h.m.AssistInsertTriage(context.Background(), a.ID, "m0", []AssistInsertCandidate{{PRDID: h.prds[2].ID}}); err == nil {
		t.Fatal("a draft Automation would take the insert at once; it must be refused")
	}
	if len(h.get(a).Members) != 1 {
		t.Fatal("the draft graph changed")
	}
}

// breachRig is a day-window dollar ceiling already breached when m0 finishes,
// so the start of m1 is gated; by default that pauses (the reset time is known).
func breachRig(t *testing.T, mode string, model modelScript) (*gateHarness, *Automation, *atomic.Int32) {
	t.Helper()
	g := newGateHarness(t, 2, nil)
	a := g.activateRunning(g.create(claudeDay(10), 2))
	g.overCeiling(12)
	calls := wireAssistant(t, g.m, a, mode, DecisionHookBudgetBreach, assistBreachBlock, model)
	return g, a, calls
}

func TestDecisionAssistantHookBudgetBreach(t *testing.T) {
	runHookMatrix(t, func(t *testing.T, mode string, model modelScript) hookCase {
		g, a, calls := breachRig(t, mode, model)
		return hookCase{
			hook:  DecisionHookBudgetBreach,
			calls: calls,
			run:   g.finishFirst,
			acted: func() bool { return g.get(a).Status == AutomationBlocked },
			today: func() bool { return g.get(a).Status == AutomationPaused },
			auto:  func() *Automation { return g.get(a) },
		}
	})
}

func TestAssistantHookBudgetCeilingUnchanged(t *testing.T) {
	g, a, _ := breachRig(t, DecisionModeLive, modelScript{conf: 0.99})
	g.finishFirst()
	got := g.get(a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want the assistant's block", got.Status)
	}
	if p := got.BudgetPolicy; p == nil || p.CeilingValue != 10 || p.EffectiveOnBreach() != "pause" {
		t.Fatalf("policy changed: %+v", p)
	}
	if bs := got.BudgetState; bs == nil || bs.CeilingUSD != 10 || bs.Action != BudgetActionBlocked {
		t.Fatalf("budget state = %+v", bs)
	}
	g.noStarts()
	if m0 := got.Members[0]; m0.Status != MemberCompleted {
		t.Fatalf("in-flight work was touched: %+v", m0)
	}
	// An option that raises the ceiling is never offered and never accepted.
	for _, o := range []string{"raise_ceiling", "cancel_in_flight", "remove_member"} {
		if guardAssistantOption(o, nil) == nil {
			t.Fatalf("guard accepted %q", o)
		}
	}
}

func TestAssistantHookBudgetCannotOverrideUnknownReset(t *testing.T) {
	g := newGateHarness(t, 2, nil)
	p := claudeLifetime(10) // no window reset: pausing is impossible, the gate blocks
	a := g.activateRunning(g.create(p, 2))
	g.overCeiling(12)
	wireAssistant(t, g.m, a, DecisionModeLive, DecisionHookBudgetBreach, assistBreachPause, modelScript{conf: 0.99})
	g.finishFirst()
	if got := g.get(a); got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want blocked: pause was not available", got.Status)
	}
}
