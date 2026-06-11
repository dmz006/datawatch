package session

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronNext returns the next time after `from` that matches the cron expression.
// expr must be 5 space-separated fields: minute hour day-of-month month day-of-week.
// Supported field syntax: * */n n n-m n,m,...
func CronNext(expr string, from time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron: expected 5 fields, got %d", len(fields))
	}

	minuteSpec := fields[0]
	hourSpec := fields[1]
	domSpec := fields[2]
	monthSpec := fields[3]
	dowSpec := fields[4]

	// Validate field ranges during parse
	minutes, err := parseCronField(minuteSpec, 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron minute: %w", err)
	}
	hours, err := parseCronField(hourSpec, 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron hour: %w", err)
	}
	doms, err := parseCronField(domSpec, 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron day-of-month: %w", err)
	}
	months, err := parseCronField(monthSpec, 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron month: %w", err)
	}
	dows, err := parseCronField(dowSpec, 0, 6)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron day-of-week: %w", err)
	}

	// Advance to at least the next minute boundary
	t := from.Add(time.Minute - time.Duration(from.Second())*time.Second - time.Duration(from.Nanosecond())*time.Nanosecond)

	// Search forward up to 4 years
	limit := from.Add(4 * 365 * 24 * time.Hour)
	for t.Before(limit) {
		// Check month (1-12)
		if !months[int(t.Month())] {
			// Advance to first day of next matching month
			t = advanceToNextMonth(t, months)
			if t.IsZero() || !t.Before(limit) {
				break
			}
			continue
		}
		// Check day of month and day of week
		if !doms[t.Day()] || !dows[int(t.Weekday())] {
			// Advance by one day
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		// Check hour
		if !hours[t.Hour()] {
			// Advance to next matching hour in same day
			next := advanceToNextHour(t, hours)
			if next.IsZero() {
				// No matching hour today; advance to next day
				t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			} else {
				t = next
			}
			continue
		}
		// Check minute
		if !minutes[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		// All fields match
		return t.In(time.Local), nil
	}
	return time.Time{}, fmt.Errorf("cron: no matching time found within 4 years for %q", expr)
}

// ValidateCron returns an error if expr is not a valid 5-field cron expression.
func ValidateCron(expr string) error {
	_, err := CronNext(expr, time.Now())
	if err != nil && strings.Contains(err.Error(), "no matching time") {
		return nil // valid syntax, just no match in window
	}
	return err
}

// parseCronField parses a single cron field and returns a boolean set
// for matching values. min and max define the valid range (inclusive).
func parseCronField(field string, min, max int) ([]bool, error) {
	result := make([]bool, max+1)

	parts := strings.Split(field, ",")
	for _, part := range parts {
		if err := parseCronPart(part, min, max, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func parseCronPart(part string, min, max int, result []bool) error {
	// Handle step: */n or n-m/n
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s < 1 {
			return fmt.Errorf("invalid step %q", part[idx+1:])
		}
		step = s
		part = part[:idx]
	}

	var lo, hi int
	if part == "*" {
		lo, hi = min, max
	} else if idx := strings.Index(part, "-"); idx >= 0 {
		a, err1 := strconv.Atoi(part[:idx])
		b, err2 := strconv.Atoi(part[idx+1:])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("invalid range %q", part)
		}
		if a < min || b > max || a > b {
			return fmt.Errorf("range %d-%d out of bounds [%d,%d]", a, b, min, max)
		}
		lo, hi = a, b
	} else {
		n, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid value %q", part)
		}
		if n < min || n > max {
			return fmt.Errorf("value %d out of bounds [%d,%d]", n, min, max)
		}
		lo, hi = n, n
	}

	for v := lo; v <= hi; v += step {
		if v >= 0 && v < len(result) {
			result[v] = true
		}
	}
	return nil
}

// advanceToNextMonth advances t to the first day of the next month that matches.
func advanceToNextMonth(t time.Time, months []bool) time.Time {
	// Try up to 12 months ahead
	next := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
	for i := 0; i < 12; i++ {
		m := int(next.Month())
		if m < len(months) && months[m] {
			return next
		}
		next = time.Date(next.Year(), next.Month()+1, 1, 0, 0, 0, 0, next.Location())
	}
	return time.Time{} // no match
}

// advanceToNextHour advances t to the start of the next matching hour in the same day.
// Returns zero time if no matching hour remains today.
func advanceToNextHour(t time.Time, hours []bool) time.Time {
	for h := t.Hour() + 1; h <= 23; h++ {
		if h < len(hours) && hours[h] {
			return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, t.Location())
		}
	}
	return time.Time{}
}
