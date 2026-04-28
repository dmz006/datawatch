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

// TagCheckpoint (BL29) creates an annotated tag at HEAD pointing at
// the pre-/post-session state. Tag name pattern:
//   datawatch-{kind}-{sessionID}    (kind: "pre" | "post")
// kind must be "pre" or "post". Idempotent — re-tagging is allowed
// (uses git tag -f) so a session restart can update the marker.
func (g *ProjectGit) TagCheckpoint(kind, sessionID, task string) error {
	if !g.IsRepo() {
		return nil
	}
	if kind != "pre" && kind != "post" {
		return fmt.Errorf("invalid checkpoint kind %q (want pre|post)", kind)
	}
	tag := fmt.Sprintf("datawatch-%s-%s", kind, sessionID)
	msg := fmt.Sprintf("datawatch %s-session checkpoint for %s: %s",
		kind, sessionID, truncateStr(task, 60))
	return runGit(g.dir, "tag", "-f", "-a", tag, "-m", msg)
}

// Rollback (BL29) hard-resets the working tree to the pre-session
// checkpoint tag. Returns an error if the pre-tag doesn't exist or
// the working tree has uncommitted changes (operator must commit /
// stash / accept loss explicitly via `force=true`).
func (g *ProjectGit) Rollback(sessionID string, force bool) error {
	if !g.IsRepo() {
		return fmt.Errorf("not a git repo: %s", g.dir)
	}
	tag := "datawatch-pre-" + sessionID
	if _, err := gitOutput(g.dir, "rev-parse", "--verify", tag); err != nil {
		return fmt.Errorf("pre-session tag not found: %s", tag)
	}
	if !force && g.HasChanges() {
		return fmt.Errorf("uncommitted changes present — pass force=true to discard")
	}
	return runGit(g.dir, "reset", "--hard", tag)
}

// HasChanges returns true if there are uncommitted changes in dir.
func (g *ProjectGit) HasChanges() bool {
	out, err := gitOutput(g.dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// DiffStat (BL10) parses `git diff --stat HEAD~1..HEAD` and returns
// a structured summary of files changed + insertions + deletions. If
// HEAD~1 doesn't exist (initial commit) or the dir is not a repo,
// returns a zero-valued DiffStat with err nil so callers can treat
// "no diff" identically to "no changes".
func (g *ProjectGit) DiffStat() (DiffStat, error) {
	if !g.IsRepo() {
		return DiffStat{}, nil
	}
	out, err := gitOutput(g.dir, "diff", "--shortstat", "HEAD~1..HEAD")
	if err != nil {
		// Likely no parent commit (initial). Treat as empty.
		return DiffStat{}, nil
	}
	return parseShortstat(strings.TrimSpace(out)), nil
}

// DiffStat is a structured summary of `git diff --shortstat`.
//
// Example shortstat: " 3 files changed, 47 insertions(+), 12 deletions(-)"
type DiffStat struct {
	Files      int    `json:"files"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Summary    string `json:"summary"` // human-readable, e.g. "3 files, +47/-12"
}

// IsZero reports whether the diff summarizes no change.
func (d DiffStat) IsZero() bool {
	return d.Files == 0 && d.Insertions == 0 && d.Deletions == 0
}

// DiffNames (Phase 4 follow-up, v5.26.67) returns the list of files
// changed in HEAD~1..HEAD. Companion to DiffStat — operator-asked
// post-session hook that populates Task.FilesTouched on autonomous
// spawns. Returns nil when not a repo or when HEAD~1 doesn't exist.
// Capped at 50 entries to keep the autonomous PRD record bounded
// (per the Phase 4 design's 5KB-per-story budget).
func (g *ProjectGit) DiffNames() []string {
	if !g.IsRepo() {
		return nil
	}
	out, err := gitOutput(g.dir, "diff", "--name-only", "HEAD~1..HEAD")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l)
		if len(files) >= 50 {
			break
		}
	}
	return files
}

func parseShortstat(line string) DiffStat {
	// Tokens we care about: "<n> file[s] changed", "<n> insertion[s]", "<n> deletion[s]".
	out := DiffStat{}
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		var n int
		if _, err := fmt.Sscanf(part, "%d", &n); err != nil {
			continue
		}
		switch {
		case strings.Contains(part, "file"):
			out.Files = n
		case strings.Contains(part, "insertion"):
			out.Insertions = n
		case strings.Contains(part, "deletion"):
			out.Deletions = n
		}
	}
	if !out.IsZero() {
		out.Summary = fmt.Sprintf("%d file(s), +%d/-%d",
			out.Files, out.Insertions, out.Deletions)
	}
	return out
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
