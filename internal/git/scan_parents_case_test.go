package git

import (
	"slices"
	"strings"
	"testing"
)

// git reads the hex digits of a parent id in either case, so commitParents has
// to count an upper-case parent too and return it in the usual lower-case form.
func TestCommitParentsReadsEitherCase(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	raw := "tree " + strings.Repeat("c", 40) + "\nparent " + strings.ToUpper(a) + "\nparent " + b +
		"\nauthor x <x@example.invalid> 0 +0000\n\nmessage\n"
	if got := commitParents(raw); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("parents = %v, want %v", got, []string{a, b})
	}
}
