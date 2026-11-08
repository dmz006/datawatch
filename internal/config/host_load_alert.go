package config

import "fmt"

// Host load alert modes (AutonomousConfig.HostLoadAlert.Mode).
const (
	// HostLoadAlertModeOff turns the host load check off.
	HostLoadAlertModeOff = "off"
	// HostLoadAlertModeAlert (the default) raises one alert per sustained
	// breach and changes nothing else.
	HostLoadAlertModeAlert = "alert"
	// HostLoadAlertModeReap also ends the processes of ended sessions that
	// the orphan reaper's evidence rule proves orphaned.
	HostLoadAlertModeReap = "reap"
)

// Host load alert defaults, applied only in the Effective* accessors.
const (
	HostLoadAlertDefaultMultiple = 1.5
	HostLoadAlertDefaultMinutes  = 10
)

// HostLoadAlertConfig is autonomous.host_load_alert: when the load average
// stays above Multiple times the core count for Minutes minutes, the daemon
// raises one alert per episode naming the busiest process groups.
//
// The zero value is valid: mode alert, multiple 1.5, minutes 10. No omitempty
// on any field, same round-trip reason as VerifierPolicyConfig.
type HostLoadAlertConfig struct {
	// Mode is off, alert or reap. Empty reads as alert.
	Mode string `yaml:"mode" json:"mode"`
	// Multiple is the load-to-cores multiple that counts as a breach. Zero
	// reads as 1.5.
	Multiple float64 `yaml:"multiple" json:"multiple"`
	// Minutes is how long a breach must last before it alerts. Zero reads as 10.
	Minutes int `yaml:"minutes" json:"minutes"`
}

// ValidateHostLoadAlertMode accepts off, alert, reap and the empty string.
func ValidateHostLoadAlertMode(mode string) error {
	switch mode {
	case "", HostLoadAlertModeOff, HostLoadAlertModeAlert, HostLoadAlertModeReap:
		return nil
	}
	return fmt.Errorf("autonomous.host_load_alert.mode: unknown mode %q (use %s, %s or %s)",
		mode, HostLoadAlertModeOff, HostLoadAlertModeAlert, HostLoadAlertModeReap)
}

// ValidateHostLoadAlertMultiple accepts 0 (the default) and any positive value.
func ValidateHostLoadAlertMultiple(m float64) error {
	if m < 0 {
		return fmt.Errorf("autonomous.host_load_alert.multiple: must be 0 (default) or greater, got %v", m)
	}
	return nil
}

// ValidateHostLoadAlertMinutes accepts 0 (the default) and any positive value.
func ValidateHostLoadAlertMinutes(m int) error {
	if m < 0 {
		return fmt.Errorf("autonomous.host_load_alert.minutes: must be 0 (default) or greater, got %d", m)
	}
	return nil
}

// EffectiveMode returns the mode in effect: empty reads as alert.
func (c HostLoadAlertConfig) EffectiveMode() string {
	if c.Mode == "" {
		return HostLoadAlertModeAlert
	}
	return c.Mode
}

// EffectiveMultiple returns the multiple in effect: a non-positive value reads as 1.5.
func (c HostLoadAlertConfig) EffectiveMultiple() float64 {
	if c.Multiple <= 0 {
		return HostLoadAlertDefaultMultiple
	}
	return c.Multiple
}

// EffectiveMinutes returns the minutes in effect: a non-positive value reads as 10.
func (c HostLoadAlertConfig) EffectiveMinutes() int {
	if c.Minutes <= 0 {
		return HostLoadAlertDefaultMinutes
	}
	return c.Minutes
}
