package autonomous

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// generatedLockFiles are matched by basename at any depth and dropped from the
// judge diff size: their line counts reflect the package manager, not the work
// an Automaton did.
var generatedLockFiles = []string{
	"go.sum",
	"package-lock.json",
	"npm-shrinkwrap.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"bun.lockb",
	"Cargo.lock",
	"poetry.lock",
	"Pipfile.lock",
	"uv.lock",
	"Gemfile.lock",
	"composer.lock",
}

// JudgeDiffLines counts the changed lines (added plus deleted) of the
// Automaton's branch against its merge base with the base branch, ignoring
// generated lock files. Only committed history counts: the diff runs from the
// merge base to the branch tip, so uncommitted worktree edits are not seen.
// Binary files count 0.
func JudgeDiffLines(ctx context.Context, prd *PRD, projectDir string) (int, error) {
	branch := strings.TrimSpace(prd.Git.Branch)
	if branch == "" {
		return 0, fmt.Errorf("judge diff size: automaton %s has no branch yet", prd.ID)
	}
	base := strings.TrimSpace(prd.Git.BaseBranch)
	if base == "" {
		base = defaultBranchOf(projectDir)
	}

	mergeBase, err := judgeGit(ctx, projectDir, "merge-base", base, branch)
	if err != nil {
		return 0, fmt.Errorf("judge diff size: merge base of %s and %s: %w", base, branch, err)
	}
	mergeBase = strings.TrimSpace(mergeBase)
	if mergeBase == "" {
		return 0, fmt.Errorf("judge diff size: no merge base between %s and %s", base, branch)
	}

	args := []string{"diff", "--numstat", mergeBase, branch, "--", "."}
	for _, name := range generatedLockFiles {
		args = append(args, ":(exclude,glob)**/"+name)
	}
	out, err := judgeGit(ctx, projectDir, args...)
	if err != nil {
		return 0, fmt.Errorf("judge diff size: diff %s..%s: %w", mergeBase, branch, err)
	}
	return sumNumstat(out)
}

// sumNumstat adds the added and deleted columns of `git diff --numstat`
// output. A binary row has "-" in both columns and counts 0.
func sumNumstat(out string) (int, error) {
	total := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) < 3 {
			return 0, fmt.Errorf("judge diff size: unexpected numstat row %q", line)
		}
		for _, c := range cols[:2] {
			if c == "-" {
				continue
			}
			n, err := strconv.Atoi(c)
			if err != nil {
				return 0, fmt.Errorf("judge diff size: numstat row %q: %w", line, err)
			}
			total += n
		}
	}
	return total, nil
}

// judgeGit runs git -C dir with the given context, without a shell, and
// returns stdout. On failure the error carries git's stderr.
func judgeGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git subcommands, no shell; dir and refs come from operator-controlled config and PRD fields
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return stdout.String(), nil
}
