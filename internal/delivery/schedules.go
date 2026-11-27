package delivery

import (
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// maxMissedSlots bounds the count of one "missed" item.
const maxMissedSlots = 1000

// systemCancelPrefix starts the reason datawatch records when it cancels a
// schedule itself; the operator's own cancel records none.
const systemCancelPrefix = "cancelled: "

// ScheduleCandidates reports pending schedules that cannot fire or have waited
// too long, and failed and system-cancelled ones.
func ScheduleCandidates(all []session.ScheduledCommand, targets ScheduleTargets, now time.Time, interval time.Duration) []Candidate {
	byID := make(map[string]session.ScheduledCommand, len(all))
	for _, sc := range all {
		byID[sc.ID] = sc
	}
	var out []Candidate
	for _, sc := range all {
		switch sc.State {
		case session.SchedFailed:
			out = append(out, endedSchedule(sc, "failed"))
		case session.SchedCancelled:
			if strings.HasPrefix(sc.LastError, systemCancelPrefix) {
				out = append(out, endedSchedule(sc, "cancelled_by_datawatch"))
			}
		case session.SchedPending:
			if c, ok := pendingSchedule(sc, byID, targets, now, interval); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func endedSchedule(sc session.ScheduledCommand, how string) Candidate {
	since := sc.DoneAt
	if since.IsZero() {
		since = sc.CreatedAt
	}
	return Candidate{Kind: KindSchedule, ID: sc.ID, State: how, Since: since, Problem: how,
		Invariant: InvFailed, Sticky: true, WaitsOn: WaitsOnOperator}
}

func pendingSchedule(sc session.ScheduledCommand, byID map[string]session.ScheduledCommand,
	targets ScheduleTargets, now time.Time, interval time.Duration) (Candidate, bool) {
	c := Candidate{Kind: KindSchedule, ID: sc.ID, Since: sc.CreatedAt, Invariant: InvScheduleFire, WaitsOn: WaitsOnOperator}
	broken := func(why string) (Candidate, bool) {
		c.State, c.Problem = why, why
		return c, true
	}
	waiting := func(state string, since time.Time, limit time.Duration) (Candidate, bool) {
		c.State, c.Reason, c.Limit = state, state, limit
		if !since.IsZero() {
			c.Since = since
		}
		return c, true
	}
	if sc.FiringSince != nil {
		return waiting("stuck_firing", *sc.FiringSince, 2*interval)
	}
	if sc.RearmState == "gave_up" {
		return broken("gave_up")
	}
	command := sc.Type == "" || sc.Type == session.SchedTypeCommand
	if sc.RunAfterID != "" {
		parent, ok := byID[sc.RunAfterID]
		switch {
		case !ok:
			return broken("parent_missing")
		case parent.State == session.SchedFailed || parent.State == session.SchedCancelled:
			return broken("parent_dead")
		}
	}
	if command {
		if !targets.Reachable(sc) {
			if sc.NameTargeted() {
				c.Invariant = InvNoHolder
				return waiting("no_holder", sc.WaitingSince, LimitScheduleWait)
			}
			return broken("target_gone")
		}
	}
	if sc.RunAfterID != "" {
		if parent := byID[sc.RunAfterID]; parent.State == session.SchedDone {
			return waiting("parent_done_not_fired", parent.DoneAt, 2*interval)
		}
		return Candidate{}, false
	}
	if sc.RunAt.IsZero() {
		if !command {
			return broken("no_trigger")
		}
		return waiting("waiting_input", time.Time{}, LimitScheduleWait)
	}
	if sc.RearmState == "waiting_capacity" {
		return waiting("waiting_capacity", sc.WaitingSince, LimitScheduleWait)
	}
	if now.Sub(sc.RunAt) > 2*interval {
		// One item for every slot that did not run.
		c.Count = missedSlots(sc, now)
		c.TimeDriven, c.Since, c.Sticky = true, sc.RunAt, sc.RecurEverySeconds > 0 || sc.CronExpr != ""
		c.State, c.Problem = "missed", "missed_slots"
		return c, true
	}
	return Candidate{}, false
}

// missedSlots counts the fire times from RunAt up to now.
func missedSlots(sc session.ScheduledCommand, now time.Time) int {
	if sc.CronExpr != "" {
		n, at := 1, sc.RunAt
		for n < maxMissedSlots {
			next, err := session.CronNext(sc.CronExpr, at)
			if err != nil || !next.After(at) || next.After(now) {
				break
			}
			at = next
			n++
		}
		return n
	}
	if sc.RecurEverySeconds > 0 {
		n := int(now.Sub(sc.RunAt)/(time.Duration(sc.RecurEverySeconds)*time.Second)) + 1
		if n > maxMissedSlots {
			return maxMissedSlots
		}
		return n
	}
	return 1
}
