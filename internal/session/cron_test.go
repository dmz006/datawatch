package session

import (
	"testing"
	"time"
)

func TestCronNext_EveryMinute(t *testing.T) {
	loc := time.Local
	from := time.Date(2026, 6, 10, 14, 30, 0, 0, loc)
	next, err := CronNext("* * * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 6, 10, 14, 31, 0, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("got %v, want %v", next, expected)
	}
}

func TestCronNext_StepMinute(t *testing.T) {
	// */5 * * * * — every 5 minutes
	loc := time.Local
	from := time.Date(2026, 6, 10, 14, 3, 0, 0, loc)
	next, err := CronNext("*/5 * * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 6, 10, 14, 5, 0, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("got %v, want %v", next, expected)
	}
}

func TestCronNext_ExactMinute(t *testing.T) {
	// 30 9 * * * — 09:30 every day
	loc := time.Local
	from := time.Date(2026, 6, 10, 9, 0, 0, 0, loc)
	next, err := CronNext("30 9 * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 6, 10, 9, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("got %v, want %v", next, expected)
	}
}

func TestCronNext_ExactMinuteTomorrow(t *testing.T) {
	// 30 9 * * * — already past today, should be tomorrow
	loc := time.Local
	from := time.Date(2026, 6, 10, 10, 0, 0, 0, loc)
	next, err := CronNext("30 9 * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 6, 11, 9, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("got %v, want %v", next, expected)
	}
}

func TestCronNext_NamedRange(t *testing.T) {
	// 0 9 * * 1-5 — 9am weekdays only
	loc := time.Local
	from := time.Date(2026, 6, 13, 9, 0, 0, 0, loc) // Saturday
	next, err := CronNext("0 9 * * 1-5", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should jump to Monday June 15
	expected := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("got %v, want %v", next, expected)
	}
}

func TestCronNext_AdvancesAtLeastOneMinute(t *testing.T) {
	// Even if the current second=0, must still advance
	loc := time.Local
	from := time.Date(2026, 6, 10, 14, 30, 0, 0, loc)
	next, err := CronNext("30 14 * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must be after from (tomorrow's 14:30)
	if !next.After(from) {
		t.Errorf("next %v should be after from %v", next, from)
	}
	// Should be next day at 14:30
	expected := time.Date(2026, 6, 11, 14, 30, 0, 0, loc)
	if !next.Equal(expected) {
		t.Errorf("got %v, want %v", next, expected)
	}
}

func TestValidateCron_Valid(t *testing.T) {
	exprs := []string{
		"* * * * *",
		"*/5 * * * *",
		"30 9 * * 1-5",
		"0 0 1 * *",
		"0,30 * * * *",
	}
	for _, expr := range exprs {
		if err := ValidateCron(expr); err != nil {
			t.Errorf("ValidateCron(%q) unexpected error: %v", expr, err)
		}
	}
}

func TestValidateCron_Invalid(t *testing.T) {
	exprs := []string{
		"",
		"* * *",
		"60 * * * *",  // minute out of range
		"* 25 * * *",  // hour out of range
		"abc * * * *", // non-numeric
		"0 0 0 * *",   // day 0 out of range
	}
	for _, expr := range exprs {
		if err := ValidateCron(expr); err == nil {
			t.Errorf("ValidateCron(%q) expected error, got nil", expr)
		}
	}
}
