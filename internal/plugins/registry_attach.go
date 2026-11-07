// Attach-only service health — registry-level probe management.
//
// The attach-only mode (the counterpart of managed supervision in
// supervisor.go) means datawatch never owns the service's lifecycle; it
// can only observe it. To report "is the thing I attached to still
// there, which version, when did it last come up", the Registry runs one
// AttachHealthProbe per attach-eligible plugin and the API surface
// (api.go) reads the probe's snapshot.
//
// Probes are only started once StartAttachProbes has been called (the
// daemon wires it alongside StartManaged) and stopped by
// StopAttachProbes. Enable/disable and managed-mode flips keep the probe
// set in line without a full restart.

package plugins

import (
	"context"
	"fmt"
	"os"
)

// attachProbe pairs one health probe with the controls needed to stop it.
type attachProbe struct {
	probe    *AttachHealthProbe
	cancel   context.CancelFunc
	doneCh   chan struct{} // closed when the probe goroutine returns
	startCtx context.Context
}

// attachEligibleLocked reports whether datawatch should health-probe p:
// kind: service, enabled, not currently managed, attach-only supported
// (or no lifecycle block, which is attach-only by construction) and a
// probeable connection. Caller holds r.mu.
func (r *Registry) attachEligibleLocked(p *Plugin) bool {
	if p == nil || p.Kind != KindService || !p.Enabled {
		return false
	}
	if r.isManagedLocked(p) {
		return false
	}
	if p.Lifecycle != nil {
		attach := false
		for _, m := range p.Lifecycle.SupportedModes {
			if m == LifecycleAttachOnly {
				attach = true
				break
			}
		}
		if !attach {
			return false
		}
	}
	c := p.Connection
	return c != nil && c.BaseURL != "" && c.HealthPath != ""
}

// StartAttachProbes starts a health probe for every attach-eligible
// service plugin and keeps the probe set in line with eligibility until
// StopAttachProbes is called or ctx is cancelled. Nothing runs when no
// plugin is attach-eligible; the call is safe to repeat.
func (r *Registry) StartAttachProbes(ctx context.Context) {
	r.reconcileAttachProbes(ctx)
}

// StopAttachProbes stops every attach health probe and forgets it. Safe
// to call when nothing is running.
func (r *Registry) StopAttachProbes() {
	r.reconcileAttachProbes(nil) //nolint:staticcheck // nil context means stop every probe
}

// AttachHealth returns the probe snapshot for a plugin, or ok=false when
// no attach probe runs for it.
func (r *Registry) AttachHealth(name string) (AttachHealthReport, bool) {
	r.mu.Lock()
	ap, ok := r.attachProbes[name]
	r.mu.Unlock()
	if !ok {
		return AttachHealthReport{}, false
	}
	return ap.probe.LastCheck(), true
}

// syncAttachProbes re-runs the attach probe reconciliation under the
// current probe scope (see StartAttachProbes). It is a no-op until
// StartAttachProbes has been called and is used by SetEnabled/SetManaged
// so mode and enable flips take effect without a full restart.
func (r *Registry) syncAttachProbes() {
	r.mu.Lock()
	parent := r.attachCtx
	r.mu.Unlock()
	if parent == nil {
		return
	}
	r.reconcileAttachProbes(parent)
}

// reconcileAttachProbes brings the attach probe set in line with
// eligibility: probes for plugins that no longer qualify are stopped,
// probes for plugins that newly do are started, the rest are left
// running. A nil parent stops every probe. Probe goroutines run outside
// r.mu.
func (r *Registry) reconcileAttachProbes(parent context.Context) {
	r.mu.Lock()
	if parent == nil {
		stop := make([]*attachProbe, 0, len(r.attachProbes))
		for _, ap := range r.attachProbes {
			stop = append(stop, ap)
		}
		r.attachProbes = map[string]*attachProbe{}
		r.attachCtx, r.attachCancel = nil, nil
		r.mu.Unlock()
		for _, ap := range stop {
			ap.cancel()
			<-ap.doneCh
		}
		return
	}
	if r.attachCancel == nil {
		r.attachCtx, r.attachCancel = context.WithCancel(parent)
	}
	if r.attachProbes == nil {
		r.attachProbes = map[string]*attachProbe{}
	}
	var start, stop []*attachProbe
	for name, p := range r.byName {
		if r.attachEligibleLocked(p) {
			if _, ok := r.attachProbes[name]; ok {
				continue // already probing
			}
			pr, err := NewAttachHealthProbe(p, r.secretsStore)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[plugins] attach probe %s: %v\n", name, err)
				continue
			}
			// The daemon passes the operator's configured cadence
			// (plugins.services.health_interval_seconds) to a registry build; zero
			// keeps attach_health's package default.
			if r.cfg.HealthInterval > 0 {
				pr.Interval = r.cfg.HealthInterval
			}
			// Every completed check feeds the health observer (nil-safe;
			// in the daemon it is the service event recorder).
			pr.OnResult = func(healthy bool, code int) { r.observeHealth(name, healthy, code) }
			apCtx, cancel := context.WithCancel(r.attachCtx)
			ap := &attachProbe{probe: pr, cancel: cancel, doneCh: make(chan struct{})}
			r.attachProbes[name] = ap
			start = append(start, ap)
			ap.startCtx = apCtx
		} else if ap, ok := r.attachProbes[name]; ok {
			delete(r.attachProbes, name)
			stop = append(stop, ap)
		}
	}
	r.mu.Unlock()

	for _, ap := range stop {
		ap.cancel()
		<-ap.doneCh
	}
	for _, ap := range start {
		go func(ap *attachProbe) {
			defer close(ap.doneCh)
			ap.probe.Run(ap.startCtx)
		}(ap)
	}
}
