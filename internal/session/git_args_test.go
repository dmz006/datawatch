package session

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func newArgsRepo(t *testing.T) *ProjectGit {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", a...)
		cmd.Dir = dir
		cmd.Env = append(cleanEnv(), "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	return NewProjectGit(dir)
}

func cleanEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TMUX=") || strings.HasPrefix(e, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, e)
	}
	return env
}

func TestGitArgsRejectOptionLikeValues(t *testing.T) {
	bad := []string{"--upload-pack=x", "-c core.sshCommand=x", "-x", "a\nb", "a\x00b", ""}
	for _, v := range bad {
		if validateGitRef("branch", v) == nil {
			t.Errorf("ref %q accepted", v)
		}
		if validateGitRemoteName(v) == nil {
			t.Errorf("remote %q accepted", v)
		}
		if validateGitURL(v) == nil {
			t.Errorf("url %q accepted", v)
		}
	}
}

func TestGitArgsAcceptNormalValues(t *testing.T) {
	for _, v := range []string{"main", "feature/x-1", "datawatch-pre-ab12"} {
		if err := validateGitRef("branch", v); err != nil {
			t.Errorf("ref %q: %v", v, err)
		}
	}
	if err := validateGitRemoteName("origin"); err != nil {
		t.Error(err)
	}
	if err := validateGitURL("https://example.com/r.git"); err != nil {
		t.Error(err)
	}
	if validateGitRef("tag", "a..b") == nil || validateGitRef("tag", "a b") == nil {
		t.Error("malformed ref accepted")
	}
}

func TestPushBranchRejectsOptionLikeArgs(t *testing.T) {
	g := newArgsRepo(t)
	cases := [][3]string{
		{"--upload-pack=x", "main", ""},
		{"origin", "--upload-pack=x", ""},
		{"origin", "main", "-c core.sshCommand=x"},
	}
	for _, c := range cases {
		if err := g.PushBranch(c[0], c[1], c[2], ""); err == nil || !strings.Contains(err.Error(), "invalid git") {
			t.Errorf("PushBranch%v: got %v", c, err)
		}
	}
}

func TestCheckpointTagAndRollbackWork(t *testing.T) {
	g := newArgsRepo(t)
	if err := g.TagCheckpoint("pre", "ab12", "task"); err != nil {
		t.Fatal(err)
	}
	if err := g.Rollback("ab12", true); err != nil {
		t.Fatal(err)
	}
	if err := g.TagCheckpoint("pre", "a b", "task"); err == nil {
		t.Error("malformed session id accepted")
	}
	if err := g.Rollback("-x", true); err == nil {
		t.Error("option-like rollback accepted")
	}
}
