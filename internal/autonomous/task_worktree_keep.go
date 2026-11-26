// What may be removed of an Automaton's task worktrees and task branches, and
// the record of what was kept.
//
// Two rules protect work, and both fail closed:
//
//   - a task worktree is removed only when it holds no uncommitted or untracked
//     file other than the daemon's own generated and tooling files, no live
//     session uses it, and a detached HEAD in it holds no commit of its own;
//   - a task branch is deleted only when every one of its commits is on the
//     default branch (directly, or through an Automaton branch that is itself
//     on the default branch), and only after its tip is kept under a backup ref.
//
// Whatever cannot be established is kept. Every removal and every refusal is
// written to the audit log with the branch tip, and the Automaton record says
// how many task worktrees and branches were kept and why.
package autonomous

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Reasons a task worktree or a task branch was kept.
const (
	taskKeepCleanupOff      = "cleanup_off"
	taskKeepReportMode      = "report_mode"
	taskKeepByArchive       = "not_removed_by_archive"
	taskKeepNoBaseRepo      = "no_base_repo"
	taskKeepLiveSession     = "live_session"
	taskKeepUncommitted     = "uncommitted_work"
	taskKeepDetached        = "detached_commits"
	taskKeepUnverified      = "unverified"
	taskKeepRemoveFailed    = "remove_failed"
	taskKeepUnmerged        = "unmerged_branch"
	taskKeepNoDefaultBranch = "no_default_branch"
	taskKeepBackupFailed    = "backup_failed"
	taskKeepDeleteFailed    = "branch_delete_failed"
	taskKeepNeeded          = "needed_by_record"
	taskKeepOverLimit       = "over_limit"
)

// Audit actions for task worktrees and task branches.
const (
	auditTaskWorktreeRemove      = "autonomous_task_worktree_remove"
	auditTaskWorktreeKept        = "autonomous_task_worktree_kept"
	auditTaskWorktreeWouldRemove = "autonomous_task_worktree_would_remove"
	auditTaskBranchDelete        = "autonomous_task_branch_delete"
	auditTaskBranchKept          = "autonomous_task_branch_kept"
)

// maxKeptFiles bounds the uncommitted files listed for one kept worktree.
const maxKeptFiles = 20

// TaskWorktreeKept is one task whose worktree, branch or both were left in
// place.
type TaskWorktreeKept struct {
	TaskID string `json:"task_id"`
	// Worktree is the kept worktree's path; empty when the worktree was
	// removed (or already gone) and only the branch is kept.
	Worktree  string `json:"worktree,omitempty"`
	Branch    string `json:"branch,omitempty"`
	BranchTip string `json:"branch_tip,omitempty"`
	// Reason is one of: cleanup_off, report_mode, not_removed_by_archive,
	// no_base_repo, live_session, uncommitted_work, detached_commits,
	// unverified, remove_failed, unmerged_branch, no_default_branch,
	// backup_failed, branch_delete_failed, needed_by_record, over_limit.
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
	// Files lists (at most maxKeptFiles of) the uncommitted files, each as
	// its git status code and path.
	Files []string `json:"files,omitempty"`
}

// TaskWorktreeCleanup is what closing an Automaton did with its task worktrees
// and task branches: how many were removed, how many were kept and why.
type TaskWorktreeCleanup struct {
	At time.Time `json:"at"`
	// Mode is the cleanup mode the last pass ran in (off, report, on).
	Mode string `json:"mode"`
	// WorktreesRemoved and BranchesDeleted add up over every pass.
	WorktreesRemoved int `json:"worktrees_removed"`
	BranchesDeleted  int `json:"branches_deleted"`
	// WorktreesKept and BranchesKept count what is still there.
	WorktreesKept int `json:"worktrees_kept"`
	BranchesKept  int `json:"branches_kept"`
	// Reasons counts the Kept entries by reason.
	Reasons map[string]int `json:"reasons,omitempty"`
	// Message is the same in one sentence.
	Message string             `json:"message"`
	Kept    []TaskWorktreeKept `json:"kept,omitempty"`
}

// taskCleanupRun says how one pass over an Automaton's task worktrees behaves.
type taskCleanupRun struct {
	mode string // config.CleanupModeOff / Report / On
	// assessOnly looks and records but removes nothing (the archive step with
	// autonomous.automation.cleanup_archived off).
	assessOnly bool
	// budget, when set, is the number of removals the periodic pass may still
	// make; each task something is removed for spends one.
	budget *int
}

// taskCleanupLocks holds one mutex per Automaton, so two end paths running at
// once never act on the same task worktrees together.
var taskCleanupLocks sync.Map

func taskCleanupLock(prdID string) *sync.Mutex {
	l, _ := taskCleanupLocks.LoadOrStore(prdID, &sync.Mutex{})
	return l.(*sync.Mutex)
}

// taskGitOut runs git in dir and returns standard output only: a warning on
// standard error must never be read as a status record or a commit id. Only the
// commands checkTaskGitArgs lists are run.
func taskGitOut(ctx context.Context, dir string, args ...string) (string, error) {
	if err := checkTaskGitArgs(args); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- the command is one checkTaskGitArgs lists; no shell
	cmd.Dir = dir
	cmd.WaitDelay = mergeGitWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

type taskGitRunner struct{}

func (taskGitRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	return taskGitOut(ctx, dir, args...)
}

// branchTip returns the commit the local branch points at. exists is false only
// when git answered that there is no such branch.
func branchTip(ctx context.Context, repo, branch string) (tip string, exists bool, err error) {
	out, err := taskGitOut(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

// commitsAllOn reports whether every commit reachable from sha is on the local
// branch target: as an ancestor, or patch by patch (git cherry). git cherry
// does not look at merge commits, so a merge commit that is not itself on
// target answers no. Any git error answers no.
func commitsAllOn(ctx context.Context, repo, target, sha string) bool {
	ref := "refs/heads/" + target
	if _, err := taskGitOut(ctx, repo, "merge-base", "--is-ancestor", sha, ref); err == nil {
		return true
	}
	merges, err := taskGitOut(ctx, repo, "rev-list", "--merges", "--count", ref+".."+sha)
	if err != nil || strings.TrimSpace(merges) != "0" {
		return false
	}
	out, err := taskGitOut(ctx, repo, "cherry", ref, sha)
	if err != nil {
		return false
	}
	seen := 0
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		if !strings.HasPrefix(line, "-") {
			return false
		}
		seen++
	}
	return seen > 0
}

// taskDefaultBranch is the default branch a task branch's commits must be on.
// Empty when git cannot name one, or names an Automaton's own branch (a base
// checkout left on such a branch says nothing about what has landed).
func taskDefaultBranch(ctx context.Context, repo string, prd *PRD) string {
	name, err := defaultBranchForRepo(ctx, taskGitRunner{}, repo)
	if err != nil || name == prd.Git.Branch || strings.HasPrefix(name, automatonBranchPrefix) {
		return ""
	}
	return name
}

// commitsLanded names the branch that shows sha's commits are on the default
// branch: the default branch itself, or the Automaton branch when that branch
// is itself on the default branch. Empty when neither does.
func commitsLanded(ctx context.Context, repo, def string, prd *PRD, sha string) string {
	if def == "" {
		return ""
	}
	if commitsAllOn(ctx, repo, def, sha) {
		return def
	}
	if prd.Git.Branch == "" {
		return ""
	}
	tip, ok, err := branchTip(ctx, repo, prd.Git.Branch)
	if err != nil || !ok || !commitsAllOn(ctx, repo, def, tip) {
		return ""
	}
	if commitsAllOn(ctx, repo, prd.Git.Branch, sha) {
		return prd.Git.Branch
	}
	return ""
}

type linkedWorktree struct {
	path, head string
	detached   bool
	primary    bool
}

// findLinkedWorktree looks path up in the repository's worktree list.
func findLinkedWorktree(ctx context.Context, repo, path string) (wt linkedWorktree, found bool, err error) {
	out, err := taskGitOut(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return wt, false, err
	}
	for i, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		cur := linkedWorktree{primary: i == 0}
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				cur.path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "HEAD "):
				cur.head = strings.TrimPrefix(line, "HEAD ")
			case line == "detached":
				cur.detached = true
			}
		}
		if cur.path != "" && sameDirectory(cur.path, path) {
			return cur, true, nil
		}
	}
	return wt, false, nil
}

// taskWorktreeStatus reads the worktree's status. own lists the task's own
// uncommitted and untracked files, and the real files in ignored folders
// (parked_work.go, marked "!!"); clean is true when git reports nothing at all.
// A record that cannot be parsed is an error, never an empty answer. The flags
// override any repository setting that would hide files.
func taskWorktreeStatus(ctx context.Context, dir string, verifyDirs []string) (own []string, clean bool, err error) {
	out, err := taskGitOut(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, false, err
	}
	fields := strings.Split(out, "\x00")
	clean = true
	listed := map[string]bool{}
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if rec == "" {
			continue
		}
		if len(rec) < 4 || rec[2] != ' ' {
			return nil, false, fmt.Errorf("unreadable git status record %q", rec)
		}
		clean = false
		status, p := rec[:2], rec[3:]
		if status[0] == 'R' || status[0] == 'C' {
			i++ // the original path follows
		}
		if !daemonOwnedWorktreeFile(ctx, dir, status, p, verifyDirs) {
			own = append(own, status+" "+p)
			listed[p] = true
		}
	}
	parked, err := parkedWork(ctx, dir, verifyDirs)
	if err != nil {
		return nil, false, fmt.Errorf("checking the ignored folders: %w", err)
	}
	for _, p := range parked {
		if !listed[p] {
			own = append(own, "!! "+p)
		}
	}
	sort.Strings(own)
	return own, clean, nil
}

// daemonOwnedWorktreeFile reports whether one status entry is the daemon's own
// generated or tooling file, so removing it loses nothing. Only an untracked
// file or an unstaged change can be: a staged change is always the task's.
func daemonOwnedWorktreeFile(ctx context.Context, dir, status, p string, verifyDirs []string) bool {
	run := driftGitRunner(taskGitOut)
	switch status {
	case "??":
		if strings.HasSuffix(p, "/") {
			// With every untracked file listed, a folder entry is a nested
			// repository: its contents were not looked at.
			return false
		}
		if isGeneratedWorktreeArtifact(p) || isDaemonMarkerPath(p) || isBookkeepingPath(p) || p == defaultGateReportPath {
			return true
		}
		if sameAsSavedVerifyScript(dir, p, verifyDirs) {
			return true
		}
		// A file that exists only to carry daemon text. An empty file carries
		// none, so it stays the task's own.
		body, ok := readInside(dir, p)
		if !ok || strings.TrimSpace(string(body)) == "" {
			return false
		}
		if isIgnoreFileName(p) && isDaemonIgnoreChange(ctx, dir, p, run) {
			return true
		}
		return daemonSectionsOnlyUntracked(ctx, dir, p)
	case " M":
		if isIgnoreFileName(p) && isDaemonIgnoreChange(ctx, dir, p, run) {
			return true
		}
		return daemonSectionsOnlyChange(ctx, dir, p, run)
	}
	return false
}

// readInside reads the regular file rel under dir, refusing any path that
// leaves dir.
func readInside(dir, rel string) ([]byte, bool) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, false
	}
	defer func() { _ = root.Close() }()
	name := filepath.FromSlash(rel)
	if st, err := root.Lstat(name); err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	body, err := root.ReadFile(name)
	return body, err == nil
}

// sameAsSavedVerifyScript reports whether p is a verification script the daemon
// copied into the task worktree and an identical copy is still held elsewhere.
func sameAsSavedVerifyScript(dir, p string, verifyDirs []string) bool {
	const prefix = ".datawatch/verify/"
	name := strings.TrimPrefix(p, prefix)
	if name == p || name == "" || strings.Contains(name, "/") {
		return false
	}
	mine, ok := readInside(dir, p)
	if !ok {
		return false
	}
	for _, src := range verifyDirs {
		if other, ok := readInside(src, name); ok && bytes.Equal(mine, other) {
			return true
		}
	}
	return false
}

// writeBranchBackup keeps tip under refs/datawatch/reap-backup/ before the
// branch is deleted. It only ever creates a ref: an earlier backup of another
// commit is never overwritten.
func writeBranchBackup(ctx context.Context, repo, branch, tip string, ts int64) (string, error) {
	var lastErr error
	for i := int64(0); i < 100; i++ {
		ref := reapBackupRef(branch, ts+i)
		// An empty old value makes git refuse when the ref already exists.
		if _, err := taskGitOut(ctx, repo, "update-ref", ref, tip, ""); err != nil {
			lastErr = err
		}
		got, err := taskGitOut(ctx, repo, "rev-parse", "--verify", "--quiet", ref)
		if err == nil && strings.TrimSpace(got) == tip {
			return ref, nil
		}
		if err != nil {
			break // the ref was not written and does not exist
		}
	}
	return "", fmt.Errorf("no backup ref could be written for %s: %v", branch, lastErr)
}

// taskCleanupPass is the state of one pass over one Automaton.
type taskCleanupPass struct {
	m          *Manager
	ctx        context.Context
	prd        *PRD
	repo       string
	run        taskCleanupRun
	verifyDirs []string
	def        string
	defKnown   bool

	removed, deleted int
	// charged is the task the last removal of the limit was spent on.
	charged string
	kept    []TaskWorktreeKept
	// events are the audit records of refusals; a refusal already on the
	// Automaton record is not written again.
	events []taskAuditEvent
}

type taskAuditEvent struct {
	action string
	entry  TaskWorktreeKept
}

func (p *taskCleanupPass) defaultBranch() string {
	if !p.defKnown {
		p.def, p.defKnown = taskDefaultBranch(p.ctx, p.repo, p.prd), true
	}
	return p.def
}

func (p *taskCleanupPass) audit(action string, taskID, worktree, branch, tip string, extra map[string]any) {
	details := map[string]any{
		"automaton_id": p.prd.ID,
		"task_id":      taskID,
		"worktree":     worktree,
		"branch":       branch,
		"branch_tip":   tip,
		"mode":         p.run.mode,
	}
	for k, v := range extra {
		details[k] = v
	}
	if err := p.m.worktreeCleanupAudit()(action, details); err != nil {
		log.Printf("[autonomous] task worktree cleanup: audit %s for %s: %v", action, p.prd.ID, err)
	}
}

// keep records one task whose worktree or branch stays.
func (p *taskCleanupPass) keep(action string, e TaskWorktreeKept) {
	if len(e.Files) > maxKeptFiles {
		e.Detail = fmt.Sprintf("%s (%d files, first %d listed)", e.Detail, len(e.Files), maxKeptFiles)
		e.Files = e.Files[:maxKeptFiles]
	}
	p.kept = append(p.kept, e)
	p.events = append(p.events, taskAuditEvent{action: action, entry: e})
}

// spend takes one removal of the periodic pass's limit for a task, once for
// its worktree and branch together. It reports false when none is left.
func (p *taskCleanupPass) spend(taskID string) bool {
	if p.run.budget == nil || p.charged == taskID {
		return true
	}
	if *p.run.budget <= 0 {
		return false
	}
	*p.run.budget--
	p.charged = taskID
	return true
}

// neededBy is the refusal detail when an Automaton that has not finished still
// points at the task's folder or branch, "" otherwise.
func (p *taskCleanupPass) neededBy(path, branch string) string {
	if by := p.m.worktreeNeededBy(p.prd.ID, path, branch); by != "" {
		return "the Automaton " + by + " has not finished and still points at the worktree or the branch"
	}
	return ""
}

// handle looks at one task's worktree and branch and removes what the rules
// allow.
func (p *taskCleanupPass) handle(t *Task) {
	path, branch := t.WorktreePath, t.WorktreeBranch
	tip := ""
	if branch != "" {
		got, exists, err := branchTip(p.ctx, p.repo, branch)
		switch {
		case err != nil:
			p.keep(auditTaskWorktreeKept, TaskWorktreeKept{TaskID: t.ID, Worktree: path, Branch: branch,
				Reason: taskKeepUnverified, Detail: "the task branch could not be read: " + err.Error()})
			return
		case !exists && path == "":
			return // already gone: nothing is kept
		}
		tip = got
	}
	if path != "" && !p.handleWorktree(t, path, branch, tip) {
		return
	}
	if branch != "" && tip != "" {
		p.handleBranch(t, branch, tip)
		return
	}
	if branch != "" && !p.run.assessOnly && p.run.mode == config.CleanupModeOn {
		p.m.setTaskWorktree(t, "", "") // the branch is already gone
	}
}

// handleWorktree removes the task worktree when nothing in it would be lost and
// reports whether it is gone (or, when nothing may be removed, whether the
// branch still has to be looked at).
func (p *taskCleanupPass) handleWorktree(t *Task, path, branch, tip string) bool {
	refuse := func(reason, detail string, files ...string) bool {
		p.keep(auditTaskWorktreeKept, TaskWorktreeKept{TaskID: t.ID, Worktree: path, Branch: branch, BranchTip: tip,
			Reason: reason, Detail: detail, Files: files})
		return false
	}
	if p.m.taskWorktreeInUse(path) {
		return refuse(taskKeepLiveSession, "a live session works in the worktree")
	}
	if why := p.neededBy(path, branch); why != "" {
		return refuse(taskKeepNeeded, why)
	}
	if sameDirectory(path, p.prd.ProjectDir) {
		return refuse(taskKeepUnverified, "the recorded path is the Automaton worktree, not a task worktree")
	}
	wt, listed, err := findLinkedWorktree(p.ctx, p.repo, path)
	if err != nil {
		return refuse(taskKeepUnverified, "the worktree list could not be read: "+err.Error())
	}
	if listed && wt.primary {
		return refuse(taskKeepUnverified, "the recorded path is the main checkout")
	}
	_, statErr := os.Stat(path)
	missing := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !missing {
		return refuse(taskKeepUnverified, "the worktree could not be read: "+statErr.Error())
	}
	if missing && !listed {
		// Nothing is left on disk or in git's list; only the branch remains.
		if !p.run.assessOnly && p.run.mode == config.CleanupModeOn {
			p.m.setTaskWorktree(t, "", t.WorktreeBranch)
		}
		return true
	}
	clean := true
	if !missing {
		if !listed {
			return refuse(taskKeepUnverified, "the folder is not a worktree of the base repository")
		}
		// Without its .git entry, git would answer for a parent repository.
		if _, err := os.Lstat(filepath.Join(path, ".git")); err != nil {
			return refuse(taskKeepUnverified, "the folder has no .git entry, so its files cannot be checked")
		}
		own, isClean, err := taskWorktreeStatus(p.ctx, path, p.verifyDirs)
		if err != nil {
			return refuse(taskKeepUnverified, "git status failed: "+err.Error())
		}
		if len(own) > 0 {
			return refuse(taskKeepUncommitted, "the worktree holds uncommitted or untracked files", own...)
		}
		clean = isClean
	}
	if listed && wt.detached {
		onBranch := false
		if tip != "" {
			_, aerr := taskGitOut(p.ctx, p.repo, "merge-base", "--is-ancestor", wt.head, tip)
			onBranch = aerr == nil
		}
		if !onBranch && commitsLanded(p.ctx, p.repo, p.defaultBranch(), p.prd, wt.head) == "" {
			return refuse(taskKeepDetached, "the worktree's detached HEAD "+wt.head+" holds commits that are on neither the task branch nor the default branch")
		}
	}
	switch {
	case p.run.assessOnly:
		p.kept = append(p.kept, TaskWorktreeKept{TaskID: t.ID, Worktree: path, Branch: branch, BranchTip: tip,
			Reason: taskKeepByArchive, Detail: "the worktree is clean; archiving does not remove task worktrees"})
		return false
	case p.run.mode != config.CleanupModeOn:
		p.keep(auditTaskWorktreeWouldRemove, TaskWorktreeKept{TaskID: t.ID, Worktree: path, Branch: branch, BranchTip: tip,
			Reason: taskKeepReportMode, Detail: "cleanup_mode is report: the worktree is clean and would be removed"})
		return false
	}
	if !p.spend(t.ID) {
		p.kept = append(p.kept, TaskWorktreeKept{TaskID: t.ID, Worktree: path, Branch: branch, BranchTip: tip,
			Reason: taskKeepOverLimit, Detail: "the per-pass removal limit was reached; a later pass takes it"})
		return false
	}
	// Without --force git itself refuses a worktree that changed since the
	// check. It is passed only when what is left is the daemon's own.
	args := []string{"-c", showUntrackedOverride, "worktree", "remove"}
	if !clean || missing {
		args = append(args, "--force")
	}
	if _, err := taskGitOut(p.ctx, p.repo, append(args, path)...); err != nil {
		return refuse(taskKeepRemoveFailed, err.Error())
	}
	p.removed++
	p.audit(auditTaskWorktreeRemove, t.ID, path, branch, tip, map[string]any{"head": wt.head})
	p.m.setTaskWorktree(t, "", t.WorktreeBranch)
	return true
}

// handleBranch deletes a task branch whose commits are on the default branch,
// after its tip is kept under a backup ref.
func (p *taskCleanupPass) handleBranch(t *Task, branch, tip string) {
	refuse := func(reason, detail string) {
		p.keep(auditTaskBranchKept, TaskWorktreeKept{TaskID: t.ID, Branch: branch, BranchTip: tip, Reason: reason, Detail: detail})
	}
	if why := p.neededBy("", branch); why != "" {
		refuse(taskKeepNeeded, why)
		return
	}
	def := p.defaultBranch()
	if def == "" {
		refuse(taskKeepNoDefaultBranch, "the default branch could not be resolved, so the branch cannot be checked")
		return
	}
	on := commitsLanded(p.ctx, p.repo, def, p.prd, tip)
	if on == "" {
		refuse(taskKeepUnmerged, "the branch has commits that are not on the default branch "+def)
		return
	}
	if p.run.assessOnly {
		refuse(taskKeepByArchive, "every commit is on "+on+"; archiving does not delete task branches")
		return
	}
	if p.run.mode != config.CleanupModeOn {
		refuse(taskKeepReportMode, "cleanup_mode is report: every commit is on "+on+" and the branch would be deleted")
		return
	}
	if !p.spend(t.ID) {
		p.kept = append(p.kept, TaskWorktreeKept{TaskID: t.ID, Branch: branch, BranchTip: tip,
			Reason: taskKeepOverLimit, Detail: "the per-pass removal limit was reached; a later pass takes it"})
		return
	}
	ref, err := writeBranchBackup(p.ctx, p.repo, branch, tip, p.m.holdNow().Unix())
	if err != nil {
		refuse(taskKeepBackupFailed, err.Error())
		return
	}
	if now, ok, err := branchTip(p.ctx, p.repo, branch); err != nil || !ok || now != tip {
		refuse(taskKeepDeleteFailed, "the branch moved while it was being checked")
		return
	}
	if _, err := taskGitOut(p.ctx, p.repo, "branch", "-D", branch); err != nil {
		refuse(taskKeepDeleteFailed, "the tip is kept at "+ref+": "+err.Error())
		return
	}
	p.deleted++
	p.audit(auditTaskBranchDelete, t.ID, "", branch, tip, map[string]any{"backup_ref": ref, "contained_in": on})
	p.m.setTaskWorktree(t, "", "")
}

func (m *Manager) setTaskWorktree(t *Task, path, branch string) {
	m.store.WithLock(func() { t.WorktreePath, t.WorktreeBranch = path, branch })
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] task worktree cleanup: save task %s: %v", t.ID, err)
	}
}

// finish writes the refusals to the audit log and the outcome to the Automaton
// record. A pass that changes nothing writes nothing, so the periodic end paths
// do not repeat themselves.
func (p *taskCleanupPass) finish() {
	var prev *TaskWorktreeCleanup
	p.m.store.WithLock(func() { prev = p.prd.TaskWorktreeCleanup })

	seen := map[string]bool{}
	if prev != nil {
		for _, k := range prev.Kept {
			seen[keptKey(k)] = true
		}
	}
	// The off mode and the archive step only record; they act on nothing.
	if p.run.mode != config.CleanupModeOff && !p.run.assessOnly {
		for _, ev := range p.events {
			if seen[keptKey(ev.entry)] {
				continue
			}
			p.audit(ev.action, ev.entry.TaskID, ev.entry.Worktree, ev.entry.Branch, ev.entry.BranchTip,
				map[string]any{"reason": ev.entry.Reason, "detail": ev.entry.Detail, "files": ev.entry.Files})
		}
	}

	if prev == nil && p.removed == 0 && p.deleted == 0 && len(p.kept) == 0 {
		return
	}
	rec := &TaskWorktreeCleanup{Mode: p.run.mode, WorktreesRemoved: p.removed, BranchesDeleted: p.deleted, Kept: p.kept}
	if prev != nil {
		rec.WorktreesRemoved += prev.WorktreesRemoved
		rec.BranchesDeleted += prev.BranchesDeleted
	}
	for _, k := range rec.Kept {
		if rec.Reasons == nil {
			rec.Reasons = map[string]int{}
		}
		rec.Reasons[k.Reason]++
		if k.Worktree != "" {
			rec.WorktreesKept++
		}
		if k.Branch != "" {
			rec.BranchesKept++
		}
	}
	rec.Message = taskCleanupMessage(rec)
	if prev != nil {
		rec.At = prev.At
		if reflect.DeepEqual(prev, rec) {
			return
		}
	}
	rec.At = p.m.holdNow().UTC()
	if err := p.m.store.saveTaskWorktreeCleanup(p.prd, rec); err != nil {
		log.Printf("[autonomous] task worktree cleanup: save the outcome for %s: %v", p.prd.ID, err)
	}
	log.Printf("[autonomous] task worktree cleanup %s: mode=%q %s", p.prd.ID, rec.Mode, rec.Message)
}

func keptKey(k TaskWorktreeKept) string {
	return k.TaskID + "\x00" + k.Worktree + "\x00" + k.Branch + "\x00" + k.BranchTip + "\x00" + k.Reason
}

func taskCleanupMessage(rec *TaskWorktreeCleanup) string {
	msg := fmt.Sprintf("task worktrees: %d removed, %d kept; task branches: %d deleted, %d kept",
		rec.WorktreesRemoved, rec.WorktreesKept, rec.BranchesDeleted, rec.BranchesKept)
	reasons := make([]string, 0, len(rec.Reasons))
	for r, n := range rec.Reasons {
		reasons = append(reasons, fmt.Sprintf("%s %d", r, n))
	}
	if len(reasons) == 0 {
		return msg
	}
	sort.Strings(reasons)
	return msg + " (kept because: " + strings.Join(reasons, ", ") + ")"
}

// saveTaskWorktreeCleanup stores the outcome on the Automaton. It leaves
// UpdatedAt and Rev alone: this is a note about the work trees, and it must not
// restart the retention windows that count from UpdatedAt.
func (s *Store) saveTaskWorktreeCleanup(p *PRD, rec *TaskWorktreeCleanup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.TaskWorktreeCleanup = rec
	cur, ok := s.prds[p.ID]
	if !ok {
		return nil // not a stored Automaton
	}
	cur.TaskWorktreeCleanup = rec
	return s.persistPRD(p.ID)
}
