package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type slMemStore struct {
	mu   sync.Mutex
	recs []supervisorloop.RunRecord
}

func (s *slMemStore) Append(r supervisorloop.RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, r)
	return nil
}

func (s *slMemStore) List() ([]supervisorloop.RunRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]supervisorloop.RunRecord(nil), s.recs...), nil
}

func (s *slMemStore) byJob(name string) []supervisorloop.RunRecord {
	recs, _ := s.List()
	var out []supervisorloop.RunRecord
	for _, r := range recs {
		if r.Job == name {
			out = append(out, r)
		}
	}
	return out
}

type slClock struct{}

func (slClock) Now() time.Time { return time.Unix(1_700_000_000, 0) }

func newStepsRig(t *testing.T, hooks SupervisorLoopHooks) (*coordHarness, *SupervisorLoopSteps, *slMemStore) {
	t.Helper()
	h := newCoordHarness(t, 1)
	store := &slMemStore{}
	eng := &supervisorloop.Engine{Settings: supervisorloop.DefaultSettings(), Clock: slClock{}, Store: store}
	return h, NewSupervisorLoopSteps(h.c, eng, hooks), store
}

func resultFor(t *testing.T, rs []SupervisorLoopStepResult, step string) SupervisorLoopStepResult {
	t.Helper()
	for _, r := range rs {
		if r.Step == step {
			return r
		}
	}
	t.Fatalf("no result for step %s in %+v", step, rs)
	return SupervisorLoopStepResult{}
}

func TestSupervisorLoopStepEnabledAndDisabled(t *testing.T) {
	set := supervisorloop.DefaultSettings()
	if err := set.Set("supervisor_loop.cleanup_sweep.enabled", false); err != nil {
		t.Fatal(err)
	}
	h, steps, store := newStepsRig(t, SupervisorLoopHooks{Settings: func() supervisorloop.Settings { return set }})
	h.setStatus(0, PRDCompleted)

	rs := steps.RunPass(context.Background())
	if len(rs) != len(supervisorloop.StepNames) {
		t.Fatalf("got %d results, want %d", len(rs), len(supervisorloop.StepNames))
	}
	if r := resultFor(t, rs, stepCleanupSweep); r.Ran {
		t.Errorf("disabled step ran: %+v", r)
	}
	if n := len(store.byJob(stepCleanupSweep)); n != 0 {
		t.Errorf("disabled step wrote %d run records, want 0", n)
	}
	for _, name := range supervisorloop.StepNames {
		if name == stepCleanupSweep {
			continue
		}
		r := resultFor(t, rs, name)
		if !r.Ran || r.Err != nil {
			t.Errorf("step %s: %+v", name, r)
		}
		if n := len(store.byJob(name)); n != 1 {
			t.Errorf("step %s wrote %d run records, want 1", name, n)
		}
	}
	if got := store.byJob(stepDeployAfterShip)[0].Detail; got != stepNotConfigured {
		t.Errorf("deploy detail = %q, want %q", got, stepNotConfigured)
	}
	// close_finished_records archived the completed record.
	if p, _ := h.m.Store().GetPRD(h.prds[0].ID); p.Status != PRDArchived {
		t.Errorf("completed record status = %s, want archived", p.Status)
	}

	// The master switch turns the whole pass off.
	if err := set.Set(supervisorloop.KeyEnabled, false); err != nil {
		t.Fatal(err)
	}
	if rs := steps.RunPass(context.Background()); len(rs) != 0 {
		t.Errorf("master switch off still ran %d steps", len(rs))
	}
}

func TestSupervisorLoopStepErrorRecordedNotRetriedInPass(t *testing.T) {
	var calls int32
	h, steps, store := newStepsRig(t, SupervisorLoopHooks{
		MergeUntilConflict: func(context.Context) (SupervisorMergeResult, error) {
			atomic.AddInt32(&calls, 1)
			return SupervisorMergeResult{}, errors.New("pipeline down")
		},
	})
	_ = h
	rs := steps.RunPass(context.Background())
	r := resultFor(t, rs, stepRunMergeUntilConflict)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "pipeline down") {
		t.Fatalf("error not returned: %+v", r)
	}
	if calls != 1 {
		t.Errorf("failing step ran %d times in one pass, want 1", calls)
	}
	rec := store.byJob(stepRunMergeUntilConflict)
	if len(rec) != 1 || rec[0].Outcome != supervisorloop.OutcomeError || !strings.Contains(rec[0].Detail, "pipeline down") {
		t.Errorf("run record = %+v", rec)
	}
	// Other steps still ran.
	if r := resultFor(t, rs, stepApplyAnswers); !r.Ran {
		t.Error("a failing step stopped the pass")
	}
}

type fakeLanes struct {
	lo, hi, cur, want int
	set               []int
	err               error
}

func (f *fakeLanes) Current() int { return f.cur }
func (f *fakeLanes) Wanted() int  { return f.want }
func (f *fakeLanes) SetCount(n int) error {
	f.set = append(f.set, n)
	if f.err == nil {
		f.cur = n
	}
	return f.err
}

func TestSupervisorLoopStepLaneBoundClamp(t *testing.T) {
	cases := []struct {
		name        string
		lanes       fakeLanes
		wantSet     []int
		wantErr     bool
		wantContain string
	}{
		{"above max clamps to max", fakeLanes{lo: 1, hi: 4, cur: 2, want: 9}, []int{4}, false, "clamped"},
		{"below min clamps to min", fakeLanes{lo: 1, hi: 4, cur: 3, want: 0}, []int{1}, false, "clamped"},
		{"inside bounds moves exactly", fakeLanes{lo: 1, hi: 4, cur: 2, want: 3}, []int{3}, false, "2 -> 3"},
		{"already at target does nothing", fakeLanes{lo: 1, hi: 4, cur: 4, want: 9}, nil, false, "stays 4"},
		{"invalid bounds refuse", fakeLanes{lo: 5, hi: 2, cur: 3, want: 3}, nil, true, ""},
		{"set failure is an error", fakeLanes{lo: 1, hi: 4, cur: 2, want: 3, err: errors.New("busy")}, []int{3}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lanes := tc.lanes
			set := supervisorloop.DefaultSettings()
			set.LaneMin, set.LaneMax = lanes.lo, lanes.hi
			_, steps, store := newStepsRig(t, SupervisorLoopHooks{Lanes: &lanes, Settings: func() supervisorloop.Settings { return set }})
			rs := steps.RunPass(context.Background())
			r := resultFor(t, rs, stepLaneCount)
			if (r.Err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", r.Err, tc.wantErr)
			}
			if len(lanes.set) != len(tc.wantSet) || (len(tc.wantSet) > 0 && lanes.set[0] != tc.wantSet[0]) {
				t.Errorf("SetCount calls = %v, want %v", lanes.set, tc.wantSet)
			}
			for _, n := range lanes.set {
				if n < lanes.lo || n > lanes.hi {
					t.Errorf("set count %d outside bounds %d..%d", n, lanes.lo, lanes.hi)
				}
			}
			if tc.wantContain != "" && !strings.Contains(r.Detail, tc.wantContain) {
				t.Errorf("detail %q lacks %q", r.Detail, tc.wantContain)
			}
			if len(store.byJob(stepLaneCount)) != 1 {
				t.Error("lane step wrote no run record")
			}
		})
	}
	_, steps, _ := newStepsRig(t, SupervisorLoopHooks{})
	if r := resultFor(t, steps.RunPass(context.Background()), stepLaneCount); r.Detail != stepNotConfigured {
		t.Errorf("no lane control: detail = %q", r.Detail)
	}
}

func TestSupervisorLoopStepLaneNoBoundsConfigured(t *testing.T) {
	lanes := &fakeLanes{cur: 2, want: 9}
	_, steps, _ := newStepsRig(t, SupervisorLoopHooks{Lanes: lanes})
	r := resultFor(t, steps.RunPass(context.Background()), stepLaneCount)
	if !strings.HasPrefix(r.Detail, stepNotConfigured) || len(lanes.set) != 0 {
		t.Errorf("detail %q, set %v: want not configured and no move", r.Detail, lanes.set)
	}
}

func TestSupervisorLoopStepEscalationBodyKeepsUTF8(t *testing.T) {
	long := strings.Repeat("é", questionTextMaxBytes)
	got := truncateUTF8(long, questionTextMaxBytes-1)
	if !utf8.ValidString(got) || len(got) > questionTextMaxBytes-1 {
		t.Errorf("truncated body invalid or too long: %d bytes valid=%v", len(got), utf8.ValidString(got))
	}
}

func TestSupervisorLoopStepMergeConflictStop(t *testing.T) {
	var calls int32
	res := SupervisorMergeResult{Merged: []string{"a"}, Conflict: "b", Note: "merge of b stopped"}
	_, steps, store := newStepsRig(t, SupervisorLoopHooks{
		MergeUntilConflict: func(context.Context) (SupervisorMergeResult, error) {
			atomic.AddInt32(&calls, 1)
			// The pipeline reports the unsettled conflict as an error too.
			return res, errors.New("merge of \"b\" hit a conflict that was not settled")
		},
	})
	r := resultFor(t, steps.RunPass(context.Background()), stepRunMergeUntilConflict)
	if r.Err != nil {
		t.Fatalf("a conflict is an outcome, not a step error: %v", r.Err)
	}
	if !strings.Contains(r.Detail, "conflict on b") || !strings.Contains(r.Detail, "merging 1") {
		t.Errorf("detail = %q", r.Detail)
	}
	if got := steps.LastConflict(); got != r.Detail {
		t.Errorf("LastConflict = %q, want %q", got, r.Detail)
	}
	if calls != 1 {
		t.Errorf("pipeline ran %d times, want 1 (stop at the conflict)", calls)
	}
	if rec := store.byJob(stepRunMergeUntilConflict); len(rec) != 1 || rec[0].Outcome != supervisorloop.OutcomeOK {
		t.Errorf("run record = %+v", rec)
	}

	// No conflict: the run is recorded plainly and no conflict is remembered.
	_, clean, _ := newStepsRig(t, SupervisorLoopHooks{
		MergeUntilConflict: func(context.Context) (SupervisorMergeResult, error) {
			return SupervisorMergeResult{Merged: []string{"a", "b"}}, nil
		},
	})
	if r := resultFor(t, clean.RunPass(context.Background()), stepRunMergeUntilConflict); !strings.Contains(r.Detail, "merged 2 branches") || clean.LastConflict() != "" {
		t.Errorf("clean run: %+v conflict=%q", r, clean.LastConflict())
	}
}

func wireQuestions(t *testing.T, m *Manager) *QuestionStore {
	t.Helper()
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig {
		return config.AutonomousQuestionsConfig{Enabled: true}
	})
	return store
}

func TestSupervisorLoopStepEscalationQuestion(t *testing.T) {
	h := newCoordHarness(t, 1)
	store := wireQuestions(t, h.m)
	esc := h.m.SupervisorLoopEscalator()
	e := supervisorloop.Escalation{Job: "draft_spec", Attempts: []supervisorloop.Attempt{
		{Output: "bad one", ValidatorError: "missing title"},
		{Output: "bad two", ValidatorError: "missing body"},
	}}
	if err := esc.Escalate(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	open := store.List(QuestionFilter{State: QuestionOpen})
	if len(open) != 1 {
		t.Fatalf("open questions = %d, want 1", len(open))
	}
	q := open[0]
	if !strings.Contains(q.Text, e.Summary()) || q.AskedBy != supervisorLoopQuestionAsker || q.AskerKind != QuestionAskerSystem {
		t.Errorf("question = %+v", q)
	}
	// The same escalation while one is open raises nothing more.
	if err := esc.Escalate(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if n := len(store.List(QuestionFilter{State: QuestionOpen})); n != 1 {
		t.Errorf("open questions after repeat = %d, want 1", n)
	}

	// Through the engine: a model job that keeps failing its validator raises
	// the question and returns no output.
	store2 := &slMemStore{}
	h2 := newCoordHarness(t, 1)
	q2 := wireQuestions(t, h2.m)
	eng := &supervisorloop.Engine{
		Settings: supervisorloop.DefaultSettings(), Clock: slClock{}, Store: store2,
		Escalator: h2.m.SupervisorLoopEscalator(),
		Client:    slClient{},
	}
	job := supervisorloop.Job{Name: "classify_ci_failure", Kind: supervisorloop.KindModel, Style: supervisorloop.StyleOneshot,
		Validator: func(string) error { return errors.New("not json") }}
	if _, err := eng.Run(context.Background(), job, "log"); !errors.Is(err, supervisorloop.ErrEscalated) {
		t.Fatalf("err = %v, want ErrEscalated", err)
	}
	got := q2.List(QuestionFilter{State: QuestionOpen})
	if len(got) != 1 || !strings.Contains(got[0].Text, "classify_ci_failure") || !strings.Contains(got[0].Text, "not json") {
		t.Errorf("engine escalation question = %+v", got)
	}

	// No question queue: the escalation reports it instead of dropping it.
	bare := newCoordHarness(t, 1)
	if err := bare.m.SupervisorLoopEscalator().Escalate(context.Background(), e); err == nil {
		t.Error("escalation with no question queue returned nil")
	}
}

type slClient struct{}

func (slClient) Ask(context.Context, supervisorloop.Request) (string, error) { return "garbage", nil }

func TestSupervisorLoopStepDeployAfterShip(t *testing.T) {
	var deploys int32
	var fail error
	h, steps, store := newStepsRig(t, SupervisorLoopHooks{
		Deploy: func(context.Context) (string, error) {
			atomic.AddInt32(&deploys, 1)
			return "restarted", fail
		},
	})
	run := func() SupervisorLoopStepResult {
		return resultFor(t, steps.RunPass(context.Background()), stepDeployAfterShip)
	}
	if r := run(); deploys != 0 || !strings.Contains(r.Detail, "nothing newly shipped") {
		t.Fatalf("deployed with nothing shipped: %+v deploys=%d", r, deploys)
	}
	if _, err := h.m.RecordPRDShipped(h.prds[0].ID, "v1.2.3", time.Now(), "abc", ShippedSourceBackfill); err != nil {
		t.Fatal(err)
	}
	if r := run(); deploys != 1 || !strings.Contains(r.Detail, "deployed v1.2.3") {
		t.Fatalf("first run: %+v deploys=%d", r, deploys)
	}
	if run(); deploys != 1 {
		t.Errorf("same release deployed again: %d", deploys)
	}
	if _, err := h.m.RecordPRDShipped(h.prds[0].ID, "v1.2.4", time.Now().Add(time.Hour), "def", ShippedSourceBackfill); err != nil {
		t.Fatal(err)
	}
	fail = errors.New("restart refused")
	if r := run(); r.Err == nil || deploys != 2 {
		t.Fatalf("failed deploy: %+v deploys=%d", r, deploys)
	}
	if rec := store.byJob(stepDeployAfterShip); rec[len(rec)-1].Outcome != supervisorloop.OutcomeError {
		t.Errorf("failed deploy outcome = %s", rec[len(rec)-1].Outcome)
	}
}

func TestSupervisorLoopStepRetryFailedInfra(t *testing.T) {
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{})
	p, _ := h.m.Store().GetPRD(h.prds[0].ID)
	p.Status = PRDBlocked
	p.Story = []Story{{ID: "s1", Tasks: []Task{{
		ID: "t1", Title: "held", Status: TaskReviewNeeded,
		FailureReason: &FailureReason{Source: infraRetryHoldSource},
	}}}}
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	r := resultFor(t, steps.RunPass(context.Background()), stepRetryFailedInfra)
	if r.Err != nil || r.Detail != "retried 1 of 1 held tasks" {
		t.Fatalf("first pass: %+v", r)
	}
	if _, task := func() (*PRD, *Task) {
		got, _ := h.m.Store().GetPRD(p.ID)
		return got, &got.Story[0].Tasks[0]
	}(); task.Status == TaskReviewNeeded {
		t.Errorf("held task still %s after the retry", task.Status)
	}
	steps.mu.Lock()
	marked := steps.infraRetried[p.ID+"/t1"]
	steps.mu.Unlock()
	if !marked {
		t.Error("the loop did not remember its retry of the held task")
	}
	// The task is held again (the cause persists): the second pass leaves it
	// for the operator instead of retrying it forever.
	again, _ := h.m.Store().GetPRD(p.ID)
	again.Status = PRDBlocked
	again.Story[0].Tasks[0].Status = TaskReviewNeeded
	again.Story[0].Tasks[0].FailureReason = &FailureReason{Source: infraRetryHoldSource}
	if err := h.m.Store().SavePRD(again); err != nil {
		t.Fatal(err)
	}
	r2 := resultFor(t, steps.RunPass(context.Background()), stepRetryFailedInfra)
	if r2.Err != nil || !strings.HasPrefix(r2.Detail, "retried 0 of 1") {
		t.Errorf("second pass: %+v", r2)
	}
}

func TestSupervisorLoopStepApplyAnswersAndChain(t *testing.T) {
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{})
	rs := steps.RunPass(context.Background())
	if r := resultFor(t, rs, stepApplyAnswers); r.Detail != "no question queue wired" {
		t.Errorf("apply_answers without a queue: %q", r.Detail)
	}
	if r := resultFor(t, rs, stepStartNextChainStep); r.Detail != "advanced 0 chains" {
		t.Errorf("chain step: %q", r.Detail)
	}
	wireQuestions(t, h.m)
	if r := resultFor(t, steps.RunPass(context.Background()), stepApplyAnswers); r.Err != nil || r.Detail != "checked 0 answered held questions" {
		t.Errorf("apply_answers with an empty queue: %+v", r)
	}
}

func TestSupervisorLoopStepIdleTickRunsOnePassPerTick(t *testing.T) {
	var merges, ticks int32
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{
		MergeUntilConflict: func(context.Context) (SupervisorMergeResult, error) {
			atomic.AddInt32(&merges, 1)
			return SupervisorMergeResult{}, errors.New("always fails")
		},
	})
	h.c.SetSupervisorLoopSteps(steps)
	h.c.idleTick = 5 * time.Millisecond
	h.c.idleTickHook = func() { atomic.AddInt32(&ticks, 1) }
	h.c.Start()
	deadline := time.After(5 * time.Second)
	for atomic.LoadInt32(&merges) < 3 {
		select {
		case <-deadline:
			h.c.Stop()
			t.Fatal("the idle tick never ran the steps")
		case <-time.After(5 * time.Millisecond):
		}
	}
	h.c.Stop()
	if m, tk := atomic.LoadInt32(&merges), atomic.LoadInt32(&ticks); m > tk {
		t.Errorf("%d step runs over %d ticks: more than one pass per tick", m, tk)
	}
}
