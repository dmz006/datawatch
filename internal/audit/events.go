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
)
