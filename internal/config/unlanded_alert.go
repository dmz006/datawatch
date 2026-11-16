package config

import "fmt"

// Daily pending-branch alert (autonomous.unlanded_alert_enabled,
// autonomous.unlanded_alert_threshold). Once per calendar day the daemon
// counts the pending Automaton branches (work that later work replaced and
// that waits for cleanup); when the count is strictly above the threshold and
// the alert is on, it raises one alert naming the oldest pending branch and
// its age.
//
// Both fields are pointers so "unset" (no key in the config file) is told
// apart from an explicit value:
//
//	enabled:   nil → DefaultUnlandedAlertEnabled (true); false → off
//	threshold: nil → DefaultUnlandedAlertThreshold (3); 0 → alert on any
//	           pending branch; n → alert when more than n are pending
//
// A negative threshold is refused at every write edge (config load, PUT
// /api/config, /api/autonomous/config, MCP, CLI and the comm channel).

// DefaultUnlandedAlertEnabled is the value of autonomous.unlanded_alert_enabled
// when the key is not set.
const DefaultUnlandedAlertEnabled = true

// DefaultUnlandedAlertThreshold is the value of
// autonomous.unlanded_alert_threshold when the key is not set.
const DefaultUnlandedAlertThreshold = 3

// UnlandedAlertEnabledEffective resolves the stored flag: nil reads
// DefaultUnlandedAlertEnabled.
func UnlandedAlertEnabledEffective(v *bool) bool {
	if v == nil {
		return DefaultUnlandedAlertEnabled
	}
	return *v
}

// UnlandedAlertThresholdEffective resolves the stored threshold: nil reads
// DefaultUnlandedAlertThreshold, any set value reads as itself.
func UnlandedAlertThresholdEffective(v *int) int {
	if v == nil {
		return DefaultUnlandedAlertThreshold
	}
	return *v
}

// ValidateUnlandedAlertThreshold refuses a negative threshold. nil (unset)
// and any value of 0 or more are valid.
func ValidateUnlandedAlertThreshold(v *int) error {
	if v != nil && *v < 0 {
		return fmt.Errorf("autonomous.unlanded_alert_threshold must be a whole number >= 0 (got %d; default is %d)",
			*v, DefaultUnlandedAlertThreshold)
	}
	return nil
}
