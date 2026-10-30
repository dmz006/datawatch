// Package tests for the supervisor watch work checks
// (supervisor_watch_work.go): one test per finding kind, plus that an
// already-raised finding is not raised again and that a merge-pending
// Automaton that has not finished all of its work is not raised.
package autonomous

import (
	"testing"
)

// Fake views the checks read from, in place of the coordinator, the
// lane fill and the repo CI watch store. They return fixed state and
// record each read so a test can assert what the checks did and did
// not touch.

// fakeSupervisorWatchPRDLister returns the PRDs a test sets.
type fakeSupervisorWatchPRDLister struct {
	prds []*PRD
}

func (f *fakeSupervisorWatchPRDLister) PRDs() []*PRD { return f.prds }

// fakeSupervisorWatchLaneFill returns the status a test sets.
type fakeSupervisorWatchLaneFill struct {
	st LaneFillStatus
	ok bool
	n  int // how many times LaneFillStat was called
}

func (f *fakeSupervisorWatchLaneFill) LaneFillStat() (LaneFillStatus, bool) { f.n++; return f.st, f.ok }

// fakeSupervisorWatchCIRecords returns the records a test sets.
type fakeSupervisorWatchCIRecords struct {
	recs []*RepoCIWatchRecord
}

func (f *fakeSupervisorWatchCIRecords) CIWatchRecords() []*RepoCIWatchRecord { return f.recs }

// workTestPRD builds one Automaton with a single story carrying the
// given tasks and the given triage record.
func workTestPRD(id string, triage *TriageRecord, tasks ...Task) *PRD {
	return &PRD{
		ID:    id,
		Title: id,
		Story: []Story{
			{
				ID:    id + "-s1",
				PRDID: id,
				Tasks: tasks,
			},
		},
		Triage: triage,
	}
}

func workTestTask(prdID, id string, status TaskStatus) Task {
	return Task{
		ID:      id,
		StoryID: prdID + "-s1",
		PRDID:   prdID,
		Title:   "task " + id,
		Status:  status,
	}
}

// TestSupervisorWatchWorkTaskFailed — one task_failed finding per
// Automaton whose triage record names a task and that task sits in
// its failed state; and none when the named task is in any other
// state, when the record names no task, or when the record is absent.
func TestSupervisorWatchWorkTaskFailed(t *testing.T) {
	good := workTestPRD("p-1",
		&TriageRecord{PRDID: "p-1", Status: "failed", TaskID: "t-1"},
		workTestTask("p-1", "t-1", TaskFailed))

	otherState := workTestPRD("p-2",
		&TriageRecord{PRDID: "p-2", Status: "failed", TaskID: "t-9"},
		workTestTask("p-2", "t-9", TaskCompleted))

	noTaskID := workTestPRD("p-3", &TriageRecord{PRDID: "p-3", Status: "failed"},
		workTestTask("p-3", "t-3", TaskFailed))

	missing := workTestPRD("p-4", nil, workTestTask("p-4", "t-4", TaskFailed))

	w := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{good, otherState, noTaskID, missing}}, nil, nil)
	fs := w.CheckTaskFailed()
	if len(fs) != 1 {
		t.Fatalf("expected 1 task_failed finding, got %d", len(fs))
	}
	if fs[0].Kind != SupervisorWatchTaskFailed || fs[0].Subject != "p-1" {
		t.Errorf("expected task_failed/p-1, got %q/%q", fs[0].Kind, fs[0].Subject)
	}
	if fs[0].Text == "" {
		t.Errorf("finding has no operator-facing text")
	}
}

// TestSupervisorWatchWorkTaskReviewNeeded — one task_review_needed
// finding per running or blocked Automaton that has a task held in its
// review state, read from the tasks and not from the triage record
// (the record of an escalated Automaton names no task: it carries only
// the blocked reason). None when the task is failed instead (the
// task_failed finding's own state), when the Automaton is not running
// or blocked, or when no task is held.
func TestSupervisorWatchWorkTaskReviewNeeded(t *testing.T) {
	// The normal escalation path: blocked, a triage record with no task
	// id, one task held for review.
	escalated := workTestPRD("p-1",
		&TriageRecord{PRDID: "p-1", Status: "blocked"},
		workTestTask("p-1", "t-1", TaskReviewNeeded))
	escalated.Status = PRDBlocked

	// Running with the held task and no triage record at all.
	running := workTestPRD("p-2", nil,
		workTestTask("p-2", "t-2", TaskCompleted),
		workTestTask("p-2", "t-3", TaskReviewNeeded),
		workTestTask("p-2", "t-4", TaskReviewNeeded))
	running.Status = PRDRunning

	failedNotReview := workTestPRD("p-3",
		&TriageRecord{PRDID: "p-3", Status: "failed", TaskID: "t-5"},
		workTestTask("p-3", "t-5", TaskFailed))
	failedNotReview.Status = PRDBlocked

	cancelled := workTestPRD("p-4", nil, workTestTask("p-4", "t-6", TaskReviewNeeded))
	cancelled.Status = PRDCancelled

	w := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{escalated, running, failedNotReview, cancelled}}, nil, nil)
	fs := w.CheckTaskReviewNeeded()
	if len(fs) != 2 {
		t.Fatalf("expected 2 task_review_needed findings (p-1, p-2), got %d: %+v", len(fs), fs)
	}
	for i, want := range []string{"p-1", "p-2"} {
		if fs[i].Kind != SupervisorWatchTaskReviewNeeded || fs[i].Subject != want {
			t.Errorf("finding %d: expected task_review_needed/%s, got %q/%q", i, want, fs[i].Kind, fs[i].Subject)
		}
		if fs[i].Text == "" {
			t.Errorf("finding %d has no operator-facing text", i)
		}
	}
	if again := w.CheckTaskReviewNeeded(); len(again) != 0 {
		t.Errorf("an already-raised finding must not be raised again, got %d", len(again))
	}
}

// TestWorkNotRaisedAgain — a finding the watch already raised (because
// the store recorded it) is not raised again on the next poll, whether
// it was marked raised by its own check returning it or by the
// coordinator's Raise call.
func TestSupervisorWatchWorkNotRaisedAgain(t *testing.T) {
	p := workTestPRD("p-1",
		&TriageRecord{PRDID: "p-1", Status: "failed", TaskID: "t-1"},
		workTestTask("p-1", "t-1", TaskFailed))
	w := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{p}}, nil, nil)

	first := w.CheckTaskFailed()
	if len(first) != 1 {
		t.Fatalf("first poll: expected 1 finding, got %d", len(first))
	}
	second := w.CheckTaskFailed()
	if len(second) != 0 {
		t.Errorf("already-raised finding must not be raised again, got %d", len(second))
	}

	// An explicit Raise also suppresses the check.
	w2 := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{p}}, nil, nil)
	w2.Raise(SupervisorWatchFinding{Kind: SupervisorWatchTaskFailed, Subject: "p-1"})
	if got := w2.CheckTaskFailed(); len(got) != 0 {
		t.Errorf("Raise must suppress the check, got %d findings", len(got))
	}
}

// TestWorkAutomatonNotMerged — one automaton_not_merged finding per
// Automaton whose triage record sits at merge_pending and whose work
// is complete; and none when the record is not at merge_pending, or
// when any task is not finished, or when there are no tasks at all.
func TestSupervisorWatchWorkAutomatonNotMerged(t *testing.T) {
	good := workTestPRD("p-1",
		&TriageRecord{PRDID: "p-1", Status: TriageStatusMergePending},
		workTestTask("p-1", "t-1", TaskCompleted),
		workTestTask("p-1", "t-2", TaskCompleted))

	notMergePending := workTestPRD("p-2",
		&TriageRecord{PRDID: "p-2", Status: "failed", TaskID: "t-3"},
		workTestTask("p-2", "t-3", TaskCompleted))

	aTaskNotDone := workTestPRD("p-3",
		&TriageRecord{PRDID: "p-3", Status: TriageStatusMergePending},
		workTestTask("p-3", "t-1", TaskCompleted),
		workTestTask("p-3", "t-2", TaskPending))

	noTasks := workTestPRD("p-4", &TriageRecord{PRDID: "p-4", Status: TriageStatusMergePending})

	w := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{good, notMergePending, aTaskNotDone, noTasks}}, nil, nil)
	fs := w.CheckAutomatonNotMerged()
	if len(fs) != 1 {
		t.Fatalf("expected 1 automaton_not_merged finding, got %d", len(fs))
	}
	if fs[0].Kind != SupervisorWatchAutomatonNotMerged || fs[0].Subject != "p-1" {
		t.Errorf("expected automaton_not_merged/p-1, got %q/%q", fs[0].Kind, fs[0].Subject)
	}
}

// TestWorkIdleLane — one idle_lane_eligible_work finding per lane that
// is idle and has eligible work queued on its backend; and none for an
// idle lane with nothing queued, an occupied lane with work queued, or
// a missing / unavailable lane fill status.
func TestSupervisorWatchWorkIdleLaneEligibleWork(t *testing.T) {
	st := LaneFillStatus{
		Mode: "off",
		Lanes: []LaneFillLaneStatus{
			{ID: "lane-1", Type: LaneTypeGPU, State: "idle"},
			{ID: "lane-2", Type: LaneTypeGPU, State: "idle"},
			{ID: "lane-3", Type: LaneTypeGPU, State: "occupied"},
		},
		QueuedEligible: LaneFillQueued{GPU: 1, Claude: 0},
	}
	w := NewSupervisorWatchWork(nil, &fakeSupervisorWatchLaneFill{st: st, ok: true}, nil)
	fs := w.CheckIdleLane()
	if len(fs) != 2 {
		t.Fatalf("expected 2 idle_lane findings, got %d", len(fs))
	}
	for _, f := range fs {
		if f.Kind != SupervisorWatchIdleLaneEligibleWork {
			t.Errorf("unexpected kind %q", f.Kind)
		}
	}

	// Idle lane with nothing queued: no finding.
	stEmpty := st
	stEmpty.Lanes = []LaneFillLaneStatus{{ID: "lane-1", Type: LaneTypeGPU, State: "idle"}}
	stEmpty.QueuedEligible = LaneFillQueued{}
	wEmpty := NewSupervisorWatchWork(nil, &fakeSupervisorWatchLaneFill{st: stEmpty, ok: true}, nil)
	if got := wEmpty.CheckIdleLane(); len(got) != 0 {
		t.Errorf("idle lane with no queued work must not find, got %d", len(got))
	}

	// Occupied lane with work queued: no finding.
	stBusy := st
	stBusy.Lanes = []LaneFillLaneStatus{{ID: "lane-3", Type: LaneTypeGPU, State: "occupied"}}
	wBusy := NewSupervisorWatchWork(nil, &fakeSupervisorWatchLaneFill{st: stBusy, ok: true}, nil)
	if got := wBusy.CheckIdleLane(); len(got) != 0 {
		t.Errorf("occupied lane must not find, got %d", len(got))
	}

	// A missing lane fill statuser is no lane fill: no findings, no panic.
	wNil := NewSupervisorWatchWork(nil, nil, nil)
	if got := wNil.CheckIdleLane(); len(got) != 0 {
		t.Errorf("a missing lane fill must not find, got %d", len(got))
	}

	// An unavailable status is also no findings.
	wOff := NewSupervisorWatchWork(nil, &fakeSupervisorWatchLaneFill{ok: false}, nil)
	if got := wOff.CheckIdleLane(); len(got) != 0 {
		t.Errorf("an unavailable lane fill must not find, got %d", len(got))
	}
}

// TestWorkCIRed — one ci_red finding per record whose run failed; and
// none for any other status, a nil record, or a missing repo CI watch
// lister. The check does not call GitHub: the fake is the records the
// check reads, and nothing external is touched.
func TestSupervisorWatchWorkCIRed(t *testing.T) {
	w := NewSupervisorWatchWork(nil, nil, &fakeSupervisorWatchCIRecords{recs: []*RepoCIWatchRecord{
		{Repo: "o/r", RunID: "42", Status: "failing"},
		{Repo: "o/r", RunID: "43", Status: "green"},
		{Repo: "o/r", RunID: "", Status: "failing"}, // nil RunID: skipped
		nil,
	}})
	fs := w.CheckCIRed()
	if len(fs) != 1 {
		t.Fatalf("expected 1 ci_red finding, got %d", len(fs))
	}
	if fs[0].Kind != SupervisorWatchCIRed || fs[0].Subject != "42" {
		t.Errorf("expected ci_red/42, got %q/%q", fs[0].Kind, fs[0].Subject)
	}

	wNoCI := NewSupervisorWatchWork(nil, nil, nil)
	if got := wNoCI.CheckCIRed(); len(got) != 0 {
		t.Errorf("a missing repo CI watch must not find, got %d", len(got))
	}
}

// TestWorkCISTuck — one ci_stuck finding per record whose run is hung;
// and none for any other status, a nil record, or a missing repo CI
// watch lister. No GitHub.
func TestSupervisorWatchWorkCISTuck(t *testing.T) {
	w := NewSupervisorWatchWork(nil, nil, &fakeSupervisorWatchCIRecords{recs: []*RepoCIWatchRecord{
		{Repo: "o/r", RunID: "42", Status: "hung"},
		{Repo: "o/r", RunID: "43", Status: "green"},
		{Repo: "o/r", RunID: "44", Status: "failing"},
		nil,
	}})
	fs := w.CheckCISTuck()
	if len(fs) != 1 {
		t.Fatalf("expected 1 ci_stuck finding, got %d", len(fs))
	}
	if fs[0].Kind != SupervisorWatchCISTuck || fs[0].Subject != "42" {
		t.Errorf("expected ci_stuck/42, got %q/%q", fs[0].Kind, fs[0].Subject)
	}

	wNoCI := NewSupervisorWatchWork(nil, nil, nil)
	if got := wNoCI.CheckCISTuck(); len(got) != 0 {
		t.Errorf("a missing repo CI watch must not find, got %d", len(got))
	}
}
