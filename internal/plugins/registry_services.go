// Manifest v3 — Registry-level management of managed service plugins.
//
// The Registry owns one ServiceSupervisor per eligible plugin. A plugin is
// eligible (datawatch may start/stop it) only when all of these hold:
//
//   - Kind == KindService with a Lifecycle block,
//   - the author-declared Lifecycle.SupportedModes contains "managed"
//     (the ceiling — an operator opt-in can never force a mode the author
//     did not declare),
//   - the plugin is Enabled, and
//   - the operator opted it in (plugins.managed / SetManaged).
//
// Nothing is spawned until StartManaged is called, so tests and code paths
// without a daemon never launch processes. Supervisors are always started
// and stopped outside r.mu: their OnEvent callback reads r.mu, and Stop
// waits for the supervision goroutine, which may be emitting an event.
//
// Reuse audit: extends the existing Registry (plugins.Config + the
// Disabled/SetEnabled in-memory toggle pattern) and ServiceSupervisor;
// no new registry or persistence layer.

package plugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// serviceStopMargin is added to a supervisor's StopTimeout when bounding a
// Registry-initiated stop, covering the SIGKILL and reap after the grace
// period.
const serviceStopMargin = 5 * time.Second

// declaresManaged reports whether the plugin's author allows the managed
// lifecycle mode.
func declaresManaged(p *Plugin) bool {
	if p == nil || p.Kind != KindService || p.Lifecycle == nil {
		return false
	}
	for _, m := range p.Lifecycle.SupportedModes {
		if m == LifecycleManaged {
			return true
		}
	}
	return false
}

// isManagedLocked reports whether p is currently eligible for supervision;
// caller holds r.mu.
func (r *Registry) isManagedLocked(p *Plugin) bool {
	return declaresManaged(p) && p.Enabled && r.managed[p.Name]
}

// SetServiceEventFn sets the sink that receives every managed service's
// lifecycle events. fn must not block for long and must not call back into
// methods that start or stop services.
func (r *Registry) SetServiceEventFn(fn func(ServiceEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serviceEventFn = fn
}

// emitServiceEvent forwards a supervisor event to the configured sink.
func (r *Registry) emitServiceEvent(ev ServiceEvent) {
	r.mu.Lock()
	fn := r.serviceEventFn
	r.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// SetManaged changes the runtime operator opt-in for a plugin (in memory,
// like SetEnabled; plugins.managed in config is the persistent source).
// It returns false when the plugin is unknown or its author did not
// declare the managed lifecycle mode — that opt-in is rejected, never
// forced. Opting in starts the service once StartManaged has been called
// (and the plugin is enabled); opting out stops it.
func (r *Registry) SetManaged(name string, on bool) bool {
	r.mu.Lock()
	p, ok := r.byName[name]
	if !ok || !declaresManaged(p) {
		r.mu.Unlock()
		return false
	}
	if r.managed == nil {
		r.managed = map[string]bool{}
	}
	if on {
		r.managed[name] = true
	} else {
		delete(r.managed, name)
	}
	start, stop := r.reconcileServicesLocked(false)
	r.mu.Unlock()
	r.applyServiceChanges(context.Background(), start, stop)
	return true
}

// StartManaged marks the registry as started and launches every eligible
// managed plugin (including ones previously stopped by StopAll). An
// individual failure is logged and does not stop the others; the failed
// supervisor stays registered so ServiceStatus reports its error.
func (r *Registry) StartManaged(ctx context.Context) {
	r.mu.Lock()
	r.started = true
	start, stop := r.reconcileServicesLocked(true)
	r.mu.Unlock()
	r.applyServiceChanges(ctx, start, stop)
}

// StopAll stops every supervised service concurrently, each bounded by its
// own stop timeout, and waits for all of them. Supervisors stay registered
// (state "stopped") and no new service is started until StartManaged is
// called again.
func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.Lock()
	r.started = false
	sups := make([]*ServiceSupervisor, 0, len(r.supervisors))
	for _, s := range r.supervisors {
		sups = append(sups, s)
	}
	r.mu.Unlock()
	return stopSupervisors(ctx, sups)
}

// ServiceStatus returns the supervisor snapshot for a managed plugin, or
// false when no supervisor exists for it.
func (r *Registry) ServiceStatus(name string) (ServiceStatus, bool) {
	r.mu.Lock()
	s, ok := r.supervisors[name]
	r.mu.Unlock()
	if !ok {
		return ServiceStatus{}, false
	}
	return s.Status(), true
}

// reconcileServicesLocked brings the supervisor set in line with plugin
// eligibility; caller holds r.mu. Supervisors of removed or ineligible
// plugins are unregistered and returned in stop. When the registry has
// been started, newly eligible plugins get a supervisor returned in start;
// with restartIdle, existing supervisors that are not running are
// returned in start as well. The caller runs applyServiceChanges after
// releasing r.mu.
func (r *Registry) reconcileServicesLocked(restartIdle bool) (start, stop []*ServiceSupervisor) {
	if r.supervisors == nil {
		r.supervisors = map[string]*ServiceSupervisor{}
	}
	for name, s := range r.supervisors {
		if p, ok := r.byName[name]; ok && r.isManagedLocked(p) {
			continue
		}
		delete(r.supervisors, name)
		stop = append(stop, s)
	}
	if !r.started {
		return start, stop
	}
	for name, p := range r.byName {
		if !r.isManagedLocked(p) {
			continue
		}
		if s, ok := r.supervisors[name]; ok {
			if restartIdle && !s.active() {
				start = append(start, s)
			}
			continue
		}
		s, err := NewServiceSupervisor(p, r.secretsStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[plugins] managed service %s: %v\n", name, err)
			continue
		}
		s.OnEvent = r.emitServiceEvent
		r.supervisors[name] = s
		start = append(start, s)
	}
	return start, stop
}

// applyServiceChanges stops then starts supervisors chosen by
// reconcileServicesLocked. Must be called without r.mu held. A start that
// raced with StopAll or with the plugin becoming ineligible is undone.
func (r *Registry) applyServiceChanges(ctx context.Context, start, stop []*ServiceSupervisor) {
	if err := stopSupervisors(context.Background(), stop); err != nil {
		fmt.Fprintf(os.Stderr, "[plugins] stop managed services: %v\n", err)
	}
	for _, s := range start {
		if err := s.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[plugins] start managed service %s: %v\n", s.Name(), err)
			continue
		}
		r.mu.Lock()
		current := r.started && r.supervisors[s.Name()] == s
		r.mu.Unlock()
		if !current {
			if err := stopSupervisors(context.Background(), []*ServiceSupervisor{s}); err != nil {
				fmt.Fprintf(os.Stderr, "[plugins] stop managed service %s: %v\n", s.Name(), err)
			}
		}
	}
}

// stopSupervisors stops sups concurrently, each within its own stop
// timeout (plus serviceStopMargin), and waits for all of them.
func stopSupervisors(ctx context.Context, sups []*ServiceSupervisor) error {
	if len(sups) == 0 {
		return nil
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, s := range sups {
		wg.Add(1)
		go func(s *ServiceSupervisor) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, s.StopTimeout+serviceStopMargin)
			defer cancel()
			if err := s.Stop(sctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("plugin service %q: %w", s.Name(), err))
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	return errors.Join(errs...)
}
