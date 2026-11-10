package audit

// Audit action names for the per-backend usage back-off (BL428 Phase 4).
// Emitters pass these as Entry.Action; cefSignature maps each to a stable
// (signatureID, name, severity) triple.
const (
	// ActionUsageBackoffSet — a back-off flag was set, by rate-limit
	// detection (source=rate_limit) or by an operator (source=operator).
	// Details: backend, until (RFC 3339), source.
	ActionUsageBackoffSet = "autonomous_usage_backoff_set"
	// ActionUsageBackoffCleared — an operator cleared one back-off
	// (backend) or all of them (backend=all). Details: backend.
	ActionUsageBackoffCleared = "autonomous_usage_backoff_cleared"
	// ActionUsageBackoffAutoCleared — a back-off flag expired and the
	// sweeper cleared it. Details: backend, until (RFC 3339).
	ActionUsageBackoffAutoCleared = "autonomous_usage_backoff_auto_cleared"
	// ActionUsageBackoffPatternsRechecked — an operator recorded the current
	// rate-limit pattern fingerprint as rechecked. Details: patterns_version.
	ActionUsageBackoffPatternsRechecked = "autonomous_usage_backoff_patterns_rechecked"

	// ActionResourceCeilingKill — a run was stopped for using more memory than
	// its ceiling, or started with no ceiling because the host has neither a
	// user scope nor /proc. Details: task, process, peak_mb, ceiling_mb, reason.
	ActionResourceCeilingKill = "resource_ceiling_kill"

	// ActionBudgetCeilingWait — an LLM's dollar budget window filled, so
	// autonomous admission for it now waits. Emitted once per transition.
	// Details: llm, spend_usd, ceiling_usd, rolls_over_at (RFC 3339, when known).
	ActionBudgetCeilingWait = "autonomous_budget_ceiling_wait"
	// ActionBudgetWindowResumed — an LLM's budget window is no longer full
	// (it rolled over or spend aged out), so autonomous admission resumes.
	// Emitted once per transition. Details: llm, spend_usd, ceiling_usd.
	ActionBudgetWindowResumed = "autonomous_budget_window_resumed"

	// ActionAutomationBudgetBreach — an Automation's budget policy refused a
	// member start, so the Automation was paused, blocked or (for pacing and
	// estimate holds, which are not audited) held. Emitted once per breach
	// episode and again when a paused episode turns into a block.
	// Details: automation_id, scope, spent_usd, ceiling_usd, window, action,
	// reset_at (RFC 3339, when known), reset_known, actor, reason.
	ActionAutomationBudgetBreach = "automation_budget_breach"
	// ActionAutomationBudgetResumed — a budget pause or block was lifted, by the
	// daemon at the window rollover or by an operator. Details: automation_id,
	// scope, spent_usd, ceiling_usd, window, action (what was lifted),
	// reset_at, reset_known, actor, reason.
	ActionAutomationBudgetResumed = "automation_budget_resumed"
	// ActionCapacityWait — an autonomous task started waiting for admission,
	// or its wait cause changed. Emitted once per cause change, never per
	// poll. Subject: task id. Details: prd_id, cause, reason, pools.
	ActionCapacityWait = "autonomous_capacity_wait"
	// ActionCapacityAdmitted — a waiting task was admitted. Subject: task id.
	// Details: waited_seconds, pools.
	ActionCapacityAdmitted = "autonomous_capacity_admitted"
	// ActionBudgetWindowRollover — an LLM's budget window boundary passed and
	// its spend reset. Subject: the LLM. Details: previous_spend_usd,
	// ceiling_usd, new_window_start (RFC 3339).
	ActionBudgetWindowRollover = "autonomous_budget_window_rollover"

	// ActionFingerprintMatchConfirmed — an operator CONFIRMED a fingerprint
	// reconciliation match (a peer reported the same keyed fingerprint as one of
	// this daemon's resources), so admission now treats the two resources as one
	// shared pool. Details: kind (compute_node|billing_group), local (this
	// daemon's resource name), peer (the reporting peer's name).
	ActionFingerprintMatchConfirmed = "fingerprint_match_confirmed"
	// ActionFingerprintMatchRejected — an operator REJECTED a fingerprint
	// reconciliation match (or un-confirmed a prior one), so admission no longer
	// pools it with the peer. Details: kind, local, peer.
	ActionFingerprintMatchRejected = "fingerprint_match_rejected"
	// ActionReleaseModeChanged — a release approval mode was changed for one
	// release size (patch, minor or major). Emitted once per successful change;
	// a no-op emits nothing. Subject: the operator-facing headline. Details:
	// headline, size, old_mode, new_mode, actor, surface, decision_record_id
	// ("" when the decision record could not be stored).
	ActionReleaseModeChanged = "release_mode_changed"
	// ActionAutomationImportGraph — an orchestrator graph was converted into a
	// new Automation. A dry run is not audited. Subject: the Automation.
	// Details: graph_id, automation_id, member_count.
	ActionAutomationImportGraph = "automation_import_graph"

	// ActionNotifyEventsChanged — the per-Automaton or per-profile
	// notify_events list changed (the lifecycle event kinds that produce
	// notifications for that Automaton or profile). Details: scope
	// ("automaton" | "profile"), prd_id or profile_id, old and new (the
	// comma-separated kind lists before and after the change, "" =
	// inherit from the next tier) — plus, on the Automaton entry only,
	// source_after, the tier after the change (automaton, profile, or
	// global).
	ActionNotifyEventsChanged = "autonomous_notify_events_changed"

	// ActionLifecycleWebhookDelivery — one delivery attempt of a lifecycle
	// event to an outbound webhook receiver (outcome delivered, retry,
	// failed, refused or skipped). A successful delivery ends on its
	// "delivered" record. Details: subscription_id, delivery_id, attempt
	// (0 when no request was made), max_attempts, host (host:port only,
	// never userinfo or query string), status_code (0 when none), outcome,
	// error, duration_ms. Never the secret, the signature or the body.
	ActionLifecycleWebhookDelivery = "autonomous_lifecycle_webhook_delivery"
	// ActionLifecycleWebhookFailed — the final outcome of a delivery that
	// was dropped (retries exhausted, a permanent response, a refused
	// target or an unresolvable secret). Emitted once per dropped delivery,
	// after its attempt records, under its own action so it carries a
	// higher severity. Details: as ActionLifecycleWebhookDelivery, with
	// attempt the number of attempts made and duration_ms the total.
	ActionLifecycleWebhookFailed = "autonomous_lifecycle_webhook_failed"

	// ActionLifecycleForwardDelivery — one attempt to forward a lifecycle event
	// to a federation peer. The last record of a dropped forward carries the
	// final outcome (failed or refused); a refusal before any request has
	// attempt 0. Details: subscription_id, delivery_id, peer (the registered
	// peer name), event_kind, host (host:port only), attempt, max_attempts,
	// status_code (0 when none), outcome (delivered, retry, failed, refused or
	// skipped), error, hops, duration_ms. Never the bearer token, the signature
	// or the body.
	ActionLifecycleForwardDelivery = "autonomous_lifecycle_forward_delivery"

	// ActionLifecycleForwardReceive — one forwarded lifecycle event that reached
	// this daemon's receiving endpoint, accepted or refused. Written by the HTTP
	// layer for every request it answers (not for a request that never reaches
	// the handler). Details: outcome (accepted or refused), status_code, reason
	// (fixed text: never the body, the token or the signature), event_id,
	// event_kind, origin_host, sender_host, hops, peer (the registered peer name,
	// empty for an admin caller) and the actor. The signature header is never
	// recorded.
	ActionLifecycleForwardReceive = "autonomous_lifecycle_forward_receive"

	// Operator question channel — the lifecycle of one question, subjected
	// by the question id on every entry. A question and its answer are kept
	// forever in the audit log: the quote is stored verbatim in the
	// answered and superseded entries, exactly as the answering operator
	// wrote it.

	// ActionQuestionRaised — a question was raised on the operator question
	// channel, awaiting an operator answer. Subject: the question id.
	// Details: question_id, label, asked_by, asker_kind, context, risk,
	// option_count, default, text.
	ActionQuestionRaised = "question_raised"
	// ActionQuestionAnswered — an operator answered the question. Subject:
	// the question id. Details: question_id, answer_id, chosen_option_id,
	// quote (verbatim, kept forever), quote_sha256, actor, surface.
	ActionQuestionAnswered = "question_answered"
	// ActionQuestionWithdrawn — the asker (or an operator) withdrew the
	// question before it was answered. Subject: the question id.
	// Details: question_id, actor, reason.
	ActionQuestionWithdrawn = "question_withdrawn"
	// ActionQuestionExpired — the question reached its deadline without an
	// answer, and its default took effect (or the awaiting automation was
	// held). Subject: the question id. Details: question_id, reason.
	ActionQuestionExpired = "question_expired"
	// ActionAnswerSuperseded — a later answer replaced an earlier one for
	// the same question. Subject: the question id. Details: question_id,
	// answer_id, supersedes (the earlier answer_id), chosen_option_id,
	// quote (verbatim, kept forever), quote_sha256, actor, surface.
	ActionAnswerSuperseded = "answer_superseded"
	// ActionQuestionAnswerRefused — the channel refused an operator answer:
	// the text failed a guard rule, so it never reaches the asker. Audited
	// without its text — Details carries the name of the matched rule only,
	// NEVER the rule text or the answer itself, so a refusal of a secret
	// answer leaves no secret in the audit log. Subject: the question id.
	// Details: question_id, actor, surface, reason, pattern (the name of
	// the matched rule only, NEVER the text).
	ActionQuestionAnswerRefused = "question_answer_refused"
	// ActionQuestionMarkerRefused — the output scanner saw a worker question
	// marker line (DATAWATCH_QUESTION:) that failed validation, so no question
	// was raised. Audited without the line: Details carries the session id, the
	// refusal reason category and the SHA-256 of the line, NEVER the line text,
	// so a malformed or hostile line leaves nothing but its hash in the log.
	// Subject: the session id. Details: session_id, reason (a category such as
	// malformed or too_long), line_sha256.
	ActionQuestionMarkerRefused = "question_marker_refused"
	// ActionQuestionUpdated — an open question was changed in place (its
	// text, options, links or other editable fields). Subject: the question
	// id. Details: question_id, actor, changed (the names of the changed
	// fields), option_count.
	ActionQuestionUpdated = "question_updated"
	// Usage-calculator record mutations (Design 8 / Decision 9). Each is
	// emitted once, on the daemon that stores the write — the keeper for the
	// shared series, the daemon itself for its local remaining-work figure.
	//
	// ActionUsageReadingAdded — a typed usage reading was saved to the shared
	// record. Subject: the reading's id. Details: source, at (RFC 3339), and
	// whichever figures the reading carried (percent, spend_usd,
	// session_percent, lanes).
	ActionUsageReadingAdded = "usage_reading_added"
	// ActionUsageReadingDeleted — a usage reading was deleted from the shared
	// record (the operator correcting a typed mistake). Subject: the deleted
	// reading's id.
	ActionUsageReadingDeleted = "usage_reading_deleted"
	// ActionUsageExtraCreditSet — the bought extra credit was set (points, 0
	// or more). Details: points.
	ActionUsageExtraCreditSet = "usage_extra_credit_set"
	// ActionUsageRemainingWorkSet — the typed remaining-work figure
	// (points) of this daemon's calculator was set, or cleared
	// (details: cleared true, points omitted).
	ActionUsageRemainingWorkSet = "usage_remaining_work_set"
	// ActionStuckRunningBlocked — the stuck-running watchdog moved an Automaton
	// that sat in running (every task terminal, no live session or executor)
	// to blocked after re-firing the completion rollup once. Details: prd_id,
	// caller, stuck_seconds, bound_seconds, old_status, new_status.
	ActionStuckRunningBlocked = "autonomous_stuck_running_blocked"
	// ActionLaneFillMode — the lane fill mode in effect for one lane kind
	// changed between two polls (the operator set gpu_mode, claude_mode or
	// mode). Details: kind (gpu or claude), old_mode, new_mode. The first poll
	// after a daemon start writes nothing.
	ActionLaneFillMode = "lane_fill_mode"

	// Interstitial UI-prompt dismissal — the outcome of the daemon pressing
	// the safe dismiss key on a recognised interstitial prompt (the session
	// package owns the safe-send rules; these are the audited outcomes).
	// Every entry is subjected by the session id.

	// ActionUIPromptDismissed — a safe dismiss key was sent and the prompt
	// then cleared: the dismissal is recorded as successful. Details:
	// backend, kind, key, attempt, selected_option.
	ActionUIPromptDismissed = "ui_prompt_dismissed"
	// ActionUIPromptNotCleared — the attempts cap was used up with the
	// prompt still on screen: the daemon has given up on keys for this
	// occurrence (recorded once per occurrence). Details: backend, kind,
	// key, attempt, selected_option.
	ActionUIPromptNotCleared = "ui_prompt_not_cleared"
	// ActionUIPromptRefused — the send-site guard refused a classified
	// dismiss key (or none was classified as safe), so NOTHING was sent. A
	// refusal is a safety event: the guard worked. Details: backend, kind,
	// key (the refused key name, "" when none was offered), attempt,
	// selected_option.
	ActionUIPromptRefused = "ui_prompt_refused"

	// UI-prompt escalation — the daemon telling the operator that a
	// recognised interstitial prompt could not be cleared and the session
	// is stalled (session package owns the trigger rules; these are the
	// audited daemon-side outcomes; see internal/autonomous/ui_prompt_escalation.go).

	// ActionUIPromptEscalationRaised — the escalation question was new and
	// raised. Details: session_id, kind, trigger, selected_option, token.
	ActionUIPromptEscalationRaised = "ui_prompt_escalation_raised"
	// ActionUIPromptEscalationRefused — the store refused the escalation
	// question; the fallback alert was raised instead.
	ActionUIPromptEscalationRefused = "ui_prompt_escalation_refused"
	// ActionUIPromptEscalationWithdrawn — a still-open escalation question
	// was withdrawn because its prompt cleared or its session ended.
	ActionUIPromptEscalationWithdrawn = "ui_prompt_escalation_withdrawn"

	// ActionLLMSecretAccess — one call-time LLM API key secret resolution
	// was made. Emitted for EVERY attempt, success or failure: outcome
	// "ok" (the secret resolved and was handed to the caller), "missing"
	// (no secret is set for that LLM entry) or "refused" (resolution was
	// declined or could not be authorised). Subject: the LLM entry name.
	// Details (names only, never values): llm_name (the LLM entry name),
	// secret_name (the secret's name), outcome (ok, missing or refused).
	// The entry never carries the secret value or the literal key — the
	// constructor accepts names and the outcome only, so a secret value has
	// no path into the log.
	ActionLLMSecretAccess = "llm_secret_access"

	// Pre-push secret scan (git.secret_scan) — the scan runs over the
	// commits about to be pushed and its findings are redacted to
	// (detector, path, line, short fingerprint); the matched secret has
	// no path into the log.

	// ActionSecretScanRefused — the pre-push secret scan (enforce mode)
	// found an un-allow-listed finding, so the push was refused and the
	// remote untouched. Subject: the session or Automaton that attempted
	// the push. Details (redacted only): remote, ref, mode (enforce),
	// finding_count, findings (per finding: detector, path, line,
	// fingerprint — never the secret).
	ActionSecretScanRefused = "secret_scan_refused"
	// ActionSecretScanShadow — the pre-push secret scan (shadow mode)
	// found a finding and reported it WITHOUT refusing the push. Subject
	// and Details: the same redacted shape as ActionSecretScanRefused,
	// with mode shadow. Never the secret.
	ActionSecretScanShadow = "secret_scan_shadow"
	// ActionModelPullAllowed — the model-pull consent gate allowed a model
	// pull (compute-node model pull, or marketplace task pull). Details:
	// model, publisher, via (the allow kind: "model" or "publisher"),
	// origin (the pull path that asked), and the allow source's note when
	// one was written down.
	ActionModelPullAllowed = "model_pull_allowed"
	// ActionModelPullDenied — the gate refused a model pull: the model was
	// listed with allowed:false, or it and its publisher were unlisted. The
	// pull never happens; Details carry model, publisher, origin, and the
	// reason spoken plainly.
	ActionModelPullDenied = "model_pull_denied"
	// ActionModelPullConsentChanged — the operator wrote the
	// autonomous.model_pull_consent block (its models or publisher list).
	// Details: the now-current models and publishers lists, and origin.
	ActionModelPullConsentChanged = "model_pull_consent_changed"

	// ActionJudgeOutcome — a rules-review judge result was recorded for an
	// Automaton. Details: automaton_id, kind (judge_could_not_start|judge_timed_out|
	// judge_found_problem), headline (the plain-language wording of the kind).
	ActionJudgeOutcome = "autonomous_judge_outcome"

	// Plugin service health — the plugins package's service event recorder
	// emits these once per transition (never per poll). Every entry is
	// subjected by the plugin name and carries state ("raised" or
	// "cleared") in Details; recovery entries reuse the same action.

	// ActionPluginServiceDown — an attach-only or managed health probe
	// failed past the configured threshold and the condition raised. The
	// recovery entry (state "cleared") is written when the probe passes.
	// Details: state, plugin, consecutive_failures, threshold.
	ActionPluginServiceDown = "plugin_service_down"
	// ActionPluginServiceTokenRejected — a plugin service answered 401 or
	// 403 to a probe or a widget pull. Details: state, plugin, status_code,
	// source (probe or widget_pull).
	ActionPluginServiceTokenRejected = "plugin_service_token_rejected"
	// ActionPluginServiceEnrichmentBacklog — a plugin's pending count rose
	// across N consecutive pulls. Details: state, plugin, streak, pulls,
	// min_rise, last_pending.
	ActionPluginServiceEnrichmentBacklog = "plugin_service_enrichment_backlog"
	// ActionPluginServiceModelNodeUnreachable — a model's Ollama node did
	// not answer. Subject: the plugin, with model in Details. Details:
	// state, plugin, model.
	ActionPluginServiceModelNodeUnreachable = "plugin_service_model_node_unreachable"

	// Managed-service lifecycle — the plugins package's ServiceSupervisor
	// emits these once per transition and the server records them
	// (AuditPluginService). Subject: the plugin. Details: pid/reason/etc.
	// per the transition itself.

	// ActionPluginServiceStart — a supervised service child started. Details:
	// pid, restarts.
	ActionPluginServiceStart = "plugin_service_start"
	// ActionPluginServiceStop — a supervised service stopped (operator
	// request or supervisor shutdown). Details: reason, exit_code.
	ActionPluginServiceStop = "plugin_service_stop"
	// ActionPluginServiceRestart — a crashed unhealthy service was
	// restarted per the manifest restart policy. Details: reason, attempt.
	ActionPluginServiceRestart = "plugin_service_restart"
	// ActionPluginServiceHealthFail — a single health probe failed (below
	// the threshold that raises plugin_service_down). Details: reason,
	// consecutive_failures, threshold.
	ActionPluginServiceHealthFail = "plugin_service_health_fail"

	// Plugin widget + service-token surfaces — the server records these when a
	// widget registers or receives a push, a widget push token is issued,
	// or a plugin service token rotates or is removed. Subject: the plugin
	// (or, for the token actions, the secret being changed with the plugin
	// in Details).

	// ActionPluginWidgetRegistered — a plugin registered a widget for its
	// status surface. Details: widget_id, mode.
	ActionPluginWidgetRegistered = "plugin_widget_registered"
	// ActionPluginWidgetPush — a plugin pushed a value to one of its
	// push-mode widgets. Details: widget_id, value.
	ActionPluginWidgetPush = "plugin_widget_push"
	// ActionPluginWidgetTokenIssue — the plugin's push token was minted or
	// rotated. Details: plugin, rotated.
	ActionPluginWidgetTokenIssue = "plugin_widget_token_issue"
	// ActionPluginTokenRotate — a plugin service token was rotated
	// through the secrets vault. Details: plugin, note.
	ActionPluginTokenRotate = "plugin_token_rotate"
	// ActionPluginTokenRemove — a plugin service token was removed
	// from the secrets vault. Details: plugin, reason.
	ActionPluginTokenRemove = "plugin_token_remove"
	// ActionPluginWidgetPullFailed — a widget pull answered with an error
	// (transport failure or a non-2xx body, value never usable). Details:
	// widget_id, error.
	ActionPluginWidgetPullFailed = "plugin_widget_pull_failed"

	// Release git operations — the daemon records one entry per push or tag
	// it performs for a release, or refuses. Details: project, ref, remote,
	// credential_source (a label only: host-login, credential-helper,
	// ssh-agent, an environment-variable name or a vault-secret name, never
	// the token value), outcome.

	// ActionReleasePush — a release push to a git remote (performed or refused).
	ActionReleasePush = "release_push"
	// ActionReleaseTag — a release tag created or pushed (performed or refused).
	ActionReleaseTag = "release_tag"
)
