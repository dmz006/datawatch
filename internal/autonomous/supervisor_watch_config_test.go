package autonomous

import (
	"testing"
	"time"
)

// TestSupervisorWatchConfigDefaultModeUnset — an unset or empty mode reads
// shadow (the release default: reports only, never acts, so the watch
// ships in shadow mode first).
func TestSupervisorWatchConfigDefaultModeUnset(t *testing.T) {
	var unset SupervisorWatchConfig
	if got := unset.EffectiveMode(); got != SupervisorWatchModeShadow {
		t.Errorf("unset mode: EffectiveMode() = %q, want %q", got, SupervisorWatchModeShadow)
	}

	// Empty (explicit "" and whitespace) reads shadow too: empty = unset.
	for _, mode := range []string{"", " ", "\t"} {
		c := SupervisorWatchConfig{Mode: mode}
		if got := c.EffectiveMode(); got != SupervisorWatchModeShadow {
			t.Errorf("empty mode %q: EffectiveMode() = %q, want %q", mode, got, SupervisorWatchModeShadow)
		}
	}

	// DefaultSupervisorWatchConfig starts from the shadow default too, so
	// a caller never writes the literal.
	if got := DefaultSupervisorWatchConfig().EffectiveMode(); got != SupervisorWatchModeShadow {
		t.Errorf("DefaultSupervisorWatchConfig() EffectiveMode() = %q, want %q", got, SupervisorWatchModeShadow)
	}
}

// TestSupervisorWatchConfigLegalModes — off, shadow and on are all
// accepted (case-insensitive, trimmed), and the effective value is read
// from the accessor against the constants — never a literal at the call
// site.
func TestSupervisorWatchConfigLegalModes(t *testing.T) {
	accepted := map[string]string{
		"off":    SupervisorWatchModeOff,
		"shadow": SupervisorWatchModeShadow,
		"on":     SupervisorWatchModeOn,
		" OFF ":  SupervisorWatchModeOff,
		"Shadow": SupervisorWatchModeShadow,
		"ON":     SupervisorWatchModeOn,
		"":       SupervisorWatchModeShadow,
	}
	for raw, want := range accepted {
		c := SupervisorWatchConfig{Mode: raw}
		if err := ValidateSupervisorWatchConfig(c); err != nil {
			t.Errorf("Validate(mode=%q): %v, want accepted", raw, err)
			continue
		}
		if got := c.EffectiveMode(); got != want {
			t.Errorf("mode %q: EffectiveMode() = %q, want %q", raw, got, want)
		}
	}
}

// TestSupervisorWatchConfigIllegalModesRefused — any other value is
// refused (the config-load path and every write path run these same
// rules).
func TestSupervisorWatchConfigIllegalModesRefused(t *testing.T) {
	badModes := []string{"loud", "ShadowMode", "enabled", "off on", "ONSHADOW", "1"}
	for _, mode := range badModes {
		if err := ValidateSupervisorWatchConfig(SupervisorWatchConfig{Mode: mode}); err == nil {
			t.Errorf("Validate(mode=%q): accepted, want refused", mode)
		}
		if err := ValidateSupervisorWatchMode(mode); err == nil {
			t.Errorf("ValidateSupervisorWatchMode(%q): accepted, want refused", mode)
		}
	}
	if err := ValidateSupervisorWatchMode("loud"); err == nil || err.Error() == "" {
		// The error must name the key and the accepted set.
		t.Errorf("refusal error for a bad mode = %v, want an error naming autonomous.automation.supervisor_watch.mode and off/shadow/on", err)
	}
}

// TestSupervisorWatchConfigAlertMinutes — an unset (nil) alert_minutes
// reads the shared 30-minute default DefaultAutomationTriageAlertMinutes
// (no second constant for 30), an explicit 0 stays 0 (the alert off),
// and an explicit override is kept.
func TestSupervisorWatchConfigAlertMinutes(t *testing.T) {
	var unset SupervisorWatchConfig
	if got, want := unset.EffectiveAlertMinutes(), DefaultAutomationTriageAlertMinutes; got != want {
		t.Errorf("unset alert_minutes: EffectiveAlertMinutes() = %d, want the shared %d default", got, want)
	}

	off := 0
	c := SupervisorWatchConfig{AlertMinutes: &off}
	if got := c.EffectiveAlertMinutes(); got != 0 {
		t.Errorf("explicit 0: EffectiveAlertMinutes() = %d, want 0 (the alert off)", got)
	}

	override := 5
	c = SupervisorWatchConfig{AlertMinutes: &override}
	if got := c.EffectiveAlertMinutes(); got != 5 {
		t.Errorf("explicit 5: EffectiveAlertMinutes() = %d, want 5", got)
	}

	// The shared default applies here, nowhere else: the accessor's
	// constant is the triage one, so a default change in one place
	// reaches both watches.
	if got := DefaultSupervisorWatchConfig().EffectiveAlertMinutes(); got != DefaultAutomationTriageAlertMinutes {
		t.Errorf("DefaultSupervisorWatchConfig() EffectiveAlertMinutes() = %d, want the shared %d default",
			got, DefaultAutomationTriageAlertMinutes)
	}
}

// TestSupervisorWatchConfigNegativeRefused — a negative alert or silent
// threshold is refused (negative values never survive a write path),
// while zero (an explicit off/zero) is legal.
func TestSupervisorWatchConfigNegativeRefused(t *testing.T) {
	neg := -1
	zero := 0
	positive := 5

	for field, c := range map[string]SupervisorWatchConfig{
		"alert":  {AlertMinutes: &neg},
		"silent": {SilentMinutes: &neg},
	} {
		if err := ValidateSupervisorWatchConfig(c); err == nil {
			t.Errorf("Validate(%s_minutes=-1): accepted, want refused", field)
		}
	}
	if err := ValidateSupervisorWatchMinutes("alert_minutes", -1); err == nil {
		t.Errorf("ValidateSupervisorWatchMinutes(alert_minutes, -1): accepted, want refused")
	}
	if err := ValidateSupervisorWatchMinutes("silent_minutes", -7); err == nil || err.Error() == "" {
		// The error must name the key.
		t.Errorf("refusal error for a negative silent_minutes = %v, want an error naming autonomous.automation.supervisor_watch.silent_minutes", err)
	}

	// Zero is legal in both: an explicit off/zero, not a default.
	if err := ValidateSupervisorWatchConfig(SupervisorWatchConfig{AlertMinutes: &zero, SilentMinutes: &zero}); err != nil {
		t.Errorf("Validate(both thresholds 0): %v, want accepted", err)
	}
	if err := ValidateSupervisorWatchConfig(SupervisorWatchConfig{AlertMinutes: &positive, SilentMinutes: &positive}); err != nil {
		t.Errorf("Validate(both thresholds %d): %v, want accepted", positive, err)
	}
}

// TestSupervisorWatchConfigSilentDefault — an unset (nil) silent_minutes
// reads the 60-minute default, an explicit 0 stays 0, and an explicit
// override is kept.
func TestSupervisorWatchConfigSilentDefault(t *testing.T) {
	var unset SupervisorWatchConfig
	if got, want := unset.EffectiveSilentMinutes(), DefaultSupervisorWatchSilentMinutes; got != want {
		t.Errorf("unset silent_minutes: EffectiveSilentMinutes() = %d, want the %d default", got, want)
	}

	zero := 0
	c := SupervisorWatchConfig{SilentMinutes: &zero}
	if got := c.EffectiveSilentMinutes(); got != 0 {
		t.Errorf("explicit 0: EffectiveSilentMinutes() = %d, want 0", got)
	}

	override := 15
	c = SupervisorWatchConfig{SilentMinutes: &override}
	if got := c.EffectiveSilentMinutes(); got != 15 {
		t.Errorf("explicit 15: EffectiveSilentMinutes() = %d, want 15", got)
	}

	if got := DefaultSupervisorWatchConfig().EffectiveSilentMinutes(); got != DefaultSupervisorWatchSilentMinutes {
		t.Errorf("DefaultSupervisorWatchConfig() EffectiveSilentMinutes() = %d, want %d",
			got, DefaultSupervisorWatchSilentMinutes)
	}
}

// TestSupervisorWatchConfigPollSeconds — an unset (0) poll interval reads
// the 60-second default, an explicit legal value is kept, and the
// staleness window the compare measures against is 3 times the effective
// interval. Negative and below-minimum values are refused.
func TestSupervisorWatchConfigPollSeconds(t *testing.T) {
	var unset SupervisorWatchConfig
	if got, want := unset.EffectivePollSeconds(), DefaultSupervisorWatchPollSeconds; got != want {
		t.Errorf("unset poll_seconds: EffectivePollSeconds() = %d, want the %d default", got, want)
	}

	legal := 120
	if got := (SupervisorWatchConfig{PollSeconds: legal}).EffectivePollSeconds(); got != legal {
		t.Errorf("explicit %d: EffectivePollSeconds() = %d, want %d", legal, got, legal)
	}

	atMin := SupervisorWatchMinPollSeconds
	if got := (SupervisorWatchConfig{PollSeconds: atMin}).EffectivePollSeconds(); got != atMin {
		t.Errorf("explicit %d (the minimum): EffectivePollSeconds() = %d, want %d", atMin, got, atMin)
	}

	if got := DefaultSupervisorWatchConfig().EffectivePollSeconds(); got != DefaultSupervisorWatchPollSeconds {
		t.Errorf("DefaultSupervisorWatchConfig() EffectivePollSeconds() = %d, want %d",
			got, DefaultSupervisorWatchPollSeconds)
	}

	// The staleness window scales with the effective interval: 3x.
	if got, want := supervisorWatchScriptStale(SupervisorWatchConfig{}), 3*time.Duration(DefaultSupervisorWatchPollSeconds)*time.Second; got != want {
		t.Errorf("default stale window = %v, want %v", got, want)
	}
	if got, want := supervisorWatchScriptStale(SupervisorWatchConfig{PollSeconds: 120}), 6*time.Minute; got != want {
		t.Errorf("poll_seconds=120 stale window = %v, want %v", got, want)
	}

	// Negative values are refused; the minimum is legal.
	for _, bad := range []int{-1, 59, 1} {
		if err := ValidateSupervisorWatchConfig(SupervisorWatchConfig{PollSeconds: bad}); err == nil {
			t.Errorf("Validate(poll_seconds=%d): accepted, want refused", bad)
		}
	}
	if err := ValidateSupervisorWatchConfig(SupervisorWatchConfig{PollSeconds: SupervisorWatchMinPollSeconds}); err != nil {
		t.Errorf("Validate(poll_seconds=%d): %v, want accepted", SupervisorWatchMinPollSeconds, err)
	}
}

// TestSupervisorWatchConfigOptionalKeys — the optional path/baseline keys
// are plain strings: empty (the zero value) means unset (the main
// checkout empty disables the dirty main-checkout finding), and a set
// value is kept verbatim.
func TestSupervisorWatchConfigOptionalKeys(t *testing.T) {
	var unset SupervisorWatchConfig
	if unset.ScriptOutputPath != "" || unset.TmuxRestartBaseline != "" || unset.MainCheckout != "" {
		t.Errorf("unset config: optional keys = (%q %q %q), want all empty",
			unset.ScriptOutputPath, unset.TmuxRestartBaseline, unset.MainCheckout)
	}

	c := SupervisorWatchConfig{
		ScriptOutputPath:    "records/supervisor-watch.jsonl",
		TmuxRestartBaseline: "session-19",
		MainCheckout:        "checkouts/main",
	}
	if got := c.ScriptOutputPath; got != "records/supervisor-watch.jsonl" {
		t.Errorf("ScriptOutputPath = %q, want records/supervisor-watch.jsonl", got)
	}
	if got := c.TmuxRestartBaseline; got != "session-19" {
		t.Errorf("TmuxRestartBaseline = %q, want session-19", got)
	}
	if got := c.MainCheckout; got != "checkouts/main" {
		t.Errorf("MainCheckout = %q, want checkouts/main", got)
	}
	// No mode set: the effective mode is still the shadow default.
	if got := c.EffectiveMode(); got != SupervisorWatchModeShadow {
		t.Errorf("optional-keys config EffectiveMode() = %q, want %q", got, SupervisorWatchModeShadow)
	}
}
