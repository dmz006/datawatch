// B124 extension — PRD-completion rules review. See rules_review.go's
// own doc comment for the design rationale (judgment, not scripted).
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newRulesReviewManager returns a Manager backed by a real store. A bare
// &Manager{} is enough for the suffix tests, but runPRDCompletionRulesReview
// now persists every verdict it records, so its tests need a store to save to.
func newRulesReviewManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	st, err := NewStore(filepath.Join(t.TempDir(), "prds.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &Manager{cfg: cfg, store: st}
}

func TestRulesReviewSuffix_EmptyProjectDir(t *testing.T) {
	m := &Manager{cfg: Config{}}
	if got := m.rulesReviewSuffix(&PRD{}); got != "" {
		t.Errorf("want empty suffix for an empty ProjectDir, got %q", got)
	}
}

func TestRulesReviewSuffix_NoRulesOrContextFilePresent(t *testing.T) {
	m := &Manager{cfg: Config{}}
	dir := t.TempDir() // no AGENT.md, no CONTEXT.md
	if got := m.rulesReviewSuffix(&PRD{ProjectDir: dir}); got != "" {
		t.Errorf("want empty suffix when neither configured file exists, got %q", got)
	}
}

func TestRulesReviewSuffix_PointsAtWhicheverFilesExist(t *testing.T) {
	m := &Manager{cfg: Config{}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.rulesReviewSuffix(&PRD{ProjectDir: dir})
	if got == "" {
		t.Fatal("want a non-empty suffix when AGENT.md exists")
	}
	if !containsStr(got, "AGENT.md") {
		t.Errorf("suffix doesn't name AGENT.md: %q", got)
	}
	// It's a judgment review, not a fixed checklist — must not dictate
	// specific items, just point at the file and ask for real changes
	// to be checked against it.
	if !containsStr(got, "judgment") {
		t.Errorf("suffix doesn't frame this as a judgment review: %q", got)
	}
}

func TestRulesReviewSuffix_CustomFilenames(t *testing.T) {
	m := &Manager{cfg: Config{RulesFile: "RULES.md", ContextFile: "CTX.md"}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "RULES.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CTX.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.rulesReviewSuffix(&PRD{ProjectDir: dir})
	if !containsStr(got, "RULES.md") || !containsStr(got, "CTX.md") {
		t.Errorf("suffix doesn't honor configured filenames: %q", got)
	}
}

func TestRunPRDCompletionRulesReview_NotEnabledIsNoOp(t *testing.T) {
	// BL418 configurability follow-up: a rules file exists and a
	// GuardrailFn is wired, but "rules-review" was never opted into
	// anywhere (PRD, profile, or global Config) — must stay a no-op.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	m := &Manager{cfg: Config{}}
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		called = true
		return GuardrailVerdict{Outcome: "pass"}, nil
	})
	prd := &PRD{ID: "p1", ProjectDir: dir}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v, want false/nil when rules-review isn't opted in", blocked, err)
	}
	if called {
		t.Error("GuardrailFn should never be called when rules-review isn't in the resolved PRD-level guardrail list")
	}
}

func TestRunPRDCompletionRulesReview_NoGuardrailFnWiredIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{}}
	prd := &PRD{ID: "p1", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v, want false/nil when no GuardrailFn is wired", blocked, err)
	}
	if len(prd.Verdicts) != 0 {
		t.Errorf("want no verdict recorded when the review never ran, got %v", prd.Verdicts)
	}
}

func TestRunPRDCompletionRulesReview_NoRulesFileIsNoOp(t *testing.T) {
	dir := t.TempDir() // no AGENT.md
	called := false
	m := &Manager{cfg: Config{}}
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		called = true
		return GuardrailVerdict{Outcome: "pass"}, nil
	})
	prd := &PRD{ID: "p1", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v, want false/nil when there's no rules file to check against", blocked, err)
	}
	if called {
		t.Error("GuardrailFn should never be called when there's nothing to review against")
	}
}

func TestRunPRDCompletionRulesReview_PassDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	var captured GuardrailInvocation
	m := newRulesReviewManager(t, Config{})
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		captured = req
		return GuardrailVerdict{Outcome: "pass", Summary: "looks fine"}, nil
	})
	prd := &PRD{ID: "p1", Title: "Add a feature", Spec: "do the thing", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blocked {
		t.Fatal("a pass verdict must not block")
	}
	if captured.Level != "prd" || captured.UnitID != "p1" || captured.Guardrail != "rules-review" {
		t.Fatalf("invocation = %+v, want Level=prd UnitID=p1 Guardrail=rules-review", captured)
	}
	if !containsStr(captured.UnitSpec, "do the thing") || !containsStr(captured.UnitSpec, "AGENT.md") {
		t.Errorf("UnitSpec missing the PRD's own spec or the rules pointer: %q", captured.UnitSpec)
	}
	if len(prd.Verdicts) != 1 || prd.Verdicts[0].Outcome != "pass" {
		t.Errorf("want one recorded pass verdict, got %v", prd.Verdicts)
	}
}

func TestRunPRDCompletionRulesReview_BlockOutcomeBlocks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newRulesReviewManager(t, Config{})
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "block", Severity: "high", Summary: "version collides with a shipped release"}, nil
	})
	prd := &PRD{ID: "p1", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !blocked {
		t.Fatal("a block verdict must block")
	}
	if len(prd.Verdicts) != 1 || prd.Verdicts[0].Outcome != "block" {
		t.Errorf("want the block verdict recorded, got %v", prd.Verdicts)
	}
}

func TestRunPRDCompletionRulesReview_FnErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{}}
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		return GuardrailVerdict{}, errFakeGuardrail
	})
	prd := &PRD{ID: "p1", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	_, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err == nil {
		t.Fatal("want the GuardrailFn's error to propagate")
	}
}

func TestRunPRDCompletionRulesReview_ResolvesBackendFromPRDThenGlobal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	var captured GuardrailInvocation
	m := newRulesReviewManager(t, Config{VerificationBackend: "global-backend", VerificationModel: "global-model"})
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		captured = req
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	// PRD override wins over global.
	prd := &PRD{ID: "p1", ProjectDir: dir, GuardrailBackend: "prd-backend", GuardrailModel: "prd-model", PerPRDGuardrails: []string{"rules-review"}}
	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd); err != nil {
		t.Fatal(err)
	}
	if captured.GuardrailBackend != "prd-backend" || captured.GuardrailModel != "prd-model" {
		t.Errorf("want the PRD's own override, got backend=%q model=%q", captured.GuardrailBackend, captured.GuardrailModel)
	}

	// No PRD override — backend falls back to global, but model does
	// NOT (fixed live 2026-10-12, BL418's own first live validation: a
	// flat ask-only global VerificationModel reaching this field was
	// trusted by the B124 session-routing closure as a genuine
	// per-unit override and sent straight to a real tool-using
	// session, which rejected it and hung for its full timeout — the
	// same bug class B124 already fixed for verify).
	prd2 := &PRD{ID: "p2", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd2); err != nil {
		t.Fatal(err)
	}
	if captured.GuardrailBackend != "global-backend" {
		t.Errorf("want the global backend default, got backend=%q", captured.GuardrailBackend)
	}
	if captured.GuardrailModel != "" {
		t.Errorf("want empty model (must not leak the global ask-only model into a session-routed call), got %q", captured.GuardrailModel)
	}
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

var errFakeGuardrail = fakeErr("guardrail backend unreachable")
