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
