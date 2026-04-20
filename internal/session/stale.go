// BL40 — stale task recovery.
//
// Pure-logic helpers for "is this running session stuck?" detection.
// The REST handler in internal/server/stale.go consumes these.
//
// Definition (v1):
//   A session is "stale" when:
//     - State == StateRunning
//     - Now - UpdatedAt > stale_timeout_seconds
//
// stale_timeout_seconds = 0 disables the check (returns nil).

package session

import "time"

// StaleSession is a session reported by IsStale + ListStale.
type StaleSession struct {
	*Session
	StaleSeconds int `json:"stale_seconds"`
}

// IsStale reports whether sess crossed the threshold. threshold <= 0
// disables the check (always returns false).
func IsStale(sess *Session, threshold time.Duration, now time.Time) bool {
	if sess == nil || threshold <= 0 {
		return false
	}
	if sess.State != StateRunning {
		return false
	}
	return now.Sub(sess.UpdatedAt) > threshold
}

// ListStale walks the session list and returns the stale ones with
// the elapsed-since-update duration in seconds. Sessions on other
// hosts are skipped — use the per-host federation surface for cross-
// host queries.
func ListStale(sessions []*Session, hostname string, threshold time.Duration, now time.Time) []StaleSession {
	if threshold <= 0 {
		return nil
	}
	out := make([]StaleSession, 0)
	for _, s := range sessions {
		if hostname != "" && s.Hostname != hostname {
			continue
		}
		if IsStale(s, threshold, now) {
			out = append(out, StaleSession{
				Session:      s,
				StaleSeconds: int(now.Sub(s.UpdatedAt).Seconds()),
			})
		}
	}
	return out
}
