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
	if req.Node == "" {
		// An entry that pools two or more known nodes is placed by the ledger
		// on one of them, so its node pools are choices, not all required.
		if choices := s.nodeChoicesForBackend(backend); len(choices) >= 2 {
			req.Pools = dropNodePools(req.Pools)
			req.NodeChoices = choices
		}
	}
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

// nodeChoicesForBackend returns the nodes of the named LLM entry the compute
// registry knows, with their scheduling priority, in the entry's list order.
func (s *Server) nodeChoicesForBackend(backend string) []capacity.NodeChoice {
	if s.inferenceReg == nil || s.computeReg == nil {
		return nil
	}
	l, err := s.inferenceReg.Get(backend)
	if err != nil || l == nil {
		return nil
	}
	nodes := inference.OrderedPoolNodes(*l, func(name string) (int, bool) {
		n, err := s.computeReg.Get(name)
		if err != nil || n == nil {
			return 0, false
		}
		if n.SchedulingPriority == 0 {
			return 50, true
		}
		return n.SchedulingPriority, true
	})
	out := make([]capacity.NodeChoice, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, capacity.NodeChoice{Name: n.Name, Priority: n.Priority})
	}
	return out
}

// dropNodePools returns pools without its node:<name> entries.
func dropNodePools(pools []string) []string {
	out := make([]string, 0, len(pools))
	for _, p := range pools {
		if !strings.HasPrefix(p, "node:") {
			out = append(out, p)
		}
	}
	return out
}
