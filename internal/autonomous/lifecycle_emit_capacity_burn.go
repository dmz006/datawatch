// Capacity and budget events: a task that has waited for capacity longer
// than a threshold, and a change of the burn guard's rung. Both kinds are
// opt-in, so delivery is gated by the notify-event resolver exactly like the
// other opt-in kinds and existing subscriptions are untouched.

package autonomous

import (
	"fmt"
	"time"
)

// capacityWaitThreshold is how long a task may wait for capacity before the
// capacity-wait event fires: half of the existing capacity wait timeout
// (autonomous.capacity_wait_timeout_seconds, default defaultCapacityWait),
// so the event arrives while the wait can still end by itself.
func (m *Manager) capacityWaitThreshold() time.Duration {
	return m.capacityWait() / 2
}

// beginCapacityWait records the start of a waiting episode for t. A second
// call within the same episode keeps the first start time.
func (m *Manager) beginCapacityWait(t *Task) {
	if m == nil || t == nil {
		return
	}
	now := m.orphanClock()
	m.condMu.Lock()
	defer m.condMu.Unlock()
	if m.capWaits == nil {
		m.capWaits = map[string]*capacityWaitEpisode{}
	}
	if _, ok := m.capWaits[t.ID]; !ok {
		m.capWaits[t.ID] = &capacityWaitEpisode{prdID: t.PRDID, storyID: t.StoryID, since: now}
	}
}

// endCapacityWait closes the waiting episode of a task; the next wait starts
// a new episode that can emit again.
func (m *Manager) endCapacityWait(taskID string) {
	if m == nil {
		return
	}
	m.condMu.Lock()
	delete(m.capWaits, taskID)
	m.condMu.Unlock()
}

// capacityWaitEpisode is one task's current wait for capacity.
type capacityWaitEpisode struct {
	prdID, storyID string
	since          time.Time
	emitted        bool
}

// sweepCapacityWaits emits the capacity-wait event, once per episode, for
// every task whose wait has passed the threshold.
func (m *Manager) sweepCapacityWaits() {
	if m == nil {
		return
	}
	threshold := m.capacityWaitThreshold()
	now := m.orphanClock()
	var due []LifecycleEvent
	m.condMu.Lock()
	for id, ep := range m.capWaits {
		waited := now.Sub(ep.since)
		if ep.emitted || waited < threshold {
			continue
		}
		ep.emitted = true
		due = append(due, LifecycleEvent{
			Kind:    KindCapacityWaitTooLong,
			Level:   LevelTask,
			PRDID:   ep.prdID,
			StoryID: ep.storyID,
			TaskID:  id,
			Details: fmt.Sprintf("waiting for capacity for %s", waited.Round(time.Second)),
		})
	}
	m.condMu.Unlock()
	for _, ev := range due {
		m.emit(ev)
	}
}

// NoteBurnGuardRung publishes the rung-change event when the rung in force
// went from oldRung to newRung (an empty rung means none applied). The old
// and new rung ride in OldStatus and NewStatus. Equal values emit nothing.
func (m *Manager) NoteBurnGuardRung(oldRung, newRung string) {
	if m == nil || oldRung == newRung {
		return
	}
	m.emit(LifecycleEvent{
		Kind:      KindBurnGuardRungChanged,
		Level:     LevelPRD,
		OldStatus: oldRung,
		NewStatus: newRung,
		Details:   fmt.Sprintf("burn guard rung %q -> %q", oldRung, newRung),
	})
}
