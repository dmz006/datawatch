package plugins

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
)

// Service event recorder (plugin service health).
//
// The ServiceEventRecorder watches plugin service health from the
// outside: it is fed observations — health probes, HTTP status codes
// seen on probes or widget pulls, enrichment pending counts, and
// model-node reachability — and turns the four named conditions
// (service_down, token_rejected, enrichment_backlog_growing and
// model_node_unreachable) into state transitions.
//
// Each condition raises exactly ONE alert through the alerts store
// (alerts.EmitSystem by default) and ONE audit entry with its action
// name (plugin_service_down, plugin_service_token_rejected,
// plugin_service_enrichment_backlog or
// plugin_service_model_node_unreachable) when it starts, and a
// recovery alert plus a "cleared" audit entry when it ends. It raises
// once per transition, never on every poll. Thresholds are parameters
// on ServiceEventOptions with defaults, and the defaults are named
// variables rather than literals in the transition logic, so the
// settings surface can observe and override them.

// Default thresholds for the service event conditions.
const (
	// DefaultDownFailureThreshold — consecutive failing health probes
	// before the service_down condition raises.
	DefaultDownFailureThreshold = 3
	// DefaultBacklogRisePulls — how many consecutive rises (each at or
	// above the minimum rise) before the enrichment_backlog_growing
	// condition raises.
	DefaultBacklogRisePulls = 3
	// DefaultBacklogMinRise — the minimum rise (in pending items) a pull
	// must show over the previous pull to count as a rise.
	DefaultBacklogMinRise = 1
)

// ServiceEventAlertSink receives one alert per condition transition.
// The default is alerts.EmitSystem.
type ServiceEventAlertSink func(level alerts.Level, title, body string)

// ServiceEventAuditSink receives one audit entry per condition
// transition, raised or cleared. Details include "state" ("raised" or
// "cleared") plus the condition's own figures. A nil sink means audit
// entries are dropped (alerts still go out).
type ServiceEventAuditSink func(action, subject string, details map[string]any)

// ServiceEventOptions are the recorder's sinks and threshold
// parameters. Zero or negative thresholds select the defaults above;
// they are normalised at construction time, never left in the
// transition logic.
type ServiceEventOptions struct {
	Alert ServiceEventAlertSink
	Audit ServiceEventAuditSink

	// DownFailureThreshold — consecutive failing probes before
	// service_down raises. 0 or less uses DefaultDownFailureThreshold.
	DownFailureThreshold int
	// BacklogRisePulls — consecutive rises before the backlog condition
	// raises. 0 or less uses DefaultBacklogRisePulls.
	BacklogRisePulls int
	// BacklogMinRise — minimum rise per pull that counts. 0 or less
	// uses DefaultBacklogMinRise.
	BacklogMinRise int
}

// ServiceEventRecorder is a thread-safe event recorder. One recorder
// may serve every plugin: each condition's state is keyed by plugin
// name (and by plugin + model for the node condition), so plugins
// never interfere with each other.
type ServiceEventRecorder struct {
	alertService   ServiceEventAlertSink
	auditSink      ServiceEventAuditSink
	downThreshold  int
	backlogPulls   int
	backlogMinRise int

	mu sync.Mutex
	// probe failures: plugin -> consecutive failing probes.
	downFails map[string]int
	// service_down: plugin -> condition active.
	downActive map[string]bool
	// token_rejected: plugin -> condition active.
	tokenActive map[string]bool
	// enrichment_backlog_growing: plugin -> observation streak.
	backlogStreaks map[string]backlogStreak
	// model_node_unreachable: "plugin/model" -> condition active.
	nodeActive map[string]bool
}

// backlogStreak is one plugin's enrichment-backlog observation state.
type backlogStreak struct {
	seen   bool // any observation recorded yet
	last   int  // last pending count seen
	rises  int  // consecutive rises at or above minRise
	active bool
}

// NewServiceEventRecorder builds a recorder. With the zero
// ServiceEventOptions it alerts through alerts.EmitSystem, drops audit
// entries, and uses the default thresholds.
func NewServiceEventRecorder(opts ServiceEventOptions) *ServiceEventRecorder {
	alert := opts.Alert
	if alert == nil {
		alert = alerts.EmitSystem
	}
	down := opts.DownFailureThreshold
	if down <= 0 {
		down = DefaultDownFailureThreshold
	}
	pulls := opts.BacklogRisePulls
	if pulls <= 0 {
		pulls = DefaultBacklogRisePulls
	}
	minRise := opts.BacklogMinRise
	if minRise <= 0 {
		minRise = DefaultBacklogMinRise
	}
	return &ServiceEventRecorder{
		alertService:   alert,
		auditSink:      opts.Audit,
		downThreshold:  down,
		backlogPulls:   pulls,
		backlogMinRise: minRise,
		downFails:      map[string]int{},
		downActive:     map[string]bool{},
		tokenActive:    map[string]bool{},
		backlogStreaks: map[string]backlogStreak{},
		nodeActive:     map[string]bool{},
	}
}

// emitTransition writes the one alert (warn when raised, info when
// cleared) and, when configured, the one audit entry for a transition.
// Caller must hold r.mu. details must already carry the condition's
// figures; "state" is set here.
func (r *ServiceEventRecorder) emitTransition(action, subject, raiseTitle, raiseBody, clearTitle, clearBody string, details map[string]any, raised bool) {
	if raised {
		r.alertService(alerts.LevelWarn, raiseTitle, raiseBody)
	} else {
		r.alertService(alerts.LevelInfo, clearTitle, clearBody)
	}
	if r.auditSink == nil {
		return
	}
	d := make(map[string]any, len(details)+1)
	for k, v := range details {
		d[k] = v
	}
	d["state"] = "raised"
	if !raised {
		d["state"] = "cleared"
	}
	r.auditSink(action, subject, d)
}

// RecordHealthProbe feeds one attach-only or managed health probe
// result for plugin. Consecutive failing probes past the threshold
// raise service_down (once); the first passing probe clears it.
func (r *ServiceEventRecorder) RecordHealthProbe(plugin string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	failures := r.downFails[plugin]
	if healthy {
		r.downFails[plugin] = 0
		if !r.downActive[plugin] {
			return
		}
		r.downActive[plugin] = false
		r.emitTransition(audit.ActionPluginServiceDown, plugin,
			"", "",
			fmt.Sprintf("Plugin %s is back up", plugin),
			serviceDownClearBody(plugin, failures),
			map[string]any{"plugin": plugin, "consecutive_failures": failures, "threshold": r.downThreshold},
			false)
		return
	}
	failures++
	r.downFails[plugin] = failures
	if failures < r.downThreshold {
		return
	}
	if r.downActive[plugin] {
		return // already raised: once per transition, never per poll
	}
	r.downActive[plugin] = true
	r.emitTransition(audit.ActionPluginServiceDown, plugin,
		fmt.Sprintf("Plugin %s is down", plugin),
		serviceDownRaiseBody(plugin, failures, r.downThreshold),
		"", "",
		map[string]any{"plugin": plugin, "consecutive_failures": failures, "threshold": r.downThreshold},
		true)
}

// RecordHTTPStatus feeds one HTTP status seen from plugin on a probe
// ("probe") or a widget pull ("widget_pull"). 401 and 403 raise
// token_rejected (once); a successful answer (2xx or 3xx) clears it;
// any other status leaves the condition as it is.
func (r *ServiceEventRecorder) RecordHTTPStatus(plugin, source string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if status == 401 || status == 403 {
		if r.tokenActive[plugin] {
			return
		}
		r.tokenActive[plugin] = true
		r.emitTransition(audit.ActionPluginServiceTokenRejected, plugin,
			fmt.Sprintf("Plugin %s token rejected", plugin),
			tokenRejectedRaiseBody(plugin, source, status),
			"", "",
			map[string]any{"plugin": plugin, "status_code": status, "source": source},
			true)
		return
	}
	if status < 200 || status >= 400 {
		return // neither a rejection nor a successful answer
	}
	if !r.tokenActive[plugin] {
		return
	}
	r.tokenActive[plugin] = false
	r.emitTransition(audit.ActionPluginServiceTokenRejected, plugin,
		"", "",
		fmt.Sprintf("Plugin %s token accepted again", plugin),
		tokenRejectedClearBody(plugin),
		map[string]any{"plugin": plugin, "status_code": status, "source": source},
		false)
}

// RecordBacklogCount feeds one enrichment pending count for plugin.
// The condition raises once N consecutive pulls each rise by at least
// the minimum rise; a pull that drops or fails to rise ends the streak
// and clears the condition. The first observation for a plugin only
// establishes the baseline — it can never raise or clear.
func (r *ServiceEventRecorder) RecordBacklogCount(plugin string, pending int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.backlogStreaks[plugin]
	if !st.seen {
		st.seen = true
		st.last = pending
		r.backlogStreaks[plugin] = st
		return
	}
	if pending > st.last && pending-st.last >= r.backlogMinRise {
		st.rises++
	} else {
		st.rises = 0
	}
	st.last = pending
	if st.active {
		if st.rises == 0 {
			st.active = false
			r.backlogStreaks[plugin] = st
			r.emitTransition(audit.ActionPluginServiceEnrichmentBacklog, plugin,
				"", "",
				fmt.Sprintf("Plugin %s backlog stopped rising", plugin),
				backlogClearBody(plugin, pending),
				backlogDetails(plugin, st.rises, r.backlogPulls, r.backlogMinRise, pending),
				false)
			return
		}
		r.backlogStreaks[plugin] = st // active and still rising: no new event
		return
	}
	if st.rises < r.backlogPulls {
		r.backlogStreaks[plugin] = st
		return
	}
	st.active = true
	r.backlogStreaks[plugin] = st
	r.emitTransition(audit.ActionPluginServiceEnrichmentBacklog, plugin,
		fmt.Sprintf("Plugin %s backlog is growing", plugin),
		backlogRaiseBody(plugin, st.rises, r.backlogMinRise, pending),
		"", "",
		backlogDetails(plugin, st.rises, r.backlogPulls, r.backlogMinRise, pending),
		true)
}

// backlogDetails builds the shared figure set for backlog audit entries.
func backlogDetails(plugin string, rises, pulls, minRise, pending int) map[string]any {
	return map[string]any{
		"plugin":       plugin,
		"streak":       rises,
		"pulls":        pulls,
		"min_rise":     minRise,
		"last_pending": pending,
	}
}

// RecordModelNode feeds one reachability result for the Ollama node
// serving model on plugin. A non-answer raises
// model_node_unreachable (once); the first answer clears it.
func (r *ServiceEventRecorder) RecordModelNode(plugin, model string, reachable bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := plugin + "/" + model
	if reachable {
		if !r.nodeActive[key] {
			return
		}
		r.nodeActive[key] = false
		r.emitTransition(audit.ActionPluginServiceModelNodeUnreachable, key,
			"", "",
			fmt.Sprintf("Plugin %s model node is reachable again", plugin),
			modelNodeClearBody(plugin, model),
			map[string]any{"plugin": plugin, "model": model},
			false)
		return
	}
	if r.nodeActive[key] {
		return
	}
	r.nodeActive[key] = true
	r.emitTransition(audit.ActionPluginServiceModelNodeUnreachable, key,
		fmt.Sprintf("Plugin %s model node unreachable", plugin),
		modelNodeRaiseBody(plugin, model),
		"", "",
		map[string]any{"plugin": plugin, "model": model},
		true)
}

// Active reports which of the four conditions are currently active for
// plugin. When model is non-empty the node condition is reported for
// that model only; otherwise it is reported if ANY of the plugin's
// models is unreachable.
func (r *ServiceEventRecorder) Active(plugin string, model string) (down, token, backlog, node bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	down = r.downActive[plugin]
	token = r.tokenActive[plugin]
	if st, ok := r.backlogStreaks[plugin]; ok {
		backlog = st.active
	}
	prefix := plugin + "/"
	for key := range r.nodeActive {
		if !r.nodeActive[key] {
			continue
		}
		if model == "" {
			if plugin != "" && strings.HasPrefix(key, prefix) {
				node = true
				break
			}
			continue
		}
		if key == prefix+model {
			node = true
			break
		}
	}
	return
}

// Thresholds returns the effective thresholds in force (after
// defaulting), for diagnostics and for the settings surface.
func (r *ServiceEventRecorder) Thresholds() (down int, pulls int, minRise int) {
	return r.downThreshold, r.backlogPulls, r.backlogMinRise
}

// --- alert text -------------------------------------------------------
//
// Alert text is plain operator language: it names the plugin and says
// the next action. No internals, no tracker IDs.

func serviceDownRaiseBody(plugin string, failures, threshold int) string {
	return fmt.Sprintf(
		"The %s service failed %d health checks in a row (the limit is %d) and datawatch is treating it as down. "+
			"Next: confirm the %s service is running and reachable from this machine, then wait for the recovery alert.",
		plugin, failures, threshold, plugin)
}

func serviceDownClearBody(plugin string, failures int) string {
	return fmt.Sprintf(
		"The %s service passed its health check again after %d failed checks. "+
			"Next: nothing to do — the service is back up.",
		plugin, failures)
}

func tokenRejectedRaiseBody(plugin, source string, status int) string {
	return fmt.Sprintf(
		"The %s service answered the %s with HTTP %d (authenticating or authorisation refused). "+
			"Next: check the %s token in Settings and update it if it has expired.",
		plugin, source, status, plugin)
}

func tokenRejectedClearBody(plugin string) string {
	return fmt.Sprintf(
		"The %s service accepted its token again. "+
			"Next: nothing to do — the earlier refusal has cleared.",
		plugin)
}

func backlogRaiseBody(plugin string, rises, minRise, pending int) string {
	return fmt.Sprintf(
		"The %s enrichment backlog has risen %d pulls in a row (by at least %d items per pull) and now holds %d pending. "+
			"Next: open %s in Settings and check its processing rate and queue, or raise its worker count.",
		plugin, rises, minRise, pending, plugin)
}

func backlogClearBody(plugin string, pending int) string {
	return fmt.Sprintf(
		"The %s enrichment backlog stopped rising — the latest pull shows %d pending. "+
			"Next: nothing to do — the backlog condition is lifted.",
		plugin, pending)
}

func modelNodeRaiseBody(plugin, model string) string {
	return fmt.Sprintf(
		"The Ollama node running %s for %s did not answer its reachability check. "+
			"Next: confirm the Ollama service on that node is running and that the model %s is loaded there.",
		model, plugin, model)
}

func modelNodeClearBody(plugin, model string) string {
	return fmt.Sprintf(
		"The Ollama node running %s for %s is answering again. "+
			"Next: nothing to do — the node is reachable.",
		model, plugin)
}
