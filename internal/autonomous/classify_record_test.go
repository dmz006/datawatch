package autonomous

import (
	"testing"
	"time"
)

func classifyRecordPRD() *PRD {
	at := time.Unix(100, 0)
	scores := &VerdictScores{PassProbability: 0.55, Threshold: 0.7, OK: map[string]float64{"A": 0.55, "B": 0.45}}
	route := &VerdictRoute{Node: "gpu1", Model: "m", Path: "native"}
	return &PRD{
		Verdicts: []GuardrailVerdict{{Guardrail: "rules", Outcome: "pass", VerdictAt: at}},
		Story: []Story{{
			ID:       "s1",
			Verdicts: []GuardrailVerdict{{Guardrail: "security", Outcome: "block", Category: ClassifyCategoryInfra}},
			Tasks: []Task{
				{ID: "t1", Verification: &VerificationResult{OK: false, Category: ClassifyCategoryLowConfidence,
					VerifierMode: VerifierModeClassify, Scores: scores, Route: route, Summary: "low", VerifiedAt: at},
					Verdicts: []GuardrailVerdict{{Guardrail: "rules-review", Outcome: "block", Category: ClassifyCategoryLowConfidence,
						Mode: VerifierModeClassify, Scores: scores, Route: route}}},
				{ID: "t2", Verification: &VerificationResult{OK: true}},
				{ID: "t3"},
			},
		}},
	}
}

func TestClassifyRecordsAll(t *testing.T) {
	recs := ClassifyRecords(classifyRecordPRD(), "", false)
	if len(recs) != 5 {
		t.Fatalf("records = %d: %+v", len(recs), recs)
	}
	if recs[0].Scope != "automaton_guardrail" || recs[1].Scope != "story_guardrail" || recs[1].StoryID != "s1" {
		t.Fatalf("scopes/order wrong: %+v", recs[:2])
	}
	task := recs[2]
	if task.Scope != "task" || task.TaskID != "t1" || task.Mode != VerifierModeClassify || task.Scores == nil ||
		task.Scores.PassProbability != 0.55 || task.Route == nil || task.Route.Node != "gpu1" || !task.LowConfidence {
		t.Fatalf("task record = %+v", task)
	}
	if recs[3].Scope != "task_guardrail" || recs[3].Guardrail != "rules-review" || !recs[3].LowConfidence {
		t.Fatalf("task guardrail = %+v", recs[3])
	}
	if recs[4].Outcome != "pass" || recs[4].LowConfidence || recs[4].Mode != "" {
		t.Fatalf("passing task = %+v", recs[4])
	}
	if recs[1].LowConfidence {
		t.Fatal("an infra block is not a low-confidence block")
	}
}

func TestClassifyRecordsFilters(t *testing.T) {
	prd := classifyRecordPRD()
	low := ClassifyRecords(prd, "", true)
	if len(low) != 2 || !low[0].LowConfidence || !low[1].LowConfidence {
		t.Fatalf("low-confidence filter = %+v", low)
	}
	one := ClassifyRecords(prd, "t1", false)
	if len(one) != 2 || one[0].Scope != "task" || one[1].Scope != "task_guardrail" {
		t.Fatalf("task filter = %+v", one)
	}
	if got := ClassifyRecords(prd, "t3", false); len(got) != 0 {
		t.Fatalf("task with no verdicts = %+v", got)
	}
	if got := ClassifyRecords(nil, "", false); got != nil {
		t.Fatalf("nil PRD = %+v", got)
	}
}
