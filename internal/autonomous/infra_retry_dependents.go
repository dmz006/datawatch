// Infrastructure failures that reach dependent tasks.
//
// When a task fails and the tasks that depend on it are failed behind it, a
// dependent whose only fault is that its dependency failed for an
// infrastructure reason is marked so: its FailureReason has category infra,
// Source dependencyInfraSource, and DependsOnTaskID naming the task where the
// chain began. A later reset of that root can then find and reset every
// dependent failed behind it. A dependency that was cancelled, or that failed
// for content or scope, marks nothing, and nothing is marked when
// autonomous.infra_retry.retry_dependents is off.
//
// recoverInfraDependents is the other half: when that root is no longer failed,
// cancelled or held, the dependents marked behind it return to pending, so the
// operator's recipe of resetting the dependency, then every dependent, then
// running is done by the executor.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// infraDependentsRecoveredKind is the Decision kind recoverInfraDependents
// records.
const infraDependentsRecoveredKind = "infra_dependents_recovered"

// dependencyInfraSource is the FailureReason.Source of a task failed because a
// task it depends on failed for an infrastructure reason.
const dependencyInfraSource = "dependency-infra"

// dependencyFailedForInfra reports whether the failed dependency dep failed
// for an infrastructure reason, so that a dependent failed behind it is
// marked. It is true when retry_dependents is on, dep is failed (not
// cancelled), and its FailureReason has category infra and either
//   - Source dependency-infra, a link in a chain: rootID is the root recorded
//     on it and class the class named in its detail; or
//   - a Detail that ClassifyInfraText classifies into a class listed in the
//     configured categories: rootID is dep itself.
//
// A dependency that failed for content or scope, or has no FailureReason, is
// not infra. dep is read under the store lock.
func (m *Manager) dependencyFailedForInfra(dep *Task) (rootID, class string, ok bool) {
	if dep == nil || !m.infraRetryConfig().EffectiveRetryDependents() {
		return "", "", false
	}
	var status TaskStatus
	var reason FailureReason
	var has bool
	m.store.WithLock(func() {
		status = dep.Status
		if dep.FailureReason != nil {
			reason, has = *dep.FailureReason, true
		}
	})
	if !has || status != TaskFailed || reason.Category != infraRetryFailureCategory {
		return "", "", false
	}
	if reason.Source == dependencyInfraSource {
		class, ok = dependencyInfraClass(reason.Detail)
		if !ok {
			return "", "", false
		}
		rootID = reason.DependsOnTaskID
		if rootID == "" {
			rootID = dep.ID
		}
		return rootID, class, true
	}
	class, ok = ClassifyInfraText(reason.Detail)
	if !ok || !m.infraRetryConfig().HasCategory(class) {
		return "", "", false
	}
	return dep.ID, class, true
}

// infraDependentReason builds the FailureReason for a dependent of the failed
// task dep, or nil when dep did not fail for an infrastructure reason and the
// dependent keeps today's behaviour. The Detail names the dependency, the
// class and the original cause.
func (m *Manager) infraDependentReason(dep *Task) *FailureReason {
	rootID, class, ok := m.dependencyFailedForInfra(dep)
	if !ok {
		return nil
	}
	var cause string
	m.store.WithLock(func() { cause = dependencyInfraCause(dep.FailureReason) })
	return &FailureReason{
		Category:        infraRetryFailureCategory,
		Source:          dependencyInfraSource,
		Detail:          fmt.Sprintf("dependency %s failed for an infrastructure reason (%s): %s", dep.ID, class, cause),
		DependsOnTaskID: rootID,
		Retryable:       true,
		OccurredAt:      time.Now(),
	}
}

// dependencyInfraCause returns the original cause carried by a dependency's
// FailureReason: its Detail, or for a dependency-infra link the cause after
// the class, so a chain does not nest the same sentence.
func dependencyInfraCause(r *FailureReason) string {
	if r == nil {
		return ""
	}
	if r.Source == dependencyInfraSource {
		if _, rest, found := strings.Cut(r.Detail, "): "); found {
			return rest
		}
	}
	return boundInfraReason(r.Detail)
}

// dependencyInfraClass reads the class out of the Detail of a dependency-infra
// FailureReason, which names it in parentheses after "infrastructure reason".
func dependencyInfraClass(detail string) (string, bool) {
	for _, c := range config.InfraRetryAllClasses() {
		if strings.Contains(detail, "infrastructure reason ("+c+")") {
			return c, true
		}
	}
	return "", false
}

// infraRootRecovered reports whether a dependency-infra root in status s has
// recovered: it completed, or it is back in a state where it can still
// complete. A root that is failed, cancelled, blocked or held for review has
// not.
func infraRootRecovered(s TaskStatus) bool {
	switch s {
	case TaskFailed, TaskCancelled, TaskBlocked, TaskReviewNeeded:
		return false
	}
	return true
}

// recoverInfraDependents resets every task of prd that failed behind a
// dependency (FailureReason.Source dependency-infra) whose root task has
// recovered, and returns the ids it reset, in dependency order. Nothing runs
// unless autonomous.infra_retry.retry_dependents is on.
//
// A reset task goes back to pending with its Error, FailureReason,
// Verification and consecutive InfraRetryCount cleared; its Attempts and
// InfraRetries stay as history. The tasks are processed in dependency order, so
// a chain recovers in one pass. A task failed for any other reason, one whose
// root is still failed, cancelled or held, and one with another dependency
// that is still unmet, is left alone.
//
// When the Automaton had ended failed or blocked and no task failed, blocked or
// held remains, it returns to running, the status reset_task restores. The
// caller decides whether a run must be started for it. One
// infra_dependents_recovered decision names each root and the tasks reset.
func (m *Manager) recoverInfraDependents(prd *PRD) []string {
	if prd == nil || !m.infraRetryConfig().EffectiveRetryDependents() {
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
			if t.Status != TaskFailed || t.FailureReason == nil || t.FailureReason.Source != dependencyInfraSource {
				continue
			}
			rootID := t.FailureReason.DependsOnTaskID
			if root := lookupTask(prd, rootID); root == nil || !infraRootRecovered(root.Status) {
				continue
			}
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
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: infraDependentsRecoveredKind, Actor: "autonomous", Note: note})
	})
	if len(resets) == 0 {
		return nil
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s: save recovered infra dependents: %v", prd.ID, err)
	}
	dec := lastDecision(prd, infraDependentsRecoveredKind)
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
