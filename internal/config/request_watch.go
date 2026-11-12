// Request watch config: the autonomous.request_watch block.
//
// The daemon observes the in-flight model request of a worker or verifier
// session whose model runs on a registered compute node, so a hung request
// can be told from a slow one. The watch is observation only: it makes no
// model call and performs no remediation. This block holds the policy; the
// watcher reads it through the Effective* accessors so an unset key always
// reads its default.
//
// Booleans are *bool and the remediation list keeps nil (unset, reads as all
// four options) distinct from a non-nil empty list (explicit: only the
// always-offered keep_waiting), so an explicit false or empty value survives
// a config round trip.
package config

import (
	"fmt"
	"strings"
)

// Remediation option ids offered when a request passes the no-progress
// threshold. Answering a question only records the choice.
const (
	// RequestWatchOptionKeepWaiting leaves the worker alone. It is always
	// offered and is always the default answer.
	RequestWatchOptionKeepWaiting = "keep_waiting"
	// RequestWatchOptionEndWorker offers ending the worker.
	RequestWatchOptionEndWorker = "end_worker"
	// RequestWatchOptionRetryOtherBackend offers retrying on another backend.
	RequestWatchOptionRetryOtherBackend = "retry_other_backend"
	// RequestWatchOptionUnloadReloadModel offers unloading and reloading the
	// model on its node.
	RequestWatchOptionUnloadReloadModel = "unload_reload_model"
)

// Defaults for the keys of the autonomous.request_watch block.
const (
	RequestWatchDefaultEnabled          = true
	RequestWatchDefaultPollSeconds      = 30
	RequestWatchDefaultNoProgressMin    = 30
	RequestWatchDefaultObserveWorkers   = true
	RequestWatchDefaultObserveVerifiers = true
	RequestWatchDefaultUseGPUSignal     = true
	RequestWatchDefaultGPUBusyPercent   = 10
	RequestWatchDefaultUnreachableCount = true
	RequestWatchDefaultRaiseAlert       = true
	RequestWatchDefaultRaiseQuestion    = true
)

// RequestWatchAllOptions returns every remediation option id, in a stable
// order with keep_waiting first.
func RequestWatchAllOptions() []string {
	return []string{
		RequestWatchOptionKeepWaiting,
		RequestWatchOptionEndWorker,
		RequestWatchOptionRetryOtherBackend,
		RequestWatchOptionUnloadReloadModel,
	}
}

// RequestWatchConfig is the autonomous.request_watch block.
type RequestWatchConfig struct {
	// Enabled turns the request watch on. nil reads as true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// PollSeconds is how often each watched request is sampled. 0 (unset)
	// reads as 30; Validate rejects anything below 1 when set.
	PollSeconds int `yaml:"poll_seconds,omitempty" json:"poll_seconds,omitempty"`
	// NoProgressMinutes is how long a request may show no progress before it
	// counts as over the threshold. 0 (unset) reads as 30.
	NoProgressMinutes int `yaml:"no_progress_minutes,omitempty" json:"no_progress_minutes,omitempty"`
	// ObserveWorkers watches worker sessions. nil reads as true.
	ObserveWorkers *bool `yaml:"observe_workers,omitempty" json:"observe_workers,omitempty"`
	// ObserveVerifiers watches verifier sessions. nil reads as true.
	ObserveVerifiers *bool `yaml:"observe_verifiers,omitempty" json:"observe_verifiers,omitempty"`
	// UseGPUSignal reads the node's GPU load to tell generating from idle.
	// nil reads as true.
	UseGPUSignal *bool `yaml:"use_gpu_signal,omitempty" json:"use_gpu_signal,omitempty"`
	// GPUBusyPercent is the GPU utilisation at or above which the node counts
	// as generating. 0 (unset) reads as 10; Validate accepts 1 to 100.
	GPUBusyPercent int `yaml:"gpu_busy_percent,omitempty" json:"gpu_busy_percent,omitempty"`
	// UnreachableCounts makes time spent with an unreachable node count as
	// no progress. nil reads as true.
	UnreachableCounts *bool `yaml:"unreachable_counts,omitempty" json:"unreachable_counts,omitempty"`
	// RaiseAlert raises an alert when a request passes the threshold.
	// nil reads as true.
	RaiseAlert *bool `yaml:"raise_alert,omitempty" json:"raise_alert,omitempty"`
	// RaiseQuestion raises a non-blocking operator question when a request
	// passes the threshold. nil reads as true.
	RaiseQuestion *bool `yaml:"raise_question,omitempty" json:"raise_question,omitempty"`
	// RemediationOptions names the options the question offers besides the
	// always-present keep_waiting. nil reads as all four; a non-nil empty
	// list means keep_waiting only.
	RemediationOptions []string `yaml:"remediation_options" json:"remediation_options"`
}

// requestWatchYAML is the on-disk shape. The list is a pointer so a nil list
// is left out of the file (unset) while a non-nil empty list is written as
// [] (explicit); a plain []T with omitempty cannot tell the two apart.
type requestWatchYAML struct {
	Enabled            *bool     `yaml:"enabled,omitempty"`
	PollSeconds        int       `yaml:"poll_seconds,omitempty"`
	NoProgressMinutes  int       `yaml:"no_progress_minutes,omitempty"`
	ObserveWorkers     *bool     `yaml:"observe_workers,omitempty"`
	ObserveVerifiers   *bool     `yaml:"observe_verifiers,omitempty"`
	UseGPUSignal       *bool     `yaml:"use_gpu_signal,omitempty"`
	GPUBusyPercent     int       `yaml:"gpu_busy_percent,omitempty"`
	UnreachableCounts  *bool     `yaml:"unreachable_counts,omitempty"`
	RaiseAlert         *bool     `yaml:"raise_alert,omitempty"`
	RaiseQuestion      *bool     `yaml:"raise_question,omitempty"`
	RemediationOptions *[]string `yaml:"remediation_options,omitempty"`
}

// MarshalYAML writes an unset list as absent and an explicit empty list as [].
func (c RequestWatchConfig) MarshalYAML() (interface{}, error) {
	out := requestWatchYAML{
		Enabled:           c.Enabled,
		PollSeconds:       c.PollSeconds,
		NoProgressMinutes: c.NoProgressMinutes,
		ObserveWorkers:    c.ObserveWorkers,
		ObserveVerifiers:  c.ObserveVerifiers,
		UseGPUSignal:      c.UseGPUSignal,
		GPUBusyPercent:    c.GPUBusyPercent,
		UnreachableCounts: c.UnreachableCounts,
		RaiseAlert:        c.RaiseAlert,
		RaiseQuestion:     c.RaiseQuestion,
	}
	if c.RemediationOptions != nil {
		out.RemediationOptions = &c.RemediationOptions
	}
	return out, nil
}

// IsZero reports whether no key of the block is set. yaml.v3 consults it for
// the parent's omitempty tag: without it a block whose only key is an
// explicit empty list would count as zero and be dropped from the file, so
// the list would read back as unset.
func (c RequestWatchConfig) IsZero() bool {
	return c.Enabled == nil && c.PollSeconds == 0 && c.NoProgressMinutes == 0 &&
		c.ObserveWorkers == nil && c.ObserveVerifiers == nil && c.UseGPUSignal == nil &&
		c.GPUBusyPercent == 0 && c.UnreachableCounts == nil && c.RaiseAlert == nil &&
		c.RaiseQuestion == nil && c.RemediationOptions == nil
}

// Clone returns a deep copy so a mirror of the block never aliases the
// daemon config's slice or bool pointers. An unset list stays nil and an
// explicit empty list stays non-nil.
func (c RequestWatchConfig) Clone() RequestWatchConfig {
	out := c
	if c.RemediationOptions != nil {
		out.RemediationOptions = append([]string{}, c.RemediationOptions...)
	}
	out.Enabled = cloneBoolPtr(c.Enabled)
	out.ObserveWorkers = cloneBoolPtr(c.ObserveWorkers)
	out.ObserveVerifiers = cloneBoolPtr(c.ObserveVerifiers)
	out.UseGPUSignal = cloneBoolPtr(c.UseGPUSignal)
	out.UnreachableCounts = cloneBoolPtr(c.UnreachableCounts)
	out.RaiseAlert = cloneBoolPtr(c.RaiseAlert)
	out.RaiseQuestion = cloneBoolPtr(c.RaiseQuestion)
	return out
}

// EffectiveEnabled reports whether the request watch is on (default true).
func (c RequestWatchConfig) EffectiveEnabled() bool {
	return boolOrDefault(c.Enabled, RequestWatchDefaultEnabled)
}

// EffectivePollSeconds returns the sampling interval in seconds; 0 or less
// reads as the default.
func (c RequestWatchConfig) EffectivePollSeconds() int {
	if c.PollSeconds < 1 {
		return RequestWatchDefaultPollSeconds
	}
	return c.PollSeconds
}

// EffectiveNoProgressMinutes returns the no-progress threshold in minutes;
// 0 or less reads as the default.
func (c RequestWatchConfig) EffectiveNoProgressMinutes() int {
	if c.NoProgressMinutes < 1 {
		return RequestWatchDefaultNoProgressMin
	}
	return c.NoProgressMinutes
}

// EffectiveObserveWorkers reports whether workers are watched (default true).
func (c RequestWatchConfig) EffectiveObserveWorkers() bool {
	return boolOrDefault(c.ObserveWorkers, RequestWatchDefaultObserveWorkers)
}

// EffectiveObserveVerifiers reports whether verifiers are watched (default
// true).
func (c RequestWatchConfig) EffectiveObserveVerifiers() bool {
	return boolOrDefault(c.ObserveVerifiers, RequestWatchDefaultObserveVerifiers)
}

// EffectiveUseGPUSignal reports whether the node GPU load is read (default
// true).
func (c RequestWatchConfig) EffectiveUseGPUSignal() bool {
	return boolOrDefault(c.UseGPUSignal, RequestWatchDefaultUseGPUSignal)
}

// EffectiveGPUBusyPercent returns the GPU utilisation percent that counts as
// busy; 0 or less reads as the default.
func (c RequestWatchConfig) EffectiveGPUBusyPercent() int {
	if c.GPUBusyPercent < 1 {
		return RequestWatchDefaultGPUBusyPercent
	}
	return c.GPUBusyPercent
}

// EffectiveUnreachableCounts reports whether an unreachable node counts as no
// progress (default true).
func (c RequestWatchConfig) EffectiveUnreachableCounts() bool {
	return boolOrDefault(c.UnreachableCounts, RequestWatchDefaultUnreachableCount)
}

// EffectiveRaiseAlert reports whether passing the threshold raises an alert
// (default true).
func (c RequestWatchConfig) EffectiveRaiseAlert() bool {
	return boolOrDefault(c.RaiseAlert, RequestWatchDefaultRaiseAlert)
}

// EffectiveRaiseQuestion reports whether passing the threshold raises an
// operator question (default true).
func (c RequestWatchConfig) EffectiveRaiseQuestion() bool {
	return boolOrDefault(c.RaiseQuestion, RequestWatchDefaultRaiseQuestion)
}

// EffectiveRemediationOptions returns the option ids the question offers:
// keep_waiting first, then the configured ids in order without duplicates. An
// unset list reads as all four and an explicit empty list as keep_waiting
// only. The result is a copy.
func (c RequestWatchConfig) EffectiveRemediationOptions() []string {
	src := c.RemediationOptions
	if src == nil {
		src = RequestWatchAllOptions()
	}
	out := []string{RequestWatchOptionKeepWaiting}
	seen := map[string]bool{RequestWatchOptionKeepWaiting: true}
	for _, id := range src {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// Validate rejects a poll_seconds or no_progress_minutes below 1, a
// gpu_busy_percent outside 1 to 100 and an unknown remediation option id,
// naming the key and the allowed values. A zero number is the unset value and
// reads its default, so it is accepted.
func (c RequestWatchConfig) Validate() error {
	if c.PollSeconds < 0 {
		return fmt.Errorf("autonomous.request_watch.poll_seconds: %d is invalid; use a whole number of 1 or more (default %d)",
			c.PollSeconds, RequestWatchDefaultPollSeconds)
	}
	if c.NoProgressMinutes < 0 {
		return fmt.Errorf("autonomous.request_watch.no_progress_minutes: %d is invalid; use a whole number of 1 or more (default %d)",
			c.NoProgressMinutes, RequestWatchDefaultNoProgressMin)
	}
	if c.GPUBusyPercent != 0 && (c.GPUBusyPercent < 1 || c.GPUBusyPercent > 100) {
		return fmt.Errorf("autonomous.request_watch.gpu_busy_percent: %d is invalid; use a whole number from 1 to 100 (default %d)",
			c.GPUBusyPercent, RequestWatchDefaultGPUBusyPercent)
	}
	known := RequestWatchAllOptions()
	for i, id := range c.RemediationOptions {
		found := false
		for _, name := range known {
			if id == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("autonomous.request_watch.remediation_options[%d]: unknown option %q; valid options: %s",
				i, id, strings.Join(known, ", "))
		}
	}
	return nil
}
