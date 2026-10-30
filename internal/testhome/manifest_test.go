package testhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withManifest points the package manifest at a file inside dir (the test
// tree) and restores the real path afterwards. Every test that exercises
// the manifest does this; os.TempDir is fixed for the life of the test
// binary, so a variable is the only seam.
func withManifest(t *testing.T, dir string) {
	t.Helper()
	old := manifestPath
	t.Cleanup(func() { manifestPath = old })
	manifestPath = filepath.Join(dir, manifestName)
}

func setMtime(t *testing.T, dir string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}
}

func manifestLines(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return strings.FieldsFunc(string(b), func(r rune) bool { return r == '\n' || r == ' ' })
}

func TestRecordAppends(t *testing.T) {
	dir := t.TempDir()
	withManifest(t, dir)
	a, b := filepath.Join(dir, "one"), filepath.Join(dir, "two")
	Record(a)
	Record(b)
	lines := manifestLines(t)
	if len(lines) != 2 || lines[0] != a || lines[1] != b {
		t.Fatalf("manifest lines: %#v", lines)
	}
	fi, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("manifest mode = %o, want 0600", got)
	}
}

func TestSweepStaleOldRemovedYoungKept(t *testing.T) {
	tmp := t.TempDir()
	withManifest(t, tmp)
	now := time.Now()

	oldHome := filepath.Join(tmp, "dw-test-home-abcd1234")
	oldTmux := filepath.Join(tmp, "dw-test-tmux-wxyz5678")
	young := filepath.Join(tmp, "dw-test-home-keep99")
	for _, d := range []string{oldHome, oldTmux, young} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	setMtime(t, oldHome, now.Add(-2*time.Hour))
	setMtime(t, oldTmux, now.Add(-2*time.Hour))
	setMtime(t, young, now.Add(-10*time.Minute))
	for _, d := range []string{oldHome, oldTmux, young} {
		Record(d)
	}

	if err := SweepStale(tmp, time.Hour, now); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if _, err := os.Lstat(oldHome); !os.IsNotExist(err) {
		t.Errorf("old home dir still exists (err=%v)", err)
	}
	if _, err := os.Lstat(oldTmux); !os.IsNotExist(err) {
		t.Errorf("old tmux dir still exists (err=%v)", err)
	}
	if _, err := os.Lstat(young); err != nil {
		t.Errorf("young dir was removed: %v", err)
	}
	got := manifestLines(t)
	if len(got) != 1 || got[0] != young {
		t.Errorf("survivors = %#v, want one entry %q", got, young)
	}
}

func TestSweepStaleRefusesOutsideAndWrongPrefix(t *testing.T) {
	tmp := t.TempDir()
	withManifest(t, tmp)
	now := time.Now()

	// Right name but listed outside tmpDir (sitting in a sibling dir).
	outside := t.TempDir()
	outsideDir := filepath.Join(outside, "dw-test-home-evil")
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setMtime(t, outsideDir, now.Add(-2*time.Hour))

	// Inside tmpDir, old, but the wrong base-name prefix.
	wrongName := filepath.Join(tmp, "other-abcd")
	if err := os.Mkdir(wrongName, 0o755); err != nil {
		t.Fatal(err)
	}
	setMtime(t, wrongName, now.Add(-2*time.Hour))

	// A symlink wearing the right name — must never be followed or removed.
	target := filepath.Join(outside, "dw-test-home-target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	setMtime(t, target, now.Add(-2*time.Hour))
	link := filepath.Join(tmp, "dw-test-home-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	cases := []string{outsideDir, wrongName, link}
	for _, d := range cases {
		Record(d)
	}

	if err := SweepStale(tmp, time.Hour, now); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	for _, d := range cases {
		if _, err := os.Lstat(d); err != nil {
			t.Errorf("%s should have been refused by the sweep but is gone: %v", d, err)
		}
	}
	if _, err := os.Lstat(target); err != nil {
		t.Errorf("symlink target was removed: %v", err)
	}
	if got := manifestLines(t); len(got) != len(cases) {
		t.Errorf("manifest survivors = %#v, want all %d entries kept", got, len(cases))
	}
}

func TestSweepStaleMissingManifestIsFine(t *testing.T) {
	tmp := t.TempDir()
	withManifest(t, tmp)
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: manifest should not exist (err=%v)", err)
	}
	if err := SweepStale(tmp, time.Hour, time.Now()); err != nil {
		t.Fatalf("SweepStale with missing manifest: %v", err)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("SweepStale created a manifest where none existed (err=%v)", err)
	}
}

func TestSweepStaleRemovesReadOnlyTree(t *testing.T) {
	skipIfRoot(t)
	tmp := t.TempDir()
	withManifest(t, tmp)
	now := time.Now()

	old := filepath.Join(tmp, "dw-test-home-locked")
	nested := filepath.Join(old, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(old, "a"), nested} {
		if err := os.Chmod(p, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(old, 0o500); err != nil {
		t.Fatal(err)
	}
	setMtime(t, old, now.Add(-2*time.Hour))
	Record(old)

	if err := SweepStale(tmp, time.Hour, now); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatalf("read-only tree still exists (err=%v)", err)
	}
	if got := manifestLines(t); len(got) != 0 {
		t.Errorf("survivors = %#v, want empty", got)
	}
}
