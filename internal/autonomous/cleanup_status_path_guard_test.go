package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixCleanupPermissionsInlineGuardTable(t *testing.T) {
	base := t.TempDir()
	inDir := filepath.Join(base, "work")
	if err := os.Mkdir(inDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inFile := filepath.Join(inDir, "ro.txt")
	if err := os.WriteFile(inFile, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		path    string
		refused bool
	}{
		{"traversal", filepath.Join("work", "..", "..", "escape"), true},
		{"traversal absolute", filepath.Join(base, "..", "escape"), true},
		{"nul byte", "work/a\x00b", true},
		{"base itself", ".", true},
		{"in base", "work", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := FixCleanupPermissions(base, []string{tc.path})
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "outside the worktree base") {
					t.Fatalf("expected refusal, got n=%d err=%v", n, err)
				}
				return
			}
			if err != nil || n < 1 {
				t.Fatalf("fix: n=%d err=%v", n, err)
			}
			info, serr := os.Stat(inFile)
			if serr != nil {
				t.Fatal(serr)
			}
			if info.Mode().Perm()&0o200 == 0 {
				t.Fatalf("owner write not restored: %v", info.Mode())
			}
		})
	}
}

func TestFixCleanupPermissionsRefusesBeforeFilesystem(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	existing := filepath.Join(outside, "real.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(outside, "nope")

	errText := func(p string) string {
		t.Helper()
		_, err := FixCleanupPermissions(base, []string{p})
		if err == nil {
			t.Fatalf("expected refusal for %q", p)
		}
		if !strings.Contains(err.Error(), "outside the worktree base") {
			t.Fatalf("unexpected error for %q: %v", p, err)
		}
		return strings.ReplaceAll(err.Error(), p, "<path>")
	}

	if a, b := errText(existing), errText(missing); a != b {
		t.Fatalf("absolute outside: texts differ: %q vs %q", a, b)
	}

	// Escapes with ".." (both targets are absent; the text must not vary).
	if a, b := errText(filepath.Join(base, "..", "nope")), errText("../x"); a != b {
		t.Fatalf("dotdot: texts differ: %q vs %q", a, b)
	}
	if _, err := FixCleanupPermissions(base, []string{"a\x00b"}); err == nil {
		t.Fatal("NUL path should be refused")
	}

	// Symlink inside base pointing outside is still refused.
	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := FixCleanupPermissions(base, []string{filepath.Join(link, "real.txt")}); err == nil ||
		!strings.Contains(err.Error(), "outside the worktree base") {
		t.Fatalf("symlink escape not refused: %v", err)
	}

	// In-base read-only file is still fixed.
	wt := filepath.Join(base, "wt")
	if err := os.Mkdir(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(wt, "ro.txt")
	if err := os.WriteFile(f, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	n, err := FixCleanupPermissions(base, []string{wt})
	if err != nil || n < 1 {
		t.Fatalf("fix: n=%d err=%v", n, err)
	}
	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("owner write not restored: %v", info.Mode())
	}
}
