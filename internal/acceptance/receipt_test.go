package acceptance

import (
	"context"
	"testing"
	"time"
)

// sourced makes a fixed list the way one of datawatch's own request sources
// would: it saw where the request came from.
func (w *world) sourced(id, source, ask string, items ...Item) *List {
	w.t.Helper()
	l := must[*List](w.t)(w.svc.Edit(context.Background(), EditInput{RequestID: id, Source: source, SourceKnown: true, Ask: ask, Upsert: items}, operator))
	return must[*List](w.t)(w.svc.Freeze(context.Background(), id, l.Hash(), operator, "start"))
}

// hold records a session that does the work.
func (w *world) hold(list, session string) {
	w.t.Helper()
	if err := w.svc.NoteHolder(list, session); err != nil {
		w.t.Fatal(err)
	}
}

// receipts wires a receipt source that answers with whatever *r holds.
func receipts(w *world, r *Reading) {
	w.probes[MethodReceipt] = &fakeProbe{read: func() (Reading, error) { return *r, nil }}
	w.open()
}

func fetched(by string, at time.Time) Reading {
	return Reading{Answered: true, Value: "fetched", By: by, SourceAt: at}
}

func named(target string) Item {
	return Item{ID: "got", Statement: "it arrived", Method: MethodReceipt, State: &StateSpec{Target: target}}
}

func receiptValue(w *world, list, item string) ItemRead {
	w.t.Helper()
	return read(must[*List](w.t)(w.svc.Evaluate(context.Background(), list)), item)
}

func wantReceipt(t *testing.T, got ItemRead, met bool, what string) {
	t.Helper()
	if (got.Result.Value == ResultMet) != met {
		t.Fatalf("%s: reads %q (%s), want met=%v", what, got.Result.Value, got.Result.Reason, met)
	}
}

func TestReceiptNeedsTheHolderOrTheNamedRecipient(t *testing.T) {
	w := newWorld(t)
	r := Reading{}
	receipts(w, &r)
	w.sourced("nobody", "message", "tell Sam")
	r = fetched("unrelated-session", w.now)
	wantReceipt(t, receiptValue(w, "nobody", "d-received"), false, "no holder and no named recipient")

	w.sourced("held", "message", "tell Sam")
	w.hold("held", "w1")
	wantReceipt(t, receiptValue(w, "held", "d-received"), false, "a stranger while a holder is recorded")
	r = fetched("w1", w.now)
	wantReceipt(t, receiptValue(w, "held", "d-received"), true, "the holder")

	w.sourced("named", "message", "tell Sam", named("sam"))
	r = fetched("someone-else", w.now)
	wantReceipt(t, receiptValue(w, "named", "got"), false, "not the named recipient")
	r = fetched("sam", w.now)
	wantReceipt(t, receiptValue(w, "named", "got"), true, "the named recipient")
}

// On a list that carries work the holder is the doer, so its own fetch does not
// show the work reached anyone.
func TestReceiptOnAWorkListNeedsANamedRecipientWhoIsNotTheDoer(t *testing.T) {
	w := newWorld(t)
	r := Reading{}
	receipts(w, &r)
	w.sourced("rep", "report", "write it up", named("boss"))
	w.hold("rep", "w1")
	r = fetched("w1", w.now)
	wantReceipt(t, receiptValue(w, "rep", "d-delivered"), false, "the doer fetched its own report")
	r = fetched("boss", w.now)
	wantReceipt(t, receiptValue(w, "rep", "got"), true, "the named recipient")

	w.sourced("self", "report", "write it up", named("w1"))
	w.hold("self", "w1")
	r = fetched("w1", w.now)
	wantReceipt(t, receiptValue(w, "self", "got"), false, "the doer named as its own recipient")
}

func TestReceiptNeedsASourceTimeNewerThanTheAsk(t *testing.T) {
	w := newWorld(t)
	r := Reading{}
	receipts(w, &r)
	w.sourced("m", "message", "tell Sam", named("sam"))
	for what, at := range map[string]time.Time{
		"no time at all":  {},
		"before the ask":  w.now.Add(-time.Second),
		"from the future": w.now.Add(time.Hour),
	} {
		r = fetched("sam", at)
		wantReceipt(t, receiptValue(w, "m", "got"), false, what)
	}

	first := w.now
	w.now = w.now.Add(time.Hour)
	must[*List](t)(w.svc.AddAskWords("m", "and phone them", operator))
	r = fetched("sam", first)
	wantReceipt(t, receiptValue(w, "m", "got"), false, "taken before the later words")
	r = fetched("sam", w.now)
	wantReceipt(t, receiptValue(w, "m", "got"), true, "taken after the later words")
}

func TestReceiptDoesNotCarryOverAReplacedUnit(t *testing.T) {
	w := newWorld(t)
	r := Reading{}
	receipts(w, &r)
	w.sourced("m", "message", "tell them")
	w.hold("m", "w1")
	first := w.now
	r = fetched("w1", first)
	wantReceipt(t, receiptValue(w, "m", "d-received"), true, "the holder")

	w.now = w.now.Add(time.Hour)
	must[*List](t)(w.svc.NoteUnitReplaced("m", "reassigned", "m2", operator))
	w.hold("m", "w2")
	wantReceipt(t, receiptValue(w, "m", "d-received"), false, "the earlier holder's old receipt")
	r = fetched("w1", w.now)
	wantReceipt(t, receiptValue(w, "m", "d-received"), false, "the earlier holder, no longer holding")
	r = fetched("w2", first)
	wantReceipt(t, receiptValue(w, "m", "d-received"), false, "a time before the replacement")
	r = fetched("w2", w.now)
	wantReceipt(t, receiptValue(w, "m", "d-received"), true, "the current holder")
	// An earlier holder still never counts as someone other than the doer.
	l := must[*List](t)(w.svc.Get("m"))
	if l.independent(worker) {
		t.Fatal("an earlier holder counts as independent")
	}
}

func TestReceiptBelongsToTheOccurrence(t *testing.T) {
	w := newWorld(t)
	r := Reading{}
	receipts(w, &r)
	w.sourced("daily", "message", "send the daily note", named("sam"))
	r = fetched("sam", w.now)
	r.Occurrence, r.ResultAt = "d2", w.now
	wantReceipt(t, receiptValue(w, "daily", "got"), false, "a run's receipt on the plain list")

	due := w.now.Add(24 * time.Hour)
	must[*List](t)(w.svc.NewOccurrence("daily", "d2", due))
	id := OccurrenceID("daily", "d2")
	got := receiptValue(w, id, "got")
	wantReceipt(t, got, false, "before the run is due")
	if got.Result.Waiting == nil {
		t.Fatalf("a run that is not due yet does not read waiting: %+v", got.Result)
	}

	w.now = due.Add(time.Minute)
	good := Reading{Answered: true, Value: "fetched", By: "sam", SourceAt: w.now, Occurrence: "d2", ResultAt: w.now}
	for what, bad := range map[string]func(*Reading){
		"no occurrence":         func(r *Reading) { r.Occurrence = "" },
		"another occurrence":    func(r *Reading) { r.Occurrence = "d1" },
		"made before it is due": func(r *Reading) { r.ResultAt = due.Add(-time.Hour) },
		"no result time":        func(r *Reading) { r.ResultAt = time.Time{} },
		"taken before due":      func(r *Reading) { r.SourceAt = due.Add(-time.Hour) },
	} {
		r = good
		bad(&r)
		wantReceipt(t, receiptValue(w, id, "got"), false, what)
	}
	r = good
	wantReceipt(t, receiptValue(w, id, "got"), true, "this run's receipt")
}

// A schedule's own list is not any one run, so no receipt can be tied to it.
func TestReceiptOnAScheduleItselfCannotBeTold(t *testing.T) {
	w := newWorld(t)
	r := Reading{}
	receipts(w, &r)
	w.sourced("sch", "schedule", "send the daily note", named("sam"))
	r = fetched("sam", w.now)
	wantReceipt(t, receiptValue(w, "sch", "got"), false, "no occurrence to tie it to")
}
