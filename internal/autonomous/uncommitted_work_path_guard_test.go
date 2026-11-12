package autonomous

import (
	"context"
	"path/filepath"
	"testing"
)

func TestClassifyTaskCommitPathsUnsafePaths(t *testing.T) {
	_, wt := uwcWorktree(t)
	other := t.TempDir()
	uwWrite(t, other, "f", "outside\n")
	abs := filepath.Join(other, "f")
	rel, err := filepath.Rel(wt, abs)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, path string }{
		{"parent", "../x"},
		{"absolute into second temp dir", abs},
		{"escape after clean", "a/../../x"},
		{"nul byte", "a\x00b"},
		{"sibling directory file", rel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			add, pathspec, skipped := classifyTaskCommitPaths(context.Background(), wt, []string{tc.path})
			if len(skipped) != 0 {
				t.Errorf("%q must not be skipped, got %v", tc.path, skipped)
			}
			if len(add) != 1 || add[0] != tc.path {
				t.Errorf("%q must go to add unchanged, got %v", tc.path, add)
			}
			if len(pathspec) != 1 || pathspec[0] != tc.path {
				t.Errorf("%q must go to pathspec unchanged, got %v", tc.path, pathspec)
			}
		})
	}
}
