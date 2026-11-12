// Server side of the usage.calculator config keys: GET /api/config display,
// PUT /api/config validation (a 400 on any invalid value, nothing applied) and
// the applyConfigPatch cases. The rules themselves live in
// internal/config/usage_calculator.go.

package server

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const usageCalculatorKeyPrefix = "usage.calculator."

// usageCalculatorKeys is every settable usage.calculator key.
var usageCalculatorKeys = []string{
	"variant", "policy", "safety_points", "cost_per_task_points", "baseline_per_24h",
	"max_lanes", "auto_lanes", "auto_lanes_max", "auto_lanes_special_priority",
	"reset_weekday", "reset_time", "reset_zone", "keeper",
}

// usageCalculatorPatchString reads a patch value that must be a string.
func usageCalculatorPatchString(key string, v interface{}) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("usage.calculator.%s: must be a string", key)
	}
	return s, nil
}

// usageCalculatorPatchNumber reads a finite number from a JSON number or a
// numeric string (the CLI and the comm channel may send either).
func usageCalculatorPatchNumber(key string, v interface{}) (float64, error) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("usage.calculator.%s: must be a number", key)
		}
		f = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, fmt.Errorf("usage.calculator.%s: must be a number", key)
		}
		f = parsed
	default:
		return 0, fmt.Errorf("usage.calculator.%s: must be a number", key)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("usage.calculator.%s: must be a finite number", key)
	}
	return f, nil
}

// usageCalculatorPatchLanes reads max_lanes: a whole number.
func usageCalculatorPatchLanes(v interface{}) (int, error) {
	f, err := usageCalculatorPatchNumber("max_lanes", v)
	if err != nil {
		return 0, err
	}
	if f != math.Trunc(f) || f < math.MinInt32 || f > math.MaxInt32 {
		return 0, fmt.Errorf("usage.calculator.max_lanes: must be a whole number from 1 to %d", config.UsageMaxLanesMax)
	}
	n := int(f)
	return n, config.ValidateUsageCalculatorMaxLanes(n)
}

// usageCalculatorPatchAutoLanesMax reads auto_lanes_max: a whole number 1 to 8.
func usageCalculatorPatchAutoLanesMax(v interface{}) (int, error) {
	f, err := usageCalculatorPatchNumber("auto_lanes_max", v)
	if err != nil {
		return 0, err
	}
	if f != math.Trunc(f) || f < math.MinInt32 || f > math.MaxInt32 {
		return 0, fmt.Errorf("usage.calculator.auto_lanes_max: must be a whole number from 1 to %d", config.UsageMaxLanesMax)
	}
	n := int(f)
	return n, config.ValidateUsageCalculatorAutoLanesMax(n)
}

// usageCalculatorPatchAutoLanesSpecialPriority reads
// auto_lanes_special_priority: a whole number 0 or above (0 lets every open
// automaton count as special).
func usageCalculatorPatchAutoLanesSpecialPriority(v interface{}) (int, error) {
	f, err := usageCalculatorPatchNumber("auto_lanes_special_priority", v)
	if err != nil {
		return 0, err
	}
	if f != math.Trunc(f) || f < math.MinInt32 || f > math.MaxInt32 {
		return 0, fmt.Errorf("usage.calculator.auto_lanes_special_priority: must be a whole number 0 or above")
	}
	n := int(f)
	return n, config.ValidateUsageCalculatorAutoLanesSpecialPriority(n)
}

// validateUsageCalculatorValue checks one key's patch value.
func validateUsageCalculatorValue(key string, v interface{}) error {
	switch key {
	case "variant", "policy", "reset_weekday", "reset_time", "reset_zone":
		s, err := usageCalculatorPatchString(key, v)
		if err != nil {
			return err
		}
		switch key {
		case "variant":
			return config.ValidateUsageCalculatorVariant(s)
		case "policy":
			return config.ValidateUsageCalculatorPolicy(s)
		case "reset_weekday":
			return config.ValidateUsageCalculatorResetWeekday(s)
		case "reset_time":
			return config.ValidateUsageCalculatorResetTime(s)
		}
		return config.ValidateUsageCalculatorResetZone(s)
	case "keeper":
		switch v.(type) {
		case string, float64, int, json.Number:
			return config.ValidateUsageCalculatorKeeper(toString(v))
		}
		return fmt.Errorf("usage.calculator.keeper: must be a string")
	case "safety_points", "cost_per_task_points", "baseline_per_24h":
		f, err := usageCalculatorPatchNumber(key, v)
		if err != nil {
			return err
		}
		switch key {
		case "safety_points":
			return config.ValidateUsageCalculatorSafetyPoints(f)
		case "baseline_per_24h":
			return config.ValidateUsageCalculatorBaselinePer24h(f)
		}
		return config.ValidateUsageCalculatorCostPerTaskPoints(f)
	case "max_lanes":
		_, err := usageCalculatorPatchLanes(v)
		return err
	case "auto_lanes":
		// Strict boolean: comm and the CLI send true/false, and the config_set
		// tool's raw-then-quoted fallback never lands a bool as a string here
		// anyway, so accepting a "true" string only masks a broken caller.
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("usage.calculator.auto_lanes: must be a boolean (true or false)")
		}
		return nil
	case "auto_lanes_max":
		_, err := usageCalculatorPatchAutoLanesMax(v)
		return err
	case "auto_lanes_special_priority":
		_, err := usageCalculatorPatchAutoLanesSpecialPriority(v)
		return err
	}
	return nil
}

// validateUsageCalculatorPatch checks every usage.calculator.* key in a PUT
// /api/config body before anything is applied.
func validateUsageCalculatorPatch(patch map[string]interface{}) error {
	for _, key := range usageCalculatorKeys {
		if v, ok := patch[usageCalculatorKeyPrefix+key]; ok {
			if err := validateUsageCalculatorValue(key, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyUsageCalculatorPatchKey stores one validated usage.calculator.* key. A
// value that fails validation is left unchanged (handlePutConfig has already
// rejected it; this guards a direct applyConfigPatch caller).
func applyUsageCalculatorPatchKey(c *config.UsageCalculatorConfig, fullKey string, v interface{}) {
	key := strings.TrimPrefix(fullKey, usageCalculatorKeyPrefix)
	if validateUsageCalculatorValue(key, v) != nil {
		return
	}
	d := config.DefaultUsageCalculator()
	text := func(def string) string {
		s := strings.ToLower(strings.TrimSpace(toString(v)))
		if s == "" {
			return def
		}
		return s
	}
	switch key {
	case "variant":
		c.Variant = text(d.Variant)
	case "policy":
		c.Policy = text(d.Policy)
	case "reset_weekday":
		c.ResetWeekday = text(d.ResetWeekday)
	case "reset_time":
		if s := strings.TrimSpace(toString(v)); s != "" {
			c.ResetTime = s
		} else {
			c.ResetTime = d.ResetTime
		}
	case "reset_zone":
		// Keep "" as "": an empty zone means "follow the daemon's local zone
		// at read time" and is resolved by EffectiveUsageResetZone, never
		// rewritten to a stored default. An explicit zone ("UTC" included)
		// is stored as-is and sticks.
		if s := strings.TrimSpace(toString(v)); s != "" {
			c.ResetZone = s
		} else {
			c.ResetZone = d.ResetZone
		}
	case "keeper":
		c.Keeper = strings.TrimSpace(toString(v))
	case "safety_points":
		c.SafetyPoints, _ = usageCalculatorPatchNumber(key, v)
	case "cost_per_task_points":
		c.CostPerTaskPoints, _ = usageCalculatorPatchNumber(key, v)
	case "baseline_per_24h":
		c.BaselinePer24h, _ = usageCalculatorPatchNumber(key, v)
	case "max_lanes":
		c.MaxLanes, _ = usageCalculatorPatchLanes(v)
	case "auto_lanes":
		if b, ok := v.(bool); ok {
			c.AutoLanes = b
		}
	case "auto_lanes_max":
		if n, err := usageCalculatorPatchAutoLanesMax(v); err == nil {
			c.AutoLanesMax = n
		}
	case "auto_lanes_special_priority":
		if n, err := usageCalculatorPatchAutoLanesSpecialPriority(v); err == nil {
			c.AutoLanesSpecialPriority = n
		}
	}
}

// usageCalculatorForDisplay renders the EFFECTIVE settings for GET
// /api/config, so an unset config shows its defaults. The reset zone is the
// effective one: the operator's set zone, or the daemon's local zone when the
// operator left it unset (reset_zone_source "server"), or UTC when no zone
// resolved (reset_zone_fell_back true). A set zone reads back as stored, so
// resetting the key to "" goes through the PUT, never here.
func usageCalculatorForDisplay(c config.UsageCalculatorConfig) map[string]interface{} {
	e := config.EffectiveUsageCalculator(c)
	zone, serverZone, fellBack := config.EffectiveUsageResetZone(c)
	source := "configured"
	if serverZone {
		source = "server"
	}
	return map[string]interface{}{
		"variant":                     e.Variant,
		"policy":                      e.Policy,
		"safety_points":               e.SafetyPoints,
		"cost_per_task_points":        e.CostPerTaskPoints,
		"baseline_per_24h":            e.BaselinePer24h,
		"max_lanes":                   e.MaxLanes,
		"auto_lanes":                  e.AutoLanes,
		"auto_lanes_max":              e.AutoLanesMax,
		"auto_lanes_special_priority": e.AutoLanesSpecialPriority,
		"reset_weekday":               e.ResetWeekday,
		"reset_time":                  e.ResetTime,
		"reset_zone":                  zone,
		"reset_zone_source":           source,
		"reset_zone_fell_back":        fellBack,
		"keeper":                      e.Keeper,
	}
}
