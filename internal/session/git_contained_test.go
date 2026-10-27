package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
)

func TestContainedInDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := root + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sibling) })
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		want    string
		escapes bool
	}{
		{"root itself", root, root, false},
		{"child directory", filepath.Join(root, "sub"), filepath.Join(root, "sub"), false},
		{"missing child", filepath.Join(root, "nope", "deeper"), filepath.Join(root, "nope", "deeper"), false},
		{"dotdot is cleaned before the check", filepath.Join(root, "sub", "..", "sub"), filepath.Join(root, "sub"), false},
		{"dotdot out of root", filepath.Join(root, "..", filepath.Base(sibling)), "", true},
		{"sibling sharing the root name as prefix", sibling, "", true},
		{"unrelated directory", outside, "", true},
		{"symlink leaving root", filepath.Join(root, "escape", "file"), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := containedInDir(root, tc.path)
			if tc.escapes {
				if !errors.Is(err, gitguard.ErrPathEscapes) {
					t.Fatalf("containedInDir(%q) = %q, %v; want ErrPathEscapes", tc.path, got, err)
				}
				if got != "" {
					t.Errorf("a refused path was returned: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("containedInDir(%q): %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("containedInDir(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// A write-scope entry that is outside the repository, or a symlink out of it,
// must not be stat'ed or staged; the in-scope entry still is.
func TestStageChanges_ScopeEntriesAreContained(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	inDir := filepath.Join(dir, "in")
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inDir, "keep.txt"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "leak.txt"), []byte("out"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := g.stageChanges([]string{outside, link, inDir}); err != nil {
		t.Fatalf("stageChanges: %v", err)
	}
	staged, err := gitOutput(dir, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if staged != "in/keep.txt\n" {
		t.Errorf("staged = %q, want only in/keep.txt", staged)
	}
}

// With a run start set, an untracked file reached through a symlink that
// leaves the repository is skipped rather than stat'ed; real run output is kept.
func TestUnscopedChangedFiles_SymlinkOutOfRepoSkipped(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "elsewhere.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := g.unscopedChangedFiles(time.Now().Add(-time.Hour))
	has := map[string]bool{}
	for _, f := range got {
		has[f] = true
	}
	if !has["fresh.txt"] {
		t.Errorf("run output missing from %v", got)
	}
	if has["linkdir/elsewhere.txt"] {
		t.Errorf("file behind an out-of-repo symlink was kept: %v", got)
	}
}
