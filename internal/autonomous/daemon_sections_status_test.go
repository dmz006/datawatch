package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const dsoBase = "# Project\n\nBuild with go build ./... .\n"

const dsoDaemon = dsoBase +
	"\n# Memory & Knowledge (datawatch)\n\nUse the memory tools first.\n\n" +
	"<!-- rtk-instructions -->\nalways prefix with rtk\n<!-- /rtk-instructions -->\n"

// dsoRunner serves `git show HEAD:<rel>` from a map; absent keys error.
func dsoRunner(head map[string]string) driftGitRunner {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) == 2 && args[0] == "show" {
			if v, ok := head["x:"+args[1]]; ok {
				return v, nil
			}
		}
		return "", errors.New("not in HEAD")
	}
}

func dsoSetup(t *testing.T, work string) (string, driftGitRunner) {
	t.Helper()
	dir := t.TempDir()
	if work != "" {
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(work), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, dsoRunner(map[string]string{"x:HEAD:CLAUDE.md": dsoBase})
}

func TestDaemonSectionOnlyDifferenceIsTrue(t *testing.T) {
	dir, run := dsoSetup(t, dsoDaemon)
	if !daemonSectionsOnlyChange(context.Background(), dir, "CLAUDE.md", run) {
		t.Fatal("daemon-only difference should be true")
	}
}

func TestDaemonSectionOnlyRealEditIsFalse(t *testing.T) {
	dir, run := dsoSetup(t, dsoBase+"\nExtra operator line.\n"+dsoDaemon[len(dsoBase):])
	if daemonSectionsOnlyChange(context.Background(), dir, "CLAUDE.md", run) {
		t.Fatal("real edit outside markers should be false")
	}
}

func TestDaemonSectionOnlyNewFileIsFalse(t *testing.T) {
	dir, _ := dsoSetup(t, dsoDaemon)
	run := dsoRunner(nil)
	if daemonSectionsOnlyChange(context.Background(), dir, "CLAUDE.md", run) {
		t.Fatal("file absent from HEAD should be false")
	}
}

func TestDaemonSectionOnlyDeletedWorkingCopyIsFalse(t *testing.T) {
	dir, run := dsoSetup(t, "")
	if daemonSectionsOnlyChange(context.Background(), dir, "CLAUDE.md", run) {
		t.Fatal("missing working copy should be false")
	}
}

func TestDaemonSectionOnlyUnchangedIsFalse(t *testing.T) {
	dir, run := dsoSetup(t, dsoBase)
	if daemonSectionsOnlyChange(context.Background(), dir, "CLAUDE.md", run) {
		t.Fatal("unchanged file should be false")
	}
}

func TestDaemonSectionOnlyUnsafePathIsFalse(t *testing.T) {
	dir, run := dsoSetup(t, dsoDaemon)
	for _, p := range []string{"", "/etc/passwd", "../CLAUDE.md", "..", "CLAUDE.md\x00"} {
		if daemonSectionsOnlyChange(context.Background(), dir, p, run) {
			t.Fatalf("unsafe path %q should be false", p)
		}
	}
}

func TestDaemonSectionOnlyCancelledContextIsFalse(t *testing.T) {
	dir, run := dsoSetup(t, dsoDaemon)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if daemonSectionsOnlyChange(ctx, dir, "CLAUDE.md", run) {
		t.Fatal("cancelled context should be false")
	}
}
