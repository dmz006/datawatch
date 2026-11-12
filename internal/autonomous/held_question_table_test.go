package autonomous

import (
	"regexp"
	"strings"
	"testing"
)

// heldTestItem is a fully populated held item for one state and category;
// every field it sets is plain text without the word "PRD", so the
// question text assertions hold.
func heldTestItem(state HeldState, category string) *HeldItem {
	return &HeldItem{
		State:             state,
		PRDID:             "prd-01",
		PRDTitle:          "Fix the lane fill pass",
		TaskID:            "task-01",
		TaskTitle:         "Implement the lane fill",
		AutomationID:      "auto-01",
		AutomationName:    "Lane Fill",
		MemberID:          "member-01",
		PendingID:         "pending-01",
		FailureCategory:   category,
		Reason:            "CI failed on the verify step",
		AttemptsUsed:      3,
		AvailableBackends: []string{"claude-code", "openai-codex"},
	}
}

// heldTestCategories covers every classification the executor uses plus
// unclassified and an out-of-vocabulary value.
func heldTestCategories() []string {
	return []string{"", FailureCategoryInfra, FailureCategoryContent, FailureCategoryScope, FailureCategoryPolicy, "mystery"}
}

func heldOptionIDSet(t *testing.T, opts []QuestionOption) map[string]bool {
	t.Helper()
	set := make(map[string]bool, len(opts))
	for _, o := range opts {
		if _, dup := set[o.ID]; dup {
			t.Fatalf("option id %q is repeated", o.ID)
		}
		set[o.ID] = true
	}
	return set
}

// holdOptionIsPresentAndEveryIDIsReal asserts the options a state offers:
// "hold" is always among them and every id is one the daemon implements.
func heldOptionIsReal(t *testing.T, id string) {
	t.Helper()
	if _, ok := heldOptionLabels[id]; !ok {
		t.Fatalf("option id %q is not a recognized held option", id)
	}
}

func TestHeldQuestionTableEveryStateAndCategory(t *testing.T) {
	// The table covers every one of the five states with every failure
	// category, classified or not.
	if len(HeldStates) != 5 {
		t.Fatalf("HeldStates is %v; want the five held state ids", HeldStates)
	}
	for _, state := range HeldStates {
		for _, cat := range heldTestCategories() {
			item := heldTestItem(state, cat)

			opts := HeldOptions(item)
			optSet := heldOptionIDSet(t, opts)
			if len(opts) < 2 {
				t.Fatalf("%s/%s: %d options; each state must offer at least two", state, cat, len(opts))
			}
			for id := range optSet {
				heldOptionIsReal(t, id)
			}
			if !optSet[HeldOptionHold] {
				t.Fatalf("%s/%s: options %v lack the mandatory hold option", state, cat, opts)
			}
			for _, o := range opts {
				if o.Label == "" || len(o.Label) > questionOptionLabelMaxBytes {
					t.Fatalf("%s/%s: option %q has an invalid label %q", state, cat, o.ID, o.Label)
				}
			}

			rec := HeldRecommendation(item)
			if rec == nil {
				t.Fatalf("%s/%s: nil recommendation", state, cat)
			}
			if !optSet[rec.OptionID] {
				t.Fatalf("%s/%s: recommendation names %q, which is not among %v", state, cat, rec.OptionID, opts)
			}
			if strings.TrimSpace(rec.Rationale) == "" {
				t.Fatalf("%s/%s: recommendation has no rationale", state, cat)
			}
			if len(rec.Rationale) > questionRationaleMaxBytes {
				t.Fatalf("%s/%s: rationale exceeds the cap", state, cat)
			}

			text := HeldQuestionText(item)
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s/%s: empty question text", state, cat)
			}
			if len(text) > questionTextMaxBytes {
				t.Fatalf("%s/%s: question text is %d bytes, over the cap of %d", state, cat, len(text), questionTextMaxBytes)
			}
			if strings.Contains(text, "PRD") {
				t.Fatalf("%s/%s: question text uses the word PRD: %q", state, cat, text)
			}

			q := BuildHeldQuestion(item)
			if err := ValidateNewQuestion(&q); err != nil {
				t.Fatalf("%s/%s: built question fails validation: %v", state, cat, err)
			}
			if q.Default != HeldOptionHold {
				t.Fatalf("%s/%s: default is %q; the forced default is hold", state, cat, q.Default)
			}
			if q.Label != "decision" {
				t.Fatalf("%s/%s: label is %q; want decision", state, cat, q.Label)
			}
			if q.Source != QuestionSourceHeld {
				t.Fatalf("%s/%s: source is %q; want %q", state, cat, q.Source, QuestionSourceHeld)
			}
			if q.AskerKind != QuestionAskerSystem {
				t.Fatalf("%s/%s: asker kind is %q; want the daemon kind", state, cat, q.AskerKind)
			}
			if !q.Blocking {
				t.Fatalf("%s/%s: a held question must be blocking", state, cat)
			}
			if q.Recommendation == nil || q.Recommendation.OptionID != rec.OptionID {
				t.Fatalf("%s/%s: question recommendation %v does not match the table", state, cat, q.Recommendation)
			}
		}
	}
}

func TestHeldQuestionOptionsSets(t *testing.T) {
	want := map[HeldState][]string{
		HeldBlocked:           {HeldOptionResume, HeldOptionRetry, HeldOptionChangeSpec, HeldOptionCancel, HeldOptionHold},
		HeldReviewHeld:        {HeldOptionRetry, HeldOptionRetryStronger, HeldOptionChangeSpec, HeldOptionCancel, HeldOptionHold},
		HeldRevisionRequested: {HeldOptionChangeSpec, HeldOptionResume, HeldOptionHold},
		HeldAwaitingDecision:  {HeldOptionResume, HeldOptionCancel, HeldOptionHold},
		HeldPendingInsert:     {HeldOptionApproveInsert, HeldOptionRejectInsert, HeldOptionHold},
	}
	for state, wantIDs := range want {
		opts := HeldOptions(heldTestItem(state, ""))
		if len(opts) != len(wantIDs) {
			t.Fatalf("%s: got %d options %v; want %v", state, len(opts), opts, wantIDs)
		}
		got := []string{}
		seen := map[string]bool{}
		for _, o := range opts {
			got = append(got, o.ID)
			if seen[o.ID] {
				t.Fatalf("%s: option id %q repeated", state, o.ID)
			}
			seen[o.ID] = true
		}
		wantSeen := map[string]bool{}
		for _, id := range wantIDs {
			if !seen[id] {
				t.Fatalf("%s: options %v lack the expected option %q", state, got, id)
			}
			wantSeen[id] = true
		}
		if len(wantSeen) != len(seen) {
			t.Fatalf("%s: options %v do not match the expected set %v", state, got, wantIDs)
		}
		if last := opts[len(opts)-1].ID; last != HeldOptionHold {
			t.Fatalf("%s: last option is %q; hold must close every question", state, last)
		}
		// Option labels are unique within the question.
		labels := map[string]bool{}
		for _, o := range opts {
			if labels[o.Label] {
				t.Fatalf("%s: option label %q repeated", state, o.Label)
			}
			labels[o.Label] = true
		}
	}

	// An unknown state and a nil item still get real actions and hold.
	for name, item := range map[string]*HeldItem{
		"unknown state": {State: "bogus"},
		"nil item":      nil,
	} {
		opts := HeldOptions(item)
		set := heldOptionIDSet(t, opts)
		if len(opts) < 2 || !set[HeldOptionHold] {
			t.Fatalf("%s: options %v must carry at least two options including hold", name, opts)
		}
		for id := range set {
			heldOptionIsReal(t, id)
		}
	}
}

func TestHeldQuestionTextNamesTheAutomaton(t *testing.T) {
	text := HeldQuestionText(heldTestItem(HeldReviewHeld, FailureCategoryInfra))
	for _, needle := range []string{
		`"Fix the lane fill pass"`,  // the Automaton's title
		"prd-01",                    // and its id
		`"Implement the lane fill"`, // the task
		"task-01",
		"CI failed on the verify step", // the reason
		"infra",                        // the category
		"member-01",                    // the member
		"pending-01",                   // the pending insert
		"Automaton",                    // named as an Automaton
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("text %q does not name %q", text, needle)
		}
	}
	if strings.Contains(text, "PRD") {
		t.Fatalf("text must never use the word PRD: %q", text)
	}

	// A bare Automaton (no task, no Automation) still reads plainly.
	bare := HeldQuestionText(&HeldItem{State: HeldBlocked, PRDID: "prd-9", PRDTitle: "Tidy the docs", Reason: "waiting on review"})
	if strings.Contains(bare, "PRD") {
		t.Fatalf("bare text must never use the word PRD: %q", bare)
	}
	for _, needle := range []string{`"Tidy the docs"`, "prd-9", "waiting on review"} {
		if !strings.Contains(bare, needle) {
			t.Fatalf("bare text %q does not name %q", bare, needle)
		}
	}
	if t0 := HeldQuestionText(nil); strings.TrimSpace(t0) == "" {
		t.Fatalf("nil item must still render a plain sentence, got %q", t0)
	}

	// Overlong reasons are capped at the question text cap.
	long := &HeldItem{State: HeldReviewHeld, PRDID: "prd-01", PRDTitle: "Long reason", Reason: strings.Repeat("failure detail ", 2000)}
	ltext := HeldQuestionText(long)
	if len(ltext) > questionTextMaxBytes {
		t.Fatalf("overlong text is %d bytes; the cap is %d", len(ltext), questionTextMaxBytes)
	}
	if len(ltext) == 0 {
		t.Fatalf("overlong text was capped to nothing")
	}
}

func TestHeldQuestionRecommendFixedTable(t *testing.T) {
	cases := []struct {
		state    HeldState
		category string
		optID    string
	}{
		{HeldBlocked, "", HeldOptionHold},
		{HeldBlocked, FailureCategoryInfra, HeldOptionHold},
		{HeldBlocked, FailureCategoryContent, HeldOptionHold},
		{HeldBlocked, FailureCategoryScope, HeldOptionHold},
		{HeldBlocked, FailureCategoryPolicy, HeldOptionHold},
		{HeldReviewHeld, FailureCategoryInfra, HeldOptionRetry},
		{HeldReviewHeld, FailureCategoryContent, HeldOptionChangeSpec},
		{HeldReviewHeld, FailureCategoryScope, HeldOptionChangeSpec},
		{HeldReviewHeld, FailureCategoryPolicy, HeldOptionHold},
		{HeldReviewHeld, "", HeldOptionHold},
		{HeldReviewHeld, "mystery", HeldOptionHold}, // unknown category falls back to hold
		{HeldRevisionRequested, FailureCategoryInfra, HeldOptionChangeSpec},
		{HeldRevisionRequested, "", HeldOptionChangeSpec},
		{HeldAwaitingDecision, FailureCategoryContent, HeldOptionHold},
		{HeldAwaitingDecision, "mystery", HeldOptionHold},
		{HeldPendingInsert, "", HeldOptionHold},
		{HeldPendingInsert, FailureCategoryPolicy, HeldOptionHold},
		{HeldState("bogus"), FailureCategoryInfra, HeldOptionHold}, // unknown state falls back to hold
	}
	for _, c := range cases {
		item := heldTestItem(c.state, c.category)
		rec := HeldRecommendation(item)
		if rec == nil || rec.OptionID != c.optID {
			t.Fatalf("%s/%s: recommendation is %v; want option %q", c.state, c.category, rec, c.optID)
		}
		if strings.TrimSpace(rec.Rationale) == "" {
			t.Fatalf("%s/%s: recommendation %q has no rationale", c.state, c.category, c.optID)
		}
		// The recommendation must be one of that state's options.
		found := false
		for _, o := range HeldOptions(item) {
			if o.ID == c.optID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s/%s: recommended option %q is not among the state's options", c.state, c.category, c.optID)
		}
	}
	if rec := HeldRecommendation(nil); rec == nil || rec.OptionID != HeldOptionHold {
		t.Fatalf("nil item must recommend hold; got %v", rec)
	}
}

func TestHeldQuestionBuiltValidateAndValidateHeldSource(t *testing.T) {
	// Every held state builds a question that passes validation, in both
	// the task-linked and the Automaton-only shapes.
	for _, state := range append(HeldStates, HeldState("bogus")) {
		full := BuildHeldQuestion(heldTestItem(state, ""))
		if full.Context != QuestionContextTask || full.TaskID == "" || full.PRDID == "" {
			t.Fatalf("%s: task-shaped question has context %q task %q prd %q", state, full.Context, full.TaskID, full.PRDID)
		}
		if err := ValidateNewQuestion(&full); err != nil {
			t.Fatalf("%s: task-shaped question fails validation: %v", state, err)
		}

		bare := BuildHeldQuestion(&HeldItem{State: state, PRDID: "prd-7"})
		if bare.Context != QuestionContextPrd && bare.PRDID != "prd-7" {
			t.Fatalf("%s: prd-shaped question has context %q prd %q", state, bare.Context, bare.PRDID)
		}
		if err := ValidateNewQuestion(&bare); err != nil {
			t.Fatalf("%s: prd-shaped question fails validation: %v", state, err)
		}

		none := BuildHeldQuestion(&HeldItem{State: state})
		if none.Context != QuestionContextNone {
			t.Fatalf("%s: linkless question has context %q; want none", state, none.Context)
		}
		if err := ValidateNewQuestion(&none); err != nil {
			t.Fatalf("%s: linkless question fails validation: %v", state, err)
		}
	}

	// The held source is accepted by validation and an unknown source is still refused.
	q := BuildHeldQuestion(heldTestItem(HeldReviewHeld, ""))
	if err := ValidateNewQuestion(&q); err != nil {
		t.Fatalf("held source rejected: %v", err)
	}
	bad := BuildHeldQuestion(heldTestItem(HeldReviewHeld, ""))
	bad.Source = "carrier-pigeon"
	if err := ValidateNewQuestion(&bad); err == nil {
		t.Fatalf("unknown source %q must be refused", bad.Source)
	}
}

// heldTrackerIDRe is the shape of the internal tracker ids the
// operator-facing question text must never carry, the same families the
// repo's no-internal-refs gate checks (BL###, F##, B##, S##).
var heldTrackerIDRe = regexp.MustCompile(`\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b`)

func TestHeldOptionsCarryTradeoffs(t *testing.T) {
	// Every option id the daemon implements has a cost-and-risk note, and
	// none of the notes leaks an internal id.
	for id, note := range heldOptionTradeoffs {
		if strings.TrimSpace(note) == "" {
			t.Fatalf("option %q has an empty tradeoffs note", id)
		}
		if len(note) > questionTradeoffsMaxBytes {
			t.Fatalf("option %q note is %d bytes; the cap is %d", id, len(note), questionTradeoffsMaxBytes)
		}
		if heldTrackerIDRe.MatchString(note) {
			t.Fatalf("option %q note carries a tracker id: %q", id, note)
		}
	}
	for id := range heldOptionLabels {
		if strings.TrimSpace(heldOptionTradeoffs[id]) == "" {
			t.Fatalf("option %q has no tradeoffs note", id)
		}
	}
	// Every question the table builds carries a tradeoffs note on every
	// option, and its labels read as words, not as codes.
	for name, item := range map[string]*HeldItem{
		"blocked":           {State: HeldBlocked},
		"review_held":       {State: HeldReviewHeld},
		"revision_held":     {State: HeldRevisionRequested},
		"awaiting_decision": {State: HeldAwaitingDecision},
		"pending_insert":    {State: HeldPendingInsert},
		"unknown_state":     {State: "bogus"},
		"nil_item":          nil,
	} {
		for _, o := range HeldOptions(item) {
			if strings.TrimSpace(o.Tradeoffs) == "" {
				t.Fatalf("%s: option %q has no tradeoffs note", name, o.ID)
			}
			if len(o.Tradeoffs) > questionTradeoffsMaxBytes {
				t.Fatalf("%s: option %q tradeoffs %d bytes; the cap is %d", name, o.ID, len(o.Tradeoffs), questionTradeoffsMaxBytes)
			}
			if o.Label == o.ID || !strings.Contains(o.Label, " ") {
				t.Fatalf("%s: option %q label %q reads as a code, not a sentence", name, o.ID, o.Label)
			}
		}
	}
}

// heldQuestionTextMinLen floors the self-contained text: it must fit
// what is held, why, and who decides, and this is the shortest a held
// question that covers all three can plausibly read.
const heldQuestionTextMinLen = 180

func TestHeldQuestionTextStandsAlone(t *testing.T) {
	// Each of the five states builds text an unfamiliar operator can act
	// on without further context: it states what is held and why, meets
	// the minimum self-contained length, carries the deciding line, names
	// the forced default, and leaks no internal ids.
	for _, state := range HeldStates {
		text := HeldQuestionText(heldTestItem(state, FailureCategoryContent))
		if len(text) < heldQuestionTextMinLen {
			t.Fatalf("%s: text is %d bytes, under the %d-byte self-contained floor", state, len(text), heldQuestionTextMinLen)
		}
		for _, needle := range []string{
			"Why it is held: ",    // the why is in the text
			"You have the call",   // the deciding line
			"the default is hold", // the forced default, on the record
		} {
			if !strings.Contains(text, needle) {
				t.Fatalf("%s: text lacks %q: %q", state, needle, text)
			}
		}
		if heldTrackerIDRe.MatchString(text) {
			t.Fatalf("%s: text carries a tracker id: %q", state, text)
		}
		rec := HeldRecommendation(heldTestItem(state, FailureCategoryContent))
		if heldTrackerIDRe.MatchString(rec.Rationale) {
			t.Fatalf("%s: rationale carries a tracker id: %q", state, rec.Rationale)
		}
		opts := HeldOptions(heldTestItem(state, FailureCategoryContent))
		for _, o := range opts {
			if heldTrackerIDRe.MatchString(o.Label + " " + o.Tradeoffs) {
				t.Fatalf("%s: option %q carries a tracker id", state, o.ID)
			}
		}
	}
}
