package acceptance

import (
	"context"
	"testing"
)

var callerSources = []string{"message", "intake", "handoff", "question", "report", "research", "design", "docs", "automaton", "bug", "code", "operation", "rule", "schedule", "ticket", ""}

func draftFrom(w *world, id, source, ask string, items ...Item) *List {
	w.t.Helper()
	return must[*List](w.t)(w.svc.Edit(context.Background(), EditInput{RequestID: id, Source: source, Ask: ask, Upsert: items}, operator))
}

// dropDefaults removes every default item from a draft and fixes it.
func dropDefaults(w *world, l *List) *List {
	w.t.Helper()
	var drop []string
	for _, it := range l.Items {
		if it.Source == SourceDefault {
			drop = append(drop, it.ID)
		}
	}
	if len(drop) > 0 {
		l = must[*List](w.t)(w.svc.Edit(context.Background(), EditInput{RequestID: l.RequestID, Remove: drop}, operator))
	}
	return must[*List](w.t)(w.svc.Freeze(context.Background(), l.RequestID, l.Hash(), operator, "start"))
}

// A caller's choice of source only adds items. It never picks lighter rules.
func TestACallersSourceOnlyMakesAListStricter(t *testing.T) {
	const ask = "Fix the export crash"
	base := draftFrom(newWorld(t), "r", "nothing-known", ask)
	for _, source := range callerSources {
		w := newWorld(t)
		l := draftFrom(w, "r", source, ask)
		if l.Kind != KindOther && l.Kind != KindChange {
			t.Errorf("%q: a caller got kind %q", source, l.Kind)
		}
		for _, it := range base.Items {
			if _, got := l.item(it.ID); got == nil {
				t.Errorf("%q: item %q is missing", source, it.ID)
			}
		}
		if _, got := l.item(askItemID(ask)); got == nil {
			t.Errorf("%q: the ask opened no item: %+v", source, l.Items)
		}
	}
	// The defaults of the chosen source are added.
	if _, it := draftFrom(newWorld(t), "r", "message", ask).item("d-received"); it == nil {
		t.Error("message added no receipt item")
	}
	// The rule against only reporting a failure holds for every choice.
	const alert = "The update runs or raises an alert"
	for _, source := range callerSources {
		w := newWorld(t)
		dropDefaults(w, draftFrom(w, "r", source, alert, Item{ID: "c", Statement: alert, From: alert, Method: MethodConfirm}))
		if source == "schedule" {
			continue
		}
		w.cover("r")
		l := must[*List](t)(w.svc.Confirm("r", "c", true, "it alerted", "", "", operator))
		if got := Read(l); got.Status != StatusReportOnly {
			t.Errorf("%q: reads %s %v", source, got.Status, got.Reasons)
		}
	}
}

// The review's live case: the export crash sent as a message, its receipt item
// removed, one yes on another item and the coverage answer.
func TestACallersMessageListKeepsTheAsk(t *testing.T) {
	w := newWorld(t)
	const ask = "Fix the export crash"
	draftFrom(w, "r", "message", ask, Item{ID: "told", Statement: "someone was told", Method: MethodConfirm})
	l := must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "r", Remove: []string{"d-received"}}, operator))
	must[*List](t)(w.svc.Freeze(context.Background(), "r", l.Hash(), operator, "start"))
	w.cover("r")
	must[*List](t)(w.svc.Confirm("r", "told", true, "yes", "", "", operator))
	l = must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
	if got := Read(l); got.Done || read(l, askItemID(ask)).Result.Value != ResultUnchecked {
		t.Fatalf("%s done=%v: %+v", got.Status, got.Done, got.Items)
	}
}

func TestOnlyADeliveryListReadsDeliveredOrFiled(t *testing.T) {
	for _, source := range []string{"message", "intake"} {
		w := newWorld(t)
		r := Reading{}
		receipts(w, &r)
		draftFrom(w, "r", source, "tell Sam", Item{ID: "got", Statement: "it arrived", From: "tell Sam", Method: MethodReceipt, State: &StateSpec{Target: "sam"}})
		l := must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "r", Remove: []string{"d-received"}}, operator))
		must[*List](t)(w.svc.Freeze(context.Background(), "r", l.Hash(), operator, "start"))
		r = fetched("sam", w.now)
		l = must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
		if got := Read(l); got.Status != StatusThinList {
			t.Errorf("%s from a caller reads %s", source, got.Status)
		}
	}
}

// Words added to a delivery draft open items too.
func TestLaterWordsOnADeliveryDraftOpenAnItem(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r", Source: "message", SourceKnown: true, Ask: "tell Sam"}, operator))
	l := must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r", AddAsk: "and also delete the old export", Remove: []string{"d-received"}, Upsert: []Item{looked()}}, operator))
	id := askItemID("and also delete the old export")
	if _, it := l.item(id); it == nil {
		t.Fatalf("the later words opened no item: %+v", l.Items)
	}
	if _, it := l.item(askItemID("tell Sam")); it != nil {
		t.Fatal("the message itself was read as a list of asks")
	}
	if _, err := w.svc.Edit(ctx, EditInput{RequestID: "r", Remove: []string{id}}, operator); err == nil {
		t.Fatal("the later words were dropped before start")
	}
	must[*List](t)(w.svc.Freeze(ctx, "r", l.Hash(), operator, "start"))
	w.cover("r")
	if got := Read(must[*List](t)(w.svc.Confirm("r", "look", true, "yes", "", "", operator))); got.Done {
		t.Fatalf("%+v", got)
	}
}

// On a list datawatch drafted itself, removing or changing one of its default
// items before the start still shows.
func TestADroppedDaemonDefaultShowsAsAChange(t *testing.T) {
	ctx := context.Background()
	for name, edit := range map[string]EditInput{
		"removed": {RequestID: "r", Remove: []string{"d-live-state", "d-connected"}},
		"altered": {RequestID: "r", Remove: []string{"d-connected"}, Upsert: []Item{{ID: "d-live-state", Statement: "fine", Method: MethodConfirm}}},
	} {
		w := newWorld(t)
		must[*List](t)(w.svc.Edit(ctx, EditInput{RequestID: "r", Source: "bug", SourceKnown: true, Ask: oneAsk, Upsert: []Item{fix()}}, operator))
		l := must[*List](t)(w.svc.Edit(ctx, edit, operator))
		must[*List](t)(w.svc.Freeze(ctx, "r", l.Hash(), operator, "start"))
		if name == "altered" {
			must[*List](t)(w.svc.Confirm("r", "d-live-state", true, "yes", "", "", operator))
		}
		w.cover("r")
		l = w.work("r", "c1")
		if got := Read(l); got.Status != StatusChangedList {
			t.Errorf("%s: reads %s %v", name, got.Status, got.Reasons)
		}
		if !l.ChangesSinceFirst().Any() {
			t.Errorf("%s: no change shown", name)
		}
	}
}

// noChange is a check that passes before and after: nothing shows any work.
func noChange(t *testing.T, source string, known bool, more ...Item) *List {
	t.Helper()
	w := newWorld(t)
	w.runner.out["c0"] = afterPass
	it := Item{ID: "t", Statement: "the export test passes", From: oneAsk, Method: "project:unit", Effect: "work"}
	l := must[*List](t)(w.svc.Edit(context.Background(), EditInput{RequestID: "r", Source: source, SourceKnown: known, Ask: oneAsk, Upsert: append([]Item{it}, more...)}, operator))
	dropDefaults(w, l)
	w.cover("r")
	for _, m := range more {
		if m.Method == MethodConfirm {
			must[*List](t)(w.svc.Confirm("r", m.ID, true, "yes", "", "", operator))
		}
	}
	return must[*List](t)(w.svc.Evaluate(context.Background(), "r"))
}

func TestNoChangeChecksNeverReachDone(t *testing.T) {
	for name, c := range map[string]struct {
		source string
		known  bool
		more   []Item
	}{
		"unknown source":                    {source: "ticket"},
		"unknown source and a yes":          {source: "ticket", more: []Item{looked()}},
		"automaton, defaults removed":       {source: "automaton"},
		"automaton, defaults removed, yes":  {source: "automaton", more: []Item{looked()}},
		"a daemon's report source, and yes": {source: "report", known: true, more: []Item{looked()}},
		"a caller's message, and yes":       {source: "message", more: []Item{looked()}},
	} {
		l := noChange(t, c.source, c.known, c.more...)
		if got := Read(l); got.Done {
			t.Errorf("%s: reads %s with nothing changed", name, got.Status)
		}
	}
}

// A check that already passed may stand beside one that shows the work.
func TestAGuardCheckBesideAProvenChangeStillReadsDone(t *testing.T) {
	w := newWorld(t)
	w.set.ProjectChecks["guard"] = ProjectCheck{Command: "make lint", Expect: []Expect{{Pass: "lint ok"}}}
	w.runner.out["c0"] = RunOutcome{Finished: true, ExitCode: 1, Output: failLine + "\nlint ok"}
	w.runner.out["c1"] = RunOutcome{Finished: true, Output: passLine + "\nlint ok"}
	guard := Item{ID: "guard", Statement: "lint still passes", From: oneAsk, Method: "project:guard", Effect: "work"}
	// The guard's start run passes: it exits 0 and shows its line.
	w.runner.byCommand = map[string]map[string]RunOutcome{"make lint": {"c0": {Finished: true, Output: "lint ok"}}}
	w.frozen("r", "bug", oneAsk, fix(), guard)
	w.cover("r")
	l := w.work("r", "c1")
	if !read(l, "guard").Result.AlreadyTrue {
		t.Fatalf("the guard did not pass at the start: %+v", read(l, "guard"))
	}
	wantStatus(t, l, StatusDone)
}
