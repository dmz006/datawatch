package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests cover the stalled task-branch merge pass (task_merge_stall.go)
// with the lane fill harness's fake clock and a fake merger: no real git runs.

// stallMerger is a fake taskMerger. It records every retry and can finish the
// merge of every pending task in the Automaton it is given.
type stallMerger struct {
	mu      sync.Mutex
	m       *Manager
	calls   []string
	succeed bool
	err     error
}

func (f *stallMerger) MergeCompletedTasks(_ context.Context, prd *PRD) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, prd.ID)
	if f.err != nil {
		return 0, f.err
	}
	if !f.succeed {
		return 0, nil
	}
	n := 0
	for _, t := range laneOccTasks(prd) {
		if laneOccMergePending(t) {
			f.m.store.MutateTask(t.ID, func(tk *Task) { tk.MergeState = MergeStateMerged })
			n++
		}
	}
	return n, nil
}

func (f *stallMerger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// stallHarness is an on-mode lane fill harness with a running Automaton whose
// one task is completed on its own branch with its merge pending.
type stallHarness struct {
	*laneFillHarness
	sink   *laneFillAlertSink
	merger *stallMerger
	prd    *PRD
	task   *Task
}

func newStallHarness(t *testing.T, mode string) *stallHarness {
	t.Helper()
	h := newLaneFillHarness(t, mode)
	sink := &laneFillAlertSink{}
	h.c.alertFn = sink.add
	h.c.SetReady()
	merger := &stallMerger{m: h.m}
	h.c.lf.stall.merger = merger
	p := h.addPRD("stalled automaton", PRDRunning, "claude", "claude-code")
	task := idleOccTask(t, h.m, p.ID, TaskCompleted, "task-branch", MergeStatePending)
	h.m.store.MutateTask(task.ID, func(tk *Task) { tk.Title = "write the thing" })
	return &stallHarness{laneFillHarness: h, sink: sink, merger: merger, prd: p, task: task}
}

// stallAlerts returns the alerts about stalled merges.
func (h *stallHarness) stallAlerts() []laneFillAlertRecord {
	var out []laneFillAlertRecord
	for _, a := range h.sink.all() {
		if strings.Contains(a.title, "Task merge stalled") {
			out = append(out, a)
		}
	}
	return out
}

func TestTaskMergeStallAlertOnceAndRetry(t *testing.T) {
	h := newStallHarness(t, "on")
	h.poll() // first sight of the pending merge: age 0
	if got := h.stallAlerts(); len(got) != 0 {
		t.Fatalf("alerted on a fresh merge: %+v", got)
	}
	if h.merger.count() != 0 {
		t.Fatalf("retried a fresh merge")
	}

	h.now = h.base.Add(31 * time.Minute) // past the default 30 minutes
	h.poll()
	got := h.stallAlerts()
	if len(got) != 1 {
		t.Fatalf("want one alert at the threshold, got %+v", got)
	}
	body := got[0].body
	for _, want := range []string{"stalled automaton", "write the thing", "31m0s", "Retry ran"} {
		if !strings.Contains(body, want) {
			t.Fatalf("alert body %q does not name %q", body, want)
		}
	}
	if h.merger.count() != 1 {
		t.Fatalf("want one retry, got %d", h.merger.count())
	}
	if h.merger.calls[0] != h.prd.ID {
		t.Fatalf("retried %q, want %q", h.merger.calls[0], h.prd.ID)
	}
	if st := h.status(); len(st.Alerts) != 1 || !strings.Contains(st.Alerts[0].Message, "write the thing") {
		t.Fatalf("the alert is not in the lane fill status: %+v", st.Alerts)
	}

	// Still stalled on later polls: no second alert in the episode.
	h.now = h.now.Add(2 * time.Minute)
	h.poll()
	h.now = h.now.Add(20 * time.Minute)
	h.poll()
	if got := h.stallAlerts(); len(got) != 1 {
		t.Fatalf("alerted more than once in one episode: %+v", got)
	}
}

func TestTaskMergeStallRetryIsThrottled(t *testing.T) {
	h := newStallHarness(t, "on")
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	h.now = h.now.Add(time.Minute)
	h.poll()
	if h.merger.count() != 1 {
		t.Fatalf("retried again within the throttle window: %d calls", h.merger.count())
	}
	h.now = h.now.Add(taskMergeStallRetryEvery)
	h.poll()
	if h.merger.count() != 2 {
		t.Fatalf("want a second retry after the throttle window, got %d", h.merger.count())
	}
}

func TestTaskMergeStallRetryErrorIsInTheAlert(t *testing.T) {
	h := newStallHarness(t, "on")
	h.merger.err = errors.New("git exploded")
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	got := h.stallAlerts()
	if len(got) != 1 || !strings.Contains(got[0].body, "git exploded") {
		t.Fatalf("alert does not carry the retry error: %+v", got)
	}
}

func TestTaskMergeStallNoRetryForOperatorHeldMerge(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hold  func(tk *Task)
		prdSt PRDStatus
	}{
		{"merge block", func(tk *Task) {
			tk.MergeBlock = &MergeBlock{Kind: MergeBlockKindDirtyWorktree, Reason: "the worktree has uncommitted edits", Since: time.Time{}}
		}, PRDRunning},
		{"held task", func(tk *Task) { tk.MergeState = MergeStateHeld }, PRDRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newStallHarness(t, "on")
			h.m.store.MutateTask(h.task.ID, tc.hold)
			// A held-state task is not pending by state, so the merge block
			// case is the one that can alert; the held-state case must stay
			// silent and unretried.
			h.poll()
			h.now = h.base.Add(31 * time.Minute)
			h.poll()
			if h.merger.count() != 0 {
				t.Fatalf("retried an operator-held merge: %v", h.merger.calls)
			}
			alerts := h.stallAlerts()
			if tc.name == "merge block" {
				if len(alerts) != 1 {
					t.Fatalf("want one alert for the held merge, got %+v", alerts)
				}
				if !strings.Contains(alerts[0].body, "held for the operator") {
					t.Fatalf("alert does not say the merge is held: %q", alerts[0].body)
				}
			} else if len(alerts) != 0 {
				t.Fatalf("a held task is not a pending merge, got %+v", alerts)
			}
		})
	}
}

func TestTaskMergeStallStateClearedAfterSuccessfulMerge(t *testing.T) {
	h := newStallHarness(t, "on")
	h.merger.succeed = true
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	got := h.stallAlerts()
	if len(got) != 1 || !strings.Contains(got[0].body, "merged 1 task") {
		t.Fatalf("want one alert carrying the retry success, got %+v", got)
	}
	h.c.lf.mu.Lock()
	_, tracked := h.c.lf.mergePendingSince[h.task.ID]
	h.c.lf.mu.Unlock()
	h.c.lf.stall.mu.Lock()
	alerted, retried := h.c.lf.stall.alerted[h.task.ID], len(h.c.lf.stall.retryAt)
	h.c.lf.stall.mu.Unlock()
	if tracked || alerted || retried != 0 {
		t.Fatalf("state not cleared after the merge: tracked=%v alerted=%v retryAt=%d", tracked, alerted, retried)
	}

	// A later stall of the same task is a new episode and alerts again.
	h.m.store.MutateTask(h.task.ID, func(tk *Task) { tk.MergeState = MergeStatePending })
	h.merger.succeed = false
	h.poll()
	h.now = h.now.Add(31 * time.Minute)
	h.poll()
	if got := h.stallAlerts(); len(got) != 2 {
		t.Fatalf("a new episode should alert again, got %d alerts", len(got))
	}
}

func TestTaskMergeStallStateClearedWhenAutomatonEnds(t *testing.T) {
	h := newStallHarness(t, "on")
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	if len(h.stallAlerts()) != 1 {
		t.Fatal("setup: expected one alert")
	}
	p, _ := h.m.store.GetPRD(h.prd.ID)
	h.m.store.WithLock(func() { p.Status = PRDCompleted })
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Minute)
	h.poll()
	h.c.lf.mu.Lock()
	n := len(h.c.lf.mergePendingSince)
	h.c.lf.mu.Unlock()
	h.c.lf.stall.mu.Lock()
	a := len(h.c.lf.stall.alerted)
	h.c.lf.stall.mu.Unlock()
	if n != 0 || a != 0 {
		t.Fatalf("state kept after the Automaton ended: since=%d alerted=%d", n, a)
	}
}

func TestTaskMergeStallOffWhenThresholdZero(t *testing.T) {
	h := newStallHarness(t, "on")
	cfg := h.m.Config()
	zero := 0
	cfg.LaneFill.StalledMergeMinutes = &zero
	h.m.SetConfig(cfg)
	h.poll()
	h.now = h.base.Add(5 * time.Hour)
	h.poll()
	if got := h.stallAlerts(); len(got) != 0 {
		t.Fatalf("alerted with the threshold at 0: %+v", got)
	}
	if h.merger.count() != 0 {
		t.Fatal("retried with the threshold at 0")
	}
}

func TestTaskMergeStallOffMode(t *testing.T) {
	h := newStallHarness(t, "off")
	h.poll()
	h.now = h.base.Add(5 * time.Hour)
	h.poll()
	if len(h.stallAlerts()) != 0 || h.merger.count() != 0 {
		t.Fatalf("off mode did something: alerts=%d retries=%d", len(h.stallAlerts()), h.merger.count())
	}
	if d := h.c.m.LaneFillStatus(0).Decisions; len(d) != 0 {
		t.Fatalf("off mode recorded decisions: %+v", d)
	}
}

func TestTaskMergeStallShadowRecordsOnly(t *testing.T) {
	h := newStallHarness(t, "shadow")
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	h.now = h.now.Add(10 * time.Minute)
	h.poll()
	if got := h.stallAlerts(); len(got) != 0 {
		t.Fatalf("shadow mode alerted: %+v", got)
	}
	if h.merger.count() != 0 {
		t.Fatal("shadow mode retried a merge")
	}
	var found *LaneFillDecision
	for _, d := range h.status().Decisions {
		if d.Blocker == taskMergeStallBlocker && d.PRDID == h.prd.ID {
			d := d
			found = &d
		}
	}
	if found == nil {
		t.Fatalf("shadow mode did not record the stalled merge: %+v", h.status().Decisions)
	}
	if found.Executed || found.Mode != LaneFillModeShadow {
		t.Fatalf("shadow decision = %+v", *found)
	}
	if found.RepeatCount < 2 {
		t.Fatalf("repeats should fold into one record, repeat count = %d", found.RepeatCount)
	}
}

func TestTaskMergePendingAgeSharesTheClassifierClock(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	task := &Task{ID: "t1"}
	seen := map[string]time.Time{"t1": now.Add(-12 * time.Minute)}
	if got := taskMergePendingAge(task, seen, now); got != 12*time.Minute {
		t.Fatalf("age from the first-seen record = %v", got)
	}
	if got := taskMergePendingAge(task, nil, now); got != 0 {
		t.Fatalf("a merge seen for the first time is 0 old, got %v", got)
	}
	task.MergeBlock = &MergeBlock{Since: now.Add(-45 * time.Minute)}
	if got := taskMergePendingAge(task, seen, now); got != 45*time.Minute {
		t.Fatalf("the task's own merge block time wins, got %v", got)
	}
}

func TestTaskMergePendingKeepsOtherAutomataRecords(t *testing.T) {
	h := newStallHarness(t, "on")
	p2 := h.addPRD("second", PRDRunning, "claude", "claude-code")
	t2 := idleOccTask(t, h.m, p2.ID, TaskCompleted, "task-branch-2", MergeStatePending)
	h.poll()
	h.now = h.base.Add(31 * time.Minute)
	h.poll()
	if got := h.stallAlerts(); len(got) != 2 {
		t.Fatalf("both Automata's merges should have stalled, got %d alerts for %s and %s", len(got), h.task.ID, t2.ID)
	}
}
