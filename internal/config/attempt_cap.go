// Attempt cap config: the autonomous.attempt_cap block.
//
// The daemon counts every attempt a task is started for, across failures,
// cancels, resets and daemon restarts, and keeps the count in the task record.
// The first free_attempts attempts start at once. Each attempt after them
// waits a growing delay (backoff_minutes) after the previous one ended. When
// the count reaches max_total the task is held for the operator and nothing
// starts it again until the operator force-resets it. This block holds the
// policy; the executor reads it through the Effective* accessors, so an unset
// key always reads its default. A project may override max_total (see
// ProjectPolicy.AttemptCap).
//
// Enabled is a *bool and the list keeps nil (unset, reads the default) apart
// from a non-nil empty list (explicit: no delay), so an explicit false or empty
// list survives a config round trip.
package config

import "fmt"

// Defaults for the attempt cap. Six total attempts is the existing three-failure
// escalation ladder run twice: one ladder is the normal try, the second is the
// one retry a supervisor or operator is allowed to ask for. The delays grow by
// a factor of three (5, 15, 45 minutes) so a task that is failing for a reason
// no one has fixed waits a little more each time, 65 minutes in all.
const (
	AttemptCapDefaultMaxTotal     = 6
	AttemptCapDefaultFreeAttempts = 3
)

// AttemptCapDefaultBackoffMinutes returns the default delays in minutes before
// the attempts that follow the free ones.
func AttemptCapDefaultBackoffMinutes() []int { return []int{5, 15, 45} }

// AttemptCapConfig is the autonomous.attempt_cap block.
type AttemptCapConfig struct {
	// Enabled turns the cap and the back-off on. nil reads as true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// MaxTotal is the number of attempts a task may be started for in all.
	// 0 (unset) reads as 6; Validate rejects anything below 1.
	MaxTotal int `yaml:"max_total,omitempty" json:"max_total,omitempty"`
	// FreeAttempts is how many attempts start with no delay. 0 (unset) reads
	// as 3; Validate rejects anything below 1.
	FreeAttempts int `yaml:"free_attempts,omitempty" json:"free_attempts,omitempty"`
	// BackoffMinutes is the wait before each attempt after the free ones, in
	// order, the last entry repeating. nil reads as [5,15,45]; a non-nil empty
	// list means no wait.
	BackoffMinutes []int `yaml:"backoff_minutes" json:"backoff_minutes"`
}

// attemptCapYAML is the on-disk shape. The list is a pointer so a nil list is
// left out of the file (unset) while a non-nil empty list is written as [].
type attemptCapYAML struct {
	Enabled        *bool  `yaml:"enabled,omitempty"`
	MaxTotal       int    `yaml:"max_total,omitempty"`
	FreeAttempts   int    `yaml:"free_attempts,omitempty"`
	BackoffMinutes *[]int `yaml:"backoff_minutes,omitempty"`
}

// MarshalYAML writes an unset list as absent and an explicit empty list as [].
func (c AttemptCapConfig) MarshalYAML() (interface{}, error) {
	out := attemptCapYAML{Enabled: c.Enabled, MaxTotal: c.MaxTotal, FreeAttempts: c.FreeAttempts}
	if c.BackoffMinutes != nil {
		out.BackoffMinutes = &c.BackoffMinutes
	}
	return out, nil
}

// IsZero reports whether no key of the block is set, so yaml.v3 keeps a block
// whose only key is an explicit empty list.
func (c AttemptCapConfig) IsZero() bool {
	return c.Enabled == nil && c.MaxTotal == 0 && c.FreeAttempts == 0 && c.BackoffMinutes == nil
}

// Clone returns a deep copy so a mirror of the block never aliases the daemon
// config's slice or bool pointer. An unset list stays nil.
func (c AttemptCapConfig) Clone() AttemptCapConfig {
	out := c
	if c.BackoffMinutes != nil {
		out.BackoffMinutes = append([]int{}, c.BackoffMinutes...)
	}
	out.Enabled = cloneBoolPtr(c.Enabled)
	return out
}

// EffectiveEnabled reports whether the cap is on (default true).
func (c AttemptCapConfig) EffectiveEnabled() bool { return boolOrDefault(c.Enabled, true) }

// EffectiveMaxTotal returns the attempt cap; 0 or less reads as the default.
func (c AttemptCapConfig) EffectiveMaxTotal() int {
	if c.MaxTotal < 1 {
		return AttemptCapDefaultMaxTotal
	}
	return c.MaxTotal
}

// EffectiveFreeAttempts returns how many attempts start with no delay; 0 or
// less reads as the default.
func (c AttemptCapConfig) EffectiveFreeAttempts() int {
	if c.FreeAttempts < 1 {
		return AttemptCapDefaultFreeAttempts
	}
	return c.FreeAttempts
}

// EffectiveBackoffMinutes returns the configured delays; unset reads as the
// default list and an explicit empty list as none. The result is a copy.
func (c AttemptCapConfig) EffectiveBackoffMinutes() []int {
	if c.BackoffMinutes == nil {
		return AttemptCapDefaultBackoffMinutes()
	}
	return append([]int{}, c.BackoffMinutes...)
}

// BackoffBefore returns the wait in minutes before starting attempt n
// (1-based): 0 for the free attempts and an empty list, else the entry for
// that position, the last entry repeating.
func (c AttemptCapConfig) BackoffBefore(n int) int {
	free := c.EffectiveFreeAttempts()
	delays := c.EffectiveBackoffMinutes()
	if n <= free || len(delays) == 0 {
		return 0
	}
	i := n - free - 1
	if i >= len(delays) {
		i = len(delays) - 1
	}
	return delays[i]
}

// Validate rejects a cap or free-attempt count below 1 and a negative delay,
// naming the key. A zero value is the unset value, so a caller validating a
// whole loaded config should skip a block for which IsZero is true.
func (c AttemptCapConfig) Validate() error {
	if c.MaxTotal != 0 && c.MaxTotal < 1 {
		return fmt.Errorf("autonomous.attempt_cap.max_total: %d is invalid; use 1 or more (default %d)",
			c.MaxTotal, AttemptCapDefaultMaxTotal)
	}
	if c.FreeAttempts != 0 && c.FreeAttempts < 1 {
		return fmt.Errorf("autonomous.attempt_cap.free_attempts: %d is invalid; use 1 or more (default %d)",
			c.FreeAttempts, AttemptCapDefaultFreeAttempts)
	}
	for i, m := range c.BackoffMinutes {
		if m < 0 {
			return fmt.Errorf("autonomous.attempt_cap.backoff_minutes[%d]: %d is invalid; use 0 or more", i, m)
		}
	}
	return nil
}
