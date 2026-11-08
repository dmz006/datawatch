package autonomous

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// writeUneditedGateDoc writes an unedited generated gate report into root for
// the given automaton id and returns its tracked repository-relative path.
func writeUneditedGateDoc(t *testing.T, root, automatonID string) string {
	t.Helper()
	rel := genartifacts.TrackedPath(genartifacts.GateReport, &changerecord.ChangeRecord{AutomatonID: automatonID}, genartifacts.Options{})
	res, err := genartifacts.Write(genartifacts.GateReport, "body line one\nbody line two\n", rel, genartifacts.Options{Root: root})
	if err != nil || res.Status != genartifacts.Written {
		t.Fatalf("write fixture: %+v %v", res, err)
	}
	return rel
}

// makeEdited overwrites the generated document at root/rel so it no longer is
// an unedited generated file (the first-line marker is removed).
func makeEdited(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("body line one\nbody line two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDropUneditedGeneratedFilesIgnoredOnlyUneditedGivesEmpty(t *testing.T) {
	root := t.TempDir()
	rel := writeUneditedGateDoc(t, root, "4f2a")
	sidecar := genartifacts.SidecarRelPath(rel)

	files := []UncommittedFile{{Path: rel}, {Path: sidecar}}
	if got := dropUneditedGeneratedFiles(root, files); len(got) != 0 {
		t.Fatalf("dropUneditedGeneratedFiles = %+v, want empty", got)
	}
}

func TestDropUneditedGeneratedFilesIgnoredKeepsEveryOtherFile(t *testing.T) {
	root := t.TempDir()
	rel := writeUneditedGateDoc(t, root, "4f2a")
	sidecar := genartifacts.SidecarRelPath(rel)

	files := []UncommittedFile{
		{Path: "src/app.go"},
		{Path: rel},
		{Path: sidecar},
	}
	got := dropUneditedGeneratedFiles(root, files)
	if len(got) != 1 || got[0].Path != "src/app.go" {
		t.Fatalf("dropUneditedGeneratedFiles = %+v, want only the stray file", got)
	}
}

func TestDropUneditedGeneratedFilesIgnoredEditedDocKeptWithSidecar(t *testing.T) {
	root := t.TempDir()
	rel := writeUneditedGateDoc(t, root, "4f2a")
	makeEdited(t, root, rel)
	sidecar := genartifacts.SidecarRelPath(rel)

	files := []UncommittedFile{{Path: rel}, {Path: sidecar}}
	got := dropUneditedGeneratedFiles(root, files)
	want := []UncommittedFile{{Path: rel}, {Path: sidecar}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dropUneditedGeneratedFiles = %+v, want %+v (edited doc kept with its sidecar)", got, want)
	}
}

func TestDropUneditedGeneratedFilesIgnoredSidecarWithoutDocumentKept(t *testing.T) {
	root := t.TempDir()
	rel := writeUneditedGateDoc(t, root, "4f2a")
	sidecar := genartifacts.SidecarRelPath(rel)

	// The document is deliberately absent from the list, so its sidecar has
	// no document here and is kept.
	files := []UncommittedFile{{Path: sidecar}}
	got := dropUneditedGeneratedFiles(root, files)
	if len(got) != 1 || got[0].Path != sidecar {
		t.Fatalf("dropUneditedGeneratedFiles = %+v, want the sidecar kept", got)
	}
}

func TestDropUneditedGeneratedFilesIgnoredOrderPreserved(t *testing.T) {
	root := t.TempDir()
	relA := writeUneditedGateDoc(t, root, "aaaa")
	relB := writeUneditedGateDoc(t, root, "bbbb")

	files := []UncommittedFile{
		{Path: "z/last.go"},
		{Path: relA},
		{Path: genartifacts.SidecarRelPath(relA)},
		{Path: "a/first.go"},
		{Path: relB},
		{Path: genartifacts.SidecarRelPath(relB)},
	}
	got := dropUneditedGeneratedFiles(root, files)
	// The kept stray files appear in their original relative order.
	if len(got) != 2 || got[0].Path != "z/last.go" || got[1].Path != "a/first.go" {
		t.Fatalf("dropUneditedGeneratedFiles = %+v, want the strays in input order", got)
	}
}

func TestDropUneditedGeneratedFilesIgnoredInputUnchanged(t *testing.T) {
	root := t.TempDir()
	rel := writeUneditedGateDoc(t, root, "4f2a")
	sidecar := genartifacts.SidecarRelPath(rel)

	files := []UncommittedFile{
		{Path: "src/app.go", Status: " M", OutsideScope: true},
		{Path: rel, Status: " M"},
		{Path: sidecar, Status: "??"},
	}
	before := append([]UncommittedFile(nil), files...)

	_ = dropUneditedGeneratedFiles(root, files)

	if !reflect.DeepEqual(files, before) {
		t.Fatalf("input slice was modified: before = %+v, after = %+v", before, files)
	}
}
