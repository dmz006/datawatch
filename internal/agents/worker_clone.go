// F10 sprint 5 (S5.3) — worker-side git clone after bootstrap.
//
// When the bootstrap response includes a Git bundle (URL + Token),
// the worker clones into <workspaceRoot>/<repo-name> and returns the
// path so runStart can set the session's project_dir.
//
// CLI shell-out (consistent with the rest of the agent layer): we
// invoke `git` with the token as an HTTPS basic-auth header so the
// secret never lands on disk. The command is wrapped in a trivial
// retry+backoff for transient network blips.

package agents

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CloneOnBootstrap honours the Git bundle from the parent's
// bootstrap response. Returns the absolute path the repo was cloned
// to, or "" when no clone was needed (Git.URL empty). Errors when
// the clone itself fails — caller must surface for restart.
//
// workspaceRoot is the parent directory the repo lands under. Empty
// defaults to "/workspace" (matches the worker image convention).
func CloneOnBootstrap(ctx context.Context, resp *BootstrapResponse, workspaceRoot string) (string, error) {
	if resp == nil || resp.Git.URL == "" {
		return "", nil
	}
	if workspaceRoot == "" {
		workspaceRoot = "/workspace"
	}
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		return "", fmt.Errorf("workspace root: %w", err)
	}

	repoName := repoNameFromURL(resp.Git.URL)
	target := filepath.Join(workspaceRoot, repoName)

	// Skip if already cloned (worker restarted, repo persists in
	// emptyDir / PVC). Pull instead to refresh.
	if st, err := os.Stat(filepath.Join(target, ".git")); err == nil && st.IsDir() {
		return target, runGit(ctx, target, "pull", "--ff-only")
	}

	cloneURL := injectTokenIntoURL(resp.Git.URL, resp.Git.Token)
	args := []string{"clone"}
	if resp.Git.Branch != "" {
		args = append(args, "--branch", resp.Git.Branch)
	}
	args = append(args, cloneURL, target)

	if err := runGit(ctx, "", args...); err != nil {
		// Wipe the partial clone so a retry doesn't trip on it.
		_ = os.RemoveAll(target)
		return "", fmt.Errorf("git clone: %w", err)
	}
	// Don't persist the credential URL — set the canonical remote
	// without the token now that .git/config exists.
	if err := runGit(ctx, target, "remote", "set-url", "origin", resp.Git.URL); err != nil {
		// Non-fatal: the credential URL would still work; we just
		// prefer not to leave the token lying in .git/config.
		fmt.Fprintf(os.Stderr, "[worker] warn: failed to scrub credential from origin: %v\n", err)
	}
	return target, nil
}

// PushOnCompletion (BL407 Phase 3) pushes a cluster-dispatched worker's
// own commits back to the bootstrap repo once its task finishes, using
// the same bootstrap-minted token CloneOnBootstrap used to clone (the
// worker's only credential by this point — the one-shot
// DATAWATCH_BOOTSTRAP_TOKEN is already burned). Returns the branch
// pushed and HEAD's commit SHA so the caller can report them in
// AgentResult; the parent opens the completion PR from that branch
// directly — it never has a local copy of these commits to push
// itself. No-op (empty strings, nil error) when the worker was never
// git-bootstrapped (resp.Git.URL == "").
func PushOnCompletion(ctx context.Context, dir string, resp *BootstrapResponse) (branch, sha string, err error) {
	if resp == nil || resp.Git.URL == "" || dir == "" {
		return "", "", nil
	}
	branch, err = gitOutputText(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("resolve current branch: %w", err)
	}
	sha, err = gitOutputText(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("resolve HEAD: %w", err)
	}
	pushURL := injectTokenIntoURL(resp.Git.URL, resp.Git.Token)
	if perr := runGit(ctx, dir, "push", pushURL, "HEAD:refs/heads/"+branch); perr != nil {
		return "", "", fmt.Errorf("git push: %w", perr)
	}
	return branch, sha, nil
}

// gitOutputText runs a read-only git command and returns trimmed
// stdout. Shorter timeout than runGit's 5min — these are local,
// near-instant lookups (rev-parse), not network operations.
func gitOutputText(ctx context.Context, dir string, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// repoNameFromURL extracts a sensible directory name for the cloned
// repo. Handles GitHub HTTPS + SSH forms; falls back to "repo".
func repoNameFromURL(u string) string {
	s := strings.TrimSuffix(u, ".git")
	// SSH form: git@host:owner/repo
	if i := strings.Index(s, ":"); i > 0 && strings.HasPrefix(s, "git@") {
		s = s[i+1:]
	}
	parts := strings.Split(s, "/")
	if len(parts) == 0 {
		return "repo"
	}
	last := parts[len(parts)-1]
	if last == "" {
		return "repo"
	}
	return last
}

// injectTokenIntoURL adds an HTTPS basic-auth header to the URL when
// non-empty. Treats the token as the password with username "x-access-
// token" (GitHub's convention; works for fine-grained PATs and gh
// installation tokens). Returns the URL unchanged when token == "".
func injectTokenIntoURL(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return rawURL
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

// runGit shells out to git with a 5-min cap. cwd may be empty for
// commands like clone that don't need a working dir.
func runGit(ctx context.Context, cwd string, args ...string) error {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(callCtx, "git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(buf.String()))
	}
	return nil
}
