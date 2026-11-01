// Push-on-complete for ad-hoc one-shot sessions (sessions without a PRD
// owner). Complements the PRD push engine in internal/autonomous: after
// commitOnCompletion has committed a one-shot's work, the finished branch
// (or HEAD onto the remote's default branch) is pushed per the daemon's
// live git.push_on_complete / git.push_remote settings.
//
// Push discipline (same guarantees as the PRD path):
//
//   - never returns an error and never alters local commits — a failed
//     push is recorded on the session as PushState=not_pushed plus a
//     visible PushWarning and one operator alert, and the session's
//     completion state is left exactly as it was;
//   - never force-pushes: internal/git performs plain fast-forward-only
//     pushes and has no force option;
//   - one-shots with a PRD owner (PRDID set) are pushed by the autonomous
//     executor, not here — this path is a no-op for them.
package session

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/git"
)

// SetGitPushSettings wires the live push-on-complete reader for one-shot
// completion pushes. The closure is stored, not its result — it is read
// on every push attempt, so a runtime config change applies without a
// restart. Pass nil to disable one-shot pushes entirely (the commit still
// happens; nothing is pushed and no state is recorded).
func (m *Manager) SetGitPushSettings(fn func() (policy, remote string)) {
	m.gitPushSettings = fn
}

// SetPushAlertFn wires the operator-alert sink for one-shot push
// failures. Called as fn("warn", title, body) — the daemon maps the level
// string to the alert store's levels, like the autonomous manager's
// alert sink. Pass nil to suppress alerts (state is still recorded).
func (m *Manager) SetPushAlertFn(fn func(level, title, body string)) {
	m.pushAlertFn = fn
}

// pushAfterCommit pushes a just-committed one-shot's work per the live
// push-on-complete policy. sha is the commit's sha; an empty sha (nothing
// was committed) or a PRD-owned session (the autonomous executor pushes
// PRD work) is a silent no-op.
//
//   - policy never: does nothing, records nothing;
//   - policy branch: pushes the current branch with upstream tracking;
//   - policy default_branch: the one-shot's own validation was its final
//     gate, so a plain fast-forward push of HEAD onto the remote's
//     default branch is safe here.
//
// A success records PushState=pushed plus the pushed SHA; a failure
// records PushState=not_pushed plus PushWarning="not pushed: <reason>"
// and fires ONE warn-level alert. The session is persisted with the
// updated fields either way (the same store.Save path the completion
// commit uses for CommitSHA), and the session's completion state is never
// touched.
func (m *Manager) pushAfterCommit(sess *Session, sha string) {
	if sess == nil || sha == "" || sess.PRDID != "" {
		return
	}
	if m.gitPushSettings == nil {
		return // push-on-complete not wired (e.g. unit test / CLI)
	}
	policy, remote := m.gitPushSettings()
	p, ok := git.ParsePolicy(policy)
	if !ok {
		p = git.PushBranch
	}
	if p == git.PushNever {
		return
	}

	dir := sess.ProjectDir
	ctx := context.Background()
	var res git.PushResult
	switch p {
	case git.PushBranch:
		branch, err := NewProjectGit(dir).CurrentBranch()
		if err != nil || branch == "" || branch == "HEAD" {
			// Detached HEAD with no branch to push.
			res = git.PushResult{Kind: git.PushFailOther, Reason: "detached HEAD: no branch to push"}
		} else {
			log.Printf("session %s: one-shot push-on-complete (branch): pushing %s to remote %s", sess.ID, branch, remote)
			res = git.PushToBranch(ctx, dir, remote, branch)
		}
	case git.PushDefaultBranch:
		log.Printf("session %s: one-shot push-on-complete (default_branch): pushing HEAD to remote %s", sess.ID, remote)
		res = git.PushToDefaultBranch(ctx, dir, remote)
	}

	if res.Pushed || res.NothingToPush {
		sess.PushState = "pushed"
		sess.PushWarning = ""
		if res.SHA != "" {
			sess.PushedSHA = res.SHA
		} else {
			sess.PushedSHA = sha
		}
	} else {
		reason := res.Reason
		if reason == "" {
			reason = string(res.Kind)
		}
		sess.PushState = "not_pushed"
		sess.PushWarning = "not pushed: " + reason
		if m.pushAlertFn != nil {
			m.pushAlertFn("warn", fmt.Sprintf("One-shot %s not pushed", sess.ID), reason)
		}
		log.Printf("session %s: one-shot push-on-complete (%s) failed: %s (commit %s kept locally)", sess.ID, p, reason, sha)
	}
	// Persist exactly like the completion commit's fields: the caller's
	// Save covers it, but save here too so the push state is durable even
	// in code paths that do not re-save after commitOnCompletion.
	if m.store != nil {
		_ = m.store.Save(sess)
	}
}

// PushReport is the outcome of one teardown push attempt, returned so
// callers can log or surface what happened. Count is the number of
// unpushed commits the helper found on HEAD (0 when none, or when the
// count could not be determined); Reason is empty on success and
// explains the outcome otherwise (a clean skip or a failure).
type PushReport struct {
	Attempted bool   // a git push was actually attempted
	Pushed    bool   // the attempt succeeded (pushed, or nothing to push)
	Count     int    // unpushed commits found on HEAD
	Reason    string // why nothing was pushed / why the push failed
}

// pushUnpushedBeforeTeardown saves a session's locally committed-but-
// unpushed work to its remote before the session is torn down (killed,
// deleted, or reaped) — the remote is the durable copy, and a session's
// ProjectDir clone may be reaped right after.
//
// Guarantees (teardown must never be endangered by this helper):
//
//   - it never returns an error and never fails the teardown: the
//     caller proceeds with the kill/delete regardless of the outcome; a
//     failed push is recorded on the session (PushState=not_pushed,
//     PushWarning) plus one warn alert, and the commits are kept in the
//     local checkout;
//   - it is bounded: every git call goes through the push helpers in
//     internal/git, which cap each invocation with their own timeout
//     (GIT_TERMINAL_PROMPT=0), so teardown cannot wedge longer than the
//     helper's bounded timeout;
//   - it never force-pushes: the underlying push is plain
//     fast-forward-only, same as the completion path;
//   - it never pushes the remote's DEFAULT branch at teardown: at kill
//     time the final validation gates are unknown, so when the current
//     branch IS the remote default branch and the policy is
//     default_branch, nothing is pushed — PushState=not_pushed is
//     recorded with a warning that the default-branch push waits for
//     the final gates. Any other branch is pushed as a plain working-
//     branch push (upstream tracking, fast-forward only) under both
//     the branch and the default_branch policies.
//
// Skips (record nothing, alert nothing, leave the session untouched):
// empty ProjectDir, push-on-complete not wired, a project dir that is
// not a git repository, the never policy, or zero unpushed commits
// (which also means work commitOnCompletion/pushAfterCommit already
// pushed is never pushed a second time).
func (m *Manager) pushUnpushedBeforeTeardown(sess *Session) PushReport {
	var rep PushReport
	if sess == nil || strings.TrimSpace(sess.ProjectDir) == "" {
		rep.Reason = "no project dir"
		return rep
	}
	if m.gitPushSettings == nil {
		rep.Reason = "push-on-complete not wired"
		return rep // unit test / CLI without the daemon's settings reader
	}
	policy, remote := m.gitPushSettings()
	p, ok := git.ParsePolicy(policy)
	if !ok {
		p = git.PushBranch
	}
	if p == git.PushNever {
		rep.Reason = "push policy is never"
		return rep
	}

	dir := sess.ProjectDir
	ctx := context.Background() // each git call is separately bounded by the helper
	count, branch, err := git.UnpushedCommits(ctx, dir, remote)
	if err != nil {
		// Not a git repository (or git itself missing): nothing to save.
		rep.Reason = err.Error()
		return rep
	}
	rep.Count = count
	if count == 0 {
		return rep // nothing unpushed — no push attempt at all
	}
	if branch == "" {
		// Detached HEAD: there is no working branch to push, so the
		// commits stay local and the operator is told why.
		rep.Reason = "detached HEAD: no branch to push"
		m.recordTeardownPushFailure(sess, rep.Reason)
		m.saveTeardownPushState(sess)
		return rep
	}

	if p == git.PushDefaultBranch {
		if def, derr := git.DefaultBranch(ctx, dir, remote); derr == nil && def == branch {
			// Teardown is NOT the moment to push the default branch: the
			// final gates are unknown here. Refuse, keep the commits
			// local, and say so on the session.
			rep.Reason = fmt.Sprintf("default branch %s waits for the final gates before a push at teardown", def)
			m.recordTeardownPushRefused(sess, rep.Reason)
			m.saveTeardownPushState(sess)
			log.Printf("session %s: teardown push refused (%s), commits stay local", sess.ID, rep.Reason)
			return rep
		}
		// A side branch under the default_branch policy still gets a
		// plain working-branch push here — never the default branch.
	}

	log.Printf("session %s: teardown push: pushing %d unpushed commit(s) on %s to remote %s", sess.ID, count, branch, remote)
	res := git.PushToBranch(ctx, dir, remote, branch)
	rep.Attempted = true

	if res.Pushed || res.NothingToPush {
		rep.Pushed = true
		sess.PushState = "pushed"
		sess.PushWarning = ""
		sess.PushedSHA = res.SHA
		log.Printf("session %s: teardown push succeeded: %s on %s", sess.ID, res.SHA, branch)
	} else {
		reason := res.Reason
		if reason == "" {
			reason = string(res.Kind)
		}
		rep.Reason = reason
		m.recordTeardownPushFailure(sess, reason)
		log.Printf("session %s: teardown push failed: %s (%d commit(s) kept locally on %s)", sess.ID, reason, count, branch)
	}
	m.saveTeardownPushState(sess)
	return rep
}

// recordTeardownPushFailure records a failed teardown push on the
// session and fires one warn-level operator alert (when an alert sink
// is wired).
func (m *Manager) recordTeardownPushFailure(sess *Session, reason string) {
	sess.PushState = "not_pushed"
	sess.PushWarning = "not pushed: " + reason
	if m.pushAlertFn != nil {
		m.pushAlertFn("warn", fmt.Sprintf("Session %s not pushed before teardown", sess.ID), reason)
	}
}

// recordTeardownPushRefused records a deliberately refused teardown push
// (the default branch at teardown) on the session. This is a policy
// decision, not a failure, so no alert — the warning on the record is
// the operator-visible signal.
func (m *Manager) recordTeardownPushRefused(sess *Session, reason string) {
	sess.PushState = "not_pushed"
	sess.PushWarning = "not pushed: " + reason
}

// saveTeardownPushState persists the push fields, best effort — the
// caller owns the session lifecycle and teardown must not fail on this.
func (m *Manager) saveTeardownPushState(sess *Session) {
	if m.store != nil {
		_ = m.store.Save(sess)
	}
}
