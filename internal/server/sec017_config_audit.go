// SEC-017 (docs/plans/historical-plans/2026-08-28-security-assessment-core.md)
// — PUT /api/config could silently change any non-skipped config key (DNS
// channel settings, proxy rules, webhook secrets, detection thresholds,
// autonomous guardrails, ...) with no audit trail at all. Every other
// comparably sensitive write path in this codebase (secrets.go, skills.go,
// identity.go, compute.go, council.go, inference.go) already logs through
// s.auditLog; this closes the one broad, generic path that didn't.

package server

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
)

// sensitiveConfigKeySubstrings flags a dot-path config key as credential-
// shaped, so auditConfigPatch masks its value instead of logging it plainly.
// Substring match (case-insensitive) rather than an exhaustive key list —
// deliberately over-inclusive so a new "foo.api_key" field added later is
// masked by default instead of silently logged in the clear.
var sensitiveConfigKeySubstrings = []string{
	"token", "password", "secret", "api_key", "apikey", "auth_token",
	"access_token", "private_key", "credential",
}

func isSensitiveConfigKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sub := range sensitiveConfigKeySubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// maskConfigValue renders v for the audit log: masked if key looks
// credential-shaped (all but first/last 2 chars starred, or fully starred
// at 5 chars or fewer — matches cmd/datawatch's existing maskValue
// convention for secrets-migration output), otherwise logged as-is.
func maskConfigValue(key string, v interface{}) interface{} {
	if !isSensitiveConfigKey(key) {
		return v
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "***"
	}
	if len(s) <= 5 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

// auditConfigPatch records one audit entry per PUT /api/config call: every
// key that was actually applied (skipped keys, per applyConfigPatch, are
// reported back to the caller separately and never touch config, so they're
// excluded here too) with a masked value for anything credential-shaped.
func (s *Server) auditConfigPatch(patch map[string]interface{}, skipped []string) {
	if s.auditLog == nil || len(patch) == 0 {
		return
	}
	skip := make(map[string]bool, len(skipped))
	for _, k := range skipped {
		skip[k] = true
	}
	changes := make(map[string]interface{}, len(patch))
	keys := make([]string, 0, len(patch))
	for k, v := range patch {
		if skip[k] {
			continue
		}
		changes[k] = maskConfigValue(k, v)
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: "configure",
		Details: map[string]any{
			"keys":    keys,
			"changes": changes,
		},
	})
}
