// dependency_update_schedule.go — the weekly schedule computation for the
// dependency update Automation. Both functions are pure: the clock and the
// location are arguments, never read from the wall clock or the local zone.
// The 'weekday HH:MM' text is parsed by the config package's validation so
// there is one parser.

package autonomous

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// scheduleSearchDays bounds the day scan; a weekly schedule always has an
// instant within eight calendar days either side of any moment.
const scheduleSearchDays = 9

// scheduleInstant is the wall-clock instant of the schedule on the calendar
// day of ref in loc, and whether that day is the scheduled weekday. A wall
// time skipped by a spring-forward gap is moved forward by the gap (02:30
// becomes 03:30); an ambiguous fall-back time resolves to its first
// occurrence.
func scheduleInstant(ref time.Time, weekday string, hour, minute int, loc *time.Location) (time.Time, bool) {
	y, m, d := ref.In(loc).Date()
	t := time.Date(y, m, d, hour, minute, 0, 0, loc)
	if t.Hour() != hour || t.Minute() != minute {
		// The wall time does not exist; time.Date's choice is unspecified, so
		// shift by the missing wall-clock amount for a fixed result.
		want := time.Date(y, m, d, hour, minute, 0, 0, time.UTC)
		got := time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, time.UTC)
		t = t.Add(want.Sub(got))
	}
	return t, strings.ToLower(t.Weekday().String()) == weekday
}

func parseSchedule(schedule string, loc *time.Location) (string, int, int, error) {
	if loc == nil {
		return "", 0, 0, fmt.Errorf("dependency update schedule: location is required")
	}
	return config.ParseDependencyUpdateSchedule(schedule)
}

// NextDependencyUpdateRun returns the next occurrence, strictly after 'after',
// of the 'weekday HH:MM' schedule in loc.
func NextDependencyUpdateRun(schedule string, after time.Time, loc *time.Location) (time.Time, error) {
	wd, h, m, err := parseSchedule(schedule, loc)
	if err != nil {
		return time.Time{}, err
	}
	base := after.In(loc)
	for i := -1; i <= scheduleSearchDays; i++ {
		t, ok := scheduleInstant(base.AddDate(0, 0, i), wd, h, m, loc)
		if ok && t.After(after) {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("dependency update schedule: no occurrence found for %q", schedule)
}

// previousDependencyUpdateRun returns the latest scheduled instant at or
// before now.
func previousDependencyUpdateRun(wd string, h, m int, now time.Time, loc *time.Location) (time.Time, bool) {
	base := now.In(loc)
	for i := 1; i >= -scheduleSearchDays; i-- {
		t, ok := scheduleInstant(base.AddDate(0, 0, i), wd, h, m, loc)
		if ok && !t.After(now) {
			return t, true
		}
	}
	return time.Time{}, false
}

// DependencyUpdateDue reports whether the most recent scheduled instant at or
// before now is later than lastRun. A zero lastRun (first start) is never due,
// so the daemon does not run at start; the next scheduled instant applies.
// A daemon that was down across the scheduled time is due exactly once: after
// the run records lastRun = now, the same instant is no longer later.
func DependencyUpdateDue(schedule string, lastRun, now time.Time, loc *time.Location) (bool, error) {
	wd, h, m, err := parseSchedule(schedule, loc)
	if err != nil {
		return false, err
	}
	if lastRun.IsZero() {
		return false, nil
	}
	prev, ok := previousDependencyUpdateRun(wd, h, m, now, loc)
	if !ok {
		return false, nil
	}
	return prev.After(lastRun), nil
}
