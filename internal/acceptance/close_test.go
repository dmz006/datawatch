package acceptance

import (
	"context"
	"testing"
	"time"
)

func looked() Item { return Item{ID: "look", Statement: "a person looked", Method: MethodConfirm} }

// A check met on one version must not be carried into "done" by an answer that
// arrives after the work moved to a broken one.
func TestAnAnswerNeverClosesAListOverAStaleCheck(t *testing.T) {
	broken := RunOutcome{Finished: true, ExitCode: 1, Output: failLine}
	for name, last := range map[string]func(w *world) *List{
		"a confirm answer": func(w *world) *List {
			w.cover("r")
			w.work("r", "c1")
			w.res.at[""] = "c2"
			return must[*List](w.t)(w.svc.Confirm("r", "look", true, "yes", "", "", operator))
		},
		"an acceptance": func(w *world) *List {
			w.cover("r")
			w.work("r", "c1")
			w.res.at[""] = "c2"
			return must[*List](w.t)(w.svc.Accept("r", "look", "later", "ok", "", operator))
		},
		"the coverage answer": func(w *world) *List {
			must[*List](w.t)(w.svc.Confirm("r", "look", true, "yes", "", "", operator))
			w.work("r", "c1")
			w.res.at[""] = "c2"
			return w.cover("r")
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.runner.out["c2"] = broken
			w.frozen("r", "bug", oneAsk, fix(), looked())
			l := last(w)
			if got := Read(l); got.Done || got.Status != StatusNeedsRun {
				t.Fatalf("after the last answer: %s done=%v", got.Status, got.Done)
			}
			if !l.ClosedAt.IsZero() {
				t.Fatal("an answer closed the list")
			}
			l = must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
			wantNotMet(t, l, "fix", "exited 1")
			if w.runner.calls[len(w.runner.calls)-1] != "c2" {
				t.Fatalf("the current version was never run: %v", w.runner.calls)
			}
		})
	}
}

func TestOnlyARunClosesAList(t *testing.T) {
	w := newWorld(t)
	w.frozen("r", "bug", oneAsk, fix(), looked())
	w.cover("r")
	w.work("r", "c1")
	l := must[*List](t)(w.svc.Confirm("r", "look", true, "yes", "", "", operator))
	wantStatus(t, l, StatusNeedsRun)
	l = w.work("r", "c1")
	wantStatus(t, l, StatusDone)
	// A claim changes nothing that counts, so it does not reopen the list.
	must[error](t)(nil, w.svc.NoteHolder("r", "w1"))
	wantStatus(t, must[*List](t)(w.svc.Confirm("r", "look", true, "mine", "", "", worker)), StatusDone)
	// A counting answer does: the next run has to look again.
	wantStatus(t, must[*List](t)(w.svc.Confirm("r", "look", true, "again", "", "", operator)), StatusNeedsRun)
}

// movingRunner moves the work on while a check is running.
type movingRunner struct {
	*fakeRunner
	move func()
}

func (m movingRunner) Run(ctx context.Context, c CheckSpec, s Subject, d time.Duration) RunOutcome {
	out := m.fakeRunner.Run(ctx, c, s, d)
	m.move()
	return out
}

func TestARunDoesNotCloseAListWhoseVersionMovedUnderIt(t *testing.T) {
	w := newWorld(t)
	w.frozen("r", "bug", oneAsk, fix())
	w.cover("r")
	w.res.at[""] = "c1"
	w.svc.o.Runner = movingRunner{w.runner, func() { w.res.at[""] = "c2" }}
	l := must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
	if got := Read(l); got.Done {
		t.Fatalf("done on a version the work had left: %s", got.Status)
	}
	if got := read(l, "fix").Result; got.Value == ResultMet {
		t.Fatalf("the old result still reads met: %+v", got)
	}
}

// An answer about the work given while nothing could name its version cannot
// close a list.
func TestAnAnswerBoundToNoVersionDoesNotClose(t *testing.T) {
	w := newWorld(t)
	seen := Item{ID: "c", Statement: "I read the result", From: oneAsk, Method: MethodConfirm, Effect: "work"}
	w.frozen("r", "x", oneAsk, seen)
	w.svc.o.Resolver = nil
	w.cover("r")
	must[*List](t)(w.svc.Confirm("r", "c", true, "read it", "", "", operator))
	l := must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
	if got := Read(l); got.Done {
		t.Fatalf("done with an answer bound to no version: %s", got.Status)
	}
}

// With nothing that a run would read again, answers still complete a list.
func TestAListWithNoVersionToReadIsClosedByItsLastAnswer(t *testing.T) {
	w := newWorld(t)
	w.frozen("r", "x", oneAsk, Item{ID: "c", Statement: "told", From: oneAsk, Method: MethodConfirm})
	must[*List](t)(w.svc.Confirm("r", "c", true, "told", "", "", operator))
	wantStatus(t, w.cover("r"), StatusDone)
}
