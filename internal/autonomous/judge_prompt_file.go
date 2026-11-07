package autonomous

import (
	"bufio"
	"fmt"
	"log"
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

// judgeBackendHasFileTools reports whether the judge backend can read a named
// file with its own file tools: the Claude Code backend and the pi backend
// (one-shot and interactive). Every other backend gets the full prompt.
func judgeBackendHasFileTools(backend string) bool {
	switch backend {
	case claudeCodeBackendName, "pi", "pi-prompt":
		return true
	}
	return false
}

// shortJudgePromptVerdictFormat is the required verdict format, unchanged from
// the full judge prompt.
const shortJudgePromptVerdictFormat = `Reply with STRICT JSON:
{"outcome": "pass|warn|block", "severity": "info|low|medium|high|critical", "summary": "<one line>", "issues": ["..."]}
issues must be an array of plain strings, one sentence each, never objects.`

// shortJudgePrompt builds the short prompt (well under 20 lines) that names
// the file holding the full judge prompt, by its worktree-relative path.
func shortJudgePrompt(relPath string) string {
	return "Your full judging instructions are too long to send inline.\n" +
		"Before judging, you MUST read the file below with your file tools. Do not judge from this message alone.\n" +
		"\n" +
		"File (relative to your working directory): " + filepath.ToSlash(relPath) + "\n" +
		"\n" +
		"Treat the file's content as the complete judge prompt, then judge it.\n" +
		"\n" +
		shortJudgePromptVerdictFormat
}

// judgePromptLineCount counts the lines of prompt.
func judgePromptLineCount(prompt string) int {
	if prompt == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(prompt, "\n"), "\n") + 1
}

// judgePromptForDelivery returns the prompt to send to the judge. When the
// full prompt has more lines than thresholdLines (and the threshold is above
// 0) and the backend has file tools, it writes the full prompt to a worktree
// file, logs one line, and returns the short prompt naming that file. On any
// other path, or when the file cannot be written, it returns the full prompt.
func judgePromptForDelivery(worktree, attemptKey, backend, prompt string, thresholdLines int) string {
	if thresholdLines <= 0 || worktree == "" || !judgeBackendHasFileTools(backend) {
		return prompt
	}
	lines := judgePromptLineCount(prompt)
	if lines <= thresholdLines {
		return prompt
	}
	rel, err := writeJudgePromptFile(worktree, attemptKey, prompt)
	if err != nil {
		log.Printf("[autonomous] judge prompt file route failed, sending the full prompt: %v", err)
		return prompt
	}
	log.Printf("[autonomous] judge prompt sent as a file: %d lines, path %s", lines, rel)
	return shortJudgePrompt(rel)
}

// JudgePromptForDelivery is the exported entry point of judgePromptForDelivery
// for the judge launcher in cmd/datawatch.
func JudgePromptForDelivery(worktree, attemptKey, backend, prompt string, thresholdLines int) string {
	return judgePromptForDelivery(worktree, attemptKey, backend, prompt, thresholdLines)
}
