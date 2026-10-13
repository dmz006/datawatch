// BL410 Phase 1 — retry-policy resolution + infra auto-retry tests.
package autonomous

import (
	"context"
	"testing"
)

func TestResolveRetryPolicy_ThreeTierPrecedence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"infra": 2}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := m.CreateRetryProfile("p", "", map[string]int{"infra": 4}, 0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	prd := &PRD{}
	if got := m.resolveRetryPolicy(prd, "infra"); got != 2 {
		t.Errorf("global fallback: got %d, want 2", got)
	}

	prd.RetryProfile = prof.ID
	if got := m.resolveRetryPolicy(prd, "infra"); got != 4 {
		t.Errorf("profile should beat global: got %d, want 4", got)
	}

	prd.RetryPolicy = map[string]int{"infra": 9}
	if got := m.resolveRetryPolicy(prd, "infra"); got != 9 {
		t.Errorf("explicit PRD override should beat profile: got %d, want 9", got)
	}
}

func TestResolveRetryPolicy_ContentAndPolicyFallBackToAutoFixRetries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 3
	// Deliberately no "content"/"policy" key in RetryPolicy — the
	// backward-compat anchor: a PRD that never touches this feature
	// gets byte-for-byte today's AutoFixRetries count.
	cfg.RetryPolicy = map[string]int{}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{}
	if got := m.resolveRetryPolicy(prd, "content"); got != 3 {
		t.Errorf("content should fall back to AutoFixRetries: got %d, want 3", got)
	}
	if got := m.resolveRetryPolicy(prd, "policy"); got != 3 {
		t.Errorf("policy should fall back to AutoFixRetries: got %d, want 3", got)
	}
	if got := m.resolveRetryPolicy(prd, "scope"); got != 0 {
		t.Errorf("scope has no AutoFixRetries fallback, want 0, got %d", got)
	}
}

func TestResolveRetryCeiling_DefaultAndOverrides(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{}
	if got := m.resolveRetryCeiling(prd); got != 5 {
		t.Errorf("default ceiling: got %d, want 5 (operator's own 'normal release failing >5x' framing)", got)
	}
	prd.RetryCeilingTotal = 30 // the CVE-remediation example from the plan
	if got := m.resolveRetryCeiling(prd); got != 30 {
		t.Errorf("PRD override: got %d, want 30", got)
	}
}

func TestResolveInfraEscalateAfter_Default(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.resolveInfraEscalateAfter(&PRD{}); got != 2 {
		t.Errorf("default: got %d, want 2", got)
	}
}

func TestResolveContentAutoFixEnabled_PRDOverrideBeatsGlobal(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{}
	if !m.resolveContentAutoFixEnabled(prd) {
		t.Error("default should be true (operator's 'work auto the default' decision)")
	}
	off := false
	prd.ContentAutoFixEnabled = &off
	if m.resolveContentAutoFixEnabled(prd) {
		t.Error("explicit PRD override to false must win")
	}
}

func TestResolveAutoFixSkipApproval_PRDOverrideBeatsGlobal(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{}
	if !m.resolveAutoFixSkipApproval(prd) {
		t.Error("default should be true (skip approval, fully unattended)")
	}
	off := false
	prd.AutoFixSkipApproval = &off
	if m.resolveAutoFixSkipApproval(prd) {
		t.Error("explicit PRD override to false must win")
	}
}

func TestRunGuardrailWithInfraRetry_IncidentReproduction(t *testing.T) {
	// The exact motivating incident: an infra-category block (a
	// session timeout) must not require operator action. Attempts 1-3
	// fail infra with the original config; attempt 4 (escalated, per
	// the default InfraRetryEscalateAfter=2) succeeds.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1", GuardrailBackend: "flaky-backend", GuardrailModel: "flaky-model"}
	var seenBackends []string
	calls := 0
	fn := func(ctx context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		seenBackends = append(seenBackends, inv.GuardrailBackend)
		if calls < 4 {
			return GuardrailVerdict{Outcome: "block", Category: "infra", Summary: "session timed out"}, nil
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	}
	inv := GuardrailInvocation{GuardrailBackend: "flaky-backend", GuardrailModel: "flaky-model"}
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Outcome != "pass" {
		t.Fatalf("want eventual pass with zero operator action, got %+v", v)
	}
	if calls != 4 {
		t.Fatalf("want 4 calls (1 original + 3 retries), got %d", calls)
	}
	// Attempts 1-3 keep the original backend (escalate-after=2 means
	// attempts 2 and 3 still use it); only a 4th would have escalated,
	// but it succeeded first.
	for i := 0; i < 3; i++ {
		if seenBackends[i] != "flaky-backend" {
			t.Errorf("attempt %d backend = %q, want original \"flaky-backend\" (not yet escalated)", i+1, seenBackends[i])
		}
	}
	if prd.RetryCountsByCategory["infra"] != 3 {
		t.Errorf("want 3 consumed infra retries, got %d", prd.RetryCountsByCategory["infra"])
	}
}

func TestRunGuardrailWithInfraRetry_EscalatesBackendAfterThreshold(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1", RetryCeilingTotal: 10}
	var seenBackends []string
	calls := 0
	fn := func(ctx context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		seenBackends = append(seenBackends, inv.GuardrailBackend)
		return GuardrailVerdict{Outcome: "block", Category: "infra", Summary: "still broken"}, nil
	}
	inv := GuardrailInvocation{GuardrailBackend: "original-backend"}
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, inv, fn)
	if err != nil {
		t.Fatal(err)
	}
	if v.Outcome != "block" {
		t.Fatalf("want a block once budget is exhausted, got %+v", v)
	}
	// Default infra budget is 4, InfraRetryEscalateAfter is 2: the
	// first call plus 2 same-config retries (attempts 1-2) keep the
	// original backend; attempt 3 onward (escalated := attempt >
	// InfraRetryEscalateAfter) drops to the global default. Budget
	// exhausts after 4 consumed retries, so 5 calls total: 3 original,
	// 2 escalated.
	if calls != 5 {
		t.Fatalf("want 5 calls (1 original + 4 retries, default infra budget), got %d", calls)
	}
	for i := 0; i < 3; i++ {
		if seenBackends[i] != "original-backend" {
			t.Errorf("call %d backend = %q, want \"original-backend\" (not yet escalated)", i+1, seenBackends[i])
		}
	}
	for i := 3; i < 5; i++ {
		if seenBackends[i] != "" {
			t.Errorf("call %d backend = %q, want \"\" (escalated to global default)", i+1, seenBackends[i])
		}
	}
}

func TestRunGuardrailWithInfraRetry_ContentBlockNeverRetries(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1"}
	calls := 0
	fn := func(ctx context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return GuardrailVerdict{Outcome: "block", Summary: "real AGENT.md violation", Issues: []string{"missing version bump"}}, nil
	}
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, GuardrailInvocation{}, fn)
	_ = v
	_ = err
	if calls != 1 {
		t.Fatalf("a content-category block (empty Category) must never auto-retry in Phase 1, got %d calls", calls)
	}
}

func TestRetryProfile_CRUD(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	p, err := m.CreateRetryProfile("cve-work", "many iterations expected", map[string]int{"infra": 10, "content": 10}, 30, 3, &enabled, &enabled)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m.GetRetryProfile(p.ID)
	if !ok || got.CeilingTotal != 30 {
		t.Fatalf("GetRetryProfile: got %+v, ok=%v", got, ok)
	}
	if len(m.ListRetryProfiles()) != 1 {
		t.Fatalf("want 1 profile listed")
	}
	disabled := false
	updated, err := m.UpdateRetryProfile(p.ID, "cve-work-2", "", map[string]int{"infra": 5}, 20, 2, &disabled, &disabled)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "cve-work-2" || updated.CeilingTotal != 20 || *updated.ContentAutoFixEnabled != false {
		t.Fatalf("UpdateRetryProfile: got %+v", updated)
	}
	if err := m.DeleteRetryProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.GetRetryProfile(p.ID); ok {
		t.Fatal("profile should be gone after delete")
	}
}
