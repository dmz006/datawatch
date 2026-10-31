package autonomous

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/gitguard"
)

// UncommittedFile is one path git reports as changed in an Automaton worktree
// after a task finished. Status is the two-letter porcelain code ("M ", " M",
// "??", "D ", "R "). OutsideScope marks a file the task neither declared nor
// was allowed to write: the executor reports it but never commits it.
type UncommittedFile struct {
	Path         string `json:"path"`
	Status       string `json:"status"`
	OutsideScope bool   `json:"outside_scope,omitempty"`
}

// DetectUncommittedWork lists the files git sees as modified, staged, deleted,
// renamed or untracked in dir, using git status through the read-only run
// (nil means defaultDriftGitRunner). Ignored files never appear because git
// status omits them. Paths are normalized, de-duplicated and sorted, and the
// daemon's own bookkeeping files are dropped: scratch artifacts of the
// Automata runner and anything gitguard.Default refuses to stage. An empty dir
// or a git error returns an error so the caller can skip the check.
func DetectUncommittedWork(ctx context.Context, dir string, run driftGitRunner) ([]UncommittedFile, error) {
	if dir == "" {
		return nil, errors.New("uncommitted work check: empty directory")
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	out, err := run(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("uncommitted work check in %s: %w", dir, err)
	}
	files := parsePorcelainZ(out)
	if len(files) == 0 {
		return nil, nil
	}

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	kept, _ := gitguard.Default.Keep(dir, paths)
	keep := make(map[string]bool, len(kept))
	for _, p := range kept {
		keep[p] = true
	}

	seen := make(map[string]bool, len(files))
	result := make([]UncommittedFile, 0, len(files))
	for _, f := range files {
		if !keep[f.Path] || isBookkeepingPath(f.Path) || seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

// parsePorcelainZ parses git status --porcelain=v1 -z output. Each record is
// "XY path" ended by NUL; a rename or copy record (X is R or C) is followed by
// a second NUL-terminated field holding the original path. A rename reports
// the original path as deleted, so committing the new path also commits the
// removal. Paths are normalized with normalizeDriftPath.
func parsePorcelainZ(out string) []UncommittedFile {
	fields := strings.Split(out, "\x00")
	var files []UncommittedFile
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if len(rec) < 4 || rec[2] != ' ' {
			continue
		}
		status := rec[:2]
		if p := normalizeDriftPath(rec[3:]); p != "" {
			files = append(files, UncommittedFile{Path: p, Status: status})
		}
		if status[0] == 'R' || status[0] == 'C' {
			i++
			// A rename also removes the old path; a copy leaves it alone.
			if status[0] == 'R' && i < len(fields) {
				if old := normalizeDriftPath(fields[i]); old != "" {
					files = append(files, UncommittedFile{Path: old, Status: "D "})
				}
			}
		}
	}
	return files
}

// isBookkeepingPath reports whether p is a scratch file the Automata runner
// itself writes into a worktree: the worker checkpoint note and the decompose
// output. They are git-ignored in normal runs; this covers the window before
// that ignore entry exists.
func isBookkeepingPath(p string) bool {
	base := path.Base(p)
	if p == "CHECKPOINT.md" {
		return true
	}
	ok, _ := path.Match(".decompose-output-*.json", base)
	return ok && !strings.Contains(p, "/")
}

// ScopeUncommittedFiles narrows files to what a task owns and flags the files
// outside its declared scope. For scope config.UncommittedWorkScopeTaskFiles it
// removes every path that was already dirty or untracked before the task
// started (t.PreTaskDirtyFiles, t.PreTaskUntrackedFiles); for
// config.UncommittedWorkScopeWorktree it keeps all files. In both cases a file
// that is in neither t.FilesPlanned nor t.FilesGenerated and not under any of
// taskWriteDirs gets OutsideScope set. taskWriteDirs are absolute directories
// (see EffectiveTaskWriteDirs) and taskDir is the absolute directory the
// reported paths are relative to. The input slice is not modified.
func ScopeUncommittedFiles(files []UncommittedFile, t *Task, taskWriteDirs []string, taskDir, scope string) []UncommittedFile {
	pre := map[string]bool{}
	declared := map[string]bool{}
	if t != nil {
		if scope == config.UncommittedWorkScopeTaskFiles {
			for _, p := range t.PreTaskDirtyFiles {
				pre[normalizeDriftPath(p)] = true
			}
			for _, p := range t.PreTaskUntrackedFiles {
				pre[normalizeDriftPath(p)] = true
			}
		}
		for _, p := range t.FilesPlanned {
			declared[normalizeDriftPath(p)] = true
		}
		for _, p := range t.FilesGenerated {
			declared[normalizeDriftPath(p)] = true
		}
	}

	var out []UncommittedFile
	for _, f := range files {
		if pre[f.Path] {
			continue
		}
		f.OutsideScope = !declared[f.Path] && !underWriteDirs(f.Path, taskWriteDirs, taskDir)
		out = append(out, f)
	}
	return out
}

// underWriteDirs reports whether the repo-relative path rel, resolved against
// taskDir, lies at or under one of the absolute directories in writeDirs.
func underWriteDirs(rel string, writeDirs []string, taskDir string) bool {
	if len(writeDirs) == 0 || taskDir == "" {
		return false
	}
	return PathInDirs(filepath.Join(taskDir, filepath.FromSlash(rel)), writeDirs)
}

// InScopeUncommitted returns the paths of files not flagged OutsideScope. These
// are the only files the executor commits automatically.
func InScopeUncommitted(files []UncommittedFile) []string {
	var paths []string
	for _, f := range files {
		if !f.OutsideScope {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

// TaskCommitFn stages exactly files in dir, commits them with message and
// returns the new HEAD sha. The executor takes it as a field so tests can
// substitute a fake.
type TaskCommitFn func(ctx context.Context, dir string, files []string, message string) (sha string, err error)

// DefaultTaskCommit is the TaskCommitFn that shells out to git. It stages the
// listed files with explicit pathspecs (never -A and never "."), then commits
// without --no-verify, so a failing commit hook is a failed commit and its
// error is returned. When nothing ends up staged it makes no commit and
// returns the current HEAD with a nil error.
func DefaultTaskCommit(ctx context.Context, dir string, files []string, message string) (string, error) {
	if dir == "" {
		return "", errors.New("task commit: empty directory")
	}
	for start := 0; start < len(files); start += snapshotAddChunk {
		end := min(start+snapshotAddChunk, len(files))
		args := append([]string{"add", "--"}, files[start:end]...)
		if out, err := runGitInCtx(ctx, dir, args...); err != nil {
			return "", fmt.Errorf("task commit: git add in %s: %w: %s", dir, err, strings.TrimSpace(out))
		}
	}
	if len(files) > 0 {
		// Limit the staged check to the listed files, so changes someone
		// else staged earlier cannot turn into this commit.
		args := append([]string{"diff", "--cached", "--quiet", "--"}, files...)
		if _, err := runGitInCtx(ctx, dir, args...); err == nil {
			return taskHeadSHA(ctx, dir)
		}
	} else {
		return taskHeadSHA(ctx, dir)
	}
	// An explicit pathspec on commit records only the listed files, whatever
	// else sits in the index.
	args := append([]string{"commit", "-m", message, "--"}, files...)
	if out, err := runGitInCtx(ctx, dir, args...); err != nil {
		return "", fmt.Errorf("task commit: git commit in %s: %w: %s", dir, err, strings.TrimSpace(out))
	}
	return taskHeadSHA(ctx, dir)
}

// taskHeadSHA returns the current HEAD commit of dir.
func taskHeadSHA(ctx context.Context, dir string) (string, error) {
	out, err := runGitInCtx(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("task commit: git rev-parse HEAD in %s: %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}
