// Branch refresh surfaces: the per-Automaton override route, the two daemon
// settings on the config PATCH path, and the refresh state on the Automaton
// GET payload.
//
//	POST /api/autonomous/prds/{id}/branch-refresh   {"enabled": true|false|null}
//	PUT  /api/config  autonomous.branch_refresh_enabled, autonomous.branch_refresh_behind_limit
//
// The override wins over the daemon setting: true refreshes the Automaton even
// when the daemon setting is off, false never refreshes it, null clears the
// override so the daemon setting applies again.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// Config keys of the branch refresh settings on PUT /api/config.
const (
	branchRefreshEnabledKey = "autonomous.branch_refresh_enabled"
	branchRefreshLimitKey   = "autonomous.branch_refresh_behind_limit"
)

// branchRefreshOverrideSetter is the optional autonomous-manager surface for
// the per-Automaton override. It is an optional interface (like
// planCoverageAckner) so AutonomousAPI fakes in server tests do not need to
// implement it; a handler that hits a fake without it answers 501.
type branchRefreshOverrideSetter interface {
	SetBranchRefreshOverride(id string, v *bool) error
}

// BranchRefreshRESTOperation is one branch refresh REST route, read by the
// parity test.
type BranchRefreshRESTOperation struct {
	Name   string
	Method string
	Path   string
}

// BranchRefreshRESTOperations is the single list of branch refresh REST
// operations.
func BranchRefreshRESTOperations() []BranchRefreshRESTOperation {
	return []BranchRefreshRESTOperation{
		{"set_override", http.MethodPost, "/api/autonomous/prds/{id}/branch-refresh"},
	}
}

// BranchRefreshConfigKeys lists the two config keys settable through
// PUT /api/config (and so the CLI, the comm channel and MCP config_set).
func BranchRefreshConfigKeys() []string {
	return []string{branchRefreshEnabledKey, branchRefreshLimitKey}
}

// handleBranchRefreshOverride serves POST .../prds/{id}/branch-refresh. The
// body must carry enabled: true, false or null (null clears the override).
// The reply is the updated Automaton with its refresh state.
func (s *Server) handleBranchRefreshOverride(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	setter, ok := s.autonomousMgr.(branchRefreshOverrideSetter)
	if !ok {
		http.Error(w, "branch refresh override not supported by this manager", http.StatusNotImplemented)
		return
	}
	var req struct {
		Enabled json.RawMessage `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	v, err := parseBranchRefreshEnabled(req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, exists := s.autonomousMgr.GetPRD(id); !exists {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := setter.SetBranchRefreshOverride(id, v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var detail map[string]any
	if v == nil {
		detail = map[string]any{"branch_refresh_override": nil}
	} else {
		detail = map[string]any{"branch_refresh_override": *v}
	}
	s.audit(r.Context(), "set_branch_refresh_override", "automaton", id, detail)
	updated, _ := s.autonomousMgr.GetPRD(id)
	writeJSONOK(w, s.withBranchRefresh(updated))
}

// parseBranchRefreshEnabled reads the enabled member: true, false or null.
// A missing member or any other value is refused with a message naming the
// three accepted values.
func parseBranchRefreshEnabled(raw json.RawMessage) (*bool, error) {
	const msg = "enabled: must be true, false or null (null clears the override so the daemon setting applies)"
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s", msg)
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%s", msg)
	}
	return &b, nil
}

// withBranchRefresh adds the refresh state to an Automaton payload:
// branch_refresh (the last refresh, null when none has run),
// branch_refresh_override (true, false or null) and branch_refresh_enabled,
// the setting in effect for this Automaton (override first, then the daemon
// setting). A payload that is not an object is returned as it is.
func (s *Server) withBranchRefresh(payload any) any {
	if isNilPRD(payload) {
		return payload
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil || doc == nil {
		return payload
	}
	if _, ok := doc["branch_refresh"]; !ok {
		doc["branch_refresh"] = nil
	}
	override, ok := doc["branch_refresh_override"]
	if !ok {
		doc["branch_refresh_override"] = nil
		override = nil
	}
	if ob, isBool := override.(bool); isBool {
		doc["branch_refresh_enabled"] = ob
	} else {
		doc["branch_refresh_enabled"] = s.branchRefreshDaemonEnabled()
	}
	return doc
}

// branchRefreshDaemonEnabled reads the daemon setting in effect: the live
// manager config when there is one, else the stored daemon config.
func (s *Server) branchRefreshDaemonEnabled() bool {
	if s.autonomousMgr != nil {
		switch c := s.autonomousMgr.Config().(type) {
		case autonomous.Config:
			return c.BranchRefreshEnabled
		case *autonomous.Config:
			if c != nil {
				return c.BranchRefreshEnabled
			}
		}
	}
	if s.cfg != nil {
		return s.cfg.Autonomous.BranchRefreshEnabled
	}
	return false
}

// validateBranchRefreshPatch checks the two branch refresh keys of a PUT
// /api/config patch before anything is applied: enabled must be a boolean
// (or the strings true/false), the behind limit a whole number of zero or
// more (0 reads as the default 5). A bad value refuses the whole PUT.
func validateBranchRefreshPatch(patch map[string]interface{}) error {
	if v, ok := patch[branchRefreshEnabledKey]; ok {
		if _, isBool := parseBoolValue(v); !isBool {
			return fmt.Errorf("%s: must be true or false", branchRefreshEnabledKey)
		}
	}
	if v, ok := patch[branchRefreshLimitKey]; ok {
		n, isInt := wholeNumberValue(v)
		if !isInt || n < 0 {
			return fmt.Errorf("%s: must be a whole number of 0 or more (0 reads as the default 5)", branchRefreshLimitKey)
		}
	}
	return nil
}

// parseBoolValue reads a JSON boolean or the strings true/false (the CLI and
// the comm channel send strings). ok is false for anything else.
func parseBoolValue(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch x {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// wholeNumberValue reads a JSON number or a numeric string (the comm channel
// sends strings) as a whole number. ok is false for anything else.
func wholeNumberValue(v interface{}) (int, bool) {
	if n, ok := toInt(v); ok {
		return n, true
	}
	if str, ok := v.(string); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(str)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// applyBranchRefreshConfigKey stores one branch refresh key on cfg. It
// reports whether key is one of the two; a value validateBranchRefreshPatch
// would refuse is left unapplied (applied false) so the caller can list it
// as skipped.
func applyBranchRefreshConfigKey(cfg *config.Config, key string, v interface{}) (handled, applied bool) {
	switch key {
	case branchRefreshEnabledKey:
		b, ok := parseBoolValue(v)
		if ok {
			cfg.Autonomous.BranchRefreshEnabled = b
		}
		return true, ok
	case branchRefreshLimitKey:
		n, ok := wholeNumberValue(v)
		if ok && n >= 0 {
			cfg.Autonomous.BranchRefreshBehindLimit = n
			return true, true
		}
		return true, false
	}
	return false, false
}
