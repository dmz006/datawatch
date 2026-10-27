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

func TestResolveWithin_DanglingSymlink(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(base, "dangle")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("../escape-missing", filepath.Join(base, "reldangle")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("inner-missing", filepath.Join(base, "okdangle")); err != nil {
		t.Skip(err)
	}
	for _, rel := range []string{"dangle", "dangle/sub", "reldangle"} {
		if _, err := ResolveWithin(base, rel); !errors.Is(err, ErrPathEscapes) {
			t.Fatalf("%q: want ErrPathEscapes, got %v", rel, err)
		}
	}
	if _, err := ResolveWithin(base, "okdangle"); err != nil {
		t.Fatalf("dangling link that stays inside base refused: %v", err)
	}
}

func TestResolveWithin_SymlinkLoop(t *testing.T) {
	base := t.TempDir()
	if err := os.Symlink("b", filepath.Join(base, "a")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("a", filepath.Join(base, "b")); err != nil {
		t.Skip(err)
	}
	if _, err := ResolveWithin(base, "a"); err == nil {
		t.Fatal("symlink loop accepted")
	}
}

func TestCleanProjectDir(t *testing.T) {
	base := t.TempDir()
	got, err := CleanProjectDir(base + "/a/../b/./")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "b"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	for _, bad := range []string{"", "  ", "/", "/tmp/x\x00y"} {
		if _, err := CleanProjectDir(bad); err == nil {
			t.Errorf("CleanProjectDir(%q) accepted", bad)
		}
	}
	rel, err := CleanProjectDir("sub")
	if err != nil || !filepath.IsAbs(rel) {
		t.Fatalf("relative not made absolute: %q %v", rel, err)
	}
}

func TestContainedPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	// inside -> outside, inside -> inside, plus dangling variants of both.
	links := map[string]string{
		"out":         outside,
		"in":          filepath.Join(root, "a"),
		"dangleout":   filepath.Join(outside, "missing"),
		"dangleoutrl": "../escape-missing",
		"dangle-in":   filepath.Join(root, "a", "missing"),
		"danglein-rl": "inner-missing",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skip(err)
		}
	}
	sibling := root + "-sibling"

	ok := []struct{ name, path string }{
		{"root itself", root},
		{"root with trailing slash", root + string(filepath.Separator)},
		{"existing child", filepath.Join(root, "a", "b")},
		{"missing tail", filepath.Join(root, "new", "dir")},
		{"dot-dot that stays inside", filepath.Join(root, "a", "..", "a", "b")},
		{"symlink inside pointing inside", filepath.Join(root, "in", "b")},
		{"dangling symlink pointing inside", filepath.Join(root, "dangle-in")},
		{"dangling relative symlink pointing inside", filepath.Join(root, "danglein-rl")},
	}
	for _, c := range ok {
		got, err := ContainedPath(root, c.path)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		if want, _ := filepath.Abs(c.path); got != want {
			t.Fatalf("%s: got %q want %q", c.name, got, want)
		}
	}

	bad := []struct{ name, path string }{
		{"symlink inside pointing outside", filepath.Join(root, "out")},
		{"path below a symlink pointing outside", filepath.Join(root, "out", "sub", "not-yet")},
		{"dot-dot traversal", filepath.Join(root, "..", filepath.Base(outside))},
		{"dot-dot traversal after a child", filepath.Join(root, "a") + "/../../etc"},
		{"dangling symlink pointing outside", filepath.Join(root, "dangleout")},
		{"dangling relative symlink pointing outside", filepath.Join(root, "dangleoutrl")},
		{"path outside root", outside},
		{"sibling sharing the root name as a prefix", sibling},
		{"filesystem root", string(filepath.Separator)},
		{"NUL byte", filepath.Join(root, "a\x00b")},
	}
	for _, c := range bad {
		if _, err := ContainedPath(root, c.path); !errors.Is(err, ErrPathEscapes) {
			t.Fatalf("%s: want ErrPathEscapes, got %v", c.name, err)
		}
	}
}

func TestContainedPath_EmptyRoot(t *testing.T) {
	for _, root := range []string{"", "   "} {
		got, err := ContainedPath(root, t.TempDir())
		if err == nil || got != "" {
			t.Fatalf("root %q: want an error and no path, got %q, %v", root, got, err)
		}
	}
}

func TestContainedPath_NULInRoot(t *testing.T) {
	if _, err := ContainedPath("ro\x00ot", "x"); !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("want ErrPathEscapes, got %v", err)
	}
}

func TestContainedPath_RelativePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A relative path is made absolute against the working directory.
	t.Chdir(root)
	got, err := ContainedPath(root, filepath.Join("a", "b"))
	if err != nil {
		t.Fatalf("relative path under root: %v", err)
	}
	if want, _ := filepath.Abs(filepath.Join("a", "b")); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := ContainedPath(root, filepath.Join("..", "x")); !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("relative traversal: want ErrPathEscapes, got %v", err)
	}
	// With the working directory outside root, a relative path is outside too.
	t.Chdir(t.TempDir())
	if _, err := ContainedPath(root, "a"); !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("relative path from outside root: want ErrPathEscapes, got %v", err)
	}
	// A relative root is made absolute the same way.
	t.Chdir(root)
	if _, err := ContainedPath(".", "a"); err != nil {
		t.Fatalf("relative root: %v", err)
	}
}

func TestContainedPath_SymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if _, err := ContainedPath(link, filepath.Join(link, "x")); err != nil {
		t.Fatalf("path under a symlinked root: %v", err)
	}
}
