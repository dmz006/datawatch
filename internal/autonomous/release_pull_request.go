// Pull request release push. When an Automaton's release is finished and the
// project has autonomous.push_and_tag on with autonomous.release_path set to
// pull_request, the daemon does not write the default branch itself. It pushes
// the release branch, opens a pull request into the default branch, polls the
// pull request's checks until they settle or the window expires, and merges
// only when every check succeeded. A failed or timed-out check set records the
// outcome and leaves the pull request open, unmerged.
//
// Guardrails: the release branch is pushed with the same plain fast-forward
// helper the direct path uses (git.PushToBranch, which never forces and runs
// the secret scan policy first). The working tree must be clean. The host is
// reached only through the ReleaseHost interface, so the daemon passes the git
// provider and tests pass a fake. No credential is read, printed or stored here.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
)

// Pull request release outcomes recorded on a release push record, in addition
// to the outcomes shared with the direct push path in release_push.go.
const (
	releasePushOutcomeMerged         = "merged"
	releasePushOutcomeChecksFailed   = "checks_failed"
	releasePushOutcomeChecksTimedOut = "checks_timed_out"
)

// Pull request check states reported by ReleaseHost.PullRequestChecks.
const (
	pullRequestChecksSuccess = "success"
	pullRequestChecksFailure = "failure"
)

// ReleaseHost is the subset of the git provider the pull request release path
// uses. git.Provider satisfies it; tests pass a fake. The method signatures
// match git.Provider exactly.
type ReleaseHost interface {
	// OpenPullRequest opens a pull request from head into base and returns its id.
	OpenPullRequest(ctx context.Context, dir, base, head, title, body string) (string, error)
	// PullRequestChecks reports the aggregate check state of pull request id.
	PullRequestChecks(ctx context.Context, dir, id string) (string, error)
	// MergePullRequest merges pull request id.
	MergePullRequest(ctx context.Context, dir, id string) error
}

// pullRequestReleaseOptions describes one pull request release push. Tag must
// already be the formatted tag name (see git.FormatTag).
type pullRequestReleaseOptions struct {
	// Dir is the project working directory holding the release commit.
	Dir string
	// Project is the project name the release record and audit entries use.
	Project string
	// Tag is the release tag the pull request is for. It names the release.
	Tag string
	// Branch is the release branch to push and open the pull request from.
	Branch string
	// Base is the default branch the pull request merges into.
	Base string
	// Title and Body describe the pull request. Empty values default to the tag.
	Title string
	Body  string
	// Remote is the git remote; empty reads config.ShipDefaultRemote.
	Remote string
	// SecretScanMode is git.secret_scan.mode (off, shadow or enforce).
	SecretScanMode string
	// CredentialSource names where the push credential comes from. Empty reads
	// host-login. It is a label only, never the credential itself.
	CredentialSource string
	// PollInterval is the wait between check polls. Zero reads a 30s default.
	PollInterval time.Duration
	// Window is how long checks are polled before the release is treated as
	// timed out. Zero reads a 30m default.
	Window time.Duration
	// Store receives the release push record. A nil store skips recording.
	Store *ReleaseRecordStore
}

// runPullRequestReleasePush runs the pull request release described by opts
// against host and records the outcome on the release record. It returns an
// error for every refusal or failure; the outcome is recorded in either case.
func runPullRequestReleasePush(ctx context.Context, opts pullRequestReleaseOptions, host ReleaseHost) (releasePushResult, error) {
	var res releasePushResult
	if strings.TrimSpace(opts.Dir) == "" {
		return res, fmt.Errorf("pull request release: no project directory")
	}
	if opts.Project == "" || opts.Tag == "" {
		return res, fmt.Errorf("pull request release: project and tag are required")
	}
	if opts.Branch == "" || opts.Base == "" {
		return res, fmt.Errorf("pull request release: release branch and base branch are required")
	}
	if host == nil {
		return res, fmt.Errorf("pull request release: no release host")
	}
	remote := opts.Remote
	if remote == "" {
		remote = config.ShipDefaultRemote
	}
	cred := opts.CredentialSource
	if cred == "" {
		cred = "host-login"
	}
	pollEvery := opts.PollInterval
	if pollEvery <= 0 {
		pollEvery = 30 * time.Second
	}
	window := opts.Window
	if window <= 0 {
		window = 30 * time.Minute
	}
	title := opts.Title
	if title == "" {
		title = "Release " + opts.Tag
	}
	res.Tag, res.Branch, res.ReleasePath = opts.Tag, opts.Branch, config.ReleasePathPullRequest
	releaseRef := "refs/heads/" + opts.Branch
	defRef := "refs/heads/" + opts.Base

	// fail records the outcome, then returns the error to the caller.
	fail := func(outcome string, err error) (releasePushResult, error) {
		res.Outcome = outcome
		if rerr := recordReleasePushOutcome(releasePushOptions{Project: opts.Project, Tag: opts.Tag, ReleasePath: res.ReleasePath, Store: opts.Store}, res, outcome); rerr != nil {
			return res, fmt.Errorf("%w; record release push: %v", err, rerr)
		}
		return res, err
	}

	clean, err := git.IsClean(ctx, opts.Dir)
	if err != nil {
		return fail(releasePushOutcomeFailed, fmt.Errorf("pull request release: check working tree: %w", err))
	}
	if !clean {
		return fail(releasePushOutcomeRefused, fmt.Errorf("pull request release: refusing to run, the working tree is dirty"))
	}
	head, err := git.HeadCommit(ctx, opts.Dir)
	if err != nil {
		return fail(releasePushOutcomeFailed, fmt.Errorf("pull request release: resolve HEAD: %w", err))
	}
	res.Commit = head

	// Push the release branch with the existing fast-forward push helper.
	pr := git.PushToBranch(ctx, opts.Dir, remote, opts.Branch, git.SecretScanPolicy{Mode: opts.SecretScanMode})
	if !pr.Pushed && !pr.NothingToPush {
		recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, releaseRef, remote, cred, releasePushOutcomeFailed)
		return fail(releasePushOutcomeFailed, fmt.Errorf("pull request release: push %s to %s: %s", opts.Branch, remote, pr.Reason))
	}
	recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, releaseRef, remote, cred, releasePushOutcomePushed)
	res.Pushed = true

	id, err := host.OpenPullRequest(ctx, opts.Dir, opts.Base, opts.Branch, title, opts.Body)
	if err != nil {
		return fail(releasePushOutcomeFailed, fmt.Errorf("pull request release: open pull request: %w", err))
	}

	// Poll checks until they settle, the window expires, or ctx ends.
	deadline := time.Now().Add(window)
	state := ""
	for {
		state, err = host.PullRequestChecks(ctx, opts.Dir, id)
		if err != nil {
			return fail(releasePushOutcomeFailed, fmt.Errorf("pull request release: check pull request %s: %w", id, err))
		}
		if state == pullRequestChecksSuccess || state == pullRequestChecksFailure {
			break
		}
		if !time.Now().Before(deadline) {
			return fail(releasePushOutcomeChecksTimedOut, fmt.Errorf("pull request release: checks on pull request %s still %q after %s; not merged", id, state, window))
		}
		timer := time.NewTimer(pollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail(releasePushOutcomeChecksTimedOut, fmt.Errorf("pull request release: stopped waiting on pull request %s: %w", id, ctx.Err()))
		case <-timer.C:
		}
	}
	if state == pullRequestChecksFailure {
		return fail(releasePushOutcomeChecksFailed, fmt.Errorf("pull request release: checks failed on pull request %s; not merged", id))
	}

	// Checks succeeded: merge.
	if err := host.MergePullRequest(ctx, opts.Dir, id); err != nil {
		recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, defRef, remote, cred, releasePushOutcomeFailed)
		return fail(releasePushOutcomeFailed, fmt.Errorf("pull request release: merge pull request %s: %w", id, err))
	}
	recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, defRef, remote, cred, releasePushOutcomeMerged)

	res.Outcome = releasePushOutcomeMerged
	return res, recordReleasePushOutcome(releasePushOptions{Project: opts.Project, Tag: opts.Tag, ReleasePath: res.ReleasePath, Store: opts.Store}, res, releasePushOutcomeMerged)
}
