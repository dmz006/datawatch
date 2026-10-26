package autonomous

// JudgeTimeoutCategory is the verdict category of a judge that hit its time
// limit on every backend in its judge policy, one attempt each. It is neither
// "infra" (the judge is not re-run as a whole) nor "content" (no content
// auto-fix is attempted). A PRD whose rules-review judge ends this way is
// moved to needs_review, not blocked, so nothing is approved, passed or
// blocked silently.
const JudgeTimeoutCategory = "judge-timeout"

// IsJudgeTimeoutHold reports whether v is a judge-timeout hold verdict.
func IsJudgeTimeoutHold(v GuardrailVerdict) bool {
	return v.Outcome == "block" && v.Category == JudgeTimeoutCategory
}

// rulesReviewBlockStatus is the status a PRD-level rules-review block moves
// the PRD to, with its rollup reason: needs_review for a judge hold, blocked
// for a judgment that found a problem.
func rulesReviewBlockStatus(prd *PRD) (PRDStatus, string) {
	if v := lastBlockVerdict(prd.Verdicts); v != nil && IsJudgeTimeoutHold(*v) {
		return PRDNeedsReview, "judge_timeout_hold"
	}
	return PRDBlocked, "rules_review_block"
}

// judgeHoldOrBlocked is the status a story or task block moves the PRD to:
// needs_review when the block is a judge hold (the judge timed out on every
// backend), blocked otherwise. v is the unit's last block verdict, or nil.
func judgeHoldOrBlocked(v *GuardrailVerdict) PRDStatus {
	if v != nil && IsJudgeTimeoutHold(*v) {
		return PRDNeedsReview
	}
	return PRDBlocked
}

// judgeHoldHeld reports whether prd is waiting in needs_review because its
// judge (plan review or guardrail) did not finish on any backend, at the PRD,
// story or task level. Lane fill must not approve and run such a PRD by
// itself: only the operator's decision moves it on.
func judgeHoldHeld(prd *PRD) bool {
	if NormalizePRDStatus(prd.Status) != PRDNeedsReview {
		return false
	}
	if v := prd.PlanReview; v != nil && v.Held {
		return true
	}
	if v := lastBlockVerdict(prd.Verdicts); v != nil && IsJudgeTimeoutHold(*v) {
		return true
	}
	for i := range prd.Story {
		s := &prd.Story[i]
		if v := lastBlockVerdict(s.Verdicts); v != nil && IsJudgeTimeoutHold(*v) {
			return true
		}
		for j := range s.Tasks {
			if v := lastBlockVerdict(s.Tasks[j].Verdicts); v != nil && IsJudgeTimeoutHold(*v) {
				return true
			}
		}
	}
	return false
}
