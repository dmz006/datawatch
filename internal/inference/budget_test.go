package inference

import (
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func TestBudgetWindowParse(t *testing.T) {
	cases := []struct {
		name    string
		b       BudgetWindow
		wantErr string // substring; empty means valid
		mode    string
	}{
		{"rolling", BudgetWindow{CeilingUSD: 50, Hours: 5}, "", "rolling"},
		{"rolling max hours", BudgetWindow{CeilingUSD: 1, Hours: 720}, "", "rolling"},
		{"rolling with zone", BudgetWindow{CeilingUSD: 1, Hours: 24, Zone: "America/New_York"}, "", "rolling"},
		{"weekly default time", BudgetWindow{CeilingUSD: 10, Weekday: "monday"}, "", "weekly"},
		{"weekly mixed case", BudgetWindow{CeilingUSD: 10, Weekday: "FrIdAy", Time: "17:45", Zone: "Europe/Berlin"}, "", "weekly"},
		{"weekly peers", BudgetWindow{CeilingUSD: 10, Weekday: "sunday", PeerWindow: []string{"a", "b"}}, "", "weekly"},
		{"zero ceiling", BudgetWindow{Hours: 5}, "ceiling_usd", ""},
		{"negative ceiling", BudgetWindow{CeilingUSD: -1, Hours: 5}, "ceiling_usd", ""},
		{"no window style", BudgetWindow{CeilingUSD: 5}, "window style", ""},
		{"both styles", BudgetWindow{CeilingUSD: 5, Hours: 5, Weekday: "monday"}, "not both", ""},
		{"hours too large", BudgetWindow{CeilingUSD: 5, Hours: 721}, "out of range", ""},
		{"hours negative", BudgetWindow{CeilingUSD: 5, Hours: -3}, "out of range", ""},
		{"unknown weekday", BudgetWindow{CeilingUSD: 5, Weekday: "funday"}, "weekday", ""},
		{"bad time", BudgetWindow{CeilingUSD: 5, Weekday: "monday", Time: "25:00"}, "HH:MM", ""},
		{"bad time format", BudgetWindow{CeilingUSD: 5, Weekday: "monday", Time: "9am"}, "HH:MM", ""},
		{"time on rolling", BudgetWindow{CeilingUSD: 5, Hours: 5, Time: "09:00"}, "weekly", ""},
		{"unknown zone", BudgetWindow{CeilingUSD: 5, Weekday: "monday", Zone: "Mars/Olympus"}, "zone", ""},
		{"empty peer", BudgetWindow{CeilingUSD: 5, Hours: 5, PeerWindow: []string{"a", " "}}, "non-empty", ""},
		{"duplicate peer", BudgetWindow{CeilingUSD: 5, Hours: 5, PeerWindow: []string{"a", "a"}}, "duplicate", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				if got := tc.b.Mode(); got != tc.mode {
					t.Fatalf("Mode() = %q, want %q", got, tc.mode)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}

	// Defaults: empty time anchors at midnight, empty zone is UTC.
	b := BudgetWindow{CeilingUSD: 10, Weekday: "monday"}
	start, _ := b.WindowAt(mustTime(t, "2026-03-04T12:00:00Z"))
	if want := mustTime(t, "2026-03-02T00:00:00Z"); !start.Equal(want) {
		t.Fatalf("default anchor start = %v, want %v", start, want)
	}
	if start.Location() != time.UTC {
		t.Fatalf("empty zone should resolve to UTC, got %v", start.Location())
	}
}

func TestBudgetWindowAnchors(t *testing.T) {
	// 2026-03-02 is a Monday.
	b := BudgetWindow{CeilingUSD: 10, Weekday: "monday", Time: "09:30"}
	cases := []struct {
		name       string
		now        string
		start, end string
	}{
		{"before anchor", "2026-03-02T09:29:59Z", "2026-02-23T09:30:00Z", "2026-03-02T09:30:00Z"},
		{"on anchor", "2026-03-02T09:30:00Z", "2026-03-02T09:30:00Z", "2026-03-09T09:30:00Z"},
		{"after anchor", "2026-03-02T09:30:01Z", "2026-03-02T09:30:00Z", "2026-03-09T09:30:00Z"},
		{"mid week", "2026-03-05T18:00:00Z", "2026-03-02T09:30:00Z", "2026-03-09T09:30:00Z"},
		{"same weekday earlier", "2026-03-09T08:00:00Z", "2026-03-02T09:30:00Z", "2026-03-09T09:30:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, e := b.WindowAt(mustTime(t, tc.now))
			if want := mustTime(t, tc.start); !s.Equal(want) {
				t.Errorf("start = %v, want %v", s, want)
			}
			if want := mustTime(t, tc.end); !e.Equal(want) {
				t.Errorf("end = %v, want %v", e, want)
			}
		})
	}

	t.Run("non-UTC zone", func(t *testing.T) {
		jst := BudgetWindow{CeilingUSD: 10, Weekday: "monday", Zone: "Asia/Tokyo"}
		// 2026-03-01T16:00Z is Monday 2026-03-02 01:00 in Tokyo.
		s, e := jst.WindowAt(mustTime(t, "2026-03-01T16:00:00Z"))
		if want := mustTime(t, "2026-03-01T15:00:00Z"); !s.Equal(want) {
			t.Errorf("start = %v, want %v", s, want)
		}
		if want := mustTime(t, "2026-03-08T15:00:00Z"); !e.Equal(want) {
			t.Errorf("end = %v, want %v", e, want)
		}
		// One hour earlier it is still Sunday in Tokyo: previous window.
		s, e = jst.WindowAt(mustTime(t, "2026-03-01T14:59:59Z"))
		if want := mustTime(t, "2026-02-22T15:00:00Z"); !s.Equal(want) {
			t.Errorf("prior start = %v, want %v", s, want)
		}
		if want := mustTime(t, "2026-03-01T15:00:00Z"); !e.Equal(want) {
			t.Errorf("prior end = %v, want %v", e, want)
		}
	})

	t.Run("daylight-saving week", func(t *testing.T) {
		// New York springs forward Sunday 2026-03-08. The Saturday 12:00
		// anchor must stay at noon local on both sides of the change.
		ny := BudgetWindow{CeilingUSD: 10, Weekday: "saturday", Time: "12:00", Zone: "America/New_York"}
		s, e := ny.WindowAt(mustTime(t, "2026-03-10T00:00:00Z"))
		if want := mustTime(t, "2026-03-07T17:00:00Z"); !s.Equal(want) { // noon EST
			t.Errorf("start = %v, want %v", s, want)
		}
		if want := mustTime(t, "2026-03-14T16:00:00Z"); !e.Equal(want) { // noon EDT
			t.Errorf("end = %v, want %v", e, want)
		}
		if got := e.Sub(s); got != 167*time.Hour {
			t.Errorf("window across spring-forward = %v, want 167h", got)
		}
		if s.Hour() != 12 || e.Hour() != 12 {
			t.Errorf("local clock time shifted: start %v end %v", s, e)
		}
	})
}

func TestBudgetRollingWindowMath(t *testing.T) {
	b := BudgetWindow{CeilingUSD: 25, Hours: 5}
	now := mustTime(t, "2026-03-04T12:00:00Z")
	s, e := b.WindowAt(now)
	if !e.Equal(now) {
		t.Errorf("end = %v, want now %v", e, now)
	}
	if want := mustTime(t, "2026-03-04T07:00:00Z"); !s.Equal(want) {
		t.Errorf("start = %v, want %v", s, want)
	}

	long := BudgetWindow{CeilingUSD: 25, Hours: 720}
	s, e = long.WindowAt(now)
	if got := e.Sub(s); got != 720*time.Hour {
		t.Errorf("720h window spans %v", got)
	}
}

func TestBudgetRolloverMath(t *testing.T) {
	b := BudgetWindow{CeilingUSD: 10, Weekday: "wednesday", Time: "06:00"}
	anchor := mustTime(t, "2026-03-04T06:00:00Z") // a Wednesday

	_, endBefore := b.WindowAt(anchor.Add(-time.Second))
	if !endBefore.Equal(anchor) {
		t.Errorf("one second before: end = %v, want the anchor %v", endBefore, anchor)
	}

	startAt, endAt := b.WindowAt(anchor)
	if !startAt.Equal(anchor) {
		t.Errorf("at anchor: start = %v, want %v", startAt, anchor)
	}
	if want := anchor.AddDate(0, 0, 7); !endAt.Equal(want) {
		t.Errorf("at anchor: end = %v, want %v", endAt, want)
	}

	startAfter, endAfter := b.WindowAt(anchor.Add(time.Second))
	if !startAfter.Equal(anchor) || !endAfter.Equal(endAt) {
		t.Errorf("one second after: got [%v, %v), want [%v, %v)", startAfter, endAfter, anchor, endAt)
	}
}
