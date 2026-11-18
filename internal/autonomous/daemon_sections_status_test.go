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

func TestDaemonSectionInlineGuardTable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	sibling := filepath.Join(root, "proj-evil")
	for _, d := range []string{dir, sibling} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "CLAUDE.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	run := dsoRunner(map[string]string{
		"x:HEAD:CLAUDE.md": dsoBase, "x:HEAD:link.md": dsoBase,
		"x:HEAD:../proj-evil/CLAUDE.md": dsoBase,
	})
	cases := []struct {
		name string
		rel  string
		want bool
	}{
		{"in base", "CLAUDE.md", true},
		{"absolute", filepath.Join(sibling, "CLAUDE.md"), false},
		{"traversal", "../outside.md", false},
		{"base sibling", "../proj-evil/CLAUDE.md", false},
		{"nul byte", "CLAUDE.md\x00", false},
		{"empty", "", false},
		{"symlink out of base", "link.md", false},
	}
	for _, tc := range cases {
		t.Run("change/"+tc.name, func(t *testing.T) {
			if got := daemonSectionsOnlyChange(context.Background(), dir, tc.rel, run); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	// Untracked sibling: a file holding only daemon sections counts as empty.
	only := "# Memory & Knowledge (datawatch)\n\nUse the memory tools first.\n"
	if err := os.WriteFile(filepath.Join(dir, "only.md"), []byte(only), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "only.md"), []byte(only), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sibling, "only.md"), filepath.Join(dir, "olink.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	for _, tc := range []struct {
		name string
		rel  string
		want bool
	}{
		{"in base", "only.md", true},
		{"absolute", filepath.Join(sibling, "only.md"), false},
		{"traversal", "../proj-evil/only.md", false},
		{"base sibling", "../proj-evil/only.md", false},
		{"nul byte", "only.md\x00", false},
		{"empty", "", false},
		{"symlink out of base", "olink.md", false},
	} {
		t.Run("untracked/"+tc.name, func(t *testing.T) {
			if got := daemonSectionsOnlyUntracked(context.Background(), dir, tc.rel); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
