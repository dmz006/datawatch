package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

// SetCapacity wires the admission ledger exposed at GET /api/capacity.
func (s *Server) SetCapacity(l *capacity.Ledger) { s.capacityLedger = l }

// SetCapacity wires the admission ledger on the HTTP server.
func (s *HTTPServer) SetCapacity(l *capacity.Ledger) {
	if s.api != nil {
		s.api.SetCapacity(l)
	}
}

// InferenceRegistry returns the wired LLM registry (nil when not set).
func (s *HTTPServer) InferenceRegistry() *inference.Registry {
	if s.api == nil {
		return nil
	}
	return s.api.inferenceReg
}

// NodeGPUStats returns the busiest GPU's utilisation (percent) reported for a
// compute node, and whether any GPU data was available.
func (s *HTTPServer) NodeGPUStats(name string) (maxUtilPct float64, ok bool) {
	if s.api == nil || s.api.computeReg == nil {
		return 0, false
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/compute/nodes/"+name+"/detail", nil)
	s.api.handleComputeNodeDetail(rec, req, name)
	if rec.Code != http.StatusOK {
		return 0, false
	}
	var d struct {
		GPU []struct {
			UtilPct float64 `json:"util_pct"`
		} `json:"gpu"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &d) != nil || len(d.GPU) == 0 {
		return 0, false
	}
	for _, g := range d.GPU {
		if g.UtilPct > maxUtilPct {
			maxUtilPct = g.UtilPct
		}
	}
	return maxUtilPct, true
}

// handleCapacity serves GET /api/capacity: pools, limits, holders and the
// wait queue.
func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.capacityLedger == nil {
		writeJSONOK(w, capacity.Status{Pools: []capacity.PoolStatus{}, Leases: []capacity.Lease{}, Waiting: []capacity.Waiter{}})
		return
	}
	st := s.capacityLedger.Snapshot()
	if st.Pools == nil {
		st.Pools = []capacity.PoolStatus{}
	}
	if st.Leases == nil {
		st.Leases = []capacity.Lease{}
	}
	if st.Waiting == nil {
		st.Waiting = []capacity.Waiter{}
	}
	writeJSONOK(w, st)
}
