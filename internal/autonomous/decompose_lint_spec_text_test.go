// decompose_lint_spec_text_test.go — the check_matches_spec_text rule:
// a verification script (or the helper it sources) may only assert a
// markdown heading or the exact gate output formats "Final answer" /
// "Verification summary" when the task's own spec names that heading or
// format verbatim. Like the other verify-script stability tests the
// scripts live in a temp project directory and the engine reads them;
// no git, no daemon.
package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestCheckMatchesSpecText(t *testing.T) {
	rule := verifyStableRule(t, "check_matches_spec_text")
	if rule.Severity != config.DecomposeLintSeverityBlock {
		t.Fatalf("check_matches_spec_text severity = %q, want block", rule.Severity)
	}

	dir := t.TempDir()
	verifyStableWrite(t, dir, "v/anchored.sh", "#!/bin/sh\nout=.datawatch/reports/gate.md\ngrep -q '## Gates' \"out\" || exit 1\n")
	verifyStableWrite(t, dir, "v/final_answer.sh", "#!/bin/sh\nout=.datawatch/reports/task.md\ngrep -q 'Final answer' \"out\" || exit 1\n")
	verifyStableWrite(t, dir, "v/seven_hashes.sh", "#!/bin/sh\nout=.datawatch/reports/gate.md\ngrep -q '####### Too deep' \"out\" || exit 1\n")
	verifyStableWrite(t, dir, "v/seven_anchored.sh", "#!/bin/sh\nout=.datawatch/reports/plan.md\ngrep -q '####### Too deep' \"out\" || exit 1\n")
	verifyStableWrite(t, dir, "v/arbitrary.sh", "#!/bin/sh\nout=.datawatch/reports/task.md\ngrep -q 'done' \"out\" || exit 1\n")
	verifyStableWrite(t, dir, "v/echo_only.sh", "#!/bin/sh\necho '## Gates' > /dev/null\nls .datawatch/reports\n")
	verifyStableWrite(t, dir, "v/sed_address.sh", "#!/bin/sh\nout=.datawatch/reports/gate.md\nsed -n '/## Gates/p' \"$out\" | grep -q FINAL_GATE\n")
	verifyStableWrite(t, dir, "v/helper.sh", "#!/bin/sh\n. ./v/lib.sh\nrun_final_checks\n")
	verifyStableWrite(t, dir, "v/lib.sh", "grep -q '## Gates' report.md\n")

	for _, c := range []struct {
		name         string
		spec, script string
		want         int
		wantItem     string
		wantScript   string
	}{
		{
			name:   "a heading the spec itself names is anchored",
			spec:   "the report under .datawatch/reports/ must carry the '## Gates' heading",
			script: "v/anchored.sh",
			want:   0,
		},
		{
			name:       "a heading the spec never names blocks",
			spec:       "run the gate checks and write the report under .datawatch/reports/",
			script:     "v/anchored.sh",
			want:       1,
			wantItem:   "## Gates",
			wantScript: "v/anchored.sh",
		},
		{
			name:       "the Final answer format the spec never names blocks",
			spec:       "write the outcome to .datawatch/reports/task.md",
			script:     "v/final_answer.sh",
			want:       1,
			wantItem:   "Final answer",
			wantScript: "v/final_answer.sh",
		},
		{
			name:   "the Final answer format the spec names is anchored",
			spec:   "the report must print the Final answer line",
			script: "v/final_answer.sh",
			want:   0,
		},
		{
			name:   "seven hashes is not a markdown heading, unanchored or not",
			spec:   "write the outcome",
			script: "v/seven_hashes.sh",
			want:   0,
		},
		{
			name:   "the seven-hash line anchors in the spec and stays clean",
			spec:   "the report must print the '####### Too deep' line",
			script: "v/seven_anchored.sh",
			want:   0,
		},
		{
			name:   "an arbitrary word on a check line is never a match",
			spec:   "write the outcome",
			script: "v/arbitrary.sh",
			want:   0,
		},
		{
			name:   "a heading quoted on a non-check line is never a match",
			spec:   "list the reports",
			script: "v/echo_only.sh",
			want:   0,
		},
		{
			name:   "a spec that names the heading text without the hashes anchors the #-prefixed check",
			spec:   "the report must carry the Gates section",
			script: "v/anchored.sh",
			want:   0,
		},
		{
			name:       "a sed address heading the spec never names blocks",
			spec:       "verify the gate report",
			script:     "v/sed_address.sh",
			want:       1,
			wantItem:   "## Gates",
			wantScript: "v/sed_address.sh",
		},
		{
			name:   "a heading the spec names anchors a sourced helper's identical check",
			spec:   "the report must carry the ## Gates heading",
			script: "v/helper.sh",
			want:   0,
		},
		{
			name:       "an unanchored heading in a sourced helper blocks, naming the helper",
			spec:       "run the checks and record the report",
			script:     "v/helper.sh",
			want:       1,
			wantItem:   "## Gates",
			wantScript: "v/lib.sh",
		},
		{
			name:   "a script that does not exist yet is skipped, never guessed",
			spec:   "anything",
			script: "v/missing.sh",
			want:   0,
		},
	} {
		got := verifyStableLint(dir, c.name, rule, Task{ID: "t", Spec: c.spec, VerificationScript: c.script})
		if len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %+v", c.name, len(got), c.want, got)
			continue
		}
		if c.want == 1 {
			f := got[0]
			if f.RuleID != "check_matches_spec_text" || f.Severity != config.DecomposeLintSeverityBlock {
				t.Errorf("%s: finding %+v, want a block finding for check_matches_spec_text", c.name, f)
			}
			if f.Script != c.wantScript {
				t.Errorf("%s: finding script path = %q, want %q", c.name, f.Script, c.wantScript)
			}
			if !strings.Contains(f.Matched, c.wantItem) {
				t.Errorf("%s: matched %q does not name the unanchored item %q", c.name, f.Matched, c.wantItem)
			}
		}
	}
}
