package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var notifyNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

type notifySender struct {
	mu       sync.Mutex
	channels []string
	fail     map[string]bool
	hang     map[string]chan struct{}
	sent     map[string][]string
}

func (s *notifySender) Channels() []string { return s.channels }

func (s *notifySender) Send(_ context.Context, channel, text string) error {
	s.mu.Lock()
	block := s.hang[channel]
	s.mu.Unlock()
	if block != nil {
		<-block // ignores its context, as a backend with no timeout does
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent == nil {
		s.sent = map[string][]string{}
	}
	s.sent[channel] = append(s.sent[channel], text)
	if s.fail[channel] {
		return errors.New("401 unauthorized token=hunter2")
	}
	return nil
}

func notifyEntry(id string) *Entry {
	return &Entry{
		Key: "automaton:" + id + ":blocked", Kind: KindAutomaton, ID: id, State: "blocked",
		Reason: "awaiting_you", WaitsOn: WaitsOnOperator, Since: notifyNow.Add(-time.Hour), ListedAt: notifyNow,
	}
}

func notifyEntries(n int) []*Entry {
	out := make([]*Entry, n)
	for i := range out {
		out[i] = notifyEntry(fmt.Sprintf("a%04d", i))
	}
	return out
}

// notifyPass is what the engine does with the notify code in one pass.
func notifyPass(entries []*Entry, s Sender, now time.Time) (Notice, []SendResult, bool) {
	n, ok := DueNotice("host1", entries, now)
	if !ok {
		return n, nil, false
	}
	res := Deliver(context.Background(), s, n.Text, now)
	ApplySend(entries, n, res, now)
	return n, res, true
}

func TestNotifyThreeFailuresThenSuccessGivesOneToldRecord(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy"}, fail: map[string]bool{"ntfy": true}}
	es := notifyEntries(1)
	now := notifyNow
	for i, wait := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute} {
		_, res, ok := notifyPass(es, s, now)
		if !ok || Told(res) {
			t.Fatalf("attempt %d: due=%v told=%v", i+1, ok, Told(res))
		}
		e := es[0]
		if e.ToldAt != nil || e.LastSentAt != nil || e.SendFails != i+1 || e.NextTryAt == nil || !e.NextTryAt.Equal(now.Add(wait)) {
			t.Fatalf("attempt %d: told=%v last=%v fails=%d next=%v want +%s", i+1, e.ToldAt, e.LastSentAt, e.SendFails, e.NextTryAt, wait)
		}
		if _, ok := DueNotice("host1", es, now.Add(wait-time.Second)); ok {
			t.Fatalf("attempt %d: retried before the backoff ended", i+1)
		}
		now = now.Add(wait)
	}
	s.fail["ntfy"] = false
	if _, res, ok := notifyPass(es, s, now); !ok || !Told(res) {
		t.Fatalf("fourth attempt: due=%v %v", ok, res)
	}
	e := es[0]
	if e.ToldAt == nil || !e.ToldAt.Equal(now) || e.LastSentAt == nil || !e.LastSentAt.Equal(now) || e.SendFails != 0 || e.NextTryAt != nil {
		t.Fatalf("after success: %+v", e)
	}
	ok := 0
	for _, r := range e.Sends {
		if r.OK {
			ok++
		}
	}
	if len(e.Sends) != 4 || ok != 1 {
		t.Fatalf("history=%d accepted=%d, want 4 and 1", len(e.Sends), ok)
	}
	if _, due := DueNotice("host1", es, now.Add(time.Hour)); due {
		t.Fatal("a told item was sent again within the day")
	}
}

func TestNotifyBackoffStopsAtOneHourAndHistoryIsBounded(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy", "email"}, fail: map[string]bool{"ntfy": true, "email": true}}
	es := notifyEntries(1)
	now := notifyNow
	for i := 0; i < 200; i++ {
		if _, _, ok := notifyPass(es, s, now); !ok {
			t.Fatalf("attempt %d not due at its next try", i)
		}
		gap := es[0].NextTryAt.Sub(now)
		if gap > time.Hour || (i >= 6 && gap != time.Hour) {
			t.Fatalf("attempt %d: backoff %s", i, gap)
		}
		now = *es[0].NextTryAt
	}
	if len(es[0].Sends) != notifySendHistory || es[0].SendFails != 200 {
		t.Fatalf("history=%d fails=%d", len(es[0].Sends), es[0].SendFails)
	}
	for _, r := range es[0].Sends {
		if r.Code != sendCodeFailed {
			t.Fatalf("code=%q", r.Code)
		}
		if strings.Contains(fmt.Sprintf("%+v", r), "hunter2") {
			t.Fatalf("the channel's error text was kept: %+v", r)
		}
	}
}

func TestNotifyOneAcceptingChannelMeansTold(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy", "email"}, fail: map[string]bool{"ntfy": true}}
	es := notifyEntries(1)
	_, res, _ := notifyPass(es, s, notifyNow)
	if len(res) != 2 || res[0].Channel != "ntfy" || res[0].OK || res[1].Channel != "email" || !res[1].OK || !Told(res) {
		t.Fatalf("%+v", res)
	}
	if es[0].ToldAt == nil || es[0].NextTryAt != nil {
		t.Fatalf("%+v", es[0])
	}
}

func TestNotifyNoChannelTellsNothingAndRecordsNothing(t *testing.T) {
	for name, s := range map[string]Sender{"nil sender": nil, "no channel": &notifySender{}} {
		es := notifyEntries(1)
		_, res, ok := notifyPass(es, s, notifyNow)
		if !ok || len(res) != 0 || Told(res) {
			t.Fatalf("%s: due=%v results=%v", name, ok, res)
		}
		if es[0].ToldAt != nil || es[0].SendFails != 0 || es[0].NextTryAt != nil || len(es[0].Sends) != 0 {
			t.Fatalf("%s: a send that never happened changed the record: %+v", name, es[0])
		}
	}
}

func TestNotifyNeverSendsBacklogSeenAcceptedOrClosed(t *testing.T) {
	at := notifyNow.Add(-48 * time.Hour)
	es := notifyEntries(1000)
	for _, e := range es {
		e.Backlog = true
	}
	for i, mark := range []func(*Entry){
		func(e *Entry) { e.SeenAt = &at },
		func(e *Entry) { e.Accepted = &Accept{Reason: "later", Until: notifyNow.Add(time.Hour)} },
		func(e *Entry) { e.ClosedAt = &at },
	} {
		e := notifyEntry(fmt.Sprintf("x%d", i))
		mark(e)
		es = append(es, e)
		// The same, already told a long time ago: no reminder either.
		told := notifyEntry(fmt.Sprintf("y%d", i))
		told.ToldAt, told.LastSentAt = &at, &at
		mark(told)
		es = append(es, told)
	}
	oldBacklog := notifyEntry("told-backlog")
	oldBacklog.Backlog, oldBacklog.ToldAt, oldBacklog.LastSentAt = true, &at, &at
	es = append(es, oldBacklog, nil)
	s := &notifySender{channels: []string{"ntfy"}}
	for i := 0; i < 300; i++ {
		if n, _, ok := notifyPass(es, s, notifyNow.Add(time.Duration(i)*time.Second)); ok {
			t.Fatalf("pass %d sent %q", i, n.Text)
		}
	}
	if len(s.sent) != 0 {
		t.Fatalf("sent %v", s.sent)
	}
}

func TestNotifyAnEndedAcceptanceIsToldAgain(t *testing.T) {
	es := notifyEntries(1)
	es[0].Accepted = &Accept{Reason: "later", Until: notifyNow}
	if _, ok := DueNotice("host1", es, notifyNow.Add(time.Second)); !ok {
		t.Fatal("an acceptance that ended still hides the item")
	}
}

func TestNotifyDigestOncePerDayForToldAndNotAcknowledged(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy"}}
	es := notifyEntries(2)
	n, _, ok := notifyPass(es, s, notifyNow)
	if !ok || n.Reminder || len(n.Keys) != 2 {
		t.Fatalf("first: %v %+v", ok, n)
	}
	// A third item arrives and is told on its own, 20 hours later.
	third := notifyEntry("late")
	es = append(es, third)
	n, _, ok = notifyPass(es, s, notifyNow.Add(20*time.Hour))
	if !ok || n.Reminder || len(n.Keys) != 1 || n.Keys[0] != third.Key {
		t.Fatalf("a new item is a first notice of its own: %v %+v", ok, n)
	}
	// Every five minutes for three more days: one digest a day, naming all three.
	var digests []time.Time
	for at := notifyNow.Add(20 * time.Hour); at.Before(notifyNow.Add(96 * time.Hour)); at = at.Add(5 * time.Minute) {
		if n, _, ok := notifyPass(es, s, at); ok {
			if !n.Reminder || len(n.Keys) != 3 || !strings.Contains(n.Text, "reminder") {
				t.Fatalf("at %s: %+v", at, n)
			}
			digests = append(digests, at)
		}
	}
	if len(digests) != 3 {
		t.Fatalf("digests at %v, want 3", digests)
	}
	for i, at := range digests {
		if want := notifyNow.Add(time.Duration(i+1) * 24 * time.Hour); !at.Equal(want) {
			t.Fatalf("digest %d at %s, want %s", i, at, want)
		}
	}
	if es[0].ToldAt == nil || !es[0].ToldAt.Equal(notifyNow) {
		t.Fatalf("a reminder moved the first told time: %v", es[0].ToldAt)
	}
}

func TestNotifyNewItemsGoBeforeADueDigest(t *testing.T) {
	at := notifyNow.Add(-48 * time.Hour)
	old := notifyEntry("old")
	old.ToldAt, old.LastSentAt = &at, &at
	es := []*Entry{old, notifyEntry("new")}
	s := &notifySender{channels: []string{"ntfy"}}
	n, _, _ := notifyPass(es, s, notifyNow)
	if n.Reminder || len(n.Keys) != 1 || n.Keys[0] != es[1].Key {
		t.Fatalf("first: %+v", n)
	}
	n, _, ok := notifyPass(es, s, notifyNow.Add(5*time.Minute))
	if !ok || !n.Reminder {
		t.Fatalf("the digest is next: %v %+v", ok, n)
	}
}

func TestNotifyABackwardClockJumpDoesNotSilenceRetriesOrDigests(t *testing.T) {
	future := notifyNow.Add(240 * time.Hour)
	retry := notifyEntry("retry")
	retry.NextTryAt, retry.SendFails = &future, 3
	if _, ok := DueNotice("host1", []*Entry{retry}, notifyNow); !ok {
		t.Fatal("a retry time days ahead is still waited for")
	}
	told := notifyEntry("told")
	told.ToldAt, told.LastSentAt = &future, &future
	if _, ok := DueNotice("host1", []*Entry{told}, notifyNow); !ok {
		t.Fatal("a last send days ahead silences the digest")
	}
}

func TestNotifyMessageIsBoundedAndOnlyNamedItemsBecomeTold(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy"}}
	es := notifyEntries(75)
	n, _, _ := notifyPass(es, s, notifyNow)
	if len(n.Keys) != notifyMaxLines {
		t.Fatalf("keys=%d, want %d", len(n.Keys), notifyMaxLines)
	}
	named := map[string]bool{}
	for _, k := range n.Keys {
		named[k] = true
		if !strings.Contains(n.Text, k) {
			t.Fatalf("%s is marked but has no line", k)
		}
	}
	for _, e := range es {
		if named[e.Key] != (e.ToldAt != nil) {
			t.Fatalf("%s named=%v told=%v", e.Key, named[e.Key], e.ToldAt)
		}
		if !named[e.Key] && strings.Contains(n.Text, e.Key) {
			t.Fatalf("%s has a line but is not marked", e.Key)
		}
	}
	if !strings.Contains(n.Text, "75 items") || !strings.Contains(n.Text, "55 more") {
		t.Fatalf("counts missing: %s", n.Text)
	}
	// The rest follow, one message a pass.
	msgs := 1
	for at := notifyNow.Add(5 * time.Minute); msgs < 10; at = at.Add(5 * time.Minute) {
		if _, _, ok := notifyPass(es, s, at); !ok {
			break
		}
		msgs++
	}
	if msgs != 4 {
		t.Fatalf("75 items took %d messages, want 4", msgs)
	}
	for _, e := range es {
		if e.ToldAt == nil {
			t.Fatalf("%s never told", e.Key)
		}
	}
}

// Twilio refuses more than 1600 characters and Discord more than 2000: a
// message longer than that would fail for ever.
func TestNotifyMessageFitsTheSmallestChannelLimit(t *testing.T) {
	es := notifyEntries(40)
	for i, e := range es {
		e.ID = fmt.Sprintf("%s-%d", strings.Repeat("x", 400), i)
		e.Key = "automaton:" + e.ID + ":blocked"
	}
	n, ok := DueNotice(strings.Repeat("h", 300), es, notifyNow)
	if !ok || len(n.Text) > notifyMaxBytes || len(n.Keys) == 0 {
		t.Fatalf("ok=%v bytes=%d keys=%d", ok, len(n.Text), len(n.Keys))
	}
	at := notifyNow.Add(-48 * time.Hour)
	for _, e := range es {
		e.ToldAt, e.LastSentAt = &at, &at
	}
	n, ok = DueNotice("host1", es, notifyNow)
	if !ok || !n.Reminder || len(n.Text) > notifyMaxBytes || len(n.Keys) != 40 {
		t.Fatalf("digest: ok=%v bytes=%d keys=%d (a digest covers all of them)", ok, len(n.Text), len(n.Keys))
	}
}

func TestNotifyMessageHoldsOnlyIDsKindsStatesAndCodes(t *testing.T) {
	e := notifyEntry("a1")
	e.Reason = "SECRET title of the work"
	e.WaitsOn = "SECRET-waits"
	e.SeenBy, e.ClosedBy, e.DropReason, e.Source = "SECRET", "SECRET", "SECRET", "SECRET"
	e.Sends = []SendResult{{Channel: "SECRET", Code: "SECRET"}}
	e.Count = 3
	other := notifyEntry("a2")
	other.Key = "automaton:a2\nstop all:blocked"
	n, ok := DueNotice("host1", []*Entry{e, other}, notifyNow)
	if !ok || strings.Contains(n.Text, "SECRET") {
		t.Fatalf("ok=%v %q", ok, n.Text)
	}
	if !strings.HasPrefix(n.Text, "[host1] ") || !strings.Contains(n.Text, "automaton:a1:blocked") || !strings.Contains(n.Text, "x3") {
		t.Fatalf("%q", n.Text)
	}
	for _, line := range strings.Split(n.Text, "\n") {
		if strings.HasPrefix(line, "stop all") {
			t.Fatalf("a key started its own line: %q", n.Text)
		}
	}
	e.Reason = "awaiting_you"
	if n, _ = DueNotice("host1", []*Entry{e}, notifyNow); !strings.Contains(n.Text, "awaiting_you") {
		t.Fatalf("the reason code is missing: %q", n.Text)
	}
	if strings.Contains(n.Text, "PRD") {
		t.Fatalf("%q", n.Text)
	}
}

func TestNotifyOldestFirstAndInputOrderKept(t *testing.T) {
	a, b := notifyEntry("newer"), notifyEntry("older")
	b.Since = notifyNow.Add(-100 * time.Hour)
	es := []*Entry{a, b}
	n, _ := DueNotice("host1", es, notifyNow)
	if len(n.Keys) != 2 || n.Keys[0] != b.Key {
		t.Fatalf("oldest first: %v", n.Keys)
	}
	if es[0] != a || es[1] != b {
		t.Fatal("the caller's list was reordered")
	}
}

func TestNotifyFixedTexts(t *testing.T) {
	for name, text := range map[string]string{"cannot record": CannotRecordText("host1"), "test": TestText("host1")} {
		if !strings.HasPrefix(text, "[host1] ") || len(text) > 400 || strings.Contains(text, "PRD") {
			t.Fatalf("%s: %q", name, text)
		}
	}
	if CannotRecordText("host1") == TestText("host1") {
		t.Fatal("the two texts are the same")
	}
}

func TestNotifyAHungSendTimesOutAsAFailure(t *testing.T) {
	old := notifySendTimeout
	notifySendTimeout = 100 * time.Millisecond
	defer func() { notifySendTimeout = old }()
	release := make(chan struct{})
	defer close(release)
	s := &notifySender{channels: []string{"email", "ntfy"}, hang: map[string]chan struct{}{"email": release}}
	es := notifyEntries(1)
	done := make(chan []SendResult, 1)
	go func() {
		_, res, _ := notifyPass(es, s, notifyNow)
		done <- res
	}()
	select {
	case res := <-done:
		if len(res) != 2 || res[0].OK || res[0].Code != sendCodeTimeout || !res[1].OK {
			t.Fatalf("%+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a hung send held the pass")
	}
}

func TestNotifyACancelledPassIsAFailureWithACode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release := make(chan struct{})
	defer close(release)
	s := &notifySender{channels: []string{"email"}, hang: map[string]chan struct{}{"email": release}}
	res := Deliver(ctx, s, "x", notifyNow)
	if len(res) != 1 || res[0].OK || res[0].Code != sendCodeCancelled {
		t.Fatalf("%+v", res)
	}
}
