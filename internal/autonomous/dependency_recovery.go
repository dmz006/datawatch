// Automatic requeue of dependents that failed behind a dependency.
//
// When a task's dependency fails (or is cancelled), the executor's
// dependency propagation fails the dependent with the exact error
// "dependency <id> failed" or "dependency <id> was cancelled" — unless
// infra_retry_dependents.go additionally recorded the infrastructure chain
// (a FailureReason with Source "dependency-infra"), that text is the
// dependent's only fault. The moment the dependency it failed behind is no
// longer failed or cancelled — it completed, or was reset back to a state
// where it can still complete — such a dependent is requeued, for every
// failure class of the dependency, not just infrastructure:
//
//   - recoverDependencyDependents is the manager-level recovery. It is
//     unconditional: it does not read autonomous.infra_retry.retry_dependents
//     and it also covers dependents that failed behind content or scope
//     failures. The infrastructure-only recoverInfraDependents keeps running
//     in parallel; whichever recovery runs first wins and the other simply
//     finds fewer failed dependents.
//   - The executor calls it at the same step a task reaches completed, and
//     an operator reset of a dependency releases its dependents with it.
//
// A dependent with a FailureReason of its own (content, scope or any other
// source than the dependency-infra link) failed for its own reasons and is
// left alone, as is a dependent with another dependency that is still failed,
// cancelled, blocked or held.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// dependentsRecoveredKind is the Decision kind recoverDependencyDependents
// records, the mirror of infraDependentsRecoveredKind.
const dependentsRecoveredKind = "dependents_recovered"

// dependencyFailedBehind reports whether the failed task's only failure is
// the failure of a task it depends on: its error carries the executor's
// dependency-failure text verbatim, naming one of the task's dependencies as
// a task of this Automaton, and it has no failure reason of its own (none, or
// the infrastructure dependency link from the infra path). It returns the id
// of the dependency it failed behind.
func dependencyFailedBehind(prd *PRD, t *Task) (string, bool) {
	if t.Status != TaskFailed || t.Error == "" || len(t.DependsOn) == 0 {
		return "", false
	}
	if t.FailureReason != nil && t.FailureReason.Source != dependencyInfraSource {
		return "", false
	}
	for _, dep := range t.DependsOn {
		if t.Error != "dependency "+dep+" failed" && t.Error != "dependency "+dep+" was cancelled" {
			continue
		}
		if lookupTask(prd, dep) != nil {
			return dep, true
		}
	}
	return "", false
}

// recoverDependencyDependents resets every task of prd that failed behind a
// task it depends on — for any failure class of that dependency — once every
// dependency of that task can complete again, and returns the ids it reset,
// in dependency order.
//
// Unlike recoverInfraDependents it is unconditional: it does not read
// autonomous.infra_retry.retry_dependents, and it applies to dependents that
// failed behind content, scope or unmarked infrastructure failures as well.
// A reset task goes back to pending with its Error, FailureReason,
// Verification and stale session cleared; its Attempts and InfraRetries stay
// as history. Its containing story is reopened when terminal.
//
// When the Automaton had ended failed or blocked and no task failed, blocked
// or held for review remains, it returns to running, the status reset_task
// restores. One dependents_recovered decision names each root and the tasks
// reset.
func (m *Manager) recoverDependencyDependents(prd *PRD) []string {
	if prd == nil {
		return nil
	}
	type resetEntry struct {
		t   *Task
		old TaskStatus
	}
	var (
		resets    []resetEntry
		rootOrder []string
		byRoot    = map[string][]string{}
		reopened  PRDStatus
		now       = time.Now()
	)
	m.store.WithLock(func() {
		if prd.Status == PRDHeld {
			return // a held Automaton keeps every task as it is
		}
		var all []*Task
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				all = append(all, &prd.Story[si].Tasks[ti])
			}
		}
		order := all
		if ids, err := topoSort(all); err == nil {
			order = order[:0:0]
			for _, id := range ids {
				if t := lookupTask(prd, id); t != nil {
					order = append(order, t)
				}
			}
		}
		for _, t := range order {
			rootID, ok := dependencyFailedBehind(prd, t)
			if !ok {
				continue
			}
			// A dependency that is still failed, cancelled, blocked or held
			// for review cannot fulfil the dependent: it stays failed.
			unmet := false
			for _, dep := range t.DependsOn {
				if d := lookupTask(prd, dep); d != nil && !infraRootRecovered(d.Status) {
					unmet = true
					break
				}
			}
			if unmet {
				continue
			}
			resets = append(resets, resetEntry{t: t, old: t.Status})
			if _, seen := byRoot[rootID]; !seen {
				rootOrder = append(rootOrder, rootID)
			}
			byRoot[rootID] = append(byRoot[rootID], t.ID)
			t.Status = TaskPending
			t.Error = ""
			t.FailureReason = nil
			t.Verification = nil
			t.SessionID = ""
			t.StartedAt = nil
			t.CompletedAt = nil
			t.UpdatedAt = now
			clearInfraRetryCount(t)
			if s := lookupStoryByTaskID(prd, t.ID); s != nil && (s.Status == StoryFailed || s.Status == StoryCompleted) {
				s.Status = StoryPending
			}
		}
		if len(resets) == 0 {
			return
		}
		if prd.Status == PRDFailed || prd.Status == PRDBlocked {
			stuck := false
			for _, t := range all {
				if t.Status == TaskFailed || t.Status == TaskBlocked || t.Status == TaskReviewNeeded {
					stuck = true
					break
				}
			}
			if !stuck {
				reopened = prd.Status
				prd.Status = PRDRunning
				prd.BlockedReason = ""
			}
		}
		parts := make([]string, 0, len(rootOrder))
		for _, rootID := range rootOrder {
			parts = append(parts, fmt.Sprintf("root=%s reset=%s", rootID, strings.Join(byRoot[rootID], ",")))
		}
		note := "dependency recovered; dependents returned to pending: " + strings.Join(parts, "; ")
		if reopened != "" {
			note += fmt.Sprintf("; Automaton reopened from %s", reopened)
		}
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: dependentsRecoveredKind, Actor: "autonomous", Note: note})
	})
	if len(resets) == 0 {
		return nil
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s: save recovered dependents: %v", prd.ID, err)
	}
	dec := lastDecision(prd, dependentsRecoveredKind)
	ids := make([]string, 0, len(resets))
	for _, r := range resets {
		ids = append(ids, r.t.ID)
		m.emitTaskStatus(prd, r.t, r.old, dec, nil)
	}
	if reopened != "" {
		m.emitPRDStatus(prd, reopened, dec, nil)
	}
	return ids
}
