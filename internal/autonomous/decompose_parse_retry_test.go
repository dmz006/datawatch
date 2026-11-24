package autonomous

// A decompose parse failure is a written verdict (the session produced
// unparseable output) — not an infra failure and not a terminal
// invalid-response. It is a fixable content failure: the parse error names
// the offending field and task, so it rides back to the planner as the next
// attempt's guidance within the content retry budget (AutoFixRetries,
// default 1), and only spent budget fails the attempt. These tests pin the
// reclassification on both entry points — the manager's retry loops and the
// reattach reader's IngestPlannerOutput.

import (
	"testing"
)

// badVerificationPlanJSON is a plan ParsePlanning rejects: task "Task"
// carries a number-typed "verification" field (it must be an object or a
// string), so the parse error names both the "verification" field and the
// offending task.
const badVerificationPlanJSON = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it","verification":42}]}]}`

func TestDecomposeParseFailureReclassifiedContentRetriedOnce(t *testing.T) {
	// Content budget of exactly 1 (AutoFixRetries): one parse failure may
	// consume it, the same second call succeeds on the guided re-attempt.
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1

	var gotSpecs []string
	fn := func(req DecomposeRequest) (string, error) {
		gotSpecs = append(gotSpecs, req.Spec)
		if len(gotSpecs) == 1 {
			return badVerificationPlanJSON, nil
		}
		return validPlanJSON, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	updated, derr := m.Decompose(prd.ID)
	if derr != nil {
		t.Fatalf("Decompose: %v — the second, parse-valid planner answer must be the saved plan", derr)
	}
	if len(gotSpecs) != 2 {
		t.Fatalf("decompose fn called %d times, want exactly 2 (one content retry)", len(gotSpecs))
	}
	if updated.Status != PRDNeedsReview {
		t.Errorf("status = %s, want %s (the valid second plan is saved)", updated.Status, PRDNeedsReview)
	}
	stories := m.store.storiesSnapshot(prd.ID)
	if len(stories) != 1 || len(stories[0].Tasks) != 1 || stories[0].Tasks[0].Title != "Task" {
		t.Errorf("saved stories = %+v, want the one story and its Task intact", stories)
	}
	// The second attempt's prompt carries the FIRST attempt's parse
	// failure — reason, the offending field, and the offending task — so
	// the planner fixes that specific output instead of guessing.
	spec2 := gotSpecs[1]
	for _, want := range []string{"Your previous attempt failed", "verification", "Task"} {
		if !contains(spec2, want) {
			t.Errorf("second prompt must carry guidance naming %q (parse reason + offending field + task)", want)
		}
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if got := final.RetryCountsByCategory["content"]; got != 1 {
		t.Errorf("content retry budget = %d, want exactly 1 consumed by the parse retry", got)
	}
	if got := final.RetryCountsByCategory["infra"]; got != 0 {
		t.Errorf("infra retry budget = %d, want 0 (a parse failure never touches infra)", got)
	}
	if final.LastDecomposeFailure != nil {
		t.Errorf("LastDecomposeFailure = %+v, want nil (the attempt succeeded on the retry)", final.LastDecomposeFailure)
	}
}

func TestIngestPlannerOutputParseFailureContentReason(t *testing.T) {
	f := newReattachFixture(t, "", "", nil)

	if err := f.m.IngestPlannerOutput(f.prdID, badVerificationPlanJSON); err == nil {
		t.Fatal("want a parse error for the number-typed verification field")
	}
	// The failed attempt must leave the PRD out of planning — the reader
	// can never spawn a fresh planner, so the attempt fails after
	// recording the content reason (and only after that record).
	if got := f.status(t); got != PRDDraft {
		t.Fatalf("status = %s, want %s (a failed reattach ingest leaves planning)", got, PRDDraft)
	}
	p, _ := f.m.Store().GetPRD(f.prdID)
	fr := p.LastDecomposeFailure
	if fr == nil {
		t.Fatal("LastDecomposeFailure = nil, want the recorded content-classified parse failure")
	}
	if fr.Category != "content" {
		t.Errorf("Category = %q, want %q (a written-but-unparseable plan is a content judgement, not a terminal invalid-response)", fr.Category, "content")
	}
	if fr.Source != "decompose-parse" {
		t.Errorf("Source = %q, want decompose-parse", fr.Source)
	}
	if !contains(fr.Detail, "verification") {
		t.Errorf("Detail = %q must name the failing field (verification)", fr.Detail)
	}
}
