package autonomous

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// judgePromptDir is the worktree-relative directory that holds judge prompts
// handed over as files.
const judgePromptDir = ".datawatch/judge"

// judgePromptExcludeLine keeps the judge prompt directory out of commits.
const judgePromptExcludeLine = ".datawatch/judge/"

// sanitizeJudgeAttemptKey reduces an attempt key to letters, digits and dash
// so it is always a safe file name.
func sanitizeJudgeAttemptKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "attempt"
	}
	return b.String()
}

// ensureJudgeExcluded appends the judge directory to the worktree's git
// exclude file when it is not already listed.
func ensureJudgeExcluded(worktree string) error {
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return fmt.Errorf("resolve git exclude file: %w", err)
	}
	excl := strings.TrimSpace(string(out))
	if !filepath.IsAbs(excl) {
		excl = filepath.Join(worktree, excl)
	}
	if f, err := os.Open(excl); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == judgePromptExcludeLine {
				_ = f.Close()
				return nil
			}
		}
		_ = f.Close()
	}
	if err := os.MkdirAll(filepath.Dir(excl), 0o700); err != nil {
		return err
	}
	existing, _ := os.ReadFile(excl) // #nosec G304 -- path resolved from git
	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	f, err := os.OpenFile(excl, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path resolved from git
	if err != nil {
		return err
	}
	if _, err := f.WriteString(prefix + judgePromptExcludeLine + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writeJudgePromptFile hands an oversized judge prompt over as a file. It
// writes prompt to <worktree>/.datawatch/judge/<attempt-key>-prompt.md, makes
// sure the directory is git-excluded so it is never committed, and returns the
// path relative to the worktree.
func writeJudgePromptFile(worktree, attemptKey, prompt string) (string, error) {
	if err := ensureJudgeExcluded(worktree); err != nil {
		return "", err
	}
	dir := filepath.Join(worktree, filepath.FromSlash(judgePromptDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create judge prompt dir: %w", err)
	}
	rel := filepath.Join(filepath.FromSlash(judgePromptDir), sanitizeJudgeAttemptKey(attemptKey)+"-prompt.md")
	if err := os.WriteFile(filepath.Join(worktree, rel), []byte(prompt), 0o600); err != nil {
		return "", fmt.Errorf("write judge prompt file: %w", err)
	}
	return rel, nil
}
