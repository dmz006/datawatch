// BL33 — adapter that lets the server package consume the plugin
// Registry through the narrow server.PluginsAPI interface (no import
// cycle). Concrete types leak out as `any`; JSON encoding on the
// server side handles it.

package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// API wraps a *Registry for the REST/MCP/CLI layers.
type API struct {
	R *Registry

	mu sync.Mutex
	// openEventsFn reports how many service events are currently open
	// for one plugin (an "open" service event is an active condition on
	// the service event recorder — for example service_down or
	// token_rejected). The service views carry the count as
	// open_events; 0 means none and the field is omitted. Nil means the
	// server did not wire one (open_events is always 0 for then).
	openEventsFn func(plugin string) int
}

// NewAPI is a convenience constructor.
func NewAPI(r *Registry) *API { return &API{R: r} }

// SetOpenEventsCounter wires fn, which reports the number of open
// service events for one plugin (active conditions on the service event
// recorder). It may be called before or after the API is in use and is
// safe to call concurrently with view rendering.
func (a *API) SetOpenEventsCounter(fn func(plugin string) int) {
	a.mu.Lock()
	a.openEventsFn = fn
	a.mu.Unlock()
}

// openEvents reports the open service events for plugin (0 when no
// counter is wired).
func (a *API) openEvents(plugin string) int {
	a.mu.Lock()
	fn := a.openEventsFn
	a.mu.Unlock()
	if fn == nil {
		return 0
	}
	return fn(plugin)
}

// pluginView is the JSON shape of a plugin: the plugin itself plus, for a
// service-kind plugin, a service status object. Non-service plugins
// encode exactly as before.
type pluginView struct {
	*Plugin
	Service any `json:"service,omitempty"`
}

// serviceView is the operator-facing status of a managed service-kind
// plugin. Fields that have no value (no health data yet, service not up,
// never restarted) are omitted rather than sent as placeholders.
type serviceView struct {
	Mode             string       `json:"mode"` // "managed" | "attach_only"
	State            ServiceState `json:"state,omitempty"`
	Address          string       `json:"address,omitempty"`
	Version          string       `json:"version,omitempty"`        // from the health response when it carries one
	UptimeSeconds    *int64       `json:"uptime_seconds,omitempty"` // set while the service is up
	LastHealthCheck  *time.Time   `json:"last_health_check,omitempty"`
	LastHealthStatus string       `json:"last_health_status,omitempty"` // "up" or "down" of the last probe
	LastHealthError  string       `json:"last_health_error,omitempty"`
	PID              int          `json:"pid,omitempty"`
	Restarts         int          `json:"restarts"`
	LastError        string       `json:"last_error,omitempty"`
	// OpenEvents — how many service events are currently open for this
	// plugin (active service-down, token-rejected, enrichment-backlog or
	// model-node conditions). 0 is omitted; the client renders an
	// "N open service events — Events →" link only when it is > 0.
	OpenEvents int `json:"open_events,omitempty"`
}

// attachServiceView is the service object for a plugin datawatch only
// attaches to (never launches): no lifecycle state, PID or restart
// accounting exists for it. Everything is omitted until the first health
// probe has completed.
type attachServiceView struct {
	Mode             string     `json:"mode"`
	Address          string     `json:"address,omitempty"`
	Version          string     `json:"version,omitempty"`
	UptimeSeconds    *int64     `json:"uptime_seconds,omitempty"` // set while the service is up
	LastHealthCheck  *time.Time `json:"last_health_check,omitempty"`
	LastHealthStatus string     `json:"last_health_status,omitempty"`
	LastHealthError  string     `json:"last_health_error,omitempty"`
	// OpenEvents — see serviceView; the same count applies to an
	// attach-only plugin's service (its probe results feed the same
	// recorder).
	OpenEvents int `json:"open_events,omitempty"`
}

// view renders p for the API: a service-kind plugin always carries the
// service object, populated from the supervisor when the plugin is
// managed or from the attach health probe otherwise; other kinds encode
// the manifest as-is.
func (a *API) view(p *Plugin) any {
	if p.Kind != KindService {
		return p
	}
	if st, ok := a.R.ServiceStatus(p.Name); ok {
		var lhc *time.Time
		if !st.LastHealthCheck.IsZero() {
			t := st.LastHealthCheck
			lhc = &t
		}
		return pluginView{Plugin: p, Service: &serviceView{
			Mode:             LifecycleManaged,
			State:            st.State,
			Address:          connAddress(p),
			Version:          st.Version,
			UptimeSeconds:    uptimePtr(runningUptimeSeconds(st)),
			LastHealthCheck:  lhc,
			LastHealthStatus: st.LastHealthStatus,
			LastHealthError:  st.LastError,
			PID:              st.PID,
			Restarts:         st.Restarts,
			LastError:        st.LastError,
			OpenEvents:       a.openEvents(p.Name),
		}}
	}
	sv := &attachServiceView{Mode: a.serviceMode(p), Address: connAddress(p)}
	sv.OpenEvents = a.openEvents(p.Name)
	if rep, ok := a.R.AttachHealth(p.Name); ok {
		if !rep.LastCheck.IsZero() {
			sv.Version = rep.Version
			t := rep.LastCheck
			sv.LastHealthCheck = &t
			sv.LastHealthStatus = string(rep.Status)
			sv.LastHealthError = rep.LastError
		}
		if rep.Status == AttachHealthUp && !rep.UpSince.IsZero() {
			u := int64(time.Since(rep.UpSince).Seconds())
			sv.UptimeSeconds = &u
		}
	}
	return pluginView{Plugin: p, Service: sv}
}

// serviceMode resolves the lifecycle mode a service-kind plugin runs in:
// the operator's choice (managed opt-in) when the author supports both,
// otherwise the single supported mode. A plugin with no lifecycle block
// is attach-only by construction.
func (a *API) serviceMode(p *Plugin) string {
	attach, managed := false, false
	if p.Lifecycle != nil {
		for _, m := range p.Lifecycle.SupportedModes {
			switch m {
			case LifecycleAttachOnly:
				attach = true
			case LifecycleManaged:
				managed = true
			}
		}
		if attach && managed {
			if a.R.IsManaged(p.Name) {
				return LifecycleManaged
			}
			return LifecycleAttachOnly
		}
		if managed {
			return LifecycleManaged
		}
	}
	return LifecycleAttachOnly
}

// connAddress is the plugin's connection address ("" when undeclared).
func connAddress(p *Plugin) string {
	if p.Connection == nil {
		return ""
	}
	return p.Connection.BaseURL
}

// runningUptimeSeconds is the elapsed time since the supervised service
// last came up; ok is false when it is not running or never started.
func runningUptimeSeconds(st ServiceStatus) (int64, bool) {
	if st.StartedAt.IsZero() {
		return 0, false
	}
	switch st.State {
	case StateStarting, StateHealthy, StateUnhealthy, StateRestarting, StateStopping:
		return int64(time.Since(st.StartedAt).Seconds()), true
	}
	return 0, false
}

// uptimePtr renders an uptime result as the view's pointer field (nil =
// not running, so the JSON field is omitted).
func uptimePtr(secs int64, ok bool) *int64 {
	if !ok {
		return nil
	}
	return &secs
}

func (a *API) List() []any {
	src := a.R.List()
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = a.view(p)
	}
	return out
}

func (a *API) Get(name string) (any, bool) {
	p, ok := a.R.Get(name)
	if !ok {
		return nil, false
	}
	return a.view(p), true
}

// Manifest returns the manifest plugin name declares, for surfaces that
// need manifest-level fields rather than a rendered view (e.g. the
// per-plugin token-usage listing). (zero Manifest, false) when unknown.
func (a *API) Manifest(name string) (Manifest, bool) {
	p, ok := a.R.Get(name)
	if !ok {
		return Manifest{}, false
	}
	return p.Manifest, true
}

// Widgets returns the widgets plugin name declares in its manifest (BL403).
func (a *API) Widgets(name string) ([]Widget, bool) {
	p, ok := a.R.Get(name)
	if !ok {
		return nil, false
	}
	return append([]Widget(nil), p.Widgets...), true
}

func (a *API) Reload() error { return a.R.Discover() }

func (a *API) SetEnabled(name string, on bool) bool {
	return a.R.SetEnabled(name, on)
}

func (a *API) SetManaged(name string, on bool) bool {
	return a.R.SetManaged(name, on)
}

func (a *API) Install(sourceDir string) error { return a.R.Install(sourceDir) }

func (a *API) Test(ctx context.Context, name, hook string, payload map[string]any) (any, error) {
	req := Request{Hook: Hook(hook), Payload: payload}
	// Convenience — promote common top-level fields from payload.
	if v, ok := payload["line"].(string); ok {
		req.Line = v
	}
	if v, ok := payload["severity"].(string); ok {
		req.Severity = v
	}
	if v, ok := payload["text"].(string); ok {
		req.Text = v
	}
	if v, ok := payload["session_id"].(string); ok {
		req.Session = v
	}
	resp, err := a.R.Invoke(ctx, name, Hook(hook), req)
	if err != nil {
		return nil, fmt.Errorf("invoke: %w", err)
	}
	return resp, nil
}
