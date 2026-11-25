// Completion never moves the remote's default branch, never creates or pushes
// a tag, and every push it makes goes through the outgoing check. All remotes
// are local bare repositories.

package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
)

// completionRepo is a work clone whose remote already has main, plus a PRD
// pointing at it.
func completionRepo(t *testing.T, branch string) (m *Manager, prd *PRD, work, remote string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	work, remote = shipRepo(t)
	sgit(t, work, "push", "-q", "origin", "main")
	if branch != "main" {
		sgit(t, work, "checkout", "-q", "-b", branch)
	}
	commitText(t, work, "work.txt", "finished work\n")
	prd, err = m.CreatePRD("spec", work, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.Git.Branch = branch
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return m, prd, work, remote
}

func commitText(t *testing.T, work, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sgit(t, work, "add", "-A")
	sgit(t, work, "commit", "-q", "-m", "add "+name)
}

// plantedSecretLine is assembled at runtime so no credential-shaped text is
// committed to this repository.
func plantedSecretLine() string {
	return "GITHUB_TOKEN=\"" + "gh" + "p_" + strings.Repeat("aB3d", 7) + "\"\n"
}

func enforceScan(m *Manager) {
	m.SetSecretScanSettings(func() git.SecretScanPolicy { return git.SecretScanPolicy{Mode: git.ScanEnforce} })
}

func shipOn(t *testing.T, push, tag string) {
	t.Helper()
	SetShipConfig(config.AutonomousShipConfig{Push: push, Tag: tag, CIWatch: "off"})
	t.Cleanup(func() { SetShipConfig(config.AutonomousShipConfig{}) })
}

func remoteRefs(t *testing.T, remote string) string {
	t.Helper()
	return sgit(t, remote, "for-each-ref", "--format=%(refname) %(objectname)")
}

func TestCompletion_ShipTagOnCreatesNoTag(t *testing.T) {
	shipOn(t, "on", "on")
	m, prd, work, remote := completionRepo(t, "work")
	mainBefore := sgit(t, remote, "rev-parse", "main")

	m.handleWorktreeCompletion(prd)

	if got := sgit(t, work, "tag", "-l"); got != "" {
		t.Errorf("completion created a local tag: %q", got)
	}
	if got := sgit(t, remote, "tag", "-l"); got != "" {
		t.Errorf("completion pushed a tag: %q", got)
	}
	if got := sgit(t, remote, "rev-parse", "main"); got != mainBefore {
		t.Errorf("remote main moved from %s to %s", mainBefore, got)
	}
	if got, want := sgit(t, remote, "rev-parse", "work"), sgit(t, work, "rev-parse", "HEAD"); got != want {
		t.Errorf("the work branch should be pushed: remote %s, want %s", got, want)
	}
}

// Shadow tag mode at completion runs nothing and says the commands would not
// run with tag mode on.
func TestCompletion_ShadowTagRecordsAndSaysSo(t *testing.T) {
	shipOn(t, "off", "shadow")
	m, prd, work, remote := completionRepo(t, "work")
	before := remoteRefs(t, remote)

	res := m.runShipStep(prd)

	if res == nil || res.Err != nil || !strings.Contains(strings.Join(res.Notes, " "), "completion never creates or pushes a tag") {
		t.Fatalf("ship result %+v", res)
	}
	if got := remoteRefs(t, remote); got != before || sgit(t, work, "tag", "-l") != "" {
		t.Error("shadow must run nothing")
	}
}

func TestCompletion_ShipNeverMovesRemoteMain(t *testing.T) {
	shipOn(t, "on", "on")
	m, prd, work, remote := completionRepo(t, "main")
	before := remoteRefs(t, remote)

	m.handleWorktreeCompletion(prd)

	if got := remoteRefs(t, remote); got != before {
		t.Errorf("remote changed:\nbefore %s\nafter  %s", before, got)
	}
	if got := sgit(t, work, "tag", "-l"); got != "" {
		t.Errorf("completion created a local tag: %q", got)
	}
}

func TestCompletion_PushPolicyNeverMovesRemoteMain(t *testing.T) {
	for _, tc := range []struct{ policy, branch string }{
		{git.PushBranch, "main"},
		{git.PushDefaultBranch, "work"},
	} {
		t.Run(tc.policy+"_from_"+tc.branch, func(t *testing.T) {
			m, prd, _, remote := completionRepo(t, tc.branch)
			m.SetGitPushSettings(func() (string, string) { return tc.policy, "origin" })
			before := remoteRefs(t, remote)

			m.pushValidatedWork(prd, true)

			if got := remoteRefs(t, remote); got != before {
				t.Errorf("remote changed:\nbefore %s\nafter  %s", before, got)
			}
			saved, _ := m.store.GetPRD(prd.ID)
			if saved.Git.PushState != PushStateNotPushed || !strings.Contains(saved.Git.PushWarning, "default branch") {
				t.Errorf("state %q warning %q, want not_pushed naming the default branch", saved.Git.PushState, saved.Git.PushWarning)
			}
		})
	}
}

func TestCompletion_ShipPushOfPlantedSecretIsRefused(t *testing.T) {
	shipOn(t, "on", "off")
	m, prd, work, remote := completionRepo(t, "work")
	enforceScan(m)
	commitText(t, work, "env", plantedSecretLine())
	before := remoteRefs(t, remote)

	res := m.runShipStep(prd)

	if res == nil || res.Err == nil {
		t.Fatalf("want a refused ship step, got %+v", res)
	}
	if got := remoteRefs(t, remote); got != before {
		t.Errorf("remote changed:\nbefore %s\nafter  %s", before, got)
	}
	if strings.Contains(res.Err.Error(), strings.Repeat("aB3d", 7)) {
		t.Error("the refusal prints the secret")
	}
}

func TestCompletion_AutoPRPushOfPlantedSecretIsRefused(t *testing.T) {
	shipOn(t, "off", "off")
	m, prd, work, remote := completionRepo(t, "work")
	enforceScan(m)
	commitText(t, work, "env", plantedSecretLine())
	prd.Git.AutoPR = true
	before := remoteRefs(t, remote)

	m.handleWorktreeCompletion(prd)

	if got := remoteRefs(t, remote); got != before {
		t.Errorf("remote changed:\nbefore %s\nafter  %s", before, got)
	}
	saved, _ := m.store.GetPRD(prd.ID)
	if saved.Git.PRURL != "" {
		t.Errorf("a pull request was recorded for refused work: %s", saved.Git.PRURL)
	}
}
