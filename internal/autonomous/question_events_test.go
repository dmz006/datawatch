// question_events_test.go — the two operator-question lifecycle event
// kinds, question.answered and question.withdrawn: both are registered in
// the known kind set so every existing subscription surface accepts them
// (no separate validation per surface), question.expired is and stays a
// rejected unknown kind (a question never expires by a clock), the kinds
// are kept out of the default notify set (a subscriber must list them
// explicitly), and the LifecycleEvent JSON shape carries the question and
// answer record ids as optional fields.
package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestQuestionEventKindsKnown: the two kinds are in the known set and
// question.expired is not.
func TestQuestionEventKindsKnown(t *testing.T) {
	for _, k := range []string{KindQuestionAnswered, KindQuestionWithdrawn} {
		if !strings.HasPrefix(k, "question.") {
			t.Errorf("kind %q does not use the question. prefix", k)
		}
		if !KnownEventKinds[k] {
			t.Errorf("KnownEventKinds missing %q", k)
		}
		if !IsKnownEventKind(k) {
			t.Errorf("IsKnownEventKind(%q) = false, want true", k)
		}
	}
	if KnownEventKinds["question.expired"] {
		t.Error("KnownEventKinds must not contain question.expired")
	}
	if IsKnownEventKind("question.expired") {
		t.Error("IsKnownEventKind(\"question.expired\") = true, want false")
	}
}

// TestQuestionEventKindsValidForSubscriptions: the central
// ValidateSubscription accepts both kinds for every sink accepted in this
// phase, and rejects question.expired with the unknown-kind error the
// REST/MCP/CLI/comm surfaces all surface.
func TestQuestionEventKindsValidForSubscriptions(t *testing.T) {
	now := time.Now()
	sinks := []SubscriptionSink{
		{Type: SinkComm, Target: "chan:main"},
		{Type: SinkSession, Target: "abcd"},
		{Type: SinkTopic, Target: "topic:main"},
		{Type: SinkAlert, Target: "alert:main"},
		{Type: SinkMCPQueue, Target: "ci.events:main-1"},
	}
	for _, k := range []string{KindQuestionAnswered, KindQuestionWithdrawn} {
		for _, sink := range sinks {
			sub := Subscription{Filter: LifecycleFilter{Kinds: []string{k}}, Sink: sink}
			if err := ValidateSubscription(sub, now); err != nil {
				t.Errorf("ValidateSubscription(kinds=[%q], sink=%s) = %v, want nil", k, sink.Type, err)
			}
		}
		// The notification gate accepts them too: a subscriber may list
		// them explicitly.
		if err := ValidateNotifyEvents([]string{k}); err != nil {
			t.Errorf("ValidateNotifyEvents([%q]) = %v, want nil", k, err)
		}
	}

	// ValidNotifyEventKinds (the closed valid set every surface validates
	// against) exposes both, and still rejects question.expired.
	valid := map[string]bool{}
	for _, k := range ValidNotifyEventKinds() {
		valid[k] = true
	}
	if !valid[KindQuestionAnswered] || !valid[KindQuestionWithdrawn] {
		t.Errorf("ValidNotifyEventKinds() = %v; missing the question kinds", ValidNotifyEventKinds())
	}
	if valid["question.expired"] {
		t.Error("ValidNotifyEventKinds() must not contain question.expired")
	}

	// The rejection side: the exact-kind subscription the operator would
	// type in any of the surfaces is refused before it can be stored.
	bad := Subscription{Filter: LifecycleFilter{Kinds: []string{"question.expired"}}, Sink: SubscriptionSink{Type: SinkComm, Target: "chan:main"}}
	err := ValidateSubscription(bad, now)
	if err == nil {
		t.Fatal("ValidateSubscription(kinds=[question.expired]) = nil, want the unknown-kind error")
	}
	if !strings.Contains(err.Error(), "question.expired") {
		t.Errorf("error %q does not name the rejected kind", err)
	}
	for _, mixed := range [][]string{{KindQuestionAnswered, "question.expired"}, {"question.expired", KindQuestionWithdrawn}} {
		err := ValidateNotifyEvents(mixed)
		if err == nil {
			t.Errorf("ValidateNotifyEvents(%v) = nil, want a rejection", mixed)
		}
		if !strings.Contains(err.Error(), "question.expired") {
			t.Errorf("ValidateNotifyEvents(%v) error %q does not name the rejected kind", mixed, err)
		}
	}
}

// TestQuestionEventKindsNotInDefaultEvents: the kinds stay out of the
// default notify set — subscribers must list them explicitly.
func TestQuestionEventKindsNotInDefaultEvents(t *testing.T) {
	for _, k := range DefaultNotifyEvents() {
		if k == KindQuestionAnswered || k == KindQuestionWithdrawn {
			t.Errorf("DefaultNotifyEvents() contains %q, want the question kinds absent (opt-in only)", k)
		}
	}
}

// TestQuestionEventKindsJSONShape: the LifecycleEvent wire shape for the
// two kinds — kind string, the optional question/answer ids present when
// set and omitted when empty (every other kind keeps its current shape).
func TestQuestionEventKindsJSONShape(t *testing.T) {
	answeredRaw, err := json.Marshal(LifecycleEvent{
		Kind:       KindQuestionAnswered,
		Level:      LevelPRD,
		PRDID:      "prd_a1b2",
		QuestionID: "q_0123456789abcdef",
		AnswerID:   "a_0123456789abcdef",
		Details:    "operator chose Yes",
	})
	if err != nil {
		t.Fatalf("marshal question.answered event: %v", err)
	}
	for _, want := range []string{
		`"kind":"question.answered"`,
		`"level":"prd"`,
		`"prd_id":"prd_a1b2"`,
		`"question_id":"q_0123456789abcdef"`,
		`"answer_id":"a_0123456789abcdef"`,
		`"details":"operator chose Yes"`,
	} {
		if !strings.Contains(string(answeredRaw), want) {
			t.Errorf("question.answered JSON lacks %s:\n%s", want, answeredRaw)
		}
	}

	withdrawnRaw, err := json.Marshal(LifecycleEvent{
		Kind:       KindQuestionWithdrawn,
		Level:      LevelPRD,
		PRDID:      "prd_a1b2",
		QuestionID: "q_0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("marshal question.withdrawn event: %v", err)
	}
	for _, want := range []string{
		`"kind":"question.withdrawn"`,
		`"question_id":"q_0123456789abcdef"`,
	} {
		if !strings.Contains(string(withdrawnRaw), want) {
			t.Errorf("question.withdrawn JSON lacks %s:\n%s", want, withdrawnRaw)
		}
	}
	if strings.Contains(string(withdrawnRaw), `answer_id`) {
		t.Errorf("question.withdrawn JSON must omit the empty answer_id:\n%s", withdrawnRaw)
	}

	// A non-question kind keeps today's shape: no question_id/answer_id
	// keys leak into other kinds' payload.
	otherRaw, err := json.Marshal(LifecycleEvent{Kind: PRDStatusKind(PRDBlocked), Level: LevelPRD, PRDID: "prd_a1b2"})
	if err != nil {
		t.Fatalf("marshal prd.blocked event: %v", err)
	}
	for _, absent := range []string{`question_id`, `answer_id`, `question`} {
		if strings.Contains(string(otherRaw), absent) {
			t.Errorf("%s JSON must not contain %q:\n%s", PRDStatusKind(PRDBlocked), absent, otherRaw)
		}
	}
}
