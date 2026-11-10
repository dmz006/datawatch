// Direct release push. When an Automaton's release is finished and the
// project has autonomous.push_and_tag on with autonomous.release_path set to
// direct_push, the daemon fast-forwards the default branch to the release
// commit, creates the annotated release tag and pushes it. With push_and_tag
// off it only creates the tag locally and records the release as prepared.
//
// Guardrails: the push is always a plain fast-forward (git.PushToDefaultBranch
// and git.PushTag never force), releasePushRefusal is checked before anything
// is pushed to the default branch, and the working tree must be clean. The
// credential is named by a source label only; no token is read, printed or
// stored here.
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

// Outcomes recorded on a release push record.
const (
	releasePushOutcomePushed   = "pushed"
	releasePushOutcomePrepared = "prepared"
	releasePushOutcomeRefused  = "refused"
	releasePushOutcomeFailed   = "failed"
)

// releasePushOptions describes one release push. Tag must already be the
// formatted tag name (see git.FormatTag); Message defaults to "Release <tag>".
type releasePushOptions struct {
	// Dir is the project working directory holding the release commit.
	Dir string
	// Project is the project name the release record and audit entries use.
	Project string
	// Tag is the annotated release tag to create and push.
	Tag string
	// Message is the annotated tag message.
	Message string
	// Remote is the git remote; empty reads config.ShipDefaultRemote.
	Remote string
	// PushAndTag is autonomous.push_and_tag. When false nothing is pushed.
	PushAndTag bool
	// ReleasePath is autonomous.release_path; empty reads direct_push.
	ReleasePath string
	// SecretScanMode is git.secret_scan.mode (off, shadow or enforce).
	SecretScanMode string
	// Unattended is true when the daemon, not an operator, performs the push.
	Unattended bool
	// CredentialSource names where the push credential comes from (for example
	// host-login or environment-variable-name:DATAWATCH_GIT_TOKEN). Empty reads
	// host-login. It is a label only, never the credential itself.
	CredentialSource string
	// Store receives the release push record. A nil store skips recording.
	Store *ReleaseRecordStore
}

// releasePushResult is what one release push did.
type releasePushResult struct {
	Branch      string
	Tag         string
	Commit      string
	ReleasePath string
	TagCreated  bool
	Pushed      bool
	Outcome     string
}

// runDirectReleasePush runs the release push described by opts and records the
// outcome on the release record. It returns an error for every refusal or
// failure; the outcome is recorded in either case.
func runDirectReleasePush(ctx context.Context, opts releasePushOptions) (releasePushResult, error) {
	var res releasePushResult
	if strings.TrimSpace(opts.Dir) == "" {
		return res, fmt.Errorf("release push: no project directory")
	}
	if opts.Project == "" || opts.Tag == "" {
		return res, fmt.Errorf("release push: project and tag are required")
	}
	remote := opts.Remote
	if remote == "" {
		remote = config.ShipDefaultRemote
	}
	cred := opts.CredentialSource
	if cred == "" {
		cred = "host-login"
	}
	rp := opts.ReleasePath
	if rp == "" {
		rp = config.ReleasePathDirectPush
	}
	res.Tag, res.ReleasePath = opts.Tag, rp
	message := opts.Message
	if message == "" {
		message = "Release " + opts.Tag
	}
	if opts.PushAndTag && rp != config.ReleasePathDirectPush {
		return res, fmt.Errorf("release push: release_path %q is not direct_push; this release is published by the pull-request path", rp)
	}

	clean, err := git.IsClean(ctx, opts.Dir)
	if err != nil {
		return res, fmt.Errorf("release push: check working tree: %w", err)
	}
	if !clean {
		return res, fmt.Errorf("release push: refusing to run, the working tree is dirty")
	}
	head, err := git.HeadCommit(ctx, opts.Dir)
	if err != nil {
		return res, fmt.Errorf("release push: resolve HEAD: %w", err)
	}
	res.Commit = head

	// fail records the outcome, then returns the error to the caller.
	fail := func(outcome string, err error) (releasePushResult, error) {
		res.Outcome = outcome
		if rerr := recordReleasePushOutcome(opts, res, outcome); rerr != nil {
			return res, fmt.Errorf("%w; record release push: %v", err, rerr)
		}
		return res, err
	}

	if !opts.PushAndTag {
		created, err := ensureLocalReleaseTag(ctx, opts.Dir, opts.Tag, message, head)
		if err != nil {
			return fail(releasePushOutcomeFailed, err)
		}
		res.TagCreated = created
		recordReleaseGitAudit(audit.ActionReleaseTag, opts.Project, opts.Tag, remote, cred, tagAuditOutcome(created))
		res.Outcome = releasePushOutcomePrepared
		return res, recordReleasePushOutcome(opts, res, releasePushOutcomePrepared)
	}

	def, err := git.DefaultBranch(ctx, opts.Dir, remote)
	if err != nil {
		return fail(releasePushOutcomeFailed, fmt.Errorf("release push: resolve default branch: %w", err))
	}
	branch, err := git.CurrentBranch(ctx, opts.Dir)
	if err != nil || branch == "" {
		return fail(releasePushOutcomeFailed, fmt.Errorf("release push: cannot determine the branch to push (detached HEAD?)"))
	}
	res.Branch = branch
	defRef := "refs/heads/" + def
	if err := releasePushRefusal(opts.SecretScanMode, branch, def, opts.Unattended); err != nil {
		recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, defRef, remote, cred, releasePushOutcomeRefused)
		return fail(releasePushOutcomeRefused, err)
	}
	if branch != def {
		recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, defRef, remote, cred, releasePushOutcomeRefused)
		return fail(releasePushOutcomeRefused, fmt.Errorf("release push: HEAD is on %q, not the default branch %q; only the default branch is pushed", branch, def))
	}

	created, err := ensureLocalReleaseTag(ctx, opts.Dir, opts.Tag, message, head)
	if err != nil {
		return fail(releasePushOutcomeFailed, err)
	}
	res.TagCreated = created
	recordReleaseGitAudit(audit.ActionReleaseTag, opts.Project, opts.Tag, remote, cred, tagAuditOutcome(created))

	pr := git.PushToDefaultBranch(ctx, opts.Dir, remote, git.SecretScanPolicy{Mode: opts.SecretScanMode})
	if !pr.Pushed && !pr.NothingToPush {
		recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, defRef, remote, cred, releasePushOutcomeFailed)
		return fail(releasePushOutcomeFailed, fmt.Errorf("release push: push %s to %s: %s", def, remote, pr.Reason))
	}
	recordReleaseGitAudit(audit.ActionReleasePush, opts.Project, defRef, remote, cred, releasePushOutcomePushed)

	if err := git.PushTag(ctx, opts.Dir, remote, opts.Tag); err != nil {
		recordReleaseGitAudit(audit.ActionReleaseTag, opts.Project, opts.Tag, remote, cred, releasePushOutcomeFailed)
		return fail(releasePushOutcomeFailed, fmt.Errorf("release push: push tag %s: %w", opts.Tag, err))
	}
	recordReleaseGitAudit(audit.ActionReleaseTag, opts.Project, opts.Tag, remote, cred, releasePushOutcomePushed)

	res.Pushed = true
	res.Outcome = releasePushOutcomePushed
	return res, recordReleasePushOutcome(opts, res, releasePushOutcomePushed)
}

// ensureLocalReleaseTag creates the annotated tag at head unless it already
// exists there. It reports whether a tag was created. A tag on a different
// commit is an error: it is never moved.
func ensureLocalReleaseTag(ctx context.Context, dir, tag, message, head string) (bool, error) {
	state, at, err := git.TagStatus(ctx, dir, tag, head)
	if err != nil {
		return false, fmt.Errorf("release push: inspect tag %s: %w", tag, err)
	}
	switch state {
	case git.TagOtherCommit:
		return false, fmt.Errorf("release push: tag %s already exists on a different commit (%s, HEAD is %s)", tag, at, head)
	case git.TagSameCommit:
		return false, nil
	}
	if err := git.CreateAnnotatedTag(ctx, dir, tag, message); err != nil {
		return false, fmt.Errorf("release push: create tag %s: %w", tag, err)
	}
	return true, nil
}

// tagAuditOutcome names a tag operation for the audit log.
func tagAuditOutcome(created bool) string {
	if created {
		return "created"
	}
	return "unchanged"
}

// recordReleasePushOutcome writes the release push record for res. A nil
// store records nothing.
func recordReleasePushOutcome(opts releasePushOptions, res releasePushResult, outcome string) error {
	if opts.Store == nil {
		return nil
	}
	_, err := opts.Store.recordReleasePush(opts.Project, opts.Tag, res.Commit, res.ReleasePath, time.Now(), outcome)
	return err
}
