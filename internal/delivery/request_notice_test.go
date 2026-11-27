package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func cxSettings() RequestSettings {
	return RequestSettings{ReminderInterval: 15 * time.Minute, RemindersPerSessionHour: 2, RemindersPerHostHour: 20}
}

type cxNoteFx struct {
	b     *cxBook
	s     cxSessions
	holds cxHolds
	ty    *cxTyper
	set   RequestSettings
	clock *cxClock
	v     *cxStartVerifier
}

// cxStartVerifier records only what the reminders tell the list.
type cxStartVerifier struct {
	Verifier
	started []string
}

func (v *cxStartVerifier) Started(_ context.Context, id string, _ RequestKind) error {
	v.started = append(v.started, id)
	return nil
}

func cxNotes() *cxNoteFx {
	f := &cxNoteFx{b: cxNewBook(), s: cxSessions{}, holds: cxHolds{}, set: cxSettings(),
		clock: &cxClock{t: cxStart}, v: &cxStartVerifier{}}
	f.ty = &cxTyper{trace: f.b.trace}
	return f
}

// notifier is a new one each time it is asked for: a restart.
func (f *cxNoteFx) notifier() Notifier {
	return NewNotifier(NotifierOptions{Typer: f.ty, Holds: f.holds, Sessions: f.s, Verifier: f.v,
		Settings: func() RequestSettings { return f.set }})
}

func (f *cxNoteFx) run(t *testing.T, n Notifier) {
	t.Helper()
	cxNoErr(t, n.Notify(context.Background(), f.b, f.clock.now()))
}

func (f *cxNoteFx) held(id, session, family string) *Request {
	f.s.add(session, family)
	f.b.put(cxHeld(id, session, family), RequestContent{Ask: "SECRET-CX-1 do the thing", Detail: "SECRET-CX-2"})
	return f.b.reqs[id]
}

func TestReqNoticeOneContentFreeLineForAHundred(t *testing.T) {
	f := cxNotes()
	for i := 0; i < 100; i++ {
		f.held(fmt.Sprintf("r-%03d", i), "s1", "claude-code")
	}
	f.run(t, f.notifier())
	if len(f.ty.notices) != 1 || len(f.ty.requests) != 0 {
		t.Fatalf("typed %d lines and %d contents", len(f.ty.notices), len(f.ty.requests))
	}
	line := f.ty.notices[0].text
	if !strings.Contains(line, "100") || strings.Contains(line, "SECRET") || strings.ContainsAny(line, "\n\r") {
		t.Fatalf("the line: %q", line)
	}
	if len(f.b.notices) != 1 || len(f.b.notices[0].RequestIDs) != 100 {
		t.Fatalf("saved: %+v", f.b.notices)
	}
	if tr := *f.b.trace; tr[0] != "notice s1" || tr[len(tr)-1] != "type s1" {
		t.Fatalf("it must save before it types: %v ... %v", tr[0], tr[len(tr)-1])
	}
	if f.b.reqs["r-007"].Delivery.NoticeAt == nil || f.b.reqs["r-007"].State != StateWaiting {
		t.Fatalf("a typed line proves nothing: %+v", f.b.reqs["r-007"])
	}
	if len(f.v.started) != 100 {
		t.Fatalf("the lists were told of %d starts", len(f.v.started))
	}
	f.clock.add(time.Minute)
	f.run(t, f.notifier())
	if len(f.ty.notices) != 1 || len(f.v.started) != 100 {
		t.Fatalf("again within the interval: %d", len(f.ty.notices))
	}
}

func TestReqNoticePerSessionHourlyLimitSurvivesARestart(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "claude-code")
	for i := 0; i < 3; i++ { // 0, 16, 32 minutes
		f.run(t, f.notifier())
		f.clock.add(16 * time.Minute)
	}
	if len(f.ty.notices) != 2 {
		t.Fatalf("2 per session each hour, got %d", len(f.ty.notices))
	}
	f.clock.add(16 * time.Minute) // 64 minutes: the first is over an hour old
	f.run(t, f.notifier())
	if len(f.ty.notices) != 3 {
		t.Fatalf("after the hour: %d", len(f.ty.notices))
	}
}

func TestReqNoticePerComputerLimitAndLiveSettings(t *testing.T) {
	f := cxNotes()
	for i := 0; i < 30; i++ {
		f.held(fmt.Sprintf("r-%02d", i), fmt.Sprintf("s%02d", i), "claude-code")
	}
	n := f.notifier()
	f.run(t, n)
	if len(f.ty.notices) != 20 {
		t.Fatalf("20 per computer each hour, got %d", len(f.ty.notices))
	}
	f.set.RemindersPerHostHour = 25
	f.run(t, n)
	if len(f.ty.notices) != 25 {
		t.Fatalf("a live setting: %d", len(f.ty.notices))
	}
}

func TestReqNoticeZeroMeansNone(t *testing.T) {
	for name, edit := range map[string]func(*RequestSettings){
		"per session":  func(s *RequestSettings) { s.RemindersPerSessionHour = 0 },
		"per computer": func(s *RequestSettings) { s.RemindersPerHostHour = 0 },
	} {
		f := cxNotes()
		edit(&f.set)
		f.held("r-1", "s1", "claude-code")
		f.run(t, f.notifier())
		if len(f.ty.notices) != 0 || len(f.b.notices) != 0 {
			t.Errorf("%s 0 typed %d", name, len(f.ty.notices))
		}
	}
}

func TestReqNoticeFetchedNotAcceptedStopsAtTwo(t *testing.T) {
	f := cxNotes()
	r := f.held("r-1", "s1", "claude-code")
	at := cxStart
	r.State, r.Delivery.FetchedAt = StateDelivered, &at
	n := f.notifier()
	for i := 0; i < 8; i++ {
		f.run(t, n)
		f.clock.add(2 * time.Hour)
	}
	if len(f.ty.notices) != MaxFetchedReminders || f.b.reqs["r-1"].Delivery.Reminders != MaxFetchedReminders {
		t.Fatalf("typed %d, counted %d", len(f.ty.notices), f.b.reqs["r-1"].Delivery.Reminders)
	}
}

func TestReqNoticeNoneForWhatNeedsNone(t *testing.T) {
	for name, edit := range map[string]func(*Request){
		"accepted":  func(r *Request) { r.State = StateAccepted },
		"reported":  func(r *Request) { r.State = StateReported },
		"failed":    func(r *Request) { r.State = StateFailed },
		"no holder": func(r *Request) { r.Holder = nil },
	} {
		f := cxNotes()
		edit(f.held("r-1", "s1", "claude-code"))
		f.run(t, f.notifier())
		if len(f.ty.notices)+len(f.ty.requests) != 0 {
			t.Errorf("%s was reminded", name)
		}
	}
	f := cxNotes()
	f.held("r-1", "s1", "claude-code")
	f.s["s1"] = SessionInfo{ID: "s1", Family: "claude-code"}
	f.run(t, f.notifier())
	if len(f.ty.notices) != 0 {
		t.Error("an ended session was typed into")
	}
}

func TestReqNoticeNoneWhileHeld(t *testing.T) {
	for _, cause := range []string{WhyHeldPause, WhyHeldUsage, WhyHeldRateLimit} {
		f := cxNotes()
		f.held("r-1", "s1", "claude-code")
		f.held("r-2", "s2", "opencode")
		f.holds[""] = cause
		f.run(t, f.notifier())
		if len(f.ty.notices)+len(f.ty.requests) != 0 || len(f.b.notices) != 0 || f.b.reqs["r-2"].Delivery.TypeClaimAt != nil {
			t.Errorf("%s: something was typed or counted", cause)
		}
	}
}

func TestReqNoticeAPromptRefusalDoesNotCount(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "claude-code")
	at := cxStart
	r := f.held("r-2", "s1", "claude-code")
	r.State, r.Delivery.FetchedAt = StateDelivered, &at
	n := f.notifier()
	f.ty.err = ErrPromptRefused
	for i := 0; i < 100; i++ {
		f.run(t, n)
		f.clock.add(time.Second)
	}
	if got := f.b.reqs["r-2"].Delivery; got.Reminders != 0 || got.NoticeAt != nil {
		t.Fatalf("100 refusals changed %+v", got)
	}
	f.ty.err = nil
	f.run(t, n)
	f.clock.add(16 * time.Minute)
	f.run(t, n)
	if len(f.ty.notices) != 2 {
		t.Fatalf("both allowed reminders are still there, got %d", len(f.ty.notices))
	}
}

func TestReqNoticeAnUnknownTypingErrorCounts(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "claude-code")
	n := f.notifier()
	f.ty.err = errors.New("send input: tmux failed")
	if err := n.Notify(context.Background(), f.b, f.clock.now()); err == nil {
		t.Fatal("the failure must be returned")
	}
	f.ty.err = nil
	f.run(t, n)
	if len(f.ty.notices) != 0 {
		t.Fatal("a try that may have typed counts toward the interval")
	}
}

func TestReqNoticeAFailedSaveTypesNothing(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "claude-code")
	f.held("r-2", "s2", "opencode")
	f.b.failSave = true
	if err := f.notifier().Notify(context.Background(), f.b, f.clock.now()); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("%v", err)
	}
	if len(f.ty.notices)+len(f.ty.requests) != 0 {
		t.Fatal("typed without a saved record")
	}
}

func TestReqNoticePiAndShellAreNeverTypedInto(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "pi")
	f.held("r-2", "s2", "pi-prompt")
	f.held("r-3", "s3", "shell")
	f.held("r-4", "s4", "")
	f.run(t, f.notifier())
	if len(f.ty.notices)+len(f.ty.requests) != 0 {
		t.Fatalf("typed %+v %+v", f.ty.notices, f.ty.requests)
	}
}

func TestReqCapReminderAllowed(t *testing.T) {
	now := cxStart
	at := func(d time.Duration, s string) RequestNotice { return RequestNotice{At: now.Add(-d), SessionID: s} }
	many := func(n int) (out []RequestNotice) {
		for i := 0; i < n; i++ {
			out = append(out, at(30*time.Minute, fmt.Sprintf("x%d", i)))
		}
		return out
	}
	for name, tc := range map[string]struct {
		set  RequestSettings
		sent []RequestNotice
		ok   bool
	}{
		"nothing sent":         {cxSettings(), nil, true},
		"within the interval":  {cxSettings(), []RequestNotice{at(14*time.Minute, "s1")}, false},
		"after the interval":   {cxSettings(), []RequestNotice{at(15*time.Minute, "s1")}, true},
		"session limit":        {cxSettings(), []RequestNotice{at(50*time.Minute, "s1"), at(20*time.Minute, "s1")}, false},
		"an old one is gone":   {cxSettings(), []RequestNotice{at(time.Hour, "s1"), at(20*time.Minute, "s1")}, true},
		"computer limit":       {cxSettings(), many(20), false},
		"under computer limit": {cxSettings(), many(19), true},
		"no session limit set": {RequestSettings{RemindersPerHostHour: 20}, nil, false},
		"no computer limit":    {RequestSettings{RemindersPerSessionHour: 2}, nil, false},
		"negative":             {RequestSettings{RemindersPerSessionHour: -1, RemindersPerHostHour: -1}, nil, false},
		// A clock set back must not open the limits.
		"saved in the future": {cxSettings(), []RequestNotice{at(-time.Minute, "s1")}, false},
	} {
		ok, why := ReminderAllowed(tc.set, tc.sent, "s1", now)
		if ok != tc.ok || (!ok && CheckReasonCode(why) != nil) {
			t.Errorf("%s: %v %q", name, ok, why)
		}
	}
}
