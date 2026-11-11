// Actions for an unlanded Automaton branch: keep it, archive it under a ref
// namespace, or delete its worktree and branch. Deleting is the only
// destructive action, so it refuses unless the branch has been classified and
// the caller passed an explicit confirm, and it writes a git bundle backup of
// the branch before removing anything. If the backup fails nothing is removed.
package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// UnlandedArchiveRefPrefix is the ref namespace archived branches move to.
const UnlandedArchiveRefPrefix = "refs/archive/"

// UnlandedBackupsDir is the folder under the worktree base that holds bundle
// backups written before a delete.
const UnlandedBackupsDir = "backups"

const unlandedKeepFile = "unlanded-keep.json"

// UnlandedKeep is a recorded decision to leave a branch alone.
type UnlandedKeep struct {
	Branch string    `json:"branch"`
	Note   string    `json:"note,omitempty"`
	At     time.Time `json:"at"`
}

var unlandedKeepMu sync.Mutex

func loadUnlandedKeeps(stateDir string) ([]UnlandedKeep, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, unlandedKeepFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keeps []UnlandedKeep
	if err := json.Unmarshal(b, &keeps); err != nil {
		return nil, fmt.Errorf("read keep decisions: %w", err)
	}
	return keeps, nil
}

// Keep records that the operator wants branch left alone. The cleaner and the
// unlanded list consult UnlandedKept and skip it. Keeping twice updates the
// note.
func Keep(stateDir, branch, note string) error {
	branch = strings.TrimSpace(branch)
	if stateDir == "" || branch == "" {
		return errors.New("keep: state dir and branch are required")
	}
	unlandedKeepMu.Lock()
	defer unlandedKeepMu.Unlock()
	keeps, err := loadUnlandedKeeps(stateDir)
	if err != nil {
		return err
	}
	entry := UnlandedKeep{Branch: branch, Note: note, At: time.Now().UTC()}
	replaced := false
	for i := range keeps {
		if keeps[i].Branch == branch {
			keeps[i] = entry
			replaced = true
		}
	}
	if !replaced {
		keeps = append(keeps, entry)
	}
	b, err := json.MarshalIndent(keeps, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, unlandedKeepFile), b, 0o600)
}

// UnlandedKept reports whether a keep decision exists for branch.
func UnlandedKept(stateDir, branch string) bool {
	unlandedKeepMu.Lock()
	defer unlandedKeepMu.Unlock()
	keeps, _ := loadUnlandedKeeps(stateDir)
	for _, k := range keeps {
		if k.Branch == branch {
			return true
		}
	}
	return false
}

// ArchiveBranch moves branch under the archive ref namespace
// (refs/archive/<branch>) and removes the branch name. The commits stay
// reachable from the archive ref. It returns the new ref.
func ArchiveBranch(ctx context.Context, git MergeGitRunner, baseRepo, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", errors.New("archive: branch is required")
	}
	sha, err := git.Run(ctx, baseRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("archive: branch %s not found: %w", branch, err)
	}
	ref := UnlandedArchiveRefPrefix + branch
	if _, err := git.Run(ctx, baseRepo, "update-ref", ref, strings.TrimSpace(sha)); err != nil {
		return "", fmt.Errorf("archive: write %s: %w", ref, err)
	}
	if out, err := git.Run(ctx, baseRepo, "worktree", "list", "--porcelain"); err == nil {
		if wt, ok := firstMap(parseWorktreeList(out))[branch]; ok {
			return "", fmt.Errorf("archive: branch %s is checked out in %s; archive ref %s was written, branch left in place", branch, wt, ref)
		}
	}
	if _, err := git.Run(ctx, baseRepo, "branch", "-D", branch); err != nil {
		return "", fmt.Errorf("archive: remove branch %s: %w", branch, err)
	}
	return ref, nil
}

func firstMap(m map[string]string, _ string) map[string]string { return m }

// DeleteWorktreeOptions controls DeleteWorktree.
type DeleteWorktreeOptions struct {
	// Branch is the unlanded branch entry, as classified by
	// ListUnlandedBranches. An entry without a known recommendation is
	// refused.
	Branch UnlandedBranch
	// Confirm must be true; the delete never runs on a default.
	Confirm bool
	// BaseRepo is the repository holding the branch.
	BaseRepo string
	// WorktreeBase is the worktree base folder; backups go under its
	// backups folder.
	WorktreeBase string
}

// DeleteWorktreeResult describes a completed delete.
type DeleteWorktreeResult struct {
	Branch       string `json:"branch"`
	BundlePath   string `json:"bundle_path"`
	WorktreePath string `json:"worktree_path,omitempty"`
}

var unlandedUnsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DeleteWorktree backs up the branch as a git bundle, then removes its
// worktree and branch. It refuses unless the branch is classified and Confirm
// is set, and aborts without removing anything if the bundle cannot be
// written and verified.
func DeleteWorktree(ctx context.Context, git MergeGitRunner, opts DeleteWorktreeOptions) (DeleteWorktreeResult, error) {
	b := opts.Branch
	res := DeleteWorktreeResult{Branch: b.Branch}
	switch b.Recommendation {
	case UnlandedLanded, UnlandedSuperseded, UnlandedNeedsDecision:
	default:
		return res, fmt.Errorf("delete refused: branch %q is not classified", b.Branch)
	}
	if strings.TrimSpace(b.Branch) == "" {
		return res, errors.New("delete refused: branch is required")
	}
	if !opts.Confirm {
		return res, fmt.Errorf("delete refused: confirm is required to delete %s", b.Branch)
	}
	if opts.BaseRepo == "" || opts.WorktreeBase == "" {
		return res, errors.New("delete refused: base repo and worktree base are required")
	}
	if _, err := git.Run(ctx, opts.BaseRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+b.Branch); err != nil {
		return res, fmt.Errorf("delete refused: branch %s not found: %w", b.Branch, err)
	}

	// Backup first. Nothing is removed until the bundle is verified.
	dir := filepath.Join(opts.WorktreeBase, UnlandedBackupsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, fmt.Errorf("delete aborted: backup folder: %w", err)
	}
	name := unlandedUnsafeName.ReplaceAllString(b.Branch, "_") + "-" + time.Now().UTC().Format("20060102T150405Z") + ".bundle"
	bundle := filepath.Join(dir, name)
	if _, err := git.Run(ctx, opts.BaseRepo, "bundle", "create", bundle, "refs/heads/"+b.Branch); err != nil {
		_ = os.Remove(bundle)
		return res, fmt.Errorf("delete aborted: backup bundle failed: %w", err)
	}
	if _, err := git.Run(ctx, opts.BaseRepo, "bundle", "verify", bundle); err != nil {
		_ = os.Remove(bundle)
		return res, fmt.Errorf("delete aborted: backup bundle did not verify: %w", err)
	}
	res.BundlePath = bundle

	if out, err := git.Run(ctx, opts.BaseRepo, "worktree", "list", "--porcelain"); err == nil {
		byBranch, _ := parseWorktreeList(out)
		if wt, ok := byBranch[b.Branch]; ok {
			if _, err := git.Run(ctx, opts.BaseRepo, "worktree", "remove", "--force", wt); err != nil {
				return res, fmt.Errorf("remove worktree %s (backup kept at %s): %w", wt, bundle, err)
			}
			res.WorktreePath = wt
		}
	}
	if _, err := git.Run(ctx, opts.BaseRepo, "branch", "-D", b.Branch); err != nil {
		return res, fmt.Errorf("remove branch %s (backup kept at %s): %w", b.Branch, bundle, err)
	}
	return res, nil
}
