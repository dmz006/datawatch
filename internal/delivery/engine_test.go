package delivery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
)

const testInterval = 5 * time.Minute

type testSender struct {
	mu       sync.Mutex
	channels []string
	fail     bool
	sent     []string
}

func (s *testSender) Channels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.channels...)
}

func (s *testSender) Send(_ context.Context, _ string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, text)
	if s.fail {
		return errors.New("channel down: token=hunter2")
	}
	return nil
}

func (s *testSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *testSender) setFail(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = v
}

type harness struct {
	t      *testing.T
	dir    string
	key    []byte
	now    time.Time
	cands  []Candidate
	srcErr error
	sender *testSender
	notify bool
	diskOK bool
	e      *Engine
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t: t, dir: t.TempDir(), now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC),
		sender: &testSender{channels: []string{"fake"}}, notify: true, diskOK: true,
	}
	h.open()
	return h
}

func (h *harness) options() Options {
	return Options{
		Dir: h.dir, Key: h.key, Interval: testInterval, Host: "host1",
		Now:    func() time.Time { return h.now },
		Sender: h.sender,
		Notify: func() bool { return h.notify },
		Sources: []Source{{Name: "planted", Collect: func(context.Context, time.Time) ([]Candidate, error) {
			if h.srcErr != nil {
				return nil, h.srcErr
			}
			return append([]Candidate(nil), h.cands...), nil
		}}},
		writeFile: func(path string, data []byte, key []byte) error {
			if !h.diskOK {
				return errors.New("no space left on device")
			}
			return writeRecordFile(path, data, key)
		},
	}
}

// open starts a new engine over the same records, as a daemon restart does.
func (h *harness) open() {
	h.t.Helper()
	e, err := New(h.options())
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	h.e = e
}

func (h *harness) pass() Report {
	h.t.Helper()
	r, err := h.e.Pass(context.Background())
	if err != nil {
		h.t.Fatalf("Pass: %v", err)
	}
	return r
}

// run advances the clock by d one interval at a time, passing each time.
func (h *harness) run(d time.Duration) Report {
	h.t.Helper()
	var r Report
	for gone := time.Duration(0); gone < d; gone += testInterval {
		h.now = h.now.Add(testInterval)
		r = h.pass()
	}
	return r
}

// next is one interval later.
func (h *harness) next() Report { return h.run(testInterval) }

func planted(id, state string, since time.Time, limit time.Duration) Candidate {
	return Candidate{Kind: KindAutomaton, ID: id, State: state, Since: since, Limit: limit, Reason: "awaiting_you", WaitsOn: WaitsOnOperator}
}

func failedWork(id string, at time.Time) Candidate {
	return Candidate{Kind: KindSchedule, ID: id, State: "failed", Since: at, Problem: "failed", Invariant: InvFailed, Sticky: true, WaitsOn: WaitsOnOperator}
}

func findEntry(es []Entry, id string) (Entry, bool) {
	for _, e := range es {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func open(r Report) []Entry { return append(append([]Entry(nil), r.Items...), r.Backlog...) }

func hasRed(r Report, why string) bool {
	for _, w := range r.Red {
		if w == why {
			return true
		}
	}
	return false
}

func readHeartbeat(t *testing.T, dir string) (Heartbeat, bool) {
	t.Helper()
	hb, err := ReadHeartbeat(filepath.Join(dir, HeartbeatFile))
	if err != nil {
		return Heartbeat{}, false
	}
	return hb, true
}

func TestFirstPassMakesOneBacklogAndSendsNothing(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 1000; i++ {
		h.cands = append(h.cands, failedWork(fmt.Sprintf("old%04d", i), h.now.Add(-72*time.Hour)))
	}
	r := h.pass()
	r = h.run(time.Hour)
	if len(r.Backlog) != 1000 || len(r.Items) != 0 {
		t.Fatalf("backlog=%d items=%d, want 1000 and 0", len(r.Backlog), len(r.Items))
	}
	if h.sender.count() != 0 {
		t.Fatalf("%d sends for the backlog, want 0", h.sender.count())
	}
	if r.Result != ResultRed || r.StartMark == nil {
		t.Fatalf("result=%s startMark=%v, want red with a start mark", r.Result, r.StartMark)
	}

	// One occurrence after the start mark is listed once.
	h.cands = append(h.cands, failedWork("later", h.now))
	r = h.next()
	r = h.next()
	if _, ok := findEntry(r.Items, "later"); !ok || len(r.Items) != 1 {
		t.Fatalf("items=%v, want only the later one", r.Items)
	}
	if h.sender.count() != 1 {
		t.Fatalf("sends=%d, want 1", h.sender.count())
	}
}

func TestFiveHundredNewOccurrencesStopAtFifty(t *testing.T) {
	h := newHarness(t)
	h.pass()
	for i := 0; i < 500; i++ {
		h.cands = append(h.cands, failedWork(fmt.Sprintf("n%04d", i), h.now))
	}
	r := h.next()
	r = h.next()
	listed := 0
	for _, e := range r.Items {
		if e.Kind == KindSchedule {
			listed++
		}
	}
	alarm, ok := findEntry(r.Items, "flood")
	if listed != 50 || !ok || r.Unrecorded != 450 || !hasRed(r, RedUnrecorded) {
		t.Fatalf("listed=%d alarm=%v unrecorded=%d red=%v", listed, ok, r.Unrecorded, r.Red)
	}
	if n, err := h.e.Ack(Selector{Keys: []string{alarm.Key}}, "operator"); err != nil || n != 1 {
		t.Fatalf("ack alarm: %d %v", n, err)
	}
	r = h.next()
	listed = 0
	for _, e := range r.Items {
		if e.Kind == KindSchedule {
			listed++
		}
	}
	if listed != 100 {
		t.Fatalf("after the alarm was acknowledged listed=%d, want 100", listed)
	}
}

func TestOverLimitIsListedAtTheFirstPassAfterTheLimit(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.cands = []Candidate{planted("a1", "planning", h.now, LimitPlanning)}
	r := h.run(LimitPlanning)
	if _, ok := findEntry(open(r), "a1"); ok {
		t.Fatalf("listed before the limit")
	}
	r = h.next()
	e, ok := findEntry(r.Items, "a1")
	if !ok || e.Invariant != InvOverdue || e.Reason != "over_limit" {
		t.Fatalf("entry=%+v ok=%v, want an I2 over_limit item", e, ok)
	}
}

func TestSendsAllFailedTurnsRedAndTheHeartbeatCannotTell(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.sender.setFail(true)
	h.cands = []Candidate{failedWork("s1", h.now)}
	r := h.next()
	if !hasRed(r, RedSendsFail) || r.Heartbeat != HeartbeatCannotTell {
		t.Fatalf("red=%v heartbeat=%s, want all_sends_failed and cannot_tell", r.Red, r.Heartbeat)
	}
	hb, ok := readHeartbeat(t, h.dir)
	if !ok || hb.State != HeartbeatCannotTell {
		t.Fatalf("heartbeat file=%+v ok=%v", hb, ok)
	}
	e, _ := findEntry(r.Items, "s1")
	if e.ToldAt != nil || len(e.Sends) == 0 || e.Sends[0].OK {
		t.Fatalf("entry=%+v, want a failed send recorded and not told", e)
	}
	for _, s := range e.Sends {
		if strings.Contains(s.Code, "hunter2") {
			t.Fatalf("the channel's error text was kept: %q", s.Code)
		}
	}
	h.sender.setFail(false)
	r = h.run(2 * time.Hour)
	e, _ = findEntry(r.Items, "s1")
	if e.ToldAt == nil || r.Heartbeat != HeartbeatRed || hasRed(r, RedSendsFail) {
		t.Fatalf("after the channel returned: told=%v heartbeat=%s red=%v", e.ToldAt, r.Heartbeat, r.Red)
	}
}

func TestNoChannelOrNotifyOffCannotTell(t *testing.T) {
	for name, change := range map[string]func(*harness){
		"no channel": func(h *harness) { h.sender.channels = nil },
		"notify off": func(h *harness) { h.notify = false },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			change(h)
			r := h.pass()
			if !hasRed(r, RedNoChannel) || r.Result != ResultRed || r.Heartbeat != HeartbeatCannotTell {
				t.Fatalf("result=%s red=%v heartbeat=%s", r.Result, r.Red, r.Heartbeat)
			}
		})
	}
}

func TestGreenCheckWritesAnOKHeartbeat(t *testing.T) {
	h := newHarness(t)
	r := h.pass()
	if r.Result != ResultGreen || r.Heartbeat != HeartbeatOK || !r.ReportOnly {
		t.Fatalf("report=%+v", r)
	}
	hb, ok := readHeartbeat(t, h.dir)
	if !ok || hb.State != HeartbeatOK || hb.Unix != h.now.Unix() || hb.IntervalSeconds != 300 {
		t.Fatalf("heartbeat=%+v ok=%v", hb, ok)
	}
	if len(r.Uncovered) == 0 {
		t.Fatal("a green report must say what is not covered")
	}
}

func TestSixHundredLaterAlertsRemoveNothing(t *testing.T) {
	h := newHarness(t)
	h.pass()
	if err := h.e.Record(Candidate{Kind: KindFilter, ID: "f1@host1-ab12", State: "send_failed", Since: h.now, Problem: "send_failed", Invariant: InvFailed, Sticky: true}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	store, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		store.Add(alerts.LevelWarn, "later", fmt.Sprintf("alert %d", i), "")
	}
	h.next()
	h.open() // a restart
	r := h.next()
	if _, ok := findEntry(r.Items, "f1@host1-ab12"); !ok {
		t.Fatalf("the failed send left the list: %v", r.Items)
	}
}

func TestFailedWorkStaysListedAfterItsRecordIsGone(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.cands = []Candidate{failedWork("s1", h.now)}
	h.next()
	h.cands = nil
	r := h.run(time.Hour)
	if _, ok := findEntry(r.Items, "s1"); !ok {
		t.Fatal("failed work left the list without an operator action")
	}
	if n, err := h.e.Ack(Selector{Keys: []string{"schedule:s1:failed"}}, "operator"); err != nil || n != 1 {
		t.Fatalf("ack: %d %v", n, err)
	}
	r = h.next()
	if _, ok := findEntry(r.Items, "s1"); !ok {
		t.Fatal("seen is not dropped: it stays listed")
	}
	if n, err := h.e.Drop(Selector{Keys: []string{"schedule:s1:failed"}}, "not-needed", "operator"); err != nil || n != 1 {
		t.Fatalf("drop: %d %v", n, err)
	}
	if r = h.next(); r.Result != ResultGreen {
		t.Fatalf("after the drop: %s %v %v", r.Result, r.Red, r.Items)
	}
}

func TestDroppedWorkIsNotListedAgainWhileItsRecordStays(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.cands = []Candidate{failedWork("s1", h.now)}
	h.next()
	if _, err := h.e.Drop(Selector{Keys: []string{"schedule:s1:failed"}}, "not-needed", "operator"); err != nil {
		t.Fatal(err)
	}
	h.open()
	if r := h.run(time.Hour); len(open(r)) != 0 {
		t.Fatalf("listed again: %v", open(r))
	}
}

func TestConditionTrueFalseTrueGivesTwoEpisodes(t *testing.T) {
	h := newHarness(t)
	h.cands = []Candidate{planted("a1", "blocked", h.now.Add(-48*time.Hour), LimitWaiting)}
	r := h.pass()
	first, _ := findEntry(r.Backlog, "a1")
	if first.Episode != 1 {
		t.Fatalf("first=%+v", first)
	}
	h.cands = nil
	if r = h.next(); len(open(r)) != 0 {
		t.Fatalf("a resolved state entry stays listed: %v", open(r))
	}
	h.cands = []Candidate{{Kind: KindAutomaton, ID: "a1", State: "blocked", Since: h.now, Problem: "no_reason"}}
	r = h.next()
	second, ok := findEntry(r.Items, "a1")
	if !ok || second.Episode != 2 || second.Backlog {
		t.Fatalf("second=%+v ok=%v, want a new episode outside the backlog", second, ok)
	}
}

func TestClockJumpAgesAtMostTwoIntervals(t *testing.T) {
	h := newHarness(t)
	h.pass()
	for i := 0; i < 30; i++ {
		h.cands = append(h.cands, planted(fmt.Sprintf("a%02d", i), "needs_review", h.now, LimitWaiting))
	}
	h.next()
	h.now = h.now.Add(72 * time.Hour) // a suspend or a clock jump
	r := h.pass()
	for _, e := range open(r) {
		if e.Kind == KindAutomaton {
			t.Fatalf("a forward jump made %s overdue", e.ID)
		}
	}
	jumps := 0
	for _, e := range r.Items {
		if e.Kind == KindClock {
			jumps++
		}
	}
	if jumps != 1 {
		t.Fatalf("jumps listed=%d, want 1", jumps)
	}
	r = h.next()
	n := 0
	for _, e := range r.Items {
		if e.Kind == KindClock {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the jump was listed again: %d", n)
	}
}

func TestBackwardJumpMakesNothingYoung(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.cands = []Candidate{planted("a1", "planning", h.now, LimitPlanning)}
	h.run(LimitPlanning + testInterval)
	h.now = h.now.Add(-48 * time.Hour)
	r := h.pass()
	if _, ok := findEntry(r.Items, "a1"); !ok {
		t.Fatal("a backward jump removed an overdue item")
	}
}

func TestMoreThanTwentyExpiringAtOnceExpiresNone(t *testing.T) {
	h := newHarness(t)
	h.pass()
	for i := 0; i < 21; i++ {
		h.cands = append(h.cands, planted(fmt.Sprintf("a%02d", i), "planning", h.now, LimitPlanning))
	}
	r := h.run(LimitPlanning + 3*testInterval)
	n := 0
	for _, e := range open(r) {
		if e.Kind == KindAutomaton {
			n++
		}
	}
	alarm, ok := findEntry(r.Items, "mass_expiry")
	if n != 0 || !ok || len(r.Guarded) != 21 || !hasRed(r, RedGuarded) {
		t.Fatalf("expired=%d alarm=%v guarded=%d red=%v", n, ok, len(r.Guarded), r.Red)
	}
	if _, err := h.e.Ack(Selector{Keys: []string{alarm.Key}}, "operator"); err != nil {
		t.Fatal(err)
	}
	r = h.next()
	n = 0
	for _, e := range r.Items {
		if e.Kind == KindAutomaton {
			n++
		}
	}
	if n != 21 || len(r.Guarded) != 0 {
		t.Fatalf("after the alarm was acknowledged listed=%d guarded=%d", n, len(r.Guarded))
	}
}

func TestFullDiskTypesNothingSendsOneWarningAndLetsTheHeartbeatGoStale(t *testing.T) {
	h := newHarness(t)
	h.pass()
	before, _ := readHeartbeat(t, h.dir)
	h.diskOK = false
	h.cands = []Candidate{failedWork("s1", h.now)}
	for i := 0; i < 4; i++ {
		h.now = h.now.Add(testInterval)
		r, err := h.e.Pass(context.Background())
		if !errors.Is(err, ErrCannotRecord) || !hasRed(r, RedCannotRecord) {
			t.Fatalf("pass on a full disk: err=%v red=%v", err, r.Red)
		}
	}
	after, _ := readHeartbeat(t, h.dir)
	if after.Unix != before.Unix {
		t.Fatalf("the heartbeat moved on a full disk: %d -> %d", before.Unix, after.Unix)
	}
	if h.sender.count() != 1 || h.sender.sent[0] != CannotRecordText("host1") {
		t.Fatalf("sends=%d %q, want one warning from memory", h.sender.count(), h.sender.sent)
	}
	if _, err := h.e.Ack(Selector{Kind: KindSchedule}, "operator"); err == nil {
		t.Fatal("an action that cannot be saved must fail")
	}
	// The source was not consumed: once the disk is back it is listed.
	h.diskOK = true
	r := h.next()
	if _, ok := findEntry(r.Items, "s1"); !ok {
		t.Fatalf("lost: %v", r.Items)
	}
}

func TestDiskTestFailureAloneStalesTheHeartbeat(t *testing.T) {
	h := newHarness(t)
	h.pass()
	before, _ := readHeartbeat(t, h.dir)
	opts := h.options()
	opts.writeFile = func(path string, data []byte, key []byte) error {
		if strings.HasSuffix(path, diskTestFile) {
			return errors.New("input/output error")
		}
		return writeRecordFile(path, data, key)
	}
	e, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(testInterval)
	if _, err := e.Pass(context.Background()); !errors.Is(err, ErrCannotRecord) {
		t.Fatalf("err=%v", err)
	}
	after, _ := readHeartbeat(t, h.dir)
	if after.Unix != before.Unix {
		t.Fatal("the heartbeat was written although the disk test failed")
	}
}

func TestMissingStartMarkDoesNothingAndIsRed(t *testing.T) {
	for name, damage := range map[string]func(path string) error{
		"missing":    os.Remove,
		"unreadable": func(p string) error { return os.WriteFile(p, []byte("not a record"), 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.pass()
			if err := damage(filepath.Join(h.dir, startMarkFile)); err != nil {
				t.Fatal(err)
			}
			state, _ := os.ReadFile(filepath.Join(h.dir, stateFileName))
			before, _ := readHeartbeat(t, h.dir)
			h.cands = []Candidate{failedWork("s1", h.now)}
			h.open()
			h.now = h.now.Add(testInterval)
			r, _ := h.e.Pass(context.Background())
			if !hasRed(r, RedStartMark) || r.Result != ResultRed || len(open(r)) != 0 {
				t.Fatalf("result=%s red=%v open=%v", r.Result, r.Red, open(r))
			}
			now, _ := os.ReadFile(filepath.Join(h.dir, stateFileName))
			if string(now) != string(state) {
				t.Fatal("records changed without a start mark")
			}
			after, _ := readHeartbeat(t, h.dir)
			if after.Unix != before.Unix {
				t.Fatal("heartbeat written without a start mark")
			}
			for _, s := range h.sender.sent {
				if strings.Contains(s, "s1") {
					t.Fatalf("an item was sent: %q", s)
				}
			}
		})
	}
}

func TestUnopenableRecordIsRedAndNotOverwritten(t *testing.T) {
	h := newHarness(t)
	h.cands = []Candidate{failedWork("s1", h.now)}
	h.pass()
	path := filepath.Join(h.dir, stateFileName)
	if err := os.WriteFile(path, []byte("DWDAT2\nnot-base64!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.open()
	h.now = h.now.Add(testInterval)
	r, _ := h.e.Pass(context.Background())
	if !hasRed(r, RedRecords) || r.ByRule[InvUnopenable] == 0 || r.Result != ResultRed {
		t.Fatalf("red=%v rules=%v", r.Red, r.ByRule)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "DWDAT2\nnot-base64!\n" {
		t.Fatalf("the unreadable record was overwritten: %q", got)
	}
	if _, err := h.e.Ack(Selector{Kind: KindSchedule}, "operator"); !errors.Is(err, ErrRecords) {
		t.Fatalf("ack over unreadable records: %v", err)
	}
}

func TestUnreadableSourceIsRedAndKeepsItsEntries(t *testing.T) {
	h := newHarness(t)
	h.cands = []Candidate{planted("a1", "blocked", h.now.Add(-48*time.Hour), LimitWaiting)}
	h.pass()
	h.srcErr = errors.New("store unreadable")
	r := h.run(time.Hour)
	if !hasRed(r, RedSourceUnreadable) || r.ByRule[InvUnopenable] == 0 {
		t.Fatalf("red=%v rules=%v", r.Red, r.ByRule)
	}
	if _, ok := findEntry(r.Backlog, "a1"); !ok {
		t.Fatal("an unreadable source emptied the list")
	}
}

func TestSourceFirstReadLaterJoinsTheBacklog(t *testing.T) {
	h := newHarness(t)
	h.cands = []Candidate{failedWork("s1", h.now.Add(-time.Hour))}
	h.srcErr = errors.New("not ready")
	h.pass()
	h.srcErr = nil
	r := h.next()
	if _, ok := findEntry(r.Backlog, "s1"); !ok || h.sender.count() != 0 {
		t.Fatalf("backlog=%v sends=%d", r.Backlog, h.sender.count())
	}
}

// Every rule: plant the violation, the check turns red; remove it, green.
func TestEachRuleTurnsRedOnAPlantedFaultAndGreenWithout(t *testing.T) {
	type fault struct {
		plant  func(h *harness)
		remove func(h *harness)
		wait   time.Duration
	}
	drop := func(h *harness) {
		h.cands = nil
		_, _ = h.e.Drop(Selector{Kind: KindSchedule}, "test", "operator")
	}
	clear := func(h *harness) { h.cands = nil }
	cand := func(c Candidate) func(*harness) {
		return func(h *harness) {
			c.Since = h.now
			if c.Invariant == InvDeadlines {
				c.Since = time.Time{}
			}
			h.cands = []Candidate{c}
		}
	}
	faults := map[string]fault{
		InvDeadlines: {plant: func(h *harness) {
			h.cands = AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: "needs_review"}}, h.now, testInterval)
		}, remove: clear},
		InvOverdue:      {plant: cand(planted("a1", "planning", time.Time{}, LimitPlanning)), wait: LimitPlanning + testInterval, remove: clear},
		InvFailed:       {plant: cand(failedWork("s1", time.Time{})), remove: drop},
		InvScheduleFire: {plant: cand(Candidate{Kind: KindSchedule, ID: "s1", State: "target_gone", Problem: "target_gone", Invariant: InvScheduleFire}), remove: clear},
		InvNoHolder:     {plant: cand(Candidate{Kind: KindSchedule, ID: "s1", State: "no_holder", Limit: LimitScheduleWait, Invariant: InvNoHolder}), wait: LimitScheduleWait + testInterval, remove: clear},
		InvRunning: {plant: func(h *harness) {
			h.cands = AutomatonCandidates([]AutomatonFact{{ID: "a1", Status: "running", Since: h.now}}, h.now, testInterval)
		}, wait: 3 * testInterval, remove: clear},
	}
	for rule, f := range faults {
		t.Run(rule, func(t *testing.T) {
			h := newHarness(t)
			if r := h.pass(); r.Result != ResultGreen {
				t.Fatalf("before: %s %v", r.Result, r.Red)
			}
			f.plant(h)
			r := h.next()
			if f.wait > 0 {
				r = h.run(f.wait)
			}
			if r.Result != ResultRed || r.ByRule[rule] == 0 {
				t.Fatalf("planted %s: result=%s rules=%v items=%v", rule, r.Result, r.ByRule, r.Items)
			}
			f.remove(h)
			if r = h.next(); r.Result != ResultGreen {
				t.Fatalf("removed %s: %s %v %v", rule, r.Result, r.Red, open(r))
			}
		})
	}
	t.Run(InvCheckRan, func(t *testing.T) {
		h := newHarness(t)
		h.pass()
		h.now = h.now.Add(3 * testInterval)
		r := h.e.Report()
		if !hasRed(r, RedStale) || r.ByRule[InvCheckRan] == 0 {
			t.Fatalf("no pass for three intervals: %v %v", r.Red, r.ByRule)
		}
		if r = h.pass(); hasRed(r, RedStale) {
			t.Fatalf("after a pass: %v", r.Red)
		}
	})
	t.Run(InvCheckRan+" never ran", func(t *testing.T) {
		h := newHarness(t)
		if r := h.e.Report(); r.Result != ResultRed || !hasRed(r, RedNeverRan) {
			t.Fatalf("a check that never ran is not green: %s %v", r.Result, r.Red)
		}
	})
	t.Run(InvTold, func(t *testing.T) {
		h := newHarness(t)
		h.pass()
		h.sender.setFail(true)
		h.cands = []Candidate{failedWork("s1", h.now)}
		r := h.run(30 * time.Minute)
		if !hasRed(r, RedNotTold) || r.ByRule[InvTold] == 0 {
			t.Fatalf("an old unsent item: %v %v", r.Red, r.ByRule)
		}
		if _, err := h.e.Ack(Selector{Keys: []string{"schedule:s1:failed"}}, "operator"); err != nil {
			t.Fatal(err)
		}
		if r = h.next(); hasRed(r, RedNotTold) || r.ByRule[InvTold] != 0 {
			t.Fatalf("after the acknowledgement: %v %v", r.Red, r.ByRule)
		}
	})
	t.Run(InvUnopenable, func(t *testing.T) {
		h := newHarness(t)
		h.pass()
		h.srcErr = errors.New("sealed with another key")
		if r := h.next(); r.ByRule[InvUnopenable] == 0 || r.Result != ResultRed {
			t.Fatalf("planted: %v %v", r.Red, r.ByRule)
		}
		h.srcErr = nil
		if r := h.next(); r.Result != ResultGreen {
			t.Fatalf("removed: %v %v", r.Red, r.ByRule)
		}
	})
}

func TestAcceptAGroupWithOneReasonAndDate(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		h.cands = append(h.cands, planted(fmt.Sprintf("a%d", i), "blocked", h.now.Add(-48*time.Hour), LimitWaiting))
	}
	h.pass()
	until := h.now.Add(48 * time.Hour)
	for _, bad := range []struct {
		sel    Selector
		reason string
		until  time.Time
		want   error
	}{
		{Selector{}, "later", until, ErrSelector},
		{Selector{Backlog: true}, "Waiting for the next release!", until, ErrReasonCode},
		{Selector{Backlog: true}, "later", h.now.Add(31 * 24 * time.Hour), ErrUntil},
		{Selector{Backlog: true}, "later", h.now.Add(-time.Hour), ErrUntil},
		{Selector{Keys: []string{"automaton:nope:blocked"}}, "later", until, ErrNoMatch},
	} {
		if _, err := h.e.Accept(bad.sel, bad.reason, bad.until, "operator"); !errors.Is(err, bad.want) {
			t.Fatalf("accept %+v %q: %v, want %v", bad.sel, bad.reason, err, bad.want)
		}
	}
	if _, err := h.e.Drop(Selector{Keys: []string{"nothing"}}, "x", "operator"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("drop nothing: %v", err)
	}
	n, err := h.e.Accept(Selector{Backlog: true, Kind: KindAutomaton}, "waits-for-review", until, "session:ab12")
	if err != nil || n != 3 {
		t.Fatalf("accept: %d %v", n, err)
	}
	r := h.next()
	if r.Result != ResultGreen || len(r.Accepted) != 3 || r.NonOperatorActions != 1 {
		t.Fatalf("result=%s red=%v accepted=%d nonOperator=%d", r.Result, r.Red, len(r.Accepted), r.NonOperatorActions)
	}
	if r.Accepted[0].Accepted.By != "session:ab12" {
		t.Fatalf("who accepted is not recorded: %+v", r.Accepted[0].Accepted)
	}
	h.now = until.Add(-testInterval)
	h.pass()
	r = h.run(3 * testInterval)
	if r.Result != ResultRed || len(r.Items) < 3 {
		t.Fatalf("after the date: %s items=%d", r.Result, len(r.Items))
	}
	if h.sender.count() == 0 {
		t.Fatal("an acceptance that ran out is told again")
	}
}

func TestDropRefusesStuckWorkThatIsNotFailedOrBacklog(t *testing.T) {
	h := newHarness(t)
	h.pass()
	h.cands = []Candidate{{Kind: KindAutomaton, ID: "a1", State: "blocked", Since: h.now, Problem: "no_reason"}, failedWork("s1", h.now)}
	h.next()
	if _, err := h.e.Drop(Selector{Keys: []string{"automaton:a1:blocked", "schedule:s1:failed"}}, "x", "operator"); !errors.Is(err, ErrNotDroppable) {
		t.Fatalf("err=%v", err)
	}
	if r := h.next(); len(r.Items) != 2 {
		t.Fatalf("a refused drop must change nothing: %v", r.Items)
	}
}

func TestRecordsAreSealedAndHoldNoPlainID(t *testing.T) {
	h := newHarness(t)
	h.key = []byte("0123456789abcdef0123456789abcdef")
	h.open()
	h.cands = []Candidate{failedWork("markerid42", h.now)}
	h.pass()
	for _, name := range []string{stateFileName, startMarkFile} {
		data, err := os.ReadFile(filepath.Join(h.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(data), "DWDAT2\n") || strings.Contains(string(data), "markerid42") {
			t.Fatalf("%s is not sealed: %.60q", name, data)
		}
	}
	// The heartbeat is read by the timer: plain, and with no ID in it.
	data, _ := os.ReadFile(filepath.Join(h.dir, HeartbeatFile))
	if !strings.Contains(string(data), `"state"`) || strings.Contains(string(data), "markerid42") {
		t.Fatalf("heartbeat: %q", data)
	}
	h.key = []byte("ffffffffffffffffffffffffffffffff")
	h.open()
	if r := h.e.Report(); !hasRed(r, RedRecords) {
		t.Fatalf("another key: %v", r.Red)
	}
}

func TestNewWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "delivery")
	if _, err := New(Options{Dir: dir, Interval: testInterval}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("New wrote under %s: %v", dir, err)
	}
}
