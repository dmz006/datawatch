# Configuration parity gap report

## Summary

- Date: 2026-11-07
- Total config keys audited: 902
- Keys with at least one gap: 658
- Total gaps (key/surface pairs): 1499 (no exemptions applied; exemptions file is empty)
- Gap count per surface: web 436, get 394, put 223, configure 223, cli 223

Operator question: "are all configuratable options fully aligned with parity rule? hopefully ??"

**Plain answer: No.** 658 of 902 keys are missing from at least one surface. The web Settings UI has the most gaps; GET /api/config is next; PUT, configure and CLI share one set of gaps.

## Gaps by surface

### web (436 missing)

- `acme.apply.hot_swap`
- `acme.apply.update_mcp_cert`
- `acme.dns01.provider`
- `acme.dns01.token_secret`
- `acme.dns01.zone_id`
- `acme.domains`
- `acme.enabled`
- `acme.endpoint`
- `acme.http01.listen`
- `acme.method`
- `acme.renewal_days`
- `acme.retry.interval_minutes`
- `acme.retry.max_consecutive_failures`
- `agents.audit_format_cef`
- `agents.audit_path`
- `agents.bootstrap_token_ttl_seconds`
- `agents.callback_url`
- `agents.docker_bin`
- `agents.idle_reaper_interval_seconds`
- `agents.image_prefix`
- `agents.image_tag`
- `agents.kubectl_bin`
- `agents.pqc_bootstrap`
- `agents.secrets_base_dir`
- `agents.secrets_provider`
- `agents.worker_bootstrap_deadline_seconds`
- `aider.binary`
- `aider.console_cols`
- `aider.console_rows`
- `aider.enabled`
- `aider.input_mode`
- `aider.output_mode`
- `autonomous.checks.projects`
- `autonomous.context_file`
- `autonomous.decompose_guidance`
- `autonomous.decompose_lint.disabled_rules`
- `autonomous.decompose_lint.max_retries`
- `autonomous.decompose_lint.mode`
- `autonomous.decompose_lint.rules`
- `autonomous.decomposition_backend`
- `autonomous.decomposition_effort`
- `autonomous.decomposition_model`
- `autonomous.default_auto_pr`
- `autonomous.defect_queue.projects`
- `autonomous.detail_text_cap_bytes`
- `autonomous.drift_mode`
- `autonomous.duplicate_session_keep`
- `autonomous.execution_backend`
- `autonomous.guardrail_model`
- `autonomous.max_parallel_tasks`
- `autonomous.merge_pipeline.projects`
- `autonomous.model_pull_consent.models`
- `autonomous.model_pull_consent.publishers`
- `autonomous.notify.pending_events_max`
- `autonomous.orphan_grace_seconds`
- `autonomous.override_queued_tasks`
- `autonomous.planning_routing.classes`
- `autonomous.planning_routing.ladders`
- `autonomous.planning_routing.rules`
- `autonomous.preserve_attempt_work`
- `autonomous.project_write_serialization`
- `autonomous.refuse_dirty_tree_start`
- `autonomous.requirements.min_ollama`
- `autonomous.restart_on_timeout`
- `autonomous.restart_policy`
- `autonomous.restart_quiet_timeout_seconds`
- `autonomous.retry_policy`
- `autonomous.role_scan_interval_seconds`
- `autonomous.rules_context.context_files`
- `autonomous.rules_context.digest_max_bytes`
- `autonomous.rules_context.digest_mode`
- `autonomous.rules_context.projects`
- `autonomous.rules_context.rules_file`
- `autonomous.rules_file`
- `autonomous.scan.deps_enabled`
- `autonomous.scan.enabled`
- `autonomous.scan.fail_on_severity`
- `autonomous.scan.fix_loop_enabled`
- `autonomous.scan.fix_loop_max_retries`
- `autonomous.scan.max_findings`
- `autonomous.scan.project_rules`
- `autonomous.scan.rules_grader_enabled`
- `autonomous.scan.sast_enabled`
- `autonomous.scan.secrets_enabled`
- `autonomous.scope_breadth_enabled`
- `autonomous.ship.projects`
- `autonomous.shipped.backfill_on_start`
- `autonomous.shipped.count_separately`
- `autonomous.shipped.enabled`
- `autonomous.shipped.filter_default`
- `autonomous.shipped.show_chip`
- `autonomous.supervisor_loop`
- `autonomous.tags.placement.custom`
- `autonomous.task_templates.enabled`
- `autonomous.task_templates.projects`
- `autonomous.task_templates.templates`
- `autonomous.upstream_repos`
- `autonomous.verification_record_scores`
- `autonomous.verifier_policy.backends`
- `autonomous.verifier_policy.claude_cap_window_minutes`
- `autonomous.verifier_policy.claude_session_cap`
- `autonomous.verifier_policy.final_gate_stronger`
- `autonomous.verifier_policy.force_stronger_on_retry`
- `autonomous.verifier_policy.mode`
- `autonomous.verifier_policy.stronger_backend`
- `autonomous.verifier_policy.stronger_model`
- `autonomous.worktree_base_repo`
- `autonomous.worktree_dir`
- `classifier.publisher_allowlist`
- `compute_nodes`
- `council.backends`
- `council.comm_firehose`
- `council.draft_retention_days`
- `council.include_claude_code`
- `council.llm_ref`
- `council.max_parallel`
- `council.personas`
- `dashboard.console.enabled`
- `dashboard.console.show_ci_release`
- `dashboard.console.show_held_blocked`
- `dashboard.console.show_lanes`
- `dashboard.console.show_questions`
- `dashboard.console.show_usage`
- `dashboard.hide_inactive_cards`
- `data_dir`
- `detection.alert_repeat`
- `detection.alert_settle`
- `detection.completion_patterns`
- `detection.input_needed_patterns`
- `detection.notify_cooldown`
- `detection.prompt_debounce`
- `detection.prompt_patterns`
- `detection.rate_limit_patterns`
- `discord.auto_manage_channel`
- `discord.channel_id`
- `discord.enabled`
- `discord.token`
- `dns_channel.domain`
- `dns_channel.enabled`
- `dns_channel.listen`
- `dns_channel.max_response_size`
- `dns_channel.mode`
- `dns_channel.poll_interval`
- `dns_channel.rate_limit`
- `dns_channel.secret`
- `dns_channel.ttl`
- `dns_channel.upstream`
- `email.enabled`
- `email.from`
- `email.host`
- `email.password`
- `email.port`
- `email.to`
- `email.username`
- `gemini.binary`
- `gemini.console_cols`
- `gemini.console_rows`
- `gemini.enabled`
- `gemini.input_mode`
- `gemini.output_mode`
- `github_webhook.addr`
- `github_webhook.enabled`
- `github_webhook.secret`
- `goose.console_cols`
- `goose.console_rows`
- `goose.input_mode`
- `goose.output_mode`
- `hostname`
- `identity.backups_to_keep`
- `imap_mcp.account`
- `imap_mcp.enabled`
- `imap_mcp.subject_prefix`
- `imap_mcp.to`
- `imap_mcp.token`
- `imap_mcp.url`
- `lsp.servers`
- `matrix.access_token`
- `matrix.application_service.as_token`
- `matrix.application_service.enabled`
- `matrix.application_service.hs_token`
- `matrix.application_service.listen_addr`
- `matrix.application_service.namespace`
- `matrix.application_service.registration_file`
- `matrix.auto_manage_room`
- `matrix.device_id`
- `matrix.device_name`
- `matrix.enabled`
- `matrix.encryption.crypto_store`
- `matrix.encryption.mode`
- `matrix.homeserver`
- `matrix.room_id`
- `matrix.user_id`
- `mcp.allow_self_config`
- `mcp.elicitation.enabled`
- `mcp.prompts.enabled`
- `mcp.resources.enabled`
- `mcp.sampling.council_enabled`
- `mcp.sampling.enabled`
- `mcp.sampling.plugin_triggers`
- `mcp.sampling.triggers.alert_triage`
- `mcp.sampling.triggers.anomaly_analysis`
- `mcp.sampling.triggers.morning_briefing`
- `mcp.self_config_audit_path`
- `mcp.sse_bind_timeout`
- `mcp.token`
- `memory.dimensions`
- `memory.fallback_sqlite`
- `memory.openai_key`
- `memory.retention_chunk_days`
- `memory.retention_session_days`
- `ntfy.enabled`
- `ntfy.server_url`
- `ntfy.token`
- `ntfy.topic`
- `observer.backend_attribution`
- `observer.conn_correlator`
- `observer.docker_discovery`
- `observer.ebpf_enabled`
- `observer.federation.insecure`
- `observer.federation.parent_url`
- `observer.federation.peer_name`
- `observer.federation.push_interval_seconds`
- `observer.federation.token_path`
- `observer.gpu_attribution`
- `observer.include_kthreads`
- `observer.ollama_tap.endpoint`
- `observer.peers.allow_register`
- `observer.peers.listen_addr`
- `observer.peers.push_interval_seconds`
- `observer.peers.token_ttl_rotation_grace_s`
- `observer.plugin_enabled`
- `observer.process_tree_enabled`
- `observer.session_attribution`
- `observer.tick_interval_ms`
- `observer.top_n_broadcast`
- `ollama.console_cols`
- `ollama.console_rows`
- `ollama.detection.alert_repeat`
- `ollama.detection.alert_settle`
- `ollama.detection.completion_patterns`
- `ollama.detection.input_needed_patterns`
- `ollama.detection.notify_cooldown`
- `ollama.detection.prompt_debounce`
- `ollama.detection.prompt_patterns`
- `ollama.detection.rate_limit_patterns`
- `ollama.enabled`
- `ollama.host`
- `ollama.input_mode`
- `ollama.model`
- `ollama.output_mode`
- `opencode.binary`
- `opencode.build_temperature`
- `opencode.console_cols`
- `opencode.console_rows`
- `opencode.enabled`
- `opencode.input_mode`
- `opencode.ollama_chunk_timeout_sec`
- `opencode.ollama_header_timeout_sec`
- `opencode.output_mode`
- `opencode.providers`
- `opencode_acp.acp_health_interval`
- `opencode_acp.acp_message_timeout`
- `opencode_acp.acp_startup_timeout`
- `opencode_acp.binary`
- `opencode_acp.console_cols`
- `opencode_acp.console_rows`
- `opencode_acp.enabled`
- `opencode_acp.input_mode`
- `opencode_acp.output_mode`
- `opencode_prompt.binary`
- `opencode_prompt.console_cols`
- `opencode_prompt.console_rows`
- `opencode_prompt.enabled`
- `opencode_prompt.input_mode`
- `opencode_prompt.output_mode`
- `openwebui.api_key`
- `openwebui.console_cols`
- `openwebui.console_rows`
- `openwebui.enabled`
- `openwebui.input_mode`
- `openwebui.model`
- `openwebui.output_mode`
- `openwebui.url`
- `orchestrator.default_guardrails`
- `pi.binary`
- `pi.console_cols`
- `pi.console_rows`
- `pi.enabled`
- `pi.input_mode`
- `pi.model`
- `pi.output_mode`
- `pi_prompt.binary`
- `pi_prompt.console_cols`
- `pi_prompt.console_rows`
- `pi_prompt.enabled`
- `pi_prompt.input_mode`
- `pi_prompt.model`
- `pi_prompt.output_mode`
- `pipeline.quality_gates.block_on_regression`
- `pipeline.quality_gates.enabled`
- `pipeline.quality_gates.scope`
- `pipeline.quality_gates.smoke_packages`
- `pipeline.quality_gates.test_command`
- `pipeline.quality_gates.timeout`
- `plugins.disabled`
- `plugins.managed`
- `profiles`
- `projects`
- `proxy.circuit_breaker_reset`
- `proxy.circuit_breaker_threshold`
- `proxy.enabled`
- `proxy.health_interval`
- `proxy.offline_queue_size`
- `proxy.request_timeout`
- `push.allow_insecure_endpoints`
- `push.apns.bundle_id`
- `push.apns.enabled`
- `push.apns.key_id`
- `push.apns.key_path`
- `push.apns.key_secret`
- `push.apns.sandbox`
- `push.apns.team_id`
- `push.block_private_endpoints`
- `secrets.backend`
- `secrets.keepass_binary`
- `secrets.keepass_db`
- `secrets.keepass_group`
- `secrets.keepass_password`
- `secrets.op_binary`
- `secrets.op_token`
- `secrets.op_vault`
- `secrets.vault.address`
- `secrets.vault.auth_method`
- `secrets.vault.kv_mount`
- `secrets.vault.namespace`
- `secrets.vault.path_layout`
- `secrets.vault.path_prefix`
- `secrets.vault.request_timeout`
- `secrets.vault.tls_ca_file`
- `secrets.vault.tls_skip_verify`
- `secrets.vault.token`
- `secrets.vault.write_policy`
- `server.public_url`
- `server.tls_auto_generate`
- `server.tls_cert`
- `server.tls_enabled`
- `server.tls_key`
- `server.token`
- `servers`
- `session.alerts_rich_format`
- `session.assistant_backend`
- `session.assistant_model`
- `session.assistant_system_prompt`
- `session.auto_install_hooks`
- `session.capabilities`
- `session.claude_enabled`
- `session.cleanup_artifacts_on_end`
- `session.cost_rates`
- `session.device_aliases`
- `session.exit_hooks`
- `session.extra_mcp_servers`
- `session.file_service_root`
- `session.git_default`
- `session.gitignore_artifacts`
- `session.gitignore_check_on_start`
- `session.hook_script_dir`
- `session.llm_backend`
- `session.one_shot_sessions`
- `session.persist_telemetry_on_stop`
- `session.quick_commands`
- `session.rate_limit_global_pause`
- `session.reconcile_on_startup`
- `session.routing_rules`
- `session.secure_tracking`
- `session.session_retention_days`
- `session.splash_logo_path`
- `session.splash_tagline`
- `session.stale_timeout_seconds`
- `session.summarizer.prompt`
- `session.workspace_root`
- `shell_backend.console_cols`
- `shell_backend.console_rows`
- `shell_backend.enabled`
- `shell_backend.input_mode`
- `shell_backend.output_mode`
- `shell_backend.script_path`
- `signal.account_number`
- `signal.config_dir`
- `signal.device_name`
- `signal.group_id`
- `skills.add_default_on_start`
- `skills.auto_ignore_on_session_start`
- `skills.registries`
- `slack.auto_manage_channel`
- `slack.channel_id`
- `slack.enabled`
- `slack.token`
- `smoke.forward_token`
- `smoke.forward_url`
- `stats.ebpf_enabled`
- `tailscale.acl.allowed_peers`
- `tailscale.acl.managed_tags`
- `tailscale.api_key`
- `tailscale.auth_key`
- `tailscale.coordinator_url`
- `tailscale.enabled`
- `tailscale.image`
- `tailscale.tags`
- `telegram.auto_manage_group`
- `telegram.chat_id`
- `telegram.enabled`
- `telegram.token`
- `templates`
- `twilio.account_sid`
- `twilio.auth_token`
- `twilio.enabled`
- `twilio.from_number`
- `twilio.to_number`
- `twilio.webhook_addr`
- `twilio.webhook_public_url`
- `ui.splash_min_seconds`
- `ui.splash_min_seconds_new_version`
- `usage.billing`
- `usage.burn_guard.effort_ceiling`
- `usage.burn_guard.ladder`
- `web_search.engine`
- `web_search.num_results`
- `web_search.provider`
- `web_search.providers`
- `web_search.url`
- `webhook.addr`
- `webhook.enabled`
- `webhook.image_dir`
- `webhook.token`
- `whisper.api_key`
- `whisper.endpoint`

### get (394 missing)

- `acme.apply.update_mcp_cert`
- `acme.http01.listen`
- `acme.retry.interval_minutes`
- `acme.retry.max_consecutive_failures`
- `agents.audit_format_cef`
- `agents.audit_path`
- `agents.idle_reaper_interval_seconds`
- `agents.pqc_bootstrap`
- `agents.secrets_base_dir`
- `agents.secrets_provider`
- `autonomous.auto_approve_children`
- `autonomous.auto_fix_skip_approval`
- `autonomous.auto_tags.automation_template`
- `autonomous.auto_tags.enabled`
- `autonomous.auto_tags.kind_labels`
- `autonomous.auto_tags.kind_template`
- `autonomous.auto_tags.stage_template`
- `autonomous.automation.repo_ci_watch.alert_minutes`
- `autonomous.automation.repo_ci_watch.delete_superseded`
- `autonomous.automation.repo_ci_watch.hung_after_minutes`
- `autonomous.automation.repo_ci_watch.mode`
- `autonomous.automation.repo_ci_watch.poll_seconds`
- `autonomous.automation.repo_ci_watch.repos`
- `autonomous.automation.repo_ci_watch.security_workflows`
- `autonomous.automation.repo_ci_watch.tag_count`
- `autonomous.automation.security_failure.mode`
- `autonomous.automation.supervisor_watch.alert_minutes`
- `autonomous.automation.supervisor_watch.main_checkout`
- `autonomous.automation.supervisor_watch.mode`
- `autonomous.automation.supervisor_watch.poll_seconds`
- `autonomous.automation.supervisor_watch.script_output_path`
- `autonomous.automation.supervisor_watch.silent_minutes`
- `autonomous.automation.supervisor_watch.tmux_restart_baseline`
- `autonomous.automation.supervisor_watch.working_model`
- `autonomous.checks.after_done`
- `autonomous.checks.block_when_missing`
- `autonomous.checks.plan_rules`
- `autonomous.checks.pre_start_verifier`
- `autonomous.checks.projects`
- `autonomous.checks.registry_readme`
- `autonomous.content_auto_fix_enabled`
- `autonomous.decisions.clarity_min_text_chars`
- `autonomous.decisions.clarity_mode`
- `autonomous.decisions.clarity_require_label`
- `autonomous.decisions.clarity_require_tradeoffs`
- `autonomous.decisions.clarity_tracker_id_pattern`
- `autonomous.decisions.doc_root`
- `autonomous.decisions.doc_status_mode`
- `autonomous.decisions.doc_status_pattern`
- `autonomous.decisions.doc_status_template`
- `autonomous.decisions.enabled`
- `autonomous.decisions.folder`
- `autonomous.decisions.folder_glob`
- `autonomous.decisions.folder_withdraw_on_delete`
- `autonomous.decisions.follow_ups`
- `autonomous.decisions.front_matter_delimiter`
- `autonomous.decisions.front_matter_field_separator`
- `autonomous.decisions.front_matter_keys`
- `autonomous.decisions.front_matter_separator`
- `autonomous.decompose_guidance`
- `autonomous.decompose_lint.disabled_rules`
- `autonomous.decompose_lint.max_retries`
- `autonomous.decompose_lint.mode`
- `autonomous.decompose_lint.rules`
- `autonomous.decomposition_model`
- `autonomous.defect_queue.batch.allow_unrelated`
- `autonomous.defect_queue.batch.grouping_keys`
- `autonomous.defect_queue.batch.priority_rule`
- `autonomous.defect_queue.batch.size_cap`
- `autonomous.defect_queue.enabled`
- `autonomous.defect_queue.projects`
- `autonomous.defect_queue.tiny_fix.allowed_kinds`
- `autonomous.defect_queue.tiny_fix.allowed_path_globs`
- `autonomous.defect_queue.tiny_fix.enabled`
- `autonomous.defect_queue.tiny_fix.max_files`
- `autonomous.defect_queue.tiny_fix.operator_visible_path_globs`
- `autonomous.generated_artefacts.change_record`
- `autonomous.generated_artefacts.changelog_draft`
- `autonomous.generated_artefacts.combined_docs_task`
- `autonomous.generated_artefacts.gate_report`
- `autonomous.generated_artefacts.parity_draft`
- `autonomous.generated_artefacts.registry_row`
- `autonomous.generated_artefacts.tracker_rows`
- `autonomous.guardrail_model`
- `autonomous.infra_retry.backoff_seconds`
- `autonomous.infra_retry.categories`
- `autonomous.infra_retry.enabled`
- `autonomous.infra_retry.exhausted_action`
- `autonomous.infra_retry.max_tries`
- `autonomous.infra_retry.raise_question`
- `autonomous.infra_retry.retry_dependents`
- `autonomous.infra_retry_escalate_after`
- `autonomous.lane_fill.check_main_carries`
- `autonomous.lane_fill.claude_backend`
- `autonomous.lane_fill.claude_lanes`
- `autonomous.lane_fill.claude_mode`
- `autonomous.lane_fill.count_verifier_wait`
- `autonomous.lane_fill.decompose_ahead_min`
- `autonomous.lane_fill.gpu_backend`
- `autonomous.lane_fill.gpu_lanes`
- `autonomous.lane_fill.gpu_mode`
- `autonomous.lane_fill.gpu_model`
- `autonomous.lane_fill.idle_alert_minutes`
- `autonomous.lane_fill.lane_counts_from_caps`
- `autonomous.lane_fill.mode`
- `autonomous.lane_fill.poll_seconds`
- `autonomous.lane_fill.require_confirmed_proposal`
- `autonomous.lane_fill.require_worktree`
- `autonomous.lane_fill.stale_after_hours`
- `autonomous.max_recursion_depth`
- `autonomous.model_pull_consent.models`
- `autonomous.model_pull_consent.publishers`
- `autonomous.model_routing.alert_on_refusal`
- `autonomous.model_routing.probe_timeout_seconds`
- `autonomous.model_routing.refusal_mode`
- `autonomous.model_routing.served_models_cache_seconds`
- `autonomous.model_routing.unknown_node_policy`
- `autonomous.model_routing.verifier_follows_worker_node`
- `autonomous.notify.default_events`
- `autonomous.notify.forwarding.attempt_timeout_seconds`
- `autonomous.notify.forwarding.backoff_seconds`
- `autonomous.notify.forwarding.enabled`
- `autonomous.notify.forwarding.jitter_percent`
- `autonomous.notify.forwarding.max_attempts`
- `autonomous.notify.forwarding.max_hops`
- `autonomous.notify.pending_events_max`
- `autonomous.notify.webhook.allow_hosts`
- `autonomous.notify.webhook.attempt_timeout_seconds`
- `autonomous.notify.webhook.backoff_seconds`
- `autonomous.notify.webhook.enabled`
- `autonomous.notify.webhook.jitter_percent`
- `autonomous.notify.webhook.max_attempts`
- `autonomous.notify.webhook.max_body_bytes`
- `autonomous.notify.webhook.signature_tolerance_seconds`
- `autonomous.parallel.merge_conflict_model`
- `autonomous.parallel.merge_conflict_policy`
- `autonomous.parallel.read_only_tasks`
- `autonomous.parallel.worktree_per_task`
- `autonomous.per_story_guardrails`
- `autonomous.per_task_guardrails`
- `autonomous.planning_model`
- `autonomous.planning_routing.classes`
- `autonomous.planning_routing.enabled`
- `autonomous.planning_routing.ladders`
- `autonomous.planning_routing.rules`
- `autonomous.planning_routing.shadow`
- `autonomous.project_write_serialization`
- `autonomous.refuse_dirty_tree_start`
- `autonomous.request_watch.enabled`
- `autonomous.request_watch.gpu_busy_percent`
- `autonomous.request_watch.no_progress_minutes`
- `autonomous.request_watch.observe_verifiers`
- `autonomous.request_watch.observe_workers`
- `autonomous.request_watch.poll_seconds`
- `autonomous.request_watch.raise_alert`
- `autonomous.request_watch.raise_question`
- `autonomous.request_watch.remediation_options`
- `autonomous.request_watch.unreachable_counts`
- `autonomous.request_watch.use_gpu_signal`
- `autonomous.retry_ceiling_total`
- `autonomous.retry_policy`
- `autonomous.role_scan_interval_seconds`
- `autonomous.rules_context.context_files`
- `autonomous.rules_context.digest_max_bytes`
- `autonomous.rules_context.digest_mode`
- `autonomous.rules_context.projects`
- `autonomous.rules_context.rules_file`
- `autonomous.ship.ci_watch`
- `autonomous.ship.ci_workflows`
- `autonomous.ship.projects`
- `autonomous.ship.push`
- `autonomous.ship.remote`
- `autonomous.ship.tag`
- `autonomous.ship.tag_format`
- `autonomous.shipped.backfill_on_start`
- `autonomous.shipped.count_separately`
- `autonomous.shipped.enabled`
- `autonomous.shipped.filter_default`
- `autonomous.shipped.show_chip`
- `autonomous.tags.attention_statuses`
- `autonomous.tags.default_match_mode`
- `autonomous.tags.default_view`
- `autonomous.tags.direct_counts_visible`
- `autonomous.tags.placement.backend.enabled`
- `autonomous.tags.placement.backend.template`
- `autonomous.tags.placement.compute.enabled`
- `autonomous.tags.placement.compute.template`
- `autonomous.tags.placement.custom`
- `autonomous.tags.placement.enabled`
- `autonomous.tags.placement.lane.enabled`
- `autonomous.tags.placement.lane.template`
- `autonomous.tags.placement.peer.enabled`
- `autonomous.tags.placement.peer.template`
- `autonomous.tags.placement.project.enabled`
- `autonomous.tags.placement.project.template`
- `autonomous.tags.placement.role.enabled`
- `autonomous.tags.placement.role.template`
- `autonomous.tags.registry_keeper`
- `autonomous.tags.registry_sync_seconds`
- `autonomous.task_templates.enabled`
- `autonomous.task_templates.projects`
- `autonomous.task_templates.templates`
- `autonomous.verification_model`
- `autonomous.verifier_policy.backends`
- `autonomous.verifier_policy.claude_cap_window_minutes`
- `autonomous.verifier_policy.claude_session_cap`
- `autonomous.verifier_policy.final_gate_stronger`
- `autonomous.verifier_policy.force_stronger_on_retry`
- `autonomous.verifier_policy.mode`
- `autonomous.verifier_policy.stronger_backend`
- `autonomous.verifier_policy.stronger_model`
- `classifier.abstain_margin`
- `classifier.calibration.false_pass_weight`
- `classifier.calibration.min_fit_samples`
- `classifier.calibration.rotation_bias_threshold`
- `classifier.calibration.rotation_flip_threshold`
- `classifier.calibration.split_seed`
- `classifier.calibration.test_fraction`
- `classifier.min_option_mass`
- `classifier.min_probability`
- `classifier.probe_timeout_seconds`
- `classifier.probe_ttl_seconds`
- `classifier.publisher_allowlist`
- `classifier.rotate_options`
- `classifier.thinking_off_models`
- `classifier.timeout_seconds`
- `classifier.top_logprobs`
- `compute_nodes`
- `council.backends`
- `council.include_claude_code`
- `council.personas`
- `data_dir`
- `lsp.servers`
- `matrix.application_service.as_token`
- `matrix.application_service.enabled`
- `matrix.application_service.hs_token`
- `matrix.application_service.listen_addr`
- `matrix.application_service.namespace`
- `matrix.application_service.registration_file`
- `matrix.device_id`
- `matrix.device_name`
- `matrix.encryption.crypto_store`
- `matrix.encryption.mode`
- `mcp.allow_self_config`
- `mcp.elicitation.enabled`
- `mcp.prompts.enabled`
- `mcp.resources.enabled`
- `mcp.sampling.council_enabled`
- `mcp.sampling.enabled`
- `mcp.sampling.plugin_triggers`
- `mcp.sampling.triggers.alert_triage`
- `mcp.sampling.triggers.anomaly_analysis`
- `mcp.sampling.triggers.morning_briefing`
- `mcp.self_config_audit_path`
- `mcp.sse_bind_timeout`
- `memory.retention_chunk_days`
- `memory.retention_session_days`
- `observer.backend_attribution`
- `observer.conn_correlator`
- `observer.docker_discovery`
- `observer.ebpf_enabled`
- `observer.federation.insecure`
- `observer.federation.parent_url`
- `observer.federation.peer_name`
- `observer.federation.push_interval_seconds`
- `observer.federation.token_path`
- `observer.gpu_attribution`
- `observer.include_kthreads`
- `observer.ollama_tap.endpoint`
- `observer.peers.allow_register`
- `observer.peers.listen_addr`
- `observer.peers.push_interval_seconds`
- `observer.peers.token_ttl_rotation_grace_s`
- `observer.plugin_enabled`
- `observer.process_tree_enabled`
- `observer.session_attribution`
- `observer.tick_interval_ms`
- `observer.top_n_broadcast`
- `ollama.detection.alert_repeat`
- `ollama.detection.alert_settle`
- `ollama.detection.completion_patterns`
- `ollama.detection.input_needed_patterns`
- `ollama.detection.notify_cooldown`
- `ollama.detection.prompt_debounce`
- `ollama.detection.prompt_patterns`
- `ollama.detection.rate_limit_patterns`
- `onboarding_report.disabled_checks`
- `onboarding_report.enabled`
- `onboarding_report.include_next_steps`
- `opencode.build_temperature`
- `opencode.providers`
- `orchestrator.guardrail_model`
- `pipeline.quality_gates.block_on_regression`
- `pipeline.quality_gates.enabled`
- `pipeline.quality_gates.scope`
- `pipeline.quality_gates.smoke_packages`
- `pipeline.quality_gates.test_command`
- `pipeline.quality_gates.timeout`
- `plugins.managed`
- `projects`
- `secrets.backend`
- `secrets.keepass_binary`
- `secrets.keepass_db`
- `secrets.keepass_group`
- `secrets.keepass_password`
- `secrets.op_binary`
- `secrets.op_token`
- `secrets.op_vault`
- `secrets.vault.address`
- `secrets.vault.auth_method`
- `secrets.vault.kv_mount`
- `secrets.vault.namespace`
- `secrets.vault.path_layout`
- `secrets.vault.path_prefix`
- `secrets.vault.request_timeout`
- `secrets.vault.tls_ca_file`
- `secrets.vault.tls_skip_verify`
- `secrets.vault.token`
- `secrets.vault.write_policy`
- `server.tls_enabled`
- `servers`
- `session.alerts_rich_format`
- `session.assistant_backend`
- `session.assistant_model`
- `session.assistant_system_prompt`
- `session.auto_install_hooks`
- `session.capabilities`
- `session.capacity_wait_seconds`
- `session.cleanup_artifacts_on_end`
- `session.cost_rates`
- `session.device_aliases`
- `session.exit_hooks`
- `session.extra_mcp_servers`
- `session.file_service_root`
- `session.gitignore_artifacts`
- `session.gitignore_check_on_start`
- `session.hook_script_dir`
- `session.persist_telemetry_on_stop`
- `session.quick_commands`
- `session.rate_limit_global_pause`
- `session.reconcile_on_startup`
- `session.routing_rules`
- `session.secure_tracking`
- `session.session_retention_days`
- `session.splash_logo_path`
- `session.splash_tagline`
- `session.stale_timeout_seconds`
- `session.summarizer.model`
- `skills.add_default_on_start`
- `skills.auto_ignore_on_session_start`
- `skills.registries`
- `smoke.forward_token`
- `smoke.forward_url`
- `stats.ebpf_enabled`
- `tailscale.acl.allowed_peers`
- `tailscale.acl.managed_tags`
- `tailscale.api_key`
- `tailscale.auth_key`
- `tailscale.coordinator_url`
- `tailscale.enabled`
- `tailscale.image`
- `tailscale.tags`
- `templates`
- `tracing.arg_cap_bytes`
- `tracing.backends`
- `tracing.mode`
- `tracing.retention_days`
- `twilio.webhook_public_url`
- `usage.burn_guard.ask_on_change`
- `usage.burn_guard.cache_read_points_per_million`
- `usage.burn_guard.cache_write_points_per_million`
- `usage.burn_guard.effort_ceiling`
- `usage.burn_guard.enabled`
- `usage.burn_guard.interval_minutes`
- `usage.burn_guard.ladder`
- `usage.burn_guard.margin_points`
- `usage.burn_guard.refit_on_reading`
- `usage.burn_guard.retry_limit_claude`
- `usage.burn_guard.step_back`
- `usage.calculator.baseline_per_24h`
- `usage.calculator.cost_per_task_points`
- `usage.calculator.keeper`
- `usage.calculator.max_lanes`
- `usage.calculator.policy`
- `usage.calculator.reset_time`
- `usage.calculator.reset_weekday`
- `usage.calculator.reset_zone`
- `usage.calculator.safety_points`
- `usage.calculator.variant`
- `web_search.cache_enabled`
- `web_search.cache_ttl_seconds`
- `web_search.providers`
- `whisper.api_key`
- `whisper.endpoint`

### put (223 missing)

- `acme.apply.update_mcp_cert`
- `acme.http01.listen`
- `acme.retry.interval_minutes`
- `acme.retry.max_consecutive_failures`
- `agents.audit_format_cef`
- `agents.audit_path`
- `agents.idle_reaper_interval_seconds`
- `agents.pqc_bootstrap`
- `agents.secrets_base_dir`
- `agents.secrets_provider`
- `autonomous.checks.projects`
- `autonomous.decompose_guidance`
- `autonomous.decompose_lint.disabled_rules`
- `autonomous.decompose_lint.max_retries`
- `autonomous.decompose_lint.mode`
- `autonomous.decompose_lint.rules`
- `autonomous.defect_queue.batch.allow_unrelated`
- `autonomous.defect_queue.batch.grouping_keys`
- `autonomous.defect_queue.batch.priority_rule`
- `autonomous.defect_queue.batch.size_cap`
- `autonomous.defect_queue.enabled`
- `autonomous.defect_queue.projects`
- `autonomous.defect_queue.tiny_fix.allowed_kinds`
- `autonomous.defect_queue.tiny_fix.allowed_path_globs`
- `autonomous.defect_queue.tiny_fix.enabled`
- `autonomous.defect_queue.tiny_fix.max_files`
- `autonomous.defect_queue.tiny_fix.operator_visible_path_globs`
- `autonomous.failure_escalation.enabled`
- `autonomous.failure_escalation.max_failures`
- `autonomous.failure_escalation.steps`
- `autonomous.infra_retry.backoff_seconds`
- `autonomous.infra_retry.categories`
- `autonomous.infra_retry.enabled`
- `autonomous.infra_retry.exhausted_action`
- `autonomous.infra_retry.max_tries`
- `autonomous.infra_retry.raise_question`
- `autonomous.infra_retry.retry_dependents`
- `autonomous.merge_pipeline.conflict_model`
- `autonomous.merge_pipeline.conflict_policy`
- `autonomous.merge_pipeline.fallback_on_bad_file`
- `autonomous.merge_pipeline.file_name`
- `autonomous.merge_pipeline.post_ship.companion_repository`
- `autonomous.merge_pipeline.post_ship.enabled`
- `autonomous.merge_pipeline.post_ship.hooks`
- `autonomous.merge_pipeline.post_ship.issue_title_template`
- `autonomous.merge_pipeline.post_ship.notify_template`
- `autonomous.merge_pipeline.post_ship.registry_file`
- `autonomous.merge_pipeline.post_ship.registry_row_pattern`
- `autonomous.merge_pipeline.projects`
- `autonomous.merge_pipeline.source_order`
- `autonomous.merge_pipeline.version_files`
- `autonomous.model_pull_consent.models`
- `autonomous.model_pull_consent.publishers`
- `autonomous.model_routing.alert_on_refusal`
- `autonomous.model_routing.probe_timeout_seconds`
- `autonomous.model_routing.refusal_mode`
- `autonomous.model_routing.served_models_cache_seconds`
- `autonomous.model_routing.unknown_node_policy`
- `autonomous.model_routing.verifier_follows_worker_node`
- `autonomous.planning_routing.classes`
- `autonomous.planning_routing.ladders`
- `autonomous.planning_routing.rules`
- `autonomous.project_write_serialization`
- `autonomous.request_watch.enabled`
- `autonomous.request_watch.gpu_busy_percent`
- `autonomous.request_watch.no_progress_minutes`
- `autonomous.request_watch.observe_verifiers`
- `autonomous.request_watch.observe_workers`
- `autonomous.request_watch.poll_seconds`
- `autonomous.request_watch.raise_alert`
- `autonomous.request_watch.raise_question`
- `autonomous.request_watch.remediation_options`
- `autonomous.request_watch.unreachable_counts`
- `autonomous.request_watch.use_gpu_signal`
- `autonomous.requirements.min_ollama`
- `autonomous.role_scan_interval_seconds`
- `autonomous.scan.deps_enabled`
- `autonomous.scan.enabled`
- `autonomous.scan.fail_on_severity`
- `autonomous.scan.fix_loop_enabled`
- `autonomous.scan.fix_loop_max_retries`
- `autonomous.scan.max_findings`
- `autonomous.scan.project_rules`
- `autonomous.scan.rules_grader_enabled`
- `autonomous.scan.sast_enabled`
- `autonomous.scan.secrets_enabled`
- `autonomous.ship.projects`
- `autonomous.shipped.backfill_on_start`
- `autonomous.shipped.count_separately`
- `autonomous.shipped.enabled`
- `autonomous.shipped.filter_default`
- `autonomous.shipped.show_chip`
- `autonomous.supervisor_loop`
- `autonomous.tags.placement.custom`
- `autonomous.task_templates.enabled`
- `autonomous.task_templates.projects`
- `autonomous.task_templates.templates`
- `classifier.publisher_allowlist`
- `compute_nodes`
- `council.backends`
- `council.include_claude_code`
- `council.personas`
- `data_dir`
- `hostname`
- `lsp.servers`
- `matrix.application_service.as_token`
- `matrix.application_service.enabled`
- `matrix.application_service.hs_token`
- `matrix.application_service.listen_addr`
- `matrix.application_service.namespace`
- `matrix.application_service.registration_file`
- `matrix.device_id`
- `matrix.device_name`
- `matrix.encryption.crypto_store`
- `matrix.encryption.mode`
- `mcp.allow_self_config`
- `mcp.elicitation.enabled`
- `mcp.prompts.enabled`
- `mcp.resources.enabled`
- `mcp.sampling.council_enabled`
- `mcp.sampling.enabled`
- `mcp.sampling.plugin_triggers`
- `mcp.sampling.triggers.alert_triage`
- `mcp.sampling.triggers.anomaly_analysis`
- `mcp.sampling.triggers.morning_briefing`
- `mcp.self_config_audit_path`
- `mcp.sse_bind_timeout`
- `memory.retention_chunk_days`
- `memory.retention_session_days`
- `ollama.detection.alert_repeat`
- `ollama.detection.alert_settle`
- `ollama.detection.completion_patterns`
- `ollama.detection.input_needed_patterns`
- `ollama.detection.notify_cooldown`
- `ollama.detection.prompt_debounce`
- `ollama.detection.prompt_patterns`
- `ollama.detection.rate_limit_patterns`
- `onboarding_report.disabled_checks`
- `onboarding_report.enabled`
- `onboarding_report.include_next_steps`
- `opencode.build_temperature`
- `opencode.providers`
- `orchestrator.default_guardrails`
- `pipeline.quality_gates.block_on_regression`
- `pipeline.quality_gates.enabled`
- `pipeline.quality_gates.scope`
- `pipeline.quality_gates.smoke_packages`
- `pipeline.quality_gates.test_command`
- `pipeline.quality_gates.timeout`
- `plugins.disabled`
- `plugins.managed`
- `profiles`
- `projects`
- `secrets.backend`
- `secrets.keepass_binary`
- `secrets.keepass_db`
- `secrets.keepass_group`
- `secrets.keepass_password`
- `secrets.op_binary`
- `secrets.op_token`
- `secrets.op_vault`
- `secrets.vault.address`
- `secrets.vault.auth_method`
- `secrets.vault.kv_mount`
- `secrets.vault.namespace`
- `secrets.vault.path_layout`
- `secrets.vault.path_prefix`
- `secrets.vault.request_timeout`
- `secrets.vault.tls_ca_file`
- `secrets.vault.tls_skip_verify`
- `secrets.vault.token`
- `secrets.vault.write_policy`
- `server.tls_enabled`
- `servers`
- `session.alerts_rich_format`
- `session.assistant_backend`
- `session.assistant_model`
- `session.assistant_system_prompt`
- `session.auto_install_hooks`
- `session.capabilities`
- `session.cleanup_artifacts_on_end`
- `session.cost_rates`
- `session.device_aliases`
- `session.exit_hooks`
- `session.extra_mcp_servers`
- `session.file_service_root`
- `session.gitignore_artifacts`
- `session.gitignore_check_on_start`
- `session.hook_script_dir`
- `session.persist_telemetry_on_stop`
- `session.rate_limit_global_pause`
- `session.reconcile_on_startup`
- `session.routing_rules`
- `session.secure_tracking`
- `session.session_retention_days`
- `session.splash_logo_path`
- `session.splash_tagline`
- `session.stale_timeout_seconds`
- `session.summarizer.model`
- `signal.account_number`
- `skills.add_default_on_start`
- `skills.auto_ignore_on_session_start`
- `skills.registries`
- `smoke.forward_token`
- `smoke.forward_url`
- `stats.ebpf_enabled`
- `tailscale.acl.allowed_peers`
- `tailscale.acl.managed_tags`
- `tailscale.api_key`
- `tailscale.auth_key`
- `tailscale.coordinator_url`
- `tailscale.enabled`
- `tailscale.image`
- `tailscale.tags`
- `templates`
- `twilio.webhook_public_url`
- `usage.billing`
- `usage.burn_guard.effort_ceiling`
- `vision.max_image_bytes`
- `web_search.cache_enabled`
- `web_search.cache_ttl_seconds`
- `web_search.provider`
- `web_search.providers`

### configure (223 missing)

The configure surface forwards generically to PUT /api/config, so its gaps mirror PUT exactly (same key set).

- `acme.apply.update_mcp_cert`
- `acme.http01.listen`
- `acme.retry.interval_minutes`
- `acme.retry.max_consecutive_failures`
- `agents.audit_format_cef`
- `agents.audit_path`
- `agents.idle_reaper_interval_seconds`
- `agents.pqc_bootstrap`
- `agents.secrets_base_dir`
- `agents.secrets_provider`
- `autonomous.checks.projects`
- `autonomous.decompose_guidance`
- `autonomous.decompose_lint.disabled_rules`
- `autonomous.decompose_lint.max_retries`
- `autonomous.decompose_lint.mode`
- `autonomous.decompose_lint.rules`
- `autonomous.defect_queue.batch.allow_unrelated`
- `autonomous.defect_queue.batch.grouping_keys`
- `autonomous.defect_queue.batch.priority_rule`
- `autonomous.defect_queue.batch.size_cap`
- `autonomous.defect_queue.enabled`
- `autonomous.defect_queue.projects`
- `autonomous.defect_queue.tiny_fix.allowed_kinds`
- `autonomous.defect_queue.tiny_fix.allowed_path_globs`
- `autonomous.defect_queue.tiny_fix.enabled`
- `autonomous.defect_queue.tiny_fix.max_files`
- `autonomous.defect_queue.tiny_fix.operator_visible_path_globs`
- `autonomous.failure_escalation.enabled`
- `autonomous.failure_escalation.max_failures`
- `autonomous.failure_escalation.steps`
- `autonomous.infra_retry.backoff_seconds`
- `autonomous.infra_retry.categories`
- `autonomous.infra_retry.enabled`
- `autonomous.infra_retry.exhausted_action`
- `autonomous.infra_retry.max_tries`
- `autonomous.infra_retry.raise_question`
- `autonomous.infra_retry.retry_dependents`
- `autonomous.merge_pipeline.conflict_model`
- `autonomous.merge_pipeline.conflict_policy`
- `autonomous.merge_pipeline.fallback_on_bad_file`
- `autonomous.merge_pipeline.file_name`
- `autonomous.merge_pipeline.post_ship.companion_repository`
- `autonomous.merge_pipeline.post_ship.enabled`
- `autonomous.merge_pipeline.post_ship.hooks`
- `autonomous.merge_pipeline.post_ship.issue_title_template`
- `autonomous.merge_pipeline.post_ship.notify_template`
- `autonomous.merge_pipeline.post_ship.registry_file`
- `autonomous.merge_pipeline.post_ship.registry_row_pattern`
- `autonomous.merge_pipeline.projects`
- `autonomous.merge_pipeline.source_order`
- `autonomous.merge_pipeline.version_files`
- `autonomous.model_pull_consent.models`
- `autonomous.model_pull_consent.publishers`
- `autonomous.model_routing.alert_on_refusal`
- `autonomous.model_routing.probe_timeout_seconds`
- `autonomous.model_routing.refusal_mode`
- `autonomous.model_routing.served_models_cache_seconds`
- `autonomous.model_routing.unknown_node_policy`
- `autonomous.model_routing.verifier_follows_worker_node`
- `autonomous.planning_routing.classes`
- `autonomous.planning_routing.ladders`
- `autonomous.planning_routing.rules`
- `autonomous.project_write_serialization`
- `autonomous.request_watch.enabled`
- `autonomous.request_watch.gpu_busy_percent`
- `autonomous.request_watch.no_progress_minutes`
- `autonomous.request_watch.observe_verifiers`
- `autonomous.request_watch.observe_workers`
- `autonomous.request_watch.poll_seconds`
- `autonomous.request_watch.raise_alert`
- `autonomous.request_watch.raise_question`
- `autonomous.request_watch.remediation_options`
- `autonomous.request_watch.unreachable_counts`
- `autonomous.request_watch.use_gpu_signal`
- `autonomous.requirements.min_ollama`
- `autonomous.role_scan_interval_seconds`
- `autonomous.scan.deps_enabled`
- `autonomous.scan.enabled`
- `autonomous.scan.fail_on_severity`
- `autonomous.scan.fix_loop_enabled`
- `autonomous.scan.fix_loop_max_retries`
- `autonomous.scan.max_findings`
- `autonomous.scan.project_rules`
- `autonomous.scan.rules_grader_enabled`
- `autonomous.scan.sast_enabled`
- `autonomous.scan.secrets_enabled`
- `autonomous.ship.projects`
- `autonomous.shipped.backfill_on_start`
- `autonomous.shipped.count_separately`
- `autonomous.shipped.enabled`
- `autonomous.shipped.filter_default`
- `autonomous.shipped.show_chip`
- `autonomous.supervisor_loop`
- `autonomous.tags.placement.custom`
- `autonomous.task_templates.enabled`
- `autonomous.task_templates.projects`
- `autonomous.task_templates.templates`
- `classifier.publisher_allowlist`
- `compute_nodes`
- `council.backends`
- `council.include_claude_code`
- `council.personas`
- `data_dir`
- `hostname`
- `lsp.servers`
- `matrix.application_service.as_token`
- `matrix.application_service.enabled`
- `matrix.application_service.hs_token`
- `matrix.application_service.listen_addr`
- `matrix.application_service.namespace`
- `matrix.application_service.registration_file`
- `matrix.device_id`
- `matrix.device_name`
- `matrix.encryption.crypto_store`
- `matrix.encryption.mode`
- `mcp.allow_self_config`
- `mcp.elicitation.enabled`
- `mcp.prompts.enabled`
- `mcp.resources.enabled`
- `mcp.sampling.council_enabled`
- `mcp.sampling.enabled`
- `mcp.sampling.plugin_triggers`
- `mcp.sampling.triggers.alert_triage`
- `mcp.sampling.triggers.anomaly_analysis`
- `mcp.sampling.triggers.morning_briefing`
- `mcp.self_config_audit_path`
- `mcp.sse_bind_timeout`
- `memory.retention_chunk_days`
- `memory.retention_session_days`
- `ollama.detection.alert_repeat`
- `ollama.detection.alert_settle`
- `ollama.detection.completion_patterns`
- `ollama.detection.input_needed_patterns`
- `ollama.detection.notify_cooldown`
- `ollama.detection.prompt_debounce`
- `ollama.detection.prompt_patterns`
- `ollama.detection.rate_limit_patterns`
- `onboarding_report.disabled_checks`
- `onboarding_report.enabled`
- `onboarding_report.include_next_steps`
- `opencode.build_temperature`
- `opencode.providers`
- `orchestrator.default_guardrails`
- `pipeline.quality_gates.block_on_regression`
- `pipeline.quality_gates.enabled`
- `pipeline.quality_gates.scope`
- `pipeline.quality_gates.smoke_packages`
- `pipeline.quality_gates.test_command`
- `pipeline.quality_gates.timeout`
- `plugins.disabled`
- `plugins.managed`
- `profiles`
- `projects`
- `secrets.backend`
- `secrets.keepass_binary`
- `secrets.keepass_db`
- `secrets.keepass_group`
- `secrets.keepass_password`
- `secrets.op_binary`
- `secrets.op_token`
- `secrets.op_vault`
- `secrets.vault.address`
- `secrets.vault.auth_method`
- `secrets.vault.kv_mount`
- `secrets.vault.namespace`
- `secrets.vault.path_layout`
- `secrets.vault.path_prefix`
- `secrets.vault.request_timeout`
- `secrets.vault.tls_ca_file`
- `secrets.vault.tls_skip_verify`
- `secrets.vault.token`
- `secrets.vault.write_policy`
- `server.tls_enabled`
- `servers`
- `session.alerts_rich_format`
- `session.assistant_backend`
- `session.assistant_model`
- `session.assistant_system_prompt`
- `session.auto_install_hooks`
- `session.capabilities`
- `session.cleanup_artifacts_on_end`
- `session.cost_rates`
- `session.device_aliases`
- `session.exit_hooks`
- `session.extra_mcp_servers`
- `session.file_service_root`
- `session.gitignore_artifacts`
- `session.gitignore_check_on_start`
- `session.hook_script_dir`
- `session.persist_telemetry_on_stop`
- `session.rate_limit_global_pause`
- `session.reconcile_on_startup`
- `session.routing_rules`
- `session.secure_tracking`
- `session.session_retention_days`
- `session.splash_logo_path`
- `session.splash_tagline`
- `session.stale_timeout_seconds`
- `session.summarizer.model`
- `signal.account_number`
- `skills.add_default_on_start`
- `skills.auto_ignore_on_session_start`
- `skills.registries`
- `smoke.forward_token`
- `smoke.forward_url`
- `stats.ebpf_enabled`
- `tailscale.acl.allowed_peers`
- `tailscale.acl.managed_tags`
- `tailscale.api_key`
- `tailscale.auth_key`
- `tailscale.coordinator_url`
- `tailscale.enabled`
- `tailscale.image`
- `tailscale.tags`
- `templates`
- `twilio.webhook_public_url`
- `usage.billing`
- `usage.burn_guard.effort_ceiling`
- `vision.max_image_bytes`
- `web_search.cache_enabled`
- `web_search.cache_ttl_seconds`
- `web_search.provider`
- `web_search.providers`

### cli (223 missing)

The cli surface forwards generically to PUT /api/config, so its gaps mirror PUT exactly (same key set).

- `acme.apply.update_mcp_cert`
- `acme.http01.listen`
- `acme.retry.interval_minutes`
- `acme.retry.max_consecutive_failures`
- `agents.audit_format_cef`
- `agents.audit_path`
- `agents.idle_reaper_interval_seconds`
- `agents.pqc_bootstrap`
- `agents.secrets_base_dir`
- `agents.secrets_provider`
- `autonomous.checks.projects`
- `autonomous.decompose_guidance`
- `autonomous.decompose_lint.disabled_rules`
- `autonomous.decompose_lint.max_retries`
- `autonomous.decompose_lint.mode`
- `autonomous.decompose_lint.rules`
- `autonomous.defect_queue.batch.allow_unrelated`
- `autonomous.defect_queue.batch.grouping_keys`
- `autonomous.defect_queue.batch.priority_rule`
- `autonomous.defect_queue.batch.size_cap`
- `autonomous.defect_queue.enabled`
- `autonomous.defect_queue.projects`
- `autonomous.defect_queue.tiny_fix.allowed_kinds`
- `autonomous.defect_queue.tiny_fix.allowed_path_globs`
- `autonomous.defect_queue.tiny_fix.enabled`
- `autonomous.defect_queue.tiny_fix.max_files`
- `autonomous.defect_queue.tiny_fix.operator_visible_path_globs`
- `autonomous.failure_escalation.enabled`
- `autonomous.failure_escalation.max_failures`
- `autonomous.failure_escalation.steps`
- `autonomous.infra_retry.backoff_seconds`
- `autonomous.infra_retry.categories`
- `autonomous.infra_retry.enabled`
- `autonomous.infra_retry.exhausted_action`
- `autonomous.infra_retry.max_tries`
- `autonomous.infra_retry.raise_question`
- `autonomous.infra_retry.retry_dependents`
- `autonomous.merge_pipeline.conflict_model`
- `autonomous.merge_pipeline.conflict_policy`
- `autonomous.merge_pipeline.fallback_on_bad_file`
- `autonomous.merge_pipeline.file_name`
- `autonomous.merge_pipeline.post_ship.companion_repository`
- `autonomous.merge_pipeline.post_ship.enabled`
- `autonomous.merge_pipeline.post_ship.hooks`
- `autonomous.merge_pipeline.post_ship.issue_title_template`
- `autonomous.merge_pipeline.post_ship.notify_template`
- `autonomous.merge_pipeline.post_ship.registry_file`
- `autonomous.merge_pipeline.post_ship.registry_row_pattern`
- `autonomous.merge_pipeline.projects`
- `autonomous.merge_pipeline.source_order`
- `autonomous.merge_pipeline.version_files`
- `autonomous.model_pull_consent.models`
- `autonomous.model_pull_consent.publishers`
- `autonomous.model_routing.alert_on_refusal`
- `autonomous.model_routing.probe_timeout_seconds`
- `autonomous.model_routing.refusal_mode`
- `autonomous.model_routing.served_models_cache_seconds`
- `autonomous.model_routing.unknown_node_policy`
- `autonomous.model_routing.verifier_follows_worker_node`
- `autonomous.planning_routing.classes`
- `autonomous.planning_routing.ladders`
- `autonomous.planning_routing.rules`
- `autonomous.project_write_serialization`
- `autonomous.request_watch.enabled`
- `autonomous.request_watch.gpu_busy_percent`
- `autonomous.request_watch.no_progress_minutes`
- `autonomous.request_watch.observe_verifiers`
- `autonomous.request_watch.observe_workers`
- `autonomous.request_watch.poll_seconds`
- `autonomous.request_watch.raise_alert`
- `autonomous.request_watch.raise_question`
- `autonomous.request_watch.remediation_options`
- `autonomous.request_watch.unreachable_counts`
- `autonomous.request_watch.use_gpu_signal`
- `autonomous.requirements.min_ollama`
- `autonomous.role_scan_interval_seconds`
- `autonomous.scan.deps_enabled`
- `autonomous.scan.enabled`
- `autonomous.scan.fail_on_severity`
- `autonomous.scan.fix_loop_enabled`
- `autonomous.scan.fix_loop_max_retries`
- `autonomous.scan.max_findings`
- `autonomous.scan.project_rules`
- `autonomous.scan.rules_grader_enabled`
- `autonomous.scan.sast_enabled`
- `autonomous.scan.secrets_enabled`
- `autonomous.ship.projects`
- `autonomous.shipped.backfill_on_start`
- `autonomous.shipped.count_separately`
- `autonomous.shipped.enabled`
- `autonomous.shipped.filter_default`
- `autonomous.shipped.show_chip`
- `autonomous.supervisor_loop`
- `autonomous.tags.placement.custom`
- `autonomous.task_templates.enabled`
- `autonomous.task_templates.projects`
- `autonomous.task_templates.templates`
- `classifier.publisher_allowlist`
- `compute_nodes`
- `council.backends`
- `council.include_claude_code`
- `council.personas`
- `data_dir`
- `hostname`
- `lsp.servers`
- `matrix.application_service.as_token`
- `matrix.application_service.enabled`
- `matrix.application_service.hs_token`
- `matrix.application_service.listen_addr`
- `matrix.application_service.namespace`
- `matrix.application_service.registration_file`
- `matrix.device_id`
- `matrix.device_name`
- `matrix.encryption.crypto_store`
- `matrix.encryption.mode`
- `mcp.allow_self_config`
- `mcp.elicitation.enabled`
- `mcp.prompts.enabled`
- `mcp.resources.enabled`
- `mcp.sampling.council_enabled`
- `mcp.sampling.enabled`
- `mcp.sampling.plugin_triggers`
- `mcp.sampling.triggers.alert_triage`
- `mcp.sampling.triggers.anomaly_analysis`
- `mcp.sampling.triggers.morning_briefing`
- `mcp.self_config_audit_path`
- `mcp.sse_bind_timeout`
- `memory.retention_chunk_days`
- `memory.retention_session_days`
- `ollama.detection.alert_repeat`
- `ollama.detection.alert_settle`
- `ollama.detection.completion_patterns`
- `ollama.detection.input_needed_patterns`
- `ollama.detection.notify_cooldown`
- `ollama.detection.prompt_debounce`
- `ollama.detection.prompt_patterns`
- `ollama.detection.rate_limit_patterns`
- `onboarding_report.disabled_checks`
- `onboarding_report.enabled`
- `onboarding_report.include_next_steps`
- `opencode.build_temperature`
- `opencode.providers`
- `orchestrator.default_guardrails`
- `pipeline.quality_gates.block_on_regression`
- `pipeline.quality_gates.enabled`
- `pipeline.quality_gates.scope`
- `pipeline.quality_gates.smoke_packages`
- `pipeline.quality_gates.test_command`
- `pipeline.quality_gates.timeout`
- `plugins.disabled`
- `plugins.managed`
- `profiles`
- `projects`
- `secrets.backend`
- `secrets.keepass_binary`
- `secrets.keepass_db`
- `secrets.keepass_group`
- `secrets.keepass_password`
- `secrets.op_binary`
- `secrets.op_token`
- `secrets.op_vault`
- `secrets.vault.address`
- `secrets.vault.auth_method`
- `secrets.vault.kv_mount`
- `secrets.vault.namespace`
- `secrets.vault.path_layout`
- `secrets.vault.path_prefix`
- `secrets.vault.request_timeout`
- `secrets.vault.tls_ca_file`
- `secrets.vault.tls_skip_verify`
- `secrets.vault.token`
- `secrets.vault.write_policy`
- `server.tls_enabled`
- `servers`
- `session.alerts_rich_format`
- `session.assistant_backend`
- `session.assistant_model`
- `session.assistant_system_prompt`
- `session.auto_install_hooks`
- `session.capabilities`
- `session.cleanup_artifacts_on_end`
- `session.cost_rates`
- `session.device_aliases`
- `session.exit_hooks`
- `session.extra_mcp_servers`
- `session.file_service_root`
- `session.gitignore_artifacts`
- `session.gitignore_check_on_start`
- `session.hook_script_dir`
- `session.persist_telemetry_on_stop`
- `session.rate_limit_global_pause`
- `session.reconcile_on_startup`
- `session.routing_rules`
- `session.secure_tracking`
- `session.session_retention_days`
- `session.splash_logo_path`
- `session.splash_tagline`
- `session.stale_timeout_seconds`
- `session.summarizer.model`
- `signal.account_number`
- `skills.add_default_on_start`
- `skills.auto_ignore_on_session_start`
- `skills.registries`
- `smoke.forward_token`
- `smoke.forward_url`
- `stats.ebpf_enabled`
- `tailscale.acl.allowed_peers`
- `tailscale.acl.managed_tags`
- `tailscale.api_key`
- `tailscale.auth_key`
- `tailscale.coordinator_url`
- `tailscale.enabled`
- `tailscale.image`
- `tailscale.tags`
- `templates`
- `twilio.webhook_public_url`
- `usage.billing`
- `usage.burn_guard.effort_ceiling`
- `vision.max_image_bytes`
- `web_search.cache_enabled`
- `web_search.cache_ttl_seconds`
- `web_search.provider`
- `web_search.providers`

## Keys to file

One row per key, grouped by feature area (first key segment). Each row can be filed as its own work item. Configure and cli appear together with put because they mirror it.

### acme

| Key | Missing surfaces |
|---|---|
| `acme.apply.hot_swap` | web |
| `acme.apply.update_mcp_cert` | web, get, put, configure, cli |
| `acme.dns01.provider` | web |
| `acme.dns01.token_secret` | web |
| `acme.dns01.zone_id` | web |
| `acme.domains` | web |
| `acme.enabled` | web |
| `acme.endpoint` | web |
| `acme.http01.listen` | web, get, put, configure, cli |
| `acme.method` | web |
| `acme.renewal_days` | web |
| `acme.retry.interval_minutes` | web, get, put, configure, cli |
| `acme.retry.max_consecutive_failures` | web, get, put, configure, cli |

### agents

| Key | Missing surfaces |
|---|---|
| `agents.audit_format_cef` | web, get, put, configure, cli |
| `agents.audit_path` | web, get, put, configure, cli |
| `agents.bootstrap_token_ttl_seconds` | web |
| `agents.callback_url` | web |
| `agents.docker_bin` | web |
| `agents.idle_reaper_interval_seconds` | web, get, put, configure, cli |
| `agents.image_prefix` | web |
| `agents.image_tag` | web |
| `agents.kubectl_bin` | web |
| `agents.pqc_bootstrap` | web, get, put, configure, cli |
| `agents.secrets_base_dir` | web, get, put, configure, cli |
| `agents.secrets_provider` | web, get, put, configure, cli |
| `agents.worker_bootstrap_deadline_seconds` | web |

### aider

| Key | Missing surfaces |
|---|---|
| `aider.binary` | web |
| `aider.console_cols` | web |
| `aider.console_rows` | web |
| `aider.enabled` | web |
| `aider.input_mode` | web |
| `aider.output_mode` | web |

### autonomous

| Key | Missing surfaces |
|---|---|
| `autonomous.auto_approve_children` | get |
| `autonomous.auto_fix_skip_approval` | get |
| `autonomous.auto_tags.automation_template` | get |
| `autonomous.auto_tags.enabled` | get |
| `autonomous.auto_tags.kind_labels` | get |
| `autonomous.auto_tags.kind_template` | get |
| `autonomous.auto_tags.stage_template` | get |
| `autonomous.automation.repo_ci_watch.alert_minutes` | get |
| `autonomous.automation.repo_ci_watch.delete_superseded` | get |
| `autonomous.automation.repo_ci_watch.hung_after_minutes` | get |
| `autonomous.automation.repo_ci_watch.mode` | get |
| `autonomous.automation.repo_ci_watch.poll_seconds` | get |
| `autonomous.automation.repo_ci_watch.repos` | get |
| `autonomous.automation.repo_ci_watch.security_workflows` | get |
| `autonomous.automation.repo_ci_watch.tag_count` | get |
| `autonomous.automation.security_failure.mode` | get |
| `autonomous.automation.supervisor_watch.alert_minutes` | get |
| `autonomous.automation.supervisor_watch.main_checkout` | get |
| `autonomous.automation.supervisor_watch.mode` | get |
| `autonomous.automation.supervisor_watch.poll_seconds` | get |
| `autonomous.automation.supervisor_watch.script_output_path` | get |
| `autonomous.automation.supervisor_watch.silent_minutes` | get |
| `autonomous.automation.supervisor_watch.tmux_restart_baseline` | get |
| `autonomous.automation.supervisor_watch.working_model` | get |
| `autonomous.checks.after_done` | get |
| `autonomous.checks.block_when_missing` | get |
| `autonomous.checks.plan_rules` | get |
| `autonomous.checks.pre_start_verifier` | get |
| `autonomous.checks.projects` | web, get, put, configure, cli |
| `autonomous.checks.registry_readme` | get |
| `autonomous.content_auto_fix_enabled` | get |
| `autonomous.context_file` | web |
| `autonomous.decisions.clarity_min_text_chars` | get |
| `autonomous.decisions.clarity_mode` | get |
| `autonomous.decisions.clarity_require_label` | get |
| `autonomous.decisions.clarity_require_tradeoffs` | get |
| `autonomous.decisions.clarity_tracker_id_pattern` | get |
| `autonomous.decisions.doc_root` | get |
| `autonomous.decisions.doc_status_mode` | get |
| `autonomous.decisions.doc_status_pattern` | get |
| `autonomous.decisions.doc_status_template` | get |
| `autonomous.decisions.enabled` | get |
| `autonomous.decisions.folder` | get |
| `autonomous.decisions.folder_glob` | get |
| `autonomous.decisions.folder_withdraw_on_delete` | get |
| `autonomous.decisions.follow_ups` | get |
| `autonomous.decisions.front_matter_delimiter` | get |
| `autonomous.decisions.front_matter_field_separator` | get |
| `autonomous.decisions.front_matter_keys` | get |
| `autonomous.decisions.front_matter_separator` | get |
| `autonomous.decompose_guidance` | web, get, put, configure, cli |
| `autonomous.decompose_lint.disabled_rules` | web, get, put, configure, cli |
| `autonomous.decompose_lint.max_retries` | web, get, put, configure, cli |
| `autonomous.decompose_lint.mode` | web, get, put, configure, cli |
| `autonomous.decompose_lint.rules` | web, get, put, configure, cli |
| `autonomous.decomposition_backend` | web |
| `autonomous.decomposition_effort` | web |
| `autonomous.decomposition_model` | web, get |
| `autonomous.default_auto_pr` | web |
| `autonomous.defect_queue.batch.allow_unrelated` | get, put, configure, cli |
| `autonomous.defect_queue.batch.grouping_keys` | get, put, configure, cli |
| `autonomous.defect_queue.batch.priority_rule` | get, put, configure, cli |
| `autonomous.defect_queue.batch.size_cap` | get, put, configure, cli |
| `autonomous.defect_queue.enabled` | get, put, configure, cli |
| `autonomous.defect_queue.projects` | web, get, put, configure, cli |
| `autonomous.defect_queue.tiny_fix.allowed_kinds` | get, put, configure, cli |
| `autonomous.defect_queue.tiny_fix.allowed_path_globs` | get, put, configure, cli |
| `autonomous.defect_queue.tiny_fix.enabled` | get, put, configure, cli |
| `autonomous.defect_queue.tiny_fix.max_files` | get, put, configure, cli |
| `autonomous.defect_queue.tiny_fix.operator_visible_path_globs` | get, put, configure, cli |
| `autonomous.detail_text_cap_bytes` | web |
| `autonomous.drift_mode` | web |
| `autonomous.duplicate_session_keep` | web |
| `autonomous.execution_backend` | web |
| `autonomous.failure_escalation.enabled` | put, configure, cli |
| `autonomous.failure_escalation.max_failures` | put, configure, cli |
| `autonomous.failure_escalation.steps` | put, configure, cli |
| `autonomous.generated_artefacts.change_record` | get |
| `autonomous.generated_artefacts.changelog_draft` | get |
| `autonomous.generated_artefacts.combined_docs_task` | get |
| `autonomous.generated_artefacts.gate_report` | get |
| `autonomous.generated_artefacts.parity_draft` | get |
| `autonomous.generated_artefacts.registry_row` | get |
| `autonomous.generated_artefacts.tracker_rows` | get |
| `autonomous.guardrail_model` | web, get |
| `autonomous.infra_retry.backoff_seconds` | get, put, configure, cli |
| `autonomous.infra_retry.categories` | get, put, configure, cli |
| `autonomous.infra_retry.enabled` | get, put, configure, cli |
| `autonomous.infra_retry.exhausted_action` | get, put, configure, cli |
| `autonomous.infra_retry.max_tries` | get, put, configure, cli |
| `autonomous.infra_retry.raise_question` | get, put, configure, cli |
| `autonomous.infra_retry.retry_dependents` | get, put, configure, cli |
| `autonomous.infra_retry_escalate_after` | get |
| `autonomous.lane_fill.check_main_carries` | get |
| `autonomous.lane_fill.claude_backend` | get |
| `autonomous.lane_fill.claude_lanes` | get |
| `autonomous.lane_fill.claude_mode` | get |
| `autonomous.lane_fill.count_verifier_wait` | get |
| `autonomous.lane_fill.decompose_ahead_min` | get |
| `autonomous.lane_fill.gpu_backend` | get |
| `autonomous.lane_fill.gpu_lanes` | get |
| `autonomous.lane_fill.gpu_mode` | get |
| `autonomous.lane_fill.gpu_model` | get |
| `autonomous.lane_fill.idle_alert_minutes` | get |
| `autonomous.lane_fill.lane_counts_from_caps` | get |
| `autonomous.lane_fill.mode` | get |
| `autonomous.lane_fill.poll_seconds` | get |
| `autonomous.lane_fill.require_confirmed_proposal` | get |
| `autonomous.lane_fill.require_worktree` | get |
| `autonomous.lane_fill.stale_after_hours` | get |
| `autonomous.max_parallel_tasks` | web |
| `autonomous.max_recursion_depth` | get |
| `autonomous.merge_pipeline.conflict_model` | put, configure, cli |
| `autonomous.merge_pipeline.conflict_policy` | put, configure, cli |
| `autonomous.merge_pipeline.fallback_on_bad_file` | put, configure, cli |
| `autonomous.merge_pipeline.file_name` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.companion_repository` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.enabled` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.hooks` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.issue_title_template` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.notify_template` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.registry_file` | put, configure, cli |
| `autonomous.merge_pipeline.post_ship.registry_row_pattern` | put, configure, cli |
| `autonomous.merge_pipeline.projects` | web, put, configure, cli |
| `autonomous.merge_pipeline.source_order` | put, configure, cli |
| `autonomous.merge_pipeline.version_files` | put, configure, cli |
| `autonomous.model_pull_consent.models` | web, get, put, configure, cli |
| `autonomous.model_pull_consent.publishers` | web, get, put, configure, cli |
| `autonomous.model_routing.alert_on_refusal` | get, put, configure, cli |
| `autonomous.model_routing.probe_timeout_seconds` | get, put, configure, cli |
| `autonomous.model_routing.refusal_mode` | get, put, configure, cli |
| `autonomous.model_routing.served_models_cache_seconds` | get, put, configure, cli |
| `autonomous.model_routing.unknown_node_policy` | get, put, configure, cli |
| `autonomous.model_routing.verifier_follows_worker_node` | get, put, configure, cli |
| `autonomous.notify.default_events` | get |
| `autonomous.notify.forwarding.attempt_timeout_seconds` | get |
| `autonomous.notify.forwarding.backoff_seconds` | get |
| `autonomous.notify.forwarding.enabled` | get |
| `autonomous.notify.forwarding.jitter_percent` | get |
| `autonomous.notify.forwarding.max_attempts` | get |
| `autonomous.notify.forwarding.max_hops` | get |
| `autonomous.notify.pending_events_max` | web, get |
| `autonomous.notify.webhook.allow_hosts` | get |
| `autonomous.notify.webhook.attempt_timeout_seconds` | get |
| `autonomous.notify.webhook.backoff_seconds` | get |
| `autonomous.notify.webhook.enabled` | get |
| `autonomous.notify.webhook.jitter_percent` | get |
| `autonomous.notify.webhook.max_attempts` | get |
| `autonomous.notify.webhook.max_body_bytes` | get |
| `autonomous.notify.webhook.signature_tolerance_seconds` | get |
| `autonomous.orphan_grace_seconds` | web |
| `autonomous.override_queued_tasks` | web |
| `autonomous.parallel.merge_conflict_model` | get |
| `autonomous.parallel.merge_conflict_policy` | get |
| `autonomous.parallel.read_only_tasks` | get |
| `autonomous.parallel.worktree_per_task` | get |
| `autonomous.per_story_guardrails` | get |
| `autonomous.per_task_guardrails` | get |
| `autonomous.planning_model` | get |
| `autonomous.planning_routing.classes` | web, get, put, configure, cli |
| `autonomous.planning_routing.enabled` | get |
| `autonomous.planning_routing.ladders` | web, get, put, configure, cli |
| `autonomous.planning_routing.rules` | web, get, put, configure, cli |
| `autonomous.planning_routing.shadow` | get |
| `autonomous.preserve_attempt_work` | web |
| `autonomous.project_write_serialization` | web, get, put, configure, cli |
| `autonomous.refuse_dirty_tree_start` | web, get |
| `autonomous.request_watch.enabled` | get, put, configure, cli |
| `autonomous.request_watch.gpu_busy_percent` | get, put, configure, cli |
| `autonomous.request_watch.no_progress_minutes` | get, put, configure, cli |
| `autonomous.request_watch.observe_verifiers` | get, put, configure, cli |
| `autonomous.request_watch.observe_workers` | get, put, configure, cli |
| `autonomous.request_watch.poll_seconds` | get, put, configure, cli |
| `autonomous.request_watch.raise_alert` | get, put, configure, cli |
| `autonomous.request_watch.raise_question` | get, put, configure, cli |
| `autonomous.request_watch.remediation_options` | get, put, configure, cli |
| `autonomous.request_watch.unreachable_counts` | get, put, configure, cli |
| `autonomous.request_watch.use_gpu_signal` | get, put, configure, cli |
| `autonomous.requirements.min_ollama` | web, put, configure, cli |
| `autonomous.restart_on_timeout` | web |
| `autonomous.restart_policy` | web |
| `autonomous.restart_quiet_timeout_seconds` | web |
| `autonomous.retry_ceiling_total` | get |
| `autonomous.retry_policy` | web, get |
| `autonomous.role_scan_interval_seconds` | web, get, put, configure, cli |
| `autonomous.rules_context.context_files` | web, get |
| `autonomous.rules_context.digest_max_bytes` | web, get |
| `autonomous.rules_context.digest_mode` | web, get |
| `autonomous.rules_context.projects` | web, get |
| `autonomous.rules_context.rules_file` | web, get |
| `autonomous.rules_file` | web |
| `autonomous.scan.deps_enabled` | web, put, configure, cli |
| `autonomous.scan.enabled` | web, put, configure, cli |
| `autonomous.scan.fail_on_severity` | web, put, configure, cli |
| `autonomous.scan.fix_loop_enabled` | web, put, configure, cli |
| `autonomous.scan.fix_loop_max_retries` | web, put, configure, cli |
| `autonomous.scan.max_findings` | web, put, configure, cli |
| `autonomous.scan.project_rules` | web, put, configure, cli |
| `autonomous.scan.rules_grader_enabled` | web, put, configure, cli |
| `autonomous.scan.sast_enabled` | web, put, configure, cli |
| `autonomous.scan.secrets_enabled` | web, put, configure, cli |
| `autonomous.scope_breadth_enabled` | web |
| `autonomous.ship.ci_watch` | get |
| `autonomous.ship.ci_workflows` | get |
| `autonomous.ship.projects` | web, get, put, configure, cli |
| `autonomous.ship.push` | get |
| `autonomous.ship.remote` | get |
| `autonomous.ship.tag` | get |
| `autonomous.ship.tag_format` | get |
| `autonomous.shipped.backfill_on_start` | web, get, put, configure, cli |
| `autonomous.shipped.count_separately` | web, get, put, configure, cli |
| `autonomous.shipped.enabled` | web, get, put, configure, cli |
| `autonomous.shipped.filter_default` | web, get, put, configure, cli |
| `autonomous.shipped.show_chip` | web, get, put, configure, cli |
| `autonomous.supervisor_loop` | web, put, configure, cli |
| `autonomous.tags.attention_statuses` | get |
| `autonomous.tags.default_match_mode` | get |
| `autonomous.tags.default_view` | get |
| `autonomous.tags.direct_counts_visible` | get |
| `autonomous.tags.placement.backend.enabled` | get |
| `autonomous.tags.placement.backend.template` | get |
| `autonomous.tags.placement.compute.enabled` | get |
| `autonomous.tags.placement.compute.template` | get |
| `autonomous.tags.placement.custom` | web, get, put, configure, cli |
| `autonomous.tags.placement.enabled` | get |
| `autonomous.tags.placement.lane.enabled` | get |
| `autonomous.tags.placement.lane.template` | get |
| `autonomous.tags.placement.peer.enabled` | get |
| `autonomous.tags.placement.peer.template` | get |
| `autonomous.tags.placement.project.enabled` | get |
| `autonomous.tags.placement.project.template` | get |
| `autonomous.tags.placement.role.enabled` | get |
| `autonomous.tags.placement.role.template` | get |
| `autonomous.tags.registry_keeper` | get |
| `autonomous.tags.registry_sync_seconds` | get |
| `autonomous.task_templates.enabled` | web, get, put, configure, cli |
| `autonomous.task_templates.projects` | web, get, put, configure, cli |
| `autonomous.task_templates.templates` | web, get, put, configure, cli |
| `autonomous.upstream_repos` | web |
| `autonomous.verification_model` | get |
| `autonomous.verification_record_scores` | web |
| `autonomous.verifier_policy.backends` | web, get |
| `autonomous.verifier_policy.claude_cap_window_minutes` | web, get |
| `autonomous.verifier_policy.claude_session_cap` | web, get |
| `autonomous.verifier_policy.final_gate_stronger` | web, get |
| `autonomous.verifier_policy.force_stronger_on_retry` | web, get |
| `autonomous.verifier_policy.mode` | web, get |
| `autonomous.verifier_policy.stronger_backend` | web, get |
| `autonomous.verifier_policy.stronger_model` | web, get |
| `autonomous.worktree_base_repo` | web |
| `autonomous.worktree_dir` | web |

### classifier

| Key | Missing surfaces |
|---|---|
| `classifier.abstain_margin` | get |
| `classifier.calibration.false_pass_weight` | get |
| `classifier.calibration.min_fit_samples` | get |
| `classifier.calibration.rotation_bias_threshold` | get |
| `classifier.calibration.rotation_flip_threshold` | get |
| `classifier.calibration.split_seed` | get |
| `classifier.calibration.test_fraction` | get |
| `classifier.min_option_mass` | get |
| `classifier.min_probability` | get |
| `classifier.probe_timeout_seconds` | get |
| `classifier.probe_ttl_seconds` | get |
| `classifier.publisher_allowlist` | web, get, put, configure, cli |
| `classifier.rotate_options` | get |
| `classifier.thinking_off_models` | get |
| `classifier.timeout_seconds` | get |
| `classifier.top_logprobs` | get |

### compute_nodes

| Key | Missing surfaces |
|---|---|
| `compute_nodes` | web, get, put, configure, cli |

### council

| Key | Missing surfaces |
|---|---|
| `council.backends` | web, get, put, configure, cli |
| `council.comm_firehose` | web |
| `council.draft_retention_days` | web |
| `council.include_claude_code` | web, get, put, configure, cli |
| `council.llm_ref` | web |
| `council.max_parallel` | web |
| `council.personas` | web, get, put, configure, cli |

### dashboard

| Key | Missing surfaces |
|---|---|
| `dashboard.console.enabled` | web |
| `dashboard.console.show_ci_release` | web |
| `dashboard.console.show_held_blocked` | web |
| `dashboard.console.show_lanes` | web |
| `dashboard.console.show_questions` | web |
| `dashboard.console.show_usage` | web |
| `dashboard.hide_inactive_cards` | web |

### data_dir

| Key | Missing surfaces |
|---|---|
| `data_dir` | web, get, put, configure, cli |

### detection

| Key | Missing surfaces |
|---|---|
| `detection.alert_repeat` | web |
| `detection.alert_settle` | web |
| `detection.completion_patterns` | web |
| `detection.input_needed_patterns` | web |
| `detection.notify_cooldown` | web |
| `detection.prompt_debounce` | web |
| `detection.prompt_patterns` | web |
| `detection.rate_limit_patterns` | web |

### discord

| Key | Missing surfaces |
|---|---|
| `discord.auto_manage_channel` | web |
| `discord.channel_id` | web |
| `discord.enabled` | web |
| `discord.token` | web |

### dns_channel

| Key | Missing surfaces |
|---|---|
| `dns_channel.domain` | web |
| `dns_channel.enabled` | web |
| `dns_channel.listen` | web |
| `dns_channel.max_response_size` | web |
| `dns_channel.mode` | web |
| `dns_channel.poll_interval` | web |
| `dns_channel.rate_limit` | web |
| `dns_channel.secret` | web |
| `dns_channel.ttl` | web |
| `dns_channel.upstream` | web |

### email

| Key | Missing surfaces |
|---|---|
| `email.enabled` | web |
| `email.from` | web |
| `email.host` | web |
| `email.password` | web |
| `email.port` | web |
| `email.to` | web |
| `email.username` | web |

### gemini

| Key | Missing surfaces |
|---|---|
| `gemini.binary` | web |
| `gemini.console_cols` | web |
| `gemini.console_rows` | web |
| `gemini.enabled` | web |
| `gemini.input_mode` | web |
| `gemini.output_mode` | web |

### github_webhook

| Key | Missing surfaces |
|---|---|
| `github_webhook.addr` | web |
| `github_webhook.enabled` | web |
| `github_webhook.secret` | web |

### goose

| Key | Missing surfaces |
|---|---|
| `goose.console_cols` | web |
| `goose.console_rows` | web |
| `goose.input_mode` | web |
| `goose.output_mode` | web |

### hostname

| Key | Missing surfaces |
|---|---|
| `hostname` | web, put, configure, cli |

### identity

| Key | Missing surfaces |
|---|---|
| `identity.backups_to_keep` | web |

### imap_mcp

| Key | Missing surfaces |
|---|---|
| `imap_mcp.account` | web |
| `imap_mcp.enabled` | web |
| `imap_mcp.subject_prefix` | web |
| `imap_mcp.to` | web |
| `imap_mcp.token` | web |
| `imap_mcp.url` | web |

### lsp

| Key | Missing surfaces |
|---|---|
| `lsp.servers` | web, get, put, configure, cli |

### matrix

| Key | Missing surfaces |
|---|---|
| `matrix.access_token` | web |
| `matrix.application_service.as_token` | web, get, put, configure, cli |
| `matrix.application_service.enabled` | web, get, put, configure, cli |
| `matrix.application_service.hs_token` | web, get, put, configure, cli |
| `matrix.application_service.listen_addr` | web, get, put, configure, cli |
| `matrix.application_service.namespace` | web, get, put, configure, cli |
| `matrix.application_service.registration_file` | web, get, put, configure, cli |
| `matrix.auto_manage_room` | web |
| `matrix.device_id` | web, get, put, configure, cli |
| `matrix.device_name` | web, get, put, configure, cli |
| `matrix.enabled` | web |
| `matrix.encryption.crypto_store` | web, get, put, configure, cli |
| `matrix.encryption.mode` | web, get, put, configure, cli |
| `matrix.homeserver` | web |
| `matrix.room_id` | web |
| `matrix.user_id` | web |

### mcp

| Key | Missing surfaces |
|---|---|
| `mcp.allow_self_config` | web, get, put, configure, cli |
| `mcp.elicitation.enabled` | web, get, put, configure, cli |
| `mcp.prompts.enabled` | web, get, put, configure, cli |
| `mcp.resources.enabled` | web, get, put, configure, cli |
| `mcp.sampling.council_enabled` | web, get, put, configure, cli |
| `mcp.sampling.enabled` | web, get, put, configure, cli |
| `mcp.sampling.plugin_triggers` | web, get, put, configure, cli |
| `mcp.sampling.triggers.alert_triage` | web, get, put, configure, cli |
| `mcp.sampling.triggers.anomaly_analysis` | web, get, put, configure, cli |
| `mcp.sampling.triggers.morning_briefing` | web, get, put, configure, cli |
| `mcp.self_config_audit_path` | web, get, put, configure, cli |
| `mcp.sse_bind_timeout` | web, get, put, configure, cli |
| `mcp.token` | web |

### memory

| Key | Missing surfaces |
|---|---|
| `memory.dimensions` | web |
| `memory.fallback_sqlite` | web |
| `memory.openai_key` | web |
| `memory.retention_chunk_days` | web, get, put, configure, cli |
| `memory.retention_session_days` | web, get, put, configure, cli |

### ntfy

| Key | Missing surfaces |
|---|---|
| `ntfy.enabled` | web |
| `ntfy.server_url` | web |
| `ntfy.token` | web |
| `ntfy.topic` | web |

### observer

| Key | Missing surfaces |
|---|---|
| `observer.backend_attribution` | web, get |
| `observer.conn_correlator` | web, get |
| `observer.docker_discovery` | web, get |
| `observer.ebpf_enabled` | web, get |
| `observer.federation.insecure` | web, get |
| `observer.federation.parent_url` | web, get |
| `observer.federation.peer_name` | web, get |
| `observer.federation.push_interval_seconds` | web, get |
| `observer.federation.token_path` | web, get |
| `observer.gpu_attribution` | web, get |
| `observer.include_kthreads` | web, get |
| `observer.ollama_tap.endpoint` | web, get |
| `observer.peers.allow_register` | web, get |
| `observer.peers.listen_addr` | web, get |
| `observer.peers.push_interval_seconds` | web, get |
| `observer.peers.token_ttl_rotation_grace_s` | web, get |
| `observer.plugin_enabled` | web, get |
| `observer.process_tree_enabled` | web, get |
| `observer.session_attribution` | web, get |
| `observer.tick_interval_ms` | web, get |
| `observer.top_n_broadcast` | web, get |

### ollama

| Key | Missing surfaces |
|---|---|
| `ollama.console_cols` | web |
| `ollama.console_rows` | web |
| `ollama.detection.alert_repeat` | web, get, put, configure, cli |
| `ollama.detection.alert_settle` | web, get, put, configure, cli |
| `ollama.detection.completion_patterns` | web, get, put, configure, cli |
| `ollama.detection.input_needed_patterns` | web, get, put, configure, cli |
| `ollama.detection.notify_cooldown` | web, get, put, configure, cli |
| `ollama.detection.prompt_debounce` | web, get, put, configure, cli |
| `ollama.detection.prompt_patterns` | web, get, put, configure, cli |
| `ollama.detection.rate_limit_patterns` | web, get, put, configure, cli |
| `ollama.enabled` | web |
| `ollama.host` | web |
| `ollama.input_mode` | web |
| `ollama.model` | web |
| `ollama.output_mode` | web |

### onboarding_report

| Key | Missing surfaces |
|---|---|
| `onboarding_report.disabled_checks` | get, put, configure, cli |
| `onboarding_report.enabled` | get, put, configure, cli |
| `onboarding_report.include_next_steps` | get, put, configure, cli |

### opencode

| Key | Missing surfaces |
|---|---|
| `opencode.binary` | web |
| `opencode.build_temperature` | web, get, put, configure, cli |
| `opencode.console_cols` | web |
| `opencode.console_rows` | web |
| `opencode.enabled` | web |
| `opencode.input_mode` | web |
| `opencode.ollama_chunk_timeout_sec` | web |
| `opencode.ollama_header_timeout_sec` | web |
| `opencode.output_mode` | web |
| `opencode.providers` | web, get, put, configure, cli |

### opencode_acp

| Key | Missing surfaces |
|---|---|
| `opencode_acp.acp_health_interval` | web |
| `opencode_acp.acp_message_timeout` | web |
| `opencode_acp.acp_startup_timeout` | web |
| `opencode_acp.binary` | web |
| `opencode_acp.console_cols` | web |
| `opencode_acp.console_rows` | web |
| `opencode_acp.enabled` | web |
| `opencode_acp.input_mode` | web |
| `opencode_acp.output_mode` | web |

### opencode_prompt

| Key | Missing surfaces |
|---|---|
| `opencode_prompt.binary` | web |
| `opencode_prompt.console_cols` | web |
| `opencode_prompt.console_rows` | web |
| `opencode_prompt.enabled` | web |
| `opencode_prompt.input_mode` | web |
| `opencode_prompt.output_mode` | web |

### openwebui

| Key | Missing surfaces |
|---|---|
| `openwebui.api_key` | web |
| `openwebui.console_cols` | web |
| `openwebui.console_rows` | web |
| `openwebui.enabled` | web |
| `openwebui.input_mode` | web |
| `openwebui.model` | web |
| `openwebui.output_mode` | web |
| `openwebui.url` | web |

### orchestrator

| Key | Missing surfaces |
|---|---|
| `orchestrator.default_guardrails` | web, put, configure, cli |
| `orchestrator.guardrail_model` | get |

### pi

| Key | Missing surfaces |
|---|---|
| `pi.binary` | web |
| `pi.console_cols` | web |
| `pi.console_rows` | web |
| `pi.enabled` | web |
| `pi.input_mode` | web |
| `pi.model` | web |
| `pi.output_mode` | web |

### pi_prompt

| Key | Missing surfaces |
|---|---|
| `pi_prompt.binary` | web |
| `pi_prompt.console_cols` | web |
| `pi_prompt.console_rows` | web |
| `pi_prompt.enabled` | web |
| `pi_prompt.input_mode` | web |
| `pi_prompt.model` | web |
| `pi_prompt.output_mode` | web |

### pipeline

| Key | Missing surfaces |
|---|---|
| `pipeline.quality_gates.block_on_regression` | web, get, put, configure, cli |
| `pipeline.quality_gates.enabled` | web, get, put, configure, cli |
| `pipeline.quality_gates.scope` | web, get, put, configure, cli |
| `pipeline.quality_gates.smoke_packages` | web, get, put, configure, cli |
| `pipeline.quality_gates.test_command` | web, get, put, configure, cli |
| `pipeline.quality_gates.timeout` | web, get, put, configure, cli |

### plugins

| Key | Missing surfaces |
|---|---|
| `plugins.disabled` | web, put, configure, cli |
| `plugins.managed` | web, get, put, configure, cli |

### profiles

| Key | Missing surfaces |
|---|---|
| `profiles` | web, put, configure, cli |

### projects

| Key | Missing surfaces |
|---|---|
| `projects` | web, get, put, configure, cli |

### proxy

| Key | Missing surfaces |
|---|---|
| `proxy.circuit_breaker_reset` | web |
| `proxy.circuit_breaker_threshold` | web |
| `proxy.enabled` | web |
| `proxy.health_interval` | web |
| `proxy.offline_queue_size` | web |
| `proxy.request_timeout` | web |

### push

| Key | Missing surfaces |
|---|---|
| `push.allow_insecure_endpoints` | web |
| `push.apns.bundle_id` | web |
| `push.apns.enabled` | web |
| `push.apns.key_id` | web |
| `push.apns.key_path` | web |
| `push.apns.key_secret` | web |
| `push.apns.sandbox` | web |
| `push.apns.team_id` | web |
| `push.block_private_endpoints` | web |

### secrets

| Key | Missing surfaces |
|---|---|
| `secrets.backend` | web, get, put, configure, cli |
| `secrets.keepass_binary` | web, get, put, configure, cli |
| `secrets.keepass_db` | web, get, put, configure, cli |
| `secrets.keepass_group` | web, get, put, configure, cli |
| `secrets.keepass_password` | web, get, put, configure, cli |
| `secrets.op_binary` | web, get, put, configure, cli |
| `secrets.op_token` | web, get, put, configure, cli |
| `secrets.op_vault` | web, get, put, configure, cli |
| `secrets.vault.address` | web, get, put, configure, cli |
| `secrets.vault.auth_method` | web, get, put, configure, cli |
| `secrets.vault.kv_mount` | web, get, put, configure, cli |
| `secrets.vault.namespace` | web, get, put, configure, cli |
| `secrets.vault.path_layout` | web, get, put, configure, cli |
| `secrets.vault.path_prefix` | web, get, put, configure, cli |
| `secrets.vault.request_timeout` | web, get, put, configure, cli |
| `secrets.vault.tls_ca_file` | web, get, put, configure, cli |
| `secrets.vault.tls_skip_verify` | web, get, put, configure, cli |
| `secrets.vault.token` | web, get, put, configure, cli |
| `secrets.vault.write_policy` | web, get, put, configure, cli |

### server

| Key | Missing surfaces |
|---|---|
| `server.public_url` | web |
| `server.tls_auto_generate` | web |
| `server.tls_cert` | web |
| `server.tls_enabled` | web, get, put, configure, cli |
| `server.tls_key` | web |
| `server.token` | web |

### servers

| Key | Missing surfaces |
|---|---|
| `servers` | web, get, put, configure, cli |

### session

| Key | Missing surfaces |
|---|---|
| `session.alerts_rich_format` | web, get, put, configure, cli |
| `session.assistant_backend` | web, get, put, configure, cli |
| `session.assistant_model` | web, get, put, configure, cli |
| `session.assistant_system_prompt` | web, get, put, configure, cli |
| `session.auto_install_hooks` | web, get, put, configure, cli |
| `session.capabilities` | web, get, put, configure, cli |
| `session.capacity_wait_seconds` | get |
| `session.claude_enabled` | web |
| `session.cleanup_artifacts_on_end` | web, get, put, configure, cli |
| `session.cost_rates` | web, get, put, configure, cli |
| `session.device_aliases` | web, get, put, configure, cli |
| `session.exit_hooks` | web, get, put, configure, cli |
| `session.extra_mcp_servers` | web, get, put, configure, cli |
| `session.file_service_root` | web, get, put, configure, cli |
| `session.git_default` | web |
| `session.gitignore_artifacts` | web, get, put, configure, cli |
| `session.gitignore_check_on_start` | web, get, put, configure, cli |
| `session.hook_script_dir` | web, get, put, configure, cli |
| `session.llm_backend` | web |
| `session.one_shot_sessions` | web |
| `session.persist_telemetry_on_stop` | web, get, put, configure, cli |
| `session.quick_commands` | web, get |
| `session.rate_limit_global_pause` | web, get, put, configure, cli |
| `session.reconcile_on_startup` | web, get, put, configure, cli |
| `session.routing_rules` | web, get, put, configure, cli |
| `session.secure_tracking` | web, get, put, configure, cli |
| `session.session_retention_days` | web, get, put, configure, cli |
| `session.splash_logo_path` | web, get, put, configure, cli |
| `session.splash_tagline` | web, get, put, configure, cli |
| `session.stale_timeout_seconds` | web, get, put, configure, cli |
| `session.summarizer.model` | get, put, configure, cli |
| `session.summarizer.prompt` | web |
| `session.workspace_root` | web |

### shell_backend

| Key | Missing surfaces |
|---|---|
| `shell_backend.console_cols` | web |
| `shell_backend.console_rows` | web |
| `shell_backend.enabled` | web |
| `shell_backend.input_mode` | web |
| `shell_backend.output_mode` | web |
| `shell_backend.script_path` | web |

### signal

| Key | Missing surfaces |
|---|---|
| `signal.account_number` | web, put, configure, cli |
| `signal.config_dir` | web |
| `signal.device_name` | web |
| `signal.group_id` | web |

### skills

| Key | Missing surfaces |
|---|---|
| `skills.add_default_on_start` | web, get, put, configure, cli |
| `skills.auto_ignore_on_session_start` | web, get, put, configure, cli |
| `skills.registries` | web, get, put, configure, cli |

### slack

| Key | Missing surfaces |
|---|---|
| `slack.auto_manage_channel` | web |
| `slack.channel_id` | web |
| `slack.enabled` | web |
| `slack.token` | web |

### smoke

| Key | Missing surfaces |
|---|---|
| `smoke.forward_token` | web, get, put, configure, cli |
| `smoke.forward_url` | web, get, put, configure, cli |

### stats

| Key | Missing surfaces |
|---|---|
| `stats.ebpf_enabled` | web, get, put, configure, cli |

### tailscale

| Key | Missing surfaces |
|---|---|
| `tailscale.acl.allowed_peers` | web, get, put, configure, cli |
| `tailscale.acl.managed_tags` | web, get, put, configure, cli |
| `tailscale.api_key` | web, get, put, configure, cli |
| `tailscale.auth_key` | web, get, put, configure, cli |
| `tailscale.coordinator_url` | web, get, put, configure, cli |
| `tailscale.enabled` | web, get, put, configure, cli |
| `tailscale.image` | web, get, put, configure, cli |
| `tailscale.tags` | web, get, put, configure, cli |

### telegram

| Key | Missing surfaces |
|---|---|
| `telegram.auto_manage_group` | web |
| `telegram.chat_id` | web |
| `telegram.enabled` | web |
| `telegram.token` | web |

### templates

| Key | Missing surfaces |
|---|---|
| `templates` | web, get, put, configure, cli |

### tracing

| Key | Missing surfaces |
|---|---|
| `tracing.arg_cap_bytes` | get |
| `tracing.backends` | get |
| `tracing.mode` | get |
| `tracing.retention_days` | get |

### twilio

| Key | Missing surfaces |
|---|---|
| `twilio.account_sid` | web |
| `twilio.auth_token` | web |
| `twilio.enabled` | web |
| `twilio.from_number` | web |
| `twilio.to_number` | web |
| `twilio.webhook_addr` | web |
| `twilio.webhook_public_url` | web, get, put, configure, cli |

### ui

| Key | Missing surfaces |
|---|---|
| `ui.splash_min_seconds` | web |
| `ui.splash_min_seconds_new_version` | web |

### usage

| Key | Missing surfaces |
|---|---|
| `usage.billing` | web, put, configure, cli |
| `usage.burn_guard.ask_on_change` | get |
| `usage.burn_guard.cache_read_points_per_million` | get |
| `usage.burn_guard.cache_write_points_per_million` | get |
| `usage.burn_guard.effort_ceiling` | web, get, put, configure, cli |
| `usage.burn_guard.enabled` | get |
| `usage.burn_guard.interval_minutes` | get |
| `usage.burn_guard.ladder` | web, get |
| `usage.burn_guard.margin_points` | get |
| `usage.burn_guard.refit_on_reading` | get |
| `usage.burn_guard.retry_limit_claude` | get |
| `usage.burn_guard.step_back` | get |
| `usage.calculator.baseline_per_24h` | get |
| `usage.calculator.cost_per_task_points` | get |
| `usage.calculator.keeper` | get |
| `usage.calculator.max_lanes` | get |
| `usage.calculator.policy` | get |
| `usage.calculator.reset_time` | get |
| `usage.calculator.reset_weekday` | get |
| `usage.calculator.reset_zone` | get |
| `usage.calculator.safety_points` | get |
| `usage.calculator.variant` | get |

### vision

| Key | Missing surfaces |
|---|---|
| `vision.max_image_bytes` | put, configure, cli |

### web_search

| Key | Missing surfaces |
|---|---|
| `web_search.cache_enabled` | get, put, configure, cli |
| `web_search.cache_ttl_seconds` | get, put, configure, cli |
| `web_search.engine` | web |
| `web_search.num_results` | web |
| `web_search.provider` | web, put, configure, cli |
| `web_search.providers` | web, get, put, configure, cli |
| `web_search.url` | web |

### webhook

| Key | Missing surfaces |
|---|---|
| `webhook.addr` | web |
| `webhook.enabled` | web |
| `webhook.image_dir` | web |
| `webhook.token` | web |

### whisper

| Key | Missing surfaces |
|---|---|
| `whisper.api_key` | web, get |
| `whisper.endpoint` | web, get |

## Method

Produced by `python3 -I scripts/config-parity-audit.py --no-exemptions --json` plus the table form. The audit reads source text only (no Go toolchain, no running daemon) through `scripts/config_parity_surfaces.py`:

- **Key list**: every config key of the daemon's configuration struct, as dotted paths. The yaml surface is the struct itself, so it is always present and not audited.
- **web**: the `*_CONFIG_FIELDS` arrays in the web app script; `key:` values inside each section's `fields` array. Keys starting with an underscore are UI-local and skipped.
- **get**: the object emitted by the GET /api/config handler, with nested objects flattened to dotted paths and `*Surface` helper functions expanded from their returned map literals. An unknown helper leaves the key as a leaf.
- **put**: every `case` label of the key switch in the config patch function, plus dynamic prefixes reported as `prefix.*` (tracing backends, supervisor loop, minimum-Ollama requirements).
- **configure** and **cli**: both forward generically to PUT /api/config, so the audit reuses the PUT key set. Their gaps therefore mirror PUT and cannot diverge.

Known blind spots:
- Dynamic key sets are matched by prefix only; a prefix entry counts as covering every key beneath it even if some sub-keys are rejected at runtime.
- Text parsing can miss keys built by code (computed names, helpers it does not know) and report false gaps, or accept a key that the handler later rejects.
- A key present on a surface is counted as aligned without checking its type, validation, or that the value actually persists.
- Web coverage means a field exists in the Settings UI arrays; it does not check that the field renders or is reachable.
- Keys the daemon accepts but that are absent from the struct are not seen.
- Some gaps may be intentional (secrets, internal-only knobs); with the exemptions file empty, none are treated as such. Triage should decide per key.
