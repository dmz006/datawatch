package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

// admissionRequest resolves the query parameters of GET
// /api/capacity/admission into the ledger request a real start of that
// backend would make: the implicit host pool, the pools resolvePoolsForBackend
// names, and the node pool when a node is given. A non-empty message is the
// 400 reason.
func (s *Server) admissionRequest(q map[string][]string) (capacity.Request, string) {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	backend := get("backend")
	if backend == "" {
		backend = get("llm")
	}
	if backend == "" {
		return capacity.Request{}, "backend (or llm) is required"
	}
	if s.inferenceReg != nil {
		if _, err := s.inferenceReg.Get(backend); err != nil && !inference.IsSessionBackendKind(inference.Kind(backend)) {
			return capacity.Request{}, "unknown backend: " + backend
		}
	}
	req := capacity.Request{Holder: "admission-query", PRDID: get("prd_id"), Model: get("model"), Node: get("node")}
	if p := get("priority"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return capacity.Request{}, "priority must be an integer"
		}
		req.Priority = n
	}
	switch role := get("role"); role {
	case "", "worker":
	case "verifier":
		req.Role = capacity.RoleVerifier
	case "planner":
		req.Role = capacity.RolePlanner
	case "interactive":
		req.Interactive = true
	default:
		return capacity.Request{}, "role must be worker, verifier, planner or interactive"
	}
	req.Pools = append([]string{"host"}, s.resolvePoolsForBackend(backend)...)
	if req.Node != "" {
		np := "node:" + req.Node
		have := false
		for _, p := range req.Pools {
			have = have || p == np
		}
		if !have {
			req.Pools = append(req.Pools, np)
		}
	}
	return req, ""
}

// handleCapacityAdmission serves GET /api/capacity/admission: whether a start
// of the given backend/node/model would be admitted now, queued (with its
// position) or could use an alternate pool. Read-only: it takes no lease, adds
// no waiter and changes no state.
func (s *Server) handleCapacityAdmission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	req, msg := s.admissionRequest(r.URL.Query())
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	if s.capacityLedger == nil {
		writeJSONOK(w, capacity.Decision{Kind: capacity.DecisionAdmit})
		return
	}
	writeJSONOK(w, s.capacityLedger.Check(req))
}

// AdmissionHandler returns the admission query handler over the given ledger
// and LLM registry, for surface tests outside this package.
func AdmissionHandler(l *capacity.Ledger, reg *inference.Registry) http.Handler {
	s := &Server{inferenceReg: reg}
	s.SetCapacity(l)
	return http.HandlerFunc(s.handleCapacityAdmission)
}
