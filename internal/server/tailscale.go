// BL243 Phase 1+2+3 — REST handlers for the Tailscale k8s sidecar feature.
//
//   GET  /api/tailscale/status        — aggregated status + node list
//   GET  /api/tailscale/nodes         — raw node/device list
//   POST /api/tailscale/acl/push      — push ACL policy to headscale
//   POST /api/tailscale/acl/generate  — generate ACL policy (no push) (Phase 3)
//   POST /api/tailscale/auth/key      — generate headscale pre-auth key (Phase 2)

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/dmz006/datawatch/internal/tailscale"
)

// tailscaleClient is the narrow interface the REST handlers need.
type tailscaleClient interface {
	Status(ctx context.Context) (*tailscale.StatusResponse, error)
	Nodes(ctx context.Context) ([]tailscale.NodeInfo, error)
	PushACL(ctx context.Context, policy string) error
	GeneratePreAuthKey(ctx context.Context, opts tailscale.PreAuthKeyOptions) (*tailscale.PreAuthKeyResult, error)
	GenerateACLPolicy(ctx context.Context) (string, error)
	GenerateAndPushACL(ctx context.Context) (string, error)
}

// SetTailscaleClient wires the Tailscale client (called from main.go when
// tailscale.enabled=true).
func (s *Server) SetTailscaleClient(c *tailscale.Client) {
	s.tailscaleClient = c
}

func (s *Server) handleTailscaleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.tailscaleClient == nil {
		http.Error(w, `{"error":"tailscale not configured"}`, http.StatusServiceUnavailable)
		return
	}
	status, err := s.tailscaleClient.Status(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleTailscaleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.tailscaleClient == nil {
		http.Error(w, `{"error":"tailscale not configured"}`, http.StatusServiceUnavailable)
		return
	}
	nodes, err := s.tailscaleClient.Nodes(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"nodes": nodes})
}

func (s *Server) handleTailscaleACLPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.tailscaleClient == nil {
		http.Error(w, `{"error":"tailscale not configured"}`, http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// BL243 Phase 3: empty body → auto-generate from config then push.
	if len(body) == 0 {
		policy, err := s.tailscaleClient.GenerateAndPushACL(r.Context())
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "generated_policy": policy})
		return
	}

	// Accept {"policy":"..."} (from MCP/CLI) or raw HCL/JSON directly.
	policy := string(body)
	var wrapper struct {
		Policy string `json:"policy"`
	}
	if json.Unmarshal(body, &wrapper) == nil && wrapper.Policy != "" {
		policy = wrapper.Policy
	}
	if err := s.tailscaleClient.PushACL(r.Context(), policy); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// handleTailscaleACLGenerate — POST /api/tailscale/acl/generate
// Generates an ACL policy from the current config + live node list without pushing.
// BL243 Phase 3.
func (s *Server) handleTailscaleACLGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.tailscaleClient == nil {
		http.Error(w, `{"error":"tailscale not configured"}`, http.StatusServiceUnavailable)
		return
	}
	policy, err := s.tailscaleClient.GenerateACLPolicy(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"policy": policy})
}

// handleTailscaleAuthKey — POST /api/tailscale/auth/key
// Generates a new headscale pre-auth key (BL243 Phase 2).
// Body (all fields optional): {"reusable":false,"ephemeral":false,"tags":["tag:dw-agent"],"expiry_hours":24}
func (s *Server) handleTailscaleAuthKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.tailscaleClient == nil {
		http.Error(w, `{"error":"tailscale not configured"}`, http.StatusServiceUnavailable)
		return
	}
	var opts tailscale.PreAuthKeyOptions
	if body, err := io.ReadAll(r.Body); err == nil && len(body) > 0 {
		_ = json.Unmarshal(body, &opts)
	}
	result, err := s.tailscaleClient.GeneratePreAuthKey(r.Context(), opts)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
