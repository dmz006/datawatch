package session

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseScheduleTime parses a natural language time expression relative to now.
// Supported formats:
//   - "in 30 minutes", "in 2 hours", "in 1 day"
//   - "at 14:00", "at 2:30pm", "at 2pm"
//   - "tomorrow at 9am", "tomorrow at 14:00"
//   - "next monday at 10:00", "next wednesday at 2pm"
//   - "2026-03-30 14:00", "2026-03-30T14:00:00"
//   - Raw duration: "30m", "2h", "1h30m"
func ParseScheduleTime(input string, now time.Time) (time.Time, error) {
	original := strings.TrimSpace(input)
	input = strings.ToLower(original)
	if input == "" || input == "now" {
		return now, nil
	}

	// "on input" / "on-input" / "next input" / "on next input" = zero time (fire on waiting_input)
	if input == "on input" || input == "on-input" || input == "next input" || input == "on next input" {
		return time.Time{}, nil
	}

	// Try "in X duration" format
	if strings.HasPrefix(input, "in ") {
		return parseRelative(input[3:], now)
	}

	// Try raw Go duration ("30m", "2h", "1h30m")
	if d, err := time.ParseDuration(input); err == nil {
		return now.Add(d), nil
	}

	// Try "tomorrow at HH:MM"
	if strings.HasPrefix(input, "tomorrow") {
		rest := strings.TrimPrefix(input, "tomorrow")
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "at"))
		t, err := parseTimeOfDay(rest, now.AddDate(0, 0, 1))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time in 'tomorrow %s': %w", rest, err)
		}
		return t, nil
	}

	// Try "next <weekday> at HH:MM"
	if strings.HasPrefix(input, "next ") {
		return parseNextWeekday(input[5:], now)
	}

	// Try "at HH:MM" (today, or tomorrow if past)
	if strings.HasPrefix(input, "at ") {
		t, err := parseTimeOfDay(strings.TrimSpace(input[3:]), now)
		if err != nil {
			return time.Time{}, err
		}
		if t.Before(now) {
			t = t.AddDate(0, 0, 1) // tomorrow
		}
		return t, nil
	}

	// Try absolute datetime formats (use original case for ISO format T)
	for _, layout := range []string{
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"01/02/2006 15:04",
	} {
		if t, err := time.ParseInLocation(layout, original, now.Location()); err == nil {
			return t, nil
		}
	}

	// Try just a time of day (same as "at")
	if t, err := parseTimeOfDay(input, now); err == nil {
		if t.Before(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t, nil
	}

	return time.Time{}, fmt.Errorf("cannot parse time expression: %q", input)
}

var relativeRe = regexp.MustCompile(`(?i)(\d+)\s*(second|sec|s|minute|min|m|hour|hr|h|day|d|week|w)s?`)

func parseRelative(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)

	// Try Go duration first ("1h30m")
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(d), nil
	}

	matches := relativeRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return time.Time{}, fmt.Errorf("cannot parse relative time: %q", s)
	}

	t := now
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		unit := strings.ToLower(m[2])
		switch {
		case strings.HasPrefix(unit, "s"):
			t = t.Add(time.Duration(n) * time.Second)
		case strings.HasPrefix(unit, "min") || unit == "m":
			t = t.Add(time.Duration(n) * time.Minute)
		case strings.HasPrefix(unit, "h"):
			t = t.Add(time.Duration(n) * time.Hour)
		case strings.HasPrefix(unit, "d"):
			t = t.AddDate(0, 0, n)
		case strings.HasPrefix(unit, "w"):
			t = t.AddDate(0, 0, n*7)
		}
	}
	return t, nil
}

// parseTimeOfDay parses "14:00", "2:30pm", "2pm", "14:00:00" and sets on the given date.
func parseTimeOfDay(s string, date time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"3:04pm", "3:04 pm", "3pm", "3 pm",
		"15:04", "15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(date.Year(), date.Month(), date.Day(),
				t.Hour(), t.Minute(), t.Second(), 0, date.Location()), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time of day: %q", s)
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

func parseNextWeekday(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, " at ", 2)
	dayName := strings.TrimSpace(parts[0])
	timeStr := ""
	if len(parts) == 2 {
		timeStr = strings.TrimSpace(parts[1])
	}

	targetDay, ok := weekdays[dayName]
	if !ok {
		return time.Time{}, fmt.Errorf("unknown weekday: %q", dayName)
	}

	// Find next occurrence of that weekday
	daysAhead := int(targetDay) - int(now.Weekday())
	if daysAhead <= 0 {
		daysAhead += 7
	}
	targetDate := now.AddDate(0, 0, daysAhead)

	if timeStr != "" {
		return parseTimeOfDay(timeStr, targetDate)
	}
	// Default to 9am
	return time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(),
		9, 0, 0, 0, now.Location()), nil
}
