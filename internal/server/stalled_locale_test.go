// StateStalled label — every locale bundle must carry the "stalled"
// session-state label (session_chip_stalled, the key group that already
// holds the waiting-input / active / done session-state labels) with a
// non-empty value, so the new state renders a translated label rather than
// falling back to the raw state string.
package server

import (
	"testing"
)

func TestLocales_SessionStalledLabelPresent(t *testing.T) {
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		v, ok := bundle["session_chip_stalled"]
		if !ok {
			t.Errorf("locale %s is missing the session-state label key session_chip_stalled", lang)
			continue
		}
		if v == "" {
			t.Errorf("locale %s has an empty value for session_chip_stalled", lang)
		}
	}
}

// Liveness settings and session-state chip keys: every key the settings
// fields and the state chips look up must exist with a non-empty value in
// every locale bundle.
func TestLocales_LivenessSettingsAndChipKeysPresent(t *testing.T) {
	keys := []string{
		"settings_session_liveness_window_label",
		"settings_session_liveness_window_hint",
		"settings_session_liveness_overrides_label",
		"settings_session_liveness_overrides_hint",
		"session_chip_all",
		"session_chip_waiting",
		"session_chip_stalled",
		"session_chip_running",
		"session_chip_rate_limited",
		"session_chip_complete",
		"session_chip_failed",
		"session_chip_killed",
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range keys {
			v, ok := bundle[k]
			if !ok {
				t.Errorf("locale %s is missing key %s", lang, k)
				continue
			}
			if v == "" {
				t.Errorf("locale %s has an empty value for %s", lang, k)
			}
		}
	}
}
