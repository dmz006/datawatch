package autonomous

import (
	"context"
	"log"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
)

// driftGitRunner runs one git command in dir and returns its stdout.
// Injectable so tests (and callers with their own git plumbing) can
// substitute a fake; nil means defaultDriftGitRunner.
type driftGitRunner func(ctx context.Context, dir string, args ...string) (string, error)

// defaultDriftGitRunner shells out to git with -C dir. Read-only
// subcommands only (diff, log) — this file never mutates the repo.
func defaultDriftGitRunner(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed read-only git subcommands; dir comes from operator-controlled PRD ProjectDir, same trust model as runGitIn (worktree.go)
	out, err := cmd.Output()
	return string(out), err
}

// CapturePreTaskDirtyFiles returns the sorted, de-duplicated set of
// tracked files with unstaged or staged-but-uncommitted modifications
// in dir: git diff --name-only HEAD ∪ git diff --name-only --cached HEAD.
// Best-effort: an empty dir or any git error yields nil, never an error.
func CapturePreTaskDirtyFiles(ctx context.Context, dir string, run driftGitRunner) []string {
	if dir == "" {
		return nil
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	unstaged, err := run(ctx, dir, "diff", "--name-only", "HEAD")
	if err != nil {
		return nil
	}
	staged, err := run(ctx, dir, "diff", "--name-only", "--cached", "HEAD")
	if err != nil {
		return nil
	}
	return sortedUnion(splitLines(unstaged), splitLines(staged))
}

// CommittedFilesSince returns the de-duplicated union of every file
// touched by any commit in preSHA..HEAD in dir (git log --name-only).
// An empty preSHA yields nil, nil.
func CommittedFilesSince(ctx context.Context, dir, preSHA string, run driftGitRunner) ([]string, error) {
	if preSHA == "" {
		return nil, nil
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	out, err := run(ctx, dir, "log", preSHA+"..HEAD", "--name-only", "--format=")
	if err != nil {
		return nil, err
	}
	return sortedUnion(splitLines(out)), nil
}

// DetectForeignFileDrift returns the sorted files that a task's own
// commit(s) swept in although they were already dirty or untracked
// before its worker session started and were never planned by the
// task: (a) in actualCommittedFiles AND (b) in preTaskUntracked ∪
// preTaskDirty AND (c) NOT in task.FilesPlanned (json "files", the
// same list LintTaskScope checks). Paths are compared after
// normalizeDriftPath on every side. Pure and deterministic — no LLM,
// no git, no guardrail session. Returns nil when nothing drifted.
func DetectForeignFileDrift(task *Task, preTaskUntracked, preTaskDirty, actualCommittedFiles []string) []string {
	pre := make(map[string]bool, len(preTaskUntracked)+len(preTaskDirty))
	for _, f := range preTaskUntracked {
		if n := normalizeDriftPath(f); n != "" {
			pre[n] = true
		}
	}
	for _, f := range preTaskDirty {
		if n := normalizeDriftPath(f); n != "" {
			pre[n] = true
		}
	}
	if len(pre) == 0 {
		return nil
	}
	planned := map[string]bool{}
	if task != nil {
		for _, f := range task.FilesPlanned {
			if n := normalizeDriftPath(f); n != "" {
				planned[n] = true
			}
		}
	}
	var drift []string
	for _, f := range actualCommittedFiles {
		n := normalizeDriftPath(f)
		if n == "" || !pre[n] || planned[n] {
			continue
		}
		drift = append(drift, n)
	}
	if len(drift) == 0 {
		return nil
	}
	return sortedUnion(drift)
}

// normalizeDriftPath cleans a repo-relative path so "./x", "x" and
// "a/../x" compare equal. Empty input stays empty.
func normalizeDriftPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = path.Clean(p)
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	if p == "." {
		return ""
	}
	return p
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// sortedUnion returns the sorted, de-duplicated union of lists, or nil
// when it is empty.
func sortedUnion(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, f := range l {
			if f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}

// resolveBlockOnFileDrift reports whether detected foreign-file drift
// should block the task (strict enforcement) rather than merely be
// flagged. Priority: explicit PRD override > named GuardrailProfile >
// global Config (default false — opt-in), the same 3-tier shape as
// resolveContentAutoFixEnabled and resolveGuardrails.
func (m *Manager) resolveBlockOnFileDrift(prd *PRD) bool {
	if prd.BlockOnFileDrift != nil {
		return *prd.BlockOnFileDrift
	}
	if prd.GuardrailProfile != "" {
		if p, ok := m.store.GetGuardrailProfile(prd.GuardrailProfile); ok && p.BlockOnFileDrift != nil {
			return *p.BlockOnFileDrift
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.BlockOnFileDrift
}

// checkForeignFileDrift runs the deterministic foreign-file-drift check
// for one worker attempt of t, right after its verifier returns. It
// compares the files committed since t.PreTaskSHA against the files
// that were already dirty/untracked before the worker started and are
// not in t.FilesPlanned. On a finding it records the files on
// t.ForeignFileDrift (accumulated across retries), appends a
// file_drift_detected Decision to the PRD, emits an audit event and
// logs a warning. It returns true (task set to TaskBlocked) only when
// resolveBlockOnFileDrift says strict enforcement is on; otherwise the
// finding is warn-only and the task continues. It never reverts,
// resets, amends, un-stages or otherwise modifies any file.
func (m *Manager) checkForeignFileDrift(ctx context.Context, prd *PRD, t *Task) (blocked bool) {
	// Worktree mode runs each task in an isolated tree (see
	// ensureWorktreeIfConfigured) — nothing pre-dirty to protect.
	if prd.Git.Branch != "" || prd.ProjectDir == "" || t.PreTaskSHA == "" {
		return false
	}
	committed, err := CommittedFilesSince(ctx, prd.ProjectDir, t.PreTaskSHA, m.driftGit)
	if err != nil {
		log.Printf("[autonomous] %s/%s file-drift: git log since %s: %v", prd.ID, t.ID, t.PreTaskSHA, err)
		return false
	}
	findings := DetectForeignFileDrift(t, t.PreTaskUntrackedFiles, t.PreTaskDirtyFiles, committed)
	if len(findings) == 0 {
		return false
	}
	t.ForeignFileDrift = sortedUnion(t.ForeignFileDrift, findings)
	joined := strings.Join(findings, ", ")
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "file_drift_detected", Actor: "autonomous",
		Note: "task " + t.ID + ": committed pre-existing uncommitted file(s) outside its planned files: " + joined,
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s file-drift: save PRD: %v", prd.ID, t.ID, err)
	}
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn != nil {
		auditFn("autonomous_file_drift_detected", map[string]any{
			"task_id": t.ID,
			"prd_id":  prd.ID,
			"files":   append([]string(nil), findings...),
		})
	}
	log.Printf("[autonomous] WARNING %s/%s foreign file drift: task committed pre-existing uncommitted file(s) outside its planned files: %s", prd.ID, t.ID, joined)
	if m.resolveBlockOnFileDrift(prd) {
		t.Status = TaskBlocked
		t.Error = "foreign_file_drift: " + joined
		_ = m.store.SaveTask(t)
		return true
	}
	_ = m.store.SaveTask(t)
	return false
}
