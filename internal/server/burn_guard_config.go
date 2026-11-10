// Server side of the usage.burn_guard config keys: PUT /api/config validation,
// the applyConfigPatch cases, and GET/PUT /api/usage/burn-guard/config, which
// reuses the config set path (handlePutConfig) so validation, save and audit are
// the same as every other setting. The rules live in internal/config/burn_guard.go.
// The effort ceiling has its own endpoints and is not settable here.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const burnGuardKeyPrefix = "usage.burn_guard."

// burnGuardKeys is every settable usage.burn_guard key.
var burnGuardKeys = []string{
	"enabled", "interval_minutes", "margin_points", "ladder", "step_back",
	"cache_write_points_per_million", "cache_read_points_per_million",
	"refit_on_reading", "retry_limit_claude",
	"ask_on_change", "max_projection_gap_points",
}

func burnGuardPatchBool(key string, v interface{}) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "1", "on":
			return true, nil
		case "false", "no", "0", "off":
			return false, nil
		}
	}
	return false, fmt.Errorf("usage.burn_guard.%s: must be true or false", key)
}

func burnGuardPatchNumber(key string, v interface{}) (float64, error) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case string:
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%g", &f); err != nil {
			return 0, fmt.Errorf("usage.burn_guard.%s: must be a number", key)
		}
	default:
		return 0, fmt.Errorf("usage.burn_guard.%s: must be a number", key)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("usage.burn_guard.%s: must be a finite number", key)
	}
	return f, nil
}

func burnGuardPatchLadder(v interface{}) ([]string, error) {
	var names []string
	if arr, ok := toStringArray(v); ok {
		names = arr
	} else if str, ok := v.(string); ok {
		names = splitCSV(str)
	} else {
		return nil, fmt.Errorf("usage.burn_guard.ladder: must be a list of rungs")
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("usage.burn_guard.ladder: at least one rung is required (allowed: %s)", strings.Join(config.BurnGuardRungs(), ", "))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if err := config.ValidateBurnGuardRung(n); err != nil {
			return nil, err
		}
		n = strings.ToLower(strings.TrimSpace(n))
		if seen[n] {
			return nil, fmt.Errorf("usage.burn_guard.ladder: %q repeated", n)
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

// validateBurnGuardValue checks one key's patch value.
func validateBurnGuardValue(key string, v interface{}) error {
	switch key {
	case "enabled", "step_back", "refit_on_reading", "ask_on_change":
		_, err := burnGuardPatchBool(key, v)
		return err
	case "ladder":
		_, err := burnGuardPatchLadder(v)
		return err
	}
	f, err := burnGuardPatchNumber(key, v)
	if err != nil {
		return err
	}
	switch key {
	case "interval_minutes":
		if f != math.Trunc(f) || f < 1 || f > config.BurnGuardIntervalMinutesMax {
			return fmt.Errorf("usage.burn_guard.interval_minutes: must be a whole number from 1 to %d", config.BurnGuardIntervalMinutesMax)
		}
	case "margin_points":
		if f < 0 || f > config.BurnGuardMarginPointsMax {
			return fmt.Errorf("usage.burn_guard.margin_points: must be from 0 to %g", config.BurnGuardMarginPointsMax)
		}
	case "cache_write_points_per_million", "cache_read_points_per_million":
		if f <= 0 || f > config.BurnGuardPointsPerMillionMax {
			return fmt.Errorf("usage.burn_guard.%s: must be above 0 and at most %g", key, config.BurnGuardPointsPerMillionMax)
		}
	case "max_projection_gap_points":
		return config.ValidateMaxProjectionGapPoints(f)
	case "retry_limit_claude":
		if f != math.Trunc(f) || f < 0 || f > config.BurnGuardRetryLimitClaudeMax {
			return fmt.Errorf("usage.burn_guard.retry_limit_claude: must be a whole number from 0 to %d", config.BurnGuardRetryLimitClaudeMax)
		}
	}
	return nil
}

// validateBurnGuardPatch checks every usage.burn_guard.* key in a PUT /api/config
// body before anything is applied. An unknown usage.burn_guard.* key is rejected.
func validateBurnGuardPatch(patch map[string]interface{}) error {
	for k, v := range patch {
		if !strings.HasPrefix(k, burnGuardKeyPrefix) {
			continue
		}
		key := strings.TrimPrefix(k, burnGuardKeyPrefix)
		known := false
		for _, kk := range burnGuardKeys {
			if kk == key {
				known = true
			}
		}
		if !known {
			if key == "effort_ceiling" || strings.HasPrefix(key, "effort_ceiling.") {
				return fmt.Errorf("usage.burn_guard.effort_ceiling: set it with the automation effort-ceiling tools, not here")
			}
			return fmt.Errorf("%s: unknown key (allowed: %s)", k, strings.Join(burnGuardKeys, ", "))
		}
		if err := validateBurnGuardValue(key, v); err != nil {
			return err
		}
	}
	return nil
}

// applyBurnGuardPatchKey stores one validated usage.burn_guard.* key; a value that
// fails validation is left unchanged.
func applyBurnGuardPatchKey(c *config.BurnGuardConfig, fullKey string, v interface{}) {
	key := strings.TrimPrefix(fullKey, burnGuardKeyPrefix)
	if validateBurnGuardValue(key, v) != nil {
		return
	}
	switch key {
	case "enabled":
		c.Enabled, _ = burnGuardPatchBool(key, v)
	case "step_back":
		c.StepBack, _ = burnGuardPatchBool(key, v)
	case "refit_on_reading":
		c.RefitOnReading, _ = burnGuardPatchBool(key, v)
	case "ask_on_change":
		c.AskOnChange, _ = burnGuardPatchBool(key, v)
	case "ladder":
		c.Ladder, _ = burnGuardPatchLadder(v)
	case "interval_minutes":
		f, _ := burnGuardPatchNumber(key, v)
		c.IntervalMinutes = int(f)
	case "margin_points":
		c.MarginPoints, _ = burnGuardPatchNumber(key, v)
	case "cache_write_points_per_million":
		c.CacheWritePointsPerMillion, _ = burnGuardPatchNumber(key, v)
	case "cache_read_points_per_million":
		c.CacheReadPointsPerMillion, _ = burnGuardPatchNumber(key, v)
	case "max_projection_gap_points":
		c.MaxProjectionGapPoints, _ = burnGuardPatchNumber(key, v)
	case "retry_limit_claude":
		f, _ := burnGuardPatchNumber(key, v)
		c.RetryLimitClaude = int(f)
	}
}

// handleBurnGuardConfig serves GET and PUT /api/usage/burn-guard/config.
// GET (usage:read) returns the live usage.burn_guard block. PUT (usage:write) takes a
// flat body of bare keys ({"margin_points": 3, "ladder": ["lower_inflight"]}) and
// sends it through the config set path as usage.burn_guard.<key>, so one invalid
// value rejects the whole body with a 400 and nothing is stored.
func (s *Server) handleBurnGuardConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, usageCapabilityRead) {
			return
		}
		cfg := config.DefaultBurnGuard()
		if s.cfg != nil {
			cfg = s.cfg.Usage.BurnGuard
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	case http.MethodPut:
		if !s.fedCap(w, r, usageCapabilityWrite) {
			return
		}
		var in map[string]interface{}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil || len(in) == 0 {
			http.Error(w, "a body of burn guard settings is required", http.StatusBadRequest)
			return
		}
		patch := make(map[string]interface{}, len(in))
		for k, v := range in {
			patch[burnGuardKeyPrefix+strings.TrimPrefix(k, burnGuardKeyPrefix)] = v
		}
		body, _ := json.Marshal(patch)
		inner := httptest.NewRecorder()
		req := r.Clone(r.Context())
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		s.handlePutConfig(inner, req)
		if inner.Code >= 400 {
			http.Error(w, strings.TrimSpace(inner.Body.String()), inner.Code)
			return
		}
		cfg := s.cfg.Usage.BurnGuard
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "config": cfg})
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBurnGuardRun serves POST /api/usage/burn-guard/run (usage:write): evaluate now.
// It runs one tick and returns the decision with the refreshed status. When the guard
// is not wired it answers 503.
func (s *Server) handleBurnGuardRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, usageCapabilityWrite) {
		return
	}
	g := s.burnGuard
	if g == nil {
		http.Error(w, "burn guard is not running on this daemon", http.StatusServiceUnavailable)
		return
	}
	d := g.Tick(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"decision": d, "status": g.Status()})
}
