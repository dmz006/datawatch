// Push-on-complete outcome recording: after the daemon attempts to push
// a completed PRD's work, recordPushOutcome folds the git.PushResult
// into the PRD's persisted Git workflow state so the operator can see,
// from the PRD itself, whether the work reached the remote — and why
// not, when it did not.
//
// Discipline: this function never touches or rolls back the commit. A
// failed push leaves the local branch exactly where it is; the PRD just
// carries PushState=not_pushed plus a human-readable PushWarning and an
// operator alert (level "warn") with the reason as body. A later
// successful push clears the warning and records ref/SHA/timestamp.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// recordPushOutcome folds res into prd.Git's push state, appends the
// matching Decision row, alerts the operator on failure (level "warn",
// title "Automaton <id> not pushed", reason as body), and saves the PRD.
// policy is the effective policy that was used for the attempt, included
// in the failure log line for operator debugging.
func (m *Manager) recordPushOutcome(prd *PRD, res git.PushResult, policy git.PushPolicy) {
	reason := strings.TrimSpace(res.Reason)
	if reason == "" {
		reason = string(res.Kind)
	}
	if res.Pushed || res.NothingToPush {
		now := time.Now()
		prd.Git.PushState = PushStatePushed
		prd.Git.PushedRef = res.Ref
		prd.Git.PushedSHA = res.SHA
		prd.Git.PushedAt = &now
		prd.Git.PushWarning = ""
		prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
			At:   now,
			Kind: "git_pushed",
		}))
	} else {
		now := time.Now()
		prd.Git.PushState = PushStateNotPushed
		prd.Git.PushWarning = "not pushed: " + reason
		prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
			At:   now,
			Kind: "git_not_pushed",
			Note: reason,
		}))
		m.mu.Lock()
		alertFn := m.alertFn
		m.mu.Unlock()
		if alertFn != nil {
			alertFn("warn", fmt.Sprintf("Automaton %s not pushed", prd.ID), reason)
		}
		log.Printf("autonomous: prd %s push-on-complete (%s) failed: %s", prd.ID, policy, reason)
	}
	prd.UpdatedAt = time.Now()
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("autonomous: prd %s saving push state: %v", prd.ID, err)
	}
}
