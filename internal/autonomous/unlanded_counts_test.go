package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestCountUnlandedBranches(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	day, week := 24*time.Hour, 7*24*time.Hour
	br := func(name, rec string, age time.Duration) UnlandedBranch {
		return UnlandedBranch{AutomatonID: name, Branch: "automaton/" + name, Title: "T " + name, Recommendation: rec, LastActivity: now.Add(-age)}
	}
	cases := []struct {
		name     string
		in       []UnlandedBranch
		state    map[string]int
		age      map[string]int
		oldest   string
		oldestAt time.Duration
	}{
		{name: "empty", in: nil,
			state: map[string]int{CountLanded: 0, CountPending: 0, CountLive: 0, CountUnlanded: 0},
			age:   map[string]int{AgeUnderDay: 0, AgeUnderWeek: 0, AgeOlder: 0}},
		{name: "just under one day", in: []UnlandedBranch{br("a", UnlandedLanded, day-time.Second)},
			state: map[string]int{CountLanded: 1}, age: map[string]int{AgeUnderDay: 1}},
		{name: "exactly one day", in: []UnlandedBranch{br("a", UnlandedNeedsDecision, day)},
			state: map[string]int{CountUnlanded: 1}, age: map[string]int{AgeUnderWeek: 1}},
		{name: "just under one week", in: []UnlandedBranch{br("a", UnlandedSuperseded, week-time.Second)},
			state: map[string]int{CountPending: 1}, age: map[string]int{AgeUnderWeek: 1}, oldest: "automaton/a", oldestAt: week - time.Second},
		{name: "exactly one week", in: []UnlandedBranch{br("a", UnlandedSuperseded, week)},
			state: map[string]int{CountPending: 1}, age: map[string]int{AgeOlder: 1}, oldest: "automaton/a", oldestAt: week},
		{name: "mixed with oldest pending", in: []UnlandedBranch{
			br("a", UnlandedSuperseded, 2*day), br("b", UnlandedSuperseded, 9*day),
			br("c", "", time.Hour), br("d", UnlandedNeedsDecision, 30*day),
			{AutomatonID: "e", Branch: "automaton/e", Recommendation: UnlandedLanded},
		},
			state:  map[string]int{CountPending: 2, CountLive: 1, CountUnlanded: 1, CountLanded: 1},
			age:    map[string]int{AgeUnderDay: 1, AgeUnderWeek: 1, AgeOlder: 3},
			oldest: "automaton/b", oldestAt: 9 * day},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := CountUnlandedBranches(tc.in, now)
			if c.Total != len(tc.in) {
				t.Errorf("total = %d, want %d", c.Total, len(tc.in))
			}
			if len(c.ByState) != 4 || len(c.ByAge) != 3 {
				t.Fatalf("missing keys: %v %v", c.ByState, c.ByAge)
			}
			for k, v := range c.ByState {
				if v != tc.state[k] {
					t.Errorf("state %s = %d, want %d", k, v, tc.state[k])
				}
			}
			for k, v := range c.ByAge {
				if v != tc.age[k] {
					t.Errorf("age %s = %d, want %d", k, v, tc.age[k])
				}
			}
			if tc.oldest == "" {
				if c.OldestPending != nil {
					t.Errorf("oldest pending = %+v, want none", c.OldestPending)
				}
				return
			}
			if c.OldestPending == nil || c.OldestPending.Branch != tc.oldest || c.OldestPending.Age != tc.oldestAt {
				t.Errorf("oldest pending = %+v, want %s at %s", c.OldestPending, tc.oldest, tc.oldestAt)
			}
		})
	}
}

func TestRenderUnlandedWorkListCountsFirst(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	l := NewUnlandedWorkList([]UnlandedBranch{{AutomatonID: "a", Branch: "automaton/a", Title: "Fix", Commits: 2,
		Recommendation: UnlandedSuperseded, LastActivity: now.Add(-50 * time.Hour)}}, now)
	out := RenderUnlandedWorkList(l)
	if !strings.HasPrefix(out, "Counts: 0 landed, 1 pending, 0 live, 0 unlanded\nAge: 0 under one day, 1 under one week, 0 older\nOldest pending: Fix (automaton/a), 2d 2h\n") {
		t.Fatalf("counts not first:\n%s", out)
	}
	empty := RenderUnlandedWorkList(NewUnlandedWorkList(nil, now))
	if !strings.HasPrefix(empty, "Counts: 0 landed") || !strings.Contains(empty, "No archived Automaton") {
		t.Fatalf("empty render:\n%s", empty)
	}
}
