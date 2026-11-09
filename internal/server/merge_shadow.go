package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// mergeShadowKeyPrefix is the config path every autonomous.merge_shadow.*
// key shares.
const mergeShadowKeyPrefix = "autonomous.merge_shadow."

// validateMergeShadowValue checks one autonomous.merge_shadow.<field> value.
// enabled must be a boolean (or a "true"/"false" string, matching how comm
// channel scalars land here); gate_timeout must parse as a positive Go
// duration; plan_keep must be a whole number >= 1. A field this does not
// know is an error, so a rejected PUT names the bad key.
func validateMergeShadowValue(field string, v any) error {
	switch field {
	case "enabled":
		if _, ok := v.(bool); ok {
			return nil
		}
		if s, ok := v.(string); ok {
			if _, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
				return nil
			}
		}
		return fmt.Errorf("%senabled: must be true or false", mergeShadowKeyPrefix)
	case "gate_timeout":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%sgate_timeout: must be a duration string such as \"20m\"", mergeShadowKeyPrefix)
		}
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil || d <= 0 {
			return fmt.Errorf("%sgate_timeout: must parse as a positive duration (e.g. \"20m\")", mergeShadowKeyPrefix)
		}
	case "plan_keep":
		if !isWholePositive(v) {
			return fmt.Errorf("%splan_keep: must be a positive whole number", mergeShadowKeyPrefix)
		}
	default:
		return fmt.Errorf("%s%s: unknown key (use enabled, gate_timeout or plan_keep)", mergeShadowKeyPrefix, field)
	}
	return nil
}

// isWholePositive reports whether a decoded JSON number is a whole number
// >= 1.
func isWholePositive(v any) bool {
	f, ok := v.(float64)
	if i, isInt := v.(int); isInt {
		f, ok = float64(i), true
	}
	return ok && f == math.Trunc(f) && f >= 1
}

// applyMergeShadowValue stores one autonomous.merge_shadow.<field> value in
// the config block. A value that fails the check is left unchanged; PUT
// /api/config has already refused it by then.
func applyMergeShadowValue(c *config.MergeShadowConfig, field string, v any) {
	switch field {
	case "enabled":
		if b, ok := v.(bool); ok {
			c.Enabled = &b
			return
		}
		if s, ok := v.(string); ok {
			if b, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
				c.Enabled = &b
			}
		}
	case "gate_timeout":
		s, ok := v.(string)
		if !ok {
			return
		}
		s = strings.TrimSpace(s)
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			c.GateTimeout = &s
		}
	case "plan_keep":
		if n, ok := toInt(v); ok && n >= 1 {
			c.PlanKeep = &n
		}
	}
}

// mergeShadowConfigSurface is the autonomous.merge_shadow block GET
// /api/config serves: the values in effect, so an unset key shows its
// default.
func mergeShadowConfigSurface(c config.MergeShadowConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":      c.EffectiveEnabled(),
		"gate_timeout": c.EffectiveGateTimeout().String(),
		"plan_keep":    c.EffectivePlanKeep(),
	}
}
