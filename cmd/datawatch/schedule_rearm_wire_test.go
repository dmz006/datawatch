package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestScheduleRearmPolicy_FromConfig(t *testing.T) {
	if p := scheduleRearmPolicy(config.ScheduleConfig{}); p.Mode != session.RearmNextFire || p.MaxRetries != 0 ||
		p.Backoff != 30*time.Second || p.BackoffMax != 15*time.Minute {
		t.Fatalf("zero config must give the default policy: %+v", p)
	}
	p := scheduleRearmPolicy(config.ScheduleConfig{RearmMode: "backoff", RearmMaxRetries: 3, RearmBackoffSeconds: 5, RearmBackoffMaxSeconds: 60})
	if p.Mode != session.RearmBackoff || p.MaxRetries != 3 || p.Backoff != 5*time.Second || p.BackoffMax != time.Minute {
		t.Fatalf("configured policy not carried over: %+v", p)
	}
}

func TestScheduleAlertText_NamesTheOutcome(t *testing.T) {
	sc := session.ScheduledCommand{ID: "ab12", ScheduleName: "detector"}
	lvl, title, body := scheduleAlertText(session.ScheduleAlert{Schedule: sc, Err: errors.New("tmux exploded"),
		Outcome: session.StartFailure{GaveUp: true, Consecutive: 4}})
	if lvl != alertspkg.LevelError || !strings.Contains(title, "gave up: detector") || !strings.Contains(body, "stays failed") {
		t.Fatalf("give-up alert: %v %q %q", lvl, title, body)
	}
	lvl, title, body = scheduleAlertText(session.ScheduleAlert{Schedule: sc, Err: errors.New("max sessions (20) reached"),
		Outcome: session.StartFailure{Wait: true, Waits: 5, NextAt: time.Now().Add(time.Minute)}})
	if lvl != alertspkg.LevelWarn || !strings.Contains(title, "waiting for capacity: detector") || !strings.Contains(body, "max sessions (20)") {
		t.Fatalf("wait alert: %v %q %q", lvl, title, body)
	}
	_, title, _ = scheduleAlertText(session.ScheduleAlert{Schedule: session.ScheduledCommand{ID: "zz"}, Err: errors.New("x"), Outcome: session.StartFailure{Consecutive: 1, Rearmed: true}})
	if !strings.Contains(title, "failed: zz") {
		t.Fatalf("an unnamed schedule falls back to its id: %q", title)
	}
}

func TestScheduleRearmPolicy_CarriesOneTimeMaxWait(t *testing.T) {
	if p := scheduleRearmPolicy(config.ScheduleConfig{}); p.OneTimeMaxWait != 12*time.Hour {
		t.Fatalf("zero config must give a 12 hour one-time wait limit: %v", p.OneTimeMaxWait)
	}
	if p := scheduleRearmPolicy(config.ScheduleConfig{OneTimeMaxWaitHours: 3}); p.OneTimeMaxWait != 3*time.Hour {
		t.Fatalf("configured one-time wait limit not carried over: %v", p.OneTimeMaxWait)
	}
}

func TestScheduleAlertText_WaitLimitSaysItWaitedTooLong(t *testing.T) {
	sc := session.ScheduledCommand{ID: "ab12", ScheduleName: "nightly"}
	lvl, title, body := scheduleAlertText(session.ScheduleAlert{Schedule: sc, Err: errors.New("gave up waiting for capacity after 12 hours: max sessions (20) reached"),
		Outcome: session.StartFailure{GaveUp: true, WaitLimit: true, Waits: 40}})
	if lvl != alertspkg.LevelError || !strings.Contains(title, "gave up waiting for capacity: nightly") {
		t.Fatalf("wait-limit alert: %v %q", lvl, title)
	}
	if strings.Contains(body, "times in a row") || !strings.Contains(body, "12 hours") || !strings.Contains(body, "schedule.one_time_max_wait_hours") {
		t.Fatalf("body must name the wait and the setting, not claim failed starts: %q", body)
	}
}
