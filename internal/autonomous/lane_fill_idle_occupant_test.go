package autonomous

import (
	"strings"
	"testing"
	"time"
)

// This file tests the classification of a running Automaton's lane hold
// (lane_fill_idle_occupant.go): the pure classifier, and the coordinator
// wiring that turns a waiting classification into an idle lane and a Waiting
// list entry, with no second run ever started.

var occNow = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

// occPRD builds one in-memory PRD with the tasks on one story, in order.
func occPRD(tasks ...Task) *PRD {
	p := &PRD{ID: "p1"}
	story := Story{ID: "s1", PRDID: "p1"}
	for i := range tasks {
		if tasks[i].ID == "" {
			tasks[i].ID = "t" + string(rune('a'+i))
		}
		tasks[i].StoryID = "s1"
		story.Tasks = append(story.Tasks, tasks[i])
	}
	p.Story = []Story{story}
	return p
}

// occFacts is the classifier's input with a fixed clock and the default
// stalled-merge threshold of 30 minutes.
func occFacts(p *PRD, held map[string]bool) laneOccFacts {
	if held == nil {
		held = map[string]bool{}
	}
	return laneOccFacts{
		prd:                 p,
		held:                held,
		pendingSince:        map[string]time.Time{},
		stalledMergeMinutes: 30,
		now:                 occNow,
	}
}

func TestClassifyLaneOccupant(t *testing.T) {
	cases := []struct {
		name    string
		facts   laneOccFacts
		wantOcc bool
		wantCod string
		wantTxt string
	}{
		{
			name:    "no snapshot keeps the lane",
			facts:   laneOccFacts{prd: nil, held: map[string]bool{}, pendingSince: map[string]time.Time{}, now: occNow},
			wantOcc: true,
		},
		{
			name:    "live task with a session occupies",
			facts:   occFacts(occPRD(Task{Status: TaskInProgress, SessionID: "sess-1"}), nil),
			wantOcc: true,
		},
		{
			name:    "live task with only a verifier session occupies",
			facts:   occFacts(occPRD(Task{Status: TaskVerifying, VerifierSessionID: "v-1"}), nil),
			wantOcc: true,
		},
		{
			name:    "live task with a worker lease occupies",
			facts:   occFacts(occPRD(Task{Status: TaskRunningTests}), map[string]bool{"ta": true}),
			wantOcc: true,
		},
		{
			name:    "live task with a judge lease occupies",
			facts:   occFacts(occPRD(Task{Status: TaskInProgress}), map[string]bool{JudgeHolder("ta"): true}),
			wantOcc: true,
		},
		{
			name:    "live task occupies even with no session and no lease yet",
			facts:   occFacts(occPRD(Task{Status: TaskInProgress}), nil),
			wantOcc: true,
		},
		{
			name:    "verifying task occupies even with no verifier lease yet",
			facts:   occFacts(occPRD(Task{Status: TaskVerifying}), nil),
			wantOcc: true,
		},
		{
			name:    "running tests occupies",
			facts:   occFacts(occPRD(Task{Status: TaskRunningTests}), nil),
			wantOcc: true,
		},
		{
			name:    "pending task occupies, in the act of starting",
			facts:   occFacts(occPRD(Task{Status: TaskPending}), nil),
			wantOcc: true,
		},
		{
			name:    "waiting capacity occupies",
			facts:   occFacts(occPRD(Task{Status: TaskWaitingCapacity}), nil),
			wantOcc: true,
		},
		{
			name:    "no task yet: the Automaton is starting and keeps its lane",
			facts:   occFacts(occPRD(), nil),
			wantOcc: true,
		},
		{
			name:    "ready queued task occupies",
			facts:   occFacts(occPRD(Task{Status: TaskQueued}), nil),
			wantOcc: true,
		},
		{
			name: "queued task with an unmerged branch dependency occupies",
			facts: occFacts(occPRD(
				Task{Status: TaskCompleted, WorktreeBranch: "b1"},
				Task{Status: TaskQueued, DependsOn: []string{"ta"}},
			), nil),
			wantOcc: true,
		},
		{
			name: "queued task with a failed dependency waits",
			facts: occFacts(occPRD(
				Task{Status: TaskFailed},
				Task{Status: TaskQueued, DependsOn: []string{"ta"}},
			), nil),
			wantOcc: false,
			wantCod: LaneOccNoTaskStartable,
			wantTxt: "no task can start now",
		},
		{
			name:    "settled work with a live judge lease still occupies",
			facts:   occFacts(occPRD(Task{Status: TaskCompleted}), map[string]bool{JudgeHolder("ta"): true}),
			wantOcc: true,
		},
		{
			name:    "settled work with a live verifier lease still occupies",
			facts:   occFacts(occPRD(Task{Status: TaskCompleted}), map[string]bool{VerifyHolder("ta"): true}),
			wantOcc: true,
		},
		{
			name:    "all tasks completed with nothing to merge waits for the review",
			facts:   occFacts(occPRD(Task{Status: TaskCompleted}), nil),
			wantOcc: false,
			wantCod: LaneOccWaitingReviewMerge,
			wantTxt: "completion review",
		},
		{
			name: "all merged tasks waits for review and merge",
			facts: occFacts(occPRD(
				Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStateMerged},
				Task{Status: TaskCompleted, WorktreeBranch: "b2", MergeState: MergeStateMerged},
			), nil),
			wantOcc: false,
			wantCod: LaneOccWaitingReviewMerge,
		},
		{
			name: "blocked and cancelled tasks wait, nothing can start",
			facts: occFacts(occPRD(
				Task{Status: TaskBlocked},
				Task{Status: TaskCancelled},
			), nil),
			wantOcc: false,
			wantCod: LaneOccNoTaskStartable,
			wantTxt: "no task can start now",
		},
		{
			name: "review-needed and failed tasks wait, nothing can start",
			facts: occFacts(occPRD(
				Task{Status: TaskReviewNeeded},
				Task{Status: TaskFailed},
			), nil),
			wantOcc: false,
			wantCod: LaneOccNoTaskStartable,
		},
		{
			name:    "a fresh pending merge still holds the lane",
			facts:   occFacts(occPRD(Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending}), nil),
			wantOcc: true,
		},
		{
			name: "a pending merge past the threshold waits, stalled",
			facts: func() laneOccFacts {
				f := occFacts(occPRD(Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending}), nil)
				f.pendingSince["ta"] = occNow.Add(-40 * time.Minute)
				return f
			}(),
			wantOcc: false,
			wantCod: LaneOccMergeStalled,
			wantTxt: "40 min",
		},
		{
			name: "the rule is off at a zero threshold, the lane is kept",
			facts: func() laneOccFacts {
				f := occFacts(occPRD(Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending}), nil)
				f.stalledMergeMinutes = 0
				f.pendingSince["ta"] = occNow.Add(-10 * time.Hour)
				return f
			}(),
			wantOcc: true,
		},
		{
			name: "one fresh and one stalled pending merge keeps the lane",
			facts: func() laneOccFacts {
				p := occPRD(
					Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending},
					Task{Status: TaskCompleted, WorktreeBranch: "b2", MergeState: MergeStatePending},
				)
				f := occFacts(p, nil)
				f.pendingSince["ta"] = occNow.Add(-40 * time.Minute)
				return f
			}(),
			wantOcc: true,
		},
		{
			name: "all pending merges stalled waits, stalled",
			facts: func() laneOccFacts {
				p := occPRD(
					Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending},
					Task{Status: TaskCompleted, WorktreeBranch: "b2", MergeState: MergeStatePending},
				)
				f := occFacts(p, nil)
				f.pendingSince["ta"] = occNow.Add(-50 * time.Minute)
				f.pendingSince["tb"] = occNow.Add(-40 * time.Minute)
				return f
			}(),
			wantOcc: false,
			wantCod: LaneOccMergeStalled,
			wantTxt: "threshold 30 min",
		},
		{
			name: "failed and cancelled tasks are all terminal and wait for the review",
			facts: occFacts(occPRD(
				Task{Status: TaskFailed},
				Task{Status: TaskCancelled},
			), nil),
			wantOcc: false,
			wantCod: LaneOccWaitingReviewMerge,
		},
		{
			name: "a recorded merge time past the threshold wins over a fresh first-seen time",
			facts: func() laneOccFacts {
				f := occFacts(occPRD(Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending,
					MergeBlock: &MergeBlock{Since: occNow.Add(-45 * time.Minute)}}), nil)
				f.pendingSince["ta"] = occNow.Add(-1 * time.Minute)
				return f
			}(),
			wantOcc: false,
			wantCod: LaneOccMergeStalled,
			wantTxt: "45 min",
		},
		{
			name: "a recorded merge time inside the threshold keeps the lane",
			facts: func() laneOccFacts {
				f := occFacts(occPRD(Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStatePending,
					MergeBlock: &MergeBlock{Since: occNow.Add(-5 * time.Minute)}}), nil)
				f.pendingSince["ta"] = occNow.Add(-3 * time.Hour)
				return f
			}(),
			wantOcc: true,
		},
		{
			name:    "a conflicted merge needs the operator, waits for review and merge",
			facts:   occFacts(occPRD(Task{Status: TaskCompleted, WorktreeBranch: "b1", MergeState: MergeStateConflict}), nil),
			wantOcc: false,
			wantCod: LaneOccWaitingReviewMerge,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLaneOccupant(tc.facts)
			if got.Occupied != tc.wantOcc {
				t.Fatalf("occupied = %v, want %v (code %q, reason %q)", got.Occupied, tc.wantOcc, got.Code, got.Reason)
			}
			if tc.wantOcc {
				if got.Code != "" || got.Reason != "" {
					t.Fatalf("occupied result carries code %q reason %q", got.Code, got.Reason)
				}
				return
			}
			if got.Code != tc.wantCod {
				t.Fatalf("code = %q, want %q", got.Code, tc.wantCod)
			}
			if tc.wantTxt != "" && !strings.Contains(got.Reason, tc.wantTxt) {
				t.Fatalf("reason %q does not contain %q", got.Reason, tc.wantTxt)
			}
		})
	}
}

func TestLaneOccMergePending(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want bool
	}{
		{"completed branch pending", Task{Status: TaskCompleted, WorktreeBranch: "b"}, true},
		{"completed branch empty state", Task{Status: TaskCompleted, WorktreeBranch: "b", MergeState: ""}, true},
		{"merged is not pending", Task{Status: TaskCompleted, WorktreeBranch: "b", MergeState: MergeStateMerged}, false},
		{"conflict is not pending", Task{Status: TaskCompleted, WorktreeBranch: "b", MergeState: MergeStateConflict}, false},
		{"no branch has nothing to merge", Task{Status: TaskCompleted, WorktreeBranch: ""}, false},
		{"not completed is not pending", Task{Status: TaskQueued, WorktreeBranch: "b"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := laneOccMergePending(&tc.task); got != tc.want {
				t.Fatalf("laneOccMergePending = %v, want %v", got, tc.want)
			}
		})
	}
}

// idleOccTask adds one task to the harness PRD with the given status, branch
// and merge state.
func idleOccTask(t *testing.T, m *Manager, prdID string, st TaskStatus, branch, mergeState string) *Task {
	t.Helper()
	s, err := m.store.AddStory(prdID, "story", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := m.store.AddTask(prdID, s.ID, "task", "spec", nil)
	if err != nil {
		t.Fatal(err)
	}
	m.store.MutateTask(task.ID, func(tk *Task) {
		tk.Status = st
		tk.WorktreeBranch = branch
		if mergeState != "" {
			tk.MergeState = mergeState
		}
	})
	return task
}

func waitingOcc(t *testing.T, st LaneFillStatus, prdID string) *LaneFillWaitingOcc {
	t.Helper()
	for i := range st.Waiting {
		if st.Waiting[i].PRDID == prdID {
			return &st.Waiting[i]
		}
	}
	t.Fatalf("PRD %s not in the Waiting list: %+v", prdID, st.Waiting)
	return nil
}

// TestLaneFillIdleOccupantWaiting shows the full path: a running Automaton
// whose work is all done keeps no lane — its lane is idle, it is in the
// Waiting list with the review-merge reason, and the planner never starts a
// second run of it.
func TestLaneFillIdleOccupantWaiting(t *testing.T) {
	h := newLaneFillHarness(t, "")
	h.c.SetReady()
	p := h.addPRD("idle occupant", PRDRunning, "claude", "claude-code")
	h.addDoneTask(p.ID) // one completed task, no branch, nothing to merge

	h.poll()
	st := h.status()
	w := waitingOcc(t, st, p.ID)
	if w.ReasonCode != LaneOccWaitingReviewMerge {
		t.Fatalf("reason code = %q, want %q", w.ReasonCode, LaneOccWaitingReviewMerge)
	}
	if w.WaitReason == "" {
		t.Fatal("no wait reason recorded")
	}
	if w.Title != "idle occupant" {
		t.Fatalf("title = %q", w.Title)
	}
	// The lane it sat on is idle, not occupied by it.
	for _, l := range st.Lanes {
		if l.Occupant != nil && l.Occupant.PRDID == p.ID {
			t.Fatalf("lane %s still occupied by the idle occupant: %+v", l.ID, l.Occupant)
		}
	}
	// The planner never starts a second run of the waiting Automaton.
	if c := h.fake.all(); len(c) != 0 {
		t.Fatalf("starter calls = %v, want none", c)
	}
	for _, d := range st.Decisions {
		if d.PRDID == p.ID && (d.Action == LaneFillActionStartPRD || d.Action == LaneFillActionDecompose) {
			t.Fatalf("decision starts the waiting Automaton: %+v", d)
		}
	}
}

// TestLaneFillIdleOccupantMergeStalled shows the stalled-merge path: a
// completed task with a branch that is still pending holds the lane while it
// is fresh, then, once it has been pending past the threshold, the lane is
// freed and the Automaton waits with the stalled reason.
func TestLaneFillIdleOccupantMergeStalled(t *testing.T) {
	h := newLaneFillHarness(t, "")
	h.c.SetReady()
	p := h.addPRD("stalled merge", PRDRunning, "claude", "claude-code")
	idleOccTask(t, h.m, p.ID, TaskCompleted, "task-branch", MergeStatePending)

	h.poll()
	st := h.status()
	if len(st.Waiting) != 0 {
		t.Fatalf("a fresh pending merge frees the lane too early: %+v", st.Waiting)
	}
	found := false
	for _, l := range st.Lanes {
		if l.Occupant != nil && l.Occupant.PRDID == p.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("a fresh pending merge should keep the lane occupied")
	}

	// Past the default threshold of 30 minutes: the lane is freed.
	h.now = h.base.Add(40 * time.Minute)
	h.poll()
	st = h.status()
	w := waitingOcc(t, st, p.ID)
	if w.ReasonCode != LaneOccMergeStalled {
		t.Fatalf("reason code = %q, want %q", w.ReasonCode, LaneOccMergeStalled)
	}
	if !strings.Contains(w.WaitReason, "stalled-merge") {
		t.Fatalf("wait reason %q does not name the stalled-merge rule", w.WaitReason)
	}
	if c := h.fake.all(); len(c) != 0 {
		t.Fatalf("starter calls = %v, want none", c)
	}
}

// TestLaneFillIdleOccupantMergeRuleOff shows a zero stalled-merge threshold
// turns the rule off: the pending merge keeps the lane even when long stale.
func TestLaneFillIdleOccupantMergeRuleOff(t *testing.T) {
	h := newLaneFillHarness(t, "")
	h.c.SetReady()
	cfg := h.m.Config()
	zero := 0
	cfg.LaneFill.StalledMergeMinutes = &zero
	h.m.SetConfig(cfg)
	p := h.addPRD("merge rule off", PRDRunning, "claude", "claude-code")
	idleOccTask(t, h.m, p.ID, TaskCompleted, "task-branch", MergeStatePending)

	h.poll()
	h.now = h.base.Add(2 * time.Hour)
	h.poll()
	st := h.status()
	if len(st.Waiting) != 0 {
		t.Fatalf("rule off at 0: the pending merge still holds the lane, got %+v", st.Waiting)
	}
}
