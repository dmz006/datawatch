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
