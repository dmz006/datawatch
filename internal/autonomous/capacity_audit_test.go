package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
)

// auditCapture is an in-memory audit sink on a fake clock.
type auditCapture struct {
	mu      sync.Mutex
	now     time.Time
	entries []audit.Entry
}

func newAuditCapture() *auditCapture {
	return &auditCapture{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
}

func (c *auditCapture) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *auditCapture) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *auditCapture) sink(e audit.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, e)
}

func (c *auditCapture) recorder() *CapacityAudit {
	return &CapacityAudit{Clock: c.Now, Sink: c.sink}
}

func (c *auditCapture) all() []audit.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]audit.Entry(nil), c.entries...)
}

// only fails the test unless exactly one entry was captured, and returns it.
func (c *auditCapture) only(t *testing.T) audit.Entry {
	t.Helper()
	es := c.all()
	if len(es) != 1 {
		t.Fatalf("want exactly 1 audit entry, got %d: %+v", len(es), es)
	}
	return es[0]
}

func wantEntry(t *testing.T, e audit.Entry, actor, action, subject string, ts time.Time) {
	t.Helper()
	if e.Actor != actor || e.Action != action || e.Subject != subject {
		t.Fatalf("entry = actor %q action %q subject %q, want %q %q %q", e.Actor, e.Action, e.Subject, actor, action, subject)
	}
	if !e.Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v, want %v", e.Timestamp, ts)
	}
}

func TestCapacityAudit_AdmissionWaitAndRelease(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]

	if err := led.Acquire(context.Background(), capNodeReq(holder.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- m.admit(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker)
	}()
	capWaitUntil(t, func() bool { return len(ac.all()) == 1 })

	waitAt := ac.Now()
	wait := ac.all()[0]
	wantEntry(t, wait, "autonomous", audit.ActionCapacityWait, queued.ID, waitAt)
	if wait.Details["prd_id"] != prd.ID {
		t.Errorf("prd_id = %v, want %s", wait.Details["prd_id"], prd.ID)
	}
	if wait.Details["cause"] != string(WaitCauseNodeBusy) {
		t.Errorf("cause = %v, want %s", wait.Details["cause"], WaitCauseNodeBusy)
	}
	if reason, _ := wait.Details["reason"].(string); ClassifyWaitReason(reason) != WaitCauseNodeBusy {
		t.Errorf("reason = %q, want a node-busy reason", reason)
	}
	if pools, _ := wait.Details["pools"].([]string); len(pools) != 1 || pools[0] != "node:n1" {
		t.Errorf("pools = %v, want [node:n1]", wait.Details["pools"])
	}
	// The readable keys sit beside the existing ones, never replacing them.
	if wait.Details["task_id"] != queued.ID {
		t.Errorf("task_id = %v, want %s", wait.Details["task_id"], queued.ID)
	}
	if want, got := m.ResolveHolder(queued.ID).TaskTitle, wait.Details["task_title"].(string); want != "" && got != want {
		t.Errorf("task_title = %q, want %q", got, want)
	}
	if want, got := " held by "+m.ResolveHolder(holder.ID).Full, wait.Details["readable"].(string); !strings.HasSuffix(got, want) {
		t.Errorf("readable = %q, want a sentence ending in %q", got, want)
	} else if !strings.Contains(got, "waiting for node n1: 1/1 engines in use") {
		t.Errorf("readable = %q, want the node-busy wording", got)
	}

	ac.Advance(90 * time.Second)
	led.Release(holder.ID)
	if err := <-done; err != nil {
		t.Fatalf("admit: %v", err)
	}
	es := ac.all()
	if len(es) != 2 {
		t.Fatalf("want a wait then an admitted entry, got %+v", es)
	}
	wantEntry(t, es[1], "autonomous", audit.ActionCapacityAdmitted, queued.ID, waitAt.Add(90*time.Second))
	if es[1].Details["waited_seconds"] != 90.0 {
		t.Errorf("waited_seconds = %v, want 90", es[1].Details["waited_seconds"])
	}
	if pools, _ := es[1].Details["pools"].([]string); len(pools) != 1 || pools[0] != "node:n1" {
		t.Errorf("pools = %v, want [node:n1]", es[1].Details["pools"])
	}
	if es[1].Details["task_id"] != queued.ID {
		t.Errorf("admitted task_id = %v, want %s", es[1].Details["task_id"], queued.ID)
	}
	// The admission is not waiting, so its reason is not re-worded.
	if got, _ := es[1].Details["readable"].(string); got != "" {
		t.Errorf("admitted readable = %q, want \"\"", got)
	}
}

// A task that is admitted at once never waited, so nothing is recorded.
func TestCapacityAudit_ImmediateAdmissionRecordsNothing(t *testing.T) {
	m, _, _ := capFixtureFakeClock(t, 1)
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	if err := m.admit(context.Background(), cur, &task, "opencode", "", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if es := ac.all(); len(es) != 0 {
		t.Fatalf("an immediate admission recorded %+v", es)
	}
}

func TestCapacityAudit_BackoffSetAndClear(t *testing.T) {
	ac := newAuditCapture()
	rec := ac.recorder()
	until := time.Date(2026, 10, 22, 15, 30, 0, 0, time.UTC)

	rec.BackoffSet(AuditActorAutonomous, "claude-code", until, BackoffSourceRateLimit)
	es := ac.all()
	wantEntry(t, es[0], "autonomous", audit.ActionUsageBackoffSet, "claude-code", ac.Now())
	if es[0].Details["backend"] != "claude-code" || es[0].Details["until"] != "2026-10-22T15:30:00Z" || es[0].Details["source"] != BackoffSourceRateLimit {
		t.Errorf("rate-limit set details = %v", es[0].Details)
	}

	ac.Advance(time.Minute)
	rec.BackoffSet(AuditActorOperator, "opencode", until, BackoffSourceOperator)
	es = ac.all()
	wantEntry(t, es[1], "operator", audit.ActionUsageBackoffSet, "opencode", ac.Now())
	if es[1].Details["source"] != BackoffSourceOperator {
		t.Errorf("operator set source = %v", es[1].Details["source"])
	}

	rec.BackoffCleared(AuditActorOperator, "all")
	es = ac.all()
	wantEntry(t, es[2], "operator", audit.ActionUsageBackoffCleared, "all", ac.Now())
	if es[2].Details["backend"] != "all" {
		t.Errorf("cleared details = %v", es[2].Details)
	}

	rec.BackoffAutoCleared("claude-code", until)
	es = ac.all()
	wantEntry(t, es[3], "autonomous", audit.ActionUsageBackoffAutoCleared, "claude-code", ac.Now())
	if es[3].Details["backend"] != "claude-code" || es[3].Details["until"] != "2026-10-22T15:30:00Z" {
		t.Errorf("auto-cleared details = %v", es[3].Details)
	}
}

func TestCapacityAudit_BudgetBreach(t *testing.T) {
	ac := newAuditCapture()
	rec := ac.recorder()
	rolls := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	rec.BudgetBreach("claude-code", 25.5, 25, &rolls)
	e := ac.only(t)
	wantEntry(t, e, "autonomous", audit.ActionBudgetCeilingWait, "claude-code", ac.Now())
	if e.Details["llm"] != "claude-code" || e.Details["spend_usd"] != 25.5 || e.Details["ceiling_usd"] != 25.0 || e.Details["rolls_over_at"] != "2026-10-26T09:00:00Z" {
		t.Errorf("breach details = %v", e.Details)
	}

	ac.Advance(time.Hour)
	rec.BudgetBreach("opencode", 3, 3, nil)
	e = ac.all()[1]
	if _, ok := e.Details["rolls_over_at"]; ok {
		t.Errorf("rolls_over_at present with no known rollover: %v", e.Details)
	}

	rec.BudgetResumed("claude-code", 1, 25)
	e = ac.all()[2]
	wantEntry(t, e, "autonomous", audit.ActionBudgetWindowResumed, "claude-code", ac.Now())
	if e.Details["spend_usd"] != 1.0 || e.Details["ceiling_usd"] != 25.0 {
		t.Errorf("resumed details = %v", e.Details)
	}
}

func TestCapacityAudit_BudgetWindowRollover(t *testing.T) {
	ac := newAuditCapture()
	w := &BudgetRolloverWatcher{Audit: ac.recorder()}
	start1 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	start2 := start1.AddDate(0, 0, 7)
	weekly := func(start time.Time, spend, peer float64) BudgetStatus {
		return BudgetStatus{LLM: "claude-code", CeilingUSD: 25, SpendUSD: spend, PeerSpendUSD: peer, Window: "weekly", WindowStart: start, WindowEnd: start.AddDate(0, 0, 7)}
	}

	w.Observe([]BudgetStatus{weekly(start1, 10, 0)})
	w.Observe([]BudgetStatus{weekly(start1, 24, 1.5)})
	if es := ac.all(); len(es) != 0 {
		t.Fatalf("no boundary passed, but recorded %+v", es)
	}

	ac.Advance(7 * 24 * time.Hour)
	w.Observe([]BudgetStatus{weekly(start2, 0, 0)})
	e := ac.only(t)
	wantEntry(t, e, "autonomous", audit.ActionBudgetWindowRollover, "claude-code", ac.Now())
	if e.Details["previous_spend_usd"] != 25.5 || e.Details["ceiling_usd"] != 25.0 || e.Details["new_window_start"] != "2026-10-26T09:00:00Z" {
		t.Errorf("rollover details = %v", e.Details)
	}

	w.Observe([]BudgetStatus{weekly(start2, 1, 0)})
	if n := len(ac.all()); n != 1 {
		t.Fatalf("the same window recorded again: %d entries", n)
	}

	// A rolling window slides every check and has no boundary to record.
	rolling := BudgetStatus{LLM: "opencode", CeilingUSD: 5, Window: "rolling", WindowStart: ac.Now()}
	w.Observe([]BudgetStatus{rolling})
	ac.Advance(time.Minute)
	rolling.WindowStart = ac.Now()
	w.Observe([]BudgetStatus{rolling})
	if n := len(ac.all()); n != 1 {
		t.Fatalf("a rolling window recorded a rollover: %d entries", n)
	}
}

func TestCapacityAudit_NilRecorderAndSinkDropEvents(t *testing.T) {
	var nilRec *CapacityAudit
	nilRec.BackoffCleared("operator", "x") // must not panic
	(&CapacityAudit{}).AdmissionWait("t", "p", WaitCauseNodeBusy, "r", nil)
}

// A standalone recorder (no Names wired) leaves the readable names at their
// "(untitled <short id>)" fallbacks.
func TestCapacityAudit_AdmissionWaitReadableFallbacks(t *testing.T) {
	ac := newAuditCapture()
	rec := ac.recorder()
	rec.AdmissionWait("ab12cd34ef56aabbccdd", "pr99zz77aa00b1c2d3", WaitCauseNodeBusy,
		NodeBusyWaitText("n1", 1, 1, []string{"ab12cd34"}), []string{"node:n1"})
	e := ac.only(t)
	wantEntry(t, e, "autonomous", audit.ActionCapacityWait, "ab12cd34ef56aabbccdd", ac.Now())
	if e.Details["task_title"] != "(untitled ab12cd34)" {
		t.Errorf("task_title = %v, want %q", e.Details["task_title"], "(untitled ab12cd34)")
	}
	if e.Details["prd_title"] != "(untitled pr99zz77)" {
		t.Errorf("prd_title = %v, want %q", e.Details["prd_title"], "(untitled pr99zz77)")
	}
	if got, _ := e.Details["readable"].(string); got != "" {
		t.Errorf("readable = %q, want \"\" with no resolver", got)
	}

	// With a Names function wired, its values pass through untouched.
	ac = newAuditCapture()
	rec = ac.recorder()
	rec.Names = func(taskID, prdID, reason string) AdmissionNames {
		if reason == "" {
			return AdmissionNames{TaskTitle: "T1", PRDTitle: "s"}
		}
		return AdmissionNames{TaskTitle: "T1", PRDTitle: "s",
			Readable: "waiting for node n1: 1/1 engines in use, held by s / T0 (c3e70011)"}
	}
	rec.AdmissionWait("ab12cd34", "pr99zz77", WaitCauseNodeBusy, NodeBusyWaitText("n1", 1, 1, []string{"x"}), nil)
	e = ac.all()[0]
	if e.Details["task_title"] != "T1" || e.Details["prd_title"] != "s" {
		t.Errorf("names = %v %v, want T1 s", e.Details["task_title"], e.Details["prd_title"])
	}
	if got := e.Details["readable"].(string); got != "waiting for node n1: 1/1 engines in use, held by s / T0 (c3e70011)" {
		t.Errorf("readable = %q, want the resolved sentence", got)
	}
	rec.AdmissionAdmitted("ab12cd34", ac.Now().Add(-30*time.Second), nil)
	e = ac.all()[1]
	if e.Details["task_title"] != "T1" {
		t.Errorf("admitted task_title = %v, want T1", e.Details["task_title"])
	}
	if got, _ := e.Details["readable"].(string); got != "" {
		t.Errorf("admitted readable = %q, want \"\" with no wait reason", got)
	}
}

// Manager.AdmissionNames resolves through the shared holder-title resolver,
// so an admission entry's readable sentence names the real holder task.
func TestManager_AdmissionNamesResolvesHolders(t *testing.T) {
	m, _, _ := capFixtureFakeClock(t, 1)
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holderID, queuedID := cur.Story[0].Tasks[0].ID, cur.Story[0].Tasks[1].ID

	names := m.AdmissionNames(queuedID, prd.ID, "")
	if names.TaskTitle != "T1" {
		t.Errorf("TaskTitle = %q, want T1", names.TaskTitle)
	}
	if names.PRDTitle != cur.Title {
		t.Errorf("PRDTitle = %q, want %q", names.PRDTitle, cur.Title)
	}
	if names.Readable != "" {
		t.Errorf("Readable = %q, want \"\" with no reason", names.Readable)
	}

	reason := NodeBusyWaitText("n1", 1, 1, []string{holderID})
	names = m.AdmissionNames(queuedID, prd.ID, reason)
	want := m.ReadableWaitSentence(reason)
	if want == "" {
		t.Fatal("want a readable sentence, got \"\"")
	}
	if names.Readable != want {
		t.Errorf("readable = %q, want %q", names.Readable, want)
	}
	if !strings.Contains(want, "held by "+m.ResolveHolder(holderID).Full) {
		t.Errorf("readable = %q, want the holder named %q", want, m.ResolveHolder(holderID).Full)
	}
}
