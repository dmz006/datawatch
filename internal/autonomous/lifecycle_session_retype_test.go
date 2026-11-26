// A lifecycle send whose text may already be in the session is not sent a
// second time.

package autonomous

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionPushTextMayBeInSessionIsNotTypedAgain(t *testing.T) {
	for name, sendErr := range map[string]error{
		"prompt appeared after the text was typed": errPromptAfterTyping,
		"keys sent in part":                        errors.New("send input: exit status 1"),
		"typed but the session was not saved":      errors.New("save session: disk full"),
	} {
		t.Run(name, func(t *testing.T) {
			rec := &sessionPushRecorder{inputErr: sendErr}
			m, store, _ := newEngineForTest(t, rec, sessionTable{"ops": 0}, 0)
			var mu sync.Mutex
			var alerts []string
			m.SetAlertSink(func(level, title, body string) string {
				mu.Lock()
				defer mu.Unlock()
				alerts = append(alerts, level+"|"+title+"|"+body)
				return "a"
			})
			sub, err := store.Add(Subscription{
				Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
				Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
				OneShot: true,
			})
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			m.emit(startedEvent())
			waitFor(t, "first attempt", func() bool { return rec.inputCount() >= 1 })
			time.Sleep(150 * time.Millisecond) // bounded assert-not-happens window for a second attempt
			if n := rec.inputCount(); n != 1 {
				t.Fatalf("typed %d times, want 1", n)
			}
			if _, ok := store.Get(sub.ID); !ok {
				t.Fatal("an unsure delivery used up the one-shot")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(alerts) != 1 || !strings.Contains(alerts[0], "review needed") || !strings.Contains(alerts[0], sub.ID) {
				t.Fatalf("alerts = %q, want one 'review needed' naming the subscription", alerts)
			}
		})
	}
}
