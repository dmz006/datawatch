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
package autonomous

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

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
