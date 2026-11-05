package autonomous

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func gateMgr(mode string, res VerificationResult, calls *int) *Manager {
	m := &Manager{}
	m.SetGuardrailClassify(
		func(b, mo string) inference.VerificationMode { return inference.VerificationMode(mode) },
		func(ctx context.Context, b, mo, p string) VerificationResult { *calls++; return res })
	return m
}

var gateInv = GuardrailInvocation{Guardrail: "rules", Level: "task", UnitID: "t1", GuardrailBackend: "b", GuardrailModel: "m"}

func TestClassifyGate_GenerateModeUntouched(t *testing.T) {
	n := 0
	m := gateMgr("generate", VerificationResult{OK: false}, &n)
	v := m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "pass"})
	if v.Outcome != "pass" || v.Mode != "" || n != 0 {
		t.Fatalf("generate mode changed: %+v calls=%d", v, n)
	}
}

func TestClassifyGate_PriorBlockNeverOverridden(t *testing.T) {
	n := 0
	m := gateMgr("classify", VerificationResult{OK: true, Severity: "info"}, &n)
	v := m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "block", Category: "content"})
	if v.Outcome != "block" || n != 0 || v.Mode != "classify" {
		t.Fatalf("block downgraded: %+v calls=%d", v, n)
	}
}

func TestClassifyGate_ErrorBlocksWithInfra(t *testing.T) {
	n := 0
	m := gateMgr("classify", VerificationResult{OK: false, Severity: "medium", Category: ClassifyCategoryInfra, Summary: "transport"}, &n)
	v := m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "pass", Summary: "looks fine"})
	if v.Outcome != "block" || v.Category != "infra" || v.Mode != "classify" {
		t.Fatalf("want infra block: %+v", v)
	}
}

func TestClassifyGate_PassAndWarnMapping(t *testing.T) {
	n := 0
	scores := &VerdictScores{PassProbability: 0.9}
	m := gateMgr("classify", VerificationResult{OK: true, Severity: "info", Scores: scores, Route: &VerdictRoute{Node: "n"}}, &n)
	v := m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "pass"})
	if v.Outcome != "pass" || v.Scores != scores || v.Route == nil {
		t.Fatalf("pass: %+v", v)
	}
	v = m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "warn"})
	if v.Outcome != "warn" {
		t.Fatalf("warn raised to %s", v.Outcome)
	}
	m = gateMgr("classify", VerificationResult{OK: true, Severity: "medium"}, &n)
	if v = m.classifyGate(context.Background(), gateInv, GuardrailVerdict{Outcome: "pass"}); v.Outcome != "warn" {
		t.Fatalf("medium should warn: %+v", v)
	}
}
