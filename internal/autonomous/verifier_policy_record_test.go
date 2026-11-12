package autonomous

import (
	"testing"
	"time"
)

func TestVerifierPolicyRecordChoiceWritesTaskDecisionAndAudit(t *testing.T) {
	m, cap, prdID, ids := escalationFixture(t)
	if err := m.RecordVerifierPolicyChoice(prdID, ids[0], "ollama-a", "qwen", "prefer-free: ollama-a has a free slot"); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	got := prd.Story[0].Tasks[0].VerifierPolicyChoice
	if got == nil || got.Backend != "ollama-a" || got.Model != "qwen" || got.Reason == "" || got.At.IsZero() {
		t.Fatalf("task choice = %+v", got)
	}
	if prd.Story[0].Tasks[1].VerifierPolicyChoice != nil {
		t.Fatalf("sibling task got a choice: %+v", prd.Story[0].Tasks[1].VerifierPolicyChoice)
	}
	d := prd.Decisions[len(prd.Decisions)-1]
	if d.Kind != "verifier_policy" || d.Backend != "ollama-a" || d.Model != "qwen" || !contains(d.Note, "task="+ids[0]) {
		t.Fatalf("decision = %+v", d)
	}
	e := cap.only(t)
	if e.Action != VerifierPolicyAuditAction || e.Details["backend"] != "ollama-a" || e.Details["task_id"] != ids[0] {
		t.Fatalf("audit entry = %+v", e)
	}
}

func TestVerifierPolicyRecordChoiceUnknownTask(t *testing.T) {
	m, cap, prdID, _ := escalationFixture(t)
	if err := m.RecordVerifierPolicyChoice(prdID, "nope", "a", "", "r"); err == nil {
		t.Fatal("want error for unknown task")
	}
	if err := m.RecordVerifierPolicyChoice("nope", "x", "a", "", "r"); err == nil {
		t.Fatal("want error for unknown prd")
	}
	if len(cap.all()) != 0 {
		t.Fatalf("unexpected audit entries: %+v", cap.all())
	}
}

func TestVerifierPolicyHeldBackendsReadsBackoffRegistry(t *testing.T) {
	m, _, _, _ := escalationFixture(t)
	if got := m.HeldBackends([]string{"claude-code"}); got != nil {
		t.Fatalf("no registry: got %v", got)
	}
	bo := NewBackoff()
	m.SetBackoff(bo)
	bo.Set("claude-code", time.Now().Add(time.Hour), "paused", BackoffSourceOperator)
	got := m.HeldBackends([]string{"claude-code", "ollama-a"})
	if !got["claude-code"] || got["ollama-a"] {
		t.Fatalf("held = %v", got)
	}
}

func TestVerifierPolicyWindowExpiresEntries(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	w := NewVerifierPolicyWindow(func() time.Time { return now })
	w.Record()
	now = now.Add(30 * time.Minute)
	w.Record()
	if n := w.Count(time.Hour); n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	now = now.Add(31 * time.Minute) // first entry is now 61m old
	if n := w.Count(time.Hour); n != 1 {
		t.Fatalf("count after first expires = %d, want 1", n)
	}
	now = now.Add(time.Hour)
	if n := w.Count(time.Hour); n != 0 {
		t.Fatalf("count after all expire = %d, want 0", n)
	}
}
