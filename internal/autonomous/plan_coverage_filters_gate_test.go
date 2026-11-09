// plan_coverage_filters_gate_test.go — the gate consumer side of the two
// finding filters that landed in the config + resolver commits before
// this one: the manager computes the stored plan-coverage record WITH
// the filters in effect (suppression of created-file findings and
// scoped requirement extraction, both default on), stores how many
// findings each filter removed, pins the Filters under which the
// record was computed, and still gates on the GENUINE failures — a
// scoped requirement the plan never covered, or a file that is neither
// in the tree nor creatable by the plan.
//
// Fixtures are shaped like the real decomposed plans of 2026-10-30 —
// a long spec with a READ FIRST list, a SCOPE section and a
// Constraints section, and a story that adds new source files, a test
// file and a how-to doc — and the main spec is shaped like this
// feature's own request: a gate record that applies both filters and
// stores the suppressed counts. The pure matching logic is covered by
// plan_coverage_created_findings_test.go and
// plan_coverage_scope_report_test.go; here the goal is the WIRING:
// decompose computes through EffectivePlanCoverageFilters, the record
// carries Suppressed + Filters, block/ack behave on the genuine
// surface, and the Fingerprint ignores the filtered-out surface.
package autonomous

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// filtersEqual compares PlanCoverageFilters values (it carries a slice,
// so == is not available).
func filtersEqual(a, b config.PlanCoverageFilters) bool {
	return reflect.DeepEqual(a, b)
}

// filtersGateSpec is shaped like this feature's own request: the gate
// record applies both finding filters and stores the suppressed counts.
// READ FIRST is deliberately NOT a required section (the default
// scope is scope + constraints): its loose path and config-key tokens
// only surface as requirements under the OLD unscoped extraction,
// which is exactly the surface the scoping filter suppresses.
const filtersGateSpec = `READ FIRST: the coverage check in internal/autonomous/plan_coverage.go, and the config key plan_coverage_min_ratio it resolves.

SCOPE: add a gate record in a new source file internal/autonomous/plan_coverage_gate_record.go that applies both finding filters and stores how many findings they suppressed

Constraints: add a test file internal/autonomous/plan_coverage_gate_record_test.go that proves the record pins the filters in effect at compute time, and write the how-to doc docs/howto/gate-record.md explaining the stored counts
`

// filtersGateMissingReq is the one SCOPE requirement no task in
// filtersGatePlan covers — obligation-worded so scoping extracts it
// as a requirement, and a genuine gap scoping cannot hide because it
// sits in a required section.
const filtersGateMissingReq = " The record must expose its contents on the autonomous status endpoint"

// filtersGateSpecMissing extends the main spec's SCOPE section with
// that genuine, uncovered requirement.
const filtersGateSpecMissing = "READ FIRST: the coverage check in internal/autonomous/plan_coverage.go, and the config key plan_coverage_min_ratio it resolves.\n\n" +
	"SCOPE: add a gate record in a new source file internal/autonomous/plan_coverage_gate_record.go that applies both finding filters and stores how many findings they suppressed." +
	filtersGateMissingReq +
	"\n\nConstraints: add a test file internal/autonomous/plan_coverage_gate_record_test.go that proves the record pins the filters in effect at compute time, and write the how-to doc docs/howto/gate-record.md explaining the stored counts\n"

// filtersGatePlan is the 2026-10-30 shape: one story whose tasks each
// add one of the new source file, test file and how-to doc named in
// the spec, quoting the spec sentence each one fulfils. It covers all
// scoped requirements and creates all three new files, but names
// neither of the READ FIRST tokens (plan_coverage.go,
// plan_coverage_min_ratio) — those only surface under unscoped
// extraction.
const filtersGatePlan = `{"title":"gate record with filters","stories":[
	{"title":"gate record work","tasks":[
		{"title":"add the gate record type","files":["internal/autonomous/plan_coverage_gate_record.go"],"spec":"add a gate record in a new source file internal/autonomous/plan_coverage_gate_record.go that applies both finding filters and stores how many findings they suppressed","verification_script":"scripts/verify.sh"},
		{"title":"add the gate record test","files":["internal/autonomous/plan_coverage_gate_record_test.go"],"spec":"add a test file internal/autonomous/plan_coverage_gate_record_test.go that proves the record pins the filters in effect at compute time","verification_script":"scripts/verify.sh"},
		{"title":"write the gate record doc","files":["docs/howto/gate-record.md"],"spec":"write the how-to doc docs/howto/gate-record.md explaining the stored counts","verification_script":"scripts/verify.sh"}
	]}
]}`

const (
	// filtersGateUncreatableSpec references a file whose parent
	// directory exists nowhere and no task creates: outside the
	// created-file allowance.
	filtersGateUncreatableSpec = `SCOPE: reuse the resolver from internal/never_created_module/things.go for the filter settings

Constraints: add a test file internal/autonomous/plan_coverage_gate_record_test.go that proves the record pins the filters in effect at compute time
`
	filtersGateUncreatablePlan = `{"title":"gate record uncreatable file","stories":[
		{"title":"gate record work","tasks":[
			{"title":"reuse the resolver","files":["internal/autonomous/plan_coverage_gate_record.go"],"spec":"add a gate record in internal/autonomous/plan_coverage_gate_record.go that reuses the resolver from internal/never_created_module/things.go for the filter settings","verification_script":"scripts/verify.sh"},
			{"title":"add the gate record test","files":["internal/autonomous/plan_coverage_gate_record_test.go"],"spec":"add a test file internal/autonomous/plan_coverage_gate_record_test.go that proves the record pins the filters in effect at compute time","verification_script":"scripts/verify.sh"}
		]}
	]}`
)

// filtersGateProjectDir builds a temp project tree (as the real plans'
// trees had) with the two parent directories the fixture plans create
// files into, so the created-file allowance's existing-parent leg
// applies.
func filtersGateProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{filepath.Join("internal", "autonomous"), "docs/howto"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	writeFixtureVerifyScript(t, dir)
	return dir
}

// newFiltersGateManager builds a block-mode manager (min ratio 1.0)
// whose decompose returns planJSON for spec, having decomposed one PRD.
// suppressCreatedFiles / scopedRequirements set the two operator
// config keys; false pins that key to off (a nil pointer would
// resolve to on — the operator default).
func newFiltersGateManager(t *testing.T, spec, planJSON string, suppressCreatedFiles, scopedRequirements bool) *Manager {
	t.Helper()
	dir := t.TempDir()
	fakeLLM := func(req DecomposeRequest) (string, error) { return planJSON, nil }
	cfg := DefaultConfig()
	cfg.PlanCoverageCheck = "block"
	cfg.PlanCoverageMinRatio = 1.0
	if !suppressCreatedFiles || !scopedRequirements {
		off := false
		if !suppressCreatedFiles {
			cfg.PlanCoverageSuppressCreatedFiles = &off
		}
		if !scopedRequirements {
			cfg.PlanCoverageScopedRequirements = &off
		}
	}
	m, err := NewManager(dir, cfg, fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD(spec, filtersGateProjectDir(t), "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	return m
}

// filtersGatePRD returns the single PRD (and its stored record) a
// filters-gate manager decomposed in its store.
func filtersGatePRD(t *testing.T, m *Manager) *PRD {
	t.Helper()
	prds := m.Store().ListPRDs()
	if len(prds) != 1 {
		t.Fatalf("expected exactly one PRD in the store, got %d", len(prds))
	}
	if prds[0].PlanCoverage == nil {
		t.Fatal("no stored plan-coverage record after decompose")
	}
	return prds[0]
}

func requireUnmatchedContains(t *testing.T, pc *PlanCoverage, want string) {
	t.Helper()
	for _, u := range pc.Report.Unmatched {
		if strings.Contains(u.Requirement, want) {
			return
		}
	}
	t.Fatalf("stored record must list a requirement containing %q as unmatched; unmatched = %+v", want, pc.Report.Unmatched)
}

func requireNoUnmatchedContains(t *testing.T, pc *PlanCoverage, notWant string) {
	t.Helper()
	for _, u := range pc.Report.Unmatched {
		if strings.Contains(u.Requirement, notWant) {
			t.Fatalf("stored record must not list a requirement containing %q as unmatched; unmatched = %+v", notWant, pc.Report.Unmatched)
		}
	}
}

// unresolvedFileFindings counts unresolved_file findings whose detail
// mentions substr.
func unresolvedFileFindings(pc *PlanCoverage, substr string) int {
	n := 0
	for _, f := range pc.Findings {
		if f.Kind == FindingUnresolvedFile && strings.Contains(f.Detail, substr) {
			n++
		}
	}
	return n
}

// TestPlanCoverageFilters_GateCreatedFiles — a plan whose tasks create
// the spec's new files stores a NON-failing record with those findings
// counted, not flagged: the three new files (source file, test file,
// doc) are suppressed out of the failing surface.
func TestPlanCoverageFilters_GateCreatedFiles(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateSpec, filtersGatePlan, true, true)
	pc := filtersGatePRD(t, m).PlanCoverage
	if pc.IsFailing() {
		t.Fatalf("record must be non-failing with the created files suppressed: findings = %+v, unmatched = %+v",
			pc.Findings, pc.Report.Unmatched)
	}
	if pc.Suppressed.CreatedFiles < 3 {
		t.Fatalf("Suppressed.CreatedFiles = %d; want at least 3 (source file, test file, doc)", pc.Suppressed.CreatedFiles)
	}
	if unresolvedFileFindings(pc, "plan_coverage_gate_record.go") != 0 {
		t.Fatalf("plan-created files must not surface as unresolved findings: %+v", pc.Findings)
	}
}

// TestPlanCoverageFilters_GateStrayTokens — the READ FIRST loose
// tokens (a file path and a config key) produce NO unmatched
// requirements under scoped extraction, and the removed findings are
// counted in Suppressed.Requirements.
func TestPlanCoverageFilters_GateStrayTokens(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateSpec, filtersGatePlan, true, true)
	pc := filtersGatePRD(t, m).PlanCoverage
	requireNoUnmatchedContains(t, pc, "plan_coverage_min_ratio")
	requireNoUnmatchedContains(t, pc, "plan_coverage.go")
	if pc.Suppressed.Requirements < 2 {
		t.Fatalf("Suppressed.Requirements = %d; want at least 2 (the path token and the config-key token)", pc.Suppressed.Requirements)
	}
}

// TestPlanCoverageFilters_GateGenuineMissingRequirement — an
// uncovered requirement in a required section (SCOPE) still fails the
// record and still appears in Unmatched: the filters only remove the
// surface they are built to remove, never a genuine gap.
func TestPlanCoverageFilters_GateGenuineMissingRequirement(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateSpecMissing, filtersGatePlan, true, true)
	pc := filtersGatePRD(t, m).PlanCoverage
	if !pc.IsFailing() {
		t.Fatalf("a requirement in the SCOPE section that no task covers must fail the record: %+v", pc)
	}
	requireUnmatchedContains(t, pc, "autonomous status endpoint")
}

// TestPlanCoverageFilters_GateUncreatableFile — a referenced file whose
// parent directory exists nowhere and no task creates is outside the
// created-file allowance: the record still fails with the
// unresolved_file finding.
func TestPlanCoverageFilters_GateUncreatableFile(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateUncreatableSpec, filtersGateUncreatablePlan, true, true)
	pc := filtersGatePRD(t, m).PlanCoverage
	if !pc.IsFailing() {
		t.Fatalf("an uncreatable referenced file must still fail the record: %+v", pc)
	}
	if unresolvedFileFindings(pc, "internal/never_created_module/things.go") != 1 {
		t.Fatalf("want exactly one unresolved_file finding naming internal/never_created_module/things.go; findings = %+v", pc.Findings)
	}
}

// TestPlanCoverageFilters_GateFiltersOffReproduceFindings — with both
// config keys off, every finding these filters suppress comes back:
// the created files surface as unresolved file findings, the stray
// READ FIRST tokens surface as unmatched requirements, and the
// record's Suppressed is zero.
func TestPlanCoverageFilters_GateFiltersOffReproduceFindings(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateSpec, filtersGatePlan, false, false)
	pc := filtersGatePRD(t, m).PlanCoverage
	if pc.Suppressed.Total() != 0 {
		t.Fatalf("with both filters off the record's Suppressed must be zero, got %+v", pc.Suppressed)
	}
	if unresolvedFileFindings(pc, "plan_coverage_gate_record.go") == 0 &&
		unresolvedFileFindings(pc, "plan_coverage_gate_record_test.go") == 0 &&
		unresolvedFileFindings(pc, "gate-record.md") == 0 {
		t.Fatalf("with suppression off the created files must surface as unresolved findings; findings = %+v", pc.Findings)
	}
	requireUnmatchedContains(t, pc, "plan_coverage_min_ratio")
	if !pc.IsFailing() {
		t.Fatal("with both filters off the same plan must fail again")
	}
}

// TestPlanCoverageFilters_GatePinnedFilters — the record's Filters
// equal the settings in effect at compute time (the resolved
// defaults), and a later config change must not change the stored
// record: it keeps the filters and suppressed counts it was computed
// under, even though a future record would compute different ones.
func TestPlanCoverageFilters_GatePinnedFilters(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateSpec, filtersGatePlan, true, true)
	prd := filtersGatePRD(t, m)
	record := prd.PlanCoverage
	want := config.EffectivePlanCoverageFilters(nil, nil, "", 0) // operator defaults
	if !filtersEqual(record.Filters, want) {
		t.Fatalf("pinned Filters = %+v; want the resolved defaults %+v", record.Filters, want)
	}
	suppressedAtCompute := record.Suppressed

	// A later config flip changes what a FUTURE record would compute
	// under ...
	cfg := m.Config()
	cfg.PlanCoverageRequirementSections = "parity"
	m.SetConfig(cfg)
	if got := m.EffectivePlanCoverageFilters(); filtersEqual(got, want) {
		t.Fatalf("after the config change EffectivePlanCoverageFilters = %+v; want a different resolution", got)
	}
	// ... but must leave the already-stored record untouched.
	again, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD(%s)", prd.ID)
	}
	if !filtersEqual(again.PlanCoverage.Filters, want) {
		t.Fatalf("the stored record's Filters changed after a config change: %+v", again.PlanCoverage.Filters)
	}
	if again.PlanCoverage.Suppressed != suppressedAtCompute {
		t.Fatalf("the stored record's Suppressed changed after a config change: %+v", again.PlanCoverage.Suppressed)
	}
}

// TestPlanCoverageFilters_GateAckThenApprove — a record failing on a
// GENUINE surface (a scoped requirement the plan dropped) is blocked
// by Approve until an explicit AcknowledgePlanCoverage, exactly like
// the pre-filter gate: the filters never swallow a real gap, and the
// escape hatch still works on that genuine surface.
func TestPlanCoverageFilters_GateAckThenApprove(t *testing.T) {
	m := newFiltersGateManager(t, filtersGateSpecMissing, filtersGatePlan, true, true)
	prd := filtersGatePRD(t, m)
	if !prd.PlanCoverage.IsFailing() {
		t.Fatal("the record must fail on the genuine missing requirement")
	}
	_, err := m.Approve(prd.ID, "operator", "approve all")
	if err == nil {
		t.Fatal("Approve must be blocked in block mode with a failing record")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "status endpoint") {
		t.Fatalf("gate error must name the genuine unmatched requirement: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "acknowledge") {
		t.Fatalf("gate error must point at acknowledgement: %v", err)
	}

	ackd, err := m.AcknowledgePlanCoverage(prd.ID, "operator", "the status endpoint is deferred — tracked separately")
	if err != nil {
		t.Fatalf("AcknowledgePlanCoverage: %v", err)
	}
	if ackd.PlanCoverage.Ack == nil {
		t.Fatal("the ack must be stored on the record")
	}
	approved, err := m.Approve(prd.ID, "operator", "approve after ack")
	if err != nil {
		t.Fatalf("Approve after an ack of the genuine surface: %v", err)
	}
	if approved.Status != PRDApproved {
		t.Fatalf("status after ack+approve = %q; want approved", approved.Status)
	}
}

// TestPlanCoverageFilters_GateFingerprintIgnoresSuppressedAndFilters —
// two records with the same failing surface but different Suppressed
// counts and different pinned Filters hash to the SAME Fingerprint, so
// an acknowledgement of the genuine failure surface stays valid when
// only the suppressed counts or the filters change; widening the
// genuine surface still invalidates it.
func TestPlanCoverageFilters_GateFingerprintIgnoresSuppressedAndFilters(t *testing.T) {
	baseRep := PlanCoverageReport{
		Covered: 2,
		Missing: 1,
		Unmatched: []RequirementVerdict{{
			Requirement: "publish the stored record on the autonomous status endpoint",
			Verdict:     CoverageMissing,
		}},
	}
	baseFindings := []StructuralFinding{{Kind: FindingMissingVerification, Detail: "task t does not verify"}}

	defaults := config.EffectivePlanCoverageFilters(nil, nil, "", 0)
	base := &PlanCoverage{Mode: "block", MinRatio: 1.0, Report: baseRep, Findings: baseFindings,
		Suppressed: PlanCoverageSuppressed{}, Filters: defaults}
	other := &PlanCoverage{Mode: "block", MinRatio: 1.0, Report: baseRep, Findings: baseFindings,
		Suppressed: PlanCoverageSuppressed{CreatedFiles: 3, Requirements: 2},
		Filters:    config.PlanCoverageFilters{SuppressCreatedFiles: false, ScopedRequirements: false, RequirementSections: []string{"parity"}, MinRequirementChars: 80}}

	if base.Suppressed == other.Suppressed || filtersEqual(base.Filters, other.Filters) {
		t.Fatal("test setup: the two records must differ in Suppressed and Filters")
	}
	if base.Fingerprint() != other.Fingerprint() {
		t.Fatalf("records differing only in Suppressed and Filters must share a Fingerprint: %q vs %q",
			base.Fingerprint(), other.Fingerprint())
	}

	// An Ack recorded for the genuine surface therefore matches BOTH —
	// matching is Ack.Fingerprint == the record's Fingerprint.
	ack := &PlanCoverageAck{By: "operator", Note: "the status endpoint is deferred", Fingerprint: base.Fingerprint()}
	if ack.Fingerprint != base.Fingerprint() || ack.Fingerprint != other.Fingerprint() {
		t.Fatal("an existing Ack of the genuine surface must still match the record after only Suppressed/Filters changed")
	}

	// Widening the genuine surface changes the Fingerprint, so the same
	// Ack no longer matches.
	widened := *base
	widenedReport := baseRep
	widenedReport.Unmatched = append(append([]RequirementVerdict{}, widenedReport.Unmatched...),
		RequirementVerdict{Requirement: "add a second status endpoint for the supervisor", Verdict: CoverageMissing})
	widened.Report = widenedReport
	if ack.Fingerprint == widened.Fingerprint() {
		t.Fatal("a widened failing surface must change the Fingerprint; the old Ack would otherwise still match")
	}
}
