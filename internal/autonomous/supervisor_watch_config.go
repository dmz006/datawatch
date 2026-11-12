package autonomous

import (
	"fmt"
	"strings"
)

// Supervisor watch (autonomous.automation.supervisor_watch) — the
// daemon-side watch knobs. On each coordinator poll, with no model call,
// the daemon raises through the existing alert and notify channels, and
// records in the triage record, the findings the supervisor watch script
// produces. This block switches that on, runs it in shadow, or turns it
// off. Every default is applied in exactly one place: the Effective*
// accessors in this file. No call site may hard-code a default; callers
// read the effective value.

// Supervisor watch modes — the legal modes. An empty (unset) mode reads
// SupervisorWatchModeShadow, which is the release default: reports only,
// never acts, so the watch ships in shadow mode first. Any other value is
// refused on every write path (REST PUT and config load reject it before
// it is stored).
const (
	SupervisorWatchModeOff    = "off"
	SupervisorWatchModeShadow = "shadow"
	SupervisorWatchModeOn     = "on"
)

// DefaultSupervisorWatchSilentMinutes — the
// autonomous.automation.supervisor_watch.silent_minutes default: an
// Automaton session may go silent this many minutes before the watch
// records a silence finding.
const DefaultSupervisorWatchSilentMinutes = 60

// DefaultSupervisorWatchPollSeconds — the
// autonomous.automation.supervisor_watch.poll_seconds default: the
// configured coordinator poll interval (whole seconds) the
// script-record staleness window is measured against (a record is
// stale at 3 times the effective interval).
const DefaultSupervisorWatchPollSeconds = 60

// SupervisorWatchMinPollSeconds — the minimum configured poll interval
// (whole seconds). An explicitly set value below it is refused on every
// write path (mirroring the repo CI watch's poll_seconds rule).
const SupervisorWatchMinPollSeconds = 60

// SupervisorWatchConfig — the supervisor watch knobs
// (autonomous.automation.supervisor_watch.*).
//
//   - Mode: off, shadow or on. An empty (unset) mode reads
//     SupervisorWatchModeShadow (the release default — shadow first,
//     reports only, never acts). Any other value is refused on every
//     write path.
//
//   - AlertMinutes: pointer on purpose. Unset (nil) reads the shared
//     30-minute default DefaultAutomationTriageAlertMinutes (there is no
//     second constant for 30); an explicit 0 turns the supervisor watch
//     alert off and survives a merge round trip; a negative value is
//     refused on every write path.
//
//   - SilentMinutes: pointer on purpose. Unset (nil) reads
//     DefaultSupervisorWatchSilentMinutes (60); an explicit 0 keeps the
//     silence window at zero and survives a merge round trip; a negative
//     value is refused on every write path.
//
//   - PollSeconds: the configured coordinator poll interval (whole
//     seconds) the supervisor watch pass is expected to run at and the
//     script-record staleness window is measured against (stale at 3
//     times the effective interval). Unset (0) reads
//     DefaultSupervisorWatchPollSeconds (60); an explicitly set value
//     below SupervisorWatchMinPollSeconds (60), like a negative one, is
//     refused on every write path.
//
//   - ScriptOutputPath: optional repository-relative or absolute path of
//     the oracle script's record file (the findings this watch re-raises
//     and records). Empty means the script has not published a record
//     location; the watch then records nothing for it.
//
//   - WorkingModel: the model the watch expects to be resident on
//     every registered compute node. Empty (unset) disables the
//     model_not_resident check — there is no default working model.
//
//   - TmuxRestartBaseline: optional baseline the tmux_restart finding
//     compares against. Empty means no baseline is configured.
//
//   - MainCheckout: optional path of the main checkout. Empty disables
//     the dirty main-checkout finding altogether.
type SupervisorWatchConfig struct {
	Mode string `yaml:"mode" json:"mode"`

	// AlertMinutes is a *int on purpose: an unset key (nil) reads the
	// shared DefaultAutomationTriageAlertMinutes (30) default, while an
	// explicit 0 (a pointer to zero) turns the alert off and must
	// survive a round trip.
	AlertMinutes *int `yaml:"alert_minutes,omitempty" json:"alert_minutes,omitempty"`

	// SilentMinutes is a *int on purpose: an unset key (nil) reads the
	// DefaultSupervisorWatchSilentMinutes (60) default, while an
	// explicit 0 must survive a round trip.
	SilentMinutes *int `yaml:"silent_minutes,omitempty" json:"silent_minutes,omitempty"`

	// PollSeconds is the configured coordinator poll interval (whole
	// seconds). 0 is unset and reads DefaultSupervisorWatchPollSeconds
	// (60) via EffectivePollSeconds; an explicitly set value below
	// SupervisorWatchMinPollSeconds (60) is refused on the write paths.
	PollSeconds int `yaml:"poll_seconds,omitempty" json:"poll_seconds,omitempty"`

	ScriptOutputPath    string `yaml:"script_output_path,omitempty" json:"script_output_path,omitempty"`
	TmuxRestartBaseline string `yaml:"tmux_restart_baseline,omitempty" json:"tmux_restart_baseline,omitempty"`
	MainCheckout        string `yaml:"main_checkout,omitempty" json:"main_checkout,omitempty"`

	// WorkingModel is the working model the watch expects to be
	// resident on every registered compute node. Empty (unset) disables
	// the model_not_resident check; an explicit value is compared as-is
	// (context variants of the base name count, see
	// supervisorWatchModelResident).
	WorkingModel string `yaml:"working_model,omitempty" json:"working_model,omitempty"`
}

// EffectiveMode returns the mode in effect: an empty (unset) value reads
// SupervisorWatchModeShadow (the release default — reports only, never
// acts). Any value other than off, shadow or on is refused on the write
// paths, so this never has to guess for it. This accessor is the only
// place the shadow default is applied; a caller never writes the literal.
func (c SupervisorWatchConfig) EffectiveMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return SupervisorWatchModeShadow
	}
	return mode
}

// EffectiveAlertMinutes returns the alert threshold in effect: an unset
// (nil) value reads the shared DefaultAutomationTriageAlertMinutes (30)
// default, and an explicit 0 keeps the alert off. Negative values are
// rejected on the write paths, so this never has to guess for them. This
// accessor is the only place the 30-minute default is applied.
func (c SupervisorWatchConfig) EffectiveAlertMinutes() int {
	if c.AlertMinutes == nil {
		return DefaultAutomationTriageAlertMinutes
	}
	return *c.AlertMinutes
}

// EffectiveSilentMinutes returns the silence window in effect: an unset
// (nil) value reads DefaultSupervisorWatchSilentMinutes (60), and an
// explicit 0 keeps the window at zero. Negative values are rejected on
// the write paths, so this never has to guess for them. This accessor is
// the only place the 60-minute default is applied.
func (c SupervisorWatchConfig) EffectiveSilentMinutes() int {
	if c.SilentMinutes == nil {
		return DefaultSupervisorWatchSilentMinutes
	}
	return *c.SilentMinutes
}

// EffectivePollSeconds returns the configured coordinator poll interval
// in effect (whole seconds): an unset (0) value reads the
// DefaultSupervisorWatchPollSeconds (60) default, while an explicit
// value below SupervisorWatchMinPollSeconds (60), like a negative one,
// is refused on the write paths, so this never has to guess for it. The
// script-record staleness window is 3 times this value. This accessor
// is the only place the 60-second default is applied; a caller never
// writes the literal.
func (c SupervisorWatchConfig) EffectivePollSeconds() int {
	if c.PollSeconds == 0 {
		return DefaultSupervisorWatchPollSeconds
	}
	return c.PollSeconds
}

// EffectiveWorkingModel returns the working model in effect, trimmed:
// an unset (blank) value stays blank, which disables the
// model_not_resident check — there is no default working model. This
// accessor is the only place that policy is applied.
func (c SupervisorWatchConfig) EffectiveWorkingModel() string {
	return strings.TrimSpace(c.WorkingModel)
}

// DefaultSupervisorWatchConfig returns a SupervisorWatchConfig fully
// populated with the default every Effective* accessor applies (shadow,
// the shared 30-minute alert default, the 60-minute silence window), so
// callers can start from a known-correct value without remembering any
// default.
func DefaultSupervisorWatchConfig() SupervisorWatchConfig {
	alert := DefaultAutomationTriageAlertMinutes
	silent := DefaultSupervisorWatchSilentMinutes
	return SupervisorWatchConfig{
		Mode:          SupervisorWatchModeShadow,
		AlertMinutes:  &alert,
		SilentMinutes: &silent,
		PollSeconds:   DefaultSupervisorWatchPollSeconds,
	}
}

// ValidateSupervisorWatchMode accepts the three legal modes (off, shadow,
// on; case-insensitive, trimmed; empty = unset = shadow) and rejects
// every other value with a clear error naming the accepted set.
func ValidateSupervisorWatchMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SupervisorWatchModeOff, SupervisorWatchModeShadow, SupervisorWatchModeOn:
		return nil
	default:
		return fmt.Errorf("autonomous.automation.supervisor_watch.mode: %q is not a valid value (must be %q, %q or %q)",
			mode, SupervisorWatchModeOff, SupervisorWatchModeShadow, SupervisorWatchModeOn)
	}
}

// ValidateSupervisorWatchMinutes rejects a negative minute threshold with
// a clear error naming the key. Zero is legal (the operator said zero
// explicitly); an unset (nil) threshold is the accessors' business, not
// the validator's.
func ValidateSupervisorWatchMinutes(field string, minutes int) error {
	if minutes < 0 {
		return fmt.Errorf("autonomous.automation.supervisor_watch.%s: %d must not be negative (0 is legal; an unset key reads its default)",
			field, minutes)
	}
	return nil
}

// ValidateSupervisorWatchConfig checks a whole SupervisorWatchConfig the
// same way the write paths do. Every write path runs these same rules, so
// a bad mode or a negative threshold is refused consistently rather than
// normalized silently.
func ValidateSupervisorWatchConfig(c SupervisorWatchConfig) error {
	if err := ValidateSupervisorWatchMode(c.Mode); err != nil {
		return err
	}
	if c.AlertMinutes != nil {
		if err := ValidateSupervisorWatchMinutes("alert_minutes", *c.AlertMinutes); err != nil {
			return err
		}
	}
	if c.SilentMinutes != nil {
		if err := ValidateSupervisorWatchMinutes("silent_minutes", *c.SilentMinutes); err != nil {
			return err
		}
	}
	if c.PollSeconds < 0 {
		return fmt.Errorf("autonomous.automation.supervisor_watch.poll_seconds: %d must not be negative (0 = unset, which reads the %d-second default)",
			c.PollSeconds, DefaultSupervisorWatchPollSeconds)
	}
	if c.PollSeconds > 0 && c.PollSeconds < SupervisorWatchMinPollSeconds {
		return fmt.Errorf("autonomous.automation.supervisor_watch.poll_seconds: %d must be a whole number >= %d (unset reads the %d-second default)",
			c.PollSeconds, SupervisorWatchMinPollSeconds, DefaultSupervisorWatchPollSeconds)
	}
	return nil
}
