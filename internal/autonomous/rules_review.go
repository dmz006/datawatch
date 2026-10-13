// PRD-completion rules review (B124 extension, 2026-10-12).
//
// Found live auditing a completed Automaton (BL403 Phase 4) by hand:
// several AGENT.md Section A/B items had silently never happened —
// version collided with an already-shipped release, the backlog
// tracker was never updated, README's own release line was stale, a
// required CHANGELOG subsection was missing. Neither the per-task/
// per-story guardrails nor the deterministic prd_complete project-
// rules scan (runPRDCompletionProjectRulesCheck) catch any of this:
// guardrails only ever judge a unit against its OWN narrow spec, and
// the deterministic scan only matches a short list of hand-written
// regex rules — nothing asks "does this PRD's actual diff comply with
// whatever this project's own rules file says," end to end.
//
// Operator decision (2026-10-12): do NOT build that as a scripted
// checklist. A scripted check would have to hardcode THIS project's
// own rule content (AGENT.md Section A/B's specific items), which
// breaks the moment a different operator's repo has a different
// rules file, or none at all. The pieces to solve this already
// exist and are generic: rulesContextSuffix already points any
// tooled PRD's planning session at whatever rules_file/context_file
// that operator configured (default AGENT.md/CONTEXT.md, but never
// hardcoded content); the B124 fix chain already gives the verify/
// guardrail path real tool access when the configured backend
// supports it. What was missing was simply a completion-time call
// that reuses both — pointing the existing guardrail mechanism at
// the rules file the exact same way, but for a holistic judgment
// review instead of a fixed checklist.
//
// Follow-up (2026-10-12, same day): shipped unconditionally-on
// whenever a rules/context file exists -- every OTHER guardrail in
// this codebase (task/story-level) is operator-configured via an
// explicit opt-in list, and this one wasn't. Fixed by giving it the
// identical resolveGuardrails("prd") precedence
// (PRD.PerPRDGuardrails > GuardrailProfile > Config.PerPRDGuardrails,
// all empty-by-default) and gating on "rules-review" appearing in
// that resolved list -- see runPRDCompletionRulesReview's first line.
package autonomous

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

// rulesReviewSuffix returns the review-time pointer text for whichever
// of the configured rules/context files actually exist in
// prd.ProjectDir — the completion-time sibling of rulesContextSuffix's
// own planning-time pointer (same gate, same file-detection logic,
// different prose: a review task reads the real diff against the
// rules, not a spec against them before writing one). Empty ProjectDir
// or no file found means there's nothing to check against, so the
// review is skipped entirely by the caller (see
// runPRDCompletionRulesReview) — exactly like rulesContextSuffix's own
// behavior for decompose.
func (m *Manager) rulesReviewSuffix(prd *PRD) string {
	if prd.ProjectDir == "" {
		return ""
	}
	rulesFile, contextFile := m.rulesContextFilenames()
	var names []string
	if fileExists(filepath.Join(prd.ProjectDir, rulesFile)) {
		names = append(names, rulesFile)
	}
	if fileExists(filepath.Join(prd.ProjectDir, contextFile)) {
		names = append(names, contextFile)
	}
	if len(names) == 0 {
		return ""
	}
	list := names[0]
	if len(names) > 1 {
		list = names[0] + " and " + names[1]
	}
	return "Read " + list + " at the root of your working directory yourself," +
		" using your own tools — they define this project's development," +
		" testing and documentation rules. Then check the REAL changes this" +
		" automaton actually made (run your own git log / git diff against" +
		" the branch this worktree started from — do not assume; look) against" +
		" those rules. This is a judgment review, not a fixed checklist: some" +
		" rules will apply and some won't, and only you can tell which, since" +
		" you can see what actually changed and what the rules actually say." +
		" Report every rule you find violated or skipped, not just the" +
		" first one you notice. A clean pass is a real possibility — do not" +
		" invent an issue just to have something to report."
}

// runPRDCompletionRulesReview fires once, right before a PRD rolls up
// to PRDCompleted (see executor.go's rollup), reusing the exact same
// GuardrailFn indirection (and therefore the exact same B124 session-
// routing: a tool-capable configured backend gets real file/tool
// access, same as any other guardrail call) that per-task/per-story
// guardrails already use — "rules-review" is simply a guardrail name
// that isn't in the scan catalog, so it falls through to the ordinary
// LLM-judgment path like any other non-scan guardrail name would.
//
// Skipped entirely (not an error) when: "rules-review" is not in the
// resolved PRD-level guardrail list (operator opt-in — see
// resolveGuardrails and Config.PerPRDGuardrails/PRD.PerPRDGuardrails,
// BL418 configurability follow-up), no GuardrailFn is wired (e.g. in
// tests that never call SetGuardrail), or rulesReviewSuffix finds no
// rules/context file to check against (headless PRD, or a repo with
// neither file configured nor present — nothing to review).
func (m *Manager) runPRDCompletionRulesReview(ctx context.Context, prd *PRD) (blocked bool, err error) {
	if !slices.Contains(m.resolveGuardrails(prd, "prd"), "rules-review") {
		return false, nil
	}
	m.mu.Lock()
	fn := m.guardrail
	globalBackend, globalModel := m.cfg.VerificationBackend, m.cfg.VerificationModel
	m.mu.Unlock()
	if fn == nil {
		return false, nil
	}
	suffix := m.rulesReviewSuffix(prd)
	if suffix == "" {
		return false, nil
	}

	inv := GuardrailInvocation{
		PRDID:      prd.ID,
		Level:      "prd",
		UnitID:     prd.ID,
		UnitTitle:  prd.Title,
		UnitSpec:   fmt.Sprintf("Original request for this automaton:\n%s\n\n%s", prd.Spec, suffix),
		Guardrail:  "rules-review",
		ProjectDir: prd.ProjectDir,
		OwnerPeer:  prd.OwnerPeer, // BL369 Layer 3 — federation trust boundary
	}
	// No PRD-level Task to inherit from — resolveGuardrailBackendModel's
	// task-level branch never fires, so this resolves PRD → global,
	// identical to runPerStoryGuardrails' own reuse of the same helper.
	inv.GuardrailBackend, inv.GuardrailModel = resolveGuardrailBackendModel(&Task{}, prd, globalBackend, globalModel)

	// BL410 Phase 1 — bounded, escalating auto-retry for an infra-
	// category block (the guardrail session itself failed — this is
	// exactly the incident that triggered BL410: a 10-minute session
	// timeout required manual operator intervention, indistinguishable
	// from a genuine AGENT.md finding). A content block — a real
	// judgment, like rules-review's own post-fix `warn` — falls
	// through unchanged to the PRDBlocked handling below.
	v, ferr := m.runGuardrailWithInfraRetry(ctx, prd, inv, fn)
	if ferr != nil {
		return false, fmt.Errorf("rules review for prd %s: %w", prd.ID, ferr)
	}
	if v.VerdictAt.IsZero() {
		v.VerdictAt = time.Now()
	}
	v.Guardrail = "rules-review"
	prd.Verdicts = append(prd.Verdicts, v)
	return v.Outcome == "block", nil
}
