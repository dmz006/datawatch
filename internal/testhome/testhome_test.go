package testhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("file modes are not enforced for root")
	}
}

// fixture returns a fresh directory that the test removes with Remove.
func fixture(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "testhome-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Remove(dir) }) //nolint:errcheck
	return dir
}

func TestRemoveReadOnlyTree(t *testing.T) {
	skipIfRoot(t)
	root := fixture(t)
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "a", "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(nested, "f.txt"),
		filepath.Join(root, "a", "b", "g.txt"),
		filepath.Join(locked, "h.txt"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	// Lock directories deepest first so each chmod still has access.
	for _, d := range []string{locked, nested, filepath.Join(root, "a", "b")} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root still exists: %v", err)
	}
}

func TestRemoveMissingPath(t *testing.T) {
	root := fixture(t)
	if err := Remove(filepath.Join(root, "does-not-exist")); err != nil {
		t.Fatalf("Remove of missing path: %v", err)
	}
}

func TestRemoveDoesNotFollowSymlink(t *testing.T) {
	skipIfRoot(t)
	outside := fixture(t)
	target := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o444); err != nil {
		t.Fatal(err)
	}
	root := fixture(t)
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root still exists: %v", err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	}
	if fi.Mode().Perm() != 0o444 {
		t.Errorf("symlink target mode changed to %v", fi.Mode().Perm())
	}
}

func TestPinGoEnvFlags(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	PinGoEnv(t.TempDir())
	if got := os.Getenv("GOFLAGS"); got != "-modcacherw" {
		t.Fatalf("empty GOFLAGS: got %q", got)
	}

	t.Setenv("GOFLAGS", "-mod=vendor -count=1")
	PinGoEnv(t.TempDir())
	want := "-mod=vendor -count=1 -modcacherw"
	if got := os.Getenv("GOFLAGS"); got != want {
		t.Fatalf("existing flags: got %q, want %q", got, want)
	}

	PinGoEnv(t.TempDir())
	if got := os.Getenv("GOFLAGS"); got != want {
		t.Fatalf("not idempotent: got %q, want %q", got, want)
	}
}

func TestPinGoEnvModCache(t *testing.T) {
	t.Setenv("GOPATH", "")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOCACHE", "")
	t.Setenv("XDG_CACHE_HOME", "")

	// Missing: nothing is set and nothing is created.
	home := t.TempDir()
	PinGoEnv(home)
	if got := os.Getenv("GOMODCACHE"); got != "" {
		t.Errorf("GOMODCACHE set to %q for a missing directory", got)
	}
	if got := os.Getenv("GOCACHE"); got != "" {
		t.Errorf("GOCACHE set to %q for a missing directory", got)
	}
	if _, err := os.Stat(filepath.Join(home, "go")); !os.IsNotExist(err) {
		t.Errorf("PinGoEnv created %s/go", home)
	}

	// Existing: pinned.
	mod := filepath.Join(home, "go", "pkg", "mod")
	build := filepath.Join(home, ".cache", "go-build")
	for _, d := range []string{mod, build} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	PinGoEnv(home)
	if got := os.Getenv("GOMODCACHE"); got != mod {
		t.Errorf("GOMODCACHE = %q, want %q", got, mod)
	}
	if got := os.Getenv("GOCACHE"); got != build {
		t.Errorf("GOCACHE = %q, want %q", got, build)
	}
}

func TestPinGoEnvKeepsExisting(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "go", "pkg", "mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cache", "go-build"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("GOMODCACHE", "/already/set/mod")
	t.Setenv("GOCACHE", "/already/set/build")
	PinGoEnv(home)
	if got := os.Getenv("GOMODCACHE"); got != "/already/set/mod" {
		t.Errorf("GOMODCACHE overridden: %q", got)
	}
	if got := os.Getenv("GOCACHE"); got != "/already/set/build" {
		t.Errorf("GOCACHE overridden: %q", got)
	}
}

func TestDisableGoTelemetry(t *testing.T) {
	home := fixture(t)
	DisableGoTelemetry(home)
	b, err := os.ReadFile(filepath.Join(home, ".config", "go", "telemetry", "mode"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(b)); len(got) == 0 || got[0] != "off" {
		t.Fatalf("mode file %q does not start with off", b)
	}
}
