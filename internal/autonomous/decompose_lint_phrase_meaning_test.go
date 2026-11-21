// decompose_lint_phrase_meaning_test.go — the phraseMeaningMatches
// helper: the spec anchors a checked phrase when it states the same
// words in any order and surrounding wording — lowercase or not,
// punctuation and markdown markers stripped, stop words dropped, simple
// plural/verb endings (s/es/ed/ing plus the silent-e fold) compared
// equal. A phrase with no content words never anchors anything, and a
// phrase whose content words the spec never states still fires the
// verify_script_text_not_in_spec rule end to end.
package autonomous

import (
	"strings"
	"testing"
)

func TestPhraseMeaningMatches(t *testing.T) {
	cases := []struct {
		name, spec, phrase string
		want               bool
	}{
		{
			name:   "verbatim containment",
			spec:   "write the Final answer line in the report",
			phrase: "Final answer",
			want:   true,
		},
		{
			name:   "case only",
			spec:   "the report must carry the final answer",
			phrase: "FINAL ANSWER",
			want:   true,
		},
		{
			name:   "reordered with stop words",
			spec:   "list the gate checks and record the report",
			phrase: "the report and the gate checks record",
			want:   true,
		},
		{
			name:   "plural folded to singular",
			spec:   "the report must carry a gates section",
			phrase: "gates",
			want:   true,
		},
		{
			name:   "ed folded to base",
			spec:   "the build passed the final checks",
			phrase: "pass the final checks",
			want:   true,
		},
		{
			name:   "ing folded to base",
			spec:   "the tool is writing the report",
			phrase: "write the report",
			want:   true,
		},
		{
			name:   "stop word difference only",
			spec:   "the report of the plan",
			phrase: "the plan, the report",
			want:   true,
		},
		{
			name:   "punctuation and markers stripped",
			spec:   "'## Gates' heading required",
			phrase: "gates",
			want:   true,
		},
		{
			name:   "absent content word never matches",
			spec:   "run the builds and log the output",
			phrase: "security review",
			want:   false,
		},
		{
			name:   "one present word is not enough",
			spec:   "run the builds and log the output",
			phrase: "the build security review",
			want:   false,
		},
		{
			name:   "similar word with a shared stem does not match",
			spec:   "run the gate checks",
			phrase: "gateways",
			want:   false,
		},
		{
			name:   "empty spec never matches",
			spec:   "",
			phrase: "Final answer",
			want:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := phraseMeaningMatches(c.spec, c.phrase); got != c.want {
				t.Fatalf("phraseMeaningMatches(%q, %q) = %v, want %v", c.spec, c.phrase, got, c.want)
			}
		})
	}
}

func TestPhraseMeaningMatchesNoContentWords(t *testing.T) {
	// Phrases with no content words never match, against any spec.
	for _, phrase := range []string{"   ", "the and of to", "a and the or"} {
		if phraseMeaningMatches("the gates and the reports and checks", phrase) {
			t.Errorf("phrase %q has no content words and must never match", phrase)
		}
		if phraseMeaningMatches("", phrase) {
			t.Errorf("phrase %q must not match an empty spec", phrase)
		}
	}
	// Marker-only phrases never match either.
	for _, phrase := range []string{"###", "#", "---", "=="} {
		if phraseMeaningMatches("gates reports checks of the and to", phrase) {
			t.Errorf("marker-only phrase %q must never match", phrase)
		}
	}
	// A marker plus a real word is content: it matches when the spec
	// states that word, and not when it does not.
	if !phraseMeaningMatches("carry a gates section", "### Gates") {
		t.Fatalf("marker plus content word must still anchor")
	}
	if phraseMeaningMatches("the build and its logs", "### Gates") {
		t.Fatalf("marker plus a word the spec never states must not match")
	}
}

func TestSpecAnchorsItemEquivalentWording(t *testing.T) {
	t.Run("heading whose word the spec states in other wording anchors", func(t *testing.T) {
		if !specAnchorsItem("the report must carry a gates section", "## Gates") {
			t.Fatalf("specAnchorsItem must anchor the heading by equivalent wording")
		}
		if !specAnchorsItem("the build passed the final checks", "## Final checks") {
			t.Fatalf("specAnchorsItem must fold the -ed ending of the heading's word")
		}
	})

	t.Run("heading the spec never states does not anchor", func(t *testing.T) {
		if specAnchorsItem("list the build logs", "## Gates") {
			t.Fatalf("specAnchorsItem must not anchor a heading the spec never names")
		}
	})

	t.Run("verbatim and # stripped still anchor", func(t *testing.T) {
		if !specAnchorsItem("the report must carry the ## Gates heading", "## Gates") {
			t.Fatalf("verbatim heading must anchor")
		}
		if !specAnchorsItem("the report must carry the Gates section", "## Gates") {
			t.Fatalf("# stripped spec wording must anchor")
		}
	})

	t.Run("empty or marker-only item never anchors", func(t *testing.T) {
		if specAnchorsItem("the report must carry gates", "") ||
			specAnchorsItem("the report must carry gates", "   ") {
			t.Fatalf("an empty item must never anchor")
		}
	})
}

func TestVerifyScriptTextNotInSpecPhraseMeaning(t *testing.T) {
	const id = "verify_script_text_not_in_spec"
	const heading = "# Summary"

	t.Run("script grepping a differently worded phrase still blocks", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q 'final verification summary' docs/result.md\n")
		task := stabilityTask("write the result document", stabilityFakeScriptPath)
		findings := stabilityLint(t, dir, id, task)
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1; got %+v", len(findings), findings)
		}
		if !strings.Contains(findings[0].Matched, "final verification summary") {
			t.Errorf("matched excerpt %q should name the checked phrase", findings[0].Matched)
		}
	})

	t.Run("spec stating the same words in other order anchors", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q 'final verification summary' docs/result.md\n")
		task := stabilityTask("the result document must print a verification summary, final", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (the spec states the same words)", fs)
		}
	})

	t.Run("spec naming the heading's word in other form anchors", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q '^"+heading+"' docs/result.md\n")
		// The checked text is the heading's word itself ("Summary"); the
		// spec states the same content word — "summary" — in other form,
		// so it anchors.
		task := stabilityTask("the document's summary must appear as a heading", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (the spec names the heading's word)", fs)
		}
	})

	t.Run("plurals between spec and script still anchor", func(t *testing.T) {
		dir := stabilityWrite(t, "#!/usr/bin/env bash\nset -eu\ngrep -q 'all gates listed' docs/result.md\n")
		task := stabilityTask("the result must list all gate entries", stabilityFakeScriptPath)
		if fs := stabilityLint(t, dir, id, task); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none (gates/gate fold to one stem)", fs)
		}
	})
}
