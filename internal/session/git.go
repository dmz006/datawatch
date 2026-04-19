package session

import (
	"fmt"
	"os/exec"
	"strings"
)

// ProjectGit manages git operations on the user's project directory.
type ProjectGit struct {
	dir string
}

// NewProjectGit creates a new ProjectGit for the given directory.
func NewProjectGit(dir string) *ProjectGit {
	return &ProjectGit{dir: dir}
}

// IsRepo returns true if dir is inside a git repository.
func (g *ProjectGit) IsRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = g.dir
	return cmd.Run() == nil
}

// Init initializes a git repository in dir.
func (g *ProjectGit) Init() error {
	if err := runGit(g.dir, "init", "-b", "main"); err != nil {
		return runGit(g.dir, "init")
	}
	_ = runGit(g.dir, "config", "user.email", "datawatch@localhost")
	_ = runGit(g.dir, "config", "user.name", "datawatch")
	return nil
}

// PreSessionCommit creates a snapshot commit before the session starts.
func (g *ProjectGit) PreSessionCommit(sessionID, task string) error {
	_ = runGit(g.dir, "add", "-A")
	status, _ := gitOutput(g.dir, "status", "--porcelain")
	if strings.TrimSpace(status) == "" {
		return nil // nothing to commit
	}
	msg := fmt.Sprintf("pre-session[%s]: %s", sessionID, truncateStr(task, 60))
	return runGit(g.dir, "commit", "-m", msg)
}

// PostSessionCommit creates a final commit after the session ends.
func (g *ProjectGit) PostSessionCommit(sessionID, task string, state State) error {
	_ = runGit(g.dir, "add", "-A")
	status, _ := gitOutput(g.dir, "status", "--porcelain")
	if strings.TrimSpace(status) == "" {
		return nil
	}
	msg := fmt.Sprintf("session[%s](%s): %s", sessionID, state, truncateStr(task, 60))
	return runGit(g.dir, "commit", "-m", msg)
}

// HasChanges returns true if there are uncommitted changes in dir.
func (g *ProjectGit) HasChanges() bool {
	out, err := gitOutput(g.dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// CurrentBranch returns the name of the currently checked-out branch
// (or "HEAD" for a detached HEAD). Empty + error when not a repo.
func (g *ProjectGit) CurrentBranch() (string, error) {
	out, err := gitOutput(g.dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

// PushBranch pushes branch to remote, ephemerally re-injecting
// originURL with token-based HTTPS basic-auth so the credential
// never lands in .git/config. Origin URL is restored on return.
//
// F10 S5.4 — used by the post-session PR hook to push the worker's
// commits back to the project repo before opening a PR.
//
// remoteName is typically "origin"; pass empty for "origin".
func (g *ProjectGit) PushBranch(remoteName, branch, originURL, token string) error {
	if remoteName == "" {
		remoteName = "origin"
	}
	if branch == "" {
		return fmt.Errorf("PushBranch: branch required")
	}
	if originURL == "" {
		// Fall back to the configured origin (no token injection — caller
		// presumably has ambient creds).
		return runGit(g.dir, "push", "-u", remoteName, branch)
	}

	// Snapshot the current origin so we can restore it on exit.
	prevURL, _ := gitOutput(g.dir, "remote", "get-url", remoteName)
	prevURL = strings.TrimSpace(prevURL)

	authURL := injectTokenIntoHTTPS(originURL, token)
	if err := runGit(g.dir, "remote", "set-url", remoteName, authURL); err != nil {
		return fmt.Errorf("set credential URL: %w", err)
	}
	defer func() {
		// Restore the canonical (no-token) URL so the credential
		// never persists in .git/config across calls.
		restore := originURL
		if prevURL != "" {
			restore = prevURL
		}
		_ = runGit(g.dir, "remote", "set-url", remoteName, restore)
	}()

	return runGit(g.dir, "push", "-u", remoteName, branch)
}

// injectTokenIntoHTTPS adds an HTTPS basic-auth header to an https://
// URL with x-access-token as username. Returns rawURL unchanged when
// token is empty or scheme isn't http/https. Mirrors
// internal/agents/worker_clone.go's helper but lives here so the
// session package doesn't need to depend on agents.
func injectTokenIntoHTTPS(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	const httpsPrefix = "https://"
	const httpPrefix = "http://"
	prefix := ""
	switch {
	case strings.HasPrefix(rawURL, httpsPrefix):
		prefix = httpsPrefix
	case strings.HasPrefix(rawURL, httpPrefix):
		prefix = httpPrefix
	default:
		return rawURL
	}
	rest := rawURL[len(prefix):]
	// Skip if already has user info — operator-supplied creds win.
	if i := strings.Index(rest, "@"); i >= 0 && i < strings.Index(rest+"/", "/") {
		return rawURL
	}
	return prefix + "x-access-token:" + token + "@" + rest
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v in %s: %w\n%s", args, dir, err, out)
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
