package git

// generic_git.go is the plain-git provider for hosts with no forge API
// integration (a bare git server, Gitea, Forgejo). Clone and push use git
// itself over SSH or HTTPS; nothing here calls a forge API.

import (
	"context"
	"errors"
	"fmt"
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
	// Host is the configured git host this provider acts for. Its credential
	// source decides the environment of each git call. The zero value means no
	// configured host: git runs with the environment untouched.
	Host GitHostConfig
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

// host returns the configured host, filling in the kind name and base URL the
// provider was built with when the configured entry leaves them empty.
func (g *GenericGit) host() GitHostConfig {
	h := g.Host
	if h.Name == "" {
		h.Name = g.KindName
	}
	if h.Kind == "" {
		h.Kind = g.KindName
	}
	if h.BaseURL == "" {
		h.BaseURL = g.BaseURL
	}
	return h
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
	so, se, err := runHostCommand(ctx, g.host(), HostCmd{Name: "git", Args: []string{"-C", dir, "push", "--", remote, branch}}, false)
	if err != nil {
		return hostGitError("git push", err, so+se)
	}
	return nil
}

var _ Provider = (*GenericGit)(nil)
