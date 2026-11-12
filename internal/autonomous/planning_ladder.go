// planning_ladder.go — planning ladder executor (config.PlanningLadder).
//
// Given the ordered planner chain a planning-routing rule resolves to and
// the ladder budgets, RunPlanningLadder produces a plan that passes the
// same deterministic checks decompose runs on its parse — scope lint
// (LintPlanScope), spec-requirement coverage (CheckPlanCoverage) and the
// structural rules (CheckPlanStructure) — by spending the budgets the
// ladder declares:
//
//  1. plan  — the planner's first move: a fresh plan of the full spec;
//  2. touch_up — a targeted pass of the SAME planner over the last
//     plan: the prompt names the flagged task titles and the findings
//     and instructs the planner to replace ONLY the flagged tasks and
//     return the full plan. The ladder enforces that rule on the
//     response itself (applyTouchUp): only the flagged tasks matched
//     by title are taken from the response — every other task stays
//     unchanged from the last plan, so a planner that modifies
//     unflagged work cannot move it either way;
//  3. replan — a full re-plan of the spec by the current planner, with
//     the findings of the failed plans passed as feedback;
//  4. escalate — once the planner has burned EscalateAfterAttempts
//     failed PLAN attempts (its plan and replan moves; touch-ups never
//     count) or the shared budgets are out, the ladder restarts at the
//     next planner in the chain with a fresh plan move and keeps
//     spending whatever budget is left — the budgets a planner does not
//     spend are the budget its successor can still spend.
//
// When the chain and both budgets are exhausted, the ladder is out of
// move: it returns Passed=false with the last plan it holds, the
// planner that made it, the last findings it saw, and the full attempt
// record.
//
// The planner itself is injected (PlannerFn): this file knows nothing
// about how a model is called — only about what a plan must look like
// (it parses the raw output with ParsePlanning) and what a failed plan
// gets back. TestPlanningRouting* exercises the real state machine
// against scripted raw plans without any LLM.
package autonomous

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The three attempt kinds the ladder steps take, in the order a ladder
// can spend them at one planner.
const (
	// LadderAttemptPlan — a planner's fresh plan of the full spec (the
	// first move at a planner, and after an escalation).
	LadderAttemptPlan = "plan"
	// LadderAttemptTouchUp — a targeted pass of the current planner over
	// the last plan: only the flagged tasks matched by title are taken
	// from the response, every other task is kept unchanged. Consumes
	// one touch-up budget.
	LadderAttemptTouchUp = "touch_up"
	// LadderAttemptReplan — a full re-plan of the spec by the current
	// planner, with the findings as feedback. Consumes one replay
	// budget.
	LadderAttemptReplan = "replan"
)

// The outcomes recorded on every attempt.
const (
	// LadderOutcomePassed — the plan passed every check; the ladder
	// stops.
	LadderOutcomePassed = "passed"
	// LadderOutcomeFailed — the attempt produced no acceptable plan:
	// either it parsed but the checks reported findings, or the raw
	// output did not parse into a plan at all (the Error field says
	// which).
	LadderOutcomeFailed = "failed"
	// LadderOutcomeError — the planner call itself failed (see Error).
	LadderOutcomeError = "error"
)

// PlannerFn is one planning call: the planner's (backend, model) pair,
// the full prompt, and the raw model output (which the ladder parses
// with ParsePlanning) are all it knows. The prompt shape is the
// planner's contract: it must return ONE complete plan whose shape
// ParsePlanning accepts. Tests inject this value to run the ladder
// without a real LLM call.
type PlannerFn func(ctx context.Context, backend, model, prompt string) (raw string, err error)

// AttemptRecord is one ladder attempt, in order. Findings is non-empty
// for attempts whose plan reached the deterministic checks — whatever
// the checks reported — and is empty for attempts that never produced
// a parseable plan (planner error or parse failure, Error field set).
type AttemptRecord struct {
	Index        int       // 1-based position in the ladder's attempt sequence
	Kind         string    // LadderAttemptPlan, LadderAttemptTouchUp or LadderAttemptReplan
	At           time.Time // the attempt's instant from the injected clock
	Backend      string
	Model        string
	Outcome      string // LadderOutcomePassed, LadderOutcomeFailed or LadderOutcomeError
	Error        string // message for LadderOutcomeError attempts, and the parse error for parse failures (empty otherwise)
	FindingCount int    // len(Findings)
	Findings     []string
}

// LadderResult is the terminal state the ladder stepped to. Findings
// carries the last findings the ladder saw: empty for a passing result,
// and — for a failed one — the findings of the last attempt that
// reached the deterministic checks, so the caller does not have to dig
// through Attempts.
type LadderResult struct {
	Passed       bool              // true when the returned plan passed every check
	Planner      config.PlannerRef // the planner that produced FinalStories
	FinalStories []Story           // the plan to act on (nil when no attempt ever parsed)
	Findings     []string          // last findings (empty when Passed)
	Attempts     []AttemptRecord
}

// RunPlanningLadder steps the attempt machine described in the file
// header. Semantics:
//
//   - Every parsed plan — of any kind, at any planner — is checked
//     with the same three deterministic checks; a plan with zero
//     findings passes the ladder.
//   - A touch-up response is merged over the last plan so that ONLY the
//     flagged tasks matched by title may change; everything else is
//     kept unchanged from the last plan and any new task or
//     restructured story the response carries is dropped (see
//     applyTouchUp). The merged plan is what gets checked.
//   - touch-up and replay budgets are shared across the whole chain.
//   - A planner_error or parse_failure attempt has no plan to touch up:
//     the ladder escalates (next planner, fresh plan) or, at the end of
//     the chain, replays with the replay budget.
//   - The per-planner allowance EscalateAfterAttempts counts failed
//     PLAN attempts only (plan and replan moves; touch-ups never
//     count).
//   - ctx is honored between attempts: a cancelled context stops the
//     ladder with what it holds and what it recorded.
//
// now is the ladder's clock (every AttemptRecord.At comes from it);
// nil falls back to time.Now. plan is nil-safe: a nil planner or an
// empty chain returns the zero LadderResult (Passed=false, no
// attempts).
func RunPlanningLadder(ctx context.Context, ladder config.PlanningLadder, planners []config.PlannerRef, spec string, plan PlannerFn, now func() time.Time) LadderResult {
	if now == nil {
		now = time.Now
	}
	touchUpsLeft, replaysLeft, escalateAfter := effectiveLadder(ladder)
	result := LadderResult{Attempts: []AttemptRecord{}}
	if plan == nil || len(planners) == 0 {
		return result
	}

	kind := LadderAttemptPlan
	plannerIdx := 0
	planAttemptsAtPlanner := 0        // failed plan+replan moves so far at THIS planner (touch-ups never count)
	feedbackFindings := []string(nil) // findings of the last checked plan, fed back to the next attempt
	currentStories := []Story(nil)    // last checked plan — the base a touch-up merges over
	flaggedSet := map[string]bool{}   // titles a touch-up may replace (applyTouchUp's contract)
	lastFindings := []string(nil)     // carried onto LadderResult.Findings when the ladder fails

	finish := func() LadderResult {
		result.Passed = false
		result.Planner = planners[plannerIdx]
		result.FinalStories = currentStories
		if lastFindings != nil {
			result.Findings = lastFindings
		}
		return result
	}
	escalate := func() {
		plannerIdx++
		planAttemptsAtPlanner = 0
		kind = LadderAttemptPlan
		feedbackFindings = nil
		currentStories = nil
		flaggedSet = map[string]bool{}
	}

	for {
		if err := ctx.Err(); err != nil {
			return finish()
		}

		ref := planners[plannerIdx]
		prompt := ladderPrompt(kind, spec, feedbackFindings)

		rec := AttemptRecord{
			Index:   len(result.Attempts) + 1,
			Kind:    kind,
			At:      now(),
			Backend: ref.Backend,
			Model:   ref.Model,
		}

		callErr := error(nil)
		var parsed []Story
		var parseErr error
		if raw, err := plan(ctx, ref.Backend, ref.Model, prompt); err != nil {
			callErr = err
		} else {
			_, parsed, parseErr = ParsePlanning(raw)
		}

		budgetsExhausted := touchUpsLeft == 0 && replaysLeft == 0
		nextPlanner := plannerIdx+1 < len(planners)

		switch {
		case callErr != nil:
			rec.Outcome = LadderOutcomeError
			rec.Error = callErr.Error()
		case parseErr != nil:
			rec.Outcome = LadderOutcomeFailed
			rec.Error = parseErr.Error()
		default:
			checked := parsed
			if kind == LadderAttemptTouchUp {
				checked = applyTouchUp(currentStories, parsed, flaggedSet)
			}
			findings := ladderFindings(spec, checked)
			rec.Findings = findings
			rec.FindingCount = len(findings)
			if len(findings) == 0 {
				rec.Outcome = LadderOutcomePassed
				result.Attempts = append(result.Attempts, rec)
				result.Passed = true
				result.Planner = ref
				result.FinalStories = checked
				return result
			}
			rec.Outcome = LadderOutcomeFailed
			currentStories = checked
			feedbackFindings = findings
			lastFindings = findings
		}
		result.Attempts = append(result.Attempts, rec)
		if kind == LadderAttemptPlan || kind == LadderAttemptReplan {
			planAttemptsAtPlanner++
		}

		// Choose the next step (file-header semantics).
		switch {
		case callErr != nil || parseErr != nil:
			// No plan to touch up or keep: a fresh plan at the next
			// planner, or — at the end of the chain — the replay
			// budget.
			switch {
			case nextPlanner:
				escalate()
			case replaysLeft > 0:
				replaysLeft--
				kind = LadderAttemptReplan
			default:
				return finish()
			}
		case nextPlanner && (budgetsExhausted || planAttemptsAtPlanner >= escalateAfter):
			escalate()
		case touchUpsLeft > 0:
			flaggedSet = flaggedTitleSet(currentStories, feedbackFindings)
			touchUpsLeft--
			kind = LadderAttemptTouchUp
		case replaysLeft > 0:
			replaysLeft--
			kind = LadderAttemptReplan
		default:
			return finish()
		}
	}
}

// effectiveLadder resolves the ladder's zero (or negative) count fields
// to their defaults — a zero count means "use the default" per
// config.PlanningLadder.
func effectiveLadder(l config.PlanningLadder) (touchUpMax, replayMax, escalateAfter int) {
	touchUpMax = l.TouchUpMax
	if touchUpMax < 1 {
		touchUpMax = config.PlanningRoutingDefaultTouchUpMax
	}
	replayMax = l.ReplanMax
	if replayMax < 1 {
		replayMax = config.PlanningRoutingDefaultReplanMax
	}
	escalateAfter = l.EscalateAfterAttempts
	if escalateAfter < 1 {
		escalateAfter = config.PlanningRoutingDefaultEscalateAfterAttempts
	}
	return touchUpMax, replayMax, escalateAfter
}

// applyTouchUp enforces the touch-up contract: whatever the planner's
// full-plan response carries, the ladder substitutes ONLY the flagged
// tasks matched by title (title match is exact after trimming
// surrounding whitespace) at their positions in the base plan.
// Everything else — unflagged tasks, story structure, any added or
// reordered task — is taken unchanged from the base plan (or dropped,
// when the response added work), so a response that tampers with
// unflagged tasks cannot move them one byte. A flagged title the
// response does not carry back keeps the base task unchanged.
func applyTouchUp(base, updated []Story, flagged map[string]bool) []Story {
	byTitle := map[string]Task{}
	for _, s := range updated {
		for _, t := range s.Tasks {
			key := titleKey(t.Title)
			if key != "" {
				if _, seen := byTitle[key]; !seen {
					byTitle[key] = copyTask(t)
				}
			}
		}
	}
	out := make([]Story, len(base))
	for si, s := range base {
		ns := s
		ns.Tasks = make([]Task, len(s.Tasks))
		for ti, t := range s.Tasks {
			ns.Tasks[ti] = copyTask(t)
			if key := titleKey(t.Title); flagged[key] {
				if rep, ok := byTitle[key]; ok {
					ns.Tasks[ti] = rep
				}
			}
		}
		out[si] = ns
	}
	return out
}

// copyTask copies the planner-owned reference fields (the DependsOn
// slice, the pointer fields) so the ladder's plan never aliases the
// parsed response's memory.
func copyTask(t Task) Task {
	t.DependsOn = append([]string(nil), t.DependsOn...)
	return t
}

// titleKey is the match key for "matched by title": surrounding
// whitespace is not part of the match, everything else is.
func titleKey(title string) string { return strings.TrimSpace(title) }

// flaggedTitleSet resolves the set of task titles a touch-up may
// replace, from the failures of a checked plan: scope-lint problem
// strings label the task they flag ("task '<title>': <problem>"), and
// structural findings carry the task id they flag (resolvable back to
// its title). Coverage findings name requirements rather than tasks;
// when they are the only findings, the set is empty and the strict
// touch-up contract then holds the whole plan in place until the
// ladder spends its next budget.
func flaggedTitleSet(stories []Story, findings []string) map[string]bool {
	set := map[string]bool{}
	titleByTaskID := make(map[string]string)
	for _, s := range stories {
		for _, t := range s.Tasks {
			titleByTaskID[t.ID] = t.Title
		}
	}
	for _, line := range findings {
		if m := lintFlaggedTaskRe.FindStringSubmatch(line); m != nil {
			if k := titleKey(m[1]); k != "" {
				set[k] = true
			}
		}
	}
	for _, line := range findings {
		if m := structuralFlaggedTaskRe.FindStringSubmatch(line); m != nil {
			if k := titleKey(titleByTaskID[m[1]]); k != "" {
				set[k] = true
			}
		}
	}
	return set
}

// ladderPrompt renders the full prompt of one attempt:
//
//   - plan  — the bare spec; the planner must produce one complete
//     plan from it.
//   - touch_up — the spec, the flagged task titles and the findings,
//     with the instruction to replace ONLY those tasks and return the
//     full plan again. The ladder enforces that contract on the
//     response (applyTouchUp) whether the planner honors it or not.
//   - replan — the spec and the findings of the failed plans as
//     feedback, with the instruction to re-plan the whole spec.
func ladderPrompt(kind string, spec string, findings []string) string {
	switch kind {
	case LadderAttemptTouchUp:
		var b strings.Builder
		b.WriteString("TOUCH-UP (targeted fix)\n\n")
		b.WriteString("Full spec:\n")
		b.WriteString(spec)
		b.WriteString("\n\n")
		b.WriteString("The previous plan failed the deterministic checks. Replace ONLY the flagged tasks listed below (you may edit their spec text and their fields), keep every other task and story exactly as it stands, and return the FULL plan again in the same JSON shape ({\"title\": ..., \"stories\": [...]}).\n\n")
		titles := flaggedTaskTitleList(findings)
		if len(titles) > 0 {
			b.WriteString("Flagged tasks:\n")
			for _, title := range titles {
				b.WriteString("- ")
				b.WriteString(strconv.Quote(title))
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		b.WriteString("Findings from the previous plan:\n")
		b.WriteString(findingsBlock(findings))
		return b.String()
	case LadderAttemptReplan:
		var b strings.Builder
		b.WriteString("REPLAN (full re-plan)\n\n")
		b.WriteString("Full spec:\n")
		b.WriteString(spec)
		b.WriteString("\n\n")
		b.WriteString("The plan for this spec failed the deterministic checks. Re-plan the WHOLE spec from scratch — restructure the stories and tasks freely — and return the full new plan in the same JSON shape ({\"title\": ..., \"stories\": [...]}).\n\n")
		b.WriteString("Findings from the previous plans (feedback):\n")
		b.WriteString(findingsBlock(findings))
		return b.String()
	default:
		return spec
	}
}

func findingsBlock(findings []string) string {
	var b strings.Builder
	for i, f := range findings {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	return b.String()
}

// lintFlaggedTaskRe recovers the flagged task title from a scope-lint
// problem string: LintPlanScope renders one per flagged task/story as
// "task '<title>': <problem>".
var lintFlaggedTaskRe = regexp.MustCompile(`^task '(.*)': `)

// structuralFlaggedTaskRe recovers the flagged task id from a
// structural finding ("task <id> has neither ...", "task <id> depends_on
// ..."). The leading class excludes the quote, so lint's "task '<t>': …"
// shape does not match here.
var structuralFlaggedTaskRe = regexp.MustCompile(`^task ([^'\s][^\s]*) `)

// flaggedTaskTitleList returns the titles the findings name, in
// finding order (the prompt's "Flagged tasks" list).
func flaggedTaskTitleList(findings []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, line := range findings {
		if m := lintFlaggedTaskRe.FindStringSubmatch(line); m != nil {
			k := titleKey(m[1])
			if k != "" && !seen[k] {
				seen[k] = true
				out = append(out, m[1])
			}
		}
	}
	return out
}

// ladderFindings runs the three deterministic decompose checks of a
// parsed plan and flattens their results into one finding string list,
// in the order: scope-lint problem strings, requirements reported as
// missing or weakened, and structural findings.
func ladderFindings(spec string, stories []Story) []string {
	// The ladder runs without a PRD: scope lint and the structural
	// checks resolve against a zero PRD (their PRD context — writable
	// dirs, project tree root — is empty and their text-level rules
	// still run), which the plan-level checks need.
	zeroPRD := &PRD{}
	var findings []string
	findings = append(findings, LintPlanScope(zeroPRD, stories)...)
	for _, v := range CheckPlanCoverage(spec, stories).Unmatched {
		findings = append(findings, fmt.Sprintf("%s requirement: %s", v.Verdict, v.Requirement))
	}
	for _, f := range CheckPlanStructure(zeroPRD, stories).Findings {
		findings = append(findings, f.Detail)
	}
	return findings
}
