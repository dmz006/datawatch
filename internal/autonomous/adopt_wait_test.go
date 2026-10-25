package autonomous

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// adoptedFixture is an adopted worker under WaitAdopted: a fake clock that
// Pause advances by one poll step, a fake session whose state, family and
// completion marker the test flips, and a fakeNudgeWorker for the nudge hooks.
// The guard in Pause stops a broken wait from spinning forever.
type adoptedFixture struct {
	clock  time.Time
	state  session.State
	family string
	marker bool
	worker *fakeNudgeWorker
	polls  int
}

const adoptedPollStep = 3 * time.Second

// adoptedGuard is the longest simulated wait any test here allows.
const adoptedGuard = 2 * time.Hour

func newAdoptedFixture() *adoptedFixture {
	return &adoptedFixture{
		clock:  nudgeT0,
		state:  session.StateWaitingInput,
		family: WorkerNudgeFamily,
		worker: &fakeNudgeWorker{obs: nudgeObs(time.Hour)},
	}
}

func (f *adoptedFixture) ports() AdoptedWaitPorts {
	return AdoptedWaitPorts{
		NudgeAfter: nudgeTestWindow,
		StallAfter: nudgeTestStall,
		Now:        func() time.Time { return f.clock },
		Pause: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			f.polls++
			f.clock = f.clock.Add(adoptedPollStep)
			if f.clock.Sub(nudgeT0) > adoptedGuard {
				return errors.New("test guard: adopted wait ran past the simulated limit")
			}
			return nil
		},
		Status: func(string) (session.State, string, bool) {
			return f.state, f.family, true
		},
		Completed: func(string) bool { return f.marker },
		Nudge:     func(string) WorkerNudgeHooks { return f.worker.hooks() },
	}
}

func TestWaitAdopted_IdleWithoutWorkChildGetsOneNudgeAfterTheWindow(t *testing.T) {
	f := newAdoptedFixture()
	var sentAt time.Time
	f.worker.onSend = func() {
		// The nudge gets the worker to finish, so the wait ends on completion.
		sentAt = f.clock
		f.marker = true
	}

	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess"}
	if err := WaitAdopted(context.Background(), w, f.ports()); err != nil {
		t.Fatalf("WaitAdopted: %v", err)
	}
	if len(f.worker.sent) != 1 {
		t.Fatalf("sent %d inputs, want exactly one nudge", len(f.worker.sent))
	}
	if want := nudgeT0.Add(nudgeTestWindow); !sentAt.Equal(want) {
		t.Fatalf("nudge sent at %s, want at the window %s", sentAt, want)
	}
	if f.worker.sent[0] != WorkerNudgeText {
		t.Fatalf("nudge text = %q, want the completion nudge", f.worker.sent[0])
	}
	if len(f.worker.recorded) != 1 || !f.worker.recorded[0].Equal(sentAt) {
		t.Fatalf("recorded = %v, want the one nudge at %s", f.worker.recorded, sentAt)
	}
	if len(f.worker.ended) != 0 {
		t.Fatalf("worker was ended on a completed wait: %v", f.worker.ended)
	}
}

func TestWaitAdopted_SilencePastStallWindowReturnsErrWorkerStalled(t *testing.T) {
	f := newAdoptedFixture()
	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess"}

	err := WaitAdopted(context.Background(), w, f.ports())
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("WaitAdopted error = %v, want wrapping ErrWorkerStalled", err)
	}
	if len(f.worker.sent) != 1 {
		t.Fatalf("sent %d inputs before the stall, want one nudge", len(f.worker.sent))
	}
	stallAt := nudgeT0.Add(nudgeTestWindow + nudgeTestStall)
	if !f.clock.Equal(stallAt) {
		t.Fatalf("stalled at %s, want nudge window plus stall window %s", f.clock, stallAt)
	}
	if len(f.worker.ended) != 1 {
		t.Fatalf("worker ended %d times, want 1", len(f.worker.ended))
	}
}

func TestWaitAdopted_CompletionAfterNudgeIsNilWithNoStall(t *testing.T) {
	f := newAdoptedFixture()
	f.worker.onSend = func() { f.marker = true }
	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess"}

	if err := WaitAdopted(context.Background(), w, f.ports()); err != nil {
		t.Fatalf("completion after the nudge: %v, want nil", err)
	}
	if len(f.worker.sent) != 1 {
		t.Fatalf("sent %d inputs, want 1", len(f.worker.sent))
	}
	if len(f.worker.ended) != 0 {
		t.Fatalf("completed worker was ended: %v", f.worker.ended)
	}
}

func TestWaitAdopted_OutputAfterNudgeKeepsWaitingUntilCompletion(t *testing.T) {
	// The worker answers the nudge with fresh output. Nudge tracking ends, but
	// the worker is still running, so the wait must not return nil yet and must
	// not stall. It returns nil only once the completion marker shows up.
	f := newAdoptedFixture()
	f.worker.onSend = func() { f.worker.obs.OutputAge = time.Second }
	f.marker = false
	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess"}

	ports := f.ports()
	pause := ports.Pause
	ports.Pause = func(ctx context.Context) error {
		if f.clock.Sub(nudgeT0) >= nudgeTestWindow+nudgeTestStall*2 {
			f.marker = true
		}
		return pause(ctx)
	}
	if err := WaitAdopted(context.Background(), w, ports); err != nil {
		t.Fatalf("WaitAdopted: %v", err)
	}
	if len(f.worker.sent) != 1 {
		t.Fatalf("sent %d inputs, want 1", len(f.worker.sent))
	}
	if len(f.worker.ended) != 0 {
		t.Fatalf("responding worker was ended: %v", f.worker.ended)
	}
	if f.clock.Sub(nudgeT0) < nudgeTestWindow+nudgeTestStall*2 {
		t.Fatalf("returned at %s, before the worker completed", f.clock.Sub(nudgeT0))
	}
}

func TestWaitAdopted_PersistedNudgeIsNeverResentAndStallsFromPersistedTime(t *testing.T) {
	// The task already holds a nudge sent into this session before a restart.
	// The adopted wait must not nudge again, and must stall at persisted time
	// plus the stall window, not a full window after adoption.
	task := Task{NudgeCount: 1, NudgeEvents: []NudgeEvent{
		{At: nudgeT0.Add(10 * time.Minute), Count: 1, SessionID: "sess", Outcome: NudgeOutcomeSent},
	}}
	f := newAdoptedFixture()
	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess", NudgedAt: task.NudgeTimeForSession("sess")}

	err := WaitAdopted(context.Background(), w, f.ports())
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("WaitAdopted error = %v, want wrapping ErrWorkerStalled", err)
	}
	if len(f.worker.sent) != 0 {
		t.Fatalf("adopted worker with a persisted nudge was nudged again: %q", f.worker.sent)
	}
	if len(f.worker.recorded) != 0 {
		t.Fatalf("persisted nudge recorded again: %v", f.worker.recorded)
	}
	stallAt := nudgeT0.Add(10*time.Minute + nudgeTestStall)
	if !f.clock.Equal(stallAt) {
		t.Fatalf("stalled at %s, want persisted time plus stall window %s", f.clock, stallAt)
	}
}

func TestWaitAdopted_NonClaudeWorkerIsWaitedOnNotNudged(t *testing.T) {
	f := newAdoptedFixture()
	f.family = "pi-prompt"
	f.marker = false
	pause := f.ports().Pause
	ports := f.ports()
	ports.Pause = func(ctx context.Context) error {
		if f.clock.Sub(nudgeT0) >= 30*time.Second {
			f.marker = true
		}
		return pause(ctx)
	}
	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess"}

	if err := WaitAdopted(context.Background(), w, ports); err != nil {
		t.Fatalf("non-claude completion: %v, want nil", err)
	}
	if len(f.worker.sent) != 0 {
		t.Fatalf("non-claude worker was nudged: %q", f.worker.sent)
	}
}

func TestWaitAdopted_EndedSessionIsAnErrorNotAStall(t *testing.T) {
	f := newAdoptedFixture()
	f.state = session.StateKilled
	w := AdoptedWait{PRDID: "prd", TaskID: "task", SessionID: "sess"}

	err := WaitAdopted(context.Background(), w, f.ports())
	if err == nil {
		t.Fatal("killed session returned nil")
	}
	if errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("killed session reported as a stall: %v", err)
	}
}
