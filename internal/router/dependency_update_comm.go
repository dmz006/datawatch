// Chat `configure` for the weekly dependency update Automation keys
// (autonomous.automation.dependency_update.enabled, .schedule and
// .max_majors). Each value is checked with the config package's Validate
// before anything is written; an invalid value is refused with the allowed
// form in the reply and configureFn is never called. A valid value is
// stored through the normal configure path and the reply names the stored
// value.

package router

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	dependencyUpdateEnabledKey   = "autonomous.automation.dependency_update.enabled"
	dependencyUpdateScheduleKey  = "autonomous.automation.dependency_update.schedule"
	dependencyUpdateMaxMajorsKey = "autonomous.automation.dependency_update.max_majors"
)

// DependencyUpdateConfigureKeys lists the configure keys this file owns,
// for the parity test.
func DependencyUpdateConfigureKeys() []string {
	return []string{dependencyUpdateEnabledKey, dependencyUpdateScheduleKey, dependencyUpdateMaxMajorsKey}
}

// checkDependencyUpdateConfigure refuses an invalid dependency update
// value, naming the allowed form. Other keys pass through.
func checkDependencyUpdateConfigure(key, value string) error {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	var c config.DependencyUpdateConfig
	switch key {
	case dependencyUpdateEnabledKey:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s: %q is not valid (allowed: true or false; unset reads true)", key, value)
		}
		return nil
	case dependencyUpdateScheduleKey:
		if value == "" {
			return fmt.Errorf("%s: an empty value is not valid (allowed form: 'weekday HH:MM', for example 'monday 05:00')", key)
		}
		c.Schedule = value
	case dependencyUpdateMaxMajorsKey:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s: %q is not a whole number (allowed: 0 to %d; 0 = no majors in a run, unset reads %d)",
				key, value, config.DependencyUpdateMaxMajorsLimit, config.DependencyUpdateDefaultMaxMajors)
		}
		c.MaxMajors = &n
	default:
		return nil
	}
	return c.Validate()
}
