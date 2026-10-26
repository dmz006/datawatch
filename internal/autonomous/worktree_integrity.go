// Worktree integrity check for an Automaton's project directory: a
// directory is only trusted as a per-PRD worktree when it is a linked
// worktree registered in the base repository, not a standalone
// repository that happened to be initialised at the same path after
// the real worktree was removed. See
// docs/plans/2026-10-25-project-dir-worktree-integrity.md.
package autonomous

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ErrWorktreeIntegrity is the sentinel every *WorktreeIntegrityError
// matches through errors.Is.
var ErrWorktreeIntegrity = errors.New("project directory is not a valid worktree")

// WorktreeIntegrityError reports which check a project directory
// failed. Dir is the directory that was checked; Reason names the
// failed condition in operator-readable words.
type WorktreeIntegrityError struct {
	Dir    string
	Reason string
}

func (e *WorktreeIntegrityError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrWorktreeIntegrity.Error(), e.Dir, e.Reason)
}

// Unwrap lets errors.Is(err, ErrWorktreeIntegrity) match.
func (e *WorktreeIntegrityError) Unwrap() error { return ErrWorktreeIntegrity }

// Is reports true for the sentinel, so the match also holds for
// callers that compare without unwrapping.
func (e *WorktreeIntegrityError) Is(target error) bool { return target == ErrWorktreeIntegrity }

// rootMarkerFiles are the repository root files whose presence in the
// base repository's HEAD must carry over to a valid worktree.
var rootMarkerFiles = []string{"go.mod", "AGENT.md", "Makefile"}

// CheckProjectWorktree returns nil only when projectDir is a live
// linked worktree of baseRepo with the repository's root files
// present; otherwise it returns a *WorktreeIntegrityError naming the
// check that failed. With an empty baseRepo only the directory,
// gitlink and work-tree checks apply.
func CheckProjectWorktree(projectDir, baseRepo string) error {
	fail := func(format string, a ...any) error {
		return &WorktreeIntegrityError{Dir: projectDir, Reason: fmt.Sprintf(format, a...)}
	}

	// (a) an existing directory.
	if strings.TrimSpace(projectDir) == "" {
		return fail("project directory is empty")
	}
	st, err := os.Stat(projectDir)
	if err != nil {
		return fail("directory does not exist: %v", err)
	}
	if !st.IsDir() {
		return fail("path is not a directory")
	}

	// (b) .git is a regular file (the gitlink of a linked worktree).
	gst, err := os.Lstat(filepath.Join(projectDir, ".git"))
	if err != nil {
		return fail("no .git entry: %v", err)
	}
	if gst.IsDir() {
		return fail(".git is a directory, so this is a standalone repository and not a linked worktree")
	}
	if !gst.Mode().IsRegular() {
		return fail(".git is not a regular gitlink file")
	}

	baseRepo = strings.TrimSpace(baseRepo)
	if baseRepo == "" {
		out, gerr := runGitIn(projectDir, "rev-parse", "--is-inside-work-tree")
		if gerr != nil || strings.TrimSpace(out) != "true" {
			return fail("git does not see a work tree here: %s", strings.TrimSpace(out))
		}
		return nil
	}

	// (c) same common git directory as the base repository.
	projCommon, err := integrityCommonDir(projectDir)
	if err != nil {
		return fail("cannot resolve the git common directory: %v", err)
	}
	baseCommon, err := integrityCommonDir(baseRepo)
	if err != nil {
		return fail("cannot resolve the base repository's git common directory: %v", err)
	}
	if projCommon != baseCommon {
		return fail("belongs to a different repository (common dir %s, base repository uses %s)", projCommon, baseCommon)
	}

	// (d) registered in the base repository's worktree list.
	want := evalOrClean(projectDir)
	listing, err := runGitIn(baseRepo, "worktree", "list", "--porcelain")
	if err != nil {
		return fail("cannot list the base repository's worktrees: %s", strings.TrimSpace(listing))
	}
	registered := false
	for _, line := range strings.Split(listing, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "worktree "); ok && evalOrClean(p) == want {
			registered = true
			break
		}
	}
	if !registered {
		return fail("not registered in the base repository's worktree list")
	}

	// (e) repository root marker files are present.
	tree, err := runGitIn(baseRepo, "ls-tree", "--name-only", "HEAD")
	if err != nil {
		return fail("cannot list the base repository's root files: %s", strings.TrimSpace(tree))
	}
	tracked := map[string]bool{}
	for _, name := range strings.Split(tree, "\n") {
		tracked[strings.TrimSpace(name)] = true
	}
	var markers []string
	for _, m := range rootMarkerFiles {
		if tracked[m] {
			markers = append(markers, m)
		}
	}
	if len(markers) > 0 {
		for _, m := range markers {
			if _, serr := os.Stat(filepath.Join(projectDir, m)); serr != nil {
				return fail("repository root file %s is missing", m)
			}
		}
		return nil
	}
	files, err := runGitIn(projectDir, "ls-files")
	if err != nil {
		return fail("cannot list tracked files: %s", strings.TrimSpace(files))
	}
	first, _, _ := strings.Cut(files, "\n")
	if strings.TrimSpace(first) == "" {
		return fail("no tracked files at the repository root")
	}
	return nil
}

// integrityCommonDir returns the symlink-resolved, absolute common git
// directory of the repository dir belongs to.
func integrityCommonDir(dir string) (string, error) {
	out, err := runGitIn(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(out))
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return "", fmt.Errorf("empty output")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return evalOrClean(p), nil
}

// evalOrClean resolves symlinks, falling back to the cleaned path when
// the target cannot be resolved (for example because it is gone).
func evalOrClean(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// refuseLaunchOutsideWorktree is the executor's pre-spawn guard: a
// worker is never started in a project directory that is not a real
// worktree. It applies only to worktree-mode Automatons whose task runs
// in the Automaton's own project directory (a per-task override is not
// a worktree) and only when a base repository is configured. On a
// failed check it fails the task, raises one operator alert and
// returns an error wrapping ErrWorktreeIntegrity; the executor returns
// it without retrying, so the failure neither consumes the retry budget
// nor counts toward the failure-escalation hold. It returns nil when
// the launch may proceed.
func (m *Manager) refuseLaunchOutsideWorktree(prd *PRD, t *Task, taskDir string) error {
	if prd.Git.Branch == "" || prd.ClusterProfile != "" || prd.ProjectDir == "" || taskDir != prd.ProjectDir {
		return nil
	}
	m.mu.Lock()
	baseRepo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if baseRepo == "" {
		return nil
	}
	cerr := CheckProjectWorktree(taskDir, baseRepo)
	if cerr == nil {
		return nil
	}
	reason := cerr.Error()
	var ierr *WorktreeIntegrityError
	if errors.As(cerr, &ierr) {
		reason = ierr.Reason
	}
	refused := fmt.Errorf("worker launch refused: %w", cerr)
	log.Printf("[autonomous] prd=%s task=%s worker launch refused, not spawning: %v", prd.ID, t.ID, cerr)
	oldStatus := t.Status
	m.store.WithLock(func() {
		t.Status = TaskFailed
		t.Error = refused.Error()
	})
	if m.store.SaveTask(t) == nil {
		m.emitTaskStatus(prd, t, oldStatus, nil, nil)
	}
	m.raiseWorktreeIntegrityAlert(prd, t, taskDir, reason)
	return refused
}

// raiseWorktreeIntegrityAlert sends the one operator alert for a
// refused launch. Nothing is sent when no alert sink is wired.
func (m *Manager) raiseWorktreeIntegrityAlert(prd *PRD, t *Task, dir, reason string) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	body := fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\nProject directory: %s\nFailed check: %s\n"+
			"Ways to act: repair the directory, for example by re-adding it with "+
			"git worktree add, and run the Automaton again.",
		prd.ID, prd.Title, t.ID, t.Title, dir, reason)
	alert("error", "Automaton project directory is not a worktree", body)
}
