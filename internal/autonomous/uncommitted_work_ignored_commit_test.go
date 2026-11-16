package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uwIgnoredSetup writes a .gitignore with .datawatch/, commits it, and creates
// an ignored evidence file under .datawatch/reports.
func uwIgnoredSetup(t *testing.T) string {
	t.Helper()
	dir := uwRepo(t)
	uwWrite(t, dir, ".gitignore", ".datawatch/\n")
	uwGit(t, dir, "add", ".gitignore")
	uwGit(t, dir, "commit", "-q", "-m", "add gitignore")
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	uwWrite(t, dir, ".datawatch/reports/evidence.txt", "evidence\n")
	return dir
}

func TestTaskCommitIgnoredPathCommitsNormalFile(t *testing.T) {
	dir := uwIgnoredSetup(t)
	uwWrite(t, dir, "normal.txt", "normal change\n")
	before := uwGit(t, dir, "rev-parse", "HEAD")

	_, err := DefaultTaskCommit(context.Background(), dir,
		[]string{"normal.txt", ".datawatch/reports/evidence.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	after := uwGit(t, dir, "rev-parse", "HEAD")
	if after == before {
		t.Fatal("expected a new commit")
	}
	names := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(names, "normal.txt") {
		t.Errorf("commit missing normal.txt: %q", names)
	}
	if strings.Contains(names, ".datawatch/") {
		t.Errorf("commit contains ignored path: %q", names)
	}
}

func TestTaskCommitIgnoredOnlyMakesNoCommit(t *testing.T) {
	dir := uwIgnoredSetup(t)
	before := uwGit(t, dir, "rev-parse", "HEAD")

	_, err := DefaultTaskCommit(context.Background(), dir,
		[]string{".datawatch/reports/evidence.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved: %s -> %s", before, after)
	}
}
