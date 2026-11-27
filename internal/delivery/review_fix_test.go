package delivery

// Tests for the faults the review of the core found.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// floodHarness has a flood alarm open: 60 new failures after the start mark,
// plus one held Automaton that is listed.
func floodHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.pass()
	h.cands = []Candidate{{Kind: KindAutomaton, ID: "aaheld", State: "held", Problem: "held_no_reason", Since: h.now}}
	for i := 0; i < 60; i++ {
		h.cands = append(h.cands, failedWork(fmt.Sprintf("a%03d", i), h.now))
	}
	h.next()
	return h
}

// massHarness has the mass-expiry alarm open.
func massHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.pass()
	for i := 0; i < 25; i++ {
		h.cands = append(h.cands, Candidate{Kind: KindAutomaton, ID: fmt.Sprintf("h%02d", i), State: "held",
			Problem: "held_review_due", TimeDriven: true, Since: h.now})
	}
	h.next()
	return h
}

func openAlarm(t *testing.T, r Report, id string) Entry {
	t.Helper()
	a, ok := findEntry(r.Items, id)
	if !ok || a.Kind != KindAlarm {
		t.Fatalf("no %s alarm is listed: %v", id, r.Items)
	}
	return a
}

func TestAlarmsCannotBeAcceptedOrDropped(t *testing.T) {
	for id, build := range map[string]func(*testing.T) *harness{alarmFlood: floodHarness, alarmMassExpiry: massHarness} {
		t.Run(id, func(t *testing.T) {
			h := build(t)
			alarm := openAlarm(t, h.e.Report(), id)
			// An alarm shares no state or reason with the work it stands for.
			if alarm.State == "held" || alarm.Reason == "held" {
				t.Fatalf("alarm state=%q reason=%q collide with a held Automaton", alarm.State, alarm.Reason)
			}
			until := h.now.Add(29 * 24 * time.Hour)
			selectors := map[string]Selector{
				"key":        {Keys: []string{alarm.Key}},
				"kind":       {Kind: KindAlarm},
				"state":      {State: alarm.State},
				"reason":     {Reason: alarm.Reason},
				"mixed keys": {Keys: []string{alarm.Key, "automaton:aaheld:held"}},
			}
			for name, sel := range selectors {
				if n, err := h.e.Accept(sel, "parked", until, "cli"); !errors.Is(err, ErrAlarm) || n != 0 {
					t.Errorf("accept by %s: changed=%d err=%v, want ErrAlarm", name, n, err)
				}
				if n, err := h.e.Drop(sel, "noise", "cli"); !errors.Is(err, ErrAlarm) && !errors.Is(err, ErrNotDroppable) || n != 0 {
					t.Errorf("drop by %s: changed=%d err=%v, want a refusal", name, n, err)
				}
			}
			r := h.next()
			if len(r.Accepted) != 0 {
				t.Fatalf("a refused action changed something: %v", r.Accepted)
			}
			openAlarm(t, r, id)
		})
	}
}

func TestAcceptingHeldWorkAcceptsNoAlarmAndNothingIsHidden(t *testing.T) {
	h := floodHarness(t)
	n, err := h.e.Accept(Selector{State: "held"}, "parked", h.now.Add(29*24*time.Hour), "cli")
	if err != nil || n != 1 {
		t.Fatalf("accept --state held: changed=%d err=%v, want only the held Automaton", n, err)
	}
	openAlarm(t, h.next(), alarmFlood)

	g := massHarness(t)
	// Only the alarm is listed while the 25 are held back: there is nothing
	// held to accept, and the alarm is not taken instead.
	if n, err := g.e.Accept(Selector{State: "held"}, "parked", g.now.Add(29*24*time.Hour), "cli"); !errors.Is(err, ErrNoMatch) || n != 0 {
		t.Fatalf("accept --state held over a mass alarm: %d %v", n, err)
	}
}

func TestALostListFileIsAnUnreadableRecord(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.sender.setFail(true)
	h.cands = []Candidate{failedWork("new1", h.now)}
	h.next()
	if err := os.Remove(filepath.Join(h.dir, stateFileName)); err != nil {
		t.Fatal(err)
	}
	h.sender.setFail(false)
	before, _ := readHeartbeat(t, h.dir)
	h.open()
	h.now = h.now.Add(testInterval)
	r, err := h.e.Pass(context.Background())
	if !errors.Is(err, ErrRecords) || !hasRed(r, RedRecords) || r.ByRule[InvUnopenable] == 0 || len(open(r)) != 0 {
		t.Fatalf("err=%v red=%v rules=%v open=%v", err, r.Red, r.ByRule, open(r))
	}
	if exists(filepath.Join(h.dir, stateFileName)) {
		t.Fatal("a new list was written over the lost one")
	}
	if after, _ := readHeartbeat(t, h.dir); after.Unix != before.Unix {
		t.Fatal("the heartbeat moved")
	}
}

func filterFailure(id string, at time.Time) Candidate {
	return Candidate{Kind: KindFilter, ID: id, State: "send_failed", Problem: "send_failed", Since: at}
}

func TestAFailureThatCannotBeSavedIsHeldUntilItCan(t *testing.T) {
	for name, fault := range map[string]struct{ broke, mend func(h *harness) }{
		"full disk": {broke: func(h *harness) { h.diskOK = false }, mend: func(h *harness) { h.diskOK = true }},
		"unreadable records": {
			broke: func(h *harness) {
				data, _ := os.ReadFile(filepath.Join(h.dir, stateFileName))
				_ = os.WriteFile(filepath.Join(h.dir, "kept"), data, 0o600)
				_ = os.WriteFile(filepath.Join(h.dir, stateFileName), []byte("damaged"), 0o600)
				h.open()
			},
			mend: func(h *harness) {
				data, _ := os.ReadFile(filepath.Join(h.dir, "kept"))
				_ = os.WriteFile(filepath.Join(h.dir, stateFileName), data, 0o600)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.pass()
			fault.broke(h)
			if err := h.e.Record(filterFailure("f1@s1", h.now)); err == nil {
				t.Fatal("the save must be reported as failed")
			}
			_ = h.e.Record(filterFailure("f1@s1", h.now)) // a repeat raises the count
			for i := 0; i < 3; i++ {
				h.now = h.now.Add(testInterval)
				_, _ = h.e.Pass(context.Background())
				if r := h.e.Report(); r.Result != ResultRed || !hasRed(r, RedCannotRecord) {
					t.Fatalf("while a failure is unsaved: %s %v", r.Result, r.Red)
				}
			}
			fault.mend(h)
			h.next()
			r := h.run(time.Hour)
			e, ok := findEntry(r.Items, "f1@s1")
			if !ok || e.Count != 2 || r.Result != ResultRed || hasRed(r, RedCannotRecord) {
				t.Fatalf("after the repair: entry=%+v ok=%v red=%v", e, ok, r.Red)
			}
		})
	}
}

func TestHeldFailuresAreBoundedAndTheLossIsSaid(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.diskOK = false
	for i := 0; i < maxUnsaved+25; i++ {
		_ = h.e.Record(filterFailure(fmt.Sprintf("f%04d@s", i), h.now))
	}
	if n := len(h.e.unsaved); n != maxUnsaved {
		t.Fatalf("held %d, want at most %d", n, maxUnsaved)
	}
	h.diskOK = true
	r := h.next()
	lost, ok := findEntry(r.Items, alarmEventsLost)
	if !ok || lost.Count != 25 {
		t.Fatalf("the 25 that could not be held are not said: %+v ok=%v", lost, ok)
	}
}

// Only the heartbeat cannot be written: the list saves, so each pass gets
// further than the one before it failed at.
func TestTheCannotSaveWarningIsNotRepeatedEveryPass(t *testing.T) {
	h := newHarness(t)
	h.pass()
	hb := filepath.Join(h.dir, HeartbeatFile)
	if err := os.Remove(hb); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hb, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 288; i++ {
		h.now = h.now.Add(testInterval)
		_, _ = h.e.Pass(context.Background())
	}
	if h.sender.count() != 1 {
		t.Fatalf("%d warnings in a day, want 1", h.sender.count())
	}
	// Once a whole pass works again, a new fault is warned about again.
	if err := os.RemoveAll(hb); err != nil {
		t.Fatal(err)
	}
	h.next()
	h.diskOK = false
	h.now = h.now.Add(testInterval)
	_, _ = h.e.Pass(context.Background())
	if h.sender.count() != 2 {
		t.Fatalf("a new fault: %d warnings in all, want 2", h.sender.count())
	}
}

func TestTheCannotSaveWarningIsTriedUntilAChannelAcceptsIt(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.diskOK = false
	h.sender.setFail(true)
	for i := 0; i < 288; i++ {
		h.now = h.now.Add(testInterval)
		_, _ = h.e.Pass(context.Background())
	}
	tried := h.sender.count()
	// A day of passes: retried, backing off to once an hour.
	if tried < 20 || tried > 40 {
		t.Fatalf("%d tries in a day with every channel refusing, want about one an hour", tried)
	}
	h.sender.setFail(false)
	for i := 0; i < 288; i++ {
		h.now = h.now.Add(testInterval)
		_, _ = h.e.Pass(context.Background())
	}
	if got := h.sender.count() - tried; got != 1 {
		t.Fatalf("after the channel returned: %d more sends, want the one accepted warning", got)
	}
}

func TestANullEntryIsAnUnreadableRecordAndNoPanic(t *testing.T) {
	for name, file := range map[string]struct{ name, body string }{
		"entry":  {stateFileName, `{"version":1,"last_pass":"2026-11-26T12:00:00Z","entries":{"x":null}}`},
		"track":  {stateFileName, `{"version":1,"last_pass":"2026-11-26T12:00:00Z","tracks":{"x":null}}`},
		"source": {startMarkFile, `{"version":1,"at":"2026-11-26T12:00:00Z","sources":{"planted":null}}`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.pass()
			path := filepath.Join(h.dir, file.name)
			if err := os.WriteFile(path, []byte(file.body), 0o600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("panic: %v", p)
				}
			}()
			h.open()
			h.e.Report()
			h.now = h.now.Add(testInterval)
			r, err := h.e.Pass(context.Background())
			if !errors.Is(err, ErrRecords) || r.ByRule[InvUnopenable] == 0 {
				t.Fatalf("err=%v red=%v", err, r.Red)
			}
		})
	}
}

func TestClosedRecordedFailuresArePrunedInTime(t *testing.T) {
	h := newHarness(t)
	h.pass()
	if err := h.e.Record(filterFailure("f3@s3", h.now)); err != nil {
		t.Fatal(err)
	}
	if err := h.e.Record(filterFailure("f4@s4", h.now)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.e.Drop(Selector{Keys: []string{"filter:f3@s3:send_failed"}}, "noise", "cli"); err != nil {
		t.Fatal(err)
	}
	h.next()
	if _, kept := h.e.rec.Entries["filter:f3@s3:send_failed"]; !kept {
		t.Fatal("pruned too early")
	}
	h.now = h.now.Add(closedKept + 24*time.Hour)
	h.pass()
	h.pass()
	if _, kept := h.e.rec.Entries["filter:f3@s3:send_failed"]; kept {
		t.Fatal("a dropped filter entry is kept for ever")
	}
	if _, kept := h.e.rec.Entries["filter:f4@s4:send_failed"]; !kept {
		t.Fatal("an open one must stay")
	}
}

func TestTheTestMessageIsLimitedAndRespectsNotifyOff(t *testing.T) {
	h := newHarness(t)
	h.pass()
	send := func() error {
		_, err := h.e.TestMessage(context.Background())
		return err
	}
	if err := send(); err != nil || h.sender.count() != 1 {
		t.Fatalf("first: %v sends=%d", err, h.sender.count())
	}
	for i := 0; i < 100; i++ {
		if err := send(); !errors.Is(err, ErrTestTooSoon) {
			t.Fatalf("at once again: %v", err)
		}
	}
	accepted := 1
	for i := 0; i < 59; i++ { // once a minute for the rest of the hour
		h.now = h.now.Add(time.Minute)
		if err := send(); err == nil {
			accepted++
		} else if !errors.Is(err, ErrTestTooSoon) {
			t.Fatal(err)
		}
	}
	if accepted != testsPerHour || h.sender.count() != testsPerHour {
		t.Fatalf("accepted %d, sent %d in an hour, want %d", accepted, h.sender.count(), testsPerHour)
	}
	h.now = h.now.Add(time.Hour)
	h.notify = false
	if err := send(); !errors.Is(err, ErrNotifyOff) || h.sender.count() != testsPerHour {
		t.Fatalf("with notify off: %v sends=%d", err, h.sender.count())
	}
	if r := h.e.Report(); hasRed(r, RedSendsFail) {
		t.Fatalf("a refused test says nothing about the channels: %v", r.Red)
	}
}

func TestAKeyFirstEnableThatStoppedBeforeSealingIsFinishedAtTheNextStart(t *testing.T) {
	dir := t.TempDir()
	k, err := openKey(t, dir, []byte("pw"))
	if err != nil || !k.AdoptPlain() {
		t.Fatalf("a new key adopts plain records: %v", err)
	}
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	// The daemon stops here, before anything is sealed.
	again, err := openKey(t, dir, []byte("pw"))
	if err != nil || !again.AdoptPlain() || again.IsNew() {
		t.Fatalf("the next start still adopts them: err=%v adopt=%v", err, again.AdoptPlain())
	}
	if err := again.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := again.SealDone(); err != nil {
		t.Fatal(err)
	}
	third, err := openKey(t, dir, []byte("pw"))
	if err != nil || third.AdoptPlain() {
		t.Fatalf("once sealed, a plain record is foreign: err=%v adopt=%v", err, third.AdoptPlain())
	}
}

func TestLeavingSecureModeOnPurpose(t *testing.T) {
	dir := t.TempDir()
	k, _ := openKey(t, dir, []byte("pw"))
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.dir, h.key = filepath.Join(dir, HeartbeatDir), k.Key()
	h.open()
	h.pass()
	_, err := openKey(t, dir, nil)
	if !errors.Is(err, ErrStoreKey) || !strings.Contains(err.Error(), "delivery folder") {
		t.Fatalf("the refusal says how to leave: %v", err)
	}
	if err := os.Remove(keyPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := openKey(t, dir, nil); !errors.Is(err, ErrStoreKey) {
		t.Fatalf("the key alone is not enough: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, HeartbeatDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := openKey(t, dir, nil); err != nil {
		t.Fatalf("with the key and the delivery folder gone: %v", err)
	}
}
