package usagecalc

import (
	"testing"
	"time"
)

// fakeLaneLookup is a lane history with fixed answers.
type fakeLaneLookup map[time.Time]int

func (f fakeLaneLookup) Lookup(t time.Time) (int, bool) {
	n, ok := f[t]
	return n, ok
}

func TestLaneStamper(t *testing.T) {
	now := laneTestNow
	covered := now.Add(-3 * time.Hour)
	uncovered := now.Add(-5 * time.Hour)
	history := fakeLaneLookup{covered: 1}

	cases := []struct {
		name    string
		counter *fakeLaneCounter
		history LaneLookup
		at      time.Time
		want    int
		source  string
	}{
		{"at the clock is live", &fakeLaneCounter{count: 3, ok: true}, history, now, 3, LanesLive},
		{"within 60 seconds is live", &fakeLaneCounter{count: 3, ok: true}, history, now.Add(-60 * time.Second), 3, LanesLive},
		{"after now is live", &fakeLaneCounter{count: 2, ok: true}, history, now.Add(3 * time.Minute), 2, LanesLive},
		{"earlier and covered is recorded", &fakeLaneCounter{count: 3, ok: true}, history, covered, 1, LanesRecorded},
		{"earlier and not covered is current", &fakeLaneCounter{count: 3, ok: true}, history, uncovered, 3, LanesCurrent},
		{"no history is current", &fakeLaneCounter{count: 2, ok: true}, nil, uncovered, 2, LanesCurrent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lanes, source := NewLaneStamper(c.counter, c.history)(c.at, now)
			if lanes == nil || *lanes != c.want || source != c.source {
				t.Fatalf("got %v %q, want %d %q", lanes, source, c.want, c.source)
			}
		})
	}

	t.Run("no ledger is unavailable", func(t *testing.T) {
		stamp := NewLaneStamper(&fakeLaneCounter{}, history)
		for _, at := range []time.Time{now, uncovered} {
			if lanes, source := stamp(at, now); lanes != nil || source != LanesUnavailable {
				t.Fatalf("at %v got %v %q, want nil unavailable", at, lanes, source)
			}
		}
		// A recorded count does not need the ledger.
		if lanes, source := stamp(covered, now); lanes == nil || *lanes != 1 || source != LanesRecorded {
			t.Fatalf("covered got %v %q, want 1 recorded", lanes, source)
		}
	})

	t.Run("nil counter is unavailable", func(t *testing.T) {
		if lanes, source := NewLaneStamper(nil, nil)(now, now); lanes != nil || source != LanesUnavailable {
			t.Fatalf("got %v %q", lanes, source)
		}
	})

	t.Run("the returned count is a copy", func(t *testing.T) {
		counter := &fakeLaneCounter{count: 3, ok: true}
		stamp := NewLaneStamper(counter, nil)
		a, _ := stamp(now, now)
		*a = 99
		if b, _ := stamp(now, now); *b != 3 {
			t.Fatalf("second stamp = %d", *b)
		}
	})
}
