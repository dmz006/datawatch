// The autonomous.automation.dependency_update block: settings for the weekly
// Automation that updates every dependency together. Defaults are applied
// only in the Effective* accessors; Validate refuses a bad value on every
// write path and on config load.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DependencyUpdateDefaultSchedule — 'weekday HH:MM' in the daemon's
	// local time; what an empty schedule reads.
	DependencyUpdateDefaultSchedule = "monday 05:00"
	// DependencyUpdateDefaultMaxMajors — how many major updates go into
	// one run when max_majors is unset.
	DependencyUpdateDefaultMaxMajors = 5
	// DependencyUpdateMaxMajorsLimit — the largest legal max_majors.
	DependencyUpdateMaxMajorsLimit = 20
)

var dependencyUpdateWeekdays = []string{
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
}

// DependencyUpdateConfig — autonomous.automation.dependency_update.*.
// Enabled and MaxMajors are pointers on purpose: unset (nil) reads true
// and 5, while an explicit false or 0 (no majors in a run) survives a
// merge round trip.
type DependencyUpdateConfig struct {
	Enabled   *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Schedule  string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	MaxMajors *int   `yaml:"max_majors,omitempty" json:"max_majors,omitempty"`
}

// EffectiveEnabled returns whether the update Automation is on; unset reads true.
func (c DependencyUpdateConfig) EffectiveEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// EffectiveSchedule returns the schedule in effect, trimmed; empty reads
// the 'monday 05:00' default.
func (c DependencyUpdateConfig) EffectiveSchedule() string {
	s := strings.TrimSpace(c.Schedule)
	if s == "" {
		return DependencyUpdateDefaultSchedule
	}
	return s
}

// EffectiveMaxMajors returns how many major updates one run may include;
// unset reads 5, an explicit 0 means none.
func (c DependencyUpdateConfig) EffectiveMaxMajors() int {
	if c.MaxMajors == nil {
		return DependencyUpdateDefaultMaxMajors
	}
	return *c.MaxMajors
}

// ParseDependencyUpdateSchedule splits a 'weekday HH:MM' schedule into a
// lower-case weekday name, hour and minute.
func ParseDependencyUpdateSchedule(s string) (weekday string, hour, minute int, err error) {
	bad := fmt.Errorf("autonomous.automation.dependency_update.schedule: %q is not valid (must be 'weekday HH:MM': a weekday name monday..sunday followed by a 24-hour time, for example 'monday 05:00')", s)
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return "", 0, 0, bad
	}
	weekday = strings.ToLower(fields[0])
	found := false
	for _, d := range dependencyUpdateWeekdays {
		if d == weekday {
			found = true
			break
		}
	}
	if !found {
		return "", 0, 0, bad
	}
	hm := strings.Split(fields[1], ":")
	if len(hm) != 2 || len(hm[0]) != 2 || len(hm[1]) != 2 {
		return "", 0, 0, bad
	}
	h, errH := strconv.Atoi(hm[0])
	m, errM := strconv.Atoi(hm[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return "", 0, 0, bad
	}
	return weekday, h, m, nil
}

// Validate refuses an invalid schedule or max_majors; unset values are fine.
func (c DependencyUpdateConfig) Validate() error {
	if strings.TrimSpace(c.Schedule) != "" {
		if _, _, _, err := ParseDependencyUpdateSchedule(c.Schedule); err != nil {
			return err
		}
	}
	if c.MaxMajors != nil && (*c.MaxMajors < 0 || *c.MaxMajors > DependencyUpdateMaxMajorsLimit) {
		return fmt.Errorf("autonomous.automation.dependency_update.max_majors: %d must be a whole number from 0 to %d (0 = no majors in a run, unset reads %d)",
			*c.MaxMajors, DependencyUpdateMaxMajorsLimit, DependencyUpdateDefaultMaxMajors)
	}
	return nil
}
