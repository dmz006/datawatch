package autonomous

import (
	"strings"
	"testing"
)

func goodQualityQuestion() Question {
	return Question{
		Text: "Should the nightly import run at the start of the maintenance window or at the end of it, given the backup also runs then?",
		Options: []QuestionOption{
			{ID: "early", Label: "Run at the start", Tradeoffs: "Finishes sooner but competes with the backup for disk."},
			{ID: "late", Label: "Run at the end", Tradeoffs: "Avoids the backup but risks overrunning the window."},
		},
	}
}

func qualityCodes(fs []QualityFinding) []string {
	var c []string
	for _, f := range fs {
		c = append(c, f.Code)
	}
	return c
}

func findQuality(fs []QualityFinding, code string) *QualityFinding {
	for i := range fs {
		if fs[i].Code == code {
			return &fs[i]
		}
	}
	return nil
}

func TestQuestionQuality_CheckNoTradeoffs(t *testing.T) {
	q := goodQualityQuestion()
	q.Options[0].Tradeoffs = ""
	q.Options = append(q.Options, QuestionOption{ID: "skip", Label: "Skip it", Tradeoffs: "   "})
	fs, err := CheckQuestionQuality(q, NormalizeQualityPolicy(QualityPolicy{}))
	if err != nil {
		t.Fatal(err)
	}
	f := findQuality(fs, QualityCodeNoTradeoffs)
	if f == nil {
		t.Fatalf("want no_tradeoffs, got %v", qualityCodes(fs))
	}
	if !strings.Contains(f.Message, "early") || !strings.Contains(f.Message, "skip") || strings.Contains(f.Message, "late") {
		t.Errorf("message should name options early and skip only: %q", f.Message)
	}
}

func TestQuestionQuality_CheckTextTooShort(t *testing.T) {
	q := goodQualityQuestion()
	q.Text = "  Which one?  "
	fs, err := CheckQuestionQuality(q, NormalizeQualityPolicy(QualityPolicy{}))
	if err != nil {
		t.Fatal(err)
	}
	if findQuality(fs, QualityCodeTextTooShort) == nil {
		t.Fatalf("want text_too_short, got %v", qualityCodes(fs))
	}

	// Runes, not bytes: 5 multi-byte characters against a minimum of 5 passes.
	q.Text = "éééér "
	fs, err = CheckQuestionQuality(q, QualityPolicy{Mode: QualityModeWarn, MinTextChars: 5})
	if err != nil {
		t.Fatal(err)
	}
	if findQuality(fs, QualityCodeTextTooShort) != nil {
		t.Errorf("5 runes should meet a minimum of 5: %v", qualityCodes(fs))
	}
}

func TestQuestionQuality_CheckTrackerID(t *testing.T) {
	id := "ZQ" + "417"
	p := NormalizeQualityPolicy(QualityPolicy{})

	cases := map[string]func(q *Question){
		"text":     func(q *Question) { q.Text += " See " + id + " for background." },
		"label":    func(q *Question) { q.Options[0].Label = "Do " + id },
		"tradeoff": func(q *Question) { q.Options[1].Tradeoffs += " Matches " + id + "." },
	}
	for name, mutate := range cases {
		q := goodQualityQuestion()
		mutate(&q)
		fs, err := CheckQuestionQuality(q, p)
		if err != nil {
			t.Fatal(err)
		}
		f := findQuality(fs, QualityCodeTrackerID)
		if f == nil {
			t.Fatalf("%s: want tracker_id, got %v", name, qualityCodes(fs))
		}
		if !strings.Contains(f.Message, id) {
			t.Errorf("%s: message should quote the match: %q", name, f.Message)
		}
	}

	// A custom pattern replaces the default.
	q := goodQualityQuestion()
	q.Text += " ticket #77 applies."
	fs, err := CheckQuestionQuality(q, QualityPolicy{Mode: QualityModeWarn, MinTextChars: 10, TrackerIDPattern: `#[0-9]+`})
	if err != nil {
		t.Fatal(err)
	}
	if f := findQuality(fs, QualityCodeTrackerID); f == nil || !strings.Contains(f.Message, "#77") {
		t.Errorf("custom pattern not applied: %v", fs)
	}
}

func TestQuestionQuality_CheckClean(t *testing.T) {
	fs, err := CheckQuestionQuality(goodQualityQuestion(), NormalizeQualityPolicy(QualityPolicy{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("good question should have no findings, got %v", fs)
	}
}

func TestQuestionQuality_CheckModes(t *testing.T) {
	bad := Question{Text: "Which?", Options: []QuestionOption{{ID: "a", Label: "A"}}}

	off, err := CheckQuestionQuality(bad, QualityPolicy{Mode: QualityModeOff, MinTextChars: 60})
	if err != nil || len(off) != 0 {
		t.Errorf("off: want nothing, got %v, %v", off, err)
	}
	warn, err := CheckQuestionQuality(bad, NormalizeQualityPolicy(QualityPolicy{Mode: QualityModeWarn}))
	if err != nil || len(warn) == 0 {
		t.Fatalf("warn: want findings, got %v, %v", warn, err)
	}
	refuse, err := CheckQuestionQuality(bad, NormalizeQualityPolicy(QualityPolicy{Mode: QualityModeRefuse}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(qualityCodes(warn), ",") != strings.Join(qualityCodes(refuse), ",") {
		t.Errorf("warn and refuse should match: %v vs %v", warn, refuse)
	}

	if _, err := CheckQuestionQuality(bad, QualityPolicy{Mode: "loud"}); err == nil {
		t.Error("invalid mode should error")
	}
	if _, err := CheckQuestionQuality(bad, QualityPolicy{Mode: QualityModeWarn, TrackerIDPattern: "("}); err == nil {
		t.Error("bad pattern should error")
	}
}

func TestQuestionQuality_NormalizeDefaults(t *testing.T) {
	p := NormalizeQualityPolicy(QualityPolicy{})
	if p.Mode != DefaultQualityMode || p.MinTextChars != DefaultQualityMinTextChars || p.TrackerIDPattern != DefaultQualityTrackerIDPattern {
		t.Errorf("defaults not applied: %+v", p)
	}
	p = NormalizeQualityPolicy(QualityPolicy{Mode: QualityModeRefuse, MinTextChars: 5, TrackerIDPattern: "x"})
	if p.Mode != QualityModeRefuse || p.MinTextChars != 5 || p.TrackerIDPattern != "x" {
		t.Errorf("explicit values overwritten: %+v", p)
	}
}
