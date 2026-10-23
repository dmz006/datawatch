package autonomous

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

const ledgerRateReason = "pool llm:claude rate ceiling: requests 3/3 in 1m0s, resets at 2026-10-23T12:05:00Z"

func TestRateCeiling_ClassifyWaitReason(t *testing.T) {
	cases := []string{
		ledgerRateReason,
		"pool llm:claude rate ceiling: tokens 900/900 in 1m0s",
		"pool llm:claude backpressure: " + ledgerRateReason,
	}
	for _, r := range cases {
		if got := ClassifyWaitReason(r); got != WaitCauseRateCeiling {
			t.Errorf("ClassifyWaitReason(%q) = %q, want rate_ceiling", r, got)
		}
	}
	// Existing classifications stay put.
	if got := ClassifyWaitReason("pool host full (1/1, held by a)"); got != WaitCausePoolFull {
		t.Errorf("pool full = %q", got)
	}
	if got := ClassifyWaitReason("pool host backpressure: pool host full (1/1, held by a)"); got != WaitCausePoolFull {
		t.Errorf("wrapped pool full = %q", got)
	}
}

func TestRateCeiling_WaitTextWithAndWithoutReset(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	with := RateCeilingWaitText(clock, "llm:claude", "requests", 3, 3, now.Add(5*time.Minute))
	if want := "pool llm:claude rate ceiling: requests 3/3 used, resets in 5m"; with != want {
		t.Errorf("with reset = %q, want %q", with, want)
	}
	without := RateCeilingWaitText(clock, "llm:claude", "tokens", 900, 900, time.Time{})
	if strings.Contains(without, "resets") {
		t.Errorf("without reset should omit resets: %q", without)
	}
	for _, s := range []string{with, without} {
		if ClassifyWaitReason(s) != WaitCauseRateCeiling {
			t.Errorf("own text not classified: %q", s)
		}
	}
}

func TestRateCeiling_ReadableWaitText(t *testing.T) {
	soon := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	got := ReadableWaitText("pool p rate ceiling: requests 2/2 in 1m0s, resets at "+soon, nil, nil)
	if !strings.HasPrefix(got, "pool p rate ceiling: requests 2/2 used, resets in ") {
		t.Errorf("readable with reset = %q", got)
	}
	got = ReadableWaitText("pool p rate ceiling: tokens 5/5 in 1m0s", nil, nil)
	if got != "pool p rate ceiling: tokens 5/5 used" {
		t.Errorf("readable without reset = %q", got)
	}
}

func TestRateCeiling_ResetsAtFromReason(t *testing.T) {
	at := rateCeilingResetsAt(ledgerRateReason)
	if at == nil || !at.Equal(time.Date(2026, 10, 23, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("resets_at = %v", at)
	}
	if got := waitResetsAt(WaitCauseRateCeiling, ledgerRateReason, nil, nil, nil); got == nil || !got.Equal(*at) {
		t.Errorf("waitResetsAt = %v", got)
	}
	if rateCeilingResetsAt("pool p rate ceiling: tokens 5/5 in 1m0s") != nil {
		t.Error("no reset expected")
	}
}

func TestRateCeiling_AdmissionWaitAuditCarriesCauseAndPool(t *testing.T) {
	var got []audit.Entry
	rec := NewCapacityAudit(func(e audit.Entry) { got = append(got, e) })
	rec.AdmissionWait("task1", "prd1", WaitCauseRateCeiling, ledgerRateReason, []string{"llm:claude"})
	rec.AdmissionWait("task2", "prd1", WaitCauseRateCeiling, "pool p rate ceiling: tokens 5/5 in 1m0s", []string{"p"})
	if len(got) != 2 {
		t.Fatalf("entries = %d", len(got))
	}
	if got[0].Action != audit.ActionCapacityWait {
		t.Errorf("action = %q", got[0].Action)
	}
	d := got[0].Details
	if d["cause"] != "rate_ceiling" || d["pool"] != "llm:claude" || d["resets_at"] != "2026-10-23T12:05:00Z" {
		t.Errorf("details = %v", d)
	}
	if _, ok := got[1].Details["resets_at"]; ok {
		t.Errorf("resets_at should be absent: %v", got[1].Details)
	}
}
