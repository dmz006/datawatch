package gitguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrRepoNotOwned is returned (wrapped in *RepoNotOwnedError) when a folder
// is not the top level of its own git repository, so a git command run
// there would act on a foreign ancestor repository.
var ErrRepoNotOwned = errors.New("folder is not owned by its own git repository")

// RepoNotOwnedError names the folder and the foreign top level.
type RepoNotOwnedError struct {
	Op, Dir, Top string
}

func (e *RepoNotOwnedError) Error() string {
	return fmt.Sprintf("refusing git %s in %s: it is not a repository of its own (top level is %s)", e.Op, e.Dir, e.Top)
}

func (e *RepoNotOwnedError) Is(target error) bool { return target == ErrRepoNotOwned }

// RefusalHook, when set, is called on every refusal so callers can write a
// decision-log entry. Set it once at start-up.
var RefusalHook func(op, dir, top string)

// RepoOwnerOptions tunes CheckRepoOwner.
type RepoOwnerOptions struct {
	// ExpectedRepo is a folder of the repository a linked worktree must
	// belong to. A linked worktree is accepted only when its common git
	// dir equals the common git dir of ExpectedRepo.
	ExpectedRepo string
}

// probe runs one git rev-parse in dir with GIT_CEILING_DIRECTORIES removed.
func probe(ctx context.Context, dir string, args ...string) ([]string, error) {
	full := append([]string{"-C", dir, "rev-parse", "--path-format=absolute"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) // #nosec G204 -- fixed git args
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_CEILING_DIRECTORIES=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git rev-parse in %s: %w: %s", dir, err, strings.TrimSpace(errb.String()))
	}
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n"), nil
}

func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// CheckRepoOwner must run before any git status/add/commit/clean/reset/
// restore/checkout/stash/tag/rollback/worktree command in dir. It returns the
// resolved top level when dir is the top level of its own repository, or a
// linked worktree of opts.ExpectedRepo; otherwise an error matching
// ErrRepoNotOwned and no further git command is run.
func CheckRepoOwner(ctx context.Context, dir, op string, opts ...RepoOwnerOptions) (string, error) {
	var o RepoOwnerOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	rdir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	lines, err := probe(ctx, rdir, "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if len(lines) < 3 {
		return "", fmt.Errorf("unexpected git rev-parse output in %s", rdir)
	}
	top, gitDir, common := resolved(lines[0]), resolved(lines[1]), resolved(lines[2])
	refuse := func() (string, error) {
		if RefusalHook != nil {
			RefusalHook(op, rdir, top)
		}
		return "", &RepoNotOwnedError{Op: op, Dir: rdir, Top: top}
	}
	if top != rdir {
		return refuse()
	}
	if gitDir == common {
		return top, nil // own primary repository
	}
	// A linked worktree: only acceptable for the expected repository.
	if o.ExpectedRepo == "" {
		return refuse()
	}
	exp, err := filepath.EvalSymlinks(o.ExpectedRepo)
	if err != nil {
		return refuse()
	}
	el, err := probe(ctx, exp, "--git-common-dir")
	if err != nil || resolved(el[0]) != common {
		return refuse()
	}
	return top, nil
}

// IsHomeOrAncestor reports whether dir (symlinks resolved) is the user's
// home directory or an ancestor of it. Use it to forbid git init there.
func IsHomeOrAncestor(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	d, h := resolved(dir), resolved(home)
	if d == h {
		return true
	}
	rel, err := filepath.Rel(d, h)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
