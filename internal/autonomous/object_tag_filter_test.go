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
