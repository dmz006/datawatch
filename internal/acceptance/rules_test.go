package acceptance

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func containsStr(s, sub string) bool { return strings.Contains(s, sub) }

func TestWholePathReadsDone(t *testing.T) {
	w := newWorld(t)
	w.frozen("r1", "bug", oneAsk, fix())
	w.cover("r1")
	l := w.work("r1", "c1")
	// The ask sentence is covered, so no automatic item is left.
	if len(l.Items) != 1 {
		t.Fatalf("items = %+v", l.Items)
	}
	wantStatus(t, l, StatusDone)
}

func TestRule1StartRunMustFinishAndFailAtANamedLine(t *testing.T) {
	for name, start := range map[string]RunOutcome{
		"timeout":        {ExitCode: -1, Reason: "the check ran past its limit"},
		"exit 127":       {ExitCode: 127, Reason: "exit 127: a tool the check needs could not run"},
		"killed":         {ExitCode: -1, Reason: "the check was killed"},
		"other failure":  {Finished: true, ExitCode: 1, Output: "build failed"},
		"another test":   {Finished: true, ExitCode: 1, Output: "--- FAIL: TestXY (0s)"},
		"exit 0 nothing": {Finished: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.runner.out["c0"] = start
			w.frozen("r1", "bug", oneAsk, fix())
			w.cover("r1")
			l := w.work("r1", "c1")
			wantNotMet(t, l, "fix", "baseline")
		})
	}
}

func TestRule2ExitZeroIsNotProof(t *testing.T) {
	for name, after := range map[string]RunOutcome{
		"script true":          {Finished: true},
		"filter matches none":  {Finished: true, Output: "testing: warning: no tests to run\nok"},
		"skipped":              {Finished: true, Output: "--- SKIP: TestX (0.00s)\nok"},
		"longer name only":     {Finished: true, Output: passLine + "Other (0s)"},
		"or true hides a fail": {Finished: true, Output: passLine + " (0s)\n" + failLine + " (0s)"},
		"cut output":           {Finished: true, Cut: true, Output: passLine},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.runner.out["c1"] = after
			w.frozen("r1", "bug", oneAsk, fix())
			w.cover("r1")
			wantNotMet(t, w.work("r1", "c1"), "fix", "")
		})
	}
	t.Run("already passing", func(t *testing.T) {
		w := newWorld(t)
		w.runner.out["c0"] = afterPass
		l := w.frozen("r1", "bug", oneAsk, fix())
		if !hasFlag(Flags(l, w.set.ProjectChecks, false), "already_passing") {
			t.Fatal("not flagged")
		}
		w.cover("r1")
		l = w.work("r1", "c1")
		wantNotMet(t, l, "fix", "already passed")
		if !read(l, "fix").Result.AlreadyTrue {
			t.Fatal("already true not shown")
		}
	})
	t.Run("change item without before", func(t *testing.T) {
		w := newWorld(t)
		it := fix()
		it.Before = BeforeNone
		w.frozen("r1", "bug", oneAsk, it)
		w.cover("r1")
		wantNotMet(t, w.work("r1", "c1"), "fix", "fail_first")
	})
	t.Run("names nothing that must run", func(t *testing.T) {
		w := newWorld(t)
		w.set.ProjectChecks["unit"] = ProjectCheck{Command: "true"}
		w.frozen("r1", "bug", oneAsk, fix())
		w.cover("r1")
		wantNotMet(t, w.work("r1", "c1"), "fix", "must have run")
	})
}

func hasFlag(f []Flag, code string) bool {
	for _, x := range f {
		if x.Code == code {
			return true
		}
	}
	return false
}

func TestRule4NoResultIsImportedFromOtherRecords(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %d", err, len(files))
	}
	for _, f := range files {
		ast, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range ast.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if strings.Contains(p, "datawatch/internal/") && !strings.HasSuffix(p, "/internal/secfile") {
				t.Errorf("%s imports %s: items run only their own saved check", f, p)
			}
		}
	}
	// A passing report, an exemption note or a clean tree give nothing to an
	// item without its own saved check.
	w := newWorld(t)
	w.repo["c1"]["report.md"] = "verified: pass. no script because docs only. already landed."
	w.frozen("r1", "bug", oneAsk, Item{ID: "x", Statement: "export works", From: oneAsk, Method: MethodCommand, Effect: "work"})
	w.cover("r1")
	l := w.work("r1", "c1")
	wantNotMet(t, l, "x", "no saved check")
	if len(w.runner.calls) != 0 {
		t.Fatalf("something else was run: %v", w.runner.calls)
	}
}

func TestRule5ResultsBelongToOneVersion(t *testing.T) {
	w := newWorld(t)
	// A second open item keeps the list from closing.
	other := Item{ID: "ok", Statement: "someone looked", From: oneAsk, Method: MethodConfirm}
	w.frozen("r1", "bug", oneAsk, fix(), other)
	w.work("r1", "c1")
	runs := len(w.runner.calls)
	w.work("r1", "c1")
	if len(w.runner.calls) != runs {
		t.Fatal("same key ran again")
	}
	w.runner.out["c2"] = RunOutcome{Finished: true, ExitCode: 1, Output: failLine}
	l := w.work("r1", "c2")
	if len(w.runner.calls) != runs+1 {
		t.Fatalf("changed subject did not rerun: %v", w.runner.calls)
	}
	if read(l, "fix").Result.Value != ResultNotMet {
		t.Fatalf("old pass was reused: %+v", read(l, "fix"))
	}
	// An unread pass for an older version reads unchecked.
	l.State["fix"].Result = l.State["fix"].Runs[firstMetKey(l, "fix")]
	if r := read(l, "fix"); r.Result.Value != ResultUnchecked {
		t.Fatalf("stale pass reads %s", r.Result.Value)
	}
}

func firstMetKey(l *List, id string) string {
	for k, r := range l.State[id].Runs {
		if r.Value == ResultMet {
			return k
		}
	}
	return ""
}

func TestRule6AReportOnlyBranchNeverClosesABug(t *testing.T) {
	ask := "A due weekly update starts and finishes unattended, or raises one alert saying why not"
	w := newWorld(t)
	it := fix()
	it.From, it.Statement, it.Source = ask, ask, SourceInherited
	l := w.frozen("r1", "bug", ask, it)
	if !hasFlag(Flags(l, nil, false), "report_only") || !hasFlag(Flags(l, nil, false), "inherited_draft") {
		t.Fatalf("flags = %+v", Flags(l, nil, false))
	}
	w.cover("r1")
	l = w.work("r1", "c1")
	wantStatus(t, l, StatusReportOnly)
	if !strings.Contains(Read(l).Label, "still broken") || Read(l).Done {
		t.Fatalf("%+v", Read(l))
	}
}

// window builds a "nothing is loaded for 15 minutes" item over a probe.
func window(t *testing.T, readings *[]Reading, errs *[]error) (*world, func(time.Duration) *List) {
	t.Helper()
	w := newWorld(t)
	i := 0
	p := &fakeProbe{}
	p.read = func() (Reading, error) {
		r := (*readings)[min(i, len(*readings)-1)]
		var err error
		if errs != nil && i < len(*errs) {
			err = (*errs)[i]
		}
		i++
		if r.SourceAt.IsZero() && r.Answered {
			r.SourceAt = w.now
		}
		return r, err
	}
	w.probes["loaded"] = p
	ask := "Keep the node empty"
	it := Item{ID: "quiet", Statement: "no model is loaded for 15 minutes", From: ask, Method: "state:loaded", Negative: true,
		State: &StateSpec{Want: "none"}, Wait: &WaitSpec{WindowSec: 900, MaxGapSec: 360}, Effect: "the node"}
	w.frozen("r1", "operation-x", ask, it)
	w.cover("r1")
	start := w.now
	return w, func(at time.Duration) *List {
		w.now = start.Add(at)
		return must[*List](t)(w.svc.Evaluate(context.Background(), "r1"))
	}
}

func TestRule7SilenceIsNotSuccess(t *testing.T) {
	quiet := Reading{Answered: true, Value: "none"}
	t.Run("full coverage is met", func(t *testing.T) {
		r := []Reading{quiet}
		_, at := window(t, &r, nil)
		wantStatus(t, at(0), StatusWaiting)
		at(5 * time.Minute)
		at(10 * time.Minute)
		wantStatus(t, at(15*time.Minute), StatusDone)
	})
	t.Run("empty plus error", func(t *testing.T) {
		r := []Reading{quiet, {}, quiet}
		e := []error{nil, errors.New("unreachable")}
		_, at := window(t, &r, &e)
		at(0)
		at(5 * time.Minute)
		at(10 * time.Minute)
		wantNotMet(t, at(15*time.Minute), "quiet", "did not answer")
	})
	t.Run("stale source", func(t *testing.T) {
		r := []Reading{{Answered: true, Value: "none", SourceAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)}}
		_, at := window(t, &r, nil)
		at(0)
		at(5 * time.Minute)
		at(10 * time.Minute)
		wantNotMet(t, at(15*time.Minute), "quiet", "stale")
	})
	t.Run("one sample at the end", func(t *testing.T) {
		r := []Reading{quiet}
		_, at := window(t, &r, nil)
		at(0)
		wantNotMet(t, at(15*time.Minute), "quiet", "gap")
		// A broken window is not repaired by later readings.
		wantNotMet(t, at(16*time.Minute), "quiet", "gap")
	})
	t.Run("restart", func(t *testing.T) {
		r := []Reading{quiet, {Answered: true, Value: "none", Restarted: true}, quiet}
		_, at := window(t, &r, nil)
		at(0)
		at(5 * time.Minute)
		at(10 * time.Minute)
		wantNotMet(t, at(15*time.Minute), "quiet", "restart")
	})
	t.Run("load then unload seen by a reading", func(t *testing.T) {
		r := []Reading{quiet, {Answered: true, Value: "model"}, quiet}
		_, at := window(t, &r, nil)
		at(0)
		wantStatus(t, at(5*time.Minute), StatusNotMet)
		at(10 * time.Minute)
		wantStatus(t, at(15*time.Minute), StatusNotMet)
	})
	t.Run("load then unload only in the event log", func(t *testing.T) {
		r := []Reading{quiet}
		w, at := window(t, &r, nil)
		start := w.now
		at(0)
		at(5 * time.Minute)
		r[0] = Reading{Answered: true, Value: "none", Violations: []time.Time{start.Add(7 * time.Minute)}}
		wantStatus(t, at(10*time.Minute), StatusNotMet)
	})
	t.Run("complete event log", func(t *testing.T) {
		r := []Reading{quiet}
		w, at := window(t, &r, nil)
		start := w.now
		at(0)
		r[0] = Reading{Answered: true, Value: "none", LogComplete: true, LogFrom: start, LogTo: start.Add(15 * time.Minute)}
		wantStatus(t, at(15*time.Minute), StatusDone)
	})
	t.Run("restart of datawatch keeps the due time", func(t *testing.T) {
		r := []Reading{quiet}
		w, at := window(t, &r, nil)
		l := at(0)
		anchor := l.State["quiet"].WaitAnchor
		w.open()
		l = must[*List](t)(w.svc.Get("r1"))
		if !l.State["quiet"].WaitAnchor.Equal(anchor) || len(l.State["quiet"].Samples) != 1 {
			t.Fatalf("%+v", l.State["quiet"])
		}
		if d := read(l, "quiet").Result.Waiting; d == nil || !d.Due.Equal(anchor.Add(15*time.Minute)) {
			t.Fatalf("due = %+v", d)
		}
	})
	t.Run("negative without a window", func(t *testing.T) {
		w := newWorld(t)
		w.probes["loaded"] = &fakeProbe{read: func() (Reading, error) { return Reading{Answered: true, Value: "none", SourceAt: w.now}, nil }}
		w.frozen("r1", "x", oneAsk, Item{ID: "n", Statement: "nothing loaded", From: oneAsk, Method: "state:loaded", Negative: true, State: &StateSpec{Want: "none"}})
		wantNotMet(t, w.work("r1", "c1"), "n", "window")
	})
}

func TestRule8BuildCommitNotVersionString(t *testing.T) {
	const oldC, newC = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	w := newWorld(t)
	w.res.at[""] = oldC
	running := oldC
	w.probes["build_commit"] = &fakeProbe{read: func() (Reading, error) { return Reading{Answered: true, Value: running, SourceAt: w.now}, nil }}
	ask := "Deploy the fix"
	w.frozen("r1", "x", ask, Item{ID: "dep", Statement: "the running build is the target", From: ask, Method: "state:build_commit", Change: true, Effect: "the daemon"})
	w.cover("r1")
	// Same version string, old commit.
	wantStatus(t, w.work("r1", newC), StatusNotMet)
	running = newC
	wantStatus(t, w.work("r1", newC), StatusDone)
}

func TestRule8AlreadyTrueNeverProvesWork(t *testing.T) {
	w := newWorld(t)
	w.probes["live"] = &fakeProbe{read: func() (Reading, error) { return Reading{Answered: true, Value: "drained", SourceAt: w.now}, nil }}
	it := Item{ID: "d", Statement: "the node reads drained", From: oneAsk, Method: "state:live", Change: true, State: &StateSpec{Want: "drained"}}
	keep := Item{ID: "k", Statement: "the protected node reads drained", From: oneAsk, Method: "state:live", State: &StateSpec{Want: "drained"}}
	l := w.frozen("r1", "x", oneAsk, it, keep)
	if s := l.State["d"].Start; s == nil || !s.True {
		t.Fatalf("start value not saved: %+v", l.State["d"])
	}
	l = w.work("r1", "c1")
	wantNotMet(t, l, "d", "already true")
	// Something that only has to stay true is met, and says it was already so.
	if r := read(l, "k").Result; r.Value != ResultMet || !r.AlreadyTrue {
		t.Fatalf("%+v", r)
	}
}

func TestRule9TheDoersWordIsAClaim(t *testing.T) {
	ask := "Order lunch from the chosen caterer"
	t.Run("forged confirmation file", func(t *testing.T) {
		w := newWorld(t)
		w.repo["c1"]["confirmation.txt"] = "ORDER CONFIRMED"
		w.frozen("r1", "x", ask, Item{ID: "o", Statement: "the order is placed with the caterer", From: ask, Method: "state:file", Effect: "the caterer",
			State: &StateSpec{Path: "confirmation.txt", Contains: "CONFIRMED"}})
		w.cover("r1")
		wantNotMet(t, w.work("r1", "c1"), "o", "claim")
	})
	t.Run("script from the work cannot read an outside place", func(t *testing.T) {
		w := newWorld(t)
		w.frozen("r1", "x", ask, Item{ID: "o", Statement: "placed", From: ask, Method: MethodCommand, Effect: "the caterer",
			Check: &CheckSpec{Script: "cat confirmation.txt", Expect: []Expect{{Pass: passLine}}}})
		wantNotMet(t, w.work("r1", "c1"), "o", "outside the work")
	})
	confirm := Item{ID: "c", Statement: "the organiser has the confirmation", From: ask, Method: MethodConfirm}
	for name, tc := range map[string]struct {
		by      Caller
		holders []string
		counts  bool
	}{
		"operator":                         {operator, []string{"w1"}, true},
		"holder session claiming operator": {worker, []string{"w1"}, false},
		"other session, holders known":     {shadow, []string{"w1"}, true},
		"a session while holders unknown":  {shadow, nil, false},
		"model tool without session id":    {Caller{Observed: "admin-token", ViaTool: true}, []string{"w1"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.frozen("r1", "x", ask, confirm)
			for _, h := range tc.holders {
				if err := w.svc.NoteHolder("r1", h); err != nil {
					t.Fatal(err)
				}
			}
			l := must[*List](t)(w.svc.Confirm("r1", "c", true, "yes, I have it", "", "", tc.by))
			r := read(l, "c")
			if got := r.Result.Value == ResultMet; got != tc.counts {
				t.Fatalf("met = %v, want %v (%+v)", got, tc.counts, r)
			}
			if !tc.counts && !strings.Contains(r.Claim, "observed:") {
				t.Fatalf("claim not shown: %+v", r)
			}
			l = must[*List](t)(w.svc.Accept("r1", "c", "trust me", "accept it", "", tc.by))
			if got := l.State["c"].Accept.Counts; got != tc.counts {
				t.Fatalf("accept counts = %v", got)
			}
		})
	}
	t.Run("a person cannot set a checked item met", func(t *testing.T) {
		w := newWorld(t)
		w.frozen("r1", "bug", oneAsk, fix())
		if _, err := w.svc.Confirm("r1", "fix", true, "done", "", "", operator); !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v", err)
		}
		l := must[*List](t)(w.svc.Accept("r1", "fix", "no test host", "accept it", "", operator))
		if read(l, "fix").Result.Value == ResultMet || !read(l, "fix").Accepted {
			t.Fatalf("%+v", read(l, "fix"))
		}
		l = w.cover("r1")
		wantStatus(t, l, StatusDoneAccepted)
		if Read(l).PlainDone {
			t.Fatal("accepted read as plain done")
		}
	})
}

func judged() Item {
	return Item{ID: "j", Statement: "the report answers the question", From: oneAsk, Method: MethodReview}
}

func TestRule11OneStoredVerdict(t *testing.T) {
	rev := &fakeReviewer{settings: "m1", value: ResultNotMet}
	w := newWorld(t, func(w *world) { w.reviewer = rev })
	w.frozen("r1", "x", oneAsk, judged())
	w.work("r1", "c1")
	w.work("r1", "c1")
	if rev.calls != 1 {
		t.Fatalf("second run on the same key started: %d", rev.calls)
	}
	// Another reviewer says met on the same version.
	rev.settings, rev.value = "m2", ResultMet
	l := w.work("r1", "c1")
	wantNotMet(t, l, "j", "reviewers disagree")
	// Even a different request with the same item and version.
	w.frozen("r2", "x", oneAsk, judged())
	wantNotMet(t, w.work("r2", "c1"), "j", "reviewers disagree")
}

func TestRule11InfrastructureFaultsRerunAndMetNeedsEvidence(t *testing.T) {
	rev := &fakeReviewer{settings: "m1", err: errors.New("timeout")}
	w := newWorld(t, func(w *world) { w.reviewer = rev })
	w.frozen("r1", "x", oneAsk, judged())
	for range 6 {
		wantNotMet(t, w.work("r1", "c1"), "j", "failed")
	}
	if rev.calls != 3 { // the first run and two retries
		t.Fatalf("calls = %d", rev.calls)
	}
	rev.err, rev.value, rev.noQuote = nil, ResultMet, true
	wantNotMet(t, w.work("r1", "c2"), "j", "without evidence")
	rev2 := &fakeReviewer{settings: "m1", value: "great"}
	w2 := newWorld(t, func(w *world) { w.reviewer = rev2 })
	w2.frozen("r1", "x", oneAsk, judged())
	wantNotMet(t, w2.work("r1", "c1"), "j", "could not be read")
}

func TestRule11NoReviewerRunWhileADeterministicItemFails(t *testing.T) {
	rev := &fakeReviewer{settings: "m1", value: ResultMet}
	w := newWorld(t, func(w *world) { w.reviewer = rev })
	w.runner.out["c1"] = RunOutcome{Finished: true, ExitCode: 1}
	w.frozen("r1", "bug", oneAsk, fix(), judged())
	w.work("r1", "c1")
	if rev.calls != 0 {
		t.Fatalf("calls = %d", rev.calls)
	}
}

func TestRule12ThinOrEmptyLists(t *testing.T) {
	ctx := context.Background()
	t.Run("zero items", func(t *testing.T) {
		w := newWorld(t)
		must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Source: "message", SourceKnown: true, Ask: "tell them"}, operator))
		l := must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Remove: []string{"d-received"}}, operator))
		l = must[*List](t)(w.svc.Freeze(ctx, "r1", l.Hash(), operator, ""))
		wantStatus(t, l, StatusNoCriteria)
		if Read(l).Done {
			t.Fatal("empty list is done")
		}
	})
	t.Run("only default items on a real ask", func(t *testing.T) {
		rev := &fakeReviewer{settings: "m", value: ResultMet}
		w := newWorld(t, func(w *world) { w.reviewer = rev })
		// The sentence is only dropped by a recorded change after freeze.
		w.frozen("r1", "report", oneAsk)
		must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Remove: []string{askItemID(oneAsk)}, Words: "drop it"}, operator))
		must[*List](t)(w.svc.Accept("r1", "d-delivered", "no named recipient", "ok", "", operator))
		w.cover("r1")
		wantStatus(t, w.work("r1", "c1"), StatusThinList)
	})
	t.Run("every ask sentence gets an item", func(t *testing.T) {
		w := newWorld(t)
		ask := "Make the export work. Also move the old files; tell nobody:\n- keep the logs"
		it := fix()
		it.From = "Make the export" // quotes the sentence but says less
		l := must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Source: "x", Ask: ask, Upsert: []Item{it}}, operator))
		var got []string
		for _, i := range l.Items {
			if i.Source == SourceAsk {
				got = append(got, i.Statement)
				if i.Method == "" {
					t.Fatal("empty method")
				}
			}
		}
		want := "Make the export work|Also move the old files|tell nobody:|keep the logs"
		if strings.Join(got, "|") != want {
			t.Fatalf("ask items = %q", got)
		}
		if l.Ask.Text != ask {
			t.Fatal("the ask is not word for word")
		}
		if _, err := w.svc.Edit(ctx, EditInput{RequestID: "r1", Remove: []string{askItemID("keep the logs")}}, operator); !errors.Is(err, ErrInvalid) {
			t.Fatalf("an uncovered sentence was dropped before start: %v", err)
		}
		// An edit that keeps the id and drops the quote keeps the sentence open.
		l = must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Upsert: []Item{{ID: askItemID("keep the logs"), Statement: "ok", Method: MethodConfirm}}}, operator))
		if _, open := l.item(askItemID("keep the logs") + "-open"); open == nil {
			t.Fatalf("sentence went uncovered: %+v", l.Items)
		}
	})
	t.Run("free text with no reviewer stays not reviewed", func(t *testing.T) {
		w := newWorld(t)
		w.frozen("r1", "bug", oneAsk, fix())
		l := w.work("r1", "c1")
		wantStatus(t, l, StatusNotReviewed)
		if err := w.svc.NoteHolder("r1", "w1"); err != nil {
			t.Fatal(err)
		}
		l = must[*List](t)(w.svc.Confirm("r1", coverageItem, true, "covers", "", l.Hash(), worker))
		wantStatus(t, l, StatusNotReviewed)
	})
}

func TestRule13TheAskChangesLater(t *testing.T) {
	w := newWorld(t)
	other := Item{ID: "c", Statement: "seen", From: oneAsk, Method: MethodConfirm}
	w.frozen("r1", "bug", oneAsk, fix(), other)
	w.cover("r1")
	w.work("r1", "c1")
	must[*List](t)(w.svc.Confirm("r1", "c", true, "seen", "", "", operator))
	wantStatus(t, w.work("r1", "c1"), StatusDone)
	// "A" alone adds nothing.
	l := must[*List](t)(w.svc.NoteAnswer("r1", "A", " a. ", operator))
	wantStatus(t, l, StatusDone)
	l = must[*List](t)(w.svc.NoteAnswer("r1", "A", "A, and also move X", operator))
	wantStatus(t, l, StatusAskChanged)
	id := askItemID("and also move X")
	if _, it := l.item(id); it == nil {
		t.Fatalf("no open item for the extra words: %+v", l.Items)
	}
	// Revising does not make the new words go away: the item stays open.
	l = must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "r1", Upsert: []Item{{ID: "z", Statement: "n", Method: MethodConfirm}}, Words: "revise"}, operator))
	if Read(l).Done || read(l, id).Result.Value != ResultUnchecked {
		t.Fatalf("%+v", Read(l))
	}
}

func TestRule14KindAndTheListBelongsToTheAsk(t *testing.T) {
	ctx := context.Background()
	if KindForSource("something new") != KindOther || KindForSource("message") != KindDelivery {
		t.Fatal("source kinds")
	}
	for source, want := range map[string]Status{"message": StatusDelivered, "intake": StatusFiled} {
		w := newWorld(t)
		w.probes[MethodReceipt] = &fakeProbe{read: func() (Reading, error) { return fetched("x", w.now), nil }}
		w.sourced("r1", source, "Tell the coordinator Saturday moved and fix the roster")
		must[error](t)(nil, w.svc.NoteHolder("r1", "x"))
		l := w.work("r1", "c1")
		wantStatus(t, l, want)
		if Read(l).Done {
			t.Fatal("receipt alone read done")
		}
		// A changed ask is not "delivered".
		l = must[*List](t)(w.svc.AddAskWords("r1", "and phone them", operator))
		if Read(l).Status == want {
			t.Fatalf("still %s after the ask changed", want)
		}
	}
	t.Run("channel accepted is not receipt", func(t *testing.T) {
		w := newWorld(t)
		w.probes[MethodReceipt] = &fakeProbe{read: func() (Reading, error) { return Reading{Answered: true, Value: "channel_accepted"}, nil }}
		w.frozen("r1", "message", "hello")
		wantNotMet(t, w.work("r1", "c1"), "d-received", "not receipt")
	})
	t.Run("one list per ask; copies and replacements keep it", func(t *testing.T) {
		w := newWorld(t)
		w.frozen("r1", "bug", oneAsk, fix())
		w.cover("r1")
		w.work("r1", "c1")
		l := must[*List](t)(w.svc.Get("r1"))
		if _, err := w.svc.CloneFor("r1", "r1"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("second list: %v", err)
		}
		c := must[*List](t)(w.svc.CloneFor("r1", "r2"))
		if len(c.Items) != len(l.Items) || !c.Frozen() || read(c, "fix").Result.Value != ResultUnchecked || c.Coverage != nil {
			t.Fatalf("%+v", c)
		}
		c = must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r2", Remove: []string{"fix"}}, worker))
		if ch := c.ChangesSinceFirst(); len(ch.Removed) != 1 {
			t.Fatalf("weakened copy not shown: %+v", ch)
		}
		w.frozen("r3", "bug", oneAsk, fix())
		l = must[*List](t)(w.svc.NoteUnitReplaced("r3", "reset", "", operator))
		if !strings.Contains(l.Yours, "yours") || len(l.Items) != 1 {
			t.Fatalf("%+v", l)
		}
		l = must[*List](t)(w.svc.NoteUnitReplaced("r3", "split", "r9", operator))
		if l.Unit != "r9" || l.Yours != "" || read(l, "fix").Result.Value != ResultUnchecked {
			t.Fatalf("%+v", l)
		}
	})
}

func TestRule15ChangesAreShownAgainstRevisionOne(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	draft := must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Source: "x", Ask: oneAsk, Upsert: []Item{fix()}}, operator))
	approved := draft.Hash()
	weak := fix()
	weak.Statement = "the export test passes or is skipped"
	must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Upsert: []Item{weak}}, worker))
	if _, err := w.svc.Freeze(ctx, "r1", approved, operator, ""); !errors.Is(err, ErrListChanged) {
		t.Fatalf("a draft changed after approval was fixed: %v", err)
	}
	if _, err := w.svc.Freeze(ctx, "r1", "", operator, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("freeze without a hash: %v", err)
	}
	l := must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Upsert: []Item{fix()}}, operator))
	must[*List](t)(w.svc.Freeze(ctx, "r1", l.Hash(), operator, ""))
	w.cover("r1")
	wantStatus(t, w.work("r1", "c1"), StatusDone)

	// The worker weakens the item to a confirmation after work began.
	easy := Item{ID: "fix", Statement: "looks fine", From: oneAsk, Method: MethodConfirm}
	must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Upsert: []Item{easy}, Words: "operator said so"}, worker))
	must[*List](t)(w.svc.Confirm("r1", "fix", true, "fine", "", "", operator))
	l = w.cover("r1")
	wantStatus(t, l, StatusChangedList)
	if Read(l).Done || Read(l).PlainDone || read(l, "fix").Changed != "altered" {
		t.Fatalf("%+v", Read(l))
	}
	var seen bool
	for _, e := range l.History {
		seen = seen || e.Action == ActionRevise && e.By.Claimed == "operator" && e.By.Observed == "session:w1" && e.AfterFreeze
	}
	if !seen || l.Revisions[0].Items[0].Method != "project:unit" || len(l.Revisions) != 2 {
		t.Fatalf("history %+v", l.History)
	}
}

func TestRule3SavingACheckAgainIsAShownChange(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	w.repo["c1"]["Makefile"] = "test:\n\techo ok"
	w.frozen("r1", "bug", oneAsk, fix())
	w.cover("r1")
	l := w.work("r1", "c1")
	wantNotMet(t, l, "fix", "check changed")
	if got := read(l, "fix").Result.InputChanged; len(got) != 1 || got[0] != "Makefile" {
		t.Fatalf("changed input not shown: %v", got)
	}
	// Saving the item again after review records the new inputs, and shows.
	l = must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r1", Upsert: []Item{fix()}, Words: "reviewed: not weaker"}, operator))
	if ch := l.ChangesSinceFirst(); len(ch.Altered) != 1 {
		t.Fatalf("not shown: %+v", ch)
	}
	w.cover("r1")
	wantStatus(t, w.work("r1", "c1"), StatusChangedList)
}

func TestFrozenCheckIgnoresLaterSettings(t *testing.T) {
	w := newWorld(t)
	w.frozen("r1", "bug", oneAsk, fix())
	w.set.ProjectChecks["unit"] = ProjectCheck{Command: "true"}
	l := w.work("r1", "c1")
	if _, it := l.item("fix"); it.Check.Command != "make test" || len(it.Check.Expect) != 1 {
		t.Fatalf("%+v", it.Check)
	}
}

func TestRule16CannotRunNeverReadsMet(t *testing.T) {
	methods := []string{MethodCommand, "project:missing", "state:live", "state:scheduled_run", "state:file", "state:build_commit", MethodReceipt, MethodConfirm, MethodReview, "plugin:gone:check", "eval:suite", "made-up"}
	faults := map[string]func(*world){
		"nothing wired": func(w *world) {},
		"errors": func(w *world) {
			bad := &fakeProbe{read: func() (Reading, error) { return Reading{}, errors.New("boom") }}
			w.probes["live"], w.probes["scheduled_run"], w.probes["build_commit"], w.probes[MethodReceipt] = bad, bad, bad, bad
			w.reviewer = &fakeReviewer{err: errors.New("boom")}
		},
		"empty replies": func(w *world) {
			empty := &fakeProbe{read: func() (Reading, error) { return Reading{}, nil }}
			w.probes["live"], w.probes["scheduled_run"], w.probes["build_commit"], w.probes[MethodReceipt] = empty, empty, empty, empty
			w.reviewer = &fakeReviewer{}
		},
	}
	for fault, setup := range faults {
		for _, m := range methods {
			t.Run(fault+"/"+m, func(t *testing.T) {
				w := newWorld(t, setup)
				w.frozen("r1", "x", oneAsk, Item{ID: "i", Statement: "s", From: oneAsk, Method: m, Effect: "work"})
				w.cover("r1")
				wantNotMet(t, w.work("r1", "c1"), "i", "")
			})
		}
	}
	t.Run("only explicit results with evidence count", func(t *testing.T) {
		for _, r := range []Result{{}, {Value: "yes", Evidence: []Evidence{{}}}, {Value: ResultMet}} {
			l := &List{Items: []Item{{ID: "i"}}, State: map[string]*ItemState{"i": {Result: r}}}
			if effective(l, l.Items[0]).Result.Value == ResultMet {
				t.Fatalf("%+v read met", r)
			}
		}
	})
	t.Run("consumers", func(t *testing.T) {
		if r := Read(nil); r.Status != StatusNoCriteria || r.Done || r.Label != "no criteria" {
			t.Fatalf("%+v", r)
		}
		w := newWorld(t)
		w.frozen("r1", "bug", oneAsk, fix())
		w.cover("r1")
		w.work("r1", "c1")
		if !w.svc.Disposition("r1").Done || w.svc.Disposition("none").Status != StatusNoCriteria {
			t.Fatal("disposition")
		}
		w.mode = ModeOff
		if r := w.svc.Disposition("r1"); r.Done || r.Status != StatusNoCriteria {
			t.Fatalf("mode off: %+v", r)
		}
		for _, mode := range []string{ModeOff, "", "enforce"} {
			w.mode = mode
			if _, err := w.svc.Evaluate(context.Background(), "r1"); !errors.Is(err, ErrOff) {
				t.Fatalf("mode %q: %v", mode, err)
			}
			if _, err := w.svc.Edit(context.Background(), EditInput{RequestID: "r2", Ask: "a"}, operator); !errors.Is(err, ErrOff) {
				t.Fatalf("mode %q: %v", mode, err)
			}
		}
	})
}

func TestModeOffWritesNothing(t *testing.T) {
	w := newWorld(t, func(w *world) { w.mode = ModeOff })
	_, _ = w.svc.Edit(context.Background(), EditInput{RequestID: "r1", Ask: "a"}, operator)
	_ = w.svc.NoteStart("r1", "x")
	if _, err := os.Stat(w.path); !os.IsNotExist(err) {
		t.Fatalf("store written while off: %v", err)
	}
}

func TestWaitingSpendsNoRetryAndUsageHoldWaits(t *testing.T) {
	rev := &fakeReviewer{settings: "m", value: ResultMet, held: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)}
	w := newWorld(t, func(w *world) { w.reviewer = rev })
	w.frozen("r1", "x", oneAsk, judged())
	w.cover("r1")
	l := w.work("r1", "c1")
	wantStatus(t, l, StatusWaiting)
	if rev.calls != 0 || len(l.State["j"].InfraTries) != 0 {
		t.Fatalf("calls=%d tries=%v", rev.calls, l.State["j"].InfraTries)
	}
	ov := must[Overview](t)(w.svc.Overview())
	if len(ov.Groups) != 0 || ov.Results["waiting"] != 1 {
		t.Fatalf("waiting counted as the operator's action: %+v", ov)
	}
}

func TestDoneClosesWithItsVersions(t *testing.T) {
	rev := &fakeReviewer{settings: "m", value: ResultMet}
	w := newWorld(t, func(w *world) { w.reviewer = rev })
	it := judged()
	it.Effect = "ref:main"
	open := Item{ID: "c", Statement: "seen", From: oneAsk, Method: MethodConfirm}
	w.frozen("r1", "x", oneAsk, it, open)
	w.cover("r1")
	w.work("r1", "c1")
	before := rev.calls
	for i := range 20 { // unrelated commits on main while still open
		w.res.at["ref:main"] = "m" + strconv.Itoa(i)
		must[*List](t)(w.svc.Evaluate(context.Background(), "r1"))
	}
	must[*List](t)(w.svc.Confirm("r1", "c", true, "seen", "", "", operator))
	// A run closes it on the versions it reads; later commits do not reopen it.
	wantStatus(t, w.work("r1", "c1"), StatusDone)
	wantStatus(t, w.work("r1", "c2"), StatusDone)
	if rev.calls != before {
		t.Fatalf("reviewer runs = %d, want %d", rev.calls, before)
	}
}

func TestReworkCapAndScheduledOccurrence(t *testing.T) {
	w := newWorld(t)
	w.frozen("r1", "bug", oneAsk, fix())
	ok := func(subject string) bool {
		a, _, err := w.svc.RequestRework("r1", subject, operator)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	if !ok("a") || ok("a") {
		t.Fatal("a round needs a changed subject")
	}
	must[*List](t)(w.svc.NoteUnitReplaced("r1", "reset", "r1b", operator))
	if !ok("b") || ok("c") {
		t.Fatal("two rounds, counted across resets")
	}
	if l := must[*List](t)(w.svc.Get("r1")); !strings.Contains(l.Yours, "yours") {
		t.Fatalf("%q", l.Yours)
	}
	due := w.now.Add(time.Hour)
	occ := must[*List](t)(w.svc.NewOccurrence("r1", "2026-11-30", due))
	w.now = due.Add(48 * time.Hour)
	if Read(occ).Done || Due(occ, w.now, time.Hour) != "overdue" {
		t.Fatalf("a run that never fired must show: %+v", Read(occ))
	}
}

func TestOccurrenceResultMustBeTiedToItsRun(t *testing.T) {
	w := newWorld(t)
	reading := Reading{}
	w.probes["scheduled_run"] = &fakeProbe{read: func() (Reading, error) { return reading, nil }}
	it := Item{ID: "run", Statement: "the run finished", From: oneAsk, Method: "state:scheduled_run", State: &StateSpec{Want: "finished"}}
	w.frozen("r1", "x", oneAsk, it)
	due := w.now
	must[*List](t)(w.svc.NewOccurrence("r1", "w1", due))
	id := OccurrenceID("r1", "w1")
	w.now = due.Add(time.Minute)
	for name, r := range map[string]Reading{
		"last week's result": {Answered: true, Value: "finished", SourceAt: w.now, Occurrence: "w0", ResultAt: due.Add(-168 * time.Hour)},
		"made before due":    {Answered: true, Value: "finished", SourceAt: w.now, Occurrence: "w1", ResultAt: due.Add(-time.Second)},
		"only spawned":       {Answered: true, Value: "spawned", SourceAt: w.now, Occurrence: "w1", ResultAt: w.now},
	} {
		reading = r
		l := w.work(id, "c1")
		if read(l, "run").Result.Value == ResultMet {
			t.Fatalf("%s read met", name)
		}
	}
	reading = Reading{Answered: true, Value: "finished", SourceAt: w.now, Occurrence: "w1", ResultAt: w.now}
	if read(w.work(id, "c1"), "run").Result.Value != ResultMet {
		t.Fatal("the real run did not count")
	}
}

func TestStandingChecksSeePlantedFaults(t *testing.T) {
	w := newWorld(t)
	w.frozen("ok", "bug", oneAsk, fix())
	w.now = w.now.Add(time.Minute)
	if err := w.svc.NoteStart("ok", "bug"); err != nil {
		t.Fatal(err)
	}
	find := func(check, id, text string) bool {
		for _, f := range must[Report](t)(w.svc.Report()).Findings {
			if f.Check == check && f.RequestID == id && strings.Contains(f.Text, text) {
				return true
			}
		}
		return false
	}
	if n := len(must[Report](t)(w.svc.Report()).Findings); n != 0 {
		t.Fatalf("clean state has %d findings", n)
	}
	if err := w.svc.NoteStart("nolist", "x"); err != nil {
		t.Fatal(err)
	}
	if !find(CheckFrozenBeforeStart, "nolist", "no list") {
		t.Fatal("I18: start without a list")
	}
	if err := w.svc.NoteStart("late", "x"); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(time.Minute)
	w.frozen("late", "bug", oneAsk, fix())
	if !find(CheckFrozenBeforeStart, "late", "after its first notice") {
		t.Fatal("I18: fixed after the start")
	}
	if err := w.svc.NoteClaimedDone("ok"); err != nil {
		t.Fatal(err)
	}
	if !find(CheckDoneIsProven, "ok", "reported done") || !find(CheckDoneIsProven, "ok", "no current proof") {
		t.Fatal("I19")
	}
	must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "ok", Upsert: []Item{{ID: "x", Statement: "new", Method: MethodConfirm}}}, worker))
	if !find(CheckChangesRecorded, "ok", "no words") || !find(CheckDoneIsProven, "ok", "since revision 1") {
		t.Fatal("I20 no words, I19 revision 1")
	}
	must[*List](t)(w.svc.AddAskWords("late", "one more thing", operator))
	if find(CheckChangesRecorded, "late", "without a record") {
		t.Fatal("recorded ask words reported as unrecorded")
	}
	// Planted: the stored list is edited behind the service's back.
	w.svc.data.Lists["late"].Items[0].Statement = "easier"
	if !find(CheckChangesRecorded, "late", "without a record") {
		t.Fatal("I20 silent change")
	}
}

func TestStoreKeepsItsData(t *testing.T) {
	w := newWorld(t)
	w.frozen("r1", "bug", oneAsk, fix())
	w.open()
	if l := must[*List](t)(w.svc.Get("r1")); l.Ask.Text != oneAsk || !l.Frozen() {
		t.Fatalf("%+v", l)
	}
	if err := os.WriteFile(w.path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Path: w.path}); err == nil {
		t.Fatal("a damaged store opened as empty")
	}
	if raw, _ := os.ReadFile(w.path); string(raw) != "{broken" {
		t.Fatal("damaged store was overwritten")
	}
	key := make([]byte, 32)
	path := filepath.Join(t.TempDir(), "enc.json")
	s := must[*Service](t)(Open(Options{Path: path, Key: key, Mode: func() string { return ModeReport }}))
	must[*List](t)(s.Edit(context.Background(), EditInput{RequestID: "r", Ask: "a very private ask"}, operator))
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), "private") {
		t.Fatalf("ask stored in clear: %v", err)
	}
	if _, err := Open(Options{Path: path}); err == nil {
		t.Fatal("encrypted store opened without a key")
	}
}

func TestChangesMadeDuringARunAreKept(t *testing.T) {
	w := newWorld(t)
	ok := Item{ID: "c", Statement: "seen", From: oneAsk, Method: MethodConfirm}
	w.frozen("r1", "bug", oneAsk, fix(), ok)
	w.runner.hook = func() {
		w.runner.hook = nil
		must[*List](t)(w.svc.Confirm("r1", "c", true, "seen", "", "", operator))
	}
	l := w.work("r1", "c1")
	if read(l, "c").Result.Value != ResultMet || read(l, "fix").Result.Value != ResultMet {
		t.Fatalf("%+v", Read(l).Items)
	}
	// A unit replaced during a run discards that run's results.
	w.runner.out["c2"] = afterPass
	w.runner.hook = func() {
		w.runner.hook = nil
		must[*List](t)(w.svc.NoteUnitReplaced("r1", "reset", "r1", operator))
	}
	w.res.at[""] = "c2"
	if _, err := w.svc.Evaluate(context.Background(), "r1"); !errors.Is(err, ErrListChanged) {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultsAreDataAndOverridable(t *testing.T) {
	d, err := LoadDefaults("")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []Kind{KindDeliverable, KindChange, KindDelivery, KindOther} {
		for _, it := range d.Kinds[k] {
			if it.Method == "" || it.ID == "" || it.Statement == "" {
				t.Fatalf("%s: %+v", k, it)
			}
		}
	}
	p := filepath.Join(t.TempDir(), "d.yaml")
	if err := os.WriteFile(p, []byte("kinds:\n  delivery:\n    - {id: x, statement: y, method: receipt}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o, err := LoadDefaults(p)
	if err != nil || o.Kinds[KindDelivery][0].ID != "x" || len(o.Kinds[KindChange]) == 0 || o.Version == d.Version {
		t.Fatalf("%v %+v", err, o)
	}
	if _, err := LoadDefaults(p + ".missing"); err == nil {
		t.Fatal("a missing defaults file must not silently use fewer defaults")
	}
	w := newWorld(t)
	l := must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "s", Source: "schedule", Ask: "weekly report", Packs: []string{"software"}}, operator))
	for _, id := range []string{"s-run", "s-missed", "p-docs"} {
		if _, it := l.item(id); it == nil {
			t.Fatalf("missing %s", id)
		}
	}
}

func TestNothingAlreadyTrueClosesAChange(t *testing.T) {
	w := newWorld(t)
	w.runner.out["c0"] = afterPass
	it := fix()
	it.Change, it.Before = false, BeforeNone // a fix marked as not a change
	w.frozen("r1", "bug", oneAsk, it)
	w.cover("r1")
	l := w.work("r1", "c1")
	if Read(l).Done || !read(l, "fix").Result.AlreadyTrue {
		t.Fatalf("%+v", Read(l))
	}
}

func TestAnUnknownSourceIsHeldToTheReportOnlyRule(t *testing.T) {
	ask := "The update runs or raises an alert"
	w := newWorld(t)
	w.frozen("r1", "something-new", ask, Item{ID: "c", Statement: ask, From: ask, Method: MethodConfirm})
	w.cover("r1")
	wantStatus(t, must[*List](t)(w.svc.Confirm("r1", "c", true, "it alerted", "", "", operator)), StatusReportOnly)
}

func TestAMessageWithMoreItemsNeedsCoverage(t *testing.T) {
	w := newWorld(t)
	w.probes[MethodReceipt] = &fakeProbe{read: func() (Reading, error) { return fetched("x", w.now), nil }}
	w.sourced("r1", "message", "Tell them and fix the roster", Item{ID: "c", Statement: "told", Method: MethodConfirm})
	must[error](t)(nil, w.svc.NoteHolder("r1", "x"))
	w.work("r1", "c1")
	wantStatus(t, must[*List](t)(w.svc.Confirm("r1", "c", true, "told", "", "", operator)), StatusNotReviewed)
}

func TestAConfirmationOfTheWorkBelongsToItsVersion(t *testing.T) {
	w := newWorld(t)
	seen := Item{ID: "c", Statement: "I read the result", From: oneAsk, Method: MethodConfirm, Effect: "work"}
	open := Item{ID: "o", Statement: "later", From: oneAsk, Method: MethodConfirm}
	w.frozen("r1", "x", oneAsk, seen, open)
	w.res.at[""] = "c1"
	l := must[*List](t)(w.svc.Confirm("r1", "c", true, "read it", "", "", operator))
	if read(l, "c").Result.Value != ResultMet {
		t.Fatalf("%+v", read(l, "c"))
	}
	l = w.work("r1", "c2")
	if got := read(l, "c").Result; got.Value != ResultUnchecked || !strings.Contains(got.Reason, "stale") {
		t.Fatalf("confirmation survived a changed subject: %+v", got)
	}
}

// Without caller rights (those come later), the proof is: a session changes
// its list, the change shows, the saved check still runs on the item it did
// not touch, and no plain done appears.
func TestASessionChangesItsListAndNoPlainDoneAppears(t *testing.T) {
	w := newWorld(t)
	w.frozen("r1", "bug", oneAsk, fix())
	if err := w.svc.NoteHolder("r1", "w1"); err != nil {
		t.Fatal(err)
	}
	w.set.ProjectChecks["unit"] = ProjectCheck{Command: "true"}
	l := must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "r1", Upsert: []Item{{ID: "extra", Statement: "also fine", Method: MethodConfirm}}}, worker))
	if ch := l.ChangesSinceFirst(); len(ch.Added) != 1 || ch.Added[0] != "extra" {
		t.Fatalf("change not shown: %+v", ch)
	}
	must[*List](t)(w.svc.Confirm("r1", "extra", true, "yes", "", "", worker))
	l = w.work("r1", "c1")
	if _, it := l.item("fix"); it.Check.Command != "make test" || read(l, "fix").Result.Value != ResultMet {
		t.Fatalf("the saved check did not run: %+v", read(l, "fix"))
	}
	if r := Read(l); r.Done || r.PlainDone || read(l, "extra").Claim == "" {
		t.Fatalf("%+v", r)
	}
	// Even with the operator answering everything, the changed list is shown.
	must[*List](t)(w.svc.Confirm("r1", "extra", true, "yes", "", "", operator))
	wantStatus(t, w.cover("r1"), StatusChangedList)
}
