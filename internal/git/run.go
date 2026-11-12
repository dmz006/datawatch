package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Run executes one git command with exec (no shell) in the given working
// directory and returns its stdout. dir must name an explicit directory —
// the command is never run in the caller's own working directory — and
// args are passed to git verbatim, so an argument carrying spaces or
// metacharacters reaches git as a single argument.
//
// stdout is returned exactly as produced, even on a non-zero exit, so a
// caller can inspect partial output; on failure the error wraps the exit
// status and carries git's trimmed stderr for the operator to read.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	return RunBin(ctx, "git", dir, args...)
}

// RunBin behaves like Run but with an explicit binary name; tests point
// it at a fake executable instead of real git.
func RunBin(ctx context.Context, bin, dir string, args ...string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("git: working directory must not be empty")
	}
	if len(args) == 0 {
		return "", fmt.Errorf("git: no command given")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return stdout.String(), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, stderrText)
		}
		return stdout.String(), fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
