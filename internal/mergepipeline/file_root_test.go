package mergepipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// A pipeline file name that is a symlink leaving the project directory is
// refused, not followed.
func TestLoadFile_SymlinkOutsideProjectIsRefused(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.yaml"), []byte("steps: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret.yaml"), filepath.Join(project, "merge-pipeline.yaml")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	if _, err := LoadFile(project, "merge-pipeline.yaml", 0); err == nil {
		t.Fatal("a symlink that leaves the project directory was followed")
	}
}
