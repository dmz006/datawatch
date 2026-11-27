package delivery

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/session"
)

// declaredValues returns the string constants of one named type in file.
func declaredValues(t *testing.T, file, typeName string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		id, ok := vs.Type.(*ast.Ident)
		if !ok || id.Name != typeName {
			return true
		}
		for _, v := range vs.Values {
			if lit, ok := v.(*ast.BasicLit); ok {
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// A status added without a rule fails here.
func TestEveryDeclaredStatusHasARule(t *testing.T) {
	automaton := declaredValues(t, "../autonomous/models.go", "PRDStatus")
	task := declaredValues(t, "../autonomous/models.go", "TaskStatus")
	if len(automaton) < 15 || len(task) < 12 {
		t.Fatalf("read %d Automaton and %d task statuses", len(automaton), len(task))
	}
	for _, s := range automaton {
		if !StatusRuled(s) {
			t.Errorf("Automaton status %q has no rule", s)
		}
	}
	for _, s := range task {
		if !TaskStatusRuled(s) {
			t.Errorf("task status %q has no rule", s)
		}
	}
	if StatusRuled("brand_new_status") || TaskStatusRuled("brand_new_status") {
		t.Error("an unknown status must not count as ruled")
	}
}

func finished(s string) bool {
	switch s {
	case "completed", "rejected", "cancelled", "archived":
		return true
	}
	return false
}

func candidateFor(cs []Candidate, kind Kind, id string) (Candidate, bool) {
	for _, c := range cs {
		if c.Kind == kind && c.ID == id {
			return c, true
		}
	}
	return Candidate{}, false
}

// listedAfter reports whether c is listed once it has been seen for age.
func listedAfter(c Candidate, age time.Duration) bool {
	return c.Problem != "" || (c.Limit > 0 && age >= c.Limit)
}

func TestEveryPlantedUnfinishedStatusWithoutFactsIsListed(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for _, s := range declaredValues(t, "../autonomous/models.go", "PRDStatus") {
		if finished(s) {
			continue
		}
		// No start time, no reason, no date, no session.
		cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: s}}, now, testInterval)
		c, ok := candidateFor(cs, KindAutomaton, "a1")
		if !ok || c.Problem == "" {
			t.Errorf("%s without facts: %+v ok=%v, want listed at once", s, c, ok)
		}
		// With facts but older than any limit.
		old := AutomatonFact{ID: "a1", Status: s, Since: now.Add(-40 * 24 * time.Hour), HasBlockedReason: true,
			HeldReason: true, HeldUntil: now.Add(-time.Hour)}
		cs = AutomatonCandidates([]AutomatonFact{old}, now, testInterval)
		c, ok = candidateFor(cs, KindAutomaton, "a1")
		if !ok || !listedAfter(c, now.Sub(old.Since)) {
			t.Errorf("%s, 40 days old: %+v ok=%v, want listed", s, c, ok)
		}
		if ok && (c.WaitsOn == "" || c.State == "") {
			t.Errorf("%s: says nothing about who it waits on: %+v", s, c)
		}
	}
	for _, s := range []string{"completed", "rejected", "cancelled", "archived"} {
		if cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: s}}, now, testInterval); len(cs) != 0 {
			t.Errorf("%s is finished: %v", s, cs)
		}
	}
}

func TestStatusLimits(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	want := map[string]time.Duration{
		"planning": 2 * time.Hour, "blocked": 24 * time.Hour, "needs_review": 24 * time.Hour,
		"failed": 24 * time.Hour, "paused": 24 * time.Hour, "approved": 24 * time.Hour,
		"revisions_asked": 24 * time.Hour, "draft": 7 * 24 * time.Hour,
	}
	for status, limit := range want {
		cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: status, Since: now, HasBlockedReason: true}}, now, testInterval)
		c, ok := candidateFor(cs, KindAutomaton, "a1")
		if !ok || c.Problem != "" || c.Limit != limit {
			t.Errorf("%s: %+v, want limit %v and nothing wrong yet", status, c, limit)
		}
	}
	cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: "blocked", Since: now}}, now, testInterval)
	if c, _ := candidateFor(cs, KindAutomaton, "a1"); c.Problem != "no_reason" {
		t.Errorf("blocked with no reason: %+v", c)
	}
}

func TestHeldWithAReasonAndAFutureDateIsGreen(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	held := AutomatonFact{ID: "a1", Status: "held", Since: now.Add(-20 * 24 * time.Hour), HeldReason: true, HeldUntil: now.Add(10 * 24 * time.Hour),
		Tasks: []TaskFact{{ID: "t1", Status: "failed", Since: now.Add(-20 * 24 * time.Hour)}}}
	if cs := AutomatonCandidates([]AutomatonFact{held}, now, testInterval); len(cs) != 0 {
		t.Fatalf("a held Automaton with a reason and a date: %v", cs)
	}
	for problem, change := range map[string]func(*AutomatonFact){
		"held_no_reason":      func(f *AutomatonFact) { f.HeldReason = false },
		"held_no_review_date": func(f *AutomatonFact) { f.HeldUntil = time.Time{} },
		"held_review_due":     func(f *AutomatonFact) { f.HeldUntil = now.Add(-time.Minute) },
		"held_review_too_far": func(f *AutomatonFact) { f.HeldUntil = now.Add(31 * 24 * time.Hour) },
	} {
		f := held
		change(&f)
		cs := AutomatonCandidates([]AutomatonFact{f}, now, testInterval)
		c, ok := candidateFor(cs, KindAutomaton, "a1")
		if !ok || c.Problem != problem {
			t.Errorf("%s: got %+v ok=%v", problem, c, ok)
		}
		if problem == "held_review_due" && !c.TimeDriven {
			t.Error("a review date running out is time-driven: the mass guard covers it")
		}
	}
}

func TestRunningNeedsALiveSessionOrARecordedWait(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	run := func(f AutomatonFact) []Candidate {
		f.ID, f.Status, f.Since = "a1", "running", now
		return AutomatonCandidates([]AutomatonFact{f}, now, testInterval)
	}
	if cs := run(AutomatonFact{LiveSession: true, Tasks: []TaskFact{{ID: "t1", Status: "in_progress", Since: now, LiveSession: true}}}); len(cs) != 0 {
		t.Errorf("live: %v", cs)
	}
	c, ok := candidateFor(run(AutomatonFact{}), KindAutomaton, "a1")
	if !ok || c.Invariant != InvRunning || c.Limit != 2*testInterval || c.Reason != "no_live_session" {
		t.Errorf("no session, no wait: %+v", c)
	}
	c, ok = candidateFor(run(AutomatonFact{HasWait: true}), KindAutomaton, "a1")
	if !ok || c.Limit != LimitWaiting || c.Reason != "recorded_wait" {
		t.Errorf("recorded wait: %+v", c)
	}
	cs := run(AutomatonFact{LiveSession: true, Tasks: []TaskFact{{ID: "t1", Status: "in_progress", Since: now}}})
	if c, ok := candidateFor(cs, KindTask, "a1/t1"); !ok || c.Invariant != InvRunning || c.Limit != 2*testInterval {
		t.Errorf("a running task without a session: %+v ok=%v", c, ok)
	}
	cs = run(AutomatonFact{LiveSession: true, Tasks: []TaskFact{{ID: "t1", Status: "waiting_decision", Since: now}}})
	if c, ok := candidateFor(cs, KindTask, "a1/t1"); !ok || c.Problem != "wait_without_reason" {
		t.Errorf("a wait with no reason: %+v ok=%v", c, ok)
	}
}

func TestStuckTasksAreChecked(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for _, s := range []string{"failed", "blocked", "review_needed", "awaiting_approval"} {
		cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: "blocked", Since: now, HasBlockedReason: true,
			Tasks: []TaskFact{{ID: "t1", Status: s, Since: now}}}}, now, testInterval)
		if c, ok := candidateFor(cs, KindTask, "a1/t1"); !ok || c.Limit != LimitWaiting {
			t.Errorf("task %s: %+v ok=%v", s, c, ok)
		}
	}
	for _, s := range []string{"pending", "queued", "completed", "cancelled"} {
		cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: "blocked", Since: now, HasBlockedReason: true,
			Tasks: []TaskFact{{ID: "t1", Status: s, Since: now}}}}, now, testInterval)
		if _, ok := candidateFor(cs, KindTask, "a1/t1"); ok {
			t.Errorf("task %s is covered by its Automaton", s)
		}
	}
}

func TestAParkedBugStaysOpen(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	filed := now.Add(-30 * 24 * time.Hour)
	for _, status := range []string{"held", "draft", "archived", "cancelled", "rejected", "completed"} {
		f := AutomatonFact{ID: "batch1", Status: status, Since: now, CreatedAt: filed, IsBug: true, BugIDs: []string{"d1", "d2"},
			HeldReason: true, HeldUntil: now.Add(24 * time.Hour)}
		cs := AutomatonCandidates([]AutomatonFact{f}, now, testInterval)
		for _, id := range []string{"batch1/d1", "batch1/d2"} {
			c, ok := candidateFor(cs, KindBug, id)
			if !ok || c.Limit != LimitBug || !c.Since.Equal(filed) || c.Reason != "parked_"+status {
				t.Errorf("%s: bug %s: %+v ok=%v", status, id, c, ok)
			}
		}
	}
	closed := []AutomatonFact{
		{ID: "b1", Status: "completed", Since: now, CreatedAt: filed, IsBug: true, OnMain: true},
		{ID: "b2", Status: "archived", Since: now, CreatedAt: filed, IsBug: true, OnMain: true},
		{ID: "b3", Status: "cancelled", Since: now, CreatedAt: filed, IsBug: true, BugDropped: true},
	}
	for _, c := range AutomatonCandidates(closed, now, testInterval) {
		if c.Kind == KindBug {
			t.Errorf("closed bug listed: %+v", c)
		}
	}
	// A bug with no batch is its own bug.
	cs := AutomatonCandidates([]AutomatonFact{{ID: "b4", Status: "draft", Since: now, CreatedAt: filed, IsBug: true}}, now, testInterval)
	if _, ok := candidateFor(cs, KindBug, "b4"); !ok {
		t.Errorf("single bug: %v", cs)
	}
}

func TestCompletedWorkNotOnMainIsListed(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	cs := AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: "completed", Since: now, LandingPending: true}}, now, testInterval)
	if c, ok := candidateFor(cs, KindUnlanded, "a1"); !ok || c.Limit != LimitWaiting {
		t.Fatalf("landing pending: %+v ok=%v", c, ok)
	}
}

func TestAutomatonFactsReadTheRecords(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	until := now.Add(time.Hour)
	prds := []*autonomous.PRD{
		{ID: "t1", IsTemplate: true, Status: autonomous.PRDDraft},
		{ID: "a1", Status: autonomous.PRDStatus("decomposing"), UpdatedAt: now, CreatedAt: now},
		{ID: "a2", Status: autonomous.PRDHeld, UpdatedAt: now, Hold: &autonomous.HoldState{Reason: "secret words", Until: &until, HeldAt: now}},
		{ID: "a3", Status: autonomous.PRDRunning, Kind: autonomous.PRDKindBug, UpdatedAt: now,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{{ID: "x", Status: autonomous.TaskInProgress, SessionID: "s-live", UpdatedAt: now}}}}},
		{ID: "a4", Status: autonomous.PRDCompleted, UpdatedAt: now, Landing: &autonomous.Landing{State: autonomous.LandingPending}},
		{ID: "a5", Status: autonomous.PRDArchived, UpdatedAt: now, MergedInRelease: "9.1.0"},
	}
	got := AutomatonFacts(prds, func(id string) bool { return id == "s-live" }, func(*autonomous.PRD) []string { return nil })
	by := map[string]AutomatonFact{}
	for _, f := range got {
		by[f.ID] = f
	}
	if _, ok := by["t1"]; ok || len(got) != 5 {
		t.Fatalf("facts=%v", got)
	}
	if by["a1"].Status != "planning" {
		t.Errorf("legacy status not normalised: %q", by["a1"].Status)
	}
	if !by["a2"].HeldReason || !by["a2"].HeldUntil.Equal(until) {
		t.Errorf("hold: %+v", by["a2"])
	}
	if !by["a3"].LiveSession || !by["a3"].IsBug || len(by["a3"].Tasks) != 1 || !by["a3"].Tasks[0].LiveSession {
		t.Errorf("running bug: %+v", by["a3"])
	}
	if !by["a4"].LandingPending || by["a4"].OnMain {
		t.Errorf("landing: %+v", by["a4"])
	}
	if !by["a5"].OnMain {
		t.Errorf("shipped: %+v", by["a5"])
	}
}

type reachable map[string]bool

func (r reachable) Reachable(sc session.ScheduledCommand) bool {
	if sc.SessionName != "" {
		return r["name:"+sc.SessionName]
	}
	return r[sc.SessionID]
}

func TestPendingSchedulesThatCannotFireAreReported(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	all := []session.ScheduledCommand{
		{ID: "gone", State: session.SchedPending, SessionID: "dead", CreatedAt: old},
		{ID: "input", State: session.SchedPending, SessionID: "live", CreatedAt: old},
		{ID: "named", State: session.SchedPending, SessionName: "doer", CreatedAt: old},
		{ID: "ok", State: session.SchedPending, SessionID: "live", RunAt: now.Add(time.Hour), CreatedAt: old},
		{ID: "orphan", State: session.SchedPending, SessionID: "live", RunAfterID: "nope", CreatedAt: old},
		{ID: "deadparent", State: session.SchedPending, SessionID: "live", RunAfterID: "failedone", CreatedAt: old},
		{ID: "failedone", State: session.SchedFailed, SessionID: "live", DoneAt: old, LastError: "it could not be delivered"},
		{ID: "byop", State: session.SchedCancelled, SessionID: "live", DoneAt: old},
		{ID: "bydw", State: session.SchedCancelled, SessionID: "live", DoneAt: old, LastError: "cancelled: its session was ended"},
		{ID: "done", State: session.SchedDone, SessionID: "live", DoneAt: old},
		{ID: "missed", State: session.SchedPending, SessionID: "live", RunAt: now.Add(-5*time.Hour - time.Minute), RecurEverySeconds: 3600, CreatedAt: old},
		{ID: "notrigger", State: session.SchedPending, Type: session.SchedTypeNewSession, CreatedAt: old},
		{ID: "gaveup", State: session.SchedPending, Type: session.SchedTypeSpawn, RunAt: now.Add(time.Hour), RearmState: "gave_up", CreatedAt: old},
	}
	cs := ScheduleCandidates(all, reachable{"live": true}, now, testInterval)
	check := func(id, problem, rule string) {
		t.Helper()
		c, ok := candidateFor(cs, KindSchedule, id)
		if !ok || c.Problem != problem || c.Invariant != rule {
			t.Errorf("%s: %+v ok=%v, want problem %q rule %s", id, c, ok, problem, rule)
		}
	}
	check("gone", "target_gone", InvScheduleFire)
	check("orphan", "parent_missing", InvScheduleFire)
	check("deadparent", "parent_dead", InvScheduleFire)
	check("failedone", "failed", InvFailed)
	check("bydw", "cancelled_by_datawatch", InvFailed)
	check("missed", "missed_slots", InvScheduleFire)
	check("notrigger", "no_trigger", InvScheduleFire)
	check("gaveup", "gave_up", InvScheduleFire)
	if c, _ := candidateFor(cs, KindSchedule, "missed"); c.Count != 6 {
		t.Errorf("missed slots: count=%d, want 6 in one item", c.Count)
	}
	if c, ok := candidateFor(cs, KindSchedule, "input"); !ok || c.Problem != "" || c.Limit != LimitScheduleWait {
		t.Errorf("waiting for input has a 24 h limit: %+v", c)
	}
	if c, ok := candidateFor(cs, KindSchedule, "named"); !ok || c.Invariant != InvNoHolder || c.Limit != LimitScheduleWait {
		t.Errorf("a name nobody live holds: %+v", c)
	}
	for _, id := range []string{"ok", "byop", "done"} {
		if c, ok := candidateFor(cs, KindSchedule, id); ok {
			t.Errorf("%s must not be listed: %+v", id, c)
		}
	}
}

func TestSessionsOfAutomatedWorkWaitingForInput(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	ss := []session.Session{
		{FullID: "h-1", State: session.StateWaitingInput, UpdatedAt: now, PRDID: "a1"},
		{FullID: "h-2", State: session.StateWaitingInput, UpdatedAt: now, LaunchRole: "supervisor"},
		{FullID: "h-3", State: session.StateWaitingInput, UpdatedAt: now, ScheduleName: "nightly"},
		{FullID: "h-4", State: session.StateWaitingInput, UpdatedAt: now},
		{FullID: "h-5", State: session.StateRunning, UpdatedAt: now, PRDID: "a1"},
	}
	cs := SessionCandidates(ss)
	for _, id := range []string{"h-1", "h-2", "h-3"} {
		if c, ok := candidateFor(cs, KindSession, id); !ok || c.Limit != LimitSessionWait || c.Invariant != InvRunning {
			t.Errorf("%s: %+v ok=%v", id, c, ok)
		}
	}
	if len(cs) != 3 {
		t.Errorf("only automated waiting sessions: %v", cs)
	}
}

func TestAnswersAndOneTimeMessagesThatDidNotArrive(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	prds := []*autonomous.PRD{
		{ID: "a1", Status: autonomous.PRDRunning, Story: []autonomous.Story{{Tasks: []autonomous.Task{
			{ID: "t1", DecisionResume: &autonomous.DecisionResume{QuestionID: "q1", Delivery: autonomous.DecisionReviewNeeded, ResumedAt: now}},
			{ID: "t2", DecisionResume: &autonomous.DecisionResume{QuestionID: "q2", Delivery: autonomous.DecisionNotTyped, ResumedAt: now}},
			{ID: "t3", DecisionResume: &autonomous.DecisionResume{QuestionID: "q3", Delivery: autonomous.DecisionSent, ResumedAt: now}},
			{ID: "t4", OwedDecisions: []autonomous.OwedDecision{{QuestionID: "q4"}}},
		}}}},
		{ID: "a2", Status: autonomous.PRDCancelled, Story: []autonomous.Story{{Tasks: []autonomous.Task{
			{ID: "t9", DecisionResume: &autonomous.DecisionResume{QuestionID: "q9", Delivery: autonomous.DecisionReviewNeeded}},
		}}}},
	}
	subs := []autonomous.Subscription{
		{ID: "s1", Delivery: &autonomous.OneTimeDelivery{State: autonomous.DecisionReviewNeeded, Target: "h-1", Text: "secret words", FiredAt: now}},
		{ID: "s2", Delivery: &autonomous.OneTimeDelivery{State: autonomous.DecisionWaiting, Target: "dead", Text: "secret words", FiredAt: now}},
		{ID: "s3"},
	}
	cs := AnswerCandidates(prds, subs, func(id string) bool { return id == "h-1" })
	if c, ok := candidateFor(cs, KindAnswer, "q1"); !ok || c.Problem != "review_needed" || !c.Sticky || c.Invariant != InvFailed {
		t.Errorf("q1: %+v ok=%v", c, ok)
	}
	for _, q := range []string{"q2", "q4"} {
		if c, ok := candidateFor(cs, KindAnswer, q); !ok || c.Problem != "" || c.Limit != LimitAnswerWait {
			t.Errorf("%s: %+v ok=%v", q, c, ok)
		}
	}
	for _, q := range []string{"q3", "q9"} {
		if _, ok := candidateFor(cs, KindAnswer, q); ok {
			t.Errorf("%s must not be listed", q)
		}
	}
	if c, ok := candidateFor(cs, KindMessage, "s1"); !ok || !c.Sticky {
		t.Errorf("s1: %+v ok=%v", c, ok)
	}
	if c, ok := candidateFor(cs, KindMessage, "s2"); !ok || c.Invariant != InvNoHolder {
		t.Errorf("s2: %+v ok=%v", c, ok)
	}
	assertNoContent(t, cs, "secret words")
}

func assertNoContent(t *testing.T, cs []Candidate, marker string) {
	t.Helper()
	for _, c := range cs {
		for _, v := range []string{c.ID, c.State, c.Problem, c.Reason, c.WaitsOn} {
			if v == marker {
				t.Errorf("content in %+v", c)
			}
		}
	}
}

func TestExistingListsAreRead(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	qs := []autonomous.Question{
		{ID: "q1", State: autonomous.QuestionOpen, CreatedAt: &now, Text: "secret words"},
		{ID: "q2", State: autonomous.QuestionAnswered, CreatedAt: &now},
	}
	if cs := QuestionCandidates(qs); len(cs) != 1 || cs[0].ID != "q1" || cs[0].Limit != LimitWaiting {
		t.Errorf("questions: %v", cs)
	}
	bs := []autonomous.UnlandedBranch{
		{AutomatonID: "a1", Branch: "feature/secret-words", Recommendation: autonomous.UnlandedNeedsDecision, LastActivity: now},
		{AutomatonID: "a2", Recommendation: autonomous.UnlandedLanded, LastActivity: now},
	}
	if cs := UnlandedCandidates(bs); len(cs) != 1 || cs[0].ID != "a1" || cs[0].Kind != KindUnlanded {
		t.Errorf("unlanded: %v", cs)
	}
	items := []*autonomous.PendingItem{
		{ID: "p1", State: autonomous.PendingItemWaiting, CreatedAt: now, Title: "secret words"},
		{ID: "p2", State: autonomous.PendingItemDone, CreatedAt: now},
	}
	if cs := PendingItemCandidates(items); len(cs) != 1 || cs[0].ID != "p1" || cs[0].Limit != LimitPendingItem {
		t.Errorf("pending items: %v", cs)
	}
	fs := []autonomous.OutcomeFinding{
		{Automaton: "a1", Kind: autonomous.FindingClosedNoOutcome, Detail: "secret words"},
		{Kind: autonomous.FindingDaemonOlder, Detail: "secret words"},
	}
	cs := ClosedWorkCandidates(fs, now)
	if len(cs) != 2 || cs[0].Problem == "" {
		t.Errorf("closed work: %v", cs)
	}
	assertNoContent(t, cs, "secret words")
	if cs := DeployCandidates(deploy.State{Phase: deploy.PhaseDeferred, RequestedAt: now, Reason: "secret words"}, true); len(cs) != 1 || cs[0].Problem != "deploy_deferred" {
		t.Errorf("deferred deploy: %v", cs)
	}
	if cs := DeployCandidates(deploy.State{}, false); len(cs) != 0 {
		t.Errorf("no deploy record: %v", cs)
	}
}
