// The built-in 'supervisor-wake' subscription set. When
// supervisor_loop.wake.enabled is on, it subscribes to the configured event
// kinds and delivers each one, through the Coalescer and Runner, to the
// supervisor session. When the wake path is off nothing is registered, and no
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
	// Session returns the supervisor session id; empty means none is running.
	Session func() string
	// SendInput types text into a session (the session sink).
	SendInput func(sessionID, text string) error
	Clock     supervisorloop.WakeClock
	Record    *supervisorloop.WakeRecord
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
	set.runner = supervisorloop.NewRunner(func(b supervisorloop.WakeBatch) {
		sid := c.Session()
		if sid == "" {
			return
		}
		if err := c.SendInput(sid, wakeBatchText(b)); err == nil && c.Record != nil {
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
		p := ev.Kind
		if ev.AutomatonID != "" {
			p += " automaton=" + ev.AutomatonID
		}
		if ev.TaskID != "" {
			p += " task=" + ev.TaskID
		}
		parts = append(parts, p)
	}
	return fmt.Sprintf("supervisor wake: %s", strings.Join(parts, "; "))
}
