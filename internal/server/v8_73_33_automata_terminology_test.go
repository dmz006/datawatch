// v8.73.33 (GH#193) / v8.73.34 (GH#198) — guards the AGENT.md Terminology
// Rule ("Automata"/"Automaton", never "PRD", never left untranslated,
// never swapped for a different word like "Automation"): no de/es/fr/ja
// locale value may contain the literal English words "Automaton"/
// "Automata", nor the "Automation"/"automatización"/"automatisation"/
// "オートメーション" mistranslation for this feature; no locale value in any
// of the 5 bundles (English included) may say the internal-only word
// "PRD"/"PRDs".
//
// The wrong-word check no longer applies to the Automation object keys
// (nav_automation and the automation_ prefix). Automation is now a separate,
// defined object (see the addendum in AGENT.md), so its own keys legitimately
// use Automatisierung/automatización/automatisation/オートメーション. The
// untranslated-English check still applies to every key, those included.
package server

import (
	"regexp"
	"strings"
	"testing"
)

var automatonLeakRe = regexp.MustCompile(`(?i)\bAutomat(a|on)s?\b`)

// perLocaleWrongWord catches the feature mistranslated as a *different*
// word (automation/automatización/automatisation/オートメーション) rather than
// left untranslated.
var perLocaleWrongWord = map[string]*regexp.Regexp{
	"de": regexp.MustCompile(`Automatisierung`),
	"es": regexp.MustCompile(`automatización`),
	"fr": regexp.MustCompile(`automatisation`),
	"ja": regexp.MustCompile(`オートメーション`),
}

// isAutomationObjectKey reports whether a locale key belongs to the
// Automation object rather than the Automaton feature.
func isAutomationObjectKey(key string) bool {
	return key == "nav_automation" || strings.HasPrefix(key, "automation_")
}

// pendingTranslationKeys lists keys whose de/es/fr/ja value is, by the
// Localization Rule, the English en.json text until the datawatch-app
// translation issue is done. The untranslated-English check is skipped for
// such a key only while its value is exactly the en.json value, so a
// half-translated or edited value still fails the guard.
var pendingTranslationKeys = map[string]bool{
	"settings_stuck_running_seconds": true, // datawatch-app issue 290
	// Merge-pending badge, safe-to-delete report and their settings: English until the app translation lands.
	"merge_badge_title":                               true,
	"safe_delete_read_only":                           true,
	"safe_delete_safe_empty":                          true,
	"safe_delete_held_empty":                          true,
	"safe_delete_dirty":                               true,
	"settings_automation_merge_pending_mode":          true,
	"settings_automation_merge_pending_alert_minutes": true,
}

func TestLocales_AutomatonNeverUntranslatedOrMistranslated(t *testing.T) {
	en := loadLocaleBundle(t, "en")
	for _, lang := range []string{"de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		wrongWord := perLocaleWrongWord[lang]
		for key, val := range bundle {
			pending := pendingTranslationKeys[key] && val == en[key]
			if !pending && automatonLeakRe.MatchString(val) {
				t.Errorf("locale %s key %q still has the untranslated English word: %q", lang, key, val)
			}
			if wrongWord != nil && !isAutomationObjectKey(key) && wrongWord.MatchString(val) {
				t.Errorf("locale %s key %q uses the wrong word for Automaton/Automata (automation, a different concept): %q", lang, key, val)
			}
		}
	}
}

func TestLocales_AutomationKeyExemptionIsNarrow(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"nav_automation", true},
		{"automation_pause", true},
		{"fab_launch_auto", false},
		{"automata_status_draft", false},
		{"nav_automata", false},
		{"automationish", false},
	}
	for _, c := range cases {
		if got := isAutomationObjectKey(c.key); got != c.want {
			t.Errorf("isAutomationObjectKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}

	for _, lang := range []string{"de", "es", "fr", "ja"} {
		val := loadLocaleBundle(t, lang)["fab_launch_auto"]
		if val == "" {
			t.Errorf("locale %s has no fab_launch_auto value", lang)
			continue
		}
		if perLocaleWrongWord[lang].MatchString(val) {
			t.Errorf("locale %s key fab_launch_auto uses the Automation word instead of Automaton: %q", lang, val)
		}
	}
}

// GH#198 — "PRD" is internal-only (AGENT.md Terminology Rule); no
// user-facing locale value in any of the 5 bundles (English included —
// the word leaked into the English source too) may say "PRD"/"PRDs".
var prdLeakRe = regexp.MustCompile(`\bPRDs?\b`)

func TestLocales_PRDNeverUserFacing(t *testing.T) {
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for key, val := range bundle {
			if prdLeakRe.MatchString(val) {
				t.Errorf("locale %s key %q uses internal-only term PRD instead of Automaton/Automata: %q", lang, key, val)
			}
		}
	}
}
