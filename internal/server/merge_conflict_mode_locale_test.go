// Guards the merge-pipeline conflict-mode copy in every shipped locale.
// The conflict-mode label and hint describe behaviour to the operator; they
// must not leak internal policy identifiers (stop_and_ask, mechanical_and_model,
// mechanical). Each locale must also carry a non-empty value for both keys.

package server

import (
	"strings"
	"testing"
)

func TestLocalesConflictModeCopyHasNoPolicyValues(t *testing.T) {
	conflictKeys := []string{
		"settings_merge_pipeline_conflict_mode",
		"settings_merge_pipeline_conflict_mode_hint",
	}
	bannedWords := []string{"stop_and_ask", "mechanical_and_model", "mechanical"}

	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for _, key := range conflictKeys {
			value, ok := bundle[key]
			if !ok || strings.TrimSpace(value) == "" {
				t.Errorf("locale %s: key %s is missing or empty", lang, key)
				continue
			}
			for _, word := range bannedWords {
				if strings.Contains(value, word) {
					t.Errorf("locale %s: key %s contains policy value %q: %q", lang, key, word, value)
				}
			}
		}
	}
}
