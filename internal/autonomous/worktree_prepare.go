// Post-creation hygiene for per-PRD git worktrees.
// internal/docsindex/embedded.go go:embeds assets/docs-bm25-index.json,
// which is git-ignored like every other generated artifact in this repo
// — so a fresh checkout made by `git worktree add` is missing the file
// and `go build ./internal/docsindex/` (and the daemon build that
// embeds it) fails until the Makefile's docs-index target happens to
// run.
// prepareWorktree drops in the same '{}' placeholder the Makefile uses
// and makes sure it stays out of git's tracking, without ever
// touching an already-generated index or staging anything.
package autonomous

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// docsIndexPlaceholderPath is the worktree-relative path of the
// embed target (the go:embed pattern) that must exist for the build to succeed.
const docsIndexPlaceholderPath = "internal/docsindex/assets/docs-bm25-index.json"

// prepareWorktree brings a freshly created (or reused) worktree to a
// buildable state: it drops the docs-bm25-index.json placeholder in
// place when this repo's docsindex package is present and verifies the
// placeholder is git-ignored, adding it to the worktree's own
// info/exclude file if it isn't. Never runs `git add` or any
// staging/commit command. Callers treat any error as non-fatal (see
// prepareWorktreeBestEffort in worktree.go) — preparation hygiene
// must not fail worktree creation itself.
func prepareWorktree(path string) error {
	docsDir := filepath.Join(path, "internal", "docsindex")
	if fi, err := os.Stat(docsDir); err != nil || !fi.IsDir() {
		return nil // not this repo's layout (e.g. a different project's checkout); leave it alone
	}
	if err := ensureDocsIndexPlaceholder(path); err != nil {
		return fmt.Errorf("worktree prepare: %s: %w", docsIndexPlaceholderPath, err)
	}
	if out, err := runGitIn(path, "check-ignore", "-q", docsIndexPlaceholderPath); err == nil {
		return nil // already ignored (repo .gitignore carries the pattern)
	} else if exitStatusOf(err) != 1 {
		// check-ignore only exits 1 for "not ignored"; anything else
		// (128, missing git, ...) is a real failure worth surfacing.
		return fmt.Errorf("worktree prepare: git check-ignore in %s: %v\n%s", path, err, out)
	}
	excludePath, err := worktreeExcludePath(path)
	if err != nil {
		return err
	}
	return appendGitExcludeLine(excludePath, "/"+strings.TrimPrefix(docsIndexPlaceholderPath, "/"))
}

// ensureDocsIndexPlaceholder guarantees <path>/internal/docsindex/assets/
// docs-bm25-index.json exists, creating the assets dir and writing the
// minimal empty index ('{}') only if the file is absent. Only acts when
// <path>/internal/docsindex is a directory — other repos are untouched.
// An existing file (even an empty one, or a fully generated index) is
// left byte-for-byte as-is, mtime included.
func ensureDocsIndexPlaceholder(path string) error {
	docsDir := filepath.Join(path, "internal", "docsindex")
	if fi, err := os.Stat(docsDir); err != nil || !fi.IsDir() {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(docsDir, "assets"), 0o755); err != nil {
		return err
	}
	placeholder := filepath.Join(docsDir, "assets", "docs-bm25-index.json")
	if _, err := os.Stat(placeholder); err == nil {
		return nil // already present: do not rewrite or mtime-churn it
	}
	return os.WriteFile(placeholder, []byte("{}\n"), 0o644)
}

// worktreeExcludePath resolves the worktree's info/exclude file via
// `git rev-parse --git-path info/exclude` (cwd = worktree), joining
// with the worktree path when git reports it relative.
func worktreeExcludePath(worktree string) (string, error) {
	out, err := runGitIn(worktree, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return "", fmt.Errorf("worktree prepare: resolve info/exclude in %s: %v\n%s", worktree, err, out)
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return "", fmt.Errorf("worktree prepare: git rev-parse returned empty info/exclude path for %s", worktree)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(worktree, p)
	}
	return p, nil
}

// appendGitExcludeLine appends line to excludePath if (and only if) no
// existing line already matches it. Creates the parent directory (the
// info/ dir may not exist yet) and never rewrites existing contents.
func appendGitExcludeLine(excludePath, line string) error {
	if data, err := os.ReadFile(excludePath); err == nil && gitExcludeHasLine(string(data), line) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, _ := os.ReadFile(excludePath) // re-check just in case of concurrent appends
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		if _, werr := f.WriteString("\n"); werr != nil {
			return werr
		}
	}
	_, err = f.WriteString(line + "\n")
	return err
}

// gitExcludeHasLine reports whether any line of ignoreFile's contents
// (or git-check-ignore-style ignore syntax) equals line.
func gitExcludeHasLine(ignoreFile, line string) bool {
	sc := bufio.NewScanner(strings.NewReader(ignoreFile))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == line {
			return true
		}
	}
	return false
}

// exitStatusOf extracts the process exit code from a command error, or
// -1 when the error isn't a process-exit error at all.
func exitStatusOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
