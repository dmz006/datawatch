// Push-on-complete outcome recording: after the daemon attempts to push
// a completed PRD's work, recordPushOutcome folds the git.PushResult
// into the PRD's persisted Git workflow state so the operator can see,
// from the PRD itself, whether the work reached the remote — and why
// not, when it did not.
//
// Discipline: this function never touches or rolls back the commit. A
// failed push leaves the local branch exactly where it is; the PRD just
// carries PushState=not_pushed plus a human-readable PushWarning and an
// operator alert (level "warn") with the reason as body. A later
// successful push clears the warning and records ref/SHA/timestamp.
//
// Secret scan: an enforce refusal (Kind=secret_found) takes the failure
// path — its Reason is already redacted (counts, detector names,
// file:line, fingerprints; the scanner never keeps the secret itself).
// Shadow-mode findings on a successful push are recorded the same way
// the failure path records an outcome: a Decision row plus a level
// "warn" operator alert, both carrying only the redacted summary.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/git"
)

// secretScanAuditFindings converts redacted scan findings into the
// reporter's map form, carrying ONLY the four allowed fields (detector,
// path, line, fingerprint). No field of git.SecretFinding could carry
// the matched secret in the first place.
func secretScanAuditFindings(findings []git.SecretFinding) []map[string]any {
	out := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		out = append(out, map[string]any{
			"detector":    f.Detector,
			"path":        f.Path,
			"line":        f.Line,
			"fingerprint": f.Fingerprint,
		})
	}
	return out
}

// recordPushOutcome folds res into prd.Git's push state, appends the
// matching Decision row, alerts the operator on failure (level "warn",
// title "Automaton <id> not pushed", reason as body), and saves the PRD.
// policy is the effective policy that was used for the attempt, included
// in the failure log line for operator debugging.
func (m *Manager) recordPushOutcome(prd *PRD, res git.PushResult, policy git.PushPolicy) {
	reason := strings.TrimSpace(res.Reason)
	if reason == "" {
		reason = string(res.Kind)
	}
	id := prd.ID
	if res.Pushed || res.NothingToPush {
		now := time.Now()
		// prd is the shared stored pointer (Store.GetPRD); mutate under the
		// store lock. SavePRD also takes Store.mu (non-reentrant), so it stays
		// out of the closure.
		var shadowSummary string
		m.store.WithLock(func() {
			prd.Git.PushState = PushStatePushed
			prd.Git.PushedRef = res.Ref
			prd.Git.PushedSHA = res.SHA
			prd.Git.PushedAt = &now
			prd.Git.PushWarning = ""
			prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
				At:   now,
				Kind: "git_pushed",
			}))
			if len(res.SecretFindings) > 0 {
				// Shadow-mode findings: the push went through, the PRD still
				// records the redacted findings as a Decision row (count,
				// detector, file:line, fingerprint; never the secret).
				shadowSummary = git.RedactedFindingsSummary(res.SecretFindings)
				prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
					At:   now,
					Kind: "git_secret_scan_shadow",
					Note: shadowSummary,
				}))
			}
		})
		if len(res.SecretFindings) > 0 {
			// The operator gets a "warn" alert, the same alert mechanism the
			// failure path uses.
			m.mu.Lock()
			alertSink := m.alertSink
			scanAuditFn := m.secretScanAuditFn
			m.mu.Unlock()
			if scanAuditFn != nil {
				// One secret_scan_shadow audit entry per shadow report,
				// redacted fields only (count, detector, file:line,
				// fingerprint; never the matched secret).
				scanAuditFn(audit.ActionSecretScanShadow, prd.ID, res.Remote, res.Ref, secretScanAuditFindings(res.SecretFindings))
			}
			if alertSink != nil {
				alertSink("warn", fmt.Sprintf("Automaton %s: %d pre-push secret finding(s) (shadow mode)", prd.ID, len(res.SecretFindings)), shadowSummary)
			}
			log.Printf("autonomous: prd %s pushed with shadow-mode secret scan findings: %s", prd.ID, shadowSummary)
		}
	} else {
		now := time.Now()
		m.store.WithLock(func() {
			prd.Git.PushState = PushStateNotPushed
			prd.Git.PushWarning = "not pushed: " + reason
			prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
				At:   now,
				Kind: "git_not_pushed",
				Note: reason,
			}))
		})
		m.mu.Lock()
		alertSink := m.alertSink
		scanAuditFn := m.secretScanAuditFn
		m.mu.Unlock()
		if res.Kind == git.PushFailSecret && scanAuditFn != nil {
			// One secret_scan_refused audit entry per enforce refusal,
			// redacted fields only (count, detector, file:line,
			// fingerprint; never the matched secret). Other failure
			// kinds never emit a secret-scan audit entry.
			scanAuditFn(audit.ActionSecretScanRefused, prd.ID, res.Remote, res.Ref, secretScanAuditFindings(res.SecretFindings))
		}
		if alertSink != nil {
			alertSink("warn", fmt.Sprintf("Automaton %s not pushed", id), reason)
		}
		log.Printf("autonomous: prd %s push-on-complete (%s) failed: %s", id, policy, reason)
	}
	m.store.WithLock(func() { prd.UpdatedAt = time.Now() })
	if err := m.savePRDOwn(prd); err != nil { // executor-owned: the rollup's expectedRev must see this Rev bump
		log.Printf("autonomous: prd %s saving push state: %v", id, err)
	}
}
