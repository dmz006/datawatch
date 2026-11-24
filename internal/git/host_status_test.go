package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil { // #nosec G306 -- test script
		t.Fatal(err)
	}
}

func TestGitHostReadiness(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	argsFile := filepath.Join(dir, "args")
	ctx := context.Background()

	t.Run("github all scopes", func(t *testing.T) {
		fakeBin(t, dir, "gh", `echo "$@" > `+argsFile+`
echo "github.com"
echo "  ✓ Logged in to github.com account octo (keyring)"
echo "  - Token: ghp_SECRET123"
echo "  - Token scopes: 'gist', 'repo', 'workflow'"
`)
		r := CheckHostReadiness(ctx, GitHostConfig{Name: "gh", Kind: "github"})
		if !r.LoggedIn || r.Account != "octo" || !r.HasRepoScope || !r.HasWorkflowScope || r.FixCommand != "" || r.Problem != "" {
			t.Fatalf("unexpected: %+v", r)
		}
		a, _ := os.ReadFile(argsFile)
		if strings.Contains(string(a), "--hostname") {
			t.Fatalf("hostname passed without base url: %s", a)
		}
		if strings.Contains(strings.Join(r.Scopes, ","), "SECRET") {
			t.Fatal("token leaked")
		}
	})

	t.Run("github missing workflow on enterprise host", func(t *testing.T) {
		fakeBin(t, dir, "gh", `echo "$@" > `+argsFile+`
echo "Logged in to ghe.example.com as octo"
echo "Token scopes: 'repo'"
`)
		r := CheckHostReadiness(ctx, GitHostConfig{Name: "ghe", Kind: "github", BaseURL: "https://ghe.example.com"})
		if !r.LoggedIn || r.HasWorkflowScope || !r.HasRepoScope {
			t.Fatalf("unexpected: %+v", r)
		}
		if r.FixCommand != "gh auth refresh -h ghe.example.com -s repo,workflow" {
			t.Fatalf("fix: %q", r.FixCommand)
		}
		a, _ := os.ReadFile(argsFile)
		if !strings.Contains(string(a), "--hostname ghe.example.com") {
			t.Fatalf("args: %s", a)
		}
	})

	t.Run("github missing workflow on github.com", func(t *testing.T) {
		fakeBin(t, dir, "gh", `echo "Logged in to github.com account octo"
echo "Token scopes: 'repo'"
`)
		r := CheckHostReadiness(ctx, GitHostConfig{Name: "gh", Kind: "github"})
		if r.FixCommand != "gh auth refresh -s repo,workflow" || r.Problem == "" {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("github not logged in", func(t *testing.T) {
		fakeBin(t, dir, "gh", "echo 'You are not logged in ghp_LEAK' >&2\nexit 1\n")
		r := CheckHostReadiness(ctx, GitHostConfig{Name: "ghe", Kind: "github", BaseURL: "https://ghe.example.com"})
		if r.LoggedIn || r.FixCommand != "gh auth login --hostname ghe.example.com" {
			t.Fatalf("unexpected: %+v", r)
		}
		if strings.Contains(r.Problem, "LEAK") {
			t.Fatal("token leaked")
		}
	})

	t.Run("gitlab logged in and missing api", func(t *testing.T) {
		fakeBin(t, dir, "glab", `echo "$@" > `+argsFile+`
echo "gitlab.example.com"
echo "  ✓ Logged in to gitlab.example.com as dev (config)"
echo "  ✓ Token scopes: api, read_user"
`)
		h := GitHostConfig{Name: "gl", Kind: "gitlab", BaseURL: "https://gitlab.example.com"}
		r := CheckHostReadiness(ctx, h)
		if !r.LoggedIn || r.Account != "dev" || !r.HasAPIScope || r.Problem != "" {
			t.Fatalf("unexpected: %+v", r)
		}
		a, _ := os.ReadFile(argsFile)
		if !strings.Contains(string(a), "--hostname gitlab.example.com") {
			t.Fatalf("args: %s", a)
		}
		fakeBin(t, dir, "glab", "echo 'Logged in to gitlab.example.com as dev'\necho 'Token scopes: read_user'\n")
		r = CheckHostReadiness(ctx, h)
		if r.HasAPIScope || r.FixCommand != "glab auth login --hostname gitlab.example.com" {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("gitlab not logged in", func(t *testing.T) {
		fakeBin(t, dir, "glab", "exit 1\n")
		r := CheckHostReadiness(ctx, GitHostConfig{Name: "gl", Kind: "gitlab"})
		if r.LoggedIn || r.FixCommand != "glab auth login --hostname gitlab.com" {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("plain git reachability", func(t *testing.T) {
		fakeBin(t, dir, "git", `echo "$@" > `+argsFile+"\nexit 0\n")
		r := CheckHostReadiness(ctx, GitHostConfig{Name: "g", Kind: "git", BaseURL: "https://git.example.com/r.git"})
		if !r.LoggedIn || len(r.Scopes) != 0 || r.Problem != "" {
			t.Fatalf("unexpected: %+v", r)
		}
		a, _ := os.ReadFile(argsFile)
		if !strings.Contains(string(a), "ls-remote --heads https://git.example.com/r.git") {
			t.Fatalf("args: %s", a)
		}
		fakeBin(t, dir, "git", "exit 128\n")
		r = CheckHostReadiness(ctx, GitHostConfig{Name: "g", Kind: "gitea", BaseURL: "https://git.example.com"})
		if r.LoggedIn || r.Problem == "" {
			t.Fatalf("unexpected: %+v", r)
		}
	})
}
