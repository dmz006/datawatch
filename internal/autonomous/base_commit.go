package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"unicode/utf8"
)

// Base-commit resolution: one place that answers "which commit is the state
// before this work started", and one helper that reads a file as it was at a
// commit. Everything here is read-only git (rev-parse, merge-base, show)
// run through the injectable driftGitRunner, so a test substitutes a fake.

// baseCommitFileCap is the most bytes of one file readFileAtCommit returns.
const baseCommitFileCap = 262144

var (
	// ErrNoBaseCommit means no candidate for the base commit could be
	// resolved and verified. The wrapped message names what was tried.
	ErrNoBaseCommit = errors.New("no base commit could be resolved")
	// ErrPathMissingAtCommit means the path does not exist at the commit.
	ErrPathMissingAtCommit = errors.New("path does not exist at that commit")
	// ErrPathEscapesRepo means the path is empty, absolute, or climbs out of
	// the repository.
	ErrPathEscapesRepo = errors.New("path is not inside the repository")
)

// baseRulesFile is one rules or context file as it was at the base commit.
type baseRulesFile struct {
	Path      string
	Content   string
	Truncated bool
}

// baseCandidate is one place the base commit may come from. The sha is
// computed lazily so a later source costs nothing when an earlier one wins.
type baseCandidate struct {
	how string
	sha func() (string, error)
}

// resolveBaseCommit returns the commit that is the state before the work
// started, and how it was found. For a task (t != nil) the sources are, in
// order: the task's BaseSHA (the commit before its first attempt, never
// rewritten by retries), its PreTaskSHA, then the merge-base of the task
// directory's HEAD and the Automaton base branch. For the whole Automaton
// (t == nil) a worktree-mode Automaton uses the merge-base of its worktree
// HEAD and the base branch; a direct-mode one uses the BaseSHA of the task
// that started first, else that task's PreTaskSHA. Every candidate must
// resolve to a commit and be an ancestor of HEAD; one that does not is
// skipped and the next is tried. When none survive the error wraps
// ErrNoBaseCommit and names what was tried.
func (m *Manager) resolveBaseCommit(ctx context.Context, prd *PRD, t *Task) (sha, how string, err error) {
	if prd == nil {
		return "", "", fmt.Errorf("%w: no Automaton given", ErrNoBaseCommit)
	}
	dir := prd.ProjectDir
	if t != nil {
		dir = m.resolveTaskProjectDir(prd, t)
	}
	if strings.TrimSpace(dir) == "" {
		return "", "", fmt.Errorf("%w: the Automaton has no project directory", ErrNoBaseCommit)
	}
	run := m.baseCommitRunner()

	mergeBase := baseCandidate{how: "merge_base", sha: func() (string, error) {
		return m.mergeBaseWithBaseBranch(ctx, prd, dir, run)
	}}
	var cands []baseCandidate
	switch {
	case t != nil:
		cands = []baseCandidate{
			fixedCandidate("task_base_sha", t.BaseSHA),
			fixedCandidate("task_pre_sha", t.PreTaskSHA),
			mergeBase,
		}
	case prd.Git.Branch != "":
		cands = []baseCandidate{mergeBase}
	default:
		cands = []baseCandidate{
			fixedCandidate("first_task_base_sha", earliestTaskSHA(prd, func(x *Task) string { return x.BaseSHA })),
			fixedCandidate("first_task_pre_sha", earliestTaskSHA(prd, func(x *Task) string { return x.PreTaskSHA })),
		}
	}

	var tried []string
	for _, c := range cands {
		if cerr := ctx.Err(); cerr != nil {
			return "", "", cerr
		}
		cand, cerr := c.sha()
		if cerr != nil {
			tried = append(tried, fmt.Sprintf("%s (%v)", c.how, cerr))
			continue
		}
		if strings.TrimSpace(cand) == "" {
			tried = append(tried, c.how+" (empty)")
			continue
		}
		full, verr := verifyBaseCandidate(ctx, dir, cand, run)
		if verr != nil {
			tried = append(tried, fmt.Sprintf("%s (%v)", c.how, verr))
			continue
		}
		return full, c.how, nil
	}
	return "", "", fmt.Errorf("%w: tried %s", ErrNoBaseCommit, strings.Join(tried, "; "))
}

func fixedCandidate(how, sha string) baseCandidate {
	return baseCandidate{how: how, sha: func() (string, error) { return strings.TrimSpace(sha), nil }}
}

// baseCommitRunner is the git runner in effect: the manager's injected one,
// else the default.
func (m *Manager) baseCommitRunner() driftGitRunner {
	if m.driftGit != nil {
		return m.driftGit
	}
	return defaultDriftGitRunner
}

// earliestTaskSHA returns pick(task) for the earliest-started task whose
// pick is non-empty. Tasks that never started sort after every started one
// and keep their story order, so a task with no start time is only chosen
// when no started task has a value.
func earliestTaskSHA(prd *PRD, pick func(*Task) string) string {
	type entry struct {
		t   *Task
		idx int
	}
	var entries []entry
	idx := 0
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if strings.TrimSpace(pick(t)) != "" {
				entries = append(entries, entry{t, idx})
			}
			idx++
		}
	}
	if len(entries) == 0 {
		return ""
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].t.StartedAt, entries[j].t.StartedAt
		switch {
		case a != nil && b != nil:
			if !a.Equal(*b) {
				return a.Before(*b)
			}
			return entries[i].idx < entries[j].idx
		case a != nil:
			return true
		case b != nil:
			return false
		}
		return entries[i].idx < entries[j].idx
	})
	return pick(entries[0].t)
}

// verifyBaseCandidate checks that cand resolves to a commit and is an
// ancestor of HEAD in dir, returning the resolved commit.
func verifyBaseCandidate(ctx context.Context, dir, cand string, run driftGitRunner) (string, error) {
	if strings.HasPrefix(cand, "-") || strings.ContainsAny(cand, " \t\r\n\x00") {
		return "", fmt.Errorf("%q is not a commit", cand)
	}
	out, err := run(ctx, dir, "rev-parse", "--verify", cand+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%s does not resolve to a commit", shortSHA(cand))
	}
	full := strings.TrimSpace(out)
	if full == "" {
		full = cand
	}
	if _, err := run(ctx, dir, "merge-base", "--is-ancestor", full, "HEAD"); err != nil {
		return "", fmt.Errorf("%s is not an ancestor of HEAD", shortSHA(full))
	}
	return full, nil
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// mergeBaseWithBaseBranch returns the merge-base of dir's HEAD and the
// Automaton base branch, trying the local branch and then origin's copy.
func (m *Manager) mergeBaseWithBaseBranch(ctx context.Context, prd *PRD, dir string, run driftGitRunner) (string, error) {
	base := m.baseBranchFor(ctx, prd, dir, run)
	var lastErr error
	for _, ref := range []string{base, "origin/" + base} {
		out, err := run(ctx, dir, "merge-base", "HEAD", ref)
		if err != nil {
			lastErr = fmt.Errorf("merge-base with %s failed", ref)
			continue
		}
		if sha := strings.TrimSpace(out); sha != "" {
			return sha, nil
		}
		lastErr = fmt.Errorf("merge-base with %s was empty", ref)
	}
	return "", lastErr
}

// baseBranchFor names the Automaton base branch: the one recorded on the
// Automaton, else the default branch of the configured project base
// repository, else the origin default seen from dir, else "main".
func (m *Manager) baseBranchFor(ctx context.Context, prd *PRD, dir string, run driftGitRunner) string {
	if b := strings.TrimSpace(prd.Git.BaseBranch); b != "" {
		return b
	}
	if repo := strings.TrimSpace(m.Config().WorktreeBaseRepo); repo != "" {
		return defaultBranchOf(repo)
	}
	if out, err := run(ctx, dir, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			if idx := strings.LastIndex(ref, "/"); idx >= 0 {
				ref = ref[idx+1:]
			}
			if ref != "" {
				return ref
			}
		}
	}
	return "main"
}

// readFileAtCommit returns relPath as it was at commit sha in dir, read with
// git show so a working copy edited since never changes it. relPath is
// normalised and must stay inside the repo (ErrPathEscapesRepo otherwise); a
// path absent at the commit returns ErrPathMissingAtCommit. Content past
// baseCommitFileCap bytes is cut at a character boundary and truncated is
// true. Any other git failure is returned wrapped.
func readFileAtCommit(ctx context.Context, dir, sha, relPath string, run driftGitRunner) (content string, truncated bool, err error) {
	if run == nil {
		run = defaultDriftGitRunner
	}
	sha = strings.TrimSpace(sha)
	if sha == "" || strings.HasPrefix(sha, "-") || strings.ContainsAny(sha, " \t\r\n\x00:") {
		return "", false, fmt.Errorf("read at commit: %q is not a commit", sha)
	}
	rel := normalizeDriftPath(relPath)
	if rel == "" || strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return "", false, fmt.Errorf("read %q: %w", relPath, ErrPathEscapesRepo)
	}

	ref := sha + ":" + rel
	out, err := run(ctx, dir, "show", ref)
	if err != nil {
		if pathMissingErr(err) {
			return "", false, fmt.Errorf("read %s at %s: %w", rel, shortSHA(sha), ErrPathMissingAtCommit)
		}
		return "", false, fmt.Errorf("read %s at %s: %w", rel, shortSHA(sha), err)
	}
	// A directory prints as a tree listing, not file content.
	if strings.HasPrefix(out, "tree "+ref+"\n") {
		return "", false, fmt.Errorf("read %s at %s: not a file", rel, shortSHA(sha))
	}
	if len(out) > baseCommitFileCap {
		cut := baseCommitFileCap
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		return out[:cut], true, nil
	}
	return out, false, nil
}

// pathMissingErr reports whether a git show failure means the path is absent
// at the commit, from git's own message (the error text, or the stderr an
// exec error carries).
func pathMissingErr(err error) bool {
	msg := err.Error()
	var xe *exec.ExitError
	if errors.As(err, &xe) {
		msg += " " + string(xe.Stderr)
	}
	return strings.Contains(msg, "does not exist in") || strings.Contains(msg, "exists on disk, but not in")
}

// rulesAndContextFiles returns the effective rules file and context files for
// a project directory, from the resolved autonomous.rules_context policy
// (project override first, then the global values and the legacy keys). An
// empty dir reads the global values.
func (m *Manager) rulesAndContextFiles(dir string) (rulesFile string, contextFiles []string) {
	rc := m.resolvedRulesContextDir(dir)
	return rc.RulesFile, rc.ContextFiles
}

// readRulesFilesAtBase reads the configured rules and context files as they
// were at the Automaton base commit, never from the working copy. A file
// that does not exist at the base is left out, and none existing is not an
// error (an empty slice comes back).
func (m *Manager) readRulesFilesAtBase(ctx context.Context, prd *PRD) (files []baseRulesFile, sha, how string, err error) {
	sha, how, err = m.resolveBaseCommit(ctx, prd, nil)
	if err != nil {
		return nil, "", "", err
	}
	rulesFile, contextFiles := m.rulesAndContextFiles(prd.ProjectDir)
	run := m.baseCommitRunner()
	files = []baseRulesFile{}
	seen := map[string]bool{}
	for _, name := range append([]string{rulesFile}, contextFiles...) {
		key := normalizeDriftPath(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		content, trunc, rerr := readFileAtCommit(ctx, prd.ProjectDir, sha, key, run)
		if rerr != nil {
			if errors.Is(rerr, ErrPathMissingAtCommit) {
				continue
			}
			return nil, sha, how, rerr
		}
		files = append(files, baseRulesFile{Path: key, Content: content, Truncated: trunc})
	}
	return files, sha, how, nil
}
