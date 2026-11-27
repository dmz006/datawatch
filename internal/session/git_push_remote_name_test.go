package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A push remote setting that starts with "-" runs nothing at teardown, under
// either push policy, and the session still says its commits were not pushed.
func TestTeardownRefusesRemoteNameStartingWithDash(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "work"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "one"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	for _, policy := range []string{"default_branch", "branch"} {
		t.Run(policy, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "ran")
			remote := "--upload-pack=touch " + marker + "; false"
			m := &Manager{}
			m.SetGitPushSettings(func() (string, string) { return policy, remote })
			m.SetPushBeforeTeardown(func() bool { return true })
			sess := &Session{ID: "z", FullID: "h-z", ProjectDir: repo}
			rep := m.pushUnpushedBeforeTeardown(sess)
			if _, err := os.Stat(marker); err == nil {
				t.Error("git ran the command in the remote name")
			}
			if rep.Pushed || rep.Reason == "" {
				t.Errorf("report = %+v, want a refusal with its reason", rep)
			}
			if sess.PushState != "not_pushed" || !strings.HasPrefix(sess.PushWarning, "not pushed: ") {
				t.Errorf("session push state %q, warning %q: want the not-pushed warning", sess.PushState, sess.PushWarning)
			}
		})
	}
}

func TestProjectGitRefusesRemoteNameStartingWithDash(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	g := NewProjectGit(t.TempDir())
	for _, push := range []func(string, string, string, string) error{g.PushBranch, g.PushWorkBranch} {
		for _, url := range []string{"", "https://example.invalid/r.git"} {
			if err := push("--upload-pack=touch "+marker+"; false", "work", url, ""); err == nil {
				t.Error("not refused")
			}
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("git ran the command in the remote name")
	}
}
