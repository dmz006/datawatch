package autonomous

import (
	"reflect"
	"testing"
)

func TestLintSpecText_Kinds(t *testing.T) {
	cases := []struct {
		name string
		text string
		want SpecFinding
	}{
		{"fixed release number", "Do the work, then a version bump to version 9.120.0 in the changelog.",
			SpecFinding{Rule: SpecRuleReleaseVersionFixed, Phrase: "version 9.120.0", Severity: SpecSeverityWarning, Correctable: true}},
		{"tracker id", "Fix the crash described in BL123 for the Automaton.",
			SpecFinding{Rule: SpecRuleTrackerID, Phrase: "BL123", Severity: SpecSeverityWarning, Correctable: true}},
		{"system path", "Run /usr/bin/time around the build.",
			SpecFinding{Rule: SpecRuleSystemPath, Phrase: "/usr/bin/time", Severity: SpecSeverityWarning, Correctable: true}},
		{"private address", "Connect to 192.168." + "1.20 for the check.",
			SpecFinding{Rule: SpecRulePrivateAddress, Phrase: "192.168." + "1.20", Severity: SpecSeverityError, Correctable: false}},
		{"worktree path", "Edit /ho" + "me/someone/.datawatch/prd-worktrees/abc/def/main.go directly.",
			SpecFinding{Rule: SpecRuleWorktreePath, Phrase: "/ho" + "me/someone/.datawatch/prd-worktrees/abc/def/main.go", Severity: SpecSeverityError, Correctable: false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := LintSpecText(c.text)
			if len(got) != 1 {
				t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
			}
			if got[0].Suggestion == "" {
				t.Errorf("empty suggestion")
			}
			got[0].Suggestion = ""
			if !reflect.DeepEqual(got[0], c.want) {
				t.Errorf("got %+v want %+v", got[0], c.want)
			}
		})
	}
}

func TestLintSpecText_Clean(t *testing.T) {
	for _, s := range []string{
		"",
		"Add a helper in internal/foo and cover it with a table test.",
		"Upgrade to Go 1.22 and call 8.8.8.8 for DNS.", // two-part version, public address
		"Mention version 9.5.27 of a library while doing unrelated work.",
	} {
		if got := LintSpecText(s); len(got) != 0 {
			t.Errorf("%q: want none, got %+v", s, got)
		}
	}
}

func TestLintSpecText_SeveralInOrder(t *testing.T) {
	text := "Per GH456 call /usr/bin/foo against 10.0." + "0.5, then bump version to 9.9.9 in the changelog."
	got := LintSpecText(text)
	var rules []string
	for _, f := range got {
		rules = append(rules, f.Rule)
	}
	want := []string{SpecRuleTrackerID, SpecRuleSystemPath, SpecRulePrivateAddress, SpecRuleReleaseVersionFixed}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules %v want %v", rules, want)
	}
}

func TestLintSpecText_Deterministic(t *testing.T) {
	text := "see BL99 and 172.16." + "0.1"
	if !reflect.DeepEqual(LintSpecText(text), LintSpecText(text)) {
		t.Fatal("not deterministic")
	}
}
