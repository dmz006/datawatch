// The operator's decisions of 2026-11-27 on reminders and typed requests.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Reminders about a request nobody fetched stop when its pickup limit passes.
func TestReqNoticeNeverFetchedStopsAtTheTakeByLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		edit     func(*Request)
		reminded bool
	}{
		"before the limit": {func(r *Request) { r.TakeBySeconds, r.AgeSeconds = 3600, 3599 }, true},
		"at the limit":     {func(r *Request) { r.TakeBySeconds, r.AgeSeconds = 3600, 3600 }, false},
		"held time does not count": {func(r *Request) {
			r.TakeBySeconds, r.AgeSeconds, r.HeldSeconds = 3600, 4000, 1000
		}, true},
		"already listed as passed": {func(r *Request) { r.TakeBySeconds, r.ReasonCode = 3600, WhyTakeByPassed }, false},
		"fetched keeps its two": {func(r *Request) {
			at := cxStart
			r.State, r.Delivery.FetchedAt = StateDelivered, &at
			r.TakeBySeconds, r.AgeSeconds = 3600, 7200
		}, true},
	} {
		f := cxNotes()
		tc.edit(f.held("r-1", "s1", "claude-code"))
		f.run(t, f.notifier())
		if got := len(f.ty.notices) == 1; got != tc.reminded {
			t.Errorf("%s: reminded=%v, want %v", name, got, tc.reminded)
		}
	}
}

// Over the real store: the ignored request is reminded within its pickup limit
// and never after it.
func TestReqNoticeStoreStopsRemindingAfterTakeBy(t *testing.T) {
	f := newReqFx(t, nil)
	ty := &cxTyper{}
	o := f.options()
	o.Notifier = NewNotifier(NotifierOptions{Typer: ty, Sessions: f.sessions, Settings: o.Settings})
	s, err := OpenRequests(o)
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	f.supervise("a", "claude-code")
	v := f.create(ReqAsk)
	f.run(55 * time.Minute)
	within := len(ty.notices)
	if within == 0 {
		t.Fatal("no reminder inside the pickup limit")
	}
	f.run(5 * time.Hour)
	if len(ty.notices) != within {
		t.Fatalf("reminded after the pickup limit: %d, then %d", within, len(ty.notices))
	}
	if c, ok := f.listed()[v.ID]; !ok || c.Problem != WhyTakeByPassed {
		t.Fatalf("it must stay listed: %+v", f.listed())
	}
}

// A typed full request counts toward the session's hourly limit.
func TestReqTypedCountsTowardTheSessionLimit(t *testing.T) {
	f := cxNotes()
	for i := 0; i < 4; i++ {
		f.held(fmt.Sprintf("r-%d", i), "s1", "fake-worker")
	}
	for i := 0; i < 3; i++ { // 0, 16 and 32 minutes: all within one hour
		f.run(t, f.notifier()) // a new one each time: a restart keeps the count
		f.clock.add(16 * time.Minute)
	}
	if len(f.ty.requests) != 2 {
		t.Fatalf("2 per session each hour, typed %d", len(f.ty.requests))
	}
	if f.b.reqs["r-2"].Delivery.TypeClaimAt != nil {
		t.Fatal("a request over the limit must not be claimed")
	}
	f.clock.add(16 * time.Minute) // 64 minutes: the first is over an hour old
	f.run(t, f.notifier())
	if len(f.ty.requests) != 3 {
		t.Fatalf("after the hour: %d", len(f.ty.requests))
	}
}

// Typed requests and reminder lines share the computer's hourly limit.
func TestReqTypedAndRemindersShareTheComputerLimit(t *testing.T) {
	f := cxNotes()
	for i := 0; i < 15; i++ {
		f.held(fmt.Sprintf("r-c%02d", i), fmt.Sprintf("c%02d", i), "claude-code")
		f.held(fmt.Sprintf("r-t%02d", i), fmt.Sprintf("t%02d", i), "fake-worker")
	}
	n := f.notifier()
	f.run(t, n)
	f.run(t, n)
	if got := len(f.ty.notices) + len(f.ty.requests); got != 20 {
		t.Fatalf("20 per computer each hour, typed %d (%d lines, %d requests)", got, len(f.ty.notices), len(f.ty.requests))
	}
	// The count is in the saved records, so a restart keeps it.
	f.run(t, f.notifier())
	if got := len(f.ty.notices) + len(f.ty.requests); got != 20 {
		t.Fatalf("after a restart: %d", got)
	}
}

// A limit of 0 stops typed requests too.
func TestReqTypedALimitOfZeroStopsIt(t *testing.T) {
	for name, edit := range map[string]func(*RequestSettings){
		"per session":  func(s *RequestSettings) { s.RemindersPerSessionHour = 0 },
		"per computer": func(s *RequestSettings) { s.RemindersPerHostHour = 0 },
	} {
		f := cxNotes()
		edit(&f.set)
		f.held("r-1", "s1", "fake-worker")
		f.run(t, f.notifier())
		if len(f.ty.requests) != 0 || f.b.reqs["r-1"].Delivery.TypeClaimAt != nil {
			t.Errorf("%s 0 typed %d", name, len(f.ty.requests))
		}
	}
}

// A try that typed nothing does not count; one that may have typed does.
func TestReqTypedOnlyWhatMayHaveBeenTypedCounts(t *testing.T) {
	f := cxNotes()
	f.set.RemindersPerHostHour = 1
	f.held("r-1", "s1", "fake-worker")
	f.held("r-2", "s2", "fake-worker")
	f.ty.err = ErrPromptRefused
	f.run(t, f.notifier())
	f.ty.err = nil
	f.run(t, f.notifier())
	if len(f.ty.requests) != 1 {
		t.Fatalf("a refused try used the limit: %d", len(f.ty.requests))
	}

	g := cxNotes()
	g.set.RemindersPerHostHour = 1
	g.held("r-1", "s1", "fake-worker")
	g.held("r-2", "s2", "fake-worker")
	g.ty.err = errors.New("send input: tmux failed")
	if err := g.notifier().Notify(context.Background(), g.b, g.clock.now()); err == nil {
		t.Fatal("the failure must be returned")
	}
	g.ty.err = nil
	g.run(t, g.notifier())
	if len(g.ty.requests) != 0 {
		t.Fatalf("a try that may have typed did not count: %d", len(g.ty.requests))
	}
}

// Forcing a typed request off its session does not give the hour its send back.
func TestReqTypedStillCountsAfterAForce(t *testing.T) {
	f := newReqFx(t, nil)
	f.settings.RemindersPerHostHour = 1
	ty := &cxTyper{}
	o := f.options()
	o.Notifier = NewNotifier(NotifierOptions{Typer: ty, Sessions: f.sessions, Settings: o.Settings})
	s, err := OpenRequests(o)
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	f.supervise("a", "fake-worker")
	b, ok := f.keeper.Resolve(f.s, RoleSupervisor, projScope)
	if !ok {
		t.Fatal("no binding")
	}
	at := f.now
	b.CanTakeAt = &at // the fake keeper: let the role get work
	if err := f.s.PutBinding(b); err != nil {
		t.Fatal(err)
	}
	v := f.create(ReqAsk)
	f.tick(time.Minute)
	if len(ty.requests) != 1 {
		t.Fatalf("typed %d", len(ty.requests))
	}
	if _, err := f.s.Force(bg, opCaller, v.ID, ForceInput{ReasonCode: "test"}); err != nil {
		t.Fatal(err)
	}
	f.tick(20 * time.Minute)
	if len(ty.requests) != 1 {
		t.Fatalf("typed again inside the hour: %d", len(ty.requests))
	}
}

// The approved times: pickup 1 hour, finish 24 hours, reminders 15 minutes
// apart, a hello 1 hour. Each stays a setting.
func TestReqKeyApprovedTimes(t *testing.T) {
	f := newReqFx(t, nil)
	f.settings = RequestSettings{} // nothing set
	f.sessions.add("s", "claude-code")
	toS := func(in *CreateInput) { in.Role, in.Scope = RoleSession, Scope{Kind: ScopeSession, ID: "s"} }
	ask := f.get(f.create(ReqAsk).ID)
	if ask.TakeBySeconds != 3600 || ask.DueSeconds != 86400 {
		t.Fatalf("ask: take-by %d, due %d", ask.TakeBySeconds, ask.DueSeconds)
	}
	h, _, err := f.s.Create(bg, Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon},
		func() CreateInput {
			in := CreateInput{Kind: ReqHello, Key: "hello:s:times"}
			toS(&in)
			return in
		}())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.get(h.ID); r.DueSeconds != 3600 {
		t.Fatalf("hello due %d", r.DueSeconds)
	}
	f.settings = RequestSettings{TakeBy: 2 * time.Hour, Due: 48 * time.Hour,
		DueByKind: map[RequestKind]time.Duration{ReqHello: 3 * time.Hour}}
	set := f.get(f.create(ReqAsk).ID)
	h2, _, err := f.s.Create(bg, Caller{Kind: CallerDatawatch, Surface: SurfaceDaemon},
		func() CreateInput {
			in := CreateInput{Kind: ReqHello, Key: "hello:s:times-set"}
			toS(&in)
			return in
		}())
	if err != nil {
		t.Fatal(err)
	}
	if set.TakeBySeconds != 7200 || set.DueSeconds != 172800 || f.get(h2.ID).DueSeconds != 10800 {
		t.Fatalf("the settings must apply: %d %d %d", set.TakeBySeconds, set.DueSeconds, f.get(h2.ID).DueSeconds)
	}
}
