// Package git is the provider abstraction for F10 sprint 5 — token
// broker, worker clone, and PR-on-complete all flow through this
// interface so adding a forge (gitlab, gitea, …) is one new file.
//
// Implementations:
//
//   GitHub  — shells out to the `gh` CLI. Tokens minted via either the
//             host's `gh auth status` PAT or a fine-grained personal
//             token (`gh api -X POST .../installations/.../access_tokens`).
//   GitLab  — stub that returns ErrNotImplemented until S5+ promotes it.
//             Schema-level acceptance lives in profile.GitSpec.Provider.
//
// All methods are context-aware so the calling token broker can honour
// per-session deadlines without leaking goroutines.

package git

import (
	"context"
	"errors"
	"time"
)

// ErrNotImplemented is returned by stub providers (currently GitLab)
// for any non-stubbed call. Callers should surface this verbatim so
// operators see exactly which provider needs implementation work.
var ErrNotImplemented = errors.New("git provider: not implemented")

// MintedToken is the result of a Provider.MintToken call. The token
// itself is opaque — callers must not parse or modify it. ExpiresAt
// is in UTC.
type MintedToken struct {
	Token     string
	ExpiresAt time.Time
}

// PROptions captures the common fields needed to open a pull request.
// HeadBranch is the worker's working branch; BaseBranch defaults to
// the repo's default branch when empty.
type PROptions struct {
	Repo       string // "owner/repo" for github; "group/project" for gitlab
	HeadBranch string
	BaseBranch string // "" → provider default branch
	Title      string
	Body       string
}

// Provider abstracts a forge. Concrete implementations:
//
//   GitHub.Kind()  → "github"
//   GitLab.Kind()  → "gitlab"
//
// The interface is intentionally narrow — the F10 lifecycle only
// needs token mint/revoke and PR open. Cross-provider feature parity
// (issues, releases, comments) lands when a concrete need surfaces.
type Provider interface {
	// Kind returns the provider name as it appears in
	// ProjectProfile.Git.Provider ("github" | "gitlab").
	Kind() string

	// MintToken issues a short-lived token scoped to repo, valid for
	// ttl. Caller stores the token + expiry; provider does NOT
	// retain state.
	MintToken(ctx context.Context, repo string, ttl time.Duration) (*MintedToken, error)

	// RevokeToken invalidates a token previously returned by MintToken.
	// Best-effort: provider implementations may swallow "already
	// revoked" / "not found" errors silently. Caller should treat a
	// nil error as "the token is no longer usable" not "the call
	// definitely reached the provider".
	RevokeToken(ctx context.Context, token string) error

	// OpenPR creates a pull/merge request from HeadBranch into
	// BaseBranch (or the repo's default branch). Returns the public
	// URL of the resulting PR.
	OpenPR(ctx context.Context, opts PROptions) (string, error)
}

// Resolve returns the Provider implementation matching kind. Unknown
// kinds get the stub provider (errors on any call) so callers can
// still inspect Kind() for logging without crashing.
func Resolve(kind string) Provider {
	switch kind {
	case "github":
		return NewGitHub()
	case "gitlab":
		return NewGitLab()
	default:
		return &stubProvider{kind: kind}
	}
}

// stubProvider is the fallback for unknown kinds. Every call returns
// ErrNotImplemented; Kind() echoes the requested kind so error
// messages are informative.
type stubProvider struct{ kind string }

func (s *stubProvider) Kind() string { return s.kind }
func (s *stubProvider) MintToken(_ context.Context, _ string, _ time.Duration) (*MintedToken, error) {
	return nil, ErrNotImplemented
}
func (s *stubProvider) RevokeToken(_ context.Context, _ string) error  { return ErrNotImplemented }
func (s *stubProvider) OpenPR(_ context.Context, _ PROptions) (string, error) {
	return "", ErrNotImplemented
}
