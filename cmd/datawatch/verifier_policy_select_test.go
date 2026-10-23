package main

import (
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// policyProbeFake stands in for verifierCapacityTryAdmit: a backend in full
// refuses, every other admits. It tracks which leases are still held.
type policyProbeFake struct {
	full   map[string]bool
	probes []string
	held   map[string]int
}

func newPolicyProbeFake(full ...string) *policyProbeFake {
	f := &policyProbeFake{full: map[string]bool{}, held: map[string]int{}}
	for _, b := range full {
		f.full[b] = true
	}
	return f
}

func (f *policyProbeFake) tryAdmit(backend, _ string) (func(), bool) {
	f.probes = append(f.probes, backend)
	if f.full[backend] {
		return func() {}, false
	}
	f.held[backend]++
	return func() { f.held[backend]-- }, true
}

func policyTestResolvers() (func(string) (string, string, error), func(string) bool) {
	return fakeAskResolver(
		map[string]string{"ollama-a": "ollama", "ollama-b": "ollama", "claude-code": "claude-code"},
		map[string]string{"ollama-a": "qwen-a", "ollama-b": "qwen-b", "claude-code": "opus"},
	)
}

func pickForTest(t *testing.T, in verifierPolicyInput, f *policyProbeFake) (verifierPolicyPick, bool) {
	t.Helper()
	resolve, compatible := policyTestResolvers()
	in.ProjectDir = t.TempDir()
	return pickVerifierByPolicy(in, f.tryAdmit, resolve, compatible, fakeIsSessionKind)
}

func TestVerifierPolicyPreferFreeSkipsFullFirstBackend(t *testing.T) {
	f := newPolicyProbeFake("ollama-a")
	pick, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"ollama-a", "ollama-b"}},
	}, f)
	if !ok || pick.Selection.Backend != "ollama-b" || pick.Selection.Release == nil {
		t.Fatalf("pick = %+v ok=%v, want admitted ollama-b", pick.Selection, ok)
	}
	if pick.Selection.Source != autonomouspkg.VerifierSourcePolicy || pick.Selection.Model != "qwen-b" {
		t.Fatalf("selection = %+v", pick.Selection)
	}
	if f.held["ollama-b"] != 1 {
		t.Fatalf("held = %v, want one lease on ollama-b", f.held)
	}
}

func TestVerifierPolicyBalancePicksLighterBackendAndKeepsOneLease(t *testing.T) {
	loads := map[string]float64{"ollama-a": 0.75, "ollama-b": 0.25}
	old := verifierBackendLoad
	verifierBackendLoad = func(backend, _ string) float64 { return loads[backend] }
	defer func() { verifierBackendLoad = old }()

	f := newPolicyProbeFake()
	pick, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "balance", Backends: []string{"ollama-a", "ollama-b"}},
	}, f)
	if !ok || pick.Selection.Backend != "ollama-b" {
		t.Fatalf("pick = %+v ok=%v, want ollama-b", pick.Selection, ok)
	}
	if pick.Selection.Release == nil {
		t.Fatal("chosen backend was probed free; want a lease")
	}
	if f.held["ollama-a"] != 0 || f.held["ollama-b"] != 1 {
		t.Fatalf("held = %v, want only ollama-b", f.held)
	}
	pick.Selection.Release()
	if f.held["ollama-b"] != 0 {
		t.Fatalf("held after release = %v", f.held)
	}
}

func TestVerifierPolicyGPUOnlyNeverPicksClaude(t *testing.T) {
	f := newPolicyProbeFake("ollama-a")
	pick, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "gpu-only", Backends: []string{"claude-code", "ollama-a"}},
	}, f)
	if !ok || pick.Selection.Backend != "ollama-a" || pick.Claude {
		t.Fatalf("pick = %+v ok=%v, want ollama-a", pick.Selection, ok)
	}
	for _, p := range f.probes {
		if p == "claude-code" {
			t.Fatalf("claude-code was probed: %v", f.probes)
		}
	}
	// Only Claude listed: nothing eligible.
	if _, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "gpu-only", Backends: []string{"claude-code"}},
	}, newPolicyProbeFake()); ok {
		t.Fatal("gpu-only with only claude-code must have no eligible backend")
	}
}

func TestVerifierPolicyHoldRemovesClaude(t *testing.T) {
	f := newPolicyProbeFake()
	pick, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"claude-code", "ollama-a"}},
		Flags:  autonomouspkg.VerifierSelectFlags{ClaudeHold: map[string]bool{"claude-code": true}},
	}, f)
	if !ok || pick.Selection.Backend != "ollama-a" {
		t.Fatalf("pick = %+v ok=%v, want ollama-a while claude-code is held", pick.Selection, ok)
	}
}

func TestVerifierPolicyClaudeSessionCapRemovesClaude(t *testing.T) {
	pick, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"claude-code", "ollama-a"}, ClaudeSessionCap: 2},
		Flags:  autonomouspkg.VerifierSelectFlags{ClaudeSessionsInWindow: 2},
	}, newPolicyProbeFake())
	if !ok || pick.Selection.Backend != "ollama-a" {
		t.Fatalf("pick = %+v ok=%v, want ollama-a at the cap", pick.Selection, ok)
	}
}

func TestVerifierPolicyRetryForcesStrongerVerifier(t *testing.T) {
	f := newPolicyProbeFake()
	pick, ok := pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true},
		Flags:  autonomouspkg.VerifierSelectFlags{Retry: true},
	}, f)
	if !ok || pick.Selection.Backend != "claude-code" || !pick.Claude {
		t.Fatalf("pick = %+v ok=%v, want the stronger claude-code", pick.Selection, ok)
	}
	if pick.ModelChosen != config.VerifierPolicyStrongerModelDefault || pick.Selection.Model != config.VerifierPolicyStrongerModelDefault {
		t.Fatalf("model = %q / %q, want the stronger default", pick.ModelChosen, pick.Selection.Model)
	}
	if !pick.Selection.UseSession {
		t.Fatal("claude-code is session-only; want session routing")
	}
	// Without the retry the first listed backend runs.
	pick, ok = pickForTest(t, verifierPolicyInput{
		Policy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"ollama-a"}, ForceStrongerOnRetry: true},
	}, newPolicyProbeFake())
	if !ok || pick.Selection.Backend != "ollama-a" {
		t.Fatalf("no retry: pick = %+v ok=%v", pick.Selection, ok)
	}
}

func TestVerifierPolicyFixedModeLeavesSelectionAlone(t *testing.T) {
	p := config.VerifierPolicyConfig{Mode: "fixed"}
	if verifierPolicyApplies(p, autonomouspkg.VerifierSelectFlags{}) {
		t.Fatal("plain fixed policy must not apply")
	}
	if verifierPolicyApplies(p, autonomouspkg.VerifierSelectFlags{Retry: true, FinalGate: true}) {
		t.Fatal("fixed policy without stronger triggers must not apply on retry/final gate")
	}
	if !verifierPolicyApplies(config.VerifierPolicyConfig{Mode: "fixed", ForceStrongerOnRetry: true}, autonomouspkg.VerifierSelectFlags{Retry: true}) {
		t.Fatal("force_stronger_on_retry must apply on a retry")
	}
	if verifierPolicyApplies(config.VerifierPolicyConfig{Mode: "bogus"}, autonomouspkg.VerifierSelectFlags{}) {
		t.Fatal("an invalid mode must not apply")
	}

	resolve, compatible := policyTestResolvers()
	prd := &autonomouspkg.PRD{ID: "p1", Backend: "ollama-a"}
	task := &autonomouspkg.Task{ID: "t1"}
	f := newPolicyProbeFake()
	sel, ok := policyVerifierSelection(nil, prd, task,
		autonomouspkg.VerificationLLM{Source: autonomouspkg.VerifierSourceGlobal},
		autonomouspkg.DefaultConfig(), t.TempDir(), f.tryAdmit, resolve, compatible, fakeIsSessionKind)
	if ok || sel.Backend != "" || len(f.probes) != 0 {
		t.Fatalf("fixed mode: sel=%+v ok=%v probes=%v, want no policy decision and no probes", sel, ok, f.probes)
	}
}

func TestVerifierPolicyExplicitVerificationLLMBeatsPolicy(t *testing.T) {
	resolve, compatible := policyTestResolvers()
	cfg := autonomouspkg.DefaultConfig()
	cfg.VerifierPolicy = config.VerifierPolicyConfig{Mode: "balance", Backends: []string{"ollama-a", "ollama-b"}}
	f := newPolicyProbeFake()
	_, ok := policyVerifierSelection(nil, &autonomouspkg.PRD{ID: "p1"}, &autonomouspkg.Task{ID: "t1"},
		autonomouspkg.VerificationLLM{Source: autonomouspkg.VerifierSourceTask, Backend: "ollama-b"},
		cfg, t.TempDir(), f.tryAdmit, resolve, compatible, fakeIsSessionKind)
	if ok || len(f.probes) != 0 {
		t.Fatalf("ok=%v probes=%v, want the explicit override to skip the policy", ok, f.probes)
	}
}

func TestVerifierPolicyRetryAndFinalGateFlags(t *testing.T) {
	if verifierRetryOf(&autonomouspkg.Task{}) {
		t.Fatal("fresh task is not a retry")
	}
	if !verifierRetryOf(&autonomouspkg.Task{VerifierInfraFailures: 1}) {
		t.Fatal("infra failure counts as a retry")
	}
	if !verifierRetryOf(&autonomouspkg.Task{Verification: &autonomouspkg.VerificationResult{OK: false}}) {
		t.Fatal("a stored failed verification counts as a retry")
	}
	if verifierRetryOf(&autonomouspkg.Task{Verification: &autonomouspkg.VerificationResult{OK: true}}) {
		t.Fatal("a stored pass is not a retry")
	}
}

func TestVerifierPolicyClaudePickRecordsWindow(t *testing.T) {
	resolve, compatible := policyTestResolvers()
	cfg := autonomouspkg.DefaultConfig()
	cfg.VerifierPolicy = config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"claude-code"}}
	before := verifierClaudeWindow.Count(1 << 62)
	f := newPolicyProbeFake()
	sel, ok := policyVerifierSelection(nil, &autonomouspkg.PRD{ID: "p1"}, &autonomouspkg.Task{ID: "t1"},
		autonomouspkg.VerificationLLM{Source: autonomouspkg.VerifierSourceGlobal},
		cfg, t.TempDir(), f.tryAdmit, resolve, compatible, fakeIsSessionKind)
	if !ok || sel.Backend != "claude-code" || sel.Source != autonomouspkg.VerifierSourcePolicy {
		t.Fatalf("sel=%+v ok=%v", sel, ok)
	}
	if after := verifierClaudeWindow.Count(1 << 62); after != before+1 {
		t.Fatalf("claude window count %d -> %d, want +1", before, after)
	}
}
