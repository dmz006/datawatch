package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type auditRec struct {
	action  string
	details map[string]any
}

// assistantHarness is an Automation whose member m0 has the assistant enabled
// for every hook, a scripted fake model and a captured audit trail.
type assistantHarness struct {
	h      *coordHarness
	a      *Automation
	mu     sync.Mutex
	audits []auditRec
	prompt string
	calls  int
}

func newAssistantHarness(t *testing.T, mode string, reply func(ctx context.Context) (string, error)) *assistantHarness {
	t.Helper()
	h, a := llmAuto(t)
	ah := &assistantHarness{h: h}
	da := validAssistant()
	da.Mode = mode
	da.Hooks = append([]string(nil), decisionHookNames...)
	out, err := h.m.SetDecisionAssistant(a.ID, "", "alice", da, a.Version)
	if err != nil {
		t.Fatal(err)
	}
	ah.a = out
	h.m.SetDecisionAssistantModelFn(func(ctx context.Context, backend, model, prompt string) (string, error) {
		ah.mu.Lock()
		ah.prompt = prompt
		ah.calls++
		ah.mu.Unlock()
		return reply(ctx)
	})
	h.m.SetAuditFn(func(action string, details map[string]any) {
		ah.mu.Lock()
		ah.audits = append(ah.audits, auditRec{action, details})
		ah.mu.Unlock()
	})
	h.m.mu.Lock()
	h.m.coordinator = nil
	h.m.mu.Unlock()
	return ah
}

func fixedReply(s string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return s, nil }
}

const pauseAnswer = `{"option":"pause_automation","confidence":0.97,"reason":"The member failed on infra only."}`

var emptyEdgeOptions = []string{"pause_automation", "skip_member", "escalate_to_operator"}

func (ah *assistantHarness) decide(hook string, packet DecisionEvidence, allowed []string) AssistantResult {
	return ah.h.m.decide(context.Background(), ah.a.ID, "m0", hook, packet, allowed)
}

func (ah *assistantHarness) lastDecision(t *testing.T) Decision {
	t.Helper()
	cur, _ := ah.h.m.GetAutomation(ah.a.ID)
	if len(cur.Decisions) == 0 {
		t.Fatal("no Decision recorded")
	}
	return cur.Decisions[len(cur.Decisions)-1]
}

func TestDecisionAssistantConfidentPick(t *testing.T) {
	ah := newAssistantHarness(t, DecisionModeLive, fixedReply(pauseAnswer))
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{MemberID: "m0", FailureReason: "infra: session crashed"}, emptyEdgeOptions)
	if res.Outcome != AssistantDecided || !res.Actionable() || res.Option != "pause_automation" || res.Confidence != 0.97 {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(ah.prompt, "infra: session crashed") || !strings.Contains(ah.prompt, "- skip_member") {
		t.Fatalf("prompt lacks evidence or options: %q", ah.prompt)
	}
}

func TestDecisionAssistantLowConfidenceEscalates(t *testing.T) {
	low := `{"option":"pause_automation","confidence":0.5,"reason":"Not sure."}`
	ah := newAssistantHarness(t, DecisionModeLive, fixedReply(low))
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || res.Actionable() || !strings.Contains(res.Reason, "below") {
		t.Fatalf("got %+v", res)
	}
	// The budget hook's higher default threshold applies: 0.92 passes empty-edge, not budget.
	mid := `{"option":"pause","confidence":0.92,"reason":"Close to done."}`
	ah = newAssistantHarness(t, DecisionModeLive, fixedReply(mid))
	if r := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, []string{"pause", "block"}); r.Outcome != AssistantDecided {
		t.Fatalf("empty_edge at 0.92: %+v", r)
	}
	if r := ah.decide(DecisionHookBudgetBreach, DecisionEvidence{}, []string{"pause", "block"}); r.Outcome != AssistantEscalate {
		t.Fatalf("budget_breach at 0.92 must escalate: %+v", r)
	}
}

func TestDecisionAssistantGarbageEscalates(t *testing.T) {
	ah := newAssistantHarness(t, DecisionModeLive, fixedReply("I think you should pause it."))
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || !strings.Contains(res.Reason, "unusable answer") {
		t.Fatalf("got %+v", res)
	}
}

func TestDecisionAssistantTimeoutEscalates(t *testing.T) {
	// A model that ignores its context entirely must still be cut off.
	block := make(chan struct{})
	defer close(block)
	ah := newAssistantHarness(t, DecisionModeLive, func(context.Context) (string, error) {
		<-block
		return pauseAnswer, nil
	})
	ah.h.m.SetDecisionAssistantTimeout(30 * time.Millisecond)
	start := time.Now()
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || !strings.Contains(res.Reason, "timed out") {
		t.Fatalf("got %+v", res)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout not enforced: %v", time.Since(start))
	}
}

func TestDecisionAssistantUnreachableModelEscalates(t *testing.T) {
	ah := newAssistantHarness(t, DecisionModeLive, func(context.Context) (string, error) {
		return "", errors.New("connection refused")
	})
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || !strings.Contains(res.Reason, "connection refused") {
		t.Fatalf("got %+v", res)
	}
	ah.h.m.SetDecisionAssistantModelFn(nil)
	res = ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || !strings.Contains(res.Reason, "no decision assistant model") {
		t.Fatalf("unwired model: %+v", res)
	}
}

func TestDecisionAssistantOffOrHookDisabledEscalates(t *testing.T) {
	ah := newAssistantHarness(t, DecisionModeLive, fixedReply(pauseAnswer))
	da := validAssistant()
	da.Mode = DecisionModeLive
	da.Hooks = []string{DecisionHookSkipMember}
	cur, _ := ah.h.m.GetAutomation(ah.a.ID)
	if _, err := ah.h.m.SetDecisionAssistant(ah.a.ID, "", "alice", da, cur.Version); err != nil {
		t.Fatal(err)
	}
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || ah.calls != 0 {
		t.Fatalf("disabled hook must not call the model: %+v calls=%d", res, ah.calls)
	}
	cur, _ = ah.h.m.GetAutomation(ah.a.ID)
	if _, err := ah.h.m.SetDecisionAssistant(ah.a.ID, "", "alice", nil, cur.Version); err != nil {
		t.Fatal(err)
	}
	res = ah.decide(DecisionHookSkipMember, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantEscalate || ah.calls != 0 {
		t.Fatalf("off assistant must not call the model: %+v calls=%d", res, ah.calls)
	}
}

func TestDecisionAssistantShadowNotActionable(t *testing.T) {
	ah := newAssistantHarness(t, DecisionModeShadow, fixedReply(pauseAnswer))
	res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, emptyEdgeOptions)
	if res.Outcome != AssistantWouldDecide || res.Actionable() || !res.Shadow || res.Option != "pause_automation" {
		t.Fatalf("got %+v", res)
	}
	if d := ah.lastDecision(t); !strings.Contains(d.Note, "SHADOW") || !strings.Contains(d.Note, "not acted on") {
		t.Fatalf("shadow not marked clearly: %q", d.Note)
	}
}

func TestDecisionAssistantDecisionAndAuditShape(t *testing.T) {
	ah := newAssistantHarness(t, DecisionModeLive, fixedReply(pauseAnswer))
	ah.decide(DecisionHookEmptyEdge, DecisionEvidence{MemberID: "m0", FailureReason: "infra: session crashed", Progress: "2/3 done", Spend: "$1.20"}, emptyEdgeOptions)
	d := ah.lastDecision(t)
	if d.Actor != "assistant:qwen3.8:27b" || d.Kind != "assistant_empty_edge" || d.Backend != "pi-prompt" || d.Model != "qwen3.8:27b" {
		t.Fatalf("decision shape: %+v", d)
	}
	for _, want := range []string{"member=m0", "infra: session crashed", "2/3 done", "0.97", "pause_automation", "[live]"} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("note lacks %q: %q", want, d.Note)
		}
	}
	if len(ah.audits) != 1 || ah.audits[0].action != "assistant_empty_edge" {
		t.Fatalf("audits: %+v", ah.audits)
	}
	det := ah.audits[0].details
	if det["automation_id"] != ah.a.ID || det["actor"] != "assistant:qwen3.8:27b" || det["outcome"] != AssistantDecided ||
		det["option"] != "pause_automation" || det["confidence"] != 0.97 || det["hook"] != DecisionHookEmptyEdge || det["shadow"] != false {
		t.Fatalf("audit details: %+v", det)
	}
	// An escalation is recorded too.
	ah.h.m.SetDecisionAssistantModelFn(func(context.Context, string, string, string) (string, error) { return "nope", nil })
	ah.decide(DecisionHookSkipMember, DecisionEvidence{}, emptyEdgeOptions)
	if d := ah.lastDecision(t); d.Kind != "assistant_skip_member" || !strings.Contains(d.Note, "outcome=escalate") {
		t.Fatalf("escalation not recorded: %+v", d)
	}
}

func TestDecisionAssistantEvidencePacketBounds(t *testing.T) {
	big := strings.Repeat("é", 5000)
	e := DecisionEvidence{MemberID: big, FailureReason: big, Verdict: big, Progress: big, Spend: big}
	for i := 0; i < 40; i++ {
		e.HumanSignOff = append(e.HumanSignOff, big)
	}
	b := e.bounded()
	if len(b.FailureReason) > evidenceFailureCap || len(b.Verdict) > evidenceVerdictCap || len(b.Progress) > evidenceProgressCap ||
		len(b.Spend) > evidenceSpendCap || len(b.MemberID) > evidenceOptionCap {
		t.Fatalf("a field exceeds its cap: %d %d %d %d", len(b.FailureReason), len(b.Verdict), len(b.Progress), len(b.Spend))
	}
	if len(b.HumanSignOff) != evidenceMaxOptions || len(b.HumanSignOff[0]) > evidenceOptionCap {
		t.Fatalf("sign-off list unbounded: %d", len(b.HumanSignOff))
	}
	if strings.ContainsRune(b.FailureReason, '�') {
		t.Fatal("truncation split a rune")
	}
	opts := make([]string, 50)
	for i := range opts {
		opts[i] = big
	}
	if p := buildAssistantPrompt(DecisionHookEmptyEdge, e, opts); len(p) > 8192 {
		t.Fatalf("prompt is %d bytes", len(p))
	}
}

func TestAssistantAnswerParse(t *testing.T) {
	allowed := []string{"pause", "block"}
	ok, err := parseAssistantAnswer("```json\n{\"option\":\"pause\",\"confidence\":1,\"reason\":\"ok.\"}\n```", allowed)
	if err != nil || ok.Option != "pause" || ok.Confidence != 1 {
		t.Fatalf("valid fenced answer: %+v %v", ok, err)
	}
	if _, err := parseAssistantAnswer(`{"option":"pause","confidence":0,"reason":"r"}`, allowed); err != nil {
		t.Fatalf("confidence 0 is valid: %v", err)
	}
	bad := map[string]string{
		"not JSON":        `pause please`,
		"empty":           ``,
		"missing option":  `{"confidence":0.9,"reason":"r"}`,
		"missing conf":    `{"option":"pause","reason":"r"}`,
		"missing reason":  `{"option":"pause","confidence":0.9}`,
		"empty reason":    `{"option":"pause","confidence":0.9,"reason":"  "}`,
		"conf above one":  `{"option":"pause","confidence":1.1,"reason":"r"}`,
		"conf negative":   `{"option":"pause","confidence":-0.1,"reason":"r"}`,
		"unlisted option": `{"option":"cancel","confidence":0.9,"reason":"r"}`,
		"array":           `["pause"]`,
	}
	for name, raw := range bad {
		if _, err := parseAssistantAnswer(raw, allowed); err == nil {
			t.Errorf("%s: accepted %q", name, raw)
		}
	}
}

func TestAssistantGuardForbiddenOptions(t *testing.T) {
	forbidden := []string{
		"cancel_in_flight", "Cancel In-Flight", "cancel_automation cancel_in_flight=true",
		"remove_member", "remove_member m1", "remove-member of a started member",
		"raise_budget_ceiling", "increase the budget", "bump_ceiling", "lift budget limit", "set_budget",
	}
	for _, opt := range forbidden {
		if err := guardAssistantOption(opt, nil); err == nil {
			t.Errorf("guard accepted %q", opt)
		}
	}
	if err := guardAssistantOption("approve_release", []string{"Approve Release"}); err == nil {
		t.Error("guard accepted an option marked as needing human sign-off")
	}
	for _, opt := range []string{"pause_automation", "skip_member", "block", "pause", "escalate_to_operator", "insert_member corrective"} {
		if err := guardAssistantOption(opt, []string{"approve_release"}); err != nil {
			t.Errorf("guard rejected %q: %v", opt, err)
		}
	}
}

// Every forbidden action is rejected by decide even when the fake model asks
// for it and the option was listed as allowed by mistake.
func TestAssistantGuardRejectsEvenWhenListedAndRequested(t *testing.T) {
	cases := []struct {
		hook, option string
		signOff      []string
	}{
		{DecisionHookEmptyEdge, "cancel_in_flight", nil},
		{DecisionHookSkipMember, "remove_member m0", nil},
		{DecisionHookBudgetBreach, "raise_budget_ceiling", nil},
		{DecisionHookInsertMember, "insert_member release_now", []string{"insert_member release_now"}},
	}
	for _, c := range cases {
		reply := `{"option":"` + c.option + `","confidence":1,"reason":"Obviously best."}`
		ah := newAssistantHarness(t, DecisionModeLive, fixedReply(reply))
		res := ah.decide(c.hook, DecisionEvidence{HumanSignOff: c.signOff}, []string{"pause", c.option})
		if res.Outcome != AssistantEscalate || res.Actionable() || !strings.Contains(res.Reason, "guard rejected") {
			t.Errorf("%s: got %+v", c.option, res)
		}
		if d := ah.lastDecision(t); !strings.Contains(d.Note, "outcome=escalate") {
			t.Errorf("%s: rejection not recorded: %q", c.option, d.Note)
		}
	}
	// Shadow mode does not soften the guard.
	ah := newAssistantHarness(t, DecisionModeShadow, fixedReply(`{"option":"cancel_in_flight","confidence":1,"reason":"r"}`))
	if res := ah.decide(DecisionHookEmptyEdge, DecisionEvidence{}, []string{"cancel_in_flight"}); res.Outcome != AssistantEscalate {
		t.Fatalf("shadow: got %+v", res)
	}
}
