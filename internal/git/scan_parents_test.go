package git

import (
	"strings"
	"testing"
)

// commitParents counts the parents git itself counts: the consecutive
// "parent <id>" lines directly after the "tree" line, in the order they
// appear, and nothing else in the object.

func TestCommitParentsNormalCommits(t *testing.T) {
	tree := strings.Repeat("c", 40)
	one := strings.Repeat("d", 40)
	two := strings.Repeat("e", 40)

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "no parents",
			raw:  "tree " + tree + "\nauthor x\ncommitter x\n\nmessage\n",
			want: "",
		},
		{
			name: "one parent",
			raw:  "tree " + tree + "\nparent " + one + "\nauthor x\ncommitter x\n\nmessage\n",
			want: one,
		},
		{
			name: "two parents",
			raw:  "tree " + tree + "\nparent " + one + "\nparent " + two + "\nauthor x\ncommitter x\n\nmessage\n",
			want: one + "," + two,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(commitParents(tc.raw), ","); got != tc.want {
				t.Errorf("parents = %q, want %q", got, tc.want)
			}
		})
	}
}

// A hand-written header may repeat "parent" after "author" and "committer";
// git does not read those lines, and neither does the scan.
func TestCommitParentsStopsAtFirstOtherHeaderLine(t *testing.T) {
	tree := strings.Repeat("c", 40)
	one := strings.Repeat("d", 40)
	late := strings.Repeat("f", 40)
	raw := "tree " + tree + "\nparent " + one + "\nauthor x\ncommitter x\nparent " + late + "\n\nmessage\n"
	if got := strings.Join(commitParents(raw), ","); got != one {
		t.Errorf("parents = %q, want %q", got, one)
	}
}

// A "parent" line in the commit message is not a header line.
func TestCommitParentsIgnoresMessageLines(t *testing.T) {
	tree := strings.Repeat("c", 40)
	one := strings.Repeat("d", 40)
	late := strings.Repeat("e", 40)
	raw := "tree " + tree + "\nparent " + one + "\nauthor x\ncommitter x\n\nparent " + late + "\nsecond line\n"
	if got := strings.Join(commitParents(raw), ","); got != one {
		t.Errorf("parents = %q, want %q", got, one)
	}
}
