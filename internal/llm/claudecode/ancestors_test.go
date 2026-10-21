package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAncestorClaudeMds(t *testing.T) {
	root := t.TempDir()
	mid := filepath.Join(root, "a")
	proj := filepath.Join(mid, "proj")
	if err := os.MkdirAll(filepath.Join(proj, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "CLAUDE.md"), filepath.Join(proj, "CLAUDE.md"), filepath.Join(proj, "sub", "CLAUDE.md")} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := ancestorClaudeMds(proj)
	var inTemp []string
	for _, g := range got {
		if strings.HasPrefix(g, root) {
			inTemp = append(inTemp, g)
		}
		if strings.HasPrefix(g, proj) {
			t.Errorf("included project path %s", g)
		}
	}
	if len(inTemp) != 1 || inTemp[0] != filepath.Join(root, "CLAUDE.md") {
		t.Errorf("got %v", inTemp)
	}
}

func TestSettingsFlag(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "p")
	_ = os.MkdirAll(proj, 0o755)
	_ = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("x"), 0o644)
	b := &Backend{}
	if f := b.settingsFlag(proj); f != "" {
		t.Errorf("off by default, got %q", f)
	}
	b.SetExcludeAncestorClaudeMd(true)
	f := b.settingsFlag(proj)
	if !strings.HasPrefix(f, " --settings '{\"claudeMdExcludes\":[") || !strings.Contains(f, filepath.Join(root, "CLAUDE.md")) {
		t.Errorf("flag = %q", f)
	}
}
