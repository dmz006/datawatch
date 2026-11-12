package autonomous

import "testing"

func selection(tags ...string) ObjectTagSelection { return ObjectTagSelection{Tags: tags} }

func TestObjectTagSelectionEmptyMatchesEverything(t *testing.T) {
	sel := ObjectTagSelection{}
	if !sel.MatchesObjectTags([]string{"project:x"}, []string{"t1"}) {
		t.Fatal("an empty selection keeps the object")
	}
	if !sel.MatchesObjectTags(nil, nil) {
		t.Fatal("an empty selection keeps an object with no tags")
	}
}

func TestObjectTagSelectionAny(t *testing.T) {
	sel := selection("project:datawatch", "stage:implement")

	if !sel.MatchesObjectTags([]string{"project:datawatch", "kind:build"}, nil) {
		t.Error("any: one present tag must match")
	}
	if sel.MatchesObjectTags([]string{"project:other"}, nil) {
		t.Error("any: no tag present must not match")
	}
	if !sel.MatchesObjectTags(nil, []string{"stage:implement"}) {
		t.Error("any: a matching managed tag id must match")
	}
	if sel.MatchesObjectTags(nil, []string{"t-abc"}) {
		t.Error("any: a managed id the selection does not name must not match")
	}

	// A selection that names only a managed id matches on that id alone.
	idSel := selection("t-abc")
	if !idSel.MatchesObjectTags(nil, []string{"t-abc", "t-other"}) {
		t.Error("any: a managed id named in the selection must match")
	}
}

func TestObjectTagSelectionAll(t *testing.T) {
	sel := ObjectTagSelection{Tags: []string{"project:datawatch", "automaton:dwa"}, AllMatch: true}

	if !sel.MatchesObjectTags([]string{"project:datawatch", "automaton:dwa", "kind:build"}, nil) {
		t.Error("all: every tag present must match")
	}
	if sel.MatchesObjectTags([]string{"project:datawatch"}, nil) {
		t.Error("all: one tag missing must not match")
	}
	// All-match spanning system tags and managed tag ids.
	sel2 := ObjectTagSelection{Tags: []string{"t-abc", "project:datawatch"}, AllMatch: true}
	if !sel2.MatchesObjectTags([]string{"project:datawatch"}, []string{"t-abc"}) {
		t.Error("all: tags split across systems and ids must all match")
	}
	if sel2.MatchesObjectTags([]string{"project:datawatch"}, []string{"t-other"}) {
		t.Error("all: one id missing must not match")
	}
}

func TestMatchesObjectTagsIgnoresBlank(t *testing.T) {
	sel := selection("  ", "project:datawatch")
	if !sel.MatchesObjectTags([]string{"project:datawatch"}, nil) {
		t.Error("blank tags are ignored in the selection")
	}
	if len(sel.Tags) != 2 {
		t.Errorf("blank tags are kept in the selection (len %d), the matcher ignores them", len(sel.Tags))
	}
}

func TestObjectTagShortcut(t *testing.T) {
	cases := []struct {
		axis, value, want string
	}{
		{"project", "datawatch", "project:datawatch"},
		{"automation", "nightly", "automation:nightly"},
		{"automaton", "dwa", "automaton:dwa"},
		{"", "datawatch", ""},
		{"bogus-axis", "x", ""},
		{"project", "  ", ""},
	}
	for _, c := range cases {
		if got := ObjectTagShortcut(c.axis, c.value); got != c.want {
			t.Errorf("ObjectTagShortcut(%q, %q) = %q, want %q", c.axis, c.value, got, c.want)
		}
	}
}

func TestFilterTasksView(t *testing.T) {
	view := TasksView{Tasks: []TaskEntry{
		{ID: "t1", SystemTags: []string{"stage:implement", "project:datawatch"}},
		{ID: "t2", SystemTags: []string{"stage:review", "project:datawatch"}},
	}}

	if got := FilterTasksView(view, ObjectTagSelection{}); len(got.Tasks) != 2 {
		t.Fatalf("empty selection keeps every task, got %d", len(got.Tasks))
	}

	any := selection("stage:implement")
	one := FilterTasksView(view, any)
	if len(one.Tasks) != 1 || one.Tasks[0].ID != "t1" {
		t.Fatalf("any tag=stage:implement -> %v", one.Tasks)
	}

	// all-match narrows to the task carrying both tags.
	all := ObjectTagSelection{Tags: []string{"stage:implement", "project:datawatch"}, AllMatch: true}
	both := FilterTasksView(view, all)
	if len(both.Tasks) != 1 || both.Tasks[0].ID != "t1" {
		t.Fatalf("all tags -> %v", both.Tasks)
	}

	none := FilterTasksView(view, selection("stage:nope"))
	if len(none.Tasks) != 0 {
		t.Fatalf("no matching task -> %v", none.Tasks)
	}
}

// TestMatchesObjectTagsResults is a table over MatchesObjectTags. Each
// expectation follows the matcher: an empty selection matches everything; the
// object's system tags and managed tag ids are merged into one set; any-match
// is true when one requested tag is in the set; all-match is true only when
// every requested tag is in the set. Duplicates on either side change nothing.
func TestMatchesObjectTagsResults(t *testing.T) {
	cases := []struct {
		name       string
		tags       []string
		allMatch   bool
		systemTags []string
		tagIDs     []string
		want       bool
	}{
		// Any-match rule.
		{"any: one of two present", []string{"a", "b"}, false, []string{"a"}, nil, true},
		{"any: all present", []string{"a", "b"}, false, []string{"a", "b"}, nil, true},
		{"any: none present", []string{"a", "b"}, false, []string{"x", "y"}, []string{"z"}, false},

		// All-match rule.
		{"all: every tag present", []string{"a", "b"}, true, []string{"a", "b"}, nil, true},
		{"all: one tag missing", []string{"a", "b"}, true, []string{"a"}, nil, false},
		{"all: none present", []string{"a", "b"}, true, []string{"x"}, []string{"y"}, false},
		{"all: split across both lists", []string{"a", "b"}, true, []string{"a"}, []string{"b"}, true},

		// Nil sets.
		{"any: nil systemTags, id present", []string{"a"}, false, nil, []string{"a"}, true},
		{"any: nil tagIDs, system tag present", []string{"a"}, false, []string{"a"}, nil, true},
		{"any: both nil", []string{"a"}, false, nil, nil, false},
		{"all: nil systemTags, ids cover all", []string{"a", "b"}, true, nil, []string{"a", "b"}, true},
		{"all: nil tagIDs, system tags cover all", []string{"a", "b"}, true, []string{"a", "b"}, nil, true},
		{"all: nil systemTags, id missing", []string{"a", "b"}, true, nil, []string{"a"}, false},
		{"all: both nil", []string{"a"}, true, nil, nil, false},

		// Empty selection matches everything, rule and sets notwithstanding.
		{"empty selection, any, nil sets", nil, false, nil, nil, true},
		{"empty selection, all, nil sets", nil, true, nil, nil, true},
		{"empty selection, any, tags carried", []string{}, false, []string{"a"}, []string{"b"}, true},
		{"empty selection, all, tags carried", []string{}, true, []string{"a"}, []string{"b"}, true},
		{"empty selection, empty non-nil sets", nil, false, []string{}, []string{}, true},

		// A requested tag present in only one list.
		{"any: only in systemTags", []string{"a"}, false, []string{"a"}, []string{"x"}, true},
		{"any: only in tagIDs", []string{"a"}, false, []string{"x"}, []string{"a"}, true},
		{"all: only in systemTags, other in neither", []string{"a", "b"}, true, []string{"a"}, []string{"x"}, false},
		{"all: only in tagIDs, other in neither", []string{"a", "b"}, true, []string{"x"}, []string{"b"}, false},

		// Duplicates inside the object's two lists.
		{"any: duplicates within systemTags", []string{"a"}, false, []string{"a", "a", "a"}, nil, true},
		{"any: duplicates within tagIDs", []string{"a"}, false, nil, []string{"a", "a"}, true},
		{"any: same tag in both lists", []string{"a"}, false, []string{"a"}, []string{"a"}, true},
		{"any: duplicates of a non-requested tag", []string{"a"}, false, []string{"x", "x"}, []string{"x"}, false},
		{"all: duplicate does not stand in for a missing tag", []string{"a", "b"}, true, []string{"a", "a"}, []string{"a"}, false},
		{"all: duplicates with every tag present", []string{"a", "b"}, true, []string{"a", "a", "b"}, []string{"b", "a"}, true},

		// Duplicates inside the selection.
		{"any: duplicate selection tag, present", []string{"a", "a"}, false, []string{"a"}, nil, true},
		{"any: duplicate selection tag, absent", []string{"a", "a"}, false, []string{"x"}, nil, false},
		{"all: duplicate selection tag, present", []string{"a", "a"}, true, []string{"a"}, nil, true},
		{"all: duplicate selection tag plus a missing tag", []string{"a", "a", "b"}, true, []string{"a"}, nil, false},
		{"all: duplicate selection tags, all present", []string{"a", "a", "b", "b"}, true, []string{"a"}, []string{"b"}, true},

		// A selection with no present tag.
		{"any: no present tag, empty non-nil sets", []string{"a", "b"}, false, []string{}, []string{}, false},
		{"all: no present tag, empty non-nil sets", []string{"a", "b"}, true, []string{}, []string{}, false},
		{"any: no present tag, near-miss strings", []string{"project:datawatch"}, false, []string{"project:datawatch2", "Project:datawatch"}, []string{"project"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sel := ObjectTagSelection{Tags: c.tags, AllMatch: c.allMatch}
			if got := sel.MatchesObjectTags(c.systemTags, c.tagIDs); got != c.want {
				t.Errorf("MatchesObjectTags(%q, %q) with selection %q (all=%v) = %v, want %v",
					c.systemTags, c.tagIDs, c.tags, c.allMatch, got, c.want)
			}
		})
	}
}
