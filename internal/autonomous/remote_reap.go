// Remote branch reap. Automata that are fully merged into the project's
// default branch and whose work stays on that branch leave their remote
// branch ref behind; ReapPass's local reaper deletes the local leftover but
// never touches the remote. The remote pass closes that half. It lists the
// Automata stored here (the ones with a work branch), proves each branch is
// merged (MergedMemberProof, read-only git in the base repo), checks that
// the branch tip is older than the retention window (whole days), and only
// then deletes the remote ref — after the branch tip's commit is safe: a
// backup ref is written first, verified to point at the tip, and the branch
// is deleted only after the backup exists. A branch that is not merged, is
// too new, has no remote ref on origin, or whose backup could not be verified
// is held and named with its reason; nothing ever deletes a branch that is
// not provably merged and old enough.
//
// The pass runs once, when the operator asks for it (REST, MCP, CLI or the
// chat surface) — there is no autonomous remote-deletion timer.
// autonomous.remote_reap_enabled (false or unset by default) decides whether
// that pass deletes or only reports; the read surface is the same either
// way.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// remoteReapBackupRefPrefix is the namespace that holds the commit kept when
// a remote branch is deleted: one ref per deleted branch, the newest last.
// It is separate from the local reap-backup namespace so the two surfaces
// list and restore their own refs.
const remoteReapBackupRefPrefix = "refs/datawatch/remote-reap-backup/"

func remoteReapBackupRef(branch string, ts int64) string {
	return remoteReapBackupRefPrefix + sanitizeAttemptTaskKey(branch) + "/" + strconv.FormatInt(ts, 10)
}

// RemoteReapEntry is one Automaton work branch in the remote reap report.
// Reaped holds the branches that would be reaped (report mode) or were
// deleted (an enabled pass); Held holds the branches that were kept and the
// reason they were not touched.
type RemoteReapEntry struct {
	// AutomatonID is the stored Automaton the branch belongs to, when one is
	// stored here; empty for a branch the engine cannot attribute to a
	// stored Automaton.
	AutomatonID string `json:"automaton_id,omitempty"`
	// Branch is the work branch on origin that is reaped or held.
	Branch string `json:"branch"`
	// Base is the default branch the branch is merged into (empty only for
	// entries held before the proof resolved one).
	Base string `json:"base,omitempty"`
	// Tip is the branch tip commit the entry is about; empty when git could
	// not resolve the local ref.
	Tip string `json:"tip,omitempty"`
	// AgeDays is the branch tip's age in whole days at the pass instant, for
	// entries that far enough to read it.
	AgeDays int `json:"age_days,omitempty"`
	// BackupRef is the backup ref that keeps the branch tip: the one written
	// by an enabled pass that deleted the branch, or an existing one in a
	// report-only pass when a previous enabled pass already kept the tip.
	BackupRef string `json:"backup_ref,omitempty"`
	// Reason explains a held entry, and the outcome of a reaped one ("would
	// be reaped" in report mode, "reaped" in an enabled pass).
	Reason string `json:"reason,omitempty"`
}

// RemoteReapReport is the result of one remote reap evaluation (or pass):
// the reaped list, the held list, and whether the pass was report-only.
type RemoteReapReport struct {
	// Reaped is every Automaton work branch that is merged and old enough to
	// be reaped: those that would be (ReportOnly is true) or those that were
	// deleted (ReportOnly is false). Sorted by branch name.
	Reaped []RemoteReapEntry `json:"reaped"`
	// Held is every branch that was not reaped and why: not merged, too new
	// for the retention window, no remote ref on origin, or (in an enabled
	// pass) a backup that could not be verified before the delete. Sorted by
	// branch name.
	Held []RemoteReapEntry `json:"held"`
	// ReportOnly is true when the pass only reported — autonomous.
	// remote_reap_enabled was false or unset (the default) — and false when
	// the pass is enabled to delete.
	ReportOnly bool `json:"report_only"`
}

// RemoteReapSurfaceReport is the read side of the remote reap REST surface:
// the report plus the backup refs that exist and the effective retention
// window, so the operator can see and restore what each pass keeps.
type RemoteReapSurfaceReport struct {
	RemoteReapReport
	// Backups is every ref under the remote reap-backup namespace, with the
	// object id it points at; branch is filled when the ref key matches a
	// stored Automaton work branch.
	Backups []ReapBackup `json:"backups"`
	// RetentionDays is the effective autonomous.remote_reap_retention_days
	// at the instant the report was computed.
	RetentionDays int `json:"retention_days"`
	// ComputedAt is when the report was computed (UTC).
	ComputedAt time.Time `json:"computed_at"`
}

// RemoteReapRestoreResult is the answer to a remote reap restore: the
// branch, the backup ref it was restored from, and the commit it now points
// at.
type RemoteReapRestoreResult struct {
	Branch    string `json:"branch"`
	BackupRef string `json:"backup_ref"`
	SHA       string `json:"sha"`
}

// ErrRemoteReapBusy is returned by Manager.RemoteReapRun when another pass
// is still in flight.
var ErrRemoteReapBusy = errors.New("remote branch reap: another pass is already running")

// remoteReapRunning serializes remote reap passes on one base repo.
var remoteReapRunning sync.Mutex

// RemoteReapSettingsFunc reports the live autonomous.remote_reap settings:
// enabled (delete rather than report) and the retention window in whole
// days. The daemon wires one closure that reads the live config so a
// settings change applies without a restart; nil = the defaults (disabled,
// 30 days).
type RemoteReapSettingsFunc func() (enabled bool, retentionDays int)

// SetRemoteReapSettings wires the daemon's live remote-reap settings onto
// the manager, like SetGitPushSettings: the function is read on every pass.
func (m *Manager) SetRemoteReapSettings(fn RemoteReapSettingsFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.remoteReapSettingsFn = fn
}

// remoteReapSettings returns the live settings, or the defaults (disabled,
// RemoteReapRetentionDaysDefault days) when the daemon wired none.
func (m *Manager) remoteReapSettings() (bool, int) {
	m.mu.Lock()
	fn := m.remoteReapSettingsFn
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return false, config.RemoteReapRetentionDaysDefault
}

// RemoteReapPreview evaluates the remote reap without deleting (or writing
// any backup ref) and returns the report, the backup refs under the remote
// reap-backup namespace, and an error when the base repo is not configured.
func (m *Manager) RemoteReapPreview(ctx context.Context) (RemoteReapSurfaceReport, []ReapBackup, error) {
	repo, err := m.reapRepo()
	if err != nil {
		return RemoteReapSurfaceReport{}, nil, err
	}
	_, retention := m.remoteReapSettings()
	git := NewMergeGitRunner()
	now := time.Now().UTC()

	rep, err := RemoteReapPreview(ctx, git, repo, m.reapPRDs(), now, retention)
	if err != nil {
		return RemoteReapSurfaceReport{}, nil, err
	}
	backups, err := ListRemoteReapBackups(ctx, git, repo)
	if err != nil {
		return RemoteReapSurfaceReport{}, nil, err
	}
	for i := range backups {
		backups[i].Branch = m.remoteReapBackupBranch(backups[i].Ref)
	}

	report := RemoteReapSurfaceReport{
		RemoteReapReport: rep,
		Backups:          backups,
		RetentionDays:    retention,
		ComputedAt:       now,
	}
	return report, backups, nil
}

// RemoteReapRun performs one remote reap pass in the base repo. It deletes
// nothing while autonomous.remote_reap_enabled is false or unset (the
// default): the same evaluation runs and ReportOnly is true in the report.
// Only merged, old-enough branches with their tip backed up are deleted.
// Concurrent passes on the same manager are refused with ErrRemoteReapBusy.
func (m *Manager) RemoteReapRun(ctx context.Context) (RemoteReapReport, error) {
	if !remoteReapRunning.TryLock() {
		return RemoteReapReport{}, ErrRemoteReapBusy
	}
	defer remoteReapRunning.Unlock()

	repo, err := m.reapRepo()
	if err != nil {
		return RemoteReapReport{}, err
	}
	enabled, retention := m.remoteReapSettings()
	return RemoteReapPass(ctx, NewMergeGitRunner(), repo, m.reapPRDs(), time.Now().UTC(), retention, enabled)
}

// RemoteReapRestore recreates one reaped branch from its newest backup ref
// under the remote reap-backup namespace, returning the ref and the commit
// the restored branch points at. It errors — without creating a branch —
// when no backup ref for the branch exists.
func (m *Manager) RemoteReapRestore(ctx context.Context, branch string) (RemoteReapRestoreResult, error) {
	repo, err := m.reapRepo()
	if err != nil {
		return RemoteReapRestoreResult{}, err
	}
	ref, sha, err := RestoreRemoteReapedDetail(ctx, NewMergeGitRunner(), repo, branch)
	if err != nil {
		return RemoteReapRestoreResult{}, err
	}
	return RemoteReapRestoreResult{Branch: strings.TrimSpace(branch), BackupRef: ref, SHA: sha}, nil
}

// remoteReapBackupBranch recovers the stored Automaton work-branch name a
// backup ref was written for: the ref key is the sanitized branch name, so
// the match is exact-key. Empty when no stored Automaton owns the key.
func (m *Manager) remoteReapBackupBranch(ref string) string {
	rest := strings.TrimPrefix(ref, remoteReapBackupRefPrefix)
	i := strings.LastIndexByte(rest, '/')
	if i < 0 {
		return ""
	}
	key := rest[:i]
	for _, p := range m.reapPRDs() {
		if strings.TrimSpace(p.Git.Branch) != "" && sanitizeAttemptTaskKey(p.Git.Branch) == key {
			return p.Git.Branch
		}
	}
	return ""
}

// RemoteReapPreview is the report side of the remote reap: which Automaton
// work branches would be deleted from origin and which would be held, with
// reasons. It writes nothing — no backup refs, no deletions. A repo of empty
// reads as "nothing here to reap", not an error (the Manager gates the
// missing-base-repo case for the surfaces).
func RemoteReapPreview(ctx context.Context, git MergeGitRunner, repo string, prds []*PRD, now time.Time, retentionDays int) (RemoteReapReport, error) {
	return remoteReapPass(ctx, git, repo, prds, now, retentionDays, false)
}

// RemoteReapPass performs one remote reap pass. With apply true it deletes
// the merged, old-enough remote branches after backing up and verifying
// their tip; with apply false it is the report-only evaluation (the default
// mode). It never deletes a branch that is not provably merged into its
// base, whose tip is not older than the retention window, that has no
// remote ref on origin, or whose backup could not be verified.
func RemoteReapPass(ctx context.Context, git MergeGitRunner, repo string, prds []*PRD, now time.Time, retentionDays int, apply bool) (RemoteReapReport, error) {
	return remoteReapPass(ctx, git, repo, prds, now, retentionDays, apply)
}

func remoteReapPass(ctx context.Context, git MergeGitRunner, repo string, prds []*PRD, now time.Time, retentionDays int, apply bool) (RemoteReapReport, error) {
	rep := RemoteReapReport{Reaped: []RemoteReapEntry{}, Held: []RemoteReapEntry{}}
	repo = strings.TrimSpace(repo)
	rep.ReportOnly = !apply
	if retentionDays < 0 {
		retentionDays = config.RemoteReapRetentionDaysDefault
	}
	if repo == "" || git == nil || len(prds) == 0 {
		return rep, nil
	}

	seen := make(map[string]bool, len(prds))
	for _, p := range prds {
		if p == nil {
			continue
		}
		branch := strings.TrimSpace(p.Git.Branch)
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true

		entry := RemoteReapEntry{AutomatonID: p.ID, Branch: branch}
		held := func(reason string) {
			entry.Reason = reason
			rep.Held = append(rep.Held, entry)
		}

		// 1. Merged proof: read-only git in the base repo; the default
		// branch is resolved per project, never assumed.
		proof := MergedMemberProof(ctx, git, repo, branch)
		if !proof.Merged {
			why := "not fully merged"
			if base := strings.TrimSpace(proof.Base); base != "" {
				why = "not fully merged into " + base
			}
			if reason := strings.TrimSpace(proof.Reason); reason != "" {
				why = why + ": " + reason
			}
			entry.Base = proof.Base
			held(why)
			continue
		}
		base := proof.Base
		entry.Base = base

		// 2. The branch tip and its commit date: the age is measured from
		// the tip of the branch ref locally (the base repo is the operator's
		// checkout, where the branch ref exists to be reaped).
		out, err := git.Run(ctx, repo, "log", "-1", "--format=%H %cI", "refs/heads/"+branch)
		if err != nil {
			held("no local branch ref to read the tip of " + branch + ": " + err.Error() + " " + strings.TrimSpace(out))
			continue
		}
		fields := strings.Fields(strings.TrimSpace(out))
		if len(fields) < 2 {
			held("could not read the tip of " + branch + ": got " + strconv.Quote(strings.TrimSpace(out)))
			continue
		}
		tip := fields[0]
		commitTime, perr := time.Parse(time.RFC3339, fields[1])
		if perr != nil {
			held("could not read the commit date of the tip of " + branch + ": " + perr.Error())
			continue
		}
		if !commitTime.Before(now) {
			held("the branch tip of " + branch + " is not in the past of the pass instant (clock skew); not reaped")
			continue
		}
		ageDays := int(now.Sub(commitTime).Truncate(24*time.Hour) / (24 * time.Hour))
		entry.Tip = tip
		entry.AgeDays = ageDays
		if ageDays < retentionDays {
			held(fmt.Sprintf("the merged commit is %d day(s) old; the retention window is %d whole day(s)", ageDays, retentionDays))
			continue
		}

		// 3. Remote presence: an enabled pass would push, and a report should
		// not list a branch whose remote ref is already gone as reaped.
		if !remoteBranchOnOrigin(ctx, git, repo, branch) {
			held("the remote branch does not exist on origin; there is nothing to delete")
			continue
		}

		// Eligible.
		if !apply {
			entry.Reason = fmt.Sprintf("would be reaped: fully merged into %s and %d day(s) old (retention window %d whole day(s))", base, ageDays, retentionDays)
			if existing, _, lerr := latestRemoteReapBackupRef(ctx, git, repo, branch); lerr == nil {
				entry.BackupRef = existing
				entry.Reason += "; a backup ref from a previous pass already keeps the tip at " + existing
			}
			rep.Reaped = append(rep.Reaped, entry)
			continue
		}

		// 4. Backup first: reuse a backup that already keeps this exact tip,
		// otherwise write a fresh one; verify it before the delete.
		ref, bsha, lerr := latestRemoteReapBackupRef(ctx, git, repo, branch)
		if lerr != nil || strings.TrimSpace(bsha) != tip {
			ref = remoteReapBackupRef(branch, now.Unix())
			if _, uerr := git.Run(ctx, repo, "update-ref", ref, tip); uerr != nil {
				held("the backup ref could not be written (" + ref + "); the remote branch was not deleted: " + uerr.Error())
				entry.BackupRef = ""
				continue
			}
		}
		if verify, verr := git.Run(ctx, repo, "rev-parse", "--verify", ref); verr != nil || strings.TrimSpace(verify) != tip {
			held("the backup ref " + ref + " does not point at the branch tip; the remote branch was not deleted")
			entry.BackupRef = ref
			continue
		}

		// 5. Only now, with the tip kept and verified, delete the remote ref.
		if out, derr := git.Run(ctx, repo, "push", "--delete", "origin", branch); derr != nil {
			entry.BackupRef = ref
			held("the remote branch could not be deleted; its tip is kept at " + ref + ": " + strings.TrimSpace(out) + " " + derr.Error())
			continue
		}
		entry.BackupRef = ref
		entry.Reason = fmt.Sprintf("reaped: fully merged into %s and %d day(s) old (retention window %d whole day(s)); the remote ref was deleted and the tip is kept at %s", base, ageDays, retentionDays, ref)
		rep.Reaped = append(rep.Reaped, entry)
	}

	sort.Slice(rep.Reaped, func(i, j int) bool { return rep.Reaped[i].Branch < rep.Reaped[j].Branch })
	sort.Slice(rep.Held, func(i, j int) bool { return rep.Held[i].Branch < rep.Held[j].Branch })
	return rep, nil
}

// remoteBranchOnOrigin reports whether origin carries the remote ref for
// branch. A git error (no origin configured, network refused) reads as
// "not present": the engine then holds the entry instead of pushing.
func remoteBranchOnOrigin(ctx context.Context, git MergeGitRunner, repo, branch string) bool {
	out, err := git.Run(ctx, repo, "ls-remote", "--heads", "origin", branch)
	if err != nil {
		return false
	}
	want := "refs/heads/" + branch
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == want {
			return true
		}
	}
	return false
}

// ErrNoRemoteReapBackup is the contract RestoreRemoteReaped reports when the
// branch was never remotely reaped here (no backup ref under the namespace).
var ErrNoRemoteReapBackup = errors.New("no remote reap backup ref")

// latestRemoteReapBackupRef returns the newest backup ref for branch (highest
// unix timestamp suffix) and the object id it points at.
func latestRemoteReapBackupRef(ctx context.Context, git MergeGitRunner, repo, branch string) (ref, sha string, err error) {
	key := sanitizeAttemptTaskKey(branch)
	prefix := remoteReapBackupRefPrefix + key + "/"
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return "", "", fmt.Errorf("no remote reap backup ref for branch %s: listing %s failed: %w", branch, prefix, err)
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
			continue
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
		return "", "", fmt.Errorf("no remote reap backup ref for branch %s: nothing under %s: %w", branch, prefix, ErrNoRemoteReapBackup)
	}
	sha, err = git.Run(ctx, repo, "rev-parse", newest)
	if err != nil {
		return "", "", fmt.Errorf("no remote reap backup ref for branch %s: reading %s: %v", branch, newest, err)
	}
	return newest, strings.TrimSpace(sha), nil
}

// RestoreRemoteReapedDetail recreates branch from its newest backup ref
// (refs/datawatch/remote-reap-backup/<branch>/<unix-ts>) and returns the
// backup ref it was restored from and the commit the branch now points at.
// It errors — without creating a branch — when no backup ref exists.
func RestoreRemoteReapedDetail(ctx context.Context, git MergeGitRunner, repo, branch string) (ref, sha string, err error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", "", fmt.Errorf("restore: a branch name is required")
	}
	// The pattern is matched here, in front of the git calls, so that no value
	// git could read as an option gets past this function.
	if !branchCharsRe.MatchString(branch) || !branchShapeOK(branch) {
		return "", "", fmt.Errorf("restore: %q: %w", branch, ErrBadRestoreBranch)
	}
	ref, sha, err = latestRemoteReapBackupRef(ctx, git, repo, branch)
	if err != nil {
		return "", "", err
	}
	if out, err := git.Run(ctx, repo, "branch", branch, sha); err != nil {
		return "", "", fmt.Errorf("restore %s from %s: %v: %s", branch, ref, err, strings.TrimSpace(out))
	}
	return ref, sha, nil
}

// RestoreRemoteReaped recreates branch from its newest remote reap backup
// ref and returns the commit it now points at. It errors — without creating
// a branch — when no backup ref exists.
func RestoreRemoteReaped(ctx context.Context, git MergeGitRunner, repo, branch string) (string, error) {
	_, sha, err := RestoreRemoteReapedDetail(ctx, git, repo, branch)
	return sha, err
}

// ListRemoteReapBackups lists every backup ref under refs/datawatch/
// remote-reap-backup/ with the object id it points at, sorted by ref name.
// An empty namespace lists as an empty, non-nil slice.
func ListRemoteReapBackups(ctx context.Context, git MergeGitRunner, repo string) ([]ReapBackup, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return []ReapBackup{}, fmt.Errorf("list remote reap backups: no base repo is configured")
	}
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname) %(objectname)", remoteReapBackupRefPrefix)
	if err != nil {
		return nil, fmt.Errorf("list remote reap backups: listing %s: %v", remoteReapBackupRefPrefix, err)
	}
	backups := []ReapBackup{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], remoteReapBackupRefPrefix) {
			continue
		}
		backups = append(backups, ReapBackup{Ref: parts[0], SHA: parts[1]})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Ref < backups[j].Ref })
	return backups, nil
}
