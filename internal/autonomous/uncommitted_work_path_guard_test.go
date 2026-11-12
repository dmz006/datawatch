package autonomous

import (
	"context"
	"os"
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

func TestIsDaemonIgnoreChangeUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	abs := filepath.Join(other, ".gitignore")
	if err := os.WriteFile(abs, []byte("x\n"+daemonIgnoreComment+"\n"+daemonIgnoreDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, d string, args ...string) (string, error) { return "x\n", nil }
	for _, rel := range []string{"../.gitignore", abs, ".gitignore\x00x"} {
		if isDaemonIgnoreChange(context.Background(), dir, rel, run) {
			t.Errorf("%q must be false", rel)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("x\n"+daemonIgnoreComment+"\n"+daemonIgnoreDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isDaemonIgnoreChange(context.Background(), dir, ".gitignore", run) {
		t.Error("daemon-appended lines on a normal path must be true")
	}
}
