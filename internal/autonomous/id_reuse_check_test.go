package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIDReuse_StartupCheckReportsArtifactsOlderThanTheRecord(t *testing.T) {
	repo, worktrees := idTestRepo(t), t.TempDir()
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo, cfg.WorktreeDir = repo, worktrees
	m := idTestManager(t, t.TempDir(), cfg)

	reused := idTestPRD(t, m.Store(), "reused id")
	clean := idTestPRD(t, m.Store(), "clean id")

	// Left over from an earlier record with the same id.
	old := time.Now().Add(-96 * time.Hour)
	oldEnv := []string{fmt.Sprintf("GIT_COMMITTER_DATE=@%d +0000", old.Unix())}
	idTestGit(t, repo, oldEnv, "branch", "automaton/"+reused.ID+"-old-work")
	folder := filepath.Join(worktrees, reused.ID)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(folder, old, old); err != nil {
		t.Fatal(err)
	}
	// The clean record's own branch and worktree are younger than it.
	idTestGit(t, repo, nil, "branch", "automaton/"+clean.ID+"-own-work")
	if err := os.MkdirAll(filepath.Join(worktrees, clean.ID), 0o755); err != nil {
		t.Fatal(err)
	}

	got := m.CheckIDReuse()
	if len(got) != 1 || got[0].ID != reused.ID {
		t.Fatalf("CheckIDReuse = %+v, want only %s", got, reused.ID)
	}
	var names []string
	for _, a := range got[0].Artifacts {
		names = append(names, a.Name)
	}
	want := []string{"refs/heads/automaton/" + reused.ID + "-old-work", folder}
	for _, w := range want {
		if !strings.Contains(strings.Join(names, "\n"), w) {
			t.Fatalf("artifacts %v lack %s", names, w)
		}
	}

	var level, title, body string
	alerts := 0
	m.SetAlertSink(func(l, ti, b string) string { level, title, body = l, ti, b; alerts++; return "a1" })
	m.ReportIDReuse()
	if alerts != 1 || level != "warn" || title == "" || !strings.Contains(body, reused.ID) || strings.Contains(body, clean.ID) {
		t.Fatalf("alerts=%d level=%q title=%q body=%q", alerts, level, title, body)
	}
	// Report only: nothing is changed.
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("the check touched %s: %v", folder, err)
	}
	idTestGit(t, repo, nil, "rev-parse", "--verify", "refs/heads/automaton/"+reused.ID+"-old-work")
}

func TestIDReuse_StartupCheckSaysNothingWhenClean(t *testing.T) {
	m := idTestManager(t, t.TempDir(), DefaultConfig())
	idTestPRD(t, m.Store(), "x")
	alerts := 0
	m.SetAlertSink(func(_, _, _ string) string { alerts++; return "" })
	m.ReportIDReuse()
	if alerts != 0 {
		t.Fatalf("alerts = %d, want 0", alerts)
	}
}

// Work done on another branch and renamed to the record's name afterwards is
// not older work under this id: the name is what counts.
func TestIDReuse_StartupCheckDatesARenamedBranchFromTheRename(t *testing.T) {
	repo := idTestRepo(t)
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo, cfg.WorktreeDir = repo, t.TempDir()
	m := idTestManager(t, t.TempDir(), cfg)
	old := time.Now().Add(-96 * time.Hour)
	oldEnv := []string{fmt.Sprintf("GIT_COMMITTER_DATE=@%d +0000", old.Unix())}
	idTestGit(t, repo, oldEnv, "branch", "fix/by-hand")
	adopted := idTestPRD(t, m.Store(), "adopts a hand-made branch")
	idTestGit(t, repo, nil, "branch", "-m", "fix/by-hand", "automaton/"+adopted.ID)
	if got := m.CheckIDReuse(); len(got) != 0 {
		t.Fatalf("CheckIDReuse = %+v, want none: the branch took this name after the record", got)
	}

	// Renamed to the id before the record existed: still older work.
	later := idTestPRD(t, m.Store(), "issued after the rename")
	idTestGit(t, repo, oldEnv, "branch", "fix/other")
	idTestGit(t, repo, oldEnv, "branch", "-m", "fix/other", "automaton/"+later.ID)
	if got := m.CheckIDReuse(); len(got) != 1 || got[0].ID != later.ID {
		t.Fatalf("CheckIDReuse = %+v, want only %s", got, later.ID)
	}
}
