// The built-in 'supervisor-wake' subscription set. When
// supervisor_loop.wake.enabled is on, it subscribes to the configured event
// kinds and delivers each one, through the Coalescer and Runner, to the
// supervisor pass and the designated supervisor session. When the wake path is off nothing is registered, and no
// stored subscription is touched.
//
// A wake line is delivered only once the send into the designated session
// worked. When nothing was typed (no session can take it, or a prompt is on
// screen) the line waits and the set tries again by itself, into whatever
// session is designated then. A send that failed in any other way cannot be
// told apart from a delivered one: it is never typed again and the operator
// gets an alert.

package autonomous

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// A waiting wake is tried again after wakeRetryFirst, then twice as long each
// time, up to wakeRetryMax.
const (
	wakeRetryFirst = 30 * time.Second
	wakeRetryMax   = 5 * time.Minute
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
	// scheduled check; a retry never runs it again. The wake record counts
	// the pass when it has run, and the wake only once its line was typed.
	Run func(supervisorloop.WakeBatch)
	// Alert raises an operator alert; nil uses the Subscriber's alert sink
	// when it has one (the Manager does).
	Alert func(level, title, body string)
}

// alertSource is a subscriber that can raise operator alerts.
type alertSource interface {
	AlertSink() func(level, title, body string) (id string)
}

// The daemon's subscriber is the Manager: its wake alerts depend on this.
var _ alertSource = (*Manager)(nil)

// alert raises an operator alert, resolving the sink at the time of the alert.
func (c SupervisorWakeConfig) alert(level, title, body string) {
	if c.Alert != nil {
		c.Alert(level, title, body)
		return
	}
	if src, ok := c.Subscriber.(alertSource); ok {
		if sink := src.AlertSink(); sink != nil {
			sink(level, title, body)
		}
	}
}

// SupervisorWakeSet is a registered wake subscription set.
type SupervisorWakeSet struct {
	Name      string
	Kinds     []string
	coalescer *supervisorloop.Coalescer
	runner    *supervisorloop.Runner
	cancels   []func()
	once      sync.Once

	cfg    SupervisorWakeConfig
	memory *supervisorloop.WakeSendMemory
	// sendMu lets one send run at a time: a new batch or a retry.
	sendMu sync.Mutex

	retryMu   sync.Mutex
	stopRetry func() bool
	retryWait time.Duration
	closed    bool
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
	set.cfg, set.memory = c, memory
	set.runner = supervisorloop.NewRunner(func(b supervisorloop.WakeBatch) {
		if c.Run != nil {
			c.Run(b)
			if c.Record != nil {
				c.Record.MarkPass()
			}
		}
		set.send(b.Events)
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
	// Wakes a replaced set left waiting.
	if memory.Waiting() > 0 {
		set.scheduleRetry()
	}
	return set
}

// send types what is waiting, including events, into the designated session.
// Whatever was certainly not typed stays waiting and a retry is scheduled.
// events is empty for a retry.
func (s *SupervisorWakeSet) send(events []supervisorloop.WakeEvent) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	pending := s.memory.Hold(events)
	fresh := len(events) > 0
	if s.isClosed() {
		if fresh {
			s.memory.Owe() // the next set types it
		}
		return
	}
	if len(pending) == 0 {
		return
	}
	sid := s.cfg.Session()
	if sid == "" {
		if fresh {
			s.memory.Owe()
		}
		s.wait(pending, "no supervisor session can take input now")
		return
	}
	c := s.cfg
	interval := time.Duration(c.Settings.FallbackIntervalMinutes) * time.Minute
	if c.FallbackInterval != nil {
		interval = c.FallbackInterval()
	}
	var stalls []string
	if c.Stalls != nil {
		for _, ev := range pending {
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
	news, typed := s.memory.Select(sid, pending, stalls, interval, now)
	if len(news) == 0 {
		s.finished(pending) // the session was already told
		return
	}
	line := wakeBatchText(supervisorloop.WakeBatch{Events: news})
	err := c.SendInput(sid, line)
	if session.RefusedBeforeTyping(err) {
		log.Printf("[supervisor-wake] session %s: nothing typed, %d wake(s) waiting: %v", sid, len(pending), err)
		if fresh {
			s.memory.Owe()
		}
		s.wait(pending, "session "+sid+" shows a prompt or its screen cannot be read")
		return
	}
	// Sent, or possibly in the session already: either way it is not typed
	// again.
	typed()
	if err == nil {
		if owed := s.memory.TakeOwed(); fresh || owed {
			s.count()
		}
	}
	s.finished(pending)
	if err != nil {
		log.Printf("[supervisor-wake] session %s: send failed, not typing it again: %v", sid, err)
		c.alert("error", "Supervisor wake: review needed", fmt.Sprintf(
			"review needed: session=%s: %s. datawatch cannot tell that the session got this wake and will not type it again.", sid, line))
	}
}

// count marks one wake on the wake record.
func (s *SupervisorWakeSet) count() {
	if s.cfg.Record != nil {
		s.cfg.Record.MarkEvent()
	}
}

// wait keeps the pending wakes for a retry and reports the wait once.
func (s *SupervisorWakeSet) wait(pending []supervisorloop.WakeEvent, why string) {
	if s.memory.FirstNotice() {
		s.cfg.alert("warn", "Supervisor wake not delivered yet", fmt.Sprintf(
			"%s. Nothing was typed; %d wake(s) wait. datawatch keeps trying, into the designated supervisor session only, while the daemon runs with the wake on: a restart drops waiting wakes without another alert, the oldest are dropped beyond 200, and nothing is tried while the wake is turned off. First: %s. See datawatch supervisor-loop wake.",
			why, len(pending), pending[0].Line()))
	}
	if n := s.memory.Dropped(); n > 0 {
		log.Printf("[supervisor-wake] waiting list is full: %d older wake(s) dropped; the scheduled check still covers them", n)
	}
	s.scheduleRetry()
}

// finished takes wakes off the waiting list and, when none is left, resets the
// retry delay.
func (s *SupervisorWakeSet) finished(events []supervisorloop.WakeEvent) {
	s.memory.Release(events)
	if s.memory.Waiting() > 0 {
		return
	}
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	if s.stopRetry != nil {
		s.stopRetry()
		s.stopRetry = nil
	}
	s.retryWait = 0
}

// scheduleRetry arms one retry timer unless one is armed or the set is closed.
func (s *SupervisorWakeSet) scheduleRetry() {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	if s.closed || s.stopRetry != nil {
		return
	}
	switch {
	case s.retryWait == 0:
		s.retryWait = wakeRetryFirst
	case s.retryWait*2 < wakeRetryMax:
		s.retryWait *= 2
	default:
		s.retryWait = wakeRetryMax
	}
	after := func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	if s.cfg.Clock != nil {
		after = s.cfg.Clock.AfterFunc
	}
	s.stopRetry = after(s.retryWait, s.retry)
}

// retry types the waiting wakes. It never runs the supervisor pass again.
func (s *SupervisorWakeSet) retry() {
	s.retryMu.Lock()
	s.stopRetry = nil
	s.retryMu.Unlock()
	s.send(nil)
}

func (s *SupervisorWakeSet) isClosed() bool {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	return s.closed
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
		s.retryMu.Lock()
		s.closed = true
		if s.stopRetry != nil {
			s.stopRetry()
			s.stopRetry = nil
		}
		s.retryMu.Unlock()
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
