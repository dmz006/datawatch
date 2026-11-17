package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAlertClock is a settable clock for the daily check.
type fakeAlertClock struct{ t time.Time }

func (c *fakeAlertClock) now() time.Time { return c.t }

// unlandedAlertFixture builds an alerter on a temp folder with a fake clock,
// a fake branch list and a recorded alert sink.
type unlandedAlertFixture struct {
	dir      string
	clock    *fakeAlertClock
	branches []UnlandedBranch
	listErr  error
	lists    int
	alerts   []string
}

func newUnlandedAlertFixture(t *testing.T, start time.Time) *unlandedAlertFixture {
	t.Helper()
	return &unlandedAlertFixture{dir: t.TempDir(), clock: &fakeAlertClock{t: start}}
}

func (f *unlandedAlertFixture) alerter() *unlandedAlerter {
	a := newUnlandedAlerter(f.dir)
	a.now = f.clock.now
	return a
}

func (f *unlandedAlertFixture) list(context.Context) ([]UnlandedBranch, error) {
	f.lists++
	return f.branches, f.listErr
}

func (f *unlandedAlertFixture) alert(title, body string) {
	f.alerts = append(f.alerts, title+"\n"+body)
}

func (f *unlandedAlertFixture) check(a *unlandedAlerter, enabled bool, threshold int) UnlandedAlertResult {
	return a.check(context.Background(), enabled, threshold, f.list, f.alert)
}

// pendingBranches returns n superseded (pending) branches; branch i's last
// commit is (i+1) days before now, so the last one is the oldest.
func pendingBranches(now time.Time, n int) []UnlandedBranch {
	out := make([]UnlandedBranch, 0, n)
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		out = append(out, UnlandedBranch{
			AutomatonID: id, Branch: "automaton/" + id, Title: "Work " + id,
			Recommendation: UnlandedSuperseded, LastActivity: now.Add(-time.Duration(i+1) * 24 * time.Hour),
		})
	}
	return out
}

var alertDay1 = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)

func TestUnlandedAlertBelowThresholdRaisesNothing(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	a := f.alerter()
	a.checkEvery = 0
	for _, n := range []int{0, 2, 3} { // 3 equals the threshold: still nothing
		f.branches = pendingBranches(alertDay1, n)
		// Branches in other states never count toward the threshold.
		f.branches = append(f.branches,
			UnlandedBranch{Branch: "automaton/x", Recommendation: UnlandedNeedsDecision, LastActivity: alertDay1.Add(-90 * 24 * time.Hour)},
			UnlandedBranch{Branch: "automaton/y", Recommendation: UnlandedLanded, LastActivity: alertDay1.Add(-90 * 24 * time.Hour)},
			UnlandedBranch{Branch: "automaton/z", Recommendation: "", LastActivity: alertDay1.Add(-90 * 24 * time.Hour)},
		)
		res := f.check(a, true, 3)
		if res.Outcome != UnlandedAlertAtOrBelow || res.Pending != n {
			t.Fatalf("n=%d: result = %+v, want at_or_below with pending %d", n, res, n)
		}
	}
	if len(f.alerts) != 0 {
		t.Fatalf("alerts = %v, want none", f.alerts)
	}
	if _, err := os.Stat(filepath.Join(f.dir, unlandedAlertStateFile)); !os.IsNotExist(err) {
		t.Fatalf("state file written without an alert (err=%v)", err)
	}
}

func TestUnlandedAlertAboveThresholdOncePerDay(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	f.branches = pendingBranches(alertDay1, 5)
	a := f.alerter()
	a.checkEvery = 0 // every tick re-checks; only the day record stops a repeat

	// Several ticks through the first day.
	for i, at := range []time.Duration{0, time.Minute, 2 * time.Hour, 8 * time.Hour, 14*time.Hour + 59*time.Minute} {
		f.clock.t = alertDay1.Add(at)
		res := f.check(a, true, 3)
		want := UnlandedAlertAlreadyToday
		if i == 0 {
			want = UnlandedAlertRaised
		}
		if res.Outcome != want {
			t.Fatalf("tick %d: outcome %q, want %q", i, res.Outcome, want)
		}
	}
	if len(f.alerts) != 1 {
		t.Fatalf("alerts on day 1 = %d, want 1: %v", len(f.alerts), f.alerts)
	}
	if f.lists != 1 {
		t.Fatalf("branch lists on day 1 = %d, want 1 (an alerted day must not list again)", f.lists)
	}
	got := f.alerts[0]
	for _, want := range []string{unlandedAlertTitle, "5 pending Automaton branches", "threshold 3", "automaton/e (Work e)", "5d 0h"} {
		if !strings.Contains(got, want) {
			t.Errorf("alert %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "PRD") {
		t.Errorf("alert uses PRD wording: %q", got)
	}

	// Next calendar day: exactly one more.
	day2 := alertDay1.Add(24 * time.Hour)
	for i, at := range []time.Duration{-8 * time.Hour, 0, time.Hour, 5 * time.Hour} {
		f.clock.t = day2.Add(at) // day2-8h is 01:00 on day 2
		f.check(a, true, 3)
		if i == 0 && len(f.alerts) != 2 {
			t.Fatalf("first tick of day 2 raised %d total alerts, want 2", len(f.alerts))
		}
	}
	if len(f.alerts) != 2 {
		t.Fatalf("alerts after day 2 = %d, want 2", len(f.alerts))
	}
}

func TestUnlandedAlertDisabledRaisesNothing(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	f.branches = pendingBranches(alertDay1, 10)
	a := f.alerter()
	a.checkEvery = 0
	for i := 0; i < 3; i++ {
		f.clock.t = alertDay1.Add(time.Duration(i) * 24 * time.Hour)
		if res := f.check(a, false, 3); res.Outcome != UnlandedAlertDisabled {
			t.Fatalf("outcome %q, want disabled", res.Outcome)
		}
	}
	if len(f.alerts) != 0 || f.lists != 0 {
		t.Fatalf("disabled check alerted %d times and listed %d times, want 0 and 0", len(f.alerts), f.lists)
	}
	// Turning it back on applies at once.
	if res := f.check(a, true, 3); res.Outcome != UnlandedAlertRaised {
		t.Fatalf("after enable: outcome %q, want raised", res.Outcome)
	}
}

func TestUnlandedAlertRestartSameDayNoRepeat(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	f.branches = pendingBranches(alertDay1, 4)
	first := f.alerter()
	if res := f.check(first, true, 3); res.Outcome != UnlandedAlertRaised {
		t.Fatalf("first run: outcome %q, want raised", res.Outcome)
	}

	// The persisted record names the day, the count and the branch.
	data, err := os.ReadFile(filepath.Join(f.dir, unlandedAlertStateFile))
	if err != nil {
		t.Fatal(err)
	}
	var st unlandedAlertState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st.LastAlertDay != "2026-11-15" || st.LastPending != 4 || st.LastBranch != "automaton/d" {
		t.Fatalf("persisted state = %+v", st)
	}

	// A restart later the same day: a new alerter on the same folder.
	f.clock.t = alertDay1.Add(10 * time.Hour)
	restarted := f.alerter()
	if res := f.check(restarted, true, 3); res.Outcome != UnlandedAlertAlreadyToday {
		t.Fatalf("after restart: outcome %q, want already_alerted_today", res.Outcome)
	}
	if len(f.alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(f.alerts))
	}
	// A restart on the next day alerts again.
	f.clock.t = alertDay1.Add(24 * time.Hour)
	if res := f.check(f.alerter(), true, 3); res.Outcome != UnlandedAlertRaised {
		t.Fatalf("next day after restart: outcome %q, want raised", res.Outcome)
	}
}

func TestUnlandedAlertCheckThrottleAndListFailure(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	f.branches = pendingBranches(alertDay1, 1)
	a := f.alerter() // default throttle of one hour

	f.check(a, true, 3)
	f.clock.t = alertDay1.Add(30 * time.Minute)
	if res := f.check(a, true, 3); res.Outcome != UnlandedAlertNotDue {
		t.Fatalf("within the hour: outcome %q, want not_due", res.Outcome)
	}
	if f.lists != 1 {
		t.Fatalf("lists = %d, want 1", f.lists)
	}
	// Past the hour the count has grown: the alert fires.
	f.clock.t = alertDay1.Add(61 * time.Minute)
	f.branches = pendingBranches(alertDay1, 4)
	if res := f.check(a, true, 3); res.Outcome != UnlandedAlertRaised {
		t.Fatalf("after the hour: outcome %q, want raised", res.Outcome)
	}

	// A failed listing raises nothing and records no day.
	g := newUnlandedAlertFixture(t, alertDay1)
	g.listErr = errors.New("git broke")
	ga := g.alerter()
	if res := g.check(ga, true, 0); res.Outcome != UnlandedAlertListFailed {
		t.Fatalf("outcome %q, want list_failed", res.Outcome)
	}
	if len(g.alerts) != 0 || ga.lastAlertDay() != "" {
		t.Fatalf("list failure alerted or recorded a day")
	}
}

func TestUnlandedAlertZeroThresholdAndUnknownAge(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	// One pending branch with no last-commit time: still named.
	f.branches = []UnlandedBranch{{AutomatonID: "q1", Branch: "automaton/q1", Recommendation: UnlandedSuperseded}}
	a := f.alerter()
	res := f.check(a, true, 0)
	if res.Outcome != UnlandedAlertRaised || res.Oldest == nil || res.Oldest.Branch != "automaton/q1" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(f.alerts[0], "1 pending Automaton branch wait") || !strings.Contains(f.alerts[0], "automaton/q1 (q1)") ||
		!strings.Contains(f.alerts[0], unlandedAlertUnknownAgeText) {
		t.Fatalf("alert = %q", f.alerts[0])
	}
}

func TestUnlandedAlertCorruptStateStartsClean(t *testing.T) {
	f := newUnlandedAlertFixture(t, alertDay1)
	if err := os.WriteFile(filepath.Join(f.dir, unlandedAlertStateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.branches = pendingBranches(alertDay1, 4)
	if res := f.check(f.alerter(), true, 3); res.Outcome != UnlandedAlertRaised {
		t.Fatalf("outcome %q, want raised", res.Outcome)
	}
}

func TestValidateUnlandedAlertPatch(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{}`, true},
		{`{"unlanded_alert_enabled": true, "unlanded_alert_threshold": 0}`, true},
		{`{"unlanded_alert_enabled": null, "unlanded_alert_threshold": null}`, true},
		{`{"unlanded_alert_threshold": 7}`, true},
		{`{"unlanded_alert_enabled": "yes"}`, false},
		{`{"unlanded_alert_threshold": -1}`, false},
		{`{"unlanded_alert_threshold": 2.5}`, false},
		{`{"unlanded_alert_threshold": "3"}`, false},
	} {
		err := validateUnlandedAlertPatch(json.RawMessage(tc.body))
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.body, err, tc.ok)
		}
	}
}

// TestManagerUnlandedAlertConfig covers the Manager path: the settings in
// the live Config (defaults when unset, a SetConfig change applied at once)
// and the alert sink and audit record.
func TestManagerUnlandedAlertConfig(t *testing.T) {
	m, err := NewManager(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeAlertClock{t: alertDay1}
	m.unlandedAlert.now = clock.now
	m.unlandedAlert.checkEvery = 0
	branches := pendingBranches(alertDay1, 3)
	m.unlandedAlertList = func(context.Context) ([]UnlandedBranch, error) { return branches, nil }
	var alerts []string
	m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, level+"|"+title+"|"+body)
		return "id"
	})
	var audits []string
	m.SetAuditFn(func(action string, details map[string]any) { audits = append(audits, action) })
	api := &API{M: m}

	// Defaults: on, threshold 3. Three pending is at the threshold.
	if res := m.UnlandedAlertCheck(context.Background()); res.Outcome != UnlandedAlertAtOrBelow || res.Threshold != 3 {
		t.Fatalf("default: %+v", res)
	}
	// Turn it off through the autonomous config set path: nothing.
	if err := api.SetConfig(json.RawMessage(`{"unlanded_alert_enabled": false, "unlanded_alert_threshold": 2}`)); err != nil {
		t.Fatal(err)
	}
	if res := m.UnlandedAlertCheck(context.Background()); res.Outcome != UnlandedAlertDisabled {
		t.Fatalf("disabled: %+v", res)
	}
	// Back on with threshold 2: three pending is above it.
	if err := api.SetConfig(json.RawMessage(`{"unlanded_alert_enabled": true}`)); err != nil {
		t.Fatal(err)
	}
	if res := m.UnlandedAlertCheck(context.Background()); res.Outcome != UnlandedAlertRaised || res.Threshold != 2 {
		t.Fatalf("enabled: %+v", res)
	}
	if len(alerts) != 1 || !strings.HasPrefix(alerts[0], "warn|"+unlandedAlertTitle+"|3 pending Automaton branches") {
		t.Fatalf("alerts = %v", alerts)
	}
	if len(audits) != 1 || audits[0] != "autonomous_unlanded_alert" {
		t.Fatalf("audits = %v", audits)
	}
	// A bad value is refused and leaves the live setting alone.
	if err := api.SetConfig(json.RawMessage(`{"unlanded_alert_threshold": -4}`)); err == nil {
		t.Fatal("negative threshold accepted")
	}
	if got := m.Config().UnlandedAlertThreshold; got == nil || *got != 2 {
		t.Fatalf("threshold after refused write = %v, want 2", got)
	}

	// The goroutine wrapper runs the same check; a hand-built Manager is a no-op.
	m.startUnlandedAlertCheck(context.Background())
	(&Manager{}).startUnlandedAlertCheck(context.Background())
	if res := (&Manager{}).UnlandedAlertCheck(context.Background()); res.Outcome != UnlandedAlertDisabled {
		t.Fatalf("hand-built manager: %+v", res)
	}
	deadline := time.Now().Add(2 * time.Second)
	for m.unlandedAlert.running.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(alerts) != 1 {
		t.Fatalf("background check repeated today's alert: %v", alerts)
	}
}
