// Compute-node headroom: on-demand measurement and the stored view.
//
//	POST /api/compute/nodes/{name}/headroom  measure the node now (idle only) and store it
//	GET  /api/compute/nodes/{name}/headroom  the stored measurement: age, freshness, supported cap
//
// Every headroom surface (this REST pair, the MCP tools, the CLI and the
// comm-channel verb) reaches the same service: measureNodeHeadroom for the
// measurement and nodeHeadroomView for the stored view. A measurement an operator
// asks for is run on demand, and only while the node holds no lease. The
// daemon also keeps an idle node's measurement fresh on a timer: its
// verifierHeadroomMeasurer (cmd/datawatch/verifier_headroom_measure.go)
// re-measures an idle node whose configured verifier count is above 1 and
// writes to the same store, so a stored measurement does not age out. The model gate is the operator's model-pull consent
// (autonomous.model_pull_consent), the same allow-list model pulls use.
//
// The one store is the daemon's compute.HeadroomStore (SetHeadroomStore);
// the verifier_slots write gate and the admission gate read it.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// errHeadroomUnknownNode is returned when the named compute node is not in
// the registry.
var errHeadroomUnknownNode = errors.New("unknown compute node")

// errHeadroomNoMeasurement is returned by the stored view when the node has
// never been measured.
var errHeadroomNoMeasurement = errors.New("no headroom measurement")

// errHeadroomNotWired is returned when the registry or the store is not
// wired into the server.
var errHeadroomNotWired = errors.New("headroom measurement is not available")

// SetHeadroomProbe overrides the headroom measurement transport. A nil
// argument restores the default Ollama probe over the observer.
func (s *Server) SetHeadroomProbe(p compute.HeadroomProbe) { s.headroomProbe = p }

// headroomNow is the measurement clock: the injected one when set, else the
// server clock.
func (s *Server) headroomNow() time.Time {
	if s.headroomClock != nil {
		return s.headroomClock()
	}
	return serverNow()
}

// nodeLeaseHeld counts the leases on the node now: workers in the node's
// pool and in its verifier pool, including external and overflow leases.
// The ledger is read under its own lock only; the count is a snapshot.
func (s *Server) nodeLeaseHeld(name string) int {
	if s.capacityLedger == nil {
		return 0
	}
	total := 0
	verifierPool := capacity.VerifierPool(name)
	for _, p := range s.capacityLedger.Snapshot().Pools {
		if p.Name == "node:"+name || p.Name == verifierPool {
			total += p.Held + p.External + p.Overflow
		}
	}
	return total
}

// headroomAllowedModels is the probe's allow-list for node n: the models the
// node serves that the model-pull consent allows, smallest first. It is
// empty when the served models cannot be read or no consent is configured,
// which makes the measurement refuse with ErrModelNotAllowed.
func (s *Server) headroomAllowedModels(ctx context.Context, n *compute.Node) []string {
	if s.servedModels == nil || s.cfg == nil {
		return nil
	}
	sm := s.servedModels.Get(ctx, n, false)
	if !sm.Known {
		return nil
	}
	consent := s.cfg.Autonomous.ModelPullConsent
	var allowed []string
	for _, m := range sm.Models {
		if autonomous.ModelPullConsentCheck(consent, m).Allowed {
			allowed = append(allowed, m)
		}
	}
	return compute.SortSmallestFirst(allowed)
}

// headroomProbeFor returns the probe a measurement uses: the injected one,
// else the Ollama probe reading the node's observer data.
func (s *Server) headroomProbeFor() compute.HeadroomProbe {
	if s.headroomProbe != nil {
		return s.headroomProbe
	}
	observe := func(n compute.Node) compute.NodeObservation {
		o, _ := s.nodeObservation(n.Name)
		return o
	}
	return compute.NewOllamaHeadroomProbe(observe, nil)
}

// measureNodeHeadroom is the one measurement service. It refuses a busy node
// (compute.ErrNodeBusy) and a node whose resident or probe model is not
// pull-allowed (compute.ErrModelNotAllowed), otherwise measures the node and
// stores the result in the daemon's one store.
func (s *Server) measureNodeHeadroom(ctx context.Context, name string) (compute.HeadroomMeasurement, error) {
	if s.computeReg == nil || s.headroomStore == nil {
		return compute.HeadroomMeasurement{}, errHeadroomNotWired
	}
	n, err := s.computeReg.Get(name)
	if err != nil || n == nil {
		return compute.HeadroomMeasurement{}, fmt.Errorf("%w %q", errHeadroomUnknownNode, name)
	}
	m, err := compute.Measure(ctx, compute.MeasureDeps{
		Probe:         s.headroomProbeFor(),
		Now:           s.headroomNow,
		Held:          s.nodeLeaseHeld(name),
		AllowedModels: s.headroomAllowedModels(ctx, n),
		Node:          *n,
	})
	if err != nil {
		return compute.HeadroomMeasurement{}, err
	}
	s.headroomStore.Put(m)
	return m, nil
}

// nodeHeadroomView returns the stored measurement of node as the view every
// surface shows. It reads only; nothing is measured.
func (s *Server) nodeHeadroomView(name string) (map[string]any, error) {
	if s.headroomStore == nil {
		return nil, errHeadroomNotWired
	}
	m, ok := s.headroomStore.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w for node %q", errHeadroomNoMeasurement, name)
	}
	return s.headroomView(m), nil
}

// headroomView renders one measurement: the measurement itself, its age in
// seconds, whether it is fresh under the configured age bound, the configured
// free-memory floor and age bound, and supported_cap. supported_cap is 1 when
// stale or when the floor leaves no room, 0 when the per-request cost is zero
// (unbounded, the ledger's 0 = unlimited convention), and otherwise the
// largest cap CapCoverage accepts.
func (s *Server) headroomView(m compute.HeadroomMeasurement) map[string]any {
	now := s.headroomNow()
	ac := s.headroomAutonomous()
	minFreeMB := ac.EffectiveVerifierMinFreeMB()
	maxAge := ac.EffectiveVerifierHeadroomMaxAge()

	var ageSeconds int64
	if !m.MeasuredAt.IsZero() {
		if age := now.Sub(m.MeasuredAt); age > 0 {
			ageSeconds = int64(age.Seconds())
		}
	}
	fresh := !m.MeasuredAt.IsZero() && now.Sub(m.MeasuredAt) <= maxAge

	supported := 1
	switch {
	case !fresh:
		supported = 1
	case m.PerRequestMB <= 0:
		supported = 0
	default:
		if avail := m.FreeMB - minFreeMB; avail >= m.PerRequestMB {
			supported = 1 + avail/m.PerRequestMB
			if got, refusal := compute.CapCoverage(&m, supported, minFreeMB, now, maxAge); refusal != nil || got != supported {
				supported = 1
			}
		}
	}

	out := map[string]any{
		"node":           m.Node,
		"free_mb":        m.FreeMB,
		"per_request_mb": m.PerRequestMB,
		"resident_model": m.ResidentModel,
		"age_seconds":    ageSeconds,
		"fresh":          fresh,
		"min_free_mb":    minFreeMB,
		"max_age_hours":  int(maxAge / time.Hour),
		"supported_cap":  supported,
	}
	if !m.MeasuredAt.IsZero() {
		out["measured_at"] = m.MeasuredAt.UTC().Format(time.RFC3339)
	}
	return out
}

// headroomAutonomous returns the autonomous config the view reads, or the
// zero value (whose accessors give the defaults) when no config is wired.
func (s *Server) headroomAutonomous() config.AutonomousConfig {
	if s.cfg == nil {
		return config.AutonomousConfig{}
	}
	return s.cfg.Autonomous
}

// handleComputeNodeHeadroomMeasure is POST .../headroom. 404 unknown node,
// 409 node busy (a lease is held), 403 model not pull-allowed, 502 the probe
// failed, 503 not wired.
func (s *Server) handleComputeNodeHeadroomMeasure(w http.ResponseWriter, r *http.Request, name string) {
	m, err := s.measureNodeHeadroom(r.Context(), name)
	if err != nil {
		switch {
		case errors.Is(err, errHeadroomNotWired):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		case errors.Is(err, errHeadroomUnknownNode):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, compute.ErrNodeBusy):
			writeHeadroomError(w, http.StatusConflict, map[string]any{
				"error": err.Error(), "node": name, "held": s.nodeLeaseHeld(name)})
		case errors.Is(err, compute.ErrModelNotAllowed):
			writeHeadroomError(w, http.StatusForbidden, map[string]any{"error": err.Error(), "node": name})
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	s.auditCompute(name, "compute_node_headroom_measure")
	writeJSONOK(w, s.headroomView(m))
}

// handleComputeNodeHeadroomGet is GET .../headroom: the stored view. 404 when
// the node has never been measured, 503 when not wired.
func (s *Server) handleComputeNodeHeadroomGet(w http.ResponseWriter, _ *http.Request, name string) {
	view, err := s.nodeHeadroomView(name)
	if err != nil {
		if errors.Is(err, errHeadroomNoMeasurement) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, view)
}

func writeHeadroomError(w http.ResponseWriter, code int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
