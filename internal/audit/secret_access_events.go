// LLM API key secret-access audit (AGENT.md's Audit Logging Rule).
//
// Every call-time resolution of an LLM API key secret produces exactly
// one ActionLLMSecretAccess entry — success or failure. The entry carries
// names and the outcome only; the secret value and the literal key have no
// path into it (the constructor accepts names and the outcome and nothing
// else), so a failed or hostile resolution leaves nothing but the name and
// the outcome in the audit log.

package audit

import (
	"fmt"
	"os"
	"time"
)

// Outcomes for ActionLLMSecretAccess.
const (
	// SecretAccessOk — the secret resolved and was handed to the caller.
	SecretAccessOk = "ok"
	// SecretAccessMissing — no secret is set for that LLM entry.
	SecretAccessMissing = "missing"
	// SecretAccessRefused — resolution was declined or could not be
	// authorised.
	SecretAccessRefused = "refused"
)

func validSecretAccessOutcome(outcome string) bool {
	switch outcome {
	case SecretAccessOk, SecretAccessMissing, SecretAccessRefused:
		return true
	}
	return false
}

// SecretAccessEvent builds the ActionLLMSecretAccess entry for one
// call-time secret resolution. llmName and secretName are NAMES ONLY:
// the secret value and the literal key are never parameters and therefore
// can never reach the log. Returns an error on an empty name or an
// unknown outcome rather than dropping or misclassifying the event. The
// caller's timestamp wins; a zero timestamp falls back to now.
func SecretAccessEvent(llmName, secretName, outcome string, at time.Time) (Entry, error) {
	if llmName == "" {
		return Entry{}, fmt.Errorf("audit: llm name required for %s", ActionLLMSecretAccess)
	}
	if secretName == "" {
		return Entry{}, fmt.Errorf("audit: secret name required for %s", ActionLLMSecretAccess)
	}
	if !validSecretAccessOutcome(outcome) {
		return Entry{}, fmt.Errorf("audit: unknown secret access outcome %q (want %s, %s or %s)",
			outcome, SecretAccessOk, SecretAccessMissing, SecretAccessRefused)
	}
	if at.IsZero() {
		at = time.Now()
	}
	return Entry{
		Timestamp: at,
		Actor:     "daemon",
		Action:    ActionLLMSecretAccess,
		Subject:   llmName, // the LLM entry the key belongs to
		Details: map[string]any{
			"llm_name":    llmName,
			"secret_name": secretName,
			"outcome":     outcome,
			// NEVER the secret value, NEVER the literal key: the constructor
			// has no parameter for them.
		},
	}, nil
}

// SecretAccessReporter is the plain callback the dispatcher uses to report
// each call-time resolution: (llm entry name, secret name, outcome). It is
// a plain three-string function type, so the inference package can carry a
// one of — or a compatible plain func(llmName, secretName, outcome string)
// value — WITHOUT importing internal/audit; the daemon wires the sink below
// into it at construction time.
type SecretAccessReporter func(llmName, secretName, outcome string)

// NewSecretAccessSink returns a SecretAccessReporter that records each
// reported resolution as an ActionLLMSecretAccess entry in l. A nil log
// yields a no-op reporter so a missing audit log can never be an error path
// in the dispatcher.
func NewSecretAccessSink(l *Log) SecretAccessReporter {
	if l == nil {
		return func(llmName, secretName, outcome string) {}
	}
	return func(llmName, secretName, outcome string) {
		e, err := SecretAccessEvent(llmName, secretName, outcome, time.Time{})
		if err != nil {
			// A malformed report must not crash the call path; surface it
			// loudly instead of inventing an entry that would fail the same
			// validation.
			fmt.Fprintf(os.Stderr, "[audit] dropped %s event: %v\n", ActionLLMSecretAccess, err)
			return
		}
		_ = l.Write(e)
	}
}
