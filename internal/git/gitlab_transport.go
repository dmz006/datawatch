package git

// gitlab_transport.go wires git's own transport to the GitLab host login so
// clone and push over HTTPS use the credentials `glab` already holds. No token
// is read, stored or logged here.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const defaultGitLabBaseURL = "https://gitlab.com"

// gitLabBase returns the host base URL, defaulting to https://gitlab.com.
func gitLabBase(host GitHostConfig) string {
	base := strings.TrimRight(strings.TrimSpace(host.BaseURL), "/")
	if base == "" {
		return defaultGitLabBaseURL
	}
	return base
}

// gitLabIsSSH reports whether the host base URL names an SSH remote.
func gitLabIsSSH(base string) bool {
	if strings.HasPrefix(base, "ssh://") {
		return true
	}
	return !strings.Contains(base, "://") && strings.Contains(base, "@") && strings.Contains(base, ":")
}

// gitLabCredentialHelperArgs builds the `git config` arguments (after "git")
// that set glab as the HTTPS credential helper for the host, scoped to the
// repository at dir. It returns nil for an SSH remote.
func gitLabCredentialHelperArgs(dir string, host GitHostConfig) []string {
	base := gitLabBase(host)
	if gitLabIsSSH(base) {
		return nil
	}
	return []string{
		"-C", dir, "config", "--local",
		"credential." + base + ".helper", "!glab auth git-credential",
	}
}

// ConfigureGitLabTransport sets the repository's local git config so the HTTPS
// credential helper for host is `glab auth git-credential`, making git clone
// and git push use the host login. For an SSH remote it changes nothing and
// relies on the SSH agent.
func ConfigureGitLabTransport(ctx context.Context, dir string, host GitHostConfig) error {
	args := gitLabCredentialHelperArgs(dir, host)
	if args == nil {
		return nil
	}
	env, _, err := HostCommandEnv(ctx, host, nil)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed binary, args built from validated config
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config credential helper: %w\n%s", err, strings.TrimSpace(redactHostText(string(out))))
	}
	return nil
}

// gitLabCloneRemote builds the clone remote from the base URL and repo path. A
// repo that is already a full remote (scheme, scp-style or absolute path) is
// used as given.
func gitLabCloneRemote(host GitHostConfig, repo string) string {
	repo = strings.TrimSpace(repo)
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "git@") {
		return repo
	}
	return gitLabBase(host) + "/" + strings.Trim(repo, "/")
}

// CloneGitLabRepo runs `git clone` of the remote built from host.BaseURL
// (default https://gitlab.com) and repo into dest, with the environment from
// HostCommandEnv for host.
func CloneGitLabRepo(ctx context.Context, host GitHostConfig, repo, dest string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("CloneGitLabRepo: repo required")
	}
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("CloneGitLabRepo: dest required")
	}
	env, _, err := HostCommandEnv(ctx, host, nil)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--", gitLabCloneRemote(host, repo), dest) // #nosec G204 -- fixed binary, args built from validated config
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, strings.TrimSpace(redactHostText(string(out))))
	}
	return nil
}
