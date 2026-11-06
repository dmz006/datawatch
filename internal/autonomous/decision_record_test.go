package autonomous

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// validDecisionRecord returns a record that passes validation.
func validDecisionRecord() DecisionRecord {
	return DecisionRecord{
		Title:     "Pick the storage engine",
		Situation: "The service keeps its state in memory and loses it on restart.",
		Deciding:  "Which storage engine the service uses from now on.",
		Options: []DecisionOption{
			{ID: "sqlite", Label: "Use SQLite", Tradeoff: "simple, one writer at a time"},
			{ID: "files", Label: "Keep append-only files", Tradeoff: "no new dependency, slow queries"},
		},
		Recommendation: &DecisionRecommendation{OptionID: "sqlite", Reason: "queries stay fast"},
		Default:        "hold",
		Links:          DecisionLinks{Doc: "docs/plans/storage.md", Automaton: "ee660001", RegistryRow: "row-12"},
	}
}

func TestDecisionRecord_ValidRecordPasses(t *testing.T) {
	r := validDecisionRecord()
	if err := ValidateNewDecisionRecord(&r); err != nil {
		t.Fatalf("valid record refused: %v", err)
	}
	if r.Label != DecisionDefaultLabel {
		t.Fatalf("label = %q, want the default %q", r.Label, DecisionDefaultLabel)
	}
	if err := ValidateNewDecisionRecord(nil); !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Fatalf("nil record: err = %v, want ErrInvalidDecisionRecord", err)
	}
}

func TestDecisionRecord_EveryProblemIsNamed(t *testing.T) {
	r := DecisionRecord{Options: []DecisionOption{{ID: "a", Label: "A"}}}
	err := ValidateNewDecisionRecord(&r)
	if !errors.Is(err, ErrInvalidDecisionRecord) {
		t.Fatalf("err = %v, want ErrInvalidDecisionRecord", err)
	}
	for _, want := range []string{"title is required", "situation is required", "deciding is required",
		"at least 2 options", "default is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestDecisionRecord_OptionRules(t *testing.T) {
	r := validDecisionRecord()
	r.Options = []DecisionOption{{ID: "a", Label: "A"}, {ID: "a", Label: ""}, {ID: "", Label: "C"}}
	r.Recommendation = nil
	r.Default = "hold"
	err := ValidateNewDecisionRecord(&r)
	if err == nil {
		t.Fatal("bad options accepted")
	}
	for _, want := range []string{`option id "a" is repeated`, "option 2 has an empty label", "option 3 has an empty id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}

	r = validDecisionRecord()
	r.Options = nil
	for i := 0; i < 21; i++ {
		id := string(rune('a' + i))
		r.Options = append(r.Options, DecisionOption{ID: id, Label: "option " + id})
	}
	r.Recommendation = nil
	if err := ValidateNewDecisionRecord(&r); err == nil || !strings.Contains(err.Error(), "21 options exceed the cap of 20") {
		t.Fatalf("21 options: err = %v", err)
	}
	r.Options = r.Options[:20]
	if err := ValidateNewDecisionRecord(&r); err != nil {
		t.Fatalf("20 options refused: %v", err)
	}
}

func TestDecisionRecord_RecommendationAndDefaultRules(t *testing.T) {
	r := validDecisionRecord()
	r.Recommendation = &DecisionRecommendation{OptionID: "nope", Reason: ""}
	r.Default = "nope"
	err := ValidateNewDecisionRecord(&r)
	if err == nil {
		t.Fatal("bad recommendation and default accepted")
	}
	for _, want := range []string{`recommendation names option "nope"`, "recommendation needs a reason", `default "nope" is neither an option nor hold`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	r = validDecisionRecord()
	r.Default = "files"
	if err := ValidateNewDecisionRecord(&r); err != nil {
		t.Fatalf("option id default refused: %v", err)
	}
}

func TestDecisionRecord_LinkDocPath(t *testing.T) {
	cases := []struct {
		doc string
		ok  bool
	}{
		{"", true},
		{"docs/a.md", true},
		{"a.md", true},
		{"docs/..hidden/a.md", true},
		{"/etc/passwd", false},
		{"../a.md", false},
		{"docs/../../a.md", false},
		{"docs\\..\\a.md", false},
		{"\\share\\a.md", false},
		{"docs/a\x00.md", false},
	}
	for _, c := range cases {
		r := validDecisionRecord()
		r.Links.Doc = c.doc
		err := ValidateNewDecisionRecord(&r)
		if c.ok && err != nil {
			t.Errorf("doc %q refused: %v", c.doc, err)
		}
		if !c.ok && (err == nil || !strings.Contains(err.Error(), "link doc")) {
			t.Errorf("doc %q: err = %v, want a link doc problem", c.doc, err)
		}
	}
	// Automaton and registry row are plain strings: anything goes.
	r := validDecisionRecord()
	r.Links = DecisionLinks{Automaton: "../../weird id", RegistryRow: "/x/y"}
	if err := ValidateNewDecisionRecord(&r); err != nil {
		t.Fatalf("plain-string links refused: %v", err)
	}
}

func TestDecisionRecord_SizeCaps(t *testing.T) {
	r := validDecisionRecord()
	r.Label = strings.Repeat("x", questionLabelMaxChars+1)
	r.Title = strings.Repeat("t", decisionTitleMaxBytes+1)
	r.Situation = strings.Repeat("s", questionTextMaxBytes)
	r.Options[0].Label = strings.Repeat("l", questionOptionLabelMaxBytes+1)
	r.Options[0].Tradeoff = strings.Repeat("o", questionTradeoffsMaxBytes+1)
	r.Recommendation.Reason = strings.Repeat("r", questionRationaleMaxBytes+1)
	err := ValidateNewDecisionRecord(&r)
	if err == nil {
		t.Fatal("oversized fields accepted")
	}
	for _, want := range []string{"label is 65 characters", "title is", "together; the cap is", "option 1 label is",
		"option 1 tradeoff is", "recommendation reason is"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	r = validDecisionRecord()
	r.Label = "has\ncontrol"
	if err := ValidateNewDecisionRecord(&r); err == nil || !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("control label: err = %v", err)
	}
}

func TestDecisionRecord_SourceRules(t *testing.T) {
	r := validDecisionRecord()
	r.Source = "pigeon"
	if err := ValidateNewDecisionRecord(&r); err == nil || !strings.Contains(err.Error(), `source is "pigeon"`) {
		t.Fatalf("bad source: err = %v", err)
	}
	r.Source = DecisionSourceFile
	if err := ValidateNewDecisionRecord(&r); err == nil || !strings.Contains(err.Error(), "requires source_ref") {
		t.Fatalf("file without ref: err = %v", err)
	}
	r.Source, r.SourceRef = "", "x.md"
	if err := ValidateNewDecisionRecord(&r); err == nil || !strings.Contains(err.Error(), "source_ref requires source") {
		t.Fatalf("ref without source: err = %v", err)
	}
	r.Source, r.SourceRef = DecisionSourceFile, "x.md"
	if err := ValidateNewDecisionRecord(&r); err != nil {
		t.Fatalf("file source refused: %v", err)
	}
}

func TestDecisionRecord_ContentHash(t *testing.T) {
	a, b := validDecisionRecord(), validDecisionRecord()
	if a.ContentHash() != b.ContentHash() {
		t.Fatal("same content gave different hashes")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a.ContentHash()) {
		t.Fatalf("hash %q is not 64 lowercase hex", a.ContentHash())
	}
	// Bookkeeping is not content.
	b.ID, b.State, b.QuestionID, b.Source = "dr-x", DecisionRecordAnswered, "q-1", DecisionSourceMCP
	b.Answer = &DecisionAnswer{AnswerID: "a-1"}
	if a.ContentHash() != b.ContentHash() {
		t.Fatal("bookkeeping fields changed the hash")
	}
	// An empty label is the default label; empty and nil options agree.
	b = validDecisionRecord()
	b.Label = DecisionDefaultLabel
	if a.ContentHash() != b.ContentHash() {
		t.Fatal("empty label and the default label hash differently")
	}
	if (DecisionRecord{}).ContentHash() != (DecisionRecord{Options: []DecisionOption{}}).ContentHash() {
		t.Fatal("nil and empty options hash differently")
	}
	mutations := map[string]func(*DecisionRecord){
		"label":          func(r *DecisionRecord) { r.Label = "other" },
		"title":          func(r *DecisionRecord) { r.Title += "!" },
		"situation":      func(r *DecisionRecord) { r.Situation += "!" },
		"deciding":       func(r *DecisionRecord) { r.Deciding += "!" },
		"option label":   func(r *DecisionRecord) { r.Options[0].Label += "!" },
		"option order":   func(r *DecisionRecord) { r.Options[0], r.Options[1] = r.Options[1], r.Options[0] },
		"recommendation": func(r *DecisionRecord) { r.Recommendation.Reason += "!" },
		"no recommend":   func(r *DecisionRecord) { r.Recommendation = nil },
		"default":        func(r *DecisionRecord) { r.Default = "sqlite" },
		"link doc":       func(r *DecisionRecord) { r.Links.Doc = "docs/other.md" },
		"link automaton": func(r *DecisionRecord) { r.Links.Automaton = "other" },
		"link row":       func(r *DecisionRecord) { r.Links.RegistryRow = "other" },
	}
	for name, mutate := range mutations {
		c := validDecisionRecord()
		mutate(&c)
		if c.ContentHash() == a.ContentHash() {
			t.Errorf("changing %s did not change the hash", name)
		}
	}
	// Moving text between neighbouring fields must not collide.
	x := DecisionRecord{Title: "ab", Situation: "c"}
	y := DecisionRecord{Title: "a", Situation: "bc"}
	if x.ContentHash() == y.ContentHash() {
		t.Fatal("field boundaries are ambiguous in the hash")
	}
}

func TestDecisionRecord_NewIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^dr-[0-9a-f]{20}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := NewDecisionRecordID()
		if !re.MatchString(id) {
			t.Fatalf("id %q does not match dr-<20 hex>", id)
		}
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}
}

func TestDecisionRecord_NewIDInjectable(t *testing.T) {
	old := decisionIDReader
	defer func() { decisionIDReader = old }()

	decisionIDReader = strings.NewReader("0123456789abcdefghij")
	want := "dr-30313233343536373839"
	if got := NewDecisionRecordID(); got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
	// A failing source (too short here) still yields a well-formed id.
	decisionIDReader = strings.NewReader("short")
	if got := NewDecisionRecordID(); !regexp.MustCompile(`^dr-[0-9a-f]{20}$`).MatchString(got) {
		t.Fatalf("fallback id %q is malformed", got)
	}
	if _, err := newDecisionRecordIDFrom(strings.NewReader("")); err == nil {
		t.Fatal("empty reader gave no error")
	}
}

func TestDecisionRecord_QuestionTextShape(t *testing.T) {
	r := validDecisionRecord()
	want := r.Title + "\n\n" + r.Situation + "\n\nDeciding: " + r.Deciding
	if got := DecisionQuestionText(r); got != want {
		t.Fatalf("question text = %q, want %q", got, want)
	}
}
