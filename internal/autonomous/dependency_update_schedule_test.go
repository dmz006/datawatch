package autonomous

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone data for %s unavailable: %v", name, err)
	}
	return l
}

func TestNextDependencyUpdateRun(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	tokyo := mustLoc(t, "Asia/Tokyo")
	utc := time.UTC
	at := func(l *time.Location, y int, mo time.Month, d, h, mi, s int) time.Time {
		return time.Date(y, mo, d, h, mi, s, 0, l)
	}
	tests := []struct {
		name     string
		schedule string
		after    time.Time
		loc      *time.Location
		want     time.Time
	}{
		// 2026-11-09 is a Monday.
		{"one second before", "monday 05:00", at(utc, 2026, 11, 9, 4, 59, 59), utc, at(utc, 2026, 11, 9, 5, 0, 0)},
		{"exact instant is strictly after", "monday 05:00", at(utc, 2026, 11, 9, 5, 0, 0), utc, at(utc, 2026, 11, 16, 5, 0, 0)},
		{"one second after", "monday 05:00", at(utc, 2026, 11, 9, 5, 0, 1), utc, at(utc, 2026, 11, 16, 5, 0, 0)},
		{"different zone", "monday 05:00", at(utc, 2026, 11, 8, 19, 0, 0), tokyo, at(tokyo, 2026, 11, 9, 5, 0, 0)},
		{"zone rolls the weekday", "monday 05:00", at(utc, 2026, 11, 9, 21, 0, 0), tokyo, at(tokyo, 2026, 11, 16, 5, 0, 0)},
		{"mixed case weekday", "Monday 05:00", at(utc, 2026, 11, 1, 0, 0, 0), utc, at(utc, 2026, 11, 2, 5, 0, 0)},
		// US spring forward: Sunday 2026-03-08, 02:00 -> 03:00 in New York.
		{"spring forward gap", "sunday 02:30", at(ny, 2026, 3, 7, 12, 0, 0), ny, at(ny, 2026, 3, 8, 3, 30, 0)},
		{"after spring forward", "sunday 02:30", at(ny, 2026, 3, 8, 3, 30, 0), ny, at(ny, 2026, 3, 15, 2, 30, 0)},
		// US fall back: Sunday 2026-11-01, 02:00 -> 01:00; 01:30 happens twice.
		{"fall back ambiguous hour", "sunday 01:30", at(ny, 2026, 10, 31, 12, 0, 0), ny, at(ny, 2026, 11, 1, 1, 30, 0)},
		{"fall back past 02:30", "sunday 02:30", at(ny, 2026, 10, 31, 12, 0, 0), ny, at(ny, 2026, 11, 1, 2, 30, 0)},
		{"week after fall back", "sunday 02:30", at(ny, 2026, 11, 1, 2, 30, 0), ny, at(ny, 2026, 11, 8, 2, 30, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextDependencyUpdateRun(tc.schedule, tc.after, tc.loc)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestNextDependencyUpdateRunInvalid(t *testing.T) {
	for _, s := range []string{"", "monday", "funday 05:00", "monday 5:00", "monday 24:00", "monday 05:60", "monday 05:00 extra"} {
		if _, err := NextDependencyUpdateRun(s, time.Unix(0, 0), time.UTC); err == nil {
			t.Errorf("schedule %q: expected an error", s)
		}
		if _, err := DependencyUpdateDue(s, time.Unix(1, 0), time.Unix(2, 0), time.UTC); err == nil {
			t.Errorf("due schedule %q: expected an error", s)
		}
	}
	if _, err := NextDependencyUpdateRun("monday 05:00", time.Unix(0, 0), nil); err == nil {
		t.Error("nil location: expected an error")
	}
}

func TestDependencyUpdateDue(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	tokyo := mustLoc(t, "Asia/Tokyo")
	utc := time.UTC
	at := func(l *time.Location, y int, mo time.Month, d, h, mi, s int) time.Time {
		return time.Date(y, mo, d, h, mi, s, 0, l)
	}
	sched := at(utc, 2026, 11, 9, 5, 0, 0)
	tests := []struct {
		name     string
		schedule string
		last     time.Time
		now      time.Time
		loc      *time.Location
		want     bool
	}{
		{"zero lastRun is not due", "monday 05:00", time.Time{}, at(utc, 2026, 11, 9, 6, 0, 0), utc, false},
		{"one second before", "monday 05:00", sched.AddDate(0, 0, -7), sched.Add(-time.Second), utc, false},
		{"exact instant", "monday 05:00", sched.AddDate(0, 0, -7), sched, utc, true},
		{"one second after", "monday 05:00", sched.AddDate(0, 0, -7), sched.Add(time.Second), utc, true},
		{"already ran at the instant", "monday 05:00", sched, sched.Add(time.Hour), utc, false},
		{"ran after the instant", "monday 05:00", sched.Add(time.Minute), sched.Add(time.Hour), utc, false},
		{"different zone due", "monday 05:00", at(tokyo, 2026, 11, 2, 5, 0, 0), at(tokyo, 2026, 11, 9, 5, 0, 1), tokyo, true},
		{"different zone not yet", "monday 05:00", at(tokyo, 2026, 11, 2, 5, 0, 0), at(tokyo, 2026, 11, 9, 4, 59, 59), tokyo, false},
		{"down across the time", "monday 05:00", sched.AddDate(0, 0, -7), sched.AddDate(0, 0, 3), utc, true},
		{"spring forward before gap run", "sunday 02:30", at(ny, 2026, 3, 1, 2, 30, 0), at(ny, 2026, 3, 8, 3, 29, 59), ny, false},
		{"spring forward at resolved instant", "sunday 02:30", at(ny, 2026, 3, 1, 2, 30, 0), at(ny, 2026, 3, 8, 3, 30, 0), ny, true},
		{"fall back first 01:30", "sunday 01:30", at(ny, 2026, 10, 25, 1, 30, 0), at(ny, 2026, 11, 1, 1, 30, 0), ny, true},
		{"fall back repeated hour not due again", "sunday 01:30", at(ny, 2026, 11, 1, 1, 30, 0), at(ny, 2026, 11, 1, 1, 45, 0).Add(time.Hour), ny, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DependencyUpdateDue(tc.schedule, tc.last, tc.now, tc.loc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// A daemon down across the scheduled time is due once, then not again after
// the run records its time.
func TestDependencyUpdateDueOnceAfterDowntime(t *testing.T) {
	utc := time.UTC
	last := time.Date(2026, 11, 2, 5, 0, 0, 0, utc)
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, utc)
	due, err := DependencyUpdateDue("monday 05:00", last, now, utc)
	if err != nil || !due {
		t.Fatalf("first check: due=%v err=%v", due, err)
	}
	due, err = DependencyUpdateDue("monday 05:00", now, now.Add(time.Minute), utc)
	if err != nil || due {
		t.Fatalf("second check: due=%v err=%v", due, err)
	}
}
