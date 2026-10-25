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
)
