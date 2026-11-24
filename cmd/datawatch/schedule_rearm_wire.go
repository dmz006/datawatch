package main

import (
	"fmt"
	"time"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// scheduleRearmPolicy turns the schedule config into the policy the manager
// reads at every failed start.
func scheduleRearmPolicy(c config.ScheduleConfig) session.RearmPolicy {
	return session.RearmPolicy{
		Mode:       c.EffectiveRearmMode(),
		MaxRetries: c.EffectiveRearmMaxRetries(),
		Backoff:    time.Duration(c.EffectiveRearmBackoffSeconds()) * time.Second,
		BackoffMax: time.Duration(c.EffectiveRearmBackoffMaxSeconds()) * time.Second,
	}
}

// scheduleAlertText words the operator alert for a schedule that keeps waiting
// for capacity or that the re-arm policy gave up on.
func scheduleAlertText(a session.ScheduleAlert) (level alertspkg.Level, title, body string) {
	name := a.Schedule.ScheduleName
	if name == "" {
		name = a.Schedule.ID
	}
	switch {
	case a.Outcome.GaveUp:
		return alertspkg.LevelError, "Scheduled spawn gave up: " + name,
			fmt.Sprintf("Schedule %s could not start its session %d times in a row (%v). The re-arm policy gave up, so it stays failed until you re-create or re-enable it.",
				a.Schedule.ID, a.Outcome.Consecutive, a.Err)
	case a.Outcome.Wait:
		return alertspkg.LevelWarn, "Scheduled spawn is waiting for capacity: " + name,
			fmt.Sprintf("Schedule %s has been refused %d times in a row (%v). It stays scheduled and retries with backoff; next try %s.",
				a.Schedule.ID, a.Outcome.Waits, a.Err, a.Outcome.NextAt.Local().Format("15:04:05"))
	default:
		next := "it will retry"
		if a.Outcome.Rearmed {
			next = "it will retry at " + a.Outcome.NextAt.Local().Format("2006-01-02 15:04")
		}
		return alertspkg.LevelWarn, "Scheduled spawn failed: " + name,
			fmt.Sprintf("Schedule %s could not start its session (%d in a row): %v — %s.", a.Schedule.ID, a.Outcome.Consecutive, a.Err, next)
	}
}

// wireScheduleRearm hands the manager the live re-arm policy and the alert
// hook. The policy is read from cfg at every failed start.
func wireScheduleRearm(mgr *session.Manager, cfg *config.Config) {
	mgr.SetScheduleRearmPolicy(func() session.RearmPolicy { return scheduleRearmPolicy(cfg.Schedule) })
	mgr.SetScheduleAlertHook(func(a session.ScheduleAlert) {
		level, title, body := scheduleAlertText(a)
		alertspkg.EmitSystem(level, title, body)
	})
}
