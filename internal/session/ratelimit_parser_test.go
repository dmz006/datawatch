package session

import (
	"testing"
	"time"
)

func TestParseRateLimitResetTime_DatawatchProtocol(t *testing.T) {
	in := "DATAWATCH_RATE_LIMITED: resets at 2030-01-02T15:04:05Z"
	got := parseRateLimitResetTime(in)
	want, _ := time.Parse(time.RFC3339, "2030-01-02T15:04:05Z")
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseRateLimitResetTime_DateOnly(t *testing.T) {
	in := "limit info: resets at 2030-06-15"
	got := parseRateLimitResetTime(in)
	want, _ := time.Parse("2006-01-02", "2030-06-15")
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseRateLimitResetTime_ClaudeProseClock12h(t *testing.T) {
	in := "Your usage limit will reset at 9pm (America/Los_Angeles)."
	got := parseRateLimitResetTime(in)
	if got.IsZero() {
		t.Fatalf("expected non-zero time, got zero")
	}
	if got.Hour() != 21 || got.Minute() != 0 {
		t.Fatalf("expected 21:00, got %02d:%02d", got.Hour(), got.Minute())
	}
}

func TestParseRateLimitResetTime_ClaudeProseClockMinutes(t *testing.T) {
	in := "Rate limit will reset at 5:30 PM PST."
	got := parseRateLimitResetTime(in)
	if got.IsZero() {
		t.Fatalf("expected non-zero, got zero")
	}
	if got.Hour() != 17 || got.Minute() != 30 {
		t.Fatalf("expected 17:30, got %02d:%02d", got.Hour(), got.Minute())
	}
}

func TestParseRateLimitResetTime_Relative(t *testing.T) {
	cases := []struct {
		in     string
		minDur time.Duration
		maxDur time.Duration
	}{
		{"resets in 30m", 29 * time.Minute, 31 * time.Minute},
		{"resets in 4h 23m", 4*time.Hour + 22*time.Minute, 4*time.Hour + 24*time.Minute},
		{"try again in 2 hours", 1*time.Hour + 59*time.Minute, 2*time.Hour + 1*time.Minute},
	}
	for _, tc := range cases {
		got := parseRateLimitResetTime(tc.in)
		if got.IsZero() {
			t.Errorf("%q: expected non-zero time", tc.in)
			continue
		}
		d := time.Until(got)
		if d < tc.minDur || d > tc.maxDur {
			t.Errorf("%q: duration %v out of expected range [%v, %v]", tc.in, d, tc.minDur, tc.maxDur)
		}
	}
}

func TestParseRateLimitResetTime_Unparseable(t *testing.T) {
	cases := []string{
		"some random output",
		"resets at unknown",
		"resets at",
	}
	for _, in := range cases {
		got := parseRateLimitResetTime(in)
		if !got.IsZero() {
			t.Errorf("%q: expected zero time, got %v", in, got)
		}
	}
}

// BL185 — operator repro 2026-04-26:
//
//	"⎿  You've hit your limit · resets 10pm (America/New_York)"
//
// The newer claude rate-limit message uses "resets <time> (<zone>)"
// without the "at" the older format had. Verify the parser picks
// up the new shape.
func TestParseRateLimitResetTime_ClaudeNewFormat(t *testing.T) {
	cases := []string{
		"⎿  You've hit your limit · resets 10pm (America/New_York)",
		"You've hit your limit · resets 10pm",
		"resets 22:00 (US/Pacific)",
		"You've hit your limit · resets 9:30 PM",
	}
	for _, in := range cases {
		got := parseRateLimitResetTime(in)
		if got.IsZero() {
			t.Errorf("%q: expected a parsed time, got zero", in)
			continue
		}
		// Reset time must be in the future (parser rolls forward when
		// the clock-time has already passed today).
		if !got.After(time.Now().Add(-time.Minute)) {
			t.Errorf("%q: parsed time %v is too far in the past", in, got)
		}
	}
}
