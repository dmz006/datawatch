// Compute-node headroom measurement surface.
//
//	POST   /api/compute/nodes/{name}/headroom  measure the node's headroom while idle and store it
//	GET    /api/compute/nodes/{name}/headroom  the last measurement: its age, freshness, and the cap it supports
//
// A measurement is only valid on an idle node: any worker holding a lease
// in the node's node:<name> pool (the same held view /api/capacity shows)
// refuses it with 409. Only the pull-allowed models take part — the caller
// keeps them smallest first — and a measurement is refused with 403 when
// the allow list is empty or the node's resident model is not on it.
//
// The probe is a compute.HeadroomProbe. The default implementation reads
// the node's observer data through the same helper the capacity sync uses
// (falling back to system memory when the node reports no GPUs), and
// issues one small request at the node's Ollama endpoint against the
// smallest allowed model to estimate the per-request footprint. Tests
// inject a fake probe and a fixed clock.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// SetHeadroomProbe overrides the headroom measurement transport. A nil
// argument restores the daemon's default probe (observer read plus the
// Ollama probe request).
func (s *Server) SetHeadroomProbe(p compute.HeadroomProbe) { s.headroomProbe = p }

// SetHeadroomAllowedModels sets the pull-allowed models for headroom
// measurement. The caller keeps them smallest first: Measure always uses
// the first as the probe request's model. An empty list refuses every
// measurement (403).
func (s *Server) SetHeadroomAllowedModels(models []string) {
	list := make([]string, 0, len(models))
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			list = append(list, m)
		}
	}
	s.headroomAllowedModels = list
}

// SetHeadroomClock fixes the measurement clock for deterministic tests.
// A nil argument restores time.Now.
func (s *Server) SetHeadroomClock(fn func() time.Time) { s.headroomClock = fn }

func (s *Server) headroomNow() time.Time {
	if s.headroomClock != nil {
		return s.headroomClock()
	}
	return time.Now()
}

// nodeHeldCount is the number of workers currently holding leases in the
// node's pool: the same pool:<node> held figure /api/capacity reports,
// including overflow leases, which also occupy the node.
func (s *Server) nodeHeldCount(name string) int {
	if s.capacityLedger == nil {
		return 0
	}
	for _, p := range s.capacityLedger.Snapshot().Pools {
		if p.Name == "node:"+name {
			return p.Held + p.Overflow
		}
	}
	return 0
}

// handleComputeNodeHeadroomMeasure runs the idle measurement and stores
// the result. 409 = the node is busy, 403 = the model gate, 502 = the
// probe failed. The stored measurement is what the verifier_slots write
// gate and the GET endpoint read.
func (s *Server) handleComputeNodeHeadroomMeasure(w http.ResponseWriter, r *http.Request, name string) {
	if s.computeReg == nil {
		http.Error(w, "compute registry disabled", http.StatusServiceUnavailable)
		return
	}
	if s.verifierHeadroom == nil {
		http.Error(w, "headroom store not wired", http.StatusServiceUnavailable)
		return
	}
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	probe := s.headroomProbe
	if probe == nil {
		probe = &serverHeadroomProbe{s: s}
	}
	deps := compute.MeasureDeps{
		Probe:         probe,
		Now:           s.headroomNow,
		Held:          s.nodeHeldCount(name),
		AllowedModels: s.headroomAllowedModels,
		Node:          *n,
	}
	m, err := compute.Measure(r.Context(), deps)
	if err != nil {
		switch {
		case errors.Is(err, compute.ErrNodeBusy):
			writeHeadroomError(w, http.StatusConflict, map[string]any{"error": err.Error(), "node": name, "held": s.nodeHeldCount(name)})
		case errors.Is(err, compute.ErrModelNotAllowed):
			writeHeadroomError(w, http.StatusForbidden, map[string]any{"error": err.Error(), "node": name})
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	s.verifierHeadroom.Put(m)
	s.auditCompute(name, "compute_node_headroom_measure")
	writeJSONOK(w, s.headroomResponse(m))
}

// handleComputeNodeHeadroomGet returns the last stored measurement for
// the node with its age, whether it is still fresh under the autonomous
// config's headroom freshness bound, and the verifier cap it supports.
// 404 when the node has never been measured.
func (s *Server) handleComputeNodeHeadroomGet(w http.ResponseWriter, r *http.Request, name string) {
	if s.verifierHeadroom == nil {
		http.Error(w, "headroom store not wired", http.StatusServiceUnavailable)
		return
	}
	m, ok := s.verifierHeadroom.Get(name)
	if !ok {
		http.Error(w, "no headroom measurement for node "+name, http.StatusNotFound)
		return
	}
	writeJSONOK(w, s.headroomResponse(m))
}

// headroomResponse is the GET/measurement view of one HeadroomMeasurement:
// the measurement itself plus age_seconds, fresh (still inside the
// autonomous config's headroom-freshness bound), and supported_cap — the
// verifier cap this measurement covers. supported_cap is 1 while the
// measurement is stale (the gate's fallback), 0 when unbounded (the
// per-request cost measured zero, the ledger's 0 = unlimited convention),
// and otherwise 1 + the free MB beyond the floor divided by the MB each
// request costs.
func (s *Server) headroomResponse(m compute.HeadroomMeasurement) map[string]any {
	now := s.headroomNow()
	minFreeMB := 0
	maxAgeHours := 0
	if s.cfg != nil {
		minFreeMB = s.cfg.Autonomous.VerifierMinFreeMemMB
		maxAgeHours = s.cfg.Autonomous.VerifierHeadroomMaxAge()
	}
	var ageSeconds int64
	if !m.MeasuredAt.IsZero() {
		ageSeconds = int64(now.Sub(m.MeasuredAt).Seconds())
		if ageSeconds < 0 {
			ageSeconds = 0
		}
	}
	fresh := !m.MeasuredAt.IsZero() && maxAgeHours > 0 && now.Sub(m.MeasuredAt) <= time.Duration(maxAgeHours)*time.Hour
	supported := 1
	if fresh {
		if m.PerRequestMB <= 0 {
			supported = 0 // unbounded: a zero-cost request admits any cap
		} else if avail := m.FreeMB - minFreeMB; avail >= 0 {
			supported = 1 + avail/m.PerRequestMB
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
		"max_age_hours":  maxAgeHours,
		"supported_cap":  supported,
	}
	if !m.MeasuredAt.IsZero() {
		out["measured_at"] = m.MeasuredAt.UTC().Format(time.RFC3339)
	}
	return out
}

func writeHeadroomError(w http.ResponseWriter, code int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// nodeObservation returns the observer figures a compute node's slot count
// reads, from the node detail the observer serves: each GPU's memory, the
// VRAM of every model the node has loaded (the "ollama_model:" envelopes),
// and the busiest GPU's utilisation. ok is false when the detail could not
// be read; a node whose detail has no GPUs returns ok with an empty GPU
// list. HTTPServer.NodeObservation delegates here so the capacity sync and
// the headroom probe share one reader.
func (s *Server) nodeObservation(name string) (obs compute.NodeObservation, ok bool) {
	if s.computeReg == nil {
		return obs, false
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/compute/nodes/"+name+"/detail", nil)
	s.serveComputeNodeDetail(rec, req, name)
	if rec.Code != http.StatusOK {
		return obs, false
	}
	var d struct {
		GPU []struct {
			UtilPct       float64 `json:"util_pct"`
			MemUsedBytes  uint64  `json:"mem_used_bytes"`
			MemTotalBytes uint64  `json:"mem_total_bytes"`
		} `json:"gpu"`
		Mem struct {
			UsedBytes  uint64 `json:"used_bytes"`
			TotalBytes uint64 `json:"total_bytes"`
		} `json:"mem"`
		Envelopes []struct {
			ID          string `json:"id"`
			GPUMemBytes uint64 `json:"gpu_mem_bytes"`
			RSSBytes    uint64 `json:"rss_bytes"`
		} `json:"envelopes"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &d) != nil {
		return obs, false
	}
	for _, g := range d.GPU {
		obs.GPUs = append(obs.GPUs, compute.GPUMemory{TotalBytes: g.MemTotalBytes, UsedBytes: g.MemUsedBytes})
		obs.MaxUtilPct = max(obs.MaxUtilPct, g.UtilPct)
	}
	obs.SystemTotalBytes = d.Mem.TotalBytes
	obs.SystemUsedBytes = d.Mem.UsedBytes
	for _, e := range d.Envelopes {
		if !strings.HasPrefix(e.ID, "ollama_model:") {
			continue
		}
		if e.GPUMemBytes > 0 {
			obs.LoadedModelBytes = append(obs.LoadedModelBytes, e.GPUMemBytes)
		}
		if e.RSSBytes > 0 {
			obs.CPUModelBytes = append(obs.CPUModelBytes, e.RSSBytes)
		}
	}
	return obs, true
}

// serverHeadroomProbe is the daemon's default compute.HeadroomProbe.
//
// ReadFreeMB reads the node's observer data with the same helper the
// capacity sync uses (s.nodeObservation): free video memory summed over
// the node's GPUs, falling back to free system memory when the node
// reports no GPUs. The node's resident model is its pinned model.
//
// RequestMemoryMB issues one small chat request at the node's Ollama
// endpoint (the same inline-client, self-signed-remote pattern
// compute.probeHTTP uses for node HTTP) and reports the drop in free
// memory the request cost. It is an estimate: the observer read is a
// pull, not a live counter, so a model that was already loaded before the
// request reports a small or zero cost.
type serverHeadroomProbe struct{ s *Server }

var _ compute.HeadroomProbe = (*serverHeadroomProbe)(nil)

func (p *serverHeadroomProbe) ReadFreeMB(_ context.Context, node compute.Node) (int, string, error) {
	obs, ok := p.s.nodeObservation(node.Name)
	if !ok {
		return 0, "", fmt.Errorf("read free memory: node observation unavailable for %q", node.Name)
	}
	if len(obs.GPUs) > 0 {
		var freeBytes int64
		for _, g := range obs.GPUs {
			if d := int64(g.TotalBytes) - int64(g.UsedBytes); d > 0 {
				freeBytes += d
			}
		}
		return int(freeBytes / 1 << 20), node.NodePinnedModel, nil
	}
	if obs.SystemTotalBytes > 0 {
		d := int64(obs.SystemTotalBytes) - int64(obs.SystemUsedBytes)
		if d < 0 {
			d = 0
		}
		return int(d / 1 << 20), node.NodePinnedModel, nil
	}
	return 0, "", fmt.Errorf("read free memory: %q reports no GPU or system memory", node.Name)
}

func (p *serverHeadroomProbe) RequestMemoryMB(ctx context.Context, node compute.Node, model string) (int, error) {
	addr := strings.TrimRight(strings.TrimSpace(node.Address), "/")
	if addr == "" {
		return 0, fmt.Errorf("request memory: node %q has no address", node.Name)
	}
	before, _, err := p.ReadFreeMB(ctx, node)
	if err != nil {
		return 0, err
	}
	// Same inline-client pattern as compute.probeHTTP: the node is an
	// operator-declared remote, often behind a self-signed certificate.
	client := &http.Client{ //nolint:noctx // per-request probe; ctx is on the request itself
		Timeout: 90 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- operator-declared remote, often self-signed
		},
	}
	payload := map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "Count from 1 to 5."}},
		"stream":     false,
		"keep_alive": "10m",
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addr+"/api/chat", strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("request memory: build probe request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request memory: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close() //nolint:errcheck
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("request memory: model %q on %s answered HTTP %d", model, addr, resp.StatusCode)
	}
	after, _, err := p.ReadFreeMB(ctx, node)
	if err != nil {
		return 0, err
	}
	if d := before - after; d > 0 {
		return d, nil
	}
	return 0, nil
}
