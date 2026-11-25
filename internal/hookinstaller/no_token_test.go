package hookinstaller

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No file the installer writes into a project folder may hold an API token.
func TestInstallWritesNoTokenIntoTheProject(t *testing.T) {
	dir := t.TempDir()
	if err := Install(dir, "host-abcd", "http://localhost:1", "the-daemons-admin-token"); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(raw), "the-daemons-admin-token") {
			t.Errorf("token written into %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
