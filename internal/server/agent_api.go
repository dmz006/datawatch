// F10 sprint 3 agent REST handlers.
//
// /api/agents                     GET  list, POST spawn
// /api/agents/{id}                GET  status, DELETE terminate
// /api/agents/{id}/logs           GET  last N lines
// /api/agents/bootstrap           POST  worker calls this on startup
//
// The bootstrap endpoint is the one piece of unauthenticated surface
// — the worker can't know the browser/session token yet. Instead it
// presents the single-use bootstrap token + agent ID from its env,
// which the Manager mints at spawn and burns on first use.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/agents"
)

// SetAgentManager wires the agent manager for the /api/agents routes.
// Pass nil in tests to keep handlers in 503 mode.
func (s *Server) SetAgentManager(m *agents.Manager) { s.agentMgr = m }

// handleAgents dispatches /api/agents (collection) and
// /api/agents/{id}[/logs] (named resource). Path parsing mirrors the
// profile handlers.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if s.agentMgr == nil {
		http.Error(w, "agent manager not available", http.StatusServiceUnavailable)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/agents")
	tail = strings.Trim(tail, "/")

	// Collection
	if tail == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"agents": s.agentMgr.List(),
			})
		case http.MethodPost:
			s.spawnAgent(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.SplitN(tail, "/", 2)
	id := parts[0]

	// /api/agents/{id}/logs
	if len(parts) == 2 && parts[1] == "logs" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.agentLogs(w, r, id)
		return
	}
	// /api/agents/{id}/result — F10 S7.2 fan-in
	if len(parts) == 2 && parts[1] == "result" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.agentRecordResult(w, r, id)
		return
	}
	if len(parts) > 1 {
		http.Error(w, "unknown subpath", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getAgent(w, r, id)
	case http.MethodDelete:
		s.terminateAgent(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) spawnAgent(w http.ResponseWriter, r *http.Request) {
	var req agents.SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	a, err := s.agentMgr.Spawn(r.Context(), req)
	if err != nil {
		// Agent record is still useful to the UI even on failure.
		status := http.StatusInternalServerError
		msg := err.Error()
		switch {
		case strings.Contains(msg, "project profile") && strings.Contains(msg, "not found"):
			status = http.StatusNotFound
		case strings.Contains(msg, "cluster profile") && strings.Contains(msg, "not found"):
			status = http.StatusNotFound
		case strings.Contains(msg, "invalid project profile"),
			strings.Contains(msg, "invalid cluster profile"):
			status = http.StatusBadRequest
		case strings.Contains(msg, "no driver registered"):
			status = http.StatusBadRequest
		}
		// If we have an agent record (driver failed after Spawn
		// started tracking), return it with 500 so the UI shows
		// the failure reason.
		if a != nil {
			writeJSON(w, status, map[string]interface{}{
				"error": msg,
				"agent": a,
			})
			return
		}
		http.Error(w, msg, status)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) getAgent(w http.ResponseWriter, _ *http.Request, id string) {
	a := s.agentMgr.Get(id)
	if a == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) terminateAgent(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.agentMgr.Terminate(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentLogs(w http.ResponseWriter, r *http.Request, id string) {
	lines := 200
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 10000 {
			lines = n
		}
	}
	out, err := s.agentMgr.Logs(r.Context(), id, lines)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(out))
}

// agentRecordResult handles POST /api/agents/{id}/result. The
// worker posts a structured payload of its session output (status,
// summary, artifacts) on session-end / task-complete; the parent's
// orchestrator merges these into the parent session's context (S7.1).
func (s *Server) agentRecordResult(w http.ResponseWriter, r *http.Request, id string) {
	if s.agentMgr == nil {
		http.Error(w, "agent manager not available", http.StatusServiceUnavailable)
		return
	}
	var body agents.AgentResult
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if err := s.agentMgr.RecordResult(id, &body); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ── Bootstrap ──────────────────────────────────────────────────────────

// BootstrapRequest is what a worker POSTs on startup. Keys kept in
// snake_case for consistency with the rest of the datawatch API.
type BootstrapRequest struct {
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
}

// BootstrapResponse is what the worker receives on success. Future
// sprints will stuff more into this: memory conn (S6), worker
// identity cert (S7).
type BootstrapResponse struct {
	AgentID        string            `json:"agent_id"`
	ProjectProfile string            `json:"project_profile"`
	ClusterProfile string            `json:"cluster_profile"`
	Task           string            `json:"task,omitempty"`
	// Git carries the parent-minted short-lived token + repo coords
	// the worker uses for clone + push (F10 S5.3). Token is empty
	// for read-only / no-token-broker setups.
	Git BootstrapGit `json:"git,omitempty"`
	// Memory tells the worker which federation mode + namespace its
	// Project Profile selected (F10 S6.2). Empty mode means the
	// worker uses purely local memory.
	Memory BootstrapMemory `json:"memory,omitempty"`
	// Comm lists the parent's outbound channel names this worker
	// should route alerts through (F10 S7.7). Empty = no inheritance.
	Comm BootstrapComm `json:"comm,omitempty"`
	// Env the worker should set before starting its own daemon.
	// Includes everything the agent/sidecar images need to self-
	// configure: workspace root, memory URL, etc.
	Env map[string]string `json:"env"`
}

// BootstrapGit is the git-clone bundle delivered to the worker on
// bootstrap. Token is sensitive — never logged, never echoed in any
// /api/agents snapshot (only ever in the bootstrap response body
// over the worker's pinned TLS connection).
type BootstrapGit struct {
	URL      string `json:"url,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Token    string `json:"token,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// BootstrapMemory is the memory federation bundle (F10 S6.2).
// Tells the worker which mode it's running in and which namespace
// it owns under the parent's memory store. The parent's reachable
// URL the worker uses for /api/memory/{save,search,import} is
// already in DATAWATCH_BOOTSTRAP_URL so we don't repeat it here.
type BootstrapMemory struct {
	// Mode is one of "shared", "sync-back", "ephemeral", or empty
	// (worker uses local memory only — pre-F10 default).
	Mode string `json:"mode,omitempty"`
	// Namespace is the per-Project-Profile bucket under which the
	// worker's writes are stored on the parent. Workers in shared /
	// sync-back mode tag every Save with this value.
	Namespace string `json:"namespace,omitempty"`
}

// BootstrapComm is the comm-channel inheritance list (F10 S7.7).
// Empty = worker uses no outbound comms. Each entry names a parent-
// configured messaging backend the worker should route its alerts
// through; the parent surfaces the actual credential material via
// its existing per-channel config endpoints (the worker proxies
// alert sends to the parent rather than holding credentials itself).
type BootstrapComm struct {
	Channels []string `json:"channels,omitempty"`
}

// handleAgentCAPEM serves the parent's TLS certificate as a PEM blob,
// useful for projecting into worker Pods (Sprint 4 trusted_cas) or
// fetching during operator setup (`curl … > /etc/ssl/parent.pem`).
//
// Unauthenticated and read-only — the cert itself is public anyway,
// the file just delivers it conveniently. 404 when TLS isn't enabled
// or the cert path isn't readable.
//
// Route: GET /api/agents/ca.pem
func (s *Server) handleAgentCAPEM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil || !s.cfg.Server.TLSEnabled || s.cfg.Server.TLSCert == "" {
		http.Error(w, "TLS not enabled — no certificate to serve", http.StatusNotFound)
		return
	}
	pem, err := os.ReadFile(s.cfg.Server.TLSCert)
	if err != nil {
		http.Error(w, "cert unreadable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	_, _ = w.Write(pem)
}

// handleAgentBootstrap is the ONE unauthenticated endpoint the worker
// calls. Validated strictly: token must match, agent must be in the
// starting state, single-use.
func (s *Server) handleAgentBootstrap(w http.ResponseWriter, r *http.Request) {
	if s.agentMgr == nil {
		http.Error(w, "agent manager not available", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if req.AgentID == "" || req.Token == "" {
		http.Error(w, "agent_id and token are required", http.StatusBadRequest)
		return
	}
	agent, err := s.agentMgr.ConsumeBootstrap(req.Token, req.AgentID)
	if err != nil {
		// Unauthorized is the safer default for bootstrap failures —
		// a legit worker with a good token never sees 401.
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// The worker needs: its ID, its profile names, its task, and a
	// bag of env-vars to inject into its own session manager. Keep
	// the env minimal for now; later sprints add git tokens, memory
	// connections, identity certs, etc.
	resp := BootstrapResponse{
		AgentID:        agent.ID,
		ProjectProfile: agent.ProjectProfile,
		ClusterProfile: agent.ClusterProfile,
		Task:           agent.Task,
		Env: map[string]string{
			"DATAWATCH_AGENT_ID": agent.ID,
		},
	}

	// F10 S5.3 — git bundle. Worker uses these to clone its repo at
	// session start and push back via the token. Token only travels
	// in this single response over the worker's pinned TLS connection;
	// never logged, never re-served via /api/agents.
	if proj := s.agentMgr.GetProjectFor(agent.ID); proj != nil {
		if proj.Git.URL != "" {
			resp.Git = BootstrapGit{
				URL:      proj.Git.URL,
				Branch:   proj.Git.Branch,
				Provider: proj.Git.Provider,
				Token:    s.agentMgr.GetGitTokenFor(agent.ID),
			}
		}
		// F10 S6.2 — memory federation bundle. Tells the worker
		// which namespace its writes land under + which mode (shared
		// / sync-back / ephemeral) the parent expects it to follow.
		// Mode = "" means no federation (worker uses local memory).
		if proj.Memory.Mode != "" {
			resp.Memory = BootstrapMemory{
				Mode:      string(proj.Memory.Mode),
				Namespace: proj.EffectiveNamespace(),
			}
		}
		// F10 S7.7 — comm-channel inheritance. Worker proxies alert
		// sends to the parent through these named channels (parent
		// holds the credentials, worker just identifies which
		// channels to use). Empty = no inheritance, default behaviour.
		if len(proj.CommInheritance) > 0 {
			resp.Comm = BootstrapComm{Channels: append([]string{}, proj.CommInheritance...)}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
