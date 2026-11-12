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

func TestClassifyTaskCommitPathsPathGuardTable(t *testing.T) {
	_, wt := uwcWorktree(t)
	uwWrite(t, wt, "inside.txt", "in\n")
	cases := []struct {
		name, path string
		wantAdd    bool
		wantSkip   bool
	}{
		{"traversal", "../escape.txt", true, false},
		{"nul byte", "a\x00b", true, false},
		{"in base", "inside.txt", true, false},
		{"in base missing", "nowhere.txt", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			add, pathspec, skipped := classifyTaskCommitPaths(context.Background(), wt, []string{tc.path})
			if tc.wantAdd && (len(add) != 1 || len(pathspec) != 1) {
				t.Errorf("want add+pathspec, got add=%v pathspec=%v", add, pathspec)
			}
			if tc.wantSkip && len(skipped) != 1 {
				t.Errorf("want skipped, got %v", skipped)
			}
			if !tc.wantSkip && len(skipped) != 0 {
				t.Errorf("must not skip, got %v", skipped)
			}
		})
	}
}

func TestIsDaemonIgnoreChangePathGuardTable(t *testing.T) {
	dir := t.TempDir()
	body := "x\n" + daemonIgnoreComment + "\n" + daemonIgnoreDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, d string, args ...string) (string, error) { return "x\n", nil }
	cases := []struct {
		name, rel string
		want      bool
	}{
		{"traversal", "../.gitignore", false},
		{"nul byte", ".gitignore\x00x", false},
		{"in base", ".gitignore", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDaemonIgnoreChange(context.Background(), dir, tc.rel, run); got != tc.want {
				t.Errorf("%q = %v, want %v", tc.rel, got, tc.want)
			}
		})
	}
}
