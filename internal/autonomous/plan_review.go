// Plan review — a one-shot LLM judgment over the freshly decomposed PRD
// plan (spec vs. stories/tasks), recorded on the PRD and optionally
// blocking Manager.Approve depending on the configured mode.
//
// Modes (plan_review_mode, empty default = "off"):
//   - "off"   — runPlanReview is a no-op; PRD.PlanReview stays nil;
//     Approve is never affected. (The 2026-10-17 baseline: no
//     pre-approval review exists at all — this task adds it.)
//   - "warn"  — runs + persists the verdict + a Decision row, but is
//     never a decompose nor an approval failure.
//   - "block" — runs + persists; if the latest verdict has ok: false
//     (and was produced, i.e. not a reviewer outage) Approve refuses
//     with the summary. A reviewer outage (ReviewerFailed=true) is
//     recorded as evidence and displayed, but is NOT an approval
//     blocker — an outage must never wedge the operator's gate.
//
// A bad or unparseable reviewer response degrades to
// verdict.ReviewerFailed=true and ok:false with a "reviewer failed /
// unparseable verdict" summary; a missing backend falls back like the
// decompose/guardrail wiring (askCompatible → "ollama").
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// Finding categories (bounded set; the reviewer is instructed to use
// only these — an unknown category is still recorded verbatim so an
// operator can see what the reviewer said).
const (
	PlanReviewCategoryMissing             = "missing"
	PlanReviewCategoryWeakened            = "weakened"
	PlanReviewCategoryTooLarge            = "too_large"
	PlanReviewCategoryWrongDependency     = "wrong_dependency"
	PlanReviewCategoryFragileVerification = "fragile_verification"
	PlanReviewCategoryOutOfScope          = "out_of_scope"
)

// Plan-review modes.
const (
	PlanReviewModeOff   = "off"
	PlanReviewModeWarn  = "warn"
	PlanReviewModeBlock = "block"
)

// normalizePlanReviewMode lower-cases/ trims the raw configured mode and
// maps everything unrecognized (including empty) to "off". The
// operator-facing validity check is validPlanReviewMode: internal/config
// rejects typos at load time so a misspelling cannot silently disable a
// warn/block gate.
func normalizePlanReviewMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PlanReviewModeWarn:
		return PlanReviewModeWarn
	case PlanReviewModeBlock:
		return PlanReviewModeBlock
	default:
		return PlanReviewModeOff
	}
}

// validPlanReviewMode reports whether s is an acceptable value for
// plan_review_mode: empty or one of off/warn/block (case/trimmable).
func validPlanReviewMode(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", PlanReviewModeOff, PlanReviewModeWarn, PlanReviewModeBlock:
		return true
	}
	return false
}

// ValidPlanReviewMode is the exported form of validPlanReviewMode for the
// operator-facing config surfaces (REST PUT /api/config, MCP, comm, CLI)
// that must reject an unknown mode as a client error instead of silently
// normalizing it to off.
func ValidPlanReviewMode(s string) bool {
	return validPlanReviewMode(s)
}

// PlanFinding is one concrete problem the reviewer located in the plan.
// TaskID/TaskTitle identify the task when the finding is task-scoped
// (category "missing" is plan-scoped and may leave both empty).
type PlanFinding struct {
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	Category  string `json:"category,omitempty"` // one of the PlanReviewCategory* constants
	Detail    string `json:"detail,omitempty"`
}

// PlanReviewTask is the bounded per-task projection of a decomposed task
// that goes to the reviewer (plan projection input, not the store shape).
type PlanReviewTask struct {
	ID                 string   `json:"id,omitempty"`
	Title              string   `json:"title,omitempty"`
	Spec               string   `json:"spec,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	Kind               string   `json:"kind,omitempty"`
	VerificationScript string   `json:"verification_script,omitempty"`
}

// PlanReviewStory is the bounded per-story projection.
type PlanReviewStory struct {
	Title string           `json:"title,omitempty"`
	Tasks []PlanReviewTask `json:"tasks,omitempty"`
}

// PlanReviewRequest is exactly what the reviewer receives: PRDID, the
// (bounded) original spec, and the bounded plan projection.
type PlanReviewRequest struct {
	PRDID   string            `json:"prd_id"`
	Spec    string            `json:"spec,omitempty"`
	Stories []PlanReviewStory `json:"stories,omitempty"`
}

// Bounded-size caps for the request projection (a giant plan must not
// turn the review into a several-hundred-KB prompt).
const (
	planReviewSpecMax       = 8000
	planReviewTitleMax      = 200
	planReviewTaskSpecMax   = 4000
	planReviewFindingMaxLen = 500
)

// buildPlanReviewRequest projects a PRD (+ stored stories) into the
// bounded reviewer request. Story/task order is preserved.
func buildPlanReviewRequest(prd *PRD, stories []Story) PlanReviewRequest {
	req := PlanReviewRequest{PRDID: prd.ID, Spec: truncate(prd.Spec, planReviewSpecMax)}
	for _, s := range stories {
		ps := PlanReviewStory{Title: truncate(s.Title, planReviewTitleMax)}
		for _, t := range s.Tasks {
			pt := PlanReviewTask{
				ID:                 t.ID,
				Title:              truncate(t.Title, planReviewTitleMax),
				Spec:               truncate(t.Spec, planReviewTaskSpecMax),
				DependsOn:          t.DependsOn,
				Kind:               t.Kind,
				VerificationScript: truncate(t.VerificationScript, planReviewTaskSpecMax),
			}
			if len(t.DependsOn) == 0 {
				pt.DependsOn = nil
			}
			ps.Tasks = append(ps.Tasks, pt)
		}
		if len(ps.Tasks) == 0 {
			ps.Tasks = nil
		}
		req.Stories = append(req.Stories, ps)
	}
	if len(req.Stories) == 0 {
		req.Stories = nil
	}
	return req
}

// PlanReviewVerdict is the persisted result of one review (latest wins —
// re-running ReviewPlan or re-decomposing REPLACES it).
type PlanReviewVerdict struct {
	// Ok is the reviewer's overall judgment: true = plan is faithful and
	// correctly scoped; false = the reviewer found blocking-grade gaps
	// (see Findings).
	Ok bool `json:"ok"`
	// Severity — low | medium | high (empty allowed; informational).
	Severity string `json:"severity,omitempty"`
	// Summary — the reviewer's overall one-line note.
	Summary string `json:"summary,omitempty"`
	// Findings — concrete problems. Nil for an ok:true verdict.
	Findings []PlanFinding `json:"findings,omitempty"`
	// Backend / Model — the reviewer identity actually used (set by the
	// daemon-side fn; informational).
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	// Mode — the configured mode at the time this verdict was produced.
	Mode string `json:"mode,omitempty"`
	// At — decision time.
	At time.Time `json:"at"`
	// Prompt/Response sizes of the reviewer LLM call (bounded input was
	// the prompt; the raw answer was the response).
	PromptChars   int `json:"prompt_chars,omitempty"`
	ResponseChars int `json:"response_chars,omitempty"`
	// Reviewer spend (0 when the backend response does not expose it —
	// /api/ask answers do not today). The automation budget policy reads
	// these off the verdict's PRD for its PRD-level cost accounting.
	TokensIn   int     `json:"tokens_in,omitempty"`
	TokensOut  int     `json:"tokens_out,omitempty"`
	EstCostUSD float64 `json:"est_cost_usd,omitempty"`
	// ReviewerFailed distinguishes "the review call itself failed or
	// returned something we could not parse" from a genuine bad verdict:
	// true means the verdict above is a placeholder (ok defaults false)
	// and Approve must NOT treat it as a blocking judgment.
	ReviewerFailed bool `json:"reviewer_failed,omitempty"`
}

// PlanReviewFn is the daemon-side reviewer entry point (wired via
// Manager.SetPlanReview; the concrete implementation lives in
// cmd/datawatch/main.go and calls the configured backend).
type PlanReviewFn func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error)

// runPlanReview runs the configured plan review on a freshly decomposed
// PRD (or on ReviewPlan's explicit re-run). It NEVER fails its caller:
// every failure mode is represented by a recorded verdict.
//
// The caller passes the store-fresh (Get) PRD; runPlanReview re-saves via
// savePlanReview. stories come from the just-decomposed plan (identical
// to what the store holds post-persist). actor is the explicit caller
// (e.g. "operator" / UI) when re-running via ReviewPlan; empty at
// decompose time.
func (m *Manager) runPlanReview(prd *PRD, stories []Story, actor string) *PlanReviewVerdict {
	if prd == nil {
		return nil
	}
	mode := normalizePlanReviewMode(m.cfg.PlanReviewMode)
	if mode == PlanReviewModeOff {
		return nil
	}
	req := buildPlanReviewRequest(prd, stories)
	if m.planReview == nil {
		// Mode explicitly asked for a review but no reviewer is wired:
		// degraded 'skipped' verdict (recorded so the operator sees the
		// gap), never a decompose/review failure.
		verdict := PlanReviewVerdict{
			Ok:             false,
			Severity:       "low",
			Summary:        "plan review skipped: no reviewer configured (mode " + mode + ")",
			Mode:           mode,
			At:             time.Now(),
			ReviewerFailed: true,
		}
		log.Printf("[autonomous] plan-review: mode %q requested but no reviewer wired; skipping (prd %s)", mode, prd.ID)
		m.savePlanReview(prd, &verdict, actor)
		return &verdict
	}
	verdict, err := m.planReview(context.Background(), req)
	if err != nil {
		// Reviewer outage: record, do not block approval.
		verdict = PlanReviewVerdict{
			Ok:             false,
			Severity:       "medium",
			Summary:        "reviewer failed: " + err.Error(),
			Mode:           mode,
			At:             time.Now(),
			ReviewerFailed: true,
			PromptChars:    len(req.Spec),
		}
		m.savePlanReview(prd, &verdict, actor)
		return &verdict
	}
	// A zero / empty non-ok verdict is unparseable-by-construction:
	// treat it like a reviewer failure (the daemon-side fn already
	// marks truly-unparseable output; this is the belt-and-braces case
	// where it slipped through).
	if !verdict.Ok && !verdict.ReviewerFailed && len(verdict.Findings) == 0 && verdict.Summary == "" {
		verdict = PlanReviewVerdict{
			Ok:             false,
			Severity:       "medium",
			Summary:        "reviewer returned an empty verdict; treated as unparseable",
			Mode:           mode,
			At:             time.Now(),
			ReviewerFailed: true,
			PromptChars:    len(req.Spec),
		}
		m.savePlanReview(prd, &verdict, actor)
		return &verdict
	}
	verdict.Mode = mode
	if verdict.At.IsZero() {
		verdict.At = time.Now()
	}
	m.savePlanReview(prd, &verdict, actor)
	return &verdict
}

// savePlanReview persists the latest verdict on the PRD (+ one Decision
// row of kind "plan_review") and notifies the UI. The verdict row is
// always appended after the decompose row. actor, when non-empty, is
// recorded at the head of the decision Note.
func (m *Manager) savePlanReview(prd *PRD, verdict *PlanReviewVerdict, actor string) {
	note := verdict.Summary
	if actor != "" {
		note = actor + ": " + note
	}
	decision := Decision{
		At:             verdict.At,
		Kind:           "plan_review",
		Actor:          "autonomous",
		Backend:        verdict.Backend,
		Model:          verdict.Model,
		PromptChars:    verdict.PromptChars,
		ResponseChars:  verdict.ResponseChars,
		CostUSD:        verdict.EstCostUSD,
		VerdictOutcome: verdict.Outcome(),
		Note:           note,
	}
	// Mutate the store-fresh PRD in place (the shared stored pointer, as
	// Decompose/Approve do) and persist; SavePRD trims Decisions.
	prd.PlanReview = verdict
	prd.Decisions = append(prd.Decisions, decision)
	prd.UpdatedAt = time.Now()
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] plan-review: save failed: %v", err)
	}
	m.emitPRDStatus(prd, prd.Status, &decision, nil)
	// BL245 — SSE on the PRD channel so the UI re-fetches.
	m.EmitPRDUpdate(prd.ID)
}

// Outcome is the Decision-side outcome string for a verdict.
func (v *PlanReviewVerdict) Outcome() string {
	if v.ReviewerFailed {
		return "n/a"
	}
	if v.Ok {
		return "pass"
	}
	return "block"
}

// planReviewCategories returns the unique finding categories (first
// appearance order) for operator-facing summary strings (the Approve
// gate's error message).
func planReviewCategories(fs []PlanFinding) []string {
	seen := make(map[string]bool, len(fs))
	var out []string
	for _, f := range fs {
		if c := strings.TrimSpace(f.Category); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// ReviewPlan re-runs the plan review for a stored PRD (an explicit
// operator/API action). Re-running (or re-decomposing) REPLACES the
// previous verdict, so a new ok:true verdict clears a blocking one.
//
// Skipped entirely when the mode normalizes to off (returns the PRD as
// stored); when no reviewer is wired and the mode asked for a review, a
// degraded 'skipped' verdict is recorded (never a failure).
func (m *Manager) ReviewPlan(prdID string, actor string) (*PRD, error) {
	pr, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if normalizePlanReviewMode(m.cfg.PlanReviewMode) == PlanReviewModeOff {
		return pr, nil // off: nothing to run, nothing to clear
	}
	stories := pr.Story // stored on the PRD (Decompose sets prd.Story)
	m.runPlanReview(pr, stories, actor)
	updated, ok := m.store.GetPRD(prdID)
	if !ok {
		return pr, nil
	}
	return updated, nil
}
