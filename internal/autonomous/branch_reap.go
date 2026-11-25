// Branch reaper. ReapPass reaps the leftover task sub-branches of Automata
// whose work has settled: an Automaton's landing state is landed (every
// commit of its work branch is already on the base branch by content, git
// cherry) or dropped (its work branch is gone). A task sub-branch is reaped
// only when every one of its commits is contained in the parent Automaton's
// branch or in the base branch; a backup ref is written first and the branch
// is deleted only after the backup exists. Nothing here reaps a branch that
// still carries commits, touches an Automaton whose landing state is
// pending, or acts at all while a merge is in progress (the pass returns an
// early waiting report instead). All git work goes through MergeGitRunner so
// tests substitute a fake runner.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// reapBackupRefPrefix is the ref namespace holding one backup ref per reaped
// branch: refs/datawatch/reap-backup/<branch>/<unix-ts>.
const reapBackupRefPrefix = "refs/datawatch/reap-backup/"

// Landing states of an Automaton's work, as the reaper sees them.
const (
	// LandingStateLanded: the work branch exists and every commit of it is
	// already on the base branch by content.
	LandingStateLanded = "landed"
	// LandingStateDropped: the work branch no longer exists.
	LandingStateDropped = "dropped"
	// LandingStatePending: the work branch exists and still carries commits
	// that are not on the base branch; its task sub-branches are kept.
	LandingStatePending = "pending"
)

// ReapEntry is one branch the pass looked at, in either list: the branch, the
// Automaton it belongs to, and a plain reason — for a reaped branch where the
// backup lives and what contained it, for a waiting branch why it was kept.
type ReapEntry struct {
	AutomatonID string `json:"automaton_id,omitempty"`
	Branch      string `json:"branch"`
	// BackupRef is the ref the reaped branch's tip was kept at (reaped list).
	BackupRef string `json:"backup_ref,omitempty"`
	// ContainedIn names the branch (Automaton work branch or base branch)
	// whose content covers every commit of the reaped branch.
	ContainedIn string `json:"contained_in,omitempty"`
	// Commits is the number of commits the branch carried (reaped list).
	Commits int `json:"commits,omitempty"`
	// Reason is the plain sentence for either list.
	Reason string `json:"reason,omitempty"`
}

// ReapReport is the outcome of one ReapPass: the branches reaped (backup
// written, branch deleted) and the branches left standing, each with a plain
// reason. A pass that finds a merge in progress returns only waiting entries.
type ReapReport struct {
	Reaped  []ReapEntry `json:"reaped"`
	Waiting []ReapEntry `json:"waiting"`
}

// branchExists reports whether repo has a local branch named branch.
func localBranchExists(ctx context.Context, git MergeGitRunner, repo, branch string) bool {
	if strings.TrimSpace(branch) == "" {
		return false
	}
	_, err := git.Run(ctx, repo, "rev-parse", "-q", "--verify", "refs/heads/"+branch)
	return err == nil
}

// cherryUncontained counts the commits of branch whose patch is absent from
// target (git cherry lines beginning with plus). ok is false on a git error.
func cherryUncontained(ctx context.Context, git MergeGitRunner, repo, target, branch string) (uncontained, commits int, ok bool) {
	out, err := git.Run(ctx, repo, "cherry", target, branch)
	if err != nil {
		return 0, 0, false
	}
	uncontained, commits = 0, 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		commits++
		if strings.HasPrefix(line, "+") {
			uncontained++
		}
	}
	return uncontained, commits, true
}

// automatonLandingState classifies the landing state of one Automaton's work
// branch in repo against base: dropped when the branch is gone, landed when
// every commit is on base by content, pending otherwise. An unanswerable
// cherry reads as pending, so an unknown state never loses a branch.
func automatonLandingState(ctx context.Context, git MergeGitRunner, repo, base, branch string) string {
	if !localBranchExists(ctx, git, repo, branch) {
		return LandingStateDropped
	}
	uncontained, _, ok := cherryUncontained(ctx, git, repo, base, branch)
	if !ok || uncontained > 0 {
		return LandingStatePending
	}
	return LandingStateLanded
}

// mergeInProgress reports whether a merge is in progress in dir (MERGE_HEAD
// resolves). The same check abortMerge uses before it backs an unfinished
// merge out.
func mergeInProgress(ctx context.Context, git MergeGitRunner, dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, err := git.Run(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

// reapBackupRef names the backup ref for one reaped branch at unix time ts.
// The branch is sanitized to ref-safe characters (a slash-named branch such
// as automaton/abc becomes automaton-abc), so RestoreReaped and
// ExpireBackups recompute the same key from the branch alone.
func reapBackupRef(branch string, ts int64) string {
	return reapBackupRefPrefix + sanitizeAttemptTaskKey(branch) + "/" + strconv.FormatInt(ts, 10)
}

// ReapPass reaps the contained task sub-branches of the given Automata in
// repo, once per pass, in the given order. For every Automaton whose landing
// state is pending, and for every branch that still carries commits, the
// branch is left standing and reported waiting with a plain reason. A merge
// in progress in any of the Automata's worktrees ends the pass early: no
// backup ref is written, nothing is deleted, and the report lists only the
// waiting branches. repo empty means nothing is configured and nothing runs.
func ReapPass(ctx context.Context, git MergeGitRunner, repo string, prds []*PRD, now time.Time) (ReapReport, error) {
	return reapPass(ctx, git, repo, prds, now, true)
}

// ReapPreview computes the same report as ReapPass without applying it: it
// lists the branches that would be reaped and the branches that would be
// held, writes no backup ref and deletes no branch. A would-be-reaped
// branch's BackupRef is filled when a backup ref from an earlier pass
// already exists; the reason says so. It is the read half of the operator
// surface, so calling it never changes the repo.
func ReapPreview(ctx context.Context, git MergeGitRunner, repo string, prds []*PRD, now time.Time) (ReapReport, error) {
	return reapPass(ctx, git, repo, prds, now, false)
}

// reapPass is the shared body of ReapPass and ReapPreview; apply selects
// whether contained branches are backed up and deleted (true) or only
// reported (false).
func reapPass(ctx context.Context, git MergeGitRunner, repo string, prds []*PRD, now time.Time, apply bool) (ReapReport, error) {
	rep := ReapReport{Reaped: []ReapEntry{}, Waiting: []ReapEntry{}}
	repo = strings.TrimSpace(repo)
	if repo == "" || len(prds) == 0 {
		return rep, nil
	}

	// A merge in progress anywhere: the pass reaps nothing this round.
	for _, p := range prds {
		if p == nil {
			continue
		}
		if mergeInProgress(ctx, git, p.ProjectDir) {
			rep.Waiting = append(rep.Waiting, ReapEntry{
				AutomatonID: p.ID,
				Reason:      "a merge is in progress in " + p.ProjectDir,
			})
		}
	}
	if len(rep.Waiting) > 0 {
		return rep, nil
	}

	base, err := defaultBranchForRepo(ctx, git, repo)
	if err != nil {
		return rep, fmt.Errorf("branch reap: %w", err)
	}

	for _, p := range prds {
		if p == nil {
			continue
		}
		branch := strings.TrimSpace(p.Git.Branch)
		if branch == "" {
			continue
		}
		landing := automatonLandingState(ctx, git, repo, base, branch)
		switch landing {
		case LandingStatePending:
			rep.Waiting = append(rep.Waiting, ReapEntry{
				AutomatonID: p.ID,
				Branch:      branch,
				Reason:      "the Automaton's landing state is pending: " + branch + " still has commits not on " + base,
			})
			continue
		case LandingStateDropped:
			// The work branch is gone; only the base branch can contain a
			// task sub-branch's commits.
		case LandingStateLanded:
		}

		// Containment targets, in order of preference: the Automaton work
		// branch first (when it still exists), then the base branch.
		targets := []string{base}
		if landing == LandingStateLanded {
			targets = []string{branch, base}
		}

		seen := map[string]struct{}{}
		var uncontainedCommits int
		var lastTarget string
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				t := &p.Story[si].Tasks[ti]
				tb := strings.TrimSpace(t.WorktreeBranch)
				if tb == "" {
					continue
				}
				if _, dup := seen[tb]; dup {
					continue
				}
				seen[tb] = struct{}{}
				if !localBranchExists(ctx, git, repo, tb) {
					continue // already gone; nothing to reap
				}

				containedIn := ""
				kept := false
				for _, target := range targets {
					uncontained, _, ok := cherryUncontained(ctx, git, repo, target, tb)
					if !ok {
						rep.Waiting = append(rep.Waiting, ReapEntry{
							AutomatonID: p.ID,
							Branch:      tb,
							Reason:      "git could not compare " + tb + " with " + target + "; the branch is kept",
						})
						kept = true
						break
					}
					if uncontained == 0 {
						containedIn = target
						break
					}
					uncontainedCommits = uncontained
					lastTarget = target
				}
				if kept || containedIn == "" {
					if !kept {
						rep.Waiting = append(rep.Waiting, ReapEntry{
							AutomatonID: p.ID,
							Branch:      tb,
							Reason:      fmt.Sprintf("%d commit(s) of %s are not in %s", uncontainedCommits, tb, lastTarget),
						})
					}
					continue
				}

				if apply {
					// Backup first: the branch tip is kept under the backup ref
					// before the branch is deleted, so a failed delete leaves a
					// restorable ref and a failed backup deletes nothing.
					_, commits, _ := cherryUncontained(ctx, git, repo, containedIn, tb)
					ref := reapBackupRef(tb, now.Unix())
					if _, err := git.Run(ctx, repo, "update-ref", ref, tb); err != nil {
						rep.Waiting = append(rep.Waiting, ReapEntry{
							AutomatonID: p.ID,
							Branch:      tb,
							Reason:      "backup ref " + ref + " could not be written: " + err.Error(),
						})
						continue
					}
					if out, err := git.Run(ctx, repo, "branch", "-D", tb); err != nil {
						rep.Waiting = append(rep.Waiting, ReapEntry{
							AutomatonID: p.ID,
							Branch:      tb,
							BackupRef:   ref,
							Reason:      "the branch could not be deleted but its tip is kept at " + ref + ": " + strings.TrimSpace(out) + " " + err.Error(),
						})
						continue
					}
					rep.Reaped = append(rep.Reaped, ReapEntry{
						AutomatonID: p.ID,
						Branch:      tb,
						BackupRef:   ref,
						ContainedIn: containedIn,
						Commits:     commits,
						Reason:      "every commit of " + tb + " is on " + containedIn,
					})
					continue
				}

				// Preview: name the branch as would-be-reaped. A backup ref
				// from an earlier pass is named when one exists.
				previous := ""
				if r, _, err := latestReapBackupRef(ctx, git, repo, tb); err == nil {
					previous = r
				}
				reason := "would be reaped: every commit of " + tb + " is on " + containedIn
				if previous != "" {
					reason += "; a backup ref from an earlier pass already exists at " + previous
				}
				rep.Reaped = append(rep.Reaped, ReapEntry{
					AutomatonID: p.ID,
					Branch:      tb,
					BackupRef:   previous,
					ContainedIn: containedIn,
					Reason:      reason,
				})
			}
		}
	}
	return rep, nil
}

// ErrNoReapBackup is wrapped by latestReapBackupRef when the branch has no
// backup ref under the reap-backup namespace; the REST surface answers it
// with a 404.
var ErrNoReapBackup = errors.New("no reap backup ref")

// latestReapBackupRef returns the newest backup ref for branch (highest unix
// timestamp suffix) and the object id it points at. The error is the contract
// RestoreReaped reports when the branch was never reaped here.
func latestReapBackupRef(ctx context.Context, git MergeGitRunner, repo, branch string) (ref, sha string, err error) {
	key := sanitizeAttemptTaskKey(branch)
	prefix := reapBackupRefPrefix + key + "/"
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return "", "", fmt.Errorf("no backup ref for branch %s: listing %s failed: %w", branch, prefix, err)
	}
	var newest string
	newestTS := int64(-1)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		suffix := line[len(prefix):]
		if i := strings.IndexByte(suffix, '/'); i >= 0 {
			continue // deeper nesting is not a reaped-branch backup
		}
		ts, convErr := strconv.ParseInt(suffix, 10, 64)
		if convErr != nil {
			continue
		}
		if ts > newestTS {
			newestTS = ts
			newest = line
		}
	}
	if newest == "" {
		return "", "", fmt.Errorf("no backup ref for branch %s: nothing under %s: %w", branch, prefix, ErrNoReapBackup)
	}
	sha, err = git.Run(ctx, repo, "rev-parse", newest)
	if err != nil {
		return "", "", fmt.Errorf("no backup ref for branch %s: reading %s: %v", branch, newest, err)
	}
	return newest, strings.TrimSpace(sha), nil
}

// RestoreReapedDetail recreates branch from its newest backup ref
// (refs/datawatch/reap-backup/<branch>/<unix-ts>) and returns the backup ref
// it was restored from and the commit the restored branch now points at. It
// errors — without creating a branch — when no backup ref for the branch
// exists.
func RestoreReapedDetail(ctx context.Context, git MergeGitRunner, repo, branch string) (ref, sha string, err error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", "", fmt.Errorf("restore: a branch name is required")
	}
	// The pattern is matched here, in front of the git calls, so that no value
	// git could read as an option gets past this function.
	if !branchCharsRe.MatchString(branch) || !branchShapeOK(branch) {
		return "", "", fmt.Errorf("restore: %q: %w", branch, ErrBadRestoreBranch)
	}
	ref, sha, err = latestReapBackupRef(ctx, git, repo, branch)
	if err != nil {
		return "", "", err
	}
	if out, err := git.Run(ctx, repo, "branch", branch, sha); err != nil {
		return "", "", fmt.Errorf("restore %s from %s: %v: %s", branch, ref, err, strings.TrimSpace(out))
	}
	return ref, sha, nil
}

// RestoreReaped recreates branch from its newest backup ref and returns the
// commit the restored branch points at (the ref itself is available from
// RestoreReapedDetail). It errors — without creating a branch — when no
// backup ref for the branch exists.
func RestoreReaped(ctx context.Context, git MergeGitRunner, repo, branch string) (string, error) {
	_, sha, err := RestoreReapedDetail(ctx, git, repo, branch)
	return sha, err
}

// ReapBackup is one backup ref under the reap-backup namespace: the ref name
// and the object id it points at. Branch is filled by the Manager when the
// sanitized ref key matches a stored task sub-branch; the engine leaves it
// empty.
type ReapBackup struct {
	Branch string `json:"branch,omitempty"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
}

// ListBackups lists every backup ref under refs/datawatch/reap-backup/ with
// the object id it points at, sorted by ref name. An empty namespace lists
// as an empty, non-nil slice.
func ListBackups(ctx context.Context, git MergeGitRunner, repo string) ([]ReapBackup, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return []ReapBackup{}, fmt.Errorf("list reap backups: no base repo is configured")
	}
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname) %(objectname)", reapBackupRefPrefix)
	if err != nil {
		return nil, fmt.Errorf("list reap backups: listing %s: %v", reapBackupRefPrefix, err)
	}
	backups := []ReapBackup{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], reapBackupRefPrefix) {
			continue
		}
		backups = append(backups, ReapBackup{Ref: parts[0], SHA: parts[1]})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Ref < backups[j].Ref })
	return backups, nil
}

// ExpireBackups deletes the reap-backup refs older than days (whole days
// before now) and returns the refs it deleted, oldest first. A days of zero
// or less reads as the autonomous.reaped_branch_backup_days default. A ref
// whose name does not end in a unix timestamp is never deleted.
func ExpireBackups(ctx context.Context, git MergeGitRunner, repo string, now time.Time, days int) ([]string, error) {
	if days <= 0 {
		days = config.ReapedBranchBackupDaysDefault
	}
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname)", reapBackupRefPrefix)
	if err != nil {
		return nil, fmt.Errorf("expire reap backups: listing %s: %v", reapBackupRefPrefix, err)
	}
	cutoff := now.Unix() - int64(days*24*60*60)
	type datedRef struct {
		ref string
		ts  int64
	}
	var due []datedRef
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, reapBackupRefPrefix) {
			continue
		}
		suffix := line[len(reapBackupRefPrefix):]
		i := strings.LastIndexByte(suffix, '/')
		if i < 0 {
			continue
		}
		ts, convErr := strconv.ParseInt(suffix[i+1:], 10, 64)
		if convErr != nil {
			continue
		}
		if ts < cutoff {
			due = append(due, datedRef{ref: line, ts: ts})
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].ts != due[j].ts {
			return due[i].ts < due[j].ts
		}
		return due[i].ref < due[j].ref
	})
	removed := []string{}
	for _, d := range due {
		if out, err := git.Run(ctx, repo, "update-ref", "-d", d.ref); err != nil {
			return removed, fmt.Errorf("expire reap backups: deleting %s: %v: %s", d.ref, err, strings.TrimSpace(out))
		}
		removed = append(removed, d.ref)
	}
	return removed, nil
}
