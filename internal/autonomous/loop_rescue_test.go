// loop_rescue_test.go — the loop rescue the executor's periodic progress
// poll applies once DetectLoop (loop_detect.go) finds a loop in the worker
// session's log:
//
//  1. record a "looping" warning on the task and emit a task.looping event;
//  2. input-capable sessions get exactly one corrective message (naming the
//     announced action and directing the worker to call the write tool);
//  3. one-shot print sessions are stopped, and the task is routed through
//     the existing retry path with the finding's hint (naming the action);
//  4. a distinct finding fires at most once per session.

package autonomous

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/pilog"
)

// fakeLoopSession records what the rescue did.
type fakeLoopSession struct {
	id      string
	input   bool
	msgs    []string // corrective messages sent (in order)
	stops   int      // times Stop was called
	sendErr error    // injected on SendInput
}

func (f *fakeLoopSession) ID() string         { return f.id }
func (f *fakeLoopSession) AcceptsInput() bool { return f.input }
func (f *fakeLoopSession) SendInput(text string) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.msgs = append(f.msgs, text)
	return nil
}
func (f *fakeLoopSession) Stop() error {
	f.stops++
	return nil
}

func loopingEvents(evs []LifecycleEvent, taskID string) []LifecycleEvent {
	var out []LifecycleEvent
	for _, ev := range evs {
		if ev.Level == LevelTask && ev.TaskID == taskID && ev.Kind == KindTaskLooping {
			out = append(out, ev)
		}
	}
	return out
}

// newRescueFixture builds a single-task PRD whose task is in_progress —
// the state the executor is in while its periodic poll can find a loop —
// since the store only keeps a task's Progress/LoopingWarning while that
// status is progress-active.
func newRescueFixture(t *testing.T) emitTaskFixture {
	t.Helper()
	return newEmitTaskFixture(t, DefaultConfig(), PRDApproved, func(tk *Task) { tk.Status = TaskInProgress })
}

func TestRescueLoop_InputCapable_OneCorrectiveMessage(t *testing.T) {
	fx := newRescueFixture(t)
	defer fx.m.lifecycleBus().Close()
	c := &collector{}
	fx.m.Subscribe(LifecycleFilter{}, c.add)

	sess := &fakeLoopSession{id: "host-in1", input: true}
	finding := LoopFinding{
		Kind:   KindAnnounce,
		Action: "create the test file",
		Hint:   "call the write tool and create the file now",
	}

	action, err := fx.m.RescueLoop(fx.taskID, sess, finding)
	if err != nil {
		t.Fatalf("RescueLoop: %v", err)
	}
	if action != LoopRescueCorrected {
		t.Fatalf("action = %q, want %q", action, LoopRescueCorrected)
	}
	if len(sess.msgs) != 1 {
		t.Fatalf("got %d corrective messages, want exactly 1", len(sess.msgs))
	}
	msg := sess.msgs[0]
	if !strings.Contains(msg, "create the test file") {
		t.Errorf("corrective message %q does not name the announced action", msg)
	}
	if !strings.Contains(msg, "write") {
		t.Errorf("corrective message %q does not direct the worker to call the write tool", msg)
	}

	// The "looping" warning is recorded on the task.
	tk, ok := fx.m.Store().GetTask(fx.taskID)
	if !ok {
		t.Fatal("task vanished")
	}
	if tk.LoopingWarning != finding.Warning() {
		t.Errorf("task.LoopingWarning = %q, want %q", tk.LoopingWarning, finding.Warning())
	}
	if tk.LoopingWarning == "" || !strings.Contains(tk.LoopingWarning, "looping") {
		t.Errorf("task.LoopingWarning = %q, want a looping warning naming the action", tk.LoopingWarning)
	}

	// One task.looping event, with the warning as detail.
	waitFor(t, "task.looping event", func() bool { return len(loopingEvents(c.snapshot(), fx.taskID)) == 1 })
	evs := loopingEvents(c.snapshot(), fx.taskID)
	if evs[0].Details != finding.Warning() {
		t.Errorf("event details = %q, want %q", evs[0].Details, finding.Warning())
	}
	if evs[0].Level != LevelTask || evs[0].TaskID != fx.taskID {
		t.Errorf("event level/task = %s/%s, want task level / %s", evs[0].Level, evs[0].TaskID, fx.taskID)
	}

	// Same finding again on a later poll: fires-once → skipped, no second
	// message, no second event.
	action, err = fx.m.RescueLoop(fx.taskID, sess, finding)
	if err != nil {
		t.Fatalf("RescueLoop (repeat): %v", err)
	}
	if action != LoopRescueSkipped {
		t.Fatalf("repeat action = %q, want %q", action, LoopRescueSkipped)
	}
	if len(sess.msgs) != 1 {
		t.Fatalf("got %d corrective messages after repeat fire, want exactly 1", len(sess.msgs))
	}
	if n := len(loopingEvents(c.snapshot(), fx.taskID)); n != 1 {
		t.Fatalf("got %d task.looping events after repeat fire, want 1", n)
	}

	// A different finding (different action) is a new finding: it fires on
	// its own with one more corrective message.
	finding2 := finding
	finding2.Action = "update the migration script"
	action, err = fx.m.RescueLoop(fx.taskID, sess, finding2)
	if err != nil {
		t.Fatalf("RescueLoop (distinct finding): %v", err)
	}
	if action != LoopRescueCorrected {
		t.Fatalf("distinct-finding action = %q, want %q", action, LoopRescueCorrected)
	}
	if len(sess.msgs) != 2 {
		t.Fatalf("got %d corrective messages after distinct finding, want 2", len(sess.msgs))
	}
	if !strings.Contains(sess.msgs[1], "update the migration script") {
		t.Errorf("second corrective message %q does not name its action", sess.msgs[1])
	}
}

func TestRescueLoop_OneShot_StopAndRetryWithHint(t *testing.T) {
	fx := newRescueFixture(t)
	defer fx.m.lifecycleBus().Close()

	sess := &fakeLoopSession{id: "host-p1", input: false} // one-shot print backend
	finding := LoopFinding{
		Kind:   KindRepeat,
		Action: "read",
		Hint:   "stop re-reading models.go; call the write tool now and create the file",
	}

	action, err := fx.m.RescueLoop(fx.taskID, sess, finding)
	if err != nil {
		t.Fatalf("RescueLoop: %v", err)
	}
	if action != LoopRescueStopped {
		t.Fatalf("action = %q, want %q", action, LoopRescueStopped)
	}
	if sess.stops != 1 {
		t.Fatalf("Stop called %d times, want exactly 1", sess.stops)
	}
	if len(sess.msgs) != 0 {
		t.Fatalf("sent %d corrective messages to a one-shot session, want 0", len(sess.msgs))
	}

	// Retry routing: LoopRetryError wraps ErrWorkerStalled (the executor's
	// stall-retry trigger) and its message carries the hint naming the
	// action — the executor appends that to the task spec on retry.
	re := LoopRetryError(finding)
	if !errors.Is(re, ErrWorkerStalled) {
		t.Errorf("LoopRetryError does not wrap ErrWorkerStalled: %v", re)
	}
	if !strings.Contains(re.Error(), "read") {
		t.Errorf("retry error %q does not name the looping action", re)
	}
	if !strings.Contains(re.Error(), "write tool") {
		t.Errorf("retry error %q does not carry the hint about the write tool", re)
	}

	// Same finding again on a later poll: fires-once → skipped, no second stop.
	action, err = fx.m.RescueLoop(fx.taskID, sess, finding)
	if err != nil {
		t.Fatalf("RescueLoop (repeat): %v", err)
	}
	if action != LoopRescueSkipped {
		t.Fatalf("repeat action = %q, want %q", action, LoopRescueSkipped)
	}
	if sess.stops != 1 {
		t.Fatalf("Stop called %d times after repeat fire, want 1", sess.stops)
	}
}

func TestRescueLoop_SkippedDoesNotRecordOrEmit(t *testing.T) {
	// Dedup is the whole point of "fires at most once per distinct
	// finding": skips must leave the task and the event stream untouched.
	fx := newRescueFixture(t)
	defer fx.m.lifecycleBus().Close()
	c := &collector{}
	cancel := fx.m.Subscribe(LifecycleFilter{}, c.add)
	defer cancel()

	sess := &fakeLoopSession{id: "host-s1", input: true}
	finding := LoopFinding{Kind: KindAnnounce, Action: "refactor the store", Hint: "act, don't restate"}

	// Nothing fired yet for this finding…
	noop := &fakeLoopSession{id: "other", input: true}
	if action, _ := fx.m.RescueLoop(fx.taskID, noop, LoopFinding{Kind: KindAnnounce, Action: "refactor the store"}); action != LoopRescueCorrected {
		t.Fatalf("first fire = %q, want corrected", action)
	}
	// …and a repeat for the original session's same key must skip…
	if action, _ := fx.m.RescueLoop(fx.taskID, noop, finding); action != LoopRescueSkipped {
		t.Fatalf("repeat = %q, want skipped", action)
	}
	// …while the distinct session's finding still fires (dedup is per session).
	if action, _ := fx.m.RescueLoop(fx.taskID, sess, finding); action != LoopRescueCorrected {
		t.Fatalf("other session fire = %q, want corrected", action)
	}
	if len(sess.msgs) != 1 {
		t.Fatalf("sess got %d messages, want 1", len(sess.msgs))
	}

	// No Manager / no session → clean skip, no state written.
	if action, err := fx.m.RescueLoop(fx.taskID, nil, finding); err != nil || action != LoopRescueSkipped {
		t.Fatalf("nil session: got %q, %v; want skipped, nil", action, err)
	}
	var nilMgr *Manager
	if action, err := nilMgr.RescueLoop(fx.taskID, sess, finding); err != nil || action != LoopRescueSkipped {
		t.Fatalf("nil manager: got %q, %v; want skipped, nil", action, err)
	}
}

func TestRescueLoop_DeliveryFailureRetriesNextPoll(t *testing.T) {
	// A failed corrective send must not be counted as fired, must not emit
	// the event, and must not record the warning — the next poll retries.
	fx := newRescueFixture(t)
	defer fx.m.lifecycleBus().Close()
	c := &collector{}
	fx.m.Subscribe(LifecycleFilter{}, c.add)

	sess := &fakeLoopSession{id: "host-f1", input: true, sendErr: errors.New("pane closed")}
	finding := LoopFinding{Kind: KindAnnounce, Action: "create the report", Hint: "call the write tool now"}

	action, err := fx.m.RescueLoop(fx.taskID, sess, finding)
	if err == nil {
		t.Fatal("expected an error when the corrective send fails")
	}
	if action != LoopRescueSkipped {
		t.Fatalf("action = %q, want skipped on failed delivery", action)
	}
	if len(loopingEvents(c.snapshot(), fx.taskID)) != 0 {
		t.Fatalf("task.looping event fired before successful delivery")
	}
	tk, _ := fx.m.Store().GetTask(fx.taskID)
	if tk.LoopingWarning != "" {
		t.Fatalf("warning recorded before successful delivery: %q", tk.LoopingWarning)
	}

	// Next poll: delivery succeeds → fires exactly once.
	sess.sendErr = nil
	action, err = fx.m.RescueLoop(fx.taskID, sess, finding)
	if err != nil {
		t.Fatalf("retry RescueLoop: %v", err)
	}
	if action != LoopRescueCorrected {
		t.Fatalf("retry action = %q, want corrected", action)
	}
	if len(sess.msgs) != 1 {
		t.Fatalf("got %d successful deliveries, want 1", len(sess.msgs))
	}
	waitFor(t, "task.looping event", func() bool { return len(loopingEvents(c.snapshot(), fx.taskID)) == 1 })

	// And the now-fired finding does not re-fire.
	if action, _ := fx.m.RescueLoop(fx.taskID, sess, finding); action != LoopRescueSkipped {
		t.Fatalf("after successful fire, repeat = %q, want skipped", action)
	}
	if len(sess.msgs) != 1 {
		t.Fatalf("got %d deliveries after final state, want 1", len(sess.msgs))
	}
}

// Detection off (n <= 0) or a worker that is not looping means no finding is
// reported and therefore no rescue — send, stop, record, event.
func TestRescueLoop_NoFindingNoRescue(t *testing.T) {
	busyTurns := []pilog.Turn{
		{Text: "let me read the file", ToolCalls: []pilog.ToolCall{{Name: "read", Args: `{"path":"a.go"}`}}},
		{Text: "and now the edit", ToolCalls: []pilog.ToolCall{{Name: "edit", Args: `{"path":"a.go"}`}}},
		{Text: "and now the build", ToolCalls: []pilog.ToolCall{{Name: "bash", Args: `{"command":"go build"}`}}},
	}

	// detection off (n=0).
	if _, ok := DetectLoop(busyTurns, 0); ok {
		t.Fatal("DetectLoop fired with detection disabled (n=0)")
	}
	// detection on, but the worker is legitimately acting.
	if f, ok := DetectLoop(busyTurns, 3); ok {
		t.Fatalf("DetectLoop fired on a non-looping worker: %+v", f)
	}
	// no turns at all: no finding.
	if _, ok := DetectLoop(nil, 3); ok {
		t.Fatal("DetectLoop fired with no turns")
	}
}
