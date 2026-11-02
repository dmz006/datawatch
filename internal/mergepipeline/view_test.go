package mergepipeline

import (
	"strings"
	"testing"
	"time"
)

func TestParseView_RejectsBadText(t *testing.T) {
	for _, in := range []string{"", "not json", "[1,2]", `{"project": 5}`} {
		if _, err := ParseView([]byte(in)); err == nil {
			t.Errorf("ParseView(%q) must fail", in)
		}
	}
	if _, err := ParseValidation([]byte("nope")); err == nil {
		t.Error("ParseValidation must fail on bad text")
	}
	if _, err := ParseRecordReport([]byte(`{"updated_at": "yesterday"}`)); err == nil {
		t.Error("ParseRecordReport must fail on a bad time")
	}
}

func TestRenderViewText_SourcesAndSettings(t *testing.T) {
	def := &Definition{Steps: []Step{
		{Name: "tests", Kind: KindTest, Command: "make test", Dir: "sub", RequiresMakeTarget: "test", Timeout: 90 * time.Second},
		{Name: "ship", Kind: KindShip, Action: ActionPushRelease, OnFailure: OnFailureContinue},
	}}
	cases := map[string]string{
		"file":    "Source: the pipeline file in the repository\n",
		"record":  "Source: the pipeline record kept by the daemon (set 2026-11-01T10:00:00Z)\n",
		"default": "Source: the built-in pipeline\n",
		"":        "Source: none chosen\n",
		"odd":     "Source: odd\n",
	}
	for src, want := range cases {
		text := RenderViewText(View{Project: "demo", Source: src, RecordUpdatedAt: "2026-11-01T10:00:00Z", Definition: def,
			Settings: ViewSettings{FileName: "p.yaml", SourceOrder: []string{"file", "default"}, FallbackOnBadFile: "record"}})
		if !strings.Contains(text, want) {
			t.Errorf("source %q: lacks %q in\n%s", src, want, text)
		}
	}
	text := RenderViewText(View{Project: "demo", Source: "default", Definition: def,
		Settings: ViewSettings{FileName: "p.yaml", SourceOrder: []string{"file", "default"}, FallbackOnBadFile: "record"}})
	for _, want := range []string{
		"Steps (2):\n",
		"  1. tests [test] make test (in sub, only if the Makefile has test, limit 1m30s)\n",
		"  2. ship [ship] built-in push_release (on failure continue)\n",
		"Pipeline file name: p.yaml\n",
		"Order sources are tried in: file, default\n",
		"Files that hold the version: (none)\n",
		"Project overrides: none\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q in\n%s", want, text)
		}
	}
}

func TestRenderViewText_NoPipelineAndDegradedPolicy(t *testing.T) {
	text := RenderViewText(View{
		Project: "demo", FileError: "line 3: bad", Reason: "the file is unusable",
		ConflictPolicy: "model_assisted", ConflictPolicyInEffect: "stop_and_ask", ConflictDegradedReason: "no model is available",
		ConflictModel: "big", ConflictModelInEffect: "",
	})
	for _, want := range []string{
		"Problem with the repository file: line 3: bad\n",
		"No pipeline could be chosen, so a merge run would not start.\n",
		"Merge conflict policy: model_assisted (acting as stop_and_ask: no model is available)\n",
		"Conflict model: big (using (none))\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q in\n%s", want, text)
		}
	}
	if !strings.HasPrefix(RenderViewText(View{}), "Merge pipeline for (no project)\n") {
		t.Error("an empty view must still render")
	}
}

func TestRenderViewText_OverridesAreSortedAndPlain(t *testing.T) {
	v, err := ParseView([]byte(`{"project":"p","overrides":{"version_files":["a","b"],"file_name":"x.yaml"}}`))
	if err != nil {
		t.Fatal(err)
	}
	text := RenderViewText(v)
	i, j := strings.Index(text, "file_name = x.yaml"), strings.Index(text, "version_files = a, b")
	if i < 0 || j < 0 || i > j {
		t.Errorf("overrides must be sorted and plain:\n%s", text)
	}
}

func TestRenderValidationText(t *testing.T) {
	ok := RenderValidationText(ValidationReport{Valid: true, Source: "body", Definition: &Definition{Steps: []Step{{Name: "a", Kind: KindTest, Command: "x"}}}})
	if !strings.HasPrefix(ok, "The pipeline you gave is valid.\n") || !strings.Contains(ok, "1. a [test] x") {
		t.Errorf("valid: %s", ok)
	}
	one := RenderValidationText(ReportOf(false, "file", "p.yaml", "boom"))
	if !strings.Contains(one, "The repository pipeline file p.yaml is not valid (1 problem):\n  - boom\n") {
		t.Errorf("one problem: %s", one)
	}
	two := RenderValidationText(ReportOf(false, "body", "", "a", "b"))
	if !strings.Contains(two, "is not valid (2 problems):") {
		t.Errorf("two problems: %s", two)
	}
}

// ReportOf builds a validation report for the tests.
func ReportOf(valid bool, source, file string, problems ...string) ValidationReport {
	return ValidationReport{Valid: valid, Source: source, File: file, Errors: problems}
}

func TestRenderRecordTexts(t *testing.T) {
	r := RenderRecordText(RecordReport{Project: "demo", Definition: Definition{Steps: []Step{{Name: "a", Kind: KindTest, Command: "x"}, {Name: "b", Kind: KindLint, Command: "y"}}}})
	if !strings.HasPrefix(r, "Saved the merge pipeline record for demo (2 steps).\n") {
		t.Errorf("record: %s", r)
	}
	if got := RenderRecordClearedText("demo"); !strings.Contains(got, "Removed the merge pipeline record for demo.") || strings.Contains(got, "PRD") {
		t.Errorf("cleared: %s", got)
	}
}

func TestRenderedTextNeverSaysPRD(t *testing.T) {
	all := RenderViewText(View{Project: "p", Source: "record", Definition: &Definition{Steps: []Step{{Name: "a", Kind: KindTest, Command: "x"}}}}) +
		RenderValidationText(ReportOf(false, "body", "", "x")) +
		RenderRecordText(RecordReport{Project: "p"}) + RenderRecordClearedText("p")
	if strings.Contains(all, "PRD") {
		t.Errorf("user-facing text must say Automaton, not PRD:\n%s", all)
	}
}
