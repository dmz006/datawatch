package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReqTypedAnUnprovenWorkerIsTypedOnceEver(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "fake-worker")
	f.run(t, f.notifier())
	if len(f.ty.requests) != 1 || f.ty.requests[0].id != "r-1" {
		t.Fatalf("typed: %+v", f.ty.requests)
	}
	text := f.ty.requests[0].text
	if !strings.Contains(text, "r-1") || !strings.Contains(text, "SECRET-CX-1") || !strings.Contains(text, "SECRET-CX-2") {
		t.Fatalf("the text: %q", text)
	}
	got := f.b.reqs["r-1"]
	if got.Delivery.TypeClaimAt == nil || got.Delivery.ContentTypedAt == nil || got.State != StateWaiting {
		t.Fatalf("%+v", got)
	}
	p := PolicyFor("fake-worker")
	if p.Delivered(got.Delivery) || p.Level(got.Delivery) != LevelTypedUnconfirmed {
		t.Fatal("it must read typed, unconfirmed")
	}
	if tr := *f.b.trace; tr[0] != "save r-1" || tr[1] != "type s1" {
		t.Fatalf("the claim is saved before typing: %v", tr)
	}
	for i := 0; i < 3; i++ { // restarts, days later
		f.clock.add(25 * time.Hour)
		f.run(t, f.notifier())
	}
	if len(f.ty.requests) != 1 || len(f.ty.notices) != 0 {
		t.Fatalf("typed again: %d, reminded %d", len(f.ty.requests), len(f.ty.notices))
	}
}

func TestReqTypedOneSessionGetsOneSendPerInterval(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "fake-worker")
	f.held("r-2", "s1", "fake-worker")
	n := f.notifier()
	f.run(t, n)
	f.run(t, n)
	if len(f.ty.requests) != 1 {
		t.Fatalf("%d", len(f.ty.requests))
	}
	f.clock.add(16 * time.Minute)
	f.run(t, f.notifier())
	if len(f.ty.requests) != 2 || f.ty.requests[1].id != "r-2" {
		t.Fatalf("%+v", f.ty.requests)
	}
}

func TestReqTypedAHelloIsNotTyped(t *testing.T) {
	f := cxNotes()
	r := f.held("r-1", "s1", "fake-worker")
	r.Kind, r.Role, r.Scope = ReqHello, RoleSession, Scope{Kind: ScopeSession, ID: "s1"}
	f.run(t, f.notifier())
	if len(f.ty.requests) != 0 {
		t.Fatal("typing a hello proves nothing")
	}
}

func TestReqTypedRefusalsAndFailures(t *testing.T) {
	setup := func() *cxNoteFx {
		f := cxNotes()
		f.held("r-1", "s1", "fake-worker")
		return f
	}
	ctx := context.Background()
	f := setup()
	f.ty.err = ErrPromptRefused
	if err := TypeOnce(ctx, f.b, f.ty, "r-1"); !errors.Is(err, ErrPromptRefused) {
		t.Fatalf("%v", err)
	}
	if d := f.b.reqs["r-1"].Delivery; d.TypeClaimAt != nil || d.ContentTypedAt != nil {
		t.Fatalf("nothing was typed, so the try does not count: %+v", d)
	}
	f = setup()
	f.ty.err = errors.New("send input: tmux failed")
	if err := TypeOnce(ctx, f.b, f.ty, "r-1"); err == nil {
		t.Fatal("the failure must be returned")
	}
	if d := f.b.reqs["r-1"].Delivery; d.TypeClaimAt == nil || d.ContentTypedAt != nil {
		t.Fatalf("unknown outcome keeps the claim: %+v", d)
	}
	f.ty.err = nil
	if err := TypeOnce(ctx, f.b, f.ty, "r-1"); !errors.Is(err, ErrRequestState) || len(f.ty.requests) != 0 {
		t.Fatalf("typed twice: %v", err)
	}
	f = setup()
	f.b.failSave = true
	if err := TypeOnce(ctx, f.b, f.ty, "r-1"); !errors.Is(err, ErrCannotRecord) || len(*f.b.trace) != 0 {
		t.Fatalf("a failed save types nothing: %v %v", err, *f.b.trace)
	}
}

func TestReqTypedOnlyWhereTypingIsTheWay(t *testing.T) {
	at := cxStart
	for name, edit := range map[string]func(*Request){
		"claude takes":      func(r *Request) { r.Holder.Family = "claude-code" },
		"pi polls":          func(r *Request) { r.Holder.Family = "pi" },
		"pi one-shot":       func(r *Request) { r.Holder.Family = "pi-prompt" },
		"a shell runs text": func(r *Request) { r.Holder.Family = "shell" },
		"unknown family":    func(r *Request) { r.Holder.Family = "" },
		"no holder":         func(r *Request) { r.Holder = nil },
		"not waiting":       func(r *Request) { r.State = StateFailed },
		"fetched":           func(r *Request) { r.Delivery.FetchedAt = &at },
		"already claimed":   func(r *Request) { r.Delivery.TypeClaimAt = &at },
	} {
		f := cxNotes()
		edit(f.held("r-1", "s1", "fake-worker"))
		if err := TypeOnce(context.Background(), f.b, f.ty, "r-1"); !errors.Is(err, ErrRequestState) || len(f.ty.requests) != 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
	f := cxNotes()
	if err := TypeOnce(context.Background(), f.b, f.ty, "r-none"); !errors.Is(err, ErrRequestNotFound) {
		t.Errorf("%v", err)
	}
}

func TestReqTypedTextIsOneLineWithoutControlBytes(t *testing.T) {
	f := cxNotes()
	f.held("r-1", "s1", "fake-worker")
	f.b.content["r-1"] = RequestContent{Ask: "first\nsecond\r\n\x1b[2J\x03third\tfourth"}
	cxNoErr(t, TypeOnce(context.Background(), f.b, f.ty, "r-1"))
	text := f.ty.requests[0].text
	for _, r := range text {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			t.Fatalf("a control byte %U in %q", r, text)
		}
	}
	if !strings.HasPrefix(text, "datawatch request r-1") || !strings.Contains(text, "first second") {
		t.Fatalf("%q", text)
	}
}
