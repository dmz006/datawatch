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
//
// Outgoing check: every push here goes through checkedPush (outgoing.go),
// which checks exactly what would leave, refuses when a check cannot run, and
// sends the checked commit id. The secret scan follows the SecretScanPolicy a
// caller passes (see push_scan.go): enforce mode refuses the push with
// Kind=secret_found and the remote untouched; shadow mode logs and returns the
// (always redacted) findings on the PushResult and lets the push proceed.

package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/proclimit"
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
	// PushFailSecret: the pre-push secret scan (enforce mode) found
	// secret material in the commits about to be pushed, so the push
	// was refused before anything ran. The remote is untouched, and
	// Reason and SecretFindings are redacted (counts, detector names,
	// file:line, fingerprints) — the matched secret is never in them.
	PushFailSecret PushFailureKind = "secret_found"
	// PushFailCheck: the outgoing check refused the push, either because the
	// project's own check did not pass or because a check could not run.
	// The remote is untouched.
	PushFailCheck PushFailureKind = "check_failed"
	// PushFailProtected: a push limited to work branches was asked to move
	// the remote's default branch or push something that is not a branch.
	PushFailProtected PushFailureKind = "default_branch_refused"
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
	// SecretFindings carries the redacted pre-push secret-scan
	// findings when a scan ran and reported: in shadow mode on the
	// success shape (the caller records them for the operator), and on
	// the secret_found refusal in enforce mode. Findings never hold,
	// and never render, the matched secret itself.
	SecretFindings []SecretFinding
	// ProjectCheckRan says the project's own check target ran over the push,
	// and ProjectCheckOutput is the end of its output with secrets removed.
	// SecretScanned says the secret scan ran.
	ProjectCheckRan    bool
	ProjectCheckOutput string
	SecretScanned      bool
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
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	for _, kv := range os.Environ() {
		// A push runs the repository's hooks; they never reach the
		// daemon's own terminal server.
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// runGit executes a bounded `git <args...>` in dir and returns the
// combined stdout+stderr text. The caller's ctx is honoured too.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	cmd, done := Command(ctx, args...)
	defer done()
	cmd.Dir = dir
	cmd.Env = gitEnv()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := proclimit.Run(ctx, cmd)
	var over *proclimit.LimitError
	if errors.As(err, &over) {
		out.WriteString("\n" + over.Why)
	}
	return out.String(), err
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

// PushToBranch pushes the local branch `branch` up to remote/<branch> and
// records it as the branch's upstream so a later plain `git push` works. The
// outgoing check runs first and the checked commit id is what is sent. A
// plain, fast-forward-only push: no force flag and no '+' refspec, so a
// non-fast-forward state on the remote is a terminal, reported failure
// (Kind=non_fast_forward) and the remote is left untouched. An unconfigured
// remote is reported as Kind=no_remote before anything is attempted. See
// SecretScanPolicy for the secret scan modes.
//
// It will push any branch, the default one included; a push made because work
// finished uses PushWorkBranch.
func PushToBranch(ctx context.Context, dir, remote, branch string, scan ...SecretScanPolicy) (res PushResult) {
	return pushBranch(ctx, dir, remote, branch, pushRules{upstream: branch}, scan)
}

// PushWorkBranch is PushToBranch for work that finished: it additionally
// refuses (Kind=default_branch_refused) to move the remote's default branch,
// main or master.
func PushWorkBranch(ctx context.Context, dir, remote, branch string, scan ...SecretScanPolicy) (res PushResult) {
	return pushBranch(ctx, dir, remote, branch, pushRules{upstream: branch, workBranchOnly: true}, scan)
}

// PushFromWorker is PushWorkBranch for a push made inside a container worker:
// the project's check, when there is one, gets no project working tree.
func PushFromWorker(ctx context.Context, dir, remote, branch string, scan ...SecretScanPolicy) PushResult {
	return pushBranch(ctx, dir, remote, branch, pushRules{upstream: branch, workBranchOnly: true, noProjectTree: true}, scan)
}

// CompletionDefaultBranchRefusal is the outcome of the default_branch push
// policy at completion: nothing is pushed.
func CompletionDefaultBranchRefusal(remote string) PushResult {
	return PushResult{Remote: remote, Kind: PushFailProtected, Reason: "completion never moves the remote's default branch, so push_on_complete=default_branch pushes nothing; merge the work instead"}
}

func pushBranch(ctx context.Context, dir, remote, branch string, rules pushRules, scan []SecretScanPolicy) (res PushResult) {
	defer func() { RecordHostOp(HostOpPush, res.Kind == "" && (res.Pushed || res.NothingToPush)) }()
	if remote == "" {
		remote = "origin"
	}
	if err := checkRefName(branch); err != nil {
		return PushResult{Remote: remote, Ref: branch, Kind: PushFailOther, Reason: err.Error()}
	}
	ref := "refs/heads/" + branch
	return checkedPush(ctx, dir, remote, branch, []OutgoingRef{{Src: ref, Dst: ref}}, pushScanPolicy(scan), rules)
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
func PushToDefaultBranch(ctx context.Context, dir, remote string, scan ...SecretScanPolicy) (res PushResult) {
	defer func() { RecordHostOp(HostOpPush, res.Kind == "" && (res.Pushed || res.NothingToPush)) }()
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
	return checkedPush(ctx, dir, remote, def, []OutgoingRef{{Src: "HEAD", Dst: "refs/heads/" + def}}, pushScanPolicy(scan), pushRules{})
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

// ForeignUnpushedCommits counts the commits on HEAD that are on no ref of the
// remote and are also held by another local branch. Those are commits the
// current branch inherited (a task branch cut from a local main that is ahead
// of the remote), not work done on it. Pushing the branch would carry them to
// the remote, so a caller that may push only its own commits refuses when the
// count is not zero. A detached HEAD has no current branch, so every other
// branch is considered. A remote that is not configured yields zero (the push
// itself fails with its own reason); a non-repository yields an error.
func ForeignUnpushedCommits(ctx context.Context, dir, remote string) (int, error) {
	if remote == "" {
		remote = "origin"
	}
	if _, err := runGit(ctx, dir, "remote", "get-url", remote); err != nil {
		// No such remote: a push fails on its own with a clear reason and
		// nothing can be carried anywhere, so there is nothing foreign to refuse.
		return 0, nil
	}
	args := []string{"rev-list", "--count", "HEAD", "--not", "--remotes=" + remote}
	if sym, err := runGit(ctx, dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		if b := strings.TrimSpace(sym); b != "" {
			args = append(args, "--exclude="+b)
		}
	}
	args = append(args, "--branches")
	out, err := runGit(ctx, dir, args...)
	if err != nil {
		return 0, fmt.Errorf("count foreign unpushed commits vs %q: %s", remote, tailReason(out))
	}
	// rev-list HEAD --not <other branches> counts what only HEAD holds; the
	// foreign ones are the unpushed total minus that.
	own, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse own unpushed count %q: %w", out, err)
	}
	total, _, err := UnpushedCommits(ctx, dir, remote)
	if err != nil {
		return 0, err
	}
	if own > total {
		return 0, nil
	}
	return total - own, nil
}
