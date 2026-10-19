package autonomous

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SnapshotAttemptWork records the complete working state of dir (modified
// tracked files plus untracked files that are not ignored) as a commit under
// refs/datawatch/attempts/<taskKey>/<attempt> and returns that full ref name.
// It works through a temporary index file, so the user's real index, working
// tree and stash are never touched. When dir has no changes relative to HEAD,
// it returns an empty ref, a nil error, and creates no ref.
func SnapshotAttemptWork(ctx context.Context, dir, taskKey string, attempt int) (ref string, err error) {
	tmpDir, err := os.MkdirTemp("", "attempt-snapshot-")
	if err != nil {
		return "", fmt.Errorf("create snapshot temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	key := sanitizeAttemptTaskKey(taskKey)
	env := append(os.Environ(),
		"GIT_INDEX_FILE="+filepath.Join(tmpDir, "index"),
		"GIT_AUTHOR_NAME=datawatch",
		"GIT_AUTHOR_EMAIL=datawatch@localhost",
		"GIT_COMMITTER_NAME=datawatch",
		"GIT_COMMITTER_EMAIL=datawatch@localhost",
	)

	head, err := snapshotGit(ctx, dir, env, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	headTree, err := snapshotGit(ctx, dir, env, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	if _, err := snapshotGit(ctx, dir, env, "read-tree", head); err != nil {
		return "", err
	}
	if _, err := snapshotGit(ctx, dir, env, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := snapshotGit(ctx, dir, env, "write-tree")
	if err != nil {
		return "", err
	}
	if tree == headTree {
		return "", nil
	}

	msg := fmt.Sprintf("datawatch attempt snapshot %s #%d", key, attempt)
	commit, err := snapshotGit(ctx, dir, env, "commit-tree", tree, "-p", head, "-m", msg)
	if err != nil {
		return "", err
	}
	ref = fmt.Sprintf("refs/datawatch/attempts/%s/%d", key, attempt)
	if _, err := snapshotGit(ctx, dir, env, "update-ref", ref, commit); err != nil {
		return "", err
	}
	return ref, nil
}

// sanitizeAttemptTaskKey keeps ASCII letters, digits, dash and underscore and
// replaces every other byte with a dash, so the key cannot carry path
// separators, dots or spaces into a ref name. An empty key becomes "task".
func sanitizeAttemptTaskKey(taskKey string) string {
	var b strings.Builder
	for i := 0; i < len(taskKey); i++ {
		c := taskKey[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}

// snapshotGit runs a fixed git subcommand in dir with the given environment
// and returns its trimmed stdout.
func snapshotGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git subcommands; only the sanitized task key and git's own output are passed as arguments
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
