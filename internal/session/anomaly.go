// BL11 — anomaly detection for session output streams.
//
// Pure-logic helpers that the session monitor calls to flag stuck-loop
// output, abnormally long input-wait duration, and abnormal session
// duration vs historical average. Configurable thresholds live in
// config.AnomalyConfig (mirrored to package-level types here so the
// session package keeps a clean detector contract).

package session

import (
	"strings"
	"time"
)

// AnomalyKind enumerates detection categories.
type AnomalyKind string

const (
	AnomalyStuckLoop      AnomalyKind = "stuck_loop"
	AnomalyLongInputWait  AnomalyKind = "long_input_wait"
	AnomalyDurationOutlier AnomalyKind = "duration_outlier"
)

// Anomaly is a single detection, surfaced via alerts/messages.
type Anomaly struct {
	Kind       AnomalyKind `json:"kind"`
	SessionID  string      `json:"session_id"`
	Detail     string      `json:"detail"`
	DetectedAt time.Time   `json:"detected_at"`
}

// AnomalyThresholds is the set of operator-tunable knobs.
type AnomalyThresholds struct {
	// StuckRepeatCount: same line N times in a row → stuck. 0 disables.
	StuckRepeatCount int
	// InputWaitSeconds: seconds in waiting_input before flagging. 0 disables.
	InputWaitSeconds int
	// DurationMultiplier: current/historical duration ratio that
	// triggers a duration outlier. 0 disables.
	DurationMultiplier float64
}

// DetectStuckLoop scans the tail of a session output buffer and
// returns a non-nil Anomaly if the last `threshold` lines are
// identical (modulo trailing whitespace). Empty/short buffers and
// threshold <= 1 return nil.
func DetectStuckLoop(sessionID, output string, threshold int) *Anomaly {
	if threshold <= 1 || output == "" {
		return nil
	}
	lines := splitNonEmptyLines(output)
	if len(lines) < threshold {
		return nil
	}
	tail := lines[len(lines)-threshold:]
	first := tail[0]
	for _, l := range tail[1:] {
		if l != first {
			return nil
		}
	}
	return &Anomaly{
		Kind:       AnomalyStuckLoop,
		SessionID:  sessionID,
		Detail:     "same line repeated " + itoa(threshold) + " times: " + truncateStr(first, 80),
		DetectedAt: time.Now(),
	}
}

// DetectLongInputWait flags sessions that have been in StateWaitingInput
// longer than threshold seconds.
func DetectLongInputWait(sess *Session, thresholdSeconds int) *Anomaly {
	if sess == nil || sess.State != StateWaitingInput || thresholdSeconds <= 0 {
		return nil
	}
	waited := time.Since(sess.UpdatedAt)
	if waited < time.Duration(thresholdSeconds)*time.Second {
		return nil
	}
	return &Anomaly{
		Kind:       AnomalyLongInputWait,
		SessionID:  sess.FullID,
		Detail:     "waiting_input for " + waited.Truncate(time.Second).String(),
		DetectedAt: time.Now(),
	}
}

// DetectDurationOutlier flags sessions whose current run-time exceeds
// historicalAvg * multiplier. historicalAvg <= 0 or multiplier <= 0
// disables the check.
func DetectDurationOutlier(sess *Session, historicalAvg time.Duration, multiplier float64) *Anomaly {
	if sess == nil || historicalAvg <= 0 || multiplier <= 0 {
		return nil
	}
	threshold := time.Duration(float64(historicalAvg) * multiplier)
	current := time.Since(sess.CreatedAt)
	if current < threshold {
		return nil
	}
	return &Anomaly{
		Kind:       AnomalyDurationOutlier,
		SessionID:  sess.FullID,
		Detail:     "running for " + current.Truncate(time.Second).String() +
			" (avg=" + historicalAvg.Truncate(time.Second).String() +
			", threshold=" + threshold.Truncate(time.Second).String() + ")",
		DetectedAt: time.Now(),
	}
}

func splitNonEmptyLines(s string) []string {
	out := make([]string, 0, 16)
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimRight(l, " \t\r")
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
