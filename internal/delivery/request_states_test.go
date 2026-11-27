// Part A's first failing tests 1-7: holders, take and seen, lost replies,
// keys, reports, done, and refused calls.

package delivery

import (
	"context"
	"errors"
	"testing"
	"time"
)

var bg = context.Background()

// 1. Holder accepted X; Y passes take-by; then the holder ends.
func TestReqStateHolderGoneRunsNothingTwice(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	x := f.create(ReqAsk)
	y := f.create(ReqAsk)
	f.take(x.ID, "a")
	f.accept(x.ID, "a")
	f.run(90 * time.Minute) // Y passes take-by
	if r := f.want(y.ID, StateWaiting); r.Holder == nil || r.Holder.SessionID != "a" || r.Delivery.FetchedAt != nil {
		t.Fatalf("a late take must move nothing: %+v", r)
	}
	f.want(x.ID, StateAccepted)
	if c, ok := f.listed()[y.ID]; !ok || c.Problem != WhyTakeByPassed {
		t.Fatalf("Y must be listed as past take-by: %+v", c)
	}
	if len(f.s.Bindings()) != 1 {
		t.Fatal("a late take must not end the role")
	}

	hello := f.create(ReqHandover) // read-only: may be sent again after a fetch
	f.take(hello.ID, "a")
	f.end("a")
	if r := f.want(x.ID, StateFailed); r.ReasonCode != WhyHolderGone {
		t.Fatalf("X: %+v", r)
	}
	if _, ok := f.listed()[x.ID]; !ok {
		t.Fatal("X must be listed")
	}
	if r := f.want(y.ID, StateWaiting); r.Holder != nil {
		t.Fatalf("Y keeps an ended holder: %+v", r.Holder)
	}
	if r := f.want(hello.ID, StateWaiting); r.Holder != nil || r.Delivery.FetchedAt != nil || len(r.FormerHolders) != 1 {
		t.Fatalf("a fetched read-only request goes again, with a new delivery record: %+v", r)
	}
	f.supervise("b", "claude-code")
	if r := f.want(y.ID, StateWaiting); r.Holder == nil || r.Holder.SessionID != "b" {
		t.Fatalf("never-fetched Y goes to the next holder: %+v", r.Holder)
	}
	f.want(x.ID, StateFailed)
}

// Fetched pi work that never counted is handed over: it fails and is listed.
func TestReqStateFetchedUncountedWorkFailsWithItsHolder(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("p", "pi")
	x := f.create(ReqAsk)
	f.take(x.ID, "p")
	f.want(x.ID, StateWaiting)
	f.end("p")
	f.want(x.ID, StateFailed)
	if _, ok := f.listed()[x.ID]; !ok {
		t.Fatal("not listed")
	}
}

// A session that ended while datawatch was down is found by the check. A
// request to one session has no next holder.
func TestReqStateEndedWithoutAReportIsFoundByTheCheck(t *testing.T) {
	f := newReqFx(t, nil)
	f.sessions.add("s", "claude-code")
	// Only datawatch itself creates a hello.
	v, _, err := f.s.Create(bg, Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon}, CreateInput{Kind: ReqHello,
		Role: RoleSession, Scope: Scope{Kind: ScopeSession, ID: "s"}, Key: "hello:s:ended", Content: RequestContent{Ask: "do the thing"}})
	if err != nil {
		t.Fatal(err)
	}
	f.sessions.end("s")
	f.reopen()
	f.tick(time.Minute)
	if r := f.want(v.ID, StateFailed); r.ReasonCode != WhyHolderGone {
		t.Fatalf("%+v", r)
	}
}

// 2. When a take counts, by worker family.
func TestReqStateTakeCountsByWorkerPolicy(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("c", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "c")
	if r := f.want(v.ID, StateDelivered); r.Delivery.FetchedAt == nil || r.Delivery.FetchedVia != ViaTool {
		t.Fatalf("%+v", r.Delivery)
	}

	g := newReqFx(t, nil)
	g.supervise("p", "pi")
	w := g.create(ReqAsk)
	// Seen before a take is refused.
	if _, err := g.s.Seen(bg, sessCaller("p"), w.ID, SeenInput{Digest: "00"}); !errors.Is(err, ErrRequestState) {
		t.Fatalf("seen first: %v", err)
	}
	rep := g.take(w.ID, "p")
	if r := g.want(w.ID, StateWaiting); r.Delivery.FetchedAt == nil {
		t.Fatal("pi: the take is recorded")
	}
	if rep.Request.Label != LabelFetched {
		t.Fatalf("label %q", rep.Request.Label)
	}
	// Accept before seen moves nothing.
	code, _ := g.s.Content(w.ID)
	if _, err := g.s.Accept(bg, sessCaller("p"), w.ID, AcceptInput{Code: "wrong"}); !errors.Is(err, ErrRequestInput) {
		t.Fatalf("a wrong code: %v", err)
	}
	if _, err := g.s.Accept(bg, sessCaller("p"), w.ID, AcceptInput{Code: code.ReceiptCode}); err != nil {
		t.Fatal(err)
	}
	if r := g.want(w.ID, StateWaiting); r.Delivery.AcceptedAt == nil || !r.Delivery.CodeOK {
		t.Fatalf("%+v", r.Delivery)
	}
	// Take-by keeps running while unconfirmed.
	g.run(90 * time.Minute)
	if c := g.listed()[w.ID]; c.Problem != WhyTakeByPassed {
		t.Fatalf("take-by stopped: %+v", c)
	}
	if _, err := g.s.Seen(bg, sessCaller("p"), w.ID, SeenInput{Digest: "00"}); !errors.Is(err, ErrRequestState) {
		t.Fatalf("a wrong digest: %v", err)
	}
	g.want(w.ID, StateWaiting)
	v2, err := g.s.Seen(bg, sessCaller("p"), w.ID, SeenInput{Digest: rep.Digest, Via: ViaProgram, ReporterVersion: "1.3.0"})
	if err != nil {
		t.Fatal(err)
	}
	if v2.State != StateAccepted || v2.Delivery.InModelCallAt == nil {
		t.Fatalf("after seen: %+v", v2)
	}
}

func TestReqStateAnUnprovenFamilyNeverCounts(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("o", "opencode")
	v := f.create(ReqAsk)
	rep := f.take(v.ID, "o")
	if _, err := f.s.Seen(bg, sessCaller("o"), v.ID, SeenInput{Digest: rep.Digest}); err != nil {
		t.Fatal(err)
	}
	f.want(v.ID, StateWaiting)
}

// 3. A lost take reply.
func TestReqStateRetakeGivesTheSameContent(t *testing.T) {
	f := newReqFx(t, testKey(1))
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk, func(in *CreateInput) {
		in.Content = RequestContent{Ask: "fix it", Detail: "the detail", Params: map[string]string{"b": "2", "a": "1"},
			Items: []DoneItem{{Statement: "the file exists", Method: "state:file", Path: "x.txt"}}}
	})
	first := f.take(v.ID, "a")
	rec := f.get(v.ID).Delivery
	f.now = f.now.Add(time.Minute)
	f.reopen()
	again := f.take(v.ID, "a")
	if first.Repeat || !again.Repeat || first.Content != again.Content || first.Digest != again.Digest || first.Digest == "" {
		t.Fatalf("first %+v\nagain %+v", first, again)
	}
	if got := f.get(v.ID).Delivery; !got.FetchedAt.Equal(*rec.FetchedAt) || got.Digest != rec.Digest {
		t.Fatalf("the delivery record changed: %+v -> %+v", rec, got)
	}
	if first.ResendOK {
		t.Fatal("an ask is not read-only")
	}
	for _, part := range []string{"fix it", "the detail", "the file exists", v.ID} {
		if !contains(first.Content, part) {
			t.Errorf("the content lacks %q:\n%s", part, first.Content)
		}
	}
	if len(again.Open) != 1 || again.Open[0] != v.ID {
		t.Fatalf("open: %v", again.Open)
	}
	// Accept and report answer the same again.
	f.accept(v.ID, "a")
	f.accept(v.ID, "a")
	f.report(v.ID, "a")
	f.report(v.ID, "a")
	f.want(v.ID, StateReported)
	mine, err := f.s.Mine(bg, sessCaller("a"))
	if err != nil || len(mine.Items) != 1 || len(mine.Roles) != 1 {
		t.Fatalf("mine: %+v %v", mine, err)
	}
}

// 4. A key names the occurrence.
func TestReqStateRepeatsAndStubs(t *testing.T) {
	f := newReqFx(t, testKey(2))
	f.supervise("a", "claude-code")
	obs := func(cond Condition) (RequestView, bool) {
		t.Helper()
		cond.Request = func(int) CreateInput {
			return CreateInput{Kind: ReqRules, Role: RoleSupervisor, Scope: projScope, Content: RequestContent{Ask: "same words"}}
		}
		v, created, err := f.s.Observe(bg, cond)
		if err != nil {
			t.Fatal(err)
		}
		return v, created
	}
	// A (the baseline), B, A: two requests.
	made := 0
	for _, val := range []string{"A", "B", "B", "A", "A"} {
		if _, created := obs(Condition{ID: "rules@proj", Value: val}); created {
			made++
		}
	}
	if made != 2 {
		t.Fatalf("A, B, A made %d requests, want 2", made)
	}
	var wakes []string
	for _, h := range []bool{true, true, false, true, true} {
		if v, created := obs(Condition{ID: "wake@proj", Holds: h}); created {
			wakes = append(wakes, v.ID)
		}
	}
	if len(wakes) != 2 {
		t.Fatalf("true, false, true made %d wakes, want 2", len(wakes))
	}
	// The same occurrence key returns the request already made.
	a := f.create(ReqAsk, func(in *CreateInput) { in.Key = "schedule:s1:100" })
	b, created, err := f.s.Create(bg, opCaller, CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "schedule:s1:100"})
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("same key: %v %v %s %s", created, err, a.ID, b.ID)
	}
	// Done, pruned, reopened: nothing new.
	f.take(a.ID, "a")
	f.accept(a.ID, "a")
	f.report(a.ID, "a")
	f.ver.set(a.ID, Verdict{Status: "done", Closes: true})
	f.tick(time.Minute)
	f.want(a.ID, StateDone)
	if err := f.s.prune(a.ID); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	if r := f.get(a.ID); !r.Pruned || r.KeyHash != KeyHash("schedule:s1:100") {
		t.Fatalf("stub: %+v", r)
	}
	if _, err := f.s.Content(a.ID); err == nil {
		t.Fatal("a stub has no content")
	}
	if _, created, err := f.s.Create(bg, opCaller, CreateInput{Kind: ReqAsk, Role: RoleSupervisor, Scope: projScope, Key: "schedule:s1:100"}); err != nil || created {
		t.Fatalf("made again after pruning: %v %v", created, err)
	}
	if has, err := f.s.Has(bg, "schedule:s1:100"); err != nil || !has {
		t.Fatalf("has: %v %v", has, err)
	}
	if _, created := obs(Condition{ID: "wake@proj", Holds: true}); created {
		t.Fatal("a condition still true made a request after a reopen")
	}
	if _, created := obs(Condition{ID: "rules@proj", Value: "A"}); created {
		t.Fatal("an unchanged value made a request after a reopen")
	}
}

func TestReqStateSourceGoneRemovesOnlySettledStubs(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	src := SourceRef{Type: "schedule", ID: "s9"}
	open := f.create(ReqAsk, func(in *CreateInput) { in.Source = src })
	gone := f.create(ReqAsk, func(in *CreateInput) { in.Source = src })
	failed := f.create(ReqAsk, func(in *CreateInput) { in.Source = src })
	if _, err := f.s.Drop(bg, opCaller, gone.ID, ReasonInput{ReasonCode: "obsolete"}); err != nil {
		t.Fatal(err)
	}
	f.take(failed.ID, "a")
	if _, err := f.s.Decline(bg, sessCaller("a"), failed.ID, DeclineInput{ReasonCode: "cannot"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SourceGone(bg, src); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.s.Request(gone.ID); ok {
		t.Fatal("a settled request of a gone source stays")
	}
	f.want(open.ID, StateWaiting)
	f.want(failed.ID, StateFailed)
	f.reopen()
	f.want(failed.ID, StateFailed)
}

// 5. A report never expires and nothing goes to the doer again.
func TestReqStateReportedNeverExpires(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "a")
	f.accept(v.ID, "a")
	f.report(v.ID, "a")
	before := f.get(v.ID)
	for day := 0; day < 365; day++ {
		f.tick(24 * time.Hour)
	}
	r := f.want(v.ID, StateReported)
	if r.Delivery != before.Delivery && r.Delivery.Reminders != before.Delivery.Reminders {
		t.Fatalf("something went to the doer: %+v", r.Delivery)
	}
	f.run(48 * time.Hour)
	f.want(v.ID, StateReported)
	l := f.listed()
	if len(l) != 1 || l[v.ID].Problem != WhyWaitingOnYou || l[v.ID].WaitsOn != WaitsOnOperator {
		t.Fatalf("listed once, waiting on you: %+v", l)
	}
	// The holder ending does not move a report.
	f.end("a")
	f.want(v.ID, StateReported)
}

// 6. No call by any caller reaches done.
func TestReqStateOnlyTheCheckSetsDone(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "a")
	f.accept(v.ID, "a")
	f.report(v.ID, "a") // the report text is "done"
	f.ver.waive = Verdict{Status: "done_accepted", Closes: true}
	for _, c := range []Caller{opCaller, sessCaller("a"), sessCaller("z"), {Kind: CallerSessionUnknown},
		{Kind: CallerPeer, Peer: "p"}, {Kind: CallerDatawatch}, {}} {
		_, _ = f.s.Take(bg, c, v.ID, TakeInput{})
		_, _ = f.s.Seen(bg, c, v.ID, SeenInput{Digest: f.get(v.ID).Delivery.Digest})
		_, _ = f.s.Accept(bg, c, v.ID, AcceptInput{})
		_, _ = f.s.Report(bg, c, v.ID, ReportInput{Text: "done"})
		_, _ = f.s.AcceptUnproven(bg, c, v.ID, ReasonInput{ReasonCode: "ok"})
		_, _ = f.s.Reissue(bg, c, v.ID, ReasonInput{ReasonCode: "ok", RepeatOK: true})
		if r := f.get(v.ID); r.State != StateReported {
			t.Fatalf("%s moved it to %s", c.Observed(), r.State)
		}
	}
	// A verdict that does not close leaves it; then the check closes it.
	f.tick(time.Minute)
	f.want(v.ID, StateReported)
	f.ver.set(v.ID, Verdict{Status: "met_not_reviewed"})
	f.tick(time.Minute)
	f.want(v.ID, StateReported)
	f.ver.set(v.ID, Verdict{Status: "done", Closes: true})
	f.tick(time.Minute)
	if r := f.want(v.ID, StateDone); r.ProofAs != ProofDone || r.Verdict == nil || !r.Verdict.Closes || r.ClosedBy != "datawatch" {
		t.Fatalf("%+v", r)
	}
}

func TestReqStateAcceptUnprovenReadsAsSuch(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "a")
	f.accept(v.ID, "a")
	if _, err := f.s.AcceptUnproven(bg, opCaller, v.ID, ReasonInput{ReasonCode: "seen_it"}); !errors.Is(err, ErrRequestState) {
		t.Fatalf("nothing reported yet: %v", err)
	}
	f.report(v.ID, "a")
	if _, err := f.s.AcceptUnproven(bg, opCaller, v.ID, ReasonInput{}); !errors.Is(err, ErrReasonNeeded) {
		t.Fatalf("no reason: %v", err)
	}
	got, err := f.s.AcceptUnproven(bg, opCaller, v.ID, ReasonInput{ReasonCode: "seen_it", Words: "I looked"})
	if err != nil || got.State != StateReported {
		t.Fatalf("%v %v", got.State, err)
	}
	f.ver.set(v.ID, Verdict{Status: "done_accepted", Closes: true})
	f.tick(time.Minute)
	if r := f.want(v.ID, StateDone); r.ProofAs != ProofAcceptedUnproven {
		t.Fatalf("%+v", r)
	}
	if got, _ := f.s.Get(bg, opCaller, v.ID); got.Label != LabelAcceptedUnproven {
		t.Fatalf("label %q", got.Label)
	}
}

func TestReqStateDeliveryOnlyClosesFromDeliveredByTheCheck(t *testing.T) {
	f := newReqFx(t, nil)
	f.keeper.prove = false
	f.supervise("p", "pi")
	hello := f.s.Requests(RequestFilter{Kind: ReqHello})
	if len(hello) != 1 {
		t.Fatalf("a hello is made: %+v", hello)
	}
	id := hello[0].ID
	f.ver.set(id, Verdict{Status: "delivered", Closes: true})
	rep := f.take(id, "p")
	f.tick(time.Minute)
	f.want(id, StateWaiting) // a fetch alone never counts for pi, whatever the list reads
	if _, err := f.s.Seen(bg, sessCaller("p"), id, SeenInput{Digest: rep.Digest}); err != nil {
		t.Fatal(err)
	}
	f.want(id, StateDelivered)
	f.tick(time.Minute)
	if r := f.want(id, StateDone); r.ProofAs != ProofDelivered {
		t.Fatalf("%+v", r)
	}
	if got, _ := f.s.Get(bg, opCaller, id); got.Label != LabelDeliveredOnly {
		t.Fatalf("label %q", got.Label)
	}
	// Work kinds never close on receipt.
	f.supervise("c", "claude-code")
}

func TestReqStateWorkNeverClosesOnReceipt(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "a")
	f.ver.set(v.ID, Verdict{Status: "delivered", Closes: true})
	f.tick(time.Minute)
	f.want(v.ID, StateDelivered)
	f.accept(v.ID, "a")
	f.report(v.ID, "a")
	f.tick(time.Minute)
	f.want(v.ID, StateReported)
}

// A failed check goes back a bounded number of times, then fails. Cannot tell
// stays reported.
func TestReqStateFailedCheckReworkAndListsOff(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "a")
	f.accept(v.ID, "a")
	f.report(v.ID, "a")
	f.ver.set(v.ID, Verdict{Status: "open", Reason: WhyListsOff})
	f.run(72 * time.Hour)
	f.want(v.ID, StateReported)
	if c := f.listed()[v.ID]; c.Problem != WhyListsOff {
		t.Fatalf("lists off must be listed: %+v", c)
	}
	f.ver.set(v.ID, Verdict{Status: "not_met", NotMet: true})
	f.ver.rework = 1
	f.tick(time.Minute)
	if r := f.want(v.ID, StateAccepted); r.Report != nil || r.ReasonCode != WhyProofFailed {
		t.Fatalf("%+v", r)
	}
	f.report(v.ID, "a")
	f.tick(time.Minute)
	if r := f.want(v.ID, StateFailed); r.ReasonCode != WhyProofFailed {
		t.Fatalf("%+v", r)
	}
}

// 7. Refused calls are recorded under the real caller and change nothing.
func TestReqStateRefusedCallsRecordTheRealCaller(t *testing.T) {
	f := newReqFx(t, testKey(3))
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	liar := Caller{Kind: CallerOperator, Surface: SurfaceCLI, ClaimedName: "a", ClaimedSession: "a"}
	before := f.get(v.ID)
	for i := 0; i < 60; i++ {
		if _, err := f.s.Take(bg, liar, v.ID, TakeInput{}); !errors.Is(err, ErrNotHolder) {
			t.Fatalf("%v", err)
		}
	}
	other := sessCaller("z")
	calls := []func() error{
		func() error { _, e := f.s.Seen(bg, other, v.ID, SeenInput{}); return e },
		func() error { _, e := f.s.Accept(bg, other, v.ID, AcceptInput{}); return e },
		func() error { _, e := f.s.Report(bg, other, v.ID, ReportInput{}); return e },
		func() error { _, e := f.s.Decline(bg, other, v.ID, DeclineInput{ReasonCode: "no"}); return e },
	}
	for _, call := range calls {
		if err := call(); !errors.Is(err, ErrNotHolder) {
			t.Fatalf("%v", err)
		}
	}
	r := f.get(v.ID)
	if r.State != before.State || r.Delivery.FetchedAt != nil || len(r.Calls) != MaxCallRecords {
		t.Fatalf("state %s, %d calls", r.State, len(r.Calls))
	}
	if r.Calls[0] != before.Calls[0] {
		t.Fatalf("the first call is kept: %+v", r.Calls[0])
	}
	last := r.Calls[len(r.Calls)-1]
	if last.Observed != "session:z" || last.Verb != VerbDecline || last.Refused == "" {
		t.Fatalf("%+v", last)
	}
	if c := r.Calls[len(r.Calls)-5]; c.Observed != "admin-token" {
		t.Fatalf("the real caller, not the claimed one: %+v", c)
	}
	content, err := f.s.Content(v.ID)
	if err != nil || len(content.ClaimedNames) != 1 {
		t.Fatalf("claimed names are kept in the content: %v %v", content.ClaimedNames, err)
	}
	if _, err := f.s.Take(bg, sessCaller("a"), "r-nothing", TakeInput{}); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestReqStateNonOperatorActionsAreCounted(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	// A drop by a session cancels and stays listed.
	got, err := f.s.Drop(bg, sessCaller("a"), v.ID, ReasonInput{ReasonCode: "mine"})
	if err != nil || got.State != StateCancelled || got.SettledAt != nil || got.NonOperatorActions != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, ok := f.listed()[v.ID]; !ok {
		t.Fatal("must stay listed")
	}
	got, err = f.s.Drop(bg, sessCaller("a"), v.ID, ReasonInput{ReasonCode: "mine"})
	if err != nil || got.SettledAt != nil || got.NonOperatorActions != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = f.s.Drop(bg, opCaller, v.ID, ReasonInput{ReasonCode: "agreed"})
	if err != nil || got.SettledAt == nil || got.NonOperatorActions != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, ok := f.listed()[v.ID]; ok {
		t.Fatal("the operator's drop settles it")
	}
}

// A failed or cancelled request leaves the list only by reissue or drop, and a
// reissue after possible work must say it may run twice.
func TestReqStateReissue(t *testing.T) {
	f := newReqFx(t, testKey(4))
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	if _, err := f.s.Reissue(bg, opCaller, v.ID, ReasonInput{ReasonCode: "again"}); !errors.Is(err, ErrRequestState) {
		t.Fatalf("an open request is not reissued: %v", err)
	}
	f.take(v.ID, "a")
	f.accept(v.ID, "a")
	f.end("a")
	f.run(100 * 24 * time.Hour)
	if _, ok := f.listed()[v.ID]; !ok {
		t.Fatal("a failed request stays listed")
	}
	if _, err := f.s.Reissue(bg, opCaller, v.ID, ReasonInput{ReasonCode: "again"}); !errors.Is(err, ErrMayRunTwice) {
		t.Fatalf("%v", err)
	}
	n, err := f.s.Reissue(bg, opCaller, v.ID, ReasonInput{ReasonCode: "again", RepeatOK: true})
	if err != nil || n.ID == v.ID || n.ReissueOf != v.ID || !n.MayRunTwice || n.State != StateWaiting {
		t.Fatalf("%+v %v", n, err)
	}
	old := f.get(v.ID)
	if old.ReissuedAs != n.ID || old.SettledAt == nil {
		t.Fatalf("%+v", old)
	}
	same, err := f.s.Reissue(bg, opCaller, v.ID, ReasonInput{ReasonCode: "again", RepeatOK: true})
	if err != nil || same.ID != n.ID {
		t.Fatalf("reissued twice: %v %v", same.ID, err)
	}
	a, _ := f.s.Content(v.ID)
	b, _ := f.s.Content(n.ID)
	if b.Ask != a.Ask || b.ReceiptCode == a.ReceiptCode {
		t.Fatalf("content %+v", b)
	}
	if _, ok := f.listed()[v.ID]; ok {
		t.Fatal("the reissued one leaves the list")
	}
}

func TestReqStateForceSaysItMayRunTwice(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	f.sessions.add("b", "claude-code")
	v := f.create(ReqAsk)
	f.take(v.ID, "a")
	f.accept(v.ID, "a")
	if _, err := f.s.Force(bg, opCaller, v.ID, ForceInput{}); !errors.Is(err, ErrReasonNeeded) {
		t.Fatalf("%v", err)
	}
	got, err := f.s.Force(bg, opCaller, v.ID, ForceInput{ReasonCode: "stuck", ToSession: "b"})
	if err != nil || got.State != StateWaiting || !got.MayRunTwice || got.Holder == nil || got.Holder.SessionID != "b" || got.Delivery.FetchedAt != nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := f.s.Report(bg, sessCaller("a"), v.ID, ReportInput{}); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("the old holder: %v", err)
	}
	f.take(v.ID, "b")
	f.accept(v.ID, "b")
	f.report(v.ID, "b")
	if _, err := f.s.Force(bg, opCaller, v.ID, ForceInput{ReasonCode: "stuck"}); !errors.Is(err, ErrRequestState) {
		t.Fatalf("a report is never sent again: %v", err)
	}
}

// A role released or moved from a live holder moves only what may go again.
func TestReqStateReassignedRoleMovesOnlyWhatMayGoAgain(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	fresh := f.create(ReqAsk)
	worked := f.create(ReqAsk)
	f.take(worked.ID, "a")
	f.accept(worked.ID, "a")
	f.supervise("b", "claude-code")
	if r := f.want(fresh.ID, StateWaiting); r.Holder == nil || r.Holder.SessionID != "b" {
		t.Fatalf("%+v", r.Holder)
	}
	if r := f.want(worked.ID, StateAccepted); r.Holder.SessionID != "a" {
		t.Fatalf("accepted work left a live holder: %+v", r.Holder)
	}
	if err := f.s.ReleaseRole(bg, opCaller, RoleInput{Role: RoleSupervisor, Scope: projScope, SessionID: "b"}); err != nil {
		t.Fatal(err)
	}
	if r := f.want(fresh.ID, StateWaiting); r.Holder != nil || r.ReasonCode != WhyNoHolder {
		t.Fatalf("%+v", r)
	}
}

func contains(s, part string) bool { return len(part) > 0 && indexOf(s, part) >= 0 }

func indexOf(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

// Job 6's rules version may read the store: no lock of the store is held while
// it is asked.
func TestReqStateRulesVersionMayReadTheStore(t *testing.T) {
	f := newReqFx(t, nil)
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.s.opts.RulesVersion = func(Scope) string {
		f.s.Requests(RequestFilter{})
		return "rules-7"
	}
	done := make(chan TakeReply, 1)
	go func() {
		rep, _ := f.s.Take(bg, sessCaller("a"), v.ID, TakeInput{})
		done <- rep
	}()
	select {
	case rep := <-done:
		if rep.RulesVersion != "rules-7" || rep.Request.RulesVersionGiven != "rules-7" {
			t.Fatalf("%+v", rep)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the take never returned")
	}
}
