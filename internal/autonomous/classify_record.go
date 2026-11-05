package autonomous

import "time"

// ClassifyRecord is one verdict as the verdict-record surfaces (REST, MCP, CLI
// and chat) show it: the mode, the score vector and the route that make a
// classify-mode outcome reproducible, plus the outcome and category.
type ClassifyRecord struct {
	// Scope is "task" (the task verification), "task_guardrail",
	// "story_guardrail" or "automaton_guardrail".
	Scope     string         `json:"scope"`
	StoryID   string         `json:"story_id,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	Guardrail string         `json:"guardrail,omitempty"`
	Outcome   string         `json:"outcome"` // pass | warn | block
	Category  string         `json:"category,omitempty"`
	Mode      string         `json:"mode,omitempty"`
	Scores    *VerdictScores `json:"scores,omitempty"`
	Route     *VerdictRoute  `json:"route,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	At        time.Time      `json:"at"`
	// LowConfidence marks a block that the classifier reached because its
	// confidence was too low, not because it found the work wrong. It is the
	// block an operator reviews by hand.
	LowConfidence bool `json:"low_confidence"`
}

// ClassifyRecords lists the verdict records of an Automaton. taskID narrows the
// list to one task (the Automaton and story guardrail verdicts are then left
// out); onlyLowConfidence keeps just the low-confidence blocks. Verdicts that
// carry no classify mode, scores or route (generate mode, deterministic checks)
// are listed too, so the record is complete: their Mode is empty.
func ClassifyRecords(prd *PRD, taskID string, onlyLowConfidence bool) []ClassifyRecord {
	if prd == nil {
		return nil
	}
	var out []ClassifyRecord
	add := func(r ClassifyRecord) {
		r.LowConfidence = r.Outcome == "block" && r.Category == ClassifyCategoryLowConfidence
		if onlyLowConfidence && !r.LowConfidence {
			return
		}
		out = append(out, r)
	}
	fromGuardrail := func(scope, storyID, tID string, v GuardrailVerdict) ClassifyRecord {
		return ClassifyRecord{Scope: scope, StoryID: storyID, TaskID: tID, Guardrail: v.Guardrail,
			Outcome: v.Outcome, Category: v.Category, Mode: v.Mode, Scores: v.Scores, Route: v.Route,
			Summary: v.Summary, At: v.VerdictAt}
	}
	if taskID == "" {
		for _, v := range prd.Verdicts {
			add(fromGuardrail("automaton_guardrail", "", "", v))
		}
	}
	for i := range prd.Story {
		st := &prd.Story[i]
		if taskID == "" {
			for _, v := range st.Verdicts {
				add(fromGuardrail("story_guardrail", st.ID, "", v))
			}
		}
		for j := range st.Tasks {
			t := &st.Tasks[j]
			if taskID != "" && t.ID != taskID {
				continue
			}
			if v := t.Verification; v != nil {
				outcome := "pass"
				if !v.OK {
					outcome = "block"
				}
				add(ClassifyRecord{Scope: "task", StoryID: st.ID, TaskID: t.ID, Outcome: outcome,
					Category: v.Category, Mode: v.VerifierMode, Scores: v.Scores, Route: v.Route,
					Summary: v.Summary, At: v.VerifiedAt})
			}
			for _, v := range t.Verdicts {
				add(fromGuardrail("task_guardrail", st.ID, t.ID, v))
			}
		}
	}
	return out
}
