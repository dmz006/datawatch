// GitHub provider — shells out to the `gh` CLI.
//
// Why CLI shell-out (same rationale as docker/kubectl drivers):
//   * `gh auth status` is the source of truth for the operator's
//     credentials — re-implementing PAT/installation flows would
//     diverge from what the operator already configured
//   * Zero new dependencies; gh's auth handling, retries, and
//     pagination are battle-tested
//   * Trivial debugging — every call reproducible at a shell prompt
//
// Trade-offs:
//   * Each call forks `gh` (~50-200ms)
//   * Requires `gh auth login` to have run; we surface gh's own
//     error message verbatim when not authenticated

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GitHub is the gh-CLI-backed Provider implementation.
//
// Token minting today: returns the operator's existing PAT (whatever
// `gh auth token` prints). This is intentionally a v1 simplification
// — Sprint 5+ can promote to fine-grained per-spawn tokens via
// `gh api -X POST /user/installations/.../access_tokens` once we
// have the GitHub App registered. The TTL is honoured at the broker
// layer (broker tracks expiry + revokes on session end).
type GitHub struct {
	// Bin is the gh binary; defaults to "gh".
	Bin string
}

// NewGitHub returns a GitHub provider with default binary path.
func NewGitHub() *GitHub { return &GitHub{Bin: "gh"} }

// Kind implements Provider.
func (g *GitHub) Kind() string { return "github" }

// MintToken returns the host's current `gh` auth token. ExpiresAt is
// best-effort: gh doesn't expose the underlying PAT expiry, so we
// honour the requested TTL at the broker level (broker re-mints
// after expiry).
func (g *GitHub) MintToken(ctx context.Context, repo string, ttl time.Duration) (*MintedToken, error) {
	tok, err := g.run(ctx, "auth", "token")
	if err != nil {
		return nil, fmt.Errorf("gh auth token: %w (run `gh auth login` on the parent host)", err)
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return nil, fmt.Errorf("gh auth token: empty (no auth configured)")
	}
	// Cheap probe: is the token actually usable for this repo? We
	// only run it when repo is non-empty so token-only callers don't
	// pay the round-trip.
	if repo != "" {
		if _, err := g.run(ctx, "api", "-X", "GET",
			"/repos/"+repo, "--silent",
			"--jq", ".name"); err != nil {
			return nil, fmt.Errorf("gh api /repos/%s: %w (check token scope + repo access)", repo, err)
		}
	}
	return &MintedToken{
		Token:     tok,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

// RevokeToken is a no-op for the v1 PAT-passthrough flow — we don't
// have permission to revoke the operator's host token. Returns nil so
// the broker's lifecycle still completes cleanly. When fine-grained
// per-spawn tokens land, this becomes `gh api -X DELETE …`.
func (g *GitHub) RevokeToken(_ context.Context, _ string) error { return nil }

// OpenPR shells out to `gh pr create`. Title + body via flags so the
// commit message + body don't get clobbered by `gh`'s editor.
func (g *GitHub) OpenPR(ctx context.Context, opts PROptions) (string, error) {
	if opts.Repo == "" {
		return "", fmt.Errorf("OpenPR: Repo required")
	}
	if opts.HeadBranch == "" {
		return "", fmt.Errorf("OpenPR: HeadBranch required")
	}
	args := []string{
		"pr", "create",
		"--repo", opts.Repo,
		"--head", opts.HeadBranch,
		"--title", opts.Title,
		"--body", opts.Body,
	}
	if opts.BaseBranch != "" {
		args = append(args, "--base", opts.BaseBranch)
	}
	out, err := g.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w\n%s", err, strings.TrimSpace(out))
	}
	// gh prints the PR URL on its own line; grab the last URL-looking line.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "https://") {
			return line, nil
		}
	}
	// gh returned 0 but no URL — surface the raw output so operators can debug.
	return strings.TrimSpace(out), nil
}

// run is gh exec with a 30s cap; combined stdout+stderr returned.
func (g *GitHub) run(ctx context.Context, args ...string) (string, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gh"
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// _ silences vet's "unused json" diagnostic in the v1 implementation
// — Sprint 5+ token rotation parses gh's JSON response.
var _ = json.Marshal
