// 2026-10-19 incident pin — "a verdict, once, is not twice, and not zero".
//
// The incident: a guardrail/decompose verdict produced from WRITTEN output
// (a response the session actually wrote but that could not be parsed as
// a verdict) was being classified into the infra retry path. That is
// wrong twice over: (a) an infra retry re-runs a judge whose written
// response we could not understand, risking running it twice and
// discarding a good second plan/response; (b) the infra retry budget
// (operator money + time) gets consumed by a failure that was never a
// mechanical one. The fix (cmd/datawatch/main.go) maps written-but-
// unparseable output to a TERMINAL "invalid-response" category verdict —
// consumed exactly once, budget untouched — while the genuinely
// no-output (infra) case keeps exactly one bounded retry. These tests
// pin both sides of that line, plus the decompose-side twin (a plan the
// session wrote but ParsePlanning could not parse fails on the first
// attempt, with the parse reason on the returned error).
package autonomous

import (
	"context"
	"strings"
	"testing"
)

func TestRunGuardrailWithInfraRetry_WrittenInvalidResponseVerdict_ConsumedOnce_NoInfraBudget(t *testing.T) {
	// The new terminal unparseable-response verdict the fix in
	// cmd/datawatch/main.go produces: the judge DID write a response, it
	// just could not be parsed — Category "invalid-response" (any
	// non-infra block carries the same contract). It must be consumed
	// exactly once: no retry, no re-run, no dropped verdict, and — the
	// heart of the incident fix — the infra retry budget untouched.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1"}
	calls := 0
	want := GuardrailVerdict{
		Outcome:   "block",
		Severity:  "medium",
		Category:  "invalid-response",
		Summary:   "guardrail judge returned 42 bytes — its response could not be parsed as a verdict: parse error",
		Issues:    []string{"raw judge output (42 bytes): <not json>"},
	}
	fn := func(ctx context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		wantCopy := want
		return wantCopy, nil
	}
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, GuardrailInvocation{PRDID: prd.ID}, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want EXACTLY 1 — a written verdict is consumed once, not twice", calls)
	}
	// The verdict must be that same one: not dropped, not replaced, not
	// re-parsed from an empty file.
	if v.Outcome != want.Outcome || v.Category != want.Category || v.Summary != want.Summary {
		t.Fatalf("returned verdict = %+v, want the written invalid-response verdict %+v (consumed once, not dropped)", v, want)
	}
	if len(v.Issues) != 1 || v.Issues[0] != want.Issues[0] {
		t.Fatalf("returned verdict issues = %v, want %v (the written verdict's own findings preserved)", v.Issues, want.Issues)
	}
	// Heart of the fix: an invalid-response verdict must NOT consume the
	// infra retry budget.
	if got := prd.RetryCountsByCategory["infra"]; got != 0 {
		t.Fatalf("infra retry budget consumed %d — an invalid-response (non-infra) verdict must never touch the infra budget", got)
	}
}

func TestRunGuardrailWithInfraRetry_NoOutputInfraVerdict_RetriesOnceThenPasses(t *testing.T) {
	// The genuine no-output case: the session ended WITHOUT writing any
	// verdict (the exact summary rules_review.go's OneShotNoOutputError
	// path surfaces for a judge session). That IS an infra-category
	// failure and keeps exactly one bounded retry: with the infra
	// budget at 1, the block is retried once — not twice — the pass
	// verdict on the second call is what comes back, and the budget
	// shows exactly 1 consumed.
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"infra": 1}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1"}
	calls := 0
	fn := func(ctx context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		if calls == 1 {
			return GuardrailVerdict{
				Outcome:  "block", // verdict outcome domain: "pass" | "warn" | "block"
				Category: "infra",
				Summary:  "session ended without producing output",
			}, nil
		}
		return GuardrailVerdict{Outcome: "pass", Summary: "second session recovered"}, nil
	}
	v, err := m.runGuardrailWithInfraRetry(context.Background(), prd, GuardrailInvocation{PRDID: prd.ID}, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("fn called %d times, want exactly 2 (one infra retry within budget=1, not twice)", calls)
	}
	if v.Outcome != "pass" || v.Summary != "second session recovered" {
		t.Fatalf("returned verdict = %+v, want the PASS verdict recovered on the second call", v)
	}
	if got := prd.RetryCountsByCategory["infra"]; got != 1 {
		t.Fatalf("infra budget consumed %d, want exactly 1", got)
	}
}

func TestDecompose_WrittenButUnparseable_ParsesOnce_FailsImmediatelyWithParseReason(t *testing.T) {
	// The decompose-side twin of the incident: the session DID write an
	// output file (the fn returns non-empty content, no error), but the
	// file is not parseable JSON. That is a written verdict, not an
	// infra failure — it must fail on the first attempt (no retry, no
	// discarded-second-plan risk), leave the infra retry budget
	// untouched, and the returned error must carry ParsePlanning's own
	// parse reason so the operator sees WHY.
	cfg := DefaultConfig()
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return "this is not json at all", nil // content present, no error
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	_, derr := m.Decompose(prd.ID)
	if derr == nil {
		t.Fatal("Decompose: want the parse failure returned, got success")
	}
	if calls != 1 {
		t.Fatalf("decompose fn called %d times, want EXACTLY 1 — a written-but-unparseable plan must not be retried (retry risks discarding a good second plan)", calls)
	}
	errText := derr.Error()
	if !strings.Contains(errText, "parse") {
		t.Errorf("error %q must name the parse failure (from ParsePlanning)", errText)
	}
	if !strings.Contains(errText, "invalid character") {
		t.Errorf("error %q must carry the invalid-JSON reason from ParsePlanning", errText)
	}
	if !strings.Contains(errText, "this is not json at all") {
		t.Errorf("error %q must quote the offending (cleaned) raw plan so the operator sees the actual bad output", errText)
	}

	final, _ := m.Store().GetPRD(prd.ID)
	if final.RetryCountsByCategory["infra"] != 0 {
		t.Fatalf("infra retry budget consumed %d — a written-but-unparseable plan is NOT an infra failure and must not consume the infra budget", final.RetryCountsByCategory["infra"])
	}
	// Failed attempts roll the PRD back to draft so an operator (or a
	// reset) can re-drive it.
	if final.Status != PRDDraft {
		t.Errorf("status = %s, want %s (rolled back after the failed decompose)", final.Status, PRDDraft)
	}
	// The failure must be recorded on the PRD — the 2026-10-19 incident
	// fix's contract is that a written verdict's failure is classified
	// and visible, never silently dropped: LastDecomposeFailure names the
	// parse reason and carries the terminal invalid-response category
	// (NOT infra — that would make the operator believe a healthy retry
	// could have fixed it), and is not itself retryable.
	if final.LastDecomposeFailure == nil {
		t.Fatal("LastDecomposeFailure = nil, want the recorded decompose failure")
	}
	if final.LastDecomposeFailure.Category != "invalid-response" {
		t.Errorf("LastDecomposeFailure.Category = %q, want invalid-response (written but unparseable — terminal, never infra)", final.LastDecomposeFailure.Category)
	}
	if final.LastDecomposeFailure.Retryable {
		t.Error("LastDecomposeFailure.Retryable = true, want false (written verdicts are consumed once, not auto-retryable)")
	}
	if !strings.Contains(final.LastDecomposeFailure.Detail, "parse") && !strings.Contains(final.LastDecomposeFailure.Detail, "this is not json at all") {
		t.Errorf("LastDecomposeFailure.Detail = %q must name the parse failure reason", final.LastDecomposeFailure.Detail)
	}
	if final.LastDecomposeFailure.Source != "decompose-parse" {
		t.Errorf("LastDecomposeFailure.Source = %q, want decompose-parse", final.LastDecomposeFailure.Source)
	}
}
