// plan_coverage_gate_test.go — tests for the plan-coverage gate
// consumer side (plan_coverage_gate.go): the deterministic report
// computed + stored in ALL modes at decompose time, the Approve-block in
// "block" mode (default), the AcknowledgePlanCoverage escape hatch and
// its fingerprint scoping, and the off/warn non-gating behavior.
//
// Fixtures are deliberately minimal: the pure matching logic
// (CheckPlanCoverage / CheckPlanStructure) is covered by
// plan_coverage_test.go and plan_coverage_findings_test.go — here the
// goal is to verify the WIRING: decompose attaches the report, Approve
// consults it, and ack/ack-rejection behave per their contracts.
package autonomous

import (
	"strings"
	"testing"
)

// gateSpec has exactly three extractable numbered requirements.
const gateSpec = `1. implement login form
2. enforce rate limiting
3. emit audit trail entries
`

var (
	// gateFailingPlan covers requirements 1 and 3, silently drops 2.
	gateFailingPlan = `{"title":"feature","stories":[
		{"title":"login form work","tasks":[{"title":"implement login form","spec":"build the login form","verification_script":"go test ./..."}]},
		{"title":"audit trail work","tasks":[{"title":"emit audit trail entries","spec":"emit audit trail entries","verification_script":"go test ./..."}]}
	]}`
	// gateFailingPlanAlt covers requirements 2 and 3, silently drops 1 —
	// a different failing surface (different unmatched-requirement set),
	// i.e. a different fingerprint.
	gateFailingPlanAlt = `{"title":"feature","stories":[
		{"title":"rate limiting work","tasks":[{"title":"enforce rate limiting","spec":"enforce rate limiting","verification_script":"go test ./..."}]},
		{"title":"audit trail work","tasks":[{"title":"emit audit trail entries","spec":"emit audit trail entries","verification_script":"go test ./..."}]}
	]}`
	// gateCleanPlan covers all three requirements.
	gateCleanPlan = `{"title":"feature","stories":[
		{"title":"login form work","tasks":[{"title":"implement login form","spec":"build the login form","verification_script":"go test ./..."}]},
		{"title":"rate limiting work","tasks":[{"title":"enforce rate limiting","spec":"enforce rate limiting","verification_script":"go test ./..."}]},
		{"title":"audit trail work","tasks":[{"title":"emit audit trail entries","spec":"emit audit trail entries","verification_script":"go test ./..."}]}
	]}`
	// gateFailingStructurePlan covers every requirement but carries one
	// structural finding: a task with neither a verification script nor
	// an explicit B124 exemption reason.
	gateFailingStructurePlan = `{"title":"feature","stories":[
		{"title":"login form work","tasks":[{"title":"implement login form","spec":"build the login form"}]},
		{"title":"rate limiting work","tasks":[{"title":"enforce rate limiting","spec":"enforce rate limiting","verification_script":"go test ./..."}]},
		{"title":"audit trail work","tasks":[{"title":"emit audit trail entries","spec":"emit audit trail entries","verification_script":"go test ./..."}]}
	]}`
)

func newGateManager(t *testing.T, mode string, minRatio float64, planJSON string) *Manager {
	t.Helper()
	dir := t.TempDir()
	fakeLLM := func(req DecomposeRequest) (string, error) { return planJSON, nil }
	cfg := DefaultConfig()
	cfg.PlanCoverageCheck = mode
	cfg.PlanCoverageMinRatio = minRatio
	m, err := NewManager(dir, cfg, fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func decomposeGatePRD(t *testing.T, m *Manager) *PRD {
	t.Helper()
	draft, err := m.CreatePRD(gateSpec, "", "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd, err := m.Decompose(draft.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	return prd
}

func requireUnmatched(t *testing.T, prd *PRD, want string) {
	t.Helper()
	if prd.PlanCoverage == nil {
		t.Fatalf("no stored plan-coverage record after decompose")
	}
	found := false
	for _, u := range prd.PlanCoverage.Report.Unmatched {
		if u.Requirement == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unmatched requirements = %+v; want to contain %q", prd.PlanCoverage.Report.Unmatched, want)
	}
}

// TestGate_BlockModeStopsApprove — the core contract: block mode (the
// demonstration default), a failing stored report, Approve is refused
// with a message that names the unmatched requirement(s) and points at
// the acknowledgement action, and the PRD stays in needs_review.
func TestCoverageGate_BlockModeStopsApprove(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateFailingPlan)
	prd := decomposeGatePRD(t, m)
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %q, want needs_review", prd.Status)
	}
	if prd.PlanCoverage == nil || !prd.PlanCoverage.IsFailing() {
		t.Fatalf("stored report should fail: %+v", prd.PlanCoverage)
	}
	requireUnmatched(t, prd, "enforce rate limiting")

	_, err := m.Approve(prd.ID, "operator", "approve all")
	if err == nil {
		t.Fatal("Approve must be blocked in block mode with a failing report")
	}
	if !strings.Contains(err.Error(), "enforce rate limiting") {
		t.Fatalf("gate error must name the unmatched requirement: %v", err)
	}
	if !strings.Contains(err.Error(), "AcknowledgePlanCoverage") &&
		!strings.Contains(err.Error(), "acknowledge") {
		t.Fatalf("gate error must point at acknowledgement: %v", err)
	}
	again, _ := m.Store().GetPRD(prd.ID)
	if again.Status != PRDNeedsReview {
		t.Fatalf("after blocked Approve, status = %q, want needs_review", again.Status)
	}
}

// TestGate_AckThenApprove — the escape hatch: an explicit ack with
// actor + non-empty note of the exact failing surface lets Approve
// proceed; the PRD gets the approve Decision and approved status.
func TestCoverageGate_AckThenApprove(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateFailingPlan)
	prd := decomposeGatePRD(t, m)

	ackd, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "rate limiting is deferred — tracked separately")
	if err != nil {
		t.Fatalf("AcknowledgePlanCoverage: %v", err)
	}
	fp := ackd.PlanCoverage.Fingerprint()
	if got := ackd.PlanCoverage.Ack; got == nil || got.Fingerprint != fp {
		t.Fatalf("ack fingerprint = %+v, want %q", ackd.PlanCoverage.Ack, fp)
	}

	appr, err := m.Approve(prd.ID, "operator", "approved with ack")
	if err != nil {
		t.Fatalf("Approve after matching ack: %v", err)
	}
	if appr.Status != PRDApproved {
		t.Fatalf("status = %q, want approved", appr.Status)
	}
}

// TestGate_AckDoesNotFollowSurfaceChange — a re-decompose that lands a
// DIFFERENT failing surface (different unmatched-requirement set) has a
// different fingerprint; the carried-over ack no longer covers it and
// the gate blocks again until a fresh ack.
func TestCoverageGate_AckDoesNotFollowSurfaceChange(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateFailingPlan)
	prd := decomposeGatePRD(t, m)
	firstFP := prd.PlanCoverage.Fingerprint()

	if _, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "deferring rate limiting"); err != nil {
		t.Fatalf("first ack: %v", err)
	}
	if _, err := m.Approve(prd.ID, "operator", ""); err != nil {
		t.Fatalf("approve after first ack: %v", err)
	}

	// Re-decompose: the model this time drops requirement 1 instead of
	// requirement 2 — a different failing surface.
	m.decompose = func(req DecomposeRequest) (string, error) { return gateFailingPlanAlt, nil }
	prd2, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("re-decompose: %v", err)
	}
	if prd2.Status != PRDNeedsReview {
		t.Fatalf("after re-decompose, status = %q, want needs_review", prd2.Status)
	}
	if prd2.PlanCoverage == nil || !prd2.PlanCoverage.IsFailing() {
		t.Fatalf("re-decomposed report should fail: %+v", prd2.PlanCoverage)
	}
	if prd2.PlanCoverage.Fingerprint() == firstFP {
		t.Fatal("different failing surface must not share the first fingerprint")
	}
	// The old ack is carried forward (operator judgment preserved) but
	// no longer covers this surface.
	if prd2.PlanCoverage.Ack == nil {
		t.Fatal("prior ack should be carried forward on re-decompose")
	}
	if prd2.PlanCoverage.Ack.Fingerprint != firstFP {
		t.Fatalf("carried ack fingerprint = %q, want %q", prd2.PlanCoverage.Ack.Fingerprint, firstFP)
	}

	_, err = m.Approve(prd.ID, "operator", "approve again")
	if err == nil {
		t.Fatal("Approve must be blocked again for a changed failing surface")
	}
	if !strings.Contains(err.Error(), "implement login form") {
		t.Fatalf("gate error must name the new unmatched requirement: %v", err)
	}

	// A fresh ack of the NEW surface is required to proceed.
	fr, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "login form deferred to separate effort")
	if err != nil {
		t.Fatalf("fresh ack: %v", err)
	}
	if fr.PlanCoverage.Ack.Fingerprint != prd2.PlanCoverage.Fingerprint() {
		t.Fatalf("fresh ack fingerprint = %q, want %q", fr.PlanCoverage.Ack.Fingerprint, prd2.PlanCoverage.Fingerprint())
	}
	appr, err := m.Approve(prd.ID, "operator", "approved")
	if err != nil {
		t.Fatalf("approve after fresh ack: %v", err)
	}
	if appr.Status != PRDApproved {
		t.Fatalf("status = %q, want approved", appr.Status)
	}
}

// TestGate_WarnModeStoresButDoesNotGate — warn mode stores the (failing)
// report and lets Approve proceed without any acknowledgement.
func TestCoverageGate_WarnModeStoresButDoesNotGate(t *testing.T) {
	m := newGateManager(t, "warn", 1.0, gateFailingPlan)
	prd := decomposeGatePRD(t, m)
	if prd.PlanCoverage == nil || prd.PlanCoverage.Mode != "warn" {
		t.Fatalf("stored record: %+v", prd.PlanCoverage)
	}
	requireUnmatched(t, prd, "enforce rate limiting")

	appr, err := m.Approve(prd.ID, "operator", "warn accepted")
	if err != nil {
		t.Fatalf("Approve in warn mode must not be blocked: %v", err)
	}
	if appr.Status != PRDApproved {
		t.Fatalf("status = %q, want approved", appr.Status)
	}
}

// TestGate_OffModeStoresButDoesNotGate — off mode still computes and
// stores the report (audit visibility) but gates nothing.
func TestCoverageGate_OffModeStoresButDoesNotGate(t *testing.T) {
	m := newGateManager(t, "off", 1.0, gateFailingPlan)
	prd := decomposeGatePRD(t, m)
	if prd.PlanCoverage == nil || prd.PlanCoverage.Mode != "off" || !prd.PlanCoverage.IsFailing() {
		t.Fatalf("off mode must still store the failing report: %+v", prd.PlanCoverage)
	}
	requireUnmatched(t, prd, "enforce rate limiting")

	appr, err := m.Approve(prd.ID, "operator", "off mode")
	if err != nil {
		t.Fatalf("Approve in off mode must not be blocked: %v", err)
	}
	if appr.Status != PRDApproved {
		t.Fatalf("status = %q, want approved", appr.Status)
	}
}

// TestGate_PassingReportApprovesInBlockMode — block mode only blocks
// FAILING reports; a plan that covers the full spec approves without
// any acknowledgement.
func TestCoverageGate_PassingReportApprovesInBlockMode(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateCleanPlan)
	prd := decomposeGatePRD(t, m)
	if prd.PlanCoverage == nil || prd.PlanCoverage.IsFailing() {
		t.Fatalf("clean plan should not fail coverage: %+v", prd.PlanCoverage)
	}
	appr, err := m.Approve(prd.ID, "operator", "clean")
	if err != nil {
		t.Fatalf("Approve with a passing report: %v", err)
	}
	if appr.Status != PRDApproved {
		t.Fatalf("status = %q, want approved", appr.Status)
	}
}

// TestGate_StructuralFindingFailsAndGates — a fully requirement-covered
// plan still fails (and is blocked) when it carries a structural
// finding; the ack covers the finding surface.
func TestCoverageGate_StructuralFindingFailsAndGates(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateFailingStructurePlan)
	prd := decomposeGatePRD(t, m)
	pc := prd.PlanCoverage
	if pc == nil {
		t.Fatal("no stored record")
	}
	if len(pc.Report.Unmatched) != 0 {
		t.Fatalf("all requirements should be covered, got unmatched: %+v", pc.Report.Unmatched)
	}
	if len(pc.Findings) == 0 {
		t.Fatal("expected a structural finding (missing verification script/reason)")
	}
	if len(pc.Findings) != 1 || pc.Findings[0].Kind != FindingMissingVerification {
		t.Fatalf("findings = %+v, want exactly one missing_verification", pc.Findings)
	}
	if !pc.IsFailing() {
		t.Fatal("a plan with a structural finding must be failing")
	}

	_, err := m.Approve(prd.ID, "operator", "")
	if err == nil {
		t.Fatal("Approve must be blocked on structural finding")
	}
	if !strings.Contains(err.Error(), "1 structural finding") {
		t.Fatalf("gate error must name the finding count: %v", err)
	}

	if _, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "verification deferred"); err != nil {
		t.Fatalf("ack on finding surface: %v", err)
	}
	if apr, err := m.Approve(prd.ID, "operator", "ok"); err != nil || apr.Status != PRDApproved {
		t.Fatalf("approve after ack: err=%v status=%q", err, apr.Status)
	}
}

// TestGate_AckRequiresNoteAndNeedsReview — acknowledgement contract:
// a non-empty note is required, and it is only allowed from
// needs_review.
func TestCoverageGate_AckRequiresNoteAndNeedsReview(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateFailingPlan)
	prd := decomposeGatePRD(t, m)

	if _, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "  "); err == nil {
		t.Fatal("blank note must be rejected")
	} else if !strings.Contains(err.Error(), "non-empty note") {
		t.Fatalf("error must demand a non-empty note: %v", err)
	}

	// Approve in warn mode is open (no gate) — reach approved, then
	// acknowledge: not allowed outside needs_review.
	mw := newGateManager(t, "warn", 1.0, gateFailingPlan)
	prdw := decomposeGatePRD(t, mw)
	if _, err := mw.Approve(prdw.ID, "operator", "warn ok"); err != nil {
		t.Fatalf("approve in warn: %v", err)
	}
	if _, err := mw.AcknowledgePlanCoverage(prdw.ID, "operator", "late ack"); err == nil {
		t.Fatal("acknowledge from approved must be rejected")
	} else if !strings.Contains(err.Error(), "only allowed from needs_review") {
		t.Fatalf("error must state the status constraint: %v", err)
	}
}

// TestGate_AckRefusesWhenNotFailing — a passing stored record is
// nothing to acknowledge.
func TestCoverageGate_AckRefusesWhenNotFailing(t *testing.T) {
	m := newGateManager(t, "block", 1.0, gateCleanPlan)
	prd := decomposeGatePRD(t, m)
	if _, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "nothing to ack"); err == nil {
		t.Fatal("acknowledge of a passing report must be rejected")
	}
}

// TestPlanCoverage_IsFailingAndFingerprint — the record's own pinned
// predicate and stable fingerprint: same failing surface ⇒ same
// fingerprint (order-insensitive); any different requirement set or
// finding set ⇒ different fingerprint; a findings-only failure is a
// failure.
func TestPlanCoverage_IsFailingAndFingerprint(t *testing.T) {
	reqs := []RequirementVerdict{
		{Requirement: "enforce rate limiting", Verdict: CoverageMissing},
		{Requirement: "emit audit trail entries", Verdict: CoverageWeakened},
	}
	a := &PlanCoverage{
		Mode:     "block",
		MinRatio: 1.0,
		Report:   PlanCoverageReport{Covered: 1, Missing: 1, Weakened: 1, Unmatched: reqs},
	}
	b := &PlanCoverage{
		Mode:     "block",
		MinRatio: 1.0,
		Report: PlanCoverageReport{Covered: 1, Missing: 1, Weakened: 1,
			Unmatched: []RequirementVerdict{reqs[1], reqs[0]}}, // reversed order
	}
	if !a.IsFailing() || !b.IsFailing() {
		t.Fatal("both records should be failing")
	}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("same failing surface in different entry order must share a fingerprint")
	}

	diff := &PlanCoverage{
		Mode:     "block",
		MinRatio: 1.0,
		Report: PlanCoverageReport{Covered: 1, Missing: 1, Weakened: 1,
			Unmatched: []RequirementVerdict{
				{Requirement: "implement login form", Verdict: CoverageMissing},
				{Requirement: "emit audit trail entries", Verdict: CoverageWeakened},
			}},
	}
	if diff.Fingerprint() == a.Fingerprint() {
		t.Fatal("different unmatched-requirement set must change the fingerprint")
	}
	withFinding := &PlanCoverage{
		Mode:     "block",
		MinRatio: 1.0,
		Report:   PlanCoverageReport{Covered: 1, Missing: 1, Weakened: 1, Unmatched: reqs},
		Findings: []StructuralFinding{{Kind: FindingMissingVerification, Detail: "task t1 ..."}},
	}
	if withFinding.Fingerprint() == a.Fingerprint() {
		t.Fatal("a different structural-finding set must change the fingerprint")
	}
	if !withFinding.IsFailing() {
		t.Fatal("record with findings must be failing")
	}

	// Passing dimension: 100% covered, no findings.
	pass := &PlanCoverage{Mode: "block", MinRatio: 1.0, Report: PlanCoverageReport{Covered: 3}}
	if pass.IsFailing() {
		t.Fatal("fully-covered, finding-free record is not failing")
	}

	// Zero extractable requirements is not a ratio failure.
	zero := &PlanCoverage{Mode: "block", MinRatio: 1.0}
	if zero.IsFailing() {
		t.Fatal("zero-requirement record must not fail on the ratio dimension")
	}

	// MinRatio below 1.0 admits partial coverage.
	partial := &PlanCoverage{
		Mode:     "block",
		MinRatio: 0.5,
		Report: PlanCoverageReport{Covered: 2, Missing: 1,
			Unmatched: []RequirementVerdict{{Requirement: "x", Verdict: CoverageMissing}}},
	}
	if partial.IsFailing() {
		t.Fatalf("2/3 covered at min ratio 0.5 must not fail: %+v", partial)
	}
}
