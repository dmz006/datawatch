package main

import (
	"testing"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
)

// A daemon-driven back-off or budget event reaches the audit log as actor
// "autonomous" with the backend or LLM as its subject.
func TestEmitBackoffAudit_RecordsActorAndSubject(t *testing.T) {
	l, err := auditpkg.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	setBackoffAuditLog(l)
	defer setBackoffAuditLog(nil)

	emitBackoffAudit(auditpkg.ActionUsageBackoffAutoCleared, map[string]any{"backend": "claude-code", "until": "2026-10-22T12:00:00Z"})
	emitBackoffAudit(auditpkg.ActionBudgetCeilingWait, map[string]any{"llm": "opencode", "spend_usd": 5.0, "ceiling_usd": 5.0})

	es, err := l.Read(auditpkg.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 {
		t.Fatalf("want 2 entries, got %+v", es)
	}
	subjects := map[string]string{}
	for _, e := range es {
		if e.Actor != "autonomous" {
			t.Errorf("%s actor = %q, want autonomous", e.Action, e.Actor)
		}
		subjects[e.Action] = e.Subject
	}
	if subjects[auditpkg.ActionUsageBackoffAutoCleared] != "claude-code" || subjects[auditpkg.ActionBudgetCeilingWait] != "opencode" {
		t.Errorf("subjects = %v", subjects)
	}
}
