package compute

import (
	"strings"
	"testing"
	"time"
)

var testAskNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func TestPlacementAskStore_AddAssignsSequentialIDsAndTimeout(t *testing.T) {
	st := NewPlacementAskStore()
	a := st.Add("llama3.1-8b", "gpu-box", "no node serves this model", 5, testAskNow, time.Minute)
	b := st.Add("qwen2.5-7b", "gpu-box", "no node serves this model", 0, testAskNow.Add(time.Second), time.Minute)
	if a.ID != "ask-1" || b.ID != "ask-2" {
		t.Fatalf("IDs = %q, %q; want ask-1, ask-2", a.ID, b.ID)
	}
	if !a.ExpiresAt.Equal(testAskNow.Add(time.Minute)) {
		t.Fatalf("ExpiresAt = %s; want %s", a.ExpiresAt, testAskNow.Add(time.Minute))
	}
	if a.Decision != AskDecisionPending {
		t.Fatalf("Decision = %q; want pending", a.Decision)
	}

	noTimeout := st.Add("mistral-7b", "gpu-box", "", 0, testAskNow, 0)
	if !noTimeout.ExpiresAt.IsZero() {
		t.Fatalf("timeout 0 must leave ExpiresAt zero, got %s", noTimeout.ExpiresAt)
	}
	if noTimeout.ModelSizeGB != 0 {
		t.Fatalf("ModelSizeGB = %d; want 0 (unknown size)", noTimeout.ModelSizeGB)
	}
}

func TestPlacementAskStore_GetPendingAllOrdering(t *testing.T) {
	st := NewPlacementAskStore()
	a := st.Add("a-model", "n1", "", 0, testAskNow, 0)
	b := st.Add("b-model", "n1", "", 0, testAskNow.Add(time.Second), 0)
	st.Add("c-model", "n1", "", 0, testAskNow.Add(2*time.Second), 0)

	if _, ok := st.Get(a.ID); !ok {
		t.Fatalf("Get(%s) not found", a.ID)
	}
	if _, ok := st.Get("ask-99"); ok {
		t.Fatalf("Get(ask-99) found; want not found")
	}

	if _, err := st.Answer(b.ID, AskDecisionDenied, testAskNow.Add(3*time.Second), "operator"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	pending := st.Pending()
	if len(pending) != 2 {
		t.Fatalf("Pending = %d; want 2 (denied ask excluded)", len(pending))
	}
	if pending[0].ID != a.ID || pending[1].ID != "ask-3" {
		t.Fatalf("Pending order = %s, %s; want oldest first", pending[0].ID, pending[1].ID)
	}

	if all := st.All(); len(all) != 3 {
		t.Fatalf("All = %d; want 3", len(all))
	}
}

func TestPlacementAskStore_AnswerIsOneShot(t *testing.T) {
	st := NewPlacementAskStore()
	a := st.Add("m", "n", "", 0, testAskNow, 0)

	approved, err := st.Answer(a.ID, AskDecisionApproved, testAskNow.Add(time.Second), "operator")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Decision != AskDecisionApproved || approved.DecidedBy != "operator" || approved.DecidedAt.IsZero() {
		t.Fatalf("approved ask = %+v", approved)
	}

	if _, err := st.Answer(a.ID, AskDecisionDenied, testAskNow.Add(2*time.Second), "operator"); err == nil {
		t.Fatalf("re-denying an approved ask must fail")
	} else if !strings.Contains(err.Error(), "already approved") {
		t.Fatalf("re-decision error = %q; want an already-approved message", err.Error())
	}

	// ask-2 does not exist:
	if _, err := st.Answer("ask-2", AskDecisionDenied, testAskNow, "operator"); err == nil {
		t.Fatalf("answering an unknown ask must fail")
	} else if !strings.Contains(err.Error(), "unknown ask") {
		t.Fatalf("unknown-ask error = %q; want an unknown-ask message", err.Error())
	}
	if _, err := st.Answer(a.ID, AskDecisionPending, testAskNow, "operator"); err == nil {
		t.Fatalf("decision pending is not an operator decision")
	}
}

func TestPlacementAskStore_ExpireStale(t *testing.T) {
	st := NewPlacementAskStore()
	expires := st.Add("m1", "n", "", 0, testAskNow, time.Minute)
	never := st.Add("m2", "n", "", 0, testAskNow, 0)
	far := st.Add("m3", "n", "", 0, testAskNow, time.Hour)

	n := st.ExpireStale(testAskNow.Add(2 * time.Minute))
	if n != 1 {
		t.Fatalf("ExpireStale = %d; want 1", n)
	}
	got, _ := st.Get(expires.ID)
	if got.Decision != AskDecisionExpired || got.DecidedBy != "timeout" {
		t.Fatalf("expired ask = %+v", got)
	}
	if g, _ := st.Get(never.ID); g.Decision != AskDecisionPending {
		t.Fatalf("no-timeout ask must stay pending, got %q", g.Decision)
	}
	if g, _ := st.Get(far.ID); g.Decision != AskDecisionPending {
		t.Fatalf("not-yet-expired ask must stay pending, got %q", g.Decision)
	}
	if len(st.Pending()) != 2 {
		t.Fatalf("Pending after expiry = %d; want 2", len(st.Pending()))
	}
	// an expired ask cannot be re-decided:
	if _, err := st.Answer(expires.ID, AskDecisionApproved, testAskNow.Add(3*time.Minute), "operator"); err == nil {
		t.Fatalf("answering an expired ask must fail")
	} else if !strings.Contains(err.Error(), "already expired") {
		t.Fatalf("re-deciding an expired ask = %q; want an already-expired message", err.Error())
	}
}
