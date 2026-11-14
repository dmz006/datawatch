package planrecords

import (
	"strings"
	"testing"
)

func TestParseStateList(t *testing.T) {
	rows := []string{string(StateOpen), string(StatePlanned), string(StateInProgress), string(StateBlocked), string(StateShipped), string(StateDone), string(StateDeferred), string(StateCancelled)}
	findings := []string{string(StateOpen), string(StateTriaged), string(StateFixed), string(StateWontfix), string(StateDuplicate)}
	valid := func(sts []string) []string { return sts }

	t.Run("no state is no filter", func(t *testing.T) {
		for _, raw := range []string{"", "   ", " , ,, "} {
			got, err := ParseStateList(raw, "state", valid(rows))
			if err != nil {
				t.Fatalf("%q: %v", raw, err)
			}
			if got != nil {
				t.Fatalf("%q: want no states, got %v", raw, got)
			}
		}
	})

	t.Run("one state behaves as before", func(t *testing.T) {
		got, err := ParseStateList("open", "state", valid(rows))
		if err != nil || len(got) != 1 || got[0] != "open" {
			t.Fatalf("got %v, %v", got, err)
		}
		// Surrounding whitespace was always trimmed on the single form.
		got, err = ParseStateList("  blocked  ", "state", valid(rows))
		if err != nil || len(got) != 1 || got[0] != "blocked" {
			t.Fatalf("got %v, %v", got, err)
		}
	})

	t.Run("several states trims and dedupes in place", func(t *testing.T) {
		got, err := ParseStateList("open, blocked , open ,shipped", "state", valid(rows))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"open", "blocked", "shipped"}
		if len(got) != len(want) {
			t.Fatalf("want %v, got %v", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("want %v, got %v", want, got)
			}
		}
	})

	t.Run("an unknown state is refused naming the valid ones", func(t *testing.T) {
		for _, raw := range []string{"nope", "open, nope", "  SHIPPED"} {
			_, err := ParseStateList(raw, "state", valid(rows))
			if err == nil {
				t.Fatalf("%q: want an error", raw)
			}
			if !strings.Contains(err.Error(), `" is not a valid state; use one of`) {
				t.Fatalf("%q: error must name the parameter: %v", raw, err)
			}
			for _, name := range []string{"open", "cancelled", "shipped"} {
				if !strings.Contains(err.Error(), name) {
					t.Fatalf("%q: error must name the valid %q: %v", raw, name, err)
				}
			}
		}
		if _, err := ParseStateList("nope", "state", valid(rows)); !strings.Contains(err.Error(), "nope") {
			t.Fatalf("error must name the refused token: %v", err)
		}
		// Finding states are their own vocabulary.
		got, err := ParseStateList("fixed", "state", valid(findings))
		if err != nil || len(got) != 1 || got[0] != "fixed" {
			t.Fatalf("got %v, %v", got, err)
		}
		_, err = ParseStateList("shipped", "state", valid(findings))
		if err == nil {
			t.Fatal("a row state must not pass for a finding")
		}
	})
}

func TestFilterStates(t *testing.T) {
	newRec := func(title string, kind Kind, st State) Record {
		return Record{Kind: kind, Project: "p", Title: title, State: st, UpdatedAt: "t"}
	}
	rows := []Record{
		newRec("Open row", KindRegistryRow, StateOpen),
		newRec("Blocked row", KindRegistryRow, StateBlocked),
		newRec("Shipped row", KindRegistryRow, StateShipped),
		newRec("Open finding", KindFinding, StateOpen),
	}
	seed := func(t *testing.T) *Store {
		s, _, _, _ := newTestStore(t, nil)
		for _, r := range rows {
			if _, err := s.Create(r); err != nil {
				t.Fatalf("create %q: %v", r.Title, err)
			}
		}
		return s
	}

	t.Run("no state admits all; single State is as before", func(t *testing.T) {
		s := seed(t)
		if got, total := s.List(Filter{}); total != 4 || len(got) != 4 {
			t.Fatalf("no state: want 4, got %d (%d)", len(got), total)
		}
		if _, total := s.List(Filter{State: StateOpen}); total != 2 {
			t.Fatalf("State: want 2, got %d", total)
		}
	})

	t.Run("a set of states admits each of them", func(t *testing.T) {
		s := seed(t)
		if got, total := s.List(Filter{States: []State{StateOpen, StateBlocked}}); total != 3 || len(got) != 3 {
			t.Fatalf("States open+blocked: want 3, got %d (%d)", total, len(got))
		}
		if _, total := s.List(Filter{States: []State{StateBlocked, StateShipped, StateOpen}}); total != 4 {
			t.Fatalf("three states: want 4, got %d", total)
		}
		// Search and Browse take the same set.
		if _, total := s.Search("", Filter{States: []State{StateOpen, StateBlocked}}); total != 3 {
			t.Fatalf("search with States: want 3, got %d", total)
		}
		if got, total := s.Browse(BrowseFilter{States: []State{StateShipped}}); total != 1 || len(got) != 1 {
			t.Fatalf("browse with States: want 1, got %d (%d)", total, len(got))
		}
		if _, total := s.Browse(BrowseFilter{}); total != 4 {
			t.Fatalf("browse without a state: want 4, got %d", total)
		}
		if _, total := s.ListSummaries(SummaryFilter{States: []State{StateOpen, StateBlocked}}); total != 3 {
			t.Fatalf("summaries with States: want 3, got %d", total)
		}
	})
}
