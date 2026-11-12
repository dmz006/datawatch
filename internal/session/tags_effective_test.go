package session

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestTagEffectiveSetsIncludeInherited checks that the per-session tag lists
// the autonomous counts read hold each session's own tags plus the tags it
// inherits from its Automaton, and nothing else.
func TestTagEffectiveSetsIncludeInherited(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"parent", "child"},
		map[string][]string{"prd-1": {"parent"}})
	saveTagsTestSession(t, mgr, "s1", "", []string{"child"})
	saveTagsTestSession(t, mgr, "s2", "prd-1", nil)
	saveTagsTestSession(t, mgr, "s3", "prd-1", []string{"child"})
	saveTagsTestSession(t, mgr, "s4", "", nil)

	// Each set is sorted and joined so the comparison does not depend on the
	// order of the sessions or of their tags.
	var got []string
	for _, set := range mgr.SessionEffectiveTagSets() {
		sorted := append([]string(nil), set...)
		sort.Strings(sorted)
		got = append(got, strings.Join(sorted, ","))
	}
	sort.Strings(got)
	want := []string{"", "child", "child,parent", "parent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("effective tag sets = %q, want %q", got, want)
	}
}
