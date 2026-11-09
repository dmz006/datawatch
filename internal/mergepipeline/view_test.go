package mergepipeline

import (
	"errors"
	"os"
	"path/filepath"
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
		RenderRecordText(RecordReport{Project: "p"}) + RenderRecordClearedText("p") +
		RenderPostShipDetailText(PostShipDetail{})
	if strings.Contains(all, "PRD") {
		t.Errorf("user-facing text must say Automaton, not PRD:\n%s", all)
	}
}

func TestPostShipDetailFromBody(t *testing.T) {
	body := []byte(`{"hostname":"h","autonomous":{"merge_pipeline":{"file_name":".datawatch/merge-pipeline.yaml","post_ship":{"enabled":true,"hooks":[{"name":"registry_row","builtin":"registry_row","enabled":true},{"name":"notify","command":"pw send","enabled":false}],"registry_file":".datawatch/registry.yaml","registry_row_pattern":"| {project} | {version} |","companion_repository":"acme/widgets","issue_title_template":"parity: {project} {version}","notify_template":"shipped {project} {version}"}}}}`)
	detail, err := PostShipDetailFromBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil {
		t.Fatal("a reported block must be returned")
	}
	d, perr := ParsePostShipDetail(detail)
	if perr != nil {
		t.Fatal(perr)
	}
	if !d.Enabled || len(d.Hooks) != 2 || d.Hooks[0].Builtin != "registry_row" || d.Hooks[1].Command != "pw send" || d.Hooks[1].Enabled {
		t.Errorf("parsed: %+v", d)
	}
	if d.CompanionRepository != "acme/widgets" || d.NotifyTemplate != "shipped {project} {version}" {
		t.Errorf("targets: %+v", d)
	}

	for name, in := range map[string]string{
		"no autonomous block": `{"hostname":"h"}`,
		"no merge_pipeline":   `{"autonomous":{"automation":{"enabled":true}}}`,
		"no post_ship":        `{"autonomous":{"merge_pipeline":{"file_name":"x"}}}`,
		"null post_ship":      `{"autonomous":{"merge_pipeline":{"post_ship":null}}}`,
	} {
		detail, err := PostShipDetailFromBody([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if detail != nil {
			t.Errorf("%s: the read must stay as before: %s", name, detail)
		}
	}
	if _, err := PostShipDetailFromBody([]byte("not json")); err == nil {
		t.Error("an unreadable body must be an error")
	}
}

func TestRenderPostShipDetailText(t *testing.T) {
	d := PostShipDetail{
		Enabled:      true,
		Hooks:        []PostShipHook{{Name: "registry_row", Builtin: "registry_row", Enabled: true}, {Name: "notify", Command: "pw send", Enabled: false}},
		RegistryFile: ".datawatch/registry.yaml", RegistryRowPattern: "| {project} | {version} |",
		CompanionRepository: "acme/widgets", IssueTitleTemplate: "parity: {project} {version}",
		NotifyTemplate: "shipped {project} {version}",
	}
	text := RenderPostShipDetailText(d)
	for _, want := range []string{
		"Post-ship hooks: on",
		"1. registry_row: the built-in registry_row, would run",
		"2. notify: the command pw send, skipped (disabled)",
		"Built-in targets:",
		"registry file: .datawatch/registry.yaml",
		"registry row pattern: | {project} | {version} |",
		"companion repository: acme/widgets",
		"issue title template: parity: {project} {version}",
		"notify template: shipped {project} {version}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	off := RenderPostShipDetailText(PostShipDetail{Hooks: []PostShipHook{{Name: "a", Builtin: "x", Enabled: true}}})
	if !strings.Contains(off, "Post-ship hooks: off") {
		t.Errorf("off state:\n%s", off)
	}
	none := RenderPostShipDetailText(PostShipDetail{})
	if !strings.Contains(none, "(no hook defined)") || strings.Contains(none, "Built-in targets:") {
		t.Errorf("empty block:\n%s", none)
	}
}

func TestRenderViewWithPostShipReplacesTheSummary(t *testing.T) {
	v := View{Project: "demo", Source: "default", Reason: "no file, no record"}
	d := PostShipDetail{Enabled: true, Hooks: []PostShipHook{{Name: "registry_row", Builtin: "registry_row", Enabled: true}}, RegistryFile: "r.yaml"}
	text := RenderViewWithPostShip(v, d)
	if !strings.Contains(text, "Post-ship hooks: on") || !strings.Contains(text, "registry file: r.yaml") {
		t.Errorf("detail block missing:\n%s", text)
	}
	// The one-line summary must be replaced, not added: exactly one post-ship
	// switch line stays in the text.
	if n := strings.Count(text, "Post-ship hooks:"); n != 1 {
		t.Errorf("the post-ship switch line is written %d times:\n%s", n, text)
	}
	text = RenderViewText(v)
	if !strings.Contains(text, "Post-ship hooks: off (none defined)") {
		t.Errorf("the summary line must stay when there is no detail:\n%s", text)
	}
}

// TestRenderViewText_ReleaseApprovalLine checks the release approval line is
// always rendered, next to the merge conflict policy line, with the mode in
// effect for each size; an unset mode shows as (none), never a blank.
func TestRenderViewText_ReleaseApprovalLine(t *testing.T) {
	text := RenderViewText(View{
		Project:                 "demo",
		ConflictPolicy:          "mechanical_and_model",
		ReleaseApprovalInEffect: ReleaseApprovalInEffect{Patch: "auto", Minor: "auto", Major: "ask"},
	})
	const want = "Release approval: patch auto, minor auto, major ask\n"
	if !strings.Contains(text, want) {
		t.Fatalf("lacks %q in\n%s", want, text)
	}
	policy := strings.Index(text, "Merge conflict policy: mechanical_and_model\n")
	if policy < 0 || strings.Index(text, want) != policy+len("Merge conflict policy: mechanical_and_model\n") {
		t.Errorf("release approval line must follow the merge conflict policy line:\n%s", text)
	}

	// No mode reported still renders the line, with the gap shown as (none).
	empty := RenderViewText(View{Project: "demo"})
	if !strings.Contains(empty, "Release approval: patch (none), minor (none), major (none)\n") {
		t.Errorf("an empty view must still show the line:\n%s", empty)
	}
}

// TestRenderViewText_ReleaseApprovalFromJSON checks the mode in effect is read
// from the daemon's JSON body and rendered.
func TestRenderViewText_ReleaseApprovalFromJSON(t *testing.T) {
	v, err := ParseView([]byte(`{"project":"demo","release_approval_in_effect":{"patch":"manual","minor":"ask","major":"auto"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(RenderViewText(v), "Release approval: patch manual, minor ask, major auto\n") {
		t.Errorf("JSON mode in effect not rendered:\n%s", RenderViewText(v))
	}
}

// TestMergePipelineConflictCounter checks the conflict_confirmation block: the
// values in effect, the count from the Counter, remaining never below zero
// (zero when N is 0), enabled only for mode on with N above 0, and a counter
// that cannot be read reported in Error with null counts, never as zero.
func TestMergePipelineConflictCounter(t *testing.T) {
	dir := t.TempDir()
	c, err := NewConflictCounter(dir)
	if err != nil {
		t.Fatalf("NewConflictCounter: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Increment("demo"); err != nil {
			t.Fatalf("Increment: %v", err)
		}
	}

	intp := func(n int) *int { return &n }
	cases := []struct {
		name      string
		mode      string
		n         int
		counter   ConflictCounterStore
		openErr   error
		counted   *int
		remaining *int
		enabled   bool
		wantErr   bool
	}{
		{name: "on with room left", mode: ConflictModeOn, n: 5, counter: c, counted: intp(2), remaining: intp(3), enabled: true},
		{name: "shadow is never enabled", mode: ConflictModeShadow, n: 5, counter: c, counted: intp(2), remaining: intp(3)},
		{name: "on with N zero is off", mode: ConflictModeOn, n: 0, counter: c, counted: intp(2), remaining: intp(0)},
		{name: "count past N floors at zero", mode: ConflictModeOn, n: 1, counter: c, counted: intp(2), remaining: intp(0), enabled: true},
		{name: "counter read failure is an error, not zero", mode: ConflictModeOn, n: 5, counter: failingCounter{}, enabled: true, wantErr: true},
		{name: "open failure is an error, not zero", mode: ConflictModeOn, n: 5, openErr: errors.New("corrupt"), enabled: true, wantErr: true},
		{name: "no counter is an error", mode: ConflictModeOn, n: 5, enabled: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := BuildConflictConfirmation("demo", tc.mode, tc.n, tc.counter, tc.openErr)
			if v.Mode != tc.mode || v.ConfirmFirstN != tc.n {
				t.Errorf("mode/n = %q/%d, want %q/%d", v.Mode, v.ConfirmFirstN, tc.mode, tc.n)
			}
			if v.Enabled != tc.enabled {
				t.Errorf("enabled = %v, want %v", v.Enabled, tc.enabled)
			}
			if tc.wantErr {
				if v.Error == "" || v.Counted != nil || v.Remaining != nil {
					t.Errorf("want error with null counts, got %+v", v)
				}
				return
			}
			if v.Error != "" {
				t.Errorf("unexpected error %q", v.Error)
			}
			if v.Counted == nil || *v.Counted != *tc.counted {
				t.Errorf("counted = %v, want %d", v.Counted, *tc.counted)
			}
			if v.Remaining == nil || *v.Remaining != *tc.remaining {
				t.Errorf("remaining = %v, want %d", v.Remaining, *tc.remaining)
			}
		})
	}

	// A corrupt counter file is reported by NewConflictCounter, which the
	// server turns into the error field.
	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bad, "merge-pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "merge-pipeline", "conflict-counter.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewConflictCounter(bad); err == nil {
		t.Fatal("corrupt counter file: want an error from NewConflictCounter")
	}
}

// failingCounter is a ConflictCounterStore whose reads fail.
type failingCounter struct{}

func (failingCounter) Count(string) (int, error)     { return 0, errors.New("disk gone") }
func (failingCounter) Increment(string) (int, error) { return 0, errors.New("disk gone") }
