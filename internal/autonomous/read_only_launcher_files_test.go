package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
)

// roLauncherSetup prepares a repo whose committed state includes AGENT.md
// (base dsoBase), a read-only task and its clean baseline status, then
// mimics the real session launcher writing its three artifacts into the tree
// root: the two untracked launcher files (worker-mcp.json via
// channel.WorkerMCPConfigFileName and worker-starting-context.md) and AGENT.md
// with the launcher-appended daemon sections. Nothing else in the tree
// changes.
func roLauncherSetup(t *testing.T) (string, *Manager, *PRD, *Task, string) {
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
	// The launcher fires after the baseline: worker-mcp.json (a minimal
	// mcpServers JSON object), worker-starting-context.md (a small context
	// document) and AGENT.md rewritten as base + the launcher-appended
	// sections.
	if err := os.WriteFile(filepath.Join(repo, channel.WorkerMCPConfigFileName), []byte(`{"mcpServers":{"echo":{"command":"echo","args":["ping"]}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "worker-starting-context.md"), []byte("# Worker starting context\n\nWork from the task spec only.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, mgr, prd, tk, base
}

// TestReadOnlyLauncherFilesLeaveNoViolation is the launcher-fidelity case: a
// read-only task that otherwise changed nothing leaves no violation behind,
// and its final tree status is empty.
func TestReadOnlyLauncherFilesLeaveNoViolation(t *testing.T) {
	_, mgr, prd, tk, base := roLauncherSetup(t)
	if err := mgr.readOnlyViolation(context.Background(), prd, tk, base); err != nil {
		t.Fatalf("launcher artifacts must not violate a read-only task: %v", err)
	}
	now, ok := mgr.readOnlyTreeStatus(context.Background(), prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	if got := strings.TrimSpace(now); got != "" {
		t.Fatalf("tree status after launcher artifacts must be empty, got %q", got)
	}
}

// TestReadOnlyLauncherFilesRealUntrackedStillViolates: beside the launcher
// artifacts a genuine new untracked file, and the violation names it.
func TestReadOnlyLauncherFilesRealUntrackedStillViolates(t *testing.T) {
	repo, mgr, prd, tk, base := roLauncherSetup(t)
	if err := os.WriteFile(filepath.Join(repo, "operator-notes.txt"), []byte("real work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := mgr.readOnlyViolation(context.Background(), prd, tk, base)
	if err == nil {
		t.Fatal("a real untracked file beside the launcher artifacts must violate")
	}
	if !strings.Contains(err.Error(), "operator-notes.txt") {
		t.Fatalf("violation must name the real file, got %q", err)
	}
}

// TestReadOnlyLauncherFilesSubfolderFileStillViolates: a same-named file in
// a subfolder is not a launcher file (the launcher root is the project
// root), so it is still a violation.
func TestReadOnlyLauncherFilesSubfolderFileStillViolates(t *testing.T) {
	repo, mgr, prd, tk, base := roLauncherSetup(t)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "worker-mcp.json"), []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := mgr.readOnlyViolation(context.Background(), prd, tk, base)
	if err == nil {
		t.Fatal("sub/worker-mcp.json must violate: subfolders are not the launcher root")
	}
	if !strings.Contains(err.Error(), "sub/worker-mcp.json") {
		t.Fatalf("violation must name the subfolder file, got %q", err)
	}
}

// TestReadOnlyLauncherFilesAGENTRealEditStillViolates: AGENT.md rewritten
// with a real operator edit beside the launcher-appended sections is still
// a violation — the daemon-section exclusion does not cover real edits.
func TestReadOnlyLauncherFilesAGENTRealEditStillViolates(t *testing.T) {
	repo, mgr, prd, tk, base := roLauncherSetup(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte("operator edit line\n"+dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.readOnlyViolation(context.Background(), prd, tk, base); err == nil {
		t.Fatal("a real edit beside the launcher-appended sections must violate")
	}
}
