// BL17 — hot config reload (POST /api/reload + SIGHUP).
//
// Re-reads the config file and re-applies the subset of fields that
// can safely change without a daemon restart: session.schedule_settle_ms,
// session.mcp_max_retries, session.auto_git_commit, session.tail_lines,
// session.alert_context_lines.
//
// v5.27.2 — extended with subsystem-specific reloads via the
// `?subsystem=<name>` query param + a SubsystemReloader plugin
// interface so the daemon can hot-reload plugins / comm channels /
// memory etc. without a full restart. Operator-asked: "explore
// other ways to reload service and enable services/plugins/etc
// without having to restart the entire server".
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
	Subsystem       string   `json:"subsystem,omitempty"`
	Applied         []string `json:"applied,omitempty"`
	RequiresRestart []string `json:"requires_restart,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// SubsystemReloader is the optional interface a subsystem can
// implement to participate in the per-subsystem reload path.
// Reloaders are registered via Server.RegisterReloader from main.go
// at startup so they don't have to be type-asserted at handler time.
type SubsystemReloader interface {
	Reload() error
}

// RegisterReloader (v5.27.2) wires a subsystem-specific reload entry
// point. Multiple registrations against the same name override the
// prior one (last-write-wins so main.go can re-register on hot
// re-init paths). Names are case-insensitive in lookup.
func (s *Server) RegisterReloader(name string, fn func() error) {
	if s.reloaders == nil {
		s.reloaders = map[string]func() error{}
	}
	s.reloaders[name] = fn
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subsystem := r.URL.Query().Get("subsystem")
	res := s.reloadSubsystem(subsystem)
	w.Header().Set("Content-Type", "application/json")
	if !res.OK {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_ = json.NewEncoder(w).Encode(res)
}

// Reload is the public entry-point for SIGHUP and ops tooling.
// Triggers the full hot-reload (no subsystem filter).
func (s *Server) Reload() ReloadResult {
	return s.reloadSubsystem("")
}

// ReloadSubsystem fires the registered reloader for `name`. Returns
// a ReloadResult — error when the name is unknown.
func (s *Server) ReloadSubsystem(name string) ReloadResult {
	return s.reloadSubsystem(name)
}

// reloadSubsystem dispatches to either the full reload (empty name)
// or the registered per-subsystem reloader.
func (s *Server) reloadSubsystem(name string) ReloadResult {
	if name == "" || name == "all" || name == "config" {
		return s.reload()
	}
	res := ReloadResult{Subsystem: name}
	fn, ok := s.reloaders[name]
	if !ok {
		res.Error = "unknown subsystem: " + name + " (registered: " + s.reloaderNames() + ")"
		return res
	}
	if err := fn(); err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	res.Applied = []string{name}
	return res
}

// reloaderNames returns a comma-separated list of registered
// subsystem names for error messages.
func (s *Server) reloaderNames() string {
	if len(s.reloaders) == 0 {
		return "(none)"
	}
	out := ""
	for k := range s.reloaders {
		if out != "" {
			out += ","
		}
		out += k
	}
	return out
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
		if newCfg.Session.DefaultEffort != s.cfg.Session.DefaultEffort {
			s.manager.SetDefaultEffort(newCfg.Session.DefaultEffort)
			res.Applied = append(res.Applied, "session.default_effort")
		}
		// v5.27.2 — claude_auto_accept_disclaimer is read at runtime
		// from cfg.Session so the *s.cfg = *newCfg below picks it up
		// without an explicit setter. Recording in Applied for
		// operator visibility when it actually changed.
		if newCfg.Session.ClaudeAutoAcceptDisclaimer != s.cfg.Session.ClaudeAutoAcceptDisclaimer {
			res.Applied = append(res.Applied, "session.claude_auto_accept_disclaimer")
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
