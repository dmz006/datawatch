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

	// The settings below are for requests (delivery job 4). Zero or nil means
	// the default. All are read live.

	// RequestTakeBySeconds is how long a request waits to be taken before it is
	// listed.
	RequestTakeBySeconds int `yaml:"request_take_by_seconds,omitempty" json:"request_take_by_seconds,omitempty"`
	// RequestDueSeconds is how long a request has before it is overdue.
	RequestDueSeconds int `yaml:"request_due_seconds,omitempty" json:"request_due_seconds,omitempty"`
	// RequestDueSecondsByKind replaces it for single request kinds.
	RequestDueSecondsByKind map[string]int `yaml:"request_due_seconds_by_kind,omitempty" json:"request_due_seconds_by_kind,omitempty"`
	// ReminderIntervalSeconds is the shortest time between two reminders to a
	// session.
	ReminderIntervalSeconds int `yaml:"reminder_interval_seconds,omitempty" json:"reminder_interval_seconds,omitempty"`
	// RemindersPerSessionHour is the operator's limit for one session. Default
	// 2; 0 types no reminders.
	RemindersPerSessionHour *int `yaml:"reminders_per_session_hour,omitempty" json:"reminders_per_session_hour,omitempty"`
	// RemindersPerHostHour is the operator's limit for this computer. Default
	// 20; 0 types no reminders.
	RemindersPerHostHour *int `yaml:"reminders_per_host_hour,omitempty" json:"reminders_per_host_hour,omitempty"`
}

// Defaults of the request settings. The two reminder limits are the operator's
// numbers. The three times are PLACEHOLDERS the operator has not approved (a
// design question in build/d4-split-final.md): job 4 is not released with them
// until they are.
const (
	DefaultRemindersPerSessionHour = 2
	DefaultRemindersPerHostHour    = 20
	DefaultRequestTakeBySeconds    = 3600
	DefaultRequestDueSeconds       = 86400
	DefaultReminderIntervalSeconds = 900
)

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

func secondsOr(s, def int) time.Duration {
	if s <= 0 {
		s = def
	}
	return time.Duration(s) * time.Second
}

// RequestTakeBy is how long a request waits to be taken before it is listed.
func (c DeliveryConfig) RequestTakeBy() time.Duration {
	return secondsOr(c.RequestTakeBySeconds, DefaultRequestTakeBySeconds)
}

// RequestDue is how long a request has before it is overdue.
func (c DeliveryConfig) RequestDue() time.Duration {
	return secondsOr(c.RequestDueSeconds, DefaultRequestDueSeconds)
}

// RequestDueByKind are the kinds with a due time of their own.
func (c DeliveryConfig) RequestDueByKind() map[string]time.Duration {
	out := map[string]time.Duration{}
	for kind, s := range c.RequestDueSecondsByKind {
		if s > 0 {
			out[kind] = time.Duration(s) * time.Second
		}
	}
	return out
}

// ReminderInterval is the shortest time between two reminders to a session.
func (c DeliveryConfig) ReminderInterval() time.Duration {
	return secondsOr(c.ReminderIntervalSeconds, DefaultReminderIntervalSeconds)
}

// limitOr reads a reminder limit. Unset is the default; 0, and anything below
// it, types no reminders.
func limitOr(v *int, def int) int {
	if v == nil {
		return def
	}
	return max(*v, 0)
}

// SessionReminderLimit is the most reminders to one session in an hour.
func (c DeliveryConfig) SessionReminderLimit() int {
	return limitOr(c.RemindersPerSessionHour, DefaultRemindersPerSessionHour)
}

// HostReminderLimit is the most reminders on this computer in an hour.
func (c DeliveryConfig) HostReminderLimit() int {
	return limitOr(c.RemindersPerHostHour, DefaultRemindersPerHostHour)
}
