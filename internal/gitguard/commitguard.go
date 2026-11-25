package gitguard

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// CommitTokenEnv is the environment variable that carries the per-session
// commit token. Only the harness commit path sets it.
const CommitTokenEnv = "DATAWATCH_COMMIT_TOKEN"

// guardDirPrefix starts the name of every harness guard directory so that
// reaping matches nothing else under the OS temp dir.
const guardDirPrefix = "datawatch-commitguard-"

// guardHooks lists the hooks that can refuse. The reference-transaction hook
// is the backstop that still fires for commit --no-verify and for plumbing ref
// updates.
var guardHooks = []string{"pre-commit", "commit-msg", "reference-transaction"}

// passThroughHooks lists the other hooks written into a guard directory. They
// never refuse: because core.hooksPath points at the guard directory, each one
// only runs the repository's own hook of that name. push-to-checkout and
// proc-receive are left out, as git behaves differently when either one merely
// exists.
var passThroughHooks = []string{
	"applypatch-msg", "pre-applypatch", "post-applypatch",
	"pre-merge-commit", "prepare-commit-msg", "post-commit",
	"pre-rebase", "post-checkout", "post-merge", "post-rewrite",
	"pre-push", "pre-receive", "update", "post-receive", "post-update",
	"pre-auto-gc", "post-index-change", "sendemail-validate",
	"p4-changelist", "p4-prepare-changelist", "p4-post-changelist", "p4-pre-submit",
}

// CommitGuard owns a harness-controlled hooks directory that refuses commits
// and ref updates in one repository, the session's own, unless the commit
// token is present in the environment. All its worktrees count, as they share
// its git common directory; every other repository is allowed. A guard that
// knows no repository of its own refuses in every repository. Workers get
// Env() and cannot commit; the harness commits with CommitEnv() after
// validation.
//
// The guard stops a worker from committing by mistake. It is not a barrier
// against a program that sets out to get around it.
type CommitGuard struct {
	dir   string
	token string
	// keepDir is set when nothing shows who made the directory: Cleanup then
	// leaves it on disk.
	keepDir bool
	// adopted is set when the directory already existed, so a worker may be
	// pointing at it.
	adopted bool
	repo    string

	mu      sync.Mutex
	cleaned bool
}

// NewCommitGuard creates a guard that knows no repository of its own, so it
// refuses in every repository. See NewRepoCommitGuard.
func NewCommitGuard(sessionID string) (*CommitGuard, error) {
	return NewRepoCommitGuard(sessionID, "")
}

// NewRepoCommitGuard creates the hooks directory under the OS temp dir (never
// inside a worktree or repository) and writes the hook scripts. The guard
// refuses in the repository that holds projectDir. When that cannot be
// determined (no folder, or a folder outside git) it refuses in every
// repository.
func NewRepoCommitGuard(sessionID, projectDir string) (*CommitGuard, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("commit guard token: %w", err)
	}
	token := hex.EncodeToString(buf)

	dir, err := os.MkdirTemp("", guardDirPrefix+sanitizeSessionID(sessionID)+"-")
	if err != nil {
		return nil, fmt.Errorf("commit guard dir: %w", err)
	}
	g := &CommitGuard{dir: dir, token: token, repo: repositoryCommonDir(projectDir)}
	script := hookScript(token, g.repo)
	for _, name := range append(append([]string{}, guardHooks...), passThroughHooks...) {
		// #nosec G306 -- git hooks must be executable.
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("commit guard hook %s: %w", name, err)
		}
		if err := os.Chmod(filepath.Join(dir, name), 0o700); err != nil { // #nosec G302 -- hooks must be executable
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("commit guard hook %s: %w", name, err)
		}
	}
	return g, nil
}

// repositoryCommonDir returns the real path of the git common directory of the
// repository that holds dir, or "" when it cannot be determined.
func repositoryCommonDir(dir string) string {
	if dir == "" {
		return ""
	}
	// git prints a path relative to the folder it really runs in, so resolve
	// symlinks before joining.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return ""
	}
	if real, err = filepath.Abs(real); err != nil {
		return ""
	}
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "-C", real, "rev-parse", "--git-common-dir") // #nosec G204 -- fixed arguments; real is an existing folder passed as the -C value
	// These would make git answer for another repository.
	for _, kv := range os.Environ() {
		switch name, _, _ := strings.Cut(kv, "="); name {
		case "GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "GIT_CEILING_DIRECTORIES":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	common := strings.TrimSuffix(string(out), "\n")
	if common == "" {
		return ""
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(real, common)
	}
	if common, err = filepath.EvalSymlinks(common); err != nil {
		return ""
	}
	return common
}

// shellQuote makes v one literal word of a POSIX shell script.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// hookScript is the one script every hook in a guard directory runs; it goes
// by its own file name. ownRepo is the git common directory to guard, or "" to
// guard every repository. With the commit token nothing is checked or run, so
// the harness commit runs no hook a worker could have written. The token
// comparison must stay the first one in the script: tokenFromHook reads it.
func hookScript(token, ownRepo string) string {
	return `#!/bin/sh
# Generated by the harness. Do not edit.
if [ "${` + CommitTokenEnv + `:-}" != "` + token + `" ]; then
  dw_own=` + shellQuote(ownRepo) + `
  dw_name=${0##*/}
  # The git common directory of the repository this hook runs for.
  dw_cur=$(git rev-parse --git-common-dir </dev/null 2>/dev/null) || dw_cur=
  if [ -z "$dw_cur" ] && [ -n "${GIT_DIR:-}" ] && [ ! -f "$GIT_DIR/commondir" ]; then
    # git init runs a hook before rev-parse can open the new repository.
    dw_cur=${GIT_COMMON_DIR:-$GIT_DIR}
  fi
  case $dw_name in
  ` + strings.Join(guardHooks, " | ") + `)
    # Allow only what is certainly outside the session's repository. -ef
    # compares the folders themselves, whatever path or symlink names them.
    if [ -z "$dw_own" ] || [ ! -d "$dw_own" ] || [ ! -d "$dw_cur" ] || [ "$dw_cur" -ef "$dw_own" ]; then
      echo "commit refused: the harness commits after validation; the worker must not run git commit (or update refs directly)." >&2
      exit 1
    fi
    ;;
  esac
  # Run the repository's own hook, which core.hooksPath replaced here. Without
  # GIT_CONFIG_COUNT git reports the repository's own core.hooksPath.
  dw_dir=$(unset GIT_CONFIG_COUNT; git config --type=path --get core.hooksPath </dev/null 2>/dev/null)
  case $? in
  0) ;;
  1) dw_dir=${dw_cur:+$dw_cur/hooks} ;;
  *) dw_dir= ;;
  esac
  case $dw_dir in
  "" | *` + guardDirPrefix + `*) ;;
  *)
    if [ -f "$dw_dir/$dw_name" ] && [ -x "$dw_dir/$dw_name" ]; then
      exec "$dw_dir/$dw_name" "$@"
    fi
    ;;
  esac
fi
exit 0
`
}

// Adopted reports whether the guard directory existed before this CommitGuard,
// as opposed to one just made that no worker has been given.
func (g *CommitGuard) Adopted() bool { return g.adopted }

// Dir returns the hooks directory.
func (g *CommitGuard) Dir() string { return g.dir }

// Token returns the per-session commit token.
func (g *CommitGuard) Token() string { return g.token }

// Repository returns the git common directory a guard made by this process
// guards, or "" when it refuses in every repository. An adopted guard keeps
// what its hook scripts say and reports "".
func (g *CommitGuard) Repository() string { return g.repo }

// Env returns the config-injection environment that points core.hooksPath at
// the guard directory without writing to any .git/config.
func (g *CommitGuard) Env() []string {
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=" + g.dir,
	}
}

// CommitEnv returns Env() plus the commit token. Use it only for the
// harness commit.
func (g *CommitGuard) CommitEnv() []string {
	return append(g.Env(), CommitTokenEnv+"="+g.token)
}

// Cleanup removes the guard directory, unless it was adopted without an
// ownership record. It is safe to call repeatedly.
func (g *CommitGuard) Cleanup() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cleaned || g.keepDir {
		return nil
	}
	if err := os.RemoveAll(g.dir); err != nil {
		return err
	}
	g.cleaned = true
	return nil
}

// sanitizeSessionID keeps directory names simple and comparable.
func sanitizeSessionID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ScrubHooksPath removes a leftover core.hooksPath from the repository or
// worktree local config, but only when its value lies under guardDirPrefix.
// An empty guardDirPrefix means the harness guard location under the OS temp
// dir. Values outside the prefix are left untouched.
func ScrubHooksPath(repoDir, guardDirPrefix string) error {
	if guardDirPrefix == "" {
		guardDirPrefix = filepath.Join(os.TempDir(), guardDirPrefixName())
	}
	cmd := exec.Command("git", "-C", repoDir, "config", "--local", "--get-all", "core.hooksPath")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil // key not set
		}
		return fmt.Errorf("read core.hooksPath: %w", err)
	}
	for _, v := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		v = strings.TrimSpace(v)
		if v == "" || !strings.HasPrefix(filepath.Clean(v), guardDirPrefix) {
			return nil
		}
	}
	unset := exec.Command("git", "-C", repoDir, "config", "--local", "--unset-all", "core.hooksPath")
	if b, err := unset.CombinedOutput(); err != nil {
		return fmt.Errorf("unset core.hooksPath: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

func guardDirPrefixName() string { return guardDirPrefix }

// AdoptCommitGuard is AdoptOwnedRepoCommitGuard with no project folder: a
// guard it has to create refuses in every repository.
func AdoptCommitGuard(owner, sessionID string) (*CommitGuard, error) {
	return AdoptOwnedRepoCommitGuard(owner, sessionID, "")
}

// AdoptRepoCommitGuard is AdoptOwnedRepoCommitGuard for a caller with no
// owner: it takes only a directory with no ownership record, which Cleanup
// then leaves on disk.
func AdoptRepoCommitGuard(sessionID, projectDir string) (*CommitGuard, error) {
	return AdoptOwnedRepoCommitGuard("", sessionID, projectDir)
}

// AdoptOwnedRepoCommitGuard re-attaches to the guard directory an earlier
// daemon generation created for sessionID, reading the token back from the
// hook script. The worker's environment still points at that directory, so a
// fresh directory would not guard it. The hook scripts, and with them the
// guarded repository, stay as they are. A directory recorded as owner's is
// preferred. One with no ownership record (made by an earlier version) is
// still adopted, but Cleanup leaves it on disk. Another owner's directory is
// never adopted. When there is none to adopt it creates a new guard owned by
// owner for the repository that holds projectDir, as NewOwnedRepoCommitGuard
// does.
func AdoptOwnedRepoCommitGuard(owner, sessionID, projectDir string) (*CommitGuard, error) {
	tmp := os.TempDir()
	prefix := guardNamePrefix(sessionID)
	var unrecordedGuard *CommitGuard
	entries, err := os.ReadDir(tmp)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
				continue
			}
			dir := filepath.Join(tmp, e.Name())
			rec, ok, unrecorded := readGuardRecord(dir)
			owned := ok && owner != "" && rec.Owner == owner && rec.SessionID == sessionID
			if !owned && !unrecorded {
				continue
			}
			b, rerr := os.ReadFile(filepath.Join(dir, guardHooks[0])) // #nosec G304 -- path under the OS temp dir, harness-named
			if rerr != nil {
				continue
			}
			token := tokenFromHook(string(b))
			if token == "" {
				continue
			}
			if owned {
				return &CommitGuard{dir: dir, token: token, adopted: true}, nil
			}
			unrecordedGuard = &CommitGuard{dir: dir, token: token, keepDir: true, adopted: true}
		}
	}
	if unrecordedGuard != nil {
		return unrecordedGuard, nil
	}
	return NewOwnedRepoCommitGuard(owner, sessionID, projectDir)
}

// tokenFromHook extracts the token written by hookScript.
func tokenFromHook(script string) string {
	const mid = `" != "`
	i := strings.Index(script, mid)
	if i < 0 {
		return ""
	}
	rest := script[i+len(mid):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}
