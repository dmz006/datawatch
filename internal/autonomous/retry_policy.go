// BL410 Phase 1 — retry-policy resolution.
//
// Mirrors resolveGuardrails' exact precedence shape (guardrail_registry.go):
// explicit PRD field > named profile > global Config default. Every
// decision here (3-tier precedence, a profile controlling behavior as
// well as numbers, the content/policy→AutoFixRetries fallback, the
// infra/scope-only defaults) was asked of and answered by the operator
// directly during BL410's design — see docs/plans/README.md's BL410
// entry and the plan file it references.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// resolveRetryPolicy returns the effective retry budget for prd at the
// given category ("infra" | "content" | "scope" | "policy"). Priority:
// explicit PRD.RetryPolicy > named RetryProfile > global Config. When
// nothing sets "content"/"policy" anywhere, falls back to the existing
// Config.AutoFixRetries — the backward-compat anchor, so a PRD that
// never touches this feature gets byte-for-byte today's retry count.
func (m *Manager) resolveRetryPolicy(prd *PRD, category string) int {
	if v, ok := prd.RetryPolicy[category]; ok {
		return v
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok {
			if v, ok := p.Budgets[category]; ok {
				return v
			}
		}
	}
	// Project/ Automaton tier: a category named in the higher tier's retry
	// policy applies before the daemon's global map (a category absent from
	// either keeps falling through).
	if v, ok := m.effectiveRetryPolicyEntry(prd, category); ok {
		return v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.cfg.RetryPolicy[category]; ok {
		return v
	}
	if category == "content" || category == "policy" {
		return m.cfg.AutoFixRetries
	}
	return 0
}

// resolveRetryCeiling returns the total auto-retry budget (summed
// across every category and unit) for prd's whole lifecycle. Priority:
// explicit PRD.RetryCeilingTotal > named RetryProfile > global Config
// (default 5, seeded by DefaultConfig).
func (m *Manager) resolveRetryCeiling(prd *PRD) int {
	if prd.RetryCeilingTotal > 0 {
		return prd.RetryCeilingTotal
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.CeilingTotal > 0 {
			return p.CeilingTotal
		}
	}
	// Project/ Automaton tier (a set value, including -1 = unlimited, wins
	// before the daemon's).
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.RetryCeilingTotal.Source) && pol.RetryCeilingTotal.Value != 0 {
		return pol.RetryCeilingTotal.Value
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.RetryCeilingTotal > 0 {
		return m.cfg.RetryCeilingTotal
	}
	return 5
}

// resolveInfraEscalateAfter returns how many retries keep the exact
// original backend/model config before an infra-category retry falls
// back to the global default. Same 3-tier precedence as above (default
// 2, seeded by DefaultConfig). PRD has no explicit per-PRD field for
// this (it is a RetryProfile-level behavior knob per the operator's
// own decision that a profile, not a loose PRD field, controls
// escalation behavior) — only the profile and global tiers apply.
func (m *Manager) resolveInfraEscalateAfter(prd *PRD) int {
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.InfraRetryEscalateAfter > 0 {
			return p.InfraRetryEscalateAfter
		}
	}
	// Project/ Automaton tier (above the daemon's, below the Automaton's
	// own profile).
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.InfraRetryEscalateAfter.Source) && pol.InfraRetryEscalateAfter.Value > 0 {
		return pol.InfraRetryEscalateAfter.Value
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.InfraRetryEscalateAfter > 0 {
		return m.cfg.InfraRetryEscalateAfter
	}
	return 2
}

// resolveContentAutoFixEnabled reports whether a content-category
// guardrail block should get auto-corrective retry (Phase 2). Priority:
// explicit PRD override > named RetryProfile > global Config (default
// true, seeded by DefaultConfig — the operator's "work auto the
// default" decision).
func (m *Manager) resolveContentAutoFixEnabled(prd *PRD) bool {
	if prd.ContentAutoFixEnabled != nil {
		return *prd.ContentAutoFixEnabled
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.ContentAutoFixEnabled != nil {
			return *p.ContentAutoFixEnabled
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.ContentAutoFixEnabled
}

// resolveAutoFixSkipApproval reports whether a synthesized corrective
// task/story (Phase 2) bypasses PRD.PerStoryApproval. Same 3-tier
// precedence and default-true as resolveContentAutoFixEnabled.
func (m *Manager) resolveAutoFixSkipApproval(prd *PRD) bool {
	if prd.AutoFixSkipApproval != nil {
		return *prd.AutoFixSkipApproval
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.AutoFixSkipApproval != nil {
			return *p.AutoFixSkipApproval
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.AutoFixSkipApproval
}

// retryBudgetRemaining reports whether prd has any budget left — both
// the per-category budget for this specific category, and the PRD-wide
// ceiling summed across every category — before a further auto-retry
// of any kind is attempted.
func (m *Manager) retryBudgetRemaining(prd *PRD, category string) bool {
	if m.prdIsHeld(prd.ID) {
		return false // no automatic retry for a held Automaton
	}
	budget := m.resolveRetryPolicy(prd, category)
	spent := prd.RetryCountsByCategory[category]
	if spent >= budget {
		return false
	}
	ceiling := m.resolveRetryCeiling(prd)
	total := 0
	for _, n := range prd.RetryCountsByCategory {
		total += n
	}
	return total < ceiling
}

// consumeRetryBudget records one more auto-retry attempt against prd
// for the given category, persisting the PRD so the consumption
// survives a daemon restart.
func (m *Manager) consumeRetryBudget(prd *PRD, category string) error {
	m.store.WithLock(func() {
		if prd.RetryCountsByCategory == nil {
			prd.RetryCountsByCategory = map[string]int{}
		}
		prd.RetryCountsByCategory[category]++
	})
	return m.savePRDOwn(prd)
}

// recordAutoRetryDecision appends the audit trail for one unattended
// auto-retry attempt. The executor loop has no HTTP request/actor
// context to route through internal/audit's CEF system (that's the
// server layer's own audit trail for operator-triggered actions) — the
// autonomous package's own already-universally-exposed audit trail is
// Decision (REST/MCP/PWA all already surface prd.Decisions), so an
// auto-retry gets one of those, not a new mechanism.
func (m *Manager) recordAutoRetryDecision(prd *PRD, unitID, category string, attempt int, escalated bool) {
	note := fmt.Sprintf("unit=%s category=%s attempt=%d", unitID, category, attempt)
	if escalated {
		note += " escalated-to-global-default"
	}
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: "auto_retry", Actor: "autonomous",
			Note: note, FailureCategory: category,
		})
	})
}

// recordFailureLearning (BL410 Phase 3) writes one Learning row via
// the already-existing store.AddLearning — the exact dead-code writer
// BL411 flagged as fully plumbed on the read side
// (autonomous_learnings/memory_learnings) and never called anywhere.
// Fires once a unit's retry budget is fully exhausted, not on every
// attempt, so a transient failure that self-heals on a later retry
// produces zero noise — only a genuinely unresolved failure leaves a
// durable record for future pattern analysis (BL410's own "eye toward
// skills improving from observed failure patterns" clause). category
// may be empty (meaning "content" per FailureReason's taxonomy); best
// effort — a write failure is logged, never propagated, matching
// every other best-effort side-write in this package (memory seed/
// harvest/report).
func (m *Manager) recordFailureLearning(prd *PRD, unitID, category, detail string) {
	if m.store == nil { // bare &Manager{} test fixtures never call NewManager
		return
	}
	cat := orContentCategory(category)
	if err := m.store.AddLearning(Learning{
		TaskID:   unitID,
		PRDID:    prd.ID,
		Category: cat,
		Text:     fmt.Sprintf("[%s] %s", cat, detail),
	}); err != nil {
		log.Printf("[autonomous] recordFailureLearning: prd=%s unit=%s: %v", prd.ID, unitID, err)
	}
}

// retryDecomposeFailure (BL410 remediation, 2026-10-13) closes the gap
// flagged live — "why was manager.decompose not wired in?" —
// PRD.LastDecomposeFailure existed since Phase 1 but nothing ever set
// or consulted it; every decompose failure rolled straight back to
// PRDDraft regardless of category or budget. Shared by Decompose() and
// decomposeStreamingCore() (a long-standing drifted-duplicate pair —
// see their own comments) so the retry logic exists in exactly one
// place rather than risking a third hand-copied gate.
//
// Records the failure as prd.LastDecomposeFailure — a single-use hint
// the caller appends into the NEXT attempt's prompt, mirroring
// Task.PriorAttemptHint's lifecycle — and reports whether budget
// remains for another attempt. Budget consumption and the hint are
// persisted immediately so a daemon restart mid-retry loses neither.
// A false return means the caller is about to roll back to PRDDraft;
// recordFailureLearning fires here, at that exact moment, matching
// every other "genuinely exhausted" Learning write in this package.
func (m *Manager) retryDecomposeFailure(prd *PRD, category, source, detail string) bool {
	reason := &FailureReason{Category: category, Detail: detail, Source: source, OccurredAt: time.Now(),
		Phrases: planningFailurePhrases(prd, source, detail)}
	if m.retryBudgetRemaining(prd, category) {
		_ = m.consumeRetryBudget(prd, category)
		reason.Retryable = true
		m.store.WithLock(func() { prd.LastDecomposeFailure = reason })
		m.recordAutoRetryDecision(prd, prd.ID, category, prd.RetryCountsByCategory[category], false)
		_ = m.store.SavePRD(prd)
		return true
	}
	reason.Retryable = false
	m.store.WithLock(func() { prd.LastDecomposeFailure = reason })
	m.recordFailureLearning(prd, prd.ID, category, detail)
	return false
}

// failDecomposeAttempt is the give-up tail shared by every decompose
// failure branch in Decompose() and decomposeStreamingCore(), called only
// after retryDecomposeFailure has decided no retry budget remains. It logs
// the PRD id and exact error, keeps the decompose output artifact under its
// .failed name for post-mortem, and rolls the PRD back to PRDDraft.
func (m *Manager) failDecomposeAttempt(prd *PRD, source string, cause error) {
	log.Printf("[autonomous] decompose FAILED prd=%s source=%q: %v", prd.ID, source, cause)
	if _, err := MarkDecomposeOutputArtifactFailed(m.dataDir, prd.ID); err != nil {
		log.Printf("[autonomous] decompose FAILED prd=%s: keep output artifact: %v", prd.ID, err)
	}
	// 2026-10-19 incident — the give-up paths that went through
	// retryDecomposeFailure already recorded a richer FailureReason; the
	// parse-failure path (a written-but-unparseable plan, "invalid-"
	// "response" category — terminal, never infra-retryable) is the one
	// that would otherwise leave LastDecomposeFailure nil. Record the
	// parse reason so an operator sees WHY (the same intent manager.go's
	// decom-parse comment states), without touching an existing record.
	var hasFailure bool
	m.store.WithLock(func() { hasFailure = prd.LastDecomposeFailure != nil })
	if !hasFailure {
		category := "content"
		retryable := false
		switch source {
		case "decompose-parse":
			category = "invalid-response"
		case "decompose-persist":
			category = "infra"
			retryable = true // a store persistence failure is operator-retryable, not a content judgement
		}
		m.store.WithLock(func() {
			prd.LastDecomposeFailure = &FailureReason{
				Category:   category,
				Detail:     cause.Error(),
				Source:     source,
				Retryable:  retryable,
				OccurredAt: time.Now(),
				Phrases:    planningFailurePhrases(prd, source, cause.Error()),
			}
		})
	}
	var old PRDStatus
	m.store.WithLock(func() {
		old = prd.Status
		prd.Status = PRDDraft
		prd.UpdatedAt = time.Now()
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s: give-up rollback save failed: %v", prd.ID, err)
		return
	}
	m.emitPRDStatus(prd, old, nil, nil)
	// A non-retryable planning failure raises its one alert and, when the
	// spec can be reworded by the lint, its one question.
	m.onPlanningFailureTerminal(prd)
}

// checkPersistedDecompose re-reads the PRD and its stories from the store
// after SetStories/SavePRD, so the success path only commits when the
// persisted state holds at least one story with at least one task, not
// merely the in-memory slice it was handed.
func (m *Manager) checkPersistedDecompose(prdID string) error {
	if _, ok := m.store.GetPRD(prdID); !ok {
		return fmt.Errorf("prd %q missing from store after decompose persist", prdID)
	}
	if err := validateParsedPlan(m.store.storiesSnapshot(prdID)); err != nil {
		return fmt.Errorf("persisted decompose: %w", err)
	}
	return nil
}

// decomposeSessionStopThreshold is how many consecutive no-output decompose
// sessions stop the PRD. The second such session is the last one spawned:
// the streak is treated as the session path being broken, not as a
// transient failure worth a third attempt.
const decomposeSessionStopThreshold = 2

// noteDecomposeSessionFailure handles a decompose error from a one-shot
// session that ended without writing its plan file (OneShotNoOutputError).
// The count is persisted before anything else, so a restart mid-streak
// keeps it. Below the threshold it seeds prd.LastDecomposeFailure as the
// next attempt's hint and returns false so the caller respawns. At the
// threshold it parks the PRD in PRDFailed and returns true: the caller
// must return the error and must not invoke the decompose fn again.
func (m *Manager) noteDecomposeSessionFailure(prd *PRD, backend, model string, derr error) bool {
	var failures int
	m.store.WithLock(func() {
		prd.DecomposeSessionFailures++
		failures = prd.DecomposeSessionFailures
	})
	now := time.Now()
	modelLabel := model
	if modelLabel == "" {
		modelLabel = "default"
	}
	if failures < decomposeSessionStopThreshold {
		m.store.WithLock(func() {
			prd.LastDecomposeFailure = &FailureReason{
				Category:   "infra",
				Source:     "decompose-session",
				Retryable:  true,
				Detail:     fmt.Sprintf("the decompose session ended without writing the plan file (%s, %s model); retrying with a fresh session", backend, modelLabel),
				OccurredAt: now,
			}
		})
		_ = m.store.SavePRD(prd)
		return false
	}
	detail := fmt.Sprintf("%d decompose sessions ended without writing a plan (%s, %s model); stopped — reset to draft to retry", failures, backend, modelLabel)
	var old PRDStatus
	m.store.WithLock(func() {
		prd.LastDecomposeFailure = &FailureReason{
			Category:   "infra",
			Source:     "decompose-session",
			Retryable:  false,
			Detail:     detail,
			OccurredAt: now,
		}
		old = prd.Status
		prd.Status = PRDFailed
		prd.UpdatedAt = now
	})
	log.Printf("[autonomous] prd=%s decompose stopped after %d no-output sessions (backend=%s, model=%s): %v",
		prd.ID, failures, backend, modelLabel, derr)
	m.recordFailureLearning(prd, prd.ID, "infra", detail)
	if m.store.SavePRD(prd) == nil {
		m.emitPRDStatus(prd, old, nil, nil)
	}
	return true
}

// isOneShotNoOutput reports whether a decompose error is the one-shot
// no-output family (see OneShotNoOutputError).
func isOneShotNoOutput(err error) bool {
	var noOutput *OneShotNoOutputError
	return errors.As(err, &noOutput)
}

// orContentCategory applies FailureReason's "empty means content"
// convention — shared by recordFailureLearning and every
// Task.FailureReason/Story.FailureReason assignment (BL410 remediation,
// 2026-10-13) so the rule lives in exactly one place.
func orContentCategory(category string) string {
	if category == "" {
		return "content"
	}
	return category
}

// runGuardrailWithInfraRetry (BL410 Phase 1) wraps a single guardrail
// invocation with bounded, escalating infra-category auto-retry — the
// direct fix for the motivating incident (a guardrail session timeout
// requiring manual operator intervention indistinguishable from a real
// compliance block). A content/scope/policy block, an error, or an
// infra block once the retry budget is exhausted, returns as-is for
// the caller to treat exactly as it does today (TaskBlocked/story
// block/PRDBlocked — Phase 1 does not change that terminal behavior,
// only how many chances a purely mechanical failure gets first).
//
// Per-commit verdict cache (judge_verdict_cache.go): before the judge
// runs, a completed pass/block verdict already recorded for exactly this
// (commit sha, judge policy, rules) triple is replayed instead; after the
// judge completes, a successful verdict is stored under that triple. A
// timed-out or otherwise unfinished judge is never cached, and with the
// cache disabled the behavior is byte-for-byte the pre-cache loop. It
// discards the reuse flag; callers that need it call runGuardrailReusing.
func (m *Manager) runGuardrailWithInfraRetry(ctx context.Context, prd *PRD, inv GuardrailInvocation, fn GuardrailFn) (GuardrailVerdict, error) {
	v, _, err := m.runGuardrailReusing(ctx, prd, inv, fn)
	return v, err
}

// runGuardrailReusing holds the body of runGuardrailWithInfraRetry and
// additionally reports whether the returned verdict is a stored one that
// was replayed (reused true, fn never called) or one the judge produced
// just now (reused false, including every error and every bypassed or
// disabled-cache run). Unless the context bypasses the lookup, a hit from
// the verdict cache is returned as-is; otherwise the infra-retry loop runs
// and its result is recorded in the cache.
//
// Judge resume at boot (judge_resume.go): if this same judgment was in
// flight when the daemon restarted, it is not judged again. A surviving
// guardrail-judge session is re-attached to — the daemon's background
// waiter collects its verdict (ConsumeJudgeResult) and it is returned —
// or a surviving, still-unrecorded result file is consumed as the
// verdict, recorded by the caller exactly as a fresh one. Only when
// neither survives does the judge below run once, as usual. The resume
// sits ahead of the cache lookup: a pending judgment's surviving verdict
// precedes any replay, and a resumed verdict is not cached (a resume is
// not a fresh judgment). A resumed verdict is not a replayed stored one,
// so it reports reused false.
func (m *Manager) runGuardrailReusing(ctx context.Context, prd *PRD, inv GuardrailInvocation, fn GuardrailFn) (v GuardrailVerdict, reused bool, err error) {
	// Judge resume at boot (judge_resume.go): if a verdict for this unit's
	// in-flight judgment can resume, it is the answer — no judge spawns.
	if rv, resumed := m.judgeResume(ctx, prd, inv); resumed {
		return *rv, false, nil
	}
	if !judgeVerdictCacheLookupBypassed(ctx) {
		if hit, ok := m.judgeVerdictCacheHit(prd, inv); ok {
			return hit, true, nil
		}
	}
	v, err = m.runGuardrailInfraRetry(ctx, prd, inv, fn)
	m.judgeVerdictCacheRecord(prd, inv, v, err)
	return v, false, err
}

// runGuardrailInfraRetry is the retry loop itself; the public entry
// runGuardrailWithInfraRetry adds the verdict cache around it.
func (m *Manager) runGuardrailInfraRetry(ctx context.Context, prd *PRD, inv GuardrailInvocation, fn GuardrailFn) (GuardrailVerdict, error) {
	attempt := 0
	for {
		v, err := fn(ctx, inv)
		if err != nil || v.Outcome != "block" || v.Category != "infra" {
			return v, err
		}
		if !m.retryBudgetRemaining(prd, "infra") {
			return v, nil
		}
		attempt++
		_ = m.consumeRetryBudget(prd, "infra")
		escalated := attempt > m.resolveInfraEscalateAfter(prd)
		if escalated {
			// Decision 6 — drop the per-unit override that just failed;
			// fall back to the global default, mirroring the exact
			// discipline resolveGuardrailBackendModel/
			// resolveVerifierBackendModel already apply to a FIRST
			// attempt, now applied to a RETRY.
			inv.GuardrailBackend = ""
			inv.GuardrailModel = ""
			inv.GuardrailPinned = false // the retry now runs on the global default
		}
		m.recordAutoRetryDecision(prd, inv.UnitID, "infra", attempt, escalated)
		_ = m.savePRDOwn(prd)
	}
}

// synthesizeCorrectiveWork (BL410 Phase 2) appends new corrective work
// to the SAME PRD — never a new PRD, per the operator's explicit
// "I don't want a new prd on retry, it should retry itself" decision —
// built directly from a content-category guardrail verdict's own
// findings. Reuses store.AddStory/AddTask as-is: the exact mechanism
// an operator already uses to manually add work, now pointed at
// content a guardrail (not an operator) supplied.
//
// targetStoryID, when non-empty (the story-level caller), appends the
// new task to THAT existing story instead of creating a new one — so
// the story's own storyAllTasksDone gate naturally goes false again
// until the fix lands, then re-fires the same per-story guardrail
// check once it completes. The PRD-level caller passes "" (no single
// story owns a PRD-wide finding), creating a standalone new story.
func (m *Manager) synthesizeCorrectiveWork(prd *PRD, verdict GuardrailVerdict, targetStoryID string) (*Story, *Task, error) {
	return m.synthesizeCorrectiveWorkAttempt(prd, verdict, targetStoryID, prd.RetryCountsByCategory["content"])
}

// synthesizeCorrectiveWorkAttempt is synthesizeCorrectiveWork with the
// attempt number named by the caller, for callers that create the work
// before consuming the retry budget.
func (m *Manager) synthesizeCorrectiveWorkAttempt(prd *PRD, verdict GuardrailVerdict, targetStoryID string, attempt int) (*Story, *Task, error) {
	if m.prdIsHeld(prd.ID) {
		return nil, nil, fmt.Errorf("automaton %s is on hold; no corrective work is added until the hold is released", prd.ID)
	}
	title := autoFixTaskTitlePrefix + fmt.Sprintf("%s findings (attempt %d)", verdict.Guardrail, attempt)
	spec := contentFixHint(verdict)

	var storyID string
	if targetStoryID != "" {
		storyID = targetStoryID
	} else {
		story, err := m.store.AddStory(prd.ID, title, spec)
		if err != nil {
			return nil, nil, err
		}
		storyID = story.ID
	}
	task, err := m.store.AddTask(prd.ID, storyID, title, spec, nil)
	if err != nil {
		return nil, nil, err
	}
	// Decision 8 — skip-approval is the default: force the new story
	// runnable regardless of the daemon-wide per_story_approval gate.
	// When the operator has explicitly turned skip-approval off, the
	// new story (if one was created above) waits for approval exactly
	// like normal decompose output — same StoryAwaitingApproval gate
	// (manager.go's own post-decompose transition), no new mechanism.
	m.mu.Lock()
	perStoryApproval := m.cfg.PerStoryApproval
	m.mu.Unlock()
	if targetStoryID == "" && !m.resolveAutoFixSkipApproval(prd) && perStoryApproval {
		fresh, ok := m.store.GetPRD(prd.ID)
		if ok {
			m.store.WithLock(func() {
				for i := range fresh.Story {
					if fresh.Story[i].ID == storyID {
						fresh.Story[i].Status = StoryAwaitingApproval
					}
				}
			})
			if err := m.store.SavePRD(fresh); err != nil {
				return nil, nil, err
			}
			if prd != fresh {
				m.store.WithLock(func() { *prd = *fresh })
			}
		}
	}
	story, _ := m.store.GetPRD(prd.ID)
	var createdStory *Story
	if story != nil {
		for i := range story.Story {
			if story.Story[i].ID == storyID {
				createdStory = &story.Story[i]
			}
		}
	}
	return createdStory, task, nil
}

// ceilingRetryParagraph returns the fixed paragraph added to the next worker
// prompt when the previous attempt was stopped for using more memory than its
// limit. It returns "" when t has no attempts or the last attempt was not a
// ceiling kill. The limit, process name and peak come from the attempt record;
// an empty process name reads as "unknown". The caller must hold the store
// lock when t is shared.
func ceilingRetryParagraph(t *Task) string {
	if t == nil || len(t.Attempts) == 0 {
		return ""
	}
	a := t.Attempts[len(t.Attempts)-1]
	if !a.MemoryKilled {
		return ""
	}
	name := a.KilledProcess
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf("The previous run was stopped because the process used more memory than the limit of %d MB (process %s, peak %d MB). "+
		"Bound your program: avoid unbounded recursion, stream instead of holding data, cap collection sizes, and test with a small input first.",
		a.CeilingMB, name, a.PeakMemoryMB)
}

// ceilingReviewThreshold is how many consecutive attempts of one task may be
// stopped for using more memory than the limit before the task is no longer
// retried. The first is retried as usual with the reason in the prompt; the
// second moves the task to review.
const ceilingReviewThreshold = 2

// ceilingReviewSource is the FailureReason.Source of a task held because of
// consecutive ceiling failures.
const ceilingReviewSource = "ceiling-hold"

// consecutiveCeilingFailures counts the attempts at the end of t's attempt
// list that were stopped for using more memory than the limit. An attempt that
// failed for another reason ends the run, so the count starts again after it.
// The caller must hold the store lock when t is shared.
func consecutiveCeilingFailures(t *Task) int {
	n := 0
	for i := len(t.Attempts) - 1; i >= 0 && t.Attempts[i].MemoryKilled; i-- {
		n++
	}
	return n
}

// holdAfterCeilingFailures is the ceiling step of the retry policy. When the
// task's last ceilingReviewThreshold attempts were all ceiling failures it
// moves the task to the review state with AttemptMemoryKillReason recorded as
// the failure reason, and reports held true so the caller does not retry. With
// fewer, it does nothing and the caller retries as usual. The error is from
// saving the task.
func (m *Manager) holdAfterCeilingFailures(prd *PRD, t *Task) (held bool, err error) {
	if m.prdIsHeld(prd.ID) {
		return false, nil // the tasks of a held Automaton keep their state
	}
	var count int
	m.store.WithLock(func() { count = consecutiveCeilingFailures(t) })
	if count < ceilingReviewThreshold {
		return false, nil
	}
	last, err := m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{Category: "infra", Detail: AttemptMemoryKillReason, Source: ceilingReviewSource},
		describe: func(t *Task) (string, string) {
			return AttemptMemoryKillReason,
				fmt.Sprintf("task %s moved to review: %s, %d attempts in a row", t.ID, AttemptMemoryKillReason, count)
		},
		report: func(t *Task, now time.Time) *ReviewSummary {
			var b strings.Builder
			fmt.Fprintf(&b, "Task %s (%s) is held for operator review: %s, in %d attempts in a row.\n\n",
				t.ID, t.Title, AttemptMemoryKillReason, count)
			return finishReviewSummary(&b, t, now)
		},
	})
	if err != nil {
		return true, err
	}
	m.raiseReviewAlert(prd, t, count, last)
	return true, nil
}
