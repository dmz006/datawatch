package config

import (
	"fmt"
	"strings"
)

// Housekeeping repairs. The autonomous.housekeeping_repairs block holds one
// mode per repair group (task_chain, advance_judge, lane_fill, leftovers, planning) and
// the grace period a stuck state must outlast before it is looked at.
//
// A mode is off (the group does nothing), report (the group records what it
// would do and acts on nothing; the default) or on (the group repairs). Every
// surface reaches ValidateHousekeepingRepairsSetting, so an invalid value is
// refused with the same message everywhere and nothing is written.

// HousekeepingRepairsSettingPrefix — the config keys this block owns.
const HousekeepingRepairsSettingPrefix = "autonomous.housekeeping_repairs."

// Repair modes.
const (
	HousekeepingModeOff    = "off"
	HousekeepingModeReport = "report"
	HousekeepingModeOn     = "on"
)

// HousekeepingGraceSecondsDefault — the grace when the setting is unset.
const HousekeepingGraceSecondsDefault = 300

// HousekeepingRepairGroups — the five repair groups, in run order.
var HousekeepingRepairGroups = []string{"task_chain", "advance_judge", "lane_fill", "leftovers", "planning"}

// HousekeepingRepairsSettingFields — the block's fields, for help text.
var HousekeepingRepairsSettingFields = []string{"task_chain", "advance_judge", "lane_fill", "leftovers", "planning", "grace_seconds"}

// HousekeepingRepairsConfig — the autonomous.housekeeping_repairs block.
type HousekeepingRepairsConfig struct {
	// TaskChain — mode for stuck task chains. Empty means report.
	TaskChain string `yaml:"task_chain,omitempty" json:"task_chain,omitempty"`
	// AdvanceJudge — mode for a judged Automaton that did not advance.
	AdvanceJudge string `yaml:"advance_judge,omitempty" json:"advance_judge,omitempty"`
	// LaneFill — mode for idle lanes with work waiting.
	LaneFill string `yaml:"lane_fill,omitempty" json:"lane_fill,omitempty"`
	// Leftovers — mode for leftover state left by finished work.
	Leftovers string `yaml:"leftovers,omitempty" json:"leftovers,omitempty"`
	// Planning — mode for drafts whose planning failed and will not clear.
	Planning string `yaml:"planning,omitempty" json:"planning,omitempty"`
	// GraceSeconds — seconds a stuck state must persist before it is
	// reported or repaired. Nil means the default; 0 means no grace.
	GraceSeconds *int `yaml:"grace_seconds,omitempty" json:"grace_seconds,omitempty"`
}

// ParseHousekeepingMode validates one mode value; empty means report.
func ParseHousekeepingMode(v interface{}) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("must be one of off, report, on")
	}
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return HousekeepingModeReport, nil
	case HousekeepingModeOff, HousekeepingModeReport, HousekeepingModeOn:
		return s, nil
	}
	return "", fmt.Errorf("%q is not a mode: must be one of off, report, on", s)
}

// ParseHousekeepingGraceSeconds validates the grace: a whole number >= 0.
func ParseHousekeepingGraceSeconds(v interface{}) (int, error) {
	var n int
	switch t := v.(type) {
	case int:
		n = t
	case int64:
		n = int(t)
	case float64:
		if t != float64(int(t)) {
			return 0, fmt.Errorf("must be a whole number of seconds")
		}
		n = int(t)
	case string:
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &n); err != nil {
			return 0, fmt.Errorf("must be a whole number of seconds")
		}
	default:
		return 0, fmt.Errorf("must be a whole number of seconds")
	}
	if n < 0 {
		return 0, fmt.Errorf("must be 0 or more seconds")
	}
	return n, nil
}

// ModeFor returns the effective mode of a group (report when unset or the
// group is unknown).
func (h HousekeepingRepairsConfig) ModeFor(group string) string {
	var m string
	switch group {
	case "task_chain":
		m = h.TaskChain
	case "advance_judge":
		m = h.AdvanceJudge
	case "lane_fill":
		m = h.LaneFill
	case "leftovers":
		m = h.Leftovers
	case "planning":
		m = h.Planning
	}
	if mode, err := ParseHousekeepingMode(m); err == nil {
		return mode
	}
	return HousekeepingModeReport
}

// GraceSecondsOrDefault resolves the grace.
func (h HousekeepingRepairsConfig) GraceSecondsOrDefault() int {
	if h.GraceSeconds == nil || *h.GraceSeconds < 0 {
		return HousekeepingGraceSecondsDefault
	}
	return *h.GraceSeconds
}

// Effective fills every unset mode with report and the grace with its default.
func (h HousekeepingRepairsConfig) Effective() HousekeepingRepairsConfig {
	g := h.GraceSecondsOrDefault()
	return HousekeepingRepairsConfig{
		TaskChain: h.ModeFor("task_chain"), AdvanceJudge: h.ModeFor("advance_judge"),
		LaneFill: h.ModeFor("lane_fill"), Leftovers: h.ModeFor("leftovers"), Planning: h.ModeFor("planning"),
		GraceSeconds: &g,
	}
}

// SetHousekeepingRepairsField applies one validated field and reports whether
// the key was a valid one.
func SetHousekeepingRepairsField(h HousekeepingRepairsConfig, field string, value interface{}) (HousekeepingRepairsConfig, bool) {
	switch field {
	case "task_chain", "advance_judge", "lane_fill", "leftovers", "planning":
		m, err := ParseHousekeepingMode(value)
		if err != nil {
			return h, false
		}
		switch field {
		case "task_chain":
			h.TaskChain = m
		case "advance_judge":
			h.AdvanceJudge = m
		case "lane_fill":
			h.LaneFill = m
		case "planning":
			h.Planning = m
		default:
			h.Leftovers = m
		}
	case "grace_seconds":
		n, err := ParseHousekeepingGraceSeconds(value)
		if err != nil {
			return h, false
		}
		h.GraceSeconds = &n
	default:
		return h, false
	}
	return h, true
}

// ValidateHousekeepingRepairsSetting checks the one key a surface is about to
// set; every surface reaches it so the refusal reads the same everywhere.
func ValidateHousekeepingRepairsSetting(key string, value interface{}) error {
	field, ok := strings.CutPrefix(key, HousekeepingRepairsSettingPrefix)
	if !ok {
		return fmt.Errorf("%s: not an %s key", key, HousekeepingRepairsSettingPrefix+"*")
	}
	switch field {
	case "task_chain", "advance_judge", "lane_fill", "leftovers", "planning":
		if _, err := ParseHousekeepingMode(value); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	case "grace_seconds":
		if _, err := ParseHousekeepingGraceSeconds(value); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	default:
		return fmt.Errorf("%s: must be one of %s", key, strings.Join(HousekeepingRepairsSettingFields, ", "))
	}
	return nil
}

// ValidateHousekeepingRepairsConfig checks a whole block read from config.
func ValidateHousekeepingRepairsConfig(h HousekeepingRepairsConfig) error {
	for _, g := range HousekeepingRepairGroups {
		var m string
		switch g {
		case "task_chain":
			m = h.TaskChain
		case "advance_judge":
			m = h.AdvanceJudge
		case "lane_fill":
			m = h.LaneFill
		case "planning":
			m = h.Planning
		default:
			m = h.Leftovers
		}
		if _, err := ParseHousekeepingMode(m); err != nil {
			return fmt.Errorf("%s%s: %v", HousekeepingRepairsSettingPrefix, g, err)
		}
	}
	if h.GraceSeconds != nil && *h.GraceSeconds < 0 {
		return fmt.Errorf("%sgrace_seconds: must be 0 or more seconds", HousekeepingRepairsSettingPrefix)
	}
	return nil
}

// loadHousekeepingRepairs normalizes the block and validates it.
func loadHousekeepingRepairs(cfg *Config) error {
	if err := ValidateHousekeepingRepairsConfig(cfg.Autonomous.HousekeepingRepairs); err != nil {
		return err
	}
	cfg.Autonomous.HousekeepingRepairs = cfg.Autonomous.HousekeepingRepairs.Effective()
	return nil
}
