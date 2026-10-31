// Infrastructure retry: the record, the planner and the back-off wait.
//
// When an attempt ends because the machinery around the task failed (see
// ClassifyInfraFailure), the task did not get a fair attempt, so the
// Automaton retries it on a back-off schedule instead of counting the failure
// against it. This file holds the pieces the executor loop uses to do that:
// planInfraRetry decides whether a failure is retried and when,
// recordInfraRetry writes the retry to the task and the Automaton's decision
// log, and waitInfraRetry sleeps out the delay while the task shows why it is
// waiting. The policy itself is config.InfraRetryConfig.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	// maxInfraRetryRecords is how many InfraRetryRecord entries a task keeps.
	maxInfraRetryRecords = 50
	// maxInfraRetryReasonChars bounds the error text kept in one record.
	maxInfraRetryReasonChars = 500
	// infraRetryDecisionKind is the Decision.Kind of a scheduled retry.
	infraRetryDecisionKind = "infra_retry"
	// infraRetryFailureCategory is the failure category of an infra retry.
	infraRetryFailureCategory = "infra"
	// infraRetrySourceExecutor marks a retry scheduled by the executor loop.
	infraRetrySourceExecutor = "executor"
)

// infraRetryConfig returns the infrastructure retry policy in effect.
func (m *Manager) infraRetryConfig() config.InfraRetryConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.InfraRetry.Clone()
}

// infraBackoffDelay returns the delay before retry n (1-based). The last
// configured entry repeats once n runs past the list; an empty list and a
// number below 1 give no delay.
func infraBackoffDelay(cfg config.InfraRetryConfig, n int) time.Duration {
	delays := cfg.EffectiveBackoff()
	if n < 1 || len(delays) == 0 {
		return 0
	}
	if n > len(delays) {
		n = len(delays)
	}
	return time.Duration(delays[n-1]) * time.Second
}

// infraRetryPlan is a decided infrastructure retry.
type infraRetryPlan struct {
	Class    string
	Reason   string
	Number   int
	MaxTries int
	Delay    time.Duration
}

// planInfraRetry decides whether the failure err of task t is retried. It
// returns ok only when infrastructure retry is enabled, err is classified as
// an infrastructure failure, the class is one of the configured categories
// and the task has not used up its tries.
func (m *Manager) planInfraRetry(prd *PRD, t *Task, err error) (infraRetryPlan, bool) {
	cfg := m.infraRetryConfig()
	if !cfg.EffectiveEnabled() {
		return infraRetryPlan{}, false
	}
	class, ok := ClassifyInfraFailure(err)
	if !ok || !cfg.HasCategory(class) {
		return infraRetryPlan{}, false
	}
	var used int
	m.store.WithLock(func() { used = t.InfraRetryCount })
	maxTries := cfg.EffectiveMaxTries()
	if used >= maxTries {
		return infraRetryPlan{}, false
	}
	n := used + 1
	return infraRetryPlan{
		Class:    class,
		Reason:   boundInfraReason(err.Error()),
		Number:   n,
		MaxTries: maxTries,
		Delay:    infraBackoffDelay(cfg, n),
	}, true
}

// boundInfraReason trims the error text and cuts it to
// maxInfraRetryReasonChars runes.
func boundInfraReason(text string) string {
	text = strings.TrimSpace(text)
	r := []rune(text)
	if len(r) > maxInfraRetryReasonChars {
		return string(r[:maxInfraRetryReasonChars]) + "…"
	}
	return text
}

// recordInfraRetry writes the retry the plan describes: it counts it on the
// task, appends an InfraRetryRecord (keeping the newest maxInfraRetryRecords),
// saves the task, then appends an infra_retry Decision to the Automaton and
// saves it. A save failure is logged and never changes the outcome.
func (m *Manager) recordInfraRetry(prd *PRD, t *Task, plan infraRetryPlan) {
	now := time.Now()
	m.store.WithLock(func() {
		t.InfraRetryCount++
		t.InfraRetries = append(t.InfraRetries, InfraRetryRecord{
			Number:       plan.Number,
			Class:        plan.Class,
			Reason:       plan.Reason,
			At:           now,
			DelaySeconds: int(plan.Delay / time.Second),
			NextAt:       now.Add(plan.Delay),
			Source:       infraRetrySourceExecutor,
		})
		if over := len(t.InfraRetries) - maxInfraRetryRecords; over > 0 {
			t.InfraRetries = append([]InfraRetryRecord(nil), t.InfraRetries[over:]...)
		}
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: save infra retry %d: %v", prd.ID, t.ID, plan.Number, err)
	}
	note := fmt.Sprintf("task %q infrastructure failure (%s): retry %d of %d in %s",
		t.Title, plan.Class, plan.Number, plan.MaxTries, plan.Delay)
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: now, Kind: infraRetryDecisionKind, Actor: "autonomous",
			Note: note, FailureCategory: infraRetryFailureCategory,
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: save infra retry decision: %v", prd.ID, t.ID, err)
	}
}

// infraSleep waits d or until ctx is done, returning ctx.Err() in that case.
// Tests replace it so none of them sleeps.
var infraSleep = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitInfraRetry sleeps out the plan's delay. While it waits the task's wait
// reason says so, for example "infrastructure retry 2 of 3 at 14:05:09
// (storage_write)"; the reason is cleared when the wait ends, whichever way.
// It returns the context error when ctx ends the wait early.
func (m *Manager) waitInfraRetry(ctx context.Context, prd *PRD, t *Task, plan infraRetryPlan) error {
	at := time.Now().Add(plan.Delay)
	reason := fmt.Sprintf("infrastructure retry %d of %d at %s (%s)",
		plan.Number, plan.MaxTries, at.Format("15:04:05"), plan.Class)
	m.store.WithLock(func() { t.WaitReason = reason })
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: save infra retry wait: %v", prd.ID, t.ID, err)
	}
	err := infraSleep(ctx, plan.Delay)
	m.store.WithLock(func() { t.WaitReason = "" })
	if serr := m.store.SaveTask(t); serr != nil {
		log.Printf("[autonomous] prd=%s task=%s: clear infra retry wait: %v", prd.ID, t.ID, serr)
	}
	return err
}

// clearInfraRetryCount ends an incident: the consecutive count goes back to
// zero while the records stay as history.
func clearInfraRetryCount(t *Task) {
	if t != nil {
		t.InfraRetryCount = 0
	}
}
