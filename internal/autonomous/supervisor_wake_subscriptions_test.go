package autonomous

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type wakeTestClock struct{}

func (wakeTestClock) Now() time.Time { return time.Unix(2000, 0) }
func (wakeTestClock) AfterFunc(d time.Duration, f func()) func() bool {
	return func() bool { return true }
}

type wakeTestSubscriber struct {
	mu   sync.Mutex
	subs []wakeTestSub
}

type wakeTestSub struct {
	filter LifecycleFilter
	fn     func(LifecycleEvent)
}

func (s *wakeTestSubscriber) Subscribe(f LifecycleFilter, fn func(LifecycleEvent)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, wakeTestSub{f, fn})
	return func() {}
}

func (s *wakeTestSubscriber) emit(ev LifecycleEvent) {
	s.mu.Lock()
	subs := append([]wakeTestSub(nil), s.subs...)
	s.mu.Unlock()
	for _, sub := range subs {
		if sub.filter.Matches(ev) {
			sub.fn(ev)
		}
	}
}

func TestSupervisorWakeOffByDefault(t *testing.T) {
	sub := &wakeTestSubscriber{}
	set := RegisterSupervisorWake(SupervisorWakeConfig{
		Subscriber: sub, Settings: supervisorloop.DefaultWakeSettings(),
		Session:   func() string { return "sup" },
		SendInput: func(string, string) error { t.Fatal("nothing should be sent"); return nil },
		Clock:     wakeTestClock{},
	})
	if set != nil {
		t.Fatalf("set registered while the wake path is off")
	}
	if len(sub.subs) != 0 {
		t.Fatalf("registered %d subscriptions while off", len(sub.subs))
	}
}

func TestSupervisorWakeDeliversConfiguredKinds(t *testing.T) {
	sub := &wakeTestSubscriber{}
	var mu sync.Mutex
	var sent []string
	rec := supervisorloop.NewWakeRecord(wakeTestClock{}, 0)
	st := supervisorloop.DefaultWakeSettings()
	st.Enabled = true
	st.Kinds = []string{"task.failed"}
	st.CoalesceSeconds = 0
	set := RegisterSupervisorWake(SupervisorWakeConfig{
		Subscriber: sub, Settings: st,
		Session: func() string { return "sup" },
		SendInput: func(id, text string) error {
			mu.Lock()
			defer mu.Unlock()
			sent = append(sent, id+":"+text)
			return nil
		},
		Clock: wakeTestClock{}, Record: rec,
	})
	if set == nil || set.Name != SupervisorWakeSetName {
		t.Fatalf("set not registered: %+v", set)
	}
	defer set.Close()
	sub.emit(LifecycleEvent{Kind: "task.failed", PRDID: "a1", TaskID: "t1"})
	sub.emit(LifecycleEvent{Kind: "task.review_needed", PRDID: "a1", TaskID: "t2"})
	set.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || !strings.HasPrefix(sent[0], "sup:") || !strings.Contains(sent[0], "task.failed") {
		t.Fatalf("sent = %v", sent)
	}
	if n := rec.Status().EventCount; n != 1 {
		t.Fatalf("record event count = %d", n)
	}
}
