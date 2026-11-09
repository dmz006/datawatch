// Package git — host audit trail.
//
// host_audit.go writes one audit entry per completed host-facing operation
// (change request, issue, release, tag) through the operator audit log. The
// facade functions in host_facade.go and CreateTagForHost in tag.go call
// RecordHostAudit after a successful operation. Pushes made by the merge
// pipeline are not audited here.
//
// The event carries names only: host, kind, operation, repo, ref and the
// credential source (host_login, vault or broker). No token field exists, and
// any URL credentials in the text fields are stripped with redactCreds.

package git

import (
	"context"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// Host audit operations, one per audited facade call.
const (
	HostAuditMR      = "mr"
	HostAuditIssue   = "issue"
	HostAuditRelease = "release"
	HostAuditTag     = "tag"
)

// HostAuditEvent describes one completed host operation. Host is the
// git.hosts name and Kind its kind. Action is one of the HostAudit constants.
// Repo is "owner/repo" (empty for tags). Ref is the branch, issue title, tag
// or release tag the operation used. CredentialSource is the host's
// credential source; it is never the credential itself.
type HostAuditEvent struct {
	Host             string
	Kind             string
	Action           string
	Repo             string
	Ref              string
	CredentialSource string
}

var (
	hostAuditMu  sync.RWMutex
	hostAuditLog *audit.Log
)

// SetHostAuditLog wires the operator audit log that RecordHostAudit writes
// to. Passing nil turns host auditing off.
func SetHostAuditLog(l *audit.Log) {
	hostAuditMu.Lock()
	defer hostAuditMu.Unlock()
	hostAuditLog = l
}

// RecordHostAudit writes event to the audit log as one
// audit.ActionHostGitOperation entry. It returns nil without writing when no
// audit log is wired. Callers ignore its error: a failed audit write must not
// fail the host operation it describes. ctx is accepted for the call contract;
// the audit write is not cancelled by it, so an operation that completed is
// still recorded.
func RecordHostAudit(ctx context.Context, event HostAuditEvent) error {
	_ = ctx
	hostAuditMu.RLock()
	l := hostAuditLog
	hostAuditMu.RUnlock()
	if l == nil {
		return nil
	}
	source := event.CredentialSource
	if source == "" {
		source = "host_login"
	}
	return l.Write(audit.Entry{
		Timestamp: time.Now(),
		Actor:     "daemon",
		Action:    audit.ActionHostGitOperation,
		Subject:   redactCreds(event.Repo),
		Details: map[string]any{
			"host":              redactCreds(event.Host),
			"kind":              redactCreds(event.Kind),
			"operation":         redactCreds(event.Action),
			"repo":              redactCreds(event.Repo),
			"ref":               redactCreds(event.Ref),
			"credential_source": redactCreds(source),
		},
	})
}

// recordHostOperation audits one completed facade operation on host.
func recordHostOperation(ctx context.Context, host GitHostConfig, operation, repo, ref string) {
	_ = RecordHostAudit(ctx, HostAuditEvent{
		Host:             host.Name,
		Kind:             host.Kind,
		Action:           operation,
		Repo:             repo,
		Ref:              ref,
		CredentialSource: host.CredentialSource,
	})
}
