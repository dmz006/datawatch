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
	_ = runGit(g.dir, "config", "user.email", "claude-signal@localhost")
	_ = runGit(g.dir, "config", "user.name", "claude-signal")
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
