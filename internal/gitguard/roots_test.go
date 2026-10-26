package gitguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProjectDirInsideRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"existing dir":        sub,
		"missing tail":        filepath.Join(root, "new", "deeper"),
		"the root itself":     root,
		"dot-dot stays in":    filepath.Join(root, "proj", "..", "other"),
		"redundant separator": root + "//proj/",
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ResolveProjectDir(dir, []string{root})
			if err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if got != filepath.Clean(dir) {
				t.Errorf("got %q want %q", got, filepath.Clean(dir))
			}
		})
	}
}

func TestResolveProjectDirRefusesTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		filepath.Join(root, "..", "elsewhere"),
		root + "/sub/../../elsewhere",
		root + "/../root-sibling",
		parent,
	} {
		_, err := ResolveProjectDir(dir, []string{root})
		if !errors.Is(err, ErrOutsideAllowedRoots) {
			t.Errorf("%q: want ErrOutsideAllowedRoots, got %v", dir, err)
		}
	}
}

func TestResolveProjectDirRefusesSiblingWithSharedPrefix(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	sibling := filepath.Join(parent, "work-other")
	for _, d := range []string{root, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ResolveProjectDir(sibling, []string{root}); !errors.Is(err, ErrOutsideAllowedRoots) {
		t.Fatalf("a sibling sharing a name prefix must be refused, got %v", err)
	}
}

func TestResolveProjectDirRefusesSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, dir := range []string{link, filepath.Join(link, "deeper")} {
		if _, err := ResolveProjectDir(dir, []string{root}); !errors.Is(err, ErrOutsideAllowedRoots) {
			t.Errorf("%q: want ErrOutsideAllowedRoots, got %v", dir, err)
		}
	}
}

func TestResolveProjectDirRefusesDanglingLinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	missing := filepath.Join(outside, "not-created-yet")
	abs := filepath.Join(root, "dangle-abs")
	if err := os.Symlink(missing, abs); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rel := filepath.Join(root, "dangle-rel")
	if err := os.Symlink(filepath.Join("..", filepath.Base(outside), "gone"), rel); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{abs, filepath.Join(abs, "child"), rel} {
		if _, err := ResolveProjectDir(dir, []string{root}); !errors.Is(err, ErrOutsideAllowedRoots) {
			t.Errorf("%q: want ErrOutsideAllowedRoots, got %v", dir, err)
		}
	}
	// A dangling link whose missing target stays under the root is fine.
	inside := filepath.Join(root, "dangle-inside")
	if err := os.Symlink(filepath.Join(root, "future"), inside); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProjectDir(inside, []string{root}); err != nil {
		t.Errorf("dangling link that stays inside the root must be accepted, got %v", err)
	}
}

func TestResolveProjectDirSymlinkLoop(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProjectDir(a, []string{root}); err == nil {
		t.Fatal("a symlink loop must be refused")
	}
}

func TestResolveProjectDirExtraRoot(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	dir := filepath.Join(extra, "proj")
	if _, err := ResolveProjectDir(dir, []string{primary}); !errors.Is(err, ErrOutsideAllowedRoots) {
		t.Fatalf("before the extra root is configured the path is outside: %v", err)
	}
	if _, err := ResolveProjectDir(dir, []string{primary, extra}); err != nil {
		t.Fatalf("a path inside a configured extra root must be accepted: %v", err)
	}
}

func TestResolveProjectDirOutsideEveryRoot(t *testing.T) {
	a, b, outside := t.TempDir(), t.TempDir(), t.TempDir()
	_, err := ResolveProjectDir(filepath.Join(outside, "proj"), []string{a, b})
	if !errors.Is(err, ErrOutsideAllowedRoots) {
		t.Fatalf("want ErrOutsideAllowedRoots, got %v", err)
	}
	for _, root := range []string{a, b} {
		if !strings.Contains(err.Error(), root) {
			t.Errorf("error should name the allowed root %s: %v", root, err)
		}
	}
}

func TestResolveProjectDirSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	linkParent := t.TempDir()
	root := filepath.Join(linkParent, "root-link")
	if err := os.Symlink(real, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ResolveProjectDir(filepath.Join(root, "proj"), []string{root}); err != nil {
		t.Errorf("path under a symlinked root, named through the link: %v", err)
	}
	if _, err := ResolveProjectDir(filepath.Join(real, "proj"), []string{root}); err != nil {
		t.Errorf("path under a symlinked root, named by its real location: %v", err)
	}
}

func TestResolveProjectDirNeverFailsOpen(t *testing.T) {
	dir := t.TempDir()
	for _, roots := range [][]string{nil, {}, {""}, {"  "}} {
		if _, err := ResolveProjectDir(dir, roots); !errors.Is(err, ErrOutsideAllowedRoots) {
			t.Errorf("roots %q: want ErrOutsideAllowedRoots, got %v", roots, err)
		}
	}
}

func TestResolveProjectDirRefusesUnusableValues(t *testing.T) {
	root := t.TempDir()
	for name, dir := range map[string]string{
		"empty":           "",
		"NUL":             root + "/a\x00b",
		"filesystem root": "/",
	} {
		if _, err := ResolveProjectDir(dir, []string{root, "/"}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestRefuseNonProjectEnforcesRegisteredRoots(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	outside := t.TempDir()
	mk := func(parent string) string {
		d := filepath.Join(parent, "proj")
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	inRoot, inExtra, notAllowed := mk(root), mk(extra), mk(outside)
	home := func() (string, error) { return "/nonexistent-home", nil }

	// No source registered: only the non-project checks apply.
	if err := refuseNonProject(notAllowed, home); err != nil {
		t.Fatalf("without a registered source the root check must be off: %v", err)
	}

	roots := []string{root}
	SetProjectRoots(func() []string { return roots })
	t.Cleanup(func() { SetProjectRoots(nil) })

	if err := refuseNonProject(inRoot, home); err != nil {
		t.Fatalf("project inside a root refused: %v", err)
	}
	err := refuseNonProject(notAllowed, home)
	if !IsNonProject(err) || !strings.Contains(err.Error(), "allowed project roots") {
		t.Fatalf("project outside every root: want a NonProjectError naming the roots, got %v", err)
	}
	if err := refuseNonProject(inExtra, home); !IsNonProject(err) {
		t.Fatalf("project in a root that is not configured yet must be refused: %v", err)
	}

	// The refusal is not remembered: adding the root allows the next commit.
	roots = []string{root, extra}
	if err := refuseNonProject(inExtra, home); err != nil {
		t.Fatalf("project inside a newly configured extra root refused: %v", err)
	}

	// Traversal and links out of the root.
	if err := refuseNonProject(filepath.Join(inRoot, "..", "..", filepath.Base(outside), "proj"), home); !IsNonProject(err) {
		t.Fatalf("traversal out of the root: %v", err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err == nil {
		if err := refuseNonProject(filepath.Join(link, "proj"), home); !IsNonProject(err) {
			t.Fatalf("symlink out of the root: %v", err)
		}
	}
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(outside, "missing"), dangling); err == nil {
		if err := refuseNonProject(dangling, home); !IsNonProject(err) {
			t.Fatalf("dangling link out of the root: %v", err)
		}
	}
}
