package autonomous

// overlap_sources.go implements the two "search before creating a PRD"
// candidates sources: registry rows from docs/plans/README.md and local
// automaton branches that have not been merged into main. The branch
// source deliberately ports only the subject-comparison idea of
// scripts/branch-audit.py and does not call that script.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// overlapCandidate is one thing a duplicate-check should consider before a
// new PRD is created.
type overlapCandidate struct {
	Source string
	// Row is the registry table row number, set only for row candidates.
	Row int
	// Ref is the branch name, set only for branch candidates.
	Ref string
	// Text is the row's second cell, or the branch name plus the
	// commit subjects main..branch for branch candidates.
	Text string
}

func overlapRowCandidates(projectRoot string) ([]overlapCandidate, error) {
	path := filepath.Join(projectRoot, "docs", "plans", "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry rows: %w", err)
	}
	var out []overlapCandidate
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitMarkdownRow(line)
		if len(cells) == 0 {
			continue
		}
		row, ok := plainInt(strings.TrimSpace(cells[0]))
		if !ok {
			continue
		}
		text := ""
		if len(cells) > 1 {
			text = strings.TrimSpace(cells[1])
		}
		out = append(out, overlapCandidate{Source: "registry_row", Row: row, Text: text})
	}
	return out, nil
}

// splitMarkdownRow splits a markdown table row into trimmed cells,
// dropping the empty pieces produced by the surrounding pipes.
func splitMarkdownRow(line string) []string {
	cells := strings.Split(line, "|")
	if cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// plainInt reports whether s is a plain non-negative integer.
func plainInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n, true
}

// gitRunner abstracts git so overlap tests can use a fake and never touch
// a real repository.
type gitRunner interface {
	run(args ...string) (stdout string, code int, err error)
}

type realGitRunner struct {
	dir string
}

// NewGitRunner returns a gitRunner that runs git with projectRoot as its
// working directory.
func NewGitRunner(projectRoot string) gitRunner {
	return realGitRunner{dir: projectRoot}
}

func (r realGitRunner) run(args ...string) (string, int, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), ee.ExitCode(), nil
		}
		return string(out), -1, err
	}
	return string(out), 0, nil
}

// overlapBranchCandidates returns a candidate for every local branch under
// refs/heads/automaton/ whose tip is not an ancestor of main, with the
// branch name and the subjects of main..<branch> as its text.
func overlapBranchCandidates(r gitRunner) ([]overlapCandidate, error) {
	list, code, err := r.run("for-each-ref", "refs/heads/automaton/", "--format=%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("list automaton branches: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("list automaton branches: exit %d: %s", code, strings.TrimSpace(list))
	}

	var out []overlapCandidate
	for _, branch := range strings.Fields(list) {
		if branch == "main" {
			continue
		}
		_, code, err := r.run("merge-base", "--is-ancestor", branch, "main")
		if err != nil {
			return nil, fmt.Errorf("check ancestor of %s: %w", branch, err)
		}
		switch code {
		case 0:
			continue // already on main
		case 1:
			// not an ancestor: unmerged candidate
		default:
			return nil, fmt.Errorf("merge-base for %s: unexpected exit code %d", branch, code)
		}
		subs, code, err := r.run("log", "--format=%s", "main.."+branch)
		if err != nil {
			return nil, fmt.Errorf("log %s: %w", branch, err)
		}
		if code != 0 {
			return nil, fmt.Errorf("log %s: exit %d: %s", branch, code, strings.TrimSpace(subs))
		}
		trimmed := strings.TrimSpace(subs)
		text := branch
		if trimmed != "" {
			text += "\n" + trimmed
		}
		out = append(out, overlapCandidate{Source: "unmerged_branch", Ref: branch, Text: text})
	}
	return out, nil
}
