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
)
