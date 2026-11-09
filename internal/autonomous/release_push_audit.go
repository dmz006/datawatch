// Audit recorder for release git operations (pushes and tags). It records
// which project, ref and remote were touched, which credential source was
// used and the outcome. The credential is named by a source label only (for
// example host-login, credential-helper, ssh-agent, an environment-variable
// name or a vault-secret name). The token value is never a parameter here,
// so it can never be written to the audit log.
package autonomous

import (
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// releaseGitAudit holds the process-wide sink for release git audit entries.
// It is nil until a daemon wires one, and a nil sink drops entries.
var releaseGitAudit struct {
	mu   sync.RWMutex
	sink func(audit.Entry)
	now  func() time.Time
}

// SetReleaseGitAuditSink wires the sink that receives release git audit
// entries. Passing nil disables recording.
func SetReleaseGitAuditSink(sink func(audit.Entry)) {
	releaseGitAudit.mu.Lock()
	defer releaseGitAudit.mu.Unlock()
	releaseGitAudit.sink = sink
}

// recordReleaseGitAudit writes one audit entry for a release git operation.
// action is audit.ActionReleasePush or audit.ActionReleaseTag. The entry's
// subject is the project, and its details carry ref, remote,
// credential_source and outcome. The credential is recorded as a source label
// only; the function has no parameter for a token value.
func recordReleaseGitAudit(action string, project string, ref string, remote string, credentialSource string, outcome string) {
	releaseGitAudit.mu.RLock()
	sink := releaseGitAudit.sink
	releaseGitAudit.mu.RUnlock()
	if sink == nil {
		return
	}
	sink(audit.Entry{
		Timestamp: time.Now(),
		Actor:     AuditActorAutonomous,
		Action:    action,
		Subject:   project,
		Details: map[string]any{
			"project":           project,
			"ref":               ref,
			"remote":            remote,
			"credential_source": credentialSource,
			"outcome":           outcome,
		},
	})
}
