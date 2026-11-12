// Task 6cb90009 — chat-friendly outcome lines for the PRD-level autonomous
// comm verbs (set-llm, set-story-llm, set-task-llm, decompose/re-plan): every
// one of them replies with the daemon's JSON; when the response also carries
// a structured outcome (top-level `override` and/or `planning`), the reply
// leads with an explicit line for it — which tasks the override landed on vs
// left alone (running/finished), and whether a planning requeue was requeued,
// deferred to the next run, or a no-op — instead of only the pretty JSON.
package router

import (
	"encoding/json"
	"strings"
)

// commLLMOutcomeTask mirrors one LLMOverrideOutcome as the daemon serializes
// it (internal/autonomous.LLMOverrideOutcome).
type commLLMOutcomeTask struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

// commLLMOverride mirrors a queued-mode per-task outcome
// (internal/autonomous.LLMOverrideResult).
type commLLMOverride struct {
	Changed   []commLLMOutcomeTask `json:"changed"`
	LeftAlone []commLLMOutcomeTask `json:"left_alone"`
}

// commLLMPlanning mirrors a planning requeue outcome
// (internal/autonomous.PlanningRequeueResult — only the line needs its shape).
type commLLMPlanning struct {
	Outcome string `json:"outcome"`
}

// commLLMOutcome parses the two structured-outcome keys an autonomous set-llm
// / decompose response may carry.
type commLLMOutcome struct {
	Override *commLLMOverride `json:"override"`
	Planning *commLLMPlanning `json:"planning"`
}

// commLLMOverrideLine renders the compact line for the per-task split:
// "updated: … — left alone: …". The left-alone reasons are the per-task state
// (running / finished; applies when they next run), so the operator can see
// who the change reached and who it intentionally did not.
func commLLMOverrideLine(ov *commLLMOverride) string {
	join := func(ts []commLLMOutcomeTask) string {
		parts := make([]string, 0, len(ts))
		for _, t := range ts {
			if t.Reason != "" {
				parts = append(parts, t.TaskID+" ("+t.Reason+")")
			} else {
				parts = append(parts, t.TaskID)
			}
		}
		return strings.Join(parts, ", ")
	}
	bits := []string{}
	if len(ov.Changed) > 0 {
		bits = append(bits, "updated: "+join(ov.Changed))
	} else {
		bits = append(bits, "updated: (none)")
	}
	if len(ov.LeftAlone) > 0 {
		bits = append(bits, "left alone: "+join(ov.LeftAlone))
	} else {
		bits = append(bits, "left alone: (none)")
	}
	return strings.Join(bits, " — ")
}

// commLLMOutcomeLine composes the chat-friendly outcome line for an
// autonomous set-llm / decompose response: the per-task split (when the
// response carries an override key) and the planning requeue outcome line
// (planning requeued → …, deferred to next run, or unchanged). Empty when the
// response carries no structured outcome.
func commLLMOutcomeLine(resp string) string {
	var o commLLMOutcome
	if err := json.Unmarshal([]byte(resp), &o); err != nil {
		return ""
	}
	parts := []string{}
	if o.Override != nil && (len(o.Override.Changed) > 0 || len(o.Override.LeftAlone) > 0) {
		parts = append(parts, commLLMOverrideLine(o.Override))
	}
	if o.Planning != nil && o.Planning.Outcome != "" {
		parts = append(parts, planningRequeueReplyLine(resp))
	}
	return strings.Join(parts, " | ")
}

// autonomousOutcomeReply is the comm reply for an autonomous set-llm /
// set-task-llm / decompose call: the chat-friendly outcome line (updated vs
// left alone; requeued or deferred) when one exists, then the daemon's JSON.
func autonomousOutcomeReply(out string) string {
	s := prettyJSON(out)
	if line := commLLMOutcomeLine(out); line != "" {
		s = line + "\n" + s
	}
	return s
}
