package claudecode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// Every path ancestorClaudeMds returns is a CLAUDE.md directly inside one of
// the project's ancestors, never a path outside them.
func TestAncestorClaudeMds_PathsStayInAncestors(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "a", "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "CLAUDE.md"), filepath.Join(root, "a", "CLAUDE.md")} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := ancestorClaudeMds(proj)
	if len(got) < 2 {
		t.Fatalf("want both ancestor files, got %v", got)
	}
	for _, g := range got {
		if filepath.Base(g) != "CLAUDE.md" {
			t.Errorf("unexpected entry %s", g)
		}
		if _, err := gitguard.ContainedPath(filepath.Dir(g), g); err != nil {
			t.Errorf("%s is not inside its own directory: %v", g, err)
		}
	}
}

// A path outside the directory it is checked against is refused, which is the
// check ancestorClaudeMds applies to each CLAUDE.md it stats.
func TestAncestorClaudeMds_OutsidePathRefused(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	for _, p := range []string{
		filepath.Join(other, "CLAUDE.md"),
		filepath.Join(dir, "..", "CLAUDE.md"),
		filepath.Join(dir, "sub", "..", "..", "CLAUDE.md"),
	} {
		if _, err := gitguard.ContainedEntry(dir, p); !errors.Is(err, gitguard.ErrPathEscapes) {
			t.Errorf("ContainedEntry(%q) = %v, want ErrPathEscapes", p, err)
		}
	}
}

// A symlinked CLAUDE.md in an ancestor is still reported, wherever it points.
func TestAncestorClaudeMds_KeepsSymlinkedFile(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "real.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "CLAUDE.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	found := false
	for _, g := range ancestorClaudeMds(proj) {
		if g == link {
			found = true
		}
	}
	if !found {
		t.Errorf("symlinked CLAUDE.md %s was dropped", link)
	}
}
