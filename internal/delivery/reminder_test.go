package delivery

// Tests for the faults the second review found in telling and its texts.

import (
	"os"
	"strings"
	"testing"
	"time"
)

// With more items than a message has lines, a reminder marks as sent only the
// ones it names, takes the longest unnamed first, and still goes once a day.
func TestNotifyReminderRecordsOnlyWhatItNamesAndRotates(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy"}}
	es := notifyEntries(60)
	at := notifyNow
	for i := 0; i < 3; i++ { // three first notices tell all 60
		if n, _, ok := notifyPass(es, s, at); !ok || n.Reminder || len(n.Keys) != notifyMaxLines {
			t.Fatalf("first notice %d: %v %+v", i, ok, n)
		}
		at = at.Add(5 * time.Minute)
	}
	namedOn := map[string][]int{}
	var reminders []time.Time
	for at = notifyNow.Add(15 * time.Minute); at.Before(notifyNow.Add(4 * 24 * time.Hour)); at = at.Add(5 * time.Minute) {
		before := map[string]time.Time{}
		for _, e := range es {
			before[e.Key] = *e.LastSentAt
		}
		n, _, ok := notifyPass(es, s, at)
		if !ok {
			continue
		}
		if !n.Reminder || len(n.Keys) != notifyMaxLines {
			t.Fatalf("at %s: %+v", at, n)
		}
		named := map[string]bool{}
		for _, k := range n.Keys {
			named[k] = true
			if !strings.Contains(n.Text, k) {
				t.Fatalf("%s is recorded as sent and has no line", k)
			}
			namedOn[k] = append(namedOn[k], len(reminders))
		}
		for _, e := range es {
			if moved := !e.LastSentAt.Equal(before[e.Key]); moved != named[e.Key] {
				t.Fatalf("%s: recorded as sent=%v, named=%v", e.Key, moved, named[e.Key])
			}
		}
		if !strings.Contains(n.Text, "60 items") || !strings.Contains(n.Text, "40 more") {
			t.Fatalf("counts missing: %s", n.Text)
		}
		reminders = append(reminders, at)
	}
	if len(reminders) != 3 {
		t.Fatalf("reminders at %v, want one a day", reminders)
	}
	for i := 1; i < len(reminders); i++ {
		if gap := reminders[i].Sub(reminders[i-1]); gap < 24*time.Hour {
			t.Fatalf("two reminders %s apart", gap)
		}
	}
	for _, e := range es {
		if len(namedOn[e.Key]) != 1 {
			t.Fatalf("%s was named in reminders %v, want once in three days", e.Key, namedOn[e.Key])
		}
	}
}

func TestNotifyAFailedReminderIsTriedAgainBeforeAnotherBatch(t *testing.T) {
	s := &notifySender{channels: []string{"ntfy"}}
	es := notifyEntries(60)
	at := notifyNow
	for i := 0; i < 3; i++ {
		notifyPass(es, s, at)
		at = at.Add(5 * time.Minute)
	}
	s.fail = map[string]bool{"ntfy": true}
	first, _, ok := notifyPass(es, s, notifyNow.Add(25*time.Hour))
	if !ok || !first.Reminder {
		t.Fatalf("no reminder: %v %+v", ok, first)
	}
	// Before the retry is due nothing else goes out.
	if n, _, ok := notifyPass(es, s, notifyNow.Add(25*time.Hour+10*time.Second)); ok {
		t.Fatalf("another batch while one waits for its retry: %+v", n)
	}
	s.fail = nil
	again, _, ok := notifyPass(es, s, notifyNow.Add(25*time.Hour+2*time.Minute))
	if !ok || strings.Join(again.Keys, ",") != strings.Join(first.Keys, ",") {
		t.Fatalf("the retry names other items: %v %+v", ok, again)
	}
}

// The text of a refused date and the command reference say what works.
func TestTheReviewDateRefusalSaysWhatADateMeans(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	h := newHarness(t)
	h.cands = []Candidate{failedWork("s1", h.now)}
	h.pass()
	for days, ok := range map[int]bool{29: true, 30: false} {
		until, err := ParseUntil(now.AddDate(0, 0, days).Format("2006-01-02"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.e.Accept(Selector{Backlog: true}, "parked", until, "cli")
		if (err == nil) != ok {
			t.Fatalf("today+%d: %v", days, err)
		}
		if err != nil && (!strings.Contains(err.Error(), "end of that day") || !strings.Contains(err.Error(), "29 days")) {
			t.Fatalf("the refusal does not say what a date means: %v", err)
		}
	}
}

func TestTheCommandReferenceListsTheDeliveryCommands(t *testing.T) {
	data, err := os.ReadFile("../../docs/commands.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"delivery check", "delivery ack", "delivery accept", "delivery drop", "delivery test",
		"datawatch delivery", "29 days"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("docs/commands.md lacks %q", want)
		}
	}
}
