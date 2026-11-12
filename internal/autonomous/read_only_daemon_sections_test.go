package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// roDaemonSetup commits AGENT.md in a fresh repo and returns the repo, the
// manager, the PRD and the read-only task plus the baseline status.
func roDaemonSetup(t *testing.T) (string, *Manager, *PRD, *Task, string) {
	t.Helper()
	repo := gitRepoWithDWSeed(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte(dsoBase), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "AGENT.md")
	gitRun(t, repo, "commit", "-q", "-m", "agent")
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, "")
	tk, ok := mgr.Store().GetTask(prd.Story[0].Tasks[0].ID)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}
	base, ok := mgr.readOnlyTreeStatus(context.Background(), prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	return repo, mgr, prd, tk, base
}

func TestReadOnlyDaemonSectionOnlyEditGivesNoViolation(t *testing.T) {
	repo, mgr, prd, tk, base := roDaemonSetup(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.readOnlyViolation(context.Background(), prd, tk, base); err != nil {
		t.Fatalf("daemon-section-only edit must not violate: %v", err)
	}
}

func TestReadOnlyDaemonSectionRealEditStillViolates(t *testing.T) {
	repo, mgr, prd, tk, base := roDaemonSetup(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte("real change\n"+dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.readOnlyViolation(context.Background(), prd, tk, base); err == nil {
		t.Fatal("real edit in AGENT.md must violate")
	}
}

func TestReadOnlyDaemonSectionUntrackedFileStillViolates(t *testing.T) {
	repo, mgr, prd, tk, base := roDaemonSetup(t)
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.readOnlyViolation(context.Background(), prd, tk, base); err == nil {
		t.Fatal("new untracked file must violate")
	}
}

func TestReadOnlyDaemonSectionRenameEntryKept(t *testing.T) {
	got := dropDaemonOnlyStatusEntries(context.Background(), t.TempDir(), "R  a.md -> b.md\n?? c.md", nil)
	if got != "R  a.md -> b.md\n?? c.md" {
		t.Fatalf("rename/untracked entries must be kept, got %q", got)
	}
}
