package config

// The gate repair engine (autonomous.gate_repair): what the daemon does when
// a gate fails with a class the engine knows how to repair. The block carries
// the engine's mode and is independent of autonomous.housekeeping_repairs,
// although the engine reports under the housekeeping group gate_repair.
//
// Mode:
//
//	off     no detection, nothing recorded (default when unset is report, not
//	        off — report is the safe read-only default)
//	report  detect and repair nothing: record the finding as a housekeeping
//	        repair record (group gate_repair), write one decision-log line and
//	        raise one alert per failed gate until it stops failing
//	on      apply the safe repair for the failed class, once per class per
//	        task; when the budget for a class is spent, or the failure class
//	        is unknown, raise a held operator question
//
// Invalid values are refused: the value never reaches the daemon.

import (
	"fmt"
	"strings"
)

// GateRepairSettingPrefix — the setting key prefix of the block.
const GateRepairSettingPrefix = "autonomous.gate_repair."

const (
	GateRepairModeOff    = "off"
	GateRepairModeReport = "report"
	GateRepairModeOn     = "on"
	// GateRepairModeDefault — the mode when the setting is unset or invalid.
	GateRepairModeDefault = GateRepairModeReport
)

// GateRepairModeValues — the accepted modes.
var GateRepairModeValues = []string{GateRepairModeOff, GateRepairModeReport, GateRepairModeOn}

// GateRepairSettingFields — the block's fields, for help text.
var GateRepairSettingFields = []string{"mode"}

// GateRepairConfig — the autonomous.gate_repair block.
type GateRepairConfig struct {
	// Mode — off, report or on. Empty means report (the shipped default).
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// ParseGateRepairMode coerces one value into a mode; the zero value becomes
// the report default.
func ParseGateRepairMode(v interface{}) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("must be one of off, report, on")
	}
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return GateRepairModeDefault, nil
	case GateRepairModeOff, GateRepairModeReport, GateRepairModeOn:
		return s, nil
	default:
		return "", fmt.Errorf("%q is not a mode: must be one of off, report, on", s)
	}
}

// SetGateRepairField applies one field of the block; ok is false for a
// field the block has no setter for.
func (g GateRepairConfig) SetGateRepairField(field string, value interface{}) (GateRepairConfig, bool) {
	if field != "mode" {
		return g, false
	}
	m, err := ParseGateRepairMode(value)
	if err != nil {
		return g, false
	}
	g.Mode = m
	return g, true
}

// Effective returns the block with defaults filled in: empty or invalid mode
// becomes the report default.
func (g GateRepairConfig) Effective() GateRepairConfig {
	m, err := ParseGateRepairMode(g.Mode)
	if err != nil {
		m = GateRepairModeDefault
	}
	g.Mode = m
	return g
}

// ValidateGateRepairSetting checks one setting key and value of the block,
// shared by every surface that sets it. An empty value is accepted on delete,
// and reverts to the report default. A value that parses is accepted even
// when the key is not a field of the block, so deleting one field never
// fails.
func ValidateGateRepairSetting(key string, value interface{}) error {
	field, ok := strings.CutPrefix(key, GateRepairSettingPrefix)
	if !ok {
		return fmt.Errorf("%s: not an %s key", key, GateRepairSettingPrefix+"*")
	}
	switch field {
	case "mode":
		if _, err := ParseGateRepairMode(value); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	default:
		return fmt.Errorf("%s: must be one of %s", key, strings.Join(GateRepairSettingFields, ", "))
	}
	return nil
}

// ValidateGateRepairConfig checks the whole block, shared by every surface
// that writes it.
func (g GateRepairConfig) ValidateGateRepairConfig() error {
	_, err := ParseGateRepairMode(g.Mode)
	if err != nil {
		return fmt.Errorf("%smode: %v", GateRepairSettingPrefix, err)
	}
	return nil
}

// loadGateRepair validates the block and fills in its defaults, on load.
func loadGateRepair(cfg *Config) error {
	if err := cfg.Autonomous.GateRepair.ValidateGateRepairConfig(); err != nil {
		return err
	}
	cfg.Autonomous.GateRepair = cfg.Autonomous.GateRepair.Effective()
	return nil
}
