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
		// One-time schedules refused for capacity wait this long, then fail.
		OneTimeMaxWait: time.Duration(c.EffectiveOneTimeMaxWaitHours()) * time.Hour,
	}
}

// scheduleAlertText words the operator alert for a schedule that keeps waiting
// for capacity, that the re-arm policy gave up on, that has no live session
// with its name, or whose send a restart cut off.
func scheduleAlertText(a session.ScheduleAlert) (level alertspkg.Level, title, body string) {
	name := a.Schedule.ScheduleName
	if name == "" {
		name = a.Schedule.ID
	}
	switch {
	case a.Count > 0:
		return alertspkg.LevelWarn, fmt.Sprintf("%d scheduled commands did not run", a.Count),
			"datawatch ended these schedules without delivering them. Check the sessions, and create again any still wanted.\n" + a.Settled
	case a.Settled != "":
		target := a.Schedule.SessionID
		if a.Schedule.SessionName != "" {
			target = "named " + a.Schedule.SessionName
		}
		return alertspkg.LevelWarn, "Scheduled command did not run: " + name,
			fmt.Sprintf("Schedule %s (session %s) is now %s: %s. Check the session, and create it again if it is still wanted.",
				a.Schedule.ID, target, a.Schedule.State, a.Settled)
	case a.SendFailed:
		return alertspkg.LevelWarn, "Scheduled command send failed: " + name,
			fmt.Sprintf("Sending schedule %s failed (%d in a row): %v. It stays scheduled; next fire %s.",
				a.Schedule.ID, a.Schedule.ConsecutiveFailures, a.Err, a.Schedule.RunAt.Local().Format("2006-01-02 15:04"))
	case a.Interrupted:
		next := "It is now failed. Check the session, and create it again if the command did not arrive."
		switch a.Schedule.State {
		case session.SchedPending:
			next = "It stays scheduled for its next fire. Check the session if this fire mattered."
		case session.SchedDone:
			next = "It has no further fire. Check the session if this fire mattered."
		}
		return alertspkg.LevelWarn, "Scheduled command cut off by a restart: " + name,
			fmt.Sprintf("The daemon stopped while schedule %s was being typed. The command may or may not have arrived, so it is not typed again. %s", a.Schedule.ID, next)
	case a.NoSession:
		return alertspkg.LevelWarn, "Schedule is waiting for a session: " + name,
			fmt.Sprintf("Schedule %s sends a command to the session named %q, and no live session has that name. It stays scheduled and goes to the next live session with that name: a recurring schedule skips the fires it misses, any other is sent once that session can take it. Cancel the schedule if it is no longer wanted.",
				a.Schedule.ID, a.Schedule.SessionName)
	case a.Outcome.WaitLimit:
		return alertspkg.LevelError, "Scheduled spawn gave up waiting for capacity: " + name,
			fmt.Sprintf("Schedule %s is a one-time schedule and was refused for capacity until its wait limit ran out (%v). It is now failed. Re-create it to run it again, or raise schedule.one_time_max_wait_hours.",
				a.Schedule.ID, a.Err)
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
	// A schedule that could never run is refused when it is made.
	mgr.EnableScheduleTargetCheck()
	mgr.SetScheduleRearmPolicy(func() session.RearmPolicy { return scheduleRearmPolicy(cfg.Schedule) })
	mgr.SetScheduleAlertHook(func(a session.ScheduleAlert) {
		level, title, body := scheduleAlertText(a)
		alertspkg.EmitSystem(level, title, body)
	})
}
