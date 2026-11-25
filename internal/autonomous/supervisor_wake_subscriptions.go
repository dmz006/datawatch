// The built-in 'supervisor-wake' subscription set. When
// supervisor_loop.wake.enabled is on, it subscribes to the configured event
// kinds and delivers each one, through the Coalescer and Runner, to the
// supervisor pass and the designated supervisor session. When the wake path is off nothing is registered, and no
// stored subscription is touched.

package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// SupervisorWakeSetName names the built-in wake subscription set.
const SupervisorWakeSetName = "supervisor-wake"

// WakeSubscriber is the part of the Manager the set needs: an in-process
// lifecycle subscription.
type WakeSubscriber interface {
	Subscribe(f LifecycleFilter, fn func(LifecycleEvent)) (cancel func())
}

// SupervisorWakeConfig supplies what the set needs to run.
type SupervisorWakeConfig struct {
	Subscriber WakeSubscriber
	Settings   supervisorloop.WakeSettings
	// Session returns the id of the designated supervisor session a wake may
	// type into; empty means nothing is typed.
	Session func() string
	// Memory keeps what was typed into each session; nil gives the set its own.
	Memory *supervisorloop.WakeSendMemory
	// FallbackInterval reads how long an identical batch is not typed again;
	// nil uses Settings.FallbackIntervalMinutes.
	FallbackInterval func() time.Duration
	// Stalls returns the current priority stall list as
	// supervisorloop.WakeStallEntry values; nil uses the batch's own entries.
	Stalls func() []string
	// SendInput types text into a session (the session sink).
	SendInput func(sessionID, text string) error
	Clock     supervisorloop.WakeClock
	Record    *supervisorloop.WakeRecord
	// Run, when set, is called first for each delivered batch: it is the
	// supervisor loop pass the daemon runs at once instead of waiting for the
	// scheduled check. A wake that ran it counts as delivered even when no
	// supervisor session is designated to type into.
	Run func(supervisorloop.WakeBatch)
}

// SupervisorWakeSet is a registered wake subscription set.
type SupervisorWakeSet struct {
	Name      string
	Kinds     []string
	coalescer *supervisorloop.Coalescer
	runner    *supervisorloop.Runner
	cancels   []func()
	once      sync.Once
}

// RegisterSupervisorWake registers the set. It returns nil when the wake path
// is disabled, or when there is nothing to register with.
func RegisterSupervisorWake(c SupervisorWakeConfig) *SupervisorWakeSet {
	if !c.Settings.Enabled || len(c.Settings.Kinds) == 0 || c.Subscriber == nil || c.SendInput == nil || c.Session == nil {
		return nil
	}
	set := &SupervisorWakeSet{Name: SupervisorWakeSetName, Kinds: append([]string(nil), c.Settings.Kinds...)}
	memory := c.Memory
	if memory == nil {
		memory = supervisorloop.NewWakeSendMemory()
	}
	set.runner = supervisorloop.NewRunner(func(b supervisorloop.WakeBatch) {
		delivered := false
		if c.Run != nil {
			c.Run(b)
			delivered = true
		}
		if sid := c.Session(); sid != "" && c.typeBatch(memory, sid, b) {
			delivered = true
		}
		if delivered && c.Record != nil {
			c.Record.MarkEvent()
		}
	})
	window := time.Duration(c.Settings.CoalesceSeconds) * time.Second
	set.coalescer = supervisorloop.NewCoalescer(c.Clock, window, func(b supervisorloop.WakeBatch) {
		set.runner.Submit(b)
	})
	for _, kind := range set.Kinds {
		cancel := c.Subscriber.Subscribe(LifecycleFilter{Kinds: []string{kind}}, func(ev LifecycleEvent) {
			set.coalescer.Offer(supervisorloop.WakeEvent{
				Kind: ev.Kind, AutomatonID: ev.PRDID, TaskID: ev.TaskID,
				Fingerprint: ev.NewStatus + "|" + ev.Details, At: ev.At,
			})
		})
		set.cancels = append(set.cancels, cancel)
	}
	return set
}

// typeBatch types what is new in b into session sid. It reports whether a
// line was sent.
func (c SupervisorWakeConfig) typeBatch(memory *supervisorloop.WakeSendMemory, sid string, b supervisorloop.WakeBatch) bool {
	interval := time.Duration(c.Settings.FallbackIntervalMinutes) * time.Minute
	if c.FallbackInterval != nil {
		interval = c.FallbackInterval()
	}
	var stalls []string
	if c.Stalls != nil {
		for _, ev := range b.Events {
			if ev.Kind == KindPriorityStall {
				stalls = c.Stalls()
				break
			}
		}
	}
	now := time.Now()
	if c.Clock != nil {
		now = c.Clock.Now()
	}
	events, typed := memory.Select(sid, b.Events, stalls, interval, now)
	if len(events) == 0 || c.SendInput(sid, wakeBatchText(supervisorloop.WakeBatch{Events: events})) != nil {
		return false
	}
	typed()
	return true
}

// Wait blocks until the runner has finished any active or queued run.
func (s *SupervisorWakeSet) Wait() {
	if s != nil {
		s.runner.Wait()
	}
}

// Close removes every subscription the set registered.
func (s *SupervisorWakeSet) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		for _, c := range s.cancels {
			c()
		}
	})
}

// wakeBatchText is the line typed into the supervisor session.
func wakeBatchText(b supervisorloop.WakeBatch) string {
	parts := make([]string, 0, len(b.Events))
	for _, ev := range b.Events {
		parts = append(parts, ev.Line())
	}
	return fmt.Sprintf("supervisor wake: %s", strings.Join(parts, "; "))
}
