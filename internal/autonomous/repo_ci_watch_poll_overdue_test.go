// The repo CI watch overdue alert, verified through the coordinator's
// overdue check: it fires once when the last successful poll is older
// than poll_seconds plus alert_minutes, never in mode off, never when
// alert_minutes is 0, once per episode, and an episode ends on the next
// successful poll.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// overdueRuns makes the run list succeed (empty) or fail, so a test can
// move the watch between a successful and an overdue poll.
func overdueRuns(m *Manager, ok bool) {
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		if !ok {
			return nil, fmt.Errorf("gh unavailable")
		}
		return []RepoCIWatchRun{}, nil
	}
}

// overdueHarness wires a repo CI watch at t0 with a capture on the alert
// sink and the test clock.
func overdueHarness(t *testing.T, cfg RepoCIWatchConfig) (*Manager, *Coordinator, *alertSinkCapture, *nowControl) {
	t.Helper()
	dir := t.TempDir()
	cfg.Repos = []string{dir}
	m, c, _ := newActionHarness(t, dir, cfg)
	cap := &alertSinkCapture{}
	c.alertFn = cap.fn()
	nc := &nowControl{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	c.now = func() time.Time { return nc.now }
	return m, c, cap, nc
}

// TestRepoCIWatchOverdueAlertRaised verifies a successful poll followed
// by silence past poll_seconds + alert_minutes raises one alert that
// names the repository and the last successful poll; nothing fires
// before the threshold.
func TestRepoCIWatchOverdueAlertRaised(t *testing.T) {
	m, c, cap, nc := overdueHarness(t, RepoCIWatchConfig{
		Mode: "on", PollSeconds: 60, AlertMinutes: ptrInt(5),
	})
	overdueRuns(m, true)
	c.repoCIWatchPass() // a successful poll at t0
	lastOK := nc.now

	nc.advance(60*time.Second + 5*time.Minute - time.Second)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 0 {
		t.Fatalf("just before poll_seconds+alert_minutes: alerts = %d, want 0", got)
	}

	nc.advance(2 * time.Second)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 1 {
		t.Fatalf("past poll_seconds+alert_minutes: alerts = %d, want 1", got)
	}
	if got := cap.alerts[0]; got != "warn|CI watch: repository poll overdue" {
		t.Errorf("alert = %q, want the warn overdue line", got)
	}
	if !strings.Contains(cap.body, m.Config().Automation.RepoCIWatch.Repos[0]) {
		t.Errorf("body must name the repository, got %q", cap.body)
	}
	if !strings.Contains(cap.body, lastOK.UTC().Format("2006-01-02 15:04:05 UTC")) {
		t.Errorf("body must name the last successful poll time, got %q", cap.body)
	}
}

// TestRepoCIWatchOverdueNeverPolledUsesArmedBaseline verifies a watch
// that has never polled successfully still alerts: the overdue clock
// starts when the watch is first seen active.
func TestRepoCIWatchOverdueNeverPolledUsesArmedBaseline(t *testing.T) {
	m, c, cap, nc := overdueHarness(t, RepoCIWatchConfig{
		Mode: "on", PollSeconds: 60, AlertMinutes: ptrInt(5),
	})
	overdueRuns(m, false)
	c.repoCIWatchOverdueCheck() // arms the baseline at t0
	if got := len(cap.alerts); got != 0 {
		t.Fatalf("at arm time: alerts = %d, want 0", got)
	}
	nc.advance(6 * time.Minute)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 1 {
		t.Fatalf("never polled, past threshold: alerts = %d, want 1", got)
	}
	if !strings.Contains(cap.body, "never") {
		t.Errorf("body must say no poll has succeeded, got %q", cap.body)
	}
}

// TestRepoCIWatchOverdueNoAlertWhenOff verifies mode off raises nothing,
// however long the watch has been silent.
func TestRepoCIWatchOverdueNoAlertWhenOff(t *testing.T) {
	m, c, cap, nc := overdueHarness(t, RepoCIWatchConfig{
		Mode: "off", PollSeconds: 60, AlertMinutes: ptrInt(5),
	})
	overdueRuns(m, false)
	c.repoCIWatchOverdueCheck()
	nc.advance(10 * time.Hour)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 0 {
		t.Errorf("mode off: alerts = %d, want 0", got)
	}
}

// TestRepoCIWatchOverdueNoAlertWhenAlertMinutesZero verifies alert_minutes
// of 0 disables the overdue alert entirely.
func TestRepoCIWatchOverdueNoAlertWhenAlertMinutesZero(t *testing.T) {
	m, c, cap, nc := overdueHarness(t, RepoCIWatchConfig{
		Mode: "on", PollSeconds: 60, AlertMinutes: ptrInt(0),
	})
	overdueRuns(m, false)
	c.repoCIWatchOverdueCheck()
	nc.advance(10 * time.Hour)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 0 {
		t.Errorf("alert_minutes=0: alerts = %d, want 0", got)
	}
}

// TestRepoCIWatchOverdueOncePerEpisode verifies the overdue alert fires
// once per episode no matter how many ticks follow the threshold.
func TestRepoCIWatchOverdueOncePerEpisode(t *testing.T) {
	m, c, cap, nc := overdueHarness(t, RepoCIWatchConfig{
		Mode: "on", PollSeconds: 60, AlertMinutes: ptrInt(5),
	})
	overdueRuns(m, false)
	c.repoCIWatchOverdueCheck()
	nc.advance(10 * time.Minute)
	for i := 0; i < 5; i++ {
		c.repoCIWatchOverdueCheck()
		nc.advance(time.Minute)
	}
	if got := len(cap.alerts); got != 1 {
		t.Errorf("many ticks in one overdue episode: alerts = %d, want 1", got)
	}
}

// TestRepoCIWatchOverdueEpisodeResetsAfterPoll verifies a successful poll
// ends the overdue episode, so the next overdue episode alerts again.
func TestRepoCIWatchOverdueEpisodeResetsAfterPoll(t *testing.T) {
	m, c, cap, nc := overdueHarness(t, RepoCIWatchConfig{
		Mode: "on", PollSeconds: 60, AlertMinutes: ptrInt(5),
	})
	overdueRuns(m, false)
	c.repoCIWatchOverdueCheck()
	nc.advance(10 * time.Minute)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 1 {
		t.Fatalf("first overdue episode: alerts = %d, want 1", got)
	}

	overdueRuns(m, true)
	c.repoCIWatchPass() // a successful poll ends the episode
	nc.advance(time.Minute)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 1 {
		t.Fatalf("after a successful poll, still fresh: alerts = %d, want 1", got)
	}

	overdueRuns(m, false)
	nc.advance(10 * time.Minute)
	c.repoCIWatchOverdueCheck()
	if got := len(cap.alerts); got != 2 {
		t.Errorf("second overdue episode after a poll: alerts = %d, want 2", got)
	}
}
