package autonomous

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RestoreAttemptWork writes back the working-tree state that SnapshotAttemptWork
// recorded under ref. It reads the snapshot tree into a temporary index and
// checks every entry out over dir, then removes the files the snapshot recorded
// as deleted. HEAD, the user's real index and the stash are never touched: no
// reset, stash or branch checkout is run. An empty ref is a no-op.
//
// Every restored file gets an mtime of now, so the verifier's working-tree diff
// window (keyed on the attempt's StartedAt) sees the restored work.
func RestoreAttemptWork(ctx context.Context, dir, ref string) error {
	if ref == "" {
		return nil
	}
	tmpDir, err := os.MkdirTemp("", "attempt-restore-")
	if err != nil {
		return fmt.Errorf("create restore temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmpDir, "index"))

	top, err := snapshotGit(ctx, dir, env, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	// The snapshot commit's parent is the HEAD it was taken on. Any path that
	// exists in that parent but not in the snapshot tree was deleted in the
	// attempt, so it must be removed again after the checkout.
	deleted, err := restoreDeletedPaths(ctx, dir, env, ref)
	if err != nil {
		return err
	}
	if _, err := snapshotGit(ctx, dir, env, "read-tree", ref); err != nil {
		return err
	}
	files, err := snapshotGit(ctx, dir, env, "-c", "core.quotePath=false", "ls-files")
	if err != nil {
		return err
	}
	if _, err := snapshotGit(ctx, dir, env, "checkout-index", "-a", "-f"); err != nil {
		return err
	}

	now := time.Now()
	for _, f := range strings.Split(files, "\n") {
		if f == "" {
			continue
		}
		if err := os.Chtimes(filepath.Join(top, f), now, now); err != nil {
			return fmt.Errorf("touch restored file %s: %w", f, err)
		}
	}
	for _, f := range deleted {
		if err := os.Remove(filepath.Join(top, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove file deleted in snapshot %s: %w", f, err)
		}
	}
	return nil
}

// restoreDeletedPaths returns the repo-relative paths that the snapshot commit
// ref recorded as deleted relative to its parent.
func restoreDeletedPaths(ctx context.Context, dir string, env []string, ref string) ([]string, error) {
	out, err := snapshotGit(ctx, dir, env, "-c", "core.quotePath=false", "diff", "--name-only", "--diff-filter=D", ref+"^", ref)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\n") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
