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
	"log"
	"slices"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// rulesReviewSuffix returns the review-time text for whichever of the
// configured rules/context files exist — the completion-time sibling of
// rulesContextSuffix's own planning-time pointer (same gate, different
// prose: a review task reads the real diff against the rules, not a spec
// against them before writing one). Empty ProjectDir or no file found
// means there's nothing to check against, so the review is skipped
// entirely by the caller (see runPRDCompletionRulesReview) — exactly
// like rulesContextSuffix's own behavior for decompose.
//
// With autonomous.automation.rules_review_source = base_commit (the
// default) the rules text comes from the Automaton's base commit and is
// embedded verbatim, never read from the working tree the worker changed:
// a worker can neither weaken the rules it is graded against nor skip the
// review by deleting the rules file. Existence is decided at the base
// commit. When the base cannot be resolved or a git read fails, the old
// working-tree behaviour is used for that run and one
// rules_review_source_fallback Decision is appended to the Automaton.
// With the worktree source the working-directory pointer text is kept.
func (m *Manager) rulesReviewSuffix(ctx context.Context, prd *PRD) string {
	if prd.ProjectDir == "" {
		return ""
	}
	if m.Config().Automation.EffectiveRulesReviewSource() == config.RulesReviewSourceBaseCommit {
		files, sha, _, err := m.readRulesFilesAtBase(ctx, prd)
		if err == nil {
			text := baseCommitRulesReviewText(files, sha)
			if text == "" {
				return ""
			}
			return text + m.generatedArtefactReviewText(prd)
		}
		m.recordRulesReviewSourceFallback(prd, err)
	}
	return m.worktreeRulesReviewSuffix(prd)
}

// recordRulesReviewSourceFallback logs and records (one Decision, Kind
// rules_review_source_fallback) that the rules review is reading the
// working tree this run because the base commit could not be used. The
// caller's own save persists the Decision.
func (m *Manager) recordRulesReviewSourceFallback(prd *PRD, reason error) {
	log.Printf("[autonomous] %s: rules review falls back to the working-tree rules files: %v", prd.ID, reason)
	prd.Decisions = append(prd.Decisions, Decision{
		At:    time.Now(),
		Kind:  "rules_review_source_fallback",
		Actor: "autonomous",
		Note:  "the rules review read the rules files from the working tree because the Automaton base commit could not be used: " + reason.Error(),
	})
}

// worktreeRulesReviewSuffix is the working-tree pointer text: the judge is
// told to read the files at the root of its working directory.
func (m *Manager) worktreeRulesReviewSuffix(prd *PRD) string {
	list := m.presentRulesContextFiles(prd)
	if list == "" {
		return ""
	}
	return "Read " + list + " at the root of your working directory yourself," +
		" using your own tools — they define this project's development," +
		" testing and documentation rules. " + rulesReviewJudgmentText +
		m.generatedArtefactReviewText(prd)
}

// rulesReviewJudgmentText is the judgment instruction shared by both
// sources.
const rulesReviewJudgmentText = "Then check the REAL changes this" +
	" automaton actually made (run your own git log / git diff against" +
	" the branch this worktree started from — do not assume; look) against" +
	" those rules. This is a judgment review, not a fixed checklist: some" +
	" rules will apply and some won't, and only you can tell which, since" +
	" you can see what actually changed and what the rules actually say." +
	" Report every rule you find violated or skipped, not just the" +
	" first one you notice. A clean pass is a real possibility — do not" +
	" invent an issue just to have something to report."

// baseCommitRulesReviewText embeds each rules/context file as it was at the
// base commit in a delimited block. No file at the base means "" (the
// review is skipped).
func baseCommitRulesReviewText(files []baseRulesFile, sha string) string {
	if len(files) == 0 {
		return ""
	}
	short := shortSHA(sha)
	var b strings.Builder
	b.WriteString("The text of this project's rules, taken from the commit before this" +
		" Automaton's changes, is given below. They define this project's development," +
		" testing and documentation rules. Do NOT read the rules from your working" +
		" directory: the working-tree copy may have been changed by the work you are" +
		" judging. If the working-tree copy differs from the text given here, report" +
		" the difference as a finding.\n\n")
	for _, f := range files {
		heading := fmt.Sprintf("Rules file %s as of commit %s (the state before this Automaton's changes)", f.Path, short)
		b.WriteString("===== BEGIN " + heading + " =====\n")
		b.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			b.WriteString("\n")
		}
		if f.Truncated {
			fmt.Fprintf(&b, "[truncated: %s is longer than %d bytes; the text above is only its first part]\n", f.Path, baseCommitFileCap)
		}
		b.WriteString("===== END " + heading + " =====\n\n")
	}
	b.WriteString(rulesReviewJudgmentText)
	return b.String()
}

// presentRulesContextFiles returns whichever of the resolved rules/
// context files (m.resolvedRulesContext) actually exist at the root of
// prd.ProjectDir, joined for prose ("AGENT.md" or "AGENT.md and
// CONTEXT.md" or "A, B and C"), or "" when ProjectDir is empty or none
// exists. Shared by rulesReviewSuffix and ciWatchSuffix so both
// completion-time judgments point at exactly the same files.
func (m *Manager) presentRulesContextFiles(prd *PRD) string {
	return joinRulesContextNames(presentRulesContextNames(prd, m.resolvedRulesContext(prd)))
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
//
// Returns (blocked, contentFixVerdict, err) — see runPerStoryGuardrails'
// identical doc comment for contentFixVerdict's meaning (BL410 Phase 2):
// there is no single task/story a PRD-wide finding belongs to, so a
// content-category block eligible for auto-fix is handed back for the
// caller to synthesizeCorrectiveWork from, rather than retried here.
// prdRulesReviewInvocation builds the "rules-review" GuardrailInvocation
// for a whole-PRD judgment: Level "prd", the PRD itself as the judged
// unit, the PRD spec plus the rulesReviewSuffix pointer text as the unit
// spec, and the judge backend/model from resolveUnitGuardrailModel
// (v9.10.18) — no PRD-level Task to inherit from, so the task-level
// model tier never fires, identical to runPerStoryGuardrails' own reuse
// of the same helper. Shared by runPRDCompletionRulesReview's
// completion rollup and RejudgeCompletionGuardrail's
// operator-triggered re-judge so the two invocations are built in
// exactly one place and can't drift apart.
//
// Returns an error wrapping ErrNoGuardrailModel when no tier resolves a
// model the backend can serve; callers must then record
// NoGuardrailModelVerdict instead of launching a judge (v9.10.18).
func (m *Manager) prdRulesReviewInvocation(ctx context.Context, prd *PRD) (GuardrailInvocation, error) {
	inv, err := m.buildPRDRulesReviewInvocation(prd, m.rulesReviewSuffix(ctx, prd))
	m.stampReviewIdentity(ctx, prd, &inv)
	return inv, err
}

// buildPRDRulesReviewInvocation is prdRulesReviewInvocation with the
// review text already computed, so a caller that needs the text itself
// (the completion rollup's skip check) computes it once.
func (m *Manager) buildPRDRulesReviewInvocation(prd *PRD, suffix string) (GuardrailInvocation, error) {
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
	gb, gm, pinned, resErr := m.resolveUnitGuardrailModel(&Task{}, prd)
	if resErr == nil {
		inv.GuardrailBackend, inv.GuardrailModel = gb, gm
		inv.GuardrailPinned = pinned
	}
	return inv, resErr
}

func (m *Manager) runPRDCompletionRulesReview(ctx context.Context, prd *PRD) (blocked bool, contentFixVerdict *GuardrailVerdict, err error) {
	if !slices.Contains(m.resolveGuardrails(prd, "prd"), "rules-review") {
		return false, nil, nil
	}
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	if fn == nil {
		return false, nil, nil
	}
	suffix := m.rulesReviewSuffix(ctx, prd)
	if suffix == "" {
		return false, nil, nil
	}

	inv, resErr := m.buildPRDRulesReviewInvocation(prd, suffix)
	m.stampReviewIdentity(ctx, prd, &inv)

	// BL410 Phase 1 — bounded, escalating auto-retry for an infra-
	// category block (the guardrail session itself failed — this is
	// exactly the incident that triggered BL410: a 10-minute session
	// timeout required manual operator intervention, indistinguishable
	// from a genuine AGENT.md finding). A content block — a real
	// judgment, like rules-review's own post-fix `warn` — falls
	// through unchanged to the PRDBlocked handling below.
	//
	// v9.10.18 — an unresolvable judge model is a loud, NON-retried
	// infra block (constructed directly, never through the retry
	// wrapper below) — retrying cannot configure a model that is not
	// configured, and launching the judge would fall back to the
	// backend's built-in default model.
	var v GuardrailVerdict
	var ferr error
	if resErr != nil {
		v = NoGuardrailModelVerdict("rules-review", resErr)
	} else {
		v, ferr = m.runRulesReviewJudge(ctx, prd, inv, fn)
		v = m.classifyGate(ctx, inv, v)
	}
	if ferr != nil {
		return false, nil, fmt.Errorf("rules review for prd %s: %w", prd.ID, ferr)
	}
	if v.VerdictAt.IsZero() {
		v.VerdictAt = time.Now()
	}
	v.Guardrail = "rules-review"
	// Anchor the completion verdict to the head it judged, so a later
	// rejudge can tell whether it still describes the current head. This
	// is recorded whether or not the verdict cache is enabled.
	v.JudgedCommitSHA = judgeVerdictCommitSHA(prd, inv)
	v.RulesFingerprint = inv.RulesFingerprint
	m.store.WithLock(func() { prd.Verdicts = append(prd.Verdicts, v) })
	if err := m.recordGuardrailVerdict(prd, prd.ID, v); err != nil {
		return false, nil, err
	}
	m.recordJudgeOutcomeKind(prd.ID, v)
	if v.Outcome != "block" {
		return false, nil, nil
	}
	// BL410 Phase 2 — a content-category block (a real judgment, like
	// rules-review's own post-B127-fix `warn`/`block`) gets one
	// auto-fix attempt when enabled and budget remains.
	// Eligibility only: the budget is consumed by the rollup after the
	// corrective task exists, so a failed synthesis never spends a retry.
	if v.Category == "" && m.resolveContentAutoFixEnabled(prd) && m.retryBudgetRemaining(prd, "content") {
		vCopy := v
		return false, &vCopy, nil
	}
	m.recordFailureLearning(prd, prd.ID, v.Category, contentFixHint(v)) // BL410 Phase 3
	return true, nil, nil
}

// judgeOutcomeWording is the plain-language wording of a judge outcome kind,
// used in the daemon log line and as the Events headline. "" for no judge
// problem.
func judgeOutcomeWording(kind string) string {
	switch kind {
	case JudgeKindCouldNotStart:
		return "judge could not start"
	case JudgeKindTimedOut:
		return "judge timed out"
	case JudgeKindFoundProblem:
		return "judge found a problem"
	}
	return ""
}

// recordJudgeOutcomeKind writes the outcome kind of a recorded judge result
// into the daemon log and into the Events entry for the Automaton. A result
// that is not one of the three kinds writes nothing.
func (m *Manager) recordJudgeOutcomeKind(prdID string, v GuardrailVerdict) {
	wording := judgeOutcomeWording(judgeOutcomeKind(v))
	if wording == "" {
		return
	}
	log.Printf("[autonomous] automaton=%s: %s", prdID, wording)
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn(audit.ActionJudgeOutcome, map[string]any{
			"automaton_id": prdID,
			"kind":         judgeOutcomeKind(v),
			"headline":     wording,
		})
	}
}

// judgeCouldNotStartTryKey is the judge-tries key of the could-not-start
// attempts of one judge call. It differs from the key the judge runner uses
// for its timed-out attempts, so the runner's own clear and restore never touch
// these records.
func judgeCouldNotStartTryKey(inv GuardrailInvocation) string {
	return inv.UnitID + "|" + inv.Guardrail + "|" + InfraClassJudgeCouldNotStart
}

// judgeCouldNotStart reports whether a judge run ended because its main prompt
// was never delivered: the run returned that error, or an infra block whose
// text says so.
func judgeCouldNotStart(v GuardrailVerdict, err error) bool {
	if err != nil {
		class, ok := ClassifyInfraFailure(err)
		return ok && class == InfraClassJudgeCouldNotStart
	}
	if v.Outcome != "block" || v.Category != "infra" {
		return false
	}
	class, ok := ClassifyInfraText(strings.Join(append([]string{v.Summary, v.RawJudge}, v.Issues...), "\n"))
	return ok && class == InfraClassJudgeCouldNotStart
}

// nextJudgeBackend returns the first backend of the judge policy (else the
// verifier policy) that is not in tried, or "" when none is left.
func (m *Manager) nextJudgeBackend(tried []string) string {
	cfg := m.Config()
	list := config.NormalizeJudgePolicy(cfg.JudgePolicy)
	if len(list) == 0 {
		list = config.NormalizeVerifierPolicyBackends(cfg.VerifierPolicy.Backends)
	}
	for _, b := range list {
		if !slices.Contains(tried, b) {
			return b
		}
	}
	return ""
}

// runRulesReviewJudge runs the rules-review judge. A judge whose main prompt
// was never delivered (judge_could_not_start) is an infrastructure failure, not
// a finding: it is retried on the next backend of the judge policy when one is
// left, otherwise on the same backend after the infrastructure retry delay. Each
// attempt is recorded in the judge tries. The retry limit and delays are the
// existing infra_retry ones. While tries remain nothing is recorded as a
// verdict, so the Automaton is not blocked; once they are used up the last
// result is returned unchanged.
func (m *Manager) runRulesReviewJudge(ctx context.Context, prd *PRD, inv GuardrailInvocation, fn GuardrailFn) (GuardrailVerdict, error) {
	cfg := m.infraRetryConfig()
	key := judgeCouldNotStartTryKey(inv)
	used := len(m.JudgeTries(prd.ID, key))
	for {
		v, err := m.runGuardrailWithInfraRetry(ctx, prd, inv, fn)
		if !judgeCouldNotStart(v, err) || !cfg.EffectiveEnabled() {
			_ = m.ClearJudgeTries(prd.ID, key)
			return v, err
		}
		started := time.Now()
		backend := inv.GuardrailBackend
		if rerr := m.RecordJudgeTry(prd.ID, key, JudgeAttemptRecord{
			Backend: backend, Started: started, Outcome: JudgeOutcomeCouldNotStart,
		}); rerr != nil {
			log.Printf("[autonomous] prd=%s: record judge could-not-start try: %v", prd.ID, rerr)
		}
		used++
		max := cfg.EffectiveMaxTries()
		if used > max {
			_ = m.ClearJudgeTries(prd.ID, key)
			return v, err
		}
		var tried []string
		for _, t := range m.JudgeTries(prd.ID, key) {
			tried = append(tried, t.Backend)
		}
		next := m.nextJudgeBackend(tried)
		delay := time.Duration(0)
		action := "retrying on backend " + next
		if next != "" {
			inv.GuardrailBackend, inv.GuardrailModel, inv.GuardrailPinned = next, "", false
		} else {
			delay = infraBackoffDelay(cfg, used)
			action = fmt.Sprintf("retrying the same backend in %s", delay)
		}
		m.store.WithLock(func() {
			prd.Decisions = append(prd.Decisions, Decision{
				At: time.Now(), Kind: infraRetryDecisionKind, Actor: "autonomous",
				Note: fmt.Sprintf("rules-review judge could not start (%s): retry %d of %d, %s",
					InfraClassJudgeCouldNotStart, used, max, action),
				FailureCategory: infraRetryFailureCategory,
			})
		})
		_ = m.savePRDOwn(prd)
		if serr := infraSleep(ctx, delay); serr != nil {
			return GuardrailVerdict{}, WithContextCause(ctx, serr)
		}
	}
}
