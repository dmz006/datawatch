package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/gitguard"
	"github.com/dmz006/datawatch/internal/tooling"
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
// daemon's own files are dropped: scratch artifacts of the Automata runner, the
// tooling files the daemon writes into a project directory when a session
// starts (see isDaemonToolingFile and isDaemonIgnoreChange) and anything
// gitguard.Default refuses to stage. An empty dir or a git error returns an
// error so the caller can skip the check.
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
		if !keep[f.Path] || isBookkeepingPath(f.Path) || isDaemonToolingFile(f) || seen[f.Path] {
			continue
		}
		if isIgnoreFileName(f.Path) && isDaemonIgnoreChange(ctx, dir, f.Path, run) {
			continue
		}
		// A modified tracked file whose only difference from HEAD is the
		// daemon's own marked blocks is not the task's work.
		if f.Status != "??" && daemonSectionsOnlyChange(ctx, dir, f.Path, run) {
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
// itself writes into a worktree: the worker checkpoint note, the decompose
// output and the advisory tree lock, which the Automaton still holds when it
// finishes. They are git-ignored in normal runs; this covers the window before
// that ignore entry exists, and repositories that never get one.
func isBookkeepingPath(p string) bool {
	base := path.Base(p)
	if p == "CHECKPOINT.md" || p == treeLockRelPath {
		return true
	}
	ok, _ := path.Match(".decompose-output-*.json", base)
	return ok && !strings.Contains(p, "/")
}

// automatonScratchIgnorePatterns are the lines the Automata runner adds to a
// project's ignore file the first time an Automaton runs there, so its scratch
// files stay out of the commits.
var automatonScratchIgnorePatterns = []string{".decompose-output-*.json", "CHECKPOINT.md"}

const (
	// daemonMCPConfigFile is the per-session discovery file the daemon writes
	// at the root of a project directory when a session starts.
	daemonMCPConfigFile = ".mcp.json"
	// daemonSkillsDir is where the daemon injects skills for a session.
	daemonSkillsDir = ".datawatch/skills/"
	// daemonIgnoreComment starts the comment line the daemon writes above the
	// patterns it appends to an ignore file.
	daemonIgnoreComment = "# datawatch-managed"
	// daemonIgnoreDir is the ignore line for the daemon's own folder.
	daemonIgnoreDir = ".datawatch/"
)

// isDaemonToolingFile reports whether f is a new file the daemon itself wrote
// into the project directory for a session: the per-session MCP discovery file
// and the skills it injects. They are never the task's work. Only an untracked
// file counts, so a project that tracks .mcp.json still sees a change to it.
func isDaemonToolingFile(f UncommittedFile) bool {
	if f.Status != "??" {
		return false
	}
	return f.Path == daemonMCPConfigFile || strings.HasPrefix(f.Path, daemonSkillsDir)
}

// isIgnoreFileName reports whether p is one of the ignore files the daemon
// appends patterns to.
func isIgnoreFileName(p string) bool {
	return p == ".gitignore" || p == ".cfignore" || p == ".dockerignore"
}

// daemonIgnoreLines is every non-comment line the daemon may append to an
// ignore file: the Automata scratch patterns, the artifact patterns of each
// backend and its own folder.
func daemonIgnoreLines() map[string]bool {
	set := map[string]bool{daemonIgnoreDir: true}
	for _, p := range automatonScratchIgnorePatterns {
		set[p] = true
	}
	for _, patterns := range tooling.BackendArtifacts {
		for _, p := range patterns {
			set[p] = true
		}
	}
	return set
}

// ignoreFileLines returns the trimmed non-blank lines of text as a set.
func ignoreFileLines(text string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	return set
}

// isDaemonIgnoreChange reports whether the working copy of the ignore file rel
// in dir differs from HEAD only by lines the daemon itself appends (see
// daemonIgnoreLines): no line was removed and every added line is one of those
// patterns or the daemon's comment line. An ignore file that is not in HEAD is
// compared with an empty one. Any other change, including one the worker made,
// is not the daemon's.
func isDaemonIgnoreChange(ctx context.Context, dir, rel string, run driftGitRunner) bool {
	cleaned, ok := localRelPath(rel)
	if !ok {
		return false
	}
	if strings.ContainsRune(cleaned, 0) {
		return false
	}
	checked := filepath.FromSlash(cleaned)
	if !filepath.IsLocal(checked) {
		return false
	}
	cur, err := os.ReadFile(filepath.Join(dir, checked)) // #nosec G304 -- checked is a lexically local path and dir is the Automaton's own project directory
	if err != nil {
		return false
	}
	head, herr := run(ctx, dir, "show", "HEAD:"+rel)
	if herr != nil {
		head = ""
	}
	before, now := ignoreFileLines(head), ignoreFileLines(string(cur))
	for l := range before {
		if !now[l] {
			return false
		}
	}
	allowed := daemonIgnoreLines()
	added := 0
	for l := range now {
		if before[l] {
			continue
		}
		added++
		if !allowed[l] && !strings.HasPrefix(l, daemonIgnoreComment) {
			return false
		}
	}
	return added > 0
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
// returns the current HEAD with a nil error. A listed path that was deleted
// is recorded as a deletion, whether or not the deletion was already staged,
// and a path that exists nowhere (worktree, index, HEAD) is skipped.
func DefaultTaskCommit(ctx context.Context, dir string, files []string, message string) (string, error) {
	if dir == "" {
		return "", errors.New("task commit: empty directory")
	}
	add, pathspec, _ := classifyTaskCommitPaths(ctx, dir, files)
	for start := 0; start < len(add); start += snapshotAddChunk {
		end := min(start+snapshotAddChunk, len(add))
		args := append([]string{"add", "--"}, add[start:end]...)
		if out, err := runGitInCtx(ctx, dir, args...); err != nil {
			return "", fmt.Errorf("task commit: git add in %s: %w: %s", dir, err, strings.TrimSpace(out))
		}
	}
	if len(pathspec) == 0 {
		return taskHeadSHA(ctx, dir)
	}
	// Limit the staged check to the listed files, so changes someone
	// else staged earlier cannot turn into this commit.
	checkArgs := append([]string{"diff", "--cached", "--quiet", "--"}, pathspec...)
	if _, err := runGitInCtx(ctx, dir, checkArgs...); err == nil {
		return taskHeadSHA(ctx, dir)
	}
	// An explicit pathspec on commit records only the listed files, whatever
	// else sits in the index.
	args := append([]string{"commit", "-m", message, "--"}, pathspec...)
	if out, err := runGitInCtx(ctx, dir, args...); err != nil {
		return "", fmt.Errorf("task commit: git commit in %s: %w: %s", dir, err, strings.TrimSpace(out))
	}
	return taskHeadSHA(ctx, dir)
}

// classifyTaskCommitPaths sorts the listed paths into what git add receives
// (add), what the commit pathspec receives (pathspec) and what is dropped
// (skipped). Per path:
//  1. A path that lexically escapes the repository (absolute, or starting
//     with ".." once cleaned) or contains a NUL byte is never skipped: it goes to add and pathspec so git fails
//     with its own error.
//  2. A path that exists in the worktree (Lstat succeeds) goes to add and
//     pathspec.
//  3. A path missing from the worktree but present in the index is a tracked
//     file deleted on disk: add (git add stages the deletion) and pathspec.
//  4. A path missing from the worktree and index but present in HEAD is a
//     deletion that is already staged: pathspec only, because git add would
//     fail with "pathspec did not match".
//  5. A path found nowhere is skipped and appears in neither list.
//
// A git error while probing (anything other than the probe's normal "not
// found" result) never skips a path; it goes to add and pathspec so the real
// git error surfaces.
func classifyTaskCommitPaths(ctx context.Context, dir string, files []string) (add, pathspec, skipped []string) {
	for _, p := range files {
		if strings.ContainsRune(p, 0) {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		clean := filepath.Clean(p)
		if filepath.IsAbs(clean) {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		rel := clean
		if !filepath.IsLocal(rel) {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, rel)); err == nil {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		out, err := runGitInCtx(ctx, dir, "ls-files", "--cached", "--", p)
		if err != nil || strings.TrimSpace(out) != "" {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		out, err = runGitInCtx(ctx, dir, "ls-tree", "-r", "--name-only", "HEAD", "--", p)
		if err != nil {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		if strings.TrimSpace(out) != "" {
			pathspec = append(pathspec, p)
			continue
		}
		skipped = append(skipped, p)
	}
	return add, pathspec, skipped
}

// taskHeadSHA returns the current HEAD commit of dir.
func taskHeadSHA(ctx context.Context, dir string) (string, error) {
	out, err := runGitInCtx(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("task commit: git rev-parse HEAD in %s: %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}
