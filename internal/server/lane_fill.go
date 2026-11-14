package server

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// laneFillKeyPrefix is the config path every autonomous.lane_fill.* key shares.
const laneFillKeyPrefix = "autonomous.lane_fill."

// The decision history GET /api/autonomous/lane-fill returns when limit is
// absent, and the largest limit it accepts.
const (
	laneFillDefaultLimit = 50
	laneFillMaxLimit     = 1000
)

// handleAutonomousLaneFill serves GET /api/autonomous/lane-fill: the lane fill
// status with up to limit decisions, newest first.
func (s *Server) handleAutonomousLaneFill(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	limit, err := laneFillLimit(r.URL.Query())
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONOK(w, s.autonomousMgr.LaneFillStatus(limit))
}

// laneFillLimit reads the limit query value. An absent value means the
// default; anything other than a whole number from 1 to the maximum is refused.
func laneFillLimit(q url.Values) (int, error) {
	if !q.Has("limit") {
		return laneFillDefaultLimit, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(q.Get("limit")))
	if err != nil || n < 1 || n > laneFillMaxLimit {
		return 0, fmt.Errorf("limit must be a whole number from 1 to %d", laneFillMaxLimit)
	}
	return n, nil
}

// unknownLanePreferenceMsg is the refusal for a lane preference other than
// gpu, claude or any.
func unknownLanePreferenceMsg(pref string) string {
	return "unknown lane preference " + strconv.Quote(strings.TrimSpace(pref)) + ": use gpu, claude or any"
}

// validateLaneFillValue checks one autonomous.lane_fill.<field> value. The mode
// must be off, shadow or on; the lane counts, the idle alert minutes, the
// decompose-ahead minimum and the poll interval are whole numbers >= 0; the
// backends and the model are strings; the verifier-wait rule and the four
// eligibility switches are booleans; gpu_mode and claude_mode take the mode
// values or empty; stale_after_hours, host_down_after_minutes and
// stalled_merge_minutes are whole numbers >= 0.
// A field this does not know is not checked here.
func validateLaneFillValue(field string, v any) error {
	switch field {
	case "mode":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%smode: must be \"off\", \"shadow\" or \"on\"", laneFillKeyPrefix)
		}
		return config.ValidateLaneFillMode(str)
	case "gpu_mode", "claude_mode":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s%s: must be \"off\", \"shadow\" or \"on\" (empty inherits mode)", laneFillKeyPrefix, field)
		}
		return config.ValidateLaneFillKindMode(field, str)
	case "count_verifier_wait", "require_confirmed_proposal", "check_main_carries",
		"require_worktree", "lane_counts_from_caps":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s%s: must be true or false", laneFillKeyPrefix, field)
		}
	case "stale_after_hours":
		if !isWholeNonNegative(v) {
			return fmt.Errorf("%sstale_after_hours: must be a whole number >= 0 (0 turns the stale rule off)", laneFillKeyPrefix)
		}
	case "host_down_after_minutes":
		if !isWholeNonNegative(v) {
			return fmt.Errorf("%shost_down_after_minutes: must be a whole number >= 0 (0 never treats an unreachable host as down)", laneFillKeyPrefix)
		}
	case "stalled_merge_minutes":
		return config.ValidateLaneFillStalledMergeMinutes(v)
	case "gpu_lanes", "claude_lanes", "idle_alert_minutes", "decompose_ahead_min", "poll_seconds":
		if !isWholeNonNegative(v) {
			return fmt.Errorf("%s%s: must be a whole number >= 0", laneFillKeyPrefix, field)
		}
	case "gpu_backend", "gpu_model", "claude_backend":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s%s: must be a string", laneFillKeyPrefix, field)
		}
	}
	return nil
}

// isWholeNonNegative reports whether a decoded JSON number is a whole number
// >= 0.
func isWholeNonNegative(v any) bool {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	return ok && f == math.Trunc(f) && f >= 0
}

// applyLaneFillValue stores one autonomous.lane_fill.<field> value. A value
// that fails the check is left unchanged; PUT /api/config has already refused
// it by then.
func applyLaneFillValue(c *config.AutonomousLaneFillConfig, field string, v any) {
	if field == "mode" {
		if str, ok := v.(string); ok && config.ValidateLaneFillMode(str) == nil {
			c.Mode = str
		}
		return
	}
	if field == "gpu_mode" || field == "claude_mode" {
		if str, ok := v.(string); ok && config.ValidateLaneFillKindMode(field, str) == nil {
			norm := strings.ToLower(strings.TrimSpace(str))
			if field == "gpu_mode" {
				c.GPUMode = norm
			} else {
				c.ClaudeMode = norm
			}
		}
		return
	}
	if b, ok := v.(bool); ok {
		switch field {
		case "count_verifier_wait":
			c.CountVerifierWait = &b
		case "require_confirmed_proposal":
			c.RequireConfirmedProposal = &b
		case "check_main_carries":
			c.CheckMainCarries = &b
		case "require_worktree":
			c.RequireWorktree = &b
		case "lane_counts_from_caps":
			c.LaneCountsFromCaps = &b
		}
		return
	}
	if str, ok := v.(string); ok {
		switch field {
		case "gpu_backend":
			c.GPUBackend = str
		case "gpu_model":
			c.GPUModel = str
		case "claude_backend":
			c.ClaudeBackend = str
		}
		return
	}
	n, ok := toInt(v)
	if !ok || n < 0 {
		return
	}
	switch field {
	case "gpu_lanes":
		c.GPULanes = n
	case "claude_lanes":
		c.ClaudeLanes = n
	case "idle_alert_minutes":
		c.IdleAlertMinutes = n
	case "decompose_ahead_min":
		c.DecomposeAheadMin = n
	case "poll_seconds":
		c.PollSeconds = n
	case "stale_after_hours":
		c.StaleAfterHours = &n
	case "host_down_after_minutes":
		c.HostDownAfterMinutes = &n
	case "stalled_merge_minutes":
		c.StalledMergeMinutes = &n
	}
}

// laneFillConfigSurface is the autonomous.lane_fill block GET /api/config
// serves: the values in effect, so an unset field shows its default.
func laneFillConfigSurface(c config.AutonomousLaneFillConfig) map[string]interface{} {
	return map[string]interface{}{
		"mode":                c.EffectiveMode(),
		"gpu_lanes":           c.EffectiveGPULanes(),
		"claude_lanes":        c.EffectiveClaudeLanes(),
		"idle_alert_minutes":  c.EffectiveIdleAlertMinutes(),
		"decompose_ahead_min": c.EffectiveDecomposeAheadMin(),
		"poll_seconds":        c.EffectivePollSeconds(),
		"gpu_backend":         c.EffectiveGPUBackend(),
		"gpu_model":           c.GPUModel,
		"claude_backend":      c.EffectiveClaudeBackend(),
		"count_verifier_wait": c.EffectiveCountVerifierWait(),

		"stale_after_hours":          c.EffectiveStaleAfterHours(),
		"host_down_after_minutes":    c.EffectiveHostDownAfterMinutes(),
		"stalled_merge_minutes":      c.EffectiveStalledMergeMinutes(),
		"require_confirmed_proposal": c.EffectiveRequireConfirmedProposal(),
		"check_main_carries":         c.EffectiveCheckMainCarries(),
		"require_worktree":           c.EffectiveRequireWorktree(),
		"lane_counts_from_caps":      c.EffectiveLaneCountsFromCaps(),
		"gpu_mode":                   c.GPUMode,
		"claude_mode":                c.ClaudeMode,
	}
}

// LaneFillRESTOperation is one lane fill REST route, read by the parity test.
type LaneFillRESTOperation struct {
	Name   string
	Method string
	Path   string
}

// LaneFillRESTOperations is the single list of lane fill REST operations.
func LaneFillRESTOperations() []LaneFillRESTOperation {
	return []LaneFillRESTOperation{
		{"status", http.MethodGet, "/api/autonomous/lane-fill"},
		{"set_lane_preference", http.MethodPost, "/api/autonomous/prds/{id}/set_lane_preference"},
	}
}
