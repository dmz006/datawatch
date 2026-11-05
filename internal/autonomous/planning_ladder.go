// planning_ladder.go — planning ladder executor (config.PlanningLadder).
//
// Given the ordered planner chain a planning-routing rule resolves to and
// the ladder budgets, RunPlanningLadder produces a plan that passes the
// same deterministic checks decompose runs on its parse — scope lint
// (LintPlanScope), spec-requirement coverage (CheckPlanCoverage) and the
// structural rules (CheckPlanStructure) — by spending the budgets the
// ladder declares:
//
//  1. initial   — a fresh plan of the full spec by the current planner;
//  2. touch_up  — a targeted pass of the SAME planner over the last
//     plan: the prompt names the flagged task titles and the findings,
//     and instructs the planner to replace ONLY the matched tasks and
//     return the full plan again;
//  3. replan    — a full re-plan of the spec by the current planner,
//     with the findings of the failed plans passed as feedback;
//  4. escalate  — after EscalateAfterAttempts failed attempts at a
//     planner (or when the shared budgets ran out), the ladder restarts
//     at the next planner in the chain with a fresh initial, and keeps
//     spending whatever touch-up/replan budget is left.
//
// Budgets (touch-up passes, replay budgets) are shared across the whole
// chain: a planner that does not consume them leaves them for the
// planner it escalates to. When the chain is exhausted and both budgets
// are spent, the ladder is out of move: it returns Passed=false with
// the last plan it holds, the planner that made it, and the full
// attempt record.
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
	// LadderAttemptInitial — a fresh plan of the full spec (the first
	// attempt of a planner, and of a planner a ladder escalates to).
	LadderAttemptInitial = "initial"
	// LadderAttemptTouchUp — a targeted pass of the current planner over
	// the last plan: only the flagged tasks may change. Consumes one
	// touch-up budget.
	LadderAttemptTouchUp = "touch_up"
	// LadderAttemptReplan — a full re-plan of the spec by the current
	// planner, with the findings as feedback. Consumes one replay budget.
	LadderAttemptReplan = "replan"
)

// The outcome recorded on every attempt.
const (
	// LadderOutcomePass — the plan passed every check; the ladder stops.
	LadderOutcomePass = "pass"
	// LadderOutcomeFindings — the plan parsed but one or more checks
	// reported findings.
	LadderOutcomeFindings = "findings"
	// LadderOutcomePlannerError — the planner call itself failed.
	LadderOutcomePlannerError = "planner_error"
	// LadderOutcomeParseFailure — the raw output did not parse into a
	// plan (ParsePlanning failed).
	LadderOutcomeParseFailure = "parse_failure"
)

// PlannerFn is one planning call: the planner's (backend, model) pair,
// the full prompt, and the raw model output (which the ladder parses
// with ParsePlanning) are all it knows. The prompt shape is the
// planner's contract: it must return ONE complete plan whose shape
// ParsePlanning accepts. Tests inject this value to run the ladder
// without a real LLM call.
type PlannerFn func(ctx context.Context, backend, model, prompt string) (raw string, err error)

// AttemptRecord is one ladder attempt, in order. Findings is non-empty
// only for attempts that reached a parseable plan whose checks reported
// problems, or (for the failing attempts) the findings the next attempt
// is fed back.
type AttemptRecord struct {
	Index        int       // 1-based position in the ladder's attempt sequence
	Kind         string    // LadderAttemptInitial, ...TouchUp or ...Replan
	At           time.Time // the attempt's instant from the injected clock
	Backend      string
	Model        string
	Outcome      string   // one of the LadderOutcome* values
	Error        string   // the message for planner_error / parse_failure; empty otherwise
	FindingCount int      // len(Findings)
	Findings     []string // every finding string of this attempt
}

// LadderResult is the terminal state the ladder stepped to.
type LadderResult struct {
	Passed       bool              // true when the returned plan passed every check
	Planner      config.PlannerRef // the planner that produced FinalStories
	FinalStories []Story           // the plan to act on (nil when no attempt ever parsed)
	Attempts     []AttemptRecord
}

// RunPlanningLadder steps the attempt machine described in the file
// header: initial → (touch_up ≤ TouchUpMax, replan ≤ ReplanMax) →
// escalate to the next planner after EscalateAfterAttempts failed
// attempts (or when the budgets run out) → repeat on the next planner
// with whatever budget is left → out of move when the chain and both
// budgets are exhausted.
//
// Semantics:
//
//   - Every attempt (of any kind, against any planner) checks its plan
//     with the same three deterministic checks; a plan with zero
//     findings passes the ladder.
//   - touch-up and replay budgets are shared across the whole chain:
//     the budgets a planner does not spend are the budget the ladder
//     can still spend after escalating.
//   - A planner_error or parse_failure attempt has no plan to touch up,
//     so the ladder either escalates (next planner, fresh initial) or,
//     at the end of the chain, replays with a fresh budget.
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

	kind := LadderAttemptInitial
	plannerIdx := 0
	attemptsAtPlanner := 0
	feedbackFindings := []string(nil) // findings of the last parsed attempt, fed back to the next attempt of the same planner
	currentStories := []Story(nil)    // last parsed plan (a touch-up base)

	appendRecord := func(rec AttemptRecord) {
		result.Attempts = append(result.Attempts, rec)
	}
	stopExhausted := func(ref config.PlannerRef) LadderResult {
		result.Passed = false
		result.Planner = ref
		result.FinalStories = currentStories
		return result
	}

	for {
		if err := ctx.Err(); err != nil {
			return stopExhausted(planners[plannerIdx])
		}

		ref := planners[plannerIdx]
		prompt := ladderPrompt(kind, spec, feedbackFindings, currentStories)

		rec := AttemptRecord{
			Index:   len(result.Attempts) + 1,
			Kind:    kind,
			At:      now(),
			Backend: ref.Backend,
			Model:   ref.Model,
		}

		raw, callErr := plan(ctx, ref.Backend, ref.Model, prompt)
		var stories []Story
		var parseErr error
		if callErr == nil {
			_, stories, parseErr = ParsePlanning(raw)
		}

		switch {
		case callErr != nil:
			rec.Outcome = LadderOutcomePlannerError
			rec.Error = callErr.Error()
		case parseErr != nil:
			rec.Outcome = LadderOutcomeParseFailure
			rec.Error = parseErr.Error()
		default:
			findings := ladderFindings(spec, stories)
			rec.Findings = findings
			rec.FindingCount = len(findings)
			if len(findings) == 0 {
				rec.Outcome = LadderOutcomePass
				appendRecord(rec)
				result.Passed = true
				result.Planner = ref
				result.FinalStories = stories
				return result
			}
			rec.Outcome = LadderOutcomeFindings
			currentStories = stories
			feedbackFindings = findings
		}
		appendRecord(rec)
		attemptsAtPlanner++

		// Choose the next step (file-header semantics).
		switch {
		case callErr != nil || parseErr != nil:
			switch {
			case plannerIdx+1 < len(planners):
				// no plan to touch up: try the next planner fresh
				plannerIdx++
				attemptsAtPlanner = 0
				kind = LadderAttemptInitial
				feedbackFindings = nil
				currentStories = nil
			case replaysLeft > 0:
				replaysLeft--
				kind = LadderAttemptReplan
			default:
				return stopExhausted(ref)
			}
		default:
			budgetsExhausted := touchUpsLeft == 0 && replaysLeft == 0
			switch {
			case plannerIdx+1 < len(planners) && (budgetsExhausted || attemptsAtPlanner >= escalateAfter):
				// escalate: the next planner starts fresh and keeps
				// spending whatever budget is left
				plannerIdx++
				attemptsAtPlanner = 0
				kind = LadderAttemptInitial
				feedbackFindings = nil
				currentStories = nil
			case touchUpsLeft > 0:
				touchUpsLeft--
				kind = LadderAttemptTouchUp
			case replaysLeft > 0:
				replaysLeft--
				kind = LadderAttemptReplan
			default:
				return stopExhausted(ref)
			}
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

// ladderPrompt renders the full prompt of one attempt:
//
//   - initial — the bare spec; the planner must produce one complete
//     plan from it.
//   - touch_up — the spec, the flagged task titles and the findings,
//     with the instruction to replace ONLY the flagged tasks and return
//     the full plan again.
//   - replan — the spec and the findings of the failed plans as
//     feedback, with the instruction to re-plan the whole spec.
func ladderPrompt(kind string, spec string, findings []string, stories []Story) string {
	switch kind {
	case LadderAttemptTouchUp:
		var b strings.Builder
		b.WriteString("TOUCH-UP (targeted fix)\n\n")
		b.WriteString("Full spec:\n")
		b.WriteString(spec)
		b.WriteString("\n\n")
		b.WriteString("The previous plan failed the deterministic checks. Replace ONLY the flagged tasks listed below (you may edit their spec text and their fields); keep every other task and story unchanged, and return the FULL plan again in the same JSON shape ({\"title\": ..., \"stories\": [...]}).\n\n")
		if flagged := flaggedTaskTitles(stories, findings); len(flagged) > 0 {
			b.WriteString("Flagged tasks:\n")
			for _, title := range flagged {
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
		b.WriteString("The plan for this spec has failed the deterministic checks the same way. Re-plan the WHOLE spec from scratch — restructure the stories and tasks freely — and return the full new plan in the same JSON shape ({\"title\": ..., \"stories\": [...]}).\n\n")
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
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
	}
	return b.String()
}

// lintFlaggedTaskRe matches a scope-lint problem string's label:
// LintPlanScope renders one string per story/task as
// "task '<title>': <problem>" (or "story '<title>': ...").
var lintFlaggedTaskRe = regexp.MustCompile(`^task '(.*)': `)

// flaggedTaskTitles returns the titles the findings name, in finding
// order: scope-lint strings label the task they flag, and structural
// findings carry the task id of the task they flag (resolvable back to
// its title). Coverage findings name requirements rather than tasks;
// when they are the only findings the touch-up prompt leaves the pick
// of what to change to the planner.
func flaggedTaskTitles(stories []Story, findings []string) []string {
	titleByTaskID := make(map[string]string)
	for _, s := range stories {
		for _, t := range s.Tasks {
			titleByTaskID[t.ID] = t.Title
		}
	}
	seen := make(map[string]bool)
	var out []string
	add := func(title string) {
		title = strings.TrimSpace(title)
		if title != "" && !seen[title] {
			seen[title] = true
			out = append(out, title)
		}
	}
	for _, line := range findings {
		if m := lintFlaggedTaskRe.FindStringSubmatch(line); m != nil {
			add(m[1])
		}
	}
	for _, line := range findings {
		if m := structuralFindingTaskTitleRe.FindStringSubmatch(line); m != nil {
			add(titleByTaskID[m[1]])
		}
	}
	return out
}

// structuralFindingTaskTitleRe recovers the task id from a structural
// finding's stable first word ("task <id> has neither ...", "task <id>
// names file ...", "task <id> depends_on ...", "task <id> title ...").
var structuralFindingTaskTitleRe = regexp.MustCompile(`^task (\S+) `)

// ladderFindings runs the three deterministic decompose checks of a
// parsed plan and flattens their results into one finding string list,
// in the order: scope-lint problem strings, requirements reported as
// missing or weakened, and structural findings. Any finding makes the
// plan fail the ladder.
func ladderFindings(spec string, stories []Story) []string {
	// The ladder runs without a PRD: scope lint and the structural file
	// checks resolve against a zero PRD (their PRD context — writable
	// dirs, project tree root — is empty and their text-level rules
	// still run), which a plan-level execution needs none of.
	zeroPRD := &PRD{}

	var findings []string
	lint := LintPlanScope(zeroPRD, stories)
	findings = append(findings, lint...)
	for _, v := range CheckPlanCoverage(spec, stories).Unmatched {
		findings = append(findings, fmt.Sprintf("%s requirement: %s", v.Verdict, v.Requirement))
	}
	for _, f := range CheckPlanStructure(zeroPRD, stories).Findings {
		findings = append(findings, f.Detail)
	}
	return findings
}
