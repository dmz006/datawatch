package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// A killed session frees the lease bound to it at once; other sessions' leases
// stay held. Both the full and the short session id name the binding.
func TestSessionEndReleasesBoundLeaseAtKillTime(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 3)
	led.Adopt(reapReq("task-a"), "host-aa01")
	led.Adopt(reapReq("task-b"), "host-bb02")
	led.Adopt(reapReq("task-c"), "cc03")
	got := releaseLeasesOfEndedSession(led, nil, &session.Session{ID: "aa01", FullID: "host-aa01"})
	if len(got) != 1 || got[0] != "task-a" || led.Held("task-a") {
		t.Fatalf("released %v, held(task-a)=%v", got, led.Held("task-a"))
	}
	if !led.Held("task-b") {
		t.Fatal("another session's lease was freed")
	}
	if got := releaseLeasesOfEndedSession(led, nil, &session.Session{ID: "cc03", FullID: "host-cc03"}); len(got) != 1 || led.Held("task-c") {
		t.Fatalf("lease bound by short id not released: %v", got)
	}
}

// A verifier lease follows its verifier session, which only the registry knows.
func TestSessionEndReleasesVerifierLeaseBoundThroughRegistry(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 2)
	reg := newVerifierRegistry()
	holder := autonomous.VerifyHolder("t9")
	led.Adopt(reapReq(holder), "")
	reg.begin(holder)
	reg.bindSession(holder, "host-vv01")
	got := releaseLeasesOfEndedSession(led, reg, &session.Session{ID: "vv01", FullID: "host-vv01"})
	if len(got) != 1 || led.Held(holder) {
		t.Fatalf("verifier lease not released: %v", got)
	}
}

func TestSessionEndReleaseWithNoLeaseOrLedgerIsSafe(t *testing.T) {
	led := capacity.New(capacity.Options{})
	if got := releaseLeasesOfEndedSession(led, nil, &session.Session{FullID: "host-zz"}); len(got) != 0 {
		t.Fatalf("released %v", got)
	}
	if got := releaseLeasesOfEndedSession(nil, nil, &session.Session{FullID: "host-zz"}); got != nil {
		t.Fatalf("nil ledger: %v", got)
	}
	if got := releaseLeasesOfEndedSession(led, nil, nil); got != nil {
		t.Fatalf("nil session: %v", got)
	}
	if got := releaseLeasesOfEndedSession(led, nil, &session.Session{}); got != nil {
		t.Fatalf("empty ids must release nothing: %v", got)
	}
}

// A lease handed to a verifier that has not started its session yet still names
// the worker session; ending the worker must not free the verifier's slot.
func TestSessionEndKeepsLeaseHandedToActiveVerifier(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("gpu", 1)
	reg := newVerifierRegistry()
	led.Adopt(reapReq("t2"), "host-ww01")
	if !led.Handoff("t2", autonomous.VerifyHolder("t2")) {
		t.Fatal("handoff")
	}
	reg.begin(autonomous.VerifyHolder("t2"))
	if got := releaseLeasesOfEndedSession(led, reg, &session.Session{ID: "ww01", FullID: "host-ww01"}); len(got) != 0 {
		t.Fatalf("freed the verifier's lease: %v", got)
	}
}
