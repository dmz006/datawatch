package session

import (
	"testing"
	"time"
)

func TestParseScheduleTime(t *testing.T) {
	// Fixed reference time: Wednesday 2026-03-29 14:00:00
	now := time.Date(2026, 3, 29, 14, 0, 0, 0, time.Local)

	tests := []struct {
		input string
		want  time.Time
	}{
		// Relative durations
		{"in 30 minutes", now.Add(30 * time.Minute)},
		{"in 2 hours", now.Add(2 * time.Hour)},
		{"in 1 day", now.AddDate(0, 0, 1)},
		{"in 1 week", now.AddDate(0, 0, 7)},

		// Raw Go durations
		{"30m", now.Add(30 * time.Minute)},
		{"2h", now.Add(2 * time.Hour)},
		{"1h30m", now.Add(90 * time.Minute)},

		// "at" time of day (today since 15:00 > 14:00)
		{"at 15:00", time.Date(2026, 3, 29, 15, 0, 0, 0, time.Local)},
		// "at" time of day (tomorrow since 10:00 < 14:00)
		{"at 10:00", time.Date(2026, 3, 30, 10, 0, 0, 0, time.Local)},

		// Tomorrow
		{"tomorrow at 9:00", time.Date(2026, 3, 30, 9, 0, 0, 0, time.Local)},

		// Next weekday (Wednesday is today, next thursday = March 30? No, 29 is Sunday actually)
		// Let me use the actual weekday for 2026-03-29

		// Absolute datetime
		{"2026-03-30 14:00", time.Date(2026, 3, 30, 14, 0, 0, 0, time.Local)},
		{"2026-03-30T14:00:00", time.Date(2026, 3, 30, 14, 0, 0, 0, time.Local)},

		// Now
		{"now", now},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseScheduleTime(tt.input, now)
			if err != nil {
				t.Fatalf("ParseScheduleTime(%q): %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("ParseScheduleTime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseScheduleTimeNextWeekday(t *testing.T) {
	// Sunday 2026-03-29 14:00:00
	now := time.Date(2026, 3, 29, 14, 0, 0, 0, time.Local)

	// Next Monday should be March 30
	got, err := ParseScheduleTime("next monday at 10:00", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 30, 10, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseScheduleTimeErrors(t *testing.T) {
	now := time.Now()
	badInputs := []string{
		"banana",
		"next blurgsday at noon",
		"in purple hours",
	}
	for _, input := range badInputs {
		_, err := ParseScheduleTime(input, now)
		if err == nil {
			t.Errorf("expected error for %q, got nil", input)
		}
	}
}
