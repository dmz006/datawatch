package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// retargetFake is the set of fake collaborators a retargetWait test drives.
type retargetFake struct {
	waitCalls  atomic.Int32
	waitExited atomic.Int32
	grant      chan struct{} // closed to grant the original wait
	waitErr    error         // when set, Wait returns it instead of waiting

	mu        sync.Mutex
	nextCalls int
	lefts     [][]string
	waitings  []string
	takeCalls int
	moves     []string
	released  atomic.Int32
}

func newRetargetFake() *retargetFake { return &retargetFake{grant: make(chan struct{})} }

func (f *retargetFake) wait(ctx context.Context, backend, model string) (func(), string, error) {
	f.waitCalls.Add(1)
	defer f.waitExited.Add(1)
	if f.waitErr != nil {
		return nil, "", f.waitErr
	}
	grant := func() (func(), string, error) {
		return func() { f.released.Add(1) }, "node-" + backend, nil
	}
	select {
	case <-f.grant:
		return grant()
	case <-ctx.Done():
		// A grant that landed at the same instant wins over the cancel.
		select {
		case <-f.grant:
			return grant()
		default:
			return nil, "", ctx.Err()
		}
	}
}

func (f *retargetFake) recordNext(waiting string, left []string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextCalls++
	f.waitings = append(f.waitings, waiting)
	f.lefts = append(f.lefts, append([]string(nil), left...))
	return f.nextCalls
}

func (f *retargetFake) nextCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nextCalls
}

func (f *retargetFake) onMove(from, to, model, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, from+">"+to+":"+model+":"+reason)
}

func (f *retargetFake) take(backend, model string) (func(), string, bool) {
	f.mu.Lock()
	f.takeCalls++
	f.mu.Unlock()
	return func() { f.released.Add(1) }, "node-" + backend, true
}

func retargetTestCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func shortRetargetTick(t *testing.T, d time.Duration) {
	t.Helper()
	old := retargetTick
	retargetTick = d
	t.Cleanup(func() { retargetTick = old })
}

func TestRetargetWaitMovesAndTakesLease(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			if f.recordNext(waiting, left) < 3 {
				return "", "", "", false
			}
			return "b", "mb", "b is free", true
		},
		Take:   f.take,
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got.Moved || got.Backend != "b" || got.Model != "mb" || got.Node != "node-b" || got.Release == nil {
		t.Fatalf("out = %+v", got)
	}
	if len(f.moves) != 1 || f.moves[0] != "a>b:mb:b is free" {
		t.Fatalf("moves = %v", f.moves)
	}
	if f.takeCalls != 1 {
		t.Fatalf("take calls = %d", f.takeCalls)
	}
	if f.waitCalls.Load() != f.waitExited.Load() {
		t.Fatalf("wait goroutine still running: calls=%d exited=%d", f.waitCalls.Load(), f.waitExited.Load())
	}
	got.Release()
	if f.released.Load() != 1 {
		t.Fatalf("released = %d, want only the taken lease", f.released.Load())
	}
}

func TestRetargetWaitGrantAtSameMomentKeepsOriginal(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			// The grant lands as the target appears.
			select {
			case <-f.grant:
			default:
				close(f.grant)
			}
			return "b", "mb", "b is free", true
		},
		Take:   f.take,
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Moved || got.Backend != "a" || got.Model != "ma" || got.Node != "node-a" || got.Release == nil {
		t.Fatalf("out = %+v", got)
	}
	if f.takeCalls != 0 || len(f.moves) != 0 {
		t.Fatalf("take=%d moves=%v, want neither", f.takeCalls, f.moves)
	}
	if f.waitCalls.Load() != f.waitExited.Load() {
		t.Fatal("wait goroutine leaked")
	}
}

func TestRetargetWaitPinnedNeverCallsNextOrTake(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	changedCalls := 0
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(f.grant)
	}()
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Take: f.take,
		Changed: func() <-chan struct{} {
			changedCalls++
			return nil
		},
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Moved || got.Backend != "a" || got.Release == nil {
		t.Fatalf("out = %+v", got)
	}
	if f.takeCalls != 0 || len(f.moves) != 0 || changedCalls != 0 {
		t.Fatalf("take=%d moves=%v changed=%d, want none", f.takeCalls, f.moves, changedCalls)
	}
	if f.waitCalls.Load() != 1 {
		t.Fatalf("wait calls = %d, want 1", f.waitCalls.Load())
	}
}

func TestRetargetWaitPassesLeftAndNeverReturnsToIt(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "b", Model: "mb",
		Left: []string{"a"},
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			n := f.recordNext(waiting, left)
			if n < 4 {
				// A policy that ignores left and keeps pointing at the origin.
				return "a", "ma", "a looks free", true
			}
			return "c", "mc", "c is free", true
		},
		Take:   f.take,
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got.Moved || got.Backend != "c" {
		t.Fatalf("out = %+v, want a move to c and never back to a", got)
	}
	if f.takeCalls != 1 {
		t.Fatalf("take calls = %d, want 1 (the origin must not be tried)", f.takeCalls)
	}
	for i, l := range f.lefts {
		if len(l) != 1 || l[0] != "a" || f.waitings[i] != "b" {
			t.Fatalf("next call %d: waiting=%q left=%v", i, f.waitings[i], l)
		}
	}
}

func TestRetargetWaitFailedTakeResumesAndCoolsDown(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	var takeAt []int
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			return "b", "mb", "b is free", true
		},
		Take: func(backend, model string) (func(), string, bool) {
			takeAt = append(takeAt, f.nextCount())
			if len(takeAt) == 1 {
				return nil, "", false // loses the race
			}
			return f.take(backend, model)
		},
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got.Moved || got.Backend != "b" {
		t.Fatalf("out = %+v", got)
	}
	if len(takeAt) != 2 || takeAt[1]-takeAt[0] != retargetCooldownTicks+1 {
		t.Fatalf("take attempts at next-call %v, want 2 attempts %d evaluations apart", takeAt, retargetCooldownTicks+1)
	}
	if f.waitCalls.Load() != 2 {
		t.Fatalf("wait calls = %d, want the original plus one restart", f.waitCalls.Load())
	}
	if f.waitCalls.Load() != f.waitExited.Load() {
		t.Fatal("wait goroutine leaked")
	}
	if len(f.moves) != 1 {
		t.Fatalf("moves = %v, want only the successful one", f.moves)
	}
}

func TestRetargetWaitFailedTakeThenGrantOnOriginal(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			return "b", "mb", "b is free", true
		},
		Take: func(string, string) (func(), string, bool) {
			close(f.grant) // the restarted wait on a is granted
			return nil, "", false
		},
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Moved || got.Backend != "a" || got.Release == nil {
		t.Fatalf("out = %+v, want the original grant", got)
	}
}

func TestRetargetWaitChangedTriggersReevaluation(t *testing.T) {
	shortRetargetTick(t, time.Hour) // only Changed can trigger
	f := newRetargetFake()

	var mu sync.Mutex
	cur := make(chan struct{})
	changed := func() <-chan struct{} {
		mu.Lock()
		defer mu.Unlock()
		return cur
	}
	fire := func() {
		mu.Lock()
		old := cur
		cur = make(chan struct{})
		mu.Unlock()
		close(old)
	}
	nextSeen := make(chan int, 4)

	type res struct {
		out retargetWaitOut
		err error
	}
	done := make(chan res, 1)
	go func() {
		out, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
			Backend: "a", Model: "ma",
			Wait: f.wait,
			Next: func(waiting string, left []string) (string, string, string, bool) {
				n := f.recordNext(waiting, left)
				nextSeen <- n
				if n < 2 {
					return "", "", "", false
				}
				return "b", "mb", "b is free", true
			},
			Take:    f.take,
			Changed: changed,
			OnMove:  f.onMove,
		})
		done <- res{out, err}
	}()

	time.Sleep(10 * time.Millisecond)
	if f.nextCount() != 0 {
		t.Fatalf("next called %d times before any signal", f.nextCount())
	}
	fire()
	select {
	case n := <-nextSeen:
		if n != 1 {
			t.Fatalf("first evaluation = %d", n)
		}
	case <-time.After(time.Second):
		t.Fatal("Changed did not trigger an evaluation")
	}
	fire() // the channel must have been re-taken
	select {
	case r := <-done:
		if r.err != nil || !r.out.Moved || r.out.Backend != "b" {
			t.Fatalf("out = %+v err = %v", r.out, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("second Changed did not trigger an evaluation")
	}
}

func TestRetargetWaitCtxCancelReturnsErrAndEndsWait(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	_, err := retargetWait(ctx, retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			return "", "", "", false
		},
		Take: f.take,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if f.waitCalls.Load() != 1 || f.waitExited.Load() != 1 {
		t.Fatalf("wait calls=%d exited=%d, want the goroutine ended", f.waitCalls.Load(), f.waitExited.Load())
	}
	if f.takeCalls != 0 {
		t.Fatal("take called after cancel")
	}
}

func TestRetargetWaitMoveOnlyReturnsMovedWithoutLease(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	f := newRetargetFake()
	got, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			return "b", "mb", "b is free", true
		},
		OnMove: f.onMove,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got.Moved || got.Backend != "b" || got.Model != "mb" || got.Release != nil || got.Node != "" {
		t.Fatalf("out = %+v", got)
	}
	if len(f.moves) != 1 || f.moves[0] != "a>b:mb:b is free" {
		t.Fatalf("moves = %v", f.moves)
	}
	if f.waitCalls.Load() != f.waitExited.Load() {
		t.Fatal("wait goroutine leaked")
	}
	if f.released.Load() != 0 {
		t.Fatalf("released = %d, want no lease touched", f.released.Load())
	}
}

func TestRetargetWaitNoFlappingAfterMove(t *testing.T) {
	shortRetargetTick(t, 2*time.Millisecond)
	// A policy that always prefers the other backend of {a, b}.
	other := func(waiting string) (string, string) {
		if waiting == "a" {
			return "b", "mb"
		}
		return "a", "ma"
	}
	f := newRetargetFake()
	in := retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			b, m := other(waiting)
			return b, m, "other looks free", true
		},
		Take:   f.take,
		OnMove: f.onMove,
	}
	first, err := retargetWait(retargetTestCtx(t), in)
	if err != nil || !first.Moved || first.Backend != "b" {
		t.Fatalf("first = %+v err = %v", first, err)
	}

	// Second call: the request now waits on b and lists a as left. It must
	// stay on b even though the policy keeps pointing back at a.
	in.Backend, in.Model = first.Backend, first.Model
	in.Left = []string{"a"}
	in.Wait = func(ctx context.Context, backend, model string) (func(), string, error) {
		select {
		case <-time.After(30 * time.Millisecond):
			return func() {}, "node-" + backend, nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	takesBefore := f.takeCalls
	second, err := retargetWait(retargetTestCtx(t), in)
	if err != nil {
		t.Fatalf("second err = %v", err)
	}
	if second.Moved || second.Backend != "b" {
		t.Fatalf("second = %+v, want it to stay on b", second)
	}
	if f.takeCalls != takesBefore {
		t.Fatal("second call took a lease on the origin")
	}
	if len(f.moves) != 1 {
		t.Fatalf("moves = %v, want exactly one", f.moves)
	}
}

func TestRetargetWaitWaitErrorReturnedAsIs(t *testing.T) {
	shortRetargetTick(t, time.Hour)
	f := newRetargetFake()
	f.waitErr = context.DeadlineExceeded
	_, err := retargetWait(retargetTestCtx(t), retargetWaitIn{
		Backend: "a", Model: "ma",
		Wait: f.wait,
		Next: func(waiting string, left []string) (string, string, string, bool) {
			f.recordNext(waiting, left)
			return "", "", "", false
		},
		Take: f.take,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the Wait error unchanged", err)
	}
	if f.waitCalls.Load() != f.waitExited.Load() {
		t.Fatal("wait goroutine leaked")
	}
}
