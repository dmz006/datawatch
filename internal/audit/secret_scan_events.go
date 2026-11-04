// Pre-push secret-scan audit (AGENT.md's Audit Logging Rule).
//
// The pre-push secret scan (git.secret_scan) emits exactly one entry per
// scanned push that is security-relevant: an enforce-mode refusal
// (ActionSecretScanRefused) or a shadow-mode finding report
// (ActionSecretScanShadow). Every entry carries the subject, the push
// target, the mode and the REDACTED findings (detector name, file path,
// line, short fingerprint) only. The matched secret has no path into the
// entry: neither the constructor nor the sink accepts it, so a failed or
// hostile scan leaves nothing but redacted metadata in the audit log.
//
// The package deliberately does NOT import internal/git: SecretScanFinding
// is a small local struct and callers convert their git findings into it.

package audit

import (
	"fmt"
	"os"
	"time"
)

// SecretScanFinding is the audit package's local copy of one redacted
// pre-push secret-scan finding. Fingerprint is a short (16-hex) SHA-256
// digest of the matched material — stable for allow-listing and dedupe,
// reveals nothing usable about the secret itself. No field of
// SecretScanFinding holds the matched secret.
type SecretScanFinding struct {
	Detector    string // detector name, e.g. "github_token"
	Path        string // repository-relative file path
	Line        int    // 1-based line number of the match
	Fingerprint string // short SHA-256 hex digest of the matched material
}

// secretScanMode reports the secret-scan mode an action was recorded in,
// so the entry's details carry the mode even though the action name alone
// already implies it.
func secretScanMode(action string) string {
	if action == ActionSecretScanRefused {
		return "enforce"
	}
	return "shadow"
}

// SecretScanEvent builds the audit entry for one scanned push that was
// refused (enforce mode) or reported (shadow mode) from REDACTED inputs
// only: the subject (the session or Automaton that attempted the push),
// the push target (remote, ref), and per finding the detector name, file
// path, line number and short fingerprint. The constructor has NO
// parameter that could carry a matched secret, so one can never reach the
// log. Returns an error on an empty subject or an unknown action rather
// than dropping or misclassifying the event. The caller's timestamp wins;
// a zero timestamp falls back to now.
func SecretScanEvent(action, subject, remote, ref string, findings []SecretScanFinding, at time.Time) (Entry, error) {
	switch action {
	case ActionSecretScanRefused, ActionSecretScanShadow:
	default:
		return Entry{}, fmt.Errorf("audit: unknown secret scan action %q (want %s or %s)",
			action, ActionSecretScanRefused, ActionSecretScanShadow)
	}
	if subject == "" {
		return Entry{}, fmt.Errorf("audit: subject required for %s", action)
	}
	if at.IsZero() {
		at = time.Now()
	}
	redacted := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		redacted = append(redacted, map[string]any{
			"detector":    f.Detector,
			"path":        f.Path,
			"line":        f.Line,
			"fingerprint": f.Fingerprint,
			// NEVER the matched secret: the struct has no field for it.
		})
	}
	return Entry{
		Timestamp: at,
		Actor:     "daemon",
		Action:    action,
		Subject:   subject, // the session or Automaton id that attempted the push
		Details: map[string]any{
			"remote":        remote,
			"ref":           ref,
			"mode":          secretScanMode(action),
			"finding_count": len(findings),
			"findings":      redacted,
			// NEVER the matched secret: the constructor has no parameter for it.
		},
	}, nil
}

// SecretScanReporter is the plain callback the session and Automata
// managers use to report one scanned push: (action, subject, remote, ref,
// redacted findings). It is a plain function type whose parameters are all
// stdlib types, so a manager can carry a compatible plain func value
// WITHOUT importing internal/audit; the daemon wires the sink below into
// it at construction time.
type SecretScanReporter func(action, subject, remote, ref string, findings []map[string]any)

// NewSecretScanSink returns a SecretScanReporter that redacts each
// reported finding to its four allowed keys (detector, path, line,
// fingerprint) and records the push as a secret_scan_refused or
// secret_scan_shadow entry in l. Any other key a caller reports — secret,
// value or otherwise — is dropped at this boundary, so a matched secret
// has no path into the log even if a caller tried to smuggle one in. A
// nil log yields a no-op reporter so a missing audit log can never be an
// error path in a manager.
func NewSecretScanSink(l *Log) SecretScanReporter {
	if l == nil {
		return func(action, subject, remote, ref string, findings []map[string]any) {}
	}
	return func(action, subject, remote, ref string, findings []map[string]any) {
		redacted := make([]SecretScanFinding, 0, len(findings))
		for _, m := range findings {
			redacted = append(redacted, SecretScanFinding{
				Detector:    secretScanString(m["detector"]),
				Path:        secretScanString(m["path"]),
				Line:        secretScanInt(m["line"]),
				Fingerprint: secretScanString(m["fingerprint"]),
			})
		}
		e, err := SecretScanEvent(action, subject, remote, ref, redacted, time.Time{})
		if err != nil {
			// A malformed report must not crash the call path; surface it
			// loudly instead of inventing an entry that would fail the same
			// validation.
			fmt.Fprintf(os.Stderr, "[audit] dropped secret scan event: %v\n", err)
			return
		}
		_ = l.Write(e)
	}
}

// secretScanString coerces a reported finding field to a string, mapping
// anything else to "" (a line number under "path" is noise, not data).
func secretScanString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// secretScanInt coerces a reported line number to an int, accepting the
// JSON round-trip float64 form as well; anything else maps to 0.
func secretScanInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
