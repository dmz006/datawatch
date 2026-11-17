// Daily pending-branch alert settings on PUT /api/config (and so the CLI
// `datawatch config set`, the comm channel `configure` and MCP config_set):
//
//	autonomous.unlanded_alert_enabled    true|false (unset reads true)
//	autonomous.unlanded_alert_threshold  whole number >= 0 (unset reads 3)
//
// The dedicated /api/autonomous/config surface (and MCP
// autonomous_config_set) takes the same two keys without the prefix; the
// autonomous manager validates them there (autonomous.validateUnlandedAlertPatch).
// Both paths end in the manager's live Config, which the daily check reads
// on every tick, so a change applies without a restart.
package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Config keys of the daily pending-branch alert on PUT /api/config.
const (
	unlandedAlertEnabledKey   = "autonomous.unlanded_alert_enabled"
	unlandedAlertThresholdKey = "autonomous.unlanded_alert_threshold"
)

// UnlandedAlertConfigKeys lists the two config keys settable through
// PUT /api/config.
func UnlandedAlertConfigKeys() []string {
	return []string{unlandedAlertEnabledKey, unlandedAlertThresholdKey}
}

// strictWholeNumber reads a JSON number with no fraction, an int, or a
// numeric string (the CLI and the comm channel send strings).
func strictWholeNumber(v interface{}) (int, bool) {
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) || math.IsInf(x, 0) || math.IsNaN(x) {
			return 0, false
		}
		return int(x), true
	case int:
		return x, true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// validateUnlandedAlertPatch checks the two keys of a PUT /api/config patch
// before anything is applied. A bad value refuses the whole PUT.
func validateUnlandedAlertPatch(patch map[string]interface{}) error {
	if v, ok := patch[unlandedAlertEnabledKey]; ok {
		if _, isBool := parseBoolValue(v); !isBool {
			return fmt.Errorf("%s: must be true or false", unlandedAlertEnabledKey)
		}
	}
	if v, ok := patch[unlandedAlertThresholdKey]; ok {
		n, isInt := strictWholeNumber(v)
		if !isInt {
			return fmt.Errorf("%s: must be a whole number >= 0 (default %d)", unlandedAlertThresholdKey, config.DefaultUnlandedAlertThreshold)
		}
		if err := config.ValidateUnlandedAlertThreshold(&n); err != nil {
			return err
		}
	}
	return nil
}

// applyUnlandedAlertConfigKey stores one of the two keys on cfg. handled
// reports whether key is one of them; applied is false for a value
// validateUnlandedAlertPatch would refuse, so the caller lists it as skipped.
func applyUnlandedAlertConfigKey(cfg *config.Config, key string, v interface{}) (handled, applied bool) {
	switch key {
	case unlandedAlertEnabledKey:
		b, ok := parseBoolValue(v)
		if ok {
			cfg.Autonomous.UnlandedAlertEnabled = &b
		}
		return true, ok
	case unlandedAlertThresholdKey:
		n, ok := strictWholeNumber(v)
		if ok && n >= 0 {
			cfg.Autonomous.UnlandedAlertThreshold = &n
			return true, true
		}
		return true, false
	}
	return false, false
}
