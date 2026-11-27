package mergeshadow

import (
	"testing"
	"time"
)

// Git keeps a commit's time to the second, so two hand merges can tie on it.
// The one whose shadow was first stored is then the older, however the keys,
// which start with a random Automaton id, sort. The records are read back as
// stored: the order needs nothing a record saved earlier lacks.
func TestReplaysOfHandMergesInOneSecondKeepCreationOrder(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	same := func([]string) string { return "c1" }
	for name, ids := range map[string][3]string{
		"keys rising with age":  {"c", "b", "a"},
		"keys falling with age": {"a", "b", "c"},
		"keys mixed":            {"b", "c", "a"},
	} {
		t.Run(name, func(t *testing.T) {
			// Oldest first: one second for all three commits, each merge
			// starting at the commit before.
			var made []*Replay
			for i, id := range ids {
				r := matchedReplay(id, at)
				r.CreatedAt = at.Add(time.Duration(i+1) * time.Microsecond)
				r.Hand.Base, r.Hand.Commit = []string{"h0", "h1", "h2"}[i], []string{"h1", "h2", "h3"}[i]
				made = append(made, r)
			}
			dir := t.TempDir()
			for _, r := range made {
				if err := SaveReplay(dir, r); err != nil {
					t.Fatal(err)
				}
				if err := SaveHandChecks(dir, r.Key, r.HandChecks); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := ListReplays(dir)
			if err != nil || len(stored) != 3 {
				t.Fatalf("%v, %d replays", err, len(stored))
			}
			for i, r := range stored {
				if want := ids[2-i]; r.AutomatonID != want {
					t.Errorf("place %d holds %s, want %s", i, r.AutomatonID, want)
				}
			}
			// Whatever order they are handed over in.
			for _, p := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
				var asked string
				s := ReplayStreak([]*Replay{made[p[0]], made[p[1]], made[p[2]]}, "proj", "s1", same,
					func(c string) string { asked = c; return "" })
				if s.Length != 3 || s.StoppedBy != "" || asked != "h3" {
					t.Errorf("given %v: %+v (asked about %q), want a streak of 3 from h3", p, s, asked)
				}
			}
		})
	}
}
