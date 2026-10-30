// The repo CI watch alert, verified at the gate and the sink: it fires
// once per episode after alert_minutes, never in shadow mode, and never
// when alert_minutes is 0 (disabled).
package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newAlertHarness wires a capture on the Coordinator's alert sink so
// the test observes exactly what the operator would receive.
func newAlertHarness(t *testing.T, repo string, cfg RepoCIWatchConfig) (*Manager, *Coordinator, *alertSinkCapture) {
	t.Helper()
	m, c, ac := newActionHarness(t, repo, cfg)
	cap := &alertSinkCapture{}
	c.alertFn = cap.fn()
	_ = ac
	return m, c, cap
}

// oneFailingRun points the run list at a single failing infra run so
// the pass records a failing episode without any write (infra budget
// unset in DefaultConfig means no rerun).
func oneFailingRun(m *Manager) {
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if sha != fakeHeadSHA {
			return []RepoCIWatchRun{}, nil
		}
		return []RepoCIWatchRun{finished(fakeRun(300, "CI"), "cancelled")}, nil
	}
	base := m.Config()
	base.RetryPolicy = map[string]int{"infra": 0} // no rerun: this test is about the alert
	m.SetConfig(base)
}

func findRecordByRun(t *testing.T, m *Manager, runID string) *RepoCIWatchRecord {
	t.Helper()
	for _, r := range m.ListRepoCIWatchRecords() {
		if r.RunID == runID {
			return r
		}
	}
	return nil
}

// TestRepoCIWatchAlertFiresAfterThreshold verifies the alert fires once
// an episode has been unresolved since first_seen_at past
// alert_minutes — not before, not after the threshold is crossed.
func TestRepoCIWatchAlertFiresAfterThreshold(t *testing.T) {
	dir := t.TempDir()
	m, c, cap := newAlertHarness(t, dir, RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60,
		AlertMinutes: ptrInt(30),
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	oneFailingRun(m)

	c.repoCIWatchPass()
	if got := len(cap.alerts); got != 0 {
		t.Errorf("immediately after the failed run first appeared: alerts = %d, want 0 (first_seen_at is now, the threshold has not elapsed)", got)
	}

	nc.advance(31 * time.Minute) // over alert_minutes
	c.repoCIWatchPass()
	if got := len(cap.alerts); got != 1 {
		t.Fatalf("past alert_minutes: alerts = %d, want 1", got)
	}
	if !strings.Contains(cap.body, "Automaton") {
		t.Error("the alert body must name the Automaton watch, the operator's product")
	}
	if strings.Contains(cap.body, "PRD") {
		t.Error("the alert body must never say PRD — that is the internal record type")
	}
	if rec := findRecordByRun(t, m, "300"); rec == nil || rec.AlertedAt == nil {
		t.Errorf("the record must carry the alert stamp (got %+v)", rec)
	}
}

// TestRepoCIWatchAlertOncePerEpisode verifies the alert fires once per
// episode: an unresolved failing/hung/pending episode that crosses the
// threshold alerts on the pass that crosses it and on the next pass
// does not alert again, no matter how many more passes follow.
func TestRepoCIWatchAlertOncePerEpisode(t *testing.T) {
	dir := t.TempDir()
	m, c, cap := newAlertHarness(t, dir, RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60,
		AlertMinutes: ptrInt(30),
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	oneFailingRun(m)

	c.repoCIWatchPass() // t0: the episode starts

	nc.advance(35 * time.Minute)
	c.repoCIWatchPass() // the threshold crosses — the one alert
	if got := len(cap.alerts); got != 1 {
		t.Fatalf("at t0+35m: alerts = %d, want 1", got)
	}

	nc.advance(35 * time.Minute)
	c.repoCIWatchPass()
	if got := len(cap.alerts); got != 1 {
		t.Errorf("at t0+70m: alerts = %d, want 1 (already alerted for this episode)", got)
	}

	nc.advance(35 * time.Minute)
	c.repoCIWatchPass()
	if got := len(cap.alerts); got != 1 {
		t.Errorf("at t0+105m: alerts = %d, want 1 (the episode is alerted once, not per pass)", got)
	}
	if rec := findRecordByRun(t, m, "300"); rec != nil && rec.AlertedAt == nil {
		t.Error("the record's alert stamp must be present — it is what gates the next pass")
	}
}

// TestRepoCIWatchAlertNeverShadow verifies shadow mode never alerts:
// the episode is recorded, the would-do stand-ins are written, and the
// operator-facing sink is untouched no matter how long the episode
// stays unresolved.
func TestRepoCIWatchAlertNeverShadow(t *testing.T) {
	dir := t.TempDir()
	m, c, cap := newAlertHarness(t, dir, RepoCIWatchConfig{
		Mode: "shadow", Repos: []string{dir}, PollSeconds: 60,
		AlertMinutes: ptrInt(30),
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	oneFailingRun(m)

	c.repoCIWatchPass()
	nc.advance(45 * time.Minute)
	c.repoCIWatchPass()

	if got := len(cap.alerts); got != 0 {
		t.Errorf("shadow: alerts = %d, want 0 — shadow mode only records", got)
	}
	if rec := findRecordByRun(t, m, "300"); rec != nil && rec.AlertedAt != nil {
		t.Error("shadow: the record's alert stamp must stay unset (nothing was alerted)")
	}
}

// TestRepoCIWatchAlertNeverWhenZero verifies alert_minutes of 0
// disables alerting entirely: an episode past every plausible threshold
// still produces nothing at the sink.
func TestRepoCIWatchAlertNeverWhenZero(t *testing.T) {
	dir := t.TempDir()
	m, c, cap := newAlertHarness(t, dir, RepoCIWatchConfig{
		Mode: "on", Repos: []string{dir}, PollSeconds: 60,
		AlertMinutes: ptrInt(0),
	})
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	nc := &nowControl{now: t0}
	c.now = func() time.Time { return nc.now }
	oneFailingRun(m)

	c.repoCIWatchPass()
	nc.advance(90 * time.Minute) // well past any threshold
	c.repoCIWatchPass()

	if got := len(cap.alerts); got != 0 {
		t.Errorf("alert_minutes=0: alerts = %d, want 0 — 0 disables alerting", got)
	}
	if rec := findRecordByRun(t, m, "300"); rec != nil && rec.AlertedAt != nil {
		t.Error("alert_minutes=0: the record's alert stamp must stay unset")
	}
}
