// question_raise_notifier_test.go — the daemon-side adapter: every
// active messaging router receives one raise exactly once, and the
// router named by notify_exclude gets nothing — the same rules the
// lifecycle CommSink applies.

package main

import (
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/router"
)

// questionRaiseNotifierSample is the question the tests fan out.
func questionRaiseNotifierSample() autonomouspkg.Question {
	return autonomouspkg.Question{
		ID:       "q-abc123",
		Label:    "decision",
		Text:     "Cap the retry backoff at five minutes?",
		Options:  []autonomouspkg.QuestionOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}},
		Priority: autonomouspkg.QuestionPriorityHigh,
	}
}

func questionRaiseNotifierNew(t *testing.T, name string) (*router.Router, *fakeAlertCapture) {
	t.Helper()
	cap := newFakeAlertCapture(name)
	return router.NewRouter("host", name+"-1", cap, nil, 0, nil), cap
}

// TestQuestionRaiseNotifier_FansOutToEveryActiveRouter — one raise → one
// send on each active router (and not a second send on any of them).
func TestQuestionRaiseNotifier_FansOutToEveryActiveRouter(t *testing.T) {
	sigR, sig := questionRaiseNotifierNew(t, "signal")
	tgR, tg := questionRaiseNotifierNew(t, "telegram")
	n := questionRaiseNotifier{
		routers: []*router.Router{sigR, tgR},
		cfg:     &config.Config{},
	}
	q := questionRaiseNotifierSample()

	n.NotifyQuestionRaised(q)

	deadline := time.Now().Add(2 * time.Second)
	for (sig.count() < 1 || tg.count() < 1) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let any (wrong) extra sends land

	for name, cap := range map[string]*fakeAlertCapture{"signal": sig, "telegram": tg} {
		if nct := cap.count(); nct != 1 {
			t.Errorf("%s router got %d send(s), want exactly one: %q", name, nct, cap.all())
		} else if !containsAll(cap.all()[0], q.ID, "POST /api/questions/"+q.ID+"/answer") {
			t.Errorf("%s text missing the question id or the answer route:\n%s", name, cap.all()[0])
		}
	}
}

// TestQuestionRaiseNotifier_RespectsNotifyExclude — the router named by
// notify_exclude gets nothing (exactly like the Commsink), the others
// get the one send.
func TestQuestionRaiseNotifier_RespectsNotifyExclude(t *testing.T) {
	sigR, sig := questionRaiseNotifierNew(t, "signal")
	tgR, tg := questionRaiseNotifierNew(t, "telegram")
	n := questionRaiseNotifier{
		routers: []*router.Router{sigR, tgR},
		cfg:     &config.Config{NotifyExclude: []string{"telegram"}},
	}
	n.NotifyQuestionRaised(questionRaiseNotifierSample())

	deadline := time.Now().Add(2 * time.Second)
	for sig.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	if nct := sig.count(); nct != 1 {
		t.Fatalf("signal router got %d send(s), want exactly one", nct)
	}
	if nct := tg.count(); nct != 0 {
		t.Fatalf("telegram router got %d send(s) although it is in notify_exclude, want 0", nct)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
