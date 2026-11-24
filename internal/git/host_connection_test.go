package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHostTestConnection(t *testing.T) {
	ctx := context.Background()

	t.Run("passing probe", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		apiArgs := filepath.Join(dir, "api-args")
		fakeBin(t, dir, "gh", `case "$1" in
auth)
  echo "github.com"
  echo "  ✓ Logged in to github.com account octo (keyring)"
  echo "  - Token: ghp_SECRET123"
  echo "  - Token scopes: 'repo', 'workflow'"
  exit 0 ;;
api)
  echo "$@" > `+apiArgs+`
  echo '{"login":"octo","id":1}'
  exit 0 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "gh", Kind: "github"})
		if !r.Ok || r.Account != "octo" || r.FixCommand != "" {
			t.Fatalf("unexpected: %+v", r)
		}
		if r.Message != "Connected to github.com as octo." {
			t.Fatalf("message: %q", r.Message)
		}
		a, err := os.ReadFile(apiArgs)
		if err != nil {
			t.Fatalf("probe did not run: %v", err)
		}
		if strings.TrimSpace(string(a)) != "api user" {
			t.Fatalf("probe args: %q", a)
		}
		if strings.Contains(r.Message+r.Account+r.FixCommand, "SECRET") {
			t.Fatal("token leaked into result")
		}
	})

	t.Run("passing probe on self-hosted github passes hostname", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		apiArgs := filepath.Join(dir, "api-args")
		fakeBin(t, dir, "gh", `case "$1" in
auth)
  echo "Logged in to ghe.example.com as octo"
  echo "Token scopes: 'repo', 'workflow'"
  exit 0 ;;
api)
  echo "$@" > `+apiArgs+`
  echo '{"login":"octo"}'
  exit 0 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "ghe", Kind: "github", BaseURL: "https://ghe.example.com"})
		if !r.Ok || r.Account != "octo" || r.Message != "Connected to ghe.example.com as octo." {
			t.Fatalf("unexpected: %+v", r)
		}
		a, _ := os.ReadFile(apiArgs)
		if strings.TrimSpace(string(a)) != "api user --hostname ghe.example.com" {
			t.Fatalf("probe args: %q", a)
		}
	})

	t.Run("probe without login keeps readiness account", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		fakeBin(t, dir, "gh", `case "$1" in
auth)
  echo "Logged in to github.com as octo"
  echo "Token scopes: 'repo', 'workflow'"
  exit 0 ;;
api)
  echo '{"id":1}'
  exit 0 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "gh", Kind: "github"})
		if !r.Ok || r.Account != "octo" || r.Message != "Connected to github.com as octo." {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("failing probe", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		fakeBin(t, dir, "gh", `case "$1" in
auth)
  echo "github.com"
  echo "  ✓ Logged in to github.com account octo (keyring)"
  echo "  - Token scopes: 'repo', 'workflow'"
  exit 0 ;;
api)
  echo "HTTP 401: Bad credentials ghp_SECRET123" >&2
  exit 1 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "gh", Kind: "github"})
		if r.Ok {
			t.Fatalf("expected failure: %+v", r)
		}
		if r.FixCommand != "gh auth login --hostname github.com" {
			t.Fatalf("fix: %q", r.FixCommand)
		}
		if r.Message == "" {
			t.Fatal("expected a plain-language message")
		}
		if strings.Contains(r.Message+r.Account+r.FixCommand, "SECRET") || strings.Contains(r.Message, "401") {
			t.Fatalf("probe output leaked: %+v", r)
		}
	})

	t.Run("not logged in", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		apiArgs := filepath.Join(dir, "api-args")
		fakeBin(t, dir, "gh", `case "$1" in
auth)
  echo "You are not logged into any GitHub hosts. Run gh auth login to authenticate." >&2
  exit 1 ;;
api)
  echo "$@" > `+apiArgs+`
  exit 0 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "gh", Kind: "github"})
		if r.Ok {
			t.Fatalf("expected failure: %+v", r)
		}
		if r.FixCommand != "gh auth login --hostname github.com" {
			t.Fatalf("fix: %q", r.FixCommand)
		}
		if r.Message != "Not logged in to github.com with gh." {
			t.Fatalf("message: %q", r.Message)
		}
		if _, err := os.Stat(apiArgs); err == nil {
			t.Fatal("probe ran although the host is not logged in")
		}
	})

	t.Run("gitlab passing probe", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		apiArgs := filepath.Join(dir, "api-args")
		fakeBin(t, dir, "glab", `case "$1" in
auth)
  echo "Logged in to gitlab.example.com as octo"
  echo "Token scopes: 'api'"
  exit 0 ;;
api)
  echo "$@" > `+apiArgs+`
  echo '{"username":"octo"}'
  exit 0 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "gl", Kind: "gitlab", BaseURL: "https://gitlab.example.com"})
		if !r.Ok || r.Account != "octo" || r.Message != "Connected to gitlab.example.com as octo." {
			t.Fatalf("unexpected: %+v", r)
		}
		a, _ := os.ReadFile(apiArgs)
		if strings.TrimSpace(string(a)) != "api user --hostname gitlab.example.com" {
			t.Fatalf("probe args: %q", a)
		}
	})

	t.Run("gitlab failing probe", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		fakeBin(t, dir, "glab", `case "$1" in
auth)
  echo "Logged in to gitlab.com as octo"
  echo "Token scopes: 'api'"
  exit 0 ;;
api)
  echo "401 Unauthorized" >&2
  exit 1 ;;
esac
exit 2
`)
		r := TestHostConnection(ctx, GitHostConfig{Name: "gl", Kind: "gitlab"})
		if r.Ok || r.FixCommand != "glab auth login --hostname gitlab.com" {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("plain remote probe", func(t *testing.T) {
		repo := filepath.Join(t.TempDir(), "remote.git")
		if out, err := exec.Command("git", "init", "--bare", repo).CombinedOutput(); err != nil {
			t.Skipf("git init unavailable: %v: %s", err, out)
		}
		r := TestHostConnection(ctx, GitHostConfig{Name: "g", Kind: "git", BaseURL: repo})
		if !r.Ok || r.FixCommand != "" || !strings.HasPrefix(r.Message, "Reached ") {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("plain remote unreachable", func(t *testing.T) {
		r := TestHostConnection(ctx, GitHostConfig{Name: "g", Kind: "git", BaseURL: filepath.Join(t.TempDir(), "missing.git")})
		if r.Ok || r.Message == "" {
			t.Fatalf("unexpected: %+v", r)
		}
	})

	t.Run("probe failure messages", func(t *testing.T) {
		if got := probeFailureMessage(context.DeadlineExceeded, "GitHub", "github.com", "gh"); !strings.Contains(got, "timed out after 15 seconds") {
			t.Fatalf("timeout: %q", got)
		}
		if got := probeFailureMessage(exec.ErrNotFound, "GitHub", "github.com", "gh"); !strings.Contains(got, "gh command is not installed") {
			t.Fatalf("missing tool: %q", got)
		}
		if got := probeFailureMessage(errors.New("boom"), "", "example.com", "git"); got != "Could not reach example.com." {
			t.Fatalf("plain: %q", got)
		}
		if got := probeFailureMessage(errors.New("boom"), "GitLab", "gitlab.com", "glab"); !strings.Contains(got, "login may have expired") {
			t.Fatalf("generic: %q", got)
		}
	})

	t.Run("account parsing", func(t *testing.T) {
		if got := parseProbeAccount(`{"login":" octo "}`, "login"); got != "octo" {
			t.Fatalf("login: %q", got)
		}
		if got := parseProbeAccount("not json", "login"); got != "" {
			t.Fatalf("bad json: %q", got)
		}
		if got := parseProbeAccount(`{"id":1}`, "login"); got != "" {
			t.Fatalf("missing field: %q", got)
		}
	})

	t.Run("connected message without account", func(t *testing.T) {
		if got := connectedMessage("", "gitlab.com"); got != "Connected to gitlab.com." {
			t.Fatalf("message: %q", got)
		}
	})
}
