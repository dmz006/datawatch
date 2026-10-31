// v7.0.0 S1 — REST surface for the ComputeNode registry.
//
//	GET    /api/compute/nodes              list all nodes
//	POST   /api/compute/nodes              create a new node
//	GET    /api/compute/nodes/{name}       fetch one
//	PUT    /api/compute/nodes/{name}       replace one
//	DELETE /api/compute/nodes/{name}       remove one
//	GET    /api/compute/nodes/{name}/health    static + live capacity
//	GET    /api/compute/nodes/{name}/detail    on-demand pull from stub /api/stats sidecar (ASK 24 hybrid)
//
// Returns 503 when no registry is wired.

package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
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

// ComputeRegistry returns the wired registry (nil when not set).
// alpha.26 #238 — main.go uses this from the peer-registry init block
// to run the leaked-auto-node sweep without holding a separate ref.
func (s *Server) ComputeRegistry() *compute.Registry { return s.computeReg }

// clusterLookup wraps the existing s.clusterStore so the
// compute.Probe(k8s) path can resolve ClusterProfile by name without
// pulling the profile package into internal/compute (which would
// create an import cycle). v7.0.0-alpha.19 #245.
func (s *Server) clusterLookup() compute.ClusterLookup { return &clusterLookupAdapter{s: s} }

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
		if s.peerRegistry != nil && body.Peer != s.hostname {
			if _, ok := s.peerRegistry.Get(body.Peer); !ok {
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
	out := map[string]any{
		"name":                n.Name,
		"kind":                n.Kind,
		"in_maintenance":      n.InMaintenance(time.Now().UTC()),
		"declared_capacity":   n.DeclaredCapacity,
		"scheduling_priority": n.SchedulingPriority,
		"address":             n.Address,
		"auto_created":        n.AutoCreated,
	}
	writeJSONOK(w, out)
}

// handleComputeNodeDetail serves the node detail with the node's slot figures
// added: session_slots is the operator's override (0 = derive from observer
// data), and derived_slots is the slot count in effect, the pool limit syncPools
// set from the same figures. The figures are added to a JSON object body only.
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

// withNodeSlots adds session_slots and derived_slots to a node detail body. A
// body that is not a JSON object is returned unchanged. derived_slots is left
// out when the node has no pool limit yet.
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
	if s.capacityLedger != nil {
		for _, p := range s.capacityLedger.Snapshot().Pools {
			if p.Name == "node:"+name && p.Limit > 0 {
				fields["derived_slots"] = json.RawMessage(strconv.Itoa(p.Limit))
			}
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
		parser = func(b []byte) []string {
			var doc struct {
				Models []struct {
					Name string `json:"name"`
				} `json:"models"`
			}
			if json.Unmarshal(b, &doc) != nil {
				return nil
			}
			out := make([]string, 0, len(doc.Models))
			for _, m := range doc.Models {
				if m.Name != "" {
					out = append(out, m.Name)
				}
			}
			return out
		}
	case "openwebui":
		// OpenWebUI exposes /api/v1/models — same OpenAI-compat shape.
		probeURL = strings.TrimRight(addr, "/") + "/api/v1/models"
		parser = func(b []byte) []string {
			var doc struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(b, &doc) != nil {
				return nil
			}
			out := make([]string, 0, len(doc.Data))
			for _, m := range doc.Data {
				if m.ID != "" {
					out = append(out, m.ID)
				}
			}
			return out
		}
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
