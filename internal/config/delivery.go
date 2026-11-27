package config

import "time"

// DefaultDeliveryIntervalSeconds is how often the delivery check runs.
const DefaultDeliveryIntervalSeconds = 300

// MaxDeliveryIntervalSeconds is the longest time between checks: the outside
// timer reads a heartbeat with a longer one as unreadable.
const MaxDeliveryIntervalSeconds = 86400

// DeliveryConfig is the delivery check: one list of undelivered and unfinished
// work, a check that reports and blocks nothing, and messages to the operator.
type DeliveryConfig struct {
	// Enabled turns the list and the check on. Default true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Notify sends listed entries to the message channels. Default true.
	Notify *bool `yaml:"notify,omitempty" json:"notify,omitempty"`
	// IntervalSeconds is the time between checks. Default 300, 60 to 86400.
	IntervalSeconds int `yaml:"interval_seconds,omitempty" json:"interval_seconds,omitempty"`
}

// EnabledOn reports whether the check runs.
func (c DeliveryConfig) EnabledOn() bool { return c.Enabled == nil || *c.Enabled }

// NotifyOn reports whether messages are sent.
func (c DeliveryConfig) NotifyOn() bool { return c.Notify == nil || *c.Notify }

// Interval is the effective time between checks.
func (c DeliveryConfig) Interval() time.Duration {
	s := c.IntervalSeconds
	if s <= 0 {
		s = DefaultDeliveryIntervalSeconds
	}
	s = min(max(s, 60), MaxDeliveryIntervalSeconds)
	return time.Duration(s) * time.Second
}
