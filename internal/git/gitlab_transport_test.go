package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func transportGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitLabTransport(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()

	t.Run("helper args default host", func(t *testing.T) {
		got := gitLabCredentialHelperArgs("/r", GitHostConfig{Kind: "gitlab"})
		want := []string{"-C", "/r", "config", "--local", "credential.https://gitlab.com.helper", "!glab auth git-credential"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("args = %v, want %v", got, want)
		}
	})

	t.Run("helper args custom host", func(t *testing.T) {
		got := gitLabCredentialHelperArgs("/r", GitHostConfig{BaseURL: "https://gl.example.com/"})
		if got[4] != "credential.https://gl.example.com.helper" {
			t.Fatalf("key = %q", got[4])
		}
	})

	t.Run("ssh changes nothing", func(t *testing.T) {
		for _, base := range []string{"ssh://git@gl.example.com", "git@gl.example.com:grp"} {
			if a := gitLabCredentialHelperArgs("/r", GitHostConfig{BaseURL: base}); a != nil {
				t.Fatalf("%s: args = %v, want nil", base, a)
			}
		}
		dir := t.TempDir()
		transportGit(t, dir, "init")
		if err := ConfigureGitLabTransport(ctx, dir, GitHostConfig{BaseURL: "ssh://git@gl.example.com"}); err != nil {
			t.Fatal(err)
		}
		cfg, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
		if strings.Contains(string(cfg), "credential") {
			t.Fatalf("ssh config changed:\n%s", cfg)
		}
	})

	t.Run("configure sets local helper", func(t *testing.T) {
		dir := t.TempDir()
		transportGit(t, dir, "init")
		host := GitHostConfig{Kind: "gitlab", BaseURL: "https://gl.example.com"}
		if err := ConfigureGitLabTransport(ctx, dir, host); err != nil {
			t.Fatal(err)
		}
		got := transportGit(t, dir, "config", "--local", "credential.https://gl.example.com.helper")
		if got != "!glab auth git-credential" {
			t.Fatalf("helper = %q", got)
		}
	})

	t.Run("remote built from base and repo", func(t *testing.T) {
		if got := gitLabCloneRemote(GitHostConfig{}, "grp/proj.git"); got != "https://gitlab.com/grp/proj.git" {
			t.Fatalf("got %q", got)
		}
		if got := gitLabCloneRemote(GitHostConfig{BaseURL: "https://gl.example.com/"}, "grp/proj"); got != "https://gl.example.com/grp/proj" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("environment from HostCommandEnv", func(t *testing.T) {
		env, source, err := HostCommandEnv(ctx, GitHostConfig{Kind: "gitlab"}, nil)
		if err != nil || source != "host_login" || len(env) != len(os.Environ()) {
			t.Fatalf("env=%d source=%q err=%v", len(env), source, err)
		}
		err = CloneGitLabRepo(ctx, GitHostConfig{Kind: "gitlab", CredentialSource: "vault"}, "g/p", t.TempDir())
		if err == nil {
			t.Fatal("vault source without secret should fail")
		}
	})

	t.Run("clone local bare repo", func(t *testing.T) {
		root := t.TempDir()
		bare := filepath.Join(root, "remote.git")
		transportGit(t, root, "init", "--bare", "-b", "main", bare)
		work := filepath.Join(root, "work")
		transportGit(t, root, "init", "-b", "main", work)
		if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("hi"), 0o600); err != nil {
			t.Fatal(err)
		}
		transportGit(t, work, "add", ".")
		transportGit(t, work, "commit", "-m", "init")
		transportGit(t, work, "push", bare, "main")

		dest := filepath.Join(root, "clone")
		host := GitHostConfig{Kind: "gitlab", BaseURL: root}
		if err := CloneGitLabRepo(ctx, host, "remote.git", dest); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(filepath.Join(dest, "f.txt")); err != nil || string(b) != "hi" {
			t.Fatalf("clone content = %q, err %v", b, err)
		}
		if err := CloneGitLabRepo(ctx, host, "", filepath.Join(root, "x")); err == nil {
			t.Fatal("empty repo should fail")
		}
	})
}
