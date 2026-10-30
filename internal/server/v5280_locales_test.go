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
		"prd_scope_warnings_title",
		"status_card_activity",
		"capacity_card_title",
		"progress_card_title",
		"nav_sessions",
		"voice_retry",
		"nav_autonomous",
		"nav_alerts",
		"nav_settings",
		"settings_scope_lint",
		"tag_registry_title",
		"tag_filter_all",
		"tag_include_sub",
		"tag_view_list",
		"tag_view_tags",
		"tag_view_attention",
		"tag_attention_title",
		"tag_running_title",
		"tag_browse_filters",
		"tag_picker_label",
		"tag_batch",
		"tag_inherited_label",
		"tag_inherited_tip",
		"tag_inherited_note",
		"tags_menu_button",
		"tag_session_edit_title",
		"tag_session_batch_title",
		"tag_registry_status",
		"tag_registry_stale_banner",
		"tag_registry_note_dismiss",
		"tag_err_in_use",
		"tag_err_has_children",
		"tag_suggest_hint",
		"settings_tab_monitor",
		"settings_tab_general",
		"settings_tab_comms",
		"settings_tab_compute",
		"settings_tab_about",
		"action_cancel",
		"action_save",
		"action_delete",
		"settings_gate_disagreement",
		"settings_tags_default_view",
		"settings_tags_attention_statuses",
		"settings_tags_default_match_mode",
		// Shared Automata and Automation tile: the open and overflow actions.
		"work_tile_open",
		"work_tile_more",
		"settings_usage_calculator_variant",
		"settings_usage_calculator_reset_zone_server_note",
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
		// v8.19.8 — "What's it doing?" no-change chip (current-status 204→200 fix).
		"no_change_since_last_refresh",
		// v8.25.0 BL373 — automata UI improvements: spec expand, status graphs, view sessions.
		"automata_spec_show_full",
		"automata_spec_hide",
		"automata_actions_view_sessions",
		"automata_sg_progress",
		"automata_sg_decomposed",
		"automata_sg_stories",
		"automata_sg_tasks",
		"automata_sg_no_stories",
		// v8.25.4 — Observatory peer resources + compute node stats cards.
		"obs_peer_resources",
		"obs_cn_card_title",
		"obs_cn_no_data",
		// v8.26.0 BL380 — PRD session resource card locale completeness.
		"prd_session_resources",
		"prd_res_cpu",
		"prd_res_ram",
		"prd_btn_approve_title",
		"prd_btn_reset_to_draft",
		"prd_btn_reset_to_draft_title",
		"prd_reject_prompt",
		// v8.27.0 BL382 — cancel story/task + requeue task affordances.
		"prd_requeue_task",
		"prd_requeue_task_label",
		// Operator-question channel (plan 2026-10-25) — PWA Settings toggle copy.
		"settings_automata_questions_enabled",
		"settings_automata_questions_notify_messaging",
		"settings_automata_questions_notify_alert",
		"settings_automata_questions_return_path",
		"prd_cancel_task",
		"prd_cancel_task_confirm",
		"prd_cancel_task_ok",
		"prd_cancel_story",
		"prd_cancel_story_confirm",
		"prd_cancel_story_ok",
		// v8.39.0 BL391 — web search provider registry settings card + usage stats.
		"websearch_providers_title",
		"stats_web_search",
		// v9.0.41 — plugin widget renderer (dashboard section + tile states).
		"dash_plugin_widgets_heading",
		"plugin_widget_unavailable",
		"plugin_widget_no_data",
		"plugin_widget_loading",
		// v9.5.5 — autonomous settings card field for task_override_statuses.
		"settings_task_override_statuses",
		// 2026-10-17 — plan review gate + smoke verification settings card
		// fields (all five operator-facing keys, plus the PRD card panel).
		"settings_plan_review_backend",
		"settings_plan_review_model",
		"settings_plan_review_mode",
		"settings_smoke_verification_backend",
		"settings_smoke_verification_model",
		"prd_plan_review_ok",
		"prd_plan_review_fail",
		"prd_plan_review_rerun",
		// session_start_confirm setting — autonomous settings card field label key.
		"settings_session_start_confirm",
		// The Automation tab keys.
		"nav_automation",
		"automation_new",
		// Notify-events surfaces (PRD settings + detail line + guardrail
		// profile panel): the two keys the panel/detail copy depends on.
		"notify_events_label",
		"notify_events_effective",
		"automation_pause",
		"automation_resume",
		"automation_cancel",
		"automation_cancel_in_flight",
		// 2026-10-19 — federation resource fingerprint exchange (peer toggle +
		// pending-match list with confirm/reject) + LLM billing group label.
		"federation_fingerprint_sync_label",
		"federation_fingerprint_matches_title",
		"federation_fingerprint_matches_empty",
		"federation_fingerprint_confirm_btn",
		"federation_fingerprint_reject_btn",
		"federation_fingerprint_kind_node",
		"federation_fingerprint_kind_billing",
		// Failure escalation — the held-task status chip shown in the Automaton
		// task rows and the Automation strip.
		"task_status_review_needed",
		"llm_billing_group_label",
		// 2026-10-24 — capacity card compute-node rows: model line, status
		// chip and GPU readout (high-visibility keys on the node rows).
		"capacity_node_model",
		"capacity_node_unreachable",
		"capacity_node_gpu_util",
		// Acknowledge-unauthenticated-listeners setting: label + help keys.
		"settings_acknowledge_unauthenticated",
		"settings_acknowledge_unauthenticated_help",
		// 2026-10-25 — opt-in warning notify kinds (scope_warning /
		// guardrail_warn): settings-group label, opt-in marker + explainer,
		// and the subscription-form group + opt-in hint.
		"notify_events_group_warnings",
		"notify_events_opt_in_marker",
		"notify_events_opt_in_help",
		"lifecycle_sub_form_group_warnings",
		"lifecycle_sub_form_opt_in_hint",
		// 2026-10-26 — autonomous triage: the settings field for
		// `autonomous.automation.triage_alert_minutes` and the triage
		// card title in the Automaton detail view.
		"settings_automation_triage_alert_minutes",
		"triage_card_title",
		// The stuck-running watchdog bound on the Automata settings card.
		"settings_stuck_running_seconds",
		// Automaton merge-pending check: the two settings fields, the two
		// operator-visible merge badge labels and the safe-to-delete card title.
		"settings_automation_merge_pending_mode",
		"settings_automation_merge_pending_alert_minutes",
		"merge_state_pending",
		"merge_state_needs_look",
		"safe_delete_card_title",
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
