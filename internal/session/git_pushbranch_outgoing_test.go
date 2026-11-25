package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	dwgit "github.com/dmz006/datawatch/internal/git"
)

// The worker pull request hook's push runs the outgoing check too.
func TestProjectGit_PushBranchRefusesPlantedSecret(t *testing.T) {
	bare, dir := setupBareClone(t)
	gitRunT(t, dir, "checkout", "-q", "-b", "feat/x")
	line := "GITHUB_TOKEN=\"" + "gh" + "p_" + strings.Repeat("aB3d", 7) + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "env"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "work")
	dwgit.SetDaemonScanPolicy(func() dwgit.SecretScanPolicy { return dwgit.SecretScanPolicy{Mode: dwgit.ScanEnforce} })
	t.Cleanup(func() { dwgit.SetDaemonScanPolicy(nil) })

	if err := NewProjectGit(dir).PushBranch("origin", "feat/x", "", ""); err == nil {
		t.Fatal("want a refusal")
	}
	if got := bareHead(t, bare, "feat/x"); got != "" {
		t.Fatalf("the branch was pushed: %s", got)
	}
}
