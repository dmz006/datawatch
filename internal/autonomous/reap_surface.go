// Operator surface for the task sub-branch reaper. ReapPreview is the read
// side: which task sub-branches would be reaped, which would be held, and
// which backup refs already exist. ReapRun performs one pass (a backup ref
// is written before each branch is deleted). ReapRestore recreates one
// reaped branch from its newest backup ref. The preview never writes a ref
// and never deletes a branch; nothing here is served from the safe-to-delete
// cache.
package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrReapBusy is returned by ReapRun when another reap pass is already in
// progress; the REST surface answers it with 409.
var ErrReapBusy = errors.New("branch reap: another pass is already running")

// reapRunning keeps two reap passes from interleaving their backup-ref
// writes and branch deletions.
var reapRunning sync.Mutex

// ReapSurfaceReport is the GET /api/autonomous/branch-reap body: the
// preview split (reaped and waiting), the backup refs that already exist
// and the backup retention in whole days.
type ReapSurfaceReport struct {
	ReapReport
	// Backups lists every backup ref under refs/datawatch/reap-backup/
	// (branch named when it matches a stored task sub-branch).
	Backups []ReapBackup `json:"backups"`
	// BackupDays is the retention in whole days (0 or unset reads 30);
	// the server fills it from the operator's config.
	BackupDays int `json:"backup_days"`
	// ComputedAt is when the preview was computed.
	ComputedAt time.Time `json:"computed_at"`
}

// ReapRestoreResult is the POST /api/autonomous/branch-reap/restore body:
// the branch that came back, the backup ref it was restored from and the
// commit it now points at.
type ReapRestoreResult struct {
	Branch    string `json:"branch"`
	BackupRef string `json:"backup_ref"`
	SHA       string `json:"sha"`
}

// reapPRDs is the slice of stored Automata the reaper looks at: the ones
// with a work branch set.
func (m *Manager) reapPRDs() []*PRD {
	all := m.store.ListPRDs()
	prds := []*PRD{}
	for _, p := range all {
		if p != nil && strings.TrimSpace(p.Git.Branch) != "" {
			prds = append(prds, p)
		}
	}
	return prds
}

// reapRepo is the configured base repo; it errors with ErrMergeNoBaseRepo
// when none is configured (the REST surface answers 503, as the other merge
// reads do).
func (m *Manager) reapRepo() (string, error) {
	repo := strings.TrimSpace(m.Config().WorktreeBaseRepo)
	if repo == "" {
		return "", ErrMergeNoBaseRepo
	}
	return repo, nil
}

// ReapPreview is the read side of the reap surface: the branches that would
// be reaped and held and every backup ref already under the namespace. It
// never writes a ref and never deletes a branch.
func (m *Manager) ReapPreview(ctx context.Context) (rep ReapReport, backups []ReapBackup, err error) {
	repo, err := m.reapRepo()
	if err != nil {
		return ReapReport{}, nil, err
	}
	git := NewMergeGitRunner()
	report, err := ReapPreview(ctx, git, repo, m.reapPRDs(), time.Now())
	if err != nil {
		return ReapReport{}, nil, err
	}
	backups, err = ListBackups(ctx, git, repo)
	if err != nil {
		return ReapReport{}, nil, err
	}
	// Name the branch each backup ref comes from when the sanitized ref key
	// matches a stored task sub-branch (several branches can sanitize to the
	// same key; the first stored match wins).
	for i := range backups {
		backups[i].Branch = m.reapBackupBranch(backups[i].Ref)
	}
	return report, backups, nil
}

// reapBackupBranch maps one backup ref name (
// refs/datawatch/reap-backup/<key>/<unix-ts>) back to the stored task
// sub-branch whose sanitized key is <key>.
func (m *Manager) reapBackupBranch(ref string) string {
	rest := strings.TrimPrefix(ref, reapBackupRefPrefix)
	i := strings.LastIndexByte(rest, '/')
	if i < 0 {
		return ""
	}
	key := rest[:i]
	for _, p := range m.reapPRDs() {
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				tb := strings.TrimSpace(p.Story[si].Tasks[ti].WorktreeBranch)
				if tb == "" {
					continue
				}
				if sanitizeAttemptTaskKey(tb) == key {
					return tb
				}
			}
		}
	}
	return ""
}

// ReapRun performs one reap pass: for every Automaton whose landing state
// has settled, each contained task sub-branch is backed up first and then
// deleted. Exactly one pass runs at a time; a second caller gets ErrReapBusy.
func (m *Manager) ReapRun(ctx context.Context) (ReapReport, error) {
	if !reapRunning.TryLock() {
		return ReapReport{}, ErrReapBusy
	}
	defer reapRunning.Unlock()
	repo, err := m.reapRepo()
	if err != nil {
		return ReapReport{}, err
	}
	return ReapPass(ctx, NewMergeGitRunner(), repo, m.reapPRDs(), time.Now())
}

// ReapRestore recreates branch from its newest backup ref and returns the
// backup ref it was restored from and the commit the branch now points at.
func (m *Manager) ReapRestore(ctx context.Context, branch string) (ref, sha string, err error) {
	repo, err := m.reapRepo()
	if err != nil {
		return "", "", err
	}
	return RestoreReapedDetail(ctx, NewMergeGitRunner(), repo, branch)
}
