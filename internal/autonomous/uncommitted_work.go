package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
// (nil means defaultDriftGitRunner). Files the repository's own ignore rules
// ignore and that are not in the index never appear, along with the daemon's
// own files: scratch artifacts of the Automata runner, the tooling files the
// daemon writes into a project directory when a session starts (see
// isDaemonToolingFile and isDaemonIgnoreChange), anything gitguard.Default
// refuses to stage, and files whose content is only the daemon's injected
// marked sections (see daemonSectionsOnlyChange and daemonSectionsOnlyUntracked).
// Paths are normalized, de-duplicated and sorted, and an empty dir or a git
// error returns an error so the caller can skip the check.
func DetectUncommittedWork(ctx context.Context, dir string, run driftGitRunner) ([]UncommittedFile, error) {
	files, _, err := DetectUncommittedWorkSkipping(ctx, dir, run)
	return files, err
}

// DetectUncommittedWorkSkipping does what DetectUncommittedWork does and also
// returns the sorted, de-duplicated paths of the daemon's marker files (see
// isDaemonMarkerPath) it left out, whatever their git status, so a caller can
// tell the operator which files were skipped.
func DetectUncommittedWorkSkipping(ctx context.Context, dir string, run driftGitRunner) ([]UncommittedFile, []string, error) {
	if dir == "" {
		return nil, nil, errors.New("uncommitted work check: empty directory")
	}
	if run == nil {
		run = defaultDriftGitRunner
	}
	out, err := run(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, nil, fmt.Errorf("uncommitted work check in %s: %w", dir, err)
	}
	files := parsePorcelainZ(out)
	if len(files) == 0 {
		return nil, nil, nil
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

	// A file the repository's own ignore rules ignore and that is not in the
	// index cannot be part of a commit anyway, so it is never the task's work.
	// An index check that fails keeps the entry, following the conservative
	// pattern of classifyTaskCommitPaths.
	ignoreSet := ignoredUntrackedPaths(ctx, dir, paths)
	dropIgnored := map[string]bool{}
	for p := range ignoreSet {
		cached, cerr := run(ctx, dir, "ls-files", "--cached", "--", p)
		if cerr == nil && strings.TrimSpace(cached) == "" {
			dropIgnored[normalizeDriftPath(p)] = true
		}
	}

	seen := make(map[string]bool, len(files))
	result := make([]UncommittedFile, 0, len(files))
	var skipped []string
	skippedSeen := map[string]bool{}
	for _, f := range files {
		if isDaemonMarkerPath(f.Path) {
			if !skippedSeen[f.Path] {
				skippedSeen[f.Path] = true
				skipped = append(skipped, f.Path)
			}
			continue
		}
		if !keep[f.Path] || isBookkeepingPath(f.Path) || isDaemonToolingFile(f) || seen[f.Path] || dropIgnored[f.Path] {
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
		// A newly untracked file that holds nothing but the daemon's own
		// marked blocks is not the task's work either.
		if f.Status == "??" && daemonSectionsOnlyUntracked(ctx, dir, f.Path) {
			continue
		}
		seen[f.Path] = true
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	sort.Strings(skipped)
	return result, skipped, nil
}

// skippedMarkerFilesNote names the daemon marker files the check skipped, in
// one sentence for the question text. Empty when none were skipped.
func skippedMarkerFilesNote(skipped []string) string {
	if len(skipped) == 0 {
		return ""
	}
	names := append([]string(nil), skipped...)
	sort.Strings(names)
	if len(names) == 1 {
		return fmt.Sprintf("Skipped the Automaton marker file %s (the daemon's own file, not work to commit).", names[0])
	}
	return fmt.Sprintf("Skipped the Automaton marker files %s (the daemon's own files, not work to commit).", strings.Join(names, ", "))
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
// and the skills it injects, plus the two root launcher files the session
// launcher writes when it starts (see launcherToolingFiles). They are never
// the task's work. Only an untracked file counts, so a project that tracks one
// of these files still sees a change to it.
func isDaemonToolingFile(f UncommittedFile) bool {
	if f.Status != "??" {
		return false
	}
	return f.Path == daemonMCPConfigFile ||
		strings.HasPrefix(f.Path, daemonSkillsDir) ||
		isLauncherToolingFile(f.Path)
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
	// Containment, checked inline on rel before any file call: reject empty,
	// NUL-byte, absolute, '.' and any '..' escape; the cleaned local form is
	// what is joined and read. The resolved-base check (a symlink pointing
	// outside dir) follows in repoFileInside.
	if rel == "" || strings.ContainsRune(rel, 0) || filepath.IsAbs(rel) {
		return false
	}
	relClean := filepath.Clean(filepath.FromSlash(rel))
	if relClean == "." || !filepath.IsLocal(relClean) {
		return false
	}
	curPath, err := repoFileInside(dir, filepath.ToSlash(relClean))
	if err != nil {
		return false
	}
	cur, err := os.ReadFile(curPath)
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
// and a path that exists nowhere (worktree, index, HEAD) is skipped; so is
// a path git ignores that is not in the index, because git add would refuse
// it.
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
//  2. A path that exists in the worktree — under dir and not reachable only
//     through a symlink that escapes dir (the stat is containment-checked) —
//     goes to add and pathspec, unless it is git-ignored (git check-ignore)
//     and not in the index (`git ls-files --cached` returns nothing for it):
//     then it is skipped and appears in neither list, because git add would
//     refuse it. A path in the index is never skipped, whatever its ignore
//     status.
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
	// Which listed paths the repository's ignore rules ignore, asked of git
	// check-ignore in chunks of 500. When git cannot answer (error, context
	// cancel) the set is empty, so no path is dropped below and the real
	// git error surfaces at add.
	ignoreSet := ignoredUntrackedPaths(ctx, dir, files)
	for _, p := range files {
		if p == "" || strings.ContainsRune(p, 0) {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
		clean := filepath.Clean(p)
		if clean == "." {
			add = append(add, p)
			pathspec = append(pathspec, p)
			continue
		}
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
		if inside, ierr := repoFileInside(dir, rel); ierr == nil {
			if _, lerr := os.Lstat(inside); lerr == nil {
				// Git-ignored and untracked (not in the index) cannot be staged:
				// git add refuses it. A path in the index is tracked and is kept
				// even if it matches an ignore rule, and so is a path whose
				// index check failed.
				if ignoreSet[p] {
					if cached, cerr := runGitInCtx(ctx, dir, "ls-files", "--cached", "--", p); cerr == nil && strings.TrimSpace(cached) == "" {
						skipped = append(skipped, p)
						continue
					}
				}
				add = append(add, p)
				pathspec = append(pathspec, p)
				continue
			}
		}
		// A path that is not stat-able under dir (missing, or reachable only
		// through a symlink leaving dir) falls through to the index/HEAD
		// probes below instead of being committed by its relative name.
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

// ignoredUntrackedPaths reports which of the given repo-relative paths the
// repository's ignore rules ignore (git check-ignore), asked in chunks of 500
// paths. git exits 0 when at least one path is ignored and 1 when none is;
// both are normal outcomes here. Any other failure (git error, context
// cancel) yields an empty set — a path whose ignore status cannot be
// confirmed is kept, following the conservative pattern of ignoredSet in
// internal/session/git.go.
func ignoredUntrackedPaths(ctx context.Context, dir string, paths []string) map[string]bool {
	ignored := map[string]bool{}
	for start := 0; start < len(paths); start += 500 {
		end := min(start+500, len(paths))
		args := append([]string{"check-ignore", "--"}, paths[start:end]...)
		out, err := runGitInCtx(ctx, dir, args...)
		if err != nil {
			// exit 0 is success and exit 1 means "none ignored"; anything
			// else means git could not answer this chunk.
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				continue
			}
		}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				ignored[l] = true
			}
		}
	}
	return ignored
}

// taskHeadSHA returns the current HEAD commit of dir.
func taskHeadSHA(ctx context.Context, dir string) (string, error) {
	out, err := runGitInCtx(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("task commit: git rev-parse HEAD in %s: %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}
