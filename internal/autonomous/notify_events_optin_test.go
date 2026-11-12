package autonomous

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOptInNotifyEvents(t *testing.T) {
	want := []string{KindGuardrailWarn, KindScopeWarning, KindTaskSessionCleared, KindTaskSessionStarted}
	got := OptInNotifyEvents()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OptInNotifyEvents() = %v; want %v", got, want)
	}
	got[0] = "corrupted"
	if OptInNotifyEvents()[0] == "corrupted" {
		t.Fatal("OptInNotifyEvents() must return a fresh slice each call")
	}
	if KindScopeWarning != "scope_warning" || KindGuardrailWarn != "guardrail_warn" {
		t.Fatalf("warning kind spellings changed: %q %q", KindScopeWarning, KindGuardrailWarn)
	}
}

func TestWarningKindsAreKnownAndValid(t *testing.T) {
	for _, k := range []string{KindScopeWarning, KindGuardrailWarn} {
		if !IsKnownEventKind(k) {
			t.Errorf("%q must be a known event kind", k)
		}
		if err := ValidateNotifyEvents([]string{k}); err != nil {
			t.Errorf("ValidateNotifyEvents(%q) = %v; want nil", k, err)
		}
		for _, sink := range []string{SinkComm, SinkSession, SinkTopic, SinkAlert, SinkMCPQueue} {
			for _, lvl := range []LifecycleLevel{"", LevelTask, LevelPRD} {
				sub := Subscription{
					Sink:   SubscriptionSink{Type: sink, Target: "target"},
					Filter: LifecycleFilter{Kinds: []string{k}, Level: lvl},
				}
				if err := ValidateSubscription(sub, time.Now()); err != nil {
					t.Errorf("ValidateSubscription(sink %q kind %q level %q) = %v; want nil", sink, k, lvl, err)
				}
			}
		}
	}
	if err := ValidateNotifyEvents([]string{KindPRDUpdated}); err == nil {
		t.Error("prd.updated must still be rejected")
	}
	if err := ValidateNotifyEvents([]string{"scope.warning"}); err == nil {
		t.Error("a misspelled warning kind must still be rejected")
	}
}

func TestDescribeLifecycleEventWarnings(t *testing.T) {
	cases := []struct {
		name string
		ev   LifecycleEvent
		want string
	}{
		{"scope warning", LifecycleEvent{Kind: KindScopeWarning, Level: LevelTask, PRDID: "p1", TaskID: "t1", Files: []string{"a.go", "b.go"}},
			"Automaton p1 (task t1): scope warning - files: a.go, b.go"},
		{"scope warning no files", LifecycleEvent{Kind: KindScopeWarning, Level: LevelPRD, PRDID: "p1"},
			"Automaton p1: scope warning"},
		{"guardrail warn", LifecycleEvent{Kind: KindGuardrailWarn, Level: LevelTask, PRDID: "p1", TaskID: "t1", Guardrail: "rules",
			Verdict: &GuardrailVerdict{Summary: "two issues\nfound"}},
			"Automaton p1 (task t1): guardrail rules warn - two issues found"},
		{"guardrail warn with link", LifecycleEvent{Kind: KindGuardrailWarn, Level: LevelPRD, PRDID: "p1", Guardrail: "rules", Link: "?alert=a1"},
			"Automaton p1: guardrail rules warn (link ?alert=a1)"},
	}
	for _, tc := range cases {
		if got := DescribeLifecycleEvent(tc.ev); got != tc.want {
			t.Errorf("%s: got %q; want %q", tc.name, got, tc.want)
		}
		if lvl := sinkLevel(tc.ev); lvl != "warn" {
			t.Errorf("%s: sinkLevel = %q; want warn", tc.name, lvl)
		}
	}
	long := LifecycleEvent{Kind: KindScopeWarning, PRDID: "p", Files: []string{strings.Repeat("x", 400)}}
	if got := DescribeLifecycleEvent(long); !strings.Contains(got, lifecycleSinkMsgSuffix) {
		t.Errorf("long file list should be truncated: %q", got)
	}
	// Existing rendering is unchanged.
	st := LifecycleEvent{Kind: "prd.failed", PRDID: "p1", OldStatus: "running", NewStatus: "failed"}
	if got := DescribeLifecycleEvent(st); got != "PRD p1: running -> failed" {
		t.Errorf("status rendering changed: %q", got)
	}
}
