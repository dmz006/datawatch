package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotSubjectNamesPRDAndTitle(t *testing.T) {
	got := snapshotSubject("abcdef1234567890", "Add the parser")
	want := "snapshot before task Add the parser (PRD abcdef12)"
	if got != want {
		t.Errorf("snapshotSubject = %q, want %q", got, want)
	}

	// Multi-line titles collapse to one line.
	got = snapshotSubject("abcdef1234567890", "Add\n  the\tparser")
	if got != want {
		t.Errorf("multi-line title: snapshotSubject = %q, want %q", got, want)
	}

	// Titles are capped at 120 runes.
	long := strings.Repeat("x", 200)
	got = snapshotSubject("", long)
	wantLong := "snapshot before task " + strings.Repeat("x", 120)
	if got != wantLong {
		t.Errorf("long title: got %d runes, want %d", len([]rune(got)), len([]rune(wantLong)))
	}
}

func TestSnapshotSubjectEmptyTitle(t *testing.T) {
	if got, want := snapshotSubject("", ""), "snapshot before session work"; got != want {
		t.Errorf("empty title: snapshotSubject = %q, want %q", got, want)
	}
	if got, want := snapshotSubject("", "  \n\t "), "snapshot before session work"; got != want {
		t.Errorf("blank title: snapshotSubject = %q, want %q", got, want)
	}
	if got, want := snapshotSubject("abcdef1234567890", ""), "snapshot before session work (PRD abcdef12)"; got != want {
		t.Errorf("empty title with PRD: snapshotSubject = %q, want %q", got, want)
	}
}

func TestSnapshotSubjectNoPRD(t *testing.T) {
	if got, want := snapshotSubject("", "Fix the thing"), "snapshot before task Fix the thing"; got != want {
		t.Errorf("no PRD: snapshotSubject = %q, want %q", got, want)
	}
}

func TestPreSessionCommitSubjectIsPurpose(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	g := NewProjectGit(dir)
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "t@x")
	mustGit(t, dir, "config", "user.name", "T")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "init")

	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}

	const prdID = "abcdef1234567890"
	const title = "Wire the snapshot"
	if err := g.PreSessionCommit(prdID, title, []string{dir}); err != nil {
		t.Fatalf("PreSessionCommit: %v", err)
	}

	subject, err := gitOutput(dir, "log", "-1", "--format=%s")
	if err != nil {
		t.Fatal(err)
	}
	subject = strings.TrimSpace(subject)
	want := snapshotSubject(prdID, title)
	if subject != want {
		t.Errorf("commit subject = %q, want %q", subject, want)
	}
	if strings.Contains(subject, commitPromptPhrase) {
		t.Errorf("commit subject is prompt text: %q", subject)
	}
}
