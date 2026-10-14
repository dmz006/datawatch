// BL427 — task-level lifecycle events are emitted at every task
// Status / SessionID write site. Table-driven over the task-level
// transition table in docs/plans/2026-10-13-prd-lifecycle-events.md
// ("Event source").

package autonomous

import (
	"context"
	"testing"
)

// taskKinds returns the kinds of evs for taskID, in delivery order.
func taskKinds(evs []LifecycleEvent, taskID string) []string {
	var out []string
	for _, ev := range evs {
		if ev.Level == LevelTask && ev.TaskID == taskID {
			out = append(out, ev.Kind)
		}
	}
	return out
}

// isSubsequence reports whether want appears in got in order.
func isSubsequence(want, got []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

func hasKind(evs []LifecycleEvent, taskID, kind string) bool {
	for _, k := range taskKinds(evs, taskID) {
		if k == kind {
			return true
		}
	}
	return false
}

// emitTaskOf returns the first event of kind for taskID.
func emitTaskOf(evs []LifecycleEvent, taskID, kind string) (LifecycleEvent, bool) {
	for _, ev := range evs {
		if ev.Level == LevelTask && ev.TaskID == taskID && ev.Kind == kind {
			return ev, true
		}
	}
	return LifecycleEvent{}, false
}

var (
	kInProgress      = TaskStatusKind(TaskInProgress)
	kVerifying       = TaskStatusKind(TaskVerifying)
	kCompleted       = TaskStatusKind(TaskCompleted)
	kFailed          = TaskStatusKind(TaskFailed)
	kBlocked         = TaskStatusKind(TaskBlocked)
	kWaitingCapacity = TaskStatusKind(TaskWaitingCapacity)
	kCancelled       = TaskStatusKind(TaskCancelled)
	kPending         = TaskStatusKind(TaskPending)
	kAwaitApproval   = TaskStatusKind(TaskAwaitingApproval)
)

// emitTaskFixture is one Manager + single-task PRD under test.
type emitTaskFixture struct {
	m      *Manager
	prdID  string
	taskID string
}

func newEmitTaskFixture(t *testing.T, cfg Config, status PRDStatus, mutate func(*Task)) emitTaskFixture {
	t.Helper()
	m := newEmitManager(t, cfg, fakeDecompose)
	p := newEmitPRD(t, m, status)
	if mutate != nil {
		mutate(&p.Story[0].Tasks[0])
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	}
	return emitTaskFixture{m: m, prdID: p.ID, taskID: p.Story[0].Tasks[0].ID}
}

func TestLifecycleEmitTask_Transitions(t *testing.T) {
	cases := []struct {
		name string
		// setup builds the fixture; drive performs the transition(s).
		setup func(t *testing.T) (emitTaskFixture, func(f emitTaskFixture) error)
		// seq must appear in order in the task's event stream; its last
		// element is the event the remaining fields are checked against.
		seq      []string
		exact    bool // seq must be the whole stream, not a subsequence
		old      TaskStatus
		decision string // expected Decision.Kind on the last seq event
		verdict  bool   // expect a block verdict on the last seq event
		session  bool   // expect SessionID set on the last seq event
	}{
		{
			name:  "happy path InProgress → session_started → Verifying → Completed",
			seq:   []string{kInProgress, KindTaskSessionStarted, kVerifying, kCompleted},
			exact: true, old: TaskVerifying, session: true,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				return newEmitTaskFixture(t, DefaultConfig(), PRDApproved, nil), func(f emitTaskFixture) error {
					return f.m.Run(context.Background(), f.prdID, fakeSpawn, fakeVerify)
				}
			},
		},
		{
			name: "verify fail exhausts retries → Failed",
			seq:  []string{kInProgress, KindTaskSessionStarted, kVerifying, kFailed},
			old:  TaskVerifying,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				return newEmitTaskFixture(t, DefaultConfig(), PRDApproved, nil), func(f emitTaskFixture) error {
					return f.m.Run(context.Background(), f.prdID, fakeSpawn, failingVerify)
				}
			},
		},
		{
			name: "per-task guardrail block → Blocked",
			seq:  []string{kInProgress, KindTaskSessionStarted, kVerifying, kBlocked},
			old:  TaskVerifying, verdict: true,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				cfg := DefaultConfig()
				cfg.PerTaskGuardrails = []string{"security"}
				f := newEmitTaskFixture(t, cfg, PRDApproved, nil)
				f.m.SetGuardrail(fakeGuardrailBlocking)
				return f, func(f emitTaskFixture) error {
					return f.m.Run(context.Background(), f.prdID, fakeSpawn, fakeVerify)
				}
			},
		},
		{
			name: "setWaiting → WaitingCapacity",
			seq:  []string{kWaitingCapacity}, exact: true, old: TaskInProgress,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				f := newEmitTaskFixture(t, DefaultConfig(), PRDRunning, func(tk *Task) { tk.Status = TaskInProgress })
				return f, func(f emitTaskFixture) error {
					tk, _ := f.m.Store().GetTask(f.taskID)
					f.m.setWaiting(tk, "slots full")
					return nil
				}
			},
		},
		{
			name: "clearWaiting → InProgress",
			seq:  []string{kInProgress}, exact: true, old: TaskWaitingCapacity,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				f := newEmitTaskFixture(t, DefaultConfig(), PRDRunning, func(tk *Task) { tk.Status = TaskWaitingCapacity })
				return f, func(f emitTaskFixture) error {
					tk, _ := f.m.Store().GetTask(f.taskID)
					f.m.clearWaiting(tk)
					return nil
				}
			},
		},
		{
			name: "CancelTask → Cancelled",
			seq:  []string{kCancelled}, exact: true, old: TaskInProgress, decision: "cancel_task",
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				f := newEmitTaskFixture(t, DefaultConfig(), PRDRunning, func(tk *Task) { tk.Status = TaskInProgress })
				return f, func(f emitTaskFixture) error {
					_, err := f.m.CancelTask(f.prdID, f.taskID, "op", "stop")
					return err
				}
			},
		},
		{
			name: "CancelStory → Cancelled",
			seq:  []string{kCancelled}, exact: true, old: TaskPending, decision: "cancel_story",
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				f := newEmitTaskFixture(t, DefaultConfig(), PRDRunning, func(tk *Task) { tk.Status = TaskPending })
				return f, func(f emitTaskFixture) error {
					p, _ := f.m.Store().GetPRD(f.prdID)
					_, err := f.m.CancelStory(f.prdID, p.Story[0].ID, "op", "stop")
					return err
				}
			},
		},
		{
			name: "ResetTask → Pending + session_cleared",
			seq:  []string{kPending, KindTaskSessionCleared}, exact: true, old: TaskFailed,
			decision: "reset_task", session: true,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				f := newEmitTaskFixture(t, DefaultConfig(), PRDFailed, func(tk *Task) {
					tk.Status = TaskFailed
					tk.SessionID = "ses_old"
				})
				return f, func(f emitTaskFixture) error {
					_, err := f.m.ResetTask(f.prdID, f.taskID, "op", false)
					return err
				}
			},
		},
		{
			name: "boot reset-in-progress → Pending + session_cleared",
			seq:  []string{kPending, KindTaskSessionCleared}, exact: true, old: TaskInProgress, session: true,
			setup: func(t *testing.T) (emitTaskFixture, func(emitTaskFixture) error) {
				f := newEmitTaskFixture(t, DefaultConfig(), PRDRunning, func(tk *Task) {
					tk.Status = TaskInProgress
					tk.SessionID = "ses_dead"
				})
				return f, func(f emitTaskFixture) error {
					p, _ := f.m.Store().GetPRD(f.prdID)
					f.m.resetInProgressTasksForResume(p)
					return nil
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, drive := tc.setup(t)
			c := &collector{}
			defer f.m.Subscribe(LifecycleFilter{Level: LevelTask, PRDID: f.prdID}, c.add)()

			if err := drive(f); err != nil {
				t.Fatalf("drive: %v", err)
			}
			last := tc.seq[len(tc.seq)-1]
			waitFor(t, "event "+last, func() bool { return hasKind(c.snapshot(), f.taskID, last) })
			evs := c.snapshot()
			got := taskKinds(evs, f.taskID)
			if tc.exact {
				if len(got) != len(tc.seq) || !isSubsequence(tc.seq, got) {
					t.Fatalf("kinds = %v, want exactly %v", got, tc.seq)
				}
			} else if !isSubsequence(tc.seq, got) {
				t.Fatalf("kinds = %v, want subsequence %v", got, tc.seq)
			}
			// Status checks run against the last status-kind event in seq
			// (Decision / Verdict ride the status event, not the session one);
			// the SessionID check runs against seq's last event.
			statusKind := last
			for i := len(tc.seq) - 1; i >= 0; i-- {
				if k := tc.seq[i]; k != KindTaskSessionCleared && k != KindTaskSessionStarted {
					statusKind = k
					break
				}
			}
			ev, _ := emitTaskOf(evs, f.taskID, statusKind)
			if ev.Level != LevelTask || ev.PRDID != f.prdID || ev.StoryID == "" || ev.ID == "" || ev.At.IsZero() {
				t.Fatalf("event identity = %+v", ev)
			}
			if ev.OldStatus != string(tc.old) {
				t.Errorf("OldStatus = %q, want %q", ev.OldStatus, tc.old)
			}
			if TaskStatusKind(TaskStatus(ev.NewStatus)) != statusKind {
				t.Errorf("NewStatus = %q, want kind %s", ev.NewStatus, statusKind)
			}
			if lastEv, _ := emitTaskOf(evs, f.taskID, last); tc.session && lastEv.SessionID == "" {
				t.Errorf("SessionID empty on %s", last)
			}
			switch {
			case tc.decision == "" && ev.Decision != nil:
				t.Errorf("unexpected Decision %q", ev.Decision.Kind)
			case tc.decision != "" && (ev.Decision == nil || ev.Decision.Kind != tc.decision):
				t.Errorf("Decision = %+v, want kind %q", ev.Decision, tc.decision)
			}
			if tc.verdict && (ev.Verdict == nil || ev.Verdict.Outcome != "block") {
				t.Errorf("Verdict = %+v, want a block verdict", ev.Verdict)
			}
			if !tc.verdict && ev.Verdict != nil {
				t.Errorf("unexpected Verdict %+v", ev.Verdict)
			}
		})
	}
}

// task.session_started carries the spawned session's ID — the signal
// the design doc's motivating example 1 waits for.
func TestLifecycleEmitTask_SessionStartedCarriesSessionID(t *testing.T) {
	f := newEmitTaskFixture(t, DefaultConfig(), PRDApproved, nil)
	c := &collector{}
	defer f.m.Subscribe(LifecycleFilter{TaskID: f.taskID, Kinds: []string{KindTaskSessionStarted}}, c.add)()
	if err := f.m.Run(context.Background(), f.prdID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitFor(t, "session_started", func() bool { return len(c.snapshot()) > 0 })
	ev := c.snapshot()[0]
	if ev.SessionID != "ses_"+f.taskID || ev.NewStatus != string(TaskInProgress) {
		t.Fatalf("session_started = %+v, want session ses_%s while in_progress", ev, f.taskID)
	}
}

// A verify-fail retry clears the previous attempt's session before the
// respawn and starts a fresh one.
func TestLifecycleEmitTask_RetryClearsSession(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1
	f := newEmitTaskFixture(t, cfg, PRDApproved, nil)
	c := &collector{}
	defer f.m.Subscribe(LifecycleFilter{TaskID: f.taskID}, c.add)()
	if err := f.m.Run(context.Background(), f.prdID, fakeSpawn, failingVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitFor(t, "task.failed", func() bool { return hasKind(c.snapshot(), f.taskID, kFailed) })
	got := taskKinds(c.snapshot(), f.taskID)
	want := []string{KindTaskSessionStarted, kVerifying, KindTaskSessionCleared, kInProgress, KindTaskSessionStarted, kFailed}
	if !isSubsequence(want, got) {
		t.Fatalf("kinds = %v, want subsequence %v", got, want)
	}
	if got[len(got)-1] != kFailed {
		t.Fatalf("last kind = %s, want %s (kinds %v)", got[len(got)-1], kFailed, got)
	}
}

// Approve under operator guided mode gates pending tasks to
// awaiting_approval; ApproveTask releases them back to pending.
func TestLifecycleEmitTask_GuidedApproval(t *testing.T) {
	f := newEmitTaskFixture(t, DefaultConfig(), PRDNeedsReview, nil)
	p, _ := f.m.Store().GetPRD(f.prdID)
	p.GuidedMode = true
	p.GuidedModeSource = GuidedModeOperator
	if err := f.m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if f.m.resolveGuidedModeSource(p) != GuidedModeOperator {
		t.Skip("guided-mode operator source not resolvable from PRD fields")
	}
	c := &collector{}
	defer f.m.Subscribe(LifecycleFilter{TaskID: f.taskID}, c.add)()
	if _, err := f.m.Approve(f.prdID, "op", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := f.m.ApproveTask(f.prdID, f.taskID, "op"); err != nil {
		t.Fatalf("ApproveTask: %v", err)
	}
	waitFor(t, "task.pending", func() bool { return hasKind(c.snapshot(), f.taskID, kPending) })
	got := taskKinds(c.snapshot(), f.taskID)
	if want := []string{kAwaitApproval, kPending}; len(got) != 2 || !isSubsequence(want, got) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	ev, _ := emitTaskOf(c.snapshot(), f.taskID, kPending)
	if ev.Decision == nil || ev.Decision.Kind != "approve_task" {
		t.Fatalf("Decision = %+v, want approve_task", ev.Decision)
	}
}

// Writes that leave the status unchanged emit nothing.
func TestLifecycleEmitTask_NoEventWhenUnchanged(t *testing.T) {
	m := &Manager{}
	c := &collector{}
	defer m.Subscribe(LifecycleFilter{}, c.add)()
	tk := &Task{ID: "t1", PRDID: "p1", Status: TaskFailed}
	m.emitTaskStatus(nil, tk, TaskFailed, nil, nil)
	m.emitTaskSession(nil, tk, "")
	tk.Status = TaskPending
	m.emitTaskStatus(nil, tk, "", nil, nil) // "" normalises to pending
	m.emitTaskStatus(nil, tk, TaskFailed, nil, nil)
	waitFor(t, "sentinel", func() bool { return len(c.snapshot()) > 0 })
	if evs := c.snapshot(); len(evs) != 1 || evs[0].Kind != kPending || evs[0].PRDID != "p1" {
		t.Fatalf("events = %+v, want one task.pending for p1", evs)
	}
}
