package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
)

// launcherSnapshotSetup is a fresh git repo in which the fake session
// launcher has done everything it does at session start: written its two
// root tooling files (channel.WorkerMCPConfigFileName = worker-mcp.json and
// worker-starting-context.md), and the worker has since modified one real
// tracked file. Nothing else in the tree changes.
func launcherSnapshotSetup(t *testing.T) (repo string, realFile string) {
	t.Helper()
	repo = gitRepoWithDWSeed(t)
	realFile = "notes.txt"
	if err := os.WriteFile(filepath.Join(repo, realFile), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", realFile)
	gitRun(t, repo, "commit", "-q", "-m", "real file")
	// The launcher fires: its two root files appear, and the worker then
	// modifies its one real file.
	if err := os.WriteFile(filepath.Join(repo, channel.WorkerMCPConfigFileName), []byte(`{"mcpServers":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "worker-starting-context.md"), []byte("# Worker starting context\n\nWork from the task spec only.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, realFile), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, realFile
}

// TestSnapshotLauncherFilesExcluded is the snapshot side of the launcher
// fidelity guarantee: the launcher's own root files are session artifacts,
// not the worker's output, so snapshotCandidates must not return them —
// the worker's real modified file stays a candidate.
func TestSnapshotLauncherFilesExcluded(t *testing.T) {
	repo, realFile := launcherSnapshotSetup(t)

	got, err := snapshotCandidates(context.Background(), repo)
	if err != nil {
		t.Fatalf("snapshotCandidates: %v", err)
	}
	byName := make(map[string]bool, len(got))
	for _, f := range got {
		byName[f] = true
	}
	if !byName[realFile] {
		t.Fatalf("real modified file %q missing from candidates %v", realFile, got)
	}
	for _, l := range launcherToolingFiles() {
		if byName[l] {
			t.Fatalf("launcher file %q must not be a snapshot candidate (candidates: %v)", l, got)
		}
	}
}

// TestSnapshotLauncherFilesSubfolderStays is the exact-root-match boundary:
// a same-named file inside a subfolder is the worker's work, not the
// launcher's, and must remain a candidate.
func TestSnapshotLauncherFilesSubfolderStays(t *testing.T) {
	repo, _ := launcherSnapshotSetup(t)
	sub := filepath.Join(repo, "pkg", channel.WorkerMCPConfigFileName)
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte(`{"mcpServers":{"echo":{"command":"echo"}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := snapshotCandidates(context.Background(), repo)
	if err != nil {
		t.Fatalf("snapshotCandidates: %v", err)
	}
	want := "pkg/" + channel.WorkerMCPConfigFileName
	found := false
	for _, f := range got {
		if f == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("subfolder file %q must stay a snapshot candidate (candidates: %v)", want, got)
	}
}
