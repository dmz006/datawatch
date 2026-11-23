// Rejudgment of the PRD-completion guardrail.
//
// A PRD that ended PRDBlocked on its completion-time "rules-review"
// judgment is stuck awaiting operator action. This gives the operator a
// path to ask for the SAME judgment to run again — e.g. after the finding
// has been addressed or the prior verdict was an infra-category fluke —
// without resetting or editing any task.
//
// RejudgeCompletionGuardrail is deliberately narrow in what it accepts:
// the PRD must exist, have every task terminal, and be one of:
//   - PRDBlocked (work done, the verdict blocked);
//   - PRDNeedsReview held by a rules-review retries-spent or
//     auto-fix-disabled decision;
//   - completed or archived (PRDCompleted or PRDArchived) with its work
//     branch still carrying commits not on the base branch (its merge
//     state is finished_not_merged) and its completion review missing or
//     its last verdict a block. In that case the review re-runs once for
//     the current head commit, and a pass clears the stale block records
//     (BlockedReason, RulesReviewOutcome) while the record keeps its
//     completed/archived status — a pass un-blocks the merge step, it
//     does not un-archive a record.
//
// It then runs
// the identical rules-review GuardrailInvocation the completion rollup
// runs (prdRulesReviewInvocation) through the same
// runGuardrailWithInfraRetry infra-retry wrapper that
// runPRDCompletionRulesReview runs, and records the outcome itself: the
// verdict is durably saved FIRST (recordGuardrailVerdict), a
// "guardrail_rejudge" Decision with the acting operator names the
// outcome, and only then does the status change — a pass unblocks the
// PRD (PRDCompleted, BlockedReason cleared, one CommitPRDIfRev save
// following commitRollupStatus's concurrency guard), a block keeps the
// PRD PRDBlocked with BlockedReason set from the verdict, and no verdict
// (judge error or empty outcome) leaves the PRD blocked with its
// previous BlockedReason and returns an error so the operator sees it.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// judgeCompletionRulesReview runs the PRD-level "rules-review" judgment
// for prd and returns its raw verdict and whether that verdict is a stored
// one that was reused (no new review ran), without recording anything on
// the PRD (no Verdicts append, no Decision, no status change). It is the
// shared judge step of RejudgeCompletionGuardrail: building the
// invocation, the infra-retry wrapper and the guardrail wiring are
// exactly what runPRDCompletionRulesReview uses, differing only in that
// a missing guardrail function is an error here (an operator asked for a
// judgment and got none) instead of the silent skip the completion
// rollup applies (opt-in feature never turned on).
//
// backend names an explicit judge backend for this run — the reroute of a
// review state with action "reroute": the prior judgment timed out on
// every backend it tried, and the operator asks for the re-judgment on a
// stronger backend. It wins over the invocation's resolved tier and is
// pinned, so the guardrail runner does not fall back behind it, and it
// also makes the run possible when no tier resolves (a reroute names the
// backend directly). Empty re-runs the resolved tier.
func (m *Manager) judgeCompletionRulesReview(ctx context.Context, prd *PRD, backend string) (GuardrailVerdict, bool, error) {
	m.mu.Lock()
	fn := m.guardrail
	m.mu.Unlock()
	if fn == nil {
		return GuardrailVerdict{}, false, fmt.Errorf("no guardrail function is wired for prd %s (SetGuardrail); the rules-review judgment cannot run", prd.ID)
	}
	inv, resErr := m.prdRulesReviewInvocation(ctx, prd)
	inv.Rejudge = true // re-judgment of the same rules-review decision (not a first judgment) — launch-tag only, see GuardrailInvocation.Rejudge
	backend = strings.TrimSpace(backend)
	if resErr != nil && backend == "" {
		// v9.10.18 — no judge model resolvable at any level: loud,
		// non-retried infra block, recorded by RejudgeCompletionGuardrail
		// exactly like any other block verdict.
		return NoGuardrailModelVerdict("rules-review", resErr), false, nil
	}
	if backend != "" {
		// Reroute: the operator's explicit choice of judge backend wins
		// over whatever tier resolves, and pinned keeps the guardrail
		// runner off its default fallback behind it.
		inv.GuardrailBackend = backend
		inv.GuardrailModel = ""
		inv.GuardrailPinned = true
	}
	return m.runGuardrailReusing(ctx, prd, inv, fn)
}

// firstNonCompletedTask returns the (in story, then task order) first
// task of prd whose Status is not TaskCompleted — cancelled and failed
// included, exactly like any other non-completed state. All three
// returns are empty when every task is completed.
func firstNonCompletedTask(prd *PRD) (storyID, taskID string, status TaskStatus) {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.Status != TaskCompleted {
				return prd.Story[si].ID, t.ID, t.Status
			}
		}
	}
	return "", "", ""
}

// reusedVerdictNote is the audit note for a rejudge that returned the
// stored verdict instead of running a new review: it names the head commit
// and the time of the verdict being reused, and says no review ran.
func reusedVerdictNote(sha string, at time.Time) string {
	return fmt.Sprintf("stored verdict reused: the head commit %s and the rules text are unchanged since that verdict was made at %s; no new review ran", sha, at.Format(time.RFC3339))
}

// verdictAlreadyRecorded reports whether prd.Verdicts already holds a
// verdict identical to v, meaning the same VerdictAt, JudgedCommitSHA and
// JudgeSessionID. A reused verdict is appended only when this is false, so
// the list keeps one entry per review run.
func verdictAlreadyRecorded(prd *PRD, v GuardrailVerdict) bool {
	for i := range prd.Verdicts {
		r := &prd.Verdicts[i]
		if r.VerdictAt.Equal(v.VerdictAt) && r.JudgedCommitSHA == v.JudgedCommitSHA && r.JudgeSessionID == v.JudgeSessionID {
			return true
		}
	}
	return false
}

// recordedVerdictMatching returns the verdict already on prd that a reused
// cache copy stands for: a rules-review verdict judged on sha under the
// same rules fingerprint with the same outcome and judge session. The
// cache copy can lack the timestamp, commit and fingerprint that the
// recorded one carries, so the recorded one is preferred to keep those
// fields as they were. ok is false when the PRD holds no such verdict.
func recordedVerdictMatching(prd *PRD, v GuardrailVerdict, sha, fingerprint string) (GuardrailVerdict, bool) {
	for i := len(prd.Verdicts) - 1; i >= 0; i-- {
		r := prd.Verdicts[i]
		if r.Guardrail == "rules-review" && r.Outcome == v.Outcome && r.JudgeSessionID == v.JudgeSessionID &&
			sha != "" && r.JudgedCommitSHA == sha && r.RulesFingerprint == fingerprint {
			return r, true
		}
	}
	return GuardrailVerdict{}, false
}

// RejudgeCompletionGuardrail re-runs the PRD-completion "rules-review"
// judgment for a PRD whose work is fully done, on an
// operator's behalf (actor is recorded as the "guardrail_rejudge"
// Decision's Actor, not hardcoded to "autonomous"). It refuses — with a
// clear error — states outside "PRDBlocked, a PRDNeedsReview hold by the
// rules review, or a completed/archived PRD whose work is not yet merged
// into the base branch (merge state finished_not_merged) and whose
// completion review is missing or blocked", with no side effects on any
// task (never a ResetTask, never a status change); a record whose
// completion review already passed, and one with any task not
// completed-or-cancelled, are both refused, the latter naming the first
// offending task's id and status.
//
// Outcome handling, in order:
//  1. The verdict is recorded durably FIRST, via
//     recordGuardrailVerdict (guardrail_verdict Decision + save +
//     judge-output cleanup), so it is in the append-only log before
//     anything else changes — exactly the order
//     runPRDCompletionRulesReview uses.
//  2. A "guardrail_rejudge" Decision (Actor = actor) names the outcome,
//     carrying the same verdict detail (outcome, category, summary,
//     judge session or backend) the guardrail_verdict Decision does.
//  3. Pass (Outcome != "block") — the verdict is appended to
//     prd.Verdicts and the block records are cleared (BlockedReason,
//     RulesReviewOutcome) in ONE store commit;
//     Status is then lifted to PRDCompleted (a record that was already
//     completed or archived keeps that status) — through
//     CommitPRDIfRev with the same re-checks commitRollupStatus uses
//     (the pass did not rewrite the status, every task still terminal,
//     under the store lock), then emitPRDStatus fires
//     with the "guardrail_rejudge" Decision attached.
//  4. Block — the verdict flips the PRD to PRDBlocked with BlockedReason
//     set from blockedReasonFor(v) (a completed or archived record is
//     flipped back to blocked — the work is still not judged clean, and
//     a later pass lifts it again), and the event
//     carries the last block verdict.
//  5. No verdict (the judge errored, or returned an empty Outcome) — a
//     "guardrail_rejudge" Decision notes that no verdict was produced,
//     the PRD's current status and BlockedReason are untouched, and an
//     error is returned so the operator sees the
//     re-judge failed.
//
// backend selects the judge backend for the re-judgment (a reroute, see
// judgeCompletionRulesReview): empty re-runs the resolved tier.
func (m *Manager) RejudgeCompletionGuardrail(ctx context.Context, prdID, actor, backend string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %q not found", prdID)
	}
	origStatus := prd.Status
	heldReview := false
	// A finished record (completed or archived) is re-judged only when
	// its work has not landed and its review never passed on the current
	// head; the merge state is derived on read (never persisted) and the
	// verdict gate asks the append-only verdict log of the Automaton.
	finished := origStatus == PRDCompleted || origStatus == PRDArchived
	finishedNotMerged := false
	if finished {
		finishedNotMerged = m.AutomatonMergeState(prd) == MergeStateFinishedNotMerged
	}
	switch {
	case prd.Status == PRDBlocked:
	case prd.Status == PRDNeedsReview && rulesReviewHoldIsLast(prd):
		heldReview = true
	case prd.Status == PRDCompleted || prd.Status == PRDArchived:
		if !finishedNotMerged {
			// Work already landed on the base branch, or the merge state is
			// undetermined: nothing unmerged for the review to clear.
			return nil, fmt.Errorf("rejudge completion guardrail: prd %s is %s but its work is already on the base branch or its merge state is undetermined; rejudgment of a finished record applies only when its merge state is finished_not_merged", prd.ID, prd.Status)
		}
		// finished_not_merged: the review may run again only when it is still
		// pending (no verdict yet), its newest verdict is a block, or its last
		// pass or warn was made on an OLDER head than the record's current
		// head — the head moved past that verdict, so it no longer describes
		// the record's tree and the review should run once on the new head.
		// A pass or warn made at the current head settles the review: there is
		// no block left to clear, so re-judging has nothing to un-block.
		finHead := m.currentReviewHead(ctx, prd)
		if !rejudgeWarrantForFinished(prd, finHead) {
			last := lastRulesReviewVerdict(prd)
			return nil, fmt.Errorf("rejudge completion guardrail: prd %s is %s and its completion review already passed (last verdict %s, made %s); there is no block left to clear", prd.ID, prd.Status, last.Outcome, last.VerdictAt.Format(time.RFC3339))
		}
	default:
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s is %s, not eligible; rejudgment applies to a blocked PRD, a PRD held for review by the rules review, or a completed or archived PRD whose work is not yet merged into the base branch and whose completion review is missing or blocked", prd.ID, prd.Status)
	}
	if storyID, taskID, status := firstBlockingTask(prd); taskID != "" {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s task %s (story %s) is %s, not completed; every task must be completed or cancelled, with at least one completed, before the completion guardrail can be re-run", prd.ID, taskID, storyID, status)
	}
	if m.recordCancelledTasksSkipped(prd) {
		_ = m.savePRDOwn(prd)
	}

	// No-verdict path (the judge errored, or returned an empty outcome):
	// record a "guardrail_rejudge" Decision stating no verdict was
	// produced, keep the PRD's current status and BlockedReason
	// untouched, and return an error so the operator sees the re-judge
	// did not yield a judgment.
	var headSHA, fingerprint string
	// resolveHead fills headSHA and fingerprint from the stamped review
	// invocation when they are not known yet, so every outcome names the
	// head the review judged.
	resolveHead := func() {
		if headSHA != "" {
			return
		}
		inv, _ := m.buildPRDRulesReviewInvocation(prd, "") // head lookup only: no rules read, no fallback Decision
		m.stampReviewIdentity(ctx, prd, &inv)
		headSHA = judgeVerdictCommitSHA(prd, inv)
		fingerprint = inv.RulesFingerprint
	}
	recordNoVerdict := func(detail string) error {
		detail = "no verdict was produced: " + detail
		resolveHead()
		log.Printf("[autonomous] rejudge %s: commit %s: no verdict", prd.ID, headSHA)
		m.store.WithLock(func() {
			prd.Decisions = append(prd.Decisions, Decision{
				At:              time.Now(),
				Kind:            "guardrail_rejudge",
				Actor:           actor,
				Note:            fmt.Sprintf("rejudge of the rules-review completion guardrail for prd %s: %s", prd.ID, detail),
				JudgedCommitSHA: headSHA,
				VerdictReused:   false,
			})
		})
		if serr := m.store.SavePRD(prd); serr != nil {
			return fmt.Errorf("rejudge completion guardrail: prd %s: record no-verdict decision: %w", prd.ID, serr)
		}
		return fmt.Errorf("rejudge completion guardrail: prd %s: %s", prd.ID, detail)
	}

	// A rules-review hold is re-judged only on new commits: the held
	// verdict describes the old head, and an unchanged head would give the
	// same answer. Refuse before touching anything.
	if heldReview {
		inv, _ := m.buildPRDRulesReviewInvocation(prd, "") // head lookup only: no rules read, no fallback Decision
		m.stampReviewIdentity(ctx, prd, &inv)
		head := judgeVerdictCommitSHA(prd, inv)
		heldSHA := ""
		if stored := lastBlockVerdict(prd.Verdicts); stored != nil {
			heldSHA = stored.JudgedCommitSHA
		}
		if head == "" || head == heldSHA {
			return nil, fmt.Errorf("rejudge completion guardrail: prd %s is held for review and the branch head %q is unchanged since the held verdict was made; new commits are needed before a rejudge", prd.ID, head)
		}
		ctx = withJudgeVerdictCacheLookupBypassed(ctx)
	}

	// Commit-anchored replay gate (judge verdict cache enabled only; with
	// the cache off the path below is exactly the pre-cache one). The
	// stored blocking verdict is replayed only when it was judged on the
	// PRD's current head; otherwise the cache lookup is bypassed so the
	// judge runs fresh on the new head and its verdict is stored under the
	// new key. A stored verdict with no recorded commit cannot be shown to
	// match the head, so it is treated as stale too.
	cacheOn := m.judgeVerdictCacheEnabled()
	if cacheOn {
		resolveHead()
		if stored := lastBlockVerdict(prd.Verdicts); stored != nil &&
			((headSHA != "" && stored.JudgedCommitSHA != headSHA) ||
				stored.RulesFingerprint == "" || stored.RulesFingerprint != fingerprint) {
			// Judged on another head, or under other rules text, or recorded
			// before the rules fingerprint existed: judge again.
			ctx = withJudgeVerdictCacheLookupBypassed(ctx)
		}
	}

	v, reused, err := m.judgeCompletionRulesReview(ctx, prd, backend)
	if err != nil {
		return nil, recordNoVerdict(fmt.Sprintf("the guardrail judgment failed: %v", err))
	}
	if v.Outcome == "" {
		return nil, recordNoVerdict("the guardrail judgment returned an empty outcome")
	}
	resolveHead()

	var rej Decision
	if reused {
		// A stored verdict came back: keep it exactly as it was made, with
		// its own time, commit and rules fingerprint. The copy already on
		// the PRD wins over a cache copy; fields the stored verdict never
		// had are filled only so it can be told apart from a new review.
		if rec, found := recordedVerdictMatching(prd, v, headSHA, fingerprint); found {
			v = rec
		}
		v.Guardrail = "rules-review"
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		if v.JudgedCommitSHA == "" {
			v.JudgedCommitSHA = headSHA
		}
		if v.RulesFingerprint == "" {
			v.RulesFingerprint = fingerprint
		}
		// Nothing new was judged, so no guardrail_verdict Decision is
		// recorded and prd.Verdicts is not appended to here.
		rej = Decision{
			At:              time.Now(),
			Kind:            "guardrail_rejudge",
			Actor:           actor,
			VerdictOutcome:  v.Outcome,
			FailureCategory: v.Category,
			Note:            reusedVerdictNote(v.JudgedCommitSHA, v.VerdictAt),
			JudgeSessionID:  v.JudgeSessionID,
			JudgedCommitSHA: v.JudgedCommitSHA,
			VerdictReused:   true,
		}
	} else {
		if v.VerdictAt.IsZero() {
			v.VerdictAt = time.Now()
		}
		v.Guardrail = "rules-review"
		v.JudgedCommitSHA = headSHA
		v.RulesFingerprint = fingerprint

		// Record the verdict durably BEFORE anything else changes — the same
		// guardrail_verdict Decision + save + judge-output cleanup
		// runPRDCompletionRulesReview uses.
		if err := m.recordGuardrailVerdict(prd, prd.ID, v); err != nil {
			return nil, fmt.Errorf("rejudge completion guardrail: prd %s: record verdict: %w", prd.ID, err)
		}

		// The "guardrail_rejudge" Decision naming the re-judge outcome,
		// attributed to the acting operator.
		rej = Decision{
			At:              time.Now(),
			Kind:            "guardrail_rejudge",
			Actor:           actor,
			VerdictOutcome:  v.Outcome,
			FailureCategory: v.Category,
			Note:            fmt.Sprintf("new review ran on commit %s: rejudge of the rules-review completion guardrail: %s", v.JudgedCommitSHA, v.Outcome),
			JudgeSessionID:  v.JudgeSessionID,
			JudgedCommitSHA: v.JudgedCommitSHA,
			VerdictReused:   false,
		}
		if v.Summary != "" {
			rej.Note += " - " + v.Summary
		}
	}
	if reused {
		log.Printf("[autonomous] rejudge %s: commit %s: reused", prd.ID, rej.JudgedCommitSHA)
	} else {
		log.Printf("[autonomous] rejudge %s: commit %s: new review", prd.ID, rej.JudgedCommitSHA)
	}
	if v.JudgeSessionID == "" {
		rej.Backend = v.Backend
	}
	if backend = strings.TrimSpace(backend); backend != "" {
		// Name the reroute in the audit trail: which backend the operator
		// asked the re-judgment to run on.
		if !reused {
			rej.Note += " (rerouted to " + backend + ")"
		}
		if rej.Backend == "" {
			rej.Backend = backend
		}
	}

	if v.Outcome != "block" {
		// Pass — verdict + "guardrail_rejudge" Decision + cleared block
		// records in ONE conditional commit, the same
		// CommitPRDIfRev pattern commitRollupStatus uses: only if nobody
		// rewrote the PRD since this pass observed it and every task is
		// still terminal (re-checked under the store lock).
		//
		// A blocked or held record is lifted to PRDCompleted; a record
		// that is already completed or archived keeps that status — the
		// pass clears the stale block it recorded (BlockedReason and the
		// structured RulesReviewOutcome), which is what the merge step
		// keys off.
		passStatus := PRDCompleted
		if origStatus == PRDArchived {
			passStatus = PRDArchived
		}
		rev, ok := m.store.PRDRev(prd.ID)
		if !ok {
			return nil, fmt.Errorf("rejudge completion guardrail: prd %q not found", prdID)
		}
		if cerr := m.store.CommitPRDIfRev(prd.ID, rev, func(p *PRD) bool {
			if p.Status != origStatus || !prdAllTasksTerminal(p) {
				return false
			}
			if !reused || !verdictAlreadyRecorded(p, v) {
				p.Verdicts = append(p.Verdicts, v)
			}
			p.Decisions = append(p.Decisions, rej)
			p.Status = passStatus
			p.BlockedReason = ""
			p.RulesReviewOutcome = nil
			return true
		}); cerr != nil {
			return nil, fmt.Errorf("rejudge completion guardrail: prd %s: commit re-judgment pass: %w", prd.ID, cerr)
		}
		old := origStatus
		m.store.WithLock(func() {
			if !reused || !verdictAlreadyRecorded(prd, v) {
				prd.Verdicts = append(prd.Verdicts, v)
			}
			prd.Decisions = append(prd.Decisions, rej)
			prd.Status = passStatus
			prd.BlockedReason = ""
			prd.RulesReviewOutcome = nil
		})
		m.emitPRDStatus(prd, old, lastDecision(prd, "guardrail_rejudge"), nil)
		m.recordProgressMilestone(prd.ID)
		return prd, nil
	}

	// Block — the same PRDBlocked signal the completion rollup uses: the
	// verdict goes on prd.Verdicts and the reason on BlockedReason in the
	// one save (SavePRD's clearStaleBlockedReason leaves a fresh reason
	// in place only while the status is blocked, and drops a stale one —
	// the exact hygiene for the finished-record case, whose stale reason
	// would otherwise survive the save). A block flips a completed or
	// archived record back to blocked: the work is still not judged
	// clean, and a pass on a later re-judgment is what lifts it again.
	var old PRDStatus
	m.store.WithLock(func() {
		old = prd.Status
		if !reused || !verdictAlreadyRecorded(prd, v) {
			prd.Verdicts = append(prd.Verdicts, v)
		}
		prd.Decisions = append(prd.Decisions, rej)
		prd.Status = PRDBlocked
		prd.BlockedReason = blockedReasonFor(v)
	})
	if serr := m.store.SavePRD(prd); serr != nil {
		return nil, fmt.Errorf("rejudge completion guardrail: prd %s: record block outcome: %w", prd.ID, serr)
	}
	if bl := lastBlockVerdict(prd.Verdicts); bl != nil {
		m.emitPRDStatus(prd, old, lastDecision(prd, "guardrail_rejudge"), bl)
	}
	m.EmitPRDUpdate(prd.ID)
	return prd, nil
}

// rulesReviewHoldIsLast reports whether the most recent guardrail-related
// decision on prd is a rules-review retries-spent or auto-fix-disabled hold.
func rulesReviewHoldIsLast(prd *PRD) bool {
	for i := len(prd.Decisions) - 1; i >= 0; i-- {
		switch prd.Decisions[i].Kind {
		case "rules_review_retries_spent", "rules_review_auto_fix_disabled":
			return true
		case "guardrail_verdict", "guardrail_rejudge", "rules_review_fix_failed":
			return false
		}
	}
	return false
}

// lastRulesReviewVerdict is the newest Automaton-level verdict of the
// "rules-review" guardrail (PRD.Verdicts holds the Automaton-level
// verdicts; task-level verdicts live on the task record), or nil when the
// Automaton's completion review never ran to a verdict.
func lastRulesReviewVerdict(prd *PRD) *GuardrailVerdict {
	for i := len(prd.Verdicts) - 1; i >= 0; i-- {
		if prd.Verdicts[i].Guardrail == "rules-review" {
			return &prd.Verdicts[i]
		}
	}
	return nil
}

// rulesReviewVerdictPendingOrBlocked reports whether, as far as the
// Automaton-level verdict log shows, the completion review is pending
// (no rules-review verdict recorded on the Automaton yet) or blocked
// (the newest rules-review verdict is a block). A newest verdict that
// passed (pass or warn) means the review is settled and a re-judgment
// has nothing left to clear.
func rulesReviewVerdictPendingOrBlocked(prd *PRD) bool {
	v := lastRulesReviewVerdict(prd)
	return v == nil || v.Outcome == "block"
}

// rejudgeWarrantForFinished reports whether a finished_not_merged record's
// (completed or archived) completion review is still worth re-running for the
// record's current head (head, from currentReviewHead):
//
//   - the review is pending (no rules-review verdict recorded yet);
//   - the newest rules-review verdict is a block; or
//   - the newest verdict passed or warned but was made on an OLDER head than
//     the record's current head — the head moved past that verdict, so it no
//     longer describes the record's tree and the review should run once on
//     the new head (the stale-pass case an auto-fix produces: the fix moves
//     the head past a verdict, and the review must still run for the new
//     tree).
//
// A pass or warn made at the CURRENT head settles the review — there is no
// block left to clear and re-judging has nothing to do — and a pass or warn
// that cannot be proven tied to an older head (no head, no recorded commit)
// is treated as settled too: the conservative call, so the same verdict is
// never re-judged on the head it already answered.
func rejudgeWarrantForFinished(prd *PRD, head string) bool {
	v := lastRulesReviewVerdict(prd)
	if v == nil {
		return true
	}
	if v.Outcome == "block" {
		return true
	}
	if head == "" || v.JudgedCommitSHA == "" {
		return false
	}
	return v.JudgedCommitSHA != head
}
