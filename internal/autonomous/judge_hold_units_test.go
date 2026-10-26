package autonomous

import "testing"

func TestJudgeHoldOrBlockedHoldsOnlyAJudgeTimeout(t *testing.T) {
	hold := judgeTimeoutVerdict()
	if got := judgeHoldOrBlocked(&hold); got != PRDNeedsReview {
		t.Fatalf("judge timeout -> %s, want needs_review", got)
	}
	content := GuardrailVerdict{Outcome: "block", Category: "content", Summary: "rule broken"}
	if got := judgeHoldOrBlocked(&content); got != PRDBlocked {
		t.Fatalf("content block -> %s, want blocked", got)
	}
	if got := judgeHoldOrBlocked(nil); got != PRDBlocked {
		t.Fatalf("no block verdict -> %s, want blocked as before", got)
	}
}

func TestJudgeHoldHeldFollowsAStoryOrTaskJudgeTimeout(t *testing.T) {
	taskHold := &PRD{
		Status: PRDNeedsReview,
		Story:  []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Verdicts: []GuardrailVerdict{judgeTimeoutVerdict()}}}}},
	}
	if !judgeHoldHeld(taskHold) {
		t.Fatal("a needs-review PRD held by a task's judge timeout must stay held for lane fill")
	}
	storyHold := &PRD{
		Status: PRDNeedsReview,
		Story:  []Story{{ID: "s1", Verdicts: []GuardrailVerdict{judgeTimeoutVerdict()}}},
	}
	if !judgeHoldHeld(storyHold) {
		t.Fatal("a needs-review PRD held by a story's judge timeout must stay held for lane fill")
	}
	plain := &PRD{
		Status: PRDNeedsReview,
		Story:  []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Verdicts: []GuardrailVerdict{{Outcome: "block", Category: "content"}}}}}},
	}
	if judgeHoldHeld(plain) {
		t.Fatal("a content block on a task is not a judge hold")
	}
	approved := &PRD{
		Status: PRDApproved,
		Story:  []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Verdicts: []GuardrailVerdict{judgeTimeoutVerdict()}}}}},
	}
	if judgeHoldHeld(approved) {
		t.Fatal("an approved PRD is not held by a judge timeout on one of its tasks")
	}
}
