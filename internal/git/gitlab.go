// GitLab provider — stub.
//
// Schema-level acceptance is already in profile.GitSpec.Provider so
// operators can author profiles ahead of time. Concrete
// implementation (likely shelling out to `glab` CLI, mirroring
// github.go's approach) lands when a GitLab-hosted repo enters the
// active rotation. Tracked as task #164.

package git

import (
	"context"
	"time"
)

// GitLab is the placeholder Provider implementation. Every real
// operation returns ErrNotImplemented; Kind() returns "gitlab" so
// callers can still inspect the provider for routing decisions.
type GitLab struct{}

// NewGitLab returns the stub.
func NewGitLab() *GitLab { return &GitLab{} }

// Kind implements Provider.
func (*GitLab) Kind() string { return "gitlab" }

// MintToken is a stub — returns ErrNotImplemented.
func (*GitLab) MintToken(_ context.Context, _ string, _ time.Duration) (*MintedToken, error) {
	return nil, ErrNotImplemented
}

// RevokeToken is a stub — returns ErrNotImplemented.
func (*GitLab) RevokeToken(_ context.Context, _ string) error { return ErrNotImplemented }

// OpenPR is a stub — returns ErrNotImplemented.
func (*GitLab) OpenPR(_ context.Context, _ PROptions) (string, error) {
	return "", ErrNotImplemented
}
