package autonomous

import (
	"context"
	"log"
	"strings"
)

// Auto re-review of the Automaton's completion when an auto-fix's work has
// landed.
//
// The completion rules review blocks a finished Automaton on the head it
// judged. An auto-fix task then merges a fix onto the Automaton branch,
// moving that head past the verdict the block was made on. The review must
// run again for the new head — and, if it passes, clear the stale block —
// without an operator touching anything. AutoRejudgeAfterAutoFixMerge is
// exactly that one-time, automatic re-judgment; it is the automatic
// counterpart of RejudgeCompletionGuardrail (rejudge.go), which is the
// operator's manual form of the same action.
//
// Run-once per head. The only way to re-judge a head that never passed is a
// new head; a verdict made on the current head already answers it, so a
// second call for the same head is a no-op. The hook therefore reads the
// record's current head (stampReviewIdentity → currentReviewHead, the same
// head the judge is anchored to) and the newest rules-review verdict, and
// fires the re-judgment ONLY when the record is in a state where the review
// is still open for re-running:
//
//   - the review is pending (no rules-review verdict recorded yet), or
//   - the newest verdict is a block (a stale block the auto-fix should
//     clear), or
//   - the newest verdict passed or warned but was made on an OLDER head
//     than the record's current head — the head moved past that verdict, so
//     it no longer describes the tree and the review runs once on the new
//     head (the "stale pass" case; see rejudgeWarrantForFinished in
//     rejudge.go, the gate RejudgeCompletionGuardrail applies).
//
// If the newest verdict was made on the CURRENT head (passed, warned, or
// blocked) the review has already answered this head and the hook does
// nothing — that is what keeps it to a single judgment per head change.
//
// The hook is deliberately side-effect free except for the re-judgment
// itself: it acquires no merge lock (MergeTaskBranch's per-PRD lock is
// released before MergeCompletedTasks calls it, and
// RejudgeCompletionGuardrail takes no merge lock), records no Decisions of
// its own, and never rewrites a task. All of the verdict recording, block
// clearing, status lift, and event emission is RejudgeCompletionGuardrail's
// job, so the automatic path and the manual path keep one shared definition
// of what a re-judgment does.
//
// It returns true when a (re-)judgment ran and recorded a verdict, false
// when it was gated out or the judge produced no verdict; it never returns an
// error to the caller, so merging can never fail because of the review step.
func (m *Manager) AutoRejudgeAfterAutoFixMerge(ctx context.Context, prd *PRD, t *Task) bool {
	if m == nil || prd == nil || prd.ID == "" {
		return false
	}
	if t == nil || !isAutoFixTask(t) {
		return false
	}
	// Only a record whose completion review is still open for re-running is
	// eligible; these are exactly the states RejudgeCompletionGuardrail
	// accepts (a blocked record, or a finished record that has not landed).
	switch prd.Status {
	case PRDBlocked, PRDNeedsReview, PRDCompleted, PRDArchived:
	default:
		return false
	}
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		return false
	}
	// The head the review will judge: the review identity stamps it, so this
	// is the same commit the judge and the verdict key read. Computing it
	// here lets the hook tell "already answered this head" apart from "the
	// head moved past the verdict".
	var inv GuardrailInvocation
	m.stampReviewIdentity(ctx, fresh, &inv)
	head := judgeVerdictCommitSHA(fresh, inv)
	v := lastRulesReviewVerdict(fresh)
	switch {
	case v == nil:
		// Pending: the review never ran (or ran to no verdict), so it is
		// free to run now for the head the auto-fix just landed on.
	case v.Outcome == "block":
		// Stale block: the auto-fix is exactly what moved the head past the
		// finding; the review re-runs and, if it passes, clears it.
	case v.JudgedCommitSHA != "" && head != "" && v.JudgedCommitSHA == head:
		// The newest verdict already answered THIS head — passed, warned or
		// blocked. Nothing to clear for a head the review has judged; run once
		// only.
		return false
	case head == "" || v.JudgedCommitSHA == "":
		// A pass/warn verdict we cannot prove was made on an older head: be
		// conservative and do not re-judge.
		return false
	default:
		// Stale pass/warn: the head moved past the verdict, so re-run once.
	}
	if _, err := m.RejudgeCompletionGuardrail(ctx, fresh.ID, "auto-fix-merge", ""); err != nil {
		log.Printf("[autonomous] %s: auto re-review after auto-fix merge of task %s recorded no verdict: %v", fresh.ID, t.ID, err)
		return false
	}
	log.Printf("[autonomous] %s: completion review re-ran once after auto-fix task %s merged (head %s)", fresh.ID, t.ID, head)
	return true
}

// isAutoFixTask reports whether t is the auto-fix kind of work — a corrected
// task the content auto-fix synthesized, or any task launched with a fix
// context (see TaskLaunchKind, placement_tags.go). This is the same
// vocabulary the placement side tags auto-fix sessions with.
func isAutoFixTask(t *Task) bool {
	return t != nil && strings.HasPrefix(t.Title, autoFixTaskTitlePrefix)
}
