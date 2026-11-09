package git

// generic_git.go is the plain-git provider for hosts with no forge API
// integration (a bare git server, Gitea, Forgejo). Clone and push use git
// itself over SSH or HTTPS; nothing here calls a forge API.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ErrNoForgeAPI is returned by plain-git providers for actions that need a
// forge API (pull requests, issues, releases).
var ErrNoForgeAPI = errors.New("git provider: this host has no forge API integration; open the change in the host's web interface")

// GenericGit is a Provider for a plain git host. KindName holds the
// configured kind ("git", "gitea" or "forgejo"); it cannot be called Kind
// because Kind is the Provider method.
type GenericGit struct {
	KindName string
	BaseURL  string
}

// NewGenericGit returns a plain-git provider for kind and base URL.
func NewGenericGit(kind, baseURL string) *GenericGit {
	return &GenericGit{KindName: kind, BaseURL: baseURL}
}

// Kind returns the configured kind.
func (g *GenericGit) Kind() string { return g.KindName }

// MintToken is not supported for a plain git host.
func (g *GenericGit) MintToken(_ context.Context, _ string, _ time.Duration) (*MintedToken, error) {
	return nil, ErrNotImplemented
}

// RevokeToken is not supported for a plain git host.
func (g *GenericGit) RevokeToken(_ context.Context, _ string) error { return ErrNotImplemented }

// OpenPR returns ErrNoForgeAPI.
func (g *GenericGit) OpenPR(_ context.Context, _ PROptions) (string, error) {
	return "", ErrNoForgeAPI
}

// CreateIssue returns ErrNoForgeAPI.
func (g *GenericGit) CreateIssue(_ context.Context, _ IssueOptions) (string, error) {
	return "", ErrNoForgeAPI
}

// CreateRelease returns ErrNoForgeAPI.
func (g *GenericGit) CreateRelease(_ context.Context, _ ReleaseOptions) (string, error) {
	return "", ErrNoForgeAPI
}

func (g *GenericGit) host() GitHostConfig {
	return GitHostConfig{Name: g.KindName, Kind: g.KindName, BaseURL: g.BaseURL}
}

// Clone runs a plain `git clone` of repo into dest using the shared
// transport helper. repo is a full remote or a path under BaseURL.
func (g *GenericGit) Clone(ctx context.Context, repo, dest string) error {
	return CloneGitLabRepo(ctx, g.host(), repo, dest)
}

// Push runs a plain `git push` of branch to remote from the repository at dir.
func (g *GenericGit) Push(ctx context.Context, dir, remote, branch string) error {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(remote) == "" || strings.TrimSpace(branch) == "" {
		return fmt.Errorf("Push: dir, remote and branch required")
	}
	env, _, err := HostCommandEnv(ctx, g.host(), nil)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "push", "--", remote, branch) // #nosec G204 -- fixed binary, args validated above
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w\n%s", err, strings.TrimSpace(redactHostText(string(out))))
	}
	return nil
}

var _ Provider = (*GenericGit)(nil)
