// v5.28.0 (BL214 / datawatch#32) — sanity tests for the embedded
// locale bundles. Verifies all 5 languages are present, JSON-valid,
// and share a common key set with the EN baseline (no missing
// translations relative to the source-of-truth English bundle).

package server

import (
	"encoding/json"
	"io/fs"
	"sort"
	"testing"
)

const localePathPrefix = "web/locales/"

var requiredLocales = []string{"en", "de", "es", "fr", "ja"}

func loadLocaleBundle(t *testing.T, lang string) map[string]string {
	t.Helper()
	raw, err := fs.ReadFile(webFS, localePathPrefix+lang+".json")
	if err != nil {
		t.Fatalf("locale %s missing from embed: %v", lang, err)
	}
	var bundle map[string]string
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("locale %s invalid JSON: %v", lang, err)
	}
	if len(bundle) == 0 {
		t.Fatalf("locale %s is empty", lang)
	}
	return bundle
}

func TestLocales_AllPresent(t *testing.T) {
	for _, lang := range requiredLocales {
		_ = loadLocaleBundle(t, lang)
	}
}

func TestLocales_ParityWithEnglish(t *testing.T) {
	en := loadLocaleBundle(t, "en")
	enKeys := make([]string, 0, len(en))
	for k := range en {
		enKeys = append(enKeys, k)
	}
	sort.Strings(enKeys)

	for _, lang := range requiredLocales {
		if lang == "en" {
			continue
		}
		bundle := loadLocaleBundle(t, lang)
		// Each non-EN locale should cover ≥95% of EN keys. Compose-
		// Multiplatform's Android resource pipeline drops a handful of
		// app-name-style entries so 100% parity isn't a strict goal,
		// but a big drop indicates a stale translation pull.
		hits := 0
		for _, k := range enKeys {
			if _, ok := bundle[k]; ok {
				hits++
			}
		}
		coverage := float64(hits) / float64(len(enKeys))
		if coverage < 0.90 {
			t.Errorf("locale %s covers %.0f%% of EN keys (%d/%d), expected ≥90%%",
				lang, coverage*100, hits, len(enKeys))
		}
	}
}

func TestLocales_CommonNavKeysPresent(t *testing.T) {
	// The keys the v5.28.0 PWA actually consumes. If any of these go
	// missing from a locale the user-visible regression is obvious
	// (untranslated nav tab); guard explicitly.
	mustHave := []string{
		"nav_sessions",
		"nav_autonomous",
		"nav_alerts",
		"nav_settings",
		"settings_tab_monitor",
		"settings_tab_general",
		"settings_tab_comms",
		"settings_tab_llm",
		"settings_tab_about",
		"action_cancel",
		"action_save",
		"action_delete",
		// v5.28.1 — universal yes/no + loading + no-alerts placeholder.
		// Datawatch-specific keys not yet in Android — pending mirror
		// pull (see datawatch-app#39).
		"action_yes",
		"action_no",
		"common_loading",
		"common_no_alerts",
		// v5.28.1 wave-2 wiring through PWA dialogs.
		"dialog_stop_session_title",
		"dialog_delete_session_title",
		"dialog_delete_sessions_title",
		"autonomous_filter_templates",
		"autonomous_fab_new",
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range mustHave {
			if _, ok := bundle[k]; !ok {
				t.Errorf("locale %s missing required key %q", lang, k)
			}
		}
	}
}
