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
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/compute"
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
		writeJSONOK(w, map[string]any{"nodes": s.computeReg.List()})

	case rest == "" && r.Method == http.MethodPost:
		var n compute.Node
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		// v7.0.0-alpha.19 #245 Q3 — operator-spec'd save-time probe for
		// all kinds. Bypass with ?probe=skip for emergency saves (e.g.
		// when the operator KNOWS the node is currently unreachable but
		// wants to persist the entry).
		if r.URL.Query().Get("probe") != "skip" {
			pctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			if perr := compute.Probe(pctx, &n, s.clusterLookup()); perr != nil {
				http.Error(w, "probe failed (use ?probe=skip to save anyway): "+perr.Error(), http.StatusBadGateway)
				return
			}
		}
		if err := s.computeReg.Add(&n); err != nil {
			code := http.StatusBadRequest
			if err == compute.ErrConflict {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditCompute(n.Name, "compute_node_add")
		writeJSONOK(w, map[string]any{"name": n.Name, "ok": true})

	// v7.0.0-alpha.23 (Q6) — operator on/off toggle for ComputeNodes,
	// matching the LLM enable/disable contract. Body: {"enabled": bool}.
	// Disabling is unconditional; enabling clears any LastDispatchError.
	case strings.HasSuffix(rest, "/enabled") && (r.Method == http.MethodPatch || r.Method == http.MethodPost):
		name := strings.TrimSuffix(rest, "/enabled")
		var body struct{ Enabled bool `json:"enabled"` }
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
		name := strings.TrimSuffix(rest, "/observer-peer")
		var body struct{ Peer string `json:"peer"` }
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
		if s.peerRegistry != nil {
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
		name := strings.TrimSuffix(rest, "/health")
		s.handleComputeNodeHealth(w, r, name)

	case strings.HasSuffix(rest, "/detail") && r.Method == http.MethodGet:
		name := strings.TrimSuffix(rest, "/detail")
		s.handleComputeNodeDetail(w, r, name)

	// v7.0.0-alpha.18 #242 — list models available on this Node for the
	// requested LLM kind. Used by PWA's kind-aware model dropdown so
	// operators don't have to know exact model names.
	case strings.HasSuffix(rest, "/models") && r.Method == http.MethodGet:
		name := strings.TrimSuffix(rest, "/models")
		s.handleComputeNodeModels(w, r, name)

	// alpha.33 #244 — start a background pull on this node.
	case strings.HasSuffix(rest, "/models/pull") && r.Method == http.MethodPost:
		name := strings.TrimSuffix(rest, "/models/pull")
		s.handleComputeNodeModelPull(w, r, name)

	// alpha.33 #244 — DELETE a model variant from this node.
	// Path: /api/compute/nodes/<n>/models/<model-name-or-tag>
	case strings.Contains(rest, "/models/") && r.Method == http.MethodDelete:
		idx := strings.Index(rest, "/models/")
		name := rest[:idx]
		model := rest[idx+len("/models/"):]
		s.handleComputeNodeModelDelete(w, r, name, model)

	case rest != "" && r.Method == http.MethodGet:
		n, err := s.computeReg.Get(rest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONOK(w, n)

	case rest != "" && r.Method == http.MethodPut:
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
			pctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
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
		s.auditCompute(n.Name, "compute_node_update")
		writeJSONOK(w, map[string]any{"name": n.Name, "ok": true})

	case rest != "" && r.Method == http.MethodDelete:
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
		"name":              n.Name,
		"kind":              n.Kind,
		"in_maintenance":    n.InMaintenance(time.Now().UTC()),
		"declared_capacity": n.DeclaredCapacity,
		"scheduling_priority": n.SchedulingPriority,
		"address":           n.Address,
		"auto_created":      n.AutoCreated,
	}
	writeJSONOK(w, out)
}

// handleComputeNodeDetail performs the on-demand pull (ASK 24 hybrid)
// to the Node's monitoring sidecar (--listen address). Operator clicks
// "live process detail" in Automata UI; daemon hits stub:9001/api/stats
// once and returns the snapshot.
func (s *Server) handleComputeNodeDetail(w http.ResponseWriter, r *http.Request, name string) {
	n, err := s.computeReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// When MonitoringEndpoint is empty, fall back to the last cached
	// snapshot from the peer registry (observer pushing stats without a
	// pull endpoint configured — common when the observer uses a non-
	// default port or is behind a firewall).
	if n.MonitoringEndpoint == "" {
		if s.peerRegistry != nil {
			if snap := s.peerRegistry.LastPayload(name); snap != nil {
				writeJSONOK(w, snap)
				return
			}
		}
		http.Error(w, "compute node has no monitoring_endpoint configured and no cached snapshot (set the stub --listen address or wait for a push)", http.StatusServiceUnavailable)
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
	defer resp.Body.Close()
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
	parser := func([]byte) []string { return nil }
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
	defer resp.Body.Close()
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
