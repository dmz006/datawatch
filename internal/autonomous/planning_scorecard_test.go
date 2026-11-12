package autonomous

import (
	"strings"
	"testing"
	"time"
)

var scFixed = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func scNow() time.Time { return scFixed }

func scPRD(rec *PlanningRecord) *PRD { return &PRD{PlanningRecord: rec} }

func scAttempt(kind, outcome, backend, model string) AttemptRecord {
	return AttemptRecord{Kind: kind, Outcome: outcome, Backend: backend, Model: model}
}

func scRec(at time.Time, classes ...string) *PlanningRecord {
	return &PlanningRecord{
		RuleName: "rule-a", Classes: classes, PlannerBackend: "pi", PlannerModel: "qwen",
		RecordedAt: at,
		Attempts:   []AttemptRecord{scAttempt(LadderAttemptPlan, LadderOutcomePassed, "pi", "qwen")},
	}
}

func TestPlanningRoutingScorecardEmpty(t *testing.T) {
	sc := Scorecard(nil, time.Hour, scNow)
	if len(sc.Rows) != 0 || len(sc.WouldRoute) != 0 {
		t.Fatalf("expected empty, got %+v", sc)
	}
	sc = Scorecard([]*PRD{nil, {}}, time.Hour, scNow)
	if len(sc.Rows) != 0 {
		t.Fatalf("nil record counted: %+v", sc)
	}
	g, text := PlanningGateReport(sc)
	if g.Decomposes != 0 || !strings.Contains(text, "decomposes=0") {
		t.Fatalf("bad empty report: %q", text)
	}
}

func TestPlanningRoutingWindowEdge(t *testing.T) {
	w := 24 * time.Hour
	prds := []*PRD{
		scPRD(scRec(scFixed.Add(-w), "high")),                 // exactly at boundary: counts
		scPRD(scRec(scFixed.Add(-w-time.Nanosecond), "high")), // just outside
		scPRD(scRec(scFixed, "high")),                         // now: counts
		scPRD(scRec(scFixed.Add(time.Second), "high")),        // future
	}
	sc := Scorecard(prds, w, scNow)
	if len(sc.Rows) != 1 || sc.Rows[0].Decomposes != 2 {
		t.Fatalf("window edge wrong: %+v", sc.Rows)
	}
}

func TestPlanningRoutingShadowSeparation(t *testing.T) {
	shadow := scRec(scFixed, "high")
	shadow.Shadow = true
	prds := []*PRD{scPRD(scRec(scFixed, "high")), scPRD(shadow), scPRD(shadow)}
	sc := Scorecard(prds, time.Hour, scNow)
	if len(sc.Rows) != 1 || sc.Rows[0].Decomposes != 1 {
		t.Fatalf("shadow leaked into decomposes: %+v", sc.Rows)
	}
	if len(sc.WouldRoute) != 1 || sc.WouldRoute[0].WouldRoute != 2 || sc.WouldRoute[0].Rule != "rule-a" {
		t.Fatalf("would_route wrong: %+v", sc.WouldRoute)
	}
}

func TestPlanningRoutingUnclassified(t *testing.T) {
	sc := Scorecard([]*PRD{scPRD(scRec(scFixed))}, time.Hour, scNow)
	if len(sc.Rows) != 1 || sc.Rows[0].Class != "unclassified" {
		t.Fatalf("want unclassified row: %+v", sc.Rows)
	}
}

func TestPlanningRoutingOutcomesAndRounding(t *testing.T) {
	touch := scRec(scFixed, "high")
	touch.Attempts = []AttemptRecord{
		scAttempt(LadderAttemptPlan, LadderOutcomeFailed, "pi", "qwen"),
		scAttempt(LadderAttemptTouchUp, LadderOutcomePassed, "pi", "qwen"),
	}
	touch.FindingCount = 1
	replan := scRec(scFixed, "high")
	replan.Attempts = []AttemptRecord{
		scAttempt(LadderAttemptPlan, LadderOutcomeError, "pi", "qwen"),
		scAttempt(LadderAttemptReplan, LadderOutcomePassed, "claude", "sonnet"),
	}
	replan.FindingCount = 1
	replan.SupervisorEdits = 1
	first := scRec(scFixed, "high")
	first.FindingCount = 1
	sc := Scorecard([]*PRD{scPRD(first), scPRD(touch), scPRD(replan)}, time.Hour, scNow)
	var r ScorecardRow
	for _, x := range sc.Rows {
		if x.Backend == "pi" && x.Class == "high" {
			r = x
		}
	}
	if r.Decomposes != 3 || r.PassedFirstAttempt != 1 || r.PassedAfterTouchUp != 1 || r.PassedAfterReplan != 1 {
		t.Fatalf("outcomes: %+v", r)
	}
	if r.Errors != 1 || r.Escalations != 1 {
		t.Fatalf("errors/escalations: %+v", r)
	}
	if r.MeanFindings != 1.0 || r.MeanSupervisorEdits != 0.3 { // 1/3 rounds to 0.3
		t.Fatalf("rounding: findings=%v edits=%v", r.MeanFindings, r.MeanSupervisorEdits)
	}
	// 2/3 rounds up to 0.7
	a, b := scRec(scFixed, "x"), scRec(scFixed, "x")
	c := scRec(scFixed, "x")
	a.SupervisorEdits, b.SupervisorEdits = 1, 1
	sc = Scorecard([]*PRD{scPRD(a), scPRD(b), scPRD(c)}, time.Hour, scNow)
	if sc.Rows[0].MeanSupervisorEdits != 0.7 {
		t.Fatalf("want 0.7, got %v", sc.Rows[0].MeanSupervisorEdits)
	}
}

func TestPlanningRoutingMultiClassAndOrder(t *testing.T) {
	sc := Scorecard([]*PRD{scPRD(scRec(scFixed, "zeta", "alpha", "alpha"))}, 0, scNow)
	if len(sc.Rows) != 2 || sc.Rows[0].Class != "alpha" || sc.Rows[1].Class != "zeta" {
		t.Fatalf("rows: %+v", sc.Rows)
	}
	if sc.Rows[0].Decomposes != 1 {
		t.Fatalf("duplicate class double counted: %+v", sc.Rows[0])
	}
}

func TestPlanningRoutingGateReportNoFindingText(t *testing.T) {
	rec := scRec(scFixed, "high")
	rec.FindingCount = 2
	rec.Attempts = []AttemptRecord{{
		Kind: LadderAttemptPlan, Outcome: LadderOutcomeFailed, Backend: "pi", Model: "qwen",
		FindingCount: 2, Findings: []string{"SECRET-FINDING-ONE", "SECRET-FINDING-TWO"},
		Error: "SECRET-ERROR-TEXT",
	}}
	p := &PRD{Spec: "SECRET-SPEC-TEXT", PlanningRecord: rec,
		Story: []Story{{Title: "SECRET-STORY-TITLE", Tasks: []Task{{Title: "SECRET-TASK-TITLE"}}}}}
	shadow := *rec
	shadow.Shadow = true
	sc := Scorecard([]*PRD{p, scPRD(&shadow)}, time.Hour, scNow)
	g, text := PlanningGateReport(sc)
	if strings.Contains(text, "SECRET") {
		t.Fatalf("gate report leaks text: %q", text)
	}
	if g.Decomposes != 1 || g.WouldRouteTotal != 1 {
		t.Fatalf("summary: %+v", g)
	}
	for _, want := range []string{"planner=pi/qwen class=high", "mean_findings=2.0", "would_route planner=pi/qwen rule=rule-a count=1"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}
