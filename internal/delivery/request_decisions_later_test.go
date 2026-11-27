// The operator's decision of 2026-11-27 (D3, "A"): after the pickup limit a
// request's text is not typed any more, and a holder given a role later is told
// once about the older requests that wait for it.

package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A request whose text was never typed is not typed once its pickup limit
// passed.
func TestReqTypedNotAfterTheTakeByLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		edit  func(*Request)
		typed bool
	}{
		"before the limit": {func(r *Request) { r.TakeBySeconds, r.AgeSeconds = 3600, 3599 }, true},
		"at the limit":     {func(r *Request) { r.TakeBySeconds, r.AgeSeconds = 3600, 3600 }, false},
		"held time does not count": {func(r *Request) {
			r.TakeBySeconds, r.AgeSeconds, r.HeldSeconds = 3600, 4000, 1000
		}, true},
		"already listed as passed": {func(r *Request) { r.TakeBySeconds, r.ReasonCode = 3600, WhyTakeByPassed }, false},
	} {
		f := cxNotes()
		tc.edit(f.held("r-1", "s1", "fake-worker"))
		f.run(t, f.notifier())
		claimed := f.b.reqs["r-1"].Delivery.TypeClaimAt != nil
		if got := len(f.ty.requests) == 1; got != tc.typed || claimed != tc.typed {
			t.Errorf("%s: typed=%v claimed=%v, want %v", name, got, claimed, tc.typed)
		}
		if tc.typed {
			continue
		}
		if err := TypeOnce(context.Background(), f.b, f.ty, "r-1"); !errors.Is(err, ErrRequestState) || len(f.ty.requests) != 0 {
			t.Errorf("%s: TypeOnce: %v, typed %d", name, err, len(f.ty.requests))
		}
	}
}

// laterFx is the real store with the real reminder sender over it.
type laterFx struct {
	*reqFx
	ty *cxTyper
}

func newLaterFx(t *testing.T) *laterFx {
	t.Helper()
	f := &laterFx{reqFx: newReqFx(t, nil), ty: &cxTyper{}}
	f.restart()
	return f
}

// restart is a daemon restart: the store is read again from disk and the
// reminder sender starts with nothing in memory.
func (f *laterFx) restart() {
	f.t.Helper()
	o := f.options()
	o.Notifier = NewNotifier(NotifierOptions{Typer: f.ty, Sessions: f.sessions, Holds: f.holds, Settings: o.Settings})
	s, err := OpenRequests(o)
	if err != nil {
		f.t.Fatal(err)
	}
	f.s = s
}

func (f *laterFx) linesTo(session string) (out []string) {
	for _, n := range f.ty.notices {
		if n.session == session {
			out = append(out, n.text)
		}
	}
	return out
}

// A role given after the pickup limit: the new holder first takes its hello,
// then gets one line for all the older requests, and no second one, also after
// restarts and for an older request that reaches it later.
func TestReqNoticeALaterHolderIsToldOnceAboutOlderRequests(t *testing.T) {
	f := newLaterFx(t)
	a := f.create(ReqAsk)
	f.create(ReqAsk)
	f.create(ReqAsk)
	child := f.create(ReqAsk, func(in *CreateInput) { in.After = a.ID })
	f.run(2 * time.Hour)
	if len(f.ty.notices) != 0 {
		t.Fatalf("nobody holds the role: %+v", f.ty.notices)
	}

	f.keeper.prove = false // the new holder proves itself with its hello
	f.supervise("b", "claude-code")
	f.tick(time.Minute)
	if got := f.linesTo("b"); len(got) != 1 || !strings.Contains(got[0], " 1 request(s)") {
		t.Fatalf("the hello's line: %q", got)
	}
	hello := f.s.Bindings()[0].HelloID
	f.take(hello, "b")
	f.ver.set(hello, Verdict{Status: "delivered", Closes: true}) // as the real list reads it
	f.run(20 * time.Minute)
	got := f.linesTo("b")
	if len(got) != 2 || got[1] != reminderLine(3) {
		t.Fatalf("one line for the three older requests: %q", got)
	}
	if len(f.ty.requests) != 0 {
		t.Fatalf("their text was typed: %+v", f.ty.requests)
	}

	for i := 0; i < 3; i++ {
		f.restart()
		f.run(2 * time.Hour)
	}
	if got := f.linesTo("b"); len(got) != 2 {
		t.Fatalf("told again: %q", got)
	}

	// The request that waited for the first one reaches the same holder now.
	f.take(a.ID, "b")
	f.accept(a.ID, "b")
	f.report(a.ID, "b")
	f.ver.set(a.ID, Verdict{Status: "done", Closes: true})
	f.run(2 * time.Hour)
	f.want(a.ID, StateDone)
	if r := f.want(child.ID, StateWaiting); r.Holder == nil || r.Holder.SessionID != "b" {
		t.Fatalf("the child must have reached b: %+v", r.Holder)
	}
	if got := f.linesTo("b"); len(got) != 2 {
		t.Fatalf("one line per holder about older requests: %q", got)
	}

	// Another holder is newly given the role: it is told once, itself.
	f.keeper.prove = true
	f.supervise("c", "claude-code")
	f.run(time.Hour)
	f.restart()
	f.run(3 * time.Hour)
	if got := f.linesTo("c"); len(got) != 1 {
		t.Fatalf("the second new holder: %q", got)
	}
}

// A holder told of an older request while it could still be picked up is not
// told again when the limit has passed.
func TestReqNoticeALaterHolderInsideTheLimitGetsNoExtraLine(t *testing.T) {
	f := newLaterFx(t)
	f.settings.ReminderInterval = 30 * time.Minute // the next one is past the limit
	f.create(ReqAsk)
	f.run(40 * time.Minute)
	f.supervise("b", "claude-code")
	f.run(5 * time.Hour)
	if got := f.linesTo("b"); len(got) != 1 {
		t.Fatalf("%q", got)
	}
}

// The one line that typed nothing, because of a prompt or a pause, is not
// used up.
func TestReqNoticeTheLaterHoldersLineWaitsUntilItCanBeTyped(t *testing.T) {
	f := newLaterFx(t)
	f.create(ReqAsk)
	f.run(2 * time.Hour)
	f.supervise("b", "claude-code")
	f.holds.cause[""] = WhyHeldPause
	f.run(time.Hour)
	delete(f.holds.cause, "")
	f.ty.err = ErrPromptRefused
	f.run(time.Hour)
	if len(f.ty.notices) != 0 {
		t.Fatalf("%+v", f.ty.notices)
	}
	f.ty.err = nil
	f.restart()
	f.run(3 * time.Hour)
	if got := f.linesTo("b"); len(got) != 1 || got[0] != reminderLine(1) {
		t.Fatalf("%q", got)
	}
}

// A request addressed straight to a worker with no proven take, still untyped
// when its pickup limit passed (reminders were off), is never typed later.
func TestReqTypedStoreNeverTypesAfterTheLimit(t *testing.T) {
	f := newLaterFx(t)
	f.sessions.add("o", "opencode")
	f.settings.RemindersPerHostHour = 0
	v := f.create(ReqAsk, func(in *CreateInput) {
		in.Role, in.Scope = RoleSession, Scope{Kind: ScopeSession, ID: "o"}
	})
	f.run(2 * time.Hour)
	f.settings.RemindersPerHostHour = 20
	f.run(2 * time.Hour)
	if len(f.ty.requests) != 0 {
		t.Fatalf("typed after the limit: %+v", f.ty.requests)
	}
	if c, ok := f.listed()[v.ID]; !ok || c.Problem != WhyTakeByPassed {
		t.Fatalf("it must stay listed: %+v", f.listed())
	}
}
