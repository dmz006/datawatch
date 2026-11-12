package autonomous

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// TestRecordReleaseGitAudit_NeverWritesTokenValue sets a sentinel token in
// DATAWATCH_GIT_TOKEN, records one release push, and checks that the
// serialized event names the credential source but never contains the token.
func TestRecordReleaseGitAudit_NeverWritesTokenValue(t *testing.T) {
	const sentinel = "sentinel-token-7f3a9c2e-do-not-log"
	t.Setenv("DATAWATCH_GIT_TOKEN", sentinel)

	var got []audit.Entry
	SetReleaseGitAuditSink(func(e audit.Entry) { got = append(got, e) })
	t.Cleanup(func() { SetReleaseGitAuditSink(nil) })

	recordReleaseGitAudit(audit.ActionReleasePush, "datawatch", "refs/heads/main", "origin", "environment-variable-name:DATAWATCH_GIT_TOKEN", "pushed")

	if len(got) != 1 {
		t.Fatalf("recorded %d events, want 1", len(got))
	}
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	line := string(raw)

	if got[0].Action != audit.ActionReleasePush {
		t.Errorf("action = %q, want %q", got[0].Action, audit.ActionReleasePush)
	}
	if !strings.Contains(line, "environment-variable-name:DATAWATCH_GIT_TOKEN") {
		t.Errorf("serialized event lacks the credential source label: %s", line)
	}
	if strings.Contains(line, sentinel) {
		t.Errorf("serialized event contains the token value: %s", line)
	}
}

// TestRecordReleaseGitAudit_NilSinkDrops checks that recording with no sink
// wired is a silent no-op rather than a panic.
func TestRecordReleaseGitAudit_NilSinkDrops(t *testing.T) {
	SetReleaseGitAuditSink(nil)
	recordReleaseGitAudit(audit.ActionReleaseTag, "datawatch", "v1.2.3", "origin", "host-login", "refused")
}
