package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkerRemoveRegularFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, completionMarkerFile), "x")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "keep")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "sub", completionMarkerFile), "keep")

	removed, err := removeCompletionMarker(dir)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v, want true,nil", removed, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, completionMarkerFile)); !os.IsNotExist(err) {
		t.Fatalf("marker still present: %v", err)
	}
	for _, keep := range []string{"notes.txt", filepath.Join("sub", completionMarkerFile)} {
		if _, err := os.Lstat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s should remain: %v", keep, err)
		}
	}
}

func TestMarkerRemoveMissing(t *testing.T) {
	removed, err := removeCompletionMarker(t.TempDir())
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v, want false,nil", removed, err)
	}
}

func TestMarkerRemoveRefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, completionMarkerFile)
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(d, "inner.txt"), "keep")
	removed, err := removeCompletionMarker(dir)
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v, want false,nil", removed, err)
	}
	if _, err := os.Stat(filepath.Join(d, "inner.txt")); err != nil {
		t.Errorf("directory contents lost: %v", err)
	}
}

func TestMarkerRemoveRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.txt")
	mustWrite(t, target, "keep")
	link := filepath.Join(dir, completionMarkerFile)
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	removed, err := removeCompletionMarker(dir)
	if removed || err != nil {
		t.Fatalf("removed=%v err=%v, want false,nil", removed, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("symlink target lost: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink removed: %v", err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
