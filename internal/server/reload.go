// BL17 — hot config reload (POST /api/reload + SIGHUP).
//
// Re-reads the config file and re-applies the subset of fields that
// can safely change without a daemon restart: session.schedule_settle_ms,
// session.mcp_max_retries, session.auto_git_commit, session.tail_lines,
// session.alert_context_lines.
//
// Fields that cannot hot-reload (server.host/port, signal.account_number,
// mcp.sse_host/port, agents.*, database settings) are ignored with a
// "requires restart" note in the response.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/config"
)

// ReloadResult describes what reload did.
type ReloadResult struct {
	OK              bool     `json:"ok"`
	Applied         []string `json:"applied,omitempty"`
	RequiresRestart []string `json:"requires_restart,omitempty"`
	Error           string   `json:"error,omitempty"`
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res := s.reload()
	w.Header().Set("Content-Type", "application/json")
	if !res.OK {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_ = json.NewEncoder(w).Encode(res)
}

// Reload is the public entry-point for SIGHUP and ops tooling.
func (s *Server) Reload() ReloadResult {
	return s.reload()
}

// reload re-reads config from disk and applies the hot-reloadable
// subset. Returns a ReloadResult suitable for JSON or caller logging.
func (s *Server) reload() ReloadResult {
	if s.cfgPath == "" || s.cfg == nil {
		return ReloadResult{Error: "config not loaded from a file"}
	}

	newCfg, err := config.Load(s.cfgPath)
	if err != nil {
		return ReloadResult{Error: "load: " + err.Error()}
	}

	res := ReloadResult{OK: true}

	// Session — hot-reloadable subset.
	if s.manager != nil {
		if newCfg.Session.ScheduleSettleMs != s.cfg.Session.ScheduleSettleMs {
			s.manager.SetScheduleSettleMs(newCfg.Session.ScheduleSettleMs)
			res.Applied = append(res.Applied, "session.schedule_settle_ms")
		}
		if newCfg.Session.MCPMaxRetries != s.cfg.Session.MCPMaxRetries {
			s.manager.SetMCPMaxRetries(newCfg.Session.MCPMaxRetries)
			res.Applied = append(res.Applied, "session.mcp_max_retries")
		}
	}

	// Swap config pointer contents — a shallow copy of the loaded
	// struct so subsequent reads reflect the new values for
	// inspection-only fields. Handlers that cache should refresh.
	*s.cfg = *newCfg

	// Fields we don't attempt to hot-apply (operator must restart).
	res.RequiresRestart = []string{
		"server.host", "server.port",
		"signal.account_number", "signal.group_id",
		"mcp.sse_host", "mcp.sse_port",
		"database.*", "agents.*",
	}

	return res
}
