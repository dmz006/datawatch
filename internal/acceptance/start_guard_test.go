package acceptance

import (
	"context"
	"strings"
	"testing"
	"time"
)

// plain is a command check that does not claim to show a change.
func plain(id, check string) Item {
	return Item{ID: id, Statement: id + " passes", From: oneAsk, Method: "project:" + check, Effect: "work"}
}

// fixedFrom fixes a list of exactly these items under an unknown source.
func fixedFrom(w *world, items ...Item) {
	w.t.Helper()
	dropDefaults(w, must[*List](w.t)(w.svc.Edit(context.Background(), EditInput{RequestID: "r", Source: "ticket", Ask: oneAsk, Upsert: items}, operator)))
	w.cover("r")
}

func wantUnknownStart(t *testing.T, l *List, id string) {
	t.Helper()
	if got := Read(l); got.Done {
		t.Fatalf("done on an unknown start: %s %+v", got.Status, got.Items)
	}
	if r := read(l, id).Result; r.Value != ResultCannotTell || !strings.Contains(r.Reason, "start") {
		t.Fatalf("%s reads %q (%s), want cannot tell for the unknown start", id, r.Value, r.Reason)
	}
}

func TestAnUnknownStartNeverClosesAnItem(t *testing.T) {
	// The check has no listed inputs, so nothing else stops it.
	bare := func(w *world) {
		w.set.ProjectChecks["bare"] = ProjectCheck{Command: "make test", Expect: []Expect{{Pass: passLine}}}
	}
	for name, start := range map[string]RunOutcome{
		"timed out":      {ExitCode: -1, Reason: "timed out"},
		"missing tool":   {ExitCode: 127, Reason: "a tool the check needs could not run"},
		"output was cut": {Finished: true, Cut: true, Output: passLine},
	} {
		t.Run("start run "+name, func(t *testing.T) {
			w := newWorld(t, bare)
			w.runner.out["c0"] = start
			fixedFrom(w, plain("t", "bare"))
			w.runner.out["c0"] = afterPass
			wantUnknownStart(t, w.work("r", "c0"), "t")
		})
	}
	t.Run("no start version", func(t *testing.T) {
		w := newWorld(t, bare)
		w.runner.out["c0"] = afterPass
		delete(w.res.at, "")
		fixedFrom(w, plain("t", "bare"))
		wantUnknownStart(t, w.work("r", "c0"), "t")
	})
	t.Run("unreadable start of a file", func(t *testing.T) {
		w := newWorld(t)
		w.repo["c1"]["notes.md"] = "the heading"
		delete(w.res.at, "")
		it := Item{ID: "f", Statement: "the notes have the heading", From: oneAsk, Method: "state:file", Effect: "work", State: &StateSpec{Path: "notes.md", Contains: "heading"}}
		fixedFrom(w, it)
		wantUnknownStart(t, w.work("r", "c1"), "f")
	})
	t.Run("a source silent at the start", func(t *testing.T) {
		w := newWorld(t)
		answer := Reading{}
		w.probes["live"] = &fakeProbe{read: func() (Reading, error) { return answer, nil }}
		w.open()
		fixedFrom(w, Item{ID: "s", Statement: "the service says up", From: oneAsk, Method: "state:live", State: &StateSpec{Want: "up"}})
		answer = Reading{Answered: true, Value: "up", SourceAt: w.now}
		wantUnknownStart(t, w.work("r", "c1"), "s")
	})
	t.Run("a known start still counts", func(t *testing.T) {
		w := newWorld(t, bare)
		w.runner.out["c0"] = RunOutcome{Finished: true, ExitCode: 1}
		w.runner.out["c1"] = afterPass
		fixedFrom(w, plain("t", "bare"))
		wantStatus(t, w.work("r", "c1"), StatusDone)
	})
}

// guardWorld has two checks that pass before and after, beside the change
// check "unit".
func guardWorld(t *testing.T) *world {
	pass := func(line string) map[string]RunOutcome {
		o := RunOutcome{Finished: true, Output: line}
		return map[string]RunOutcome{"c0": o, "c1": o}
	}
	return newWorld(t, func(w *world) {
		w.set.ProjectChecks["lint"] = ProjectCheck{Command: "make lint", Expect: []Expect{{Pass: "lint ok"}}}
		w.set.ProjectChecks["vet"] = ProjectCheck{Command: "make vet", Expect: []Expect{{Pass: "vet ok"}}}
		w.runner.byCommand = map[string]map[string]RunOutcome{"make lint": pass("lint ok"), "make vet": pass("vet ok")}
	})
}

func accept(w *world, item string) *List {
	w.t.Helper()
	return must[*List](w.t)(w.svc.Accept("r", item, "no time", "ok", "", operator))
}

func evaluated(w *world) *List {
	w.t.Helper()
	return must[*List](w.t)(w.svc.Evaluate(context.Background(), "r"))
}

func TestAcceptingAnUnrelatedItemKeepsTheNoChangeGuard(t *testing.T) {
	w := guardWorld(t)
	fixedFrom(w, plain("lint", "lint"), looked())
	accept(w, "look")
	if got := Read(evaluated(w)); got.Done {
		t.Fatalf("nothing changed and an unrelated item was accepted: %s", got.Status)
	}

	// Accepting one of two checks that were already true leaves the other.
	w = guardWorld(t)
	fixedFrom(w, plain("lint", "lint"), plain("vet", "vet"))
	evaluated(w)
	accept(w, "lint")
	if got := Read(evaluated(w)); got.Done {
		t.Fatalf("one of two accepted: %s", got.Status)
	}
}

// The ways an acceptance may end the guard: each is about the missing proof of
// work itself, and the list never reads plain done.
func TestAcceptingTheMissingProofReadsDoneAccepted(t *testing.T) {
	// The already true check itself.
	w := guardWorld(t)
	fixedFrom(w, plain("lint", "lint"), looked())
	must[*List](t)(w.svc.Confirm("r", "look", true, "yes", "", "", operator))
	evaluated(w)
	l := accept(w, "lint")
	if !read(l, "lint").Accepted {
		t.Fatalf("an accepted already-true check does not read accepted: %+v", read(l, "lint"))
	}
	wantStatus(t, evaluated(w), StatusDoneAccepted)

	// The check that had to show the change, beside a guard.
	w = guardWorld(t)
	fixedFrom(w, fix(), plain("lint", "lint"))
	w.runner.out["c1"] = RunOutcome{Finished: true, ExitCode: 1}
	w.work("r", "c1")
	accept(w, "fix")
	wantStatus(t, evaluated(w), StatusDoneAccepted)
}

func TestARunOfNoTestsIsNotAPass(t *testing.T) {
	const pkg = "ok  \texample/export"
	for out, met := range map[string]bool{
		pkg + "\t0.002s":                                 true,
		pkg + "\t(cached)":                               true,
		pkg + "\t0.002s [no tests to run]":               false,
		pkg + "\t(cached) [no tests to run]":             false,
		pkg + "\t0.002s\tcoverage: 0.0% [no test files]": false,
		pkg + " - no tests ran":                          false,
		pkg + ": no tests found":                         false,
		pkg + ": collected 0 items":                      false,
	} {
		r := judgeRun(Item{Check: &CheckSpec{Expect: []Expect{{Pass: pkg}}}}, &ItemState{}, RunOutcome{Finished: true, Output: out})
		if (r.Value == ResultMet) != met {
			t.Errorf("%q -> %s (%s)", out, r.Value, r.Reason)
		}
	}
}

// A window's readings are not tied to a scheduled run, so a run's own list gets
// no pass on a window whose start is unknown.
func TestAWindowOnARunsListNeedsAKnownStartToo(t *testing.T) {
	w := newWorld(t)
	answer := Reading{}
	w.probes["live"] = &fakeProbe{read: func() (Reading, error) { return answer, nil }}
	w.open()
	quiet := Item{ID: "q", Statement: "it stays up", From: oneAsk, Method: "state:live", State: &StateSpec{Want: "up"}, Wait: &WaitSpec{WindowSec: 60, MaxGapSec: 60}}
	fixedFrom(w, quiet)
	due := w.now
	must[*List](t)(w.svc.NewOccurrence("r", "d2", due))
	id := OccurrenceID("r", "d2")
	var l *List
	for _, at := range []time.Duration{0, 30 * time.Second, 60 * time.Second} {
		w.now = due.Add(at)
		answer = Reading{Answered: true, Value: "up", SourceAt: w.now}
		l = must[*List](t)(w.svc.Evaluate(context.Background(), id))
	}
	wantUnknownStart(t, l, "q")
}
