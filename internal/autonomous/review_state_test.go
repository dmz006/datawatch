package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var (
	reviewT0 = time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	reviewT1 = reviewT0.Add(time.Hour)
	reviewT2 = reviewT1.Add(time.Hour)
)

func reviewHold(at time.Time) *GuardrailVerdict {
	return &GuardrailVerdict{
		Outcome:   "block",
		VerdictAt: at,
		Guardrail: "rules",
		Category:  JudgeTimeoutCategory,
		Summary:   "judge timed out on every backend tried (claude-code, qwen), 1h0m0s in total; the Automaton needs review",
		Issues: []string{
			"backend claude-code, started " + reviewT0.Format(time.RFC3339) + ": timed out after 30m0s (limit 30m0s)",
			"backend qwen, started " + reviewT1.Format(time.RFC3339) + ": timed out after 30m0s (limit 30m0s)",
		},
		JudgeAttempts: []JudgeAttemptRecord{
			{Backend: "claude-code", Started: reviewT0, Duration: 30 * time.Minute, Outcome: JudgeOutcomeTimedOut},
			{Backend: "qwen", Started: reviewT1, Duration: 30 * time.Minute, Outcome: JudgeOutcomeTimedOut},
		},
	}
}

func reviewBlock(at time.Time) *GuardrailVerdict {
	return &GuardrailVerdict{
		Outcome:         "block",
		VerdictAt:       at,
		Guardrail:       "rules",
		Category:        "content",
		Backend:         "claude-code",
		Summary:         "missing version bump",
		JudgeOutputPath: "/tmp/judge-out.txt",
	}
}

func reviewPass(at time.Time) *GuardrailVerdict {
	return &GuardrailVerdict{
		Outcome:   "pass",
		VerdictAt: at,
		Guardrail: "rules",
	}
}

func TestReviewStateOfTimeoutHold(t *testing.T) {
	prd := &PRD{ID: "r1", Verdicts: []GuardrailVerdict{*reviewHold(reviewT2)}}
	got := ReviewStateOf(prd)
	want := ReviewResult{
		State:    ReviewStateTimedOut,
		Cause:    "judge timed out on 2 backend(s): claude-code, qwen, limit 30m0s",
		Action:   ReviewActionRetry,
		Attempts: 2,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReviewStateOfTimeoutHoldFromVerdictText(t *testing.T) {
	// A hold persisted before it carried the structured attempt records:
	// the backends and the attempt count come out of the verdict text.
	v := reviewHold(reviewT2)
	v.JudgeAttempts = nil
	got := ReviewStateOf(&PRD{ID: "r2", Verdicts: []GuardrailVerdict{*v}})
	if got.State != ReviewStateTimedOut || got.Attempts != 2 {
		t.Fatalf("got %+v, want a timed_out state with 2 attempts from the text", got)
	}
	for _, want := range []string{"claude-code, qwen", "limit 30m0s"} {
		if !strings.Contains(got.Cause, want) {
			t.Errorf("cause %q lacks %q", got.Cause, want)
		}
	}
	if got.Action != ReviewActionRetry {
		t.Errorf("action = %q, want %q", got.Action, ReviewActionRetry)
	}
}

func TestReviewStateOfTimeoutHoldNamesOnlyTimedOutBackends(t *testing.T) {
	// A backend that only lacked a capacity slot did not time out; it is
	// not named in the cause.
	v := reviewHold(reviewT2)
	v.JudgeAttempts = []JudgeAttemptRecord{
		{Backend: "claude-code", Started: reviewT0, Duration: 30 * time.Minute, Outcome: JudgeOutcomeTimedOut},
		{Backend: "qwen", Started: reviewT1, Duration: time.Minute, Outcome: JudgeOutcomeNoSlot},
	}
	got := ReviewStateOf(&PRD{ID: "r3", Verdicts: []GuardrailVerdict{*v}})
	if got.State != ReviewStateTimedOut {
		t.Fatalf("got state %q, want timed_out", got.State)
	}
	if !strings.Contains(got.Cause, "claude-code") || strings.Contains(got.Cause, "qwen") {
		t.Errorf("cause %q should name only the backend that timed out", got.Cause)
	}
}

func TestReviewStateOfTimeoutHoldRetargetsToConfiguredBackend(t *testing.T) {
	// A stronger guardrail backend is configured for this Automaton and
	// none of the timed-out attempts tried it: reroute.
	prd := &PRD{ID: "r4", GuardrailBackend: "opus", Verdicts: []GuardrailVerdict{*reviewHold(reviewT2)}}
	got := ReviewStateOf(prd)
	if got.State != ReviewStateTimedOut || got.Action != ReviewActionReroute {
		t.Fatalf("got %+v, want a timed_out state with action reroute", got)
	}
	// …but when the configured backend was already among the timed-out
	// attempts there is nothing to reroute to: same-backend retry.
	prd.GuardrailBackend = "qwen"
	if got := ReviewStateOf(prd); got.Action != ReviewActionRetry {
		t.Fatalf("got action %q, want retry on the same backend list", got.Action)
	}
	// No configured backend: retry.
	if got := ReviewStateOf(&PRD{ID: "r5", Verdicts: []GuardrailVerdict{*reviewHold(reviewT2)}}); got.Action != ReviewActionRetry {
		t.Fatalf("got action %q, want retry", got.Action)
	}
}

func TestReviewStateOfRealBlockStaysBlocked(t *testing.T) {
	prd := &PRD{ID: "r6", Verdicts: []GuardrailVerdict{*reviewBlock(reviewT1)}}
	got := ReviewStateOf(prd)
	want := ReviewResult{
		State:    ReviewStateBlocked,
		Cause:    "guardrail rules returned block: missing version bump",
		Action:   ReviewActionFix,
		Attempts: 0,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReviewStateOfMixedHistory(t *testing.T) {
	table := []struct {
		name string
		vs   []GuardrailVerdict
		want ReviewResult
	}{
		{
			name: "real block then a newer hold keeps the hold",
			vs:   []GuardrailVerdict{*reviewBlock(reviewT0), *reviewHold(reviewT1)},
			want: ReviewResult{
				State:    ReviewStateTimedOut,
				Cause:    "judge timed out on 2 backend(s): claude-code, qwen, limit 30m0s",
				Action:   ReviewActionRetry,
				Attempts: 2,
			},
		},
		{
			name: "hold then a newer real block is a real block",
			vs:   []GuardrailVerdict{*reviewHold(reviewT0), *reviewBlock(reviewT1)},
			want: ReviewResult{
				State:    ReviewStateBlocked,
				Cause:    "guardrail rules returned block: missing version bump",
				Action:   ReviewActionFix,
				Attempts: 0,
			},
		},
		{
			name: "pass after an older block supersedes it",
			vs:   []GuardrailVerdict{*reviewBlock(reviewT0), *reviewPass(reviewT1)},
			want: ReviewResult{State: ReviewStatePassed, Attempts: 0},
		},
		{
			name: "pass after an older hold supersedes the hold",
			vs:   []GuardrailVerdict{*reviewHold(reviewT0), *reviewPass(reviewT1)},
			want: ReviewResult{State: ReviewStatePassed, Attempts: 0},
		},
		{
			name: "only a warning is pending with the warning as cause",
			vs: []GuardrailVerdict{{
				Outcome:   "warn",
				VerdictAt: reviewT0,
				Guardrail: "rules",
				Summary:   "missing version bump",
			}},
			want: ReviewResult{
				State:    ReviewStatePending,
				Cause:    "guardrail rules returned warn: missing version bump",
				Attempts: 0,
			},
		},
	}
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			got := ReviewStateOf(&PRD{ID: "mixed", Verdicts: tc.vs})
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReviewStateOfStoryAndTaskVerdicts(t *testing.T) {
	// The deciding verdict may sit on a story or a task, not only on the
	// Automaton's own level.
	got := ReviewStateOf(&PRD{
		ID:    "r7",
		Story: []Story{{Tasks: []Task{{ID: "t1", Verdicts: []GuardrailVerdict{*reviewBlock(reviewT0)}}}}},
	})
	if got.State != ReviewStateBlocked || got.Action != ReviewActionFix {
		t.Fatalf("got %+v, want a blocked review with action fix", got)
	}
	// A newer pass on the PRD's own level supersedes the older task hold.
	p := &PRD{
		ID:       "r8",
		Verdicts: []GuardrailVerdict{*reviewPass(reviewT1)},
		Story:    []Story{{Tasks: []Task{{ID: "t1", Verdicts: []GuardrailVerdict{*reviewHold(reviewT0)}}}}},
	}
	if got := ReviewStateOf(p); got.State != ReviewStatePassed {
		t.Fatalf("got %+v, want the newer pass to supersede the older hold", got)
	}
}

func TestReviewStateOfNoVerdictsAndNilPRD(t *testing.T) {
	for name, prd := range map[string]*PRD{
		"no verdicts": {ID: "r9"},
		"one level":   {ID: "r10", Story: []Story{{ID: "s1"}}},
	} {
		if got := ReviewStateOf(prd); got != (ReviewResult{State: ReviewStatePending}) {
			t.Errorf("%s: got %+v, want a cause-free pending state", name, got)
		}
	}
	if got := ReviewStateOf(nil); got != (ReviewResult{State: ReviewStatePending}) {
		t.Fatalf("nil PRD: got %+v, want a cause-free pending state", got)
	}
}

func TestPRDViewCarriesReviewState(t *testing.T) {
	// The capped copy the default detail GET returns carries review_state:
	// a state, cause and action derived from the verdicts.
	capped := CapPRDText(&PRD{
		ID:       "cap1",
		Spec:     "spec",
		Verdicts: []GuardrailVerdict{*reviewHold(reviewT0)},
	}, 4096)
	if capped == nil || capped.ReviewState == nil {
		t.Fatalf("capped copy: ReviewState = %v, want the review view", capped)
	}
	var m map[string]any
	buf, err := json.Marshal(capped)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatal(err)
	}
	rs, ok := m["review_state"].(map[string]any)
	if !ok {
		t.Fatalf("capped copy JSON has no review_state: %v", m)
	}
	if rs["state"] != "timed_out" || rs["action"] != "retry" || rs["attempts"].(float64) != 2 {
		t.Fatalf("review_state = %v, want a timed_out review of 2 attempts", rs)
	}
	if !strings.Contains(rs["cause"].(string), "claude-code") {
		t.Errorf("review_state.cause %v does not name the timed-out backend", rs["cause"])
	}

	// A pending review keeps state and attempts but omits cause and
	// action (both omitempty).
	buf, err = json.Marshal(CapPRDText(&PRD{ID: "cap2"}, 4096))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatal(err)
	}
	if rs, ok = m["review_state"].(map[string]any); !ok {
		t.Fatalf("capped copy JSON has no review_state: %v", m)
	}
	if rs["state"] != "pending" {
		t.Fatalf("review_state = %v, want pending", rs)
	}
	if _, present := rs["cause"]; present {
		t.Errorf("pending review_state should omit cause: %v", rs)
	}
	if _, present := rs["action"]; present {
		t.Errorf("pending review_state should omit action: %v", rs)
	}
	if rs["attempts"].(float64) != 0 {
		t.Errorf("pending review_state should carry no attempts: %v", rs)
	}

	// The stored record is never modified, and a call with max <= 0
	// returns the input untouched, so a record read with no cap does not
	// grow a review_state field it did not carry.
	stored := &PRD{ID: "cap3", Verdicts: []GuardrailVerdict{*reviewHold(reviewT0)}}
	got := CapPRDText(stored, 0)
	if got != stored {
		t.Fatalf("CapPRDText with max 0 must return the input unchanged; pointers differ")
	}
	if stored.ReviewState != nil {
		t.Fatalf("stored record must not carry ReviewState; got %+v", stored.ReviewState)
	}
}
