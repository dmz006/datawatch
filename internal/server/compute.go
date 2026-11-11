// v7.0.0 S1 — REST surface for the ComputeNode registry.
//
//	GET    /api/compute/nodes              list all nodes
//	POST   /api/compute/nodes              create a new node
//	GET    /api/compute/nodes/{name}       fetch one
//	PUT    /api/compute/nodes/{name}       replace one
//	DELETE /api/compute/nodes/{name}       remove one
//	GET    /api/compute/nodes/{name}/health    static + live capacity
//	GET    /api/compute/nodes/{name}/detail    on-demand pull from stub /api/stats sidecar (ASK 24 hybrid)
//	POST   /api/compute/nodes/{name}/drain    drain the node (optional until, reason)
//	DELETE /api/compute/nodes/{name}/drain    undrain (idempotent)
//
// Returns 503 when no registry is wired.

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
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/federation"
)

// SetComputeRegistry wires the runtime *compute.Registry into the
// server. Called from cmd/datawatch/main.go after registry init.
func (s *Server) SetComputeRegistry(r *compute.Registry) { s.computeReg = r }

// VerifierNodeStatus is one compute node's verifier admission state as the
// daemon's capacity wiring last saw it (cmd/datawatch's VerifierNodeState):
// the caps it applied and reported, the free GPU memory in MB, the headroom
// verdict for new verifier work, and the advice slot figure. SuggestedCap is
// nil when the node has no usable figure — advice only, never applied
// automatically.
type VerifierNodeStatus struct {
	ExecutorCap    int
	VerifierCap    int
	FreeMB         uint64
	HasData        bool
	HeadroomOK     bool
	HeadroomReason string
	SuggestedCap   *int
}

// VerifierNodeStatusReader reports one compute node's last cached verifier
// admission state (ok=false until the node has been synced). It must read
// only the cache — never an observer — so the REST handlers stay
// observation-free, like the ledger's own VerifierHeadroom gate.
type VerifierNodeStatusReader func(name string) (VerifierNodeStatus, bool)

// SetVerifierNodeStatus wires the daemon's verifier admission state reader
// into the REST surface (node detail + the capacity view). When nil (not
// wired) the admission-state figures are simply absent from both responses.
func (s *Server) SetVerifierNodeStatus(fn VerifierNodeStatusReader) { s.verifierNodeStatus = fn }

// SetVerifierNodeStatus wires the verifier admission state reader on the
// HTTP server (pass-through to the API server it wraps).
func (s *HTTPServer) SetVerifierNodeStatus(fn VerifierNodeStatusReader) {
	if s.api != nil {
		s.api.SetVerifierNodeStatus(fn)
	}
}

// ComputeRegistry returns the wired registry (nil when not set).
// alpha.26 #238 — main.go uses this from the peer-registry init block
// to run the leaked-auto-node sweep without holding a separate ref.
func (s *Server) ComputeRegistry() *compute.Registry { return s.computeReg }

// clusterLookup wraps the existing s.clusterStore so the
// compute.Probe(k8s) path can resolve ClusterProfile by name without
// pulling the profile package into internal/compute (which would
// create an import cycle). v7.0.0-alpha.19 #245.
func (s *Server) clusterLookup() compute.ClusterLookup { return &clusterLookupAdapter{s: s} }

// versionProbeFn returns the backend version probe used after a
// successful save-time reachability probe. Overridable in tests (in
// production it always uses the injectable-default probe; tests in
// the compute package fake at the VersionProbe level).
func (s *Server) versionProbeFn() *compute.VersionProbe { return compute.NewVersionProbe() }

// captureBackendVersion records the node's backend version (Ollama
// /api/version, docker ServerVersion) on the node record so it
// appears in the node detail payload. Non-fatal by design: the
// reachability probe already gated the save, a missing or
// unparseable version is not a save error.
func (s *Server) captureBackendVersion(ctx context.Context, n *compute.Node) {
	vp := s.versionProbeFn()
	if vr, verr := vp.ProbeVersion(ctx, n); verr == nil {
		n.RecordBackendVersion(vr)
	}
}

type clusterLookupAdapter struct{ s *Server }

func (a *clusterLookupAdapter) GetClusterProfile(name string) (kubeconfigPath, contextName string, extraArgs []string, err error) {
	if a == nil || a.s == nil || a.s.clusterStore == nil {
		return "", "", nil, fmt.Errorf("cluster store unavailable")
	}
	cp, gerr := a.s.clusterStore.Get(name)
	if gerr != nil {
		return "", "", nil, gerr
	}
	// ClusterProfile carries a kubectl context name; the kubeconfig
	// path is implicit (operator's default ~/.kube/config). Returning
	// empty kubeconfigPath tells the probe to use the kubectl default.
	return "", cp.Context, nil, nil
}

// serverNow is the clock the server uses for drain bookkeeping and node
// health "now" checks. Tests override it with a fixed clock.
var serverNow = func() time.Time { return time.Now().UTC() }

func (s *Server) handleComputeNodes(w http.ResponseWriter, r *http.Request) {
	if s.computeReg == nil {
		http.Error(w, "compute registry disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/compute/nodes")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapComputeList) {
			return
		}
		writeJSONOK(w, map[string]any{"nodes": s.computeReg.List()})

	case rest == "" && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		var n compute.Node
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Validate before probing so field errors return 400, not 502.
		// Validate before probing so field errors return 400, not 502.
		if verr := n.Validate(); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
		// v7.0.0-alpha.19 #245 Q3 — operator-spec’d save-time probe for
		// all kinds. Bypass with ?probe=skip for emergency saves (e.g.
		// when the operator KNOWS the node is currently unreachable but
		// wants to persist the entry).
		if r.URL.Query().Get("probe") != "skip" {
			probeTimeout := 15 * time.Second
			if n.Routing == compute.RoutingDockerNetwork && n.RoutingDockerNetwork != nil && n.RoutingDockerNetwork.AutoStart {
				probeTimeout = 30 * time.Second
			}
			pctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
			defer cancel()
			if perr := compute.Probe(pctx, &n, s.clusterLookup()); perr != nil {
				http.Error(w, "probe failed (use ?probe=skip to save anyway): "+perr.Error(), http.StatusBadGateway)
				return
			}
			// Reachable — capture the backend version (non-fatal).
			s.captureBackendVersion(pctx, &n)
		}
		code := http.StatusBadRequest
		if err := s.computeReg.Add(&n); err != nil {
			if err == compute.ErrConflict {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditComputeTLS(&n, "compute_node_add")
		writeJSONOK(w, map[string]any{"name": n.Name, "ok": true})

	// v7.0.0-alpha.23 (Q6) — operator on/off toggle for ComputeNodes,
	// matching the LLM enable/disable contract. Body: {"enabled": bool}.
	// Disabling is unconditional; enabling clears any LastDispatchError.
	case strings.HasSuffix(rest, "/enabled") && (r.Method == http.MethodPatch || r.Method == http.MethodPost):
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		name := strings.TrimSuffix(rest, "/enabled")
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		n, err := s.computeReg.Get(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		n.Disabled = !body.Enabled
		if body.Enabled {
			n.LastDispatchError = "" // operator re-enabled; clear stale notice
		}
		if err := s.computeReg.Update(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		action := "compute_node_enable"
		if !body.Enabled {
			action = "compute_node_disable"
		}
		s.auditCompute(name, action)
		writeJSONOK(w, map[string]any{"name": name, "enabled": !n.Disabled, "ok": true})

	// v7.0.0-alpha.23b — attach/detach an observer peer to a ComputeNode.
	// PUT body: {"peer": "<peer-name>"}. DELETE clears the binding.
	// Observer-down does NOT touch this binding (Q4) — operator-driven only.
	case strings.HasSuffix(rest, "/observer-peer") && (r.Method == http.MethodPut || r.Method == http.MethodPost):
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		name := strings.TrimSuffix(rest, "/observer-peer")
		var body struct {
			Peer string `json:"peer"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		body.Peer = strings.TrimSpace(body.Peer)
		if body.Peer == "" {
			http.Error(w, "peer required (use DELETE to clear)", http.StatusBadRequest)
			return
		}
		n, err := s.computeReg.Get(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// 2026-10-15 — operator-reported: a ComputeNode representing this
		// same machine (e.g. a CPU-only logical partition) could never be
		// attached to the local daemon's own self-peer, because the self
		// peer is only ever synthesized for display (synthesizeSelfPeer)
		// — it's never a real entry in s.peerRegistry, so this lookup
		// always rejected it as "unknown". handleComputeNodeDetail's own
		// self-peer fallback already treats body.Peer == s.hostname as
		// always-valid (served live, no registration needed) — this check
		// was inconsistent with that. Match it.
		//
		// Pulled peers (federated, observer_pull on) are bindable exactly
		// like pushed peers — they live in the federation peer store, not
		// the push registry, so check both. Shadow peers (federated,
		// observer_pull off, never pushed) carry no data to bind a node
		// to — reject with 409 and say why.
		if body.Peer != s.hostname {
			if bindable, conflict := s.observerPeerAttachCheck(body.Peer); !bindable {
				if conflict {
					http.Error(w, "peer "+body.Peer+" has observer pull disabled (observer_pull is off) and has not pushed any stats, so there is no data to bind a compute node to. Enable observer pull for this peer, or let it push stats, then attach it again.", http.StatusConflict)
					return
				}
				http.Error(w, "unknown observer peer: "+body.Peer, http.StatusBadRequest)
				return
			}
		}
		n.ObserverPeer = body.Peer
		if err := s.computeReg.Update(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditCompute(name, "compute_node_attach_observer")
		writeJSONOK(w, map[string]any{"name": name, "observer_peer": body.Peer, "ok": true})

	case strings.HasSuffix(rest, "/observer-peer") && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		name := strings.TrimSuffix(rest, "/observer-peer")
		n, err := s.computeReg.Get(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		n.ObserverPeer = ""
		if err := s.computeReg.Update(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditCompute(name, "compute_node_detach_observer")
		writeJSONOK(w, map[string]any{"name": name, "observer_peer": "", "ok": true})

	// Drain the node: stop new placements, keep in-flight work. POST body
	// is optional: {"until": "<RFC3339>", "reason": "..."}. DELETE clears
	// the drain; it is idempotent and 200 even when the node was not
	// drained. Both reply with the node health JSON so the caller sees the
	// new state.
	case strings.HasSuffix(rest, "/drain") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		s.handleComputeNodeDrain(w, r, strings.TrimSuffix(rest, "/drain"), true)

	case strings.HasSuffix(rest, "/drain") && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		s.handleComputeNodeDrain(w, r, strings.TrimSuffix(rest, "/drain"), false)

	case strings.HasSuffix(rest, "/health") && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapComputeRead) {
			return
		}
		name := strings.TrimSuffix(rest, "/health")
		s.handleComputeNodeHealth(w, r, name)

	case strings.HasSuffix(rest, "/detail") && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapComputeRead) {
			return
		}
		name := strings.TrimSuffix(rest, "/detail")
		s.handleComputeNodeDetail(w, r, name)

	// v7.0.0-alpha.18 #242 — list models available on this Node for the
	// requested LLM kind. Used by PWA's kind-aware model dropdown so
	// operators don't have to know exact model names.
	case strings.HasSuffix(rest, "/models") && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapComputeRead) {
			return
		}
		name := strings.TrimSuffix(rest, "/models")
		s.handleComputeNodeModels(w, r, name)

	case strings.HasSuffix(rest, "/models/pull") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		name := strings.TrimSuffix(rest, "/models/pull")
		s.handleComputeNodeModelPull(w, r, name)

	case strings.Contains(rest, "/models/") && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		idx := strings.Index(rest, "/models/")
		name := rest[:idx]
		model := rest[idx+len("/models/"):]
		s.handleComputeNodeModelDelete(w, r, name, model)

	case rest != "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapComputeRead) {
			return
		}
		n, err := s.computeReg.Get(rest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONOK(w, n)

	case rest != "" && r.Method == http.MethodPut:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		var n compute.Node
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if n.Name == "" {
			n.Name = rest
		}
		if n.Name != rest {
			http.Error(w, "name mismatch (path vs body)", http.StatusBadRequest)
			return
		}
		// Validate before probing so field errors return 400, not 502 —
		// same contract as the POST path, so a negative verifier setting
		// is refused on replace too ("compute node: verifier_slots %d must
		// be >= 0"), not saved and rejected later at admission.
		if verr := n.Validate(); verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("probe") != "skip" {
			probeTimeout := 15 * time.Second
			if n.Routing == compute.RoutingDockerNetwork && n.RoutingDockerNetwork != nil && n.RoutingDockerNetwork.AutoStart {
				probeTimeout = 30 * time.Second
			}
			pctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
			defer cancel()
			if perr := compute.Probe(pctx, &n, s.clusterLookup()); perr != nil {
				http.Error(w, "probe failed (use ?probe=skip to save anyway): "+perr.Error(), http.StatusBadGateway)
				return
			}
			// Reachable — capture the backend version (non-fatal).
			s.captureBackendVersion(pctx, &n)
		}
		if err := s.computeReg.Update(&n); err != nil {
			code := http.StatusBadRequest
			if err == compute.ErrNotFound {
				code = http.StatusNotFound
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditComputeTLS(&n, "compute_node_update")
		writeJSONOK(w, map[string]any{"name": n.Name, "ok": true})

	case rest != "" && r.Method == http.MethodDelete:
		if !s.fedCap(w, r, federation.CapComputeWrite) {
			return
		}
		// Tear down docker-network lifecycle before registry removal so
		// the container and network are cleaned up while we still have
		// the node config.
		if delNode, getErr := s.computeReg.Get(rest); getErr == nil &&
			delNode.Routing == compute.RoutingDockerNetwork {
			lc := &compute.DockerLifecycle{}
			_ = lc.Teardown(r.Context(), delNode)
		}
		if err := s.computeReg.Delete(rest); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.auditCompute(rest, "compute_node_delete")
		writeJSONOK(w, map[string]any{"name": rest, "ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleComputeNodeHealth returns declared capacity + maintenance
// state + the latest known stats peer push for this Node (when
// available via the existing observer-peer surface).
func (s *Server) handleComputeNodeHealth(w http.ResponseWriter, _ *http.Request, name string) {
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	now := serverNow()

	// heldTasks is the number of admission-ledger leases that name this node;
	// 0 when the server has no ledger wired.
	heldTasks := 0
	if s.capacityLedger != nil {
		for _, ls := range s.capacityLedger.Snapshot().Leases {
			if ls.Node == name {
				heldTasks++
			}
		}
	}

	// deniedConsumers is the node's RBAC denial list; always a list, empty
	// when the node denies no consumer.
	denied := n.Permissions.DeniedConsumers
	if denied == nil {
		denied = []string{}
	}

	// activeWindows is each maintenance window that covers now (an open-ended
	// window is still active); empty when none does.
	activeWindows := []map[string]any{}
	for _, w := range n.MaintenanceWindows {
		if w.From.After(now) || (!w.To.IsZero() && w.To.Before(now)) {
			continue
		}
		wm := map[string]any{"from": w.From.UTC().Format(time.RFC3339), "reason": w.Reason}
		if !w.To.IsZero() {
			wm["to"] = w.To.UTC().Format(time.RFC3339)
		}
		activeWindows = append(activeWindows, wm)
	}

	out := map[string]any{
		"name":                       n.Name,
		"kind":                       n.Kind,
		"in_maintenance":             n.InMaintenance(now),
		"declared_capacity":          n.DeclaredCapacity,
		"scheduling_priority":        n.SchedulingPriority,
		"address":                    n.Address,
		"auto_created":               n.AutoCreated,
		"drained":                    n.Drained,
		"drain_active":               n.DrainActive(now),
		"drain_reason":               n.DrainReason,
		"held_tasks":                 heldTasks,
		"denied_consumers":           denied,
		"maintenance_windows_active": activeWindows,
	}
	if !n.DrainUntil.IsZero() {
		out["drain_until"] = n.DrainUntil.UTC().Format(time.RFC3339)
	}
	writeJSONOK(w, out)
}

// handleComputeNodeDrain sets (drain=true) or clears (drain=false) the
// node's drain via the registry and replies with the node health JSON.
// A non-zero until that is not after now is a 400; an unknown node is a
// 404. Undrain is idempotent: 200 even when the node was not drained.
func (s *Server) handleComputeNodeDrain(w http.ResponseWriter, r *http.Request, name string, drain bool) {
	now := serverNow()
	var until time.Time
	var reason string
	if drain {
		var body struct {
			Until  string `json:"until"`
			Reason string `json:"reason"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		if body.Until != "" {
			parsed, err := time.Parse(time.RFC3339, body.Until)
			if err != nil {
				http.Error(w, "until must be an RFC3339 timestamp", http.StatusBadRequest)
				return
			}
			if !parsed.After(now) {
				http.Error(w, "until must be after now", http.StatusBadRequest)
				return
			}
			until = parsed
		}
		reason = strings.TrimSpace(body.Reason)
	}
	if err := s.computeReg.SetDrain(name, drain, until, now, reason); err != nil {
		code := http.StatusInternalServerError
		if err == compute.ErrNotFound {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	if drain {
		s.auditCompute(name, "compute_node_drain")
	} else {
		s.auditCompute(name, "compute_node_undrain")
	}
	s.handleComputeNodeHealth(w, r, name)
}

// handleComputeNodeDetail serves the node detail with the node's slot
// figures added: session_slots is the operator's override (0 = derive from
// observer data), derived_slots the slot count in effect (the pool limit
// syncPools set from the same figures), and the verifier settings and
// admission state (see withNodeSlots). The figures are added to a JSON
// object body only.
func (s *Server) handleComputeNodeDetail(w http.ResponseWriter, r *http.Request, name string) {
	rec := httptest.NewRecorder()
	s.serveComputeNodeDetail(rec, r, name)
	body := rec.Body.Bytes()
	if rec.Code == http.StatusOK {
		body = s.withNodeSlots(name, body)
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(rec.Code)
	w.Write(body) //nolint:errcheck
}

// withNodeSlots adds the node's slot figures to a node detail body: the
// operator's settings and the counts in effect — so operators set, see the
// effect, and can judge their settings.
//
//	session_slots                  the operator's setting (0 = derive)
//	derived_slots                  the executor pool limit (in effect), if any
//	executor_slots                 the same figure as derived_slots — the
//	                                executor pair shown beside the verifier pair
//	verifier_slots                 the operator's verifier_slots or null (unset)
//	verifier_min_free_mem_mb       the operator's setting or null (unset)
//	verifier_min_free_mem_mb_effective the effective setting (1024 when unset)
//	verifier_context_mb            the operator's setting
//	verifier_slots_effective       the verifier cap last applied (reader)
//	suggested_verifier_slots       the advice figure, null when no usable data
//	headroom_ok / headroom_reason  the last headroom verdict (reader)
//	free_gpu_mb                    the last free GPU memory in MB (reader)
//
// The reader-backed figures (verifier_slots_effective, suggested, headroom,
// free_gpu_mb) are absent until the daemon's capacity wiring has synced the
// node; withNodeSlots reads only the injected cache reader and performs no
// observer I/O. A body that is not a JSON object is returned unchanged.
func (s *Server) withNodeSlots(name string, body []byte) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return body
	}
	n, err := s.computeReg.Get(name)
	if err != nil {
		return body
	}
	fields["session_slots"] = json.RawMessage(strconv.Itoa(n.SessionSlots))

	// The verifier settings straight from the node record.
	if n.VerifierSlots != nil {
		fields["verifier_slots"] = json.RawMessage(strconv.Itoa(*n.VerifierSlots))
	} else {
		fields["verifier_slots"] = json.RawMessage("null")
	}
	if n.VerifierMinFreeMemMB != nil {
		fields["verifier_min_free_mem_mb"] = json.RawMessage(strconv.Itoa(*n.VerifierMinFreeMemMB))
	} else {
		fields["verifier_min_free_mem_mb"] = json.RawMessage("null")
	}
	minFree := compute.EffectiveVerifierMinFreeMB(n.VerifierMinFreeMemMB)
	fields["verifier_min_free_mem_mb_effective"] = json.RawMessage(strconv.Itoa(minFree))
	fields["verifier_context_mb"] = json.RawMessage(strconv.Itoa(n.VerifierContextMB))

	// The slot counts in effect, from the ledger pools: the executor pair
	// (derived_slots) and, beside it, the verifier pair.
	if s.capacityLedger != nil {
		for _, p := range s.capacityLedger.Snapshot().Pools {
			switch p.Name {
			case "node:" + name:
				if p.Limit > 0 {
					derived := json.RawMessage(strconv.Itoa(p.Limit))
					fields["derived_slots"] = derived
					fields["executor_slots"] = derived // the same figure, beside the verifier pair
				}
			case "node-verify:" + name:
				if p.Limit > 0 {
					fields["verifier_slots_effective"] = json.RawMessage(strconv.Itoa(p.Limit))
				}
			}
		}
	}

	// The daemon's last-seen verifier admission state (advice + headroom).
	if s.verifierNodeStatus != nil {
		if st, ok := s.verifierNodeStatus(name); ok {
			fields["verifier_slots_effective"] = json.RawMessage(strconv.Itoa(st.VerifierCap))
			if st.SuggestedCap != nil {
				fields["suggested_verifier_slots"] = json.RawMessage(strconv.Itoa(*st.SuggestedCap))
			} else {
				fields["suggested_verifier_slots"] = json.RawMessage("null")
			}
			fields["headroom_ok"] = json.RawMessage(strconv.FormatBool(st.HeadroomOK))
			reason, _ := json.Marshal(st.HeadroomReason)
			fields["headroom_reason"] = reason
			fields["free_gpu_mb"] = json.RawMessage(strconv.FormatUint(st.FreeMB, 10))
		}
	}

	out, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return out
}

// serveComputeNodeDetail performs the on-demand pull (ASK 24 hybrid)
// to the Node's monitoring sidecar (--listen address). Operator clicks
// "live process detail" in Automata UI; daemon hits stub:9001/api/stats
// once and returns the snapshot.
func (s *Server) serveComputeNodeDetail(w http.ResponseWriter, r *http.Request, name string) {
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// When MonitoringEndpoint is empty, fall back to the last cached
	// snapshot from the peer registry (observer pushing stats without a
	// pull endpoint configured — common when the observer uses a non-
	// default port or is behind a firewall).
	// Use ObserverPeer name when set (e.g. "datawatch") rather than the
	// compute node name (e.g. "datawatch-ollama") — multiple compute nodes
	// can share one observer peer (e.g. ollama + openwebui on same box).
	if n.MonitoringEndpoint == "" {
		peerName := name
		if n.ObserverPeer != "" {
			peerName = n.ObserverPeer
		}
		if s.peerRegistry != nil {
			if snap := s.peerRegistry.LastPayload(peerName); snap != nil {
				writeJSONOK(w, snap)
				return
			}
		}
		// Self-peer fallback: when the bound peer is the local daemon and the
		// local observer is running, serve live stats directly (the self-peer
		// never pushes snapshots — it's always live).
		//
		// 2026-10-15 — operator-reported: "local-node-cpu" (a CPU-only
		// logical ComputeNode for the same physical host as the daemon)
		// 503'd here because its ObserverPeer was never set, so peerName
		// fell through to its own node name, which doesn't equal
		// s.hostname. The fix is NOT to infer locality from the node's
		// Address (hostname/IP resolution) — operator explicitly rejected
		// that approach: this system already has a real, configured way to
		// say "this ComputeNode is served by the local daemon" (the
		// ObserverPeer field, set via compute_node_attach_observer /
		// PUT .../observer_peer). The actual fix is data, not code: give
		// every ComputeNode that represents this machine an ObserverPeer
		// of s.hostname when it's created or configured. This check stays
		// exactly as designed — name-based, not inferred.
		if s.observerAPI != nil && peerName == s.hostname {
			s.handleObserverStats(w, r)
			return
		}
		// For docker-network nodes, return container status as the detail
		// payload so operators and tests can see container_running without
		// needing a full monitoring sidecar.
		if n.Routing == compute.RoutingDockerNetwork && n.RoutingDockerNetwork != nil {
			lc := &compute.DockerLifecycle{}
			running, containerID, _ := lc.Status(r.Context(), n)
			writeJSONOK(w, map[string]any{
				"container_running": running,
				"container_id":      containerID,
				"image":             n.RoutingDockerNetwork.Image,
				"network_name":      n.RoutingDockerNetwork.NetworkName,
				"container_name":    n.RoutingDockerNetwork.ContainerName,
			})
			return
		}
		http.Error(w, "compute node has no monitoring_endpoint configured and no cached observer snapshot (set monitoring_endpoint or wait for the observer peer to push stats)", http.StatusServiceUnavailable)
		return
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- operator-declared monitoring endpoint, often self-signed; matches existing observer-peer pattern
		},
	}
	endpoint := n.MonitoringEndpoint
	// If only host:port supplied, default to /api/stats path.
	if !strings.Contains(endpoint, "/api/") {
		endpoint = strings.TrimRight(endpoint, "/") + "/api/stats"
	}
	resp, err := client.Get(endpoint)
	if err != nil {
		http.Error(w, fmt.Sprintf("stub unreachable: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()                                 //nolint:errcheck
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap at 1MB
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("stub HTTP %d: %s", resp.StatusCode, string(body)), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body) //nolint:errcheck
}

// handleComputeNodeModels (v7.0.0-alpha.18 #242) — list models on this
// Node for the requested LLM kind. Probes the kind-appropriate model
// endpoint and returns a flat string list. PWA uses this to populate
// the kind-aware model dropdown so operators don't have to know exact
// model identifiers.
//
//	GET /api/compute/nodes/{name}/models?kind=ollama
//
// Response: {"models": ["llama3:70b", "qwen3:8b", ...], "kind": "ollama"}
//
// Empty list returned (with 200) when the probe succeeds but lists no
// models — caller falls back to free-text input. 502 only when the
// probe itself fails (Node unreachable, malformed response).
func (s *Server) handleComputeNodeModels(w http.ResponseWriter, r *http.Request, name string) {
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "ollama"
	}
	addr := n.Address
	if addr == "" {
		writeJSONOK(w, map[string]any{"models": []string{}, "kind": kind, "note": "node has no address"})
		return
	}
	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- operator-declared node URL, often self-signed
		},
	}
	var probeURL string
	var parser func([]byte) []string
	switch strings.ToLower(kind) {
	case "ollama", "opencode":
		// Ollama protocol — GET /api/tags returns {"models":[{"name":"llama3:70b",...}]}.
		probeURL = strings.TrimRight(addr, "/") + "/api/tags"
		parser = compute.ParseOllamaTags
	case "openwebui":
		// OpenWebUI exposes /api/v1/models — same OpenAI-compat shape.
		probeURL = strings.TrimRight(addr, "/") + "/api/v1/models"
		parser = compute.ParseOpenAIModels
	default:
		// Session-backend kinds (claude-code, aider, goose, gemini,
		// shell, opencode-acp, opencode-prompt) don't expose a model
		// list endpoint — model is the binary's own choice. Return
		// empty so the PWA falls back to free text.
		writeJSONOK(w, map[string]any{"models": []string{}, "kind": kind, "note": "kind has no model probe"})
		return
	}
	resp, perr := client.Get(probeURL)
	if perr != nil {
		http.Error(w, fmt.Sprintf("probe %s failed: %v", probeURL, perr), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("probe %s returned HTTP %d", probeURL, resp.StatusCode), http.StatusBadGateway)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	models := parser(body)
	if models == nil {
		writeJSONOK(w, map[string]any{"models": []string{}, "kind": kind, "note": "could not parse probe response"})
		return
	}
	writeJSONOK(w, map[string]any{"models": models, "kind": kind, "node": name})
}

func (s *Server) auditCompute(name, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "compute_node",
			"resource_id":   name,
		},
	})
}

// auditComputeTLS records an add or update of a node together with its TLS
// settings: whether verification is off, and whether a CA file or a
// certificate fingerprint pin is set. Only booleans are written, never the
// CA path or the fingerprint. When verification is off a second entry with
// the action compute_node_tls_verification_off is written, so the opt-out
// can be filtered on its own.
func (s *Server) auditComputeTLS(n *compute.Node, action string) {
	if s.auditLog == nil || n == nil {
		return
	}
	insecure := n.TLSInsecureSkipVerify
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type":            "compute_node",
			"resource_id":              n.Name,
			"tls_insecure_skip_verify": insecure,
			"tls_ca_file_set":          strings.TrimSpace(n.TLSCAFile) != "",
			"tls_cert_fingerprint_set": n.TLSCertFingerprint != "",
		},
	})
	if !insecure {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: "compute_node_tls_verification_off",
		Details: map[string]any{
			"resource_type":            "compute_node",
			"resource_id":              n.Name,
			"tls_insecure_skip_verify": true,
			"tls_ca_file_set":          strings.TrimSpace(n.TLSCAFile) != "",
			"tls_cert_fingerprint_set": n.TLSCertFingerprint != "",
			"source":                   action,
		},
	})
}
