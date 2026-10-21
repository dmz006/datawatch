// Plan-review gate (2026-10-17) — pre-approval LLM judgment over the
// decomposed plan: default-off, warn never fails anything, block
// refuses approval on a real bad verdict (never on a reviewer outage),
// verdict + Decision-row persistence, re-run semantics, Task kind
// round-trip, and the request's bounded projection.
package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// planReviewConfig returns DefaultConfig with the plan-review gate in
// the given mode ("" = off default). The plan-coverage gate is switched
// off so these tests exercise the plan-review gate in isolation (the
// minimal one-task fixture plan carries structural coverage findings).
func planReviewConfig(mode string) Config {
	cfg := DefaultConfig()
	cfg.PlanCoverageCheck = "off"
	cfg.PlanReviewMode = mode
	return cfg
}

// planReviewReviewer is a scriptable reviewer: set ok/summary/findings
// to control the verdict, err to simulate an outage, and called is
// incremented per invocation.
type planReviewReviewer struct {
	ok       bool
	summary  string
	findings []PlanFinding
	err      error
	called   int
}

func (r *planReviewReviewer) fn(ctx interface{ Done() <-chan struct{} }) {} // never used

// newPlanReviewManager builds a manager whose decompose emits one
// story with one task; attach the reviewer via SetPlanReview when
// wantReviewer is true.
func newPlanReviewManager(t *testing.T, cfg Config, reviewer PlanReviewFn) *Manager {
	t.Helper()
	decompose := func(req DecomposeRequest) (string, error) {
		return `{"title":"Ship it","stories":[{"title":"A","tasks":[{"title":"T","spec":"do x"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), cfg, decompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if reviewer != nil {
		m.SetPlanReview(reviewer)
	}
	return m
}

// lastPlanReviewDecision returns the most recent plan_review decision
// row on the stored PRD (nil when absent).
func lastPlanReviewDecision(t *testing.T, m *Manager, prdID string) *Decision {
	t.Helper()
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s missing from store", prdID)
	}
	var last *Decision
	for i := range prd.Decisions {
		if prd.Decisions[i].Kind == "plan_review" {
			last = &prd.Decisions[i]
		}
	}
	return last
}

// Reviewer that fails the test if ever invoked (proves it did not run).
func reviewerMustNotRun(t *testing.T) PlanReviewFn {
	t.Helper()
	return func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		t.Error("plan reviewer ran although it must not be called")
		return PlanReviewVerdict{}, nil
	}
}

// --- default-off -------------------------------------------------------

func TestPlanReview_OffByDefault_NeverRuns(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig(""), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		t.Error("reviewer ran although default mode is off")
		return PlanReviewVerdict{}, nil
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	stored, _ := m.store.GetPRD(prd.ID)
	if stored.PlanReview != nil {
		t.Fatalf("PlanReview = %+v, want nil in off mode (default)", stored.PlanReview)
	}
	if d := lastPlanReviewDecision(t, m, prd.ID); d != nil {
		t.Fatalf("plan_review decision row %v found in off mode", d)
	}
	// Approve is unaffected.
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve in off mode: %v", err)
	}
}

func TestPlanReview_OffExplicit_NeverRuns(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig("off"), nil)
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	stored, _ := m.store.GetPRD(prd.ID)
	if stored.PlanReview != nil {
		t.Fatalf("PlanReview = %+v, want nil with mode off", stored.PlanReview)
	}
	// ReviewPlan in off mode is a no-op, not an error.
	if updated, err := m.ReviewPlan(prd.ID, "op"); err != nil {
		t.Fatalf("ReviewPlan in off mode: %v", err)
	} else if updated.PlanReview != nil {
		t.Fatalf("ReviewPlan in off mode must not record a verdict, got %+v", updated.PlanReview)
	}
}

// --- warn --------------------------------------------------------------

func TestPlanReview_Warn_PersistsOkVerdict_NeverBlocks(t *testing.T) {
	r := &planReviewReviewer{ok: true, summary: "looks fine"}
	m := newPlanReviewManager(t, planReviewConfig("warn"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		r.called++
		if req.PRDID == "" || len(req.Stories) == 0 {
			t.Errorf("reviewer request is incomplete: %+v", req)
		}
		return PlanReviewVerdict{
			Ok:         r.ok,
			Severity:   "low",
			Summary:    r.summary,
			Backend:    "ollama",
			Model:      "judge",
			Findings:   r.findings,
			EstCostUSD: 0.004,
		}, r.err
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if r.called != 1 {
		t.Fatalf("reviewer called %d times, want 1", r.called)
	}
	stored, _ := m.store.GetPRD(prd.ID)
	if stored.PlanReview == nil || !stored.PlanReview.Ok {
		t.Fatalf("stored verdict = %+v, want recorded ok:true", stored.PlanReview)
	}
	if stored.PlanReview.Mode != "warn" {
		t.Fatalf("verdict.Mode = %q, want warn", stored.PlanReview.Mode)
	}
	d := lastPlanReviewDecision(t, m, prd.ID)
	if d == nil {
		t.Fatal("no plan_review decision row for a warn verdict")
	}
	if d.VerdictOutcome != "pass" {
		t.Fatalf("decision outcome = %q, want pass", d.VerdictOutcome)
	}
	if d.CostUSD != 0.004 {
		t.Fatalf("decision CostUSD = %v, want 0.004", d.CostUSD)
	}
	// Warn never blocks approval.
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve after a warn verdict: %v", err)
	}
}

func TestPlanReview_Warn_BadVerdictRecorded_NeverBlocksApprove(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig("warn"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		return PlanReviewVerdict{
			Ok:       false,
			Severity: "high",
			Summary:  "story coverage is incomplete",
			Findings: []PlanFinding{{
				TaskID:    "t1",
				TaskTitle: "T",
				Category:  PlanReviewCategoryMissing,
				Detail:    "no task covers the retry requirement",
			}},
		}, nil
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose must succeed in warn mode, got err: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review (warn never fails decompose)", updated.Status)
	}
	if updated.PlanReview == nil || updated.PlanReview.Ok || updated.PlanReview.ReviewerFailed {
		t.Fatalf("stored verdict = %+v, want recorded bad verdict (not failed)", updated.PlanReview)
	}
	d := lastPlanReviewDecision(t, m, prd.ID)
	if d == nil || d.VerdictOutcome != "block" {
		t.Fatalf("decision = %+v, want outcome block on a bad verdict row", d)
	}
	if !strings.Contains(d.Note, "story coverage is incomplete") {
		t.Fatalf("decision note %q does not carry the reviewer summary", d.Note)
	}
	// Warn: even a bad verdict never blocks approval.
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve after a bad warn verdict: %v", err)
	}
}

// --- block -------------------------------------------------------------

func TestPlanReview_Block_BadVerdictBlocksApprove_RerunClears(t *testing.T) {
	state := &struct {
		ok   bool
		says string
	}{ok: false, says: "missing rollback story"}
	m := newPlanReviewManager(t, planReviewConfig("block"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		if state.ok {
			return PlanReviewVerdict{Ok: true, Severity: "low", Summary: "looks fine"}, nil
		}
		return PlanReviewVerdict{
			Ok:       false,
			Severity: "high",
			Summary:  state.says,
			Findings: []PlanFinding{{Category: PlanReviewCategoryMissing, Detail: state.says}},
		}, nil
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose must succeed in block mode when the reviewer runs: %v", err)
	}
	// 1) Bad verdict → Approve refused with the reviewer's reason.
	if _, err := m.Approve(prd.ID, "op", ""); err == nil {
		t.Fatal("Approve succeeded although a bad verdict is recorded in block mode")
	} else if !strings.Contains(err.Error(), "plan review blocks approval") {
		t.Fatalf("Approve error %q does not explain the plan-review block", err.Error())
	} else if !strings.Contains(err.Error(), state.says) {
		t.Fatalf("Approve error %q does not carry the reviewer summary", err.Error())
	}
	// 2) Operator re-runs the review after the plan has been fixed — a
	// new ok:true verdict replaces the blocking one (last-write-wins).
	state.ok = true
	if updated, err := m.ReviewPlan(prd.ID, "operator"); err != nil {
		t.Fatalf("ReviewPlan: %v", err)
	} else if !updated.PlanReview.Ok {
		t.Fatalf("after re-run verdict = %+v, want ok:true", updated.PlanReview)
	}
	// 3) Approve now succeeds; the actor is attributed on the decision.
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve after a clearing re-run: %v", err)
	}
	last := lastPlanReviewDecision(t, m, prd.ID)
	if last == nil || !strings.HasPrefix(last.Note, "operator:") {
		t.Fatalf("final plan_review decision = %+v, want Note attributed to the operator re-run", last)
	}
}

// --- reviewer failure semantics ----------------------------------------

func TestPlanReview_Outage_RecordsFailed_NeverBlocks(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig("block"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		return PlanReviewVerdict{}, errors.New("upstream llm at https://127.0.0.1:1455 refused")
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose must not fail on a reviewer outage: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review after a reviewer outage", updated.Status)
	}
	if updated.PlanReview == nil || !updated.PlanReview.ReviewerFailed || updated.PlanReview.Ok {
		t.Fatalf("stored verdict = %+v, want recorded reviewer-failed", updated.PlanReview)
	}
	if !strings.Contains(updated.PlanReview.Summary, "upstream llm") {
		t.Fatalf("summary %q does not carry the outage reason", updated.PlanReview.Summary)
	}
	d := lastPlanReviewDecision(t, m, prd.ID)
	if d == nil || d.VerdictOutcome != "n/a" {
		t.Fatalf("decision = %+v, want outcome n/a for a reviewer outage", d)
	}
	// Outage must never wedge the operator's approval gate.
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve after a reviewer outage: %v", err)
	}
}

func TestPlanReview_EmptyVerdict_TreatedAsUnparseable(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig("block"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		return PlanReviewVerdict{}, nil // zero verdict
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.PlanReview == nil || !updated.PlanReview.ReviewerFailed {
		t.Fatalf("stored verdict = %+v, want reviewer-failed for an empty verdict", updated.PlanReview)
	}
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve after an unparseable verdict: %v", err)
	}
}

func TestPlanReview_NoReviewerWired_SkipsGracefully(t *testing.T) {
	for _, mode := range []string{"warn", "block"} {
		t.Run(mode, func(t *testing.T) {
			m := newPlanReviewManager(t, planReviewConfig(mode), nil) // SetPlanReview never called
			prd, _ := m.CreatePRD("spec", "", "", "", "")
			updated, err := m.Decompose(prd.ID)
			if err != nil {
				t.Fatalf("Decompose with no reviewer wired: %v", err)
			}
			if updated.PlanReview == nil || !strings.Contains(updated.PlanReview.Summary, "skipped") {
				t.Fatalf("stored verdict = %+v, want a 'skipped' degraded verdict", updated.PlanReview)
			}
			if !updated.PlanReview.ReviewerFailed {
				t.Fatal("'skipped' verdict must carry ReviewerFailed=true so it can never block")
			}
			if _, err := m.Approve(prd.ID, "op", ""); err != nil {
				t.Fatalf("Approve after a skipped review: %v", err)
			}
		})
	}
}

// --- persistence + decision ordering ------------------------------------

func TestPlanReview_DecisionOrdering_AfterDecomposeRow(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig("warn"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		return PlanReviewVerdict{Ok: true, Severity: "low", Summary: "ok", PromptChars: 12, ResponseChars: 7}, nil
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	stored, _ := m.store.GetPRD(prd.ID)
	if len(stored.Decisions) < 2 {
		t.Fatalf("decisions = %d, want at least decompose + plan_review rows", len(stored.Decisions))
	}
	for i := len(stored.Decisions) - 1; i >= 0; i-- {
		if stored.Decisions[i].Kind == "decompose" {
			// The plan_review row must come after the decompose row.
			found := false
			for j := i + 1; j < len(stored.Decisions); j++ {
				if stored.Decisions[j].Kind == "plan_review" {
					found = true
				}
			}
			if !found {
				t.Fatal("plan_review decision row must land after the decompose row")
			}
			break
		}
	}
	// Telemetry carried onto the decision row.
	last := lastPlanReviewDecision(t, m, prd.ID)
	if last == nil {
		t.Fatal("no plan_review decision row")
	}
	if last.PromptChars != 12 || last.ResponseChars != 7 {
		t.Fatalf("decision telemetry = %d/%d, want 12/7", last.PromptChars, last.ResponseChars)
	}
}

// --- ReviewPlan ----------------------------------------------------------

func TestReviewPlan_NotFound(t *testing.T) {
	m := newPlanReviewManager(t, planReviewConfig("warn"), nil)
	if _, err := m.ReviewPlan("nope", "op"); err == nil {
		t.Fatal("ReviewPlan on a missing PRD: want error")
	}
}

// --- Task kind round trip --------------------------------------------------

func TestTaskKind_JSONRoundTrip(t *testing.T) {
	in := Task{ID: "t1", Title: "smoke run", Kind: TaskKindSmoke, Spec: "run smoke test.sh"}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"kind":"smoke"`) {
		t.Fatalf("json lacks kind: %s", raw)
	}
	var out Task
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Kind != TaskKindSmoke {
		t.Fatalf("kind = %q, want %q", out.Kind, TaskKindSmoke)
	}
	// Empty kind omits (backward compat: no key at all).
	var none Task
	if err := json.Unmarshal([]byte(`{"id":"t2"}`), &none); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if none.Kind != "" {
		t.Fatalf("kind = %q, want empty for pre-kind tasks", none.Kind)
	}
}

// --- outcome / projection / mode helpers ----------------------------------

func TestPlanReviewVerdict_Outcome(t *testing.T) {
	cases := []struct {
		name string
		v    PlanReviewVerdict
		want string
	}{
		{"reviewer failure", PlanReviewVerdict{Ok: true, ReviewerFailed: true}, "n/a"},
		{"ok verdict", PlanReviewVerdict{Ok: true}, "pass"},
		{"bad verdict", PlanReviewVerdict{Ok: false, Findings: []PlanFinding{{Category: PlanReviewCategoryWeakened}}}, "block"},
	}
	for _, c := range cases {
		if got := c.v.Outcome(); got != c.want {
			t.Errorf("Outcome(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValidPlanReviewMode(t *testing.T) {
	for _, ok := range []string{"", "off", "warn", "block", " WARN", "Block"} {
		if !validPlanReviewMode(ok) {
			t.Errorf("validPlanReviewMode(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"yes", "BLOCK!", "warn block", " " + "x"} {
		if validPlanReviewMode(bad) {
			t.Errorf("validPlanReviewMode(%q) = true, want false", bad)
		}
	}
	if normalizePlanReviewMode("  BLOCK ") != PlanReviewModeBlock {
		t.Error("normalizePlanReviewMode must be case/trim-insensitive")
	}
	if normalizePlanReviewMode("garbage") != PlanReviewModeOff {
		t.Error("normalizePlanReviewMode must map unknowns to off")
	}
}

func TestBuildPlanReviewRequest_BoundedProjection(t *testing.T) {
	big := strings.Repeat("a", planReviewSpecMax+5000)
	prd := &PRD{ID: "p1", Spec: big}
	longTask := strings.Repeat("s", planReviewTaskSpecMax+3000)
	story := Story{ID: "st1", Title: "Story A", PRDID: "p1", Tasks: []Task{
		{ID: "t1", Title: "T", Spec: longTask, Kind: "smoke", DependsOn: []string{"t0"}},
	}}
	req := buildPlanReviewRequest(prd, []Story{story})
	if len(req.Spec) > planReviewSpecMax+3 {
		t.Fatalf("request spec = %d chars, want bounded to ~%d", len(req.Spec), planReviewSpecMax)
	}
	if len(req.Stories) != 1 || len(req.Stories[0].Tasks) != 1 {
		t.Fatalf("projection lost stories/tasks: %+v", req)
	}
	if len(req.Stories[0].Tasks[0].Spec) > planReviewTaskSpecMax+3 {
		t.Fatalf("task spec = %d chars, want bounded to ~%d", len(req.Stories[0].Tasks[0].Spec), planReviewTaskSpecMax)
	}
	if req.Stories[0].Tasks[0].Kind != "smoke" {
		t.Fatalf("task kind = %q, want smoke in the projection", req.Stories[0].Tasks[0].Kind)
	}
	// Empty plan projects to empty (not an error; the review still runs
	// with just the spec when decompose produced nothing reviewable).
	empty := buildPlanReviewRequest(prd, nil)
	if empty.Stories != nil {
		t.Fatalf("empty stories = %+v, want nil", empty.Stories)
	}
}

func TestPlanReviewCategories_Dedup(t *testing.T) {
	fs := []PlanFinding{
		{Category: "missing"},
		{Category: "missing"},
		{Category: ""},
		{Category: "too_large"},
	}
	got := planReviewCategories(fs)
	if len(got) != 2 || got[0] != "missing" || got[1] != "too_large" {
		t.Fatalf("planReviewCategories = %v, want [missing too_large]", got)
	}
}

// --- decompose tail integration (streaming path, failure routing) -----

// The streaming decompose shares finalizeDecomposedPlan with Decompose, so
// a block-mode verdict must land there too and gate Approve.
func TestPlanReview_Streaming_BlockVerdictRecordedAndGatesApprove(t *testing.T) {
	calls := 0
	m := newPlanReviewManager(t, planReviewConfig("block"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		calls++
		return PlanReviewVerdict{Ok: false, Severity: "high", Summary: "plan misses the spec"}, nil
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	updated, err := m.decomposeStreamingCore(prd.ID, nil)
	if err != nil {
		t.Fatalf("decomposeStreamingCore must not fail on a negative verdict: %v", err)
	}
	if calls != 1 {
		t.Fatalf("reviewer called %d times, want 1", calls)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %q, want needs_review", updated.Status)
	}
	if _, err := m.Approve(prd.ID, "op", ""); err == nil {
		t.Fatal("Approve must be refused by a blocking verdict")
	}
}

// A decompose that fails (here: unparseable output) is routed through the
// existing failure path and must never invoke the reviewer.
func TestPlanReview_DecomposeFailure_NeverReviews(t *testing.T) {
	m, err := NewManager(t.TempDir(), planReviewConfig("block"), func(req DecomposeRequest) (string, error) {
		return "not json at all", nil
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetPlanReview(reviewerMustNotRun(t))
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.Decompose(prd.ID); err == nil {
		t.Fatal("expected decompose failure for unparseable output")
	}
	stored, _ := m.store.GetPRD(prd.ID)
	if stored.PlanReview != nil {
		t.Fatalf("failed decompose must not record a review, got %+v", stored.PlanReview)
	}
}
