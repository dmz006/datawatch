package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonSectionsOnlyChangeUnsafePaths(t *testing.T) {
	dir, run := dsoSetup(t, dsoDaemon)
	other := t.TempDir()
	abs := filepath.Join(other, "CLAUDE.md")
	if err := os.WriteFile(abs, []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../README.md", abs, "CLAUDE.md\x00x"} {
		if daemonSectionsOnlyChange(context.Background(), dir, rel, run) {
			t.Errorf("%q must be false", rel)
		}
	}
	if !daemonSectionsOnlyChange(context.Background(), dir, "CLAUDE.md", run) {
		t.Error("daemon-only difference on a normal path must be true")
	}
}
