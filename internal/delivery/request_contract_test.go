package delivery

import (
	"testing"
	"time"
)

func TestReqContractOnlyTheHoldersOwnCredentialHolds(t *testing.T) {
	if !(Caller{Kind: CallerSession, SessionID: "h-1"}).Holds("h-1") {
		t.Fatal("the holder's own session credential must hold")
	}
	for name, c := range map[string]Caller{
		"operator token":              {Kind: CallerOperator},
		"operator claiming a session": {Kind: CallerOperator, ClaimedSession: "h-1", ClaimedName: "h-1"},
		"another session":             {Kind: CallerSession, SessionID: "h-2", ClaimedSession: "h-1"},
		"unknown session":             {Kind: CallerSessionUnknown, SessionID: "h-1"},
		"peer":                        {Kind: CallerPeer, Peer: "h-1", SessionID: "h-1"},
		"datawatch":                   {Kind: CallerDatawatch, SessionID: "h-1"},
		"empty":                       {},
	} {
		if c.Holds("h-1") {
			t.Errorf("%s must not hold", name)
		}
	}
	if (Caller{Kind: CallerSession}).Holds("") {
		t.Error("no session must never hold an unheld request")
	}
}

func TestReqContractWhatCountsAsDelivery(t *testing.T) {
	at := time.Unix(1, 0)
	typed := DeliveryRecord{NoticeAt: &at, TypeClaimAt: &at, ContentTypedAt: &at}
	fetched := DeliveryRecord{FetchedAt: &at}
	inCall := DeliveryRecord{FetchedAt: &at, InModelCallAt: &at}
	reportedOnly := DeliveryRecord{InModelCallAt: &at}
	for _, family := range []string{"claude-code", "pi", "opencode", ""} {
		p := PolicyFor(family)
		if p.Delivered(typed) {
			t.Errorf("%q: typing counted as delivery", family)
		}
		if p.Delivered(reportedOnly) {
			t.Errorf("%q: a program report with no fetch counted", family)
		}
		if family != "claude-code" && p.Delivered(fetched) {
			t.Errorf("%q: a fetch alone counted", family)
		}
		if family != "claude-code" && family != "pi" && p.Delivered(inCall) {
			t.Errorf("%q counted a delivery with no recorded proof for the family", family)
		}
		unproven := p
		unproven.Proven = false
		if unproven.Delivered(fetched) || unproven.Delivered(inCall) {
			t.Errorf("%q: an unproven row counted a delivery", family)
		}
	}
	if !PolicyFor("claude-code").Delivered(fetched) {
		t.Error("claude-code: the model's take must count")
	}
	if !typed.MaybeHandedOver() || !fetched.MaybeHandedOver() || (DeliveryRecord{NoticeAt: &at}).MaybeHandedOver() {
		t.Error("typed content and a fetch may have handed over; a content-free line does not")
	}
}

func TestReqContractKindTableIsSafe(t *testing.T) {
	kinds := []RequestKind{ReqHello, ReqAsk, ReqBug, ReqWake, ReqHandover, ReqParentReply, ReqAnswer,
		ReqScheduleAsk, ReqExitHook, ReqListGap, ReqRules}
	if len(kinds) != len(kindRules) {
		t.Fatalf("%d kinds, %d rules", len(kinds), len(kindRules))
	}
	// Only these may be sent again after a fetch (design v2, break 1).
	resend := map[RequestKind]bool{ReqHello: true, ReqRules: true, ReqHandover: true, ReqWake: true}
	for _, k := range kinds {
		r, ok := RuleFor(k)
		if !ok {
			t.Fatalf("no rule for %s", k)
		}
		if r.ReadOnly != resend[k] {
			t.Errorf("%s: read-only is %v", k, r.ReadOnly)
		}
		if r.DeliveryOnly != (r.ListSource == "message" || r.ListSource == "handoff" || r.ListSource == "question") {
			t.Errorf("%s: delivery-only and its list source %q disagree", k, r.ListSource)
		}
	}
	for _, k := range []RequestKind{ReqAsk, ReqBug, ReqScheduleAsk, ReqExitHook, ReqListGap} {
		if r, _ := RuleFor(k); r.DeliveryOnly {
			t.Errorf("%s asks for work, so receipt must never close it", k)
		}
	}
}
