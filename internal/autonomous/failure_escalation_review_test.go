package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// newReviewFixture is newFailureEscalationFixture with the global failure
// escalation policy and any other config edits set by adjust.
func newReviewFixture(t *testing.T, global FailureEscalationPolicy, adjust func(*Config)) (*Manager, *API) {
	t.Helper()
	fakeLLM := func(_ DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 8
	cfg.FailureEscalation = global
	if adjust != nil {
		adjust(&cfg)
	}
	m, err := NewManager(t.TempDir(), cfg, fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := NewAPI(m)
	drainOnCleanup(t, m, api)
	return m, api
}

// spawnRequests returns a copy of every spawn request the log recorded.
func spawnRequests(l *escalationSpawnLog) []SpawnRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]SpawnRequest(nil), l.reqs...)
}

// heldTaskOf runs prd until its one task is held for review and returns the
// held task and the spawn log of that run. The task runs MaxFailures times.
func heldTaskOf(t *testing.T, m *Manager, api *API, prd *PRD) (*Task, *escalationSpawnLog) {
	t.Helper()
	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, alwaysFailVerify)
	task := escalationTask(t, m, prd.ID)
	if task.Status != TaskReviewNeeded {
		t.Fatalf("task status = %q, want %q", task.Status, TaskReviewNeeded)
	}
	return task, &spawns
}

// TestFailureEscalation_OperatorRetry: a retry requeues the held task to
// pending, restarts the failure count, and clears the report. The next attempt
// is given the held report, and a second hold comes after another MaxFailures
// failed attempts.
func TestFailureEscalation_OperatorRetry(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, spawns := heldTaskOf(t, m, api, prd)
	if task.ReviewSummary == nil {
		t.Fatal("held task has no ReviewSummary")
	}
	held := task.ReviewSummary.Text
	first := len(spawns.workers())

	if _, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionRetry}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview retry: %v", err)
	}
	task = escalationTask(t, m, prd.ID)
	if normTaskStatus(task.Status) != TaskPending {
		t.Fatalf("after retry status = %q, want %q", task.Status, TaskPending)
	}
	if task.EscalationFailures != 0 {
		t.Fatalf("EscalationFailures after retry = %d, want 0", task.EscalationFailures)
	}
	if task.ReviewSummary != nil {
		t.Fatal("ReviewSummary survived the retry")
	}
	if !historyHasNote(task, held) {
		t.Fatalf("History does not keep the cleared report: %+v", task.History)
	}

	pollEscalationSettled(t, m, prd.ID, 10*time.Second)
	if n := len(spawns.workers()) - first; n != defaultFailureEscalationMaxFailures {
		t.Fatalf("attempts after retry = %d, want %d before the second hold", n, defaultFailureEscalationMaxFailures)
	}
	task = escalationTask(t, m, prd.ID)
	if task.Status != TaskReviewNeeded {
		t.Fatalf("second hold status = %q, want %q", task.Status, TaskReviewNeeded)
	}
	if task.ReviewSummary == nil || task.ReviewSummary.FailureCount != defaultFailureEscalationMaxFailures {
		t.Fatalf("second hold ReviewSummary = %+v, want FailureCount %d", task.ReviewSummary, defaultFailureEscalationMaxFailures)
	}

	reqs := spawnRequests(spawns)
	if len(reqs) <= first {
		t.Fatalf("no spawn after the retry: %d spawns in total", len(reqs))
	}
	if !strings.Contains(reqs[first].RetryHint, held) {
		t.Fatalf("first spawn after retry does not carry the held report:\n%s", reqs[first].RetryHint)
	}
	if !prdHasDecisionKind(m, prd.ID, reviewDecisionKind) {
		t.Fatal("retry left no review_resolved decision")
	}
}

// TestFailureEscalation_OperatorChangeSpec: a retry with a new spec runs the
// new spec, and keeps the previous spec in the task's History.
func TestFailureEscalation_OperatorChangeSpec(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, spawns := heldTaskOf(t, m, api, prd)
	first := len(spawns.workers())
	oldSpec := task.Spec

	const newSpec = "parse the tokens with the new grammar"
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionRetry, Spec: newSpec}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview retry with spec: %v", err)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)

	task = escalationTask(t, m, prd.ID)
	if task.Spec != newSpec {
		t.Fatalf("task spec = %q, want %q", task.Spec, newSpec)
	}
	if !historyHasNote(task, oldSpec) {
		t.Fatalf("History does not keep the previous spec %q: %+v", oldSpec, task.History)
	}
	reqs := spawnRequests(spawns)
	if len(reqs) <= first {
		t.Fatal("no spawn after the spec change")
	}
	if !strings.Contains(reqs[first].Spec, newSpec) || strings.Contains(reqs[first].Spec, oldSpec) {
		t.Fatalf("spawn after the spec change ran spec %q, want the new spec only", reqs[first].Spec)
	}
}

// TestFailureEscalation_OperatorChangeWorker: a retry with a new worker runs the
// next attempt on it. An invalid pair is refused, and the task stays held with
// nothing spawned.
func TestFailureEscalation_OperatorChangeWorker(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, spawns := heldTaskOf(t, m, api, prd)
	before := len(spawns.workers())

	// A bare Claude alias cannot run on the pi family: the pair is refused.
	bad := ReviewResolution{Action: reviewActionRetry, Backend: escPiBackend, Model: escClaudeModel}
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, bad, "operator-test"); err == nil {
		t.Fatal("ResolveTaskReview accepted an invalid backend and model pair")
	}
	task = escalationTask(t, m, prd.ID)
	if task.Status != TaskReviewNeeded {
		t.Fatalf("after refused pair status = %q, want %q", task.Status, TaskReviewNeeded)
	}
	if task.Backend != "" || task.Model != "" {
		t.Fatalf("refused pair changed the task worker to %q/%q", task.Backend, task.Model)
	}
	if n := len(spawns.workers()); n != before {
		t.Fatalf("refused retry spawned %d attempts, want none", n-before)
	}

	good := ReviewResolution{Action: reviewActionRetry, Backend: escClaudeBackend, Model: escClaudeModel, Effort: EffortNormal}
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, good, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview retry with worker: %v", err)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)
	reqs := spawnRequests(spawns)
	if len(reqs) <= before {
		t.Fatal("no spawn after the worker change")
	}
	next := reqs[before]
	if next.Backend != escClaudeBackend || next.Model != escClaudeModel {
		t.Fatalf("next spawn worker = %s/%s, want %s/%s", next.Backend, next.Model, escClaudeBackend, escClaudeModel)
	}
}

// TestFailureEscalation_OperatorNarrowScope: a retry with fewer planned files
// stores them on the task. A file the Automaton may not write is refused, and
// the task stays held with its files unchanged.
func TestFailureEscalation_OperatorNarrowScope(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.Story[0].Tasks[0].FilesPlanned = []string{"internal/a.go", "internal/b.go", "internal/c.go"}
	})
	task, spawns := heldTaskOf(t, m, api, prd)
	before := len(spawns.workers())

	outside := ReviewResolution{Action: reviewActionRetry, Files: []string{"/etc/outside-the-project"}}
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, outside, "operator-test"); err == nil || !strings.Contains(err.Error(), "scope:") {
		t.Fatalf("out-of-scope file: err = %v, want a scope refusal", err)
	}
	task = escalationTask(t, m, prd.ID)
	if task.Status != TaskReviewNeeded || len(task.FilesPlanned) != 3 {
		t.Fatalf("after refused scope: status %q, files %v; want held with 3 files", task.Status, task.FilesPlanned)
	}

	narrow := ReviewResolution{Action: reviewActionRetry, Files: []string{"internal/a.go"}}
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, narrow, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview narrow scope: %v", err)
	}
	task = escalationTask(t, m, prd.ID)
	if len(task.FilesPlanned) != 1 || task.FilesPlanned[0] != "internal/a.go" {
		t.Fatalf("FilesPlanned = %v, want [internal/a.go]", task.FilesPlanned)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)
	if n := len(spawns.workers()); n <= before {
		t.Fatal("no attempt ran after the scope was narrowed")
	}
}

// TestFailureEscalation_OperatorCancelRollsUp: cancelling the only held task
// reopens the Automaton and lets it roll up. A cancelled task is not a failure,
// so the Automaton completes.
func TestFailureEscalation_OperatorCancelRollsUp(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, _ := heldTaskOf(t, m, api, prd)

	if _, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionCancel, Note: "not worth another try"}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview cancel: %v", err)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)

	if got := escalationTask(t, m, prd.ID); got.Status != TaskCancelled {
		t.Fatalf("task status = %q, want %q", got.Status, TaskCancelled)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("Automaton status = %q, want %q after the held task was cancelled", got.Status, PRDCompleted)
	}
}

// TestFailureEscalation_ResolveNotHeldIsRejected: a task that is not held for
// review is refused with ErrTaskNotInReview, whatever the action. An unknown
// action is refused and the error names the valid actions.
func TestFailureEscalation_ResolveNotHeldIsRejected(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task := escalationTask(t, m, prd.ID)

	for _, action := range []string{reviewActionRetry, reviewActionCancel} {
		_, err := m.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: action}, "operator-test")
		if !errors.Is(err, ErrTaskNotInReview) {
			t.Fatalf("%s on a pending task: err = %v, want ErrTaskNotInReview", action, err)
		}
	}

	_, err := m.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: "pause"}, "operator-test")
	if err == nil || errors.Is(err, ErrTaskNotInReview) {
		t.Fatalf("unknown action: err = %v, want a validation error", err)
	}
	for _, want := range []string{reviewActionRetry, reviewActionCancel} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unknown action error %q does not name %q", err, want)
		}
	}
}

// TestFailureEscalation_SetOverrideAutomatonAndTask: the override is set at
// Automaton and task level, each shows its source, and clearing restores the
// level below. Every change is recorded as a decision.
func TestFailureEscalation_SetOverrideAutomatonAndTask(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	taskID := escalationTask(t, m, prd.ID).ID

	sourceOf := func() (int, string) {
		t.Helper()
		cur, _ := m.Store().GetPRD(prd.ID)
		task := escalationTask(t, m, prd.ID)
		eff := m.resolveFailureEscalation(cur, task)
		return eff.MaxFailures, eff.Sources.MaxFailures
	}

	if _, err := m.SetFailureEscalation(prd.ID, "", &FailureEscalationPolicy{MaxFailures: 4}, "operator-test"); err != nil {
		t.Fatalf("set Automaton override: %v", err)
	}
	if n, src := sourceOf(); n != 4 || src != escalationSourcePRD {
		t.Fatalf("with Automaton override: max %d from %q, want 4 from prd", n, src)
	}

	if _, err := m.SetFailureEscalation(prd.ID, taskID, &FailureEscalationPolicy{MaxFailures: 5}, "operator-test"); err != nil {
		t.Fatalf("set task override: %v", err)
	}
	if n, src := sourceOf(); n != 5 || src != escalationSourceTask {
		t.Fatalf("with task override: max %d from %q, want 5 from task", n, src)
	}

	if _, err := m.SetFailureEscalation(prd.ID, taskID, nil, "operator-test"); err != nil {
		t.Fatalf("clear task override: %v", err)
	}
	if n, src := sourceOf(); n != 4 || src != escalationSourcePRD {
		t.Fatalf("after clearing task override: max %d from %q, want 4 from prd", n, src)
	}

	if _, err := m.SetFailureEscalation(prd.ID, "", nil, "operator-test"); err != nil {
		t.Fatalf("clear Automaton override: %v", err)
	}
	if n, src := sourceOf(); n != defaultFailureEscalationMaxFailures || src != escalationSourceGlobal {
		t.Fatalf("after clearing Automaton override: max %d from %q, want %d from global", n, src, defaultFailureEscalationMaxFailures)
	}

	if _, err := m.SetFailureEscalation(prd.ID, "no-such-task", &FailureEscalationPolicy{MaxFailures: 2}, "operator-test"); err == nil {
		t.Fatal("override on an unknown task was accepted")
	}
	if _, err := m.SetFailureEscalation(prd.ID, "", &FailureEscalationPolicy{MaxFailures: -1}, "operator-test"); err == nil {
		t.Fatal("negative max_failures was accepted")
	}

	cur, _ := m.Store().GetPRD(prd.ID)
	n := 0
	for _, d := range cur.Decisions {
		if d.Kind == failureEscalationSetKind {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("failure_escalation_set decisions = %d, want 4", n)
	}
}

// TestFailureEscalation_OverrideOrderEndToEnd: with the global default at 3,
// an Automaton override at 4 and a task override at 5, each run holds the task
// after exactly the count of the most specific level that sets it.
func TestFailureEscalation_OverrideOrderEndToEnd(t *testing.T) {
	cases := []struct {
		name      string
		prd       *FailureEscalationPolicy
		task      *FailureEscalationPolicy
		wantHolds int
	}{
		{name: "global_only", wantHolds: 3},
		{name: "automaton_beats_global", prd: &FailureEscalationPolicy{MaxFailures: 4}, wantHolds: 4},
		{name: "task_beats_automaton", prd: &FailureEscalationPolicy{MaxFailures: 4}, task: &FailureEscalationPolicy{MaxFailures: 5}, wantHolds: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, api := newReviewFixture(t, FailureEscalationPolicy{MaxFailures: 3}, nil)
			prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
				p.FailureEscalation = tc.prd
				p.Story[0].Tasks[0].FailureEscalation = tc.task
			})
			_, spawns := heldTaskOf(t, m, api, prd)
			if n := len(spawns.workers()); n != tc.wantHolds {
				t.Fatalf("attempts before the hold = %d, want %d", n, tc.wantHolds)
			}
		})
	}
}

// TestFailureEscalation_SetOverrideRespectsAcceptedStatuses: an override is
// refused while the Automaton is in a status that does not accept overrides,
// and the list in autonomous.task_override_statuses is what decides it.
func TestFailureEscalation_SetOverrideRespectsAcceptedStatuses(t *testing.T) {
	t.Run("running_refused_by_default", func(t *testing.T) {
		m, _ := newReviewFixture(t, FailureEscalationPolicy{}, nil)
		prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
		prd.Status = PRDRunning
		if err := m.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
		_, err := m.SetFailureEscalation(prd.ID, "", &FailureEscalationPolicy{MaxFailures: 4}, "operator-test")
		if err == nil || !strings.Contains(err.Error(), "locked") {
			t.Fatalf("override while running: err = %v, want a locked refusal", err)
		}
		if got, _ := m.Store().GetPRD(prd.ID); got.FailureEscalation != nil {
			t.Fatalf("refused override was stored: %+v", got.FailureEscalation)
		}
	})

	t.Run("blocked_refused_when_list_narrowed", func(t *testing.T) {
		m, api := newReviewFixture(t, FailureEscalationPolicy{}, func(c *Config) {
			c.TaskOverrideStatuses = []string{string(PRDNeedsReview)}
		})
		prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
		task, _ := heldTaskOf(t, m, api, prd)
		_, err := m.SetFailureEscalation(prd.ID, task.ID, &FailureEscalationPolicy{MaxFailures: 4}, "operator-test")
		if err == nil || !strings.Contains(err.Error(), "task_override_statuses") {
			t.Fatalf("task override on a blocked Automaton: err = %v, want a task_override_statuses refusal", err)
		}
		if got := escalationTask(t, m, prd.ID); got.FailureEscalation != nil {
			t.Fatalf("refused task override was stored: %+v", got.FailureEscalation)
		}
	})

	t.Run("invalid_policy_refused_before_status", func(t *testing.T) {
		m, _ := newFailureEscalationFixture(t, t.TempDir())
		prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
		_, err := m.SetFailureEscalation(prd.ID, "", &FailureEscalationPolicy{MaxFailures: -2}, "operator-test")
		if err == nil || !strings.Contains(err.Error(), "max_failures") {
			t.Fatalf("invalid policy: err = %v, want a max_failures error", err)
		}
	})
}

// historyHasNote reports whether one of the task's History notes contains text.
func historyHasNote(task *Task, text string) bool {
	for _, h := range task.History {
		if strings.Contains(h.Note, text) {
			return true
		}
	}
	return false
}

// prdHasDecisionKind reports whether the Automaton's decision log holds one entry of
// the given kind.
func prdHasDecisionKind(m *Manager, prdID, kind string) bool {
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		return false
	}
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			return true
		}
	}
	return false
}
