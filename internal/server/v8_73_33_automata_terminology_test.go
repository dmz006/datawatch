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
	if key == "nav_automation" || strings.HasPrefix(key, "automation_") {
		return true
	}
	// The Events tab owner label names the Automation object, so it carries the Automation word in every locale.
	if key == "automata_events_label_automation" {
		return true
	}
	// These two settings fields name the Automation object specifically -- the
	// named Automation that receives kind-bug defect filings -- not the
	// Automaton unit of work. Their de/es/fr/ja values therefore legitimately
	// carry the localized Automatisierung/automatización/automatisation/
	// オートメーション wording (AGENT.md Automation addendum; the guard-narrowing
	// that the Terminology Rule reserves).
	return key == "settings_defect_intake_automation" ||
		key == "settings_defect_intake_automation_hint" ||
		// The placement-tags group help and the automation-axis rows name the
		// Automation object specifically -- the automation axis of the placement
		// tags (docs/howto/automata-tags.md). Their de/es/fr/ja values therefore
		// legitimately carry the localized Automatisierung/automatización/
		// automatisation/オートメーション wording, like the defect-intake
		// Automation field above. The automaton axis stays out of this exemption
		// (it must be the feature word Automat/autómata/automate/オートマトン).
		key == "settings_placement_group_help" ||
		key == "settings_placement_automation" ||
		key == "settings_placement_automation_help" ||
		key == "settings_placement_automation_template"
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
	"cleanup_skipped_by_reason":                         true,
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
	// Cost report card group label: the value names "Automaton" and ships as
	// the English placeholder until the app translation lands.
	"cost_report_group_automaton": true,
	// The question quality and queue preview settings labels ship as the
	// English text in the four non-English bundles until the app translation
	// lands.
	"settings_automata_questions_quality_mode":               true,
	"settings_automata_questions_quality_min_text_chars":     true,
	"settings_automata_questions_quality_tracker_id_pattern": true,
	"settings_automata_questions_queue_preview_lines":        true,
	// Uncommitted-work card, task chip, Automaton banner and settings labels: English until the app translation lands.
	"uncommitted_work_card_title":                 true,
	"uncommitted_work_read_only":                  true,
	"uncommitted_work_empty":                      true,
	"uncommitted_work_held_task":                  true,
	"uncommitted_work_held_automaton":             true,
	"uncommitted_work_failed":                     true,
	"uncommitted_work_chip_tip":                   true,
	"uncommitted_work_banner":                     true,
	"settings_automation_uncommitted_work_policy": true,
	"settings_automation_uncommitted_work_scope":  true,
	// Checks and ship status cards: English until the app translation lands.
	"checks_card_title":     true,
	"ship_card_title":       true,
	"checks_ship_read_only": true,
	"checks_failed":         true,
	"ship_failed":           true,
	// Lane fill eligibility settings labels: English until the app translation lands.
	"settings_lane_fill_stale_after_hours":          true,
	"settings_lane_fill_require_confirmed_proposal": true,
	"settings_lane_fill_check_main_carries":         true,
	"settings_lane_fill_require_worktree":           true,
	// Parallel task worktree settings and task merge badge: English until the app translation lands.
	"settings_parallel_worktree_per_task": true,
	"task_merge_badge_tip":                true,
	"task_merge_badge_tip_commit":         true,
	// Model request progress card: English until the app translation lands.
	"request_watch_card_title": true,
	"request_watch_read_only":  true,
	"request_watch_empty":      true,
	"request_watch_failed":     true,
	// v1 held-questions: the held section card ships its strings as the
	// English placeholders until the app sends back DE/ES/FR/JA
	// translations (the values name Automaton/Automata and must not be
	// half-translated or swapped for the Automation word meanwhile).
	"held_question_title":           true,
	"held_question_mode_off":        true,
	"held_question_mode_shadow":     true,
	"held_question_mode_on":         true,
	"held_question_empty":           true,
	"held_question_recommended":     true,
	"held_question_default_hold":    true,
	"held_question_answer":          true,
	"held_question_open_automaton":  true,
	"held_question_open_task":       true,
	"held_question_shadow_title":    true,
	"held_question_failed":          true,
	"held_question_settings_mode":   true,
	"held_question_settings_states": true,
	// Planning routing card: English until the app translation lands.
	"planning_routing_title":             true,
	"planning_routing_desc":              true,
	"planning_routing_enabled":           true,
	"planning_routing_shadow":            true,
	"planning_routing_field_enabled":     true,
	"planning_routing_field_shadow":      true,
	"planning_routing_rules_empty":       true,
	"planning_routing_match_any":         true,
	"planning_routing_json_title":        true,
	"planning_routing_json_save":         true,
	"planning_routing_saved":             true,
	"planning_routing_save_failed":       true,
	"planning_routing_failed":            true,
	"planning_routing_invalid":           true,
	"planning_routing_test_title":        true,
	"planning_routing_test_run":          true,
	"planning_routing_test_failed":       true,
	"planning_routing_scorecard_title":   true,
	"planning_routing_scorecard_empty":   true,
	"planning_routing_gate_line":         true,
	"planning_routing_would_route_title": true,
	// Protected-paths card, task chip, Automaton banner, authorised editor and settings labels: English until the app translation lands.
	"protected_paths_card_title":                 true,
	"protected_paths_help_link":                  true,
	"protected_paths_read_only":                  true,
	"protected_paths_action_line":                true,
	"protected_paths_source_line":                true,
	"protected_paths_list_line":                  true,
	"protected_paths_list_none":                  true,
	"protected_paths_action_hold":                true,
	"protected_paths_action_warn":                true,
	"protected_paths_action_block":               true,
	"protected_paths_action_off":                 true,
	"rules_review_source_base_commit":            true,
	"rules_review_source_worktree":               true,
	"protected_paths_empty":                      true,
	"protected_paths_held_task":                  true,
	"protected_paths_held_automaton":             true,
	"protected_paths_paths":                      true,
	"protected_paths_more":                       true,
	"protected_paths_reason":                     true,
	"protected_paths_since":                      true,
	"protected_paths_awaiting_spec_change":       true,
	"protected_paths_open_question":              true,
	"protected_paths_authorised_line":            true,
	"protected_paths_failed":                     true,
	"protected_paths_chip":                       true,
	"protected_paths_chip_tip":                   true,
	"protected_paths_banner":                     true,
	"protected_paths_authorised_title":           true,
	"protected_paths_authorised_hint":            true,
	"protected_paths_authorised_none":            true,
	"protected_paths_authorised_placeholder":     true,
	"protected_paths_authorised_label":           true,
	"protected_paths_authorised_save":            true,
	"protected_paths_authorised_saved":           true,
	"protected_paths_authorised_failed":          true,
	"settings_automation_protected_paths_group":  true,
	"settings_automation_protected_paths":        true,
	"settings_automation_protected_paths_action": true,
	"settings_automation_rules_review_source":    true,
	// Host load check settings labels: English until the app translation lands.
	"settings_host_load_alert_mode":     true,
	"settings_host_load_alert_multiple": true,
	"settings_host_load_alert_minutes":  true,
	// Automata Events tab: the Automaton label stays English in de/es/fr until the app translation lands.
	"automata_events_label_automaton": true,
	// Pending item settings: the labels and option labels that name an Automaton ship as English until the app translation lands.
	"settings_automation_pending_item_auto_resume": true,
	"pending_item_policy_hold":                     true,
	// Pending item card, hold badge, attach control and insert picker: the strings that name an Automaton ship as English until the app translation lands.
	"pending_item_auto_resume_line":   true,
	"pending_item_held_row":           true,
	"pending_item_attached_to":        true,
	"pending_item_attach_label":       true,
	"pending_item_attach_placeholder": true,
	"pending_item_attach_button":      true,
	"pending_item_attach_required":    true,
	"pending_item_attached_toast":     true,
	"pending_item_attach_failed":      true,
	"pending_item_badge":              true,
	"pending_item_badge_tip":          true,
	"pending_item_insert_heading":     true,
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
		{"settings_defect_intake_automation", true},
		{"settings_defect_intake_automation_hint", true},
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
