package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// regressionPinnedModel is the exact pinned model of the PRD shape
// observed on 2026-10-20 22:29: PRD backend is pi, no guardrail model at
// any unit level, and both autonomous.verification_model and
// autonomous.planning_model are "ollama/qwen3.8-64k:27b". Every judge
// launch path must resolve exactly that string — never truncated to
// "ollama/qwen3.8:27b", never swapped, and never replaced by the
// harness's host-default model (the "pi default" label).
const regressionPinnedModel = "ollama/qwen3.8-64k:27b"

const truncatedRegressionModel = "ollama/qwen3.8:27b"

// modelJudgeRecorder captures the exact model every judge invocation
// receives and returns a pass verdict.
type modelJudgeRecorder struct {
	calls  int
	models []string
}

func (r *modelJudgeRecorder) fn(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
	r.calls++
	r.models = append(r.models, inv.GuardrailModel)
	return GuardrailVerdict{Outcome: "pass", Summary: "judge ran"}, nil
}

func (r *modelJudgeRecorder) assertAll(t *testing.T, want string) {
	t.Helper()
	if r.calls == 0 {
		t.Fatalf("judge fn was never called; want it called with %q", want)
	}
	for i, got := range r.models {
		if got != want {
			t.Fatalf("judge invocation %d got model %q, want exactly %q", i, got, want)
		}
		if got == truncatedRegressionModel {
			t.Fatalf("judge invocation %d got the truncated model %q", i, got)
		}
		if got == "pi default" {
			t.Fatalf("judge invocation %d got the 'pi default' fallback label", i)
		}
	}
}

// newJudgeShapeManager builds a manager in the 2026-10-20 22:29 PRD
// shape: pi backends throughout, verification_model and planning_model
// both pinned to the 64k model, no guardrail_model at any level.
func newJudgeShapeManager(t *testing.T) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.VerificationBackend = "pi"
	cfg.VerificationModel = regressionPinnedModel
	cfg.PlanningBackend = "pi"
	cfg.PlanningModel = regressionPinnedModel
	cfg.PerTaskGuardrails = []string{"content-policy"}
	cfg.PerStoryGuardrails = []string{"story-policy"}
	m, err := NewManager(t.TempDir(), cfg, fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// newJudgeShapePRD creates a pi-backend PRD with one story and one
// (non-completed) task in the store, no unit-level model overrides.
// Returns the stored pointer so mutations plus SavePRD are visible to
// the judge paths.
func newJudgeShapePRD(t *testing.T, m *Manager) (*PRD, *Story, *Task) {
	t.Helper()
	dir := t.TempDir()
	created, err := m.CreatePRD("spec", dir, "pi", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(created.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := m.Store().GetPRD(created.ID)
	if !ok {
		t.Fatal("GetPRD failed")
	}
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd, &prd.Story[0], &prd.Story[0].Tasks[0]
}

// TestJudgeKeepsPinnedModel_PRDShape is the regression test for the
// 2026-10-20 22:29 shape across every judge launch path the manager
// owns — per-task guardrail, per-story guardrail, the rules-review
// invocation (completion rollup + rejudge), ci-watch — plus the
// decompose session's planning-model resolution. None may lose,
// truncate, or replace the pinned model with the harness's host default.
func TestJudgeKeepsPinnedModel_PRDShape(t *testing.T) {
	m := newJudgeShapeManager(t)
	prd, story, task := newJudgeShapePRD(t, m)
	rec := &modelJudgeRecorder{}
	m.SetGuardrail(rec.fn)
	ctx := context.Background()

	// per-task guardrail judge
	blocked, _, err := m.runPerTaskGuardrails(ctx, prd, task)
	if err != nil || blocked {
		t.Fatalf("per-task guardrail: blocked=%v err=%v, want clean pass", blocked, err)
	}
	// per-story guardrail judge
	blocked, _, err = m.runPerStoryGuardrails(ctx, prd, story)
	if err != nil || blocked {
		t.Fatalf("per-story guardrail: blocked=%v err=%v, want clean pass", blocked, err)
	}
	// rules-review invocation (run by the completion rollup and rejudge)
	inv, err := m.prdRulesReviewInvocation(prd)
	if err != nil {
		t.Fatalf("rules-review invocation: %v", err)
	}
	if inv.GuardrailModel != regressionPinnedModel {
		t.Fatalf("rules-review invocation model = %q, want exactly %q", inv.GuardrailModel, regressionPinnedModel)
	}
	// ci-watch judge
	blocked, _, err = m.ciWatchJudge(ctx, prd, CIWatchResult{SHA: "sha", Repo: "o/r"}, []CIWatchRun{{ID: 1, Name: "build", Status: "failure"}})
	if err != nil || blocked {
		t.Fatalf("ci-watch judge: blocked=%v err=%v, want clean pass", blocked, err)
	}
	// rejudge judge
	v, err := m.judgeCompletionRulesReview(ctx, prd)
	if err != nil || v.Outcome != "pass" {
		t.Fatalf("rejudge judge: verdict=%+v err=%v, want clean pass", v, err)
	}

	rec.assertAll(t, regressionPinnedModel)
	if want := 4; rec.calls != want { // task, story, ci-watch, rejudge
		t.Fatalf("judge calls = %d, want %d", rec.calls, want)
	}

	// decompose session: the global planning tier must resolve the exact
	// pinned model (the PRD has no decomposition_model override), and
	// the pi-family launch gate must accept it rather than fail loudly.
	got := resolvePlannedModel("pi", "", prd.Backend, prd.Model, "pi", regressionPinnedModel, regressionPinnedModel)
	if got != regressionPinnedModel {
		t.Fatalf("decompose session model = %q, want exactly %q", got, regressionPinnedModel)
	}
	if err := RequirePinnedModel("pi", got, "the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model"); err != nil {
		t.Fatalf("pinned decompose model rejected by the launch gate: %v", err)
	}
}

// TestGuardrailJudgeModel_PathTierTable sweeps the manager judge paths
// against every model precedence tier and asserts the exact model the
// judge receives in each combination. Tier order (guardrail_model.go):
// PRD guardrail_model, task guardrail_model, autonomous.guardrail_model,
// autonomous.verification_model, autonomous.planning_model.
//
// The task tier applies only to the per-task path: the per-story and the
// PRD-level paths (rules-review, ci-watch, rejudge) resolve with an empty
// task, so there the PRD tier is followed directly by the global tiers.
func TestGuardrailJudgeModel_PathTierTable(t *testing.T) {
	ctx := context.Background()
	scenarios := []struct {
		name     string
		prepare  func(m *Manager, prd *PRD, story *Story, task *Task)
		taskWant string // what the per-task judge must get
		unitWant string // what the per-story / PRD-level judges must get
	}{
		{"PRD guardrail_model beats every other tier", func(m *Manager, prd *PRD, s *Story, t *Task) {
			prd.GuardrailModel = "prd/guard:27b"
			t.GuardrailModel = "task/guard:27b"
			m.cfg.GuardrailModel = "g/guard:27b"
			m.cfg.VerificationModel = "g/verify:27b"
			m.cfg.PlanningModel = "g/plan:27b"
		}, "prd/guard:27b", "prd/guard:27b"},
		{"task guardrail_model tier: per-task only, others fall to global", func(m *Manager, prd *PRD, s *Story, t *Task) {
			t.GuardrailModel = "task/guard:27b"
			m.cfg.GuardrailModel = "g/guard:27b"
			m.cfg.VerificationModel = "g/verify:27b"
			m.cfg.PlanningModel = "g/plan:27b"
		}, "task/guard:27b", "g/guard:27b"},
		{"autonomous.guardrail_model tier", func(m *Manager, prd *PRD, s *Story, t *Task) {
			m.cfg.GuardrailModel = "g/guard:27b"
			m.cfg.VerificationModel = "g/verify:27b"
			m.cfg.PlanningModel = "g/plan:27b"
		}, "g/guard:27b", "g/guard:27b"},
		{"autonomous.verification_model tier", func(m *Manager, prd *PRD, s *Story, t *Task) {
			m.cfg.GuardrailModel = ""
			m.cfg.VerificationModel = "g/verify:27b"
			m.cfg.PlanningModel = "g/plan:27b"
		}, "g/verify:27b", "g/verify:27b"},
		{"autonomous.planning_model tier (lowest)", func(m *Manager, prd *PRD, s *Story, t *Task) {
			m.cfg.GuardrailModel = ""
			m.cfg.VerificationModel = ""
			m.cfg.PlanningModel = "g/plan:27b"
		}, "g/plan:27b", "g/plan:27b"},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			m := newJudgeShapeManager(t)
			prd, story, task := newJudgeShapePRD(t, m)
			sc.prepare(m, prd, story, task)
			rec := &modelJudgeRecorder{}
			m.SetGuardrail(rec.fn)

			blocked, _, err := m.runPerTaskGuardrails(ctx, prd, task)
			if err != nil || blocked {
				t.Fatalf("per-task guardrail: blocked=%v err=%v", blocked, err)
			}
			assertRecorderModel(t, rec, "per-task", sc.taskWant)

			blocked, _, err = m.runPerStoryGuardrails(ctx, prd, story)
			if err != nil || blocked {
				t.Fatalf("per-story guardrail: blocked=%v err=%v", blocked, err)
			}
			assertRecorderModel(t, rec, "per-story", sc.unitWant)

			inv, err := m.prdRulesReviewInvocation(prd)
			if err != nil {
				t.Fatalf("rules-review invocation: %v", err)
			}
			if inv.GuardrailModel != sc.unitWant {
				t.Fatalf("rules-review invocation model = %q, want exactly %q", inv.GuardrailModel, sc.unitWant)
			}
			blocked, _, err = m.ciWatchJudge(ctx, prd, CIWatchResult{SHA: "sha", Repo: "o/r"}, []CIWatchRun{{ID: 1, Name: "build", Status: "failure"}})
			if err != nil || blocked {
				t.Fatalf("ci-watch judge: blocked=%v err=%v", blocked, err)
			}
			assertRecorderModel(t, rec, "ci-watch", sc.unitWant)

			v, err := m.judgeCompletionRulesReview(ctx, prd)
			if err != nil || v.Outcome != "pass" {
				t.Fatalf("rejudge judge: verdict=%+v err=%v", v, err)
			}
			assertRecorderModel(t, rec, "rejudge", sc.unitWant)
		})
	}
}

func assertRecorderModel(t *testing.T, rec *modelJudgeRecorder, path, want string) {
	t.Helper()
	if len(rec.models) == 0 {
		t.Fatalf("[%s] judge fn was never called", path)
	}
	got := rec.models[len(rec.models)-1]
	if got != want {
		t.Fatalf("[%s] judge model = %q, want exactly %q", path, got, want)
	}
	if got == truncatedRegressionModel || got == "pi default" || got == "" {
		t.Fatalf("[%s] judge model %q is truncated, a host default, or empty", path, got)
	}
}

// TestGuardrailJudgeModel_FailLoudlyWhenNoTier asserts that when no
// model tier resolves, no judge path launches anything: each records a
// loud, non-retried infra-category block (or errors naming
// ErrNoGuardrailModel) instead of falling back to the harness's
// host-default model ("pi default").
func TestGuardrailJudgeModel_FailLoudlyWhenNoTier(t *testing.T) {
	ctx := context.Background()
	t.Run("per-task", func(t *testing.T) {
		m := newNoModelShapeManager(t)
		prd, _, task := newJudgeShapePRD(t, m)
		rec := &modelJudgeRecorder{}
		m.SetGuardrail(rec.fn)
		blocked, _, err := m.runPerTaskGuardrails(ctx, prd, task)
		assertJudgePathFailsLoudly(t, blocked, err, rec, task.Verdicts)
	})
	t.Run("per-story", func(t *testing.T) {
		m := newNoModelShapeManager(t)
		prd, story, _ := newJudgeShapePRD(t, m)
		rec := &modelJudgeRecorder{}
		m.SetGuardrail(rec.fn)
		blocked, _, err := m.runPerStoryGuardrails(ctx, prd, story)
		assertJudgePathFailsLoudly(t, blocked, err, rec, story.Verdicts)
	})
	t.Run("rules-review-invocation", func(t *testing.T) {
		m := newNoModelShapeManager(t)
		prd, _, _ := newJudgeShapePRD(t, m)
		_, err := m.prdRulesReviewInvocation(prd)
		if !errors.Is(err, ErrNoGuardrailModel) {
			t.Fatalf("invocation error = %v, want one wrapping %v", err, ErrNoGuardrailModel)
		}
	})
	t.Run("rejudge", func(t *testing.T) {
		m := newNoModelShapeManager(t)
		prd, _, _ := newJudgeShapePRD(t, m)
		rec := &modelJudgeRecorder{}
		m.SetGuardrail(rec.fn)
		v, err := m.judgeCompletionRulesReview(ctx, prd)
		if err != nil {
			t.Fatalf("rejudge returned err=%v; want a loud verdict (recorded by RejudgeCompletionGuardrail)", err)
		}
		if rec.calls != 0 {
			t.Fatalf("rejudge: judge fn called %d times, want 0 (never launch on the host default)", rec.calls)
		}
		assertLoudVerdict(t, v)
		if v.Guardrail != "rules-review" {
			t.Fatalf("loud verdict guardrail = %q, want rules-review", v.Guardrail)
		}
	})
	t.Run("ci-watch", func(t *testing.T) {
		m := newNoModelShapeManager(t)
		prd, _, _ := newJudgeShapePRD(t, m)
		rec := &modelJudgeRecorder{}
		m.SetGuardrail(rec.fn)
		blocked, _, _ := m.ciWatchJudge(ctx, prd, CIWatchResult{SHA: "sha", Repo: "o/r"}, []CIWatchRun{{ID: 1, Name: "build", Status: "failure"}})
		if !blocked {
			t.Fatal("ci-watch: want blocked=true when no judge model resolves")
		}
		if rec.calls != 0 {
			t.Fatalf("ci-watch: judge fn called %d times, want 0", rec.calls)
		}
		assertJudgePathFailsLoudly(t, blocked, nil, rec, prd.Verdicts)
	})
	t.Run("decompose-session", func(t *testing.T) {
		got := resolvePlannedModel("pi", "", "pi", "", "pi", "", "")
		if got != "" {
			t.Fatalf("decompose resolved model %q with no tiers set, want empty (+ fail loudly)", got)
		}
		serr := RequirePinnedModel("pi", got, "the PRD's decomposition_model", "autonomous.planning_model", "the PRD's model", "autonomous.verification_model")
		if !errors.Is(serr, ErrNoPinnedModel) {
			t.Fatalf("RequirePinnedModel with empty model = %v, want %v", serr, ErrNoPinnedModel)
		}
		// Non pi-family backends may legitimately use their registry
		// default: no loud failure.
		if serr := RequirePinnedModel("claude-code", "", "autonomous.verification_model"); serr != nil {
			t.Fatalf("claude-code with empty model must be allowed (registry default), got %v", serr)
		}
	})
}

// newNoModelShapeManager is newJudgeShapeManager with every model tier
// cleared, so no judge model resolves at any level.
func newNoModelShapeManager(t *testing.T) *Manager {
	t.Helper()
	m := newJudgeShapeManager(t)
	m.cfg.GuardrailModel = ""
	m.cfg.VerificationModel = ""
	m.cfg.PlanningModel = ""
	return m
}

func assertLoudVerdict(t *testing.T, v GuardrailVerdict) {
	t.Helper()
	if v.Outcome != "block" || v.Category != "infra" {
		t.Fatalf("verdict = %+v, want a loud infra block", v)
	}
	if !strings.Contains(v.Summary, "no judge model") {
		t.Fatalf("loud verdict summary = %q, want it to name the missing judge model", v)
	}
}

func assertJudgePathFailsLoudly(t *testing.T, blocked bool, err error, rec *modelJudgeRecorder, verdicts []GuardrailVerdict) {
	t.Helper()
	if !blocked {
		t.Fatal("want blocked=true when no judge model resolves")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.calls != 0 {
		t.Fatalf("judge fn called %d times, want 0 (fail loudly, never launch on the host default)", rec.calls)
	}
	foundLoud := false
	for _, v := range verdicts {
		if v.Category == "infra" && v.Outcome == "block" && strings.Contains(v.Summary, "no judge model") {
			foundLoud = true
		}
	}
	if !foundLoud {
		t.Fatalf("verdicts = %+v, want a loud no-judge-model infra block", verdicts)
	}
}

// TestResolveGuardrailModel_TierTable covers the pure tier resolver
// (used by the daemon's judge closures and by resolveUnitGuardrailModel):
// tier precedence, backend-unservable skip, and the loud empty result.
func TestResolveGuardrailModel_TierTable(t *testing.T) {
	tests := []struct {
		name                      string
		backend                   string
		prd, task                 string
		cfgGuard, cfgVer, cfgPlan string
		want                      string
		wantErr                   error
	}{
		{"PRD tier wins on pi", "pi", "pi/prd:27b", "pi/task:27b", "g/guard:27b", "g/verify:27b", "g/plan:27b", "pi/prd:27b", nil},
		{"task tier next", "pi", "", "pi/task:27b", "g/guard:27b", "g/verify:27b", "g/plan:27b", "pi/task:27b", nil},
		{"autonomous.guardrail_model tier", "pi", "", "", "g/guard:27b", "g/verify:27b", "g/plan:27b", "g/guard:27b", nil},
		{"autonomous.verification_model tier", "pi", "", "", "", "g/verify:27b", "g/plan:27b", "g/verify:27b", nil},
		{"autonomous.planning_model tier alone", "pi", "", "", "", "", "g/plan:27b", "g/plan:27b", nil},
		{"no tier at all fails loudly", "pi", "", "", "", "", "", "", ErrNoGuardrailModel},
		{"unservable unit skipped to next tier", "pi", "claude-haiku-5-5", "pi/task:27b", "", "", "", "pi/task:27b", nil},
		{"pi-prompt family same discipline", "pi-prompt", "pp/prd:27b", "", "", "", "", "pp/prd:27b", nil},
		{"claude-code bare aliases allowed, pi-tiers skipped", "claude-code", "x/y", "haiku-4-5", "opp/model:1", "opp/model:2", "opp/model:3", "haiku-4-5", nil},
		{"claude-code unservable tiers loud", "claude-code", "a/b", "c/d", "e/f", "g/h", "i/j", "", ErrNoGuardrailModel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveGuardrailModel(tc.backend, tc.prd, tc.task, tc.cfgGuard, tc.cfgVer, tc.cfgPlan)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want one wrapping %v", err, tc.wantErr)
				}
				if got != "" {
					t.Fatalf("model = %q, want empty on loud failure", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("model = %q, want exactly %q", got, tc.want)
			}
		})
	}
}

// TestPinnedModelHelpers_EdgeBranches pins the remaining pinned-model
// helper branches: the default remediation text when the caller listed
// no configuration keys, a tier chain that is entirely unset, and tiers
// skipped as unservable by the backend.
func TestPinnedModelHelpers_EdgeBranches(t *testing.T) {
	// RequirePinnedModel with no keys listed still fails loudly with a
	// generic operator remediation hint.
	err := RequirePinnedModel("pi", "")
	if !errors.Is(err, ErrNoPinnedModel) {
		t.Fatalf("RequirePinnedModel(pi, empty) = %v, want ErrNoPinnedModel", err)
	}
	if !strings.Contains(err.Error(), "this session's model settings") {
		t.Fatalf("no-key error must carry the generic remediation hint, got: %v", err)
	}

	// A pi-family backend with a non-empty model always passes, whatever
	// the keys say.
	if err := RequirePinnedModel("pi", "ollama/qwen3.8-64k:27b", "a", "b"); err != nil {
		t.Fatalf("pinned model rejected: %v", err)
	}

	// ResolvePinnedModel entirely empty tier chain: loud, generic hint.
	if model, err := ResolvePinnedModel("pi"); model != "" || !errors.Is(err, ErrNoPinnedModel) {
		t.Fatalf("ResolvePinnedModel(pi) = %q, %v; want empty model and an error wrapping ErrNoPinnedModel", model, err)
	}

	// All tiers named but unset: the error names every listed key.
	_, err = ResolvePinnedModel("pi",
		ModelTier{Key: "prd.guardrail_model", Model: ""},
		ModelTier{Key: "task.guardrail_model", Model: "   "})
	if !errors.Is(err, ErrNoPinnedModel) {
		t.Fatalf("all-unset tiers: err = %v, want ErrNoPinnedModel", err)
	}
	for _, key := range []string{"prd.guardrail_model", "task.guardrail_model"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("all-unset error does not name key %q: %v", key, err)
		}
	}

	// A tier with an empty key but an unservable value: the skip detail
	// appears even though the key list falls back to the generic hint.
	_, err = ResolvePinnedModel("pi", ModelTier{Key: "", Model: "claude-haiku-5-5"})
	if !errors.Is(err, ErrNoPinnedModel) {
		t.Fatalf("unservable keyless tier: err = %v, want ErrNoPinnedModel", err)
	}
	if !strings.Contains(err.Error(), "this session's model settings") || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("keyless unservable error = %v, want the generic hint plus the skip detail", err)
	}

	// Blank-valued tiers are treated as unset and fall through to the
	// next servable tier.
	model, err := ResolvePinnedModel("pi",
		ModelTier{Key: "a", Model: "  "},
		ModelTier{Key: "b", Model: "claude-opus-4-8"},
		ModelTier{Key: "c", Model: "ollama/qwen3.8-64k:27b"})
	if err != nil || model != "ollama/qwen3.8-64k:27b" {
		t.Fatalf("tier fallthrough: model=%q err=%v, want exactly the third tier's model", model, err)
	}
}

// TestResolvePlannedModel_Tiers pins the decompose-session model
// precedence: PRD decomposition_model, global planning_model (paired with
// the planning backend), PRD model (only when the PRD's own backend
// matches), global verification_model (paired), and the empty result
// that callers must fail loudly on for pi-family backends.
func TestResolvePlannedModel_Tiers(t *testing.T) {
	tests := []struct {
		name                                                               string
		backend, prdDecomp, prdBack, prdModel, gBack, gPlan, gVerify, want string
	}{
		{"prd decomposition_model wins", "pi", "decomp:27b", "pi", "prd:27b", "pi", "plan:27b", "ver:27b", "decomp:27b"},
		{"global planning_model paired with backend", "pi", "", "pi", "prd:27b", "pi", "plan:27b", "ver:27b", "plan:27b"},
		{"prd model only when prd backend matches", "pi", "", "pi", "prd:27b", "pi", "", "ver:27b", "prd:27b"},
		{"prd model ignored when backends differ, falls to global verification", "pi", "", "claude-code", "prd:27b", "pi", "", "ver:27b", "ver:27b"},
		{"global verification_model is the lowest tier", "pi", "", "claude-code", "", "pi", "", "ver:27b", "ver:27b"},
		{"no tier, empty result (callers fail loudly)", "pi", "", "pi", "", "pi", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePlannedModel(tc.backend, tc.prdDecomp, tc.prdBack, tc.prdModel, tc.gBack, tc.gPlan, tc.gVerify)
			if got != tc.want {
				t.Fatalf("model = %q, want exactly %q", got, tc.want)
			}
		})
	}
}
