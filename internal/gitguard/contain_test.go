package gitguard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWithin_ValidPaths(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"a", "a/b", "a/b/", "./a", "new/dir/not/yet/created", "a//b"} {
		got, err := ResolveWithin(base, rel)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", rel, err)
		}
		if want := filepath.Join(base, filepath.Clean(rel)); got != want {
			t.Fatalf("%q: got %q want %q", rel, got, want)
		}
	}
}

func TestResolveWithin_RejectsTraversalAndAbsolute(t *testing.T) {
	base := t.TempDir()
	for _, rel := range []string{"..", "../x", "a/../../x", "a/../b", "../../etc/passwd", "a/..", "/etc/passwd", base, "a\x00b"} {
		if _, err := ResolveWithin(base, rel); !errors.Is(err, ErrPathEscapes) {
			t.Fatalf("%q: want ErrPathEscapes, got %v", rel, err)
		}
	}
}

func TestResolveWithin_RejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skip(err)
	}
	for _, rel := range []string{"link", "link/sub", "link/not/yet/created"} {
		if _, err := ResolveWithin(base, rel); !errors.Is(err, ErrPathEscapes) {
			t.Fatalf("%q: want ErrPathEscapes, got %v", rel, err)
		}
	}
}

func TestResolveWithin_AllowsSymlinkInsideBase(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "alias")); err != nil {
		t.Skip(err)
	}
	if _, err := ResolveWithin(base, "alias/sub"); err != nil {
		t.Fatalf("inside link: %v", err)
	}
}

func TestResolveWithin_SymlinkedBase(t *testing.T) {
	real := t.TempDir()
	linkBase := filepath.Join(t.TempDir(), "base")
	if err := os.Symlink(real, linkBase); err != nil {
		t.Skip(err)
	}
	if _, err := ResolveWithin(linkBase, "child"); err != nil {
		t.Fatalf("symlinked base: %v", err)
	}
}

func TestResolveWithin_MissingBase(t *testing.T) {
	_, err := ResolveWithin(filepath.Join(t.TempDir(), "absent"), "x")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	if _, err := ResolveWithin("", "x"); err == nil {
		t.Fatal("empty base must fail")
	}
}
