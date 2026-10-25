// Audit-entry tests for the notify-events setter (AGENT.md Audit Logging
// Rule): SetPRDNotifyEvents writes exactly one autonomous_notify_events_changed
// record per actual change (old "", then the replaced list; clear sets
// new to ""), and CreateGuardrailProfile / UpdateGuardrailProfile write one
// scope-"profile" record with the id, old and new — no record on
// no-change writes or on invalid writes.

package autonomous

import (
	"slices"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// fakeAuditLog captures the (action, details) pairs handed to the Manager
// via SetAuditFn, in order.
type fakeAuditLog struct {
	actions []string
	details []map[string]any
}

func (f *fakeAuditLog) fn(action string, details map[string]any) {
	f.actions = append(f.actions, action)
	f.details = append(f.details, details)
}

func (f *fakeAuditLog) reset() { f.actions, f.details = nil, nil }

func (f *fakeAuditLog) count(action string) int {
	n := 0
	for _, a := range f.actions {
		if a == action {
			n++
		}
	}
	return n
}

func TestAutomatonNotifyEventsAudit_entryOnSet(t *testing.T) {
	m := notifyTestManager(t)
	fake := &fakeAuditLog{}
	m.SetAuditFn(fake.fn)
	proj := t.TempDir()
	prd, err := m.CreatePRD("spec", proj, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	_, err = m.SetPRDNotifyEvents(prd.ID, []string{"prd.blocked", "prd.completed"})
	if err != nil {
		t.Fatalf("SetPRDNotifyEvents: %v", err)
	}

	want := audit.ActionNotifyEventsChanged
	if got := fake.count(want); got != 1 {
		t.Fatalf("audited %q records = %d, want 1 (actions=%v)", want, got, fake.actions)
	}
	d := fake.details[0]
	if d["scope"] != "automaton" {
		t.Errorf("scope = %v, want %q", d["scope"], "automaton")
	}
	if d["prd_id"] != prd.ID {
		t.Errorf("prd_id = %v, want %q", d["prd_id"], prd.ID)
	}
	if d["old"] != "" {
		t.Errorf("old = %q, want empty (inherit before the write)", d["old"])
	}
	if d["new"] != "prd.blocked,prd.completed" {
		t.Errorf("new = %q, want %q", d["new"], "prd.blocked,prd.completed")
	}
	if d["source_after"] != NotifySourceAutomaton {
		t.Errorf("source_after = %v, want %q", d["source_after"], NotifySourceAutomaton)
	}
}

func TestAutomatonNotifyEventsAudit_entryOnClear(t *testing.T) {
	m := notifyTestManager(t)
	fake := &fakeAuditLog{}
	m.SetAuditFn(fake.fn)
	proj := t.TempDir()
	prd, err := m.CreatePRD("spec", proj, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.SetPRDNotifyEvents(prd.ID, []string{"prd.blocked"}); err != nil {
		t.Fatalf("SetPRDNotifyEvents(set): %v", err)
	}
	fake.reset()

	// Clear → one entry with old list and new ""; the tier falls back to
	// the global default (no profile assigned).
	if _, err := m.SetPRDNotifyEvents(prd.ID, []string{}); err != nil {
		t.Fatalf("SetPRDNotifyEvents(clear): %v", err)
	}
	want := audit.ActionNotifyEventsChanged
	if got := fake.count(want); got != 1 {
		t.Fatalf("audited %q records = %d, want 1 (actions=%v)", want, got, fake.actions)
	}
	d := fake.details[0]
	if d["scope"] != "automaton" || d["prd_id"] != prd.ID {
		t.Errorf("scope/prd_id = %v/%v, want automaton/%q", d["scope"], d["prd_id"], prd.ID)
	}
	if d["old"] != "prd.blocked" {
		t.Errorf("old = %q, want %q", d["old"], "prd.blocked")
	}
	if d["new"] != "" {
		t.Errorf("new = %q, want empty (cleared to inherit)", d["new"])
	}
	if d["source_after"] != NotifySourceGlobal {
		t.Errorf("source_after = %v, want %q", d["source_after"], NotifySourceGlobal)
	}
}

func TestAutomatonNotifyEventsAudit_unchangedWritesNoRecord(t *testing.T) {
	m := notifyTestManager(t)
	fake := &fakeAuditLog{}
	m.SetAuditFn(fake.fn)
	proj := t.TempDir()
	prd, err := m.CreatePRD("spec", proj, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	list := []string{"task.session_started", "prd.completed"}
	if _, err := m.SetPRDNotifyEvents(prd.ID, list); err != nil {
		t.Fatalf("SetPRDNotifyEvents(first): %v", err)
	}
	if got := fake.count(audit.ActionNotifyEventsChanged); got != 1 {
		t.Fatalf("after first write: %d records, want 1", got)
	}
	fake.reset()

	// Same list again → no actual change → no audit entry.
	if _, err := m.SetPRDNotifyEvents(prd.ID, []string{"task.session_started", "prd.completed"}); err != nil {
		t.Fatalf("SetPRDNotifyEvents(unchanged): %v", err)
	}
	if got := fake.count(audit.ActionNotifyEventsChanged); got != 0 {
		t.Fatalf("unchanged write: %d records, want 0 (actions=%v)", got, fake.actions)
	}
}

func TestProfileNotifyEventsAudit_entryOnChange(t *testing.T) {
	m := notifyTestManager(t)
	fake := &fakeAuditLog{}
	m.SetAuditFn(fake.fn)
	p, err := m.CreateGuardrailProfile("ne-audit", "", nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateGuardrailProfile: %v", err)
	}

	// Update with a list → one scope-"profile" entry.
	if _, err := m.UpdateGuardrailProfile(p.ID, "ne-audit", "", nil, nil,
		[]string{"task.completed", "prd.completed"}); err != nil {
		t.Fatalf("UpdateGuardrailProfile(set): %v", err)
	}
	want := audit.ActionNotifyEventsChanged
	if got := fake.count(want); got != 1 {
		t.Fatalf("audited %q records = %d, want 1 (actions=%v)", want, got, fake.actions)
	}
	d := fake.details[0]
	if d["scope"] != "profile" {
		t.Errorf("scope = %v, want %q", d["scope"], "profile")
	}
	if d["profile_id"] != p.ID {
		t.Errorf("profile_id = %v, want %q", d["profile_id"], p.ID)
	}
	if d["old"] != "" {
		t.Errorf("old = %q, want empty (profile started empty)", d["old"])
	}
	if d["new"] != "task.completed,prd.completed" {
		t.Errorf("new = %q, want %q", d["new"], "task.completed,prd.completed")
	}
	if _, ok := d["source_after"]; ok {
		t.Errorf("profile entry must not carry source_after")
	}

	// Same list again → no actual change → no entry.
	fake.reset()
	if _, err := m.UpdateGuardrailProfile(p.ID, "ne-audit", "", nil, nil,
		[]string{"task.completed", "prd.completed"}); err != nil {
		t.Fatalf("UpdateGuardrailProfile(unchanged): %v", err)
	}
	if got := fake.count(want); got != 0 {
		t.Fatalf("unchanged update: %d records, want 0 (actions=%v)", got, fake.actions)
	}

	// Invalid list → error, no entry, stored list unchanged.
	fake.reset()
	_, err = m.UpdateGuardrailProfile(p.ID, "ne-audit", "", nil, nil, []string{"bad.kind"})
	if err == nil {
		t.Fatalf("UpdateGuardrailProfile(invalid): want error")
	}
	if got := fake.count(want); got != 0 {
		t.Fatalf("invalid update: %d records, want 0 (actions=%v)", got, fake.actions)
	}
	stored, _ := m.GetGuardrailProfile(p.ID)
	if !slices.Equal(stored.NotifyEvents, []string{"task.completed", "prd.completed"}) {
		t.Fatalf("stored NotifyEvents = %v, want [task.completed prd.completed]", stored.NotifyEvents)
	}
}
