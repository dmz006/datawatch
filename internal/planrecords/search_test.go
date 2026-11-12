package planrecords

import (
	"fmt"
	"strings"
	"testing"
)

func TestListPagedNewestFirst(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	var ids []string
	for i := 0; i < 7; i++ {
		ids = append(ids, mustCreate(t, s, row(fmt.Sprintf("Item %d", i))).ID)
	}
	body := "touched"
	if _, err := s.Update(ids[0], Patch{Body: &body}); err != nil { // oldest becomes newest
		t.Fatal(err)
	}
	all, total := s.List(Filter{})
	if total != 7 || len(all) != 7 || all[0].ID != ids[0] || all[1].ID != ids[6] {
		t.Fatalf("order wrong: total=%d first=%s", total, all[0].ID)
	}
	p1, total := s.List(Filter{Limit: 3})
	p2, _ := s.List(Filter{Limit: 3, Offset: 3})
	p3, _ := s.List(Filter{Limit: 3, Offset: 6})
	if total != 7 || len(p1) != 3 || len(p2) != 3 || len(p3) != 1 || p2[0].ID != all[3].ID {
		t.Fatal("paging wrong")
	}
	if none, tot := s.List(Filter{Offset: 100}); len(none) != 0 || tot != 7 {
		t.Fatal("offset past the end")
	}
	if neg, _ := s.List(Filter{Offset: -5, Limit: 1}); len(neg) != 1 || neg[0].ID != ids[0] {
		t.Fatal("negative offset")
	}
	if big, _ := s.List(Filter{Limit: 100000}); len(big) != 7 {
		t.Fatal("limit cap")
	}
	// returned records are copies
	all[0].Title = "mutated"
	if again, _ := s.List(Filter{Limit: 1}); again[0].Title == "mutated" {
		t.Fatal("List leaks index memory")
	}
}

func seedSearch(t *testing.T) *Store {
	s, _, _, _ := newTestStore(t, nil)
	mk := func(r Record) Record { return mustCreate(t, s, r) }
	mk(Record{Kind: KindRegistryRow, Project: "datawatch", Title: "Registry move", Body: "plan for the board", LegacyNumber: 10, ShippedBy: "auto-a"})
	mk(Record{Kind: KindRegistryRow, Project: "datawatch", Title: "Move the board", Body: "registry notes"})
	mk(Record{Kind: KindRegistryRow, Project: "datawatch", Title: "Unrelated", Body: "talks about the registry move in passing", LegacyNumber: 11})
	al := mk(Record{Kind: KindFinding, Project: "datawatch-app", Title: "Crash", RaisedBy: "auto-b", Links: []Link{{"automaton", "auto-c"}}})
	if _, err := s.Update(al.ID, Patch{AddAliases: []string{"Registry crash"}}); err != nil {
		t.Fatal(err)
	}
	st := StateTriaged
	if _, err := s.Update(al.ID, Patch{State: &st}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSearchRankingTitleFirst(t *testing.T) {
	s := seedSearch(t)
	hits, total := s.Search("REGISTRY", Filter{})
	if total != 4 || len(hits) != 4 {
		t.Fatalf("total %d", total)
	}
	order := []string{"Registry move", "Move the board", "Registry crash", "Unrelated"}
	// title prefix, then body hit (newer), then ... compute: title(80) > alias(40) > body(20)
	want := []string{"Registry move", "Crash", "Unrelated", "Move the board"}
	_ = order
	for i, w := range want {
		if hits[i].Record.Title != w {
			t.Fatalf("rank %d = %q, want %q (%+v)", i, hits[i].Record.Title, w, titles(hits))
		}
	}
	if hits[0].Matched != MatchTitle || hits[1].Matched != MatchAlias || hits[2].Matched != MatchBody {
		t.Fatalf("matched labels %v", hits)
	}
	if !(hits[0].Score > hits[1].Score && hits[1].Score > hits[2].Score) {
		t.Fatalf("scores %v", hits)
	}
	// exact > prefix > contains
	ex, _ := s.Search("crash", Filter{})
	if ex[0].Score != scoreTitleExact {
		t.Fatalf("exact score %d", ex[0].Score)
	}
	has, _ := s.Search("the board", Filter{})
	if has[0].Record.Title != "Move the board" || has[0].Score != scoreTitleHas {
		t.Fatalf("contains: %+v", has[0])
	}
}

func titles(h []Hit) []string {
	var o []string
	for _, x := range h {
		o = append(o, x.Record.Title)
	}
	return o
}

func TestSearchMultiWordAndEmpty(t *testing.T) {
	s := seedSearch(t)
	hits, _ := s.Search("registry move", Filter{})
	if len(hits) != 3 || hits[0].Record.Title != "Registry move" {
		t.Fatalf("%v", titles(hits))
	}
	if hits[0].Score != scoreTitleExact {
		t.Fatalf("whole phrase equal to the title should score as an exact hit, got %d", hits[0].Score)
	}
	if none, total := s.Search("registry zebra", Filter{}); len(none) != 0 || total != 0 {
		t.Fatal("every word must match")
	}
	all, total := s.Search("   ", Filter{})
	if total != 4 || len(all) != 4 || all[0].Matched != MatchAll {
		t.Fatalf("empty query lists everything: %d", total)
	}
	paged, total := s.Search("registry", Filter{Limit: 2, Offset: 1})
	if len(paged) != 2 || total != 4 {
		t.Fatal("search paging")
	}
	// returned hits are copies
	hits[0].Record.Aliases = append(hits[0].Record.Aliases, "x")
}

func TestSearchAndListFilters(t *testing.T) {
	s := seedSearch(t)
	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"kind finding", Filter{Kind: KindFinding}, 1},
		{"kind row", Filter{Kind: KindRegistryRow}, 3},
		{"project", Filter{Project: "datawatch"}, 3},
		{"state", Filter{State: StateTriaged}, 1},
		{"legacy number", Filter{LegacyNumber: 11}, 1},
		{"legacy number none", Filter{LegacyNumber: 99}, 0},
		{"automaton shipped_by", Filter{Automaton: "auto-a"}, 1},
		{"automaton raised_by", Filter{Automaton: " auto-b "}, 1},
		{"automaton link", Filter{Automaton: "auto-c"}, 1},
		{"automaton none", Filter{Automaton: "nobody"}, 0},
		{"combined", Filter{Kind: KindRegistryRow, Project: "datawatch", State: StateOpen}, 3},
		{"combined miss", Filter{Kind: KindFinding, Project: "datawatch"}, 0},
	}
	for _, c := range cases {
		if _, total := s.List(c.f); total != c.want {
			t.Errorf("List %s: %d want %d", c.name, total, c.want)
		}
		if _, total := s.Search("", c.f); total != c.want {
			t.Errorf("Search %s: %d want %d", c.name, total, c.want)
		}
	}
	if hits, _ := s.Search("registry", Filter{Kind: KindFinding}); len(hits) != 1 || hits[0].Record.Title != "Crash" {
		t.Fatal("search honours the filter")
	}
}

func TestParsePolicy(t *testing.T) {
	for in, want := range map[string]ConflictPolicy{"": PolicySuffix, " Suffix ": PolicySuffix, "RENAME": PolicyRename, "skip": PolicySkip} {
		if got, err := ParsePolicy(in); err != nil || got != want {
			t.Errorf("ParsePolicy(%q)=%v,%v", in, got, err)
		}
	}
	if _, err := ParsePolicy("overwrite"); err == nil || !strings.Contains(err.Error(), "suffix, rename or skip") {
		t.Fatal("bad policy must be refused")
	}
}

func TestPreviewConflicts(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	a := row("Taken Title")
	a.LegacyNumber = 3
	mustCreate(t, s, a)
	mustCreate(t, s, row("Taken Title (2)"))
	before := s.Len()

	proposed := []Record{
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Fresh"},                         // 0 clean
		{Kind: KindRegistryRow, Project: "datawatch", Title: " taken title "},                 // 1 title vs store, no number -> (2) is taken -> (3)
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Taken Title", LegacyNumber: 40}, // 2 title vs store, suffix by row number
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Fresh"},                         // 3 clashes with proposal 0
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Other", LegacyNumber: 3},        // 4 row 3 already imported
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Twin A", LegacyNumber: 50},      // 5 clean
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Twin B", LegacyNumber: 50},      // 6 number clashes with proposal 5
		{Kind: KindRegistryRow, Project: "datawatch-app", Title: "Taken Title"},               // 7 other project: clean
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Fresh"},                         // 8 clashes with 0; suffix (2); then...
		{Kind: KindRegistryRow, Project: "datawatch", Title: "Fresh (2)"},                     // 9 clashes with the suffix just handed to 8
	}
	pv := s.PreviewConflicts(proposed, "")
	if s.Len() != before {
		t.Fatal("preview must not write")
	}
	if pv.Policy != PolicySuffix || pv.Total != 10 || pv.Clean != 3 || len(pv.Conflicts) != 7 {
		t.Fatalf("preview %+v", pv)
	}
	by := map[int]Conflict{}
	for _, c := range pv.Conflicts {
		by[c.Index] = c
	}
	c1 := by[1]
	if c1.Reason != ConflictTitle || c1.Existing == nil || c1.Existing.Title != "Taken Title" || c1.Applied.NewTitle != "taken title (3)" || !c1.Applied.NeedsReview {
		t.Fatalf("c1 %+v", c1)
	}
	if got := by[2].Applied.NewTitle; got != "Taken Title (row 40)" {
		t.Fatalf("c2 suffix %q", got)
	}
	if c := by[3]; c.Existing != nil || c.EarlierIndex != 0 || c.Applied.NewTitle != "Fresh (2)" {
		t.Fatalf("c3 %+v", c)
	}
	if c := by[4]; c.Reason != ConflictLegacyNumber || !c.Applied.Skip || c.Existing == nil || c.Existing.LegacyNumber != 3 {
		t.Fatalf("c4 %+v", c)
	}
	for pol, r := range by[4].Resolutions {
		if !r.Skip {
			t.Errorf("a repeated row number skips under %s", pol)
		}
	}
	if c := by[6]; c.Reason != ConflictLegacyNumber || c.EarlierIndex != 5 || !c.Applied.Skip {
		t.Fatalf("c6 %+v", c)
	}
	if c := by[8]; c.Applied.NewTitle != "Fresh (3)" {
		t.Fatalf("c8 must not reuse the suffix handed to c3: %+v", c)
	}
	if c := by[9]; c.EarlierIndex != 3 || c.Applied.NewTitle != "Fresh (2) (2)" {
		t.Fatalf("c9 collides with the suffix given to c3: %+v", c)
	}
	// every conflict reports all three policies
	for _, c := range pv.Conflicts {
		if len(c.Resolutions) != 3 {
			t.Fatalf("resolutions %v", c.Resolutions)
		}
	}
	if r := by[1].Resolutions[PolicyRename]; !r.RequiresInput || r.NewTitle != "" || r.Skip {
		t.Fatalf("rename %+v", r)
	}
	if r := by[1].Resolutions[PolicySkip]; !r.Skip {
		t.Fatalf("skip %+v", r)
	}
	// the policy that was asked for is the one applied
	sk := s.PreviewConflicts(proposed[1:2], PolicySkip)
	if sk.Policy != PolicySkip || !sk.Conflicts[0].Applied.Skip {
		t.Fatalf("%+v", sk)
	}
	rn := s.PreviewConflicts(proposed[1:2], PolicyRename)
	if !rn.Conflicts[0].Applied.RequiresInput {
		t.Fatalf("%+v", rn)
	}
	// an empty batch is a valid preview with a non-nil conflict list
	if e := s.PreviewConflicts(nil, ""); e.Total != 0 || e.Conflicts == nil || len(e.Conflicts) != 0 {
		t.Fatalf("%+v", e)
	}
	// the store's configured default policy is honoured
	s2, err := Open(dir, nil, Options{ImportConflictPolicy: PolicySkip})
	if err != nil {
		t.Fatal(err)
	}
	if p := s2.PreviewConflicts(proposed[1:2], ""); p.Policy != PolicySkip {
		t.Fatalf("default policy %v", p.Policy)
	}
}
