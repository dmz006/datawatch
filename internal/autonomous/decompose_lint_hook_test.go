// decompose_lint_hook_test.go — the decompose-time decompose-lint hook
// and the read-only LintPRD dry run (see decompose_lint_hook.go).
//
// Coverage: off = complete no-op (no record, no retry, no template
// apply); shadow = records the finding set without retrying; enforce =
// re-runs the decomposer with the findings fed back until a pass is
// clean or the retry budget is spent, then records what remained and
// still stores the plan (the check never fails a decompose).
package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// decomposeLintHookBadPlanJSON is a plan whose task spec invokes the
// hosting CLI — the default pack's runs_hosting_cli rule (severity
// block) fires. It also carries no verification script, so the
// no_verification_script warn fires alongside it in every mode.
const decomposeLintHookBadPlanJSON = `{"title":"Ship it","stories":[{"title":"S","tasks":[{"title":"Task","spec":"Open the PR with gh pr create after the checks pass"}]}]}`

// decomposeLintHookGoodPlanJSON passes the pack with zero block
// findings (only the no_verification_script warn): the task declares a
// verification script and trips no rule.
const decomposeLintHookGoodPlanJSON = `{"title":"Ship it","stories":[{"title":"S","tasks":[{"title":"Task","spec":"Record the handoff note for the operator","verification_script":".datawatch/verify/hook-note.sh"}]}]}`

// lintHookFindingRule returns the finding with rule id ruleID from f, if
// present.
func lintHookFindingRule(f []LintFinding, ruleID string) (LintFinding, bool) {
	for _, x := range f {
		if x.RuleID == ruleID {
			return x, true
		}
	}
	return LintFinding{}, false
}

// TestDecomposeLintHook_ShadowRecordsFindingsNoRetry: shadow mode runs
// one lint pass, records the findings (block and warn alike) on the
// stored PRD, and never re-runs the decomposer.
func TestDecomposeLintHook_ShadowRecordsFindingsNoRetry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{
		Mode:       config.DecomposeLintModeShadow,
		MaxRetries: config.DefaultDecomposeLintMaxRetries,
	}
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return decomposeLintHookBadPlanJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v (shadow findings must never fail the decompose)", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review (shadow records, never blocks)", updated.Status)
	}
	if calls != 1 {
		t.Fatalf("decompose calls = %d, want 1 (shadow never retries)", calls)
	}
	rec := updated.DecomposeLint
	if rec == nil {
		t.Fatal("PRD.DecomposeLint is nil, want the recorded pass")
	}
	if rec.Mode != config.DecomposeLintModeShadow {
		t.Errorf("record mode = %q, want shadow", rec.Mode)
	}
	if rec.Attempts != 1 {
		t.Errorf("record attempts = %d, want 1 (shadow runs one pass)", rec.Attempts)
	}
	bf, ok := lintHookFindingRule(rec.Findings, "runs_hosting_cli")
	if !ok {
		t.Fatalf("record findings = %+v, want the runs_hosting_cli block finding", rec.Findings)
	}
	if bf.Severity != config.DecomposeLintSeverityBlock {
		t.Errorf("finding severity = %q, want block", bf.Severity)
	}
	if _, ok := lintHookFindingRule(rec.Findings, "no_verification_script"); !ok {
		t.Errorf("record findings = %+v, want the no_verification_script warn too (shadow records everything)", rec.Findings)
	}
	// The hook must have persisted the record on the stored row.
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.DecomposeLint == nil {
		t.Fatal("stored PRD.DecomposeLint is nil, want the recorded pass")
	}
	if len(stored.DecomposeLint.Findings) != len(rec.Findings) {
		t.Errorf("stored findings = %d, want %d (same pass record)", len(stored.DecomposeLint.Findings), len(rec.Findings))
	}
}

// TestDecomposeLintHook_EnforceRetriesThenAttaches: enforce mode
// re-runs the decomposer with the current findings fed back into the
// prompt; when a retry produces a clean pass the clean plan is stored
// and the record shows the final (warning-only) findings.
func TestDecomposeLintHook_EnforceRetriesThenAttaches(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{
		Mode:       config.DecomposeLintModeEnforce,
		MaxRetries: 2,
	}
	calls := 0
	var prompts []string
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		prompts = append(prompts, req.Spec)
		if calls <= 2 {
			return decomposeLintHookBadPlanJSON, nil
		}
		return decomposeLintHookGoodPlanJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if calls != 3 {
		t.Fatalf("decompose calls = %d, want 3 (initial + 2 enforced retries)", calls)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	// The stored plan is the clean retry result.
	if len(updated.Story) != 1 {
		t.Fatalf("story count = %d, want 1", len(updated.Story))
	}
	if got := updated.Story[0].Tasks[0].Spec; got != "Record the handoff note for the operator" {
		t.Errorf("stored task spec = %q, want the clean retry plan's task", got)
	}
	rec := updated.DecomposeLint
	if rec == nil {
		t.Fatal("PRD.DecomposeLint is nil, want the recorded enforce pass")
	}
	if rec.Mode != config.DecomposeLintModeEnforce {
		t.Errorf("record mode = %q, want enforce", rec.Mode)
	}
	if rec.Attempts != 3 {
		t.Errorf("record attempts = %d, want 3 (1 initial + 2 retries)", rec.Attempts)
	}
	if s := SummaryOf(rec.Findings); s.Block != 0 {
		t.Errorf("final findings block = %d (findings=%+v), want 0 — the last pass is clean", s.Block, rec.Findings)
	}
	if _, ok := lintHookFindingRule(rec.Findings, "no_verification_script"); ok {
		t.Errorf("final findings = %+v, want no warn (the clean task declares a script)", rec.Findings)
	}
	// The prompts of both retry attempts (calls 2 and 3) carried the
	// findings of the pass they follow; the initial prompt did not.
	if len(prompts) != 3 {
		t.Fatalf("decompose prompts = %d, want 3", len(prompts))
	}
	if strings.Contains(prompts[0], "Plan check findings from the previous plan") {
		t.Errorf("initial prompt carried the findings feed-back block: %q", prompts[0])
	}
	for i, p := range prompts[1:] {
		if !strings.Contains(p, "Plan check findings from the previous plan") {
			t.Errorf("retry prompt %d did not carry the findings feed-back block: %q", i+1, p)
		}
		if !strings.Contains(p, "runs_hosting_cli") {
			t.Errorf("retry prompt %d did not name the offending rule: %q", i+1, p)
		}
	}
}

// TestDecomposeLintHook_EnforceBudgetExhaustedStillStoresPlan: when the
// retry budget is spent and the plan still carries block findings, the
// last plan is stored untouched (the check never fails a decompose) and
// the record keeps the residual block findings.
func TestDecomposeLintHook_EnforceBudgetExhaustedStillStoresPlan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{
		Mode:       config.DecomposeLintModeEnforce,
		MaxRetries: 1,
	}
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return decomposeLintHookBadPlanJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v (exhausted enforce budget must not fail the decompose)", err)
	}
	if calls != 2 {
		t.Fatalf("decompose calls = %d, want 2 (initial + 1 enforced retry)", calls)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review (residual block findings attach, never block)", updated.Status)
	}
	rec := updated.DecomposeLint
	if rec == nil {
		t.Fatal("PRD.DecomposeLint is nil, want the recorded enforce pass")
	}
	if rec.Attempts != 2 {
		t.Errorf("record attempts = %d, want 2", rec.Attempts)
	}
	if s := SummaryOf(rec.Findings); s.Block == 0 {
		t.Errorf("final findings block = 0, want the residual runs_hosting_cli block finding kept on the record")
	}
	if _, ok := lintHookFindingRule(rec.Findings, "runs_hosting_cli"); !ok {
		t.Errorf("record findings = %+v, want runs_hosting_cli (residual)", rec.Findings)
	}
}

// TestDecomposeLintHook_EnforceMaxRetriesZero: with a zero budget
// enforce mode behaves like shadow for the retry question (no
// re-run) but keeps the enforce record mode.
func TestDecomposeLintHook_EnforceMaxRetriesZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{
		Mode:       config.DecomposeLintModeEnforce,
		MaxRetries: 0,
	}
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return decomposeLintHookBadPlanJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if calls != 1 {
		t.Fatalf("decompose calls = %d, want 1 (max_retries=0 never retries)", calls)
	}
	rec := updated.DecomposeLint
	if rec == nil {
		t.Fatal("PRD.DecomposeLint is nil, want the recorded enforce pass")
	}
	if rec.Mode != config.DecomposeLintModeEnforce {
		t.Errorf("record mode = %q, want enforce (zero budget is still an enforce record)", rec.Mode)
	}
	if rec.Attempts != 1 {
		t.Errorf("record attempts = %d, want 1", rec.Attempts)
	}
	if SummaryOf(rec.Findings).Block == 0 {
		t.Error("record findings carry no block, want the residual runs_hosting_cli finding")
	}
}

// TestDecomposeLintHook_OffRecordsNothing: off is a complete no-op —
// one decompose call, no record on the stored PRD.
func TestDecomposeLintHook_OffRecordsNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{
		Mode:       config.DecomposeLintModeOff,
		MaxRetries: config.DefaultDecomposeLintMaxRetries,
	}
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return decomposeLintHookBadPlanJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if calls != 1 {
		t.Fatalf("decompose calls = %d, want 1 (off never retries)", calls)
	}
	if updated.DecomposeLint != nil {
		t.Fatalf("PRD.DecomposeLint = %+v, want nil (off records nothing)", updated.DecomposeLint)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.DecomposeLint != nil {
		t.Fatalf("stored PRD.DecomposeLint = %+v, want nil (off records nothing)", stored.DecomposeLint)
	}
}

// TestDecomposeLintPRD_DryRunReportsStoredPlan: LintPRD lints the PRD's stored
// stories with the effective rules and never mutates the PRD or calls
// the decomposer.
func TestDecomposeLintPRD_DryRunReportsStoredPlan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{
		Mode:       config.DecomposeLintModeShadow,
		MaxRetries: config.DefaultDecomposeLintMaxRetries,
	}
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return "", nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "S", Tasks: []Task{{Title: "Task", Spec: "Open the PR with gh pr create after the checks pass"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}

	rep, err := m.LintPRD(prd.ID)
	if err != nil {
		t.Fatalf("LintPRD: %v", err)
	}
	if rep.PRD != prd.ID {
		t.Errorf("report prd = %q, want %q", rep.PRD, prd.ID)
	}
	if rep.Mode != config.DecomposeLintModeShadow {
		t.Errorf("report mode = %q, want shadow (the mode in effect)", rep.Mode)
	}
	wantRules := len(EffectiveRules(cfg.DecomposeLint))
	if wantRules == 0 {
		t.Fatal("EffectiveRules is empty, want the default pack (fixture bug)")
	}
	if rep.RuleCount != wantRules {
		t.Errorf("report rule count = %d, want %d (the effective rule set)", rep.RuleCount, wantRules)
	}
	bf, ok := lintHookFindingRule(rep.Findings, "runs_hosting_cli")
	if !ok {
		t.Fatalf("report findings = %+v, want the runs_hosting_cli block finding", rep.Findings)
	}
	if bf.Severity != config.DecomposeLintSeverityBlock {
		t.Errorf("finding severity = %q, want block", bf.Severity)
	}
	if rep.Summary.Block != 1 || rep.Summary.Warn != 1 {
		t.Errorf("report summary = %+v, want block=1 warn=1 (runs_hosting_cli + no_verification_script)", rep.Summary)
	}
	// LintPRD is a dry run: the PRD is untouched and the decomposer is
	// never called.
	if calls != 0 {
		t.Fatalf("decompose calls = %d, want 0 (LintPRD never calls the decomposer)", calls)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.Status != PRDDraft {
		t.Errorf("stored status = %s, want draft (dry run must not move the PRD)", stored.Status)
	}
	if stored.DecomposeLint != nil {
		t.Errorf("stored PRD.DecomposeLint = %+v, want nil (dry run writes nothing)", stored.DecomposeLint)
	}
	if len(stored.Story) != 1 || stored.Story[0].Tasks[0].Spec != "Open the PR with gh pr create after the checks pass" {
		t.Errorf("stored plan = %+v, want unchanged (dry run must not re-plan)", stored.Story)
	}
}

// TestDecomposeLintPRD_DryRunCleanPlan: a stored plan that trips no rule reports
// an empty finding set and an empty summary.
func TestDecomposeLintPRD_DryRunCleanPlan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{Mode: config.DecomposeLintModeShadow}
	m, err := NewManager(t.TempDir(), cfg, func(req DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "S", Tasks: []Task{{Title: "Task", Spec: "Record the handoff note for the operator", VerificationScript: ".datawatch/verify/hook-note.sh"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	rep, err := m.LintPRD(prd.ID)
	if err != nil {
		t.Fatalf("LintPRD: %v", err)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("clean plan findings = %+v, want none", rep.Findings)
	}
	if rep.Summary != (LintSummary{}) {
		t.Errorf("clean plan summary = %+v, want empty", rep.Summary)
	}
}

// TestDecomposeLintPRD_OffModeStillReports: the report is mode-agnostic — off
// still describes what a run would record (the record simply never
// happens at decompose time).
func TestDecomposeLintPRD_OffModeStillReports(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DecomposeLint = config.DecomposeLintConfig{Mode: config.DecomposeLintModeOff}
	m, err := NewManager(t.TempDir(), cfg, func(req DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose the ship-it feature", "/p", "", "", "")
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "S", Tasks: []Task{{Title: "Task", Spec: "Open the PR with gh pr create after the checks pass"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	rep, err := m.LintPRD(prd.ID)
	if err != nil {
		t.Fatalf("LintPRD: %v", err)
	}
	if rep.Mode != config.DecomposeLintModeOff {
		t.Errorf("report mode = %q, want off", rep.Mode)
	}
	if _, ok := lintHookFindingRule(rep.Findings, "runs_hosting_cli"); !ok {
		t.Errorf("report findings = %+v, want the runs_hosting_cli finding (the report describes what a run would record)", rep.Findings)
	}
}

// TestDecomposeLintPRD_UnknownPRDErrors: a dry run over an unknown PRD id is an
// error, not an empty report.
func TestDecomposeLintPRD_UnknownPRDErrors(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), func(req DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := m.LintPRD("no-such-prd"); err == nil {
		t.Fatal("LintPRD on an unknown PRD: want error, got nil")
	}
}
