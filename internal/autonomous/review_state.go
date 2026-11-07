package autonomous

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ReviewState classifies where an Automaton's review stands: passed
// (a pass verdict is the newest pass/block judgment), blocked (a real
// judgment blocked it — the verdict's issues say what to fix), timed_out
// (its last blocking verdict is a judge-timeout hold: the judge hit its
// time limit on every backend it tried, so it is held for review, not
// passed or blocked by a judgment) or pending (no judgment decided it
// yet — no verdicts at all, or only warnings).
type ReviewState string

// Review outcomes.
const (
	ReviewStatePassed   ReviewState = "passed"
	ReviewStateBlocked  ReviewState = "blocked"
	ReviewStateTimedOut ReviewState = "timed_out"
	ReviewStatePending  ReviewState = "pending"
)

// Review actions: what the operator (or an automation) should do next.
const (
	// ReviewActionFix: the block is a real judgment — work on what its
	// issues say, then re-review.
	ReviewActionFix = "fix"
	// ReviewActionRetry: the judge timed out on every backend it tried —
	// run it again on the same backend list.
	ReviewActionRetry = "retry"
	// ReviewActionReroute: the judge timed out, a stronger backend is
	// configured for the review and none of the timed-out attempts tried
	// it yet — run the review on that backend instead.
	ReviewActionReroute = "reroute"
)

// ReviewResult is the computed outcome of an Automaton's review, exposed
// as the review_state field of the PRD JSON view (see CapPRDText). It is
// always derived from the verdicts recorded on the PRD — never stored.
type ReviewResult struct {
	// State is one of ReviewStatePassed, ReviewStateBlocked,
	// ReviewStateTimedOut, ReviewStatePending.
	State ReviewState `json:"state"`
	// Cause is a one-line account of why the review is in this state. For
	// a timeout hold it names the backends that timed out and their time
	// limit; for a real block the judgment behind it. Omitted when there
	// is nothing to name.
	Cause string `json:"cause,omitempty"`
	// Action is ReviewActionFix, ReviewActionRetry or ReviewActionReroute,
	// the next step the review state points to. Omitted for pending.
	Action string `json:"action,omitempty"`
	// Attempts is the number of judge attempts behind the deciding verdict
	// (the hold's one record per backend); 0 for a single-judgment verdict
	// and for pending.
	Attempts int `json:"attempts"`
	// JudgeOutcome is how the judge ended (judge_could_not_start,
	// judge_timed_out or judge_found_problem), copied from the PRD's rules
	// review outcome. JudgeOutcomeText is its plain-language wording. Both
	// are omitted when no judge problem is recorded.
	JudgeOutcome     string `json:"judge_outcome,omitempty"`
	JudgeOutcomeText string `json:"judge_outcome_text,omitempty"`
}

// withJudgeOutcome copies the judge outcome kind and its wording from the
// PRD's rules review outcome onto r. Nothing is set when the kind is empty.
func withJudgeOutcome(r ReviewResult, prd *PRD) ReviewResult {
	if prd == nil || prd.RulesReviewOutcome == nil {
		return r
	}
	kind := prd.RulesReviewOutcome.JudgeOutcome
	if kind == "" {
		return r
	}
	r.JudgeOutcome = kind
	r.JudgeOutcomeText = judgeOutcomeWording(kind)
	return r
}

// reviewVerdicts collects every guardrail verdict recorded on the PRD —
// its own, then each story's and each task's, in the order recorded.
// Verdicts are appended over time, so a later entry is a later judgment.
func reviewVerdicts(prd *PRD) []GuardrailVerdict {
	var out []GuardrailVerdict
	out = append(out, prd.Verdicts...)
	for i := range prd.Story {
		s := &prd.Story[i]
		out = append(out, s.Verdicts...)
		for j := range s.Tasks {
			out = append(out, s.Tasks[j].Verdicts...)
		}
	}
	return out
}

// latestVerdict returns the newest verdict in vs with the given outcome,
// or nil. Newest means the latest VerdictAt; on equal timestamps the later
// entry wins (the later record is the later judgment).
func latestVerdict(vs []GuardrailVerdict, outcome string) *GuardrailVerdict {
	best := -1
	for i := range vs {
		if vs[i].Outcome != outcome {
			continue
		}
		if best == -1 || !vs[i].VerdictAt.Before(vs[best].VerdictAt) {
			best = i
		}
	}
	if best == -1 {
		return nil
	}
	return &vs[best]
}

// ReviewStateOf derives the review state of prd from the verdicts
// recorded on it (its own, and those of its stories and tasks):
//
//   - the newest block verdict is a judge-timeout hold → timed_out, with
//     the Cause naming the backends that timed out and their time limit,
//     and the Action "retry" (same backend list) or "reroute" (a stronger
//     backend is configured and not yet tried);
//   - the newest block verdict is any other judgment → blocked, Action
//     "fix" (the verdict's issues say what to fix);
//   - the newest judgment is a pass → passed;
//   - no pass or block at all (no verdicts, or only warnings) → pending.
//
// A nil PRD is pending.
func ReviewStateOf(prd *PRD) ReviewResult {
	return withJudgeOutcome(reviewStateOf(prd), prd)
}

func reviewStateOf(prd *PRD) ReviewResult {
	if prd == nil {
		return ReviewResult{State: ReviewStatePending}
	}
	vs := reviewVerdicts(prd)
	latestPass := latestVerdict(vs, "pass")
	latestBlock := latestVerdict(vs, "block")

	// A block (real or held) decides the review unless a strictly newer
	// pass superseded it: re-reviewing after the review was blocked or
	// held and passing replaces the block, it does not add to it.
	if latestBlock != nil && (latestPass == nil || latestBlock.VerdictAt.After(latestPass.VerdictAt)) {
		res := ReviewResult{Attempts: reviewAttemptCount(*latestBlock)}
		if IsJudgeTimeoutHold(*latestBlock) {
			res.State = ReviewStateTimedOut
			res.Cause = timeoutHoldCause(*latestBlock)
			res.Action = timeoutHoldAction(prd, *latestBlock)
			return res
		}
		res.State = ReviewStateBlocked
		res.Cause = realBlockCause(*latestBlock)
		res.Action = ReviewActionFix
		return res
	}

	if latestPass != nil {
		return ReviewResult{State: ReviewStatePassed, Attempts: reviewAttemptCount(*latestPass)}
	}

	// No pass and no block: pending. The newest warning, when there is
	// one, names what a real judgment already found without blocking, so
	// it is carried as the cause, leaving pending without a judgment at
	// all cause- and action-free.
	w := latestVerdict(vs, "warn")
	if w == nil {
		return ReviewResult{State: ReviewStatePending}
	}
	return ReviewResult{State: ReviewStatePending, Cause: realBlockCause(*w)}
}

// reviewAttemptCount is the number of judge attempts behind v: the
// structured per-backend summary when present, otherwise the per-attempt
// lines in the verdict text ("backend …, started …"), zero otherwise.
func reviewAttemptCount(v GuardrailVerdict) int {
	if len(v.JudgeAttempts) > 0 {
		return len(v.JudgeAttempts)
	}
	n := 0
	for _, s := range v.Issues {
		if strings.HasPrefix(strings.TrimSpace(s), "backend ") {
			n++
		}
	}
	return n
}

var (
	judgeLimitPattern   = regexp.MustCompile(`\(limit ([0-9][^)]*)\)`)
	judgeBackendPattern = regexp.MustCompile(`backend (\S+), started`)
)

// timeoutHoldBackends names the backends whose attempt timed out, from
// the structured attempt records and, when there are none, from the
// verdict text. Backends that only lacked a capacity slot are not timed
// out and are not named.
func timeoutHoldBackends(v GuardrailVerdict) []string {
	var names []string
	for _, a := range v.JudgeAttempts {
		if a.Outcome == JudgeOutcomeTimedOut && a.Backend != "" {
			names = append(names, a.Backend)
		}
	}
	if len(names) > 0 {
		return names
	}
	if v.Backend != "" {
		return []string{v.Backend}
	}
	txt := strings.Join(append(append([]string{v.Summary}, v.Issues...), v.RawJudge), "\n")
	for _, m := range judgeBackendPattern.FindAllStringSubmatch(txt, -1) {
		if len(m) == 2 {
			names = append(names, m[1])
		}
	}
	return names
}

// timeoutHoldLimit extracts the time limit of the timed-out attempts
// from the verdict text ("timed out after … (limit 30m0s)"), or "" when
// the text carries no limit.
func timeoutHoldLimit(v GuardrailVerdict) string {
	txt := strings.Join(append(append([]string{v.Summary}, v.Issues...), v.RawJudge), "\n")
	if m := judgeLimitPattern.FindStringSubmatch(txt); m != nil {
		return m[1]
	}
	return ""
}

// timeoutHoldCause words the cause of a judge-timeout hold: which
// backends timed out, how many, and the time limit of the attempts.
func timeoutHoldCause(v GuardrailVerdict) string {
	names := timeoutHoldBackends(v)
	if len(names) == 0 {
		return "judge timed out on every backend it tried"
	}
	cause := fmt.Sprintf("judge timed out on %d backend(s): %s", len(names), strings.Join(names, ", "))
	if limit := timeoutHoldLimit(v); limit != "" {
		cause += ", limit " + limit
	}
	return cause
}

// timeoutHoldAction is the next step for a held review: reroute to the
// configured stronger guardrail backend when one is set and none of the
// timed-out attempts tried it, otherwise retry on the same backend list.
func timeoutHoldAction(prd *PRD, v GuardrailVerdict) string {
	if prd != nil && prd.GuardrailBackend != "" &&
		!slices.Contains(timeoutHoldBackends(v), prd.GuardrailBackend) {
		return ReviewActionReroute
	}
	return ReviewActionRetry
}

// realBlockCause words the judgment behind a block verdict (the pending
// state uses it the same way for the newest warning): the guardrail that
// judged, and the judgment itself.
func realBlockCause(v GuardrailVerdict) string {
	detail := v.Summary
	if detail == "" && len(v.Issues) > 0 {
		detail = v.Issues[0]
	}
	if v.Guardrail == "" {
		if detail == "" {
			return "guardrail returned " + v.Outcome
		}
		return detail
	}
	if detail == "" {
		return "guardrail " + v.Guardrail + " returned " + v.Outcome
	}
	return fmt.Sprintf("guardrail %s returned %s: %s", v.Guardrail, v.Outcome, detail)
}
