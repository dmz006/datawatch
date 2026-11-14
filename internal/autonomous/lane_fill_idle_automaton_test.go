package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// This file tests, end to end through the lane fill coordinator, which running
// Automata hold their lane and which do not: a finished Automaton that only
// waits on its completion review frees the lane, a task-branch merge stalled
// past stalled_merge_minutes frees it and raises one alert and a retry, and an
// Automaton with a live worker, verifier or judge session, or a task actually
// starting, keeps it.
//
// Everything is faked: the harness' fake starter, a fake clock that only moves
// when a test advances it, a fake merger for the retry and a real in-memory
// capacity ledger for the lease holders. No git, tmux or session is touched,
// nothing sleeps and nothing loads the CPU.

// idleAutoKind is one lane type under test: how an Automaton prefers it, the
// backend that lands on it and the lane id the status gives it.
type idleAutoKind struct {
	name    string
	pref    string
	backend string
	laneID  string
	laneTyp LaneType
}

var idleAutoKinds = []idleAutoKind{
	{name: "claude", pref: "claude", backend: "claude-code", laneID: "claude-1", laneTyp: LaneTypeClaude},
	{name: "gpu", pref: "gpu", backend: "pi-prompt", laneID: "gpu-1", laneTyp: LaneTypeGPU},
}

// idleAutoTask describes one task of a test Automaton.
type idleAutoTask struct {
	title             string
	status            TaskStatus
	branch            string
	mergeState        string
	sessionID         string
	verifierSessionID string
	dependsOn         []int // indexes of earlier tasks in the same call
}

// idleAutoAddTasks adds one story with the described tasks to the Automaton
// and returns them in order.
func idleAutoAddTasks(t *testing.T, h *laneFillHarness, prdID string, specs ...idleAutoTask) []*Task {
	t.Helper()
	story, err := h.m.store.AddStory(prdID, "story", "")
	if err != nil {
		t.Fatal(err)
	}
	var out []*Task
	for i, sp := range specs {
		var deps []string
		for _, d := range sp.dependsOn {
			deps = append(deps, out[d].ID)
		}
		title := sp.title
		if title == "" {
			title = "task"
		}
		task, err := h.m.store.AddTask(prdID, story.ID, title, "spec", deps)
		if err != nil {
			t.Fatalf("add task %d: %v", i, err)
		}
		sp := sp
		h.m.store.MutateTask(task.ID, func(tk *Task) {
			tk.Title = title
			tk.Status = sp.status
			tk.WorktreeBranch = sp.branch
			if sp.mergeState != "" {
				tk.MergeState = sp.mergeState
			}
			tk.SessionID = sp.sessionID
			tk.VerifierSessionID = sp.verifierSessionID
		})
		out = append(out, task)
	}
	return out
}

// idleAutoLedger wires a real in-memory capacity ledger into the Automaton
// manager and returns it, so a test can hold a lease under a worker, verifier
// or judge holder id exactly as the live code does.
func idleAutoLedger(t *testing.T, h *laneFillHarness) *capacity.Ledger {
	t.Helper()
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:live", 10)
	h.m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"node:live"}, "live"
	})
	return led
}

// idleAutoHold grants holder a lease on the fixture ledger.
func idleAutoHold(t *testing.T, led *capacity.Ledger, holder, prdID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := capacity.Request{Holder: holder, PRDID: prdID, Pools: []string{"node:live"}}
	if err := led.Acquire(ctx, req, 2*time.Second, nil, nil); err != nil {
		t.Fatalf("hold lease %q: %v", holder, err)
	}
	t.Cleanup(func() { led.Release(holder) })
}

// idleAutoLane returns the lane with the id from the status.
func idleAutoLane(t *testing.T, st LaneFillStatus, id string) LaneFillLaneStatus {
	t.Helper()
	for _, l := range st.Lanes {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("lane %q not in the status: %+v", id, st.Lanes)
	return LaneFillLaneStatus{}
}

// idleAutoStartDecisions returns the decisions that start or propose the
// Automaton.
func idleAutoStartDecisions(st LaneFillStatus, prdID string) []LaneFillDecision {
	var out []LaneFillDecision
	for _, d := range st.Decisions {
		if d.PRDID == prdID && d.Action == LaneFillActionStartPRD {
			out = append(out, d)
		}
	}
	return out
}

// idleAutoIsWaiting says the Automaton is in the status' waiting list.
func idleAutoIsWaiting(st LaneFillStatus, prdID string) bool {
	for _, w := range st.Waiting {
		if w.PRDID == prdID {
			return true
		}
	}
	return false
}

// TestLaneFillFinishedAutomatonFreesLane: a running Automaton whose tasks are
// all complete and which only waits on its completion review and merge holds
// no live session, so its lane is idle, the next eligible queued Automaton is
// proposed for that lane, and the finished one is in the waiting list with its
// reason and is never proposed again.
func TestLaneFillFinishedAutomatonFreesLane(t *testing.T) {
	for _, k := range idleAutoKinds {
		k := k
		t.Run(k.name, func(t *testing.T) {
			h := newLaneFillHarness(t, "shadow")
			h.c.SetReady()

			finished := h.addPRD("finished "+k.name, PRDRunning, k.pref, k.backend)
			idleAutoAddTasks(t, h, finished.ID,
				idleAutoTask{title: "first", status: TaskCompleted, branch: "task-branch-1", mergeState: MergeStateMerged},
				idleAutoTask{title: "second", status: TaskCompleted, branch: "task-branch-2", mergeState: MergeStateMerged},
			)
			queued := h.addPRD("queued "+k.name, PRDApproved, k.pref, k.backend)

			h.poll()
			st := h.status()

			// The finished Automaton is waiting, with its reason.
			w := waitingOcc(t, st, finished.ID)
			if w.ReasonCode != LaneOccWaitingReviewMerge {
				t.Fatalf("reason code = %q, want %q", w.ReasonCode, LaneOccWaitingReviewMerge)
			}
			if !strings.Contains(w.WaitReason, "completion review") {
				t.Fatalf("wait reason %q does not name the completion review", w.WaitReason)
			}
			if w.Title != "finished "+k.name || w.Backend != k.backend {
				t.Fatalf("waiting entry = %+v", w)
			}
			if idleAutoIsWaiting(st, queued.ID) {
				t.Fatalf("the queued Automaton is listed as waiting: %+v", st.Waiting)
			}

			// Its lane is idle, not held by it.
			lane := idleAutoLane(t, st, k.laneID)
			if lane.State != "idle" || lane.Occupant != nil {
				t.Fatalf("lane %s = state %q occupant %+v, want idle and empty", k.laneID, lane.State, lane.Occupant)
			}

			// The eligible queued Automaton is proposed for that lane.
			props := idleAutoStartDecisions(st, queued.ID)
			if len(props) != 1 {
				t.Fatalf("want one proposal for the queued Automaton, got %+v", st.Decisions)
			}
			d := props[0]
			if d.LaneID != k.laneID || d.LaneType != k.laneTyp {
				t.Fatalf("proposal is for lane %s/%s, want %s/%s: %+v", d.LaneType, d.LaneID, k.laneTyp, k.laneID, d)
			}
			if d.Mode != LaneFillModeShadow || d.Executed {
				t.Fatalf("a shadow proposal must not execute: %+v", d)
			}
			// The finished Automaton is never proposed a second run.
			if got := idleAutoStartDecisions(st, finished.ID); len(got) != 0 {
				t.Fatalf("the finished Automaton was proposed again: %+v", got)
			}
			if calls := h.fake.all(); len(calls) != 0 {
				t.Fatalf("shadow mode made starter calls: %v", calls)
			}

			// On mode starts the queued Automaton on the freed lane and still
			// leaves the finished one alone.
			h.setMode("on")
			h.now = h.now.Add(time.Minute)
			h.poll()
			calls := h.fake.all()
			if !containsString(calls, "run:"+queued.ID) {
				t.Fatalf("on mode did not start the queued Automaton: %v", calls)
			}
			for _, c := range calls {
				if strings.HasSuffix(c, finished.ID) {
					t.Fatalf("on mode touched the finished Automaton: %v", calls)
				}
			}
			st = h.status()
			if !idleAutoIsWaiting(st, finished.ID) {
				t.Fatalf("the finished Automaton left the waiting list: %+v", st.Waiting)
			}
			started := false
			for _, d := range idleAutoStartDecisions(st, queued.ID) {
				if d.Executed && d.LaneID == k.laneID {
					started = true
				}
			}
			if !started {
				t.Fatalf("no executed start decision on lane %s: %+v", k.laneID, st.Decisions)
			}
		})
	}
}

// TestLaneFillStalledMergeFreesLaneAndAlerts: an Automaton whose first task
// completed but whose task-branch merge stays pending past stalled_merge_minutes
// loses its lane once the fake clock passes the threshold: it is reported
// waiting with a merge reason, exactly one stalled-merge alert is raised, and a
// retry is attempted. A pending merge younger than the threshold still holds
// its lane, and no repeat poll alerts or retries again.
func TestLaneFillStalledMergeFreesLaneAndAlerts(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	sink := &laneFillAlertSink{}
	h.c.alertFn = sink.add
	h.c.SetReady()
	merger := &stallMerger{m: h.m} // the retry finishes nothing, so the merge stays pending
	h.c.lf.stall.merger = merger

	cfg := h.m.Config()
	threshold := 30
	cfg.LaneFill.StalledMergeMinutes = &threshold
	h.m.SetConfig(cfg)

	stalled := h.addPRD("stalled merge", PRDRunning, "claude", "claude-code")
	tasks := idleAutoAddTasks(t, h, stalled.ID,
		idleAutoTask{title: "first task", status: TaskCompleted, branch: "task-branch", mergeState: MergeStatePending},
		// The rest is not eligible: each waits on the one before it, and the
		// first one's branch is not merged yet.
		idleAutoTask{title: "second task", status: TaskQueued, dependsOn: []int{0}},
		idleAutoTask{title: "third task", status: TaskQueued, dependsOn: []int{1}},
	)
	ready := h.addPRD("ready for the freed lane", PRDApproved, "claude", "claude-code")

	stallAlerts := func() []laneFillAlertRecord {
		var out []laneFillAlertRecord
		for _, a := range sink.all() {
			if strings.Contains(a.title, "Task merge stalled") {
				out = append(out, a)
			}
		}
		return out
	}

	// First sight: the merge is as young as it can be, the lane is held and
	// nothing is proposed for it.
	h.poll()
	st := h.status()
	if idleAutoIsWaiting(st, stalled.ID) {
		t.Fatalf("a fresh pending merge freed the lane: %+v", st.Waiting)
	}
	lane := idleAutoLane(t, st, "claude-1")
	if lane.State != "occupied" || lane.Occupant == nil || lane.Occupant.PRDID != stalled.ID {
		t.Fatalf("claude-1 = %+v, want held by the Automaton with the pending merge", lane)
	}

	// Just inside the threshold: still held, no alert, no retry, nothing
	// started on the lane.
	h.now = h.base.Add(29 * time.Minute)
	h.poll()
	st = h.status()
	if idleAutoIsWaiting(st, stalled.ID) {
		t.Fatalf("a pending merge younger than the threshold freed the lane: %+v", st.Waiting)
	}
	if lane := idleAutoLane(t, st, "claude-1"); lane.Occupant == nil || lane.Occupant.PRDID != stalled.ID {
		t.Fatalf("claude-1 = %+v, want still held at 29 minutes", lane)
	}
	if got := stallAlerts(); len(got) != 0 {
		t.Fatalf("alerted before the threshold: %+v", got)
	}
	if merger.count() != 0 {
		t.Fatalf("retried before the threshold: %v", merger.calls)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("started work while the lane was held: %v", calls)
	}

	// Another Automaton's merge first seen now is young when the first one is
	// stalled, and keeps its own lane.
	young := h.addPRD("young merge", PRDRunning, "gpu", "pi-prompt")
	idleAutoAddTasks(t, h, young.ID,
		idleAutoTask{title: "young first", status: TaskCompleted, branch: "young-branch", mergeState: MergeStatePending},
		idleAutoTask{title: "young second", status: TaskQueued, dependsOn: []int{0}},
	)
	h.poll()

	// Past the threshold: the lane is freed.
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	st = h.status()

	w := waitingOcc(t, st, stalled.ID)
	if w.ReasonCode != LaneOccMergeStalled {
		t.Fatalf("reason code = %q, want %q", w.ReasonCode, LaneOccMergeStalled)
	}
	for _, want := range []string{tasks[0].ID, "merge", "threshold 30 min"} {
		if !strings.Contains(w.WaitReason, want) {
			t.Fatalf("wait reason %q does not name %q", w.WaitReason, want)
		}
	}

	// The younger pending merge still holds its lane and is not waiting.
	if idleAutoIsWaiting(st, young.ID) {
		t.Fatalf("a pending merge younger than the threshold is listed as waiting: %+v", st.Waiting)
	}
	if lane := idleAutoLane(t, st, "gpu-1"); lane.State != "occupied" || lane.Occupant == nil || lane.Occupant.PRDID != young.ID {
		t.Fatalf("gpu-1 = %+v, want held by the Automaton with the young pending merge", lane)
	}

	// The freed lane was given to the ready Automaton, and the stalled one was
	// never started a second time.
	if !containsString(h.fake.all(), "run:"+ready.ID) {
		t.Fatalf("the freed lane was not filled: %v", h.fake.all())
	}
	if got := idleAutoStartDecisions(st, stalled.ID); len(got) != 0 {
		t.Fatalf("the stalled Automaton was proposed a second run: %+v", got)
	}
	freed := false
	for _, d := range idleAutoStartDecisions(st, ready.ID) {
		if d.LaneID == "claude-1" && d.Executed {
			freed = true
		}
	}
	if !freed {
		t.Fatalf("no executed start on the freed lane claude-1: %+v", st.Decisions)
	}

	// Exactly one alert, naming the Automaton and the task, and one retry of
	// the stalled Automaton's merges.
	alerts := stallAlerts()
	if len(alerts) != 1 {
		t.Fatalf("want exactly one stalled-merge alert, got %+v", alerts)
	}
	if alerts[0].level != "warn" {
		t.Fatalf("alert level = %q, want warn", alerts[0].level)
	}
	for _, want := range []string{"stalled merge", "first task", "31m0s", "30-minute"} {
		if !strings.Contains(alerts[0].body, want) {
			t.Fatalf("alert body %q does not name %q", alerts[0].body, want)
		}
	}
	if strings.Contains(alerts[0].body, "young") {
		t.Fatalf("the alert names the young Automaton: %q", alerts[0].body)
	}
	if merger.count() != 1 || merger.calls[0] != stalled.ID {
		t.Fatalf("want one retry of %s, got %v", stalled.ID, merger.calls)
	}
	if len(st.Alerts) != 1 || !strings.Contains(st.Alerts[0].Message, "first task") {
		t.Fatalf("the alert is not in the lane fill status: %+v", st.Alerts)
	}

	// Later polls of the same episode neither alert nor retry again inside
	// the throttle window.
	h.now = h.now.Add(2 * time.Minute)
	h.poll()
	if got := stallAlerts(); len(got) != 1 {
		t.Fatalf("alerted again in the same episode: %+v", got)
	}
	if merger.count() != 1 {
		t.Fatalf("retried again inside the throttle window: %v", merger.calls)
	}
	if !idleAutoIsWaiting(h.status(), stalled.ID) {
		t.Fatalf("the stalled Automaton stopped waiting without a merge")
	}
}

// TestLaneFillLiveSessionHoldsLane: an Automaton with a live worker, verifier
// or judge session, or with a task actually starting, still occupies its lane
// and is never listed as waiting, however far the fake clock moves. Nothing is
// proposed for the held lane.
func TestLaneFillLiveSessionHoldsLane(t *testing.T) {
	type scenario struct {
		name  string
		tasks []idleAutoTask
		// lease, when set, is a ledger holder the Automaton's lane depends on;
		// it is built from the task ids (and the Automaton id) of the run.
		lease func(tasks []*Task, prdID string) string
	}
	scenarios := []scenario{
		{
			name:  "worker session",
			tasks: []idleAutoTask{{status: TaskInProgress, sessionID: "worker-session"}},
		},
		{
			name:  "tests running",
			tasks: []idleAutoTask{{status: TaskRunningTests, sessionID: "worker-session"}},
		},
		{
			name:  "verifier session",
			tasks: []idleAutoTask{{status: TaskVerifying, verifierSessionID: "verifier-session"}},
		},
		{
			name:  "verifier lease on settled work",
			tasks: []idleAutoTask{{status: TaskCompleted, branch: "b", mergeState: MergeStateMerged}},
			lease: func(tasks []*Task, _ string) string { return VerifyHolder(tasks[0].ID) },
		},
		{
			name:  "judge lease on a task",
			tasks: []idleAutoTask{{status: TaskCompleted, branch: "b", mergeState: MergeStateMerged}},
			lease: func(tasks []*Task, _ string) string { return JudgeHolder(tasks[0].ID) },
		},
		{
			name:  "judge lease on the Automaton",
			tasks: []idleAutoTask{{status: TaskCompleted, branch: "b", mergeState: MergeStateMerged}},
			lease: func(_ []*Task, prdID string) string { return JudgeHolder(prdID) },
		},
		{
			name:  "task waiting for its capacity slot",
			tasks: []idleAutoTask{{status: TaskWaitingCapacity}},
		},
		{
			name:  "task pending its start",
			tasks: []idleAutoTask{{status: TaskPending}},
		},
		{
			name: "ready task right behind a merged one",
			tasks: []idleAutoTask{
				{title: "done", status: TaskCompleted, branch: "b", mergeState: MergeStateMerged},
				{title: "next", status: TaskQueued, dependsOn: []int{0}},
			},
		},
		{
			name:  "Automaton with no task yet is starting",
			tasks: nil,
		},
	}

	for _, k := range idleAutoKinds {
		for _, sc := range scenarios {
			k, sc := k, sc
			t.Run(k.name+"/"+sc.name, func(t *testing.T) {
				h := newLaneFillHarness(t, "shadow")
				h.c.SetReady()
				led := idleAutoLedger(t, h)

				live := h.addPRD("live "+k.name, PRDRunning, k.pref, k.backend)
				var tasks []*Task
				if len(sc.tasks) > 0 {
					tasks = idleAutoAddTasks(t, h, live.ID, sc.tasks...)
				}
				if sc.lease != nil {
					idleAutoHold(t, led, sc.lease(tasks, live.ID), live.ID)
				}
				// Eligible work of the same lane type is queued behind it.
				queued := h.addPRD("queued "+k.name, PRDApproved, k.pref, k.backend)

				// One poll now, one three hours later on the fake clock: a live
				// session or a task starting never expires into an idle lane.
				for _, at := range []time.Duration{0, 3 * time.Hour} {
					h.now = h.base.Add(at)
					h.poll()
					st := h.status()

					if len(st.Waiting) != 0 {
						t.Fatalf("at +%s: an Automaton with live work is listed as waiting: %+v", at, st.Waiting)
					}
					lane := idleAutoLane(t, st, k.laneID)
					if lane.State != "occupied" || lane.Occupant == nil || lane.Occupant.PRDID != live.ID {
						t.Fatalf("at +%s: lane %s = %+v, want occupied by the Automaton with live work", at, k.laneID, lane)
					}
					if got := idleAutoStartDecisions(st, queued.ID); len(got) != 0 {
						t.Fatalf("at +%s: the held lane was offered to queued work: %+v", at, got)
					}
					if calls := h.fake.all(); len(calls) != 0 {
						t.Fatalf("at +%s: starter calls = %v, want none", at, calls)
					}
				}
			})
		}
	}
}
