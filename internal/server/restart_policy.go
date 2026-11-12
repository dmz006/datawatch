// GET /api/autonomous/restart-interruptions: what a daemon restart now would
// interrupt (running tasks and live sessions, with ids) and the configured
// restart policy. Read-only. The policy itself (autonomous.restart_policy,
// .restart_quiet_timeout_seconds, .restart_on_timeout) is applied by the
// daemon's restart function in cmd/datawatch.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

type restartInterruptionReader interface {
	RestartInterruptions() autonomous.RestartInterruption
}

// restartInterruptions returns the current report, and false when the
// autonomous manager is not available.
func (s *Server) restartInterruptions() (autonomous.RestartInterruption, bool) {
	if s.autonomousMgr == nil {
		return autonomous.RestartInterruption{}, false
	}
	rd, ok := s.autonomousMgr.(restartInterruptionReader)
	if !ok {
		return autonomous.RestartInterruption{}, false
	}
	return rd.RestartInterruptions(), true
}

// handleAutonomousRestartInterruptions serves the read.
func (s *Server) handleAutonomousRestartInterruptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	rep, ok := s.restartInterruptions()
	if !ok {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

// validateRestartPolicyPatch refuses a PUT whose restart-policy values are
// not recognised.
func validateRestartPolicyPatch(patch map[string]interface{}) error {
	if v, ok := patch["autonomous.restart_policy"]; ok {
		if str, isStr := v.(string); !isStr || !config.ValidRestartPolicy(str) {
			return errBadValue("autonomous.restart_policy", `must be "immediate", "wait_quiet" or "drain"`)
		}
	}
	if v, ok := patch["autonomous.planning_restart_policy"]; ok {
		if str, isStr := v.(string); !isStr || !config.ValidPlanningRestartPolicy(str) {
			return errBadValue("autonomous.planning_restart_policy", `must be "reattach", "restart" or "draft"`)
		}
	}
	if v, ok := patch["autonomous.restart_on_timeout"]; ok {
		if str, isStr := v.(string); !isStr || !config.ValidRestartOnTimeout(str) {
			return errBadValue("autonomous.restart_on_timeout", `must be "proceed" or "abort"`)
		}
	}
	if v, ok := patch["autonomous.restart_quiet_timeout_seconds"]; ok {
		if n, isInt := toInt(v); !isInt || n < 0 {
			return errBadValue("autonomous.restart_quiet_timeout_seconds", "must be a whole number of seconds, 0 or more")
		}
	}
	return nil
}

type badValueError struct{ key, msg string }

func (e badValueError) Error() string { return e.key + ": " + e.msg }

func errBadValue(key, msg string) error { return badValueError{key, msg} }

// applyRestartPolicyKey stores one already-validated restart-policy setting.
func applyRestartPolicyKey(cfg *config.Config, k string, v interface{}) {
	switch k {
	case "autonomous.restart_policy":
		if str, ok := v.(string); ok {
			cfg.Autonomous.RestartPolicy = config.NormalizeRestartPolicy(str)
		}
	case "autonomous.planning_restart_policy":
		if str, ok := v.(string); ok {
			cfg.Autonomous.PlanningRestartPolicy = config.NormalizePlanningRestartPolicy(str)
		}
	case "autonomous.restart_on_timeout":
		if str, ok := v.(string); ok {
			cfg.Autonomous.RestartOnTimeout = config.NormalizeRestartOnTimeout(str)
		}
	case "autonomous.restart_quiet_timeout_seconds":
		if n, ok := toInt(v); ok && n >= 0 {
			cfg.Autonomous.RestartQuietTimeoutSeconds = n
		}
	}
}
