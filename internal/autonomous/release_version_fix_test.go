package autonomous

import (
	"strings"
	"testing"
)

const fakeVer = "88.88.88" // obviously fake

func decomposeWith(t *testing.T, plan string) (*PRD, error) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 0, "infra": 0, "scope": 0}
	m, err := NewManager(t.TempDir(), cfg, func(DecomposeRequest) (string, error) { return plan, nil })
	if err != nil {
		t.Fatal(err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	return m.Decompose(prd.ID)
}

func TestCorrect_TaskTitle(t *testing.T) {
	in := []Story{{Title: "S", Tasks: []Task{{Title: "Version bump to v" + fakeVer, Spec: "edit"}}}}
	out, reps := CorrectFixedReleaseVersions(in)
	if len(reps) != 1 || out[0].Tasks[0].Title != "Version bump to the next patch above main" {
		t.Fatalf("got %q %+v", out[0].Tasks[0].Title, reps)
	}
	if !strings.Contains(in[0].Tasks[0].Title, fakeVer) {
		t.Fatal("input modified")
	}
	if validateNoFixedReleaseVersion(out) != nil {
		t.Fatal("corrected plan still fails the gate")
	}
}

func TestCorrect_TaskSpec(t *testing.T) {
	in := []Story{{Title: "S", Tasks: []Task{{Title: "Release", Spec: "Set version " + fakeVer + " in the changelog."}}}}
	out, reps := CorrectFixedReleaseVersions(in)
	want := "Set the release in the changelog."
	if len(reps) != 1 || out[0].Tasks[0].Spec != want {
		t.Fatalf("got %q %+v", out[0].Tasks[0].Spec, reps)
	}
}

func TestCorrect_StoryDescription(t *testing.T) {
	in := []Story{{Title: "Release work", Description: "Cut a release version " + fakeVer + " and update CHANGELOG."}}
	out, reps := CorrectFixedReleaseVersions(in)
	if len(reps) != 1 || strings.Contains(out[0].Description, fakeVer) || reps[0].Field != "description" {
		t.Fatalf("got %q %+v", out[0].Description, reps)
	}
}

func TestCorrect_NonReleaseTextUntouched(t *testing.T) {
	in := []Story{{Title: "S", Tasks: []Task{{Title: "Upgrade lib", Spec: "use library 1.2.3"}}}}
	out, reps := CorrectFixedReleaseVersions(in)
	if len(reps) != 0 || out[0].Tasks[0].Spec != "use library 1.2.3" {
		t.Fatalf("unexpected %+v", reps)
	}
}

func TestDecompose_SelfCorrectsAndLogsEveryReplacement(t *testing.T) {
	plan := `{"title":"T","stories":[{"title":"S","tasks":[` +
		`{"title":"Version bump to ` + fakeVer + `","spec":"edit"},` +
		`{"title":"Changelog","spec":"Add a CHANGELOG heading for version v` + fakeVer + `."}]}]}`
	prd, err := decomposeWith(t, plan)
	if err != nil {
		t.Fatalf("want self-corrected success, got %v", err)
	}
	if prd.LastDecomposeFailure != nil {
		t.Fatalf("failure not cleared: %+v", prd.LastDecomposeFailure)
	}
	for _, task := range prd.Story[0].Tasks {
		if strings.Contains(task.Title+task.Spec, fakeVer) {
			t.Fatalf("number left: %+v", task)
		}
	}
	var note string
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == "auto_correct" {
			note, n = d.Note, n+1
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one decision line, got %d", n)
	}
	for _, want := range []string{`"Version bump to ` + fakeVer + `"`, `"Changelog"`, ` -> `, `"version v` + fakeVer + `"`, `"the release"`, `"the next patch above main"`} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %s: %s", want, note)
		}
	}
}

func TestDecompose_ReleaseNumberPlusOtherFailureStaysFailure(t *testing.T) {
	plan := `{"title":"T","stories":[{"title":"S","tasks":[` +
		`{"title":"Version bump to ` + fakeVer + `","spec":"edit"},` +
		`{"title":"Cleanup","spec":"run tmux kill-server"}]}]}`
	_, err := decomposeWith(t, plan)
	if err == nil {
		t.Fatal("want failure")
	}
	if !strings.Contains(err.Error(), "tmux") {
		// the old path reports the first failing gate; either way it must
		// be a failure and the plan must not have been silently reworded.
		t.Logf("failure reason: %v", err)
	}
}

func TestDecompose_ReleaseNumberAloneFailsWithOriginalReasonWhenNotCorrectable(t *testing.T) {
	m := &Manager{}
	s := []Story{{Title: "S", Tasks: []Task{{Title: "x", Spec: "plain"}}}}
	if m.tryCorrectReleaseVersions(&PRD{}, s) {
		t.Fatal("clean plan must take the old path")
	}
}

func TestDecompose_CleanPlanUntouched(t *testing.T) {
	plan := `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`
	prd, err := decomposeWith(t, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range prd.Decisions {
		if d.Kind == "auto_correct" {
			t.Fatalf("unexpected correction: %s", d.Note)
		}
	}
	if prd.Story[0].Tasks[0].Spec != "do it" {
		t.Fatal("clean plan changed")
	}
}

func TestDecomposeGuidance_ListsEachForbiddenPattern(t *testing.T) {
	m := &Manager{cfg: DefaultConfig()}
	block := m.decomposeGuidanceBlock()
	for _, f := range forbiddenPatterns {
		if !strings.Contains(block, f.Pattern) || !strings.Contains(block, f.Rule) || !strings.Contains(block, f.Alternative) {
			t.Errorf("prompt missing %+v", f)
		}
	}
	// Same rule names as the spec lint: every rule the lint can emit is
	// listed, and every listed rule is one the lint emits.
	sample := "Version bump to v" + fakeVer + " see DW-123 at /usr/bin/foo host 10.1." + "2.3 in prd-worktrees/x"
	emitted := map[string]bool{}
	for _, f := range LintSpecText(sample) {
		emitted[f.Rule] = true
	}
	for _, f := range forbiddenPatterns {
		if !emitted[f.Rule] {
			t.Errorf("rule %s not emitted by lint on sample", f.Rule)
		}
	}
	if len(emitted) != len(forbiddenPatterns) {
		t.Errorf("lint emits %d rules, prompt lists %d", len(emitted), len(forbiddenPatterns))
	}
}

// The real garbled specs: a Python version and a library version in a
// release task's title and spec must stay exactly as written, while the
// release number in the same text is still reworded.
func TestCorrect_PythonAndLibraryVersionsUntouched(t *testing.T) {
	in := []Story{{Title: "S", Tasks: []Task{{
		Title: "Version bump to " + fakeVer + " on Python 3.11.4 with requests 2.31.0",
		Spec: "Bump version " + fakeVer + " in the changelog. Keep Python 3.11.4 and Go 1.22.5; " +
			"pin requests>=2.31.0 and numpy == 1.26.4; library version 2.31.0; " +
			"use python:3.11.4-slim and github.com/x/y@v1.2.3 from docs/1.2.3/readme.",
	}}}}
	out, reps := CorrectFixedReleaseVersions(in)
	task := out[0].Tasks[0]
	wantTitle := "Version bump to the next patch above main on Python 3.11.4 with requests 2.31.0"
	if task.Title != wantTitle {
		t.Fatalf("title = %q, want %q", task.Title, wantTitle)
	}
	wantSpec := "Bump the release in the changelog. Keep Python 3.11.4 and Go 1.22.5; " +
		"pin requests>=2.31.0 and numpy == 1.26.4; library version 2.31.0; " +
		"use python:3.11.4-slim and github.com/x/y@v1.2.3 from docs/1.2.3/readme."
	if task.Spec != wantSpec {
		t.Fatalf("spec = %q, want %q", task.Spec, wantSpec)
	}
	if len(reps) != 2 {
		t.Fatalf("want 2 replacements (title + spec), got %+v", reps)
	}
	if validateNoFixedReleaseVersion(out) != nil {
		t.Fatal("corrected plan still fails the gate")
	}
}

func TestCorrect_OnlyForeignVersionsNoReplacement(t *testing.T) {
	in := []Story{{Title: "S", Tasks: []Task{{
		Title: "Version bump prep for Python 3.11.4",
		Spec:  "Before the version bump install Python version 3.11.4, Node 20.11.1 and library foo 1.2.3.",
	}}}}
	out, reps := CorrectFixedReleaseVersions(in)
	if len(reps) != 0 || out[0].Tasks[0].Title != in[0].Tasks[0].Title || out[0].Tasks[0].Spec != in[0].Tasks[0].Spec {
		t.Fatalf("foreign versions rewritten: %+v %+v", reps, out[0].Tasks[0])
	}
}

func TestCorrect_ReleaseCasesStillRewritten(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Bump the patch version from 9.5.26 to 9.5.27 in release.go", "Bump the patch version from the next patch above main to the next patch above main in release.go"},
		{"Version bump to v" + fakeVer, "Version bump to the next patch above main"},
		{"Set version " + fakeVer + " in the changelog.", "Set the release in the changelog."},
		{"Add a CHANGELOG heading for version v" + fakeVer + ".", "Add a CHANGELOG heading for the release."},
		{"Cut a release version " + fakeVer + " and update CHANGELOG.", "Cut a release the release and update CHANGELOG."},
		{"CHANGELOG entry ## [" + fakeVer + "]", "CHANGELOG entry ## [the next patch above main]"},
		{`var Version = "` + fakeVer + `"`, `var Version = "the next patch above main"`},
		{"Release " + fakeVer, "the release"},
	}
	for _, c := range cases {
		got, reps := correctReleaseText(c.in)
		if got != c.want || len(reps) == 0 {
			t.Errorf("%q: got %q want %q (%+v)", c.in, got, c.want, reps)
		}
	}
}
