package planrecords

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMigratedRowTitleTable(t *testing.T) {
	cases := []struct {
		name   string
		title  string
		body   string
		num    int
		want   string
		source string
	}{
		{"plain title", "Project-tree write lock", "", 8, "Plan: Project-tree write lock", TitleDerived},
		{"status note shipped", "done, shipped in 9.5.92 (PRD 332c0001)", "Status: not started", 15, "Plan: (untitled row 15)", TitleFellBack},
		{"prd led note", "PRD d1160001 (Claude); operator decisions Q1 and Q7 recorded", "", 20, "Plan: (untitled row 20)", TitleFellBack},
		{"done with sub items", "done: 3a shipped in 9.5.85 (PRD b3ca0001), 3b shipped in 9.5.87 (PRD 412a0001)", "", 13, "Plan: (untitled row 13)", TitleFellBack},
		{"status word then real title", "Done: Fix the retry SHA", "", 3, "Plan: Fix the retry SHA", TitleDerived},
		{"bold status word then title", "**Failed** - tiering eval", "", 14, "Plan: tiering eval", TitleDerived},
		{"title from description column", "shipped in 9.5.1 (PRD abcd1234)", "Description: Retry budget for infra failures\nStatus: done", 4, "Plan: Retry budget for infra failures", TitleDerived},
		{"status column text is never a title", "done, shipped in 9.5.88 (PRD 8afa0001)", "Status: **done**, shipped\nDepends on: 12", 16, "Plan: (untitled row 16)", TitleFellBack},
		{"version led", "9.5.4 and later", "", 9, "Plan: (untitled row 9)", TitleFellBack},
		{"two lines collapse", "Line one\n  and two", "", 2, "Plan: Line one and two", TitleDerived},
		{"empty", "", "", 7, "Plan: (untitled row 7)", TitleFellBack},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Record{ID: "row-key", Title: c.title, Body: c.body, LegacyNumber: c.num}
			got, src := migratedRowTitle(r)
			if got != c.want || src != c.source {
				t.Fatalf("got %q (%s), want %q (%s)", got, src, c.want, c.source)
			}
		})
	}
}

func TestMigratedRowTitleLimits(t *testing.T) {
	long := strings.Repeat("alpha beta ", 40)
	got, _ := migratedRowTitle(Record{Title: long, LegacyNumber: 1})
	if n := utf8.RuneCountInString(got); n > maxMigratedTitleRunes || strings.ContainsAny(got, "\n\t") {
		t.Fatalf("title %q is %d runes", got, n)
	}
	got, src := migratedRowTitle(Record{ID: "some-key", Title: "done"})
	if src != TitleFellBack || got != "Plan: (untitled row some-key)" {
		t.Fatalf("no row number: %q %s", got, src)
	}
}

func TestMigrateRowTitlesCountedAndKeepStatus(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	good := row("Persisted subscriptions")
	good.LegacyNumber = 11
	good.State = StateDone
	good.Links = []Link{{Kind: "automaton", Ref: "aaaa1111"}}
	mustCreate(t, s, good)
	bad := row("done, shipped in 9.5.92 (PRD 332c0001)")
	bad.LegacyNumber = 15
	bad.State = StateDone
	bad.Links = []Link{{Kind: "automaton", Ref: "bbbb2222"}}
	mustCreate(t, s, bad)
	bad2 := row("PRD d1160001 (Claude); operator decisions Q1 and Q7 recorded")
	bad2.LegacyNumber = 20
	bad2.Links = []Link{{Kind: "automaton", Ref: "cccc3333"}}
	mustCreate(t, s, bad2)
	resolve := func(id string) (MigrateRef, bool) {
		return MigrateRef{Kind: "automaton", Title: "x"}, true
	}
	prev, err := Migrate(MigrateOptions{Store: s, Resolve: resolve})
	if err != nil || prev.TitlesDerived != 1 || prev.TitlesFellBack != 2 {
		t.Fatalf("preview: %v derived=%d fellback=%d", err, prev.TitlesDerived, prev.TitlesFellBack)
	}
	opts := MigrateOptions{Store: s, Apply: true, Resolve: resolve}
	rep, err := Migrate(opts)
	if err != nil || rep.TitlesDerived != 1 || rep.TitlesFellBack != 2 {
		t.Fatalf("apply: %v %+v", err, rep.Counts())
	}
	p, ok := s.PlanOf("bbbb2222")
	if !ok || p.Title != "Plan: (untitled row 15)" || p.State != StateDone {
		t.Fatalf("fallback plan = %+v %v", p, ok)
	}
	if q, ok := s.PlanOf("aaaa1111"); !ok || q.Title != "Plan: Persisted subscriptions" || q.State != StateDone {
		t.Fatalf("derived plan = %+v %v", q, ok)
	}
	// A plan made earlier is never renamed; a second apply changes nothing
	// and counts nothing.
	renamed := "Hand renamed"
	if _, err := s.Update(p.UID, Patch{Title: &renamed}); err != nil {
		t.Fatal(err)
	}
	n := s.Len()
	again, err := Migrate(opts)
	if err != nil || s.Len() != n || len(again.WouldCreate) != 0 || again.TitlesDerived != 0 || again.TitlesFellBack != 0 {
		t.Fatalf("second apply: %v len %d -> %d %+v", err, n, s.Len(), again.Counts())
	}
	if q, _ := s.PlanOf("bbbb2222"); q.Title != "Hand renamed" {
		t.Fatalf("plan was renamed to %q", q.Title)
	}
}
