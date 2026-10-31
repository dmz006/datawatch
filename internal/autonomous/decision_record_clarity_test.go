package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func clarityPolicy() DecisionClarityPolicy {
	return DecisionClarityPolicy{
		Mode:             QualityModeWarn,
		MinTextChars:     DefaultQualityMinTextChars,
		RequireTradeoffs: true,
		RequireLabel:     true,
		TrackerIDPattern: DefaultQualityTrackerIDPattern,
	}
}

func clarityRecord() DecisionRecord {
	r := validDecisionRecord()
	r.ID = "dr-0123456789abcdef0123"
	r.Label = "decision"
	return r
}

func runClarity(t *testing.T, r DecisionRecord, p DecisionClarityPolicy) []QualityFinding {
	t.Helper()
	fs, err := CheckDecisionClarity(r, BuildQuestionFromDecision(r), p)
	if err != nil {
		t.Fatalf("CheckDecisionClarity: %v", err)
	}
	return fs
}

func TestDecisionClarity_CleanRecordHasNoFindings(t *testing.T) {
	if fs := runClarity(t, clarityRecord(), clarityPolicy()); len(fs) != 0 {
		t.Fatalf("clean record has findings %v", qualityCodes(fs))
	}
}

func TestDecisionClarity_TextTooShort(t *testing.T) {
	r := clarityRecord()
	r.Title, r.Situation, r.Deciding = "Pick", "Now.", "Which."
	fs := runClarity(t, r, clarityPolicy())
	f := findQuality(fs, QualityCodeTextTooShort)
	if f == nil {
		t.Fatalf("want text_too_short, got %v", qualityCodes(fs))
	}
	if !strings.Contains(f.Message, "at least 60") {
		t.Errorf("message does not name the minimum: %q", f.Message)
	}
}

func TestDecisionClarity_MissingTradeoffOnOneOption(t *testing.T) {
	r := clarityRecord()
	r.Options[1].Tradeoff = "  "
	fs := runClarity(t, r, clarityPolicy())
	f := findQuality(fs, QualityCodeNoTradeoffs)
	if f == nil {
		t.Fatalf("want no_tradeoffs, got %v", qualityCodes(fs))
	}
	if !strings.Contains(f.Message, "files") || strings.Contains(f.Message, "sqlite") {
		t.Errorf("message should name only the option without a trade-off: %q", f.Message)
	}
}

func TestDecisionClarity_TrackerIdentifierInTitleOptionAndTradeoff(t *testing.T) {
	for name, mutate := range map[string]func(*DecisionRecord){
		"title":    func(r *DecisionRecord) { r.Title = "Pick the storage engine for BL123" },
		"option":   func(r *DecisionRecord) { r.Options[0].Label = "Use SQLite as in GH42" },
		"tradeoff": func(r *DecisionRecord) { r.Options[1].Tradeoff = "slow queries, see SEC123" },
	} {
		t.Run(name, func(t *testing.T) {
			r := clarityRecord()
			mutate(&r)
			fs := runClarity(t, r, clarityPolicy())
			f := findQuality(fs, QualityCodeTrackerID)
			if f == nil {
				t.Fatalf("want tracker_id, got %v", qualityCodes(fs))
			}
			if findQuality(fs, QualityCodeLabelNotHuman) != nil {
				t.Errorf("a clean label was flagged: %v", qualityCodes(fs))
			}
		})
	}
}

func TestDecisionClarity_LabelNotHuman(t *testing.T) {
	for name, label := range map[string]string{
		"record id":   "dr-0123456789abcdef0123",
		"question id": "q-0123456789abcdef0a1b",
		"upper id":    "DR-0123",
		"empty":       "",
		"blank":       "   ",
		"no letters":  "1234",
		"punctuation": "---",
		"too short":   "ab",
		"tracker":     "BL123",
	} {
		t.Run(name, func(t *testing.T) {
			r := clarityRecord()
			r.Label = label
			fs := runClarity(t, r, clarityPolicy())
			f := findQuality(fs, QualityCodeLabelNotHuman)
			if f == nil {
				t.Fatalf("label %q: want label_not_human, got %v", label, qualityCodes(fs))
			}
			if !strings.HasSuffix(f.Message, ".") {
				t.Errorf("message is not a sentence: %q", f.Message)
			}
		})
	}
	for _, label := range []string{"decision", "design review", "approval", "Q&A", "go-live"} {
		r := clarityRecord()
		r.Label = label
		if f := findQuality(runClarity(t, r, clarityPolicy()), QualityCodeLabelNotHuman); f != nil {
			t.Errorf("label %q flagged: %s", label, f.Message)
		}
	}
}

func TestDecisionClarity_EachCheckSwitchedOffByItsKey(t *testing.T) {
	r := clarityRecord()
	r.Label = "dr-0123456789abcdef0123"
	r.Options[0].Tradeoff = ""
	r.Title = "Pick the storage engine for BL123"

	all := runClarity(t, r, clarityPolicy())
	for _, code := range []string{QualityCodeNoTradeoffs, QualityCodeLabelNotHuman, QualityCodeTrackerID} {
		if findQuality(all, code) == nil {
			t.Fatalf("setup: want %s, got %v", code, qualityCodes(all))
		}
	}

	p := clarityPolicy()
	p.RequireTradeoffs = false
	fs := runClarity(t, r, p)
	if findQuality(fs, QualityCodeNoTradeoffs) != nil || findQuality(fs, QualityCodeLabelNotHuman) == nil || findQuality(fs, QualityCodeTrackerID) == nil {
		t.Errorf("require_tradeoffs off: got %v", qualityCodes(fs))
	}

	p = clarityPolicy()
	p.RequireLabel = false
	fs = runClarity(t, r, p)
	if findQuality(fs, QualityCodeLabelNotHuman) != nil || findQuality(fs, QualityCodeNoTradeoffs) == nil || findQuality(fs, QualityCodeTrackerID) == nil {
		t.Errorf("require_label off: got %v", qualityCodes(fs))
	}

	p = clarityPolicy()
	p.TrackerIDPattern = ""
	fs = runClarity(t, r, p)
	if findQuality(fs, QualityCodeTrackerID) != nil {
		t.Errorf("empty pattern still reports tracker_id: %v", qualityCodes(fs))
	}
}

func TestDecisionClarity_ModeOffReturnsNothing(t *testing.T) {
	r := clarityRecord()
	r.Label = ""
	r.Options[0].Tradeoff = ""
	r.Title, r.Situation, r.Deciding = "BL123", "x", "y"
	p := clarityPolicy()
	p.Mode = QualityModeOff
	if fs := runClarity(t, r, p); len(fs) != 0 {
		t.Fatalf("mode off returned %v", qualityCodes(fs))
	}
}

func TestDecisionClarity_WarnAndRefuseGiveSameFindings(t *testing.T) {
	r := clarityRecord()
	r.Label = "dr-0123456789abcdef0123"
	r.Options[0].Tradeoff = ""
	warn := clarityPolicy()
	refuse := clarityPolicy()
	refuse.Mode = QualityModeRefuse
	a, b := runClarity(t, r, warn), runClarity(t, r, refuse)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("warn %v, refuse %v", qualityCodes(a), qualityCodes(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("finding %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestDecisionClarity_InvalidModeAndPatternAreErrors(t *testing.T) {
	r := clarityRecord()
	p := clarityPolicy()
	p.Mode = "strict"
	if _, err := CheckDecisionClarity(r, BuildQuestionFromDecision(r), p); err == nil {
		t.Error("invalid mode accepted")
	}
	p = clarityPolicy()
	p.Mode = ""
	if _, err := CheckDecisionClarity(r, BuildQuestionFromDecision(r), p); err == nil {
		t.Error("empty mode accepted")
	}
	p = clarityPolicy()
	p.TrackerIDPattern = `([A-Z`
	if _, err := CheckDecisionClarity(r, BuildQuestionFromDecision(r), p); err == nil {
		t.Error("pattern that does not compile accepted")
	}
	// A broken pattern is an error even when the mode is off, like the
	// question checker.
	p.Mode = QualityModeOff
	if _, err := CheckDecisionClarity(r, BuildQuestionFromDecision(r), p); err == nil {
		t.Error("pattern that does not compile accepted with mode off")
	}
}

func TestDecisionClarity_CustomMinLengthAndPatternChangeResult(t *testing.T) {
	r := clarityRecord()
	p := clarityPolicy()
	if findQuality(runClarity(t, r, p), QualityCodeTextTooShort) != nil {
		t.Fatal("default policy flags the clean record")
	}
	p.MinTextChars = 10000
	if findQuality(runClarity(t, r, p), QualityCodeTextTooShort) == nil {
		t.Error("a large minimum did not flag the text")
	}
	p.MinTextChars = 0
	r2 := clarityRecord()
	r2.Title, r2.Situation, r2.Deciding = "a", "b", "c"
	if findQuality(runClarity(t, r2, p), QualityCodeTextTooShort) != nil {
		t.Error("a minimum of 0 still flags short text")
	}

	// A custom pattern: flags "TICKET-7" in the title, and no longer flags BL123.
	p = clarityPolicy()
	p.TrackerIDPattern = `TICKET-[0-9]+`
	r3 := clarityRecord()
	r3.Title = "Pick the storage engine for TICKET-7"
	if findQuality(runClarity(t, r3, p), QualityCodeTrackerID) == nil {
		t.Error("custom pattern did not match")
	}
	r3.Title = "Pick the storage engine for BL123"
	if findQuality(runClarity(t, r3, p), QualityCodeTrackerID) != nil {
		t.Error("custom pattern still matched the default shape")
	}
	// The label check uses the same pattern.
	r3.Label = "TICKET-7"
	if findQuality(runClarity(t, r3, p), QualityCodeLabelNotHuman) == nil {
		t.Error("label matching the custom pattern was not flagged")
	}
}

func TestDecisionClarity_PolicyFromConfig(t *testing.T) {
	got := DecisionClarityPolicyFromConfig(config.AutonomousDecisionsConfig{})
	if got != clarityPolicy() {
		t.Fatalf("default policy = %+v, want %+v", got, clarityPolicy())
	}
	mode, minChars, off := "refuse", 0, false
	pattern := `X[0-9]+`
	got = DecisionClarityPolicyFromConfig(config.AutonomousDecisionsConfig{
		ClarityMode:             &mode,
		ClarityMinTextChars:     &minChars,
		ClarityRequireTradeoffs: &off,
		ClarityRequireLabel:     &off,
		ClarityTrackerIDPattern: &pattern,
	})
	want := DecisionClarityPolicy{Mode: "refuse", MinTextChars: 0, TrackerIDPattern: `X[0-9]+`}
	if got != want {
		t.Fatalf("configured policy = %+v, want %+v", got, want)
	}
}
