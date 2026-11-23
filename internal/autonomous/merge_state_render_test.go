package autonomous

import (
	"reflect"
	"testing"
)

func TestMergeStateLineText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{MergeStateFinishedNotMerged, "branch work not merged into the base branch (merge_state: finished_not_merged)"},
		{"some_future_value", "merge_state: some_future_value"},
	}
	for _, tc := range cases {
		if got := MergeStateLineText(tc.in); got != tc.want {
			t.Errorf("MergeStateLineText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMergeStateGetLine(t *testing.T) {
	if got := MergeStateGetLine([]byte(`{"id":"p1"}`)); got != "" {
		t.Errorf("body without the field yields a line: %q", got)
	}
	if got := MergeStateGetLine([]byte(`{"id":"p1","merge_state":"finished_not_merged"}`)); got != "branch work not merged into the base branch (merge_state: finished_not_merged)" {
		t.Errorf("get line wrong: %q", got)
	}
	if got := MergeStateGetLine([]byte(`not json`)); got != "" {
		t.Errorf("bad body yields a line: %q", got)
	}
}

func TestMergeStateListLines(t *testing.T) {
	got := MergeStateListLines([]byte(`{` +
		`"prds":[` +
		`{"id":"p1","title":"Alpha","merge_state":"finished_not_merged"},` +
		`{"id":"p2","title":"Beta"},` +
		`{"id":"p3","merge_state":"finished_not_merged"}` +
		`]}`))
	want := []string{
		"Alpha (p1): branch work not merged into the base branch (merge_state: finished_not_merged)",
		"p3: branch work not merged into the base branch (merge_state: finished_not_merged)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list lines = %v, want %v", got, want)
	}
	if got := MergeStateListLines([]byte(`{"prds":[{"id":"p1"}]}`)); got != nil {
		t.Errorf("list without the field = %v, want nil", got)
	}
	if got := MergeStateListLines([]byte(`nope`)); got != nil {
		t.Errorf("bad body = %v, want nil", got)
	}
}
