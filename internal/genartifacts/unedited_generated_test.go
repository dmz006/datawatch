package genartifacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
)

// freshUneditedFixture writes a tracked gate report for automaton 4f2a into a
// fresh temp root and returns the root and the tracked relative path.
func freshUneditedFixture(t *testing.T) (root string, rel string) {
	t.Helper()
	root = t.TempDir()
	rel = TrackedPath(GateReport, &changerecord.ChangeRecord{AutomatonID: "4f2a"}, Options{})
	res, err := Write(GateReport, "body line one\nbody line two\n", rel, Options{Root: root})
	if err != nil || res.Status != Written {
		t.Fatalf("fixture write: %+v %v", res, err)
	}
	return root, rel
}

func TestUneditedGeneratedFilesIgnored(t *testing.T) {
	t.Run("freshly written file is unedited", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		if !IsUneditedGenerated(root, rel) {
			t.Error("want true for a freshly written generated file")
		}
	})
	t.Run("marker line removed", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("body line one\nbody line two\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false once the marker line is gone")
		}
	})
	t.Run("edited marker line after the marker", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		edited := append([]byte(Marker+"\n"+EditedMarker+"\n"), b[len(Marker):]...)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), edited, 0o644); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false when the edited line is present")
		}
	})
	t.Run("one body line changed, marker kept", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		b := []byte("body line one CHANGED\nbody line two\n")
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), append([]byte(Marker+"\n"), b...), 0o644); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false when a body line differs from the recorded hash")
		}
	})
	t.Run("tracked path is a directory", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, filepath.FromSlash(rel)), 0o750); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false when the tracked path is a directory")
		}
	})
	t.Run("sidecar deleted", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(SidecarRelPath(rel)))); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false when the sidecar is missing")
		}
	})
	t.Run("sidecar content replaced with another hash", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(SidecarRelPath(rel))), []byte("0000000000000000000000000000000000000000000000000000000000000000\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false when the sidecar hash differs")
		}
	})
	t.Run("marker at a path that is not tracked", func(t *testing.T) {
		dir := t.TempDir()
		if res, err := Write(GateReport, "body line one\n", "notes/some.md", Options{Root: dir}); err != nil || res.Status != Written {
			t.Fatalf("write: %+v %v", res, err)
		}
		if IsUneditedGenerated(dir, "notes/some.md") {
			t.Error("want false for a non-tracked path")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		root, rel := freshUneditedFixture(t)
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
		if IsUneditedGenerated(root, rel) {
			t.Error("want false for a missing file")
		}
	})
}

func TestSidecarRelPathGeneratedFilesIgnored(t *testing.T) {
	rel := TrackedPath(GateReport, &changerecord.ChangeRecord{AutomatonID: "4f2a"}, Options{})
	if got, want := SidecarRelPath(rel), "docs/plans/reports/.4f2a-gate.md.genhash"; got != want {
		t.Errorf("SidecarRelPath = %q, want %q", got, want)
	}
}
