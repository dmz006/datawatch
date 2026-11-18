package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func extSub(sinkType, target, ref string) Subscription {
	return Subscription{
		Filter: LifecycleFilter{Level: LevelTask},
		Sink:   SubscriptionSink{Type: sinkType, Target: target, SecretRef: ref},
	}
}

func TestExtensionEventKindAccepted(t *testing.T) {
	info := EventKindInfo{Kind: "ext.accepted_kind", Description: "An extension kind.", Group: EventGroupTask}
	if err := RegisterEventKind(info); err != nil {
		t.Fatal(err)
	}
	if !IsKnownEventKind(info.Kind) {
		t.Fatal("registered kind is not known")
	}
	found := false
	for _, e := range ListEventKinds() {
		if e.Kind == info.Kind {
			found = true
		}
	}
	if !found {
		t.Fatal("registered kind missing from ListEventKinds")
	}
	sub := extSub(SinkAlert, "x", "")
	sub.Filter = LifecycleFilter{Kinds: []string{info.Kind}}
	if err := ValidateSubscription(sub, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionEventKindDuplicate(t *testing.T) {
	info := EventKindInfo{Kind: "ext.dup_kind", Description: "d", Group: EventGroupTask}
	if err := RegisterEventKind(info); err != nil {
		t.Fatal(err)
	}
	if err := RegisterEventKind(info); err == nil {
		t.Fatal("duplicate accepted")
	}
	builtin := ListEventKinds()[0]
	builtin.Description = "d"
	if err := RegisterEventKind(builtin); err == nil {
		t.Fatal("built-in collision accepted")
	}
}

func TestExtensionEventKindInvalidName(t *testing.T) {
	for _, n := range []string{"", "Upper.case", "has space", "-x", "a..b", strings.Repeat("a", 65)} {
		if err := RegisterEventKind(EventKindInfo{Kind: n, Description: "d", Group: EventGroupTask}); err == nil {
			t.Errorf("name %q accepted", n)
		}
	}
}

func TestExtensionEventKindMissingDescription(t *testing.T) {
	if err := RegisterEventKind(EventKindInfo{Kind: "ext.nodesc", Group: EventGroupTask}); err == nil {
		t.Fatal("missing description accepted")
	}
	if err := RegisterEventKind(EventKindInfo{Kind: "ext.badgroup", Description: "d", Group: "nope"}); err == nil {
		t.Fatal("invalid group accepted")
	}
}

func TestExtensionSinkAccepted(t *testing.T) {
	if err := RegisterSinkType("ext_sink_ok", nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSubscription(extSub("ext_sink_ok", "dest", ""), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSubscription(extSub("ext_sink_ok", "", ""), time.Now()); err == nil {
		t.Fatal("empty target accepted")
	}
	if err := ValidateSubscription(extSub("ext_sink_ok", "dest", "${secret:k}"), time.Now()); err == nil {
		t.Fatal("secret_ref accepted without validator")
	}
	if err := ValidateSubscription(extSub("ext_sink_ok", strings.Repeat("a", MaxExtensionSinkTargetLen+1), ""), time.Now()); err == nil {
		t.Fatal("long target accepted")
	}
}

func TestExtensionSinkValidatorAndDuplicate(t *testing.T) {
	bad := errors.New("rejected")
	if err := RegisterSinkType("ext_sink_val", func(s SubscriptionSink) error {
		if s.Target == "bad" {
			return bad
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterSinkType("ext_sink_val", nil); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := RegisterSinkType(SinkComm, nil); err == nil {
		t.Fatal("built-in accepted")
	}
	if err := RegisterSinkType("Bad Name", nil); err == nil {
		t.Fatal("invalid name accepted")
	}
	if err := ValidateSubscription(extSub("ext_sink_val", "bad", ""), time.Now()); !errors.Is(err, bad) {
		t.Fatalf("want validator error, got %v", err)
	}
	if err := ValidateSubscription(extSub("ext_sink_val", "good", "${secret:k}"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSubscription(extSub("never_registered", "x", ""), time.Now()); err == nil {
		t.Fatal("unknown sink accepted")
	}
}
