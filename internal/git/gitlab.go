// GitLab provider — shells out to the `glab` CLI.
//
// Same rationale as github.go: glab owns the operator's GitLab
// credentials and host configuration, so we reuse its auth instead of
// re-implementing token flows. Token minting stays out of scope and
// returns ErrNotImplemented. Tracked as task #164.

package git

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GitLab is the glab-CLI-backed Provider implementation. Minting and
// revoking tokens return ErrNotImplemented; OpenPR shells out to
// `glab mr create`; Kind() returns "gitlab".
type GitLab struct {
	// Bin is the glab binary; defaults to "glab".
	Bin string
	// Host is the configured git host this provider acts for. Its credential
	// source decides the environment of each glab call (see runHostCommand).
	// The zero value means no configured host: glab runs with the environment
	// untouched.
	Host GitHostConfig
}

// NewGitLab returns a GitLab provider with the default binary path.
func NewGitLab() *GitLab { return &GitLab{Bin: "glab"} }

// Kind implements Provider.
func (*GitLab) Kind() string { return "gitlab" }

// MintToken is a stub — returns ErrNotImplemented.
func (*GitLab) MintToken(_ context.Context, _ string, _ time.Duration) (*MintedToken, error) {
	return nil, ErrNotImplemented
}

// RevokeToken is a stub — returns ErrNotImplemented.
func (*GitLab) RevokeToken(_ context.Context, _ string) error { return ErrNotImplemented }

// OpenPR shells out to `glab mr create` and returns the merge request
// URL glab prints on stdout.
func (g *GitLab) OpenPR(ctx context.Context, opts PROptions) (string, error) {
	if opts.Repo == "" {
		return "", fmt.Errorf("OpenPR: Repo required")
	}
	if opts.HeadBranch == "" {
		return "", fmt.Errorf("OpenPR: HeadBranch required")
	}
	args := []string{
		"mr", "create",
		"--repo", opts.Repo,
		"--source-branch", opts.HeadBranch,
	}
	if opts.BaseBranch != "" {
		args = append(args, "--target-branch", opts.BaseBranch)
	}
	args = append(args,
		"--title", opts.Title,
		"--description", opts.Body,
		"--yes",
	)
	stdout, combined, err := g.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("glab mr create: %w\n%s", err, strings.TrimSpace(combined))
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			return line, nil
		}
	}
	return "", fmt.Errorf("glab mr create: no merge request URL in output:\n%s", strings.TrimSpace(stdout))
}

// CreateIssue shells out to `glab issue create` and returns the issue
// URL glab prints on stdout. Sibling to OpenPR.
func (g *GitLab) CreateIssue(ctx context.Context, opts IssueOptions) (string, error) {
	if opts.Repo == "" {
		return "", fmt.Errorf("CreateIssue: Repo required")
	}
	if opts.Title == "" {
		return "", fmt.Errorf("CreateIssue: Title required")
	}
	stdout, combined, err := g.run(ctx, gitlabIssueCreateArgs(opts)...)
	if err != nil {
		return "", fmt.Errorf("glab issue create: %w\n%s", err, strings.TrimSpace(combined))
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			return line, nil
		}
	}
	return "", fmt.Errorf("glab issue create: no issue URL in output:\n%s", strings.TrimSpace(stdout))
}

// gitlabIssueCreateArgs builds the glab argv for CreateIssue. Kept
// separate so the argument list can be checked without running glab.
func gitlabIssueCreateArgs(opts IssueOptions) []string {
	return []string{
		"issue", "create",
		"--repo", opts.Repo,
		"--title", opts.Title,
		"--description", opts.Body,
		"--yes",
	}
}

// run executes glab with a 30s cap. It returns stdout alone (for URL
// parsing) and stdout+stderr combined (for error messages).
func (g *GitLab) run(ctx context.Context, args ...string) (string, string, error) {
	bin := g.Bin
	if bin == "" {
		bin = "glab"
	}
	so, se, err := runHostCommand(ctx, g.Host, HostCmd{Name: bin, Args: args, Timeout: 30 * time.Second}, false)
	return so, so + se, err
}
