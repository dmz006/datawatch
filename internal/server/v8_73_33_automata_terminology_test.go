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
	"merge_state_pending":                             true,
	"merge_state_needs_look":                          true,
	"merge_state_merged":                              true,
	"merge_state_empty":                               true,
	"merge_state_unknown":                             true,
	"merge_badge_title":                               true,
	"safe_delete_card_title":                          true,
	"safe_delete_safe_heading":                        true,
	"safe_delete_held_heading":                        true,
	"safe_delete_worktree":                            true,
	"safe_delete_commits":                             true,
	"safe_delete_state":                               true,
	"safe_delete_failed":                              true,
	"safe_delete_read_only":                           true,
	"safe_delete_safe_empty":                          true,
	"safe_delete_held_empty":                          true,
	"safe_delete_dirty":                               true,
	"settings_automation_merge_pending_mode":          true,
	"settings_automation_merge_pending_alert_minutes": true,
	// Cleanup status card and its settings fields: English until the app translation lands.
	"settings_automation_cleanup_mode":                  true,
	"settings_automation_cleanup_retention_hours":       true,
	"settings_automation_cleanup_max_removals_per_pass": true,
	"settings_automation_disk_alert_percent":            true,
	"cleanup_card_title":                                true,
	"cleanup_read_only":                                 true,
	"cleanup_pass_worktrees":                            true,
	"cleanup_pass_temp":                                 true,
	"cleanup_mode_line":                                 true,
	"cleanup_last_run":                                  true,
	"cleanup_never_run":                                 true,
	"cleanup_counts":                                    true,
	"cleanup_bytes_freed":                               true,
	"cleanup_split":                                     true,
	"cleanup_last_error":                                true,
	"cleanup_error_none":                                true,
	"cleanup_disk_heading":                              true,
	"cleanup_disk_worktrees":                            true,
	"cleanup_disk_temp":                                 true,
	"cleanup_disk_unreadable":                           true,
	"cleanup_disk_none":                                 true,
	"cleanup_disk_threshold":                            true,
	"cleanup_disk_alert_badge":                          true,
	"cleanup_disk_ok_badge":                             true,
	"cleanup_disk_alert_last":                           true,
	"cleanup_failed":                                    true,
	// Merged-member policy setting, the Automation detail decision view and the decision-log entry: English until the app translation lands.
	"settings_automation_merged_member_policy":  true,
	"automation_merged_members_title":           true,
	"automation_merged_member_member":           true,
	"automation_merged_member_branch":           true,
	"automation_merged_member_commit":           true,
	"automation_merged_member_policy":           true,
	"automation_merged_member_outcome":          true,
	"automation_merged_member_outcome_advanced": true,
	"automation_merged_member_outcome_blocked":  true,
	"automation_decision_merged_member":         true,
	// datawatch-app issue 301: verifier-wait lane occupancy ships this
	// settings label as the English placeholder until the app sends back
	// DE/ES/FR/JA translations.
	"settings_lane_fill_count_verifier_wait": true,
	// datawatch-app issue 306: the security failure card ships these two
	// action lines as English placeholders until the translations arrive.
	"automation_security_failure_action_stopped":    true,
	"automation_security_failure_action_would_stop": true,
	// Supervisor watch settings label: English placeholder until the app
	// translation lands.
	"settings_supervisor_watch_silent_minutes": true,
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
