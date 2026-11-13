// v5.28.0 (BL214 / datawatch#32) — sanity tests for the embedded
// locale bundles. Verifies all 5 languages are present, JSON-valid,
// and share a common key set with the EN baseline (no missing
// translations relative to the source-of-truth English bundle).

package server

import (
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
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
		"projects_subtab_plans",
		// Plans browser: backlog group, create action, the Automation heading and the move buttons.
		"plans_browser_backlog",
		"plans_browser_create_btn",
		"plans_browser_move_up",
		"plans_browser_move_down",
		"automation_plans_browser_heading",
		"observer_peer_stale",
		"settings_observer_peers",
		"settings_observer_peer_silent_alert_minutes",
		"settings_observer_peer_silent_alert_minutes_hint",
		"observer_peer_never_pushed",
		// Projects manage form action keys.
		"projects_manage_add_action",
		"projects_manage_save_action",
		"projects_manage_cancel_action",
		"projects_manage_remove_action",
		// Project link control and the Automata-without-a-project section.
		"automaton_project_label",
		"automaton_project_save",
		"projects_page_without_project",
		"projects_page_without_project_hint",
		// b4220001 — PWA splash dwell settings and the previous-version line.
		"splash_version_previous",
		"settings_splash_min_seconds",
		"settings_splash_min_seconds_hint",
		"settings_splash_min_seconds_new_version",
		"settings_splash_min_seconds_new_version_hint",
		// 70c90001 — agent tracing: settings group, the Trace card title and the Monitor card title.
		"settings_tracing_group",
		"trace_card_title",
		"stats_trace",
		// e3db0001 — capacity priority-source chip + kind table / defect intake settings.
		"priority_source_badge",
		"priority_source_explicit",
		"priority_source_kind",
		"priority_source_default",
		"priority_source_tip",
		"settings_priority_by_kind",
		"settings_priority_by_kind_hint",
		"settings_defect_intake_automation",
		"settings_defect_intake_automation_hint",
		"supervisor_loop_card_title",
		"stats_supervisor_loop",
		"prd_scope_warnings_title",
		"settings_defect_queue_group",
		"settings_defect_queue_enabled",
		"settings_defect_queue_batch_size_cap",
		"settings_defect_queue_tiny_fix_enabled",
		"settings_generated_artefacts_group",
		"settings_generated_gate_report",
		"settings_generated_combined_docs_task",
		"prd_change_record_title",
		"prd_generated_regenerate",
		"prd_generated_regenerate_failed",
		"usage_calc_whatif_label",
		"usage_calc_running_now",
		"checks_card_title",
		"ship_card_title",
		"settings_checks_group",
		"settings_ship_group",
		"burn_guard_title",
		"server_header_showing",
		"server_header_all",
		"safe_delete_stale",
		"safe_delete_computed_at",
		"burn_guard_run_now",
		"burn_guard_settings_title",
		"settings_usage_burn_guard_ask_on_change",
		"burn_guard_state_line",
		"burn_guard_decision_line",
		// Merge pipeline card empty state (ad4e0001): the project_required refusal
		// the operator sees when no project and no merge base repository is set.
		"merge_pipeline_no_project_title",
		"merge_pipeline_no_project_body",
		"merge_pipeline_no_project_choose",
		"merge_pipeline_no_project_set_base",
		"settings_merge_pipeline_conflict_mode",
		"settings_merge_pipeline_conflict_confirm_first_n",
		"merge_pipeline_conflict_confirmation_label",
		"stats_burn_guard",
		"settings_usage_burn_guard_enabled",
		"status_card_activity",
		"automation_subtab_inbox",
		"automation_subtab_automations",
		"automation_subtab_resources",
		"automation_subtab_progress",
		"automation_subtab_setup",
		"automation_subtab_housekeeping",
		// Housekeeping repairs card and Settings group.
		"settings_housekeeping_repairs_group",
		"settings_housekeeping_repairs_group_hint",
		"settings_housekeeping_task_chain",
		"settings_housekeeping_task_chain_help",
		"settings_housekeeping_advance_judge",
		"settings_housekeeping_advance_judge_help",
		"settings_housekeeping_lane_fill",
		"settings_housekeeping_lane_fill_help",
		"settings_housekeeping_leftovers",
		"settings_housekeeping_leftovers_help",
		"settings_housekeeping_planning",
		"settings_housekeeping_planning_help",
		"housekeeping_repairs_card_title",
		"housekeeping_repairs_help_link",
		"housekeeping_repairs_purpose",
		"housekeeping_repairs_grace",
		"housekeeping_repairs_group_task_chain",
		"housekeeping_repairs_group_advance_judge",
		"housekeeping_repairs_group_lane_fill",
		"housekeeping_repairs_group_leftovers",
		"housekeeping_repairs_group_planning",
		"housekeeping_repairs_mode_off",
		"housekeeping_repairs_mode_report",
		"housekeeping_repairs_mode_on",
		"housekeeping_repairs_counts",
		"housekeeping_repairs_would_line",
		"housekeeping_repairs_did_line",
		"housekeeping_repairs_failed_line",
		"housekeeping_repairs_skipped_line",
		"housekeeping_repairs_nothing",
		"housekeeping_repairs_failed",
		"automation_subtab_inbox_badge_label",
		"capacity_card_title",
		"progress_card_title",
		"nav_sessions",
		"voice_retry",
		"nav_autonomous",
		"nav_alerts",
		// e73d0004 — alerts tab: session-counting tab buttons, the heading
		// that shows both numbers, and the localized session "last" time label.
		"alert_session_last",
		"alerts_tab_active_sessions",
		"alerts_tab_historical_sessions",
		"alerts_tab_system_alerts",
		"alerts_heading_active_one",
		"alerts_heading_active_many",
		"alerts_heading_historical_one",
		"alerts_heading_historical_many",
		"alerts_heading_system_one",
		"alerts_heading_system_many",
		"nav_settings",
		"nav_projects",
		"settings_scope_lint",
		"settings_rules_context_title",
		"settings_rules_context_hint",
		"settings_rules_context_save",
		"settings_rules_context_projects",
		"rules_context_detail_title",
		"rules_context_detail_missing",
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
		// Cost report card + cost_report settings (v: cost report PWA view).
		"dash_cost_report",
		// Self-config group + recent audit (BL412).
		"settings.self_config",
		"settings.self_config.allow",
		"settings.self_config.allow_help",
		"settings.self_config.audit_path",
		"settings.self_config.audit_denied",
		"settings.self_config.audit_denied_help",
		"settings.self_config_recent",
		"settings.self_config_recent_loading",
		"settings.self_config_recent_empty",
		"settings.self_config_recent_more",
		"settings.self_config_recent_unavailable",
		"cost_report_card_title",
		"cost_report_help_link",
		"cost_report_refresh",
		"cost_report_group_by",
		"cost_report_window_days",
		"settings_cost_report",
		"settings_cost_report_default_group_by",
		"settings_cost_report_default_window_days",
		"settings_cost_report_show_usd_estimates",
		// Usage billing settings card + cost report billing columns
		// (v: add PWA settings card + report columns). The card labels,
		// the assumed-default badge, the mode/unit vocabulary, and the
		// new report columns the operator actually sees.
		"settings_usage_billing_hint",
		"settings_usage_billing_empty",
		"settings_usage_billing_add",
		"settings_usage_billing_provider",
		"settings_usage_billing_mode",
		"settings_usage_billing_plan",
		"settings_usage_billing_monthly_price",
		"settings_usage_billing_currency",
		"settings_usage_billing_reset",
		"settings_usage_billing_reset_weekday",
		"settings_usage_billing_reset_time",
		"settings_usage_billing_allowance_unit",
		"settings_usage_billing_unset",
		"settings_usage_billing_assumed",
		"settings_usage_billing_assumed_hint",
		"settings_usage_billing_remove",
		"settings_usage_billing_remove_confirm",
		"settings_usage_billing_provider_missing",
		"settings_usage_billing_saved",
		"settings_usage_billing_save_failed",
		"settings_usage_billing_removed",
		"settings_usage_billing_mode_bundled_plan",
		"settings_usage_billing_mode_api_metered",
		"settings_usage_billing_mode_free_local",
		"settings_usage_billing_unit_percent",
		"settings_usage_billing_unit_requests",
		"settings_usage_billing_unit_tokens",
		"cost_report_column_billing",
		"cost_report_column_points",
		"cost_report_column_share",
		"cost_report_column_api_equiv",
		"cost_report_column_units",
		"cost_report_points_value",
		"cost_report_share_value",
		"cost_report_group_release",
		"request_watch_card_title",
		"tags_menu_button",
		"tag_session_edit_title",
		"tag_session_batch_title",
		"tag_registry_status",
		"compute_tls_group_title",
		"compute_observed_peers_title",
		"compute_observed_peers_hint",
		"compute_observed_peer_add",
		"compute_observed_peer_add_tip",
		"compute_unanswered_title",
		"compute_unanswered_hint",
		"compute_tls_ca_file",
		"compute_tls_cert_fingerprint",
		"compute_tls_insecure_skip_verify",
		"compute_tls_insecure_skip_verify_warn",
		"compute_tls_off_badge",
		"compute_tls_off_badge_tip",
		"compute_tls_pinned_badge",
		"compute_tls_ca_badge",
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
		// Lane fill eligibility: the refusal chips, the lane counts and modes
		// lines, and the settings rows for the new autonomous.lane_fill keys.
		"lane_fill_eligibility_stale",
		"lane_fill_eligibility_held",
		"lane_fill_eligibility_sequenced_after_unshipped",
		"lane_fill_eligibility_proposal_not_confirmed",
		"lane_fill_eligibility_proposal_missing",
		"lane_fill_eligibility_main_carries_work",
		"lane_fill_eligibility_worktree_invalid",
		"lane_fill_lane_counts",
		"lane_fill_lane_counts_part",
		"lane_fill_lane_counts_source_caps",
		"lane_fill_lane_counts_source_config",
		"lane_fill_kind_modes",
		"lane_fill_kind_modes_part",
		"settings_lane_fill_stale_after_hours",
		"settings_lane_fill_require_confirmed_proposal",
		"settings_lane_fill_check_main_carries",
		"settings_lane_fill_require_worktree",
		"settings_lane_fill_lane_counts_from_caps",
		"settings_lane_fill_gpu_mode",
		"settings_lane_fill_claude_mode",
		// Decision line card, waiting_decision task label and the two gate settings.
		"decision_line_title",
		"decision_line_empty",
		"decision_state_open",
		"decision_state_answered",
		"decision_state_withdrawn",
		"decision_state_expired",
		"decision_answer_id",
		"decision_answer_actor",
		"prd_task_waiting_decision",
		"settings_automata_questions_decision_gate",
		"settings_automata_questions_decision_gate_mode",
		// Identity card actions: the explicit clear and the way back from it.
		"identity_btn_reset",
		"identity_btn_revert",
		"identity_btn_restore",
		"settings_tags_default_view",
		"settings_tags_attention_statuses",
		"settings_tags_default_match_mode",
		// Shared Automata and Automation tile: the open and overflow actions.
		"work_tile_open",
		"work_tile_more",
		"settings_usage_calculator_variant",
		"settings_usage_calculator_reset_zone_server_note",
		// Quality-gate scoping — default test-scope select for all Automata.
		"settings_gate_scope",
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
		"settings_automata_questions_worker_marker",
		"settings_automata_questions_worker_marker_help",
		"decision_answer_link",
		"decision_answer_panel_quote",
		"decision_answer_panel_option",
		"decision_answer_panel_actor",
		"decision_answer_panel_time",
		"decision_answer_panel_loading",
		"decision_answer_panel_failed",
		"decision_answer_panel_missing",
		// Repo CI watch status line (Automaton view) — the title every
		// non-English bundle must carry even before translations land.
		"repo_ci_watch_status_title",
		// Supervisor watch card (Automaton view), its Monitor card title
		// and the settings label of its mode — every bundle carries them.
		"supervisor_watch_title",
		"supervisor_watch_no_findings",
		"settings_supervisor_watch_mode",
		"stats_supervisor_watch",
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

		// Plugin services settings card (Settings → Plugins): the card title,
		// each of the five plugins.services field labels, the lifecycle-mode
		// labels and the empty-state note.
		"settings_plugin_services_title",
		"settings_plugin_services_health_interval",
		"settings_plugin_services_backlog_growth",
		"settings_plugin_services_node_load",
		"settings_plugin_services_small_max_params",
		"settings_plugin_services_small_context_tokens",
		"settings_plugin_services_per_service",
		"settings_plugin_services_lifecycle",
		"settings_plugin_services_no_services",
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
		"federation_observer_pull_label",
		"federation_observer_pull_interval",
		"federation_observer_pull_timeout",
		"federation_observer_pull_default_hint",
		"federation_observer_pull_source_push",
		"federation_observer_pull_source_pull",
		"federation_observer_pull_source_shadow",
		"federation_observer_pull_stale",
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
		// Version-skew audit (2026-10-27): the high-visibility keys the
		// operator sees while a federated peer is selected — the About-tab
		// version-skew note, the shared "Showing:" server label and the
		// local-server fallback label behind it.
		"about_version_skew_note",
		"settings_showing_server",
		"server_local_label",
		"lifecycle_sub_form_group_warnings",
		"lifecycle_sub_form_opt_in_hint",
		// 2026-10-26 — autonomous triage: the settings field for
		// `autonomous.automation.triage_alert_minutes` and the triage
		// card title in the Automaton detail view.
		"settings_automation_triage_alert_minutes",
		"settings_automation_create_search_mode",
		"settings_automation_create_search_threshold",
		"triage_card_title",
		// The stuck-running watchdog bound on the Automata settings card.
		"settings_stuck_running_seconds",
		// Automaton merge-pending check: the two settings fields, the two
		// operator-visible merge badge labels and the safe-to-delete card title.
		"settings_automation_child_inherit",
		"settings_automation_merge_pending_mode",
		"settings_automation_merge_pending_alert_minutes",
		"merge_state_pending",
		"merge_state_needs_look",
		"safe_delete_card_title",
		// Step 3 PWA (f1f20010) — operator-question channel: queue card
		// title / empty / disabled texts, answer button, history control,
		// answer-form submit label, new-question notice.
		"automation_questions_title",
		"automation_questions_empty",
		"automation_questions_disabled",
		"automation_questions_open",
		"automation_questions_history",
		"automation_questions_answer_submit",
		"automation_questions_raised",
		// Search-before-create overlap block shown after a create succeeds.
		"overlap_warnings_title",
		// The read-only security failure card in the Automation view.
		"automation_security_failure_title",
		"automation_security_failure_empty",
		"automation_security_failure_failed",
		"automation_security_failure_report_empty",
		"automation_security_failure_unknown",
		"automation_security_failure_run",
		"automation_security_failure_dependency",
		"automation_security_failure_advisory",
		"automation_security_failure_published",
		"automation_security_failure_fixed_version",
		"automation_security_failure_registry",
		"automation_security_failure_registry_none",
		"automation_security_failure_registry_unreadable",
		"automation_security_failure_lookup_failed",
		"automation_security_failure_recommendation",
		"automation_security_failure_recommend_upgrade",
		"automation_security_failure_recommend_keep",
		"automation_security_failure_recommend_keep_no_entry",
		"automation_security_failure_recommend_unknown",
		"automation_security_failure_mode",
		"automation_security_failure_action_stopped",
		"automation_security_failure_action_would_stop",
		"automation_security_failure_action_report_only",
		"automation_security_failure_action_other",
		"automation_security_failure_note",
		// The Infrastructure retries card in the Automation view.
		"infra_retry_card_title",
		"infra_retry_held_label",
		// Uncommitted-work check: the card title in the Automata overview and
		// the two settings labels.
		"uncommitted_work_card_title",
		"settings_automation_uncommitted_work_policy",
		"settings_automation_uncommitted_work_scope",
		// The Served models card in the compute node view.
		"model_routing_served_models_title",
		// The completion-review outcome on the attention row and Automaton detail.
		"rules_review_action_retries_spent",
		"rules_review_action_fix_task_failed",
		"settings_auto_fix_retries_help",
		// Project policy panel (Settings → Automata): every visible string of
		// the per-project policy tier view, one key per setting plus the
		// panel chrome (legend, badges, controls, messages).
		"project_policy_title",
		"project_policy_guide",
		"project_policy_legend",
		"project_policy_source_automaton",
		"project_policy_source_project",
		"project_policy_source_daemon",
		"project_policy_project",
		"project_policy_automaton_hint",
		"project_policy_no_projects",
		"project_policy_loading",
		"project_policy_saved",
		"project_policy_invalid",
		"project_policy_clear",
		"project_policy_apply",
		"project_policy_refresh",
		"project_policy_max",
		"project_policy_bool_on",
		"project_policy_bool_off",
		"project_policy_group_plan",
		"project_policy_group_retry",
		"project_policy_group_git",
		"project_policy_group_lanes",
		"project_policy_group_recent",
		"project_policy_plan_coverage_check",
		"project_policy_plan_coverage_min_ratio",
		"project_policy_plan_review_mode",
		"project_policy_scope_lint",
		"project_policy_retry_policy",
		"project_policy_retry_ceiling_total",
		"project_policy_auto_fix_retries",
		"project_policy_infra_retry_escalate_after",
		"project_policy_failure_escalation",
		"project_policy_default_auto_pr",
		"project_policy_max_concurrent_tasks",
		"project_policy_plan_tier_max_inflight",
		"project_policy_lane_fill_gpu_lanes",
		"project_policy_lane_fill_claude_lanes",
		"project_policy_uncommitted_work_policy",
		"project_policy_uncommitted_work_scope",
		"project_policy_burn_guard_ask_on_change",
		"project_policy_parallel_read_only_tasks",
		"project_policy_parallel_worktree_per_task",
		"project_policy_escalation_on",
		"project_policy_escalation_off",
		// Classification verdicts (LLM judge verification mode): the LLM
		// settings selector rows and the verdict card fields shown on a
		// classify-mode verdict (route + score vector).
		"llm_verification_mode_default",
		"llm_verification_mode_generate",
		"llm_verification_mode_classify",
		"llm_verification_mode_hint",
		"classif_verdict_title",
		"classif_mode",
		"classif_route",
		"classif_route_path",
		"classif_scores",
		"classif_pass_probability",
		"classif_threshold",
		"classif_severity_scores",
		"classif_category",
		"classif_low_confidence",
		// The Planning routing card in Settings → Automata.
		"planning_routing_title",
		"planning_routing_enabled",
		"planning_routing_shadow",
		"planning_routing_field_enabled",
		"planning_routing_field_shadow",
		"planning_routing_rules_title",
		"planning_routing_json_save",
		"planning_routing_test_run",
		"planning_routing_scorecard_title",
		// Requirements report card + model pull consent card in the web
		// setup view, and the node backend-versions detail row.
		"requirements_report_card_title",
		"requirements_report_section_title",
		"requirements_report_capabilities",
		"requirements_report_backends",
		"requirements_report_missing",
		"requirements_report_minimum",
		"requirements_report_node_flagged",
		"requirements_report_placeholder",
		"requirements_report_failed",
		"model_consent_card_title",
		"model_consent_section_title",
		"model_consent_intro",
		"model_consent_models",
		"model_consent_publishers",
		"model_consent_publisher",
		"model_consent_add_model",
		"model_consent_add_publisher",
		"model_consent_save",
		"model_consent_saved",
		"model_consent_save_failed",
		"model_consent_failed",
		"node_backend_versions",
		"node_backend_versions_loading",
		"node_backend_below_minimum",
		"node_backend_versions_failed",
		// Placement tags (v5.28.x): sessions list/grouping, tag menu section,
		// dashboard+Gantt filter, and the settings group the operator sees.
		"sessions_group_none",
		"session_group_filter_on",
		"tags_menu_placement",
		"placement_tag_tip",
		"sessions_group_untagged",
		// v9.159.0 — the two labeled placement facts (tile/card) + the Model axis.
		"sessions_runs_on",
		"sessions_model_on",
		"sessions_runs_on_tip",
		"sessions_model_on_tip",
		"sessions_group_model",
		"dash_filter_placement_all",
		"dash_filter_placement_axis",
		"settings_placement_group",
		"settings_placement_enabled",
		"settings_placement_compute",
		"settings_placement_project_template",
		// Protected paths: the card title in the Automata overview, the settings group
		// and the three settings labels.
		"protected_paths_card_title",
		"settings_automation_protected_paths_group",
		"settings_automation_protected_paths",
		"settings_automation_protected_paths_action",
		"settings_automation_rules_review_source",
		// Plugin service status card (kind:service cards in the Plugin Manager):
		// the card title, the three state labels and the two next-action hints —
		// all of them are what the operator actually sees on a service plugin.
		"plugin_service_card_title",
		"plugin_service_up",
		"plugin_service_down",
		"plugin_service_unknown",
		"plugin_service_action_start",
		"plugin_service_action_check_running",
		// Host load check (autonomous.host_load_alert) — the three PWA
		// Settings labels every non-English bundle must carry, English
		// placeholders pending the Android app's translations.
		"settings_host_load_alert_mode",
		"settings_host_load_alert_multiple",
		"settings_host_load_alert_minutes",
		// Release (autonomous.push_and_tag, autonomous.release_path,
		// git.release_ci_window_minutes) — the PWA Settings *Release* section
		// every non-English bundle must carry.
		"settings_release_section",
		"settings_release_push_and_tag",
		"settings_release_push_and_tag_help",
		"settings_release_path",
		"settings_release_path_help",
		"settings_release_ci_window",
		"settings_release_ci_window_help",
		"settings_release_help_link_label",
		"settings_release_last_status",
		// 6a1b0001 — Automata Inbox triage cards: headline and primary-action keys.
		"triage_headline_failed",
		"triage_headline_blocked",
		"triage_headline_merge_pending",
		"triage_headline_none",
		"triage_action_open_details",
		"triage_action_retry",
		"triage_action_resolve_review",
		"triage_action_cancel",
		"triage_detail_title",
		"triage_question_answer_in_place",
		// Pending items: the group heading and the nine settings labels.
		"settings_pending_items_group",
		"settings_automation_pending_item_policy",
		"settings_automation_pending_item_auto_resume",
		"settings_automation_pending_item_source_path",
		"settings_automation_pending_item_source_id_pattern",
		"settings_automation_pending_item_source_id_column",
		"settings_automation_pending_item_source_title_column",
		"settings_automation_pending_item_source_state_column",
		"settings_automation_pending_item_source_done_values",
		"settings_automation_pending_item_source_on_missing_row",
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

// datawatch-app parity issue — to be filed by the merge step (no gh here).
// Title: feat(i18n): add settings_defect_queue_* keys for defect queue settings parity
// New keys (English values in internal/server/web/locales/en.json; DE/ES/FR/JA
// currently carry English placeholders, please translate through the Compose pipeline).
// Each field key below also has a matching "<key>_hint" help string:
//
//	settings_defect_queue_group
//	settings_defect_queue_priority_highest_member
//	settings_defect_queue_priority_first_member
//	settings_defect_queue_enabled
//	settings_defect_queue_batch_size_cap
//	settings_defect_queue_batch_grouping_keys
//	settings_defect_queue_batch_allow_unrelated
//	settings_defect_queue_batch_priority_rule
//	settings_defect_queue_tiny_fix_enabled
//	settings_defect_queue_tiny_fix_max_files
//	settings_defect_queue_tiny_fix_allowed_path_globs
//	settings_defect_queue_tiny_fix_allowed_kinds
//	settings_defect_queue_tiny_fix_operator_visible_path_globs
//
// TestDefectQueueLocales asserts all five locales carry every defect_queue key.
func TestDefectQueueLocales(t *testing.T) {
	en := loadLocaleBundle(t, "en")
	var want []string
	for k := range en {
		if strings.HasPrefix(k, "settings_defect_queue_") {
			want = append(want, k)
		}
	}
	if len(want) < 23 {
		t.Fatalf("en carries only %d defect_queue keys, expected at least 23", len(want))
	}
	for _, lang := range requiredLocales {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range want {
			if v, ok := bundle[k]; !ok || v == "" {
				t.Errorf("locale %s missing defect_queue key %q", lang, k)
			}
		}
	}
}

// TestLocalesConflictKeys asserts that every bundle carries the merge
// pipeline conflict mode and confirmation keys, so no language falls back
// to a missing string for the conflict settings or confirmation counter.
func TestLocalesConflictKeys(t *testing.T) {
	keys := []string{
		"settings_merge_pipeline_conflict_mode",
		"settings_merge_pipeline_conflict_mode_hint",
		"settings_merge_pipeline_conflict_confirm_first_n",
		"settings_merge_pipeline_conflict_confirm_first_n_hint",
		"merge_pipeline_conflict_confirmation_label",
		"merge_pipeline_conflict_confirmation_counted",
		"merge_pipeline_conflict_confirmation_remaining",
	}
	for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
		bundle := loadLocaleBundle(t, lang)
		for _, k := range keys {
			if v, ok := bundle[k]; !ok || v == "" {
				t.Errorf("locale %s is missing conflict key %q", lang, k)
			}
		}
	}
}
