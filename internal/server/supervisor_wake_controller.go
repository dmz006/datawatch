// The daemon's owner of the built-in 'supervisor-wake' subscription set.
//
// supervisor_loop.wake.enabled can be switched at runtime (PUT /api/config, the
// CLI, MCP config_set or the comm verb all write the same setting). The
// controller reads the live settings on Reconcile and keeps the registered set
// in step: it registers the set when the wake turns on, replaces it when the
// kinds or the quiet window change, and removes it when the wake turns off. The
// daemon calls Reconcile once at start-up and then on a short timer (Start).
//
// What a wake does: each coalesced batch of subscribed events runs one
// supervisor loop pass at once (Run) and, when a supervisor session is running,
// types a one-line summary into it. The scheduled check in the coordinator's
// idle tick stays as the fallback at supervisor_loop.wake.fallback_interval_minutes.

package server

import (
	"context"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// DefaultSupervisorWakeReconcile is how often Start re-reads the settings.
const DefaultSupervisorWakeReconcile = 5 * time.Second

// SupervisorWakeControllerConfig is what the controller needs; every field is an
// injected seam so a test can drive it with a fake event source and a fake
// clock.
type SupervisorWakeControllerConfig struct {
	// Subscriber is the lifecycle event source (the autonomous Manager).
	Subscriber autonomous.WakeSubscriber
	// Settings reads the live supervisor_loop settings.
	Settings func() supervisorloop.Settings
	// Run runs one supervisor loop pass for a delivered batch.
	Run func(supervisorloop.WakeBatch)
	// Session returns the running supervisor session id, or "".
	Session func() string
	// SendInput types text into a session.
	SendInput func(sessionID, text string) error
	Clock     supervisorloop.WakeClock
	Record    *supervisorloop.WakeRecord
}

// SupervisorWakeController keeps the wake subscription set in step with the
// live settings.
type SupervisorWakeController struct {
	cfg SupervisorWakeControllerConfig

	mu      sync.Mutex
	set     *autonomous.SupervisorWakeSet
	applied supervisorloop.WakeSettings
	closed  bool
}

// NewSupervisorWakeController builds the controller and runs one Reconcile.
func NewSupervisorWakeController(cfg SupervisorWakeControllerConfig) *SupervisorWakeController {
	c := &SupervisorWakeController{cfg: cfg}
	c.Reconcile()
	return c
}

func sameWake(a, b supervisorloop.WakeSettings) bool {
	if a.Enabled != b.Enabled || a.CoalesceSeconds != b.CoalesceSeconds || len(a.Kinds) != len(b.Kinds) {
		return false
	}
	for i := range a.Kinds {
		if a.Kinds[i] != b.Kinds[i] {
			return false
		}
	}
	return true
}

// Reconcile registers, replaces or removes the set so it matches the live
// settings. It reports whether a set is registered afterwards.
func (c *SupervisorWakeController) Reconcile() bool {
	if c == nil {
		return false
	}
	var want supervisorloop.WakeSettings
	if c.cfg.Settings != nil {
		want = c.cfg.Settings().Wake
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	if c.set != nil && sameWake(c.applied, want) {
		return true
	}
	if c.set == nil && !want.Enabled {
		return false
	}
	if c.set != nil {
		c.set.Close()
		c.set = nil
	}
	c.applied = want
	if !want.Enabled {
		return false
	}
	c.set = autonomous.RegisterSupervisorWake(autonomous.SupervisorWakeConfig{
		Subscriber: c.cfg.Subscriber, Settings: want, Session: c.cfg.Session,
		SendInput: c.cfg.SendInput, Clock: c.cfg.Clock, Record: c.cfg.Record, Run: c.cfg.Run,
	})
	return c.set != nil
}

// Active reports whether the wake set is registered.
func (c *SupervisorWakeController) Active() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.set != nil
}

// Wait blocks until any wake run in flight has finished.
func (c *SupervisorWakeController) Wait() {
	if c == nil {
		return
	}
	c.mu.Lock()
	set := c.set
	c.mu.Unlock()
	set.Wait()
}

// Start re-reads the settings every interval until ctx ends. A zero interval
// is DefaultSupervisorWakeReconcile.
func (c *SupervisorWakeController) Start(ctx context.Context, every time.Duration) {
	if c == nil {
		return
	}
	if every <= 0 {
		every = DefaultSupervisorWakeReconcile
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.Reconcile()
			}
		}
	}()
}

// Close removes the set; later Reconcile calls register nothing.
func (c *SupervisorWakeController) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.set != nil {
		c.set.Close()
		c.set = nil
	}
}
