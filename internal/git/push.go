// Git push helpers for push-on-complete.
//
// Transport-level primitives shared by the push engine and the
// operator-facing API surface: push-policy parsing, working-branch
// push, default-branch push, and an unpushed-commits counter. They
// shell out to `git` with a bounded timeout (caller's context plus
// the pushTimeout cap) and GIT_TERMINAL_PROMPT=0, so a missing
// credential fails fast instead of hanging the caller.
//
// Push discipline: pushes are always fast-forward only. No force flag
// and no '+' refspec ever leave this package; a non-fast-forward push
// is refused by git and surfaced as a non_fast_forward failure
// (see PushFailureKind), with the remote left exactly as it was.

package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PushPolicy is the wire format of a push-on-complete policy. It is
// an alias for string so the Push* constants below compare naturally
// in switch statements and equality checks at every call site.
type PushPolicy = string

// The supported push-on-complete policies. PushBranch is the
// historical default: an empty policy string parses to it.
const (
	// PushNever turns push-on-complete off for the scope in question.
	PushNever = "never"
	// PushBranch pushes the worker's working branch to its remote.
	PushBranch = "branch"
	// PushDefaultBranch fast-forwards HEAD onto the remote's default
	// branch (plain push, never a force).
	PushDefaultBranch = "default_branch"
)

// ParsePolicy normalises a policy string (trimmed, lowercased) and
// reports whether it is one of the supported values. The empty string
// is the historical default and parses to PushBranch; ok=false for
// anything unrecognised, so callers can keep their current policy
// rather than guessing.
func ParsePolicy(s string) (PushPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PushBranch, "":
		return PushBranch, true
	case PushNever:
		return PushNever, true
	case PushDefaultBranch:
		return PushDefaultBranch, true
	default:
		return "", false
	}
}

// PushFailureKind names the class of a failed (or unattempted) push,
// so callers can surface operator-appropriate guidance without
// re-parsing git stderr. A zero value means "no failure" (either
// Pushed or NothingToPush).
type PushFailureKind string

// The recognised failure classes.
const (
	// PushFailNoRemote: the named remote is not configured.
	PushFailNoRemote PushFailureKind = "no_remote"
	// PushFailAuth: authentication failed, or git could not read a
	// credential.
	PushFailAuth PushFailureKind = "auth"
	// PushFailNonFastForward: the remote branch already contains
	// commits the local one does not, so git refused the push. The
	// remote is unchanged — this package never forces.
	PushFailNonFastForward PushFailureKind = "non_fast_forward"
	// PushFailOther: everything else.
	PushFailOther PushFailureKind = "other"
)

// PushResult is the outcome of one push attempt. Pushed and
// NothingToPush are the two success shapes; on failure, Kind names
// the class and Reason is a redacted, bounded slice of git's own
// output.
type PushResult struct {
	Pushed        bool
	NothingToPush bool
	Remote        string
	Ref           string
	SHA           string
	Reason        string
	Kind          PushFailureKind
}

// pushTimeout caps every individual git call, on top of the
// caller's context, so a wedged credential helper cannot hang the
// engine indefinitely.
const pushTimeout = 2 * time.Minute

// urlCredsRe matches a URL carrying embedded user:pass credentials,
// for redaction out of error text.
var urlCredsRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^/@\s]+:[^/@\s]+@[^\s]+`)

// gitEnv returns the environment for git invocations: the process
// env plus GIT_TERMINAL_PROMPT=0 (fail fast rather than block on an
// interactive credential prompt).
func gitEnv() []string {
	return append([]string{"GIT_TERMINAL_PROMPT=0"}, os.Environ()...)
}

// runGit executes a bounded `git <args...>` in dir and returns the
// combined stdout+stderr text. The caller's ctx is honoured too.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// redactCreds strips embedded user:pass pairs from any URL in text,
// so a token that accidentally leaked onto stderr cannot travel on in
// a failure Reason.
func redactCreds(text string) string {
	return urlCredsRe.ReplaceAllString(text, "URL://***@")
}

// tailReason trims the last meaningful ~400 chars of a (redacted)
// message, dropping a leading partial line, so the operator sees the
// actual error rather than the banner.
func tailReason(text string) string {
	text = redactCreds(strings.TrimSpace(text))
	const limit = 400
	if len(text) <= limit {
		return text
	}
	text = text[len(text)-limit:]
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	return strings.TrimSpace(text)
}

// classifyPush maps git's stderr wording to a PushFailureKind. More
// specific signals (non-fast-forward, auth) are checked before the
// generic buckets.
func classifyPush(out string) PushFailureKind {
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "non-fast-forward") ||
		strings.Contains(lower, "fetch first") ||
		strings.Contains(lower, "rejected (non"):
		return PushFailNonFastForward
	case strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "could not read username"):
		return PushFailAuth
	case strings.Contains(lower, "does not appear to be a git repository") ||
		strings.Contains(lower, "no such remote"):
		return PushFailNoRemote
	default:
		return PushFailOther
	}
}

// failedPush assembles a non-success PushResult from a failed git
// invocation.
func failedPush(out, remote, ref string) PushResult {
	return PushResult{
		Remote: remote,
		Ref:    ref,
		Kind:   classifyPush(out),
		Reason: tailReason(out),
	}
}

// remoteExists reports whether dir already has a remote of that name.
func remoteExists(ctx context.Context, dir, remote string) bool {
	out, err := runGit(ctx, dir, "remote", "get-url", remote)
	return err == nil && strings.TrimSpace(out) != ""
}

// headSHA returns the SHA of HEAD in dir ("" when unknown).
func headSHA(ctx context.Context, dir string) string {
	out, err := runGit(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// PushToBranch pushes dir's checkout of `branch` up to
// remote/<branch>, with upstream tracking set via `--set-upstream` so
// a later plain `git push` works. A plain, fast-forward-only push: no
// force flag and no '+' refspec, so a non-fast-forward state on the
// remote is a terminal, reported failure (Kind=non_fast_forward) and
// the remote is left untouched. An unconfigured remote is reported as
// Kind=no_remote before anything is attempted.
func PushToBranch(ctx context.Context, dir, remote, branch string) PushResult {
	if remote == "" {
		remote = "origin"
	}
	if !remoteExists(ctx, dir, remote) {
		return PushResult{
			Remote: remote,
			Ref:    branch,
			Kind:   PushFailNoRemote,
			Reason: fmt.Sprintf("remote %q is not configured in %s (try `git remote add %s <url>`)", remote, dir, remote),
		}
	}
	out, err := runGit(ctx, dir, "push", "--set-upstream", remote, branch+":refs/heads/"+branch)
	if err != nil {
		return failedPush(out, remote, branch)
	}
	sha := headSHA(ctx, dir)
	if strings.Contains(out, "Everything up-to-date") {
		return PushResult{NothingToPush: true, Remote: remote, Ref: branch, SHA: sha}
	}
	return PushResult{Pushed: true, Remote: remote, Ref: branch, SHA: sha}
}

// DefaultBranch resolves the default branch reported by remote for
// the repository in dir, in preference order:
//
//  1. the cached symref refs/remotes/<remote>/HEAD
//     (`git symbolic-ref`) — what a normal clone leaves behind;
//  2. the remote's own answer to `git ls-remote --symref`;
//  3. `main` and then `master`, each accepted only if that ref is
//     actually advertised by the remote (`git ls-remote --refs`) —
//     this package never invents a branch name.
func DefaultBranch(ctx context.Context, dir, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}

	// 1. Cached symref from a previous clone/fetch.
	if out, _ := runGit(ctx, dir, "symbolic-ref", "refs/remotes/"+remote+"/HEAD"); out != "" {
		if name := branchFromSymref(out); name != "" {
			return name, nil
		}
	}

	// 2. Ask the remote directly.
	if out, _ := runGit(ctx, dir, "ls-remote", "--symref", remote, "HEAD"); out != "" {
		if name := branchFromSymref(out); name != "" {
			return name, nil
		}
	}

	// 3. main, then master — trusted only if the remote advertises it.
	if refs, ok := remoteAdvertisedRefs(ctx, dir, remote); ok {
		for _, candidate := range []string{"main", "master"} {
			if refs["refs/heads/"+candidate] {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("cannot determine the default branch of remote %q", remote)
}

// branchFromSymref extracts the branch name from either a
// `symbolic-ref` value (e.g. "refs/heads/main") or an
// `ls-remote --symref` line (e.g. "ref: refs/heads/main\tHEAD").
func branchFromSymref(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "refs/heads/") {
			continue
		}
		if i := strings.Index(line, "ref:"); i >= 0 {
			line = line[i+len("ref:"):]
		}
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "refs/heads/"))
		if t := strings.IndexByte(name, '\t'); t >= 0 {
			name = name[:t]
		}
		name = strings.TrimSpace(name)
		if name != "" {
			return name
		}
	}
	return ""
}

// remoteAdvertisedRefs lists the refs the remote actually advertises
// (keyed by full ref name); ok=false if the listing itself failed.
func remoteAdvertisedRefs(ctx context.Context, dir, remote string) (map[string]bool, bool) {
	out, err := runGit(ctx, dir, "ls-remote", "--refs", remote)
	if err != nil {
		return nil, false
	}
	refs := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			refs[f[1]] = true
		}
	}
	return refs, true
}

// PushToDefaultBranch pushes dir's HEAD onto remote's default branch
// as a plain fast-forward push (no force flag, no '+' refspec, ever).
// If the remote already contains commits HEAD does not, git refuses
// the push and this returns Kind=non_fast_forward with the remote
// untouched — that is the never-force guarantee.
func PushToDefaultBranch(ctx context.Context, dir, remote string) PushResult {
	if remote == "" {
		remote = "origin"
	}
	if !remoteExists(ctx, dir, remote) {
		return PushResult{
			Remote: remote,
			Kind:   PushFailNoRemote,
			Reason: fmt.Sprintf("remote %q is not configured in %s (try `git remote add %s <url>`)", remote, dir, remote),
		}
	}
	def, err := DefaultBranch(ctx, dir, remote)
	if err != nil {
		return PushResult{
			Remote: remote,
			Kind:   PushFailOther,
			Reason: err.Error(),
		}
	}
	sha := headSHA(ctx, dir)
	out, err := runGit(ctx, dir, "push", remote, "HEAD:refs/heads/"+def)
	if err != nil {
		res := failedPush(out, remote, def)
		res.SHA = sha
		return res
	}
	if strings.Contains(out, "Everything up-to-date") {
		return PushResult{NothingToPush: true, Remote: remote, Ref: def, SHA: sha}
	}
	return PushResult{Pushed: true, Remote: remote, Ref: def, SHA: sha}
}

// UnpushedCommits reports how many commits on HEAD are present on no
// ref of remote (`git rev-list --count HEAD --not
// --remotes=<remote>`), plus the current branch name ("" when
// detached). A non-repository directory yields an error; an
// unconfigured remote is silently treated as having no refs, so every
// HEAD commit counts as unpushed.
func UnpushedCommits(ctx context.Context, dir, remote string) (count int, branch string, err error) {
	if remote == "" {
		remote = "origin"
	}
	out, e := runGit(ctx, dir, "rev-list", "--count", "HEAD", "--not", "--remotes="+remote)
	if e != nil {
		return 0, "", fmt.Errorf("count unpushed commits vs %q: %s", remote, tailReason(out))
	}
	count, cerr := strconv.Atoi(strings.TrimSpace(out))
	if cerr != nil {
		return 0, "", fmt.Errorf("parse unpushed count %q: %w", out, cerr)
	}
	if sym, rerr := runGit(ctx, dir, "symbolic-ref", "--short", "HEAD"); rerr == nil {
		branch = strings.TrimSpace(sym)
	}
	return count, branch, nil
}
