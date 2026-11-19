package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
)

// launchArtifactsSetup commits an AGENT.md base (dsoBase) in a fresh seeded
// repo and then mimes the session launcher writing its three artifacts into
// the tree root: worker-mcp.json (channel.WorkerMCPConfigFileName),
// worker-starting-context.md and AGENT.md rewritten as base + the
// launcher-appended daemon sections (dsoDaemon, platformcontext marker
// blocks).
func launchArtifactsSetup(t *testing.T) string {
	t.Helper()
	repo := gitRepoWithDWSeed(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte(dsoBase), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "AGENT.md")
	gitRun(t, repo, "commit", "-q", "-m", "agent")
	if err := os.WriteFile(filepath.Join(repo, channel.WorkerMCPConfigFileName), []byte(`{"mcpServers":{"echo":{"command":"echo"}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "worker-starting-context.md"), []byte("# Worker starting context\n\nWork from the task spec only.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENT.md"), []byte(dsoDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestDetectUncommittedWorkLauncherFilesReportNothing: a tree whose only
// dirt is the three launcher artifacts reports nothing at all — the two
// untracked launcher files are the daemon's and the AGENT.md delta is its
// appended sections.
func TestDetectUncommittedWorkLauncherFilesReportNothing(t *testing.T) {
	repo := launchArtifactsSetup(t)
	files, err := DetectUncommittedWork(context.Background(), repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("launcher artifacts must report no uncommitted work, got %v", files)
	}
}

// TestDetectUncommittedWorkLauncherFilesKeepOtherDirt: the exclusion is
// exact — beside the same launcher artifacts a genuine file still reports,
// and the launcher artifacts themselves stay out of the report.
func TestDetectUncommittedWorkLauncherFilesKeepOtherDirt(t *testing.T) {
	repo := launchArtifactsSetup(t)
	if err := os.WriteFile(filepath.Join(repo, "real-change.md"), []byte("operator work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "worker-mcp.json"), []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := DetectUncommittedWork(context.Background(), repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"real-change.md", "sub/worker-mcp.json"}
	if len(files) != len(want) {
		t.Fatalf("want %v, got %v", want, files)
	}
	for i, f := range files {
		if f.Path != want[i] {
			t.Fatalf("want %v, got %v", want, files)
		}
	}
}
