// Infrastructure retry config: the autonomous.infra_retry block.
//
// An infrastructure failure is one where the task itself never got a fair
// attempt (a worker that could not be spawned, a storage write that failed,
// a session killed before the task was delivered, a daemon restart). Such a
// failure is retried on a backoff schedule instead of being counted against
// the task. This block holds the policy; the retry logic reads it through
// the Effective* accessors so an unset key always reads its default.
//
// Booleans are *bool and the two lists keep nil (unset, reads the default)
// distinct from a non-nil empty list (explicit: no class is retried, or no
// delay), so an explicit false or empty value survives a config round trip.
package config

import (
	"fmt"
	"strings"
)

// Failure classes treated as transient infrastructure faults.
const (
	InfraRetryClassSpawnError    = "spawn_error"
	InfraRetryClassStorageWrite  = "storage_write"
	InfraRetryClassSessionKilled = "session_killed_before_delivery"
	InfraRetryClassDaemonRestart = "daemon_restart"
)

// What happens to a task once its retries are used up.
const (
	// ExhaustedActionHold holds the task for operator review.
	ExhaustedActionHold = "hold"
	// ExhaustedActionCount counts the failure against the task like any other.
	ExhaustedActionCount = "count"
)

// InfraRetryDefaultMaxTries is the number of tries when max_tries is unset.
const InfraRetryDefaultMaxTries = 3

// InfraRetryAllClasses returns every failure class name, in a stable order.
func InfraRetryAllClasses() []string {
	return []string{
		InfraRetryClassSpawnError,
		InfraRetryClassStorageWrite,
		InfraRetryClassSessionKilled,
		InfraRetryClassDaemonRestart,
	}
}

// InfraRetryDefaultCategories returns the classes retried when categories is
// unset: all of them.
func InfraRetryDefaultCategories() []string { return InfraRetryAllClasses() }

// InfraRetryDefaultBackoff returns the default delays in seconds: 1, 5 and 9
// minutes, 15 minutes in total.
func InfraRetryDefaultBackoff() []int { return []int{60, 300, 540} }

// InfraRetryConfig is the autonomous.infra_retry block.
type InfraRetryConfig struct {
	// Enabled turns infrastructure retry on. nil reads as true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// MaxTries is how many tries a task gets before the exhausted action
	// applies. 0 (unset) reads as 3; Validate rejects anything below 1.
	MaxTries int `yaml:"max_tries,omitempty" json:"max_tries,omitempty"`
	// BackoffSeconds is the delay before each retry. With fewer entries than
	// tries the last entry repeats. nil reads as the default [60,300,540];
	// a non-nil empty list means retry with no delay.
	BackoffSeconds []int `yaml:"backoff_seconds" json:"backoff_seconds"`
	// Categories names the failure classes treated as transient. nil reads as
	// all four; a non-nil empty list means no class is retried.
	Categories []string `yaml:"categories" json:"categories"`
	// ExhaustedAction is "hold" (default) or "count".
	ExhaustedAction string `yaml:"exhausted_action,omitempty" json:"exhausted_action,omitempty"`
	// RetryDependents also retries tasks that were blocked behind the failed
	// one. nil reads as true.
	RetryDependents *bool `yaml:"retry_dependents,omitempty" json:"retry_dependents,omitempty"`
	// RaiseQuestion raises an operator question when retries are exhausted.
	// nil reads as true.
	RaiseQuestion *bool `yaml:"raise_question,omitempty" json:"raise_question,omitempty"`
}

// infraRetryYAML is the on-disk shape. The lists are pointers so a nil list
// is left out of the file (unset) while a non-nil empty list is written as
// [] (explicit); a plain []T with omitempty cannot tell the two apart.
type infraRetryYAML struct {
	Enabled         *bool     `yaml:"enabled,omitempty"`
	MaxTries        int       `yaml:"max_tries,omitempty"`
	BackoffSeconds  *[]int    `yaml:"backoff_seconds,omitempty"`
	Categories      *[]string `yaml:"categories,omitempty"`
	ExhaustedAction string    `yaml:"exhausted_action,omitempty"`
	RetryDependents *bool     `yaml:"retry_dependents,omitempty"`
	RaiseQuestion   *bool     `yaml:"raise_question,omitempty"`
}

// MarshalYAML writes unset lists as absent and explicit empty lists as [].
func (c InfraRetryConfig) MarshalYAML() (interface{}, error) {
	out := infraRetryYAML{
		Enabled:         c.Enabled,
		MaxTries:        c.MaxTries,
		ExhaustedAction: c.ExhaustedAction,
		RetryDependents: c.RetryDependents,
		RaiseQuestion:   c.RaiseQuestion,
	}
	if c.BackoffSeconds != nil {
		out.BackoffSeconds = &c.BackoffSeconds
	}
	if c.Categories != nil {
		out.Categories = &c.Categories
	}
	return out, nil
}

// IsZero reports whether no key of the block is set. yaml.v3 consults it for
// the parent's omitempty tag: without it a block whose only key is an explicit
// empty list would count as zero and be dropped from the file, so the list
// would read back as unset. Callers that validate a whole config can skip a
// zero block, since an unset max_tries reads as the default.
func (c InfraRetryConfig) IsZero() bool {
	return c.Enabled == nil && c.MaxTries == 0 && c.BackoffSeconds == nil &&
		c.Categories == nil && c.ExhaustedAction == "" &&
		c.RetryDependents == nil && c.RaiseQuestion == nil
}

// Clone returns a deep copy so a mirror of the block (the autonomous
// manager's) never aliases the daemon config's slices or bool pointers. Unset
// lists stay nil and explicit empty lists stay non-nil.
func (c InfraRetryConfig) Clone() InfraRetryConfig {
	out := c
	if c.BackoffSeconds != nil {
		out.BackoffSeconds = append([]int{}, c.BackoffSeconds...)
	}
	if c.Categories != nil {
		out.Categories = append([]string{}, c.Categories...)
	}
	out.Enabled = cloneBoolPtr(c.Enabled)
	out.RetryDependents = cloneBoolPtr(c.RetryDependents)
	out.RaiseQuestion = cloneBoolPtr(c.RaiseQuestion)
	return out
}

func cloneBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func boolOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// EffectiveEnabled reports whether infrastructure retry is on (default true).
func (c InfraRetryConfig) EffectiveEnabled() bool { return boolOrDefault(c.Enabled, true) }

// EffectiveMaxTries returns the try limit; 0 or less reads as the default.
func (c InfraRetryConfig) EffectiveMaxTries() int {
	if c.MaxTries < 1 {
		return InfraRetryDefaultMaxTries
	}
	return c.MaxTries
}

// EffectiveBackoff returns the delay in seconds before each retry, one entry
// per try, the last configured entry repeating when the list is shorter than
// the try limit. A configured list longer than the try limit is returned in
// full. An explicit empty list yields zero-second delays.
func (c InfraRetryConfig) EffectiveBackoff() []int {
	src := c.BackoffSeconds
	if src == nil {
		src = InfraRetryDefaultBackoff()
	}
	n := c.EffectiveMaxTries()
	if len(src) > n {
		n = len(src)
	}
	out := make([]int, n)
	for i := range out {
		switch {
		case len(src) == 0:
			out[i] = 0
		case i < len(src):
			out[i] = src[i]
		default:
			out[i] = src[len(src)-1]
		}
	}
	return out
}

// EffectiveCategories returns the failure classes retried; unset reads as all
// four and an explicit empty list as none. The result is a copy.
func (c InfraRetryConfig) EffectiveCategories() []string {
	if c.Categories == nil {
		return InfraRetryDefaultCategories()
	}
	return append([]string{}, c.Categories...)
}

// EffectiveExhaustedAction returns "hold" or "count"; anything else reads as
// "hold".
func (c InfraRetryConfig) EffectiveExhaustedAction() string {
	if c.ExhaustedAction == ExhaustedActionCount {
		return ExhaustedActionCount
	}
	return ExhaustedActionHold
}

// EffectiveRetryDependents reports whether blocked dependents are retried too
// (default true).
func (c InfraRetryConfig) EffectiveRetryDependents() bool {
	return boolOrDefault(c.RetryDependents, true)
}

// EffectiveRaiseQuestion reports whether exhausted retries raise an operator
// question (default true).
func (c InfraRetryConfig) EffectiveRaiseQuestion() bool {
	return boolOrDefault(c.RaiseQuestion, true)
}

// HasCategory reports whether class is one of the retried failure classes.
func (c InfraRetryConfig) HasCategory(class string) bool {
	for _, k := range c.EffectiveCategories() {
		if k == class {
			return true
		}
	}
	return false
}

// Validate rejects a try limit below 1, a negative delay, an unknown failure
// class and an unknown exhausted action, naming the key. A zero MaxTries is
// the unset value, so a caller validating a whole loaded config should skip a
// block for which IsZero is true (or fill the defaults first).
func (c InfraRetryConfig) Validate() error {
	if c.MaxTries < 1 {
		return fmt.Errorf("autonomous.infra_retry.max_tries: %d is invalid; use 1 or more (default %d)",
			c.MaxTries, InfraRetryDefaultMaxTries)
	}
	for i, s := range c.BackoffSeconds {
		if s < 0 {
			return fmt.Errorf("autonomous.infra_retry.backoff_seconds[%d]: %d is invalid; use 0 or more", i, s)
		}
	}
	known := InfraRetryAllClasses()
	for i, k := range c.Categories {
		found := false
		for _, name := range known {
			if k == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("autonomous.infra_retry.categories[%d]: unknown class %q; valid classes: %s",
				i, k, strings.Join(known, ", "))
		}
	}
	if c.ExhaustedAction != "" && c.ExhaustedAction != ExhaustedActionHold && c.ExhaustedAction != ExhaustedActionCount {
		return fmt.Errorf("autonomous.infra_retry.exhausted_action: %q is invalid; use %q or %q",
			c.ExhaustedAction, ExhaustedActionHold, ExhaustedActionCount)
	}
	return nil
}
