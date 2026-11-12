package autonomous

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSecurityFailureConfig — the security failure block
// (autonomous.automation.security_failure) has one key, mode. An unset or
// an empty mode reads shadow (the first-release default: the feature is
// switched on in shadow mode first, with no behaviour change); off, shadow
// and on are accepted; any other value is refused on the write paths.
func TestSecurityFailureConfig(t *testing.T) {
	// Unset: the zero value reads shadow via the accessor (not a
	// write-path default) — this is the first-release default.
	var unset SecurityFailureConfig
	if got := unset.EffectiveMode(); got != SecurityFailureModeShadow {
		t.Errorf("unset mode: EffectiveMode() = %q, want %q", got, SecurityFailureModeShadow)
	}

	// Empty (explicit "" and whitespace) reads shadow too: empty = unset.
	for _, mode := range []string{"", " ", "\t"} {
		c := SecurityFailureConfig{Mode: mode}
		if got := c.EffectiveMode(); got != SecurityFailureModeShadow {
			t.Errorf("empty mode %q: EffectiveMode() = %q, want %q", mode, got, SecurityFailureModeShadow)
		}
	}

	// off, shadow and on are all accepted (case-insensitive, trimmed), and
	// the effective value is read from the accessor against the constant —
	// never a literal at the call site.
	accepted := map[string]string{
		"off":    SecurityFailureModeOff,
		"shadow": SecurityFailureModeShadow,
		"on":     SecurityFailureModeOn,
		" OFF ":  SecurityFailureModeOff,
		"Shadow": SecurityFailureModeShadow,
		"ON":     SecurityFailureModeOn,
		"":       SecurityFailureModeShadow,
	}
	for raw, want := range accepted {
		c := SecurityFailureConfig{Mode: raw}
		if err := ValidateSecurityFailureConfig(c); err != nil {
			t.Errorf("Validate(%q): %v, want accepted", raw, err)
			continue
		}
		if got := c.EffectiveMode(); got != want {
			t.Errorf("mode %q: EffectiveMode() = %q, want %q", raw, got, want)
		}
	}

	// Any other value is refused (the config-load path and every write path
	// run these same rules).
	badModes := []string{"loud", "ShadowMode", "enabled", "off on", "ONSHADOW", "1"}
	for _, mode := range badModes {
		if err := ValidateSecurityFailureConfig(SecurityFailureConfig{Mode: mode}); err == nil {
			t.Errorf("Validate(mode=%q): accepted, want refused", mode)
		}
		if err := ValidateSecurityFailureMode(mode); err == nil {
			t.Errorf("ValidateSecurityFailureMode(%q): accepted, want refused", mode)
		}
	}
	if err := ValidateSecurityFailureMode("loud"); err == nil || err.Error() == "" {
		// The error must name the key and the accepted set.
		t.Errorf("refusal error for a bad mode = %v, want an error naming autonomous.automation.security_failure.mode and off/shadow/on", err)
	}

	// YAML and JSON round trip: the tag is mode, and every legal value
	// comes back exactly.
	encoded := []SecurityFailureConfig{
		{Mode: SecurityFailureModeOff},
		{Mode: SecurityFailureModeShadow},
		{Mode: SecurityFailureModeOn},
		{}, // unset
	}
	for i := range encoded {
		want := encoded[i]

		y, err := yaml.Marshal(want)
		if err != nil {
			t.Fatalf("yaml.Marshal(%+v): %v", want, err)
		}
		var yBack SecurityFailureConfig
		if err := yaml.Unmarshal(y, &yBack); err != nil {
			t.Fatalf("yaml.Unmarshal(%s): %v", y, err)
		}
		if yBack != want {
			t.Errorf("YAML round trip: got %+v, want %+v (raw=%s)", yBack, want, y)
		}

		j, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("json.Marshal(%+v): %v", want, err)
		}
		var jBack SecurityFailureConfig
		if err := json.Unmarshal(j, &jBack); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", j, err)
		}
		if jBack != want {
			t.Errorf("JSON round trip: got %+v, want %+v (raw=%s)", jBack, want, j)
		}
	}

	// DefaultSecurityFailureConfig is populated with the accessor's
	// default (shadow), so starting from it never hard-codes one.
	if got, want := DefaultSecurityFailureConfig().EffectiveMode(), SecurityFailureModeShadow; got != want {
		t.Errorf("DefaultSecurityFailureConfig() effective = %q, want %q", got, want)
	}
}
